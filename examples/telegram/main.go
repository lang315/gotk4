package main

import (
	"os"
	"runtime"

	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

func init() { runtime.LockOSThread() } // GTK owns the main OS thread

func main() {
	app := gtk.NewApplication("com.github.diamondburned.gotk4.telegram", gio.ApplicationFlagsNone)
	app.ConnectActivate(func() {
		w := gtk.NewApplicationWindow(app)
		w.SetTitle("gotk4 Telegram")
		w.SetDefaultSize(900, 640)
		w.SetChild(gtk.NewLabel("hello gotk4"))
		w.SetVisible(true)
	})
	os.Exit(app.Run(os.Args))
}
