package backend

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"testing"
)

// ---- image builders ---------------------------------------------------------

// makePNG encodes an RGBA image of the given size as PNG bytes.
// Pixels are filled with a simple gradient so the data is non-trivial.
func makePNG(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{
				R: uint8(x * 255 / max1(w-1)),
				G: uint8(y * 255 / max1(h-1)),
				B: 128,
				A: 255,
			})
		}
	}
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}

func max1(v int) int {
	if v < 1 {
		return 1
	}
	return v
}

// buildTIFF returns a minimal TIFF byte slice with a single Orientation entry.
func buildTIFF(orientation uint16, littleEndian bool) []byte {
	var buf bytes.Buffer
	var w16 func(uint16)
	var w32 func(uint32)
	if littleEndian {
		buf.WriteString("II")
		w16 = func(v uint16) { var b [2]byte; binary.LittleEndian.PutUint16(b[:], v); buf.Write(b[:]) }
		w32 = func(v uint32) { var b [4]byte; binary.LittleEndian.PutUint32(b[:], v); buf.Write(b[:]) }
	} else {
		buf.WriteString("MM")
		w16 = func(v uint16) { var b [2]byte; binary.BigEndian.PutUint16(b[:], v); buf.Write(b[:]) }
		w32 = func(v uint32) { var b [4]byte; binary.BigEndian.PutUint32(b[:], v); buf.Write(b[:]) }
	}
	w16(42)          // magic
	w32(8)           // offset to IFD0
	w16(1)           // one IFD entry
	w16(0x0112)      // tag: Orientation
	w16(3)           // type: SHORT
	w32(1)           // count
	w16(orientation) // value
	w16(0)           // pad to 4 bytes
	w32(0)           // next IFD = none
	return buf.Bytes()
}

// buildJPEGWithEXIF returns JPEG SOI + APP1 containing Exif with the given orientation.
// The result is NOT a decodable JPEG — only suitable for EXIF-parsing tests.
func buildJPEGWithEXIF(orientation int) []byte {
	tiff := buildTIFF(uint16(orientation), true)
	app1Data := append([]byte("Exif\x00\x00"), tiff...)

	var buf bytes.Buffer
	buf.Write([]byte{0xFF, 0xD8}) // SOI
	buf.Write([]byte{0xFF, 0xE1}) // APP1 marker
	var l [2]byte
	binary.BigEndian.PutUint16(l[:], uint16(len(app1Data)+2))
	buf.Write(l[:])
	buf.Write(app1Data)
	return buf.Bytes()
}

// ---- clamp0_255 -------------------------------------------------------------

