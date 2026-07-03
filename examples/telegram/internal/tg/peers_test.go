// peers_test.go
package tg

import (
	"testing"

	tgapi "github.com/gotd/td/tg"

	"github.com/diamondburned/gotk4/examples/telegram/internal/model"
)

func TestPeerStore(t *testing.T) {
	s := newPeerStore()
	s.Add(
		[]tgapi.UserClass{&tgapi.User{ID: 1, AccessHash: 11, FirstName: "Ann", LastName: "B"}},
		[]tgapi.ChatClass{
			&tgapi.Chat{ID: 2, Title: "Group"},
			&tgapi.Channel{ID: 3, AccessHash: 33, Title: "Chan", Broadcast: true},
			&tgapi.Channel{ID: 4, AccessHash: 44, Title: "Super", Megagroup: true},
		},
	)

	p, ok := s.InputPeer(model.PeerUser, 1)
	if !ok {
		t.Fatal("user missing")
	}
	if u := p.(*tgapi.InputPeerUser); u.UserID != 1 || u.AccessHash != 11 {
		t.Fatalf("bad user peer %+v", u)
	}
	if s.Title(model.PeerUser, 1) != "Ann B" {
		t.Errorf("title = %q", s.Title(model.PeerUser, 1))
	}
	if _, ok := s.InputPeer(model.PeerChat, 2); !ok {
		t.Error("chat missing")
	}
	if s.CanSend(model.PeerChannel, 3) {
		t.Error("broadcast channel must not be sendable")
	}
	if !s.CanSend(model.PeerChannel, 4) {
		t.Error("megagroup must be sendable")
	}
	ic, ok := s.InputChannel(3)
	if !ok || ic.ChannelID != 3 || ic.AccessHash != 33 {
		t.Errorf("InputChannel = %+v ok=%v", ic, ok)
	}
	if _, ok := s.InputPeer(model.PeerUser, 999); ok {
		t.Error("unknown peer must be !ok")
	}
}
