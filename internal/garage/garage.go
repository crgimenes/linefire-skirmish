package garage

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"
)

// The garage is the player's workshop: a real window, in its own process,
// where battles are configured, fleets are assigned their programs and the
// scoreboard is watched. It never simulates anything — it launches the game
// with -control and speaks the protocol (linefire's game/control.go): battle
// events come in as JSONL on the child's stdout, match commands go out on its
// stdin. Everything the garage shows is derived from that stream, which is the
// same stream the headless runner records and the trace checker audits: one
// contract, three consumers.

// Setup is what the player configured for the next battle.
type Setup struct {
	Mode     string   `json:"mode"`
	Map      string   `json:"map"`
	Factions int      `json:"factions"`
	Ships    int      `json:"ships"`
	Duration int      `json:"duration"`
	Programs []string `json:"programs"` // one .filo path per faction, "" = house brain
	Window   bool     `json:"window"`   // an ordinary window instead of the desktop overlay
}

// FleetBoard is one faction's live standing, as the garage understands it
// from the event stream.
type FleetBoard struct {
	Faction int    `json:"faction"`
	Program string `json:"program"` // the file flying it, or "house brain"
	Alive   int    `json:"alive"`
	Kills   int    `json:"kills"`
	Losses  int    `json:"losses"`
	Shots   int    `json:"shots"`
	Salvage int    `json:"salvage"`
}

// Status is the whole garage state the UI renders each poll.
type Status struct {
	Running  bool         `json:"running"`
	Mode     string       `json:"mode"`
	MapName  string       `json:"mapName"`
	Seed     int64        `json:"seed"`
	Battles  int          `json:"battles"`
	Fleets   []FleetBoard `json:"fleets"`
	Outcome  string       `json:"outcome"`
	LastErr  string       `json:"lastErr"`
	GamePath string       `json:"gamePath"`
}

// Garage runs the game process and keeps the board. Every exported method is
// bound into JavaScript, so this type IS the UI's API.
type Garage struct {
	gamePath string
	// reclaim is called once a battle this garage launched is really up, so the
	// window can take the focus back that the game just took. Nil in tests.
	reclaim func()

	mu      sync.Mutex
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	setup   Setup
	board   map[int]*FleetBoard
	ships   map[int]int // ship id -> faction, so a kill can be credited to a fleet
	mode    string
	mapName string
	seed    int64
	battles int
	outcome string
	lastErr string
}

func newGarage(gamePath string) *Garage {
	return &Garage{gamePath: gamePath, board: map[int]*FleetBoard{}, ships: map[int]int{}}
}

// Deploy launches a battle, or redeploys the flying one.
//
// Some of the setup is the match's and can be changed mid-flight through the
// control plane — mode, fleet size, deadline — and some belongs to the game
// PROCESS: which programs fly which factions, how many factions there are, the
// battlefield, the window. Changing those means relaunching, and pretending
// otherwise would relabel the board while the old programs kept flying.
func (g *Garage) Deploy(s Setup) error {
	g.mu.Lock()
	running := g.cmd != nil
	old := g.setup
	g.setup = s
	g.mu.Unlock()

	if running && !needsRelaunch(old, s) {
		return g.command(map[string]any{
			"op": "match", "mode": s.Mode, "ships": s.Ships, "duration": s.Duration,
		})
	}
	if running {
		if err := g.Cease(); err != nil {
			return err
		}
		g.awaitStopped()
	}
	return g.launch(s)
}

// needsRelaunch reports whether the change touches the game process's launch
// flags rather than the running match.
func needsRelaunch(old, next Setup) bool {
	if old.Factions != next.Factions || old.Map != next.Map || old.Window != next.Window {
		return true
	}
	return joinPrograms(old.Programs) != joinPrograms(next.Programs)
}

