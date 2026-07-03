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
