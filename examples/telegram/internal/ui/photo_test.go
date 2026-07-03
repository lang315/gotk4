// photo_test.go
package ui

import (
	"errors"
	"testing"
)

func TestAsyncPicMarshalsOnce(t *testing.T) {
	if !gtkOK { // shared single-shot init result from TestMain (login_test.go)
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
