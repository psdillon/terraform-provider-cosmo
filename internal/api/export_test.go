package api

import "time"

// SetCreateTokenRetryDelay makes CreateToken wait d between attempts, and returns a function that
// restores the real delay.
func SetCreateTokenRetryDelay(d time.Duration) (restore func()) {
	previous := createTokenRetryDelay
	createTokenRetryDelay = func() time.Duration { return d }
	return func() { createTokenRetryDelay = previous }
}