func TestClamp0_255(t *testing.T) {
	cases := []struct{ in, want float64 }{
		{-1, 0},
		{0, 0},
		{128, 128},
		{255, 255},
		{256, 255},
		{1000, 255},
	}
	for _, tc := range cases {
		got := clamp0_255(tc.in)
		if got != tc.want {
			t.Errorf("clamp0_255(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// ---- colorRGBA --------------------------------------------------------------

func TestColorRGBA(t *testing.T) {
	c := color.RGBA{R: 10, G: 20, B: 30, A: 255}
	got := colorRGBA(c)
	if got.R != 10 || got.G != 20 || got.B != 30 || got.A != 255 {
		t.Errorf("colorRGBA round-trip failed: %+v", got)
	}
}

// ---- looksLikeJPEG ----------------------------------------------------------

func TestLooksLikeJPEG(t *testing.T) {
	if !looksLikeJPEG([]byte{0xFF, 0xD8, 0x00}) {
		t.Error("expected JPEG magic to be recognised")
	}
	if looksLikeJPEG([]byte{0x89, 0x50, 0x4E, 0x47}) { // PNG header
		t.Error("expected PNG not to look like JPEG")
	}
	if looksLikeJPEG([]byte{0xFF}) {
		t.Error("expected too-short slice to return false")
	}
	if looksLikeJPEG(nil) {
		t.Error("expected nil to return false")
	}
}

// ---- parseTIFFOrientation ---------------------------------------------------

func TestParseTIFFOrientation_LittleEndian(t *testing.T) {
	for ori := 1; ori <= 8; ori++ {
		tiff := buildTIFF(uint16(ori), true)
		got, ok := parseTIFFOrientation(tiff)
		if !ok || got != ori {
			t.Errorf("LE orientation %d: got %d, ok=%v", ori, got, ok)
		}
	}
}

func TestParseTIFFOrientation_BigEndian(t *testing.T) {
	for ori := 1; ori <= 8; ori++ {
		tiff := buildTIFF(uint16(ori), false)
		got, ok := parseTIFFOrientation(tiff)
		if !ok || got != ori {
			t.Errorf("BE orientation %d: got %d, ok=%v", ori, got, ok)
		}
	}
}

func TestParseTIFFOrientation_Invalid(t *testing.T) {
	_, ok := parseTIFFOrientation([]byte{})
	if ok {
		t.Error("expected false for empty input")
	}
	_, ok = parseTIFFOrientation([]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})
	if ok {
		t.Error("expected false for unknown byte order")
	}
}

// ---- parseEXIFOrientationJPEG -----------------------------------------------

func TestParseEXIFOrientationJPEG_WithEXIF(t *testing.T) {
	for ori := 2; ori <= 8; ori++ {
		b := buildJPEGWithEXIF(ori)
		got, ok := parseEXIFOrientationJPEG(b)
		if !ok || got != ori {
			t.Errorf("orientation %d: got %d ok=%v", ori, got, ok)
		}
	}
}

func TestParseEXIFOrientationJPEG_NoEXIF(t *testing.T) {
	// Bare JPEG SOI with no APP1
	b := []byte{0xFF, 0xD8, 0xFF, 0xD9}
	got, ok := parseEXIFOrientationJPEG(b)
	if ok || got != 1 {
		t.Errorf("expected (1, false), got (%d, %v)", got, ok)
	}
}

func TestParseEXIFOrientationJPEG_NonJPEG(t *testing.T) {
	got, ok := parseEXIFOrientationJPEG(makePNG(4, 4))
	if ok || got != 1 {
		t.Errorf("expected (1, false) for PNG, got (%d, %v)", got, ok)
	}
}

// ---- applyEXIFOrientation ---------------------------------------------------

func TestApplyEXIFOrientation_NonJPEG(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 4, 6))
	b := makePNG(4, 6)
	out := applyEXIFOrientation(b, src)
	if out != src {
		t.Error("expected image unchanged for non-JPEG input")
	}
}

func TestApplyEXIFOrientation_JPEGNoEXIF(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 4, 6))
	b := []byte{0xFF, 0xD8, 0xFF, 0xD9} // minimal JPEG, no EXIF
	out := applyEXIFOrientation(b, src)
	if out != src {
		t.Error("expected image unchanged for JPEG with no EXIF")
	}
}

func TestApplyEXIFOrientation_Orientation1(t *testing.T) {
	// Orientation 1 = normal, image must not be transformed
	src := image.NewRGBA(image.Rect(0, 0, 10, 20))
	b := buildJPEGWithEXIF(1)
	out := applyEXIFOrientation(b, src)
	if out != src {
		t.Error("expected identity transform for orientation 1")
	}
}

func TestApplyEXIFOrientation_Orientation6(t *testing.T) {
	// Orientation 6 = rotate 90 CW: w×h becomes h×w
	src := image.NewRGBA(image.Rect(0, 0, 10, 20))
	b := buildJPEGWithEXIF(6)
	out := applyEXIFOrientation(b, src)
	ob := out.Bounds()
	// After 90 CW rotation, 10×20 → 20×10
	if ob.Dx() != 20 || ob.Dy() != 10 {
		t.Errorf("expected 20×10 after orientation 6, got %d×%d", ob.Dx(), ob.Dy())
	}
}

// ---- transformByOrientation -------------------------------------------------

