package service

import (
	"log/slog"

	"github.com/alex0ptr/aigude/business"
	"github.com/alex0ptr/aigude/pkg/rss"
	"github.com/alex0ptr/aigude/pkg/userconfig"
)

// castProfileOf turns a CastProfile of the User Config into the one the business layer produces Episodes from,
// with a ContentRepository behind each of its Subscriptions.
func castProfileOf(user userconfig.User, p userconfig.CastProfile, crawler business.Crawler) business.CastProfile {
	profile := business.CastProfile{
		ID: p.ID, Name: p.DisplayName, UserPrompt: p.Prompt, MaxLookback: p.MaxLookback, WordBudget: p.WordBudget,
		Voice: business.Voice{Language: p.Language, Name: p.Voice},
	}
	for _, s := range p.Subscriptions {
		switch s.Kind {
		case userconfig.KindRSS:
			profile.Subscriptions = append(profile.Subscriptions, business.Subscription{Repository: rss.NewRepository(s.URL, crawler, rss.Opts{DropQueryParams: s.DropQueryParams})})
		default:
			slog.Warn("Ignoring Subscription of an unknown kind", "user", user.ID, "profile", p.ID, "subscription", s.ID, "kind", s.Kind)
		}
	}
	return profile
}
