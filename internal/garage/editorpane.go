package garage

import (
	"fmt"
	"os"

	linefire "github.com/crgimenes/linefire"
	"github.com/crgimenes/linefire/game"
)

// The editor pane's back end: the page edits, this side touches the disk and
// the game. Writing the fleet's program without leaving the garage is the
// point of the whole view — the game is programs fighting, so the place you
// watch battles is the place you write the combatants.

// readProgram hands the page a program's source. The size cap is a courtesy
// guard: a fleet program is hand-written Filo, and a path that turns out to be
// a gigabyte of something else should fail loudly, not stall the UI.
func readProgram(path string) (string, error) {
	const maxProgram = 1 << 20
	st, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if st.Size() > maxProgram {
		return "", fmt.Errorf("%s is %d bytes; a fleet program is not", path, st.Size())
	}
	b, err := os.ReadFile(path) // #nosec G304 -- the user picked their own file in a native dialog
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func writeProgram(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644) // #nosec G306 -- a program the user edits, not a secret
}

// quickTest fights the program headless against the house brain and reports
// the score — the editor's fast loop, in-process: no window, no temp file, a
// couple of seconds. The shape (2 factions, 6 ships, 20 seeded battles) is
// fixed on purpose: this is a smoke signal while writing, not the measuring
// instrument (-sim with -trace remains that).
func quickTest(src string) (string, error) {
	const battles = 20
	wins := map[int]int{}
	ticks := 0
	for i := range battles {
		res, err := game.RunBattle(linefire.Content(), "gameassets", game.BattleOptions{
			Programs: []string{src},
			Factions: 2,
			Ships:    6,
			Seed:     int64(i + 1),
			Battle:   i + 1,
		})
		if err != nil {
			return "", err
		}
		wins[res.Winner]++
		ticks += res.Ticks
	}
	return fmt.Sprintf("your program %d × %d house brain · %d draws — %d battles, avg %.0fs each",
		wins[1], wins[2], wins[0], battles, float64(ticks)/battles/60), nil
}
