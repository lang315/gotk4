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

// authClient is the subset of *auth.Client the login flow needs. Tests inject
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
