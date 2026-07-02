// login.go
package ui

import (
	"github.com/diamondburned/gotk4/pkg/gota"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/diamondburned/gotk4/examples/telegram/internal/model"
	"github.com/diamondburned/gotk4/examples/telegram/internal/tg"
)

type LoginPage struct {
	root   *gtk.Widget
	label  *gota.LabelWidget
	entry  *gota.EntryWidget
	submit func()
}

func NewLoginPage() *LoginPage {
	p := &LoginPage{}
	p.label = gota.Label("")
	p.entry = gota.Entry().OnEnter(func(*gota.EntryWidget) {
		if p.submit != nil {
			p.submit()
		}
	})
	btn := gota.Button("Next").OnClick(func(*gota.ButtonWidget) {
		if p.submit != nil {
			p.submit()
		}
	})
	p.root = gota.VBox(8).Append(p.label, p.entry, btn).ToWidget()
	p.root.SetMarginTop(24)
	p.root.SetMarginBottom(24)
	p.root.SetMarginStart(24)
	p.root.SetMarginEnd(24)
	return p
}

func (p *LoginPage) Widget() gtk.Widgetter { return p.root }

// Show configures the page for one auth prompt. UI thread only.
func (p *LoginPage) Show(pr tg.Prompt) {
	titles := map[model.PromptKind]string{
		model.PromptPhone:    "Phone number (international format)",
		model.PromptCode:     "Login code",
		model.PromptPassword: "Two-factor password",
	}
	title := titles[pr.Kind]
	if pr.Retry {
		title += " — previous attempt was rejected, try again"
	}
	p.label.SetText(title)
	p.entry.SetText("")
	p.entry.PasswordMode(pr.Kind == model.PromptPassword)
	p.submit = func() {
		v := p.entry.GetText()
		if v == "" {
			return
		}
		p.entry.SetText("")
		go func() { pr.Reply <- v }() // never block the UI thread
	}
}
