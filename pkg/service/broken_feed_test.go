package service_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alex0ptr/aigude/pkg/service/servicetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAFeedThatAnswersWithAServerErrorIsSkippedAndTheEpisodeIsMadeFromTheOthers(t *testing.T) {
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) }))
	t.Cleanup(down.Close)
	feed := servicetest.ServeFeed(t, feedWith("News", "https://example.org/a", "Text"))
	file := generatingFile(feed, "") + fmt.Sprintf(`
[cast_profiles.morning.subscriptions.newsletter]
display_name = "Newsletter"
kind = "rss"
url = %q
`, down.URL)
	h := servicetest.Start(t, servicetest.WithUserConfigFile("alex.toml", file))
	h.ScriptWriter.Script = threeSections()

	episodes := generateAndWaitFor(t, h, 1, 1)

	require.Empty(t, episodes[0].Failure)
	assert.Equal(t, []string{"News"}, titlesOf(h.ScriptWriter.Requests()[0]))
	assert.Equal(t, []time.Duration{time.Second, 2 * time.Second}, h.Clock.Sleeps(), "the feed was tried three times")
}
