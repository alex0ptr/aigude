package rss

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alex0ptr/aigude/business"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var now = time.Date(2026, time.October, 29, 7, 0, 0, 0, time.UTC)

// item is an item of a test feed; an empty published leaves the date out.
type item struct {
	title, link string
	published   time.Time
}

// serveFeed serves a feed of the items and returns its address.
func serveFeed(t *testing.T, items ...item) string {
	t.Helper()
	var feed strings.Builder
	feed.WriteString(`<?xml version="1.0"?><rss version="2.0"><channel><title>Feed</title>`)
	for _, i := range items {
		date := ""
		if !i.published.IsZero() {
			date = "<pubDate>" + i.published.Format(time.RFC1123Z) + "</pubDate>"
		}
		fmt.Fprintf(&feed, "<item><title>%s</title><link>%s</link><description>feed text</description>%s</item>", i.title, i.link, date)
	}
	feed.WriteString(`</channel></rss>`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(feed.String())) }))
	t.Cleanup(srv.Close)
	return srv.URL
}

// recordingCrawler remembers the addresses it was asked for.
type recordingCrawler struct{ calls []string }

func (c *recordingCrawler) Crawl(url string) (business.Page, business.Usage, error) {
	c.calls = append(c.calls, url)
	return business.Page{Text: "page text"}, nil, nil
}

func titles(contents []business.Content) []string {
	var got []string
	for _, c := range contents {
		got = append(got, c.Title)
	}
	return got
}

func TestOnlyItemsWithinTheWindowAreReadAndCostARetrieval(t *testing.T) {
	feed := serveFeed(t,
		item{"just inside", "https://example.org/a", now.Add(-72*time.Hour + time.Minute)},
		item{"just too old", "https://example.org/b", now.Add(-72*time.Hour - time.Minute)},
		item{"on the edge", "https://example.org/c", now.Add(-72 * time.Hour)},
		item{"recent", "https://example.org/d", now.Add(-time.Hour)},
		item{"from the future", "https://example.org/e", now.Add(time.Hour)},
	)
	crawler := &recordingCrawler{}

	contents, _, err := NewRepository(feed, crawler, Opts{}).Content(now.Add(-72*time.Hour), now, business.ContentFetchOpts{})

	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"just inside", "on the edge", "recent"}, titles(contents))
	assert.ElementsMatch(t, []string{"https://example.org/a", "https://example.org/c", "https://example.org/d"}, crawler.calls,
		"an item outside the window costs no retrieval")
}

func TestItemsWithoutAPublicationDateAreSkippedAndCostNoRetrieval(t *testing.T) {
	feed := serveFeed(t,
		item{title: "undated", link: "https://example.org/a"},
		item{"dated", "https://example.org/b", now.Add(-time.Hour)},
	)
	crawler := &recordingCrawler{}

	contents, _, err := NewRepository(feed, crawler, Opts{}).Content(now.Add(-24*time.Hour), now, business.ContentFetchOpts{})

	require.NoError(t, err)
	assert.Equal(t, []string{"dated"}, titles(contents))
	assert.Equal(t, []string{"https://example.org/b"}, crawler.calls)
}

func TestItemsThatAreUsedAlreadyAreSkippedBeforeAnythingIsSpentOnThem(t *testing.T) {
	feed := serveFeed(t,
		item{"used", "https://example.org/a#top", now.Add(-time.Hour)},
		item{"new", "https://example.org/b", now.Add(-time.Hour)},
	)
	crawler := &recordingCrawler{}
	skip := func(identity string) bool { return identity == "https://example.org/a" }

	contents, _, err := NewRepository(feed, crawler, Opts{}).Content(now.Add(-24*time.Hour), now, business.ContentFetchOpts{Skip: skip})

	require.NoError(t, err)
	assert.Equal(t, []string{"new"}, titles(contents))
	assert.Equal(t, []string{"https://example.org/b"}, crawler.calls)
}
