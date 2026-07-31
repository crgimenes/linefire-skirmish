// Command linefire-skirmish runs Linefire's attract demo over the desktop: an
// indestructible autopilot ship fighting an endless horde in a procedurally
// generated arena that regenerates every twenty seconds or so, drawn on a
// transparent, undecorated, click-through window covering the monitor. There is
// nothing to play — the mouse and keyboard pass straight through to whatever the
// user is actually doing — it is Linefire playing itself, as entertainment.
//
// The game is Linefire's, whole: this binary is only the window. All the code,
// art and data come from the linefire module (the assets are embedded in it), so
// what appears over the desktop is pixel for pixel the game.
//
// Usage:
//
//	linefire-skirmish            the battle, silent, over the desktop
//	linefire-skirmish -sound     same, with the arcade attract-mode audio
//	linefire-skirmish -debug     same, with the debug HUD up
//	linefire-skirmish -window    in an ordinary window, not over the desktop
//
// The game reads no keys — every keystroke belongs to the desktop. The one
// clickable thing is a small floating panel (NeoFrame-style), draggable by its
// title bar, holding the controls the program needs because it has no real
// window: Quit today, more later. Everywhere off the panel the mouse passes
// straight through. Ctrl+C in the launching terminal also quits.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/crgimenes/linefire"
	"github.com/crgimenes/linefire/game"
	"github.com/hajimehoshi/ebiten/v2"
)

// runWindowed shows the same battle in a plain window: decorated and opaque, its
// frame standing in for the arena border the overlay leaves to the screen edge.
// The game still reads no keys, and the panel works like anywhere else — minus
// the passthrough dance, since an ordinary window owns its mouse. It is a
// diagnostic, and it is also the easiest way to look at the game while working
// on it.
func runWindowed(g *game.Game) {
	ebiten.SetWindowSize(1280, 720)
	ebiten.SetWindowTitle("Linefire Skirmish (windowed)")
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
	if err := ebiten.RunGame(newShell(g, false)); err != nil {
		log.Fatalf("linefire-skirmish: %v", err)
	}
}

// version is stamped at build time (-X main.version=...).
var version = "dev"

func main() {
	log.SetFlags(0)
	showVersion := flag.Bool("version", false, "print the version and exit")
	sound := flag.Bool("sound", false, "play the attract-mode audio (silent by default)")
	debug := flag.Bool("debug", false, "start with the debug HUD up")
	window := flag.Bool("window", false, "run in an ordinary window instead of over the desktop")
	flag.Parse()

	if *showVersion {
		fmt.Println("linefire-skirmish", version)
		return
	}

	g, err := game.NewSkirmish(linefire.Content(), "gameassets", game.SkirmishOptions{
		Sound: *sound,
		Debug: *debug,
	})
	if err != nil {
		log.Fatalf("linefire-skirmish: %v", err)
	}
	if *debug {
		w, h := ebiten.Monitor().Size()
		log.Printf("monitor %dx%d, device scale %.2f", w, h, ebiten.Monitor().DeviceScaleFactor())
	}

	if *window {
		// An ordinary opaque window, for when the question is whether the GAME is
		// working or whether the OVERLAY is: if the battle shows up here and not over
		// the desktop, the window flags are the suspect and not the drawing.
		runWindowed(g)
		return
	}

	// The panel's Quit is the off switch; Ctrl+C in the launching terminal
	// still works for the terminal-minded.
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-interrupt
		os.Exit(0)
	}()

	// A transparent, undecorated, always-on-top window covering the monitor:
	// the NeoFrame recipe. The desktop underneath stays fully usable — the
	// overlay takes the mouse only while the cursor is on the floating panel
	// (the shell flips the passthrough flag per frame).
	const name = "Linefire Skirmish"
	width, height := ebiten.Monitor().Size()
	ebiten.SetWindowDecorated(false)
	ebiten.SetWindowFloating(true)
	ebiten.SetWindowMousePassthrough(true)
	ebiten.SetWindowPosition(0, 0)
	ebiten.SetWindowSize(width, height)
	ebiten.SetWindowTitle(name)
	ebiten.SetRunnableOnUnfocused(true)

	err = ebiten.RunGameWithOptions(newShell(g, true), &ebiten.RunGameOptions{
		ScreenTransparent: true,
		SkipTaskbar:       true,
		X11ClassName:      name,
		X11InstanceName:   name,
	})
	if err != nil {
		log.Fatalf("linefire-skirmish: %v", err)
	}
}
