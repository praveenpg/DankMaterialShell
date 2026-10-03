//go:build kwin_integration

// Runs the bridge against a headless kwin_wayland with two virtual outputs:
//
//	go test -tags kwin_integration ./internal/server/kwin/
package kwin

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

type session struct {
	t    *testing.T
	env  []string
	addr string
	kwin *exec.Cmd
	conn *dbus.Conn
}

// sandboxEnv isolates every XDG dir: kwin_wayland and anything the private bus
// activates otherwise rewrite the real kwinrc and kglobalshortcutsrc.
func sandboxEnv(t *testing.T) []string {
	root := t.TempDir()
	env := []string{}
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		if slices.Contains([]string{"DISPLAY", "WAYLAND_DISPLAY", "DBUS_SESSION_BUS_ADDRESS"}, key) || strings.HasPrefix(key, "XDG_") && key != "XDG_RUNTIME_DIR" {
			continue
		}
		env = append(env, kv)
	}
	for _, dir := range []string{"CONFIG", "DATA", "STATE", "CACHE"} {
		path := filepath.Join(root, strings.ToLower(dir))
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
		env = append(env, "XDG_"+dir+"_HOME="+path)
	}
	return env
}

func startSession(t *testing.T) *session {
	if _, err := exec.LookPath("kwin_wayland"); err != nil {
		t.Skip("kwin_wayland not installed")
	}
	s := &session{t: t, env: sandboxEnv(t)}

	bus := exec.Command("dbus-daemon", "--session", "--nofork", "--print-address=1")
	bus.Env = s.env
	out, err := bus.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := bus.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { bus.Process.Kill(); bus.Wait() })
	s.addr, err = bufio.NewReader(out).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	s.addr = strings.TrimSpace(s.addr)
	s.env = append(s.env, "DBUS_SESSION_BUS_ADDRESS="+s.addr)
	s.conn = s.connect()
	s.startKwin()
	t.Cleanup(s.stopKwin)
	return s
}

func (s *session) connect() *dbus.Conn {
	conn, err := dbus.Connect(s.addr)
	if err != nil {
		s.t.Fatal(err)
	}
	return conn
}

func (s *session) kwinUp() bool {
	var owned bool
	err := s.conn.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, kwinService).Store(&owned)
	return err == nil && owned
}

func (s *session) startKwin() {
	s.kwin = exec.Command("kwin_wayland", "--virtual", "--no-lockscreen", "--no-global-shortcuts",
		"--socket", fmt.Sprintf("wayland-dms-kwin-test-%d", os.Getpid()),
		"--output-count", "2", "--width", "1280", "--height", "800")
	s.kwin.Env = s.env
	if err := s.kwin.Start(); err != nil {
		s.t.Fatal(err)
	}
	waitUntil(s.t, "kwin on the bus", s.kwinUp)
}

func (s *session) stopKwin() {
	if s.kwin == nil {
		return
	}
	s.kwin.Process.Kill()
	s.kwin.Wait()
	s.kwin = nil
}

func (s *session) vdm(method string, args ...any) {
	if err := s.conn.Object(kwinService, "/VirtualDesktopManager").Call("org.kde.KWin.VirtualDesktopManager."+method, 0, args...).Err; err != nil {
		s.t.Fatalf("%s: %v", method, err)
	}
}

func (s *session) setPerOutputDesktops(enabled bool) {
	for _, kv := range s.env {
		if path, ok := strings.CutPrefix(kv, "XDG_CONFIG_HOME="); ok {
			if err := os.WriteFile(filepath.Join(path, "kwinrc"), []byte(fmt.Sprintf("[Windows]\nPerOutputVirtualDesktops=%t\n", enabled)), 0o600); err != nil {
				s.t.Fatal(err)
			}
		}
	}
	if err := s.conn.Object(kwinService, "/KWin").Call("org.kde.KWin.reconfigure", 0).Err; err != nil {
		s.t.Fatal(err)
	}
}

func waitUntil(t *testing.T, label string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", label)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func desktopID(s State, name string) string {
	for _, d := range s.Desktops {
		if d.Name == name {
			return d.ID
		}
	}
	return ""
}

func currentDesktops(s State) string {
	parts := []string{}
	for _, o := range s.Outputs {
		parts = append(parts, o.Name+"="+o.CurrentDesktop)
	}
	return strings.Join(parts, " ")
}

func TestBridgeAgainstHeadlessKwin(t *testing.T) {
	s := startSession(t)
	m, err := NewManager(s.connect(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)

	waitUntil(t, "snapshot with two outputs", func() bool {
		st := m.GetState()
		return st.Available && len(st.Outputs) == 2 && len(st.Desktops) == 1
	})
	one := m.GetState().Desktops[0].ID

	s.vdm("createDesktop", uint32(1), "Two")
	waitUntil(t, "new desktop reported", func() bool { return desktopID(m.GetState(), "Two") != "" })
	two := desktopID(m.GetState(), "Two")

	if err := m.SwitchDesktop(two, "Virtual-1"); err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("Virtual-0=%s Virtual-1=%s", two, two)
	waitUntil(t, "global mode moves every output", func() bool { return currentDesktops(m.GetState()) == want })

	if err := m.RenameDesktop(one, "One"); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "rename reported", func() bool { return desktopID(m.GetState(), "One") == one })

	s.setPerOutputDesktops(true)
	waitUntil(t, "per-output mode reported", func() bool { return m.GetState().PerOutputDesktops })
	if err := m.SwitchDesktop(one, "Virtual-1"); err != nil {
		t.Fatal(err)
	}
	want = fmt.Sprintf("Virtual-0=%s Virtual-1=%s", two, one)
	waitUntil(t, "only the non-active output switches", func() bool { return currentDesktops(m.GetState()) == want })

	if err := m.SwitchDesktop("{missing}", ""); err == nil || !strings.Contains(err.Error(), "unknown desktop") {
		t.Fatalf("SwitchDesktop(missing) error = %v, want unknown desktop", err)
	}

	updates := m.Subscribe("idle")
	time.Sleep(1500 * time.Millisecond)
	if n := len(updates); n != 0 {
		t.Fatalf("idle bridge pushed %d updates in 1.5 s, want 0", n)
	}
	m.Unsubscribe("idle")

	s.stopKwin()
	waitUntil(t, "state cleared when kwin exits", func() bool { return !m.GetState().Available })
	s.startKwin()
	waitUntil(t, "bridge reloaded after kwin restart", func() bool {
		st := m.GetState()
		return st.Available && st.PerOutputDesktops && len(st.Outputs) == 2
	})
}
