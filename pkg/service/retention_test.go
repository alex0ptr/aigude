package service_test

import (
	"testing"
	"time"

	"github.com/alex0ptr/aigude/pkg/service/servicetest"
	"github.com/alex0ptr/aigude/pkg/settings"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEpisodesOlderThanTheRetentionAreDeletedAtTheStartAndOnceADayAfter(t *testing.T) {
	h := servicetest.Start(t)
	now := h.Clock.Now()
	old := h.AddEpisode(t, episodeOf("alex", "Old", now.Add(-31*24*time.Hour)), []byte("a"))
	young := h.AddEpisode(t, episodeOf("alex", "Young", now.Add(-29*24*time.Hour)), []byte("b"))

	h.Stop(t)
	h.Restart(t)

	require.Eventually(t, func() bool { return len(h.Episodes(t, "alex")) == 1 }, 5*time.Second, 20*time.Millisecond,
		"the clean-up at the start deletes what is older than the Retention")
	assert.False(t, h.AudioExists(t, old), "the audio goes with the Episode")
	assert.True(t, h.AudioExists(t, young))

	h.Clock.Advance(25 * time.Hour)

	require.Eventually(t, func() bool { return len(h.Episodes(t, "alex")) == 0 }, 5*time.Second, 20*time.Millisecond,
		"the Episode that has aged past the Retention since then goes with the daily clean-up")
	assert.False(t, h.AudioExists(t, young))
}

func TestARetentionOfZeroDeletesNothing(t *testing.T) {
	h := servicetest.Start(t, servicetest.WithSettings(func(s *settings.Settings) { s.EpisodeRetention = 0 }))
	h.AddEpisode(t, episodeOf("alex", "Old", h.Clock.Now().Add(-400*24*time.Hour)), []byte("a"))

	h.Stop(t)
	h.Restart(t)
	h.Clock.Advance(48 * time.Hour)

	time.Sleep(300 * time.Millisecond) // the background work looks at the clock every 20 ms
	assert.Len(t, h.Episodes(t, "alex"), 1)
}
