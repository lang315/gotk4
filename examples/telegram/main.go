package main

import (
	"context"
	"errors"
	"io/fs"
	"log"
	"os"
	"runtime"

	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/diamondburned/gotk4/examples/telegram/internal/config"
	"github.com/diamondburned/gotk4/examples/telegram/internal/model"
	"github.com/diamondburned/gotk4/examples/telegram/internal/tg"
	"github.com/diamondburned/gotk4/examples/telegram/internal/ui"
)

func init() { runtime.LockOSThread() } // GTK owns the main OS thread

func main() {
	app := gtk.NewApplication("com.github.diamondburned.gotk4.telegram", gio.ApplicationFlagsNone)
	app.ConnectActivate(func() { activate(app) })
	os.Exit(app.Run(os.Args))
}

func activate(app *gtk.Application) {
	dir, err := config.DefaultDir()
	if err != nil {
		log.Fatal(err)
	}

	w := gtk.NewApplicationWindow(app)
	w.SetTitle("gotk4 Telegram")
	w.SetDefaultSize(900, 640)

	stack := gtk.NewStack()
	login := ui.NewLoginPage()
	stack.AddNamed(login.Widget(), "login")
	stack.AddNamed(gtk.NewLabel("connected — chat UI lands in Task 9"), "main")
	w.SetChild(stack)
	w.SetVisible(true)

	// Hoisted, restartable client state. UI thread owns these variables:
	// they are written only from start()/logout (both run on the UI thread).
	var (
		client *tg.Client
		cancel context.CancelFunc
	)

	onEvent := func(ev model.Event) { // UI thread
		switch ev.Kind {
		case model.EventAuthNeeded:
			stack.SetVisibleChildName("login")
		case model.EventReady:
			stack.SetVisibleChildName("main")
		}
	}

	start := func(cfg config.Config) { // UI thread; safe to call again (logout/re-login)
		if cancel != nil {
			cancel()
		}
		ctx, cn := context.WithCancel(context.Background())
		cancel = cn
		client = tg.NewClient(cfg, dir)
		cl := client // pumps capture THIS client, not the mutable variable

		go func() { // event pump
			for {
				select {
				case <-ctx.Done():
					return
				case ev := <-cl.Events:
					// ev is freshly bound per select execution (not a shared
					// loop variable), so the closure below is safe as-is.
					coreglib.IdleAdd(func() { onEvent(ev) })
				}
			}
		}()
		go func() { // auth prompt pump
			for {
				select {
				case <-ctx.Done():
					return
				case pr := <-cl.Auth.Prompts:
					// pr is freshly bound per select execution (not a shared
					// loop variable), so the closure below is safe as-is.
					coreglib.IdleAdd(func() {
						login.Show(pr)
						stack.SetVisibleChildName("login")
					})
				}
			}
		}()
		go func() {
			if err := cl.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
				log.Println("client stopped:", err)
			}
		}()
	}

	cfg, err := config.Load(dir)
	switch {
	case err == nil:
		start(*cfg)
	case errors.Is(err, fs.ErrNotExist):
		setup := ui.NewSetupPage(func(id int, hash string) {
			c := config.Config{APIID: id, APIHash: hash}
			if err := config.Save(dir, &c); err != nil {
				log.Println("save config:", err)
				return
			}
			start(c)
		})
		stack.AddNamed(setup, "setup")
		stack.SetVisibleChildName("setup")
	default:
		log.Fatal(err)
	}
}
