package main

import (
	"image"
	"log"

	"github.com/crgimenes/minigui"
	"github.com/hajimehoshi/ebiten/v2"

	"github.com/crgimenes/linefire/game"
)

// shell wraps the game with the floating panel: the one clickable surface of a
// program that has no real window. The game reads no input at all, so the panel
// is how the user controls the PROGRAM (the ships answer to their own programs);
// it starts with Quit and will grow restart, statistics and submenus.
//
// The panel is minigui drawn inside the same Ebiten window, kept clickable on a
// click-through overlay by the NeoFrame recipe: dynamic mouse passthrough. A
// per-region hit area is simulated on a window-wide flag — each frame the
// overlay grabs the mouse only while the cursor is over the panel (or dragging
// it by the title bar), and stays click-through everywhere else.
type shell struct {
	game      *game.Game
	gui       minigui.Context
	win       minigui.Window
	panelRect image.Rectangle
	overlay   bool // dynamic passthrough is for the desktop overlay; a plain window owns its mouse
}

// newShell scales the panel to the display: the game renders at device
// resolution (its Layout multiplies by the device scale factor), so the gui
// coordinate space is device pixels and the default metrics would come out
// half-size on a Retina display.
func newShell(g *game.Game, overlay bool) *shell {
	s := &shell{game: g, overlay: overlay}
	dpr := ebiten.Monitor().DeviceScaleFactor()

	st := minigui.DefaultStyle()
	st.RowH *= dpr
	st.Pad *= dpr
	st.Gap *= dpr
	st.FieldW *= dpr
	s.gui.SetStyle(st)
	face, err := minigui.SystemFace(15 * dpr)
	if err != nil {
		log.Printf("panel font: %v (using the built-in debug font)", err)
	} else {
		s.gui.SetFace(face)
	}

	s.win = minigui.Window{Title: "Skirmish", X: 8 * dpr, Y: 8 * dpr, Open: true, NoClose: true}
	return s
}

func (s *shell) Update() error {
	if err := s.game.Update(); err != nil {
		return err
	}
	if s.buildPanel() {
		return ebiten.Termination
	}
	if s.overlay {
		x, y := ebiten.CursorPosition()
		capture := image.Pt(x, y).In(s.panelRect) || s.gui.Dragging()
		ebiten.SetWindowMousePassthrough(!capture)
	}
	return nil
}

// buildPanel lays out the floating panel for one frame, records its rectangle
// for the passthrough test, and reports whether Quit was clicked.
func (s *shell) buildPanel() bool {
	quit := false
	s.gui.Begin(minigui.InputFromEbiten(), 0, 0)
	if s.gui.BeginWindow(&s.win) {
		if s.gui.Button("quit", "Quit") {
			quit = true
		}
		s.panelRect = s.gui.EndWindow()
	}
	s.gui.End()
	return quit
}

func (s *shell) Draw(screen *ebiten.Image) {
	s.game.Draw(screen)
	s.gui.Render(screen)
}

func (s *shell) Layout(outsideWidth, outsideHeight int) (int, int) {
	return s.game.Layout(outsideWidth, outsideHeight)
}
