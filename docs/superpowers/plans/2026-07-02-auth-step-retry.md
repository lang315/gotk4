# Auth Step-Retry + Auto-Submit Integration Tests Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A rejected 2FA password (or login code) re-prompts the same step instead of bouncing the user back to the phone input, with the whole login flow covered by auto-submitting integration tests that need no network.

**Architecture:** Replace gotd's one-shot `auth.Flow.Run` with a hand-rolled ~60-line flow in `internal/tg/flow.go` that loops on `PHONE_CODE_INVALID` and `auth.ErrPasswordInvalid`, re-asking only the failed step. The flow talks to a 3-method `authClient` interface (satisfied by `*auth.Client`), so tests inject a scripted fake and an auto-submit goroutine plays the user. A GTK-level test drives the real `LoginPage` via the entry's `activate` signal, skipping when no display is available.

**Tech Stack:** Go 1.26, gotd/td v0.159.0, gotk4 `pkg/` bindings (GTK 4.22.4 via brew on macOS).

## Root Cause (verified in gotd source, do not re-litigate)

gotd `telegram/auth/flow.go:138-145` asks for the password exactly once; `client.Password` maps `PASSWORD_HASH_INVALID` to `auth.ErrPasswordInvalid` (`telegram/auth/user.go:68-70`) and the flow returns it as a fatal error. Our `internal/tg/client.go` `Run` loop treats any `runOnce` error as connection-level: backoff → new `runOnce` → `Status` not authorized → brand-new flow → `PromptPhone`. Net effect: one mistyped/rejected password restarts login from the phone screen, silently (the loop never logs the error).

## Global Constraints

- gotd APIs are pinned to v0.159.0; signatures used here were verified against the module cache: `SendCode(ctx, phone string, options auth.SendCodeOptions) (tg.AuthSentCodeClass, error)`, `SignIn(ctx, phone, code, codeHash string) (*tg.AuthAuthorization, error)`, `Password(ctx, password string) (*tg.AuthAuthorization, error)`, `var ErrPasswordInvalid`, `var ErrPasswordAuthNeeded`, `type SignUpRequired`, `tgerr.New(code int, msg string) *Error`, `tgerr.Is(err, "PHONE_CODE_INVALID")`.
- NEVER log phone numbers, codes, or passwords. Logging gotd error values (`%v` on err) is allowed — their messages carry no credentials.
- All gotd calls happen in non-UI goroutines; all UI mutation goes through `coreglib.IdleAdd`. Prompt replies are sent from a goroutine so the UI thread never blocks on a channel.
- Build/test on macOS requires: `export PKG_CONFIG_PATH="$(brew --prefix)/lib/pkgconfig:$(brew --prefix)/share/pkgconfig:$PKG_CONFIG_PATH"` before any `go` command in `examples/telegram`.
- CI (`.github/workflows/qa.yml`) runs `cd examples/telegram && go build ./... && go test ./...` headless — any test that touches GTK widgets MUST `t.Skip` when `gtk.InitCheck()` returns false.
- Format with `goimports -w`, never plain gofmt. Commit per task on branch `feat/telegram-app`.
- `ld: warning: ignoring duplicate libraries` in build output is environmental brew noise, not a failure. LSP diagnostics in this workspace are chronically stale — trust real `go build` output only.

---

### Task 1: Step-retry login flow with auto-submit tests (no GTK, no network)

**Files:**
- Create: `examples/telegram/internal/tg/flow.go`
- Create: `examples/telegram/internal/tg/flow_test.go`
- Modify: `examples/telegram/internal/tg/auth.go`

**Interfaces:**
- Consumes: `UIAuth.Prompts chan Prompt`, `model.PromptKind` constants, `Client.Auth *UIAuth` (all existing).
- Produces: `Prompt.Retry bool` field; `func (a *UIAuth) ask(ctx context.Context, k model.PromptKind, retry bool) (string, error)`; `func (c *Client) loginFlow(ctx context.Context, ac authClient) error`; `type authClient interface { SendCode(...); SignIn(...); Password(...) }`. Task 2 wires `loginFlow` into `client.go` and reads `Prompt.Retry` in the UI.

