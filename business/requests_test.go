package business

import (
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeQueue is a ProductionQueue that remembers what was asked for.
type fakeQueue struct {
	mu        sync.Mutex
	producing bool
	err       error
	requested []string
}

func (f *fakeQueue) Generate(userID, castProfileID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requested = append(f.requested, userID+"/"+castProfileID)
	return f.err
}

// calls returns what was asked for so far, safe to call while another goroutine asks.
func (f *fakeQueue) calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.requested...)
}

func (f *fakeQueue) Generating(string, string) bool { return f.producing }

// definedProfiles are CastProfiles by User Id and CastProfile Id.
type definedProfiles map[string]bool

func (d definedProfiles) Exists(userID, castProfileID string) bool {
	return d[userID+"/"+castProfileID]
}

func requestsOf(queue *fakeQueue, profiles definedProfiles, repository *handingOut) *EpisodeRequests {
	return NewEpisodeRequests(queue, profiles, NewEpisodeLibrary(repository, &recordingAudio{}))
}

func TestAnEpisodeIsRequestedOnlyForACastProfileOfTheUser(t *testing.T) {
	queue := &fakeQueue{}
	requests := requestsOf(queue, definedProfiles{"alex/morning": true}, newHandingOut())

	unknown := requests.Request("alex", "evening")
	foreign := requests.Request("bea", "morning")
	require.NoError(t, requests.Request("alex", "morning"))

	assert.ErrorIs(t, unknown, ErrCastProfileNotFound)
	assert.ErrorIs(t, foreign, ErrCastProfileNotFound, "the CastProfile of another User with that Id does not count")
	assert.Equal(t, []string{"alex/morning"}, queue.requested)
}

func TestRetryQueuesANewEpisodeAndThenRemovesTheFailedOne(t *testing.T) {
	queue := &fakeQueue{}
	repository := newHandingOut(ready, failed)
	requests := requestsOf(queue, definedProfiles{"alex/morning": true}, repository)

	require.NoError(t, requests.Retry("alex", failed.ID))

	assert.Equal(t, []string{"alex/morning"}, queue.requested)
	assert.Equal(t, []int64{failed.ID}, repository.deleted)
}

func TestRetryChangesNothingWhenItIsRefusedOrTheQueueFails(t *testing.T) {
	full := errors.New("queue is full")
	for name, tc := range map[string]struct {
		user      string
		id        int64
		profiles  definedProfiles
		queue     *fakeQueue
		err       error
		requested []string
	}{
		"Episode of another User":       {user: "bea", id: failed.ID, profiles: definedProfiles{"bea/morning": true}, queue: &fakeQueue{}, err: ErrEpisodeNotFound},
		"Episode that did not fail":     {user: "alex", id: ready.ID, profiles: definedProfiles{"alex/morning": true}, queue: &fakeQueue{}, err: ErrEpisodeNotFound},
		"CastProfile removed since":     {user: "alex", id: failed.ID, profiles: definedProfiles{}, queue: &fakeQueue{}, err: ErrCastProfileNotFound},
		"Episode is being produced":     {user: "alex", id: failed.ID, profiles: definedProfiles{"alex/morning": true}, queue: &fakeQueue{producing: true}, err: ErrAlreadyProducing},
		"queue cannot take the request": {user: "alex", id: failed.ID, profiles: definedProfiles{"alex/morning": true}, queue: &fakeQueue{err: full}, err: full, requested: []string{"alex/morning"}},
	} {
		t.Run(name, func(t *testing.T) {
			repository := newHandingOut(ready, failed)

			err := requestsOf(tc.queue, tc.profiles, repository).Retry(tc.user, tc.id)

			assert.ErrorIs(t, err, tc.err)
			assert.Equal(t, tc.requested, tc.queue.requested)
			assert.Empty(t, repository.deleted, "the failed Episode stays")
		})
	}
}

func TestRetrySucceedsEvenWhenTheFailedEpisodeCannotBeRemoved(t *testing.T) {
	queue := &fakeQueue{}
	repository := newHandingOut(failed)
	repository.deleteErr = errors.New("disk full")

	err := requestsOf(queue, definedProfiles{"alex/morning": true}, repository).Retry("alex", failed.ID)

	assert.NoError(t, err, "the new Episode is queued, which is what the User asked for")
	assert.Equal(t, []string{"alex/morning"}, queue.requested)
}
