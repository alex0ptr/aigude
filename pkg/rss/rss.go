package rss

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/alex0ptr/aigude/business"
	"github.com/alex0ptr/aigude/pkg"
	"github.com/mmcdole/gofeed"
)

// Repository implements business.ContentRepository for RSS feeds.
type Repository struct {
	feedURL  string
	crawler  business.Crawler
	client   *http.Client
	identity business.IdentityOpts
}

// Opts are the optional parameters of NewRepository.
type Opts struct {
	// DropQueryParams are the names of the query parameters of an item's link that do not belong to its identity.
	DropQueryParams []string
}

// NewRepository creates a new RSS content repository for a single feed URL. The pages the items link to are
// retrieved through the crawler; the text of the feed itself is what an item falls back to.
func NewRepository(feedURL string, crawler business.Crawler, opts Opts) *Repository {
	slog.Debug("Creating RSS repository", "feedURL", feedURL, "dropQueryParams", opts.DropQueryParams)
	return &Repository{
		feedURL:  feedURL,
		crawler:  crawler,
		client:   pkg.NewHTTPClient(30 * time.Second),
		identity: business.IdentityOpts{DropQueryParams: opts.DropQueryParams},
	}
}

// Content fetches and parses the RSS feed, returning the content items inside the requested time window.
// Items without a publication date are skipped.
func (r *Repository) Content(since, until time.Time, opts business.ContentFetchOpts) ([]business.Content, business.Usage, error) {
	slog.Info("Fetching RSS feed", "feedURL", r.feedURL)

	feed, err := r.fetchFeed()
	if err != nil {
		return nil, nil, err
	}

	slog.Debug("Parsing RSS items", "itemCount", len(feed.Items), "feedTitle", feed.Title)

	contents, usage := r.convertItems(feed, since, until, opts)

	slog.Debug("Items after time filtering", "count", len(contents))

	slog.Info("RSS content fetched successfully", "finalCount", len(contents))

	return contents, usage, nil
}

// fetchFeed fetches and parses the RSS feed from the configured URL.
func (r *Repository) fetchFeed() (*gofeed.Feed, error) {
	fp := gofeed.NewParser()
	fp.Client = r.client
	feed, err := fp.ParseURL(r.feedURL)
	if err != nil {
		slog.Error("Failed to fetch RSS feed", "feedURL", r.feedURL, "error", err)
		return nil, fmt.Errorf("failed to fetch RSS feed: %w", err)
	}
	return feed, nil
}

// convertItems converts RSS feed items to business.Content, applying time filtering. The Usage is what retrieving
// the pages of the items consumed.
func (r *Repository) convertItems(feed *gofeed.Feed, since, until time.Time, opts business.ContentFetchOpts) ([]business.Content, business.Usage) {
	var contents []business.Content
	var usage business.Usage

	for _, item := range feed.Items {
		created := parseItemDate(item)

		if created.IsZero() {
			slog.Warn("Skipping item without a publication date", "feedURL", r.feedURL, "title", item.Title, "link", item.Link)
			continue
		}
		if !passesTimeFilter(created, since, until) {
			slog.Debug("Filtering out item by time", "title", item.Title, "created", created)
			continue
		}
		identity := business.ContentIdentity(item.Link, item.GUID, r.identity)
		if identity == "" {
			slog.Warn("Item has neither link nor GUID, it cannot be remembered as used", "feedURL", r.feedURL, "title", item.Title)
		}
		if identity != "" && opts.Skip != nil && opts.Skip(identity) {
			slog.Debug("Skipping item that is used already", "title", item.Title, "identity", identity)
			continue
		}

		content := convertItem(item, feed.Title, created)
		content.Identity = identity
		content, pageUsage := r.readPage(content)
		usage = usage.Add(pageUsage)
		contents = append(contents, content)
	}

	return contents, usage
}

// readPage returns the content with the text replaced by the cleaned text of the page the item links to and notes when
// the page was retrieved. When the page cannot be retrieved or holds no text after cleaning, the text of the feed
// stays. The Usage is what retrieving the page consumed.
func (r *Repository) readPage(content business.Content) (business.Content, business.Usage) {
	if content.Link == "" {
		slog.Debug("Item has no link, using the feed text", "title", content.Title)
		return content, nil
	}
	page, usage, err := r.crawler.Crawl(content.Link)
	if err != nil {
		slog.Warn("Page could not be retrieved, using the feed text", "link", content.Link, "error", err)
		return content, usage
	}
	text := business.CleanPageText(page.Text)
	if text == "" {
		slog.Warn("Page has no text after cleaning, using the feed text", "link", content.Link)
		return content, usage
	}
	content.Body, content.Retrieved = text, page.RetrievedAt
	return content, usage
}

// parseItemDate extracts the publication date from an RSS item.
// It prefers the published date over the updated date.
func parseItemDate(item *gofeed.Item) time.Time {
	if item.PublishedParsed != nil {
		return *item.PublishedParsed
	}
	if item.UpdatedParsed != nil {
		return *item.UpdatedParsed
	}
	return time.Time{}
}

// passesTimeFilter tells whether a publication date lies from since up to and including until.
func passesTimeFilter(created, since, until time.Time) bool {
	return !created.Before(since) && !created.After(until)
}

// convertItem converts a single RSS item to business.Content.
func convertItem(item *gofeed.Item, feedTitle string, created time.Time) business.Content {
	body := extractBody(item)

	return business.Content{
		Created: created,
		Title:   item.Title,
		Body:    body,
		Source:  feedTitle,
		Link:    item.Link,
	}
}

// extractBody extracts and cleans the body content from an RSS item.
// It prefers full content over description and strips HTML tags.
func extractBody(item *gofeed.Item) string {
	body := item.Content
	if body == "" {
		body = item.Description
	}
	return stripHTML(body)
}

// stripHTML removes HTML tags from a string using a simple tag-stripping algorithm.
func stripHTML(s string) string {
	var result strings.Builder
	inTag := false

	for _, r := range s {
		if r == '<' {
			inTag = true
			continue
		}
		if r == '>' {
			inTag = false
			continue
		}
		if !inTag {
			result.WriteRune(r)
		}
	}

	return strings.TrimSpace(result.String())
}
