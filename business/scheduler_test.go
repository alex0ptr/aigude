package business

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// movableClock is a Clock that tests move by hand.
type movableClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *movableClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *movableClock) Sleep(time.Duration) {}

func (c *movableClock) set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}

// definedSchedules is a ScheduleSource whose Schedules tests change by hand.
type definedSchedules struct {
	mu       sync.Mutex
	profiles []ScheduledProfile
	changes  chan struct{}
}

func newDefinedSchedules(profiles ...ScheduledProfile) *definedSchedules {
	return &definedSchedules{profiles: profiles, changes: make(chan struct{}, 1)}
}

func (d *definedSchedules) Schedules() []ScheduledProfile {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]ScheduledProfile(nil), d.profiles...)
}

func (d *definedSchedules) Changes() (<-chan struct{}, func()) { return d.changes, func() {} }

func (d *definedSchedules) define(profiles ...ScheduledProfile) {
	d.mu.Lock()
	d.profiles = profiles
	d.mu.Unlock()
	d.changes <- struct{}{}
}

// latestEpisodes is an EpisodeRepository that knows the date of the latest Episode of some CastProfiles.
type latestEpisodes struct {
	EpisodeRepository
	dates map[string]time.Time
}

func (l latestEpisodes) List(userID string, opts ListOpts) ([]Episode, error) {
	date, ok := l.dates[userID+"/"+opts.CastProfileID]
	if !ok {
		return nil, nil
	}
	return []Episode{{UserID: userID, CastProfileID: opts.CastProfileID, Date: date}}, nil
}

func daily(hour int) Schedule { return Schedule{Weekdays: everyDay, Hour: hour, Enabled: true} }

func morning(schedules ...Schedule) ScheduledProfile {
	return ScheduledProfile{UserID: "alex", CastProfileID: "morning", Enabled: true, Schedules: schedules}
}

// at is a time on 29 October 2026 in Berlin.
func at(t *testing.T, hour, minute int) time.Time {
	t.Helper()
	return time.Date(2026, time.October, 29, hour, minute, 0, 0, berlin(t))
}

type schedulerRig struct {
	scheduler *Scheduler
	clock     *movableClock
	source    *definedSchedules
	queue     *fakeQueue
}

func newSchedulerRig(t *testing.T, now time.Time, dates map[string]time.Time, profiles ...ScheduledProfile) schedulerRig {
	t.Helper()
	rig := schedulerRig{clock: &movableClock{now: now}, source: newDefinedSchedules(profiles...), queue: &fakeQueue{}}
	library := NewEpisodeLibrary(latestEpisodes{dates: dates}, &recordingAudio{})
	rig.scheduler = NewScheduler(rig.source, rig.queue, library, rig.clock, berlin(t), time.Hour)
	return rig
}

func (r schedulerRig) tickAt(t time.Time) {
	r.clock.set(t)
	r.scheduler.tick()
}

func TestAScheduleQueuesAnEpisodeAtItsTimeAndOnlyOnce(t *testing.T) {
	rig := newSchedulerRig(t, at(t, 7, 0), nil, morning(daily(8)))
	rig.scheduler.begin()

	rig.tickAt(at(t, 7, 59))
	assert.Empty(t, rig.queue.requested, "not before its time")
	rig.tickAt(at(t, 8, 0))
	rig.tickAt(at(t, 8, 30))

	assert.Equal(t, []string{"alex/morning"}, rig.queue.requested, "once, at 8:00")
}

func TestSwitchedOffCastProfilesAndSchedulesNeverRun(t *testing.T) {
	off := morning(daily(8))
	off.Enabled = false
	offSchedule := Schedule{Weekdays: everyDay, Hour: 8}
	rig := newSchedulerRig(t, at(t, 7, 0), nil, off, ScheduledProfile{UserID: "alex", CastProfileID: "evening", Enabled: true, Schedules: []Schedule{offSchedule}})
	rig.scheduler.begin()

	rig.tickAt(at(t, 9, 0))

	assert.Empty(t, rig.queue.requested)
}

func TestAJumpOfTheClockStillRunsWhatIsDueOnceAndSeveralSchedulesMakeOneEpisode(t *testing.T) {
	rig := newSchedulerRig(t, at(t, 7, 0), nil, morning(daily(8), daily(12), daily(18)))
	rig.scheduler.begin()

	rig.tickAt(at(t, 20, 0))

	assert.Equal(t, []string{"alex/morning"}, rig.queue.requested, "three due runs, one Episode")
}

