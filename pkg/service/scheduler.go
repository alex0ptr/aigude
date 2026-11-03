package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/alex0ptr/aigude/business"
	"github.com/alex0ptr/aigude/pkg/userconfig"
)

// scheduler starts the Episodes of the Schedules of the CastProfiles when they are due. It sleeps until the next
// due time and looks at the clock again after at most maxSleep, so a standby, a changed clock or a change of
// daylight saving time cannot make it miss a run. A change of the User Config wakes it up.
type scheduler struct {
	store     *userconfig.Store
	generator *generator
	episodes  business.EpisodeRepository
	clock     business.Clock
	location  *time.Location
	maxSleep  time.Duration

	// checked is the time up to which the Schedules were looked at, snapshot the User Config that was in force then.
	checked  time.Time
	snapshot *userconfig.Snapshot
	// started is, for each CastProfile, the time up to which its due runs were started. A clock that is set back
	// does not make a run due again that was started already.
	started map[generationRequest]time.Time
}

// newScheduler creates a scheduler that evaluates the Schedules in the location. The Episodes in the repository tell
// which runs were missed while the service was off.
func newScheduler(store *userconfig.Store, generator *generator, episodes business.EpisodeRepository, clock business.Clock, location *time.Location, maxSleep time.Duration) *scheduler {
	return &scheduler{
		store: store, generator: generator, episodes: episodes, clock: clock, location: location, maxSleep: maxSleep,
		started: make(map[generationRequest]time.Time),
	}
}

// run makes up the runs that were missed while the service was off, then starts what is due until ctx is cancelled,
// and returns a channel that is closed when it has stopped.
func (s *scheduler) run(ctx context.Context) <-chan struct{} {
	changes, unsubscribe := s.store.Subscribe()
	s.checked = s.clock.Now()
	s.snapshot = s.store.Snapshot()
	slog.Info("Scheduler started", "checked", s.checked, "maxSleep", s.maxSleep)
	s.makeUpMissedRuns()
	return background(func() {
		defer unsubscribe()
		for {
			s.tick()
			timer := time.NewTimer(s.sleepTime())
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-changes:
				slog.Debug("Scheduler woken up by a change of the User Config")
				timer.Stop()
			case <-timer.C:
			}
		}
	})
}

// makeUpMissedRuns asks for one Episode of each CastProfile that had a Schedule due since its latest Episode, whether
// that was made by a Schedule or by hand, and whether it succeeded or failed. Several missed runs make one Episode.
// A CastProfile that has no Episode yet is left as it is.
func (s *scheduler) makeUpMissedRuns() {
	now := s.checked
	for _, user := range s.snapshot.Users() {
		for _, profile := range user.CastProfiles {
			latest, err := s.episodes.List(user.ID, business.ListOpts{CastProfileID: profile.ID, Limit: 1})
			if err != nil {
				slog.Error("Missed runs could not be looked up", "user", user.ID, "profile", profile.ID, "error", err)
				continue
			}
			if len(latest) == 0 {
				slog.Debug("CastProfile has no Episode yet, nothing to make up", "user", user.ID, "profile", profile.ID)
				continue
			}
			due, ok := profile.NextDue(latest[0].Date, s.location)
			if !ok || due.After(now) {
				continue
			}
			slog.Info("Making up a missed run", "user", user.ID, "profile", profile.ID, "due", due, "latestEpisode", latest[0].Date)
			s.queueEpisode(user.ID, profile.ID, now)
		}
	}
}

// queueEpisode asks for an Episode of the CastProfile and remembers that its runs are started up to now.
func (s *scheduler) queueEpisode(userID, castProfileID string, now time.Time) {
	s.started[generationRequest{userID: userID, castProfileID: castProfileID}] = now
	if err := s.generator.Generate(userID, castProfileID); err != nil {
		slog.Error("Scheduled Episode could not be queued", "user", userID, "profile", castProfileID, "error", err)
	}
}

// tick queues one Episode for each CastProfile that has a Schedule due since the last look, judged by the User Config
// that was in force during that time, and takes the current User Config on.
func (s *scheduler) tick() {
	now := s.clock.Now()
	if now.Before(s.checked) {
		slog.Warn("The clock was set back, nothing is due", "checked", s.checked, "now", now)
	} else {
		s.startDueEpisodes(now)
	}
	s.checked = now
	s.snapshot = s.store.Snapshot()
}

// startDueEpisodes asks for an Episode of each CastProfile that has a Schedule due after the later of s.checked and
// the start of its latest run, up to and including now. Several due Schedules of a CastProfile make one Episode.
func (s *scheduler) startDueEpisodes(now time.Time) {
	for _, user := range s.snapshot.Users() {
		for _, profile := range user.CastProfiles {
			key := generationRequest{userID: user.ID, castProfileID: profile.ID}
			due, ok := profile.NextDue(later(s.checked, s.started[key]), s.location)
			if !ok || due.After(now) {
				continue
			}
			slog.Info("Schedule is due", "user", user.ID, "profile", profile.ID, "due", due, "now", now)
			s.queueEpisode(user.ID, profile.ID, now)
		}
	}
}

// later returns the later of two times.
func later(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

// sleepTime is the time until the next Schedule is due, but at most maxSleep.
func (s *scheduler) sleepTime() time.Duration {
	sleep := s.maxSleep
	now := s.clock.Now()
	for _, user := range s.snapshot.Users() {
		for _, profile := range user.CastProfiles {
			if due, ok := profile.NextDue(now, s.location); ok {
				sleep = min(sleep, due.Sub(now))
			}
		}
	}
	slog.Debug("Scheduler sleeps", "for", sleep)
	return sleep
}
