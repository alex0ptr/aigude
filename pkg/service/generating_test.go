package service_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/alex0ptr/aigude/pkg/service/servicetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// twoProfilesFile is a User Config file with the CastProfiles morning and evening, both reading the same feed.
func twoProfilesFile(feedURL string) string {
	profile := func(id, name string) string {
		return fmt.Sprintf(`
[cast_profiles.%[1]s]
display_name = %[2]q
max_lookback = "24h"

[cast_profiles.%[1]s.subscriptions.news]
display_name = "News"
kind = "rss"
url = %[3]q
`, id, name, feedURL)
	}
	return "[user]\nid = \"alex\"\ndisplay_name = \"Alex\"\n" + profile("morning", "Morning briefing") + profile("evening", "Evening briefing")
}

// generateButton finds the "Generate now" button of a CastProfile on a page and tells whether it is disabled.
func generateButton(t *testing.T, body, profile string) (disabled bool) {
	t.Helper()
	button := regexp.MustCompile(`<button[^>]*data-testid="generate-` + profile + `"[^>]*>`).FindString(body)
	require.NotEmpty(t, button, "the overview has a Generate now button for %s", profile)
	return strings.Contains(button, "disabled")
}

func TestGenerateNowIsOffWhileThatCastProfileIsProducedAndOnForTheOthers(t *testing.T) {
	feed := servicetest.ServeFeed(t, feedWith("Neue Nachricht", "https://example.org/a", "Kurztext"))
	h := servicetest.Start(t, servicetest.WithUserConfigFile("alex.toml", twoProfilesFile(feed)))
	h.ScriptWriter.Script = threeSections()
	h.ScriptWriter.Delay = time.Second

	overview := h.Get(t, "/")
	assert.False(t, generateButton(t, overview.Body, "morning"), "nothing is produced yet")
	assert.False(t, generateButton(t, overview.Body, "evening"))

	posted := h.Post(t, "/cast-profiles/morning/generate")

	assert.True(t, generateButton(t, posted.Body, "morning"), "the page the browser lands on shows the request as running")
	assert.False(t, generateButton(t, posted.Body, "evening"), "another CastProfile can still be requested")
	assert.Contains(t, posted.Body, "Generating")

	h.WaitFor(t, "/", "the button of the finished CastProfile is back", func(p servicetest.Page) bool {
		return !generateButton(t, p.Body, "morning")
	})
}

func TestTheRunningCardAsksForItsStateAndTheFinishedOneRefreshesThePage(t *testing.T) {
	feed := servicetest.ServeFeed(t, feedWith("Neue Nachricht", "https://example.org/a", "Kurztext"))
	h := servicetest.Start(t, servicetest.WithUserConfigFile("alex.toml", twoProfilesFile(feed)))
	h.ScriptWriter.Script = threeSections()
	h.ScriptWriter.Delay = time.Second
	posted := h.Post(t, "/cast-profiles/morning/generate")
	assert.Contains(t, posted.Body, `hx-get="/cast-profiles/morning/generation"`, "the running card polls for its state")
	assert.NotContains(t, posted.Body, `hx-get="/cast-profiles/evening/generation"`, "an idle card does not poll")

	running := h.Get(t, "/cast-profiles/morning/generation")

	assert.Equal(t, 200, running.Status)
	assert.Contains(t, running.Body, "Generating")
	assert.Empty(t, running.Header.Get("HX-Refresh"))

	h.WaitFor(t, "/cast-profiles/morning/generation", "the page is told to refresh when production ended", func(p servicetest.Page) bool {
		return p.Header.Get("HX-Refresh") == "true"
	})
	assert.Contains(t, h.Get(t, "/").Body, "Heute im Überblick", "the new Episode is on the page that the refresh loads")
}