- [ ] **Step 1: Extend Prompt and ask() in `auth.go`**

Replace the `Prompt` struct and `ask` method in `examples/telegram/internal/tg/auth.go`:

```go
type Prompt struct {
	Kind  model.PromptKind
	Retry bool // the previous answer for this step was rejected; re-asking
	Reply chan string
}
```

```go
func (a *UIAuth) ask(ctx context.Context, k model.PromptKind, retry bool) (string, error) {
	reply := make(chan string, 1)
	select {
	case a.Prompts <- Prompt{Kind: k, Retry: retry, Reply: reply}:
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
```

Keep the existing `Phone`/`Code`/`Password`/`AcceptTermsOfService`/`SignUp` methods compiling for now by passing `false`:

```go
func (a *UIAuth) Phone(ctx context.Context) (string, error) {
	return a.ask(ctx, model.PromptPhone, false)
}

func (a *UIAuth) Code(ctx context.Context, _ *tgapi.AuthSentCode) (string, error) {
	return a.ask(ctx, model.PromptCode, false)
}

func (a *UIAuth) Password(ctx context.Context) (string, error) {
	return a.ask(ctx, model.PromptPassword, false)
}
```

(They are deleted in Task 2 once `client.go` stops using `auth.NewFlow`.)

- [ ] **Step 2: Write the failing tests**

Create `examples/telegram/internal/tg/flow_test.go`:

