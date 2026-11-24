package business

import (
	"context"
	"log/slog"
	"time"
)

// cleanUpInterval is how often the chores run: the expired pages of the crawl cache and the Episodes past their
// Retention are deleted.
const cleanUpInterval = 24 * time.Hour

// CrawlCacheCleaner removes the pages from the crawl cache that are too old. CachingCrawler is one.
type CrawlCacheCleaner interface {
	DeleteExpired() error
}

// Maintenance does the chores that come around by themselves: once a day it deletes the expired pages of the crawl
// cache and the Episodes that are older than the Retention, and once right at the start.
type Maintenance struct {
	crawl     CrawlCacheCleaner
	library   *EpisodeLibrary
	retention time.Duration
	clock     Clock
	maxSleep  time.Duration
}

// NewMaintenance creates the chores for the crawl cache and the Episodes of the library; a retention of zero keeps
// every Episode. The loop looks at the clock at least every maxSleep.
func NewMaintenance(crawl CrawlCacheCleaner, library *EpisodeLibrary, retention time.Duration, clock Clock, maxSleep time.Duration) *Maintenance {
	return &Maintenance{crawl: crawl, library: library, retention: retention, clock: clock, maxSleep: maxSleep}
}

// Run does the chores until ctx is cancelled and returns a channel that is closed when it has stopped. It looks at the
// clock at least every maxSleep, so a clock that jumps is noticed.
func (m *Maintenance) Run(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		var lastCleanUp time.Time
		for {
			if now := m.clock.Now(); lastCleanUp.IsZero() || now.Sub(lastCleanUp) >= cleanUpInterval {
				m.cleanUp(now)
				lastCleanUp = now
			}
			select {
			case <-ctx.Done():
				slog.Debug("Maintenance stops")
				return
			case <-time.After(m.maxSleep):
			}
		}
	}()
	return done
}

// cleanUp does every chore once. One that fails is logged and does not keep the others from being done.
func (m *Maintenance) cleanUp(now time.Time) {
	if err := m.crawl.DeleteExpired(); err != nil {
		slog.Error("Cleaning up the crawl cache failed", "error", err)
	}
	m.deleteExpiredEpisodes(now)
}

// deleteExpiredEpisodes deletes the Episodes older than the Retention, as counted from now.
func (m *Maintenance) deleteExpiredEpisodes(now time.Time) {
	if m.retention == 0 {
		slog.Debug("Episodes are kept for ever, nothing to delete")
		return
	}
	deleted, err := m.library.DeleteBefore(now.Add(-m.retention))
	if err != nil {
		slog.Error("Deleting Episodes past their Retention failed", "error", err)
		return
	}
	slog.Info("Episodes past their Retention deleted", "deleted", deleted, "retention", m.retention)
}
