package service_test

import (
	"regexp"
	"testing"

	"github.com/alex0ptr/aigude/business"
	"github.com/alex0ptr/aigude/pkg/service/servicetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func episodeWithUsage(h *servicetest.Harness, title string, usage business.Usage) business.Episode {
	e := episodeOf("alex", title, h.Clock.Now())
	e.Usage = usage
	return e
}

var consumed = business.Usage{
	usageOf(business.UsageSpeech, business.UnitAudioTokens, 14900),
	usageOf(business.UsageScript, business.UnitOutputTokens, 2810),
	usageOf(business.UsageCrawl, business.UnitPages, 14),
	usageOf(business.UsageScript, business.UnitInputTokens, 45120),
	usageOf(business.UsageSpeech, business.UnitInputTokens, 1234567),
}

// tooltipLines returns the lines of the first tooltip in the page.
func tooltipLines(t *testing.T, body string) []string {
	t.Helper()
	tooltip := regexp.MustCompile(`(?s)<span[^>]*role="tooltip"[^>]*>(.*?)</span>\s*</span>`).FindStringSubmatch(body)
	require.NotNil(t, tooltip, "a tooltip in the page")
	var lines []string
	for _, m := range regexp.MustCompile(`<li>([^<]*)</li>`).FindAllStringSubmatch(tooltip[1], -1) {
		lines = append(lines, m[1])
	}
	return lines
}

func TestEpisodeListShowsWhatEachEpisodeConsumed(t *testing.T) {
	h := servicetest.Start(t, twoUsers()...)
	h.AddEpisode(t, episodeWithUsage(h, "With usage", consumed), []byte("a"))

	page := h.Get(t, "/")

	assert.Contains(t, page.Body, "Resources used")
	assert.Contains(t, tooltipLines(t, page.Body), "Script input tokens: 45,120")
}

func TestEpisodePageShowsTheSameTooltip(t *testing.T) {
	h := servicetest.Start(t, twoUsers()...)
	episode := h.AddEpisode(t, episodeWithUsage(h, "With usage", consumed), []byte("a"))

	page := h.Get(t, episodePath(episode.ID))

	assert.Contains(t, tooltipLines(t, page.Body), "Script input tokens: 45,120")
}

func TestSymbolIsReachableWithTheKeyboardAndHasAnAccessibleName(t *testing.T) {
	h := servicetest.Start(t, twoUsers()...)
	h.AddEpisode(t, episodeWithUsage(h, "With usage", consumed), []byte("a"))

	page := h.Get(t, "/")

	assert.Contains(t, page.Body, `tabindex="0"`)
	assert.Contains(t, page.Body, `aria-label="Resources used"`)
	assert.Contains(t, page.Body, `aria-describedby="usage-`)
}

func TestEpisodeWithoutStoredUsageShowsNoSymbol(t *testing.T) {
	h := servicetest.Start(t, twoUsers()...)
	episode := h.AddEpisode(t, episodeWithUsage(h, "Without usage", nil), []byte("a"))

	list := h.Get(t, "/")
	page := h.Get(t, episodePath(episode.ID))

	assert.NotContains(t, list.Body, "Resources used")
	assert.NotContains(t, page.Body, "Resources used")
}
