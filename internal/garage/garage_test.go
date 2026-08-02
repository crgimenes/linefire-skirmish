package garage

import "testing"

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

	proc := []Setup{base, base, base, base}
	proc[0].Factions = 3
	proc[1].Map = "maze"
	proc[2].Window = true
	proc[3].Programs = []string{"a.filo", "b.filo"}
	for _, s := range proc {
		if !needsRelaunch(base, s) {
			t.Errorf("%+v is a launch flag: it needs a relaunch", s)
		}
	}
}

// The -ai flag is only built when some faction actually has a program, and it
// keeps faction order (an empty slot is a real position, not a gap).
func TestProgramFlagKeepsFactionOrder(t *testing.T) {
	if got := joinPrograms([]string{"", ""}); got != "" {
		t.Errorf("no programs should mean no flag, got %q", got)
	}
	if got := joinPrograms([]string{"", "b.filo"}); got != ",b.filo" {
		t.Errorf("faction 2's program must stay in slot 2, got %q", got)
	}
	if got := joinPrograms([]string{"a.filo", "", "c.filo"}); got != "a.filo,,c.filo" {
		t.Errorf("faction order broken: %q", got)
	}
}