```go
// flow_test.go
package tg

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gotd/td/telegram/auth"
	tgapi "github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"

	"github.com/diamondburned/gotk4/examples/telegram/internal/model"
)

// fakeAuthClient scripts per-attempt outcomes. Error slices are consumed one
// entry per call; an empty slice means success.
type fakeAuthClient struct {
	signInErrs   []error
	passwordErrs []error

	gotPhone     string
	gotCodes     []string
	gotPasswords []string
}

func (f *fakeAuthClient) SendCode(_ context.Context, phone string, _ auth.SendCodeOptions) (tgapi.AuthSentCodeClass, error) {
	f.gotPhone = phone
	return &tgapi.AuthSentCode{PhoneCodeHash: "hash123"}, nil
}

func popErr(errs *[]error) error {
	if len(*errs) == 0 {
		return nil
	}
	err := (*errs)[0]
	*errs = (*errs)[1:]
	return err
}

func (f *fakeAuthClient) SignIn(_ context.Context, _, code, _ string) (*tgapi.AuthAuthorization, error) {
	f.gotCodes = append(f.gotCodes, code)
	if err := popErr(&f.signInErrs); err != nil {
		return nil, err
	}
	return &tgapi.AuthAuthorization{}, nil
}

func (f *fakeAuthClient) Password(_ context.Context, password string) (*tgapi.AuthAuthorization, error) {
	f.gotPasswords = append(f.gotPasswords, password)
	if err := popErr(&f.passwordErrs); err != nil {
		return nil, err
	}
	return &tgapi.AuthAuthorization{}, nil
}

// autoSubmit plays the user: it answers each prompt with the next scripted
// reply for that prompt kind and records the prompts it saw. Reading seen is
// safe after loginFlow returns: the final append happens-before the final
// reply send, which happens-before loginFlow's return.
func autoSubmit(a *UIAuth, replies map[model.PromptKind][]string) *[]Prompt {
	seen := &[]Prompt{}
	next := map[model.PromptKind]int{}
	go func() {
		for pr := range a.Prompts {
			*seen = append(*seen, pr)
			i := next[pr.Kind]
			next[pr.Kind]++
			pr.Reply <- replies[pr.Kind][i]
		}
	}()
	return seen
}

func kinds(seen []Prompt) []model.PromptKind {
	out := make([]model.PromptKind, len(seen))
	for i, p := range seen {
		out[i] = p.Kind
	}
	return out
}

func kindsEqual(a, b []model.PromptKind) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestLoginFlow2FAHappyPath(t *testing.T) {
	c := &Client{Auth: NewUIAuth()}
	fake := &fakeAuthClient{signInErrs: []error{auth.ErrPasswordAuthNeeded}}
	seen := autoSubmit(c.Auth, map[model.PromptKind][]string{
		model.PromptPhone:    {"+15551234567"},
		model.PromptCode:     {"12345"},
		model.PromptPassword: {"hunter2"},
	})

	if err := c.loginFlow(context.Background(), fake); err != nil {
		t.Fatalf("loginFlow: %v", err)
	}
	want := []model.PromptKind{model.PromptPhone, model.PromptCode, model.PromptPassword}
	if !kindsEqual(kinds(*seen), want) {
		t.Fatalf("prompt sequence = %v, want %v", kinds(*seen), want)
	}
	if fake.gotPhone != "+15551234567" || fake.gotCodes[0] != "12345" || fake.gotPasswords[0] != "hunter2" {
		t.Fatalf("submitted values did not reach the auth client: %+v", fake)
	}
}

func TestLoginFlowNo2FA(t *testing.T) {
	c := &Client{Auth: NewUIAuth()}
	fake := &fakeAuthClient{} // SignIn succeeds immediately
	seen := autoSubmit(c.Auth, map[model.PromptKind][]string{
		model.PromptPhone: {"+15551234567"},
		model.PromptCode:  {"12345"},
	})

	if err := c.loginFlow(context.Background(), fake); err != nil {
		t.Fatalf("loginFlow: %v", err)
	}
	want := []model.PromptKind{model.PromptPhone, model.PromptCode}
	if !kindsEqual(kinds(*seen), want) {
		t.Fatalf("prompt sequence = %v, want %v", kinds(*seen), want)
	}
}

// Regression for the reported bug: a rejected password must re-prompt the
// PASSWORD step (with Retry set), never bounce back to the phone prompt.
func TestLoginFlowWrongPasswordRepromptsPassword(t *testing.T) {
	c := &Client{Auth: NewUIAuth()}
	fake := &fakeAuthClient{
		signInErrs:   []error{auth.ErrPasswordAuthNeeded},
		passwordErrs: []error{auth.ErrPasswordInvalid}, // first attempt rejected
	}
	seen := autoSubmit(c.Auth, map[model.PromptKind][]string{
		model.PromptPhone:    {"+15551234567"},
		model.PromptCode:     {"12345"},
		model.PromptPassword: {"wrong", "right"},
	})

	if err := c.loginFlow(context.Background(), fake); err != nil {
		t.Fatalf("loginFlow: %v", err)
	}
	want := []model.PromptKind{
		model.PromptPhone, model.PromptCode,
		model.PromptPassword, model.PromptPassword,
	}
	if !kindsEqual(kinds(*seen), want) {
		t.Fatalf("prompt sequence = %v, want %v", kinds(*seen), want)
	}
	last := (*seen)[3]
	if !last.Retry {
		t.Fatal("re-prompt after rejected password must have Retry=true")
	}
	if (*seen)[2].Retry {
		t.Fatal("first password prompt must have Retry=false")
	}
	if len(fake.gotPasswords) != 2 || fake.gotPasswords[1] != "right" {
		t.Fatalf("passwords submitted = %v, want [wrong right]", fake.gotPasswords)
	}
}

func TestLoginFlowWrongCodeRepromptsCode(t *testing.T) {
	c := &Client{Auth: NewUIAuth()}
	fake := &fakeAuthClient{
		signInErrs: []error{tgerr.New(400, "PHONE_CODE_INVALID")}, // then success
	}
	seen := autoSubmit(c.Auth, map[model.PromptKind][]string{
		model.PromptPhone: {"+15551234567"},
		model.PromptCode:  {"00000", "12345"},
	})

	if err := c.loginFlow(context.Background(), fake); err != nil {
		t.Fatalf("loginFlow: %v", err)
	}
	want := []model.PromptKind{model.PromptPhone, model.PromptCode, model.PromptCode}
	if !kindsEqual(kinds(*seen), want) {
		t.Fatalf("prompt sequence = %v, want %v", kinds(*seen), want)
	}
	if !(*seen)[2].Retry {
		t.Fatal("re-prompt after rejected code must have Retry=true")
	}
	if fake.gotCodes[1] != "12345" {
		t.Fatalf("second code = %q, want 12345", fake.gotCodes[1])
	}
}

func TestLoginFlowFatalErrorPropagates(t *testing.T) {
	c := &Client{Auth: NewUIAuth()}
	boom := errors.New("network down")
	fake := &fakeAuthClient{signInErrs: []error{boom}}
	autoSubmit(c.Auth, map[model.PromptKind][]string{
		model.PromptPhone: {"+15551234567"},
		model.PromptCode:  {"12345"},
	})

	if err := c.loginFlow(context.Background(), fake); !errors.Is(err, boom) {
		t.Fatalf("loginFlow err = %v, want %v", err, boom)
	}
}

func TestLoginFlowCtxCancelUnblocks(t *testing.T) {
	c := &Client{Auth: NewUIAuth()}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()
	// No autoSubmit: the flow blocks on the phone prompt until ctx cancels.
	if err := c.loginFlow(ctx, &fakeAuthClient{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("loginFlow err = %v, want context.Canceled", err)
	}
}
```

