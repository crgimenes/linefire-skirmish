package garage

import (
	"slices"
	"strings"
	"testing"
	"time"
)

// The garage understands a battle purely from the event stream — the same
// stream the trace records and the checker audits. This is that fold, with no
// process and no window involved.
func TestGarageFoldsTheBattleStream(t *testing.T) {
	g := newGarage("linefire-skirmish")
	g.setup = Setup{Factions: 2, Programs: []string{"ai/hunter.filo", ""}}

	// The ship ids here (7..10) deliberately match no faction number: a kill
	// names its shooter by SHIP, and crediting the fleet means looking the
	// ship up in the spawn roll. Ids that happened to equal faction numbers
	// once hid this bug behind a coincidence.
	for _, ev := range []event{
		{Ev: "battle", Mode: "lastfleet", MapName: "Open Field", Seed: 42},
		{Ev: "spawn", ID: 7, Faction: 1}, {Ev: "spawn", ID: 8, Faction: 1},
		{Ev: "spawn", ID: 9, Faction: 2}, {Ev: "spawn", ID: 10, Faction: 2},
		{Ev: "shot", Faction: 1}, {Ev: "shot", Faction: 1}, {Ev: "shot", Faction: 2},
		{Ev: "death", ID: 9, Faction: 2, By: 7},
		{Ev: "death", ID: 10, Faction: 2, By: 8},
		{Ev: "result", Winner: 1, Ticks: 600},
	} {
		g.apply(ev)
	}

	st := g.Status()
	if st.Mode != "lastfleet" || st.MapName != "Open Field" || st.Seed != 42 || st.Battles != 1 {
		t.Fatalf("battle header not folded: %+v", st)
	}
	if len(st.Fleets) != 2 {
		t.Fatalf("want a row per faction, got %d", len(st.Fleets))
	}
	one, two := st.Fleets[0], st.Fleets[1]
	if one.Alive != 2 || one.Kills != 2 || one.Losses != 0 || one.Shots != 2 {
		t.Errorf("faction 1 board wrong: %+v", one)
	}
	if two.Alive != 0 || two.Kills != 0 || two.Losses != 2 || two.Shots != 1 {
		t.Errorf("faction 2 board wrong: %+v", two)
	}
	if one.Program != "hunter.filo" || two.Program != "house brain" {
		t.Errorf("fleet labels wrong: %q and %q", one.Program, two.Program)
	}
	if st.Outcome == "" {
		t.Error("the outcome was not reported")
	}

	// A new battle clears the board but keeps counting battles.
	g.apply(event{Ev: "battle", Mode: "timed", MapName: "Labyrinth"})
	st = g.Status()
	if st.Battles != 2 || st.Fleets[0].Kills != 0 || st.Outcome != "" {
		t.Fatalf("a new battle should start from a clean board: %+v", st)
	}
}

// A draw reads as a draw, not as "faction 0 prevails".
func TestGarageReportsDraws(t *testing.T) {
	g := newGarage("linefire-skirmish")
	g.setup = Setup{Factions: 2}
	g.apply(event{Ev: "result", Winner: 0, Ticks: 7200})
	if got := g.Status().Outcome; got != "the battle ends in a draw after 120.0s" {
		t.Fatalf("draw reported as %q", got)
	}
}

// Some of the setup is the match's and changes mid-flight; some belongs to the
// game process and needs a relaunch. Getting this wrong would relabel the
// board while the old programs kept flying — the UI would be lying.
func TestRelaunchOnlyForProcessSettings(t *testing.T) {
	base := Setup{Mode: "lastfleet", Map: "arena", Factions: 2, Ships: 8, Duration: 120,
		Programs: []string{"a.filo", ""}}

	live := []Setup{base, base, base}
	live[0].Mode = "timed"
	live[1].Ships = 4
	live[2].Duration = 60
	for _, s := range live {
		if needsRelaunch(base, s) {
			t.Errorf("%+v is a match setting: it should change in flight", s)
		}
	}

	proc := []Setup{base, base, base, base, base}
	proc[0].Factions = 3
	proc[1].Map = "maze"
	proc[2].Window = true
	proc[3].Programs = []string{"a.filo", "b.filo"}
	// An agent is a CHILD PROCESS of the game: it is started with the battle,
	// so swapping one in is a relaunch, not a match command.
	proc[4].Commands = []string{"", "python3 bot.py"}
	for _, s := range proc {
		if !needsRelaunch(base, s) {
			t.Errorf("%+v is a launch flag: it needs a relaunch", s)
		}
	}
}

// A faction flown by an external agent reaches the game as -ipc, in the same
// faction order as -ai and alongside it: one fleet list, two kinds of pilot.
func TestAgentCommandsReachTheGameInFactionOrder(t *testing.T) {
	s := Setup{Mode: "lastfleet", Map: "arena", Factions: 3,
		Programs: []string{"a.filo", "", ""},
		Commands: []string{"", "python3 bot.py", ""}}
	args := launchArgs(s)

	find := func(flag string) string {
		for i, a := range args {
			if a == flag && i+1 < len(args) {
				return args[i+1]
			}
		}
		return ""
	}
	if got := find("-ipc"); got != ",python3 bot.py," {
		t.Fatalf("the agent must land in faction 2's slot, got %q from %v", got, args)
	}
	if got := find("-ai"); got != "a.filo,," {
		t.Fatalf("the Filo program must keep its own slot, got %q", got)
	}

	// No agents at all means no flag: an empty -ipc would start a driverless
	// battle differently for no reason.
	plain := s
	plain.Commands = []string{"", "", ""}
	for _, a := range launchArgs(plain) {
		if a == "-ipc" {
			t.Fatalf("a fleet list with no agents must not pass -ipc: %v", launchArgs(plain))
		}
	}
}

