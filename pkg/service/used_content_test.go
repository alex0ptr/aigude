package service_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/alex0ptr/aigude/pkg/service/servicetest"
	"github.com/alex0ptr/aigude/pkg/settings"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// nothingNew is the log line that says why no Episode was made.
const nothingNew = "No unused Content"

// inTheWindow is a time the clock's start lies a few hours after.
func inTheWindow(hoursAgo int) time.Time { return startedAt.Add(-time.Duration(hoursAgo) * time.Hour) }

// startWithFeed starts the service with the feed, the Script that cites the first item, and a CastProfile "morning".
func startWithFeed(t *testing.T, feed *servicetest.ChangingFeed, opts ...servicetest.Option) *servicetest.Harness {
	t.Helper()
	h := servicetest.Start(t, append([]servicetest.Option{servicetest.WithUserConfigFile("alex.toml", generatingFile(feed.URL, ""))}, opts...)...)
	h.ScriptWriter.Script = threeSections()
	return h
}

// generate asks for an Episode of the CastProfile "morning".
func generate(t *testing.T, h *servicetest.Harness) {
	t.Helper()
	require.Equal(t, 200, h.Post(t, "/cast-profiles/morning/generate").Status)
}

// waitForLog waits until the service logged a line that contains all the parts.
func waitForLog(t *testing.T, logs *logCapture, parts ...string) {
	t.Helper()
	require.Eventually(t, func() bool { return logs.Contains(parts...) }, 5*time.Second, 10*time.Millisecond,
		"gave up waiting for a log line with %v; got %v", parts, logs.Lines())
}

func TestContentThatWentIntoAnEpisodeIsNotUsedAgain(t *testing.T) {
	first := feedItem{title: "Erste Nachricht", link: "https://example.org/a", description: "A", published: inTheWindow(3)}
	second := feedItem{title: "Zweite Nachricht", link: "https://example.org/b", description: "B", published: inTheWindow(2)}
	feed := servicetest.ServeChangingFeed(t, feedOfItems(first))
	h := startWithFeed(t, feed)
	generate(t, h)
	waitForEpisodes(t, h, "alex", 1)
	feed.Set(feedOfItems(first, second))

	generate(t, h)

	waitForEpisodes(t, h, "alex", 2)
	requests := h.ScriptWriter.Requests()
	require.Len(t, requests, 2)
	assert.Equal(t, []string{"Erste Nachricht"}, titlesOf(requests[0]))
	assert.Equal(t, []string{"Zweite Nachricht"}, titlesOf(requests[1]), "only what the first Episode did not have")
}

func TestAnItemWithoutALinkIsIdentifiedByItsGUID(t *testing.T) {
	logs := captureLogs(t)
	withoutLink := feedItem{title: "Ohne Link", description: "A", published: inTheWindow(2)}
	feed := servicetest.ServeChangingFeed(t, feedOfItems(withoutLink))
	h := startWithFeed(t, feed)
	generate(t, h)
	waitForEpisodes(t, h, "alex", 1)

	generate(t, h) // the same item, the same GUID g0
	waitForLog(t, logs, nothingNew)
	assert.Len(t, h.Episodes(t, "alex"), 1)

	feed.Set(feedOfItems(withoutLink, feedItem{title: "Auch ohne Link", description: "B", published: inTheWindow(1)})) // GUID g1
	generate(t, h)
	waitForEpisodes(t, h, "alex", 2)
	assert.Equal(t, []string{"Auch ohne Link"}, titlesOf(h.ScriptWriter.Requests()[1]))
}

func TestEveryItemInThePromptCountsAsUsedNotOnlyTheOnesTheScriptCites(t *testing.T) {
	logs := captureLogs(t)
	feed := servicetest.ServeChangingFeed(t, feedOfItems(
		feedItem{title: "Eins", link: "https://example.org/1", description: "1", published: inTheWindow(3)},
		feedItem{title: "Zwei", link: "https://example.org/2", description: "2", published: inTheWindow(2)},
		feedItem{title: "Drei", link: "https://example.org/3", description: "3", published: inTheWindow(1)}))
	h := startWithFeed(t, feed) // the Script cites item 1 only
	generate(t, h)
	waitForEpisodes(t, h, "alex", 1)

	generate(t, h)

	waitForLog(t, logs, nothingNew)
	assert.Len(t, h.Episodes(t, "alex"), 1)
}

func TestAWindowWithoutNewContentMakesNoEpisodeAndTheLogSaysWhy(t *testing.T) {
	logs := captureLogs(t)
	feed := servicetest.ServeChangingFeed(t, feedOfItems(feedItem{title: "Zu alt", link: "https://example.org/a", description: "A", published: inTheWindow(48)}))
	h := startWithFeed(t, feed)

	generate(t, h)

	waitForLog(t, logs, nothingNew, "profile=morning")
	assert.Empty(t, h.Episodes(t, "alex"), "not even a failed Episode")
	assert.Empty(t, h.ScriptWriter.Requests())
}