- [ ] **Step 3: Run tests, verify they fail to compile**

```sh
cd examples/telegram
export PKG_CONFIG_PATH="$(brew --prefix)/lib/pkgconfig:$(brew --prefix)/share/pkgconfig:$PKG_CONFIG_PATH"
go test ./internal/tg/ -run TestLoginFlow -v
```

Expected: compile error — `c.loginFlow undefined` (and `authClient` undefined).

- [ ] **Step 4: Implement the flow**

Create `examples/telegram/internal/tg/flow.go`:

```go
// flow.go
package tg

import (
	"context"
	"errors"

	"github.com/gotd/td/telegram/auth"
	tgapi "github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"

	"github.com/diamondburned/gotk4/examples/telegram/internal/model"
)

// authClient is the slice of *auth.Client the login flow needs. Tests inject
// a scripted fake so the entire flow runs without network.
type authClient interface {
	SendCode(ctx context.Context, phone string, options auth.SendCodeOptions) (tgapi.AuthSentCodeClass, error)
	SignIn(ctx context.Context, phone, code, codeHash string) (*tgapi.AuthAuthorization, error)
	Password(ctx context.Context, password string) (*tgapi.AuthAuthorization, error)
}

var _ authClient = (*auth.Client)(nil)

// loginFlow replaces gotd's auth.Flow.Run, which is one-shot per step: any
// rejected code or password fails the whole flow, and the reconnect loop then
// restarts auth from the phone prompt. Here a rejected step re-asks only that
// step (Prompt.Retry=true); everything else propagates and restarts as before.
func (c *Client) loginFlow(ctx context.Context, ac authClient) error {
	phone, err := c.Auth.ask(ctx, model.PromptPhone, false)
	if err != nil {
		return err
	}
	sent, err := ac.SendCode(ctx, phone, auth.SendCodeOptions{})
	if err != nil {
		return err
	}
	sc, ok := sent.(*tgapi.AuthSentCode)
	if !ok {
		// AuthSentCodeSuccess: no code step required; already authorized.
		return nil
	}

	retry := false
	for {
		code, err := c.Auth.ask(ctx, model.PromptCode, retry)
		if err != nil {
			return err
		}
		_, signInErr := ac.SignIn(ctx, phone, code, sc.PhoneCodeHash)
		var signUp *auth.SignUpRequired
		switch {
		case signInErr == nil:
			return nil
		case errors.Is(signInErr, auth.ErrPasswordAuthNeeded):
			return c.passwordLoop(ctx, ac)
		case tgerr.Is(signInErr, "PHONE_CODE_INVALID"):
			retry = true // wrong code: re-ask the code, not the phone
		case errors.As(signInErr, &signUp):
			return errors.New("phone number not registered; sign up with an official client first")
		default:
			return signInErr
		}
	}
}

func (c *Client) passwordLoop(ctx context.Context, ac authClient) error {
	retry := false
	for {
		pw, err := c.Auth.ask(ctx, model.PromptPassword, retry)
		if err != nil {
			return err
		}
		_, err = ac.Password(ctx, pw)
		switch {
		case err == nil:
			return nil
		case errors.Is(err, auth.ErrPasswordInvalid):
			retry = true // wrong password: re-ask the password, not the phone
		default:
			return err
		}
	}
}
```

