package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"runtime"
	"sync/atomic"

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
	cacheDir, _ := config.DefaultCacheDir()

	w := gtk.NewApplicationWindow(app)
	w.SetTitle("gotk4 Telegram")
	w.SetDefaultSize(900, 640)

	stack := gtk.NewStack()
	login := ui.NewLoginPage()
	stack.AddNamed(login.Widget(), "login")
	w.SetChild(stack)
	w.SetVisible(true)

	// Hoisted, restartable client state. UI thread owns cancel/start: they are
	// written only from start()/logout (both run on the UI thread). clientPtr
	// is also read from asyncPic's fetch goroutine (LoadPhoto), off the UI
	// thread, so it must be an atomic pointer rather than a plain variable.
	var (
		clientPtr atomic.Pointer[tg.Client]
		cancel    context.CancelFunc
		start     func(cfg config.Config) // forward-declared: Logout below calls start() before its literal is assigned
	)

	var chat *ui.ChatView
	chat = ui.NewChatView(ui.ChatActions{
		LoadHistory: func(d model.Dialog) {
			cl := clientPtr.Load()
			go func() {
				ms, err := cl.History(context.Background(), d.Kind, d.PeerID)
				if err != nil {
					coreglib.IdleAdd(func() { chat.SetBanner("failed to load history") })
					return
				}
				_ = cl.MarkRead(context.Background(), d.Kind, d.PeerID)
				coreglib.IdleAdd(func() { chat.SetMessages(d, ms) })
			}()
		},
		Send: func(d model.Dialog, text string) {
			cl := clientPtr.Load()
			go func() {
				if err := cl.Send(context.Background(), d.Kind, d.PeerID, text); err != nil {
					coreglib.IdleAdd(func() { chat.SetBanner("send failed") })
				}
			}()
		},
		Logout: func() {
			cl := clientPtr.Load()
			if cancel != nil {
				cancel() // stops pumps + Run
			}
			go func() {
				_ = cl.Logout(context.Background())
				coreglib.IdleAdd(func() {
					chat.Reset()
					stack.SetVisibleChildName("login")
					cfg, err := config.Load(dir)
					if err == nil {
						start(*cfg) // fresh client; auth flow will prompt
					}
				})
			}()
		},
		LoadPhoto: func(ref model.PhotoRef) (string, error) {
			cl := clientPtr.Load()
			if cl == nil {
				return "", fmt.Errorf("not connected")
			}
			return cl.DownloadPhoto(context.Background(), ref, cacheDir)
		},
	})
	stack.AddNamed(chat.Widget(), "main")

	onEvent := func(ev model.Event) { // UI thread
		switch ev.Kind {
		case model.EventAuthNeeded:
			stack.SetVisibleChildName("login")
		case model.EventReady:
			chat.SetBanner("")
			stack.SetVisibleChildName("main")
			cl := clientPtr.Load()
			go func() {
				ds, err := cl.Dialogs(context.Background())
				if err != nil {
					coreglib.IdleAdd(func() { chat.SetBanner("failed to load chats") })
					return
				}
				coreglib.IdleAdd(func() { chat.SetDialogs(ds) })
			}()
		case model.EventNewMessage:
			chat.AppendMessage(ev.Msg)
		case model.EventConnecting:
			chat.SetBanner("connecting…")
		}
	}

	start = func(cfg config.Config) { // UI thread; safe to call again (logout/re-login)
		if cancel != nil {
			cancel()
		}
		ctx, cn := context.WithCancel(context.Background())
		cancel = cn
		cl := tg.NewClient(cfg, dir) // pumps capture THIS client, not the mutable variable
		clientPtr.Store(cl)

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
