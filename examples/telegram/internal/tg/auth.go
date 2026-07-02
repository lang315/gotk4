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
	Retry bool // the previous answer for this step was rejected; re-asking
	Reply chan string
}

type UIAuth struct {
	Prompts chan Prompt
}

// Compile-time pin: verified against gotd/td v0.159.0's
// telegram/auth.UserAuthenticator (Phone/Code/Password/
// AcceptTermsOfService/SignUp).
var _ auth.UserAuthenticator = (*UIAuth)(nil)

func NewUIAuth() *UIAuth { return &UIAuth{Prompts: make(chan Prompt)} }

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

func (a *UIAuth) Phone(ctx context.Context) (string, error) {
	return a.ask(ctx, model.PromptPhone, false)
}

func (a *UIAuth) Code(ctx context.Context, _ *tgapi.AuthSentCode) (string, error) {
	return a.ask(ctx, model.PromptCode, false)
}

func (a *UIAuth) Password(ctx context.Context) (string, error) {
	return a.ask(ctx, model.PromptPassword, false)
}

func (a *UIAuth) AcceptTermsOfService(ctx context.Context, tos tgapi.HelpTermsOfService) error {
	return nil
}

func (a *UIAuth) SignUp(ctx context.Context) (auth.UserInfo, error) {
	return auth.UserInfo{}, errors.New("sign-up is not supported; register with an official client first")
}
