package garage

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// The whole chain, with the real game: the garage launches it, the battle
// events arrive over the control protocol, and the board fills. It needs a
// built game binary and a display, so it runs only when SKIRMISH_GAME points
// at one — absent, it skips rather than fails.
//
//	go build -o /tmp/linefire-skirmish ./cmd/linefire-skirmish
//	SKIRMISH_GAME=/tmp/linefire-skirmish go test ./internal/garage/ -run Live -v
//
// The garage normally launches THIS executable (one binary, two front doors),
// but a test binary is not the game — so the live test points it at a real
// build, which is what Options.Game exists for.
func TestLiveBattleFillsTheBoard(t *testing.T) {
	bin := os.Getenv("SKIRMISH_GAME")
	if bin == "" {
		t.Skip("SKIRMISH_GAME is not set: skipping the live battle")
	}

	g := newGarage(bin)
	setup := Setup{
		Mode: "lastfleet", Map: "arena", Factions: 2, Ships: 3,
		Programs: []string{"../../ai/hunter.filo", ""},
		Window:   true, // a real window: the overlay would cover the screen mid-test
	}
	if err := g.Deploy(setup); err != nil {
		t.Fatalf("deploy: %v", err)
	}
	defer func() { _ = g.Cease() }()

	// Wait for the battle to be announced and its fleets to land.
	deadline := time.Now().Add(20 * time.Second)
	var st Status
	for time.Now().Before(deadline) {
		st = g.Status()
		if st.Battles > 0 && st.Fleets[0].Alive > 0 && st.Fleets[1].Alive > 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if st.Battles == 0 {
		t.Fatalf("no battle was announced in 20s: %+v", st)
	}
	if st.Mode != "lastfleet" || st.MapName == "" || st.Seed == 0 {
		t.Fatalf("the battle header did not arrive: %+v", st)
	}
	if st.Fleets[0].Alive == 0 || st.Fleets[1].Alive == 0 {
		t.Fatalf("the fleets did not land: %+v", st.Fleets)
	}

	// Let them fight: shots must be recorded for both, and the piloted fleet
	// should be scoring (it beats the house brain overwhelmingly).
	deadline = time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		st = g.Status()
		if st.Fleets[0].Kills > 0 && st.Fleets[0].Shots > 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if st.Fleets[0].Shots == 0 {
		t.Fatalf("no shots reached the board in 30s: %+v", st.Fleets)
	}
	if st.Fleets[0].Kills == 0 {
		t.Fatalf("the piloted fleet scored nothing in 30s: %+v", st.Fleets)
	}
	if st.Fleets[0].Program != "hunter.filo" {
		t.Errorf("fleet 1 label is %q", st.Fleets[0].Program)
	}

	// Ceasing stops the game, and the garage notices.
	if err := g.Cease(); err != nil {
		t.Fatalf("cease: %v", err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !g.Status().Running {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("the garage still reports a battle five seconds after ceasing it")
}

// Cease ASKS the battle to end, over the channel it is already configured
// through, and the battle ends itself. The proof is the exit status: a game
// that left its own loop exits cleanly, while a killed one comes back from
// Wait as "signal: killed" — which the garage puts on screen in red, so for
// as long as Cease meant kill, every ordinary stop reported an error for the
// thing the user had just asked for.
//
// It is also the portable half of crg's point: no signals, no per-platform
// process handling, the same seven bytes everywhere.
func TestLiveCeaseAsksInsteadOfKilling(t *testing.T) {
	bin := os.Getenv("SKIRMISH_GAME")
	if bin == "" {
		t.Skip("SKIRMISH_GAME is not set: skipping the live battle")
	}
	g := newGarage(bin)
	defer func() { _ = g.Cease() }()

	err := g.Deploy(Setup{Mode: "endless", Map: "arena", Factions: 2, Window: true})
	if err != nil {
		t.Fatalf("deploy: %v", err)
	}
	// Let the battle really be up: quitting during the launch would prove
	// nothing about a running game answering its control plane.
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) && g.Status().Launching {
		time.Sleep(100 * time.Millisecond)
	}
	if g.Status().Battles == 0 {
		t.Fatalf("no battle came up to ask: %+v", g.Status())
	}

	start := time.Now()
	if err := g.Cease(); err != nil {
		t.Fatalf("cease: %v", err)
	}
	st := g.Status()
	if st.Running {
		t.Fatal("Cease returned with the battle still flying")
	}
	if st.LastErr != "" {
		t.Fatalf("stopping the battle reported %q — it was killed, not asked", st.LastErr)
	}
	t.Logf("the battle ended itself in %s", time.Since(start).Round(time.Millisecond))
}

// Toggling a LAUNCH flag has to replace the process, not reconfigure the one
// flying: start a battle in a window, uncheck windowed, deploy again, and the
// window must go away and the battle come back as the overlay. crg asked for
// exactly this and it is easy to get wrong, because every other knob on the
// form is handled by talking to the running game.
func TestUncheckingWindowedReplacesTheBattle(t *testing.T) {
	bin := os.Getenv("SKIRMISH_GAME")
	if bin == "" {
		t.Skip("SKIRMISH_GAME is not set: skipping the live battle")
	}
	g := newGarage(bin)
	defer func() { _ = g.Cease() }()

	windowed := Setup{Mode: "lastfleet", Map: "arena", Factions: 2, Ships: 3, Window: true}
	if err := g.Deploy(windowed); err != nil {
		t.Fatalf("deploy windowed: %v", err)
	}
	first := g.pid()
	if first == 0 {
		t.Fatal("no battle process after the first deploy")
	}

	overlay := windowed
	overlay.Window = false
	if !needsRelaunch(windowed, overlay) {
		t.Fatal("unchecking windowed did not ask for a relaunch")
	}
	if err := g.Deploy(overlay); err != nil {
		t.Fatalf("deploy overlay: %v", err)
	}
	second := g.pid()
	if second == 0 {
		t.Fatal("no battle process after the second deploy")
	}
	if second == first {
		t.Error("the same process kept flying: the window would still be on screen")
	}
	// The old one being GONE — not merely replaced in the garage's hand — is
	// what the second Deploy returning nil now MEANS: it refuses to launch
	// until the previous process has been reaped, and reaped is cmd.Wait
	// returning, which is the operating system's own word for it. This used to
	// be a syscall.Kill(pid, 0) probe, which asked the OS the same question in
	// a way that did not exist on Windows and broke the build there.
}

// Deploy returns the moment the process starts, but the battle takes seconds
// to appear — a window has to be created and the view has to settle. crg had
// time to click Deploy several times into that gap because nothing on screen
// said anything was happening. Launching is that gap, named: true from the
// launch until the battle reports in, which is what the page disables the row
// and runs the bar on.
func TestLaunchingCoversTheGapUntilTheBattleReportsIn(t *testing.T) {
	bin := os.Getenv("SKIRMISH_GAME")
	if bin == "" {
		t.Skip("SKIRMISH_GAME is not set: skipping the live battle")
	}
	g := newGarage(bin)
	defer func() { _ = g.Cease() }()

	if st := g.Status(); st.Launching {
		t.Fatal("an idle garage reports itself launching")
	}
	start := time.Now()
	if err := g.Deploy(Setup{Mode: "lastfleet", Map: "arena", Factions: 2, Ships: 3, Window: true}); err != nil {
		t.Fatalf("deploy: %v", err)
	}
	if st := g.Status(); !st.Launching || !st.Running {
		t.Fatalf("right after Deploy: launching=%v running=%v — the gap is unnamed", st.Launching, st.Running)
	}

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if !g.Status().Launching {
			t.Logf("the gap the user was clicking into: %s", time.Since(start).Round(time.Millisecond))
			if !g.Status().Running {
				t.Fatal("stopped launching by dying rather than by starting")
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("still launching after 20s")
}

// agentScript writes a driver that flies whatever fleet it is given: on every
// state line it restates one standing order for every plausible ship id —
// move east, never fire. Not knowing which ids it owns is fine, since an order
// for a ship that is not yours is an order nobody reads.
//
// It is a FILE, not an inline command, and that is the documented workaround
// for the one sharp edge of -ipc: commands are separated by commas, so a
// command containing one cannot be written inline (the garage refuses it
// rather than launching a battle the user did not describe).
func agentScript(t *testing.T) string {
	t.Helper()
	const src = `
o=""
i=1
while [ $i -le 64 ]; do
  o="$o{\"id\":$i,\"move\":[1,0]}"
  [ $i -lt 64 ] && o="$o,"
  i=$((i+1))
done
o="{\"orders\":[$o]}"
while IFS= read -r line; do printf '%s\n' "$o"; done
`
	path := filepath.Join(t.TempDir(), "agent.sh")
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatalf("writing the agent: %v", err)
	}
	return path
}

// The whole external-pilot chain against the real game: the garage passes the
// command as -ipc, the game runs it, and its orders fly a faction — the door
// an LLM agent (or a program in any language) comes through.
//
// The proof is an ASYMMETRY the board can see. A ship under a standing order
// is flown by that order alone: the house brain, which is what makes ships
// hunt and shoot, never runs for it. So an agent that orders movement and
// never orders fire produces a fleet that barely shoots, next to a
// house-brain fleet that shoots steadily. Counting shots on one fleet would
// prove nothing — both would climb.
func TestLiveAgentFliesAFaction(t *testing.T) {
	bin := os.Getenv("SKIRMISH_GAME")
	if bin == "" {
		t.Skip("SKIRMISH_GAME is not set: skipping the live battle")
	}
	if runtime.GOOS == "windows" {
		t.Skip("the driver in this test is a shell script; agents themselves are cross-platform")
	}
	command := "/bin/sh " + agentScript(t)

	g := newGarage(bin)
	defer func() { _ = g.Cease() }()
	err := g.Deploy(Setup{
		Mode: "endless", Map: "arena", Factions: 2,
		Commands: []string{command, ""},
		Window:   true,
	})
	if err != nil {
		t.Fatalf("deploy: %v", err)
	}

	deadline := time.Now().Add(40 * time.Second)
	var st Status
	for time.Now().Before(deadline) {
		st = g.Status()
		if st.Battles > 0 && st.Fleets[1].Shots > 20 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if st.Battles == 0 {
		t.Fatalf("no battle was announced: the game may have refused the agent: %+v", st)
	}
	if st.Fleets[1].Shots <= 20 {
		t.Fatalf("the house-brain fleet never got going, so there is nothing to compare: %+v", st.Fleets)
	}
	if st.Fleets[0].Down != "" {
		t.Fatalf("the agent stopped flying: %s", st.Fleets[0].Down)
	}
	// A few shots are honest: between the fleet landing and the agent's first
	// order there is a breath during which the house brain still flies them.
	if st.Fleets[0].Shots > st.Fleets[1].Shots/4 {
		t.Fatalf("the agent's fleet fired %d against the house brain's %d: its orders are not flying the ships",
			st.Fleets[0].Shots, st.Fleets[1].Shots)
	}
	if st.Fleets[0].Program != command {
		t.Errorf("the board should name the agent flying fleet 1, got %q", st.Fleets[0].Program)
	}
}

// An agent that dies hands its faction back to the house brain — and the
// board says so. Without this the fleet just carries on flying differently,
// and the user cannot tell a crashed program from a program that decided to
// patrol. The whole chain is under test: the child exits, the engine notices,
// it says so on the control stream, and the garage puts it on the board.
func TestLiveDeadAgentIsReportedOnTheBoard(t *testing.T) {
	bin := os.Getenv("SKIRMISH_GAME")
	if bin == "" {
		t.Skip("SKIRMISH_GAME is not set: skipping the live battle")
	}

	g := newGarage(bin)
	defer func() { _ = g.Cease() }()
	err := g.Deploy(Setup{
		Mode: "endless", Map: "arena", Factions: 2,
		Commands: []string{"exit 3", ""}, // a program that dies at once
		Window:   true,
	})
	if err != nil {
		t.Fatalf("deploy: %v", err)
	}

	deadline := time.Now().Add(40 * time.Second)
	for time.Now().Before(deadline) {
		st := g.Status()
		if st.Fleets[0].Down != "" {
			if !st.Running {
				t.Fatal("the game died with its agent; a dead driver must only cost its faction")
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("the agent died and the board never said so: %+v", g.Status().Fleets)
}
