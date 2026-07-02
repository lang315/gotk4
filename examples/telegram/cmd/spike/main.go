// cmd/spike/main.go — TEMPORARY. Deleted in Task 6.
package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"runtime"
	"strconv"

	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/diamondburned/gotk4/examples/telegram/internal/config"
	"github.com/diamondburned/gotk4/examples/telegram/internal/model"
	"github.com/diamondburned/gotk4/examples/telegram/internal/tg"
)

func init() { runtime.LockOSThread() }

func main() {
	apiID, err := strconv.Atoi(os.Getenv("TG_API_ID"))
	if err != nil || os.Getenv("TG_API_HASH") == "" {
		log.Fatal("set TG_API_ID and TG_API_HASH")
	}
	cfg := config.Config{APIID: apiID, APIHash: os.Getenv("TG_API_HASH")}
	dir, err := config.DefaultDir()
	if err != nil {
		log.Fatal(err)
	}

	app := gtk.NewApplication("com.github.diamondburned.gotk4.telegram.spike", gio.ApplicationFlagsNone)
	app.ConnectActivate(func() {
		label := gtk.NewLabel("connecting…")
		w := gtk.NewApplicationWindow(app)
		w.SetDefaultSize(400, 120)
		w.SetChild(label)
		w.SetVisible(true)

		client := tg.NewClient(cfg, dir)

		go func() { // auth prompts answered on stdin (spike only!)
			in := bufio.NewScanner(os.Stdin)
			for p := range client.Auth.Prompts {
				names := map[model.PromptKind]string{
					model.PromptPhone: "phone", model.PromptCode: "code", model.PromptPassword: "2FA password",
				}
				fmt.Printf("enter %s: ", names[p.Kind])
				in.Scan()
				p.Reply <- in.Text()
			}
		}()
		go func() {
			for ev := range client.Events {
				if ev.Kind == model.EventReady {
					name := client.SelfName()
					coreglib.IdleAdd(func() { // THE proof: UI mutation from gotd goroutine
						label.SetText("logged in as " + name)
					})
				}
			}
		}()
		go func() {
			if err := client.Run(context.Background()); err != nil {
				log.Println("client stopped:", err)
			}
		}()
	})
	os.Exit(app.Run(nil)) // nil args: spike takes no CLI flags
}
