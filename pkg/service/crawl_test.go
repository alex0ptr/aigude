package service_test

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/alex0ptr/aigude/business"
	"github.com/alex0ptr/aigude/pkg/service/servicetest"
	"github.com/alex0ptr/aigude/pkg/settings"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const pageURL = "https://example.org/seite"

// feedItem is one entry of a test feed.
type feedItem struct {
	title, link, description string
	published                time.Time
}

// feedOfItems is an RSS document with the given entries.
func feedOfItems(items ...feedItem) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?><rss version="2.0"><channel><title>Test feed</title>`)
	for i, item := range items {
		fmt.Fprintf(&b, `<item><guid>g%d</guid><title>%s</title><link>%s</link><description>%s</description>`, i, item.title, item.link, item.description)
		if !item.published.IsZero() {
			fmt.Fprintf(&b, `<pubDate>%s</pubDate>`, item.published.Format(time.RFC1123Z))
		}
		b.WriteString(`</item>`)
	}
	b.WriteString(`</channel></rss>`)
	return b.String()
}

// startedAt is the instant the test clock starts at.
var startedAt = time.Date(2026, time.October, 29, 7, 0, 0, 0, time.UTC)

// crawlingHarness serves a feed with one item and returns the harness, whose CastProfile looks back thirty days, so
// that the item stays in the window while the clock moves. The article behind the link is the noisy page.
func crawlingHarness(t *testing.T, opts ...servicetest.Option) *servicetest.Harness {
	t.Helper()
	feed := servicetest.ServeFeed(t, feedOfItems(feedItem{"Der Zug", pageURL, "Kurztext des Feeds", startedAt.Add(-2 * time.Hour)}))
	file := withLookback(generatingFile(feed, ""), "720h")
	h := servicetest.Start(t, append([]servicetest.Option{servicetest.WithUserConfigFile("alex.toml", file)}, opts...)...)
	h.ScriptWriter.Script = threeSections()
	page, err := os.ReadFile("../../business/testdata/noisy-page.md")
	require.NoError(t, err)
	h.Crawler.AddPage(pageURL, business.Page{Title: "Wasserstoffzug", Text: string(page)})
	return h
}

// deleteEpisodes deletes the Episodes of the User, which gives their Content back.
func deleteEpisodes(t *testing.T, h *servicetest.Harness) {
	t.Helper()
	for _, episode := range h.Episodes(t, "alex") {
		require.Equal(t, 200, h.Post(t, fmt.Sprintf("/episodes/%d/delete", episode.ID)).Status)
	}
	require.Empty(t, h.Episodes(t, "alex"))
}

func TestThePromptHoldsTheCleanedArticleNotTheFeedTeaser(t *testing.T) {
	h := crawlingHarness(t)

	generateAndWaitFor(t, h, 1, 1)

	body := h.ScriptWriter.Requests()[0].Contents[0].Body
	assert.Contains(t, body, "Pro Tankfüllung sollen die Züge rund 800 Kilometer schaffen.")
	assert.NotContains(t, body, "Kurztext des Feeds")
	assert.NotContains(t, body, "https://", "links are reduced to their text")
	assert.NotContains(t, body, "Dunkles Schema", "page furniture is gone")
}

func TestTheSameArticleIsRetrievedOnceForTwoUsers(t *testing.T) {
	feed := servicetest.ServeFeed(t, feedOfItems(feedItem{"Der Zug", pageURL, "Kurztext", startedAt.Add(-2 * time.Hour)}))
	h := servicetest.Start(t,
		servicetest.WithUserConfigFile("alex.toml", generatingFile(feed, "")),
		servicetest.WithUserConfigFile("bea.toml", strings.Replace(generatingFile(feed, ""), `id = "alex"`, `id = "bea"`, 1)))
	h.ScriptWriter.Script = threeSections()
	h.Crawler.AddPage(pageURL, business.Page{Title: "T", Text: "Ein Absatz mit Inhalt."})

	generateAndWaitFor(t, h, 1, 1)
	h.SelectUser(t, "bea")
	require.Equal(t, 200, h.Post(t, "/cast-profiles/morning/generate").Status)
	waitForEpisodes(t, h, "bea", 1)

	assert.Equal(t, []string{pageURL}, h.Crawler.Calls(), "the cache belongs to no User")
}

func TestAnArticleWithinTheTimeToLiveIsNotRetrievedAgain(t *testing.T) {
	h := crawlingHarness(t)
	generateAndWaitFor(t, h, 1, 1)

	deleteEpisodes(t, h)
	h.Clock.Advance(9 * 24 * time.Hour)
	generateAndWaitFor(t, h, 1, 1)

	assert.Equal(t, []string{pageURL}, h.Crawler.Calls())
}

func TestTheTimeToLiveIsASetting(t *testing.T) {
	h := crawlingHarness(t, servicetest.WithSettings(func(s *settings.Settings) { s.CrawlCacheTTL = time.Hour }))
	generateAndWaitFor(t, h, 1, 1)

	deleteEpisodes(t, h)
	h.Clock.Advance(2 * time.Hour)
	generateAndWaitFor(t, h, 1, 1)

	assert.Len(t, h.Crawler.Calls(), 2)
}

func TestExpiredPagesAreDeletedFromTheCacheOnTheirOwn(t *testing.T) {
	h := crawlingHarness(t)
	generateAndWaitFor(t, h, 1, 1)
	require.Len(t, h.CachedPages(t), 1)

	h.Clock.Advance(11 * 24 * time.Hour)

	require.Eventually(t, func() bool { return len(h.CachedPages(t)) == 0 }, 5*time.Second, 20*time.Millisecond,
		"the clean-up removes what is older than the time to live")
}

func TestAFailingRetrievalFallsBackToTheFeedText(t *testing.T) {
	h := crawlingHarness(t)
	h.Crawler.FailFor(pageURL, errors.New("firecrawl is down"))

	episodes := generateAndWaitFor(t, h, 1, 1)

	assert.Empty(t, episodes[0].Failure, "one unreachable page does not fail the Episode")
	assert.Equal(t, "Kurztext des Feeds", h.ScriptWriter.Requests()[0].Contents[0].Body)
	assert.Empty(t, h.CachedPages(t), "nothing is cached for a failed retrieval")
}

func TestALongArticleIsCutInThePromptButStaysWholeInTheCache(t *testing.T) {
	h := crawlingHarness(t, servicetest.WithSettings(func(s *settings.Settings) { s.ArticleCharLimit = 300 }))
	long := strings.Repeat("Dies ist ein ganzer Satz im Artikel. ", 100)
	h.Crawler.AddPage(pageURL, business.Page{Title: "Lang", Text: long})

	generateAndWaitFor(t, h, 1, 1)

	body := h.ScriptWriter.Requests()[0].Contents[0].Body
	assert.LessOrEqual(t, len([]rune(body)), 300)
	assert.Greater(t, len(body), 200, "the prompt holds as much as the limit allows")
	assert.Equal(t, long, h.CachedPages(t)[pageURL].Text, "the cache keeps the full text, uncleaned")
}
