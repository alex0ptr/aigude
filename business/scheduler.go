package business

import (
	"context"
	"log/slog"
	"time"
)

// Scheduler starts the Episodes of the Schedules of the CastProfiles when they are due. It sleeps until the next due
// time and looks at the clock again after at most maxSleep, so a standby, a changed clock or a change of daylight
// saving time cannot make it miss a run. A change of the User Config wakes it up.
type Scheduler struct {
	source   ScheduleSource
	queue    ProductionQueue
	library  *EpisodeLibrary
	clock    Clock
	location *time.Location
	maxSleep time.Duration

	// checked is the time up to which the Schedules were looked at, snapshot the Schedules that were in force then.
	checked  time.Time
	snapshot []ScheduledProfile
	// started is, for each CastProfile, the time up to which its due runs were started. A clock that is set back
	// does not make a run due again that was started already.
	started map[profileKey]time.Time
}

// NewScheduler creates a scheduler that evaluates the Schedules in the location. The Episodes in the library tell
// which runs were missed while the service was off.
func NewScheduler(source ScheduleSource, queue ProductionQueue, library *EpisodeLibrary, clock Clock, location *time.Location, maxSleep time.Duration) *Scheduler {
	return &Scheduler{
		source: source, queue: queue, library: library, clock: clock, location: location, maxSleep: maxSleep,
		started: make(map[profileKey]time.Time),
	}
}

// Run makes up the runs that were missed while the service was off, then starts what is due until ctx is cancelled,
// and returns a channel that is closed when it has stopped.
func (s *Scheduler) Run(ctx context.Context) <-chan struct{} {
	changes, stop := s.source.Changes()
	s.begin()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer stop()
		for {
			s.tick()
			timer := time.NewTimer(s.sleepTime())
			select {
			case <-ctx.Done():
				timer.Stop()
				slog.Debug("Scheduler stops")
				return
			case <-changes:
				slog.Debug("Scheduler woken up by a change of the User Config")
				timer.Stop()
			case <-timer.C:
			}
		}
	}()
	return done
}

// begin takes the time and the Schedules of now as the first look and makes up the missed runs.
func (s *Scheduler) begin() {
	s.checked = s.clock.Now()
	s.snapshot = s.source.Schedules()
	slog.Info("Scheduler started", "checked", s.checked, "maxSleep", s.maxSleep)
	s.makeUpMissedRuns()
}

// makeUpMissedRuns asks for one Episode of each CastProfile that had a Schedule due since its latest Episode, whether
// that was made by a Schedule or by hand, and whether it succeeded or failed. Several missed runs make one Episode.
// A CastProfile that has no Episode yet is left as it is.
func (s *Scheduler) makeUpMissedRuns() {
	now := s.checked
	for _, profile := range s.snapshot {
		latest, err := s.library.List(profile.UserID, ListOpts{CastProfileID: profile.CastProfileID, Limit: 1})
		if err != nil {
			slog.Error("Missed runs could not be looked up", "user", profile.UserID, "profile", profile.CastProfileID, "error", err)
			continue
		}
		if len(latest) == 0 {
			slog.Debug("CastProfile has no Episode yet, nothing to make up", "user", profile.UserID, "profile", profile.CastProfileID)
			continue
		}
		due, ok := profile.NextDue(latest[0].Date, s.location)
		if !ok || due.After(now) {
			continue
		}
		slog.Info("Making up a missed run", "user", profile.UserID, "profile", profile.CastProfileID, "due", due, "latestEpisode", latest[0].Date)
		s.queueEpisode(profile.UserID, profile.CastProfileID, now)
	}
}

// queueEpisode asks for an Episode of the CastProfile and remembers that its runs are started up to now.
func (s *Scheduler) queueEpisode(userID, castProfileID string, now time.Time) {
	s.started[profileKey{userID: userID, castProfileID: castProfileID}] = now
	if err := s.queue.Generate(userID, castProfileID); err != nil {
		slog.Error("Scheduled Episode could not be queued", "user", userID, "profile", castProfileID, "error", err)
	}
}

// tick queues one Episode for each CastProfile that has a Schedule due since the last look, judged by the Schedules
// that were in force during that time, and takes the current Schedules on.
func (s *Scheduler) tick() {
	now := s.clock.Now()
	if now.Before(s.checked) {
		slog.Warn("The clock was set back, nothing is due", "checked", s.checked, "now", now)
	} else {
		s.startDueEpisodes(now)
	}
	s.checked = now
	s.snapshot = s.source.Schedules()
}

// startDueEpisodes asks for an Episode of each CastProfile that has a Schedule due after the later of s.checked and
// the start of its latest run, up to and including now. Several due Schedules of a CastProfile make one Episode.
func (s *Scheduler) startDueEpisodes(now time.Time) {
	for _, profile := range s.snapshot {
		key := profileKey{userID: profile.UserID, castProfileID: profile.CastProfileID}
		due, ok := profile.NextDue(later(s.checked, s.started[key]), s.location)
		if !ok || due.After(now) {
			continue
		}
		slog.Info("Schedule is due", "user", profile.UserID, "profile", profile.CastProfileID, "due", due, "now", now)
		s.queueEpisode(profile.UserID, profile.CastProfileID, now)
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
func (s *Scheduler) sleepTime() time.Duration {
	sleep := s.maxSleep
	now := s.clock.Now()
	for _, profile := range s.snapshot {
		if due, ok := profile.NextDue(now, s.location); ok {
			sleep = min(sleep, due.Sub(now))
		}
	}
	slog.Debug("Scheduler sleeps", "for", sleep)
	return sleep
}
