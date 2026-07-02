# Telegram client on gotk4 — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `examples/telegram`, a real Telegram client (gotd MTProto + gotk4 GTK4 UI) proving the bindings end-to-end, per the approved spec `docs/superpowers/specs/2026-07-02-telegram-gotk4-design.md`.

**Architecture:** Direct gotd + thin event glue. A background goroutine holds `client.Run`; all RPC happens inside its callback lifetime; every UI mutation goes through `coreglib.IdleAdd`. Views live in one `gtk.Stack` (setup → login → main). List panes are `gtk.ListView` backed by `gioutil.NewListModel[T]`.

**Tech Stack:** Go 1.26, `github.com/diamondburned/gotk4/pkg` (local replace), `github.com/gotd/td`, `github.com/gotd/contrib` (floodwait), `pkg/core/gioutil`, `pkg/gota`.

## Global Constraints

- App module path: `github.com/diamondburned/gotk4/examples/telegram`; `go 1.26`; `replace github.com/diamondburned/gotk4/pkg => ../../pkg`.
- Config dir `os.UserConfigDir()/gotk4-telegram` mode 0700; config.json and session.json mode 0600. Cache dir `os.UserCacheDir()/gotk4-telegram` mode 0700.
- Session file = highest-value secret. Never log phone/code/password; error UI never echoes them; 2FA password never persisted; gotd logger off.
- All GTK label text: plain text only (no Pango markup) for message bodies and previews.
- Photo cache filenames locally generated (`<photoID hex>_<size letter>.jpg`); assert final path within cache root; download cap 10 MB.
- gotd calls only in goroutines; UI mutation only via `coreglib.IdleAdd`.
- Out of scope v1: history pagination, multiline composer, posting to broadcast channels, persisted peer/updates state, sign-up, keychain.
- macOS dev builds: `export PKG_CONFIG_PATH="$(brew --prefix)/lib/pkgconfig:$(brew --prefix)/share/pkgconfig:$PKG_CONFIG_PATH"` before any `go build/test/run` (applies to EVERY command in this plan).
- Import alias convention: our wrapper package is `internal/tg`; gotd's generated schema package is imported as `tgapi "github.com/gotd/td/tg"`.
- Branch: `feat/telegram-app` for all app tasks. Task 13 (gota widget) goes on branch `feat/gota-async-image` (pkg module, separate PR). Task 19 (TestGIRDirs) goes on branch `fix/girdirs-skip-macos` (separate PR).
- Docs to consult when signatures don't compile as written here (plan code is from review notes, verify against): pkg.go.dev/github.com/gotd/td/{telegram,telegram/auth,telegram/updates,telegram/message,telegram/downloader,telegram/query,session,tg} and gotd examples in github.com/gotd/td/examples.

## File Structure

```
examples/telegram/
  go.mod, go.sum
  main.go                      # gtk.Application + page wiring
  internal/config/config.go    # Config Load/Save
  internal/config/config_test.go
  internal/model/model.go      # pure view models: Dialog, Message, PromptKind, Event
  internal/tg/auth.go          # UIAuth channel-driven authenticator
  internal/tg/auth_test.go
  internal/tg/backoff.go       # pure backoff
  internal/tg/backoff_test.go
  internal/tg/client.go        # lifecycle: Run loop, options, updates manager wiring
  internal/tg/peers.go         # peer store (ID → InputPeer)
  internal/tg/peers_test.go
  internal/tg/dialogs.go       # fetch + map dialogs
  internal/tg/dialogs_test.go
  internal/tg/messages.go      # history, send, mapping, dedup state
  internal/tg/messages_test.go
  internal/tg/updates.go       # dispatcher → model.Event channel
  internal/tg/updates_test.go
  internal/tg/photos.go        # size pick, cache path, download
  internal/tg/photos_test.go
  internal/ui/setup.go         # api_id/api_hash form
  internal/ui/login.go         # phone/code/password pages
  internal/ui/chat.go          # sidebar + messages + composer + banner
  internal/ui/photo.go         # message photo cell (uses gota AsyncImage)
pkg/gota/asyncimage.go         # Task 13 (pkg module)
pkg/gota/asyncimage_test.go
.github/workflows/qa.yml       # Task 17 modify
gir/pkgconfig/pkgconfig_test.go# Task 19 modify
```

---

### Task 1: Module scaffold + hello window

**Files:**
- Create: `examples/telegram/go.mod`, `examples/telegram/main.go`

**Interfaces:**
- Produces: runnable `go build ./...` module every later task compiles inside.

- [ ] **Step 1: Scaffold module**

```bash
cd examples/telegram 2>/dev/null || mkdir -p examples/telegram && cd examples/telegram
go mod init github.com/diamondburned/gotk4/examples/telegram
go mod edit -replace github.com/diamondburned/gotk4/pkg=../../pkg
go get github.com/diamondburned/gotk4/pkg@v0.0.0-00010101000000-000000000000 || go get github.com/diamondburned/gotk4/pkg
```

(If the last command errors, add `require github.com/diamondburned/gotk4/pkg v0.0.0` to go.mod manually and run `go mod tidy` after Step 2.)

- [ ] **Step 2: Write minimal window**

```go
// main.go
package main

import (
	"os"

	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

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

- [ ] **Step 3: Verify build + run**

Run: `go mod tidy && go build ./...`
Expected: exit 0.
Run: `go run .` — window "hello gotk4" appears; close it.

- [ ] **Step 4: Commit**

```bash
git add examples/telegram && git commit -m "feat(examples/telegram): scaffold module with hello window"
```

---

### Task 2: config package

**Files:**
- Create: `examples/telegram/internal/config/config.go`, `config_test.go`

**Interfaces:**
- Produces: `type Config struct { APIID int; APIHash string }`; `func Load(dir string) (*Config, error)` (returns `fs.ErrNotExist`-wrapped when absent); `func Save(dir string, c *Config) error`; `func DefaultDir() (string, error)`; `func DefaultCacheDir() (string, error)`.

- [ ] **Step 1: Write failing test**

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

- [ ] **Step 2: Run, verify FAIL** — `go test ./internal/config/ -v` → undefined: Load.

- [ ] **Step 3: Implement**

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

- [ ] **Step 4: Run, verify PASS** — `go test ./internal/config/ -v`.

- [ ] **Step 5: Commit** — `git add examples/telegram/internal/config && git commit -m "feat(examples/telegram): config load/save with 0700/0600 perms"`

---

### Task 3: model package (pure view models + events)

**Files:**
- Create: `examples/telegram/internal/model/model.go`

**Interfaces:**
- Produces (used by every later task):

```go
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
	ID       int
	PeerID   int64
	Sender   string
	Text     string // plain text; placeholders like "[photo]" for media
	Out      bool
	Pending  bool  // optimistic, not yet confirmed
	Failed   bool
	LocalKey int64 // random_id for pending rows, 0 otherwise
	Photo    *PhotoRef
}

type PhotoRef struct {
	PhotoID    int64
	AccessHash int64
	FileRef    []byte
	SizeType   string // Telegram size letter, e.g. "x"
	MsgID      int    // for FILE_REFERENCE_EXPIRED refetch
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
	EventAuthNeeded   // session absent/invalid → show login
	EventNewMessage
	EventSendFailed
	EventError
)

