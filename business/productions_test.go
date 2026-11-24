package business

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// scriptedMaker is an EpisodeMaker that answers with the errors it was given, one for each call, and nil after that.
// With a gate it waits for the gate to be closed before it answers.
type scriptedMaker struct {
	mu      sync.Mutex
	calls   []string
	results []error
	gate    chan struct{}
	started chan struct{}
}

func (m *scriptedMaker) Create(user User, profile CastProfile) (Episode, error) {
	m.mu.Lock()
	m.calls = append(m.calls, user.ID+"/"+profile.ID)
	var result error
	if len(m.results) > 0 {
		result, m.results = m.results[0], m.results[1:]
	}
	m.mu.Unlock()
	if m.started != nil {
		m.started <- struct{}{}
	}
	if m.gate != nil {
		<-m.gate
	}
	return Episode{}, result
}

func (m *scriptedMaker) called() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.calls...)
}

// knownProfiles are the CastProfiles that exist, by User Id and CastProfile Id.
type knownProfiles map[string]bool

func (k knownProfiles) CastProfile(userID, castProfileID string) (User, CastProfile, bool) {
	return User{ID: userID}, CastProfile{ID: castProfileID}, k[userID+"/"+castProfileID]
}

var producedAt = time.Date(2026, time.October, 29, 12, 0, 0, 0, time.UTC)

func startProductions(t *testing.T, maker EpisodeMaker, profiles knownProfiles) (*Productions, <-chan struct{}) {
	t.Helper()
	productions := NewProductions(maker, profiles, fixedClock{now: producedAt})
	ctx, cancel := context.WithCancel(context.Background())
	done := productions.Run(ctx)
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return productions, done
}

func idle(p *Productions, userID, profileID string) func() bool {
	return func() bool { return !p.Generating(userID, profileID) }
}

func TestACastProfileThatIsQueuedOrBeingProducedIsNotQueuedAgain(t *testing.T) {
	maker := &scriptedMaker{gate: make(chan struct{}), started: make(chan struct{}, 1)}
	productions, _ := startProductions(t, maker, knownProfiles{"alex/morning": true})

	require.NoError(t, productions.Generate("alex", "morning"))
	<-maker.started
	require.NoError(t, productions.Generate("alex", "morning"))
	require.True(t, productions.Generating("alex", "morning"))
	close(maker.gate)

	require.Eventually(t, idle(productions, "alex", "morning"), time.Second, time.Millisecond)
	assert.Equal(t, []string{"alex/morning"}, maker.called(), "one Episode for two requests")
	assert.False(t, productions.Generating("alex", "evening"), "other CastProfiles are not affected")
}

func TestARequestIsRefusedWhenTooManyAreWaiting(t *testing.T) {
	productions := NewProductions(&scriptedMaker{}, knownProfiles{}, fixedClock{now: producedAt})
	for i := 0; i < queueSize; i++ {
		require.NoError(t, productions.Generate("alex", fmt.Sprint("profile-", i)))
	}

	err := productions.Generate("alex", "one-too-many")

	assert.ErrorIs(t, err, ErrQueueFull)
	assert.False(t, productions.Generating("alex", "one-too-many"), "a refused request is not pending")
}

func TestNothingNewIsRememberedUntilALaterProductionMakesAnEpisodeOrFails(t *testing.T) {
	maker := &scriptedMaker{results: []error{ErrNoNewContent, nil, ErrNoNewContent, errors.New("speech failed")}}
	productions, _ := startProductions(t, maker, knownProfiles{"alex/morning": true})
	produce := func() {
		require.NoError(t, productions.Generate("alex", "morning"))
		require.Eventually(t, idle(productions, "alex", "morning"), time.Second, time.Millisecond)
	}

	produce()
	at, found := productions.NothingNew("alex", "morning")
	assert.True(t, found)
	assert.Equal(t, producedAt, at)

	produce()
	_, found = productions.NothingNew("alex", "morning")
	assert.False(t, found, "an Episode was made")

	produce()
	_, found = productions.NothingNew("alex", "morning")
	assert.True(t, found)

	produce()
	_, found = productions.NothingNew("alex", "morning")
	assert.False(t, found, "a failure is not the same as nothing new")
}

func TestARequestForAUserOrCastProfileThatIsNotThereAnyMoreIsSkipped(t *testing.T) {
	maker := &scriptedMaker{}
	productions, _ := startProductions(t, maker, knownProfiles{"alex/morning": true})

	require.NoError(t, productions.Generate("alex", "removed"))
	require.NoError(t, productions.Generate("gone", "morning"))
	require.NoError(t, productions.Generate("alex", "morning"))

	require.Eventually(t, idle(productions, "alex", "morning"), time.Second, time.Millisecond)
	require.Eventually(t, idle(productions, "alex", "removed"), time.Second, time.Millisecond)
	require.Eventually(t, idle(productions, "gone", "morning"), time.Second, time.Millisecond)
	assert.Equal(t, []string{"alex/morning"}, maker.called(), "nothing was produced for the others")
}

func TestTheWorkerStopsWhenItsContextIsCancelled(t *testing.T) {
	productions := NewProductions(&scriptedMaker{}, knownProfiles{}, fixedClock{now: producedAt})
	ctx, cancel := context.WithCancel(context.Background())
	done := productions.Run(ctx)

	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("the worker did not stop")
	}
}
