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
