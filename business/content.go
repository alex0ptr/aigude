package business

import (
	"log/slog"
	"net/url"
	"slices"
	"strings"
	"time"
)

// ContentRepository provides Content for the production of Episodes.
// This can include RSS feeds, articles, blog posts, or any other text-based content.
type ContentRepository interface {
	// Content returns the items published from since up to and including until. The Usage is what retrieving them
	// consumed, also when it fails.
	Content(since, until time.Time, opts ContentFetchOpts) ([]Content, Usage, error)
}

// ContentFetchOpts are the optional parameters of ContentRepository.Content.
type ContentFetchOpts struct {
	// Skip says for the identity of an item (Content.Identity) whether it is left out; the repository asks before it
	// spends anything on the item, such as retrieving its page. Nothing is skipped when it is not set.
	Skip func(identity string) bool
}

// Content is one item fetched through a Subscription, such as an article.
type Content struct {
	Created time.Time
	Title   string
	Body    string
	// Retrieved is when the text of Body was retrieved from its origin; the zero time when that is not known, such as
	// for the text of a feed.
	Retrieved time.Time
	// Source of content, useful for the LLM.
	Source string
	// Link to the original content, if available.
	Link string
	// Identity tells the item from all others of its CastProfile; see ContentIdentity. It is empty when the item
	// cannot be told apart from others.
	Identity string
}

// IdentityOpts says how the identity of the items of a Subscription is formed.
type IdentityOpts struct {
	// DropQueryParams are the names of the query parameters that do not belong to the identity of a link, such as a
	// tracking parameter. The names are exact; the rest of the query stays.
	DropQueryParams []string
}

// ContentIdentity is the identity of an item: its link without the fragment and without the query parameters that
// opts lists, or the GUID the origin gave it when it has no link. It is empty when the item has neither.
func ContentIdentity(link, guid string, opts IdentityOpts) string {
	link, _, _ = strings.Cut(strings.TrimSpace(link), "#")
	if link == "" {
		return strings.TrimSpace(guid)
	}
	base, query, hasQuery := strings.Cut(link, "?")
	if !hasQuery || len(opts.DropQueryParams) == 0 {
		return link
	}
	slog.Debug("Dropping query parameters from a link", "link", link, "names", opts.DropQueryParams)
	var kept []string
	for _, pair := range strings.Split(query, "&") {
		name, _, _ := strings.Cut(pair, "=")
		if unescaped, err := url.QueryUnescape(name); err == nil {
			name = unescaped
		}
		if !slices.Contains(opts.DropQueryParams, name) {
			kept = append(kept, pair)
		}
	}
	if len(kept) == 0 {
		return base
	}
	return base + "?" + strings.Join(kept, "&")
}
