package service_test

import (
	"fmt"
	"html"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/alex0ptr/aigude/business"
	"github.com/alex0ptr/aigude/pkg/service/servicetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// episodeOf is an Episode of the given User, created at the given time.
func episodeOf(userID, title string, created time.Time) business.Episode {
	return business.Episode{
		Date:          created,
		UserID:        userID,
		CastProfileID: "morning",
		Script:        business.Script{Title: title},
		Duration:      90 * time.Second,
	}
}

func TestEpisodesPageListsOnlyTheSelectedUsersEpisodesNewestFirst(t *testing.T) {
	h := servicetest.Start(t, twoUsers()...)
	base := h.Clock.Now()
	h.AddEpisode(t, episodeOf("alex", "Alex older", base.Add(-2*time.Hour)), []byte("a"))
	h.AddEpisode(t, episodeOf("alex", "Alex newer", base.Add(-time.Hour)), []byte("b"))
	h.AddEpisode(t, episodeOf("bea", "Bea only", base), []byte("c"))

	page := h.Get(t, "/")

	assert.Regexp(t, `(?s)Alex newer.*Alex older`, page.Body)
	assert.NotContains(t, page.Body, "Bea only", "another User's Episodes stay out")
	assert.Contains(t, page.Body, "Morning briefing")
}

func TestAnEpisodeShowsTheDisplayNameOfItsCastProfileOrItsIdWhenTheUserConfigHasNoSuchProfile(t *testing.T) {
	h := servicetest.Start(t, twoUsers()...)
	base := h.Clock.Now()
	h.AddEpisode(t, episodeOf("alex", "Of a show that exists", base.Add(-time.Hour)), []byte("a"))
	retired := episodeOf("alex", "Of a show that is gone", base)
	retired.CastProfileID = "retired"
	h.AddEpisode(t, retired, []byte("b"))

	page := h.Get(t, "/")

	assert.Regexp(t, `(?s)Of a show that is gone.*?retired ·`, page.Body)
	assert.Regexp(t, `(?s)Of a show that exists.*?Morning briefing ·`, page.Body)
}

func TestOlderEpisodesAreLoadedTwentyAtATime(t *testing.T) {
	h := servicetest.Start(t, twoUsers()...)
	base := h.Clock.Now()
	for i := 1; i <= 25; i++ {
		h.AddEpisode(t, episodeOf("alex", fmt.Sprintf("Episode %02d", i), base.Add(time.Duration(i)*time.Minute)), []byte("a"))
	}

	first := h.Get(t, "/")

	assert.Equal(t, 20, strings.Count(first.Body, `data-testid="episode"`))
	assert.Contains(t, first.Body, "Episode 25")
	assert.Contains(t, first.Body, "Episode 06")
	assert.NotContains(t, first.Body, "Episode 05")
	more := regexp.MustCompile(`hx-get="([^"]+)"`).FindStringSubmatch(first.Body)
	require.NotNil(t, more, "a way to load older Episodes")

	older := h.Get(t, html.UnescapeString(more[1]))

	assert.Equal(t, http.StatusOK, older.Status)
	assert.Equal(t, 5, strings.Count(older.Body, `data-testid="episode"`))
	assert.Contains(t, older.Body, "Episode 05")
	assert.Contains(t, older.Body, "Episode 01")
	assert.NotContains(t, older.Body, "Episode 06")
	assert.NotContains(t, older.Body, "hx-get", "nothing older is left")
}

func TestLoadMoreRejectsACursorThatIsNotAnEpisodeId(t *testing.T) {
	h := servicetest.Start(t, twoUsers()...)

	for _, cursor := range []string{"abc", "-3", "0"} {
		assert.Equal(t, http.StatusBadRequest, h.Get(t, "/episodes?before="+cursor).Status, cursor)
	}
}

// episodeWithChapters is an Episode of alex whose audio is the letters a to z. Its two Chapters mark where its two
// Sections begin.
func episodeWithChapters(h *servicetest.Harness, t *testing.T) business.Episode {
	ep := episodeOf("alex", "Chapter show", h.Clock.Now())
	ep.Script.Sections = []business.Section{{Heading: "Welcome"}, {Heading: "Markets"}}
	ep.Chapters = []business.Chapter{
		{Title: "Welcome", Start: 0},
		{Title: "Markets", Start: 12500 * time.Millisecond},
	}
	return h.AddEpisode(t, ep, []byte("abcdefghijklmnopqrstuvwxyz"))
}

func TestEpisodePagePlaysTheAudioAndListsTheChapters(t *testing.T) {
	h := servicetest.Start(t, twoUsers()...)
	ep := episodeWithChapters(h, t)

	page := h.Get(t, fmt.Sprintf("/episodes/%d", ep.ID))

	assert.Equal(t, http.StatusOK, page.Status)
	assert.Contains(t, page.Body, "Chapter show")
	assert.Contains(t, page.Body, fmt.Sprintf(`<audio id="player" controls preload="metadata" src="/episodes/%d/audio"`, ep.ID))
	assert.Regexp(t, `(?s)data-chapter-start="0.000">Welcome.*data-chapter-start="12.500">Markets`, page.Body)
	assert.Contains(t, page.Body, `src="/static/player.js"`)
	script := h.Get(t, "/static/player.js")
	assert.Equal(t, http.StatusOK, script.Status)
	assert.Contains(t, script.Body, "currentTime", "choosing a Chapter sets the playback position")
}

func TestEveryPageLinksTheIconThatIsServed(t *testing.T) {
	h := servicetest.Start(t, twoUsers()...)

	page := h.Get(t, "/")
	icon := h.Get(t, "/static/favicon.svg")

	assert.Contains(t, page.Body, `href="/static/favicon.svg"`)
	assert.Equal(t, http.StatusOK, icon.Status)
	assert.Contains(t, icon.Body, "<svg")
}

func TestAudioIsServedWithRangeSupportSoTheBrowserCanSeek(t *testing.T) {
	h := servicetest.Start(t, twoUsers()...)
	ep := episodeWithChapters(h, t)
	path := fmt.Sprintf("/episodes/%d/audio", ep.ID)

	whole := h.Get(t, path)
	part := h.GetWithHeader(t, path, "Range", "bytes=5-9")

	assert.Equal(t, http.StatusOK, whole.Status)
	assert.Equal(t, "audio/ogg", whole.Header.Get("Content-Type"))
	assert.Equal(t, "bytes", whole.Header.Get("Accept-Ranges"))
	assert.Equal(t, "abcdefghijklmnopqrstuvwxyz", whole.Body)
	assert.Equal(t, http.StatusPartialContent, part.Status)
	assert.Equal(t, "bytes 5-9/26", part.Header.Get("Content-Range"))
	assert.Equal(t, "fghij", part.Body)
}

func TestDeletingAnEpisodeRemovesItsRecordAndItsAudioFile(t *testing.T) {
	h := servicetest.Start(t, twoUsers()...)
	keep := h.AddEpisode(t, episodeOf("alex", "Keep me", h.Clock.Now()), []byte("keep"))
	doomed := h.AddEpisode(t, episodeOf("alex", "Delete me", h.Clock.Now().Add(time.Minute)), []byte("doomed"))
	require.True(t, h.AudioExists(t, doomed))

	landed := h.Post(t, fmt.Sprintf("/episodes/%d/delete", doomed.ID))

	assert.Equal(t, http.StatusOK, landed.Status)
	assert.NotContains(t, landed.Body, "Delete me")
	assert.Contains(t, landed.Body, "Keep me")
	assert.Equal(t, http.StatusNotFound, h.Get(t, fmt.Sprintf("/episodes/%d", doomed.ID)).Status)
	assert.False(t, h.AudioExists(t, doomed), "the audio is removed with the Episode")
	assert.True(t, h.AudioExists(t, keep))
}

func TestAUserCannotReachAnotherUsersEpisode(t *testing.T) {
	h := servicetest.Start(t, twoUsers()...)
	alexs := h.AddEpisode(t, episodeOf("alex", "Private to Alex", h.Clock.Now()), []byte("secret"))
	h.SelectUser(t, "bea")

	for _, path := range []string{
		fmt.Sprintf("/episodes/%d", alexs.ID),
		fmt.Sprintf("/episodes/%d/audio", alexs.ID),
	} {
		assert.Equal(t, http.StatusNotFound, h.Get(t, path).Status, path)
	}
	assert.Equal(t, http.StatusNotFound, h.Post(t, fmt.Sprintf("/episodes/%d/delete", alexs.ID)).Status)

	h.SelectUser(t, "alex")
	assert.Contains(t, h.Get(t, "/").Body, "Private to Alex", "the Episode was not deleted")
	assert.True(t, h.AudioExists(t, alexs))
}

func TestEpisodesOfAUserWhoseFileIsRemovedReturnWithTheSameId(t *testing.T) {
	h := servicetest.Start(t, twoUsers()...)
	ep := h.AddEpisode(t, episodeOf("alex", "Comes back", h.Clock.Now()), []byte("a"))
	h.SelectUser(t, "alex")

	servicetest.RemoveUserConfig(t, h.Settings, "zzz-first-file.toml")
	gone := h.WaitFor(t, "/", "Alex to vanish from the dropdown", func(p servicetest.Page) bool {
		return !strings.Contains(p.Body, `value="alex"`)
	})
	assert.NotContains(t, gone.Body, "Comes back")
	assert.Equal(t, http.StatusNotFound, h.Get(t, fmt.Sprintf("/episodes/%d", ep.ID)).Status)
	assert.True(t, h.AudioExists(t, ep), "the Episode stays stored")

	servicetest.WriteUserConfig(t, h.Settings, "restored.toml", alexFile)
	h.WaitFor(t, "/", "Alex to return", func(p servicetest.Page) bool {
		return strings.Contains(p.Body, `value="alex"`)
	})
	h.SelectUser(t, "alex")
	assert.Contains(t, h.Get(t, "/").Body, "Comes back")
}