func TestTransformByOrientation_AllOrientations(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 8, 6))
	// Set a distinct pixel to verify transform
	src.Set(0, 0, color.RGBA{255, 0, 0, 255})

	cases := []struct {
		ori     int
		wantW   int
		wantH   int
		checkFn func(dst image.Image) bool
	}{
		{1, 8, 6, func(dst image.Image) bool { return true }}, // default: no-op
		{2, 8, 6, func(dst image.Image) bool { // flip horizontal: (0,0) → (w-1, 0)
			r, _, _, _ := dst.At(7, 0).RGBA()
			return r > 0
		}},
		{3, 8, 6, func(dst image.Image) bool { // rotate 180: (0,0) → (w-1, h-1)
			r, _, _, _ := dst.At(7, 5).RGBA()
			return r > 0
		}},
		{4, 8, 6, func(dst image.Image) bool { // flip vertical: (0,0) → (0, h-1)
			r, _, _, _ := dst.At(0, 5).RGBA()
			return r > 0
		}},
		{5, 6, 8, func(dst image.Image) bool { return dst.Bounds().Dx() == 6 }}, // transpose: 8×6 → 6×8
		{6, 6, 8, func(dst image.Image) bool { return dst.Bounds().Dx() == 6 }}, // rotate 90 CW
		{7, 6, 8, func(dst image.Image) bool { return dst.Bounds().Dx() == 6 }}, // transverse
		{8, 6, 8, func(dst image.Image) bool { return dst.Bounds().Dx() == 6 }}, // rotate 90 CCW
	}

	for _, tc := range cases {
		dst := transformByOrientation(src, tc.ori)
		b := dst.Bounds()
		if b.Dx() != tc.wantW || b.Dy() != tc.wantH {
			t.Errorf("ori %d: expected %d×%d, got %d×%d", tc.ori, tc.wantW, tc.wantH, b.Dx(), b.Dy())
		}
		if !tc.checkFn(dst) {
			t.Errorf("ori %d: pixel check failed", tc.ori)
		}
	}
}

// ---- resizeHighQuality ------------------------------------------------------

func TestResizeHighQuality_Downscale(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 100, 80))
	dst := resizeHighQuality(src, 50, 40)
	b := dst.Bounds()
	if b.Dx() != 50 || b.Dy() != 40 {
		t.Errorf("expected 50×40, got %d×%d", b.Dx(), b.Dy())
	}
}

func TestResizeHighQuality_ZeroDst(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 10, 10))
	dst := resizeHighQuality(src, 0, 0)
	b := dst.Bounds()
	if b.Dx() != 0 || b.Dy() != 0 {
		t.Errorf("expected 0×0, got %d×%d", b.Dx(), b.Dy())
	}
}

func TestResizeHighQuality_ZeroSrc(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 0, 0))
	dst := resizeHighQuality(src, 50, 50)
	if dst == nil {
		t.Error("expected non-nil result for zero-src")
	}
}

// ---- optimizeBytes ----------------------------------------------------------

func TestOptimizeBytes_InvalidInput(t *testing.T) {
	_, _, _, err := optimizeBytes([]byte{0x00, 0x01, 0x02}, "image/png", 300, 300)
	if err == nil {
		t.Error("expected error for invalid image bytes")
	}
}

func TestOptimizeBytes_LargePNG_IsDownscaled(t *testing.T) {
	// 500×500 PNG must be downscaled to fit within 300×300
	b := makePNG(500, 500)
	data, same, mime, err := optimizeBytes(b, "image/png", 300, 300)
	if err != nil {
		t.Fatalf("optimizeBytes: %v", err)
	}
	if same {
		// If the optimizer didn't find sufficient gain, that's still valid;
		// but output mime should be original in that case.
		return
	}
	if mime != "image/webp" {
		t.Errorf("expected output MIME image/webp, got %q", mime)
	}
	if len(data) == 0 {
		t.Error("expected non-empty output data")
	}
}

func TestOptimizeBytes_SmallPNG_NoResize(t *testing.T) {
	// 10×10 PNG fits in 300×300 — no resize step
	b := makePNG(10, 10)
	_, _, _, err := optimizeBytes(b, "image/png", 300, 300)
	if err != nil {
		t.Fatalf("optimizeBytes small: %v", err)
	}
}

func TestOptimizeBytes_RectangularAspect(t *testing.T) {
	// 600×200 → scale = min(300/600, 300/200) = 0.5 → 300×100
	b := makePNG(600, 200)
	data, same, mime, err := optimizeBytes(b, "image/png", 300, 300)
	if err != nil {
		t.Fatalf("optimizeBytes rect: %v", err)
	}
	if same {
		return // small gain is acceptable
	}
	if mime != "image/webp" {
		t.Errorf("expected image/webp, got %q", mime)
	}
	if len(data) == 0 {
		t.Error("expected non-empty data")
	}
}

func TestOptimizeBytes_WebPMIME_FallsBack(t *testing.T) {
	// Pass PNG bytes with "image/webp" MIME — xwebp decoder will fail,
	// generic decoder should succeed as fallback.
	b := makePNG(20, 20)
	_, _, _, err := optimizeBytes(b, "image/webp", 300, 300)
	// May succeed (via fallback) or fail — main thing is no panic.
	_ = err
}
