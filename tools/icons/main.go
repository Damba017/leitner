// Command icons draws the PNG app icons from the same shapes as web/icon.svg,
// using only the standard library. Run it from the repository root:
//
//	go run ./tools/icons
//
// It writes web/icons/*.png (192, 512, maskable 512 and the 180 px iOS icon).
package main

import (
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"os"
	"path/filepath"
)

// rrect is a rounded rectangle in the 100x100 design space of icon.svg.
type rrect struct {
	x, y, w, h, r float64
	c             color.NRGBA
}

var (
	accent = color.NRGBA{0x23, 0x70, 0x6a, 0xff}
	back   = color.NRGBA{0xa9, 0xdc, 0xd3, 0xff}
	white  = color.NRGBA{0xff, 0xff, 0xff, 0xff}
)

// Same shapes, same order as web/icon.svg (without the background).
var shapes = []rrect{
	{30, 24, 44, 42, 6, back},
	{24, 34, 44, 42, 6, white},
	{31, 46, 30, 4, 2, accent},
	{31, 56, 20, 4, 2, accent},
}

func (s rrect) contains(px, py float64) bool {
	cx, cy := s.x+s.w/2, s.y+s.h/2
	qx := math.Abs(px-cx) - (s.w/2 - s.r)
	qy := math.Abs(py-cy) - (s.h/2 - s.r)
	outside := math.Hypot(math.Max(qx, 0), math.Max(qy, 0))
	return outside+math.Min(math.Max(qx, qy), 0) <= s.r
}

// sample returns the color at a point of the design space; ok is false where
// the icon is transparent. bgRadius < 0 means a full-bleed background.
func sample(px, py, bgRadius float64) (c color.NRGBA, ok bool) {
	if bgRadius >= 0 && !(rrect{0, 0, 100, 100, bgRadius, accent}).contains(px, py) {
		return c, false
	}
	c = accent
	for _, s := range shapes {
		if s.contains(px, py) {
			c = s.c
		}
	}
	return c, true
}

// render draws the icon at size px with 4x4 supersampling.
func render(size int, bgRadius float64) *image.NRGBA {
	const ss = 4
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			var r, g, b, a float64
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					px := (float64(x) + (float64(sx)+0.5)/ss) * 100 / float64(size)
					py := (float64(y) + (float64(sy)+0.5)/ss) * 100 / float64(size)
					if c, ok := sample(px, py, bgRadius); ok {
						r += float64(c.R)
						g += float64(c.G)
						b += float64(c.B)
						a++
					}
				}
			}
			if a > 0 {
				img.SetNRGBA(x, y, color.NRGBA{uint8(r/a + 0.5), uint8(g/a + 0.5), uint8(b/a + 0.5), uint8(a / (ss * ss) * 255)})
			}
		}
	}
	return img
}

func write(name string, img image.Image) {
	f, err := os.Create(filepath.Join("web", "icons", name))
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		log.Fatal(err)
	}
}

func main() {
	if err := os.MkdirAll(filepath.Join("web", "icons"), 0o755); err != nil {
		log.Fatal(err)
	}
	write("icon-192.png", render(192, 22))
	write("icon-512.png", render(512, 22))
	write("icon-maskable-512.png", render(512, -1)) // full bleed; the OS crops it
	write("apple-touch-icon.png", render(180, -1))  // iOS rounds the corners itself
}
