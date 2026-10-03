package main

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/AvengeMedia/DankMaterialShell/core/internal/kscreen"
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
	out, err := kscreen.Run("--dpms", "show")
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
	_, err = kscreen.Run(args...)
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
