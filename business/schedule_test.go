package business

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var everyDay = []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday, time.Saturday, time.Sunday}

func berlin(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Berlin")
	require.NoError(t, err)
	return loc
}

func TestAScheduleIsDueAtItsWallClockTimeAlsoWhenDaylightSavingTimeEnds(t *testing.T) {
	daily := Schedule{Weekdays: everyDay, Hour: 8, Minute: 0, Enabled: true}

	// Saturday 24 October 2026, 08:30 in Berlin; the clocks go back during the night.
	next := daily.Next(time.Date(2026, time.October, 24, 6, 30, 0, 0, time.UTC), berlin(t))

	assert.True(t, next.Equal(time.Date(2026, time.October, 25, 7, 0, 0, 0, time.UTC)), "08:00 is 07:00 UTC after the change, no longer 06:00, got %s", next.UTC())
}

func TestAScheduleSkipsTheDaysItDoesNotNameAndNeverMeetsItsOwnInstantTwice(t *testing.T) {
	loc := berlin(t)
	fridays := Schedule{Weekdays: []time.Weekday{time.Friday}, Hour: 8, Minute: 30, Enabled: true}
	friday := time.Date(2026, time.October, 30, 8, 30, 0, 0, loc)

	assert.True(t, friday.Equal(fridays.Next(time.Date(2026, time.October, 29, 8, 0, 0, 0, loc), loc)), "from Thursday to Friday")
	assert.True(t, friday.AddDate(0, 0, 7).Equal(fridays.Next(friday, loc)), "at the instant itself, the next one is a week later")
}

func TestAScheduleKeepsItsWallClockTimeWhenDaylightSavingTimeStarts(t *testing.T) {
	daily := Schedule{Weekdays: everyDay, Hour: 8, Minute: 0, Enabled: true}

	// Saturday 28 March 2026, 08:30 in Berlin; the clocks go forward during the night.
	next := daily.Next(time.Date(2026, time.March, 28, 7, 30, 0, 0, time.UTC), berlin(t))

	assert.True(t, next.Equal(time.Date(2026, time.March, 29, 6, 0, 0, 0, time.UTC)), "08:00 is 06:00 UTC after the change, no longer 07:00, got %s", next.UTC())
}

func TestAScheduleInTheHourThatDoesNotExistIsDueOnceThatDay(t *testing.T) {
	loc := berlin(t)
	night := Schedule{Weekdays: everyDay, Hour: 2, Minute: 30, Enabled: true}

	// 02:30 does not exist on Sunday 29 March 2026 in Berlin.
	first := night.Next(time.Date(2026, time.March, 29, 0, 0, 0, 0, loc), loc)
	second := night.Next(first, loc)

	assert.Equal(t, 29, first.In(loc).Day(), "it is due on that day, at the closest time there is")
	assert.Equal(t, 30, second.In(loc).Day(), "and not a second time that day")
	assert.True(t, second.Equal(time.Date(2026, time.March, 30, 2, 30, 0, 0, loc)))
}

func TestACastProfileIsDueAtTheEarliestOfItsEnabledSchedulesAndNeverWhenSwitchedOff(t *testing.T) {
	loc := berlin(t)
	after := time.Date(2026, time.October, 29, 0, 0, 0, 0, loc)
	morning := Schedule{Weekdays: everyDay, Hour: 8, Enabled: true}
	evening := Schedule{Weekdays: everyDay, Hour: 18, Enabled: true}
	off := Schedule{Weekdays: everyDay, Hour: 6}
	profile := ScheduledProfile{Enabled: true, Schedules: []Schedule{evening, off, morning}}

	due, ok := profile.NextDue(after, loc)
	_, noneEnabled := ScheduledProfile{Enabled: true, Schedules: []Schedule{off}}.NextDue(after, loc)
	profile.Enabled = false
	_, switchedOff := profile.NextDue(after, loc)

	assert.True(t, ok)
	assert.True(t, due.Equal(time.Date(2026, time.October, 29, 8, 0, 0, 0, loc)), "the switched off Schedule at 6 does not count, got %s", due)
	assert.False(t, noneEnabled)
	assert.False(t, switchedOff)
}
