package wlroutput

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/AvengeMedia/DankMaterialShell/core/internal/kscreen"
)

func kscreenFixture(t *testing.T) []kscreen.Output {
	t.Helper()
	data, err := os.ReadFile("../../kscreen/testdata/doctor.json")
	if err != nil {
		t.Fatal(err)
	}
	outputs, err := kscreen.ParseOutputs(data)
	if err != nil {
		t.Fatal(err)
	}
	return outputs
}

func TestStateFromKscreenPacksStableModeIDs(t *testing.T) {
	state := stateFromKscreen(kscreenFixture(t), 7)
	if state.Serial != 7 || len(state.Outputs) != 2 {
		t.Fatalf("state = %+v", state)
	}
	dp := state.Outputs[0]
	if dp.CurrentMode == nil || dp.CurrentMode.ID != 1<<16|2 || !dp.CurrentMode.Preferred || dp.CurrentMode.Refresh != 60000 {
		t.Fatalf("DP-1 current mode = %+v, want id 0x10002, preferred, 60000 mHz", dp.CurrentMode)
	}
	if dp.Modes[0].Refresh != 59951 || dp.Modes[0].Preferred {
		t.Fatalf("DP-1 first mode = %+v, want 59951 mHz and not preferred", dp.Modes[0])
	}
	if hdmi := state.Outputs[1]; hdmi.Transform != 3 || hdmi.X != 2926 || hdmi.CurrentMode.ID != 2<<16|7 {
		t.Fatalf("HDMI-A-1 = %+v, want transform 3 at x 2926 with mode 0x20007", hdmi)
	}
}

func TestKscreenConfigArgsTranslatesHeads(t *testing.T) {
	outputs := kscreenFixture(t)
	modeID, scale, transform := uint32(1<<16|1), 1.5, int32(1)
	heads := []HeadConfig{
		{Name: "DP-1", Enabled: true, ModeID: &modeID, Scale: &scale, Transform: &transform, Position: &struct{ X, Y int32 }{0, 0}},
		{Name: "HDMI-A-1", Enabled: false},
	}
	got, err := kscreenConfigArgs(heads, outputs)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"output.DP-1.enable", "output.DP-1.mode.1", "output.DP-1.position.0,0", "output.DP-1.scale.1.5", "output.DP-1.rotation.left", "output.HDMI-A-1.disable"}
	if !slices.Equal(got, want) {
		t.Fatalf("args = %v\nwant %v", got, want)
	}
}

func TestKscreenConfigArgsRejectsWhatKscreenDoctorCannotDo(t *testing.T) {
	outputs := kscreenFixture(t)
	badMode, sync := uint32(1<<16|99), uint32(1)
	custom := HeadConfig{Name: "DP-1", Enabled: true}
	custom.CustomMode = &struct {
		Width   int32 `json:"width"`
		Height  int32 `json:"height"`
		Refresh int32 `json:"refresh"`
	}{1234, 567, 60000}
	for _, c := range []struct {
		name  string
		heads []HeadConfig
		want  string
	}{
		{"unknown output", []HeadConfig{{Name: "DP-9", Enabled: true}}, "not found"},
		{"unknown mode", []HeadConfig{{Name: "DP-1", Enabled: true, ModeID: &badMode}}, "mode"},
		{"custom mode", []HeadConfig{custom}, "custom modes"},
		{"all disabled", []HeadConfig{{Name: "DP-1"}, {Name: "HDMI-A-1"}}, "at least one"},
		{"adaptive sync", []HeadConfig{{Name: "DP-1", Enabled: true, AdaptiveSync: &sync}}, "adaptive sync"},
	} {
		if _, err := kscreenConfigArgs(c.heads, outputs); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", c.name, err, c.want)
		}
	}
}
