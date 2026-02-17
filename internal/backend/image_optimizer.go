package backend

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"runtime"
	"strings"
	"time"

	"github.com/chai2010/webp"
	"github.com/maniack/catwatch/internal/monitoring"
	"github.com/maniack/catwatch/internal/storage"
	xwebp "golang.org/x/image/webp"
)

// Image optimizer defaults
const (
	imgOptMaxW         = 300
	imgOptMaxH         = 300
	imgOptWebPQuality  = 85.0
	imgOptMinGainRatio = 0.03 // require at least 3% size reduction to rewrite
)

// startImageOptimizer launches a background goroutine that periodically
// optimizes unprocessed images in the database.
func (s *Server) startImageOptimizer() {
	s.log.Info("optimizer: starting background worker")
	go s.imageOptimizerLoop()
}

func (s *Server) imageOptimizerLoop() {
	// Initial quick run
	stats, err := s.logOptimizeRun(5)
	limit := 10
	for {
		var sleep time.Duration
		if err != nil {
			sleep = 15 * time.Second // brief backoff on error
		} else if stats.found == 0 {
			sleep = 1 * time.Minute // idle: no work found
		} else if stats.found < limit {
			sleep = 10 * time.Second // some work but not full batch
		} else {
			sleep = 0 // full batch: likely backlog, run immediately
		}
		if sleep > 0 {
			time.Sleep(sleep)
		}
		stats, err = s.logOptimizeRun(limit)
	}
}

// logOptimizeRun runs a single optimization batch and logs summary statistics.
func (s *Server) logOptimizeRun(limit int) (optStats, error) {
	start := time.Now()
	stats, err := s.optimizeImagesBatch(limit)
	dur := time.Since(start)

	// Metrics
	monitoring.ImageOptBatchDuration.Observe(dur.Seconds())
	if stats.found > 0 {
		monitoring.ImageOptFound.Add(float64(stats.found))
	}
	if stats.resized > 0 {
		monitoring.ImageOptResized.Add(float64(stats.resized))
	}
	if stats.marked > 0 {
		monitoring.ImageOptMarked.Add(float64(stats.marked))
	}
	if stats.empty > 0 {
		monitoring.ImageOptEmpty.Add(float64(stats.empty))
	}
	if stats.decodeErrors > 0 {
		monitoring.ImageOptDecodeErrors.Add(float64(stats.decodeErrors))
	}
	if stats.dbErrors > 0 {
		monitoring.ImageOptDBErrors.Add(float64(stats.dbErrors))
	}

	entry := s.log.WithField("limit", limit).
		WithField("found", stats.found).
		WithField("resized", stats.resized).
		WithField("marked", stats.marked).
		WithField("empty", stats.empty).
		WithField("decode_errors", stats.decodeErrors).
		WithField("db_errors", stats.dbErrors).
		WithField("duration_ms", float64(dur.Nanoseconds())/1e6)
	if err != nil {
		entry.WithError(err).Warn("optimizer: batch finished with errors")
		return stats, err
	}
	if stats.found > 0 {
		entry.Info("optimizer: batch finished")
	}
	return stats, nil
}

type optStats struct {
	found        int
	resized      int
	marked       int
	empty        int
	decodeErrors int
	dbErrors     int
}

