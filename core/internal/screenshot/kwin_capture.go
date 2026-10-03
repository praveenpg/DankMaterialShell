package screenshot

import (
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/AvengeMedia/DankMaterialShell/core/internal/log"
	"github.com/godbus/dbus/v5"
)

// KWin serves ScreenShot2 only to binaries an installed .desktop entry names by absolute
// path (assets/com.danklinux.dms.desktop); anything else falls back to the portal.

const (
	kwinScreenshotPath  = dbus.ObjectPath("/org/kde/KWin/ScreenShot2")
	kwinScreenshotIface = "org.kde.KWin.ScreenShot2"
	kwinNotAuthorized   = kwinScreenshotIface + ".Error.NoAuthorized"
	portalTimeout       = 60 * time.Second
)

// QImage::Format values KWin returns, by the wl_shm format with the same little-endian layout.
var qimageFormats = map[uint32]PixelFormat{
	4:  FormatXRGB8888,    // RGB32
	5:  FormatARGB8888,    // ARGB32
	6:  FormatARGB8888,    // ARGB32_Premultiplied
	16: FormatXBGR8888,    // RGBX8888
	17: FormatABGR8888,    // RGBA8888
	18: FormatABGR8888,    // RGBA8888_Premultiplied
	19: FormatXBGR2101010, // BGR30
	20: FormatABGR2101010, // A2BGR30_Premultiplied
	21: FormatXRGB2101010, // RGB30
	22: FormatARGB2101010, // A2RGB30_Premultiplied
}

func usesKwinCapture() bool {
	return DetectCompositor() == CompositorKwin
}

// kwinCaptureOutput captures one output at native resolution; outputs lists every
// output's logical bounds so a portal capture of the whole workspace can be cropped.
func kwinCaptureOutput(name string, cursor bool, outputs []Region) (*ShmBuffer, error) {
	buf, err := kwinScreenShot2("CaptureScreen", cursor, name)
	if !errors.Is(err, errKwinNotAuthorized) {
		return buf, err
	}
	return portalCaptureOutput(name, outputs)
}

func kwinCaptureActiveWindow(cursor bool) (*ShmBuffer, error) {
	buf, err := kwinScreenShot2("CaptureActiveWindow", cursor)
	if errors.Is(err, errKwinNotAuthorized) {
		return nil, fmt.Errorf("window capture on KWin needs dms installed with its desktop entry: %w", err)
	}
	return buf, err
}

var errKwinNotAuthorized = errors.New("kwin refused the screenshot")

func kwinScreenShot2(method string, cursor bool, args ...any) (*ShmBuffer, error) {
	conn, err := dbus.SessionBus()
	if err != nil {
		return nil, err
	}
	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	defer r.Close()

	// KWin writes the whole image before replying, and it outgrows the pipe buffer.
	type read struct {
		data []byte
		err  error
	}
	done := make(chan read, 1)
	go func() {
		data, err := io.ReadAll(r)
		done <- read{data, err}
	}()

	options := map[string]dbus.Variant{
		"include-cursor":     dbus.MakeVariant(cursor),
		"native-resolution":  dbus.MakeVariant(true),
		"include-decoration": dbus.MakeVariant(true),
		"include-shadow":     dbus.MakeVariant(false),
	}
	var meta map[string]dbus.Variant
	call := conn.Object("org.kde.KWin", kwinScreenshotPath).
		Call(kwinScreenshotIface+"."+method, 0, append(args, options, dbus.UnixFD(w.Fd()))...)
	w.Close()
	if call.Err != nil {
		var dbusErr dbus.Error
		if errors.As(call.Err, &dbusErr) && dbusErr.Name == kwinNotAuthorized {
			return nil, fmt.Errorf("%w: %v", errKwinNotAuthorized, call.Err)
		}
		return nil, fmt.Errorf("%s: %w", method, call.Err)
	}
	if err := call.Store(&meta); err != nil {
		return nil, err
	}

	got := <-done
	if got.err != nil {
		return nil, fmt.Errorf("read %s image: %w", method, got.err)
	}
	return bufferFromKwin(meta, got.data)
}

