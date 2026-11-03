package service_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/alex0ptr/aigude/business"
	"github.com/alex0ptr/aigude/pkg/service/servicetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const hostilePage = "# Der Zug\n\nEin Absatz mit **Betonung**.\n\n<script>alert('xss')</script>\n\n<img src=x onerror=alert(2)>\n\n[Klick](javascript:alert(3))\n"

func scriptCiting(sources ...int) business.Script {
	return business.Script{Title: "Heute im Überblick", Sections: []business.Section{
		{Heading: "Intro", Body: "Guten Morgen."},
		{Heading: "Thema", Body: "Das ist die Nachricht.", Sources: sources},
	}}
}

// sourcedEpisode produces an Episode from one item whose page is the given text.
func sourcedEpisode(t *testing.T, page string, script business.Script) (*servicetest.Harness, business.Episode) {
	t.Helper()
	feed := servicetest.ServeFeed(t, feedWith("Der Zug", pageURL, "Kurztext"))
	h := servicetest.Start(t, servicetest.WithUserConfigFile("alex.toml", generatingFile(feed, "")))
	h.ScriptWriter.Script = script
	h.Crawler.AddPage(pageURL, business.Page{Title: "Wasserstoffzug", Text: page})
	return h, generateAndWaitFor(t, h, 1, 1)[0]
}

func TestEpisodePageShowsTheScriptBeneathThePlayerWithTheSourcesOfEachSection(t *testing.T) {
	h, episode := sourcedEpisode(t, "Ein langer Artikel.", scriptCiting(1))

	page := h.Get(t, fmt.Sprintf("/episodes/%d", episode.ID))

	assert.Regexp(t, `(?s)</audio>.*Guten Morgen\..*Das ist die Nachricht\.`, page.Body)
	assert.Regexp(t, `(?s)<h2[^>]*>Thema</h2>.*data-testid="source".*Der Zug`, page.Body, "the Section lists its source")
	intro := page.Body[strings.Index(page.Body, "Intro</h"):strings.Index(page.Body, "Thema</h")]
	assert.NotContains(t, intro, `data-testid="source"`, "the introduction has no sources")
}

func TestPopupShowsTitleLinkRetrievalDateAndText(t *testing.T) {
	h, episode := sourcedEpisode(t, "# Der Zug\n\nEin Absatz mit **Betonung**.", scriptCiting(1))

	popup := h.Get(t, fmt.Sprintf("/episodes/%d/sources/1", episode.ID))

	assert.Equal(t, 200, popup.Status)
	assert.Contains(t, popup.Body, "Der Zug")
	assert.Contains(t, popup.Body, `href="`+pageURL+`"`)
	assert.Contains(t, popup.Body, "2026-10-29")
	assert.Contains(t, popup.Body, "<strong>Betonung</strong>", "the text is rendered from Markdown")
}

func TestHostileMarkupOfACrawledPageIsNeutralised(t *testing.T) {
	h, episode := sourcedEpisode(t, hostilePage, scriptCiting(1))

	popup := h.Get(t, fmt.Sprintf("/episodes/%d/sources/1", episode.ID))

	assert.Equal(t, 200, popup.Status)
	assert.NotContains(t, popup.Body, "<script>alert")
	assert.NotContains(t, popup.Body, "onerror=")
	assert.NotContains(t, strings.ToLower(popup.Body), `href="javascript:`)
	assert.Contains(t, popup.Body, "Betonung", "the harmless part of the page is still there")
}

func TestSourceNumbersThatNameNothingDoNotFailTheEpisode(t *testing.T) {
	h, episode := sourcedEpisode(t, "Ein Artikel.", scriptCiting(7, 0))

	page := h.Get(t, fmt.Sprintf("/episodes/%d", episode.ID))

	assert.Equal(t, 200, page.Status)
	assert.NotContains(t, page.Body, `data-testid="source"`)
	assert.Contains(t, strings.Join(h.Logs.Lines(), "\n"), "Dropping source number")
	assert.Empty(t, h.Episode(t, "alex", episode.ID).Failure)
}

func TestSourcesStayReadableAfterTheSubscriptionIsRemoved(t *testing.T) {
	h, episode := sourcedEpisode(t, "Ein Artikel mit Text.", scriptCiting(1))
	servicetest.WriteUserConfig(t, h.Settings, "alex.toml", "[user]\nid = \"alex\"\ndisplay_name = \"Alex\"\n")
	h.WaitFor(t, "/", "the CastProfile to vanish", pageContains("No CastProfiles yet"))

	popup := h.Get(t, fmt.Sprintf("/episodes/%d/sources/1", episode.ID))

	assert.Equal(t, 200, popup.Status)
	assert.Contains(t, popup.Body, "Ein Artikel mit Text.")
}

func TestPopupOfAnUnknownSourceOrAnotherUsersEpisodeIsNotFound(t *testing.T) {
	h := servicetest.Start(t, twoUsers()...)
	alexs := episodeOf("alex", "Alex", h.Clock.Now())
	alexs.Sources = []business.Source{{Title: "A", Text: "a", Retrieved: h.Clock.Now()}}
	stored := h.AddEpisode(t, alexs, []byte("a"))

	assert.Equal(t, 200, h.Get(t, fmt.Sprintf("/episodes/%d/sources/1", stored.ID)).Status)
	assert.Equal(t, 404, h.Get(t, fmt.Sprintf("/episodes/%d/sources/2", stored.ID)).Status)
	assert.Equal(t, 404, h.Get(t, fmt.Sprintf("/episodes/%d/sources/0", stored.ID)).Status)
	assert.Equal(t, 404, h.Get(t, fmt.Sprintf("/episodes/%d/sources/x", stored.ID)).Status)
	h.SelectUser(t, "bea")
	assert.Equal(t, 404, h.Get(t, fmt.Sprintf("/episodes/%d/sources/1", stored.ID)).Status)
}

func TestLinkOfASourceWithoutHttpAddressIsNotOffered(t *testing.T) {
	h := servicetest.Start(t, twoUsers()...)
	e := episodeOf("alex", "Alex", h.Clock.Now())
	e.Sources = []business.Source{{Title: "A", Link: "javascript:alert(1)", Text: "a", Retrieved: h.Clock.Now()}}
	stored := h.AddEpisode(t, e, []byte("a"))

	popup := h.Get(t, fmt.Sprintf("/episodes/%d/sources/1", stored.ID))

	assert.NotContains(t, strings.ToLower(popup.Body), "javascript:")
	require.Equal(t, 200, popup.Status)
}

func TestSourceFromTheCacheKeepsTheDateItWasRetrieved(t *testing.T) {
	h := crawlingHarness(t)
	generateAndWaitFor(t, h, 1, 1)
	deleteEpisodes(t, h)
	h.Clock.Advance(72 * time.Hour)

	second := generateAndWaitFor(t, h, 1, 1)[0]

	assert.Len(t, h.Crawler.Calls(), 1, "the page came from the cache")
	popup := h.Get(t, fmt.Sprintf("/episodes/%d/sources/1", second.ID))
	assert.Contains(t, popup.Body, "2026-10-29", "retrieved three days ago")
	assert.NotContains(t, popup.Body, "2026-11-01")
}
