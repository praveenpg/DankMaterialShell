package kwin

import (
	"encoding/json"
	"testing"
)

func snapshotEvent() event {
	return event{
		Type:              "snapshot",
		PerOutputDesktops: true,
		Desktops:          []Desktop{{ID: "d1", Name: "One"}, {ID: "d2", Name: "Two"}},
		Outputs:           []Output{{Name: "DP-1", CurrentDesktop: "d1"}, {Name: "DP-2", CurrentDesktop: "d1"}},
		Windows:           []Window{{ID: "a", AppID: "kate", Desktops: []string{"d1"}, Output: "DP-1"}},
	}
}

func TestApplyFoldsEventsWithoutMutatingPublishedState(t *testing.T) {
	published := apply(State{}, snapshotEvent())

	s := apply(published, event{Type: "desktopChanged", Output: "DP-2", Current: "d2"})
	s = apply(s, event{Type: "added", Window: &Window{ID: "b", AppID: "konsole", Output: "DP-2"}})
	s = apply(s, event{Type: "changed", Windows: []Window{{ID: "a", AppID: "kate", Desktops: []string{"d2"}, Output: "DP-1"}}})
	s = apply(s, event{Type: "active", ID: "b"})
	s = apply(s, event{Type: "removed", ID: "a"})

	if got := []string{s.Outputs[0].CurrentDesktop, s.Outputs[1].CurrentDesktop}; got[0] != "d1" || got[1] != "d2" {
		t.Fatalf("per-output desktops = %v, want [d1 d2]", got)
	}
	if len(s.Windows) != 1 || s.Windows[0].ID != "b" || !s.Windows[0].Active {
		t.Fatalf("windows = %+v, want only active b", s.Windows)
	}
	if published.Outputs[1].CurrentDesktop != "d1" || published.Windows[0].Desktops[0] != "d1" || published.Windows[0].Active {
		t.Fatalf("published state was mutated: %+v", published)
	}
}

func TestHandleEventAppliesInSequenceAndDropsStaleGenerations(t *testing.T) {
	m := &Manager{generation: 2, nextSeq: 1, early: map[uint64]event{}}
	send := func(generation, seq uint64, ev event) {
		ev.Generation, ev.Seq = generation, seq
		payload, _ := json.Marshal(ev)
		m.handleEvent(string(payload))
	}

	// D-Bus delivers each call on its own goroutine, so arrival order is arbitrary.
	send(2, 3, event{Type: "active", ID: "b"})
	send(2, 2, event{Type: "added", Window: &Window{ID: "b"}})
	send(1, 9, event{Type: "snapshot"})
	send(2, 1, snapshotEvent())
	send(2, 1, event{Type: "removed", ID: "a"})

	s := m.GetState()
	if !s.Available || len(s.Windows) != 2 || s.Windows[0].ID != "a" || s.Windows[1].ID != "b" || !s.Windows[1].Active {
		t.Fatalf("windows = %+v, want snapshot's a, then b added and active", s.Windows)
	}
}