type Event struct {
	Kind    EventKind
	Msg     Message // EventNewMessage / EventSendFailed
	Err     string  // EventError (already sanitized, no secrets)
}
```

- [ ] **Step 1: Create the file with exactly the content above** (package of pure data, no tests needed — no logic).
- [ ] **Step 2: Verify** — `go build ./...` → exit 0.
- [ ] **Step 3: Commit** — `git commit -am "feat(examples/telegram): view model types"`

---

### Task 4: gotd deps + channel-driven authenticator

**Files:**
- Create: `examples/telegram/internal/tg/auth.go`, `auth_test.go`

**Interfaces:**
- Consumes: `model.PromptKind`.
- Produces: `type Prompt struct { Kind model.PromptKind; Reply chan string }`; `type UIAuth struct { Prompts chan Prompt }`; `func NewUIAuth() *UIAuth`; UIAuth implements `auth.UserAuthenticator` (Phone/Code/Password block per step; AcceptTermsOfService → nil; SignUp → error).

- [ ] **Step 1: Add gotd deps**

```bash
cd examples/telegram && go get github.com/gotd/td@latest github.com/gotd/contrib@latest && go mod tidy
```

- [ ] **Step 2: Write failing test**

```go
// auth_test.go
package tg

import (
	"context"
	"testing"
	"time"

	"github.com/diamondburned/gotk4/examples/telegram/internal/model"
)

// scripted fake UI: answers each prompt kind with a canned string.
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

- [ ] **Step 3: Run, verify FAIL** — `go test ./internal/tg/ -v` → undefined: NewUIAuth.

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

(If `auth.UserAuthenticator` method signatures differ from the above, adapt to the interface — the `var _` line pins it at compile time.)

- [ ] **Step 5: Run, verify PASS** — `go test ./internal/tg/ -v`.
- [ ] **Step 6: Commit** — `git add -A examples/telegram && git commit -m "feat(examples/telegram): channel-driven gotd authenticator"`

---

### Task 5: backoff + client lifecycle

**Files:**
- Create: `examples/telegram/internal/tg/backoff.go`, `backoff_test.go`, `client.go`

**Interfaces:**
- Consumes: `UIAuth`, `config.Config`, `model.Event`.
- Produces: `func NextBackoff(prev time.Duration) time.Duration`; `type Client struct { Auth *UIAuth; Events chan model.Event; ... }`; `func NewClient(cfg config.Config, dataDir string) *Client`; `func (c *Client) Run(ctx context.Context) error` (blocking loop; re-enters on failure with backoff); `func (c *Client) Ready() bool`; `func (c *Client) Logout(ctx context.Context) error`; internal accessors used by later tasks: `c.api() *tgapi.Client`, `c.sender() *message.Sender`, `c.self() *tgapi.User`.

- [ ] **Step 1: Failing backoff test**

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

- [ ] **Step 2: Run, FAIL** — undefined: NextBackoff.

- [ ] **Step 3: Implement backoff**

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

- [ ] **Step 4: Run backoff test, PASS.**

- [ ] **Step 5: Implement lifecycle (compile-verified, exercised manually in Task 8's E2E)**

```go
// client.go
package tg

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gotd/contrib/middleware/floodwait"
	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/telegram/updates"
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
	peers  *peerStore
	chats  *chatStates
	ready  atomic.Bool
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

func (c *Client) api() *tgapi.Client        { c.mu.Lock(); defer c.mu.Unlock(); return c.rawAPI }
func (c *Client) sender() *message.Sender   { c.mu.Lock(); defer c.mu.Unlock(); return c.snd }
func (c *Client) self() *tgapi.User         { c.mu.Lock(); defer c.mu.Unlock(); return c.me }

// Run blocks; owns the gotd lifetime. Re-enters telegram.Client.Run with
// capped backoff until ctx is canceled.
func (c *Client) Run(ctx context.Context) error {
	backoff := time.Duration(0)
	for {
		err := c.runOnce(ctx)
		c.ready.Store(false)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		backoff = NextBackoff(backoff)
		c.Events <- model.Event{Kind: model.EventConnecting}
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (c *Client) runOnce(ctx context.Context) error {
	dispatcher := tgapi.NewUpdateDispatcher()
	manager := updates.New(updates.Config{Handler: dispatcher})

	client := telegram.NewClient(c.cfg.APIID, c.cfg.APIHash, telegram.Options{
		SessionStorage: &session.FileStorage{Path: c.SessionPath()},
		UpdateHandler:  manager,
		Middlewares:    []telegram.Middleware{floodwait.NewSimpleWaiter()},
	})

	return client.Run(ctx, func(ctx context.Context) error {
		status, err := client.Auth().Status(ctx)
		if err != nil {
			return err
		}
		if !status.Authorized {
			c.Events <- model.Event{Kind: model.EventAuthNeeded}
			flow := auth.NewFlow(c.Auth, auth.SendCodeOptions{})
			if err := flow.Run(ctx, client.Auth()); err != nil {
				return err
			}
		}
		me, err := client.Self(ctx)
		if err != nil {
			return err
		}

		c.mu.Lock()
		c.rawAPI = tgapi.NewClient(client)
		c.snd = message.NewSender(c.rawAPI)
		c.me = me
		c.mu.Unlock()

		c.bindUpdates(dispatcher) // Task 10

		go func() {
			_ = manager.Run(ctx, c.api(), me.ID, updates.AuthOptions{})
		}()

		c.ready.Store(true)
		c.Events <- model.Event{Kind: model.EventReady}
		<-ctx.Done()
		return ctx.Err()
	})
}
```

Add a temporary stub so this compiles until Tasks 6/9/10 land:

```go
// stubs.go — DELETED as Tasks 6, 9, 10 replace each piece.
package tg

import tgapi "github.com/gotd/td/tg"

type peerStore struct{}
func newPeerStore() *peerStore { return &peerStore{} }

type chatStates struct{}
func newChatStates() *chatStates { return &chatStates{} }

func (c *Client) bindUpdates(*tgapi.UpdateDispatcher) {}
func (c *Client) Logout(ctx context.Context) error { return nil }
```

(Move the `context` import accordingly. If `updates.New`, `manager.Run`, or `client.Self` signatures differ, follow pkg.go.dev/github.com/gotd/td/telegram/updates — the wiring pattern in the spec §"Peers, dialogs, updates" is the contract.)

- [ ] **Step 6: Verify** — `go build ./... && go test ./internal/tg/ -v` → build exit 0, tests pass.
- [ ] **Step 7: Commit** — `git add -A examples/telegram && git commit -m "feat(examples/telegram): gotd client lifecycle with backoff re-entry"`

---

### Task 6: peer store

**Files:**
- Create: `examples/telegram/internal/tg/peers.go`, `peers_test.go`
- Modify: `examples/telegram/internal/tg/stubs.go` (delete peerStore stub)

**Interfaces:**
- Produces: `func newPeerStore() *peerStore`; `func (s *peerStore) Add(users []tgapi.UserClass, chats []tgapi.ChatClass)`; `func (s *peerStore) InputPeer(kind model.PeerKind, id int64) (tgapi.InputPeerClass, bool)`; `func (s *peerStore) Title(kind model.PeerKind, id int64) string`; `func (s *peerStore) CanSend(kind model.PeerKind, id int64) bool` (false for broadcast channels).

- [ ] **Step 1: Failing test**

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
	if _, ok := s.InputPeer(model.PeerUser, 999); ok {
		t.Error("unknown peer must be !ok")
	}
}
```

- [ ] **Step 2: Run, FAIL** (methods undefined on stub).

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
		title := strings.TrimSpace(u.FirstName + " " + u.LastName)
		s.m[model.PeerUser][u.ID] = peerInfo{
			input:   &tgapi.InputPeerUser{UserID: u.ID, AccessHash: u.AccessHash},
			title:   title,
			canSend: true,
		}
	}
	for _, cc := range chats {
		switch c := cc.(type) {
		case *tgapi.Chat:
			s.m[model.PeerChat][c.ID] = peerInfo{
				input:   &tgapi.InputPeerChat{ChatID: c.ID},
				title:   c.Title,
				canSend: true,
			}
		case *tgapi.Channel:
			s.m[model.PeerChannel][c.ID] = peerInfo{
				input:   &tgapi.InputPeerChannel{ChannelID: c.ID, AccessHash: c.AccessHash},
				title:   c.Title,
				canSend: !c.Broadcast,
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

func (s *peerStore) Title(kind model.PeerKind, id int64) string {
	p, _ := s.get(kind, id)
	return p.title
}

func (s *peerStore) CanSend(kind model.PeerKind, id int64) bool {
	p, ok := s.get(kind, id)
	return ok && p.canSend
}
```

