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
