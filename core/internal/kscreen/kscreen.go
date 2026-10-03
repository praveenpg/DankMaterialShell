// Package kscreen drives kscreen-doctor (libkscreen), the supported way to read and
// change KWin outputs from outside Plasma; KWin has no wlr-output-management.
package kscreen

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type Mode struct {
	ID          string  `json:"id"`
	RefreshRate float64 `json:"refreshRate"`
	Size        Size    `json:"size"`
}

type Size struct {
	Width  int32 `json:"width"`
	Height int32 `json:"height"`
}

type Output struct {
	ID             int                  `json:"id"`
	Name           string               `json:"name"`
	Enabled        bool                 `json:"enabled"`
	Connected      bool                 `json:"connected"`
	Pos            struct{ X, Y int32 } `json:"pos"`
	Scale          float64              `json:"scale"`
	Rotation       int                  `json:"rotation"`
	CurrentModeID  string               `json:"currentModeId"`
	PreferredModes []string             `json:"preferredModes"`
	Modes          []Mode               `json:"modes"`
	SizeMM         Size                 `json:"sizeMM"`
}

func (o Output) CurrentMode() *Mode {
	for i := range o.Modes {
		if o.Modes[i].ID == o.CurrentModeID {
			return &o.Modes[i]
		}
	}
	return nil
}

func Available() bool {
	_, err := exec.LookPath("kscreen-doctor")
	return err == nil
}

// Run calls kscreen-doctor, which dumps core instead of failing when it cannot reach Wayland.
func Run(args ...string) (string, error) {
	if !Available() {
		return "", errors.New("kscreen-doctor (libkscreen) is not installed")
	}
	if os.Getenv("WAYLAND_DISPLAY") == "" {
		return "", errors.New("WAYLAND_DISPLAY is not set")
	}
	out, err := exec.Command("kscreen-doctor", args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("kscreen-doctor %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// Outputs returns connected outputs from `kscreen-doctor -j`.
func Outputs() ([]Output, error) {
	out, err := Run("-j")
	if err != nil {
		return nil, err
	}
	return ParseOutputs([]byte(out))
}

func ParseOutputs(data []byte) ([]Output, error) {
	var doc struct {
		Outputs []Output `json:"outputs"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse kscreen-doctor json: %w", err)
	}
	connected := doc.Outputs[:0]
	for _, o := range doc.Outputs {
		if o.Connected {
			connected = append(connected, o)
		}
	}
	return connected, nil
}

// KScreen rotation flags in wl_output.transform order (normal, 90, 180, 270, then flipped).
var rotations = []struct {
	flag int
	name string
}{
	{1, "none"}, {2, "left"}, {4, "inverted"}, {8, "right"},
	{16, "flipped"}, {32, "flipped90"}, {64, "flipped180"}, {128, "flipped270"},
}

func TransformFromRotation(flag int) int32 {
	for i, r := range rotations {
		if r.flag == flag {
			return int32(i)
		}
	}
	return 0
}

func RotationName(transform int32) (string, error) {
	if transform < 0 || int(transform) >= len(rotations) {
		return "", fmt.Errorf("unknown transform %d", transform)
	}
	return rotations[transform].name, nil
}
