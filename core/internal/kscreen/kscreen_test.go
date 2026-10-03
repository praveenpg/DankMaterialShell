package kscreen

import (
	"os"
	"testing"
)

func TestParseOutputsSkipsDisconnectedAndReadsGeometry(t *testing.T) {
	data, err := os.ReadFile("testdata/doctor.json")
	if err != nil {
		t.Fatal(err)
	}
	outputs, err := ParseOutputs(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(outputs) != 2 || outputs[0].Name != "DP-1" || outputs[1].Name != "HDMI-A-1" {
		t.Fatalf("outputs = %+v, want DP-1 and HDMI-A-1 only", outputs)
	}
	hdmi := outputs[1]
	if hdmi.Pos.X != 2926 || hdmi.Scale != 1 || hdmi.CurrentMode().Size.Width != 1920 {
		t.Fatalf("HDMI-A-1 = %+v", hdmi)
	}
}

func TestRotationsMapToWaylandTransforms(t *testing.T) {
	for transform, want := range []struct {
		flag int
		name string
	}{{1, "none"}, {2, "left"}, {4, "inverted"}, {8, "right"}, {16, "flipped"}, {32, "flipped90"}, {64, "flipped180"}, {128, "flipped270"}} {
		if got := TransformFromRotation(want.flag); got != int32(transform) {
			t.Errorf("TransformFromRotation(%d) = %d, want %d", want.flag, got, transform)
		}
		if name, _ := RotationName(int32(transform)); name != want.name {
			t.Errorf("RotationName(%d) = %q, want %q", transform, name, want.name)
		}
	}
	if _, err := RotationName(8); err == nil {
		t.Error("transform 8 must be rejected")
	}
}