func TestUsedAndOutOfWindowItemsAreNotRetrievedByTheCrawler(t *testing.T) {
	const old, first, second = "https://example.org/old", "https://example.org/a", "https://example.org/b"
	feed := servicetest.ServeChangingFeed(t, feedOfItems(
		feedItem{title: "Alt", link: old, description: "O", published: inTheWindow(48)},
		feedItem{title: "Erste", link: first, description: "A", published: inTheWindow(2)}))
	// A short time to live, so the cache cannot hide a retrieval that the Crawler is asked for.
	h := startWithFeed(t, feed, servicetest.WithSettings(func(s *settings.Settings) { s.CrawlCacheTTL = time.Minute }))
	generate(t, h)
	waitForEpisodes(t, h, "alex", 1)
	assert.Equal(t, []string{first}, h.Crawler.Calls(), "the item out of the window is not retrieved")
	h.Clock.Advance(time.Hour)
	feed.Set(feedOfItems(
		feedItem{title: "Alt", link: old, description: "O", published: inTheWindow(48)},
		feedItem{title: "Erste", link: first, description: "A", published: inTheWindow(2)},
		feedItem{title: "Zweite", link: second, description: "B", published: inTheWindow(1)}))

	generate(t, h)

	waitForEpisodes(t, h, "alex", 2)
	assert.Equal(t, []string{first, second}, h.Crawler.Calls(), "the used item is not retrieved again")
}

// withDroppedParams puts drop_query_params into the Subscription of a generatingFile.
func withDroppedParams(file string, names ...string) string {
	return strings.Replace(file, `kind = "rss"`, fmt.Sprintf("kind = \"rss\"\ndrop_query_params = [%s]", `"`+strings.Join(names, `", "`)+`"`), 1)
}

func TestAnArticleUsedEarlierIsRecognisedAlthoughItsLinkCarriesAListedParameter(t *testing.T) {
	logs := captureLogs(t)
	feed := servicetest.ServeChangingFeed(t, feedOfItems(feedItem{title: "Nachricht", link: "https://example.org/a?wt_mc=rss.red", description: "A", published: inTheWindow(2)}))
	h := servicetest.Start(t, servicetest.WithUserConfigFile("alex.toml", withDroppedParams(generatingFile(feed.URL, ""), "wt_mc")))
	h.ScriptWriter.Script = threeSections()
	generate(t, h)
	waitForEpisodes(t, h, "alex", 1)
	feed.Set(feedOfItems(feedItem{title: "Nachricht", link: "https://example.org/a?wt_mc=rss.feed", description: "A", published: inTheWindow(2)}))

	generate(t, h)

	waitForLog(t, logs, nothingNew)
	assert.Len(t, h.Episodes(t, "alex"), 1)
}

// twoSubscriptionsFile has the Subscription "first" and then "second", with the given extra lines in each.
func twoSubscriptionsFile(firstURL, firstExtra, secondURL, secondExtra string) string {
	file := generatingFile(firstURL, "")
	file = strings.Replace(file, `kind = "rss"`, "kind = \"rss\"\n"+firstExtra, 1)
	return file + fmt.Sprintf("\n[cast_profiles.morning.subscriptions.second]\ndisplay_name = \"Second\"\nkind = \"rss\"\nurl = %q\n%s\n", secondURL, secondExtra)
}

func TestTheSameArticleFromTwoSubscriptionsIsInTheContentsOnceFromTheFirstAndRetrievedOnce(t *testing.T) {
	const plain, tracked = "https://example.org/a", "https://example.org/a?wt_mc=rss.red"
	ix := servicetest.ServeFeed(t, feedOfItems(feedItem{title: "Aus iX", link: plain, description: "iX", published: inTheWindow(2)}))
	online := servicetest.ServeFeed(t, feedOfItems(
		feedItem{title: "Aus online", link: tracked, description: "online", published: inTheWindow(2)},
		feedItem{title: "Nur online", link: "https://example.org/b?wt_mc=rss.red", description: "nur", published: inTheWindow(1)}))
	h := servicetest.Start(t, servicetest.WithUserConfigFile("alex.toml",
		twoSubscriptionsFile(ix, "", online, `drop_query_params = ["wt_mc"]`)))
	h.ScriptWriter.Script = threeSections()

	generate(t, h)

	waitForEpisodes(t, h, "alex", 1)
	assert.Equal(t, []string{"Aus iX", "Nur online"}, titlesOf(h.ScriptWriter.Requests()[0]), "the first Subscription in file order wins")
	assert.Equal(t, []string{plain, "https://example.org/b?wt_mc=rss.red"}, h.Crawler.Calls(), "the duplicate is not retrieved, and the Crawler gets links as the feed delivered them")
}

func TestEachItemIsIdentifiedWithTheListOfTheSubscriptionThatDeliveredIt(t *testing.T) {
	// Only the first Subscription lists wt_mc. For the second, the same link with the parameter is another article.
	first := servicetest.ServeFeed(t, feedOfItems(feedItem{title: "Erster", link: "https://example.org/a", description: "1", published: inTheWindow(2)}))
	second := servicetest.ServeFeed(t, feedOfItems(feedItem{title: "Zweiter", link: "https://example.org/a?wt_mc=x", description: "2", published: inTheWindow(2)}))
	h := servicetest.Start(t, servicetest.WithUserConfigFile("alex.toml",
		twoSubscriptionsFile(first, `drop_query_params = ["wt_mc"]`, second, "")))
	h.ScriptWriter.Script = threeSections()

	generate(t, h)

	waitForEpisodes(t, h, "alex", 1)
	assert.Equal(t, []string{"Erster", "Zweiter"}, titlesOf(h.ScriptWriter.Requests()[0]))
}