Note: unlimited retries are deliberate — the user can always quit, ctx cancellation unblocks the prompt, and the floodwait middleware absorbs server-side rate limits.

- [ ] **Step 5: Run tests, verify all pass**

```sh
go test ./internal/tg/ -run TestLoginFlow -v
```

Expected: all 6 `TestLoginFlow*` tests PASS. Then run the whole package to catch fallout:

```sh
go test ./internal/tg/
```

Expected: `ok`.

- [ ] **Step 6: Format and commit**

```sh
goimports -w internal/tg/
git add internal/tg/flow.go internal/tg/flow_test.go internal/tg/auth.go
git commit -m "feat(telegram): step-retry login flow with auto-submit tests

gotd's auth.Flow.Run asks for code and password exactly once; a rejected
password fails the flow, and the reconnect loop restarts auth from the
phone prompt. Hand-roll the flow against a 3-method authClient interface
so rejected steps re-prompt in place, and cover the whole flow with
scripted no-network tests."
```

---

### Task 2: Wire the flow into the client, surface retries in the UI, log session errors

**Files:**
- Modify: `examples/telegram/internal/tg/client.go` (flow call in `runOnce`, logging in `Run`)
- Modify: `examples/telegram/internal/tg/auth.go` (delete the now-dead `auth.UserAuthenticator` implementation)
- Modify: `examples/telegram/internal/ui/login.go` (`Show` renders retry state)

**Interfaces:**
- Consumes: `Client.loginFlow(ctx, authClient)` and `Prompt.Retry` from Task 1.
- Produces: nothing new — this task swaps call sites.

- [ ] **Step 1: Swap the flow in `client.go`**

In `runOnce`, replace:

```go
		if !status.Authorized {
			c.emit(model.Event{Kind: model.EventAuthNeeded})
			flow := auth.NewFlow(c.Auth, auth.SendCodeOptions{})
			if err := flow.Run(ctx, client.Auth()); err != nil {
				return err
			}
		}
```

with:

```go
		if !status.Authorized {
			c.emit(model.Event{Kind: model.EventAuthNeeded})
			if err := c.loginFlow(ctx, client.Auth()); err != nil {
				return err
			}
		}
```

- [ ] **Step 2: Log per-iteration errors in `Run`**

The reconnect loop currently swallows every error, which made this bug
undiagnosable. In `Run`, after `err := c.runOnce(ctx)` and the state reset,
add (before the `AUTH_KEY_UNREGISTERED` check):

```go
		if err != nil && ctx.Err() == nil {
			// gotd error strings carry no credentials; never log user input here.
			log.Printf("tg: session ended, reconnecting: %v", err)
		}
```

Add `"log"` to the imports of `client.go`.

- [ ] **Step 3: Delete the dead authenticator from `auth.go`**

`client.go` no longer builds an `auth.NewFlow`, so `UIAuth` no longer needs to
implement `auth.UserAuthenticator`. In `examples/telegram/internal/tg/auth.go`
delete: the `Phone`, `Code`, `Password`, `AcceptTermsOfService`, and `SignUp`
methods, the `var _ auth.UserAuthenticator = (*UIAuth)(nil)` pin, and the
now-unused imports (`errors`, `github.com/gotd/td/telegram/auth`, and
`tgapi "github.com/gotd/td/tg"` — whichever become unused; goimports will
confirm). The file keeps only: `Prompt`, `UIAuth`, `NewUIAuth`, `ask`.

- [ ] **Step 4: Render retry state in `login.go`**

In `LoginPage.Show`, replace:

```go
	p.label.SetText(titles[pr.Kind])
```

with:

```go
	title := titles[pr.Kind]
	if pr.Retry {
		title += " — previous attempt was rejected, try again"
	}
	p.label.SetText(title)
```

- [ ] **Step 5: Build both packages and run the full module tests**

```sh
cd examples/telegram
export PKG_CONFIG_PATH="$(brew --prefix)/lib/pkgconfig:$(brew --prefix)/share/pkgconfig:$PKG_CONFIG_PATH"
go build ./... && go test ./...
```

Expected: build exit 0; all packages `ok` (config, tg, ui).

- [ ] **Step 6: Format and commit**

```sh
goimports -w internal/ main.go
git add internal/tg/client.go internal/tg/auth.go internal/ui/login.go
git commit -m "feat(telegram): wire step-retry flow into client, show retry in UI

Swap auth.NewFlow for loginFlow, drop the now-dead UserAuthenticator
implementation, log per-iteration session errors (previously swallowed,
which hid the password-rejection restart), and mark re-prompts in the
login page label."
```

---

### Task 3: GTK-level LoginPage auto-submit test (skips headless)

**Files:**
- Create: `examples/telegram/internal/ui/login_test.go`

**Interfaces:**
- Consumes: `LoginPage` internals (white-box, same package), `tg.Prompt{Kind, Retry, Reply}` from Task 1, `gtk.InitCheck() bool`.
- Produces: nothing — terminal test task.

The test drives the REAL widget path: it casts the gota entry back to the
underlying `*gtk.Entry` (same native GObject), sets text, and calls
`ent.Activate()`. GtkEntry registers "activate" as its widget activation
signal, so `Activate()` fires the exact same handler chain as pressing Enter.
(Do NOT try `Object.Emit` — the bindings do not expose it.)

- [ ] **Step 1: Write the test**

Create `examples/telegram/internal/ui/login_test.go`:

```go
// login_test.go
package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/diamondburned/gotk4/examples/telegram/internal/model"
	"github.com/diamondburned/gotk4/examples/telegram/internal/tg"
)

// gtkEntry unwraps the gota entry to the underlying *gtk.Entry so the test
// can drive the real activate signal (what Enter does).
func gtkEntry(p *LoginPage) *gtk.Entry {
	return p.entry.ToWidget().Object.Cast().(*gtk.Entry)
}

func recvReply(t *testing.T, ch chan string) string {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(2 * time.Second):
		t.Fatal("no reply within 2s")
		return ""
	}
}

func TestLoginPageAutoSubmitAllSteps(t *testing.T) {
	if !gtk.InitCheck() {
		t.Skip("no display; GTK cannot init")
	}
	p := NewLoginPage()
	ent := gtkEntry(p)

	steps := []struct {
		kind     model.PromptKind
		input    string
		wantMask bool
	}{
		{model.PromptPhone, "+15551234567", false},
		{model.PromptCode, "12345", false},
		{model.PromptPassword, "hunter2", true},
	}
	for _, s := range steps {
		pr := tg.Prompt{Kind: s.kind, Reply: make(chan string, 1)}
		p.Show(pr)

		if ent.Visibility() == s.wantMask { // Visibility()==false means masked
			t.Fatalf("kind %v: entry masking = %v, want %v", s.kind, !ent.Visibility(), s.wantMask)
		}

		ent.SetText(s.input)
		if !ent.Activate() { // real Enter path: activate signal → OnEnter → submit
			t.Fatalf("kind %v: entry did not activate", s.kind)
		}

		if got := recvReply(t, pr.Reply); got != s.input {
			t.Fatalf("kind %v: reply = %q, want %q", s.kind, got, s.input)
		}
		if ent.Text() != "" {
			t.Fatalf("kind %v: entry not cleared after submit", s.kind)
		}
	}
}

func TestLoginPageEmptySubmitIsIgnored(t *testing.T) {
	if !gtk.InitCheck() {
		t.Skip("no display; GTK cannot init")
	}
	p := NewLoginPage()
	ent := gtkEntry(p)

	pr := tg.Prompt{Kind: model.PromptPhone, Reply: make(chan string, 1)}
	p.Show(pr)
	ent.Activate() // empty entry: must not reply

	select {
	case v := <-pr.Reply:
		t.Fatalf("empty submit produced reply %q", v)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestLoginPageRetryTitle(t *testing.T) {
	if !gtk.InitCheck() {
		t.Skip("no display; GTK cannot init")
	}
	p := NewLoginPage()

	p.Show(tg.Prompt{Kind: model.PromptPassword, Retry: true, Reply: make(chan string, 1)})
	if got := p.label.GetText(); !strings.Contains(got, "rejected") {
		t.Fatalf("retry title = %q, want it to mention rejection", got)
	}

	p.Show(tg.Prompt{Kind: model.PromptPassword, Reply: make(chan string, 1)})
	if got := p.label.GetText(); strings.Contains(got, "rejected") {
		t.Fatalf("non-retry title = %q, must not mention rejection", got)
	}
}
```