// Two ways a setup would launch a process that dies on its own command line,
// where the only word of it is a line on a terminal nobody is watching.
func TestTheGarageRefusesASetupTheGameCannotFly(t *testing.T) {
	g := newGarage("linefire-skirmish")

	comma := Setup{Mode: "endless", Map: "arena", Factions: 2,
		Commands: []string{"python3 -c import x, y", ""}}
	err := g.Deploy(comma)
	if err == nil {
		t.Fatal("a comma in an agent command silently hands its tail to the next faction")
	}
	if !strings.Contains(err.Error(), "comma") {
		t.Fatalf("the message must name the comma, got %v", err)
	}

	both := Setup{Mode: "endless", Map: "arena", Factions: 2,
		Programs: []string{"a.filo", ""},
		Commands: []string{"python3 bot.py", ""}}
	if err := g.Deploy(both); err == nil {
		t.Fatal("a faction cannot be flown by a program AND an agent; the engine refuses it at launch")
	}

	if g.pid() != 0 {
		t.Fatal("a refused setup must not have started anything")
	}
}

// An agent that dies hands its faction to the house brain — which looks
// exactly like an agent that decided to patrol. The board has to say so, and
// keep saying it: the fleet flies the house brain for every restage that
// follows, so the notice must outlive the board reset a new battle performs.
func TestADeadAgentIsReportedOnTheBoardAndSurvivesTheNextBattle(t *testing.T) {
	g := newGarage("linefire-skirmish")
	g.setup = Setup{Factions: 2, Commands: []string{"", "python3 bot.py"}}

	g.apply(event{Ev: "driver", Faction: 2, Why: "closed its stream"})
	fleet := func(f int) FleetBoard { return g.Status().Fleets[f-1] }
	if got := fleet(2).Down; got != "closed its stream" {
		t.Fatalf("faction 2's agent died and the board says %q", got)
	}
	if got := fleet(2).Program; got != "python3 bot.py" {
		t.Fatalf("the row must still name the agent that was meant to fly it, got %q", got)
	}
	if fleet(1).Down != "" {
		t.Fatal("faction 1 has no agent: nothing of its own to report")
	}

	g.apply(event{Ev: "battle", Mode: "endless"})
	if fleet(2).Down == "" {
		t.Fatal("a new battle cleared the notice, but the agent is still dead")
	}
}

// The -ai flag is only built when some faction actually has a program, and it
// keeps faction order (an empty slot is a real position, not a gap).
func TestProgramFlagKeepsFactionOrder(t *testing.T) {
	if got := joinSlots([]string{"", ""}); got != "" {
		t.Errorf("no programs should mean no flag, got %q", got)
	}
	if got := joinSlots([]string{"", "b.filo"}); got != ",b.filo" {
		t.Errorf("faction 2's program must stay in slot 2, got %q", got)
	}
	if got := joinSlots([]string{"a.filo", "", "c.filo"}); got != "a.filo,,c.filo" {
		t.Errorf("faction order broken: %q", got)
	}
}

// The garage takes its focus back once the battle it launched is really up —
// the game activates its own window (it must, or macOS throttles it), and
// without this the next click on the garage is spent waking the window instead
// of pressing the button under it. Only on the FIRST battle of a launch:
// restaging must not yank focus away from whatever the user moved on to.
func TestTheGarageTakesItsFocusBackWhenTheBattleIsUp(t *testing.T) {
	g := newGarage("linefire-skirmish")
	reclaimed := make(chan struct{}, 4)
	g.reclaim = func() { reclaimed <- struct{}{} }

	g.apply(event{Ev: "battle", Mode: "lastfleet"})
	select {
	case <-reclaimed:
	case <-time.After(2 * time.Second):
		t.Fatal("the battle came up and the garage never asked for its focus back")
	}

	// The rematch of an endless run, and a restage: the user has moved on by
	// now, and a window that jumps in front of them is worse than a click.
	g.apply(event{Ev: "battle", Mode: "lastfleet"})
	g.apply(event{Ev: "battle", Mode: "lastfleet"})
	select {
	case <-reclaimed:
		t.Fatal("a later battle stole the focus back; only the first may")
	case <-time.After(300 * time.Millisecond):
	}
}

// Sound is a LAUNCH flag: a process already flying cannot grow ears, so
// toggling it has to relaunch rather than restage the running match. And it is
// off unless asked — the overlay is something people leave running over their
// desktop while doing something else, and a battle that starts making noise on
// its own is a battle they close.
func TestSoundIsOffByDefaultAndNeedsARelaunch(t *testing.T) {
	quiet := Setup{Mode: "lastfleet", Map: "arena", Factions: 2, Ships: 4}
	if quiet.Sound {
		t.Fatal("the zero setup asks for sound; silence must be what you get for free")
	}
	loud := quiet
	loud.Sound = true

	if !needsRelaunch(quiet, loud) {
		t.Error("turning sound on did not relaunch: the running process would stay silent")
	}
	if !needsRelaunch(loud, quiet) {
		t.Error("turning sound off did not relaunch: the running process would keep playing")
	}
	// Anything that is not a launch flag must still restage in place.
	louder := loud
	louder.Ships = 6
	if needsRelaunch(loud, louder) {
		t.Error("a fleet-size change relaunched the process instead of restaging the match")
	}

	// And the flag has to actually reach the game's command line.
	if slices.Contains(launchArgs(quiet), "-sound") {
		t.Error("a silent setup still passed -sound")
	}
	if !slices.Contains(launchArgs(loud), "-sound") {
		t.Error("sound was asked for and -sound never reached the game")
	}
}
