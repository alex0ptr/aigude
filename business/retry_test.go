package business

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// flakyTTS fails the first calls with the given errors and speaks a second of silence afterwards.
type flakyTTS struct {
	errs  []error
	calls *int
}

func (f flakyTTS) Synthesize(string, Voice) (Speech, Usage, error) {
	usage := Usage{{UsageSpeech, UnitInputTokens, 1}}
	n := *f.calls
	*f.calls++
	if n < len(f.errs) {
		return Speech{}, usage, f.errs[n]
	}
	return secondsOfSpeech(1), usage, nil
}

// sleepingClock is a Clock that does not wait and remembers how long it was asked to.
type sleepingClock struct {
	fixedClock
	slept *[]time.Duration
}

func (c sleepingClock) Sleep(d time.Duration) { *c.slept = append(*c.slept, d) }

var transient = fmt.Errorf("%w: rate limited", ErrTransient)

func producingWithFlakySpeech(errs ...error) (*Episodes, *recordingRepository, CastProfile, *int, *[]time.Duration) {
	calls, slept := new(int), new([]time.Duration)
	episodes, _, repository, profile := producing(flakyTTS{errs: errs, calls: calls}, "1")
	episodes.clock = sleepingClock{fixedClock: episodes.clock.(fixedClock), slept: slept}
	return episodes, repository, profile, calls, slept
}

func TestSpeechThatFailsTwiceWithATransientErrorIsRetriedAndSucceeds(t *testing.T) {
	episodes, repository, profile, calls, slept := producingWithFlakySpeech(transient, transient)

	_, err := episodes.Create(User{ID: "alex"}, profile)

	require.NoError(t, err)
	assert.Equal(t, 3, *calls)
	assert.Len(t, repository.saved, 1)
	assert.Equal(t, []time.Duration{speechRetryWait, 2 * speechRetryWait}, *slept, "the waits grow")
	assert.Equal(t, int64(3), repository.saved[0].Usage[len(repository.saved[0].Usage)-1].Quantity, "every attempt consumed something")
}

func TestThreeTransientFailuresFailTheEpisodeWithTheReason(t *testing.T) {
	episodes, repository, profile, calls, _ := producingWithFlakySpeech(transient, transient, transient, transient)

	_, err := episodes.Create(User{ID: "alex"}, profile)

	require.ErrorContains(t, err, "rate limited")
	assert.Equal(t, 3, *calls, "three tries, no more")
	require.Len(t, repository.failed, 1)
	assert.Contains(t, repository.failures[0], "rate limited")
}

func TestAnErrorThatIsNotTransientFailsAtOnce(t *testing.T) {
	episodes, repository, profile, calls, slept := producingWithFlakySpeech(errors.New("invalid voice"))

	_, err := episodes.Create(User{ID: "alex"}, profile)

	require.ErrorContains(t, err, "invalid voice")
	assert.Equal(t, 1, *calls)
	assert.Empty(t, *slept)
	assert.Len(t, repository.failed, 1)
}
