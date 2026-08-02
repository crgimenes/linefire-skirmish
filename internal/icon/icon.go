// Package icon draws the application's face: the ship itself.
//
// Not a picture of the ship — THE ship. The art is gameassets/enemy.lfa, the
// same vector hull every faction flies, read through linefire's own loader and
// filled here; the colour is game.FactionColor(1), the same red the first fleet
// wears on the battlefield. Nothing is re-derived and nothing is drawn by hand,
// so an icon that stopped looking like the game would mean the game changed.
//
// The rasterizer is a scanline fill over the flattened outline, rendered at
// several times the requested size and averaged down. That is all a polygon
// needs, and it is why this package has no dependencies: an image library to
// fill one closed path is a dependency bought for nothing.
package icon

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"sort"
	"sync"

	"github.com/crgimenes/linefire"
	"github.com/crgimenes/linefire/asset"
	"github.com/crgimenes/linefire/filoio"
	"github.com/crgimenes/linefire/game"
)

const (
	// The hull fills this much of the icon's width. The rest is margin: a mark
	// that runs to its own edges reads as cropped at Dock size.
	hullSpan = 0.72
	// Supersampling. Four is where the diagonal edges of the hull stop looking
	// stepped at 32px, which is the smallest size that has to survive.
	superSample = 4
	// The field the ship sits on: the workshop's own near-black, not pure
	// black, so the icon still has an edge against a dark Dock.
	fieldR, fieldG, fieldB = 0x0b, 0x12, 0x1a
	// The corner radius, as a fraction of the icon. Roughly the platform
	// convention, and it is what keeps the mark from reading as a screenshot.
	cornerRadius = 0.22
)

// pt is a point in icon space (pixels of the supersampled canvas).
type pt struct{ x, y float64 }

// Image draws the icon at size×size pixels.
//
// Icons are asked for at a handful of fixed sizes and never change during a
// run, so each one is built once. The art is embedded in the linefire module,
// which means this cannot fail for want of a file — but it CAN fail if that
// asset is renamed, and an icon is never worth taking a program down for, so
// callers get an image either way (see fallbackShip).
func Image(size int) image.Image {
	if size < 1 {
		size = 1
	}
	v, _ := cache.LoadOrStore(size, draw(size))
	return v.(image.Image)
}

// Images returns the icon at every size given — the shape ebiten and the
// window managers want, so they can pick per surface (taskbar, title bar,
// switcher) instead of scaling one bitmap badly.
func Images(sizes ...int) []image.Image {
	out := make([]image.Image, 0, len(sizes))
	for _, s := range sizes {
		out = append(out, Image(s))
	}
	return out
}

// PNG encodes the icon, which is what the platform APIs that take bytes want.
func PNG(size int) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, Image(size)); err != nil {
		return nil, fmt.Errorf("icon: encoding %dpx: %w", size, err)
	}
	return buf.Bytes(), nil
}

var cache sync.Map // int -> image.Image

// hull loads the ship once: the outline in asset space, plus the asset's
// nominal size so it can be centred on its own box rather than on its ink.
func hull() (paths []asset.Path, w, h float64) {
	shipOnce.Do(func() {
		a, err := filoio.LoadAssetFS(linefire.Content(), "gameassets", "enemy")
		if err != nil {
			shipPaths, shipW, shipH = fallbackShip()
			return
		}
		shipW, shipH = a.Size.W, a.Size.H
		for _, l := range a.Layers {
			if l.Hidden {
				continue
			}
			for _, p := range l.Paths {
				shipPaths = append(shipPaths, p.Flatten())
			}
		}
		if len(shipPaths) == 0 {
			shipPaths, shipW, shipH = fallbackShip()
		}
	})
	return shipPaths, shipW, shipH
}

var (
	shipOnce      sync.Once
	shipPaths     []asset.Path
	shipW, shipH  float64
	fallbackNoted bool
)

