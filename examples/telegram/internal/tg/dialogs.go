// dialogs.go
package tg

import (
	"context"
	"strings"
	"time"

	tgapi "github.com/gotd/td/tg"

	"github.com/diamondburned/gotk4/examples/telegram/internal/model"
)

func peerKey(p tgapi.PeerClass) (model.PeerKind, int64) {
	switch v := p.(type) {
	case *tgapi.PeerUser:
		return model.PeerUser, v.UserID
	case *tgapi.PeerChat:
		return model.PeerChat, v.ChatID
	case *tgapi.PeerChannel:
		return model.PeerChannel, v.ChannelID
	}
	return model.PeerUser, 0
}

// mediaLabel is the single source of the placeholder text shown for media,
// used by both dialog previews and message bodies.
func mediaLabel(m tgapi.MessageMediaClass) string {
	switch m.(type) {
	case *tgapi.MessageMediaPhoto:
		return "[photo]"
	case *tgapi.MessageMediaDocument:
		return "[file]"
	default:
		return "[media]"
	}
}

func previewText(m tgapi.MessageClass) string {
	msg, ok := m.(*tgapi.Message)
	if !ok {
		return ""
	}
	if msg.Media != nil {
		return mediaLabel(msg.Media)
	}
	return strings.Join(strings.Fields(msg.Message), " ")
}

func mapDialogs(s *peerStore, dialogs []tgapi.DialogClass, msgs []tgapi.MessageClass, users []tgapi.UserClass, chats []tgapi.ChatClass) []model.Dialog {
	s.Add(users, chats)
	top := make(map[int]*tgapi.Message, len(msgs))
	for _, m := range msgs {
		if mm, ok := m.(*tgapi.Message); ok {
			top[mm.ID] = mm
		}
	}
	out := make([]model.Dialog, 0, len(dialogs))
	for _, dc := range dialogs {
		d, ok := dc.(*tgapi.Dialog)
		if !ok {
			continue
		}
		kind, id := peerKey(d.Peer)
		md := model.Dialog{
			PeerID:  id,
			Kind:    kind,
			Title:   s.Title(kind, id),
			Unread:  d.UnreadCount,
			CanSend: s.CanSend(kind, id),
			Members: s.Members(kind, id),
		}
		if tm, ok := top[d.TopMessage]; ok {
			md.Preview = previewText(tm)
			md.LastDate = time.Unix(int64(tm.Date), 0)
		}
		out = append(out, md)
	}
	return out
}

// Dialogs fetches the top 50 dialogs and feeds the peer store.
func (c *Client) Dialogs(ctx context.Context) ([]model.Dialog, error) {
	res, err := c.api().MessagesGetDialogs(ctx, &tgapi.MessagesGetDialogsRequest{
		Limit:      50,
		OffsetPeer: &tgapi.InputPeerEmpty{},
	})
	if err != nil {
		return nil, err
	}
	switch v := res.(type) {
	case *tgapi.MessagesDialogs:
		return mapDialogs(c.peers, v.Dialogs, v.Messages, v.Users, v.Chats), nil
	case *tgapi.MessagesDialogsSlice:
		return mapDialogs(c.peers, v.Dialogs, v.Messages, v.Users, v.Chats), nil
	}
	return nil, nil
}