Delete the `peerStore` stub from `stubs.go`.

- [ ] **Step 4: Run, PASS** — `go test ./internal/tg/ -run TestPeerStore -v`.
- [ ] **Step 5: Commit** — `git add -A examples/telegram && git commit -m "feat(examples/telegram): peer store with access-hash tracking"`

---

### Task 7: dialogs fetch + mapping

**Files:**
- Create: `examples/telegram/internal/tg/dialogs.go`, `dialogs_test.go`

**Interfaces:**
- Consumes: `peerStore`, `model.Dialog`.
- Produces: `func mapDialogs(s *peerStore, dialogs []tgapi.DialogClass, msgs []tgapi.MessageClass, users []tgapi.UserClass, chats []tgapi.ChatClass) []model.Dialog` (pure); `func (c *Client) Dialogs(ctx context.Context) ([]model.Dialog, error)` (fetch top 50, feeds peer store).

- [ ] **Step 1: Failing test for the pure mapper**

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
		&tgapi.Message{ID: 200, Message: "", Media: &tgapi.MessageMediaPhoto{}, PeerID: &tgapi.PeerChannel{ChannelID: 3}},
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

- [ ] **Step 2: Run, FAIL** — undefined: mapDialogs.

- [ ] **Step 3: Implement**

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
	api := c.api()
	res, err := api.MessagesGetDialogs(ctx, &tgapi.MessagesGetDialogsRequest{
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

- [ ] **Step 4: Run, PASS** — `go test ./internal/tg/ -run TestMapDialogs -v`; `go build ./...`.
- [ ] **Step 5: Commit** — `git commit -am "feat(examples/telegram): dialog fetch and mapping"`

---

### Task 8: setup + login UI, wire lifecycle, REAL LOGIN E2E

**Files:**
- Create: `examples/telegram/internal/ui/setup.go`, `internal/ui/login.go`
- Modify: `examples/telegram/main.go`

**Interfaces:**
- Consumes: `config`, `tg.Client`, `tg.Prompt`, `model.Event`, `gota` widgets, `coreglib.IdleAdd`.
- Produces: `ui.NewSetupPage(onSave func(id int, hash string)) gtk.Widgetter`; `ui.NewLoginPage() (*LoginPage)` with `(*LoginPage).Show(p tg.Prompt)` (renders matching prompt, sends reply) and `.Widget() gtk.Widgetter`; main.go owns the `gtk.Stack` with names `"setup"`, `"login"`, `"main"`.

- [ ] **Step 1: Setup page**

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
	idEntry := gota.Entry().Placeholder("api_id (digits)")
	hashEntry := gota.Entry().Placeholder("api_hash")
	errLabel := gota.Label("").Wrap(true)

	submit := func() {
		id, err := strconv.Atoi(idEntry.Text())
		if err != nil || id <= 0 || hashEntry.Text() == "" {
			errLabel.SetText("api_id must be a positive number; api_hash must be non-empty")
			return
		}
		onSave(id, hashEntry.Text())
	}

	return gota.VBox(
		gota.Label("Telegram API credentials").Class("title-2"),
		gota.Label("Create them at my.telegram.org → API development tools"),
		idEntry, hashEntry,
		gota.Button("Save & connect").OnClick(submit),
		errLabel,
	).Spacing(8).Margin(24).Widget()
}
```

(Adapt the exact gota builder calls to `pkg/gota/widgets.go` — `Entry()`, `.Placeholder`, `.Text`, `.OnEnter`, `.PasswordMode`, `Label`, `Button().OnClick`, `VBox` exist per the capability review; if a chained method is missing use the underlying gtk widget it wraps.)

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
	box    gtk.Widgetter
	label  *gota.LabelWidget
	entry  *gota.EntryWidget
	submit func()
}

func NewLoginPage() *LoginPage {
	p := &LoginPage{}
	p.label = gota.Label("")
	p.entry = gota.Entry().OnEnter(func() {
		if p.submit != nil {
			p.submit()
		}
	})
	btn := gota.Button("Next").OnClick(func() {
		if p.submit != nil {
			p.submit()
		}
	})
	p.box = gota.VBox(p.label, p.entry, btn).Spacing(8).Margin(24).Widget()
	return p
}

func (p *LoginPage) Widget() gtk.Widgetter { return p.box }

// Show configures the page for one auth prompt. Must be called on the UI thread.
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
		v := p.entry.Text()
		if v == "" {
			return
		}
		p.entry.SetText("")
		go func() { pr.Reply <- v }() // never block the UI thread
	}
}
```

- [ ] **Step 3: Wire main.go**

```go
// main.go (replace body)
package main

import (
	"context"
	"errors"
	"io/fs"
	"log"
	"os"

	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/diamondburned/gotk4/examples/telegram/internal/config"
	"github.com/diamondburned/gotk4/examples/telegram/internal/model"
	"github.com/diamondburned/gotk4/examples/telegram/internal/tg"
	"github.com/diamondburned/gotk4/examples/telegram/internal/ui"
)

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
	stack.AddNamed(gtk.NewLabel("connected — chat UI lands in Task 11"), "main")
	w.SetChild(stack)
	w.SetVisible(true)

	start := func(cfg config.Config) {
		client := tg.NewClient(cfg, dir)
		ctx := context.Background()

		go func() { // event pump: tg goroutines → UI
			for ev := range client.Events {
				ev := ev
				coreglib.IdleAdd(func() {
					switch ev.Kind {
					case model.EventAuthNeeded:
						stack.SetVisibleChildName("login")
					case model.EventReady:
						stack.SetVisibleChildName("main")
					}
				})
			}
		}()
		go func() { // auth prompt pump
			for pr := range client.Auth.Prompts {
				pr := pr
				coreglib.IdleAdd(func() {
					login.Show(pr)
					stack.SetVisibleChildName("login")
				})
			}
		}()
		go func() {
			if err := client.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
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

- [ ] **Step 4: Build** — `go build ./...` → exit 0.

- [ ] **Step 5: MANUAL E2E GATE — real login**

Run: `go run .`
1. Setup page appears → enter real api_id/api_hash (my.telegram.org).
2. Login page: phone → code (→ 2FA if set).
3. "connected" page appears.
4. Quit, `go run .` again → goes straight to "connected" (session persisted).
5. `ls -l "$(getconf DARWIN_USER_DIR 2>/dev/null)" 2>/dev/null; ls -la ~/Library/Application\ Support/gotk4-telegram/` → dir 700, files 600.

Screenshot the connected window → save for PR evidence.

- [ ] **Step 6: Commit** — `git add -A examples/telegram && git commit -m "feat(examples/telegram): setup + login UI with real auth flow"`

---

### Task 9: messages — history, send, dedup state

**Files:**
- Create: `examples/telegram/internal/tg/messages.go`, `messages_test.go`
- Modify: `stubs.go` (delete chatStates stub)

**Interfaces:**
- Consumes: `peerStore`, `model.Message`.
- Produces:
  - `func mapMessage(s *peerStore, selfID int64, m tgapi.MessageClass) (model.Message, bool)` (pure; media→placeholder text + PhotoRef for photos).
  - `type chatStates` with `func (cs *chatStates) Note(peerID int64, msgID int) bool` (true = first sighting, false = already seen) and `func (cs *chatStates) Reconcile(peerID int64, randomID int64, realID int) bool` (true = a pending row with that LocalKey should be finalized).
  - `func (c *Client) History(ctx context.Context, kind model.PeerKind, peerID int64) ([]model.Message, error)` (latest 50, oldest first).
  - `func (c *Client) Send(ctx context.Context, kind model.PeerKind, peerID int64, text string) (randomID int64, err error)`.
  - `func (c *Client) MarkRead(ctx context.Context, kind model.PeerKind, peerID int64) error`.

- [ ] **Step 1: Failing tests**

```go
// messages_test.go
package tg

import (
	"testing"

	tgapi "github.com/gotd/td/tg"
)

func TestMapMessagePhotoAndText(t *testing.T) {
	s := newPeerStore()
	s.Add([]tgapi.UserClass{&tgapi.User{ID: 7, FirstName: "Bea"}}, nil)

	plain := &tgapi.Message{ID: 1, Message: "yo", FromID: &tgapi.PeerUser{UserID: 7}, PeerID: &tgapi.PeerUser{UserID: 7}}
	m, ok := mapMessage(s, 99, plain)
	if !ok || m.Text != "yo" || m.Sender != "Bea" || m.Out {
		t.Fatalf("plain = %+v ok=%v", m, ok)
	}

	photo := &tgapi.Message{ID: 2, PeerID: &tgapi.PeerUser{UserID: 7},
		Media: &tgapi.MessageMediaPhoto{Photo: &tgapi.Photo{ID: 555, AccessHash: 556, FileReference: []byte{1}}}}
	pm, ok := mapMessage(s, 99, photo)
	if !ok || pm.Text != "[photo]" || pm.Photo == nil || pm.Photo.PhotoID != 555 || pm.Photo.MsgID != 2 {
		t.Fatalf("photo = %+v", pm)
	}

	own := &tgapi.Message{ID: 3, Message: "mine", Out: true, PeerID: &tgapi.PeerUser{UserID: 7}}
	om, _ := mapMessage(s, 99, own)
	if !om.Out {
		t.Fatal("own message must be Out")
	}
}

func TestChatStatesDedupAndReconcile(t *testing.T) {
	cs := newChatStates()
	if !cs.Note(1, 100) {
		t.Fatal("first sighting must be true")
	}
	if cs.Note(1, 100) {
		t.Fatal("second sighting must be false")
	}
	if !cs.Note(2, 100) {
		t.Fatal("same id in another chat is independent")
	}

	cs.Pending(1, 777) // register optimistic random_id
	if !cs.Reconcile(1, 777, 101) {
		t.Fatal("reconcile of known random_id must be true")
	}
	if cs.Reconcile(1, 777, 101) {
		t.Fatal("reconcile is one-shot")
	}
	if cs.Note(1, 101) {
		t.Fatal("reconciled id must already count as seen")
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
	"crypto/rand"
	"encoding/binary"
	"sync"

	"github.com/gotd/td/telegram/message"
	tgapi "github.com/gotd/td/tg"

	"github.com/diamondburned/gotk4/examples/telegram/internal/model"
)

// ---- pure mapping ----

func mapMessage(s *peerStore, selfID int64, mc tgapi.MessageClass) (model.Message, bool) {
	m, ok := mc.(*tgapi.Message)
	if !ok {
		return model.Message{}, false
	}
	_, peerID := peerKey(m.PeerID)
	out := model.Message{ID: m.ID, PeerID: peerID, Text: m.Message, Out: m.Out}
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
				out.Photo = &model.PhotoRef{
					PhotoID: p.ID, AccessHash: p.AccessHash,
					FileRef: p.FileReference, MsgID: m.ID,
				}
			}
		case *tgapi.MessageMediaDocument:
			out.Text = "[file]"
		default:
			out.Text = "[media]"
		}
	}
	return out, true
}

