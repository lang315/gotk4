// peers.go
package tg

import (
	"strings"
	"sync"

	tgapi "github.com/gotd/td/tg"

	"github.com/diamondburned/gotk4/examples/telegram/internal/model"
)

type peerInfo struct {
	input   tgapi.InputPeerClass
	title   string
	canSend bool
	members int
}

type peerStore struct {
	mu sync.RWMutex
	m  map[model.PeerKind]map[int64]peerInfo
}

func newPeerStore() *peerStore {
	return &peerStore{m: map[model.PeerKind]map[int64]peerInfo{
		model.PeerUser: {}, model.PeerChat: {}, model.PeerChannel: {},
	}}
}

func (s *peerStore) Add(users []tgapi.UserClass, chats []tgapi.ChatClass) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, uc := range users {
		u, ok := uc.(*tgapi.User)
		if !ok {
			continue
		}
		s.m[model.PeerUser][u.ID] = peerInfo{
			input:   &tgapi.InputPeerUser{UserID: u.ID, AccessHash: u.AccessHash},
			title:   strings.TrimSpace(u.FirstName + " " + u.LastName),
			canSend: true,
		}
	}
	for _, cc := range chats {
		switch ch := cc.(type) {
		case *tgapi.Chat:
			s.m[model.PeerChat][ch.ID] = peerInfo{
				input:   &tgapi.InputPeerChat{ChatID: ch.ID},
				title:   ch.Title,
				canSend: true,
				members: ch.ParticipantsCount,
			}
		case *tgapi.Channel:
			members := 0
			if n, ok := ch.GetParticipantsCount(); ok {
				members = n
			}
			s.m[model.PeerChannel][ch.ID] = peerInfo{
				input:   &tgapi.InputPeerChannel{ChannelID: ch.ID, AccessHash: ch.AccessHash},
				title:   ch.Title,
				canSend: !ch.Broadcast,
				members: members,
			}
		}
	}
}

func (s *peerStore) get(kind model.PeerKind, id int64) (peerInfo, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.m[kind][id]
	return p, ok
}

func (s *peerStore) InputPeer(kind model.PeerKind, id int64) (tgapi.InputPeerClass, bool) {
	p, ok := s.get(kind, id)
	return p.input, ok
}

func (s *peerStore) InputChannel(id int64) (*tgapi.InputChannel, bool) {
	p, ok := s.get(model.PeerChannel, id)
	if !ok {
		return nil, false
	}
	ipc, ok := p.input.(*tgapi.InputPeerChannel)
	if !ok {
		return nil, false
	}
	return &tgapi.InputChannel{ChannelID: ipc.ChannelID, AccessHash: ipc.AccessHash}, true
}

func (s *peerStore) Title(kind model.PeerKind, id int64) string {
	p, _ := s.get(kind, id)
	return p.title
}

func (s *peerStore) CanSend(kind model.PeerKind, id int64) bool {
	p, ok := s.get(kind, id)
	return ok && p.canSend
}

// Members returns the participant count for a peer, 0 if unknown.
func (s *peerStore) Members(kind model.PeerKind, id int64) int {
	p, _ := s.get(kind, id)
	return p.members
}
