package garage

import (
	"github.com/crgimenes/glaze"
	"github.com/crgimenes/glaze/menu"
)

// installMenu gives the garage a real application menu.
//
// It is not decoration: on macOS a WKWebView has NO route to the editing
// commands unless a menu wires them, so Cmd+C and Cmd+V simply do nothing in
// every text field on the page — a program path typed by hand is a program
// path that cannot be pasted. The Edit items below carry Objective-C selectors
// instead of Go callbacks, which sends them down the responder chain to
// whatever view has focus (glaze/menu documents the mechanism). On Windows and
// Linux those items are skipped: there the focused control already handles its
// own shortcuts.
//
// A failure here is reported and swallowed on purpose. A garage without a menu
// bar is a garage with awkward text fields; a garage that refuses to open
// because a menu would not build is worse.
//
// NO Options.Dispatch, and that is not an oversight. Set with a dispatcher
// marshals the AppKit work onto it and BLOCKS until it runs — but glaze's
// Dispatch queues onto the main queue, which nothing drains until Run starts
// the run loop. Passing the obvious-looking w.Dispatch from here, before Run,
// hangs the process forever with an empty screen and no error: shipped exactly
// that and crg had to Ctrl+C it. Called from the UI thread, as it is here, the
// dispatcher must be nil.
func installMenu(w glaze.WebView, quit func()) (*menu.Menu, error) {
	return menu.Set([]menu.Item{
		{
			Title: "Skirmish",
			Submenu: []menu.Item{
				{Title: "Quit Skirmish", Shortcut: "cmd+q", OnClick: quit},
			},
		},
		{
			Title: "Edit",
			Submenu: []menu.Item{
				{Title: "Undo", Shortcut: "cmd+z", Selector: "undo:"},
				{Title: "Redo", Shortcut: "cmd+shift+z", Selector: "redo:"},
				{Separator: true},
				{Title: "Cut", Shortcut: "cmd+x", Selector: "cut:"},
				{Title: "Copy", Shortcut: "cmd+c", Selector: "copy:"},
				{Title: "Paste", Shortcut: "cmd+v", Selector: "paste:"},
				{Title: "Select All", Shortcut: "cmd+a", Selector: "selectAll:"},
			},
		},
	}, menu.Options{Window: w.Window()})
}
