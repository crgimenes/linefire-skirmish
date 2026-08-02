package garage

import (
	"os"
	"syscall"
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
	// The old one must be GONE, not merely replaced in the garage's hand.
	if err := syscall.Kill(first, 0); err == nil {
		t.Errorf("process %d is still alive after being replaced", first)
	}
}
