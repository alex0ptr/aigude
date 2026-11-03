package service_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/alex0ptr/aigude/pkg/service/servicetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTheChaptersOfAnEpisodeAreAlsoTagsInItsAudioFile(t *testing.T) {
	feed := servicetest.ServeFeed(t, feedWith("Neue Nachricht", "https://example.org/a", "Kurztext"))
	h := servicetest.Start(t, servicetest.WithUserConfigFile("alex.toml", generatingFile(feed, "")))
	h.ScriptWriter.Script = threeSections()
	h.TTS.Length = 2 * time.Second
	stored := h.Episode(t, "alex", generateAndWaitFor(t, h, 1, 1)[0].ID)

	file := h.Get(t, fmt.Sprintf("/episodes/%d/audio", stored.ID))

	require.Equal(t, 200, file.Status)
	tagged := servicetest.OggChapters(t, []byte(file.Body))
	require.Len(t, tagged, len(stored.Chapters), "one chapter per Section")
	for i, chapter := range stored.Chapters {
		assert.Equal(t, chapter.Title, tagged[i].Title, "the Section's heading is the title")
		assert.Equal(t, chapter.Start.Round(time.Millisecond), tagged[i].Start, "start of chapter %d to the millisecond", i+1)
	}
	assert.Equal(t, []string{"Intro", "Thema", "Schluss"}, []string{tagged[0].Title, tagged[1].Title, tagged[2].Title})
	assert.True(t, servicetest.OggPlays(t, []byte(file.Body)), "the tagged file is still valid audio")
	assert.InDelta(t, 8.0, servicetest.OggDuration(t, []byte(file.Body)).Seconds(), 0.1)
}
