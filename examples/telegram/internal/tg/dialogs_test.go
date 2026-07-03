// dialogs_test.go
package tg

import (
	"testing"

	tgapi "github.com/gotd/td/tg"

	"github.com/diamondburned/gotk4/examples/telegram/internal/model"
)

func TestMapDialogs(t *testing.T) {
	s := newPeerStore()
	users := []tgapi.UserClass{&tgapi.User{ID: 1, AccessHash: 11, FirstName: "Ann"}}
	chats := []tgapi.ChatClass{&tgapi.Channel{ID: 3, AccessHash: 33, Title: "News", Broadcast: true}}
	dialogs := []tgapi.DialogClass{
		&tgapi.Dialog{Peer: &tgapi.PeerUser{UserID: 1}, TopMessage: 100, UnreadCount: 2},
		&tgapi.Dialog{Peer: &tgapi.PeerChannel{ChannelID: 3}, TopMessage: 200, UnreadCount: 0},
	}
	msgs := []tgapi.MessageClass{
		&tgapi.Message{ID: 100, Message: "hi there", PeerID: &tgapi.PeerUser{UserID: 1}},
		&tgapi.Message{ID: 200, Media: &tgapi.MessageMediaPhoto{}, PeerID: &tgapi.PeerChannel{ChannelID: 3}},
	}

	got := mapDialogs(s, dialogs, msgs, users, chats)
	if len(got) != 2 {
		t.Fatalf("len = %d", len(got))
	}
	d0 := got[0]
	if d0.PeerID != 1 || d0.Kind != model.PeerUser || d0.Title != "Ann" || d0.Preview != "hi there" || d0.Unread != 2 || !d0.CanSend {
		t.Errorf("d0 = %+v", d0)
	}
	d1 := got[1]
	if d1.Kind != model.PeerChannel || d1.Preview != "[photo]" || d1.CanSend {
		t.Errorf("d1 = %+v", d1)
	}
}
