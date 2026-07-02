# Telegram client on gotk4 — Implementation Plan (v2)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

v2 after a 4-agent adversarial review of v1 (gotd API verification against
v0.159.0 sources, gotk4/gota verification against the local tree with
file:line citations, executability review, devil's-advocate). Key changes vs
v1: optimistic-send/random_id machinery cut (was dead code), send now uses
`message.NewSender` + consuming the RPC response updates; `updates.Manager`
replaced by a bare `UpdateDispatcher` (gap recovery → v2); gota AsyncImage
extracted-to-pkg cut (inline loader in the app; upstream after v1); a
walking-skeleton spike added before any UI; all gota fluent calls rewritten
against the REAL gota API; `bindUpdates` takes a dispatcher VALUE;
channel-photo file-reference refresh uses `ChannelsGetMessages`.

**Goal:** Build `examples/telegram`, a real Telegram client (gotd MTProto + gotk4 GTK4 UI) proving the bindings end-to-end, per spec `docs/superpowers/specs/2026-07-02-telegram-gotk4-design.md` (as amended alongside this plan).

**Architecture:** Direct gotd + thin event glue. A background goroutine holds `client.Run`; all RPC happens inside its callback lifetime; every UI mutation goes through `coreglib.IdleAdd`. Views live in one `gtk.Stack`. List panes are `gtk.ListView` backed by `gioutil.NewListModel[T]`.

**Tech Stack:** Go 1.26, `github.com/diamondburned/gotk4/pkg` (local replace), `github.com/gotd/td` v0.159.x, `github.com/gotd/contrib` (floodwait), `pkg/core/gioutil`, `pkg/gota`.

## Global Constraints

- App module: `github.com/diamondburned/gotk4/examples/telegram`; `go 1.26`; `replace github.com/diamondburned/gotk4/pkg => ../../pkg`.
- Config dir `os.UserConfigDir()/gotk4-telegram` 0700; config.json AND session.json 0600 (session chmod'd in code after auth — crown-jewel secret). Cache dir `os.UserCacheDir()/gotk4-telegram` 0700.
- Never log phone/code/password; error UI never echoes them; 2FA never persisted; gotd logger off.
- All message/preview labels: plain text, never Pango markup.
- Photo cache filenames locally generated (`<photoID hex>_<size letter>.jpg`); path containment asserted; 10 MB cap.
- gotd calls only in goroutines; UI mutation only via `coreglib.IdleAdd`.
- `runtime.LockOSThread()` first line of every `main()` (GTK must own the main OS thread).
- Out of scope v1: history pagination, multiline composer, posting to broadcast channels, update-gap recovery across offline periods, persisted peer state, sign-up, keychain, vendoring deps (explicitly rejected — CI fetches modules over network like the existing jobs do).
- macOS: prefix EVERY `go build/test/run` with `export PKG_CONFIG_PATH="$(brew --prefix)/lib/pkgconfig:$(brew --prefix)/share/pkgconfig:$PKG_CONFIG_PATH"` (restated in Task 1; implied everywhere after).
- Import aliases: our wrapper = `internal/tg`; gotd schema = `tgapi "github.com/gotd/td/tg"`; core glib = `coreglib "github.com/diamondburned/gotk4/pkg/core/glib"`.
- gota REAL API (verified pkg/gota/widgets.go, widget.go, gota.go — v1's fluent DSL was wrong): `VBox(spacing int) *BoxWidget` + `.Append(...IWidget)`; `.ToWidget() *gtk.Widget` (NOT `.Widget()`); `Entry().SetPlaceholder(s)`, `.GetText()`, `.SetText(s)`, `.OnEnter(func(*gota.EntryWidget))`, `.PasswordMode(bool)`; `Button(label).OnClick(func(*gota.ButtonWidget))`; `Label(s)` with `.SetText/.GetText` only (no Wrap); base chainers: `Class/Tooltip/Name/Visible/Size/HExpand/VExpand/HAlign/VAlign` (no Spacing/Margin/Expand). gota containers accept ONLY `gota.IWidget` — raw gtk widgets go in raw `gtk.NewBox`/`gtk.NewScrolledWindow` layouts instead. Margins: set on `.ToWidget()` via `SetMarginTop/Bottom/Start/End`.
- Branch: `feat/telegram-app` for Tasks 1–11. Task 12 on `fix/girdirs-skip-macos` (separate PR).
- E2E evidence screenshots are from macOS/Quartz — say so in the PR; Linux rendering is covered by the CI compile only.
- Docs when a signature fights back: pkg.go.dev/github.com/gotd/td/... — but note the 4-agent review already compile-verified every gotd call herein against v0.159.0; local bindings cites are from this repo's source.

## File Structure

```
examples/telegram/
  go.mod, go.sum
  main.go                      # gtk.Application + restartable start()
  cmd/spike/main.go            # Task 4 walking skeleton — DELETED in Task 6
  internal/config/config.go, config_test.go
  internal/model/model.go      # pure: Dialog, Message, PhotoRef, PromptKind, Event
  internal/tg/auth.go, auth_test.go
  internal/tg/backoff.go, backoff_test.go
  internal/tg/client.go        # lifecycle; dispatcher VALUE wiring
  internal/tg/peers.go, peers_test.go
  internal/tg/dialogs.go, dialogs_test.go
  internal/tg/messages.go, messages_test.go   # history, Sender send, consumeSent, dedup
  internal/tg/updates.go, updates_test.go     # handleNewMessage, bindUpdates, Logout
  internal/tg/photos.go, photos_test.go       # pickSize, cachePath, download, channel-aware refresh
  internal/ui/setup.go, login.go, chat.go
  internal/ui/photo.go, photo_test.go         # inline async picture loader (upstream to gota post-v1)
.github/workflows/qa.yml       # Task 11 modify
gir/pkgconfig/pkgconfig_test.go# Task 12 modify
```

---

### Task 1: Scaffold + hello window + PLATFORM GATE

**Files:**
- Create: `examples/telegram/go.mod`, `examples/telegram/main.go`

**Interfaces:**
- Produces: buildable module; PROOF that gio/v2 + gtk/v4 link against brew GTK on this macOS (the substrate every later E2E stands on — if this gate fails, stop and pivot E2E to Linux/Nix before continuing).

- [ ] **Step 1: Scaffold**

```bash
export PKG_CONFIG_PATH="$(brew --prefix)/lib/pkgconfig:$(brew --prefix)/share/pkgconfig:$PKG_CONFIG_PATH"
mkdir -p examples/telegram && cd examples/telegram
go mod init github.com/diamondburned/gotk4/examples/telegram
go mod edit -replace github.com/diamondburned/gotk4/pkg=../../pkg
```

- [ ] **Step 2: Minimal window**

```go
// main.go
package main

import (
	"os"
	"runtime"

	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

func init() { runtime.LockOSThread() } // GTK owns the main OS thread

func main() {
	app := gtk.NewApplication("com.github.diamondburned.gotk4.telegram", gio.ApplicationFlagsNone)
	app.ConnectActivate(func() {
		w := gtk.NewApplicationWindow(app)
		w.SetTitle("gotk4 Telegram")
		w.SetDefaultSize(900, 640)
		w.SetChild(gtk.NewLabel("hello gotk4"))
		w.SetVisible(true)
	})
	os.Exit(app.Run(os.Args))
}
```

- [ ] **Step 3: GATE — build AND run**

Run: `go mod tidy && go build ./...` → exit 0 (gio/v2 links).
Run: `go run .` → window opens, no crash after ~30s idle (LockOSThread sanity).
If either fails on macOS: STOP, record the error, move all E2E steps of this plan to Linux (nix-shell + a display) before proceeding.

- [ ] **Step 4: Commit** — `git add examples/telegram && git commit -m "feat(examples/telegram): scaffold with platform gate window"`

---

### Task 2: config + model packages

**Files:**
- Create: `examples/telegram/internal/config/config.go`, `config_test.go`, `examples/telegram/internal/model/model.go`

**Interfaces:**
- Produces: `config.Config{APIID int; APIHash string}`, `config.Load(dir) (*Config, error)` (wraps `fs.ErrNotExist` when absent), `config.Save(dir, *Config) error` (0700 dir, 0600 file), `config.DefaultDir()`, `config.DefaultCacheDir()`; and package `model` exactly as below (consumed by name everywhere).

- [ ] **Step 1: Failing config test**

```go
// config_test.go
package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestRoundtrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cfg")
	if _, err := Load(dir); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("want ErrNotExist, got %v", err)
	}
	want := &Config{APIID: 12345, APIHash: "abcdef"}
	if err := Save(dir, want); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil || *got != *want {
		t.Fatalf("got %+v err %v", got, err)
	}
	di, _ := os.Stat(dir)
	if di.Mode().Perm() != 0o700 {
		t.Errorf("dir perm = %o, want 700", di.Mode().Perm())
	}
	fi, _ := os.Stat(filepath.Join(dir, "config.json"))
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("file perm = %o, want 600", fi.Mode().Perm())
	}
}
```

- [ ] **Step 2: Run, FAIL** — `go test ./internal/config/ -v` → undefined: Load.

- [ ] **Step 3: Implement config**

```go
// config.go
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Config struct {
	APIID   int    `json:"api_id"`
	APIHash string `json:"api_hash"`
}

func DefaultDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "gotk4-telegram"), nil
}

func DefaultCacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "gotk4-telegram"), nil
}

func path(dir string) string { return filepath.Join(dir, "config.json") }

func Load(dir string) (*Config, error) {
	b, err := os.ReadFile(path(dir))
	if err != nil {
		return nil, err // wraps fs.ErrNotExist
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func Save(dir string, c *Config) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path(dir), b, 0o600)
}
```

- [ ] **Step 4: model package (pure data, no tests — no logic)**

```go
// model.go
package model

type PeerKind int

const (
	PeerUser PeerKind = iota
	PeerChat            // basic group
	PeerChannel         // supergroup or broadcast channel
)

type Dialog struct {
	PeerID  int64
	Kind    PeerKind
	Title   string
	Preview string
	Unread  int
	CanSend bool // false for broadcast channels
}

type Message struct {
	ID     int
	PeerID int64
	Kind   PeerKind
	Sender string
	Text   string // plain text; "[photo]"/"[file]"/"[media]" for media
	Out    bool
	Photo  *PhotoRef
}

type PhotoRef struct {
	PhotoID    int64
	AccessHash int64
	FileRef    []byte
	SizeType   string // Telegram size letter, e.g. "x"
	MsgID      int    // for FILE_REFERENCE_EXPIRED refetch
	PeerID     int64  // refetch routing:
	Kind       PeerKind // channels need ChannelsGetMessages
}

type PromptKind int

const (
	PromptPhone PromptKind = iota
	PromptCode
	PromptPassword
)

type EventKind int

const (
	EventReady EventKind = iota
	EventConnecting
	EventAuthNeeded // session absent/invalid → show login
	EventNewMessage
	EventError
)

type Event struct {
	Kind EventKind
	Msg  Message // EventNewMessage
	Err  string  // EventError / EventConnecting detail (sanitized, no secrets)
}
```

- [ ] **Step 5: Run, PASS** — `go test ./internal/config/ -v && go build ./...`.
- [ ] **Step 6: Commit** — `git add -A examples/telegram && git commit -m "feat(examples/telegram): config with strict perms + view models"`

---

### Task 3: authenticator + backoff

**Files:**
- Create: `examples/telegram/internal/tg/auth.go`, `auth_test.go`, `backoff.go`, `backoff_test.go`

**Interfaces:**
- Consumes: `model.PromptKind`.
- Produces: `tg.Prompt{Kind model.PromptKind; Reply chan string}`; `tg.NewUIAuth() *UIAuth` with exported `Prompts chan Prompt`, implementing `auth.UserAuthenticator`; `tg.NextBackoff(prev time.Duration) time.Duration` (0→1s, ×2, cap 60s).

- [ ] **Step 1: Deps** — `cd examples/telegram && go get github.com/gotd/td@latest github.com/gotd/contrib@latest && go mod tidy`

- [ ] **Step 2: Failing tests**

```go
// auth_test.go
package tg

import (
	"context"
	"testing"
	"time"

	"github.com/diamondburned/gotk4/examples/telegram/internal/model"
)

func script(t *testing.T, a *UIAuth, answers map[model.PromptKind]string) {
	t.Helper()
	go func() {
		for p := range a.Prompts {
			p.Reply <- answers[p.Kind]
		}
	}()
}

func TestUIAuthPromptFlow(t *testing.T) {
	a := NewUIAuth()
	script(t, a, map[model.PromptKind]string{
		model.PromptPhone:    "+15551234567",
		model.PromptCode:     "12345",
		model.PromptPassword: "hunter2",
	})
	ctx := context.Background()
	if got, err := a.Phone(ctx); err != nil || got != "+15551234567" {
		t.Fatalf("Phone = %q, %v", got, err)
	}
	if got, err := a.Code(ctx, nil); err != nil || got != "12345" {
		t.Fatalf("Code = %q, %v", got, err)
	}
	if got, err := a.Password(ctx); err != nil || got != "hunter2" {
		t.Fatalf("Password = %q, %v", got, err)
	}
}

func TestUIAuthCancel(t *testing.T) {
	a := NewUIAuth()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := a.Phone(ctx); err == nil {
		t.Fatal("want ctx error when nobody answers")
	}
}

func TestUIAuthSignUpRejected(t *testing.T) {
	if _, err := NewUIAuth().SignUp(context.Background()); err == nil {
		t.Fatal("sign-up must be rejected")
	}
}
```

```go
// backoff_test.go
package tg

import (
	"testing"
	"time"
)

func TestNextBackoff(t *testing.T) {
	cases := []struct{ prev, want time.Duration }{
		{0, time.Second},
		{time.Second, 2 * time.Second},
		{16 * time.Second, 32 * time.Second},
		{45 * time.Second, 60 * time.Second},
		{60 * time.Second, 60 * time.Second},
	}
	for _, c := range cases {
		if got := NextBackoff(c.prev); got != c.want {
			t.Errorf("NextBackoff(%v) = %v, want %v", c.prev, got, c.want)
		}
	}
}
```

- [ ] **Step 3: Run, FAIL** — `go test ./internal/tg/ -v`.

- [ ] **Step 4: Implement**

```go
// auth.go
package tg

import (
	"context"
	"errors"

	"github.com/gotd/td/telegram/auth"
	tgapi "github.com/gotd/td/tg"

	"github.com/diamondburned/gotk4/examples/telegram/internal/model"
)

type Prompt struct {
	Kind  model.PromptKind
	Reply chan string
}

type UIAuth struct {
	Prompts chan Prompt
}

// Compile-time pin: the 4-agent review verified this interface match
// against gotd v0.159.0 (Phone/Code/Password/AcceptTermsOfService/SignUp).
var _ auth.UserAuthenticator = (*UIAuth)(nil)

func NewUIAuth() *UIAuth { return &UIAuth{Prompts: make(chan Prompt)} }

func (a *UIAuth) ask(ctx context.Context, k model.PromptKind) (string, error) {
	reply := make(chan string, 1)
	select {
	case a.Prompts <- Prompt{Kind: k, Reply: reply}:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	select {
	case v := <-reply:
		return v, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (a *UIAuth) Phone(ctx context.Context) (string, error) { return a.ask(ctx, model.PromptPhone) }

func (a *UIAuth) Code(ctx context.Context, _ *tgapi.AuthSentCode) (string, error) {
	return a.ask(ctx, model.PromptCode)
}

func (a *UIAuth) Password(ctx context.Context) (string, error) {
	return a.ask(ctx, model.PromptPassword)
}

func (a *UIAuth) AcceptTermsOfService(ctx context.Context, tos tgapi.HelpTermsOfService) error {
	return nil
}

func (a *UIAuth) SignUp(ctx context.Context) (auth.UserInfo, error) {
	return auth.UserInfo{}, errors.New("sign-up is not supported; register with an official client first")
}
```

```go
// backoff.go
package tg

import "time"

const maxBackoff = 60 * time.Second

func NextBackoff(prev time.Duration) time.Duration {
	if prev <= 0 {
		return time.Second
	}
	next := prev * 2
	if next > maxBackoff {
		return maxBackoff
	}
	return next
}
```

- [ ] **Step 5: Run, PASS.** — `go test ./internal/tg/ -v`
- [ ] **Step 6: Commit** — `git commit -am "feat(examples/telegram): channel-driven authenticator + capped backoff"`

---

### Task 4: client lifecycle + WALKING-SKELETON SPIKE (first real E2E)

**Files:**
- Create: `examples/telegram/internal/tg/client.go`, `stubs.go`, `cmd/spike/main.go`

**Interfaces:**
- Consumes: `UIAuth`, `NextBackoff`, `config.Config`, `model.Event`.
- Produces: `tg.NewClient(cfg config.Config, dataDir string) *Client` with `Auth *UIAuth`, `Events chan model.Event`; `(*Client).Run(ctx) error` (blocking loop, backoff re-entry); `(*Client).Ready() bool`; `(*Client).SelfName() string`; `(*Client).SessionPath() string`; internal `c.api() *tgapi.Client`, `c.sender() *message.Sender`, `c.self() *tgapi.User`, `c.peers`, `c.chats`.
- Stubs (each deleted by the named later task): `peerStore` (Task 5), `chatStates` (Task 7), `bindUpdates` VALUE param + `Logout` (Task 8).

- [ ] **Step 1: Implement client.go**

```go
// client.go
package tg

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gotd/contrib/middleware/floodwait"
	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/tgerr"
	tgapi "github.com/gotd/td/tg"

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
		if ctx.Err() != nil {
			return ctx.Err()
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
			flow := auth.NewFlow(c.Auth, auth.SendCodeOptions{})
			if err := flow.Run(ctx, client.Auth()); err != nil {
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
```

```go
// stubs.go — each stub is DELETED by the task named next to it.
package tg

import (
	"context"

	tgapi "github.com/gotd/td/tg"
)

type peerStore struct{}                     // Task 5
func newPeerStore() *peerStore { return &peerStore{} }

type chatStates struct{}                    // Task 7
func newChatStates() *chatStates { return &chatStates{} }

func (c *Client) bindUpdates(tgapi.UpdateDispatcher) {} // Task 8 (VALUE param)

func (c *Client) Logout(ctx context.Context) error { return nil } // Task 8
```

- [ ] **Step 2: Build** — `go build ./...` → exit 0.

- [ ] **Step 3: Walking-skeleton spike** — proves the load-bearing triad (Run lifecycle × channel auth × IdleAdd-from-gotd-goroutine) + macOS substrate NOW, before any UI exists.

```go
// cmd/spike/main.go — TEMPORARY. Deleted in Task 6.
package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"runtime"
	"strconv"

	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/diamondburned/gotk4/examples/telegram/internal/config"
	"github.com/diamondburned/gotk4/examples/telegram/internal/model"
	"github.com/diamondburned/gotk4/examples/telegram/internal/tg"
)

func init() { runtime.LockOSThread() }

func main() {
	apiID, err := strconv.Atoi(os.Getenv("TG_API_ID"))
	if err != nil || os.Getenv("TG_API_HASH") == "" {
		log.Fatal("set TG_API_ID and TG_API_HASH")
	}
	cfg := config.Config{APIID: apiID, APIHash: os.Getenv("TG_API_HASH")}
	dir, err := config.DefaultDir()
	if err != nil {
		log.Fatal(err)
	}

	app := gtk.NewApplication("com.github.diamondburned.gotk4.telegram.spike", gio.ApplicationFlagsNone)
	app.ConnectActivate(func() {
		label := gtk.NewLabel("connecting…")
		w := gtk.NewApplicationWindow(app)
		w.SetDefaultSize(400, 120)
		w.SetChild(label)
		w.SetVisible(true)

		client := tg.NewClient(cfg, dir)

		go func() { // auth prompts answered on stdin (spike only!)
			in := bufio.NewScanner(os.Stdin)
			for p := range client.Auth.Prompts {
				names := map[model.PromptKind]string{
					model.PromptPhone: "phone", model.PromptCode: "code", model.PromptPassword: "2FA password",
				}
				fmt.Printf("enter %s: ", names[p.Kind])
				in.Scan()
				p.Reply <- in.Text()
			}
		}()
		go func() {
			for ev := range client.Events {
				if ev.Kind == model.EventReady {
					name := client.SelfName()
					coreglib.IdleAdd(func() { // THE proof: UI mutation from gotd goroutine
						label.SetText("logged in as " + name)
					})
				}
			}
		}()
		go func() {
			if err := client.Run(context.Background()); err != nil {
				log.Println("client stopped:", err)
			}
		}()
	})
	os.Exit(app.Run(nil)) // nil args: spike takes no CLI flags
}
```

- [ ] **Step 4: SPIKE E2E GATE**

Run: `TG_API_ID=<id> TG_API_HASH=<hash> go run ./cmd/spike`
1. Window "connecting…" opens.
2. Terminal prompts phone → code (→ 2FA); label flips to "logged in as <you>".
3. Ctrl-C, rerun → label flips straight to "logged in" (session reuse, no prompts).
4. `ls -l "$HOME/Library/Application Support/gotk4-telegram/"` → session.json `-rw-------`.

Failure here = architecture problem. STOP and fix `client.go` (this is exactly why the spike exists) before writing any UI.

- [ ] **Step 5: Commit** — `git add -A examples/telegram && git commit -m "feat(examples/telegram): client lifecycle proven by walking-skeleton spike"`

---

### Task 5: peer store + dialogs

**Files:**
- Create: `examples/telegram/internal/tg/peers.go`, `peers_test.go`, `dialogs.go`, `dialogs_test.go`
- Modify: `stubs.go` (delete peerStore stub)

**Interfaces:**
- Produces: `(*peerStore).Add(users []tgapi.UserClass, chats []tgapi.ChatClass)`, `.InputPeer(kind model.PeerKind, id int64) (tgapi.InputPeerClass, bool)`, `.InputChannel(id int64) (*tgapi.InputChannel, bool)`, `.Title(kind, id) string`, `.CanSend(kind, id) bool`; `peerKey(p tgapi.PeerClass) (model.PeerKind, int64)`; `mapDialogs(...)` pure; `(*Client).Dialogs(ctx) ([]model.Dialog, error)`.

- [ ] **Step 1: Failing tests** (both files)

```go
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
```

```go
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
```

- [ ] **Step 2: Run, FAIL.**

- [ ] **Step 3: Implement**

```go
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
			}
		case *tgapi.Channel:
			s.m[model.PeerChannel][ch.ID] = peerInfo{
				input:   &tgapi.InputPeerChannel{ChannelID: ch.ID, AccessHash: ch.AccessHash},
				title:   ch.Title,
				canSend: !ch.Broadcast,
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
```

```go
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
```

Delete the `peerStore` stub from `stubs.go`.

- [ ] **Step 4: Run, PASS** — `go test ./internal/tg/ -v && go build ./...`.
- [ ] **Step 5: Commit** — `git commit -am "feat(examples/telegram): peer store + dialog mapping"`

---

### Task 6: setup + login UI, restartable wiring — UI LOGIN E2E

**Files:**
- Create: `examples/telegram/internal/ui/setup.go`, `internal/ui/login.go`
- Modify: `examples/telegram/main.go` (full rewrite below)
- Delete: `examples/telegram/cmd/spike/` (superseded)

**Interfaces:**
- Produces: `ui.NewSetupPage(onSave func(id int, hash string)) gtk.Widgetter`; `ui.NewLoginPage() *LoginPage` with `.Widget() gtk.Widgetter` and `.Show(p tg.Prompt)` (UI thread only); main.go's restartable `start(cfg)` pattern (hoisted `client`; pumps torn down via ctx — consumed verbatim by Tasks 8/9).

- [ ] **Step 1: Setup page (REAL gota API — see Global Constraints)**

```go
// setup.go
package ui

import (
	"strconv"

	"github.com/diamondburned/gotk4/pkg/gota"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// NewSetupPage renders the api_id/api_hash form. onSave gets valid values.
func NewSetupPage(onSave func(id int, hash string)) gtk.Widgetter {
	idEntry := gota.Entry().SetPlaceholder("api_id (digits)")
	hashEntry := gota.Entry().SetPlaceholder("api_hash")
	errLabel := gota.Label("")

	submit := func() {
		id, err := strconv.Atoi(idEntry.GetText())
		if err != nil || id <= 0 || hashEntry.GetText() == "" {
			errLabel.SetText("api_id must be a positive number; api_hash must be non-empty")
			return
		}
		onSave(id, hashEntry.GetText())
	}

	box := gota.VBox(8).Append(
		gota.Label("Telegram API credentials").Class("title-2"),
		gota.Label("Create them at my.telegram.org → API development tools"),
		idEntry,
		hashEntry,
		gota.Button("Save & connect").OnClick(func(*gota.ButtonWidget) { submit() }),
		errLabel,
	)
	w := box.ToWidget()
	w.SetMarginTop(24)
	w.SetMarginBottom(24)
	w.SetMarginStart(24)
	w.SetMarginEnd(24)
	return w
}
```

- [ ] **Step 2: Login page**

```go
// login.go
package ui

import (
	"github.com/diamondburned/gotk4/pkg/gota"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/diamondburned/gotk4/examples/telegram/internal/model"
	"github.com/diamondburned/gotk4/examples/telegram/internal/tg"
)

type LoginPage struct {
	root   *gtk.Widget
	label  *gota.LabelWidget
	entry  *gota.EntryWidget
	submit func()
}

func NewLoginPage() *LoginPage {
	p := &LoginPage{}
	p.label = gota.Label("")
	p.entry = gota.Entry().OnEnter(func(*gota.EntryWidget) {
		if p.submit != nil {
			p.submit()
		}
	})
	btn := gota.Button("Next").OnClick(func(*gota.ButtonWidget) {
		if p.submit != nil {
			p.submit()
		}
	})
	p.root = gota.VBox(8).Append(p.label, p.entry, btn).ToWidget()
	p.root.SetMarginTop(24)
	p.root.SetMarginBottom(24)
	p.root.SetMarginStart(24)
	p.root.SetMarginEnd(24)
	return p
}

func (p *LoginPage) Widget() gtk.Widgetter { return p.root }

// Show configures the page for one auth prompt. UI thread only.
func (p *LoginPage) Show(pr tg.Prompt) {
	titles := map[model.PromptKind]string{
		model.PromptPhone:    "Phone number (international format)",
		model.PromptCode:     "Login code",
		model.PromptPassword: "Two-factor password",
	}
	p.label.SetText(titles[pr.Kind])
	p.entry.SetText("")
	p.entry.PasswordMode(pr.Kind == model.PromptPassword)
	p.submit = func() {
		v := p.entry.GetText()
		if v == "" {
			return
		}
		p.entry.SetText("")
		go func() { pr.Reply <- v }() // never block the UI thread
	}
}
```

- [ ] **Step 3: main.go — restartable start(), hoisted client**

```go
// main.go (full replacement)
package main

import (
	"context"
	"errors"
	"io/fs"
	"log"
	"os"
	"runtime"

	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/diamondburned/gotk4/examples/telegram/internal/config"
	"github.com/diamondburned/gotk4/examples/telegram/internal/model"
	"github.com/diamondburned/gotk4/examples/telegram/internal/tg"
	"github.com/diamondburned/gotk4/examples/telegram/internal/ui"
)

func init() { runtime.LockOSThread() }

func main() {
	app := gtk.NewApplication("com.github.diamondburned.gotk4.telegram", gio.ApplicationFlagsNone)
	app.ConnectActivate(func() { activate(app) })
	os.Exit(app.Run(os.Args))
}

func activate(app *gtk.Application) {
	dir, err := config.DefaultDir()
	if err != nil {
		log.Fatal(err)
	}

	w := gtk.NewApplicationWindow(app)
	w.SetTitle("gotk4 Telegram")
	w.SetDefaultSize(900, 640)

	stack := gtk.NewStack()
	login := ui.NewLoginPage()
	stack.AddNamed(login.Widget(), "login")
	stack.AddNamed(gtk.NewLabel("connected — chat UI lands in Task 9"), "main")
	w.SetChild(stack)
	w.SetVisible(true)

	// Hoisted, restartable client state. UI thread owns these variables:
	// they are written only from start()/logout (both run on the UI thread).
	var (
		client *tg.Client
		cancel context.CancelFunc
	)

	onEvent := func(ev model.Event) { // UI thread
		switch ev.Kind {
		case model.EventAuthNeeded:
			stack.SetVisibleChildName("login")
		case model.EventReady:
			stack.SetVisibleChildName("main")
		}
	}

	start := func(cfg config.Config) { // UI thread; safe to call again (logout/re-login)
		if cancel != nil {
			cancel()
		}
		ctx, cn := context.WithCancel(context.Background())
		cancel = cn
		client = tg.NewClient(cfg, dir)
		cl := client // pumps capture THIS client, not the mutable variable

		go func() { // event pump
			for {
				select {
				case <-ctx.Done():
					return
				case ev := <-cl.Events:
					ev := ev
					coreglib.IdleAdd(func() { onEvent(ev) })
				}
			}
		}()
		go func() { // auth prompt pump
			for {
				select {
				case <-ctx.Done():
					return
				case pr := <-cl.Auth.Prompts:
					pr := pr
					coreglib.IdleAdd(func() {
						login.Show(pr)
						stack.SetVisibleChildName("login")
					})
				}
			}
		}()
		go func() {
			if err := cl.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
				log.Println("client stopped:", err)
			}
		}()
	}

	cfg, err := config.Load(dir)
	switch {
	case err == nil:
		start(*cfg)
	case errors.Is(err, fs.ErrNotExist):
		setup := ui.NewSetupPage(func(id int, hash string) {
			c := config.Config{APIID: id, APIHash: hash}
			if err := config.Save(dir, &c); err != nil {
				log.Println("save config:", err)
				return
			}
			start(c)
		})
		stack.AddNamed(setup, "setup")
		stack.SetVisibleChildName("setup")
	default:
		log.Fatal(err)
	}
}
```

- [ ] **Step 4: Delete spike** — `git rm -r examples/telegram/cmd`

- [ ] **Step 5: Build + UI LOGIN E2E**

`go build ./... && go run .`
1. Fresh machine path: delete `~/Library/Application Support/gotk4-telegram/` first → setup page → enter creds → login pages (phone/code/2FA; password field masked) → "connected".
2. Restart → straight to "connected".
Screenshot both pages (macOS/Quartz — note platform in PR).

- [ ] **Step 6: Commit** — `git add -A examples/telegram && git commit -m "feat(examples/telegram): setup/login UI with restartable client wiring"`

---

### Task 7: messages — history, Sender send, echo consumption, dedup

**Files:**
- Create: `examples/telegram/internal/tg/messages.go`, `messages_test.go`
- Modify: `stubs.go` (delete chatStates stub)

**Interfaces:**
- Consumes: `peerStore`, `peerKey`, `model.Message`.
- Produces:
  - `mapMessage(s *peerStore, selfID int64, mc tgapi.MessageClass) (model.Message, bool)` — pure; fills `Kind`, media placeholders, `PhotoRef` (SizeType via Task 10's `pickSize`; until Task 10 lands, `SizeType` is set to `""` and photo cells don't render — see note in Step 3).
  - `(*chatStates).Note(peerID int64, msgID int) bool` — true on first sighting (the ONLY dedup mechanism; optimistic machinery was cut in v2).
  - `(*Client).History(ctx, kind, peerID) ([]model.Message, error)` — latest 50, oldest first.
  - `(*Client).Send(ctx, kind, peerID, text) error` — `message.NewSender`, then feeds the RPC response updates through the same dedup path so own messages appear exactly once (handles `*tgapi.UpdateShortSentMessage`, which carries only the new ID).
  - `(*Client).MarkRead(ctx, kind, peerID) error`.
  - Consumed by Task 8: `(*Client).handleNewMessage(mc tgapi.MessageClass)` (defined here since Send shares it).

- [ ] **Step 1: Failing tests**

```go
// messages_test.go
package tg

import (
	"testing"

	tgapi "github.com/gotd/td/tg"

	"github.com/diamondburned/gotk4/examples/telegram/internal/config"
	"github.com/diamondburned/gotk4/examples/telegram/internal/model"
)

func TestMapMessage(t *testing.T) {
	s := newPeerStore()
	s.Add([]tgapi.UserClass{&tgapi.User{ID: 7, FirstName: "Bea"}}, nil)

	plain := &tgapi.Message{ID: 1, Message: "yo", FromID: &tgapi.PeerUser{UserID: 7}, PeerID: &tgapi.PeerUser{UserID: 7}}
	m, ok := mapMessage(s, 99, plain)
	if !ok || m.Text != "yo" || m.Sender != "Bea" || m.Out || m.Kind != model.PeerUser {
		t.Fatalf("plain = %+v ok=%v", m, ok)
	}

	photo := &tgapi.Message{ID: 2, PeerID: &tgapi.PeerUser{UserID: 7},
		Media: &tgapi.MessageMediaPhoto{Photo: &tgapi.Photo{ID: 555, AccessHash: 556, FileReference: []byte{1}}}}
	pm, ok := mapMessage(s, 99, photo)
	if !ok || pm.Text != "[photo]" || pm.Photo == nil || pm.Photo.PhotoID != 555 || pm.Photo.MsgID != 2 || pm.Photo.PeerID != 7 {
		t.Fatalf("photo = %+v", pm)
	}

	own := &tgapi.Message{ID: 3, Message: "mine", Out: true, PeerID: &tgapi.PeerUser{UserID: 7}}
	if om, _ := mapMessage(s, 99, own); !om.Out {
		t.Fatal("own message must be Out")
	}
}

func TestChatStatesNote(t *testing.T) {
	cs := newChatStates()
	if !cs.Note(1, 100) {
		t.Fatal("first sighting true")
	}
	if cs.Note(1, 100) {
		t.Fatal("second sighting false")
	}
	if !cs.Note(2, 100) {
		t.Fatal("chats independent")
	}
}

func TestConsumeSentShortUpdate(t *testing.T) {
	c := NewClient(config.Config{}, t.TempDir())
	c.consumeSent(model.PeerUser, 7, "hello",
		&tgapi.UpdateShortSentMessage{ID: 900})

	select {
	case ev := <-c.Events:
		if ev.Kind != model.EventNewMessage || !ev.Msg.Out || ev.Msg.Text != "hello" || ev.Msg.ID != 900 || ev.Msg.PeerID != 7 {
			t.Fatalf("ev = %+v", ev)
		}
	default:
		t.Fatal("no event emitted")
	}
	// the seen-set now contains 900: a later push of the same message dedups
	if c.chats.Note(7, 900) {
		t.Fatal("id 900 must already be seen")
	}
}
```

- [ ] **Step 2: Run, FAIL.**

- [ ] **Step 3: Implement**

```go
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
```

Delete the `chatStates` stub from `stubs.go`. NOTE: no `telegram/message` import needed here — `c.sender()` comes from client.go.

- [ ] **Step 4: Run, PASS** — `go test ./internal/tg/ -v && go build ./...`.
- [ ] **Step 5: Commit** — `git commit -am "feat(examples/telegram): history + Sender send with echo dedup + mark-read"`

---

### Task 8: live updates + logout

**Files:**
- Create: `examples/telegram/internal/tg/updates.go`, `updates_test.go`
- Modify: delete `stubs.go` entirely; `main.go` (logout handling shown in Task 9's wiring)

**Interfaces:**
- Consumes: `handleNewMessage` (Task 7).
- Produces: `(*Client).bindUpdates(d tgapi.UpdateDispatcher)` (VALUE — verified: dispatcher copies share the handlers map); `(*Client).Logout(ctx) error`.

- [ ] **Step 1: Failing test**

```go
// updates_test.go
package tg

import (
	"testing"

	tgapi "github.com/gotd/td/tg"

	"github.com/diamondburned/gotk4/examples/telegram/internal/config"
	"github.com/diamondburned/gotk4/examples/telegram/internal/model"
)

func TestHandleNewMessageDedups(t *testing.T) {
	c := NewClient(config.Config{}, t.TempDir())
	c.peers.Add([]tgapi.UserClass{&tgapi.User{ID: 7, FirstName: "Bea"}}, nil)

	msg := &tgapi.Message{ID: 42, Message: "hey", PeerID: &tgapi.PeerUser{UserID: 7}}
	c.handleNewMessage(msg)
	c.handleNewMessage(msg)

	select {
	case ev := <-c.Events:
		if ev.Kind != model.EventNewMessage || ev.Msg.Text != "hey" {
			t.Fatalf("ev = %+v", ev)
		}
	default:
		t.Fatal("no event emitted")
	}
	select {
	case ev := <-c.Events:
		t.Fatalf("duplicate emitted: %+v", ev)
	default:
	}
}
```

- [ ] **Step 2: Run, FAIL** (handleNewMessage exists from Task 7, but the test file also pins bindUpdates/Logout below — write it, watch the dedup test pass and the build break on the stub deletion until Step 3 completes).

- [ ] **Step 3: Implement**

```go
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
```

Delete `stubs.go`.

- [ ] **Step 4: Run, PASS** — `go test ./internal/tg/ -v && go build ./...`.
- [ ] **Step 5: Commit** — `git commit -am "feat(examples/telegram): live update dispatch + logout"`

---

### Task 9: chat UI — REALTIME E2E

**Files:**
- Create: `examples/telegram/internal/ui/chat.go`
- Modify: `examples/telegram/main.go`

**Interfaces:**
- Consumes: `gioutil.NewListModel[T]` (verified: `.ListModel` embedded `*gio.ListModel` satisfies `gio.ListModeller`; `Append/Splice(pos, removals, values...)/At/Len/Set`; `gioutil.ObjectValue[T](item.Item())`), factory callbacks `func(*coreglib.Object)` + `.Cast().(*gtk.ListItem)`, `gtk.NewListView(sel, &factory.ListItemFactory)`.
- Produces:

```go
type ChatActions struct {
	LoadHistory func(d model.Dialog)
	Send        func(d model.Dialog, text string)
	Logout      func()
}
func NewChatView(actions ChatActions) *ChatView
func (v *ChatView) Widget() gtk.Widgetter
func (v *ChatView) SetDialogs(ds []model.Dialog)                    // UI thread
func (v *ChatView) SetMessages(d model.Dialog, ms []model.Message)  // UI thread
func (v *ChatView) AppendMessage(m model.Message)                   // UI thread
func (v *ChatView) SetBanner(text string)                           // "" hides
```

- [ ] **Step 1: Implement chat.go** (mixed layout uses RAW gtk containers — gota boxes reject raw gtk children)

```go
// chat.go
package ui

import (
	"strconv"

	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/core/gioutil"
	"github.com/diamondburned/gotk4/pkg/gota"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"

	"github.com/diamondburned/gotk4/examples/telegram/internal/model"
)

type ChatActions struct {
	LoadHistory func(d model.Dialog)
	Send        func(d model.Dialog, text string)
	Logout      func()
}

type ChatView struct {
	root     *gtk.Box
	banner   *gtk.Revealer
	bannerLb *gtk.Label

	dialogs  *gioutil.ListModel[model.Dialog]
	messages *gioutil.ListModel[model.Message]

	current    model.Dialog
	hasCurrent bool
}

func NewChatView(actions ChatActions) *ChatView {
	v := &ChatView{
		dialogs:  gioutil.NewListModel[model.Dialog](),
		messages: gioutil.NewListModel[model.Message](),
	}

	// banner
	v.bannerLb = gtk.NewLabel("")
	v.banner = gtk.NewRevealer()
	v.banner.SetChild(v.bannerLb)
	v.banner.SetRevealChild(false)

	// sidebar
	dlgFactory := gtk.NewSignalListItemFactory()
	dlgFactory.ConnectSetup(func(obj *coreglib.Object) {
		item := obj.Cast().(*gtk.ListItem)
		lb := gtk.NewLabel("")
		lb.SetXAlign(0)
		lb.SetEllipsize(pango.EllipsizeEnd)
		item.SetChild(lb)
	})
	dlgFactory.ConnectBind(func(obj *coreglib.Object) {
		item := obj.Cast().(*gtk.ListItem)
		d := gioutil.ObjectValue[model.Dialog](item.Item())
		text := d.Title
		if d.Unread > 0 {
			text += " (" + strconv.Itoa(d.Unread) + ")"
		}
		if d.Preview != "" {
			text += "\n" + d.Preview
		}
		item.Child().(*gtk.Label).SetText(text) // plain text only
	})
	dlgSel := gtk.NewSingleSelection(v.dialogs.ListModel)
	dlgSel.SetAutoselect(false)
	dlgSel.ConnectSelectionChanged(func(_, _ uint) {
		i := dlgSel.Selected()
		if i == gtk.InvalidListPosition {
			return
		}
		d := v.dialogs.At(int(i))
		v.current, v.hasCurrent = d, true
		actions.LoadHistory(d)
	})
	dlgList := gtk.NewListView(dlgSel, &dlgFactory.ListItemFactory)

	// message pane
	msgFactory := gtk.NewSignalListItemFactory()
	msgFactory.ConnectSetup(func(obj *coreglib.Object) {
		item := obj.Cast().(*gtk.ListItem)
		item.SetChild(newMessageCell().root)
	})
	msgFactory.ConnectBind(func(obj *coreglib.Object) {
		item := obj.Cast().(*gtk.ListItem)
		m := gioutil.ObjectValue[model.Message](item.Item())
		bindMessageCell(item.Child().(*gtk.Box), m, nil) // photo loader arrives in Task 10
	})
	msgSel := gtk.NewNoSelection(v.messages.ListModel)
	msgList := gtk.NewListView(msgSel, &msgFactory.ListItemFactory)

	// composer (gota Entry is self-contained → ToWidget into the raw box)
	composer := gota.Entry().SetPlaceholder("Message")
	composer.OnEnter(func(e *gota.EntryWidget) {
		text := e.GetText()
		if text == "" || !v.hasCurrent || !v.current.CanSend {
			return
		}
		e.SetText("")
		actions.Send(v.current, text)
	})

	logoutBtn := gota.Button("Logout").OnClick(func(*gota.ButtonWidget) { actions.Logout() })

	// layout: raw gtk (gota containers only accept gota.IWidget)
	msgScroll := gtk.NewScrolledWindow()
	msgScroll.SetChild(msgList)
	msgScroll.SetVExpand(true)

	right := gtk.NewBox(gtk.OrientationVertical, 4)
	right.Append(msgScroll)
	right.Append(composer.ToWidget())

	dlgScroll := gtk.NewScrolledWindow()
	dlgScroll.SetChild(dlgList)
	dlgScroll.SetVExpand(true)

	left := gtk.NewBox(gtk.OrientationVertical, 4)
	left.Append(logoutBtn.ToWidget())
	left.Append(dlgScroll)

	pane := gtk.NewPaned(gtk.OrientationHorizontal)
	pane.SetStartChild(left)
	pane.SetEndChild(right)
	pane.SetPosition(280)
	pane.SetVExpand(true)

	v.root = gtk.NewBox(gtk.OrientationVertical, 0)
	v.root.Append(v.banner)
	v.root.Append(pane)
	return v
}

// newMessageCell / bindMessageCell live in photo.go from Task 10 on; until
// then define them here minimally:
type messageCell struct{ root *gtk.Box }

func newMessageCell() *messageCell {
	box := gtk.NewBox(gtk.OrientationVertical, 4)
	lb := gtk.NewLabel("")
	lb.SetXAlign(0)
	lb.SetWrap(true)
	box.Append(lb)
	return &messageCell{root: box}
}

func bindMessageCell(box *gtk.Box, m model.Message, _ interface{}) {
	lb := box.FirstChild().(*gtk.Label)
	prefix := m.Sender
	if m.Out {
		prefix = "me"
	}
	lb.SetText(prefix + ": " + m.Text)
}

func (v *ChatView) Widget() gtk.Widgetter { return v.root }

func (v *ChatView) SetBanner(text string) {
	v.bannerLb.SetText(text)
	v.banner.SetRevealChild(text != "")
}

func (v *ChatView) SetDialogs(ds []model.Dialog) {
	v.dialogs.Splice(0, v.dialogs.Len(), ds...)
}

func (v *ChatView) SetMessages(d model.Dialog, ms []model.Message) {
	if !v.hasCurrent || v.current.PeerID != d.PeerID {
		return
	}
	v.messages.Splice(0, v.messages.Len(), ms...)
}

func (v *ChatView) AppendMessage(m model.Message) {
	if v.hasCurrent && v.current.PeerID == m.PeerID {
		v.messages.Append(m)
	}
	for i := 0; i < v.dialogs.Len(); i++ {
		d := v.dialogs.At(i)
		if d.PeerID == m.PeerID {
			d.Preview = m.Text
			v.dialogs.Set(i, d)
			break
		}
	}
}
```

- [ ] **Step 2: Wire main.go.** `client`/`cancel` are already hoisted (Task 6). Add after the `login` page creation, BEFORE `start` (closures read the hoisted `client` variable at call time — always the live client):

```go
	var chat *ui.ChatView
	chat = ui.NewChatView(ui.ChatActions{
		LoadHistory: func(d model.Dialog) {
			cl := client
			go func() {
				ms, err := cl.History(context.Background(), d.Kind, d.PeerID)
				if err != nil {
					coreglib.IdleAdd(func() { chat.SetBanner("failed to load history") })
					return
				}
				_ = cl.MarkRead(context.Background(), d.Kind, d.PeerID)
				coreglib.IdleAdd(func() { chat.SetMessages(d, ms) })
			}()
		},
		Send: func(d model.Dialog, text string) {
			cl := client
			go func() {
				if err := cl.Send(context.Background(), d.Kind, d.PeerID, text); err != nil {
					coreglib.IdleAdd(func() { chat.SetBanner("send failed") })
				}
			}()
		},
		Logout: func() {
			cl := client
			if cancel != nil {
				cancel() // stops pumps + Run
			}
			go func() {
				_ = cl.Logout(context.Background())
				coreglib.IdleAdd(func() {
					stack.SetVisibleChildName("login")
					cfg, err := config.Load(dir)
					if err == nil {
						start(*cfg) // fresh client; auth flow will prompt
					}
				})
			}()
		},
	})
	stack.AddNamed(chat.Widget(), "main")
```

(Remove the Task 6 placeholder `AddNamed(gtk.NewLabel(...), "main")` line.) Extend `onEvent`:

```go
		case model.EventReady:
			chat.SetBanner("")
			stack.SetVisibleChildName("main")
			cl := client
			go func() {
				ds, err := cl.Dialogs(context.Background())
				if err != nil {
					return
				}
				coreglib.IdleAdd(func() { chat.SetDialogs(ds) })
			}()
		case model.EventNewMessage:
			chat.AppendMessage(ev.Msg)
		case model.EventConnecting:
			chat.SetBanner("connecting…")
```

- [ ] **Step 3: Build + REALTIME E2E**

`go build ./... && go run .`
1. Dialog list with titles/previews/unread badges.
2. Open chat → history oldest-top.
3. Send from app → appears ONCE (echo consumption); verify on phone.
4. Send from phone → appears in-app in seconds, sidebar preview updates.
5. Unread clears after open (check another client).
6. Logout → login pages → re-login works (restartable start; no double events — watch for duplicate rows as the regression signal for leaked pumps).
Screenshot the chat (macOS/Quartz).

- [ ] **Step 4: Commit** — `git add -A examples/telegram && git commit -m "feat(examples/telegram): chat view with realtime send/receive + logout"`

---

### Task 10: photos — pipeline + inline async cell — PHOTO E2E

**Files:**
- Create: `examples/telegram/internal/tg/photos.go`, `photos_test.go`, `internal/ui/photo.go`, `photo_test.go`
- Modify: `internal/tg/messages.go` (replace `pickSizeOrEmpty` seam), `messages_test.go` (photo fixture gains Sizes), `internal/ui/chat.go` (bind cells with the loader), `main.go` (pass loader)

**Interfaces:**
- Produces: `tg.pickSize(sizes []tgapi.PhotoSizeClass) (string, bool)`; `tg.cachePath(root string, photoID int64, sizeType string) (string, error)`; `(*tg.Client).DownloadPhoto(ctx, ref model.PhotoRef, cacheDir string) (string, error)`; `ui.PhotoLoader func(ref model.PhotoRef) (string, error)`; async picture loader in `ui` (upstreamed to `pkg/gota` AFTER v1 ships — deliberately app-local now).

- [ ] **Step 1: Failing tests**

```go
// photos_test.go (package tg)
package tg

import (
	"path/filepath"
	"strings"
	"testing"

	tgapi "github.com/gotd/td/tg"
)

func TestPickSize(t *testing.T) {
	sizes := []tgapi.PhotoSizeClass{
		&tgapi.PhotoStrippedSize{Type: "i"},
		&tgapi.PhotoSize{Type: "m", W: 320, H: 240},
		&tgapi.PhotoSize{Type: "x", W: 800, H: 600},
		&tgapi.PhotoSize{Type: "y", W: 1280, H: 960},
	}
	if got, ok := pickSize(sizes); !ok || got != "x" {
		t.Fatalf("pickSize = %q, %v", got, ok)
	}
	only := []tgapi.PhotoSizeClass{&tgapi.PhotoSize{Type: "w", W: 2560, H: 1920}}
	if got, ok := pickSize(only); !ok || got != "w" {
		t.Fatalf("fallback = %q, %v", got, ok)
	}
	if _, ok := pickSize([]tgapi.PhotoSizeClass{&tgapi.PhotoStrippedSize{Type: "i"}}); ok {
		t.Fatal("no real sizes must be !ok")
	}
}

func TestCachePath(t *testing.T) {
	root := t.TempDir()
	p, err := cachePath(root, 0x1234, "x")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(p) != root || !strings.HasSuffix(p, "1234_x.jpg") {
		t.Fatalf("p = %q", p)
	}
	if _, err := cachePath(root, 1, "../evil"); err == nil {
		t.Fatal("traversal in size type must error")
	}
}
```

```go
// photo_test.go (package ui) — verifies the async loader marshals through
// the idle hook exactly once and never calls SetFilename on error.
package ui

import (
	"errors"
	"testing"

	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

func TestAsyncPicMarshalsOnce(t *testing.T) {
	if !gtk.InitCheck() {
		t.Skip("no GTK display available")
	}
	p := newAsyncPic()
	calls := 0
	done := make(chan struct{})
	p.idle = func(f func()) { // fake idle RUNS f (v1 review: a storing fake deadlocks)
		calls++
		f()
		close(done)
	}
	p.load(func() (string, error) { return "", errors.New("nope") })
	<-done
	if calls != 1 {
		t.Fatalf("idle calls = %d, want 1", calls)
	}
}
```

- [ ] **Step 2: Run, FAIL** — `go test ./internal/tg/ ./internal/ui/ -v`.

- [ ] **Step 3: Implement tg side**

```go
// photos.go
package tg

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/tgerr"
	tgapi "github.com/gotd/td/tg"

	"github.com/diamondburned/gotk4/examples/telegram/internal/model"
)

const maxPhotoBytes = 10 << 20 // 10 MB

func pickSize(sizes []tgapi.PhotoSizeClass) (string, bool) {
	best, smallest := "", ""
	bestW, smallestW := 0, int(^uint(0)>>1)
	for _, sc := range sizes {
		var typ string
		var w int
		switch s := sc.(type) {
		case *tgapi.PhotoSize:
			typ, w = s.Type, s.W
		case *tgapi.PhotoSizeProgressive:
			typ, w = s.Type, s.W
		default:
			continue // stripped/path/cached: no real dimensions
		}
		if w <= 800 && w > bestW {
			best, bestW = typ, w
		}
		if w < smallestW {
			smallest, smallestW = typ, w
		}
	}
	if best != "" {
		return best, true
	}
	if smallest != "" {
		return smallest, true
	}
	return "", false
}

var sizeTypeRe = regexp.MustCompile(`^[a-z]$`)

func cachePath(root string, photoID int64, sizeType string) (string, error) {
	if !sizeTypeRe.MatchString(sizeType) {
		return "", fmt.Errorf("invalid size type %q", sizeType)
	}
	name := fmt.Sprintf("%x_%s.jpg", photoID, sizeType)
	p := filepath.Join(root, name)
	if filepath.Dir(p) != filepath.Clean(root) {
		return "", fmt.Errorf("cache path escapes root")
	}
	return p, nil
}

// DownloadPhoto downloads ref into cacheDir. Cached files reused. One retry
// on FILE_REFERENCE_EXPIRED via a peer-kind-aware message refetch.
func (c *Client) DownloadPhoto(ctx context.Context, ref model.PhotoRef, cacheDir string) (string, error) {
	if ref.SizeType == "" {
		return "", fmt.Errorf("photo has no renderable size")
	}
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return "", err
	}
	path, err := cachePath(cacheDir, ref.PhotoID, ref.SizeType)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}

	dl := func(r model.PhotoRef) error {
		loc := &tgapi.InputPhotoFileLocation{
			ID: r.PhotoID, AccessHash: r.AccessHash,
			FileReference: r.FileRef, ThumbSize: r.SizeType,
		}
		_, err := downloader.NewDownloader().WithPartSize(512*1024).
			Download(c.api(), loc).ToPath(ctx, path)
		return err
	}

	err = dl(ref)
	if err != nil && tgerr.Is(err, "FILE_REFERENCE_EXPIRED") {
		fresh, ferr := c.refreshPhotoRef(ctx, ref)
		if ferr != nil {
			return "", ferr
		}
		err = dl(fresh)
	}
	if err != nil {
		_ = os.Remove(path)
		return "", err
	}
	if fi, serr := os.Stat(path); serr == nil && fi.Size() > maxPhotoBytes {
		_ = os.Remove(path)
		return "", fmt.Errorf("photo exceeds %d bytes", maxPhotoBytes)
	}
	return path, nil
}

// refreshPhotoRef refetches the carrying message for a fresh file_reference.
// Channel/supergroup messages need ChannelsGetMessages (messages.getMessages
// does NOT return them — verified against gotd v0.159.0 / Telegram API).
func (c *Client) refreshPhotoRef(ctx context.Context, ref model.PhotoRef) (model.PhotoRef, error) {
	var msgs []tgapi.MessageClass
	if ref.Kind == model.PeerChannel {
		ch, ok := c.peers.InputChannel(ref.PeerID)
		if !ok {
			return ref, errUnknownPeer
		}
		res, err := c.api().ChannelsGetMessages(ctx, &tgapi.ChannelsGetMessagesRequest{
			Channel: ch,
			ID:      []tgapi.InputMessageClass{&tgapi.InputMessageID{ID: ref.MsgID}},
		})
		if err != nil {
			return ref, err
		}
		if v, ok := res.(*tgapi.MessagesChannelMessages); ok {
			msgs = v.Messages
		}
	} else {
		res, err := c.api().MessagesGetMessages(ctx, []tgapi.InputMessageClass{
			&tgapi.InputMessageID{ID: ref.MsgID},
		})
		if err != nil {
			return ref, err
		}
		if v, ok := res.(*tgapi.MessagesMessages); ok {
			msgs = v.Messages
		}
	}
	for _, mc := range msgs {
		m, ok := mc.(*tgapi.Message)
		if !ok || m.ID != ref.MsgID {
			continue
		}
		if mp, ok := m.Media.(*tgapi.MessageMediaPhoto); ok {
			if p, ok := mp.Photo.(*tgapi.Photo); ok {
				ref.FileRef = p.FileReference
				return ref, nil
			}
		}
	}
	return ref, fmt.Errorf("photo not found in refetched message")
}
```

In `messages.go`: delete `pickSizeOrEmpty` and call `pickSize` directly (`ref.SizeType, _ = pickSize(p.Sizes)`). In `messages_test.go`: add `Sizes: []tgapi.PhotoSizeClass{&tgapi.PhotoSize{Type: "x", W: 800, H: 600}}` to the photo fixture and assert `pm.Photo.SizeType == "x"`.

- [ ] **Step 4: Implement ui side**

```go
// photo.go
package ui

import (
	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/diamondburned/gotk4/examples/telegram/internal/model"
)

type PhotoLoader func(ref model.PhotoRef) (string, error)

// asyncPic: goroutine fetch → idle → SetFilename. Kept app-local for v1;
// upstream to pkg/gota (with a real consumer as evidence) after v1 ships.
type asyncPic struct {
	*gtk.Picture
	idle func(func()) // test seam; defaults to coreglib.IdleAdd
}

func newAsyncPic() *asyncPic {
	p := gtk.NewPicture()
	p.SetSizeRequest(320, 240)
	p.SetCanShrink(true)
	return &asyncPic{Picture: p, idle: func(f func()) { coreglib.IdleAdd(f) }}
}

func (a *asyncPic) load(fetch func() (string, error)) {
	go func() {
		path, err := fetch()
		a.idle(func() {
			if err == nil && path != "" {
				a.SetFilename(path)
			}
		})
	}()
}
```

Update `chat.go`: change `bindMessageCell(box *gtk.Box, m model.Message, load PhotoLoader)` — after setting the label, remove any previous picture child (`for c := box.FirstChild(); ...` keep only the label, or track via `box.LastChild()` != label → `box.Remove`), and when `m.Photo != nil && m.Photo.SizeType != "" && load != nil`:

```go
	if m.Photo != nil && m.Photo.SizeType != "" && load != nil {
		ref := *m.Photo
		pic := newAsyncPic()
		box.Append(pic)
		pic.load(func() (string, error) { return load(ref) })
	}
```

`NewChatView` gains the loader via `ChatActions`: add field `LoadPhoto PhotoLoader`, pass `actions.LoadPhoto` in the Bind callback. In `main.go`:

```go
	cacheDir, _ := config.DefaultCacheDir()
	// in ChatActions literal:
	LoadPhoto: func(ref model.PhotoRef) (string, error) {
		return client.DownloadPhoto(context.Background(), ref, cacheDir)
	},
```

- [ ] **Step 5: Run, PASS** — `go test ./internal/tg/ ./internal/ui/ -v && go build ./...`.
- [ ] **Step 6: PHOTO E2E** — `go run .`; open a photo-bearing chat → placeholder → image renders; reopen instant (cache); `ls ~/Library/Caches/gotk4-telegram/` → only `<hex>_<letter>.jpg`. Screenshot.
- [ ] **Step 7: Commit** — `git commit -am "feat(examples/telegram): photo pipeline with channel-aware refresh + async cells"`

---

### Task 11: polish + README + CI + PR

**Files:**
- Modify: `examples/telegram/internal/ui/chat.go`, `.github/workflows/qa.yml`
- Create: `examples/telegram/README.md`

- [ ] **Step 1: Empty states.** Sidebar: wrap `dlgScroll` in a `gtk.Stack` with a `gtk.NewLabel("No chats yet")` page; `SetDialogs` flips: `if len(ds) == 0 { sideStack.SetVisibleChildName("empty") } else { sideStack.SetVisibleChildName("list") }`. Same pattern for the message pane ("Select a chat").
Verify: run with a test account with 0 dialogs OR temporarily `SetDialogs(nil)` → label shows.

- [ ] **Step 2: README.md** — sections: purpose (bindings proof), prerequisites (nix-shell or brew + the PKG_CONFIG_PATH export line; api_id/api_hash from my.telegram.org), run (`go run .`), storage table (config.json 0600, session.json 0600 = full-account secret, cache dir) + logout note, v1 scope + out-of-scope (copy the spec lists), screenshots (login/chat/photo; captioned "macOS/Quartz"), and "gap recovery + pagination: v2" note.

- [ ] **Step 3: goimports BEFORE CI push** (v1 review: qa.yml runs goimports + git-dirty; unformatted examples would fail the watched run)

```bash
goimports -w examples/telegram && git diff --exit-code examples/telegram || git commit -am "style: goimports"
```

- [ ] **Step 4: CI step.** Read qa.yml; mirror the existing nix step syntax exactly; add after the pkg tests:

```yaml
      - name: Build examples/telegram
        run: nix-shell --run "cd examples/telegram && go build ./... && go test ./..."
```

Module downloads use the network like every other CI `go` step — vendoring is REJECTED (Global Constraints), do not add a vendor/ tree if the run fails; fix the fetch instead.

- [ ] **Step 5: Full sweep**

```bash
cd examples/telegram && go vet ./... && go test ./... && go build ./... && cd ../..
git push -u origin feat/telegram-app && gh run watch
```

Expected: vet/test/build exit 0; CI green including the new step.

- [ ] **Step 6: PR** — `gh pr create` from `feat/telegram-app`. Body (global PR evidence rules): `go test ./...` output with exit status, build output, CI link, screenshots (labeled macOS/Quartz), list of manually-E2E-verified flows (real login, session reuse, realtime receive, send-echo single-append, logout/re-login, photo render+cache) and the explicit note that E2E is manual.

---

### Task 12: TestGIRDirs graceful skip on macOS/brew (SEPARATE PR)

**Files:**
- Modify: `gir/pkgconfig/pkgconfig_test.go`
- Branch: `git checkout main && git checkout -b fix/girdirs-skip-macos`

- [ ] **Step 1: Reproduce** — `go test ./gir/pkgconfig/ -run TestGIRDirs -v` on macOS/brew: FAIL `could not resolve install prefix for packages: [gtk4 pango cairo glib-2.0]`.
- [ ] **Step 2: Guarded skip.** Read the test; immediately before its "Did not expect an error" assertion insert:

```go
	if err != nil && runtime.GOOS == "darwin" &&
		strings.Contains(err.Error(), "could not resolve install prefix") {
		t.Skipf("homebrew installs packages under per-keg prefixes; no common install prefix: %v", err)
	}
```

(imports: `runtime`, `strings`; Linux CI path unchanged.)
- [ ] **Step 3: Verify** — macOS: SKIP with reason; `go vet ./gir/...` clean.
- [ ] **Step 4: Commit + PR** — `git commit -am "test(pkgconfig): skip TestGIRDirs on darwin when no common prefix resolves"`; push; PR body: before/after output.

---

## Follow-ups (explicitly NOT in this plan)

- Upstream `asyncPic` to `pkg/gota` as `AsyncImage`, citing examples/telegram as the consumer (post-v1, own PR).
- Update-gap recovery via `updates.Manager` + persisted state (v2).
- History pagination (v2).

## Self-Review Notes

- All 4 plan-review findings folded in: gotd wont-compile (dispatcher value) fixed at Tasks 4/8; channel photo refresh (ChannelsGetMessages + PhotoRef.Kind/PeerID) at Task 10; the 11 gota wont-compiles rewritten against the verified API (Global Constraints documents it); T13-deadlock test replaced by a fake-idle-that-runs-f; scoping/stale-capture/pump-leak fixed by the hoisted restartable start (Task 6) with per-pump ctx teardown and call-time `cl := client` captures; optimistic/random_id machinery cut (Send→Sender + consumeSent, unit-tested incl. UpdateShortSentMessage); goimports ordered before the CI push; spike gates the triad at Task 4; platform gate at Task 1; LockOSThread everywhere; session chmod 0600 in code; screenshots labeled.
- Type consistency: `model.*` (Task 2) consumed by name throughout; `pickSizeOrEmpty` seam (Task 7) explicitly replaced in Task 10; `bindMessageCell` signature change (Task 9 → Task 10) called out in Task 10's modify list.
- Spec deviations (spec amended in the same commit as this plan): bare dispatcher instead of updates.Manager; no optimistic rows (send-echo append); async image app-local instead of gota-first.
