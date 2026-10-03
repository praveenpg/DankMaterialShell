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
	"syscall"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

type session struct {
	t          *testing.T
	lockscreen bool
	env        []string
	addr       string
	socket     string
	kwin       *exec.Cmd
	conn       *dbus.Conn
}

// sandboxEnv isolates every XDG dir: kwin_wayland and anything the private bus
// activates otherwise rewrite the real kwinrc and leave sockets in the real runtime dir.
func sandboxEnv(t *testing.T) []string {
	root := t.TempDir()
	env := []string{}
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		if slices.Contains([]string{"DISPLAY", "WAYLAND_DISPLAY", "DBUS_SESSION_BUS_ADDRESS"}, key) || strings.HasPrefix(key, "XDG_") {
			continue
		}
		env = append(env, kv)
	}
	for _, dir := range []string{"CONFIG", "DATA", "STATE", "CACHE", "RUNTIME"} {
		path := filepath.Join(root, strings.ToLower(dir))
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
		suffix := "_HOME="
		if dir == "RUNTIME" {
			suffix = "_DIR="
		}
		env = append(env, "XDG_"+dir+suffix+path)
	}
	env = append(env, "MESA_SHADER_CACHE_DISABLE=true")
	// A document portal activated on the private bus FUSE-mounts runtime/doc, which TempDir cannot remove.
	t.Cleanup(func() { exec.Command("fusermount3", "-uz", filepath.Join(root, "runtime", "doc")).Run() })
	return env
}

func startSession(t *testing.T) *session {
	return startSessionWith(t, false)
}

func startSessionWith(t *testing.T, lockscreen bool) *session {
	if _, err := exec.LookPath("kwin_wayland"); err != nil {
		t.Skip("kwin_wayland not installed")
	}
	s := &session{t: t, env: sandboxEnv(t), lockscreen: lockscreen}

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
	s.socket = fmt.Sprintf("wayland-dms-kwin-test-%d", os.Getpid())
	args := []string{"--virtual", "--no-global-shortcuts", "--socket", s.socket, "--output-count", "2", "--width", "1280", "--height", "800"}
	if !s.lockscreen {
		args = append(args, "--no-lockscreen")
	}
	s.kwin = exec.Command("kwin_wayland", args...)
	s.kwin.Env = s.env
	if err := s.kwin.Start(); err != nil {
		s.t.Fatal(err)
	}
	waitUntil(s.t, "kwin on the bus", s.kwinUp)
}

