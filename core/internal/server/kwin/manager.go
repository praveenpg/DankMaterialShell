package kwin

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/AvengeMedia/DankMaterialShell/core/internal/log"
	"github.com/AvengeMedia/DankMaterialShell/core/internal/screenshot"
	"github.com/AvengeMedia/DankMaterialShell/core/internal/utils"
	"github.com/AvengeMedia/dankgo/syncmap"
	"github.com/godbus/dbus/v5"
)

const (
	busName       = "com.danklinux.KWinBridge"
	objectPath    = dbus.ObjectPath("/com/danklinux/KWinBridge")
	interfaceName = "com.danklinux.KWinBridge1"

	kwinService = "org.kde.KWin"
	scriptName  = "dms-kwin-bridge"
	resultWait  = 2 * time.Second
	maxReorder  = 64
)

//go:embed bridge.js
var bridgeJS []byte

//go:embed command.js
var commandJS string

type Manager struct {
	conn *dbus.Conn
	dir  string

	stateMutex sync.RWMutex
	state      State
	generation uint64
	nextSeq    uint64
	early      map[uint64]event

	subscribers syncmap.Map[string, chan State]
	pending     syncmap.Map[uint64, chan commandResult]
	nextID      atomic.Uint64

	signals chan *dbus.Signal
	done    chan struct{}
	wg      sync.WaitGroup
}

// Start runs the bridge on its own session bus connection when DMS is under KWin.
func Start() (*Manager, error) {
	if screenshot.DetectCompositor() != screenshot.CompositorKwin {
		return nil, errors.New("compositor is not kwin")
	}
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp(utils.RuntimeDir(), "dms-kwin-")
	if err != nil {
		conn.Close()
		return nil, err
	}
	m, err := NewManager(conn, dir)
	if err != nil {
		conn.Close()
		os.RemoveAll(dir)
		return nil, err
	}
	return m, nil
}

// NewManager takes ownership of conn and dir: Close closes the connection and removes the directory.
func NewManager(conn *dbus.Conn, dir string) (*Manager, error) {
	m := &Manager{
		conn:    conn,
		dir:     dir,
		early:   map[uint64]event{},
		signals: make(chan *dbus.Signal, 16),
		done:    make(chan struct{}),
	}

	if err := conn.Export(bridgeHandler{m}, objectPath, interfaceName); err != nil {
		return nil, fmt.Errorf("export bridge object: %w", err)
	}
	reply, err := conn.RequestName(busName, dbus.NameFlagDoNotQueue)
	if err != nil {
		return nil, fmt.Errorf("request %s: %w", busName, err)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		return nil, fmt.Errorf("%s is owned by another DMS instance", busName)
	}

	if err := conn.AddMatchSignal(
		dbus.WithMatchSender("org.freedesktop.DBus"),
		dbus.WithMatchMember("NameOwnerChanged"),
		dbus.WithMatchArg(0, kwinService),
	); err != nil {
		return nil, fmt.Errorf("watch %s: %w", kwinService, err)
	}
	conn.Signal(m.signals)
	m.wg.Go(m.watchKwin)

	if m.kwinRunning() {
		m.loadBridge()
	}
	return m, nil
}

func (m *Manager) GetState() State {
	m.stateMutex.RLock()
	defer m.stateMutex.RUnlock()
	return m.state
}

func (m *Manager) Subscribe(id string) chan State {
	ch := make(chan State, 64)
	m.subscribers.Store(id, ch)
	return ch
}

func (m *Manager) Unsubscribe(id string) {
	if ch, ok := m.subscribers.LoadAndDelete(id); ok {
		close(ch)
	}
}

// SwitchDesktop switches one output, or the active output when output is empty.
// In KWin's default global mode every output follows either way.
func (m *Manager) SwitchDesktop(desktop, output string) error {
	return m.runCommand(map[string]any{"op": "switchDesktop", "desktop": desktop, "output": output})
}

func (m *Manager) RenameDesktop(desktop, name string) error {
	return m.conn.Object(kwinService, "/VirtualDesktopManager").
		Call("org.kde.KWin.VirtualDesktopManager.setDesktopName", 0, desktop, name).Err
}

func (m *Manager) Close() {
	select {
	case <-m.done:
		return
	default:
		close(m.done)
	}
	m.conn.RemoveSignal(m.signals)
	m.wg.Wait()
	if m.kwinRunning() {
		m.unloadScript(scriptName)
	}
	m.conn.Close()
	os.RemoveAll(m.dir)
	m.subscribers.Range(func(id string, ch chan State) bool {
		close(ch)
		m.subscribers.Delete(id)
		return true
	})
}

func (m *Manager) watchKwin() {
	for {
		select {
		case <-m.done:
			return
		case sig := <-m.signals:
			if sig == nil || sig.Name != "org.freedesktop.DBus.NameOwnerChanged" || len(sig.Body) < 3 {
				continue
			}
			if name, _ := sig.Body[0].(string); name != kwinService {
				continue
			}
			if owner, _ := sig.Body[2].(string); owner != "" {
				m.loadBridge()
				continue
			}
			m.setState(State{})
		}
	}
}

