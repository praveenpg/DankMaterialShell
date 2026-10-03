package wlroutput

import (
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/AvengeMedia/DankMaterialShell/core/internal/kscreen"
	"github.com/AvengeMedia/dankgo/syncmap"
)

// Backend is what the socket handlers and server need from an output manager.
type Backend interface {
	GetState() State
	Subscribe(id string) chan State
	Unsubscribe(id string)
	ApplyConfiguration(heads []HeadConfig, test bool) error
	FatalError() <-chan error
	Close()
}

// KscreenManager serves KWin outputs, which have no wlr-output-management, through
// kscreen-doctor. It refreshes on demand rather than polling.
type KscreenManager struct {
	mu        sync.Mutex
	state     State
	outputs   []kscreen.Output
	refreshed time.Time

	subscribers syncmap.Map[string, chan State]
	fatal       chan error
}

const kscreenStateTTL = time.Second

func NewKscreenManager() (*KscreenManager, error) {
	m := &KscreenManager{fatal: make(chan error)}
	if err := m.refresh(); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *KscreenManager) GetState() State {
	m.mu.Lock()
	stale := time.Since(m.refreshed) > kscreenStateTTL
	m.mu.Unlock()
	if stale {
		if err := m.refresh(); err != nil {
			return m.cachedState()
		}
	}
	return m.cachedState()
}

func (m *KscreenManager) cachedState() State {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state
}

func (m *KscreenManager) Subscribe(id string) chan State {
	ch := make(chan State, 16)
	m.subscribers.Store(id, ch)
	return ch
}

func (m *KscreenManager) Unsubscribe(id string) {
	if ch, ok := m.subscribers.LoadAndDelete(id); ok {
		close(ch)
	}
}

func (m *KscreenManager) FatalError() <-chan error { return m.fatal }

func (m *KscreenManager) Close() {
	m.subscribers.Range(func(id string, ch chan State) bool {
		close(ch)
		m.subscribers.Delete(id)
		return true
	})
}

// ApplyConfiguration has no dry run in kscreen-doctor, so a test only validates the request.
func (m *KscreenManager) ApplyConfiguration(heads []HeadConfig, test bool) error {
	m.mu.Lock()
	outputs := m.outputs
	m.mu.Unlock()

	args, err := kscreenConfigArgs(heads, outputs)
	if err != nil || test || len(args) == 0 {
		return err
	}
	if _, err := kscreen.Run(args...); err != nil {
		return err
	}
	if err := m.refresh(); err != nil {
		return err
	}
	state := m.cachedState()
	m.subscribers.Range(func(_ string, ch chan State) bool {
		select {
		case ch <- state:
		default:
		}
		return true
	})
	return nil
}

func (m *KscreenManager) refresh() error {
	outputs, err := kscreen.Outputs()
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.outputs = outputs
	m.state = stateFromKscreen(outputs, m.state.Serial+1)
	m.refreshed = time.Now()
	return nil
}

// kscreenModeID packs the output and kscreen mode ids so ids stay unique and stable across refreshes.
func kscreenModeID(output kscreen.Output, index int) uint32 {
	id, err := strconv.Atoi(output.Modes[index].ID)
	if err != nil || id < 0 || id >= 1<<16 {
		id = index
	}
	return uint32(output.ID)<<16 | uint32(id)
}

func stateFromKscreen(outputs []kscreen.Output, serial uint32) State {
	state := State{Outputs: make([]Output, 0, len(outputs)), Serial: serial}
	for _, o := range outputs {
		out := Output{
			Name:           o.Name,
			Description:    o.Name,
			PhysicalWidth:  o.SizeMM.Width,
			PhysicalHeight: o.SizeMM.Height,
			Enabled:        o.Enabled,
			X:              o.Pos.X,
			Y:              o.Pos.Y,
			Transform:      kscreen.TransformFromRotation(o.Rotation),
			Scale:          o.Scale,
			ID:             uint32(o.ID),
		}
		for i, mode := range o.Modes {
			wm := OutputMode{
				Width:   mode.Size.Width,
				Height:  mode.Size.Height,
				Refresh: int32(mode.RefreshRate*1000 + 0.5),
				ID:      kscreenModeID(o, i),
			}
			for _, p := range o.PreferredModes {
				wm.Preferred = wm.Preferred || p == mode.ID
			}
			out.Modes = append(out.Modes, wm)
			if mode.ID == o.CurrentModeID {
				current := wm
				out.CurrentMode = &current
			}
		}
		state.Outputs = append(state.Outputs, out)
	}
	return state
}

func kscreenConfigArgs(heads []HeadConfig, outputs []kscreen.Output) ([]string, error) {
	byName := map[string]kscreen.Output{}
	for _, o := range outputs {
		byName[o.Name] = o
	}
	var args []string
	enabled := 0
	for _, h := range heads {
		o, ok := byName[h.Name]
		if !ok {
			return nil, fmt.Errorf("output %q not found", h.Name)
		}
		prefix := "output." + h.Name + "."
		if !h.Enabled {
			args = append(args, prefix+"disable")
			continue
		}
		enabled++
		args = append(args, prefix+"enable")

		if mode, err := kscreenHeadMode(h, o); err != nil {
			return nil, err
		} else if mode != "" {
			args = append(args, prefix+"mode."+mode)
		}
		if h.Position != nil {
			args = append(args, fmt.Sprintf("%sposition.%d,%d", prefix, h.Position.X, h.Position.Y))
		}
		if h.Scale != nil {
			if *h.Scale <= 0 {
				return nil, fmt.Errorf("invalid scale %g for %s", *h.Scale, h.Name)
			}
			args = append(args, prefix+"scale."+strconv.FormatFloat(*h.Scale, 'f', -1, 64))
		}
		if h.Transform != nil {
			rotation, err := kscreen.RotationName(*h.Transform)
			if err != nil {
				return nil, err
			}
			args = append(args, prefix+"rotation."+rotation)
		}
		if h.AdaptiveSync != nil && *h.AdaptiveSync != 0 {
			return nil, errors.New("adaptive sync is not supported on KWin")
		}
	}
	if len(heads) > 0 && enabled == 0 {
		return nil, errors.New("at least one output must stay enabled")
	}
	return args, nil
}

func kscreenHeadMode(h HeadConfig, o kscreen.Output) (string, error) {
	switch {
	case h.ModeID != nil:
		for i, mode := range o.Modes {
			if kscreenModeID(o, i) == *h.ModeID {
				return mode.ID, nil
			}
		}
		return "", fmt.Errorf("mode %d not found on %s", *h.ModeID, h.Name)
	case h.CustomMode != nil:
		for _, mode := range o.Modes {
			if mode.Size.Width == h.CustomMode.Width && mode.Size.Height == h.CustomMode.Height &&
				int32(mode.RefreshRate*1000+0.5) == h.CustomMode.Refresh {
				return mode.ID, nil
			}
		}
		return "", fmt.Errorf("custom modes are not supported on KWin (%dx%d@%d on %s)",
			h.CustomMode.Width, h.CustomMode.Height, h.CustomMode.Refresh, h.Name)
	}
	return "", nil
}