// stopKwin terminates rather than kills, so KWin takes its lock screen greeter down with it.
func (s *session) stopKwin() {
	if s.kwin == nil {
		return
	}
	s.kwin.Process.Signal(syscall.SIGTERM)
	done := make(chan struct{})
	go func() { s.kwin.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		s.kwin.Process.Kill()
		<-done
	}
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

func (s *session) openWindow(class string) {
	client := exec.Command("alacritty", "--class", class, "--title", class, "-e", "sleep", "600")
	client.Env = append(slices.Clone(s.env), "WAYLAND_DISPLAY="+s.socket)
	if err := client.Start(); err != nil {
		s.t.Fatal(err)
	}
	s.t.Cleanup(func() { client.Process.Kill(); client.Wait() })
}

func windowByApp(st State, app string) *Window {
	for i := range st.Windows {
		if st.Windows[i].AppID == app {
			return &st.Windows[i]
		}
	}
	return nil
}

func TestWindowActionsTargetOnlyTheNamedWindow(t *testing.T) {
	if _, err := exec.LookPath("alacritty"); err != nil {
		t.Skip("alacritty not installed")
	}
	s := startSession(t)
	m, err := NewManager(s.connect(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	waitUntil(t, "snapshot", func() bool { return m.GetState().Available })

	s.openWindow("BridgeBack")
	waitUntil(t, "background window", func() bool { return windowByApp(m.GetState(), "BridgeBack") != nil })
	s.openWindow("BridgeFront")
	waitUntil(t, "focused window", func() bool {
		w := windowByApp(m.GetState(), "BridgeFront")
		return w != nil && w.Active
	})
	back := windowByApp(m.GetState(), "BridgeBack").ID
	front := windowByApp(m.GetState(), "BridgeFront").ID

	frontUntouched := func(label string) {
		t.Helper()
		w := windowByApp(m.GetState(), "BridgeFront")
		if w == nil || !w.Active || w.Minimized || w.Maximized || w.Output != "Virtual-0" {
			t.Fatalf("%s changed the focused window: %+v", label, w)
		}
	}

	steps := []struct {
		action string
		args   map[string]any
		done   func(*Window) bool
	}{
		{"setMinimized", map[string]any{"value": true}, func(w *Window) bool { return w.Minimized }},
		{"setMinimized", map[string]any{"value": true}, func(w *Window) bool { return w.Minimized }},
		{"setMaximized", map[string]any{"value": true}, func(w *Window) bool { return w.Maximized }},
		{"moveToOutput", map[string]any{"output": "Virtual-1"}, func(w *Window) bool { return w.Output == "Virtual-1" }},
	}
	for _, step := range steps {
		if err := m.WindowAction(back, step.action, step.args); err != nil {
			t.Fatalf("%s: %v", step.action, err)
		}
		waitUntil(t, step.action, func() bool { return step.done(windowByApp(m.GetState(), "BridgeBack")) })
		frontUntouched(step.action)
	}

	for _, bad := range []struct {
		window, action string
		args           map[string]any
		want           string
	}{
		{"{missing}", "close", nil, "unknown window"},
		{back, "explode", nil, "unknown window action"},
		{back, "setMinimized", map[string]any{"value": "yes"}, "boolean"},
		{back, "moveToDesktop", map[string]any{"desktop": "{missing}"}, "unknown desktop"},
	} {
		if err := m.WindowAction(bad.window, bad.action, bad.args); err == nil || !strings.Contains(err.Error(), bad.want) {
			t.Fatalf("WindowAction(%s, %s) error = %v, want %q", bad.window, bad.action, err, bad.want)
		}
	}

	if err := m.WindowAction(back, "activate", nil); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "activate restores and focuses", func() bool {
		w := windowByApp(m.GetState(), "BridgeBack")
		return w.Active && !w.Minimized
	})

	if err := m.WindowAction(front, "close", nil); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "closed window removed", func() bool { return windowByApp(m.GetState(), "BridgeFront") == nil })
	if windowByApp(m.GetState(), "BridgeBack") == nil {
		t.Fatal("closing one window removed the other")
	}
}

func TestLogoutClosesWindowsThenQuitsKwin(t *testing.T) {
	if _, err := exec.LookPath("alacritty"); err != nil {
		t.Skip("alacritty not installed")
	}
	s := startSession(t)
	m, err := NewManager(s.connect(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	waitUntil(t, "snapshot", func() bool { return m.GetState().Available })
	s.openWindow("BridgeLogout")
	waitUntil(t, "window", func() bool { return windowByApp(m.GetState(), "BridgeLogout") != nil })

	exited := make(chan struct{})
	kwin := s.kwin
	go func() { kwin.Wait(); close(exited) }()
	if err := m.Logout(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-exited:
	case <-time.After(10 * time.Second):
		t.Fatal("kwin did not quit after logout")
	}
	s.kwin = nil
}

func TestLockUsesKwinsScreenLocker(t *testing.T) {
	s := startSessionWith(t, true)
	m, err := NewManager(s.connect(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)

	active := func() bool {
		var on bool
		err := s.conn.Object(kwinService, "/ScreenSaver").Call("org.freedesktop.ScreenSaver.GetActive", 0).Store(&on)
		return err == nil && on
	}
	if active() {
		t.Fatal("screen locked before Lock")
	}
	if err := m.Lock(); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "KWin screen locker active", active)
}
