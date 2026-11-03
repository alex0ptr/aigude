package service_test

import (
	"strings"
	"testing"
	"time"

	"github.com/alex0ptr/aigude/pkg/service/servicetest"
	"github.com/stretchr/testify/assert"
)

// withLookback sets the Max Lookback of the CastProfile in a generatingFile.
func withLookback(file, lookback string) string {
	return strings.Replace(file, `max_lookback = "24h"`, `max_lookback = `+`"`+lookback+`"`, 1)
}

// titlesOf returns the titles of the Content items of a request to the script writer.
func titlesOf(request servicetest.ScriptRequest) []string {
	var titles []string
	for _, c := range request.Contents {
		titles = append(titles, c.Title)
	}
	return titles
}

func TestChangingTheMaxLookbackChangesTheWindowOfTheNextEpisode(t *testing.T) {
	feed := servicetest.ServeFeed(t, feedOfItems(
		feedItem{title: "Von heute", link: "https://example.org/a", description: "A", published: startedAt.Add(-time.Hour)},
		feedItem{title: "Vorgestern", link: "https://example.org/b", description: "B", published: startedAt.Add(-48 * time.Hour)},
	))
	h := servicetest.Start(t, servicetest.WithUserConfigFile("alex.toml", generatingFile(feed, "")))
	h.ScriptWriter.Script = threeSections()
	generateAndWaitFor(t, h, 1, 1)
	assert.NotContains(t, titlesOf(h.ScriptWriter.Requests()[0]), "Vorgestern")

	servicetest.WriteUserConfig(t, h.Settings, "alex.toml", withLookback(generatingFile(feed, ""), "72h"))
	h.WaitFor(t, "/", "the new Max Lookback", pageContains("72h"))
	generateAndWaitFor(t, h, 1, 2)

	assert.Contains(t, titlesOf(h.ScriptWriter.Requests()[1]), "Vorgestern")
}
