// messages_test.go
package tg

import (
	"testing"

	tgapi "github.com/gotd/td/tg"

	"github.com/diamondburned/gotk4/examples/telegram/internal/config"
	"github.com/diamondburned/gotk4/examples/telegram/internal/model"
)

func TestMapMessage(t *testing.T) {
	s := newPeerStore()
	s.Add([]tgapi.UserClass{&tgapi.User{ID: 7, FirstName: "Bea"}}, nil)

	plain := &tgapi.Message{ID: 1, Message: "yo", FromID: &tgapi.PeerUser{UserID: 7}, PeerID: &tgapi.PeerUser{UserID: 7}}
	m, ok := mapMessage(s, 99, plain)
	if !ok || m.Text != "yo" || m.Sender != "Bea" || m.Out || m.Kind != model.PeerUser {
		t.Fatalf("plain = %+v ok=%v", m, ok)
	}

	photo := &tgapi.Message{ID: 2, PeerID: &tgapi.PeerUser{UserID: 7},
		Media: &tgapi.MessageMediaPhoto{Photo: &tgapi.Photo{ID: 555, AccessHash: 556, FileReference: []byte{1}}}}
	pm, ok := mapMessage(s, 99, photo)
	if !ok || pm.Text != "[photo]" || pm.Photo == nil || pm.Photo.PhotoID != 555 || pm.Photo.MsgID != 2 || pm.Photo.PeerID != 7 {
		t.Fatalf("photo = %+v", pm)
	}

	own := &tgapi.Message{ID: 3, Message: "mine", Out: true, PeerID: &tgapi.PeerUser{UserID: 7}}
	if om, _ := mapMessage(s, 99, own); !om.Out {
		t.Fatal("own message must be Out")
	}
}

func TestChatStatesNote(t *testing.T) {
	cs := newChatStates()
	if !cs.Note(1, 100) {
		t.Fatal("first sighting true")
	}
	if cs.Note(1, 100) {
		t.Fatal("second sighting false")
	}
	if !cs.Note(2, 100) {
		t.Fatal("chats independent")
	}
}

func TestConsumeSentShortUpdate(t *testing.T) {
	c := NewClient(config.Config{}, t.TempDir())
	c.consumeSent(model.PeerUser, 7, "hello",
		&tgapi.UpdateShortSentMessage{ID: 900})

	select {
	case ev := <-c.Events:
		if ev.Kind != model.EventNewMessage || !ev.Msg.Out || ev.Msg.Text != "hello" || ev.Msg.ID != 900 || ev.Msg.PeerID != 7 {
			t.Fatalf("ev = %+v", ev)
		}
	default:
		t.Fatal("no event emitted")
	}
	// the seen-set now contains 900: a later push of the same message dedups
	if c.chats.Note(7, 900) {
		t.Fatal("id 900 must already be seen")
	}
}