// awaitStopped waits briefly for the killed process to be reaped, so the new
// battle never starts while the old one still owns the screen.
func (g *Garage) awaitStopped() {
	for range 50 {
		g.mu.Lock()
		gone := g.cmd == nil
		g.mu.Unlock()
		if gone {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// launch starts the game process with the setup's flags and begins reading
// its event stream.
func (g *Garage) launch(s Setup) error {
	args := []string{
		"-control",
		"-mode", s.Mode,
		"-map", s.Map,
		"-factions", strconv.Itoa(s.Factions),
	}
	if s.Ships > 0 {
		args = append(args, "-ships", strconv.Itoa(s.Ships))
	}
	if s.Duration > 0 {
		args = append(args, "-duration", strconv.Itoa(s.Duration))
	}
	if ai := joinPrograms(s.Programs); ai != "" {
		args = append(args, "-ai", ai)
	}
	if s.Window {
		args = append(args, "-window")
	}

	cmd := exec.Command(g.gamePath, args...) // #nosec G204 -- the game binary the garage ships with
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("launching %s: %w", g.gamePath, err)
	}

	g.mu.Lock()
	g.cmd, g.stdin = cmd, stdin
	g.battles, g.outcome, g.lastErr = 0, "", ""
	g.board, g.ships = map[int]*FleetBoard{}, map[int]int{}
	g.mu.Unlock()

	go g.readEvents(stdout)
	go g.reap(cmd)
	return nil
}

// Cease ends the battle: the game process is stopped and the board freezes on
// the last standing it reported.
func (g *Garage) Cease() error {
	g.mu.Lock()
	cmd := g.cmd
	g.mu.Unlock()
	if cmd == nil {
		return nil
	}
	return cmd.Process.Kill()
}

// Restage fights the same battle again on a fresh field.
func (g *Garage) Restage() error {
	return g.command(map[string]any{"op": "restart"})
}

// command posts one control line to the running game.
func (g *Garage) command(v any) error {
	g.mu.Lock()
	w := g.stdin
	g.mu.Unlock()
	if w == nil {
		return fmt.Errorf("no battle is flying")
	}
	line, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = w.Write(append(line, '\n'))
	return err
}

// Status is the poll the UI renders from.
func (g *Garage) Status() Status {
	g.mu.Lock()
	defer g.mu.Unlock()
	st := Status{
		Running: g.cmd != nil, Mode: g.mode, MapName: g.mapName, Seed: g.seed,
		Battles: g.battles, Outcome: g.outcome, LastErr: g.lastErr, GamePath: g.gamePath,
	}
	for f := 1; f <= g.setup.Factions; f++ {
		b := g.board[f]
		if b == nil {
			b = &FleetBoard{Faction: f}
		}
		row := *b
		row.Faction = f
		row.Program = "house brain"
		if f-1 < len(g.setup.Programs) && g.setup.Programs[f-1] != "" {
			row.Program = baseName(g.setup.Programs[f-1])
		}
		st.Fleets = append(st.Fleets, row)
	}
	return st
}

// event is the garage's view of one battle-event line. The stream carries
// more than this; a consumer reads what it needs and ignores the rest, which
// is what keeps the protocol able to grow.
type event struct {
	Ev      string `json:"ev"`
	ID      int    `json:"id"` // the SHIP's id, not its faction
	Faction int    `json:"f"`
	By      int    `json:"by"` // the shooter's SHIP id: whose it is comes from the spawn roll
	Mode    string `json:"mode"`
	MapName string `json:"map"`
	Seed    int64  `json:"seed"`
	Winner  int    `json:"winner"`
	Ticks   int    `json:"ticks"`
}

// readEvents folds the battle stream into the board until the game exits.
func (g *Garage) readEvents(r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		var ev event
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			continue // an unknown line is not the garage's business
		}
		g.apply(ev)
	}
}

// apply folds one event into the board.
func (g *Garage) apply(ev event) {
	g.mu.Lock()
	defer g.mu.Unlock()
	fleet := func(f int) *FleetBoard {
		if f == 0 {
			return nil
		}
		b := g.board[f]
		if b == nil {
			b = &FleetBoard{Faction: f}
			g.board[f] = b
		}
		return b
	}

	switch ev.Ev {
	case "battle":
		g.mode, g.mapName, g.seed = ev.Mode, ev.MapName, ev.Seed
		g.battles++
		g.outcome = ""
		g.board = map[int]*FleetBoard{} // a new battle, a clean board
		g.ships = map[int]int{}
		// The battle is really up, which means the game's window has activated
		// itself and taken our focus — it HAS to activate, or macOS throttles it
		// to single digit FPS. Take the focus back now, on the FIRST battle of
		// this launch only: without it the next click on the garage is spent
		// activating the window instead of pressing the button under the cursor,
		// and the panel reads as dead. Restaging must not yank focus back from
		// whatever the user has moved on to.
		if g.battles == 1 && g.reclaim != nil {
			go g.reclaim() // off the event reader: the UI thread is somebody else's
		}
	case "spawn":
		g.ships[ev.ID] = ev.Faction // the roll: a kill names its shooter by SHIP
		if b := fleet(ev.Faction); b != nil {
			b.Alive++
		}
	case "salvage":
		if b := fleet(ev.Faction); b != nil {
			b.Salvage++
		}
	case "shot":
		if b := fleet(ev.Faction); b != nil {
			b.Shots++
		}
	case "death":
		if b := fleet(ev.Faction); b != nil {
			b.Alive--
			b.Losses++
		}
		// The kill goes to the shooter's FLEET, and the shooter is named by
		// ship id — it may even be dead by now, with its last bolt still in
		// the air, which is why the roll is never pruned.
		if b := fleet(g.ships[ev.By]); b != nil {
			b.Kills++
		}
	case "result":
		g.outcome = fmt.Sprintf("faction %d prevails after %.1fs", ev.Winner, float64(ev.Ticks)/60)
		if ev.Winner == 0 {
			g.outcome = fmt.Sprintf("the battle ends in a draw after %.1fs", float64(ev.Ticks)/60)
		}
	}
}

// reap waits for the game to exit and clears the running state, so the UI
// shows the truth when a battle is closed from its own window.
func (g *Garage) reap(cmd *exec.Cmd) {
	err := cmd.Wait()
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.cmd == cmd {
		g.cmd, g.stdin = nil, nil
		if err != nil {
			g.lastErr = err.Error()
		}
	}
}

// joinPrograms builds the -ai flag: comma-separated paths in faction order,
// empty when no faction has a program.
func joinPrograms(progs []string) string {
	out, any := "", false
	for i, p := range progs {
		if i > 0 {
			out += ","
		}
		out += p
		if p != "" {
			any = true
		}
	}
	if !any {
		return ""
	}
	return out
}

// baseName is the file name without its directory, for the board.
func baseName(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' || p[i] == '\\' {
			return p[i+1:]
		}
	}
	return p
}
