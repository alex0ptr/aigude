package service_test

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/alex0ptr/aigude/business"
	"github.com/alex0ptr/aigude/pkg/service/servicetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// feedWith is an RSS document with one item published two hours before the test clock starts.
func feedWith(title, link, description string) string {
	return fmt.Sprintf(`<?xml version="1.0"?>
<rss version="2.0"><channel><title>Test feed</title>
<item><title>%s</title><link>%s</link><description>%s</description><pubDate>Thu, 29 Oct 2026 05:00:00 +0000</pubDate></item>
</channel></rss>`, title, link, description)
}

// generatingFile is a User Config file whose only CastProfile reads the given feed.
func generatingFile(feedURL, extraProfileKeys string) string {
	return fmt.Sprintf(`
[user]
id = "alex"
display_name = "Alex"

[cast_profiles.morning]
display_name = "Morning briefing"
max_lookback = "24h"
%s

[cast_profiles.morning.subscriptions.news]
display_name = "News"
kind = "rss"
url = %q
`, extraProfileKeys, feedURL)
}

// threeSections is a Script with an introduction, one topic and a closing.
func threeSections() business.Script {
	return business.Script{Title: "Heute im Überblick", Sections: []business.Section{
		{Heading: "Intro", Body: "Guten Morgen."},
		{Heading: "Thema", Body: "Das ist die Nachricht.", Sources: []int{1}},
		{Heading: "Schluss", Body: "Bis morgen."},
	}}
}

var episodeLink = regexp.MustCompile(`/episodes/(\d+)`)

func TestGenerateNowProducesAPlayableEpisode(t *testing.T) {
	feed := servicetest.ServeFeed(t, feedWith("Neue Nachricht", "https://example.org/a", "Kurztext"))
	h := servicetest.Start(t, servicetest.WithUserConfigFile("alex.toml", generatingFile(feed, "")))
	h.ScriptWriter.Script = threeSections()
	h.TTS.Length = 2 * time.Second

	posted := h.Post(t, "/cast-profiles/morning/generate")
	require.Equal(t, 200, posted.Status, "the browser lands on the overview again")
	list := h.WaitFor(t, "/", "the generated Episode in the list", func(p servicetest.Page) bool {
		return strings.Contains(p.Body, "Heute im Überblick")
	})
	assert.Contains(t, list.Body, "Morning briefing")

	id := episodeLink.FindStringSubmatch(list.Body)
	require.NotNil(t, id, "the Episode links to its page")
	page := h.Get(t, "/episodes/"+id[1])
	assert.Equal(t, 200, page.Status)
	chapterStarts := regexp.MustCompile(`data-chapter-start="([^"]+)"[^>]*>([^<]+)<`).FindAllStringSubmatch(page.Body, -1)
	require.Len(t, chapterStarts, 3)
	// Three Sections of two seconds, separated by the default pause of one second.
	assert.Equal(t, []string{"0.000", "Intro"}, chapterStarts[0][1:])
	assert.Equal(t, []string{"3.000", "Thema"}, chapterStarts[1][1:])
	assert.Equal(t, []string{"6.000", "Schluss"}, chapterStarts[2][1:])

	audio := h.Get(t, "/episodes/"+id[1]+"/audio")
	require.Equal(t, 200, audio.Status)
	assert.Equal(t, "audio/ogg", audio.Header.Get("Content-Type"))
	assert.InDelta(t, 8.0, servicetest.OggDuration(t, []byte(audio.Body)).Seconds(), 0.1,
		"one file made of three Sections and two pauses")
	assert.Equal(t, []string{"Guten Morgen.", "Das ist die Nachricht.", "Bis morgen."}, h.TTS.Texts(),
		"each Section is spoken in its own call")
}

// generateAndWaitFor asks for Episodes and waits until the persistence holds the given number of them.
func generateAndWaitFor(t *testing.T, h *servicetest.Harness, requests, episodes int) []business.Episode {
	t.Helper()
	for i := 0; i < requests; i++ {
		require.Equal(t, 200, h.Post(t, "/cast-profiles/morning/generate").Status)
	}
	return waitForEpisodes(t, h, "alex", episodes)
}

// waitForEpisodes waits until the persistence holds the given number of the User's Episodes and returns them.
func waitForEpisodes(t *testing.T, h *servicetest.Harness, userID string, episodes int) []business.Episode {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if got := h.Episodes(t, userID); len(got) >= episodes {
			return got
		}
		require.True(t, time.Now().Before(deadline), "gave up waiting for %d Episodes", episodes)
		time.Sleep(20 * time.Millisecond)
	}
}

func TestTheScriptIsStoredWithTheEpisode(t *testing.T) {
	feed := servicetest.ServeFeed(t, feedWith("Neue Nachricht", "https://example.org/a", "Kurztext"))
	h := servicetest.Start(t, servicetest.WithUserConfigFile("alex.toml", generatingFile(feed, "")))
	h.ScriptWriter.Script = threeSections()

	episodes := generateAndWaitFor(t, h, 1, 1)

	stored := h.Episode(t, "alex", episodes[0].ID)
	assert.Equal(t, "Heute im Überblick", stored.Script.Title)
	require.Len(t, stored.Script.Sections, 3)
	assert.Equal(t, "Das ist die Nachricht.", stored.Script.Sections[1].Body)
}