// optimizeImagesBatch finds a small batch of unoptimized images and compresses them.
func (s *Server) optimizeImagesBatch(limit int) (optStats, error) {
	stats := optStats{}
	imgs, err := s.store.ListImagesToOptimize(limit)
	if err != nil {
		s.log.WithError(err).Warn("optimizer: failed to query images")
		return stats, err
	}
	stats.found = len(imgs)
	if len(imgs) == 0 {
		return stats, nil
	}

	type result struct {
		im   storage.Image
		data []byte
		same bool
		mime string
		err  error
	}

	jobs := make(chan storage.Image)
	results := make(chan result, len(imgs))

	workers := runtime.NumCPU()
	if workers > len(imgs) {
		workers = len(imgs)
	}
	if workers <= 0 {
		workers = 1
	}

	for w := 0; w < workers; w++ {
		go func() {
			for im := range jobs {
				if len(im.Data) == 0 {
					results <- result{im: im, same: true, mime: im.MIME}
					continue
				}
				data, same, outMIME, err := optimizeBytes(im.Data, im.MIME, imgOptMaxW, imgOptMaxH)
				if err != nil {
					results <- result{im: im, err: err}
					continue
				}
				results <- result{im: im, data: data, same: same, mime: outMIME}
			}
		}()
	}

	go func() {
		for _, im := range imgs {
			jobs <- im
		}
		close(jobs)
	}()

	for i := 0; i < len(imgs); i++ {
		r := <-results
		if len(r.im.Data) == 0 {
			if err := s.store.MarkImageOptimizedEmpty(r.im.ID); err != nil {
				stats.dbErrors++
				s.log.WithError(err).WithField("image_id", r.im.ID).Warn("optimizer: failed to mark empty image optimized")
			} else {
				stats.empty++
				stats.marked++
			}
			continue
		}
		if r.err != nil {
			stats.decodeErrors++
			s.log.WithError(r.err).WithField("image_id", r.im.ID).Warn("optimizer: skip image (decode/encode error)")
			continue
		}
		if r.same {
			if err := s.store.MarkImageOptimizedNoChange(r.im.ID); err != nil {
				stats.dbErrors++
				s.log.WithError(err).WithField("image_id", r.im.ID).Warn("optimizer: failed to mark optimized")
			} else {
				stats.marked++
			}
			continue
		}
		if err := s.store.UpdateImageOptimizedData(r.im.ID, r.data, r.mime); err != nil {
			stats.dbErrors++
			s.log.WithError(err).WithField("image_id", r.im.ID).Warn("optimizer: failed to update image")
		} else {
			stats.resized++
		}
	}
	return stats, nil
}

// optimizeBytes resizes/encodes the image if beneficial.
// Returns (data, same, outMIME, err). If same=true, caller should not rewrite bytes.
func optimizeBytes(b []byte, mime string, maxW, maxH int) ([]byte, bool, string, error) {
	// Decode image; support WebP input explicitly
	var img image.Image
	lower := strings.ToLower(mime)
	if strings.Contains(lower, "webp") {
		var derr error
		img, derr = xwebp.Decode(bytes.NewReader(b))
		if derr != nil {
			// fallback to generic decode
			var err2 error
			img, _, err2 = image.Decode(bytes.NewReader(b))
			if err2 != nil {
				return nil, false, "", err2
			}
		}
	} else {
		var err error
		img, _, err = image.Decode(bytes.NewReader(b))
		if err != nil {
			// If generic decode fails, try WebP as a fallback (e.g., wrong MIME stored)
			if img2, err2 := xwebp.Decode(bytes.NewReader(b)); err2 == nil {
				img = img2
			} else {
				return nil, false, "", err
			}
		}
	}

	// Apply EXIF orientation for JPEGs (best-effort)
	img = applyEXIFOrientation(b, img)

	bounds := img.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()
	if w <= 0 || h <= 0 {
		return nil, false, "", errors.New("invalid image dimensions")
	}
	// Calculate scale preserving aspect ratio
	sw := float64(maxW) / float64(w)
	sh := float64(maxH) / float64(h)
	scale := math.Min(sw, sh)

	encodeWebP := func(src image.Image) ([]byte, error) {
		var out bytes.Buffer
		if err := webp.Encode(&out, src, &webp.Options{Lossless: false, Quality: float32(imgOptWebPQuality)}); err != nil {
			return nil, err
		}
		return out.Bytes(), nil
	}

	if scale >= 1.0 {
		// No resize needed — only re-encode if there is a noticeable gain
		newData, err := encodeWebP(img)
		if err != nil {
			return nil, false, "", err
		}
		if len(b) > 0 {
			gain := float64(len(b)-len(newData)) / float64(len(b))
			if gain < imgOptMinGainRatio {
				return b, true, mime, nil
			}
		}
		return newData, false, "image/webp", nil
	}

	// Need to downscale
	newW := int(math.Max(1, math.Round(float64(w)*scale)))
	newH := int(math.Max(1, math.Round(float64(h)*scale)))
	resized := resizeHighQuality(img, newW, newH)
	newData, err := encodeWebP(resized)
	if err != nil {
		return nil, false, "", err
	}
	if len(b) > 0 {
		gain := float64(len(b)-len(newData)) / float64(len(b))
		if gain < imgOptMinGainRatio {
			return b, true, mime, nil
		}
	}
	return newData, false, "image/webp", nil
}