// ---- dedup / optimistic-send state ----

type chatStates struct {
	mu      sync.Mutex
	seen    map[int64]map[int]struct{}   // peerID → msg IDs
	pending map[int64]map[int64]struct{} // peerID → random_ids
}

func newChatStates() *chatStates {
	return &chatStates{seen: map[int64]map[int]struct{}{}, pending: map[int64]map[int64]struct{}{}}
}

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

func (cs *chatStates) Pending(peerID, randomID int64) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if cs.pending[peerID] == nil {
		cs.pending[peerID] = map[int64]struct{}{}
	}
	cs.pending[peerID][randomID] = struct{}{}
}

func (cs *chatStates) Reconcile(peerID, randomID int64, realID int) bool {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if _, ok := cs.pending[peerID][randomID]; !ok {
		return false
	}
	delete(cs.pending[peerID], randomID)
	if cs.seen[peerID] == nil {
		cs.seen[peerID] = map[int]struct{}{}
	}
	cs.seen[peerID][realID] = struct{}{}
	return true
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
	for i := len(raw) - 1; i >= 0; i-- { // API returns newest-first → reverse
		if m, ok := mapMessage(c.peers, selfID, raw[i]); ok {
			c.chats.Note(peerID, m.ID)
			out = append(out, m)
		}
	}
	return out, nil
}

func newRandomID() int64 {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return int64(binary.LittleEndian.Uint64(b[:]) &^ (1 << 63))
}

func (c *Client) Send(ctx context.Context, kind model.PeerKind, peerID int64, text string) (int64, error) {
	peer, ok := c.peers.InputPeer(kind, peerID)
	if !ok {
		return 0, errUnknownPeer
	}
	randomID := newRandomID()
	c.chats.Pending(peerID, randomID)
	_, err := c.api().MessagesSendMessage(ctx, &tgapi.MessagesSendMessageRequest{
		Peer: peer, Message: text, RandomID: randomID,
	})
	return randomID, err
}

