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

func TestBindMessageInOutAndGrouping(t *testing.T) {
	if !gtkOK {
		t.Skip("no display; GTK cannot init")
	}
	v := NewChatView(ChatActions{})
	c := newMsgCell()
	day := time.Date(2026, 7, 2, 10, 0, 0, 0, time.Local)
	alice1 := model.Message{SenderID: 1, Sender: "Alice", Text: "one", Date: day}
	alice2 := model.Message{SenderID: 1, Sender: "Alice", Text: "two", Date: day.Add(time.Minute)}
	mine := model.Message{SenderID: 9, Out: true, Text: "yo", Date: day.Add(2 * time.Minute)}

	// First of a 2-message group: sender shown, no top-near-4 radius, ghost avatar.
	v.bindMessage(c, alice1, nil, &alice2)
	cl := c.bubble.CSSClasses()
	if !slices.Contains(cl, "bubble-in") || slices.Contains(cl, "r-tl4") {
		t.Fatalf("first-of-group classes = %v", cl)
	}
	if !c.sender.Visible() || c.sender.Text() != "Alice" {
		t.Fatal("sender must show on first of group")
	}
	if c.avatar.Opacity() != 0 {
		t.Fatal("non-last incoming message must ghost the avatar")
	}
	if !c.sepBox.Visible() {
		t.Fatal("date separator must show at the top edge")
	}

	// Last of the group: r-tl4 present, avatar visible, sender hidden, no separator.
	v.bindMessage(c, alice2, &alice1, &mine)
	cl = c.bubble.CSSClasses()
	if !slices.Contains(cl, "r-tl4") {
		t.Fatalf("continuation classes = %v, want r-tl4", cl)
	}
	if c.sender.Visible() {
		t.Fatal("sender must hide inside a group")
	}
	if c.avatar.Opacity() != 1 {
		t.Fatal("last-of-group must show the avatar")
	}
	if c.sepBox.Visible() {
		t.Fatal("no separator inside the same day")
	}

	// Outgoing: right-aligned, out classes, avatar hidden entirely.
	v.bindMessage(c, mine, &alice2, nil)
	cl = c.bubble.CSSClasses()
	if !slices.Contains(cl, "bubble-out") || slices.Contains(cl, "bubble-in") {
		t.Fatalf("out classes = %v", cl)
	}
	if c.row.HAlign() != gtk.AlignEnd {
		t.Fatal("outgoing row must right-align")
	}
	if c.avatar.Visible() {
		t.Fatal("outgoing rows have no avatar")
	}
}

func TestBindMessagePhotoSlot(t *testing.T) {
	if !gtkOK {
		t.Skip("no display; GTK cannot init")
	}
	v := NewChatView(ChatActions{LoadPhoto: func(model.PhotoRef) (string, error) { return "", nil }})
	c := newMsgCell()
	day := time.Now()
	withPhoto := model.Message{SenderID: 1, Text: "[photo]", Date: day,
		Photo: &model.PhotoRef{PhotoID: 1, SizeType: "x"}}
	plain := model.Message{SenderID: 1, Text: "no photo", Date: day}

	v.bindMessage(c, withPhoto, nil, nil)
	if c.photos.FirstChild() == nil {
		t.Fatal("photo message must populate the photo slot")
	}
	v.bindMessage(c, plain, nil, nil) // recycled: slot must be cleared
	if c.photos.FirstChild() != nil {
		t.Fatal("recycled cell must clear the photo slot")
	}
}

func TestComposerFollowsCanSend(t *testing.T) {
	if !gtkOK {
		t.Skip("no display; GTK cannot init")
	}
	sent := 0
	v := NewChatView(ChatActions{
		LoadHistory: func(model.Dialog) {},
		Send:        func(model.Dialog, string) { sent++ },
	})
	if v.sendBtn.Sensitive() {
		t.Fatal("composer must start disabled")
	}
	v.showDialog(model.Dialog{Title: "RO", Kind: model.PeerChannel, PeerID: 1, CanSend: false})
	if v.sendBtn.Sensitive() || v.composer.ToWidget().Sensitive() {
		t.Fatal("read-only channel must disable the composer")
	}
	v.showDialog(model.Dialog{Title: "G", Kind: model.PeerChat, PeerID: 2, CanSend: true})
	if !v.sendBtn.Sensitive() {
		t.Fatal("writable chat must enable the composer")
	}
	v.composer.SetText("hi")
	v.submitComposer()
	if sent != 1 || v.composer.GetText() != "" {
		t.Fatalf("submit: sent=%d text=%q", sent, v.composer.GetText())
	}
}