func TestAClockStepBackDoesNotRunTheSameDueTimeTwice(t *testing.T) {
	rig := newSchedulerRig(t, at(t, 7, 0), nil, morning(daily(8)))
	rig.scheduler.begin()
	rig.tickAt(at(t, 8, 1))

	rig.tickAt(at(t, 7, 30))
	rig.tickAt(at(t, 8, 15))

	assert.Equal(t, []string{"alex/morning"}, rig.queue.requested)
}

func TestWhatIsDueIsJudgedByTheSchedulesThatWereInForceAtTheLastLook(t *testing.T) {
	rig := newSchedulerRig(t, at(t, 7, 0), nil, morning(daily(8)))
	rig.scheduler.begin()
	rig.source.profiles = []ScheduledProfile{morning(daily(23))}

	rig.tickAt(at(t, 9, 0))
	rig.tickAt(at(t, 10, 0))

	assert.Equal(t, []string{"alex/morning"}, rig.queue.requested, "the 8:00 run of the old Schedule was due, the new one is not yet")
}

func TestARunMissedWhileTheServiceWasOffIsMadeUpOnceFromTheLatestEpisode(t *testing.T) {
	yesterday := at(t, 8, 5).AddDate(0, 0, -1)
	for name, tc := range map[string]struct {
		dates   map[string]time.Time
		profile ScheduledProfile
		want    []string
	}{
		"Episode older than the due run":   {dates: map[string]time.Time{"alex/morning": yesterday}, profile: morning(daily(8)), want: []string{"alex/morning"}},
		"several missed runs make one":     {dates: map[string]time.Time{"alex/morning": yesterday.AddDate(0, 0, -3)}, profile: morning(daily(8), daily(9)), want: []string{"alex/morning"}},
		"Episode made after the due run":   {dates: map[string]time.Time{"alex/morning": at(t, 8, 30)}, profile: morning(daily(8)), want: nil},
		"CastProfile without any Episode":  {dates: nil, profile: morning(daily(8)), want: nil},
		"another CastProfile has Episodes": {dates: map[string]time.Time{"alex/evening": yesterday}, profile: morning(daily(8)), want: nil},
		"CastProfile switched off":         {dates: map[string]time.Time{"alex/morning": yesterday}, profile: ScheduledProfile{UserID: "alex", CastProfileID: "morning", Schedules: []Schedule{daily(8)}}, want: nil},
	} {
		t.Run(name, func(t *testing.T) {
			rig := newSchedulerRig(t, at(t, 10, 0), tc.dates, tc.profile)

			rig.scheduler.begin()

			assert.Equal(t, tc.want, rig.queue.requested)
		})
	}
}

func TestARunMadeUpAtTheStartIsNotRunAgainByTheFirstTick(t *testing.T) {
	rig := newSchedulerRig(t, at(t, 10, 0), map[string]time.Time{"alex/morning": at(t, 7, 0).AddDate(0, 0, -1)}, morning(daily(8)))
	rig.scheduler.begin()

	rig.tickAt(at(t, 10, 1))

	assert.Equal(t, []string{"alex/morning"}, rig.queue.requested)
}

func TestTheSleepEndsAtTheNextDueTimeAndNeverExceedsTheCap(t *testing.T) {
	rig := newSchedulerRig(t, at(t, 7, 0), nil, morning(daily(7)))
	rig.scheduler.begin()
	soon := newSchedulerRig(t, at(t, 7, 30), nil, morning(daily(8)))
	soon.scheduler.begin()
	nothing := newSchedulerRig(t, at(t, 7, 30), nil)
	nothing.scheduler.begin()

	assert.Equal(t, time.Hour, rig.scheduler.sleepTime(), "the next run is a day away, the cap is an hour")
	assert.Equal(t, 30*time.Minute, soon.scheduler.sleepTime())
	assert.Equal(t, time.Hour, nothing.scheduler.sleepTime(), "without Schedules it still looks at the clock now and then")
}

func TestTheSchedulerWakesWhenTheSchedulesChangeAndStopsWhenItsContextIsCancelled(t *testing.T) {
	rig := newSchedulerRig(t, at(t, 7, 0), nil, morning(daily(8)))
	ctx, cancel := context.WithCancel(context.Background())
	done := rig.scheduler.Run(ctx)

	rig.clock.set(at(t, 8, 10))
	require.Empty(t, rig.queue.calls(), "it sleeps for an hour, nothing wakes it by itself")
	rig.source.define(morning(daily(8)))

	require.Eventually(t, func() bool { return len(rig.queue.calls()) == 1 }, time.Second, time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("the scheduler did not stop")
	}
}
