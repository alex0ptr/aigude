package service_test

import (
	"errors"
	"fmt"
	"regexp"
	"testing"
	"time"

	"github.com/alex0ptr/aigude/business"
	"github.com/alex0ptr/aigude/pkg/service/servicetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func startGenerating(t *testing.T) *servicetest.Harness {
	t.Helper()
	feed := servicetest.ServeFeed(t, feedWith("News", "https://example.org/a", "Text"))
	h := servicetest.Start(t, servicetest.WithUserConfigFile("alex.toml", generatingFile(feed, "")))
	h.ScriptWriter.Script = threeSections()
	return h
}

func transient(reason string) error { return fmt.Errorf("%w: %s", business.ErrTransient, reason) }

func TestSpeechFailingTwiceWithATransientErrorStillYieldsAReadyEpisode(t *testing.T) {
	h := startGenerating(t)
	h.TTS.Errs = []error{transient("rate limited"), transient("rate limited")}

	episodes := generateAndWaitFor(t, h, 1, 1)

	assert.Empty(t, episodes[0].Failure)
	assert.NotEmpty(t, episodes[0].AudioLocation)
	assert.Len(t, h.TTS.Calls(), 5, "the first Section was spoken three times, the other two once")
	assert.Equal(t, []time.Duration{time.Second, 2 * time.Second}, h.Clock.Sleeps(), "the waits grow and the test did not really wait")
}

func TestFailedEpisodeShowsItsReasonAndOffersToTryAgain(t *testing.T) {
	h := startGenerating(t)
	h.TTS.Err = errors.New("speech quota used up")
	generateAndWaitFor(t, h, 1, 1)

	page := h.Get(t, "/")

	assert.Contains(t, page.Body, `data-testid="episode-failed"`)
	assert.Contains(t, page.Body, "speech quota used up")
	assert.Regexp(t, `action="/episodes/\d+/retry"`, page.Body)
}

func TestTryingAgainMakesANewAttemptThatCanSucceed(t *testing.T) {
	h := startGenerating(t)
	h.TTS.Err = errors.New("speech quota used up")
	failed := generateAndWaitFor(t, h, 1, 1)[0]
	h.TTS.Err = nil

	posted := h.Post(t, fmt.Sprintf("/episodes/%d/retry", failed.ID))
	require.Equal(t, 200, posted.Status)

	ready := h.WaitFor(t, "/", "the retried Episode in the list", pageContains(`data-testid="episode"`))
	assert.NotContains(t, ready.Body, "episode-failed", "the failed attempt makes room for the new one")
	episodes := h.Episodes(t, "alex")
	require.Len(t, episodes, 1)
	assert.Empty(t, episodes[0].Failure)
}

func TestFailedEpisodeCanBeDismissed(t *testing.T) {
	h := startGenerating(t)
	h.TTS.Err = errors.New("speech quota used up")
	failed := generateAndWaitFor(t, h, 1, 1)[0]

	require.Equal(t, 200, h.Post(t, fmt.Sprintf("/episodes/%d/delete", failed.ID)).Status)

	assert.Empty(t, h.Episodes(t, "alex"))
}

func TestRetryOfAnEpisodeThatIsNotFailedOrNotYoursIsNotFound(t *testing.T) {
	h := servicetest.Start(t, twoUsers()...)
	ready := h.AddEpisode(t, episodeOf("alex", "Fine", h.Clock.Now()), []byte("a"))
	failed := h.AddFailedEpisode(t, episodeOf("bea", "Broken", h.Clock.Now()), "boom")

	assert.Equal(t, 404, h.Post(t, fmt.Sprintf("/episodes/%d/retry", ready.ID)).Status)
	assert.Equal(t, 404, h.Post(t, fmt.Sprintf("/episodes/%d/retry", failed.ID)).Status, "another User's failed Episode")
}

func TestVisitorSeesNoReasonIsNotOfferedToTryAgainAndCannotRetry(t *testing.T) {
	h := servicetest.Start(t, threeUsers()...)
	failed := h.AddFailedEpisode(t, episodeOf("demo", "Broken", h.Clock.Now()), "speech quota used up")

	page := h.GetWithHeaders(t, "/", visitor())
	retried := h.PostWithHeaders(t, fmt.Sprintf("/episodes/%d/retry", failed.ID), visitor())

	assert.Contains(t, page.Body, `data-testid="episode-failed"`)
	assert.NotContains(t, page.Body, "speech quota used up", "the reason may name what only the person running the service should see")
	assert.NotContains(t, page.Body, "/retry")
	assert.Equal(t, 403, retried.Status)
}

func TestListShowsAnEpisodeThatIsBeingProducedAndUpdatesWhenItIsDone(t *testing.T) {
	h := startGenerating(t)
	h.ScriptWriter.Delay = 300 * time.Millisecond

	posted := h.Post(t, "/cast-profiles/morning/generate")

	assert.Contains(t, posted.Body, `data-testid="episode-generating"`)
	assert.Regexp(t, regexp.MustCompile(`hx-get="/cast-profiles/morning/generation"`), posted.Body, "the page polls on its own")
	h.WaitFor(t, "/", "the finished Episode", func(p servicetest.Page) bool {
		return !regexp.MustCompile(`episode-generating`).MatchString(p.Body) && regexp.MustCompile(`data-testid="episode"`).MatchString(p.Body)
	})
}

func TestCastProfileSaysWhenThereWasNothingNewInTheMaxLookback(t *testing.T) {
	h := startGenerating(t)
	generateAndWaitFor(t, h, 1, 1)
	require.Equal(t, 200, h.Post(t, "/cast-profiles/morning/generate").Status)

	page := h.WaitFor(t, "/", "the note on the CastProfile", pageContains(`data-testid="nothing-new-morning"`))

	assert.Contains(t, page.Body, "No new Content")
	assert.Len(t, h.Episodes(t, "alex"), 1, "no Episode was made")
}

func TestNoteGoesAwayWhenAnEpisodeIsMadeAgain(t *testing.T) {
	feed := servicetest.ServeChangingFeed(t, feedOfItems(feedItem{title: "Erste", link: "https://example.org/a", description: "A", published: inTheWindow(2)}))
	h := startWithFeed(t, feed)
	generate(t, h)
	waitForEpisodes(t, h, "alex", 1)
	generate(t, h)
	h.WaitFor(t, "/", "the note", pageContains("nothing-new-morning"))
	feed.Set(feedOfItems(feedItem{title: "Zweite", link: "https://example.org/b", description: "B", published: inTheWindow(1)}))

	generate(t, h)
	waitForEpisodes(t, h, "alex", 2)

	h.WaitFor(t, "/", "the note to vanish", func(p servicetest.Page) bool { return !regexp.MustCompile(`nothing-new-morning`).MatchString(p.Body) })
}

func TestRetryWhileTheCastProfileIsBeingProducedKeepsTheFailedEpisode(t *testing.T) {
	h := startGenerating(t)
	h.TTS.Err = errors.New("speech quota used up")
	failed := generateAndWaitFor(t, h, 1, 1)[0]
	h.ScriptWriter.Delay = 500 * time.Millisecond
	require.Equal(t, 200, h.Post(t, "/cast-profiles/morning/generate").Status)

	retried := h.Post(t, fmt.Sprintf("/episodes/%d/retry", failed.ID))

	assert.Equal(t, 409, retried.Status)
	assert.Equal(t, failed.ID, h.Episode(t, "alex", failed.ID).ID, "the failure and its reason are still there")
}