// fallbackShip is the outline to draw when the real one cannot be read. It
// exists so a renamed asset costs the icon its fidelity and nothing else: a
// program that refuses to start because it could not draw its own icon would
// be a worse bug than a plain triangle.
func fallbackShip() ([]asset.Path, float64, float64) {
	fallbackNoted = true
	return []asset.Path{{Commands: []asset.Command{
		{Op: asset.OpMoveTo, X: 32, Y: 6},
		{Op: asset.OpLineTo, X: 56, Y: 50},
		{Op: asset.OpLineTo, X: 32, Y: 40},
		{Op: asset.OpLineTo, X: 8, Y: 50},
		{Op: asset.OpClose},
	}}}, 64, 64
}

// draw builds one icon: the field, then the hull's glow, body and outline, in
// the order the game itself draws a ship.
func draw(size int) image.Image {
	n := size * superSample
	big := image.NewRGBA(image.Rect(0, 0, n, n))

	field := color.RGBA{R: fieldR, G: fieldG, B: fieldB, A: 0xff}
	fillRounded(big, float64(n)*cornerRadius, field)

	paths, aw, ah := hull()
	scale := float64(n) * hullSpan / math.Max(aw, ah)
	offX := (float64(n) - aw*scale) / 2
	offY := (float64(n) - ah*scale) / 2

	team := game.FactionColor(1) // fleet one's red: colour says faction, here too
	// The hull's own dark body: the team colour taken most of the way down, so
	// the outline reads as a lit edge around something solid.
	body := color.RGBA{R: team.R / 5, G: team.G / 5, B: team.B / 5, A: 0xff}
	line := float64(n) * 0.022

	// The halo is drawn OPAQUE into a layer of its own and composited once.
	// Painting it translucently in place instead lets every overlap between a
	// segment and its round join blend twice, and the hull comes out ringed
	// with brighter beads at exactly the corners.
	halo := image.NewRGBA(big.Rect)
	for _, p := range paths {
		poly := project(p, scale, offX, offY)
		if len(poly) < 3 {
			continue
		}
		strokePoly(halo, poly, line*3.2, team)
	}
	composite(big, halo, 0x38)

	for _, p := range paths {
		poly := project(p, scale, offX, offY)
		if len(poly) < 3 {
			continue
		}
		fillPoly(big, poly, body)
		strokePoly(big, poly, line, team)
	}
	return downsample(big, size)
}

// project turns one flattened path into icon-space points.
func project(p asset.Path, scale, offX, offY float64) []pt {
	out := make([]pt, 0, len(p.Commands))
	for _, c := range p.Commands {
		if c.Op == asset.OpClose {
			continue
		}
		out = append(out, pt{x: offX + c.X*scale, y: offY + c.Y*scale})
	}
	return out
}

// blend paints one pixel with src over what is already there.
func blend(dst *image.RGBA, x, y int, c color.RGBA) {
	if !(image.Point{X: x, Y: y}).In(dst.Rect) {
		return
	}
	if c.A == 0xff {
		dst.SetRGBA(x, y, c)
		return
	}
	a := float64(c.A) / 255
	old := dst.RGBAAt(x, y)
	mix := func(s, d uint8) uint8 {
		return uint8(float64(s)*a + float64(d)*(1-a))
	}
	dst.SetRGBA(x, y, color.RGBA{
		R: mix(c.R, old.R), G: mix(c.G, old.G), B: mix(c.B, old.B),
		A: max(c.A, old.A),
	})
}

// fillPoly fills a closed polygon by the even-odd rule: for each scanline,
// every edge it crosses contributes a crossing, and the spans between pairs of
// sorted crossings are inside. Antialiasing is the supersample's job.
func fillPoly(dst *image.RGBA, ps []pt, c color.RGBA) {
	minY, maxY := ps[0].y, ps[0].y
	for _, p := range ps {
		minY, maxY = math.Min(minY, p.y), math.Max(maxY, p.y)
	}
	y0 := max(int(math.Floor(minY)), dst.Rect.Min.Y)
	y1 := min(int(math.Ceil(maxY)), dst.Rect.Max.Y-1)

	var xs []float64
	for y := y0; y <= y1; y++ {
		yc := float64(y) + 0.5
		xs = xs[:0]
		for i := range ps {
			a, b := ps[i], ps[(i+1)%len(ps)]
			// A crossing counts when the scanline separates the endpoints. The
			// half-open comparison is what keeps a vertex exactly on the line
			// from counting twice and inverting the whole span.
			if (a.y <= yc) == (b.y <= yc) {
				continue
			}
			xs = append(xs, a.x+(yc-a.y)/(b.y-a.y)*(b.x-a.x))
		}
		sort.Float64s(xs)
		for i := 0; i+1 < len(xs); i += 2 {
			for x := int(math.Round(xs[i])); float64(x)+0.5 < xs[i+1]; x++ {
				blend(dst, x, y, c)
			}
		}
	}
}

