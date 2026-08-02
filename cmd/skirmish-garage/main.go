// Command skirmish-garage is the player's workshop: a real window where
// battles are configured, fleets are handed their programs and the scoreboard
// is watched — for everyone who will never open a terminal.
//
// It is a LAUNCHER, not a second game. The battle runs in its own process
// (linefire-skirmish -control), because the game and this window each need
// their own main thread and run loop; the two talk over the control protocol
// documented in linefire's game/control.go — battle events in, match commands
// out. Everything on screen here is folded from that stream, the same one the
// headless runner records and the trace checker audits.
//
// Usage:
//
//	skirmish-garage                      the game binary found next to this one
//	skirmish-garage -game ./linefire-skirmish   an explicit game binary
//	skirmish-garage -debug               with the webview's developer tools
package main

import (
	_ "embed"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
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

// gameBinary is the game the garage launches: the -game flag, else a binary
// named linefire-skirmish sitting next to this one (how a release ships), else
// whatever the PATH offers (how a development checkout runs).
func gameBinary(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	if exe, err := os.Executable(); err == nil {
		side := filepath.Join(filepath.Dir(exe), "linefire-skirmish")
		if _, err := os.Stat(side); err == nil {
			return side
		}
	}
	if p, err := exec.LookPath("linefire-skirmish"); err == nil {
		return p
	}
	return "linefire-skirmish"
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

func main() {
	log.SetFlags(0)
	gameFlag := flag.String("game", "", "path to the linefire-skirmish binary")
	debug := flag.Bool("debug", false, "open the webview's developer tools")
	flag.Parse()

	g := newGarage(gameBinary(*gameFlag))

	url, err := serveUI()
	if err != nil {
		log.Fatalf("skirmish-garage: %v", err)
	}

	w, err := glaze.New(*debug)
	if err != nil {
		log.Fatalf("skirmish-garage: %v", err)
	}
	defer w.Destroy()

	w.SetTitle("Linefire Skirmish — Garage")
	w.SetSize(900, 680, glaze.HintNone)
	// A floor roughly the size of a phone screen: the layout collapses to one
	// column well before this, but dragged any smaller the window stops being
	// a workshop and becomes a sliver.
	w.SetSize(garageMinWidth, garageMinHeight, glaze.HintMin)

	// The garage's own methods are the UI's API: deploy, cease, restage, poll.
	bound, err := glaze.BindMethods(w, "garage", g)
	if err != nil {
		log.Fatalf("skirmish-garage: %v", err)
	}
	_ = bound

	// The file dialogs need the window itself, so they are bound here rather
	// than living on the service. They block their goroutine, which is why the
	// binding runs them off the UI thread.
	must(w.Bind("pickProgram", func() (string, error) {
		return w.OpenFile(glaze.FileDialogOptions{
			Title:   "Choose a Filo program for this fleet",
			Filters: []glaze.FileFilter{{Name: "Filo programs", Extensions: []string{"filo"}}},
		})
	}))

	w.Navigate(url)
	w.Run()

	// Closing the garage closes the battle it launched: a launcher owns what
	// it started.
	_ = g.Cease()
}

func must(err error) {
	if err != nil {
		log.Fatalf("skirmish-garage: %v", err)
	}
}
