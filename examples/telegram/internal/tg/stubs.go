// stubs.go — each stub is DELETED by the task named next to it.
package tg

import (
	"context"

	tgapi "github.com/gotd/td/tg"
)

func (c *Client) bindUpdates(tgapi.UpdateDispatcher) {} // Task 8 (VALUE param)

func (c *Client) Logout(ctx context.Context) error { return nil } // Task 8
