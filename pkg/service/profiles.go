package service

import (
	"github.com/alex0ptr/aigude/business"
	"github.com/alex0ptr/aigude/pkg/userconfig"
)

// configProfiles implements business.CastProfiles and business.CastProfileSource with the User Config that is in
// force.
type configProfiles struct {
	store *userconfig.Store
	// crawler retrieves the pages that the items of the feeds of a CastProfile link to.
	crawler business.Crawler
}

// Exists tells whether the User Config has a User with that Id that has a CastProfile with that Id.
func (c configProfiles) Exists(userID, castProfileID string) bool {
	user, ok := c.store.Snapshot().User(userID)
	if !ok {
		return false
	}
	_, ok = user.CastProfile(castProfileID)
	return ok
}

// CastProfile returns the User and the CastProfile with the Id as the User Config defines them now, with a
// ContentRepository behind each Subscription.
func (c configProfiles) CastProfile(userID, castProfileID string) (business.User, business.CastProfile, bool) {
	user, ok := c.store.Snapshot().User(userID)
	if !ok {
		return business.User{}, business.CastProfile{}, false
	}
	profile, ok := user.CastProfile(castProfileID)
	if !ok {
		return business.User{}, business.CastProfile{}, false
	}
	return business.User{ID: user.ID}, castProfileOf(user, profile, c.crawler), true
}
