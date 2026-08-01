// Command linefire-skirmish runs Linefire's faction battle over the desktop:
// two or more teams of autonomous ships — each in its own color, materialising
// through its own vortex — hunting each other in a procedurally generated arena
// that regenerates every twenty seconds or so, drawn on a transparent,
// undecorated, click-through window covering the monitor. There is nothing to
// pilot — the mouse and keyboard pass straight through to whatever the user is
// actually doing — it is Linefire playing itself, as entertainment.
//
// The game is Linefire's, whole: this binary is only the window. All the code,
// art and data come from the linefire module (the assets are embedded in it), so
// what appears over the desktop is pixel for pixel the game.
//
// Usage:
//
//	linefire-skirmish              the battle, silent, over the desktop
//	linefire-skirmish -factions 3  three teams instead of two (up to 8)
//	linefire-skirmish -map maze    fight in a cave labyrinth instead of the open
//	linefire-skirmish -mode lastfleet   a real match: fleets land, no
//	                               reinforcements, last faction standing
//	                               prevails, rematch on a fresh field
//	linefire-skirmish -mode timed -duration 120   most kills when time runs out
//	linefire-skirmish -ships 6     fleet size per faction in a real match
//	linefire-skirmish -ai a.filo   faction 1 flies your Filo program (see ai/)
//	linefire-skirmish -ipc "python3 ai/bot.py"   faction 1 is flown by an
//	                               EXTERNAL process over stdio JSONL (any
//	                               language, any agent — see game/ipc.go)
//	linefire-skirmish -sim 100     fight 100 headless battles, print the score
//	linefire-skirmish -sim 100 -trace t.jsonl   ...recording every battle as
//	                               JSONL and running the invariant checker on it
//	linefire-skirmish -check t.jsonl   re-run the checker over a saved trace
//	linefire-skirmish -sound       same, with the arcade attract-mode audio
//	linefire-skirmish -debug       same, with the debug HUD up
//	linefire-skirmish -window      in an ordinary window, not over the desktop
//
// -ai takes comma-separated programs assigned to factions in order; an empty
// slot keeps that faction on the built-in brain. "-ai a.filo,b.filo" pits two
// programs against each other; "-ai ,b.filo" gives faction 2 the program and
// faction 1 the house AI. Writing these programs IS how skirmish is played —
// the contract they fly under is documented in linefire's game/pilot.go.
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
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strings"
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

// runSim fights n battles without a window at full CPU speed and prints the
// scoreboard: the fast loop for developing an AI — change the program, run a
// hundred fights, read numbers instead of eyeballing one battle. Each battle
// gets its own seed and replays deterministically from it. With tracePath the
// battles are also recorded as JSONL (all of them into one file) and the
// invariant checker runs over the record before the scoreboard prints.
func runSim(n int, programs, labels []string, factions int, mapMode, tracePath string) {
	var trace io.Writer
	var traceFile *os.File
	if tracePath != "" {
		f, err := os.Create(tracePath) // #nosec G304 -- the user names their own trace file
		if err != nil {
			log.Fatalf("linefire-skirmish: %v", err)
		}
		trace = f
		traceFile = f
	}

	wins := map[int]int{}
	totalTicks := 0
	for i := range n {
		res, err := game.RunBattle(linefire.Content(), "gameassets", game.BattleOptions{
			Programs: programs,
			Factions: factions,
			Map:      mapMode,
			Seed:     int64(i + 1),
			Trace:    trace,
			Battle:   i + 1,
		})
		if err != nil {
			log.Fatalf("linefire-skirmish: %v", err)
		}
		wins[res.Winner]++
		totalTicks += res.Ticks
	}

	teams := max(factions, 2)
	for f := 1; f <= teams; f++ {
		label := "house brain"
		if f-1 < len(labels) && labels[f-1] != "" {
			label = labels[f-1]
		}
		fmt.Printf("faction %d (%s): %d wins\n", f, label, wins[f])
	}
	fmt.Printf("draws: %d\n", wins[0])
	fmt.Printf("battles: %d, avg %d ticks (%.1fs of game time each)\n",
		n, totalTicks/n, float64(totalTicks)/float64(n)/60)

	if traceFile != nil {
		// The record must be safely on disk before anything audits it.
		if err := traceFile.Close(); err != nil {
			log.Fatalf("linefire-skirmish: %v", err)
		}
		fmt.Printf("trace: %s\n", tracePath)
		checkTraceFile(tracePath)
	}
}