// resizeHighQuality performs a simple bilinear downscale to dstW x dstH.
func resizeHighQuality(src image.Image, dstW, dstH int) image.Image {
	if dstW <= 0 || dstH <= 0 {
		return image.NewRGBA(image.Rect(0, 0, 0, 0))
	}
	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	b := src.Bounds()
	srcW := b.Dx()
	srcH := b.Dy()
	if srcW == 0 || srcH == 0 {
		return dst
	}
	sx := float64(srcW) / float64(dstW)
	sy := float64(srcH) / float64(dstH)
	for y := 0; y < dstH; y++ {
		fy := (float64(y)+0.5)*sy - 0.5
		y0 := int(math.Floor(fy))
		if y0 < 0 {
			y0 = 0
		}
		y1 := y0 + 1
		if y1 >= srcH {
			y1 = srcH - 1
		}
		wy := fy - float64(y0)
		for x := 0; x < dstW; x++ {
			fx := (float64(x)+0.5)*sx - 0.5
			x0 := int(math.Floor(fx))
			if x0 < 0 {
				x0 = 0
			}
			x1 := x0 + 1
			if x1 >= srcW {
				x1 = srcW - 1
			}
			wx := fx - float64(x0)

			c00 := colorRGBA(src.At(b.Min.X+x0, b.Min.Y+y0))
			c10 := colorRGBA(src.At(b.Min.X+x1, b.Min.Y+y0))
			c01 := colorRGBA(src.At(b.Min.X+x0, b.Min.Y+y1))
			c11 := colorRGBA(src.At(b.Min.X+x1, b.Min.Y+y1))

			r := (1-wx)*(1-wy)*float64(c00.R) + wx*(1-wy)*float64(c10.R) + (1-wx)*wy*float64(c01.R) + wx*wy*float64(c11.R)
			g := (1-wx)*(1-wy)*float64(c00.G) + wx*(1-wy)*float64(c10.G) + (1-wx)*wy*float64(c01.G) + wx*wy*float64(c11.G)
			bch := (1-wx)*(1-wy)*float64(c00.B) + wx*(1-wy)*float64(c10.B) + (1-wx)*wy*float64(c01.B) + wx*wy*float64(c11.B)
			a := (1-wx)*(1-wy)*float64(c00.A) + wx*(1-wy)*float64(c10.A) + (1-wx)*wy*float64(c01.A) + wx*wy*float64(c11.A)

			dst.Set(x, y, color.RGBA{R: uint8(clamp0_255(r)), G: uint8(clamp0_255(g)), B: uint8(clamp0_255(bch)), A: uint8(clamp0_255(a))})
		}
	}
	return dst
}

// colorRGBA converts a color.Color to non-premultiplied RGBA.
func colorRGBA(c color.Color) color.RGBA {
	r, g, b, a := c.RGBA()
	// RGBA returns values in 16-bit [0, 65535]; convert to 8-bit [0, 255]
	return color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: uint8(a >> 8)}
}

func clamp0_255(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return v
}

// applyEXIFOrientation rotates/flips the image according to EXIF Orientation (JPEG only).
func applyEXIFOrientation(b []byte, img image.Image) image.Image {
	if !looksLikeJPEG(b) {
		return img
	}
	if ori, ok := parseEXIFOrientationJPEG(b); ok {
		if ori >= 2 && ori <= 8 {
			return transformByOrientation(img, ori)
		}
	}
	return img
}

func looksLikeJPEG(b []byte) bool {
	return len(b) >= 2 && b[0] == 0xFF && b[1] == 0xD8
}