- [ ] **Step 2: Run the tests on macOS (display available)**

```sh
cd examples/telegram
export PKG_CONFIG_PATH="$(brew --prefix)/lib/pkgconfig:$(brew --prefix)/share/pkgconfig:$PKG_CONFIG_PATH"
go test ./internal/ui/ -run TestLoginPage -v
```

Expected: 3 tests PASS (on the user's mac a display exists). If the reply
never arrives, check whether `LoginPage.Show`'s submit goroutine ran — the
reply channel is buffered size 1 so the goroutine send completes immediately;
no GTK main loop iteration is required for `Emit`.

Note: `Activate()` invokes the signal handlers synchronously on the calling
goroutine, and `Show`/`SetText` here run on the test goroutine — that's fine
because no GTK main loop is running concurrently in this test.

- [ ] **Step 3: Verify headless skip does not break CI semantics**

Run with GTK forced headless to confirm the skip path:

```sh
GDK_BACKEND=broadwayd DISPLAY= WAYLAND_DISPLAY= go test ./internal/ui/ -run TestLoginPage -v 2>&1 | head -20
```

Expected: tests SKIP (or, if GTK still initializes on macOS regardless of these
variables, note that in the report — the `gtk.InitCheck()` guard is what CI's
truly headless Linux relies on; macOS cannot fully simulate it).

- [ ] **Step 4: Run the whole module suite, format, commit**

```sh
go build ./... && go test ./...
goimports -w internal/ui/
git add internal/ui/login_test.go
git commit -m "test(telegram): GTK-level auto-submit tests for the login page

Drive the real activate signal path for phone/code/password prompts,
assert password masking, empty-submit guard, and retry-title rendering.
Skips when GTK cannot initialize (headless CI)."
```

---

### Task 4: Rebuild binary and hand off to manual E2E

**Files:**
- Modify: none (build artifact `examples/telegram/telegram`, gitignored)

**Interfaces:**
- Consumes: everything above.
- Produces: the binary the user re-tests with.

- [ ] **Step 1: Rebuild**

```sh
cd examples/telegram
export PKG_CONFIG_PATH="$(brew --prefix)/lib/pkgconfig:$(brew --prefix)/share/pkgconfig:$PKG_CONFIG_PATH"
go build -o telegram .
```

Expected: exit 0, `./telegram` ~34MB arm64 Mach-O.

- [ ] **Step 2: Confirm tree is clean and binary ignored**

```sh
git status --porcelain | grep -v '.DS_Store\|.memsearch\|CLAUDE-FABLE' ; git check-ignore telegram && echo IGNORED
```

Expected: no tracked changes; `IGNORED`.

- [ ] **Step 3: Report the manual E2E script to the user (do not attempt real login — no credentials)**

The user verifies: (1) login with a deliberately wrong 2FA password → the page
must stay on "Two-factor password — previous attempt was rejected, try again",
NOT return to the phone prompt; (2) correct password → chat list loads;
(3) wrong login code → same-step retry behavior; (4) console shows
`tg: session ended, reconnecting: ...` only for genuine connection errors.
