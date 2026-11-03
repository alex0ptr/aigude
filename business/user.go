package business

import "time"

// User is a person using AI Gude.
type User struct {
	ID string
}

// Voice is how Episodes are spoken: a language (a BCP-47 code such as de-DE) and the name of a speech voice.
type Voice struct {
	Language string
	Name     string
}

// Or returns the Voice with every part it leaves empty taken from the fallback.
func (v Voice) Or(fallback Voice) Voice {
	if v.Language == "" {
		v.Language = fallback.Language
	}
	if v.Name == "" {
		v.Name = fallback.Name
	}
	return v
}

// CastProfile is a user's standing definition of a show: which content to draw from, which time window
// to cover and which style to use. Episodes are produced from it.
type CastProfile struct {
	ID            string
	Name          string
	Subscriptions []Subscription
	UserPrompt    string
	// MaxLookback is how far back from the moment of production the publication date of Content may lie.
	MaxLookback time.Duration
	// WordBudget is the number of spoken words an Episode of the show aims for and should not exceed; zero means the
	// service's.
	WordBudget int
	// Voice overrides the default Voice of Episodes in every part that is not empty.
	Voice Voice
}

// Subscription is a User's registered origin of Content, read through a ContentRepository.
type Subscription struct {
	Repository ContentRepository
}
