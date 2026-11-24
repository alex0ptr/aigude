package service

import (
	"github.com/alex0ptr/aigude/business"
	"github.com/alex0ptr/aigude/pkg/userconfig"
)

// configSchedules implements business.ScheduleSource with the User Config that is in force.
type configSchedules struct {
	store *userconfig.Store
}

// Schedules returns the CastProfiles of all Users with their Schedules as the User Config defines them now.
func (c configSchedules) Schedules() []business.ScheduledProfile {
	var profiles []business.ScheduledProfile
	for _, user := range c.store.Snapshot().Users() {
		for _, profile := range user.CastProfiles {
			profiles = append(profiles, business.ScheduledProfile{
				UserID: user.ID, CastProfileID: profile.ID, Enabled: profile.Enabled, Schedules: profile.Schedules,
			})
		}
	}
	return profiles
}

// Changes signals every change of the User Config.
func (c configSchedules) Changes() (<-chan struct{}, func()) {
	return c.store.Subscribe()
}
