package business

import "time"

// Clock tells the current time and waits. The business layer and everything driven by
// time (Max Lookback windows, Schedules, the waits between retries) ask a Clock, so that
// tests can control it.
type Clock interface {
	Now() time.Time
	// Sleep waits for the duration.
	Sleep(d time.Duration)
}

// SystemClock is the Clock of the real wall time.
type SystemClock struct{}

// Now returns the current wall time.
func (SystemClock) Now() time.Time { return time.Now() }

// Sleep waits for the duration.
func (SystemClock) Sleep(d time.Duration) { time.Sleep(d) }
