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
