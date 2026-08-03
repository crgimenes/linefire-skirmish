package garage

import (
	"flag"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/crgimenes/glaze"
	"github.com/ebitengine/purego/objc"
)

// The garage's GUI scenario exists because the editor's own scenario in glaze
// was not enough: every geometry and event assertion passed there, and crg
// still opened THIS window, pressed ArrowDown, and watched the caret stay
// put. Whatever eats the keystroke lives in the garage's environment — the
// navigation, the menu, the options — so the only truthful instrument is the
// real garage window with a real NSEvent sent through its responder chain.

// AppKit runs on one OS thread, so the scenario runs in TestMain (the main
// goroutine) and stashes its result; the TestXxx function only asserts.
var resGarageKey atomic.Value // string

func TestMain(m *testing.M) {
	flag.Parse()
	if !testing.Short() {
		runtime.LockOSThread()
		resGarageKey.Store(garageKeyScenario())
	}
	os.Exit(m.Run())
}

// --- minimal objc, borrowed shape from glaze ---------------------------------

func tSel(name string) objc.SEL { return objc.RegisterName(name) }

func tClass(name string) objc.ID { return objc.ID(objc.GetClass(name)) }

func tStr(s string) objc.ID {
	return tClass("NSString").Send(tSel("stringWithUTF8String:"), s)
}

const (
	nsEventTypeLeftMouseDown = 1
	nsEventTypeLeftMouseUp   = 2
	nsEventTypeKeyDown       = 10
)

type cgPoint struct{ x, y float64 }

// clickAt sends a real mouse click (down + up) through the window, in window
// coordinates (origin bottom-left). This is the user's own gesture on the
// Editor tab — hit-testing, responder moves, focus side effects and all —
// which an Eval'd editFaction() call skips entirely.
func clickAt(win objc.ID, x, y float64) {
	num := int(win.Send(tSel("windowNumber")))
	mouse := func(kind uint) objc.ID {
		return tClass("NSEvent").Send(
			tSel("mouseEventWithType:location:modifierFlags:timestamp:windowNumber:context:eventNumber:clickCount:pressure:"),
			kind, cgPoint{x, y}, uint(0), float64(0), num, objc.ID(0), 0, 1, float64(1))
	}
	win.Send(tSel("sendEvent:"), mouse(nsEventTypeLeftMouseDown))
	win.Send(tSel("sendEvent:"), mouse(nsEventTypeLeftMouseUp))
}

// pressArrowDown sends a real ArrowDown through the window's own responder
// chain — window → first responder → WebKit → DOM — the path a user's
// keystroke takes and the one thing no JS-side dispatch exercises. Must run
// on the UI thread (w.Dispatch).
func pressArrowDown(win objc.ID) {
	ev := tClass("NSEvent").Send(
		tSel("keyEventWithType:location:modifierFlags:timestamp:windowNumber:context:characters:charactersIgnoringModifiers:isARepeat:keyCode:"),
		nsEventTypeKeyDown, cgPoint{0, 0}, uint(0), float64(0),
		int(win.Send(tSel("windowNumber"))), objc.ID(0),
		tStr("\uF701"), tStr("\uF701"), false, uint16(125))
	win.Send(tSel("sendEvent:"), ev)
}

func garageKeyScenario() string {
	report := make(chan string, 1)
	coords := make(chan [2]float64, 1)
	ready := make(chan float64, 1)
	testHook = func(w glaze.WebView) {
		_ = w.Bind("probe", func(s string) {
			select {
			case report <- s:
			default:
			}
			w.Terminate()
		})
		_ = w.Bind("coords", func(x, y float64) {
			select {
			case coords <- [2]float64{x, y}:
			default:
			}
		})
		_ = w.Bind("ready", func(v float64) {
			select {
			case ready <- v:
			default:
			}
		})
		time.AfterFunc(90*time.Second, w.Terminate)
		go driveGarage(w, coords, ready)
	}
	defer func() { testHook = nil }()

	// Game points at a no-op: the scenario never deploys, and the default
	// (this very test binary) would re-open a garage if anything ever did.
	err := Run(Options{Game: "/usr/bin/true"})
	if err != nil {
		return "run: " + err.Error()
	}
	select {
	case r := <-report:
		return r
	default:
		return "no report"
	}
}

// untilReported evaluates js over and over until the page answers on ch. A
// script that runs too early simply throws (the page has not defined its
// functions yet) and is retried.
//
// Every step here used to be a fixed sleep, which is a guess about a machine.
// It held on this desk and failed on a CI runner, where the page took longer
// than the 1.5s allowed for it — a test that reported "the page never said
// where its tab is" as if the garage were broken. Waiting for the answer
// instead of for the clock is both faster here and honest there.
func untilReported[T any](w glaze.WebView, js string, ch chan T, timeout time.Duration) (T, bool) {
	var zero T
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		w.Eval(js)
		select {
		case v := <-ch:
			return v, true
		case <-time.After(250 * time.Millisecond):
		}
	}
	return zero, false
}