// parseEXIFOrientationJPEG extracts EXIF Orientation from a JPEG APP1 Exif segment.
func parseEXIFOrientationJPEG(b []byte) (int, bool) {
	if !looksLikeJPEG(b) {
		return 1, false
	}
	i := 2
	for i+4 <= len(b) {
		if b[i] != 0xFF {
			break
		}
		i++
		if i >= len(b) {
			break
		}
		marker := b[i]
		i++
		// SOS or EOI – stop
		if marker == 0xDA || marker == 0xD9 {
			break
		}
		if i+2 > len(b) {
			break
		}
		segLen := int(binary.BigEndian.Uint16(b[i : i+2]))
		i += 2
		if segLen < 2 || i+segLen-2 > len(b) {
			break
		}
		if marker == 0xE1 && segLen >= 10 { // APP1
			seg := b[i : i+segLen-2]
			if len(seg) >= 6 && string(seg[:6]) == "Exif\x00\x00" {
				tiff := seg[6:]
				if ori, ok := parseTIFFOrientation(tiff); ok {
					return ori, true
				}
			}
		}
		i += segLen - 2
	}
	return 1, false
}

func parseTIFFOrientation(tiff []byte) (int, bool) {
	if len(tiff) < 8 {
		return 1, false
	}
	le := false
	sig := string(tiff[:2])
	switch sig {
	case "II":
		le = true
	case "MM":
		le = false
	default:
		return 1, false
	}
	u16 := func(p int) uint16 {
		if p+2 > len(tiff) {
			return 0
		}
		if le {
			return binary.LittleEndian.Uint16(tiff[p : p+2])
		}
		return binary.BigEndian.Uint16(tiff[p : p+2])
	}
	u32 := func(p int) uint32 {
		if p+4 > len(tiff) {
			return 0
		}
		if le {
			return binary.LittleEndian.Uint32(tiff[p : p+4])
		}
		return binary.BigEndian.Uint32(tiff[p : p+4])
	}
	if u16(2) != 42 {
		return 1, false
	}
	off0 := int(u32(4))
	if off0 <= 0 || off0+2 > len(tiff) {
		return 1, false
	}
	count := int(u16(off0))
	pos := off0 + 2
	for i := 0; i < count; i++ {
		if pos+12 > len(tiff) {
			break
		}
		tag := u16(pos)
		typ := u16(pos + 2)
		cnt := u32(pos + 4)
		valOff := pos + 8
		if tag == 0x0112 && typ == 3 && cnt >= 1 { // Orientation, SHORT
			val := u16(valOff)
			if val >= 1 && val <= 8 {
				return int(val), true
			}
		}
		pos += 12
	}
	return 1, false
}

func transformByOrientation(src image.Image, ori int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	switch ori {
	case 2: // flip horizontal
		dst := image.NewRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				dst.Set(w-1-x, y, src.At(b.Min.X+x, b.Min.Y+y))
			}
		}
		return dst
	case 3: // rotate 180
		dst := image.NewRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				dst.Set(w-1-x, h-1-y, src.At(b.Min.X+x, b.Min.Y+y))
			}
		}
		return dst
	case 4: // flip vertical
		dst := image.NewRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				dst.Set(x, h-1-y, src.At(b.Min.X+x, b.Min.Y+y))
			}
		}
		return dst
	case 5: // transpose
		dst := image.NewRGBA(image.Rect(0, 0, h, w))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				dst.Set(y, x, src.At(b.Min.X+x, b.Min.Y+y))
			}
		}
		return dst
	case 6: // rotate 90 CW
		dst := image.NewRGBA(image.Rect(0, 0, h, w))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				dst.Set(h-1-y, x, src.At(b.Min.X+x, b.Min.Y+y))
			}
		}
		return dst
	case 7: // transverse
		dst := image.NewRGBA(image.Rect(0, 0, h, w))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				dst.Set(h-1-y, w-1-x, src.At(b.Min.X+x, b.Min.Y+y))
			}
		}
		return dst
	case 8: // rotate 90 CCW
		dst := image.NewRGBA(image.Rect(0, 0, h, w))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				dst.Set(y, w-1-x, src.At(b.Min.X+x, b.Min.Y+y))
			}
		}
		return dst
	default:
		return src
	}
}
