package business

import (
	"slices"
	"time"
)

// Schedule triggers an Episode on chosen weekdays at a time of day in the configured service time zone.
type Schedule struct {
	Weekdays []time.Weekday
	Hour     int
	Minute   int
	// Enabled is false when the Schedule is switched off.
	Enabled bool
}

// Next returns the first time after the given one at which the Schedule is due, in the time zone.
func (s Schedule) Next(after time.Time, loc *time.Location) time.Time {
	day := after.In(loc)
	for i := 0; i <= 7; i++ {
		d := day.AddDate(0, 0, i)
		candidate := time.Date(d.Year(), d.Month(), d.Day(), s.Hour, s.Minute, 0, 0, loc)
		if candidate.After(after) && slices.Contains(s.Weekdays, candidate.Weekday()) {
			return candidate
		}
	}
	return time.Time{}
}

// ScheduledProfile is a CastProfile of a User with its Schedules, as far as deciding when it is due needs them.
type ScheduledProfile struct {
	UserID        string
	CastProfileID string
	// Enabled is false when the CastProfile is switched off.
	Enabled   bool
	Schedules []Schedule
}

// NextDue returns the first time after the given one at which an enabled Schedule of the CastProfile is due, in the
// time zone, and false when the CastProfile is switched off or has no enabled Schedule.
func (p ScheduledProfile) NextDue(after time.Time, loc *time.Location) (time.Time, bool) {
	if !p.Enabled {
		return time.Time{}, false
	}
	var next time.Time
	for _, schedule := range p.Schedules {
		if !schedule.Enabled {
			continue
		}
		if due := schedule.Next(after, loc); !due.IsZero() && (next.IsZero() || due.Before(next)) {
			next = due
		}
	}
	return next, !next.IsZero()
}
