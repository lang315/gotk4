// updates.go
package tg

import (
	"context"
	"os"

	tgapi "github.com/gotd/td/tg"
)

// bindUpdates takes the dispatcher BY VALUE: tgapi.NewUpdateDispatcher()
// returns a value whose copies share the internal handlers map (v0.159.0).
func (c *Client) bindUpdates(d tgapi.UpdateDispatcher) {
	d.OnNewMessage(func(ctx context.Context, e tgapi.Entities, u *tgapi.UpdateNewMessage) error {
		c.addEntities(e)
		c.handleNewMessage(u.Message)
		return nil
	})
	d.OnNewChannelMessage(func(ctx context.Context, e tgapi.Entities, u *tgapi.UpdateNewChannelMessage) error {
		c.addEntities(e)
		c.handleNewMessage(u.Message)
		return nil
	})
}

// addEntities feeds update-borne users/chats into the peer store.
// tgapi.Entities holds maps (verified v0.159.0).
func (c *Client) addEntities(e tgapi.Entities) {
	users := make([]tgapi.UserClass, 0, len(e.Users))
	for _, u := range e.Users {
		users = append(users, u)
	}
	chats := make([]tgapi.ChatClass, 0, len(e.Chats)+len(e.Channels))
	for _, ch := range e.Chats {
		chats = append(chats, ch)
	}
	for _, ch := range e.Channels {
		chats = append(chats, ch)
	}
	c.peers.Add(users, chats)
}

// Logout best-effort revokes server-side, then deletes the session file.
// Caller cancels the run ctx and calls start() again.
func (c *Client) Logout(ctx context.Context) error {
	if api := c.api(); api != nil {
		_, _ = api.AuthLogOut(ctx) // correct call — auth.Client has no Logout method
	}
	return os.Remove(c.SessionPath())
}
