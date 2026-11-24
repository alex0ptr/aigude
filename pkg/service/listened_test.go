package service_test

import (
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/alex0ptr/aigude/pkg/service/servicetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func listenedPath(id int64) string { return episodePath(id) + "/listened" }

func TestTheListShowsUnlistenedAndFailedEpisodesUntilAllIsAsked(t *testing.T) {
	h := servicetest.Start(t, twoUsers()...)
	base := h.Clock.Now()
	heard := h.AddEpisode(t, episodeOf("alex", "Heard long ago", base.Add(-3*time.Hour)), []byte("a"))
	h.MarkListened(t, heard, true)
	h.AddEpisode(t, episodeOf("alex", "Not heard yet", base.Add(-2*time.Hour)), []byte("b"))
	h.AddFailedEpisode(t, episodeOf("alex", "", base.Add(-time.Hour)), "the model said no")

	standard := h.Get(t, "/")
	all := h.Get(t, "/?show=all")

	assert.Contains(t, standard.Body, "Not heard yet")
	assert.Contains(t, standard.Body, "the model said no", "a failed Episode stays in sight")
	assert.NotContains(t, standard.Body, "Heard long ago")
	assert.Contains(t, all.Body, "Not heard yet")
	assert.Contains(t, all.Body, "Heard long ago")
	assert.Contains(t, all.Body, "the model said no")
}

func TestAnEmptyUnlistenedListSaysSoAndOffersAllWhileNoEpisodesAtAllSaysThat(t *testing.T) {
	h := servicetest.Start(t, twoUsers()...)
	none := h.Get(t, "/")
	heard := h.AddEpisode(t, episodeOf("alex", "Heard", h.Clock.Now()), []byte("a"))
	h.MarkListened(t, heard, true)

	allHeard := h.Get(t, "/")

	assert.Contains(t, none.Body, "No Episodes yet.")
	assert.NotContains(t, none.Body, "Nothing new to listen to")
	assert.Contains(t, allHeard.Body, "Nothing new to listen to")
	assert.Contains(t, allHeard.Body, `href="/?show=all"`)
	assert.NotContains(t, allHeard.Body, "No Episodes yet.")
}

func TestLoadMoreKeepsTheChoiceOfTheList(t *testing.T) {
	h := servicetest.Start(t, twoUsers()...)
	base := h.Clock.Now()
	for i := 1; i <= 25; i++ {
		episode := h.AddEpisode(t, episodeOf("alex", fmt.Sprintf("Episode %02d", i), base.Add(time.Duration(i)*time.Minute)), []byte("a"))
		h.MarkListened(t, episode, i%5 == 0)
	}
	more := regexp.MustCompile(`hx-get="(/episodes\?[^"]+)"`)

	unlistened := h.Get(t, "/")
	all := h.Get(t, "/?show=all")

	next := more.FindStringSubmatch(all.Body)
	require.NotNil(t, next)
	assert.Contains(t, html.UnescapeString(next[1]), "show=all")
	assert.Nil(t, more.FindStringSubmatch(unlistened.Body), "20 Unlistened Episodes fit on one page")
}

func TestTheMarkerSetsTheStateInBothDirectionsAndTheFragmentShowsIt(t *testing.T) {
	h := servicetest.Start(t, twoUsers()...)
	episode := h.AddEpisode(t, episodeOf("alex", "Mine", h.Clock.Now()), []byte("a"))

	heard := h.Send(t, http.MethodPost, listenedPath(episode.ID), nil, url.Values{"listened": {"true"}})
	heardAgain := h.Send(t, http.MethodPost, listenedPath(episode.ID), nil, url.Values{"listened": {"true"}})
	stateAfterYes := h.Episode(t, "alex", episode.ID).Listened
	reset := h.Send(t, http.MethodPost, listenedPath(episode.ID), nil, url.Values{"listened": {"false"}})

	assert.Equal(t, http.StatusOK, heard.Status)
	assert.Contains(t, heard.Body, "Listened. Mark as not listened.")
	assert.Equal(t, heard.Body, heardAgain.Body, "setting the state it has changes nothing")
	assert.True(t, stateAfterYes)
	assert.Contains(t, reset.Body, "Not listened yet. Mark as listened.")
	assert.False(t, h.Episode(t, "alex", episode.ID).Listened)
}

func TestTheStateCannotBeSetForAnEpisodeThatFailedOrIsAnotherUsersOrWithAnUnknownState(t *testing.T) {
	h := servicetest.Start(t, twoUsers()...)
	failed := h.AddFailedEpisode(t, episodeOf("alex", "", h.Clock.Now()), "boom")
	beas := h.AddEpisode(t, episodeOf("bea", "Bea", h.Clock.Now()), []byte("a"))
	mine := h.AddEpisode(t, episodeOf("alex", "Mine", h.Clock.Now()), []byte("a"))
	yes := url.Values{"listened": {"true"}}

	onFailed := h.Send(t, http.MethodPost, listenedPath(failed.ID), nil, yes)
	onForeign := h.Send(t, http.MethodPost, listenedPath(beas.ID), nil, yes)
	unknown := h.Send(t, http.MethodPost, listenedPath(mine.ID), nil, url.Values{"listened": {"maybe"}})

	assert.Equal(t, http.StatusNotFound, onFailed.Status)
	assert.Equal(t, http.StatusNotFound, onForeign.Status)
	assert.False(t, h.Episode(t, "bea", beas.ID).Listened, "another User's Episode is untouched")
	assert.Equal(t, http.StatusBadRequest, unknown.Status)
	assert.False(t, h.Episode(t, "alex", mine.ID).Listened)
}

func TestMarkersAppearOnReadyEpisodesOnlyAndThePlayerReports(t *testing.T) {
	h := servicetest.Start(t, twoUsers()...)
	ready := h.AddEpisode(t, episodeOf("alex", "Ready", h.Clock.Now()), []byte("a"))
	h.AddFailedEpisode(t, episodeOf("alex", "", h.Clock.Now()), "boom")

	list := h.Get(t, "/")
	page := h.Get(t, episodePath(ready.ID))

	assert.Equal(t, 1, strings.Count(list.Body, `aria-label="Not listened yet.`), "one marker, none on the failed Episode")
	assert.NotContains(t, page.Body, "data-demo", "the player reports")
	assert.Contains(t, page.Body, "Not listened yet. Mark as listened.")
}

func TestAVisitorSeesEveryEpisodeAndNeitherMarkerNorFilter(t *testing.T) {
	h := servicetest.Start(t, threeUsers()...)
	heard := h.AddEpisode(t, episodeOf("demo", "Heard", h.Clock.Now()), []byte("a"))
	h.MarkListened(t, heard, true)
	h.AddEpisode(t, episodeOf("demo", "Unheard", h.Clock.Now()), []byte("b"))

	list := h.GetWithHeaders(t, "/", visitor())
	page := h.GetWithHeaders(t, episodePath(heard.ID), visitor())

	assert.Contains(t, list.Body, "Heard")
	assert.Contains(t, list.Body, "Unheard")
	for _, p := range []servicetest.Page{list, page} {
		assert.NotContains(t, p.Body, "Mark as")
		assert.NotContains(t, p.Body, "show=all")
	}
	assert.Contains(t, page.Body, "data-demo", "the player of a Visitor does not report")
}
