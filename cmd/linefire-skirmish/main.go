// Command linefire-skirmish is the whole product in ONE executable: run it with
// no arguments and it opens the GARAGE — the window where battles are
// configured and watched — and the garage launches battles by re-running this
// same file with -control. There is no second binary to ship, to find beside
// this one, or to look up on the PATH; installing is copying one file, which is
// what every store and every channel wants.
//
// -play (and any of the flags below) skips the garage and goes straight to the
// battle. That is the front door for a terminal; the garage is the front door
// for everyone else.
//
// The battle itself is Linefire's faction fight over the desktop:
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
//	linefire-skirmish              the GARAGE: configure and watch battles
//	linefire-skirmish -play        the battle, silent, over the desktop
//	linefire-skirmish -factions 3  three teams instead of two (up to 8)
//	linefire-skirmish -map maze    fight in a cave labyrinth instead of the open
//	linefire-skirmish -mode lastfleet   a real match: fleets land, no
//	                               reinforcements, and the last faction standing
//	                               prevails — the verdict then HOLDS on screen
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
//	linefire-skirmish -control     the garage protocol: battle events out on
//	                               stdout, match commands in on stdin (JSONL);
//	                               e.g. {"op":"match","mode":"timed","ships":6},
//	                               {"op":"restart"}, {"op":"quit"}
//	linefire-skirmish -sound       same, with the battle's sound effects (there
//	                               is no soundtrack here: the songs are the
//	                               campaign's)
//	linefire-skirmish -debug       same, with the debug HUD up
//	linefire-skirmish -window      in an ordinary window, not over the desktop
//
// -ai takes comma-separated programs assigned to factions in order; an empty
// slot keeps that faction on the built-in brain. "-ai a.filo,b.filo" pits two
// programs against each other; "-ai ,b.filo" gives faction 2 the program and
// faction 1 the house AI. Writing these programs IS how skirmish is played —
// the contract they fly under is documented in linefire's game/pilot.go.
//
// The battle window is inert: it reads no keys and takes no clicks — every
// keystroke and every click belongs to whatever is underneath. Starting and
// stopping battles is the garage's job. Ctrl+C in the launching terminal quits
// a battle started from one.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"

	"github.com/crgimenes/glaze"
	"github.com/crgimenes/linefire"
	"github.com/crgimenes/linefire-skirmish/internal/garage"
	"github.com/crgimenes/linefire-skirmish/internal/icon"
	"github.com/crgimenes/linefire/game"
	"github.com/hajimehoshi/ebiten/v2"
)

// The smallest window a battle is still worth watching in — a phone-sized
// floor, matching the garage's.
const (
	battleMinWidth  = 480
	battleMinHeight = 360
)

