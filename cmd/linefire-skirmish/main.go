// Command linefire-skirmish runs an autonomous space battle over the desktop.
// Hordes of ships from two factions fight on a transparent, click-through
// overlay; there is nothing to play, only a small toolbar to pause, inspect or
// close it. The ships fly themselves.
//
// The battle is a deterministic simulation: the same -seed always produces the
// same fight, so a run can be reproduced, replayed and scored. -ticks runs it
// headless with no window at all, which is how a ship AI is tested.
//
// Usage:
//
//	linefire-skirmish                     battle over the desktop
//	linefire-skirmish -debug              same, with the on-screen numbers
//	linefire-skirmish -json               stream the battle state to stdout
//	linefire-skirmish -ticks 3600 -json   run one minute headless and report
package main

import (
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"math/rand/v2"
	"os"

	"github.com/crgimenes/linefire-skirmish/skirmish"
	"github.com/hajimehoshi/ebiten/v2"
)

//go:embed gameassets
var assets embed.FS

// version is stamped at build time (-X main.version=...).
var version = "dev"

const (
	// The headless arena is fixed on purpose: a -ticks run must not depend on
	// the monitor of the machine that happens to run it.
	headlessWidth  = 1920.0
	headlessHeight = 1080.0

	jsonInterval = 60 // ticks between -json snapshots: one per second at 60 TPS
)

func main() {
	showVersion := flag.Bool("version", false, "print the version and exit")
	asJSON := flag.Bool("json", false, "write one JSON snapshot per second to stdout")
	debug := flag.Bool("debug", false, "log to stderr and show the on-screen numbers")
	ticks := flag.Int("ticks", 0, "run headless for N ticks and exit; no window is opened")
	seed := flag.Uint64("seed", 0, "battle seed; 0 picks one at random")
	perSide := flag.Int("ships", 8, "ships per faction")
	flag.Parse()

	if *showVersion {
		fmt.Println("linefire-skirmish", version)
		return
	}

	if *seed == 0 {
		// Picking which battle to watch, not a secret: the seed is printed and
		// echoed in every -json line precisely so the run can be repeated.
		*seed = rand.Uint64() // #nosec G404
	}
	if *debug {
		log.SetPrefix("linefire-skirmish: ")
		log.Printf("seed %d, %d ships per faction", *seed, *perSide)
	}

	content, err := fs.Sub(assets, ".")
	if err != nil {
		log.Fatalf("linefire-skirmish: %v", err)
	}

	report := reporter(*asJSON)

	if *ticks > 0 {
		runHeadless(*ticks, *seed, *perSide, report)
		return
	}
	if err := runOverlay(content, *seed, *perSide, *debug, report); err != nil {
		log.Fatalf("linefire-skirmish: %v", err)
	}
}

// reporter returns the per-tick hook that emits -json snapshots, or a no-op when
// -json is off. Sampling lives here so both the overlay and the headless run
// report identically.
func reporter(enabled bool) func(*skirmish.World) {
	if !enabled {
		return func(*skirmish.World) {}
	}
	enc := json.NewEncoder(os.Stdout)
	return func(w *skirmish.World) {
		if w.Tick%jsonInterval != 0 {
			return
		}
		if err := enc.Encode(w.Snapshot()); err != nil {
			log.Fatalf("linefire-skirmish: writing json: %v", err)
		}
	}
}

// runHeadless steps the battle without opening a window: the deterministic core
// on its own, for testing ship AI and for scoring one faction against another.
func runHeadless(ticks int, seed uint64, perSide int, report func(*skirmish.World)) {
	w := skirmish.New(skirmish.Config{
		Width:   headlessWidth,
		Height:  headlessHeight,
		PerSide: perSide,
		Seed:    seed,
	})
	for range ticks {
		w.Step()
		report(w)
	}
}

// runOverlay opens the transparent full-monitor window and runs the battle on it.
func runOverlay(content fs.FS, seed uint64, perSide int, debug bool, report func(*skirmish.World)) error {
	const name = "Linefire Skirmish"

	width, height := ebiten.Monitor().Size()
	w := skirmish.New(skirmish.Config{
		Width:   float64(width),
		Height:  float64(height),
		PerSide: perSide,
		Seed:    seed,
	})

	o, err := newOverlay(content, w, width, height, debug, report)
	if err != nil {
		return err
	}

	// Click-through until the cursor reaches the toolbar (see setPassthrough),
	// floating above everything, absent from the taskbar, and running while
	// unfocused — it is a backdrop, not an app the user switches to.
	o.setPassthrough(true)
	ebiten.SetRunnableOnUnfocused(true)
	ebiten.SetWindowDecorated(false)
	ebiten.SetWindowFloating(true)
	ebiten.SetWindowPosition(0, 0)
	ebiten.SetWindowSize(width, height)
	ebiten.SetWindowTitle(name)

	err = ebiten.RunGameWithOptions(o, &ebiten.RunGameOptions{
		InitUnfocused:     true,
		ScreenTransparent: true,
		SkipTaskbar:       true,
		X11ClassName:      name,
		X11InstanceName:   name,
	})
	if _, quit := err.(errQuit); quit {
		return nil
	}
	return err
}
