// messages.go
package tg

import (
	"context"
	"errors"
	"sync"
	"time"

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
	out := model.Message{
		ID: m.ID, PeerID: peerID, Kind: kind,
		Text: m.Message, Out: m.Out,
		Date: time.Unix(int64(m.Date), 0),
	}
	switch from := m.FromID.(type) {
	case *tgapi.PeerUser:
		out.Sender = s.Title(model.PeerUser, from.UserID)
		out.SenderID = from.UserID
		if from.UserID == selfID {
			out.Out = true
		}
	case *tgapi.PeerChannel: // anonymous admin / linked channel
		out.Sender = s.Title(model.PeerChannel, from.ChannelID)
		out.SenderID = from.ChannelID
	default: // channel post with no FromID: attribute to the channel itself
		if kind == model.PeerChannel {
			out.Sender = s.Title(kind, peerID)
			out.SenderID = peerID
		}
	}
	if m.Media != nil {
		out.Text = mediaLabel(m.Media)
		if media, ok := m.Media.(*tgapi.MessageMediaPhoto); ok {
			if p, ok := media.Photo.(*tgapi.Photo); ok {
				ref := &model.PhotoRef{
					PhotoID: p.ID, AccessHash: p.AccessHash,
					FileRef: p.FileReference, MsgID: m.ID,
					PeerID: peerID, Kind: kind,
				}
				ref.SizeType, _ = pickSize(p.Sizes)
				out.Photo = ref
			}
		}
	}
	return out, true
}

// ---- dedup ----

type seenKey struct {
	kind model.PeerKind
	id   int64
}

type chatStates struct {
	mu   sync.Mutex
	seen map[seenKey]map[int]struct{} // (kind, peerID) → msg IDs
}

func newChatStates() *chatStates {
	return &chatStates{seen: map[seenKey]map[int]struct{}{}}
}

// Note records a message ID; returns true only on first sighting. Protects
// against history-load vs live-update races and send-echo double delivery.
// Keyed by (kind, peerID): user/chat/channel IDs are separate Telegram
// namespaces and can collide numerically.
func (cs *chatStates) Note(kind model.PeerKind, peerID int64, msgID int) bool {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	key := seenKey{kind, peerID}
	if cs.seen[key] == nil {
		cs.seen[key] = map[int]struct{}{}
	}
	if _, dup := cs.seen[key][msgID]; dup {
		return false
	}
	cs.seen[key][msgID] = struct{}{}
	return true
}

// ---- shared ingestion: history, live updates, and send echoes all land here ----

func (c *Client) handleNewMessage(mc tgapi.MessageClass) {
	m, ok := mapMessage(c.peers, c.selfID(), mc)
	if !ok {
		return
	}
	if !c.chats.Note(m.Kind, m.PeerID, m.ID) {
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
	selfID := c.selfID()
	out := make([]model.Message, 0, len(raw))
	for i := len(raw) - 1; i >= 0; i-- { // newest-first → oldest-first
		if m, ok := mapMessage(c.peers, selfID, raw[i]); ok {
			c.chats.Note(kind, peerID, m.ID)
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
		if !c.chats.Note(kind, peerID, u.ID) {
			return
		}
		c.emit(model.Event{Kind: model.EventNewMessage, Msg: model.Message{
			ID: u.ID, PeerID: peerID, Kind: kind, Sender: "me", SenderID: c.selfID(),
			Text: text, Out: true, Date: time.Unix(int64(u.Date), 0),
		}})
	case *tgapi.Updates:
		c.drainNewMessages(u.Updates)
	case *tgapi.UpdatesCombined:
		c.drainNewMessages(u.Updates)
	}
}

// drainNewMessages feeds new-message updates from an RPC response batch
// through the shared ingestion path.
func (c *Client) drainNewMessages(updates []tgapi.UpdateClass) {
	for _, uc := range updates {
		switch v := uc.(type) {
		case *tgapi.UpdateNewMessage:
			c.handleNewMessage(v.Message)
		case *tgapi.UpdateNewChannelMessage:
			c.handleNewMessage(v.Message)
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
