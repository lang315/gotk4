// client.go
package tg

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gotd/contrib/middleware/floodwait"
	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/message"
	tgapi "github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"

	"github.com/diamondburned/gotk4/examples/telegram/internal/config"
	"github.com/diamondburned/gotk4/examples/telegram/internal/model"
)

type Client struct {
	Auth   *UIAuth
	Events chan model.Event

	cfg     config.Config
	dataDir string

	mu     sync.Mutex
	rawAPI *tgapi.Client
	snd    *message.Sender
	me     *tgapi.User

	peers *peerStore
	chats *chatStates
	ready atomic.Bool
}

func NewClient(cfg config.Config, dataDir string) *Client {
	return &Client{
		Auth:    NewUIAuth(),
		Events:  make(chan model.Event, 64),
		cfg:     cfg,
		dataDir: dataDir,
		peers:   newPeerStore(),
		chats:   newChatStates(),
	}
}

func (c *Client) SessionPath() string { return filepath.Join(c.dataDir, "session.json") }
func (c *Client) Ready() bool         { return c.ready.Load() }

func (c *Client) api() *tgapi.Client      { c.mu.Lock(); defer c.mu.Unlock(); return c.rawAPI }
func (c *Client) sender() *message.Sender { c.mu.Lock(); defer c.mu.Unlock(); return c.snd }
func (c *Client) self() *tgapi.User       { c.mu.Lock(); defer c.mu.Unlock(); return c.me }

func (c *Client) SelfName() string {
	me := c.self()
	if me == nil {
		return ""
	}
	if me.Username != "" {
		return me.Username
	}
	return me.FirstName
}

// Run blocks; owns the gotd lifetime. Re-enters telegram.Client.Run with
// capped backoff until ctx cancels. AUTH_KEY_UNREGISTERED → session file
// deleted → EventAuthNeeded → next entry runs the login flow.
func (c *Client) Run(ctx context.Context) error {
	backoff := time.Duration(0)
	for {
		err := c.runOnce(ctx)
		c.ready.Store(false)
		c.mu.Lock()
		c.rawAPI, c.snd, c.me = nil, nil, nil
		c.mu.Unlock()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil && ctx.Err() == nil {
			// gotd error strings carry no credentials; never log user input here.
			log.Printf("tg: session ended, reconnecting: %v", err)
		}
		if tgerr.Is(err, "AUTH_KEY_UNREGISTERED") {
			_ = os.Remove(c.SessionPath())
			c.emit(model.Event{Kind: model.EventAuthNeeded})
		}
		backoff = NextBackoff(backoff)
		c.emit(model.Event{Kind: model.EventConnecting})
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// emit never blocks gotd goroutines on a full UI queue.
func (c *Client) emit(ev model.Event) {
	select {
	case c.Events <- ev:
	default:
	}
}

func (c *Client) runOnce(ctx context.Context) error {
	if err := os.MkdirAll(c.dataDir, 0o700); err != nil {
		return err
	}

	// VALUE, not pointer: NewUpdateDispatcher returns UpdateDispatcher whose
	// value copies share the internal handlers map (verified v0.159.0).
	dispatcher := tgapi.NewUpdateDispatcher()

	client := telegram.NewClient(c.cfg.APIID, c.cfg.APIHash, telegram.Options{
		SessionStorage: &session.FileStorage{Path: c.SessionPath()},
		UpdateHandler:  dispatcher, // bare dispatcher; gap recovery is out of scope v1
		Middlewares:    []telegram.Middleware{floodwait.NewSimpleWaiter()},
	})

	return client.Run(ctx, func(ctx context.Context) error {
		status, err := client.Auth().Status(ctx)
		if err != nil {
			return err
		}
		if !status.Authorized {
			c.emit(model.Event{Kind: model.EventAuthNeeded})
			if err := c.loginFlow(ctx, client.Auth()); err != nil {
				return err
			}
		}
		// crown-jewel hardening: gotd owns the write; we own the mode.
		_ = os.Chmod(c.SessionPath(), 0o600)

		me, err := client.Self(ctx)
		if err != nil {
			return err
		}

		c.mu.Lock()
		c.rawAPI = tgapi.NewClient(client)
		c.snd = message.NewSender(c.rawAPI)
		c.me = me
		c.mu.Unlock()

		c.bindUpdates(dispatcher)

		c.ready.Store(true)
		c.emit(model.Event{Kind: model.EventReady})
		<-ctx.Done()
		return ctx.Err()
	})
}
