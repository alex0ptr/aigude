package service_test

import (
	"strings"
	"testing"
	"time"

	"github.com/alex0ptr/aigude/pkg/service/servicetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTheLogOfTheServiceCanBeOpenedOnTheOverview(t *testing.T) {
	feed := servicetest.ServeFeed(t, feedWith("Neue Nachricht", "https://example.org/a", "Kurztext"))
	h := servicetest.Start(t, servicetest.WithUserConfigFile("alex.toml", twoProfilesFile(feed)))
	h.ScriptWriter.Script = threeSections()
	h.ScriptWriter.Delay = 100 * time.Millisecond
	h.Post(t, "/cast-profiles/morning/generate")

	overview := h.Get(t, "/")
	assert.Contains(t, overview.Body, `<details id="log"`, "the log folds out on the overview")
	assert.Contains(t, overview.Body, `hx-get="/log"`, "its lines are fetched from the service")
	assert.Contains(t, overview.Body, "every 1s", "and fetched again every second while it is open")
	assert.Contains(t, overview.Body, "scrollTop = this.scrollHeight", "the log follows its newest line")
	assert.Contains(t, overview.Body, "before-swap", "unless the reader scrolled up to read")
	assert.Contains(t, overview.Body, "X-Log-Cursor", "only the lines after the last one it has are fetched")

	log := h.Get(t, "/log")
	require.Equal(t, 200, log.Status)
	assert.Contains(t, log.Body, "Episode requested")
	assert.Contains(t, log.Body, "profile=morning")
	requested, assembled := strings.Index(log.Body, "Episode requested"), strings.Index(log.Body, "Service assembled")
	require.NotEqual(t, -1, assembled, "what the service logged while it started is there")
	assert.Less(t, assembled, requested, "the newest line is at the bottom")
}

func TestTheLogHoldsWhatTheServiceLoggedWhileStarting(t *testing.T) {
	h := servicetest.Start(t)

	assert.Contains(t, h.Get(t, "/log").Body, "Service assembled")
}

func TestTheLogAnswersOnlyWithLinesAfterTheCursorOfTheRequest(t *testing.T) {
	h := servicetest.Start(t)
	first := h.Get(t, "/log")
	cursor := first.Header.Get("X-Log-Cursor")
	require.NotEmpty(t, cursor)
	assert.Equal(t, "true", first.Header.Get("X-Log-Reset"), "without a cursor the reader starts over")

	next := h.Get(t, "/log?cursor="+cursor)

	assert.NotContains(t, next.Body, "Service assembled", "what the reader has is not sent again")
	assert.Empty(t, next.Header.Get("X-Log-Reset"))
}