// strokePoly draws the polygon's outline by filling one quad per segment, with
// a ROUND join at each vertex. A square join is the obvious shortcut and it is
// wrong: at the halo's width the corners stick out as blocks, and the ship
// ends up wearing five dark boxes instead of a glow.
func strokePoly(dst *image.RGBA, ps []pt, width float64, c color.RGBA) {
	h := width / 2
	for i := range ps {
		a, b := ps[i], ps[(i+1)%len(ps)]
		dx, dy := b.x-a.x, b.y-a.y
		l := math.Hypot(dx, dy)
		if l == 0 {
			continue
		}
		nx, ny := -dy/l*h, dx/l*h
		fillPoly(dst, []pt{
			{a.x + nx, a.y + ny}, {b.x + nx, b.y + ny},
			{b.x - nx, b.y - ny}, {a.x - nx, a.y - ny},
		}, c)
		fillPoly(dst, disc(a, h), c)
	}
}

// disc approximates a circle closely enough that no edge of it survives the
// downsample as a straight line.
func disc(at pt, r float64) []pt {
	const sides = 16
	out := make([]pt, sides)
	for i := range sides {
		th := 2 * math.Pi * float64(i) / sides
		out[i] = pt{x: at.x + r*math.Cos(th), y: at.y + r*math.Sin(th)}
	}
	return out
}

// composite lays one whole layer over another at a uniform alpha, so however
// many shapes drew into it, each pixel is blended exactly once.
func composite(dst, src *image.RGBA, alpha uint8) {
	for y := dst.Rect.Min.Y; y < dst.Rect.Max.Y; y++ {
		for x := dst.Rect.Min.X; x < dst.Rect.Max.X; x++ {
			p := src.RGBAAt(x, y)
			if p.A == 0 {
				continue
			}
			blend(dst, x, y, color.RGBA{R: p.R, G: p.G, B: p.B, A: alpha})
		}
	}
}

// fillRounded paints the whole canvas with rounded corners.
func fillRounded(dst *image.RGBA, r float64, c color.RGBA) {
	b := dst.Rect
	w, h := float64(b.Dx()), float64(b.Dy())
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			px, py := float64(x)+0.5, float64(y)+0.5
			// Distance to the nearest corner circle's centre, but only in the
			// corner quadrants: everywhere else the pixel is simply inside.
			cx := math.Min(math.Max(px, r), w-r)
			cy := math.Min(math.Max(py, r), h-r)
			if math.Hypot(px-cx, py-cy) > r {
				continue
			}
			dst.SetRGBA(x, y, c)
		}
	}
}

// downsample averages each block back to one pixel — the antialiasing.
func downsample(src *image.RGBA, size int) image.Image {
	out := image.NewRGBA(image.Rect(0, 0, size, size))
	area := superSample * superSample
	for y := range size {
		for x := range size {
			var r, g, b, a int
			for dy := range superSample {
				for dx := range superSample {
					p := src.RGBAAt(x*superSample+dx, y*superSample+dy)
					r, g, b, a = r+int(p.R), g+int(p.G), b+int(p.B), a+int(p.A)
				}
			}
			out.SetRGBA(x, y, color.RGBA{
				R: uint8(r / area), G: uint8(g / area), B: uint8(b / area), A: uint8(a / area),
			})
		}
	}
	return out
}
