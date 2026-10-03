package screenshot

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/godbus/dbus/v5"
)

func kwinMeta(width, height, stride, format uint32) map[string]dbus.Variant {
	return map[string]dbus.Variant{
		"width":  dbus.MakeVariant(width),
		"height": dbus.MakeVariant(height),
		"stride": dbus.MakeVariant(stride),
		"format": dbus.MakeVariant(format),
	}
}

func TestBufferFromKwinMapsQImageFormatsAndRejectsBadImages(t *testing.T) {
	data := make([]byte, 8*2)
	data[4], data[5], data[6], data[7] = 0x10, 0x20, 0x30, 0xff // second pixel, BGRA in memory

	buf, err := bufferFromKwin(kwinMeta(1, 2, 8, 6), data)
	if err != nil {
		t.Fatal(err)
	}
	defer buf.Close()
	if buf.Format != FormatARGB8888 || buf.Width != 1 || buf.Height != 2 || buf.Stride != 8 {
		t.Fatalf("buffer = %v %dx%d stride %d, want ARGB8888 1x2 stride 8", buf.Format, buf.Width, buf.Height, buf.Stride)
	}
	if got := buf.Data()[4:8]; got[0] != 0x10 || got[2] != 0x30 {
		t.Fatalf("pixel bytes = %v, want copied unchanged", got)
	}

	for _, bad := range []struct {
		name string
		meta map[string]dbus.Variant
		data []byte
		want string
	}{
		{"unknown format", kwinMeta(1, 1, 4, 13), make([]byte, 4), "unsupported"},
		{"stride below width", kwinMeta(2, 1, 4, 4), make([]byte, 8), "invalid"},
		{"short pipe read", kwinMeta(1, 2, 4, 4), make([]byte, 4), "short"},
	} {
		if _, err := bufferFromKwin(bad.meta, bad.data); err == nil || !strings.Contains(err.Error(), bad.want) {
			t.Errorf("%s: err = %v, want %q", bad.name, err, bad.want)
		}
	}
}

func TestOutputCropScalesLogicalBoundsOntoThePortalImage(t *testing.T) {
	outputs := []Region{
		{X: 0, Y: 0, Width: 1920, Height: 1080, Output: "DP-1"},
		{X: 1920, Y: 0, Width: 1280, Height: 720, Output: "HDMI-A-1"},
	}
	// 2x scale: the portal image is twice the logical size.
	img := image.Rect(0, 0, 6400, 2160)
	got, err := outputCrop("HDMI-A-1", outputs, img)
	if err != nil {
		t.Fatal(err)
	}
	if want := image.Rect(3840, 0, 6400, 1440); got != want {
		t.Fatalf("crop = %v, want %v", got, want)
	}
	if _, err := outputCrop("DP-9", outputs, img); err == nil {
		t.Fatal("unknown output must fail")
	}
}

func TestPortalResultDecodesAndRemovesTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shot.png")
	src := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	src.Set(1, 0, color.NRGBA{R: 200, G: 100, B: 50, A: 255})
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, src); err != nil {
		t.Fatal(err)
	}
	f.Close()

	img, err := portalResult(uint32(0), map[string]dbus.Variant{"uri": dbus.MakeVariant("file://" + path)})
	if err != nil {
		t.Fatal(err)
	}
	buf, err := bufferFromImage(img, img.Bounds())
	if err != nil {
		t.Fatal(err)
	}
	defer buf.Close()
	if px := buf.Data()[4:8]; px[0] != 50 || px[1] != 100 || px[2] != 200 || px[3] != 255 {
		t.Fatalf("ARGB8888 bytes = %v, want B,G,R,A = 50,100,200,255", px)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("portal file still exists: %v", err)
	}

	if _, err := portalResult(uint32(1), map[string]dbus.Variant{}); err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("denied response err = %v, want cancelled", err)
	}
}
