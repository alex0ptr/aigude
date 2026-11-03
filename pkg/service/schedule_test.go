package service_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alex0ptr/aigude/pkg/service/servicetest"
	"github.com/alex0ptr/aigude/pkg/settings"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The test clock starts on Thursday 29 October 2026, 08:00 in Berlin.
const (
	everyDay = `"mon", "tue", "wed", "thu", "fri", "sat", "sun"`
	// settleTime is how long a test waits to be sure that the scheduler did not do anything, many times its sleep.
	settleTime = 150 * time.Millisecond
	aDay       = 24 * time.Hour
)

// scheduleAt is a Schedule on the weekdays at a time of day, to go into the CastProfile of generatingFile.
func scheduleAt(weekdays, at string) string {
	return fmt.Sprintf("[[cast_profiles.morning.schedules]]\nweekdays = [%s]\ntime = %q\n", weekdays, at)
}

// freshFeedHarness starts the service with a feed that has a new item at every request, published an hour before the
// clock's time, so that every Episode finds Content of its own. profileKeys go into the CastProfile.
// It returns the address of the feed as well.
func freshFeedHarness(t *testing.T, profileKeys string, opts ...servicetest.Option) (*servicetest.Harness, string) {
	t.Helper()
	var h *servicetest.Harness
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		now := h.Clock.Now()
		_, _ = fmt.Fprintf(w, `<?xml version="1.0"?><rss version="2.0"><channel><title>Fresh</title>
<item><guid>g%d</guid><title>News of %s</title><link>https://example.org/%d</link><description>Text</description><pubDate>%s</pubDate></item>
</channel></rss>`, now.UnixNano(), now.Format(time.RFC3339), now.UnixNano(), now.Add(-time.Hour).Format(time.RFC1123Z))
	}))
	t.Cleanup(srv.Close)
	h = servicetest.Start(t, append([]servicetest.Option{servicetest.WithUserConfigFile("alex.toml", generatingFile(srv.URL, profileKeys))}, opts...)...)
	h.ScriptWriter.Script = threeSections()
	return h, srv.URL
}

// assertEpisodes waits a moment, so that anything that is going to happen has, and checks how many Episodes there are.
func assertEpisodes(t *testing.T, h *servicetest.Harness, want int) {
	t.Helper()
	time.Sleep(settleTime)
	assert.Len(t, h.Episodes(t, "alex"), want)
}

func TestAScheduleProducesAnEpisodeAtItsTimeAndNotBefore(t *testing.T) {
	h, _ := freshFeedHarness(t, scheduleAt(everyDay, "08:30"))

	h.Clock.Advance(10 * time.Minute)
	assertEpisodes(t, h, 0)

	h.Clock.Advance(21 * time.Minute)
	episodes := waitForEpisodes(t, h, "alex", 1)
	assert.Equal(t, "morning", episodes[0].CastProfileID)
	assert.Empty(t, episodes[0].Failure)
}

func TestSwitchedOffCastProfilesAndSchedulesNeverRun(t *testing.T) {
	tests := map[string]string{
		"a switched off CastProfile": "enabled = false\n" + scheduleAt(everyDay, "08:30"),
		"a switched off Schedule":    scheduleAt(everyDay, "08:30") + "enabled = false\n",
	}
	for name, keys := range tests {
		t.Run(name, func(t *testing.T) {
			h, _ := freshFeedHarness(t, keys)

			h.Clock.Advance(aDay + time.Hour)

			assertEpisodes(t, h, 0)
		})
	}
}

func TestAChangedScheduleMovesTheRun(t *testing.T) {
	h, feed := freshFeedHarness(t, scheduleAt(everyDay, "08:30"))
	servicetest.WriteUserConfig(t, h.Settings, "alex.toml", strings.Replace(
		generatingFile(feed, scheduleAt(everyDay, "09:30")), "Morning briefing", "Early bird show", 1))
	h.WaitFor(t, "/", "the changed configuration", pageContains("Early bird show"))
	time.Sleep(settleTime)

	h.Clock.Advance(45 * time.Minute) // 08:45, the old time has passed
	assertEpisodes(t, h, 0)
	h.Clock.Advance(time.Hour) // 09:45
	waitForEpisodes(t, h, "alex", 1)
}

