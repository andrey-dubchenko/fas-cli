//go:build ignore

// genicon draws the fas-cli icon (a stack of schema versions, the live one on top) and writes it as assets/icon.ico (Windows, PNG-compressed
// entries, 16-256 px) and as assets/icon.svg (vector, the IDE project icon), and as PNG files in the freedesktop hicolor layout under assets/icons
// (Linux desktop entries). Run from the repository root: go run assets/genicon.go
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"os"
	"path/filepath"
)

const ss = 4 // supersampling per axis

// roundRect reports whether (x, y) is inside the rounded rectangle.
func roundRect(x, y, x0, y0, x1, y1, r float64) bool {
	if x < x0 || x > x1 || y < y0 || y > y1 {
		return false
	}
	cx := clamp(x, x0+r, x1-r)
	cy := clamp(y, y0+r, y1-r)
	dx, dy := x-cx, y-cy
	return dx*dx+dy*dy <= r*r
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func mix(a, b color.RGBA, t float64) color.RGBA {
	f := func(x, y uint8) uint8 { return uint8(float64(x)*(1-t) + float64(y)*t) }
	return color.RGBA{f(a.R, b.R), f(a.G, b.G), f(a.B, b.B), 255}
}

// pixel returns the color at (u, v) in the unit square, or alpha 0 outside the icon.
func pixel(u, v float64) color.RGBA {
	if !roundRect(u, v, 0.03, 0.03, 0.97, 0.97, 0.22) {
		return color.RGBA{}
	}
	// background: deep teal, lighter at the top
	c := mix(color.RGBA{0x1B, 0x7F, 0x79, 255}, color.RGBA{0x0A, 0x25, 0x2E, 255}, v)

	// a stack of three layers (schema versions): the top one is the live version
	layers := []color.RGBA{
		{0xFF, 0xB7, 0x03, 255},
		{0xE8, 0xF4, 0xF3, 255},
		{0x8E, 0xC9, 0xC5, 255},
	}
	const cx, hw, hh, step, gap = 0.5, 0.36, 0.17, 0.15, 0.06
	inside := func(i int, scale float64) bool {
		cy := 0.36 + float64(i)*step
		return math.Abs(u-cx)/(hw*scale)+math.Abs(v-cy)/(hh*scale) <= 1
	}
	for i, col := range layers {
		if !inside(i, 1) {
			continue
		}
		hidden := false
		for j := 0; j < i; j++ {
			if inside(j, 1+gap/hh*0.5) {
				hidden = true
			}
		}
		if !hidden {
			return col
		}
	}
	return c
}

func render(size int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for py := 0; py < size; py++ {
		for px := 0; px < size; px++ {
			var r, g, b, a float64
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					u := (float64(px) + (float64(sx)+0.5)/ss) / float64(size)
					v := (float64(py) + (float64(sy)+0.5)/ss) / float64(size)
					c := pixel(u, v)
					if c.A == 0 {
						continue
					}
					r += float64(c.R)
					g += float64(c.G)
					b += float64(c.B)
					a++
				}
			}
			if a == 0 {
				continue
			}
			img.SetNRGBA(px, py, color.NRGBA{uint8(r / a), uint8(g / a), uint8(b / a), uint8(255 * a / (ss * ss))})
		}
	}
	return img
}

func main() {
	sizes := []int{16, 24, 32, 48, 64, 128, 256}
	var pngs [][]byte
	for _, s := range sizes {
		var buf bytes.Buffer
		if err := png.Encode(&buf, render(s)); err != nil {
			log.Fatal(err)
		}
		pngs = append(pngs, buf.Bytes())
	}

	var out bytes.Buffer
	binary.Write(&out, binary.LittleEndian, [3]uint16{0, 1, uint16(len(sizes))}) // reserved, type=icon, count
	offset := 6 + 16*len(sizes)
	for i, s := range sizes {
		dim := byte(s)
		if s == 256 {
			dim = 0 // 0 means 256
		}
		out.Write([]byte{dim, dim, 0, 0})
		binary.Write(&out, binary.LittleEndian, [2]uint16{1, 32})                                // planes, bits per pixel
		binary.Write(&out, binary.LittleEndian, [2]uint32{uint32(len(pngs[i])), uint32(offset)}) // size, offset
		offset += len(pngs[i])
	}
	for _, p := range pngs {
		out.Write(p)
	}
	if err := os.WriteFile("assets/icon.ico", out.Bytes(), 0o644); err != nil {
		log.Fatal(err)
	}
	for i, sz := range sizes {
		dir := filepath.Join("assets", "icons", "hicolor", fmt.Sprintf("%dx%d", sz, sz), "apps")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "fas-cli.png"), pngs[i], 0o644); err != nil {
			log.Fatal(err)
		}
	}
	if err := writeSVG(); err != nil {
		log.Fatal(err)
	}
	log.Printf("assets/icon.ico: %d bytes, sizes %v; PNGs in assets/icons/hicolor", out.Len(), sizes)
}

// writeSVG writes the same icon as a vector (assets/icon.svg): used as the IDE project icon
// (.idea/icon.svg). It mirrors pixel(): background, then the layers bottom to top, each upper
// layer preceded by a slightly larger background-colored rhombus that leaves the visible gap.
func writeSVG() error {
	const size = 256.0
	rhombus := func(i int, scale float64) string {
		cx, cy := 0.5*size, (0.36+float64(i)*0.15)*size
		hw, hh := 0.36*size*scale, 0.17*size*scale
		return fmt.Sprintf("%.2f,%.2f %.2f,%.2f %.2f,%.2f %.2f,%.2f", cx-hw, cy, cx, cy-hh, cx+hw, cy, cx, cy+hh)
	}
	gapScale := 1 + 0.06/0.17*0.5
	cols := []string{"#FFB703", "#E8F4F3", "#8EC9C5"}

	var b bytes.Buffer
	fmt.Fprintf(&b, `<?xml version="1.0" encoding="UTF-8"?>`+"\n")
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="256" height="256" viewBox="0 0 256 256" version="1.1">`+"\n")
	fmt.Fprintf(&b, `  <defs><linearGradient id="bg" gradientUnits="userSpaceOnUse" x1="0" y1="0" x2="0" y2="256"><stop offset="0" stop-color="#1B7F79"/><stop offset="1" stop-color="#0A252E"/></linearGradient></defs>`+"\n")
	fmt.Fprintf(&b, `  <rect x="7.68" y="7.68" width="240.64" height="240.64" rx="56.32" fill="url(#bg)"/>`+"\n")
	for i := len(cols) - 1; i >= 0; i-- {
		if i < len(cols)-1 {
			fmt.Fprintf(&b, `  <polygon points="%s" fill="url(#bg)"/>`+"\n", rhombus(i, gapScale))
		}
		fmt.Fprintf(&b, `  <polygon points="%s" fill="%s"/>`+"\n", rhombus(i, 1), cols[i])
	}
	b.WriteString("</svg>\n")
	return os.WriteFile("assets/icon.svg", b.Bytes(), 0o644)
}