// driveGarage walks the user's own steps: CLICK the Editor tab, watch the
// caret blink, press ArrowDown, and ask where the caret ended up.
func driveGarage(w glaze.WebView, coords chan [2]float64, ready chan float64) {
	// Step one doubles as the wait for the page: the fleet list has to exist
	// before its tab has a position.
	pt, ok := untilReported(w, `(function () {
		var r = document.getElementById('tab-editor').getBoundingClientRect();
		if (r.width > 0) coords(r.left + r.width / 2, innerHeight - (r.top + r.height / 2));
	})()`, coords, 45*time.Second)
	if !ok {
		w.Eval(`probe('the page never reported the Editor tab position')`)
		return
	}

	// The agent affordance, on the real page: a faction switched to an agent
	// sends its command as commands[0] and gives up its program slot, because
	// the engine refuses a faction holding both. This is the JS wiring of the
	// fleet list — a typo here reaches the user as a battle that will not
	// launch, with the reason on a terminal they do not have open.
	_, ok = untilReported(w, `(function () {
		toggleAgent(0);
		var cmd = rows.get(1).querySelector('.cmd');
		cmd.value = 'python3 bot.py';
		cmd.dispatchEvent(new Event('change'));
		var s = setup();
		window.__agent = [
			'cmd0=' + s.commands[0],
			'prog0=' + s.programs[0],
			'cmdShown=' + !cmd.hidden,
			'editHidden=' + rows.get(1).querySelector('.edt').hidden,
			'cmd1=' + s.commands[1],
		].join(' ');
		toggleAgent(0);   // back to a program: the rest of the run is the editor
		ready(1);
	})()`, ready, 20*time.Second)
	if !ok {
		w.Eval(`probe('the fleet list never answered about its agent row')`)
		return
	}

	win := objc.ID(uintptr(w.Window())) // #nosec G103 -- the NSWindow the API hands back
	w.Dispatch(func() { clickAt(win, pt[0], pt[1]) })

	// The editor is built on the first click into it and then loads the
	// faction's program, so wait for the buffer to actually be there. The
	// caret's birthplace is recorded WITHOUT touching it: the user's test is
	// to press the arrow wherever opening the editor left it.
	_, ok = untilReported(w, `(function () {
		if (typeof ed === 'undefined' || !ed || !ed.ta.value) return;
		window.__sel0 = ed.ta.selectionStart;
		ready(1);
	})()`, ready, 20*time.Second)
	if !ok {
		w.Eval(`probe('no editor: the click missed the tab')`)
		return
	}

	w.Dispatch(func() { pressArrowDown(win) })
	// The key travels window → first responder → WebKit → DOM, so the caret
	// moves a moment later; report as soon as it has, and fall through to the
	// probe with the details when it never does.
	untilReported(w, `if (ed && ed.ta.selectionStart > 0) ready(ed.ta.selectionStart)`,
		ready, 10*time.Second)

	w.Eval(`probe([
		'sel0=' + window.__sel0,
		'sel=' + ed.ta.selectionStart,
		'end=' + ed.ta.value.length,
		'focus=' + document.hasFocus(),
		'active=' + (document.activeElement === ed.ta),
		window.__agent,
	].join(' '))`)
}

func TestArrowDownMovesTheCaretInTheRealGarage(t *testing.T) {
	got, _ := resGarageKey.Load().(string)
	if got == "" {
		t.Skip("GUI scenario skipped (-short)")
	}
	t.Logf("garage key scenario: %s", got)
	// The user's whole gesture must hold: opening the editor parks the caret
	// at the TOP (sel0=0 — WebKit's default after an assigned value is the
	// END, where ArrowDown has nowhere to go and reads as dead keys), and a
	// real ArrowDown then lands it on line 2, anywhere past 0.
	if !strings.Contains(got, "sel0=0 ") || strings.Contains(got, " sel=0 ") ||
		strings.HasPrefix(got, "run:") || got == "no report" {
		t.Fatalf("opening the editor and pressing ArrowDown did not move the caret: %s", got)
	}

	// The same run carries the fleet list's agent affordance, since a second
	// GUI scenario would cost another window and another launch.
	want := []string{
		"cmd0=python3 bot.py", // the typed command reaches the setup
		"prog0=",              // and takes the faction's whole pilot slot
		"cmdShown=true",       // the field is visible while the faction is an agent
		"editHidden=true",     // there is nothing here to edit: it lives in its own process
		"cmd1=",               // the other faction is untouched
	}
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("the agent row did not report %q: %s", w, got)
		}
	}
}
