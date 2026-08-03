//go:build ignore

// genicon writes assets/linefire-skirmish.iconset from the game's own art, for
// iconutil to turn into the .icns the release script bundles into the macOS
// app.
//
// It is a build script, not a command: the //go:build ignore tag keeps it out
// of every build and out of cmd/, which matters more here than anywhere —
// this repository ships ONE executable, and a generator is not it. Run it
// through the Makefile:
//
//	make icons
//
// The face is chosen in internal/icon and drawn by linefire; this only writes
// the sizes out.
package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/crgimenes/linefire-skirmish/internal/icon"
)

// The sizes an .iconset must carry, in Apple's own naming: each logical size
// twice, once at 1x and once at 2x. Every one is DRAWN rather than scaled from
// a big bitmap, which is the whole point of having a rasterizer.
var sizes = []struct {
	name string
	px   int
}{
	{"icon_16x16.png", 16},
	{"icon_16x16@2x.png", 32},
	{"icon_32x32.png", 32},
	{"icon_32x32@2x.png", 64},
	{"icon_128x128.png", 128},
	{"icon_128x128@2x.png", 256},
	{"icon_256x256.png", 256},
	{"icon_256x256@2x.png", 512},
	{"icon_512x512.png", 512},
	{"icon_512x512@2x.png", 1024},
}

func main() {
	out := "assets/linefire-skirmish.iconset"
	if err := os.MkdirAll(out, 0o750); err != nil {
		log.Fatalf("genicon: %v", err)
	}
	for _, s := range sizes {
		b, err := icon.PNG(s.px)
		if err != nil {
			log.Fatalf("genicon: %v", err)
		}
		if err := os.WriteFile(filepath.Join(out, s.name), b, 0o600); err != nil {
			log.Fatalf("genicon: %v", err)
		}
	}

	// The same face as a plain PNG, next to the .icns: what a README or a
	// store page wants, and what a human opens to look at the thing.
	b, err := icon.PNG(1024)
	if err != nil {
		log.Fatalf("genicon: %v", err)
	}
	if err := os.WriteFile("assets/linefire-skirmish.png", b, 0o600); err != nil {
		log.Fatalf("genicon: %v", err)
	}
	fmt.Printf("wrote %s (%d sizes) and assets/linefire-skirmish.png\n", out, len(sizes))
}
