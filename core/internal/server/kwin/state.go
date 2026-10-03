package kwin

import "slices"

// apply folds one bridge event into a copy of s, so published states are never mutated.
func apply(s State, ev event) State {
	switch ev.Type {
	case "snapshot":
		return State{
			Available:         true,
			PerOutputDesktops: ev.PerOutputDesktops,
			Desktops:          ev.Desktops,
			Outputs:           ev.Outputs,
			Windows:           ev.Windows,
		}
	case "added":
		if ev.Window == nil {
			return s
		}
		s.Windows = upsertWindows(s.Windows, []Window{*ev.Window})
	case "changed":
		s.Windows = upsertWindows(s.Windows, ev.Windows)
	case "removed":
		s.Windows = slices.DeleteFunc(slices.Clone(s.Windows), func(w Window) bool { return w.ID == ev.ID })
	case "active":
		windows := slices.Clone(s.Windows)
		for i := range windows {
			windows[i].Active = windows[i].ID == ev.ID
		}
		s.Windows = windows
	case "desktopChanged":
		outputs := slices.Clone(s.Outputs)
		for i := range outputs {
			if outputs[i].Name == ev.Output {
				outputs[i].CurrentDesktop = ev.Current
			}
		}
		s.Outputs = outputs
	case "desktopsChanged":
		s.Desktops = ev.Desktops
	case "outputsChanged":
		s.Outputs = ev.Outputs
	case "modeChanged":
		s.PerOutputDesktops = ev.PerOutputDesktops
		s.Outputs = ev.Outputs
	}
	return s
}

func upsertWindows(windows, updates []Window) []Window {
	out := slices.Clone(windows)
	for _, u := range updates {
		i := slices.IndexFunc(out, func(w Window) bool { return w.ID == u.ID })
		if i < 0 {
			out = append(out, u)
			continue
		}
		out[i] = u
	}
	return out
}
