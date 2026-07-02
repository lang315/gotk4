// dialogs.go
package tg

import (
	"context"

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

func previewText(m tgapi.MessageClass) string {
	msg, ok := m.(*tgapi.Message)
	if !ok {
		return ""
	}
	if msg.Media != nil {
		switch msg.Media.(type) {
		case *tgapi.MessageMediaPhoto:
			return "[photo]"
		case *tgapi.MessageMediaDocument:
			return "[file]"
		default:
			return "[media]"
		}
	}
	return msg.Message
}

func mapDialogs(s *peerStore, dialogs []tgapi.DialogClass, msgs []tgapi.MessageClass, users []tgapi.UserClass, chats []tgapi.ChatClass) []model.Dialog {
	s.Add(users, chats)
	top := make(map[int]tgapi.MessageClass, len(msgs))
	for _, m := range msgs {
		if mm, ok := m.(*tgapi.Message); ok {
			top[mm.ID] = m
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
		}
		if tm, ok := top[d.TopMessage]; ok {
			md.Preview = previewText(tm)
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
