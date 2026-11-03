package service_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/alex0ptr/aigude/pkg/service/servicetest"
	"github.com/stretchr/testify/assert"
)

const pinHeader = "X-Aigude-User"

func episodePath(id int64) string { return fmt.Sprintf("/episodes/%d", id) }

func pinned(id string) map[string]string { return map[string]string{pinHeader: id} }

func TestPinnedUserSeesOnlyTheirEpisodesAndAudio(t *testing.T) {
	h := servicetest.Start(t, twoUsers()...)
	alexs := h.AddEpisode(t, episodeOf("alex", "Alex only", h.Clock.Now()), []byte("alex-audio"))
	beas := h.AddEpisode(t, episodeOf("bea", "Bea only", h.Clock.Now()), []byte("bea-audio"))
	h.SelectUser(t, "alex")

	list := h.GetWithHeaders(t, "/", pinned("bea"))
	own := h.GetWithHeaders(t, episodePath(beas.ID), pinned("bea"))
	ownAudio := h.GetWithHeaders(t, episodePath(beas.ID)+"/audio", pinned("bea"))
	foreign := h.GetWithHeaders(t, episodePath(alexs.ID), pinned("bea"))
	foreignAudio := h.GetWithHeaders(t, episodePath(alexs.ID)+"/audio", pinned("bea"))
	more := h.GetWithHeaders(t, "/episodes", pinned("bea"))

	assert.Contains(t, list.Body, "Bea only")
	assert.NotContains(t, list.Body, "Alex only")
	assert.NotContains(t, more.Body, "Alex only")
	assert.Equal(t, http.StatusOK, own.Status)
	assert.Equal(t, "bea-audio", ownAudio.Body)
	assert.Equal(t, http.StatusNotFound, foreign.Status)
	assert.Equal(t, http.StatusNotFound, foreignAudio.Status)
}

func TestPinnedUserIsRefusedForAnythingThatIsNotAKnownUser(t *testing.T) {
	h := servicetest.Start(t, twoUsers()...)
	servicetest.WriteUserConfig(t, h.Settings, "carl.toml", "[user]\nid = \"carl\"\n[cast_profiles.x]\n")
	h.WaitFor(t, "/", "the problem of carl.toml", pageContains("carl.toml"))
	h.SelectUser(t, "alex")
	episode := h.AddEpisode(t, episodeOf("alex", "Alex only", h.Clock.Now()), []byte("a"))

	for name, value := range map[string]string{"empty": "", "unknown": "nobody", "broken file": "carl", "display name": "Alex", "upper case": "ALEX"} {
		for _, path := range []string{"/", "/episodes", episodePath(episode.ID), episodePath(episode.ID) + "/audio", "/static/theme.css", "/log"} {
			page := h.GetWithHeaders(t, path, pinned(value))
			assert.Equal(t, http.StatusForbidden, page.Status, "%s: %s", name, path)
			assert.NotContains(t, page.Body, "Morning briefing", "%s: %s", name, path)
		}
	}
}

func TestPinnedUserHasNoDropdownAndCannotSwitch(t *testing.T) {
	h := servicetest.Start(t, twoUsers()...)

	page := h.GetWithHeaders(t, "/", pinned("alex"))
	switched := h.SelectUserWithHeaders(t, "bea", pinned("alex"))
	after := h.GetWithHeaders(t, "/", pinned("alex"))

	assert.NotContains(t, page.Body, `<select`)
	assert.NotContains(t, page.Body, `action="/user"`)
	assert.Equal(t, http.StatusForbidden, switched.Status)
	assert.Contains(t, after.Body, "Morning briefing")
}

func TestUserHeaderRepeatedWithAnotherValueIsRefused(t *testing.T) {
	h := servicetest.Start(t, twoUsers()...)

	disagreeing := h.GetWithHeaderValues(t, "/", http.Header{pinHeader: {"alex", "bea"}})
	agreeing := h.GetWithHeaderValues(t, "/", http.Header{pinHeader: {"alex", "alex"}})

	assert.Equal(t, http.StatusForbidden, disagreeing.Status)
	assert.Equal(t, http.StatusOK, agreeing.Status)
}
