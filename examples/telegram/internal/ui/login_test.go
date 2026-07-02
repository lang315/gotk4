// login_test.go
package ui

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/diamondburned/gotk4/examples/telegram/internal/model"
	"github.com/diamondburned/gotk4/examples/telegram/internal/tg"
)

// gtkOK is decided by a single InitCheck: repeated init attempts after a
// failure can leave GTK half-initialized and segfault (observed on macOS
// with a forced-unavailable GDK_BACKEND), so every test consults this one
// result instead of re-calling InitCheck.
var gtkOK bool

func TestMain(m *testing.M) {
	gtkOK = gtk.InitCheck()
	os.Exit(m.Run())
}

// gtkEntry unwraps the gota entry to the underlying *gtk.Entry so the test
// can drive the real activate signal (what Enter does).
func gtkEntry(p *LoginPage) *gtk.Entry {
	return p.entry.ToWidget().Object.Cast().(*gtk.Entry)
}

func recvReply(t *testing.T, ch chan string) string {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(2 * time.Second):
		t.Fatal("no reply within 2s")
		return ""
	}
}

func TestLoginPageAutoSubmitAllSteps(t *testing.T) {
	if !gtkOK {
		t.Skip("no display; GTK cannot init")
	}
	p := NewLoginPage()
	ent := gtkEntry(p)

	steps := []struct {
		kind     model.PromptKind
		input    string
		wantMask bool
	}{
		{model.PromptPhone, "+15551234567", false},
		{model.PromptCode, "12345", false},
		{model.PromptPassword, "hunter2", true},
	}
	for _, s := range steps {
		pr := tg.Prompt{Kind: s.kind, Reply: make(chan string, 1)}
		p.Show(pr)

		if ent.Visibility() == s.wantMask { // Visibility()==false means masked
			t.Fatalf("kind %v: entry masking = %v, want %v", s.kind, !ent.Visibility(), s.wantMask)
		}

		ent.SetText(s.input)
		// gtk.Widget.Activate() is a no-op for GtkEntry on this GTK version:
		// GtkEntry does not register itself as its own widget-class
		// "activate signal" (that's only true for the internal delegate
		// GtkText, which forwards to GtkEntry::activate) — verified empirically
		// (Widget.Activate() returns false and never invokes ConnectActivate
		// handlers here). Emit the "activate" signal directly instead; this is
		// what OnEnter's ConnectActivate handler chain listens for, i.e. the
		// real Enter path.
		ent.Object.Emit("activate")

		if got := recvReply(t, pr.Reply); got != s.input {
			t.Fatalf("kind %v: reply = %q, want %q", s.kind, got, s.input)
		}
		if ent.Text() != "" {
			t.Fatalf("kind %v: entry not cleared after submit", s.kind)
		}
	}
}

func TestLoginPageEmptySubmitIsIgnored(t *testing.T) {
	if !gtkOK {
		t.Skip("no display; GTK cannot init")
	}
	p := NewLoginPage()
	ent := gtkEntry(p)

	pr := tg.Prompt{Kind: model.PromptPhone, Reply: make(chan string, 1)}
	p.Show(pr)
	ent.Object.Emit("activate") // empty entry: must not reply

	select {
	case v := <-pr.Reply:
		t.Fatalf("empty submit produced reply %q", v)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestLoginPageRetryTitle(t *testing.T) {
	if !gtkOK {
		t.Skip("no display; GTK cannot init")
	}
	p := NewLoginPage()

	p.Show(tg.Prompt{Kind: model.PromptPassword, Retry: true, Reply: make(chan string, 1)})
	if got := p.label.GetText(); !strings.Contains(got, "rejected") {
		t.Fatalf("retry title = %q, want it to mention rejection", got)
	}

	p.Show(tg.Prompt{Kind: model.PromptPassword, Reply: make(chan string, 1)})
	if got := p.label.GetText(); strings.Contains(got, "rejected") {
		t.Fatalf("non-retry title = %q, must not mention rejection", got)
	}
}
