package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"

	"github.com/AvengeMedia/DankMaterialShell/core/internal/screenshot"
)

type dpmsBackend interface {
	ListOutputs() []string
	SetDPMS(outputName string, on bool) error
	Close()
}

// KWin's DPMS protocol is LGPL and marked as a Plasma implementation detail;
// kscreen-doctor (libkscreen) wraps it instead.
func openDPMS() (dpmsBackend, error) {
	if screenshot.DetectCompositor() == screenshot.CompositorKwin {
		return newKscreenDPMS()
	}
	return newDPMSClient()
}

type kscreenDPMS struct {
	outputs []string
}

func newKscreenDPMS() (*kscreenDPMS, error) {
	if _, err := exec.LookPath("kscreen-doctor"); err != nil {
		return nil, errors.New("display power on KWin needs kscreen-doctor (libkscreen)")
	}
	out, err := runKscreenDoctor("--dpms", "show")
	if err != nil {
		return nil, err
	}
	outputs := parseKscreenDPMS(out)
	if len(outputs) == 0 {
		return nil, errors.New("kscreen-doctor reported no outputs with display power control")
	}
	return &kscreenDPMS{outputs: outputs}, nil
}

func (k *kscreenDPMS) ListOutputs() []string {
	return k.outputs
}

func (k *kscreenDPMS) SetDPMS(outputName string, on bool) error {
	args, err := kscreenDPMSArgs(k.outputs, outputName, on)
	if err != nil {
		return err
	}
	_, err = runKscreenDoctor(args...)
	return err
}

func (k *kscreenDPMS) Close() {}

// kscreenDPMSArgs targets one output by excluding every other one.
func kscreenDPMSArgs(outputs []string, outputName string, on bool) ([]string, error) {
	mode := "off"
	if on {
		mode = "on"
	}
	args := []string{"--dpms", mode}
	if outputName == "" {
		return args, nil
	}
	if !slices.Contains(outputs, outputName) {
		return nil, fmt.Errorf("output %q not found", outputName)
	}
	for _, o := range outputs {
		if o != outputName {
			args = append(args, "--dpms-excluded", o)
		}
	}
	return args, nil
}

func parseKscreenDPMS(out string) []string {
	var outputs []string
	for _, line := range strings.Split(out, "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), "dpms mode for screen ")
		if !ok {
			continue
		}
		if name, _, ok := strings.Cut(rest, ":"); ok && name != "" {
			outputs = append(outputs, name)
		}
	}
	return outputs
}

func runKscreenDoctor(args ...string) (string, error) {
	// kscreen-doctor aborts instead of erroring when it cannot reach Wayland.
	if os.Getenv("WAYLAND_DISPLAY") == "" {
		return "", errors.New("WAYLAND_DISPLAY is not set")
	}
	out, err := exec.Command("kscreen-doctor", args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("kscreen-doctor %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}
