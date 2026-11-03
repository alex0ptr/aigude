package service_test

import (
	"net/http"
	"testing"

	"github.com/alex0ptr/aigude/pkg/service/servicetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// visitor is the proxy header of a request for the demo User, which is in Demo Mode.
func visitor() map[string]string { return pinned("demo") }

// threeUsers are the two Users of twoUsers and the demo User.
func threeUsers() []servicetest.Option { return append(twoUsers(), withDemoUser()) }

const demoBar = "Demo mode: you can browse and play Episodes, but nothing can be changed."
const repoLink = `href="https://github.com/alex0ptr/aigude"`

func TestVisitorCanBrowseAndPlay(t *testing.T) {
	h := servicetest.Start(t, threeUsers()...)
	episode := h.AddEpisode(t, episodeOf("demo", "Demo only", h.Clock.Now()), []byte("0123456789"))

	list := h.GetWithHeaders(t, "/", visitor())
	page := h.GetWithHeaders(t, episodePath(episode.ID), visitor())
	ranged := h.GetWithHeaders(t, episodePath(episode.ID)+"/audio", map[string]string{pinHeader: "demo", "Range": "bytes=2-4"})
	head := h.Send(t, http.MethodHead, "/", visitor(), nil)

	assert.Equal(t, http.StatusOK, list.Status)
	assert.Contains(t, list.Body, "Demo only")
	assert.Equal(t, http.StatusOK, page.Status)
	assert.Equal(t, http.StatusPartialContent, ranged.Status)
	assert.Equal(t, "234", ranged.Body)
	assert.Equal(t, http.StatusOK, head.Status)
}

func TestVisitorCannotGenerateOrDeleteAndNothingChanges(t *testing.T) {
	h := servicetest.Start(t, threeUsers()...)
	episode := h.AddEpisode(t, episodeOf("demo", "Demo only", h.Clock.Now()), []byte("a"))

	generate := h.PostWithHeaders(t, "/cast-profiles/morning/generate", visitor())
	deleted := h.PostWithHeaders(t, episodePath(episode.ID)+"/delete", visitor())

	assert.Equal(t, http.StatusForbidden, generate.Status)
	assert.Equal(t, http.StatusForbidden, deleted.Status)
	assert.Len(t, h.Episodes(t, "demo"), 1, "the Episode is still there")
	assert.True(t, h.AudioExists(t, episode))
	assert.Empty(t, h.ScriptWriter.Requests(), "nothing was queued")
}

func TestDemoModeRefusesEveryRequestThatIsNotReading(t *testing.T) {
	h := servicetest.Start(t, threeUsers()...)

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		page := h.Send(t, method, "/some/route/added/later", visitor(), nil)
		assert.Equal(t, http.StatusForbidden, page.Status, method)
	}
}

func TestButtonsAndBarShowOnlyForTheDemoUserOfTheSameInstance(t *testing.T) {
	h := servicetest.Start(t, threeUsers()...)
	demoEpisode := h.AddEpisode(t, episodeOf("demo", "Demo only", h.Clock.Now()), []byte("a"))
	ownEpisode := h.AddEpisode(t, episodeOf("alex", "Alex only", h.Clock.Now()), []byte("a"))

	demoList := h.GetWithHeaders(t, "/", visitor())
	demoPage := h.GetWithHeaders(t, episodePath(demoEpisode.ID), visitor())
	ownList := h.Get(t, "/")
	ownPage := h.Get(t, episodePath(ownEpisode.ID))

	for _, page := range []servicetest.Page{demoList, demoPage} {
		assert.Contains(t, page.Body, demoBar)
		assert.Contains(t, page.Body, repoLink)
		assert.Contains(t, page.Body, `target="_blank"`)
		assert.NotContains(t, page.Body, "/generate")
		assert.NotContains(t, page.Body, "/delete")
	}
	assert.Contains(t, demoList.Body, "Morning briefing", "CastProfiles stay listed")
	assert.Contains(t, demoList.Body, "heise online", "Subscriptions stay listed")
	for _, page := range []servicetest.Page{ownList, ownPage} {
		assert.NotContains(t, page.Body, demoBar)
	}
	assert.Contains(t, ownList.Body, "/cast-profiles/morning/generate")
	assert.Contains(t, ownPage.Body, "/delete")
}

func TestDemoModeFollowsTheSelectedUserWhenNoUserIsPinned(t *testing.T) {
	h := servicetest.Start(t, threeUsers()...)
	require.NotContains(t, h.Get(t, "/").Body, demoBar)

	h.SelectUser(t, "demo")
	inDemo := h.Get(t, "/")
	refused := h.Post(t, "/cast-profiles/morning/generate")
	h.SelectUser(t, "bea")
	left := h.Get(t, "/")

	assert.Contains(t, inDemo.Body, demoBar)
	assert.Contains(t, inDemo.Body, `<select`, "nobody is pinned, so the User can still be switched")
	assert.Equal(t, http.StatusForbidden, refused.Status)
	assert.NotContains(t, left.Body, demoBar)
	assert.Contains(t, left.Body, "Evening recap")
}

func TestVisitorDoesNotSeeWhatOnlyTheOperatorShouldSee(t *testing.T) {
	h := servicetest.Start(t, servicetest.WithUserConfigFile("alex.toml", alexFile), withDemoUser())
	servicetest.WriteUserConfig(t, h.Settings, "carl.toml", "[user]\nid = \"carl\"\n[cast_profiles.x]\n")
	plain := h.WaitFor(t, "/", "the problem of carl.toml", pageContains("carl.toml"))
	require.Contains(t, plain.Body, "Problem in the User Config", "the owner sees the problem")

	page := h.GetWithHeaders(t, "/", visitor())

	assert.NotContains(t, page.Body, "carl.toml")
	assert.NotContains(t, page.Body, "Problem in the User Config")
}

func TestVisitorDoesNotSeeTheLog(t *testing.T) {
	h := servicetest.Start(t, threeUsers()...)
	require.Contains(t, h.Get(t, "/").Body, `id="log"`)

	page := h.GetWithHeaders(t, "/", visitor())
	log := h.GetWithHeaders(t, "/log", visitor())

	assert.NotContains(t, page.Body, `id="log"`)
	assert.Equal(t, http.StatusForbidden, log.Status)
}
