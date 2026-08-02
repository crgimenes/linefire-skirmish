package icon

import (
	"image/color"
	"os"
	"path/filepath"
	"testing"

	"github.com/crgimenes/linefire/game"
)

// The icon must be the SHIP, drawn from linefire's own vector art. The
// assertions are the things that would silently stop being true: the art was
// found (not the fallback outline), the hull wears fleet one's colour, and it
// sits inside the field rather than running off it.
func TestTheIconIsTheShipInTheFleetColour(t *testing.T) {
	const size = 128
	img := Image(size)
	if img.Bounds().Dx() != size || img.Bounds().Dy() != size {
		t.Fatalf("icon is %v, want %dx%d", img.Bounds(), size, size)
	}
	if _, _, _ = hull(); fallbackNoted {
		t.Fatal("the ship asset could not be read: the icon fell back to a hand-drawn outline")
	}

	team := game.FactionColor(1)
	near := func(c color.RGBA) bool {
		d := func(a, b uint8) int { return int(a) - int(b) }
		return abs(d(c.R, team.R)) < 60 && abs(d(c.G, team.G)) < 60 && abs(d(c.B, team.B)) < 60
	}
	teamPixels := 0
	for y := range size {
		for x := range size {
			r, g, b, _ := img.At(x, y).RGBA()
			if near(color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8)}) {
				teamPixels++
			}
		}
	}
	// The outline alone is a few hundred pixels at this size; anything near
	// zero means the hull was not drawn at all.
	if teamPixels < 200 {
		t.Fatalf("only %d pixels wear the fleet colour: the hull is missing", teamPixels)
	}

	// The corners are the rounded field, so they must be transparent, and the
	// centre must be ink: an icon drawn off its own canvas passes every other
	// check here.
	if _, _, _, a := img.At(1, 1).RGBA(); a != 0 {
		t.Errorf("the corner is opaque: the field is not rounded")
	}
	if _, _, _, a := img.At(size/2, size/2).RGBA(); a == 0 {
		t.Errorf("the middle of the icon is empty")
	}
}

// Every size a window manager might ask for has to come out drawn, not scaled
// from one bitmap — 16px is where a bad rasterizer shows.
func TestTheIconDrawsAtEverySizeItIsAskedFor(t *testing.T) {
	for _, s := range []int{16, 32, 64, 128, 256, 512} {
		img := Image(s)
		if img.Bounds().Dx() != s {
			t.Fatalf("asked for %dpx, got %v", s, img.Bounds())
		}
		ink := 0
		for y := range s {
			for x := range s {
				if _, _, _, a := img.At(x, y).RGBA(); a > 0 {
					ink++
				}
			}
		}
		if ink < s*s/2 {
			t.Errorf("%dpx icon is mostly empty (%d of %d pixels)", s, ink, s*s)
		}
	}
	if _, err := PNG(64); err != nil {
		t.Fatalf("encoding: %v", err)
	}
}

// Writing the icon out is how it gets LOOKED at, which no assertion replaces.
//
//	go test ./internal/icon/ -run Write -icons /tmp
func TestWriteTheIconsForAHumanToLookAt(t *testing.T) {
	dir := os.Getenv("SKIRMISH_ICON_DIR")
	if dir == "" {
		t.Skip("SKIRMISH_ICON_DIR is not set: nothing to write")
	}
	for _, s := range []int{16, 32, 128, 512} {
		b, err := PNG(s)
		if err != nil {
			t.Fatalf("%dpx: %v", s, err)
		}
		name := filepath.Join(dir, "skirmish-icon.png")
		if s != 512 {
			name = filepath.Join(dir, "skirmish-icon-"+itoa(s)+".png")
		}
		if err := os.WriteFile(name, b, 0o600); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
		t.Logf("wrote %s", name)
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