// checkTraceFile runs the invariant checker over a saved trace and prints its
// findings — the deterministic first pass of analysis; an AI (or a person)
// reads the findings, not the megabytes.
func checkTraceFile(path string) {
	f, err := os.Open(path) // #nosec G304 -- the user names their own trace file
	if err != nil {
		log.Fatalf("linefire-skirmish: %v", err)
	}
	defer func() { _ = f.Close() }() // read-only: nothing to lose on close
	finds, err := game.CheckTrace(f)
	if err != nil {
		log.Fatalf("linefire-skirmish: check: %v", err)
	}
	if len(finds) == 0 {
		fmt.Println("check: no findings")
		return
	}
	fmt.Printf("check: %d findings\n", len(finds))
	for _, s := range finds {
		fmt.Println("  " + s)
	}
}

// startDrivers turns the -ipc flag into one running child process per faction:
// commands separated by commas, assigned in faction order, an empty slot
// keeping that faction on Filo or the built-in brain. Each child speaks the
// JSONL protocol of linefire's game/ipc.go over its own stdin/stdout; stderr
// passes through, so a bot's own logs land in this terminal. When skirmish
// exits, the children's stdin closes — a well-behaved bot exits with it.
func startDrivers(ipc string) ([]*game.IPCDriver, error) {
	if ipc == "" {
		return nil, nil
	}
	cmds := strings.Split(ipc, ",")
	drivers := make([]*game.IPCDriver, len(cmds))
	for i, c := range cmds {
		fields := strings.Fields(c)
		if len(fields) == 0 {
			continue
		}
		cmd := exec.Command(fields[0], fields[1:]...) // #nosec G204 -- the user names their own bot command
		cmd.Stderr = os.Stderr
		stdin, err := cmd.StdinPipe()
		if err != nil {
			return nil, err
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return nil, err
		}
		if err := cmd.Start(); err != nil {
			return nil, fmt.Errorf("driver %q: %w", c, err)
		}
		drivers[i] = game.NewIPCDriver(stdout, stdin)
	}
	return drivers, nil
}

// readPrograms turns the -ai flag into one Filo source per faction: paths
// separated by commas, assigned in faction order, an empty slot keeping that
// faction on the built-in brain. Reading files is the shell's job — the game
// takes source text and fails at launch on a broken program.
func readPrograms(ai string) (programs, labels []string, err error) {
	if ai == "" {
		return nil, nil, nil
	}
	paths := strings.Split(ai, ",")
	programs = make([]string, len(paths))
	labels = make([]string, len(paths))
	for i, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		src, err := os.ReadFile(p) // #nosec G304 -- the user names their own program file
		if err != nil {
			return nil, nil, err
		}
		programs[i] = string(src)
		labels[i] = p
	}
	return programs, labels, nil
}

// version is stamped at build time (-X main.version=...).
var version = "dev"

func main() {
	log.SetFlags(0)
	showVersion := flag.Bool("version", false, "print the version and exit")
	sound := flag.Bool("sound", false, "play the attract-mode audio (silent by default)")
	debug := flag.Bool("debug", false, "start with the debug HUD up")
	window := flag.Bool("window", false, "run in an ordinary window instead of over the desktop")
	factions := flag.Int("factions", 2, "teams fighting in the arena (2-8)")
	mapMode := flag.String("map", game.SkirmishMapArena, "battlefield: arena (open field) or maze (cave labyrinth)")
	mode := flag.String("mode", game.MatchEndless, "match mode: endless (the aquarium), lastfleet (annihilation) or timed")
	ships := flag.Int("ships", 0, "fleet size per faction in a real match (default 8)")
	duration := flag.Int("duration", 0, "timed mode deadline in seconds (default 120)")
	ai := flag.String("ai", "", "comma-separated .filo programs, one per faction in order (empty slot = built-in brain)")
	ipc := flag.String("ipc", "", "comma-separated commands, one per faction in order; each child pilots its faction over stdio JSONL")
	sim := flag.Int("sim", 0, "fight N headless battles at full speed, print the scoreboard and exit (no window)")
	trace := flag.String("trace", "", "with -sim: record every battle as JSONL to this file and run the invariant checker")
	check := flag.String("check", "", "run the invariant checker over a saved trace file and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("linefire-skirmish", version)
		return
	}

	programs, labels, err := readPrograms(*ai)
	if err != nil {
		log.Fatalf("linefire-skirmish: %v", err)
	}

	if *check != "" {
		checkTraceFile(*check)
		return
	}
	if *sim > 0 {
		runSim(*sim, programs, labels, *factions, *mapMode, *trace)
		return
	}

	drivers, err := startDrivers(*ipc)
	if err != nil {
		log.Fatalf("linefire-skirmish: %v", err)
	}

	g, err := game.NewSkirmish(linefire.Content(), "gameassets", game.SkirmishOptions{
		Sound:    *sound,
		Debug:    *debug,
		Factions: *factions,
		Map:      *mapMode,
		Mode:     *mode,
		Ships:    *ships,
		Duration: *duration,
		Programs: programs,
		IPC:      drivers,
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