func bufferFromKwin(meta map[string]dbus.Variant, data []byte) (*ShmBuffer, error) {
	value := func(key string) uint32 {
		v, _ := meta[key].Value().(uint32)
		return v
	}
	width, height, stride := int(value("width")), int(value("height")), int(value("stride"))
	format, ok := qimageFormats[value("format")]
	switch {
	case !ok:
		return nil, fmt.Errorf("unsupported KWin image format %d", value("format"))
	case width <= 0 || height <= 0 || stride < width*format.BytesPerPixel():
		return nil, fmt.Errorf("invalid KWin image %dx%d stride %d", width, height, stride)
	case len(data) < stride*height:
		return nil, fmt.Errorf("short KWin image: %d of %d bytes", len(data), stride*height)
	}

	buf, err := CreateShmBuffer(width, height, stride)
	if err != nil {
		return nil, err
	}
	copy(buf.Data(), data[:stride*height])
	buf.Format = format
	return buf, nil
}

func portalCaptureOutput(name string, outputs []Region) (*ShmBuffer, error) {
	img, err := portalScreenshot()
	if err != nil {
		return nil, err
	}
	crop, err := outputCrop(name, outputs, img.Bounds())
	if err != nil {
		return nil, err
	}
	return bufferFromImage(img, crop)
}

// outputCrop maps an output's logical bounds onto the portal's workspace image,
// assuming one scale across outputs; mixed scales make the portal image ambiguous.
func outputCrop(name string, outputs []Region, img image.Rectangle) (image.Rectangle, error) {
	var target *Region
	minX, minY, maxX := int32(1<<30), int32(1<<30), int32(-1<<30)
	for i, o := range outputs {
		if o.Output == name {
			target = &outputs[i]
		}
		minX, minY, maxX = min(minX, o.X), min(minY, o.Y), max(maxX, o.X+o.Width)
	}
	if target == nil || maxX <= minX {
		return image.Rectangle{}, fmt.Errorf("output %q not found", name)
	}
	f := float64(img.Dx()) / float64(maxX-minX)
	at := func(v int32, origin int32) int { return int(float64(v-origin)*f + 0.5) }
	rect := image.Rect(at(target.X, minX), at(target.Y, minY), at(target.X+target.Width, minX), at(target.Y+target.Height, minY))
	return rect.Add(img.Min).Intersect(img), nil
}

func bufferFromImage(img image.Image, rect image.Rectangle) (*ShmBuffer, error) {
	w, h := rect.Dx(), rect.Dy()
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("empty capture area %v", rect)
	}
	buf, err := CreateShmBuffer(w, h, w*4)
	if err != nil {
		return nil, err
	}
	buf.Format = FormatARGB8888
	dst := buf.Data()
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, b, a := img.At(rect.Min.X+x, rect.Min.Y+y).RGBA()
			i := y*w*4 + x*4
			dst[i], dst[i+1], dst[i+2], dst[i+3] = byte(b>>8), byte(g>>8), byte(r>>8), byte(a>>8)
		}
	}
	return buf, nil
}

func portalScreenshot() (image.Image, error) {
	conn, err := dbus.SessionBus()
	if err != nil {
		return nil, err
	}
	token := fmt.Sprintf("dms%d", time.Now().UnixNano())
	sender := strings.ReplaceAll(strings.TrimPrefix(conn.Names()[0], ":"), ".", "_")
	request := dbus.ObjectPath("/org/freedesktop/portal/desktop/request/" + sender + "/" + token)

	match := []dbus.MatchOption{
		dbus.WithMatchObjectPath(request),
		dbus.WithMatchInterface("org.freedesktop.portal.Request"),
		dbus.WithMatchMember("Response"),
	}
	if err := conn.AddMatchSignal(match...); err != nil {
		return nil, err
	}
	defer conn.RemoveMatchSignal(match...)
	signals := make(chan *dbus.Signal, 8)
	conn.Signal(signals)
	defer conn.RemoveSignal(signals)

	options := map[string]dbus.Variant{
		"handle_token": dbus.MakeVariant(token),
		"interactive":  dbus.MakeVariant(false),
	}
	if err := conn.Object("org.freedesktop.portal.Desktop", "/org/freedesktop/portal/desktop").
		Call("org.freedesktop.portal.Screenshot.Screenshot", 0, "", options).Err; err != nil {
		return nil, fmt.Errorf("portal screenshot: %w", err)
	}

	timeout := time.After(portalTimeout)
	for {
		select {
		case sig := <-signals:
			if sig == nil || sig.Path != request || len(sig.Body) < 2 {
				continue
			}
			return portalResult(sig.Body[0], sig.Body[1])
		case <-timeout:
			return nil, errors.New("portal screenshot timed out")
		}
	}
}

