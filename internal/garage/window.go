// Package garage is the player's workshop: a real window where battles are
// configured, fleets are handed their programs and the scoreboard is watched —
// for everyone who will never open a terminal.
//
// It is a LAUNCHER, not a second game. The battle runs in its own process (the
// same executable, re-launched with -control), because the game and this window
// each need their own main thread and run loop; the two talk over the control
// protocol documented in linefire's game/control.go — battle events in, match
// commands out. Everything on screen here is folded from that stream, the same
// one the headless runner records and the trace checker audits.
package garage

import (
	_ "embed"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/crgimenes/glaze"
)

//go:embed ui.html
var uiHTML []byte

// The smallest window the garage is still usable in — a phone-sized floor.
const (
	garageMinWidth  = 380
	garageMinHeight = 560
)

// Options configures the garage window.
type Options struct {
	// Game is the executable battles are launched from. Empty means THIS one:
	// the garage and the game are the same binary, so it re-launches itself
	// with -control. That is the whole point of shipping one file — there is no
	// sibling binary to find, no PATH to search, nothing to guess. A path here
	// is an override for development and tests.
	Game string
	// Debug opens the webview's developer tools.
	Debug bool
}

// gameBinary is the executable battles are launched from: the override, else
// this very program.
func gameBinary(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("garage: locating this executable: %w", err)
	}
	return exe, nil
}

// serveUI starts the loopback server that hosts the page and returns its URL.
// The page is one self-contained file: no CDN, no framework, nothing to fetch
// from a network the game does not need.
func serveUI() (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(uiHTML)
	})
	go func() {
		srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
		_ = srv.Serve(ln)
	}()
	return fmt.Sprintf("http://%s/", ln.Addr().String()), nil
}

// Run opens the garage and returns when the user closes it. Call it from the
// program's main goroutine: the webview owns the UI thread.
func Run(opts Options) error {
	bin, err := gameBinary(opts.Game)
	if err != nil {
		return err
	}
	g := newGarage(bin)

	url, err := serveUI()
	if err != nil {
		return err
	}

	// The garage LOSES focus every time it deploys: launching the game activates
	// the game's window (which it must — an unactivated window is throttled to
	// single-digit FPS on macOS). Without this the next click on the garage is
	// spent bringing it forward and never reaches the button, so ordering two
	// things in a row needs a double click and the panel feels dead. Nothing
	// here destroys anything, which is exactly when this is safe to turn on.
	w, err := glaze.NewWithOptions(glaze.Options{Debug: opts.Debug, AcceptsFirstMouse: true})
	if err != nil {
		return err
	}
	defer w.Destroy()

	w.SetTitle("Linefire Skirmish — Garage")
	w.SetSize(900, 680, glaze.HintNone)
	// A floor roughly the size of a phone screen: the layout collapses to one
	// column well before this, but dragged any smaller the window stops being a
	// workshop and becomes a sliver.
	w.SetSize(garageMinWidth, garageMinHeight, glaze.HintMin)

	// The garage's own methods are the UI's API: deploy, cease, restage, poll.
	_, err = glaze.BindMethods(w, "garage", g)
	if err != nil {
		return err
	}

	// The file dialogs need the window itself, so they are bound here rather
	// than living on the service. They block their goroutine, which is why the
	// binding runs them off the UI thread.
	err = w.Bind("pickProgram", func() (string, error) {
		return w.OpenFile(glaze.FileDialogOptions{
			Title:   "Choose a Filo program for this fleet",
			Filters: []glaze.FileFilter{{Name: "Filo programs", Extensions: []string{"filo"}}},
		})
	})
	if err != nil {
		return err
	}

	w.Navigate(url)
	w.Run()

	// Closing the garage closes the battle it launched: a launcher owns what it
	// started.
	err = g.Cease()
	if err != nil {
		log.Printf("garage: stopping the battle on the way out: %v", err)
	}
	return nil
}
