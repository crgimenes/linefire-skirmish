package icon

import (
	"image/color"
	"testing"

	"github.com/crgimenes/linefire/game"
)

// The rasterizer is linefire's and tested there. What belongs here is the
// CHOICE: this program's face is a faction hull in fleet one's colour, the
// same red that fleet wears on the battlefield. A change that quietly made the
// icon something else — the player's cyan, say, or an untinted asset — would
// pass every test in linefire and still be wrong here.
func TestTheFaceIsAFactionHullInFleetOnesColour(t *testing.T) {
	const size = 128
	img := Image(size)
	if img.Bounds().Dx() != size || img.Bounds().Dy() != size {
		t.Fatalf("icon is %v, want %dx%d", img.Bounds(), size, size)
	}

	team := game.FactionColor(1)
	near := func(c color.RGBA) bool {
		d := func(a, b uint8) int { return int(a) - int(b) }
		return abs(d(c.R, team.R)) < 60 && abs(d(c.G, team.G)) < 60 && abs(d(c.B, team.B)) < 60
	}
	ink := 0
	for y := range size {
		for x := range size {
			r, g, b, _ := img.At(x, y).RGBA()
			if near(color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8)}) {
				ink++
			}
		}
	}
	if ink < 200 {
		t.Fatalf("only %d pixels wear fleet one's colour: the hull is missing or wears something else", ink)
	}
	if _, _, _, a := img.At(size/2, size/2).RGBA(); a == 0 {
		t.Error("the middle of the icon is empty")
	}
}

// Every size the window manager may ask for has to come out drawn.
func TestTheIconAnswersEverySizeAsked(t *testing.T) {
	for _, s := range []int{16, 32, 48, 64, 128, 256} {
		if got := Image(s).Bounds().Dx(); got != s {
			t.Errorf("asked for %dpx, got %dpx", s, got)
		}
	}
	if len(Images(16, 32)) != 2 {
		t.Error("Images must answer one image per size")
	}
	if _, err := PNG(64); err != nil {
		t.Fatalf("encoding: %v", err)
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
