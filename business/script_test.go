package business

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeWriter is a ScriptWriter that returns a fixed Script and remembers what it was asked.
type fakeWriter struct {
	script   Script
	err      error
	usage    Usage
	system   string
	style    string
	contents []Content
}

func (f *fakeWriter) WriteScript(system, style string, contents []Content) (Script, Usage, error) {
	f.system, f.style, f.contents = system, style, contents
	return f.script, f.usage, f.err
}

// testEpisodes are Episodes that only write Scripts, on 29 October 2026 in Berlin, in German with a budget of 4000 words.
func testEpisodes(writer ScriptWriter) *Episodes {
	return testEpisodesWith(writer, nil, nil, nil, 0)
}

// testEpisodesWith are testEpisodes that also speak, encode and keep Episodes, with the pause between Sections.
func testEpisodesWith(writer ScriptWriter, tts TTS, encoder AudioEncoder, repository EpisodeRepository, pause time.Duration) *Episodes {
	berlin, _ := time.LoadLocation("Europe/Berlin")
	clock := fixedClock{time.Date(2026, time.October, 29, 7, 0, 0, 0, time.UTC)}
	return NewEpisodes(writer, tts, encoder, repository, clock, berlin, Voice{Language: "de-DE", Name: "Achernar"}, 4000, 12000, pause)
}

// fixedClock is a Clock that stands still.
type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

func (c fixedClock) Sleep(time.Duration) {}

func twoItems() []Content { return []Content{{Title: "one"}, {Title: "two"}} }

func TestGenerateScriptHandsTheWriterRulesStyleAndContents(t *testing.T) {
	writer := &fakeWriter{script: Script{Title: "T"}}
	episodes := testEpisodes(writer)

	_, _, err := episodes.GenerateScript(CastProfile{Name: "Test", UserPrompt: "Be brief."}, twoItems())

	require.NoError(t, err)
	assert.Equal(t, "Be brief.", writer.style)
	assert.Equal(t, twoItems(), writer.contents)
	assert.Contains(t, writer.system, "German", "the rules are rendered for the language")
	assert.NotContains(t, writer.system, "{", "no placeholder is left in the rules")
}

func TestGenerateScriptUsesTheDefaultStyleWhenTheProfileHasNone(t *testing.T) {
	writer := &fakeWriter{script: Script{Title: "T"}}
	episodes := testEpisodes(writer)

	_, _, err := episodes.GenerateScript(CastProfile{Name: "Test"}, twoItems())

	require.NoError(t, err)
	assert.NotEmpty(t, writer.style)
	assert.Equal(t, defaultUserPrompt, writer.style)
}

func TestSourceNumbersOutsideTheContentItemsAreDropped(t *testing.T) {
	written := Script{Title: "T", Sections: []Section{
		{Heading: "Topic", Body: "Text", Sources: []int{0, 2, 3, 1, -1}},
		{Heading: "Closing", Body: "Bye"},
	}}
	episodes := testEpisodes(&fakeWriter{script: written})

	script, _, err := episodes.GenerateScript(CastProfile{Name: "Test"}, twoItems())

	require.NoError(t, err, "a wrong source number must not cost the whole Episode")
	assert.Equal(t, []int{2, 1}, script.Sections[0].Sources, "only the two existing items remain, in the written order")
	assert.Empty(t, script.Sections[1].Sources)
}