func portalResult(code, results any) (image.Image, error) {
	if c, _ := code.(uint32); c != 0 {
		return nil, fmt.Errorf("portal screenshot was cancelled or denied (response %d)", c)
	}
	fields, _ := results.(map[string]dbus.Variant)
	uri, _ := fields["uri"].Value().(string)
	parsed, err := url.Parse(uri)
	if err != nil || parsed.Scheme != "file" {
		return nil, fmt.Errorf("portal returned unusable uri %q", uri)
	}
	f, err := os.Open(parsed.Path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	// The portal saves a file for us; dms writes its own copy.
	defer os.Remove(parsed.Path)
	return png.Decode(f)
}

func outputRegions(outputs map[uint32]*WaylandOutput) []Region {
	regions := make([]Region, 0, len(outputs))
	for _, o := range outputs {
		regions = append(regions, Region{X: o.x, Y: o.y, Width: o.width, Height: o.height, Output: o.name})
	}
	return regions
}

func (s *Screenshoter) captureKwinOutput(output *WaylandOutput) (*CaptureResult, error) {
	s.outputsMu.Lock()
	regions := outputRegions(s.outputs)
	s.outputsMu.Unlock()

	buf, err := kwinCaptureOutput(output.name, s.config.Cursor == CursorOn, regions)
	if err != nil {
		return nil, err
	}
	return &CaptureResult{
		Buffer: buf,
		Region: Region{X: output.x, Y: output.y, Width: output.width, Height: output.height, Output: output.name},
		Format: uint32(buf.Format),
		Scale:  output.effectiveScale(),
		CICP:   s.outputCICP(output),
	}, nil
}

func (s *Screenshoter) captureKwinWindow() (*CaptureResult, error) {
	if s.config.Geometry {
		return nil, fmt.Errorf("window geometry mode is not supported on KWin")
	}
	buf, err := kwinCaptureActiveWindow(s.config.Cursor == CursorOn)
	if err != nil {
		return nil, err
	}
	return &CaptureResult{
		Buffer: buf,
		Region: Region{Width: int32(buf.Width), Height: int32(buf.Height)},
		Format: uint32(buf.Format),
		Scale:  1,
	}, nil
}

func (r *RegionSelector) preCaptureKwin() {
	r.outputsMu.Lock()
	regions := outputRegions(r.outputs)
	outputs := make([]*WaylandOutput, 0, len(r.outputs))
	for _, o := range r.outputs {
		outputs = append(outputs, o)
	}
	r.outputsMu.Unlock()

	for _, output := range outputs {
		pc := &PreCapture{}
		r.preCapture[output] = pc
		for _, cursor := range []bool{true, false} {
			buf, err := kwinCaptureOutput(output.name, cursor, regions)
			if err != nil {
				log.Error("kwin capture failed", "output", output.name, "err", err)
				continue
			}
			if buf.Format.Is10Bit() {
				buf.Convert10To8()
			}
			if pc.width == 0 {
				pc.width, pc.height, pc.stride = buf.Width, buf.Height, buf.Stride
				pc.format = uint32(buf.Format)
				r.prepareSlots(output, pc)
			}
			if cursor {
				pc.screenBuf = buf
			} else {
				pc.screenBufNoCursor = buf
			}
		}
	}
}