func TestTheSchedulerWakesWhenTheConfigurationChanges(t *testing.T) {
	h, feed := freshFeedHarness(t, scheduleAt(everyDay, "08:30"),
		servicetest.WithSettings(func(s *settings.Settings) { s.SchedulerMaxSleep = time.Hour }))
	h.Clock.Advance(31 * time.Minute)
	assertEpisodes(t, h, 0) // the scheduler sleeps until the clock it was told about reaches 08:30, which takes 30 minutes

	servicetest.WriteUserConfig(t, h.Settings, "alex.toml", strings.Replace(
		generatingFile(feed, scheduleAt(everyDay, "08:30")), "Morning briefing", "Early bird show", 1))

	waitForEpisodes(t, h, "alex", 1)
}

func TestTheSleepOfTheSchedulerNeverExceedsTheCap(t *testing.T) {
	h, _ := freshFeedHarness(t, scheduleAt(everyDay, "08:30"),
		servicetest.WithSettings(func(s *settings.Settings) { s.SchedulerMaxSleep = 200 * time.Millisecond }))

	h.Clock.Advance(31 * time.Minute)

	waitForEpisodes(t, h, "alex", 1)
}

func TestAJumpOfTheClockStillRunsWhatIsDueOnce(t *testing.T) {
	h, _ := freshFeedHarness(t, scheduleAt(everyDay, "08:30"))

	h.Clock.Advance(3*aDay + time.Hour) // three due runs

	waitForEpisodes(t, h, "alex", 1)
	assertEpisodes(t, h, 1)
}

func TestAClockStepBackDoesNotRunTheSameDueTimeTwice(t *testing.T) {
	h, _ := freshFeedHarness(t, scheduleAt(everyDay, "08:30"))
	h.Clock.Advance(31 * time.Minute) // 08:31
	waitForEpisodes(t, h, "alex", 1)

	h.Clock.Advance(-2 * time.Minute) // 08:29, as after a time server corrected a clock that ran fast
	time.Sleep(settleTime)
	h.Clock.Advance(2 * time.Minute) // 08:31 again
	time.Sleep(settleTime)

	assert.Len(t, h.ScriptWriter.Requests(), 1, "a second run would have asked the model for a Script")
}

func TestACastProfileThatIsGeneratingIsNotQueuedAgainByAClickOrASchedule(t *testing.T) {
	h, _ := freshFeedHarness(t, scheduleAt(everyDay, "08:30"))
	h.ScriptWriter.Delay = 300 * time.Millisecond

	require.Equal(t, 200, h.Post(t, "/cast-profiles/morning/generate").Status)
	require.Equal(t, 200, h.Post(t, "/cast-profiles/morning/generate").Status, "a second click is not an error")
	h.Clock.Advance(31 * time.Minute) // the Schedule is due while the first Episode is being made

	waitForEpisodes(t, h, "alex", 1)
	assertEpisodes(t, h, 1)
	assert.Len(t, h.ScriptWriter.Requests(), 1)

	require.Equal(t, 200, h.Post(t, "/cast-profiles/morning/generate").Status)
	waitForEpisodes(t, h, "alex", 2)
}

func TestTheSchedulerDoesNotPollWhileNothingIsDue(t *testing.T) {
	h, _ := freshFeedHarness(t, scheduleAt(everyDay, "08:30"),
		servicetest.WithSettings(func(s *settings.Settings) { s.SchedulerMaxSleep = time.Hour }))
	before := h.Clock.Reads()

	time.Sleep(300 * time.Millisecond)

	assert.Less(t, h.Clock.Reads()-before, 20, "it sleeps instead of looking at the clock all the time")
}
