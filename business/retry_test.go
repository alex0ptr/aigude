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

func TestSpeechIsRetriedAndEveryAttemptCountsInTheUsage(t *testing.T) {
	episodes, repository, profile, calls, _ := producingWithFlakySpeech(transient, transient)

	_, err := episodes.Create(User{ID: "alex"}, profile)

	require.NoError(t, err)
	assert.Equal(t, 3, *calls)
	assert.Len(t, repository.saved, 1)
	assert.Equal(t, int64(3), repository.saved[0].Usage[len(repository.saved[0].Usage)-1].Quantity, "every attempt consumed something")
}

func TestThreeTransientFailuresFailTheEpisodeWithTheReason(t *testing.T) {
	episodes, repository, profile, calls, _ := producingWithFlakySpeech(transient, transient, transient, transient)

	_, err := episodes.Create(User{ID: "alex"}, profile)

	require.ErrorContains(t, err, "rate limited")
	assert.Equal(t, retryAttempts, *calls)
	require.Len(t, repository.failed, 1)
	assert.Contains(t, repository.failures[0], "rate limited")
}

func TestRetryTransient(t *testing.T) {
	permanent := errors.New("invalid voice")
	tests := []struct {
		name      string
		errs      []error
		wantCalls int
		wantSlept []time.Duration
		wantErr   error
	}{
		{"succeeds at once", nil, 1, nil, nil},
		{"succeeds after two transient errors", []error{transient, transient}, 3, []time.Duration{initialWait, 2 * initialWait}, nil},
		{"gives up after the third transient error", []error{transient, transient, transient, transient}, 3, []time.Duration{initialWait, 2 * initialWait}, ErrTransient},
		{"an error that is not transient ends it at once", []error{permanent}, 1, nil, permanent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls, slept := 0, []time.Duration{}
			clock := sleepingClock{slept: &slept}

			result, usage, err := retryTransient(clock, "test", func() (string, Usage, error) {
				n := calls
				calls++
				if n < len(tt.errs) {
					return "", Usage{{UsageSpeech, UnitInputTokens, 1}}, tt.errs[n]
				}
				return "done", Usage{{UsageSpeech, UnitInputTokens, 1}}, nil
			})

			assert.Equal(t, tt.wantCalls, calls)
			assert.Equal(t, int64(tt.wantCalls), usage[0].Quantity, "every attempt counts")
			if len(tt.wantSlept) == 0 {
				assert.Empty(t, slept)
			} else {
				assert.Equal(t, tt.wantSlept, slept)
			}
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "done", result)
		})
	}
}

// failingContent is a ContentRepository that fails with err and counts how often it was asked.
type failingContent struct {
	err   error
	calls *int
}

func (f failingContent) Content(time.Time, time.Time, ContentFetchOpts) ([]Content, Usage, error) {
	*f.calls++
	return nil, nil, f.err
}

func TestASubscriptionThatKeepsFailingIsSkippedAndTheEpisodeUsesTheOthers(t *testing.T) {
	episodes, repository, profile, _, slept := producingWithFlakySpeech()
	calls := 0
	profile.Subscriptions = []Subscription{{Repository: failingContent{err: transient, calls: &calls}}, {Repository: oneItem{}}}

	_, err := episodes.Create(User{ID: "alex"}, profile)

	require.NoError(t, err)
	assert.Equal(t, retryAttempts, calls)
	assert.Equal(t, []time.Duration{initialWait, 2 * initialWait}, *slept)
	assert.Len(t, repository.saved, 1)
	assert.Empty(t, repository.failed)
}

func TestWhenEverySubscriptionFailsTheEpisodeFailsWithTheReasons(t *testing.T) {
	episodes, repository, profile, _, _ := producingWithFlakySpeech()
	calls := 0
	profile.Subscriptions = []Subscription{
		{Repository: failingContent{err: errors.New("http error: 404"), calls: &calls}},
		{Repository: failingContent{err: errors.New("http error: 500"), calls: &calls}},
	}

	_, err := episodes.Create(User{ID: "alex"}, profile)

	require.ErrorContains(t, err, "http error: 404")
	require.ErrorContains(t, err, "http error: 500")
	assert.Equal(t, 2, calls, "an error that is not transient is not tried again")
	require.Len(t, repository.failed, 1)
	assert.Empty(t, repository.saved)
}
