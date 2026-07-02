// auth.go
package tg

import (
	"context"

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
