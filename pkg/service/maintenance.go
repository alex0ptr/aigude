package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/alex0ptr/aigude/business"
)

// cleanUpInterval is how often the chores run: the expired pages of the crawl cache and the Episodes past their
// Retention are deleted.
const cleanUpInterval = 24 * time.Hour

// maintenance does the chores that come around by themselves: once a day it deletes the expired pages of the crawl
// cache and the Episodes that are older than the Retention, and once right at the start.
type maintenance struct {
	crawler   *business.CachingCrawler
	episodes  business.EpisodeRepository
	retention time.Duration
	clock     business.Clock
	maxSleep  time.Duration
}

// newMaintenance creates the chores for the crawl cache and the Episodes; a retention of zero keeps every Episode.
func newMaintenance(crawler *business.CachingCrawler, episodes business.EpisodeRepository, retention time.Duration,
	clock business.Clock, maxSleep time.Duration) *maintenance {
	return &maintenance{crawler: crawler, episodes: episodes, retention: retention, clock: clock, maxSleep: maxSleep}
}

// deleteExpiredEpisodes deletes the Episodes older than the Retention, as counted by the service clock.
func (m *maintenance) deleteExpiredEpisodes(now time.Time) {
	if m.retention == 0 {
		slog.Debug("Episodes are kept for ever, nothing to delete")
		return
	}
	deleted, err := m.episodes.DeleteBefore(now.Add(-m.retention))
	if err != nil {
		slog.Error("Deleting Episodes past their Retention failed", "error", err)
		return
	}
	slog.Info("Episodes past their Retention deleted", "deleted", deleted, "retention", m.retention)
}

// run does the chores until ctx is cancelled and returns a channel that is closed when it has stopped. It looks at
// the clock of the service at least every maxSleep, so a clock that jumps is noticed.
func (m *maintenance) run(ctx context.Context) <-chan struct{} {
	return background(func() {
		var lastCleanUp time.Time
		for {
			if now := m.clock.Now(); lastCleanUp.IsZero() || now.Sub(lastCleanUp) >= cleanUpInterval {
				if err := m.crawler.DeleteExpired(); err != nil {
					slog.Error("Cleaning up the crawl cache failed", "error", err)
				}
				m.deleteExpiredEpisodes(now)
				lastCleanUp = now
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(m.maxSleep):
			}
		}
	})
}
