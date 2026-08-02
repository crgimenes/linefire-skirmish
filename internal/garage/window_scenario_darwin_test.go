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
		time.AfterFunc(20*time.Second, w.Terminate)
		go driveGarage(w, coords)
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

// driveGarage walks the user's own steps: CLICK the Editor tab, watch the
// caret blink, press ArrowDown, and ask where the caret ended up.
func driveGarage(w glaze.WebView, coords chan [2]float64) {
	// The page, the faction list, and the editor bundle all load first.
	time.Sleep(1500 * time.Millisecond)
	w.Eval(`(function () {
		var r = document.getElementById('tab-editor').getBoundingClientRect();
		coords(r.left + r.width / 2, innerHeight - (r.top + r.height / 2));
	})()`)
	var pt [2]float64
	select {
	case pt = <-coords:
	case <-time.After(5 * time.Second):
		w.Eval(`probe('the page never reported the Editor tab position')`)
		return
	}

	win := objc.ID(uintptr(w.Window())) // #nosec G103 -- the NSWindow the API hands back
	w.Dispatch(func() { clickAt(win, pt[0], pt[1]) })
	time.Sleep(1 * time.Second)
	// The user's exact test: no repositioning, press ArrowDown right where
	// opening the editor left the caret. Record where that was.
	w.Eval(`window.__sel0 = ed ? ed.ta.selectionStart : -1`)
	time.Sleep(200 * time.Millisecond)

	w.Dispatch(func() { pressArrowDown(win) })
	time.Sleep(500 * time.Millisecond)

	w.Eval(`probe(!ed ? 'no editor: the click missed the tab' : [
		'sel0=' + window.__sel0,
		'sel=' + ed.ta.selectionStart,
		'end=' + ed.ta.value.length,
		'focus=' + document.hasFocus(),
		'active=' + (document.activeElement === ed.ta),
		'caretHidden=' + ed.caret.hidden,
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
}
