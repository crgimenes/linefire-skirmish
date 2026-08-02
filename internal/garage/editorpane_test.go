package garage

import (
	"github.com/crgimenes/linefire/game"

	"os"
	"strings"
	"testing"
)

// The reference program is the one every player starts from, and this repo
// owns it — so this repo is where it must validate clean against the same
// function the editor's Validate button calls.
func TestTheReferenceProgramValidatesClean(t *testing.T) {
	src, err := os.ReadFile("../../ai/hunter.filo")
	if err != nil {
		t.Fatalf("reading the reference program: %v", err)
	}
	if issue := game.ValidatePilot(string(src)); issue != nil {
		t.Fatalf("hunter.filo failed validation: line %d: %s", issue.Line, issue.Msg)
	}
}

// quickTest is the editor's smoke signal: it must fight real battles and
// report a line a person can read. The reference program should not be
// slaughtered by the house brain.
func TestQuickTestFightsAndReports(t *testing.T) {
	src, err := os.ReadFile("../../ai/hunter.filo")
	if err != nil {
		t.Fatalf("reading the reference program: %v", err)
	}
	out, err := quickTest(string(src))
	if err != nil {
		t.Fatalf("quickTest: %v", err)
	}
	if !strings.Contains(out, "battles") || !strings.Contains(out, "house brain") {
		t.Errorf("the report does not read like one: %q", out)
	}
}

// A broken program must fail the quick test loudly, not fight 20 battles on
// the house brain and report someone else's score as yours.
func TestQuickTestRefusesABrokenProgram(t *testing.T) {
	_, err := quickTest("(broken")
	if err == nil {
		t.Fatal("a program that does not compile quick-tested clean")
	}
}