func TestLanguageAndVoiceComeFromTheProfileAndOtherwiseFromTheServiceDefaults(t *testing.T) {
	tests := []struct {
		name, profileKeys, text, rulesIn string
		voice                            business.Voice
	}{
		{"the profile wins", "language = \"en-GB\"\nvoice = \"Kore\"", "Hello.", "English", business.Voice{Language: "en-GB", Name: "Kore"}},
		{"the service defaults apply", "", "Hallo.", "German", business.Voice{Language: "de-DE", Name: "Achernar"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			feed := servicetest.ServeFeed(t, feedWith("News", "https://example.org/a", "Text"))
			h := servicetest.Start(t, servicetest.WithUserConfigFile("alex.toml", generatingFile(feed, tt.profileKeys)))
			h.ScriptWriter.Script = business.Script{Title: "T", Sections: []business.Section{{Heading: "H", Body: tt.text}}}

			generateAndWaitFor(t, h, 1, 1)

			assert.Equal(t, []servicetest.SpeechCall{{Text: tt.text, Voice: tt.voice}}, h.TTS.Calls())
			assert.Contains(t, h.ScriptWriter.Requests()[0].System, tt.rulesIn, "the rules are written for the language of the speech")
		})
	}
}

func TestProfilePromptReplacesTheDefaultStyleButNotTheSystemRules(t *testing.T) {
	feed := servicetest.ServeFeed(t, feedWith("News", "https://example.org/a", "Text"))
	h := servicetest.Start(t, servicetest.WithUserConfigFile("alex.toml", generatingFile(feed, "prompt = \"Sei kurz.\"")))
	h.ScriptWriter.Script = threeSections()

	generateAndWaitFor(t, h, 1, 1)

	request := h.ScriptWriter.Requests()[0]
	assert.Equal(t, "Sei kurz.", request.Style)
	assert.NotEmpty(t, request.System)
	assert.NotContains(t, request.System, "Sei kurz.", "the built-in system prompt stays as it is")
}

func TestThePromptNamesTodayInTheConfiguredTimeZone(t *testing.T) {
	feed := servicetest.ServeFeed(t, feedWith("News", "https://example.org/a", "Text"))
	h := servicetest.Start(t, servicetest.WithUserConfigFile("alex.toml", generatingFile(feed, "")))
	h.ScriptWriter.Script = threeSections()
	// 23:30 UTC on the 29th is already the 30th in Berlin.
	h.Clock.Advance(16*time.Hour + 30*time.Minute)

	generateAndWaitFor(t, h, 1, 1)

	system := h.ScriptWriter.Requests()[0].System
	assert.Contains(t, system, "2026-10-30")
}

func TestEpisodesAreProducedOneAtATime(t *testing.T) {
	feed := servicetest.ServeFeed(t, feedWith("News", "https://example.org/a", "Text"))
	file := generatingFile(feed, "")
	for _, id := range []string{"noon", "evening"} {
		file += fmt.Sprintf("\n[cast_profiles.%[1]s]\ndisplay_name = %[1]q\nmax_lookback = \"24h\"\n\n"+
			"[cast_profiles.%[1]s.subscriptions.news]\ndisplay_name = \"News\"\nkind = \"rss\"\nurl = %[2]q\n", id, feed)
	}
	h := servicetest.Start(t, servicetest.WithUserConfigFile("alex.toml", file))
	h.ScriptWriter.Script = threeSections()
	h.ScriptWriter.Delay = 50 * time.Millisecond

	for _, id := range []string{"morning", "noon", "evening"} {
		require.Equal(t, 200, h.Post(t, "/cast-profiles/"+id+"/generate").Status)
	}
	waitForEpisodes(t, h, "alex", 3)

	assert.Equal(t, 1, h.ScriptWriter.MostAtOnce(), "further requests wait in the queue")
}

func TestFailingSpeechLeavesNoAudioAndAFailedEpisode(t *testing.T) {
	feed := servicetest.ServeFeed(t, feedWith("News", "https://example.org/a", "Text"))
	h := servicetest.Start(t, servicetest.WithUserConfigFile("alex.toml", generatingFile(feed, "")))
	h.ScriptWriter.Script = threeSections()
	h.TTS.Err = errors.New("speech quota used up")

	episodes := generateAndWaitFor(t, h, 1, 1)

	assert.Contains(t, episodes[0].Failure, "speech quota used up")
	assert.Empty(t, episodes[0].AudioLocation)
	assert.Empty(t, audioFiles(t, h), "no audio file remains")
	assert.NotContains(t, h.Get(t, "/").Body, `data-testid="episode"`, "a failed Episode is not offered for listening")
}

// audioFiles lists the audio files the service holds.
func audioFiles(t *testing.T, h *servicetest.Harness) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(h.Settings.DataDir, "episodes"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	require.NoError(t, err)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}
