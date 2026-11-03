package service_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/alex0ptr/aigude/business"
	"github.com/alex0ptr/aigude/pkg/service/servicetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// anOutage stops the service, lets the clock run for the duration while nothing watches it, and starts the service
// again.
func anOutage(t *testing.T, h *servicetest.Harness, duration time.Duration) {
	t.Helper()
	h.Stop(t)
	h.Clock.Advance(duration)
	h.Restart(t)
}

// madeUp is the log line with which the scheduler announces a run it makes up.
const madeUp = "Making up a missed run"

// harnessWithOneScheduledEpisode starts the service with a daily Schedule at 08:30 and lets it make the Episode of
// the first day.
func harnessWithOneScheduledEpisode(t *testing.T, profileKeys string) *servicetest.Harness {
	t.Helper()
	h, _ := freshFeedHarness(t, profileKeys)
	h.Clock.Advance(31 * time.Minute)
	waitForEpisodes(t, h, "alex", 1)
	return h
}

func TestARunMissedDuringAnOutageIsMadeUpWhenTheServiceIsBack(t *testing.T) {
	h := harnessWithOneScheduledEpisode(t, scheduleAt(everyDay, "08:30"))

	anOutage(t, h, aDay) // the next day's 08:30 passes while the service is off

	waitForEpisodes(t, h, "alex", 2)
}

func TestSeveralMissedRunsAreMadeUpAsOne(t *testing.T) {
	h := harnessWithOneScheduledEpisode(t, scheduleAt(everyDay, "08:30"))
	logs := captureLogs(t)

	anOutage(t, h, 3*aDay)

	waitForEpisodes(t, h, "alex", 2)
	assertEpisodes(t, h, 2)
	made := 0
	for _, line := range logs.Lines() {
		if strings.Contains(line, madeUp) {
			made++
		}
	}
	assert.Equal(t, 1, made, "three missed runs, one made up: %v", logs.Lines())
}

func TestARestartThatMissedNothingMakesNothing(t *testing.T) {
	logs := captureLogs(t)
	h := harnessWithOneScheduledEpisode(t, scheduleAt(everyDay, "08:30"))

	anOutage(t, h, 2*time.Hour) // still the same day, the next 08:30 is tomorrow

	assertEpisodes(t, h, 1)
	assert.False(t, logs.Contains(madeUp), "no run is made up: %v", logs.Lines())
}

func TestTheMadeUpRunIsRememberedAcrossRestarts(t *testing.T) {
	h := harnessWithOneScheduledEpisode(t, scheduleAt(everyDay, "08:30"))
	anOutage(t, h, aDay)
	waitForEpisodes(t, h, "alex", 2)
	logs := captureLogs(t)

	anOutage(t, h, time.Hour)

	assertEpisodes(t, h, 2)
	assert.False(t, logs.Contains(madeUp), "the Episode that was made up counts: %v", logs.Lines())
}

func TestAFailedScheduledRunIsNotMadeUpAfterARestart(t *testing.T) {
	logs := captureLogs(t)
	h, _ := freshFeedHarness(t, scheduleAt(everyDay, "08:30"))
	h.ScriptWriter.Err = errors.New("model unreachable")
	h.Clock.Advance(31 * time.Minute)
	waitForEpisodes(t, h, "alex", 1)

	anOutage(t, h, time.Hour)

	assertEpisodes(t, h, 1)
	assert.False(t, logs.Contains(madeUp), "no run is made up: %v", logs.Lines())
}

func TestASwitchedOffCastProfileOrScheduleIsNotMadeUp(t *testing.T) {
	tests := map[string]string{
		"a switched off CastProfile": "enabled = false\n" + scheduleAt(everyDay, "08:30"),
		"a switched off Schedule":    scheduleAt(everyDay, "08:30") + "enabled = false\n",
	}
	for name, keys := range tests {
		t.Run(name, func(t *testing.T) {
			logs := captureLogs(t)
			h, _ := freshFeedHarness(t, keys)
			require.Equal(t, 200, h.Post(t, "/cast-profiles/morning/generate").Status)
			waitForEpisodes(t, h, "alex", 1)

			anOutage(t, h, aDay)

			assertEpisodes(t, h, 1)
			assert.False(t, logs.Contains(madeUp), "no run is made up: %v", logs.Lines())
		})
	}
}

