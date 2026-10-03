package main

import (
	"slices"
	"testing"
)

func TestParseKscreenDPMSReadsEveryScreen(t *testing.T) {
	out := "dpms mode for screen DP-1: on\ndpms mode for screen HDMI-A-1: off\nsome warning\n"
	if got := parseKscreenDPMS(out); !slices.Equal(got, []string{"DP-1", "HDMI-A-1"}) {
		t.Fatalf("outputs = %v, want [DP-1 HDMI-A-1]", got)
	}
}

func TestKscreenDPMSArgsTargetOneOutputByExcludingTheRest(t *testing.T) {
	outputs := []string{"DP-1", "DP-2", "HDMI-A-1"}
	got, err := kscreenDPMSArgs(outputs, "DP-2", false)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--dpms", "off", "--dpms-excluded", "DP-1", "--dpms-excluded", "HDMI-A-1"}
	if !slices.Equal(got, want) {
		t.Fatalf("args = %v, want %v", got, want)
	}
	if got, _ := kscreenDPMSArgs(outputs, "", true); !slices.Equal(got, []string{"--dpms", "on"}) {
		t.Fatalf("all outputs args = %v, want [--dpms on]", got)
	}
	if _, err := kscreenDPMSArgs(outputs, "DP-9", false); err == nil {
		t.Fatal("unknown output must fail instead of switching every output")
	}
}
