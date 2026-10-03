// Command genicons draws the Push-Up Counter app icons.
//
// The mark is a tally: four upright strokes crossed by a fifth. It is authored
// here rather than pulled from an icon set so the geometry matches the FLOOR
// world (hard edges, no rounding, oxide orange on ink).
//
// Run with: go run ./tools/genicons
package main

import (
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"
)

var (
	ink    = color.NRGBA{0x07, 0x08, 0x07, 0xff}
	signal = color.NRGBA{0xff, 0x4b, 0x12, 0xff}
	bone   = color.NRGBA{0xef, 0xed, 0xe6, 0xff}
)

func main() {
	out := filepath.Join("web", "icons")
	if err := os.MkdirAll(out, 0o755); err != nil {
		panic(err)
	}
	targets := []struct {
		name  string
		size  int
		inset float64 // fraction of the canvas kept clear around the mark
	}{
		{"icon-192.png", 192, 0.14},
		{"icon-512.png", 512, 0.14},
		// Maskable icons are cropped to a circle on Android, so the mark sits
		// inside the 80% safe zone.
		{"icon-512-maskable.png", 512, 0.26},
		{"apple-touch-icon-180.png", 180, 0.16},
	}
	for _, t := range targets {
		img := render(t.size, t.inset)
		f, err := os.Create(filepath.Join(out, t.name))
		if err != nil {
			panic(err)
		}
		if err := png.Encode(f, img); err != nil {
			panic(err)
		}
		f.Close()
	}
}

func render(size int, inset float64) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	draw.Draw(img, img.Bounds(), &image.Uniform{ink}, image.Point{}, draw.Src)

	s := float64(size)
	pad := s * inset
	box := s - 2*pad

	// Four upright strokes.
	strokeW := box * 0.108
	gap := (box - 4*strokeW) / 3
	top := pad + box*0.14
	bottom := pad + box*0.86

	for i := 0; i < 4; i++ {
		x := pad + float64(i)*(strokeW+gap)
		fillRect(img, x, top, x+strokeW, bottom, signal)
	}

	// The fifth stroke crosses the other four.
	thickLine(img, pad-strokeW*0.35, bottom-box*0.10, pad+box+strokeW*0.35, top+box*0.10, strokeW*1.05, bone)
	return img
}

func fillRect(img *image.NRGBA, x0, y0, x1, y1 float64, c color.NRGBA) {
	for y := int(math.Floor(y0)); y < int(math.Ceil(y1)); y++ {
		for x := int(math.Floor(x0)); x < int(math.Ceil(x1)); x++ {
			cov := coverage(float64(x), float64(y), x0, y0, x1, y1)
			blend(img, x, y, c, cov)
		}
	}
}

// coverage approximates antialiasing by measuring how much of the pixel square
// falls inside the rectangle.
func coverage(px, py, x0, y0, x1, y1 float64) float64 {
	w := math.Min(px+1, x1) - math.Max(px, x0)
	h := math.Min(py+1, y1) - math.Max(py, y0)
	if w <= 0 || h <= 0 {
		return 0
	}
	return math.Min(w, 1) * math.Min(h, 1)
}

// thickLine draws a square-capped line: each pixel is transformed into the
// segment's local frame, so the ends are cut flat instead of rounded.
func thickLine(img *image.NRGBA, x0, y0, x1, y1, width float64, c color.NRGBA) {
	half := width / 2
	dx, dy := x1-x0, y1-y0
	length := math.Hypot(dx, dy)
	if length == 0 {
		return
	}
	ux, uy := dx/length, dy/length
	cx, cy := (x0+x1)/2, (y0+y1)/2
	halfLen := length / 2

	minX := int(math.Floor(math.Min(x0, x1) - width - 1))
	maxX := int(math.Ceil(math.Max(x0, x1) + width + 1))
	minY := int(math.Floor(math.Min(y0, y1) - width - 1))
	maxY := int(math.Ceil(math.Max(y0, y1) + width + 1))

	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			px, py := float64(x)+0.5-cx, float64(y)+0.5-cy
			along := math.Abs(px*ux + py*uy)
			across := math.Abs(-px*uy + py*ux)
			// One pixel of feathering keeps the diagonal from looking ragged.
			cov := math.Min(
				math.Max(0, math.Min(1, half+0.5-across)),
				math.Max(0, math.Min(1, halfLen+0.5-along)),
			)
			blend(img, x, y, c, cov)
		}
	}
}

func blend(img *image.NRGBA, x, y int, c color.NRGBA, alpha float64) {
	if alpha <= 0 || !(image.Point{x, y}.In(img.Bounds())) {
		return
	}
	if alpha > 1 {
		alpha = 1
	}
	dst := img.NRGBAAt(x, y)
	mix := func(a, b uint8) uint8 {
		return uint8(math.Round(float64(a)*(1-alpha) + float64(b)*alpha))
	}
	img.SetNRGBA(x, y, color.NRGBA{mix(dst.R, c.R), mix(dst.G, c.G), mix(dst.B, c.B), 0xff})
}
