package business

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// countingCleaner is a CrawlCacheCleaner that counts its runs.
type countingCleaner struct {
	mu   sync.Mutex
	runs int
	err  error
}

func (c *countingCleaner) DeleteExpired() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.runs++
	return c.err
}

func (c *countingCleaner) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.runs
}

// expiringEpisodes is an EpisodeRepository that remembers the cutoffs it was asked to delete before.
type expiringEpisodes struct {
	EpisodeRepository
	mu      sync.Mutex
	cutoffs []time.Time
	err     error
}

func (e *expiringEpisodes) DeleteBefore(t time.Time) (int, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cutoffs = append(e.cutoffs, t)
	return 2, e.err
}

func (e *expiringEpisodes) asked() []time.Time {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]time.Time(nil), e.cutoffs...)
}

func maintenanceOf(cleaner *countingCleaner, episodes *expiringEpisodes, retention time.Duration, clock Clock) *Maintenance {
	return NewMaintenance(cleaner, NewEpisodeLibrary(episodes, &recordingAudio{}), retention, clock, time.Millisecond)
}

func TestEpisodesOlderThanTheRetentionAreDeletedAndAZeroRetentionKeepsAll(t *testing.T) {
	now := time.Date(2026, time.October, 29, 12, 0, 0, 0, time.UTC)
	for name, tc := range map[string]struct {
		retention time.Duration
		want      []time.Time
	}{
		"30 days":       {retention: 30 * 24 * time.Hour, want: []time.Time{now.AddDate(0, 0, -30)}},
		"kept for ever": {retention: 0, want: nil},
	} {
		t.Run(name, func(t *testing.T) {
			episodes := &expiringEpisodes{}

			maintenanceOf(&countingCleaner{}, episodes, tc.retention, fixedClock{now: now}).deleteExpiredEpisodes(now)

			assert.Equal(t, tc.want, episodes.asked())
		})
	}
}

func TestAChoreThatFailsDoesNotKeepTheOthersFromRunning(t *testing.T) {
	now := time.Date(2026, time.October, 29, 12, 0, 0, 0, time.UTC)
	cleaner := &countingCleaner{err: errors.New("database is locked")}
	episodes := &expiringEpisodes{err: errors.New("disk full")}

	maintenanceOf(cleaner, episodes, time.Hour, fixedClock{now: now}).cleanUp(now)

	assert.Equal(t, 1, cleaner.count(), "the crawl cache was cleaned although the Episodes cannot be")
	assert.Len(t, episodes.asked(), 1, "the Episodes were tried although the crawl cache failed")
}

func TestTheChoresRunAtTheStartAndThenOnceADayByTheClockOfTheService(t *testing.T) {
	clock := &movableClock{now: time.Date(2026, time.October, 29, 12, 0, 0, 0, time.UTC)}
	cleaner := &countingCleaner{}
	episodes := &expiringEpisodes{}
	ctx, cancel := context.WithCancel(context.Background())
	done := maintenanceOf(cleaner, episodes, time.Hour, clock).Run(ctx)
	t.Cleanup(func() {
		cancel()
		<-done
	})

	require.Eventually(t, func() bool { return cleaner.count() == 1 }, time.Second, time.Millisecond, "at the start")
	clock.set(clock.Now().Add(23 * time.Hour))
	time.Sleep(20 * time.Millisecond)
	assert.Equal(t, 1, cleaner.count(), "not before a day has passed")
	clock.set(clock.Now().Add(2 * time.Hour))

	require.Eventually(t, func() bool { return cleaner.count() == 2 }, time.Second, time.Millisecond, "a day after the first run")
	assert.Len(t, episodes.asked(), 2)
}
