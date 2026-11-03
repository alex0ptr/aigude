package oggchapters_test

import (
	"testing"
	"time"

	"github.com/alex0ptr/aigude/business"
	"github.com/alex0ptr/aigude/pkg/audio"
	"github.com/alex0ptr/aigude/pkg/oggchapters"
	"github.com/alex0ptr/aigude/pkg/service/servicetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// opus is OGG/Opus audio of the given length, as the service encodes it.
func opus(t *testing.T, seconds int) []byte {
	t.Helper()
	const rate = 24000
	ogg, err := audio.NewFFmpegConverter().PCMToOGG(make([]byte, seconds*rate*2), rate, 1)
	require.NoError(t, err)
	return ogg
}

func TestTheChaptersCanBeReadBackFromTheFile(t *testing.T) {
	chapters := []business.Chapter{
		{Title: "Intro", Start: 0},
		{Title: "Wasserstoffzug fährt wieder", Start: 3*time.Second + 250*time.Millisecond},
		{Title: "Schluss", Start: 7*time.Second + 999*time.Millisecond},
	}

	tagged, err := oggchapters.New().TagChapters(opus(t, 9), chapters)

	require.NoError(t, err)
	assert.Equal(t, chapters, servicetest.OggChapters(t, tagged))
}

func TestTheTaggedFileStillPlaysAndKeepsItsLength(t *testing.T) {
	plain := opus(t, 9)

	tagged, err := oggchapters.New().TagChapters(plain, []business.Chapter{{Title: "Intro"}, {Title: "Thema", Start: 4 * time.Second}})

	require.NoError(t, err)
	assert.True(t, servicetest.OggPlays(t, tagged), "ffmpeg decodes the whole file without an error")
	assert.Equal(t, servicetest.OggDuration(t, plain), servicetest.OggDuration(t, tagged))
}

func TestManyLongChaptersSpreadOverSeveralPagesAndStillPlay(t *testing.T) {
	var chapters []business.Chapter
	for i := range 700 {
		chapters = append(chapters, business.Chapter{Title: "Ein ziemlich langer Kapitelname mit Umlauten: Übergrößen für die Prüfung", Start: time.Duration(i) * 10 * time.Millisecond})
	}

	tagged, err := oggchapters.New().TagChapters(opus(t, 9), chapters)

	require.NoError(t, err)
	assert.Len(t, servicetest.OggChapters(t, tagged), 700)
	assert.True(t, servicetest.OggPlays(t, tagged))
}

func TestAFileThatIsNoOggOpusIsRefused(t *testing.T) {
	_, err := oggchapters.New().TagChapters([]byte("this is not audio"), []business.Chapter{{Title: "Intro"}})

	assert.Error(t, err)
}

func TestTheEncoderReturnsAudioThatCarriesTheChapters(t *testing.T) {
	const rate = 24000
	speech := business.Speech{PCM: make([]byte, 6*rate*2), SampleRate: rate}
	chapters := []business.Chapter{{Title: "Intro"}, {Title: "Thema", Start: 3 * time.Second}}

	ogg, err := oggchapters.NewEncoder(audio.NewFFmpegConverter()).Encode(speech, chapters)

	require.NoError(t, err)
	assert.Equal(t, chapters, servicetest.OggChapters(t, ogg))
}
