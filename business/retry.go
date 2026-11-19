package business

import (
	"errors"
	"log/slog"
	"time"
)

const (
	// retryAttempts is how often a step is tried before it gives up on a transient error.
	retryAttempts = 3
	// initialWait is the wait after the first failure; every further wait is twice as long as the one before.
	initialWait = time.Second
)

// retryTransient calls the step, and again after a wait when it fails with ErrTransient, up to retryAttempts times.
// Any other error ends it at once. step names the step in the log. The Usage is what every attempt consumed, also
// when the last one failed.
func retryTransient[T any](clock Clock, step string, call func() (T, Usage, error)) (T, Usage, error) {
	var usage Usage
	wait := initialWait
	for attempt := 1; ; attempt++ {
		result, consumed, err := call()
		usage = usage.Add(consumed)
		if err == nil || !errors.Is(err, ErrTransient) || attempt == retryAttempts {
			return result, usage, err
		}
		slog.Warn("Step failed, trying again", "step", step, "attempt", attempt, "of", retryAttempts, "wait", wait, "error", err)
		clock.Sleep(wait)
		wait *= 2
	}
}