func (m *Manager) kwinRunning() bool {
	var owned bool
	err := m.conn.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, kwinService).Store(&owned)
	return err == nil && owned
}

func (m *Manager) loadBridge() {
	m.stateMutex.Lock()
	m.generation++
	m.nextSeq = 1
	m.early = map[uint64]event{}
	source := fmt.Sprintf("var GENERATION = %d;\n%s", m.generation, bridgeJS)
	m.stateMutex.Unlock()

	m.unloadScript(scriptName)
	if err := os.WriteFile(m.scriptPath(scriptName), []byte(source), 0o600); err != nil {
		log.Warn("kwin: failed to write bridge script: %v", err)
		return
	}
	if err := m.loadScript(scriptName); err != nil {
		log.Warn("kwin: failed to load bridge script: %v", err)
	}
}

func (m *Manager) loadScript(name string) error {
	scripting := m.conn.Object(kwinService, "/Scripting")
	var num int32
	if err := scripting.Call("org.kde.kwin.Scripting.loadScript", 0, m.scriptPath(name), name).Store(&num); err != nil {
		return err
	}
	if num < 0 {
		return fmt.Errorf("loadScript %s returned %d", name, num)
	}
	script := m.conn.Object(kwinService, dbus.ObjectPath(fmt.Sprintf("/Scripting/Script%d", num)))
	return script.Call("org.kde.kwin.Script.run", 0).Err
}

func (m *Manager) unloadScript(name string) {
	m.conn.Object(kwinService, "/Scripting").Call("org.kde.kwin.Scripting.unloadScript", 0, name)
}

func (m *Manager) scriptPath(name string) string {
	return filepath.Join(m.dir, name+".js")
}

func (m *Manager) runCommand(cmd map[string]any) error {
	id := m.nextID.Add(1)
	cmd["id"] = id
	payload, err := json.Marshal(cmd)
	if err != nil {
		return err
	}

	name := fmt.Sprintf("%s-command-%d", scriptName, id)
	if err := os.WriteFile(m.scriptPath(name), []byte(commandJS+"\nrun("+string(payload)+");\n"), 0o600); err != nil {
		return err
	}
	defer os.Remove(m.scriptPath(name))

	result := make(chan commandResult, 1)
	m.pending.Store(id, result)
	defer m.pending.Delete(id)

	if err := m.loadScript(name); err != nil {
		m.unloadScript(name)
		return err
	}
	defer m.unloadScript(name)

	select {
	case r := <-result:
		if !r.OK {
			return errors.New(r.Error)
		}
		return nil
	case <-time.After(resultWait):
		return errors.New("kwin did not answer")
	}
}

func (m *Manager) setState(s State) {
	m.stateMutex.Lock()
	m.state = s
	m.stateMutex.Unlock()
	m.broadcast(s)
}

func (m *Manager) broadcast(s State) {
	m.subscribers.Range(func(_ string, ch chan State) bool {
		select {
		case ch <- s:
		default:
		}
		return true
	})
}

func (m *Manager) handleEvent(payload string) {
	var ev event
	if err := json.Unmarshal([]byte(payload), &ev); err != nil {
		log.Warn("kwin: bad bridge event: %v", err)
		return
	}

	m.stateMutex.Lock()
	if ev.Generation != m.generation || ev.Seq < m.nextSeq {
		m.stateMutex.Unlock()
		return
	}
	m.early[ev.Seq] = ev
	if len(m.early) > maxReorder {
		// A lost event would stall the sequence forever; resume from the oldest held one.
		m.nextSeq = minKey(m.early)
	}
	changed := false
	for {
		next, ok := m.early[m.nextSeq]
		if !ok {
			break
		}
		delete(m.early, m.nextSeq)
		m.nextSeq++
		m.state = apply(m.state, next)
		changed = true
	}
	s := m.state
	m.stateMutex.Unlock()

	if changed {
		m.broadcast(s)
	}
}

func minKey(events map[uint64]event) uint64 {
	var lowest uint64
	for seq := range events {
		if lowest == 0 || seq < lowest {
			lowest = seq
		}
	}
	return lowest
}

// bridgeHandler is the exported D-Bus object; only its methods are callable by the script.
type bridgeHandler struct{ m *Manager }

func (h bridgeHandler) Event(payload string) *dbus.Error {
	h.m.handleEvent(payload)
	return nil
}

func (h bridgeHandler) Result(payload string) *dbus.Error {
	var r commandResult
	if err := json.Unmarshal([]byte(payload), &r); err != nil {
		return nil
	}
	if ch, ok := h.m.pending.Load(r.ID); ok {
		select {
		case ch <- r:
		default:
		}
	}
	return nil
}