func (c *Client) MarkRead(ctx context.Context, kind model.PeerKind, peerID int64) error {
	peer, ok := c.peers.InputPeer(kind, peerID)
	if !ok {
		return errUnknownPeer
	}
	if kind == model.PeerChannel {
		ch, _ := peer.(*tgapi.InputPeerChannel)
		_, err := c.api().ChannelsReadHistory(ctx, &tgapi.ChannelsReadHistoryRequest{
			Channel: &tgapi.InputChannel{ChannelID: ch.ChannelID, AccessHash: ch.AccessHash},
		})
		return err
	}
	_, err := c.api().MessagesReadHistory(ctx, &tgapi.MessagesReadHistoryRequest{Peer: peer})
	return err
}
```

Add `var errUnknownPeer = errors.New("unknown peer")` (import `errors`). Delete the `chatStates` stub from `stubs.go`. Note: raw `MessagesSendMessage` is used instead of `message.NewSender` because we need control of `RandomID` for reconciliation; drop the unused `message` import from this file if the compiler flags it (the sender stays available via `c.sender()` from Task 5).

- [ ] **Step 4: Run, PASS** — `go test ./internal/tg/ -v`; `go build ./...`.
- [ ] **Step 5: Commit** — `git commit -am "feat(examples/telegram): history, send with random_id dedup, mark-read"`

---

### Task 10: updates dispatch → events

**Files:**
- Create: `examples/telegram/internal/tg/updates.go`, `updates_test.go`
- Modify: delete `stubs.go` entirely (implement `bindUpdates` + `Logout` for real)

**Interfaces:**
- Consumes: `chatStates.Note/Reconcile`, `mapMessage`, `model.Event`.
- Produces: `func (c *Client) bindUpdates(d *tgapi.UpdateDispatcher)` emitting `model.Event{Kind: EventNewMessage}` on deduped new messages; `func (c *Client) Logout(ctx context.Context) error` (best-effort `auth.Logout` + delete session file).
- Also produces the testable pure hook: `func (c *Client) handleNewMessage(mc tgapi.MessageClass)`.

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
	c.handleNewMessage(msg) // duplicate — must be dropped

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

- [ ] **Step 2: Run, FAIL** — undefined: handleNewMessage.

- [ ] **Step 3: Implement**

```go
// updates.go
package tg

import (
	"context"
	"os"

	"github.com/gotd/td/telegram/auth"
	tgapi "github.com/gotd/td/tg"

	"github.com/diamondburned/gotk4/examples/telegram/internal/model"
)

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
		return // already seen (e.g. our own send echo)
	}
	select {
	case c.Events <- model.Event{Kind: model.EventNewMessage, Msg: m}:
	default: // never block gotd's update goroutine on a full UI queue
	}
}