// runWindowed shows the same battle in a plain window: decorated and opaque, its
// frame standing in for the arena border the overlay leaves to the screen edge.
// It is a diagnostic, and it is also the easiest way to look at the game while
// working on it.
func runWindowed(g *game.Game) {
	ebiten.SetWindowSize(1280, 720)
	ebiten.SetWindowTitle("Linefire Skirmish (windowed)")
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
	// A phone-sized floor: the arena is fitted to the view, so a window dragged
	// smaller than this leaves a battlefield with no room to manoeuvre in.
	ebiten.SetWindowSizeLimits(battleMinWidth, battleMinHeight, -1, -1)
	if err := ebiten.RunGame(g); err != nil {
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

	teams := factions
	if teams <= 0 {
		teams = 2
	}
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

// readControl feeds stdin's JSONL commands to the running game: the garage
// (or any parent process, or a person with a terminal) reconfigures, restages
// and ENDS battles without touching this process from the outside — quitting
// is {"op":"quit"}, which the game answers by leaving its own loop. Closing
// stdin only ends the feed; a battle nobody asked to end keeps flying.
func readControl(g *game.Game) {
	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var cmd game.MatchCommand
		if err := json.Unmarshal(line, &cmd); err != nil {
			log.Printf("control: bad line: %v", err)
			continue
		}
		g.PostCommand(cmd)
	}
}

// startDrivers turns the -ipc flag into one running child process per faction:
// commands separated by commas, assigned in faction order, an empty slot
// keeping that faction on Filo or the built-in brain. Each child speaks the
// JSONL protocol of linefire's game/ipc.go over its own stdin/stdout; stderr
// passes through, so a bot's own logs land in this terminal. When skirmish
// exits, the children's stdin closes — a well-behaved bot exits with it.
//
// The command runs through the SHELL, which is what a typed command means to
// the person typing it: quoted arguments hold together and a path with a space
// in it stays one path. Splitting on whitespace instead turned
// "python3 ~/my bot/agent.py" into two arguments and a mystery, and the garage
// asks for these commands in a text field.
func startDrivers(ipc string) ([]*game.IPCDriver, error) {
	if ipc == "" {
		return nil, nil
	}
	cmds := strings.Split(ipc, ",")
	drivers := make([]*game.IPCDriver, len(cmds))
	for i, c := range cmds {
		if strings.TrimSpace(c) == "" {
			continue
		}
		cmd := shellCommand(c)
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

// wearTheIcon gives this process its face: the ship, drawn from the game's own
// vector art (internal/icon).
//
// Both front doors are served from here because both are this one executable,
// and the two platforms split the work: Windows and Linux draw a WINDOW icon,
// which Ebitengine sets when it creates one, while macOS draws an APPLICATION
// icon in the Dock, which is process-wide and belongs to AppKit. Neither
// covers the icon on the executable FILE — that is a resource compiled into
// it, and it is the one piece still missing.
//
// A failure here costs the icon and nothing else. A game that refuses to start
// because it could not decorate itself would be a poor trade.
func wearTheIcon() {
	// The sizes a window manager picks from; more than one so it never has to
	// scale a big bitmap down to a title bar.
	ebiten.SetWindowIcon(icon.Images(16, 32, 48, 64, 128, 256))

	png, err := icon.PNG(512)
	if err != nil {
		log.Printf("linefire-skirmish: no icon (%v)", err)
		return
	}
	err = glaze.SetAppIcon(png)
	if err != nil && !errors.Is(err, glaze.ErrIconUnsupported) {
		log.Printf("linefire-skirmish: the Dock icon did not take (%v)", err)
	}
}

// shellCommand builds the child process for one driver command, run by the
// platform's own shell so the user's quoting is the shell's business and not
// ours to half-implement.
func shellCommand(line string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.Command("cmd", "/c", line) // #nosec G204 -- the user names their own bot command
	}
	return exec.Command("/bin/sh", "-c", line) // #nosec G204 -- the user names their own bot command
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
	factions := flag.Int("factions", 2, "teams fighting in the arena, 1 (solo practice) to 8")
	mapMode := flag.String("map", game.SkirmishMapArena, "battlefield: arena (open field) or maze (cave labyrinth)")
	mode := flag.String("mode", game.MatchEndless, "match mode: endless (the aquarium), lastfleet (annihilation) or timed")
	ships := flag.Int("ships", 0, "fleet size per faction in a real match (default 8)")
	duration := flag.Int("duration", 0, "timed mode deadline in seconds (default 120)")
	ai := flag.String("ai", "", "comma-separated .filo programs, one per faction in order (empty slot = built-in brain)")
	ipc := flag.String("ipc", "", "comma-separated commands, one per faction in order; each child pilots its faction over stdio JSONL")
	sim := flag.Int("sim", 0, "fight N headless battles at full speed, print the scoreboard and exit (no window)")
	trace := flag.String("trace", "", "with -sim: record every battle as JSONL to this file and run the invariant checker")
	check := flag.String("check", "", "run the invariant checker over a saved trace file and exit")
	control := flag.Bool("control", false, "the garage protocol: battle events as JSONL on stdout, match commands as JSONL on stdin")
	_ = flag.Bool("play", false, "skip the garage and go straight to the battle")
	garageFlag := flag.Bool("garage", false, "open the garage (the default when there are no arguments)")
	gameBin := flag.String("game", "", "with the garage: the executable to launch battles with (default: this one)")
	flag.Parse()

	if *showVersion {
		fmt.Println("linefire-skirmish", version)
		return
	}

	wearTheIcon() // before either front door: the process has one face

	// No arguments at all is the front door: the garage. Any argument means the
	// caller knows what they want and gets the battle — which is why -play needs
	// no code of its own: being present is the whole of what it does. -garage
	// exists for the garage WITH options. The child the garage launches carries
	// -control, so it can never land here and open a second garage.
	if len(os.Args) == 1 || *garageFlag {
		err := garage.Run(garage.Options{Game: *gameBin, Debug: *debug})
		if err != nil {
			log.Fatalf("linefire-skirmish: %v", err)
		}
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

	opts := game.SkirmishOptions{
		Sound:    *sound,
		Debug:    *debug,
		Factions: *factions,
		Map:      *mapMode,
		Mode:     *mode,
		Ships:    *ships,
		Duration: *duration,
		Programs: programs,
		IPC:      drivers,
	}
	if *control {
		opts.Events = os.Stdout // stderr keeps the logs; stdout is the event stream
	}

	g, err := game.NewSkirmish(linefire.Content(), "gameassets", opts)
	if err != nil {
		log.Fatalf("linefire-skirmish: %v", err)
	}
	if *control {
		go readControl(g)
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
	// The overlay is click-through, always and everywhere: it has nothing to
	// click. It used to carry a floating Quit panel, which meant flipping the
	// passthrough flag per frame to keep that one rectangle clickable — all of
	// it gone now that the garage is the place a battle is started and stopped.
	const name = "Linefire Skirmish"
	width, height := ebiten.Monitor().Size()
	ebiten.SetWindowDecorated(false)
	ebiten.SetWindowFloating(true)
	ebiten.SetWindowMousePassthrough(true)
	ebiten.SetWindowPosition(0, 0)
	ebiten.SetWindowSize(width, height)
	ebiten.SetWindowTitle(name)
	ebiten.SetRunnableOnUnfocused(true)

	err = ebiten.RunGameWithOptions(g, &ebiten.RunGameOptions{
		ScreenTransparent: true,
		SkipTaskbar:       true,
		X11ClassName:      name,
		X11InstanceName:   name,
	})
	if err != nil {
		log.Fatalf("linefire-skirmish: %v", err)
	}
}
