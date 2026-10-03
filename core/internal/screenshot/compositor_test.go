package screenshot

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectCompositorKwin(t *testing.T) {
	liveSocket := filepath.Join(t.TempDir(), "niri.sock")
	if err := os.WriteFile(liveSocket, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	staleSocket := filepath.Join(t.TempDir(), "gone.sock")

	cases := []struct {
		name       string
		niriSocket string
		kwin       bool
		want       Compositor
	}{
		{"kwin alone", "", true, CompositorKwin},
		{"compositor nested in plasma wins", liveSocket, true, CompositorNiri},
		{"stale env does not mask kwin", staleSocket, true, CompositorKwin},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, env := range []string{"AQUEOUS_SOCKET", "MANGO_INSTANCE_SIGNATURE", "UMBRIEL_SOCKET",
				"NIRI_SOCKET", "SCROLLSOCK", "MIRACLESOCK", "SWAYSOCK", "HYPRLAND_INSTANCE_SIGNATURE"} {
				t.Setenv(env, "")
			}
			t.Setenv("NIRI_SOCKET", tc.niriSocket)
			origProbe := kwinRunning
			kwinRunning = func() bool { return tc.kwin }
			detectedCompositor = -1
			t.Cleanup(func() {
				kwinRunning = origProbe
				detectedCompositor = -1
			})

			if got := DetectCompositor(); got != tc.want {
				t.Fatalf("DetectCompositor() = %v, want %v", got, tc.want)
			}
		})
	}
}