func (c *Client) bindUpdates(d *tgapi.UpdateDispatcher) {
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

// Logout best-effort revokes the session server-side, then deletes the
// session file. Caller restarts the client afterwards.
func (c *Client) Logout(ctx context.Context) error {
	if api := c.api(); api != nil {
		_, _ = api.AuthLogOut(ctx) // best-effort
	}
	_ = auth.Flow{} // keep the import honest if AuthLogOut path changes
	return os.Remove(c.SessionPath())
}
```

Delete `stubs.go`. (If `tgapi.Entities` field names differ (`Users`/`Chats`/`Channels` maps), adapt `addEntities` to the actual struct — it's a map of ID→entity in current gotd. Remove the `auth.Flow{}` line if the `auth` import is otherwise used or unused.)

- [ ] **Step 4: Run, PASS** — `go test ./internal/tg/ -v`; `go build ./...`.
- [ ] **Step 5: Commit** — `git commit -am "feat(examples/telegram): update dispatch with dedup + logout"`

---

### Task 11: chat UI — sidebar, message pane, composer

**Files:**
- Create: `examples/telegram/internal/ui/chat.go`
- Modify: `examples/telegram/main.go` (replace "main" placeholder page)

**Interfaces:**
- Consumes: `gioutil.NewListModel[T]` (`pkg/core/gioutil/listmodel.go`: `NewListModel[T any]()`, `.Append`, `.Splice`, `.At`, `.Len`, `.EmitChanged`), `tg.Client.Dialogs/History/Send/MarkRead`, `model.*`.
- Produces: `ui.NewChatView(actions ChatActions) *ChatView` with:

```go
type ChatActions struct {
	LoadHistory func(d model.Dialog)                  // called on selection (goroutine inside)
	Send        func(d model.Dialog, text string)
}
type ChatView struct{ ... }
func (v *ChatView) Widget() gtk.Widgetter
func (v *ChatView) SetDialogs(ds []model.Dialog)      // UI thread
func (v *ChatView) SetMessages(d model.Dialog, ms []model.Message) // UI thread
func (v *ChatView) AppendMessage(m model.Message)     // UI thread; routes to open chat + sidebar preview
func (v *ChatView) Current() (model.Dialog, bool)
func (v *ChatView) SetBanner(text string)             // "" hides (gtk.Revealer)
```

- [ ] **Step 1: Implement ChatView**

```go
// chat.go
package ui

import (
	"github.com/diamondburned/gotk4/pkg/core/gioutil"
	"github.com/diamondburned/gotk4/pkg/gota"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/diamondburned/gotk4/examples/telegram/internal/model"
)

type ChatActions struct {
	LoadHistory func(d model.Dialog)
	Send        func(d model.Dialog, text string)
}

type ChatView struct {
	root     gtk.Widgetter
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

	// --- banner ---
	v.bannerLb = gtk.NewLabel("")
	v.banner = gtk.NewRevealer()
	v.banner.SetChild(v.bannerLb)
	v.banner.SetRevealChild(false)

	// --- sidebar ---
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
		item.Child().(*gtk.Label).SetText(text) // plain text — no markup
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

	// --- message pane ---
	msgFactory := gtk.NewSignalListItemFactory()
	msgFactory.ConnectSetup(func(obj *coreglib.Object) {
		item := obj.Cast().(*gtk.ListItem)
		lb := gtk.NewLabel("")
		lb.SetXAlign(0)
		lb.SetWrap(true)
		item.SetChild(lb)
	})
	msgFactory.ConnectBind(func(obj *coreglib.Object) {
		item := obj.Cast().(*gtk.ListItem)
		m := gioutil.ObjectValue[model.Message](item.Item())
		prefix := m.Sender
		if m.Out {
			prefix = "me"
		}
		suffix := ""
		if m.Pending {
			suffix = " …"
		}
		if m.Failed {
			suffix = " ⚠ failed"
		}
		item.Child().(*gtk.Label).SetText(prefix + ": " + m.Text + suffix)
	})
	msgSel := gtk.NewNoSelection(v.messages.ListModel)
	msgList := gtk.NewListView(msgSel, &msgFactory.ListItemFactory)

	// --- composer ---
	composer := gota.Entry().Placeholder("Message")
	composer.OnEnter(func() {
		text := composer.Text()
		if text == "" || !v.hasCurrent || !v.current.CanSend {
			return
		}
		composer.SetText("")
		actions.Send(v.current, text)
	})

	right := gota.VBox(
		gota.Scrolled(msgList).Expand(true),
		composer,
	).Widget()

	pane := gtk.NewPaned(gtk.OrientationHorizontal)
	pane.SetStartChild(gota.Scrolled(dlgList).Widget())
	pane.SetEndChild(right)
	pane.SetPosition(280)

	v.root = gota.VBox(v.banner, pane).Widget()
	return v
}

func (v *ChatView) Widget() gtk.Widgetter { return v.root }

func (v *ChatView) SetBanner(text string) {
	v.bannerLb.SetText(text)
	v.banner.SetRevealChild(text != "")
}

func (v *ChatView) Current() (model.Dialog, bool) { return v.current, v.hasCurrent }

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

Imports to add: `coreglib "github.com/diamondburned/gotk4/pkg/core/glib"`, `"github.com/diamondburned/gotk4/pkg/pango"`, `"strconv"`. Adapt to real gioutil/gota APIs (`ListModel` embedding, `ObjectValue`, `Set`) per `pkg/core/gioutil/listmodel.go` — the capability review confirmed `Append/Remove/Splice/Set/SetSilent/EmitChanged/At/Len` exist.

- [ ] **Step 2: Wire into main.go** — replace the `"main"` placeholder:

```go
	chat := ui.NewChatView(ui.ChatActions{
		LoadHistory: func(d model.Dialog) {
			go func() {
				ms, err := client.History(ctx, d.Kind, d.PeerID)
				if err != nil {
					return
				}
				_ = client.MarkRead(ctx, d.Kind, d.PeerID)
				coreglib.IdleAdd(func() { chat.SetMessages(d, ms) })
			}()
		},
		Send: func(d model.Dialog, text string) {
			go func() {
				_, err := client.Send(ctx, d.Kind, d.PeerID, text)
				if err != nil {
					coreglib.IdleAdd(func() { chat.SetBanner("send failed") })
				}
			}()
		},
	})
	stack.AddNamed(chat.Widget(), "main")
```

And extend the event pump:

```go
				case model.EventReady:
					stack.SetVisibleChildName("main")
					go func() {
						ds, err := client.Dialogs(ctx)
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

(`client`, `ctx`, `chat`, `stack` must share scope — restructure `activate` so `chat` is created before `start` and `start` captures it.)

- [ ] **Step 3: Build + MANUAL E2E** — `go build ./... && go run .`:
1. Dialog list appears with titles/previews/unread.
2. Click a dialog → history renders oldest-top.
3. Send a message from the app → appears; check on phone.
4. Send from phone → appears in-app within seconds (updates path).
5. Unread badge clears after opening (check another client).
Screenshot main window for PR evidence.

- [ ] **Step 4: Commit** — `git add -A examples/telegram && git commit -m "feat(examples/telegram): chat view with realtime send/receive"`

---

### Task 12: logout + session-invalid recovery + optimistic send rows

**Files:**
- Modify: `examples/telegram/internal/ui/chat.go` (logout button in sidebar header), `main.go`, `internal/tg/client.go`

**Interfaces:**
- Consumes: `tg.Client.Logout`, `chatStates.Pending/Reconcile`.
- Produces: logout button → `client.Logout` → app returns to login; `AUTH_KEY_UNREGISTERED` from `runOnce` → delete session + `EventAuthNeeded`; sends render as Pending rows finalized on echo.

- [ ] **Step 1: Optimistic row.** In `main.go`'s Send action, before the goroutine: build `m := model.Message{PeerID: d.PeerID, Sender: "me", Text: text, Out: true, Pending: true, LocalKey: randomID}` — restructure so `client.Send` returns randomID first (synchronous call is fine: run `Send` in the goroutine, then `IdleAdd` the append with `Pending: true` once it returns without error; on error set `Failed: true`). In `handleNewMessage` (Task 10), before the `Note` dedup, out-messages reconcile: gotd's send echo arrives as `UpdateNewMessage` with `Out: true` — `Note` already dedupes by ID; pending row replacement: in `ChatView.AppendMessage`, if `m.Out` and the last row is `Pending && Out`, replace it (`v.messages.Set(last, m)`) instead of appending.

```go
// in AppendMessage, before the plain append:
	if m.Out && v.hasCurrent && v.current.PeerID == m.PeerID {
		for i := v.messages.Len() - 1; i >= 0; i-- {
			prev := v.messages.At(i)
			if prev.Pending && prev.Out {
				v.messages.Set(i, m)
				return
			}
		}
	}
```

- [ ] **Step 2: Session-invalid recovery.** In `client.go` `Run` loop, inspect `runOnce` error:

```go
		if isAuthKeyUnregistered(err) { // strings.Contains(err.Error(), "AUTH_KEY_UNREGISTERED")
			_ = os.Remove(c.SessionPath())
			c.Events <- model.Event{Kind: model.EventAuthNeeded}
		}
```

(gotd exposes typed RPC errors via `tgerr.Is(err, "AUTH_KEY_UNREGISTERED")` — prefer that; import `github.com/gotd/td/tgerr`.)

- [ ] **Step 3: Logout button.** Sidebar header: `gota.Button("Logout").OnClick(...)` → confirm via plain `gtk.AlertDialog` or just do it → goroutine `client.Logout(ctx)` → `IdleAdd` switch stack to "login". Cancel the client ctx and call `start` again with the same config (restructure `start` to be restartable: keep `cancel` func, `start` cancels previous run).

- [ ] **Step 4: Build + manual check** — logout returns to login page; re-login works; sent message shows `…` then finalizes.
- [ ] **Step 5: Commit** — `git commit -am "feat(examples/telegram): logout, session recovery, optimistic sends"`

---

### Task 13: gota AsyncImage widget (pkg module — SEPARATE BRANCH/PR)

**Files:**
- Create: `pkg/gota/asyncimage.go`, `pkg/gota/asyncimage_test.go`
- Branch: `git checkout main && git checkout -b feat/gota-async-image` (merge back into `feat/telegram-app` after, or rebase app branch on it).

**Interfaces:**
- Produces: `func AsyncImage() *AsyncImageWidget` (wraps `*gtk.Picture`, default size request 320×240); `func (w *AsyncImageWidget) LoadFunc(fetch func() (path string, err error))` — runs fetch in a goroutine, marshals the result via IdleAdd, sets the picture from filename on success; on error leaves the placeholder. Test seam: `w.idle func(func())` defaulting to `IdleAdd`.

- [ ] **Step 1: Read the existing patterns first** — `pkg/gota/widgets.go` (builder style) and `pkg/gota/gota_test.go` + `table_integration_test.go` (how tests initialize/skip GTK — follow the same guard so CI behavior matches).

- [ ] **Step 2: Failing test (logic-level, no display needed)**

```go
// asyncimage_test.go
package gota

import (
	"errors"
	"sync"
	"testing"
)

// TestAsyncImageLoadFuncMarshaling verifies fetch runs off-thread and the
// result is delivered through the idle hook exactly once.
func TestAsyncImageLoadFuncMarshaling(t *testing.T) {
	requireGTK(t) // same guard the existing gota tests use; adapt name

	var mu sync.Mutex
	var delivered []func()
	w := AsyncImage()
	w.idle = func(f func()) { mu.Lock(); delivered = append(delivered, f); mu.Unlock() }

	done := make(chan struct{})
	w.onLoaded = func() { close(done) }

	w.LoadFunc(func() (string, error) { return "", errors.New("nope") })
	<-done

	mu.Lock()
	defer mu.Unlock()
	if len(delivered) != 1 {
		t.Fatalf("idle deliveries = %d, want 1", len(delivered))
	}
}
```

- [ ] **Step 3: Run, FAIL** — `cd pkg && go test ./gota/ -run TestAsyncImage -v`.

- [ ] **Step 4: Implement**

```go
// asyncimage.go
package gota

import (
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// AsyncImageWidget shows a placeholder until LoadFunc delivers an image path.
type AsyncImageWidget struct {
	*gtk.Picture
	idle     func(func()) // test seam; defaults to IdleAdd
	onLoaded func()       // test seam; nil in production
}

// AsyncImage creates an async-loading picture widget.
func AsyncImage() *AsyncImageWidget {
	p := gtk.NewPicture()
	p.SetSizeRequest(320, 240)
	p.SetCanShrink(true)
	return &AsyncImageWidget{Picture: p, idle: func(f func()) { IdleAdd(f) }}
}

// LoadFunc runs fetch in a goroutine and sets the picture from the returned
// file path on the UI loop. On error the placeholder stays.
func (w *AsyncImageWidget) LoadFunc(fetch func() (string, error)) {
	go func() {
		path, err := fetch()
		w.idle(func() {
			if err == nil && path != "" {
				w.SetFilename(path)
			}
			if w.onLoaded != nil {
				w.onLoaded()
			}
		})
	}()
}
```

(Match `IdleAdd`'s actual name/signature in `pkg/gota/gota.go`; if `SetFilename` doesn't exist on `gtk.Picture`, use `gtk.NewPictureForFilename`-equivalent setter — capability review confirmed the constructors at pkg/gtk/v4/gtk.go:89805-89863.)

- [ ] **Step 5: Run, PASS** — `cd pkg && go test ./gota/ -v` (all gota tests, not just the new one).
- [ ] **Step 6: Commit + PR** — `git add pkg/gota && git commit -m "feat(gota): AsyncImage widget for goroutine-loaded pictures"`; push, open PR with test output as evidence. Rebase `feat/telegram-app` onto it (or merge) before Task 14.

---

### Task 14: photo pipeline (size pick, cache path, download)

**Files:**
- Create: `examples/telegram/internal/tg/photos.go`, `photos_test.go`

**Interfaces:**
- Consumes: `model.PhotoRef`, `telegram/downloader`.
- Produces: `func pickSize(sizes []tgapi.PhotoSizeClass) (string, bool)` (largest w ≤ 800, else smallest; only variants with real dimensions); `func cachePath(root string, photoID int64, sizeType string) (string, error)` (locally-generated name, asserts containment); `func (c *Client) DownloadPhoto(ctx context.Context, ref model.PhotoRef, cacheDir string) (string, error)` (skips if cached; 10 MB cap; one retry on FILE_REFERENCE_EXPIRED by refetching the message).

- [ ] **Step 1: Failing tests (pure parts)**

```go
// photos_test.go
package tg

import (
	"path/filepath"
	"strings"
	"testing"

	tgapi "github.com/gotd/td/tg"
)

func TestPickSize(t *testing.T) {
	sizes := []tgapi.PhotoSizeClass{
		&tgapi.PhotoStrippedSize{Type: "i"},          // no dimensions — ignored
		&tgapi.PhotoSize{Type: "m", W: 320, H: 240},
		&tgapi.PhotoSize{Type: "x", W: 800, H: 600},
		&tgapi.PhotoSize{Type: "y", W: 1280, H: 960}, // too wide
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

- [ ] **Step 2: Run, FAIL.**

- [ ] **Step 3: Implement**

```go
// photos.go
package tg

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

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
	if filepath.Dir(p) != filepath.Clean(root) || strings.Contains(name, string(os.PathSeparator)) {
		return "", fmt.Errorf("cache path escapes root")
	}
	return p, nil
}

// DownloadPhoto downloads ref into cacheDir, returning the local path.
// Cached files are reused. One retry on FILE_REFERENCE_EXPIRED.
func (c *Client) DownloadPhoto(ctx context.Context, ref model.PhotoRef, cacheDir string) (string, error) {
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

// refreshPhotoRef refetches the message to obtain a fresh file_reference.
func (c *Client) refreshPhotoRef(ctx context.Context, ref model.PhotoRef) (model.PhotoRef, error) {
	res, err := c.api().MessagesGetMessages(ctx, []tgapi.InputMessageClass{
		&tgapi.InputMessageID{ID: ref.MsgID},
	})
	if err != nil {
		return ref, err
	}
	msgs, ok := res.(*tgapi.MessagesMessages)
	if !ok {
		return ref, fmt.Errorf("unexpected getMessages result")
	}
	for _, mc := range msgs.Messages {
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

Also: in Task 9's `mapMessage`, populate `PhotoRef.SizeType` via `pickSize(p.Sizes)` (add `if st, ok := pickSize(p.Sizes); ok { ...SizeType: st... }` and skip the PhotoRef if `!ok`). Update `messages_test.go`'s photo fixture to include `Sizes: []tgapi.PhotoSizeClass{&tgapi.PhotoSize{Type: "x", W: 800, H: 600}}` and assert `SizeType == "x"`.

(A true byte-cap DURING download needs a size probe; the post-hoc stat + the ≤800px class keep v1 honest — the class bounds legitimate sizes well under 10 MB. If `downloader`'s builder differs, follow pkg.go.dev/github.com/gotd/td/telegram/downloader.)

- [ ] **Step 4: Run, PASS** — `go test ./internal/tg/ -v`; `go build ./...`.
- [ ] **Step 5: Commit** — `git commit -am "feat(examples/telegram): photo download with cache + file-reference retry"`

---

### Task 15: photos in the message pane

**Files:**
- Create: `examples/telegram/internal/ui/photo.go`
- Modify: `examples/telegram/internal/ui/chat.go` (message factory), `main.go` (pass cache dir + downloader)

**Interfaces:**
- Consumes: `gota.AsyncImage` (Task 13), `tg.Client.DownloadPhoto` (Task 14).
- Produces: `type PhotoLoader func(ref model.PhotoRef) (string, error)`; `NewChatView(actions ChatActions)` gains `actions.LoadPhoto PhotoLoader`; message cells with `m.Photo != nil` render an AsyncImage under the text.

- [ ] **Step 1: Message cell with photo**

```go
// photo.go
package ui

import (
	"github.com/diamondburned/gotk4/pkg/gota"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/diamondburned/gotk4/examples/telegram/internal/model"
)

type PhotoLoader func(ref model.PhotoRef) (string, error)

// photoCell builds a message row that may carry an image.
type photoCell struct {
	box   *gtk.Box
	label *gtk.Label
	img   *gota.AsyncImageWidget
}

func newPhotoCell() *photoCell {
	c := &photoCell{}
	c.label = gtk.NewLabel("")
	c.label.SetXAlign(0)
	c.label.SetWrap(true)
	c.box = gtk.NewBox(gtk.OrientationVertical, 4)
	c.box.Append(c.label)
	return c
}

func (c *photoCell) bind(m model.Message, load PhotoLoader) {
	// text part identical to the plain cell
	prefix := m.Sender
	if m.Out {
		prefix = "me"
	}
	c.label.SetText(prefix + ": " + m.Text)

	if c.img != nil {
		c.box.Remove(c.img)
		c.img = nil
	}
	if m.Photo != nil && load != nil {
		ref := *m.Photo
		c.img = gota.AsyncImage()
		c.box.Append(c.img)
		c.img.LoadFunc(func() (string, error) { return load(ref) })
	}
}
```

Change `chat.go`'s message factory Setup to create `newPhotoCell()` (store via `item.SetChild(c.box)` and keep the cell in a `map[uintptr]*photoCell` keyed by the ListItem, or reconstruct in Bind via child lookup), and Bind to call `cell.bind(m, actions.LoadPhoto)` — including the Pending/Failed suffix from Task 11.

- [ ] **Step 2: Wire in main.go**

```go
	cacheDir, _ := config.DefaultCacheDir()
	// in ChatActions:
	LoadPhoto: func(ref model.PhotoRef) (string, error) {
		return client.DownloadPhoto(context.Background(), ref, cacheDir)
	},
```

- [ ] **Step 3: Build + MANUAL E2E** — `go run .`; open a chat containing photos → placeholders appear, then images; second open is instant (cache hit); `ls ~/Library/Caches/gotk4-telegram/` shows `<hex>_<letter>.jpg` names only.
- [ ] **Step 4: Screenshot for evidence. Commit** — `git commit -am "feat(examples/telegram): inline photo rendering via gota AsyncImage"`

---

### Task 16: polish — empty states, failed sends, connecting banner

**Files:**
- Modify: `examples/telegram/internal/ui/chat.go`, `main.go`

- [ ] **Step 1:** Empty dialog list → `gtk.Label("No chats yet")` via `dlgList.SetPlaceholder`-equivalent (GTK4 ListView has no placeholder: instead wrap sidebar in a `gtk.Stack` toggling label/list on `SetDialogs(len==0)`). Same for empty message pane.
- [ ] **Step 2:** Send errors: `EventSendFailed` path — in main.go Send action error branch, `IdleAdd` append `model.Message{..., Failed: true}` so the row shows `⚠ failed` (Task 11 factory already renders it).
- [ ] **Step 3:** Banner text states: "connecting…" on `EventConnecting`, cleared ("" → hidden) on `EventReady`, error text on `EventError`.
- [ ] **Step 4:** Build + manual sanity run. Commit — `git commit -am "feat(examples/telegram): empty states, failed-send rows, status banner"`

---

### Task 17: CI — build the app on Linux/Nix

**Files:**
- Modify: `.github/workflows/qa.yml`

- [ ] **Step 1:** Read qa.yml; find the job that runs inside nix-shell (the one running `go generate`/tests). Add a step after the pkg tests, mirroring the surrounding step syntax exactly:

```yaml
      - name: Build examples/telegram
        run: nix-shell --run "cd examples/telegram && go build ./... && go test ./..."
```

(If existing steps use a different nix invocation — `nix develop -c`, a shared shell wrapper — copy that form instead. The git-dirty check must stay after it.)

- [ ] **Step 2:** Commit + push, verify workflow green: `git commit -am "ci: build examples/telegram in qa" && git push -u origin feat/telegram-app && gh run watch`
- [ ] **Step 3:** If the Nix pin lacks network access for `go mod download` of gotd in CI, vendor instead: `cd examples/telegram && go mod vendor && git add vendor && git commit -m "chore(examples/telegram): vendor deps for CI"` (check how CI fetches pkg deps first — if `go test` in pkg already downloads modules in CI, gotd will download fine too and vendoring is unnecessary).

---

### Task 18: README + PR with evidence

**Files:**
- Create: `examples/telegram/README.md`

- [ ] **Step 1: README** — sections: what it is (proof app for the bindings), prerequisites (GTK4 via nix-shell or brew + PKG_CONFIG_PATH line, api_id/api_hash from my.telegram.org), run (`go run .`), storage locations + permissions note (config/session 0600 in dir 0700; session file grants full account access — treat like a password), v1 scope + out-of-scope list (copy from spec), screenshots (login, chat, photos — the ones captured in Tasks 8/11/15).
- [ ] **Step 2: Full verification sweep**

```bash
cd examples/telegram && go vet ./... && go test ./... && go build ./... && cd ../..
goimports -w examples/telegram && git diff --exit-code examples/telegram
```

Expected: all exit 0, no diff after goimports.

- [ ] **Step 3: PR** — `gh pr create` from `feat/telegram-app`. Body MUST include (global PR rules): test output (`go test ./...` exit status), build output, the screenshots, and a note of what was manually E2E-verified (real login, realtime receive, photo render). State explicitly that E2E is manual.

---

### Task 19: TestGIRDirs graceful skip on macOS/brew (SEPARATE BRANCH/PR)

**Files:**
- Modify: `gir/pkgconfig/pkgconfig_test.go`
- Branch: `git checkout main && git checkout -b fix/girdirs-skip-macos`

- [ ] **Step 1: Reproduce** — `go test ./gir/pkgconfig/ -run TestGIRDirs -v` on macOS with brew GTK: FAIL `could not resolve install prefix for packages: [gtk4 pango cairo glib-2.0]`.
- [ ] **Step 2: Add guarded skip.** In the failing case's error branch (read the test first; wrap where the error is asserted):

```go
	if err != nil && runtime.GOOS == "darwin" &&
		strings.Contains(err.Error(), "could not resolve install prefix") {
		t.Skipf("homebrew installs packages under per-keg prefixes; no common install prefix: %v", err)
	}
```

(Placement: immediately before the existing `Did not expect an error` assertion; imports `runtime`, `strings`. Linux CI behavior unchanged — skip is darwin-only.)

- [ ] **Step 3: Verify both ways** — macOS: `go test ./gir/pkgconfig/ -run TestGIRDirs -v` → SKIP with reason. Confirm the file still passes `go vet ./gir/...`.
- [ ] **Step 4: Commit + PR** — `git commit -am "test(pkgconfig): skip TestGIRDirs on darwin when no common prefix resolves"`; push; PR body: before/after test output as evidence; note Linux CI unaffected.

---

## Self-Review Notes

- Spec coverage: setup/config (T1-2), auth+session+logout (T4,5,8,12), peers/dialogs/updates (T6,7,10), messages+dedup+mark-read (T9,11,12), photos (T13-15), polish/empty/failed (T16), CI (T17), evidence (T18), TestGIRDirs (T19), gota gap (T13). Out-of-scope list enforced by absence.
- Known risk, stated openly: gotd/gtk API signatures in this plan come from the review agents' verification notes, not from compiled code. Steps that hit a signature mismatch must consult the pinned docs (Global Constraints, last bullet) and adapt — the tests, not the plan's code blocks, are the contract.
- Type consistency: `model.*` defined once in Task 3 and consumed by exact name everywhere; `chatStates.Pending/Note/Reconcile` defined T9, consumed T10/T12; `PhotoRef.SizeType` produced T14's mapMessage amendment, consumed T15.
