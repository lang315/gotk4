// messages.go
package tg

import (
	"context"
	"errors"
	"sync"

	tgapi "github.com/gotd/td/tg"

	"github.com/diamondburned/gotk4/examples/telegram/internal/model"
)

var errUnknownPeer = errors.New("unknown peer")

// ---- pure mapping ----

func mapMessage(s *peerStore, selfID int64, mc tgapi.MessageClass) (model.Message, bool) {
	m, ok := mc.(*tgapi.Message)
	if !ok {
		return model.Message{}, false
	}
	kind, peerID := peerKey(m.PeerID)
	out := model.Message{ID: m.ID, PeerID: peerID, Kind: kind, Text: m.Message, Out: m.Out}
	if m.FromID != nil {
		if fu, ok := m.FromID.(*tgapi.PeerUser); ok {
			out.Sender = s.Title(model.PeerUser, fu.UserID)
			if fu.UserID == selfID {
				out.Out = true
			}
		}
	}
	if m.Media != nil {
		switch media := m.Media.(type) {
		case *tgapi.MessageMediaPhoto:
			out.Text = "[photo]"
			if p, ok := media.Photo.(*tgapi.Photo); ok {
				ref := &model.PhotoRef{
					PhotoID: p.ID, AccessHash: p.AccessHash,
					FileRef: p.FileReference, MsgID: m.ID,
					PeerID: peerID, Kind: kind,
				}
				// SizeType filled by pickSize once Task 10 lands; until then
				// the empty SizeType means "no renderable size" downstream.
				ref.SizeType, _ = pickSizeOrEmpty(p.Sizes)
				out.Photo = ref
			}
		case *tgapi.MessageMediaDocument:
			out.Text = "[file]"
		default:
			out.Text = "[media]"
		}
	}
	return out, true
}

// pickSizeOrEmpty is a seam: Task 10 replaces the body with pickSize.
func pickSizeOrEmpty(sizes []tgapi.PhotoSizeClass) (string, bool) { return "", false }

// ---- dedup ----

type chatStates struct {
	mu   sync.Mutex
	seen map[int64]map[int]struct{} // peerID → msg IDs
}

func newChatStates() *chatStates {
	return &chatStates{seen: map[int64]map[int]struct{}{}}
}

// Note records a message ID; returns true only on first sighting. Protects
// against history-load vs live-update races and send-echo double delivery.
func (cs *chatStates) Note(peerID int64, msgID int) bool {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if cs.seen[peerID] == nil {
		cs.seen[peerID] = map[int]struct{}{}
	}
	if _, dup := cs.seen[peerID][msgID]; dup {
		return false
	}
	cs.seen[peerID][msgID] = struct{}{}
	return true
}

// ---- shared ingestion: history, live updates, and send echoes all land here ----

func (c *Client) handleNewMessage(mc tgapi.MessageClass) {
	selfID := int64(0)
	if me := c.self(); me != nil {
		selfID = me.ID
	}
	m, ok := mapMessage(c.peers, selfID, mc)
	if !ok {
		return
	}
	if !c.chats.Note(m.PeerID, m.ID) {
		return
	}
	c.emit(model.Event{Kind: model.EventNewMessage, Msg: m})
}

// ---- RPC ----

func (c *Client) History(ctx context.Context, kind model.PeerKind, peerID int64) ([]model.Message, error) {
	peer, ok := c.peers.InputPeer(kind, peerID)
	if !ok {
		return nil, errUnknownPeer
	}
	res, err := c.api().MessagesGetHistory(ctx, &tgapi.MessagesGetHistoryRequest{Peer: peer, Limit: 50})
	if err != nil {
		return nil, err
	}
	var raw []tgapi.MessageClass
	switch v := res.(type) {
	case *tgapi.MessagesMessages:
		c.peers.Add(v.Users, v.Chats)
		raw = v.Messages
	case *tgapi.MessagesMessagesSlice:
		c.peers.Add(v.Users, v.Chats)
		raw = v.Messages
	case *tgapi.MessagesChannelMessages:
		c.peers.Add(v.Users, v.Chats)
		raw = v.Messages
	}
	selfID := int64(0)
	if me := c.self(); me != nil {
		selfID = me.ID
	}
	out := make([]model.Message, 0, len(raw))
	for i := len(raw) - 1; i >= 0; i-- { // newest-first → oldest-first
		if m, ok := mapMessage(c.peers, selfID, raw[i]); ok {
			c.chats.Note(peerID, m.ID)
			out = append(out, m)
		}
	}
	return out, nil
}

// Send uses the idiomatic Sender, then routes the RPC response updates
// through the same ingestion path so the sent message appears exactly once.
func (c *Client) Send(ctx context.Context, kind model.PeerKind, peerID int64, text string) error {
	peer, ok := c.peers.InputPeer(kind, peerID)
	if !ok {
		return errUnknownPeer
	}
	upd, err := c.sender().To(peer).Text(ctx, text)
	if err != nil {
		return err
	}
	c.consumeSent(kind, peerID, text, upd)
	return nil
}

// consumeSent extracts the sent message from a sendMessage RPC response.
// Telegram may answer with UpdateShortSentMessage (ID only — synthesize the
// row from what we sent) or a full Updates batch (contains the message).
func (c *Client) consumeSent(kind model.PeerKind, peerID int64, text string, upd tgapi.UpdatesClass) {
	switch u := upd.(type) {
	case *tgapi.UpdateShortSentMessage:
		if !c.chats.Note(peerID, u.ID) {
			return
		}
		c.emit(model.Event{Kind: model.EventNewMessage, Msg: model.Message{
			ID: u.ID, PeerID: peerID, Kind: kind, Sender: "me", Text: text, Out: true,
		}})
	case *tgapi.Updates:
		for _, uc := range u.Updates {
			switch v := uc.(type) {
			case *tgapi.UpdateNewMessage:
				c.handleNewMessage(v.Message)
			case *tgapi.UpdateNewChannelMessage:
				c.handleNewMessage(v.Message)
			}
		}
	case *tgapi.UpdatesCombined:
		for _, uc := range u.Updates {
			switch v := uc.(type) {
			case *tgapi.UpdateNewMessage:
				c.handleNewMessage(v.Message)
			case *tgapi.UpdateNewChannelMessage:
				c.handleNewMessage(v.Message)
			}
		}
	}
}

func (c *Client) MarkRead(ctx context.Context, kind model.PeerKind, peerID int64) error {
	peer, ok := c.peers.InputPeer(kind, peerID)
	if !ok {
		return errUnknownPeer
	}
	if kind == model.PeerChannel {
		ch, ok := c.peers.InputChannel(peerID)
		if !ok {
			return errUnknownPeer
		}
		_, err := c.api().ChannelsReadHistory(ctx, &tgapi.ChannelsReadHistoryRequest{Channel: ch})
		return err
	}
	_, err := c.api().MessagesReadHistory(ctx, &tgapi.MessagesReadHistoryRequest{Peer: peer})
	return err
}
