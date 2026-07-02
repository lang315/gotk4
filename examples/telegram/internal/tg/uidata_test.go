// uidata_test.go
package tg

import (
	"testing"
	"time"

	tgapi "github.com/gotd/td/tg"

	"github.com/diamondburned/gotk4/examples/telegram/internal/model"
)

func storeWith(t *testing.T) *peerStore {
	t.Helper()
	s := newPeerStore()
	ch := &tgapi.Channel{ID: 77, Title: "News Channel", AccessHash: 1}
	ch.SetParticipantsCount(6660) // verify setter name via go doc if compile fails
	s.Add(
		[]tgapi.UserClass{&tgapi.User{ID: 5, FirstName: "Alice", AccessHash: 2}},
		[]tgapi.ChatClass{
			&tgapi.Chat{ID: 42, Title: "Group", ParticipantsCount: 12},
			ch,
		},
	)
	return s
}

func TestMapMessageChannelSenderFallback(t *testing.T) {
	s := storeWith(t)
	m := &tgapi.Message{
		ID: 1, Message: "post", Date: 1_800_000_000,
		PeerID: &tgapi.PeerChannel{ChannelID: 77}, // FromID nil: channel post
	}
	got, ok := mapMessage(s, 0, m)
	if !ok {
		t.Fatal("mapMessage returned !ok")
	}
	if got.Sender != "News Channel" || got.SenderID != 77 {
		t.Fatalf("sender = %q/%d, want News Channel/77", got.Sender, got.SenderID)
	}
	if got.Date != time.Unix(1_800_000_000, 0) {
		t.Fatalf("date = %v", got.Date)
	}
}

func TestMapMessageUserSender(t *testing.T) {
	s := storeWith(t)
	m := &tgapi.Message{
		ID: 2, Message: "hi", Date: 1_800_000_000,
		PeerID: &tgapi.PeerChat{ChatID: 42},
		FromID: &tgapi.PeerUser{UserID: 5},
	}
	got, _ := mapMessage(s, 0, m)
	if got.Sender != "Alice" || got.SenderID != 5 {
		t.Fatalf("sender = %q/%d, want Alice/5", got.Sender, got.SenderID)
	}
}

func TestPreviewTextFlattensNewlines(t *testing.T) {
	m := &tgapi.Message{Message: "line one\nline two\n\nline three"}
	if got := previewText(m); got != "line one line two line three" {
		t.Fatalf("preview = %q", got)
	}
}

func TestMapDialogsMembersAndLastDate(t *testing.T) {
	s := storeWith(t)
	ds := mapDialogs(s,
		[]tgapi.DialogClass{&tgapi.Dialog{
			Peer: &tgapi.PeerChannel{ChannelID: 77}, TopMessage: 9, UnreadCount: 3,
		}},
		[]tgapi.MessageClass{&tgapi.Message{
			ID: 9, Message: "last", Date: 1_800_000_000,
			PeerID: &tgapi.PeerChannel{ChannelID: 77},
		}},
		nil, nil,
	)
	if len(ds) != 1 {
		t.Fatalf("dialogs = %d", len(ds))
	}
	d := ds[0]
	if d.Members != 6660 {
		t.Fatalf("members = %d, want 6660", d.Members)
	}
	if d.LastDate != time.Unix(1_800_000_000, 0) {
		t.Fatalf("lastDate = %v", d.LastDate)
	}
}

func TestPeerStoreMembers(t *testing.T) {
	s := storeWith(t)
	if got := s.Members(model.PeerChat, 42); got != 12 {
		t.Fatalf("chat members = %d, want 12", got)
	}
	if got := s.Members(model.PeerUser, 5); got != 0 {
		t.Fatalf("user members = %d, want 0", got)
	}
}
