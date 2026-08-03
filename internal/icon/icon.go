// Package icon says what this program's face IS. The drawing lives in
// linefire (icon.Ship), because everything about how this game looks comes
// from there and is never re-derived here.
//
// The choice made here: a FACTION hull, in fleet one's red. The campaign wears
// the player's own cyan, and the difference is the products' — in skirmish you
// do not fly a ship, you write a program and a faction flies for you, so the
// face is a faction's, and colour says which faction on the Dock exactly as it
// does on the battlefield.
package icon

import (
	"image"
	"sync"

	"github.com/crgimenes/linefire"
	"github.com/crgimenes/linefire/game"
	lficon "github.com/crgimenes/linefire/icon"
)

// The art this face is made of: the hull every faction flies, in the colour of
// the first one.
const (
	assetDir = "gameassets"
	assetRef = "enemy"
)

// Image draws the icon at size×size pixels. Icons are asked for at a handful
// of fixed sizes and never change during a run, so each is drawn once.
func Image(size int) image.Image {
	if size < 1 {
		size = 1
	}
	v, _ := cache.LoadOrStore(size, draw(size))
	return v.(image.Image)
}

// Images returns the icon at every size given — the shape Ebitengine and the
// window managers want, so each surface (taskbar, title bar, switcher) gets a
// drawing instead of a scaled bitmap.
func Images(sizes ...int) []image.Image {
	out := make([]image.Image, 0, len(sizes))
	for _, s := range sizes {
		out = append(out, Image(s))
	}
	return out
}

// PNG encodes the icon, which is what the platform APIs that take bytes want.
func PNG(size int) ([]byte, error) {
	return lficon.PNG(linefire.Content(), assetDir, assetRef, game.FactionColor(1), size)
}

var cache sync.Map // int -> image.Image

func draw(size int) image.Image {
	return lficon.Ship(linefire.Content(), assetDir, assetRef, game.FactionColor(1), size)
}
