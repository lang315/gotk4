// updates_test.go
package tg

import (
	"testing"

	tgapi "github.com/gotd/td/tg"

	"github.com/diamondburned/gotk4/examples/telegram/internal/config"
	"github.com/diamondburned/gotk4/examples/telegram/internal/model"
)

func TestHandleNewMessageDedups(t *testing.T) {
	c := NewClient(config.Config{}, t.TempDir())
	c.peers.Add([]tgapi.UserClass{&tgapi.User{ID: 7, FirstName: "Bea"}}, nil)

	msg := &tgapi.Message{ID: 42, Message: "hey", PeerID: &tgapi.PeerUser{UserID: 7}}
	c.handleNewMessage(msg)
	c.handleNewMessage(msg)

	select {
	case ev := <-c.Events:
		if ev.Kind != model.EventNewMessage || ev.Msg.Text != "hey" {
			t.Fatalf("ev = %+v", ev)
		}
	default:
		t.Fatal("no event emitted")
	}
	select {
	case ev := <-c.Events:
		t.Fatalf("duplicate emitted: %+v", ev)
	default:
	}
}
