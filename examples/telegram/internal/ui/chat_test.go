// chat_test.go
package ui

import (
	"slices"
	"testing"
	"time"

	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/diamondburned/gotk4/examples/telegram/internal/model"
)

func TestSetAvatarSingleColorClass(t *testing.T) {
	if !gtkOK {
		t.Skip("no display; GTK cannot init")
	}
	lb := gtk.NewLabel("")
	lb.AddCSSClass("avatar")
	setAvatar(lb, "News Channel", 8) // 8 % 7 = 1
	setAvatar(lb, "Alice", 0)        // must swap, not accumulate
	classes := lb.CSSClasses()
	if !slices.Contains(classes, "avatar-0") || slices.Contains(classes, "avatar-1") {
		t.Fatalf("classes = %v, want avatar-0 only", classes)
	}
	if lb.Text() != "A" {
		t.Fatalf("initials = %q", lb.Text())
	}
}

func TestShowDialogHeader(t *testing.T) {
	if !gtkOK {
		t.Skip("no display; GTK cannot init")
	}
	loaded := 0
	v := NewChatView(ChatActions{LoadHistory: func(model.Dialog) { loaded++ }})
	v.showDialog(model.Dialog{Title: "Group X", Kind: model.PeerChat, Members: 12,
		PeerID: 1, CanSend: true, LastDate: time.Now()})
	if v.headerTitle.Text() != "Group X" {
		t.Fatalf("header title = %q", v.headerTitle.Text())
	}
	if v.headerSub.Text() != "12 members" {
		t.Fatalf("header sub = %q", v.headerSub.Text())
	}
	if loaded != 1 {
		t.Fatalf("LoadHistory calls = %d", loaded)
	}
	v.Reset()
	if v.headerTitle.Text() != "" {
		t.Fatal("Reset must clear header")
	}
}
