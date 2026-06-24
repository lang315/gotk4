// Package gota is a small, fluent convenience layer over the generated gotk4
// bindings. It mirrors the chainable widget API of the original gota framework
// (built for gotk3) but is adapted to GTK4: constructors no longer return
// errors, layout uses Append instead of PackStart, and windows are shown via
// Present.
//
// Each widget is a thin wrapper exposing chainable setters that return the
// concrete wrapper type, so calls compose:
//
//	gota.RunApp("com.example.App", func(win *gota.WindowWidget) {
//		win.Title("Hello").DefaultSize(400, 300).Child(
//			gota.VBox(8).Append(
//				gota.Label("Name:"),
//				gota.Entry().SetPlaceholder("type here"),
//				gota.Button("OK").Class("suggested-action").
//					OnClick(func(*gota.ButtonWidget) { println("clicked") }),
//			),
//		)
//	})
package gota

import (
	"os"
	"strings"

	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// IWidget is anything that can yield its underlying *gtk.Widget, so wrappers can
// be parented into containers (Box, Window, ...) interchangeably.
type IWidget interface {
	ToWidget() *gtk.Widget
}

// RunApp creates a GtkApplication, builds its main window inside the activate
// signal, presents it, and blocks until the app exits. It returns the app's
// exit code. build receives the wrapped application window to populate.
func RunApp(appID string, build func(win *WindowWidget)) int {
	app := gtk.NewApplication(appID, gio.ApplicationFlagsNone)
	app.ConnectActivate(func() {
		win := gtk.NewApplicationWindow(app)
		build(wrapWindow(win))
		win.Present()
	})
	return app.Run(os.Args)
}

// IdleAdd schedules fn to run on the GTK main loop thread. Use it to touch
// widgets from a goroutine — GTK is not thread-safe.
func IdleAdd(fn func()) { glib.IdleAdd(fn) }

// parseMnemonic reports whether text is wrapped in {{...}}, which the original
// gota used to opt into a mnemonic (underline-accelerator) label. It returns the
// inner label and true on a match.
func parseMnemonic(text string) (string, bool) {
	if strings.HasPrefix(text, "{{") && strings.HasSuffix(text, "}}") && len(text) >= 4 {
		return text[2 : len(text)-2], true
	}
	return "", false
}
