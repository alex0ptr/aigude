// Package userconfig reads the User Config files of the Users: one TOML file
// per User holding their CastProfiles with Subscriptions and Schedules.
//
// Files are decoded strictly with go-toml/v2: an unknown key is an error. Every problem is reported with file name,
// line and column, and all problems of all files are reported together so one
// run shows everything that needs fixing. The result of loading a folder is an
// immutable Snapshot.
package userconfig

import (
	"sort"
	"strings"
	"time"

	"github.com/alex0ptr/aigude/business"
)

// SubscriptionKind says which ContentRepository reads a Subscription.
type SubscriptionKind string

// KindRSS is a Subscription to one RSS or Atom feed.
const KindRSS SubscriptionKind = "rss"

// User is a person using AI Gude together with everything they have set up.
type User struct {
	// ID is the stable, unique name of the User.
	ID string
	// DisplayName is shown in the interface.
	DisplayName string
	// CastProfiles are the User's shows, in file order.
	CastProfiles []CastProfile
}

// CastProfile returns the User's CastProfile with the Id.
func (u User) CastProfile(id string) (CastProfile, bool) {
	for _, p := range u.CastProfiles {
		if p.ID == id {
			return p, true
		}
	}
	return CastProfile{}, false
}

// Subscription is a registered origin of Content. Its Id is unique within its CastProfile.
type Subscription struct {
	ID          string
	DisplayName string
	Kind        SubscriptionKind
	// URL is the feed address of an RSS Subscription.
	URL string
	// DropQueryParams are the names of the query parameters that are ignored when the identity of its Content is
	// formed; none by default.
	DropQueryParams []string
}

// CastProfile is a User's standing definition of a show.
type CastProfile struct {
	ID          string
	DisplayName string
	// Subscriptions are the origins of Content the show draws on, in file order.
	Subscriptions []Subscription
	// MaxLookback is how far back from the moment of production the publication date of Content may lie.
	MaxLookback time.Duration
	// WordBudget is the number of spoken words an Episode of the show aims for and should not exceed; zero means the
	// service's.
	WordBudget int
	// Prompt replaces the default style prompt when not empty.
	Prompt string
	// Language and Voice override the service defaults when not empty.
	Language string
	Voice    string
	// Enabled is false when the CastProfile is switched off.
	Enabled   bool
	Schedules []Schedule
}

// MaxLookbackText is the Max Lookback in the notation of the User Config, such as "24h" or "90m".
func (p CastProfile) MaxLookbackText() string {
	text := p.MaxLookback.String()
	if strings.HasSuffix(text, "m0s") {
		text = strings.TrimSuffix(text, "0s")
	}
	if strings.HasSuffix(text, "h0m") {
		text = strings.TrimSuffix(text, "0m")
	}
	return text
}

// Schedule is the Schedule of the business layer; the User Config states it for a CastProfile.
type Schedule = business.Schedule

// Snapshot is the parsed User Config of all Users. It is never modified
// after it is built, so it can be shared and swapped as a whole.
type Snapshot struct {
	users    []User
	problems Problems
}

// newSnapshot orders the Users by Display Name, then Id, as a dropdown lists them.
func newSnapshot(users []User, problems Problems) *Snapshot {
	sorted := append([]User(nil), users...)
	sort.Slice(sorted, func(i, j int) bool {
		a, b := strings.ToLower(sorted[i].DisplayName), strings.ToLower(sorted[j].DisplayName)
		if a != b {
			return a < b
		}
		return sorted[i].ID < sorted[j].ID
	})
	return &Snapshot{users: sorted, problems: append(Problems(nil), problems...)}
}

// Users returns all Users ordered by Display Name.
func (s *Snapshot) Users() []User {
	return append([]User(nil), s.users...)
}

// User returns the User with the given Id.
func (s *Snapshot) User(id string) (User, bool) {
	for _, u := range s.users {
		if u.ID == id {
			return u, true
		}
	}
	return User{}, false
}

// Problems returns what is wrong in the files behind this Snapshot. The Users
// of such files are still here with their last valid setup, or missing when
// there was none. A Snapshot from LoadDir has none, because LoadDir fails on any problem.
func (s *Snapshot) Problems() Problems {
	return append(Problems(nil), s.problems...)
}