func TestACastProfileThatNeverMadeAnEpisodeIsNotMadeUp(t *testing.T) {
	logs := captureLogs(t)
	h, _ := freshFeedHarness(t, scheduleAt(everyDay, "08:30"))

	anOutage(t, h, aDay)

	assertEpisodes(t, h, 0)
	assert.Empty(t, h.ScriptWriter.Requests())
	assert.False(t, logs.Contains(madeUp), "no run is made up: %v", logs.Lines())
}

func TestMissedRunsAreTrackedPerCastProfile(t *testing.T) {
	logs := captureLogs(t)
	h, feed := freshFeedHarness(t, scheduleAt(everyDay, "08:30"))
	noon := fmt.Sprintf("\n[cast_profiles.noon]\ndisplay_name = \"Noon\"\nmax_lookback = \"24h\"\n%s\n"+
		"[cast_profiles.noon.subscriptions.news]\ndisplay_name = \"News\"\nkind = \"rss\"\nurl = %q\n",
		strings.ReplaceAll(scheduleAt(everyDay, "08:30"), "morning", "noon"), feed)
	servicetest.WriteUserConfig(t, h.Settings, "alex.toml", generatingFile(feed, scheduleAt(everyDay, "08:30"))+noon)
	h.WaitFor(t, "/", "the second CastProfile", pageContains("Noon"))
	// Two Episodes on the second day: Morning's was made at 07:30, before its 08:30 run, Noon's at 08:45, after it.
	morning := episodeOf("alex", "Morning before its run", startedAt.Add(aDay-30*time.Minute))
	noonEpisode := episodeOf("alex", "Noon after its due time", startedAt.Add(aDay+45*time.Minute))
	noonEpisode.CastProfileID = "noon"
	h.AddEpisode(t, morning, []byte("a"))
	h.AddEpisode(t, noonEpisode, []byte("a"))

	anOutage(t, h, aDay+time.Hour) // 09:00 on the second day

	assert.True(t, logs.Contains(madeUp, "profile=morning"), "Morning's run of the second day was missed: %v", logs.Lines())
	assert.False(t, logs.Contains(madeUp, "profile=noon"), "Noon has an Episode made after its due time: %v", logs.Lines())
	waitForEpisodes(t, h, "alex", 3)
	assert.Equal(t, 1, countProfile(h.Episodes(t, "alex"), "noon"))
}

// countProfile counts the Episodes of a CastProfile.
func countProfile(episodes []business.Episode, castProfileID string) int {
	n := 0
	for _, e := range episodes {
		if e.CastProfileID == castProfileID {
			n++
		}
	}
	return n
}

func TestARunThatFoundNothingNewIsMadeUpAfterARestartOnceContentHasArrived(t *testing.T) {
	logs := captureLogs(t)
	feed := servicetest.ServeChangingFeed(t, feedOfItems(feedItem{title: "Erste", link: "https://example.org/a", description: "A", published: inTheWindow(2)}))
	h := servicetest.Start(t, servicetest.WithUserConfigFile("alex.toml", generatingFile(feed.URL, scheduleAt(everyDay, "08:30"))))
	h.ScriptWriter.Script = threeSections()
	h.Clock.Advance(31 * time.Minute)
	waitForEpisodes(t, h, "alex", 1) // the first day's Episode
	h.Clock.Advance(aDay)            // the second day's run finds nothing new and makes no Episode, so nothing marks it as done
	waitForLog(t, logs, nothingNew)
	feed.Set(feedOfItems(feedItem{title: "Zweite", link: "https://example.org/b", description: "B", published: h.Clock.Now().Add(-time.Hour)}))

	h.Stop(t)
	h.Restart(t)

	waitForEpisodes(t, h, "alex", 2)
	assert.True(t, logs.Contains(madeUp, "profile=morning"))
	assert.Equal(t, []string{"Zweite"}, titlesOf(h.ScriptWriter.Requests()[1]))
}
