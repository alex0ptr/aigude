package business

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testSampleRate = 1000

// secondsOfSpeech is Speech of the given length.
func secondsOfSpeech(seconds int) Speech {
	return Speech{PCM: make([]byte, seconds*testSampleRate*bytesPerSample), SampleRate: testSampleRate}
}

// lengthTTS speaks a text for as many seconds as its first character says ("3" is three seconds) and fails on the
// text in failOn.
type lengthTTS struct {
	failOn string
	// usage is what every call reports.
	usage Usage
}

func (l lengthTTS) Synthesize(text string, _ Voice) (Speech, Usage, error) {
	if text == l.failOn {
		return Speech{}, l.usage, errors.New("speech unavailable")
	}
	return secondsOfSpeech(int(text[0] - '0')), l.usage, nil
}

// recordingEncoder returns the speech it is given, so a test can see what was joined.
type recordingEncoder struct {
	speech   Speech
	chapters []Chapter
	err      error
}

func (r *recordingEncoder) Encode(speech Speech, chapters []Chapter) ([]byte, error) {
	r.speech, r.chapters = speech, chapters
	if r.err != nil {
		return nil, r.err
	}
	return []byte("ogg"), nil
}

// recordingRepository remembers what was saved.
type recordingRepository struct {
	EpisodeRepository
	saved, failed []Episode
	failures      []string
}

func (r *recordingRepository) Save(e Episode, _ []byte) (Episode, error) {
	r.saved = append(r.saved, e)
	return e, nil
}

func (r *recordingRepository) UsedContent(string, string) ([]string, error) { return nil, nil }

func (r *recordingRepository) SaveFailed(e Episode, failure string) (Episode, error) {
	r.failed = append(r.failed, e)
	r.failures = append(r.failures, failure)
	return e, nil
}

// oneItem is a ContentRepository with one fresh item.
type oneItem struct{ usage Usage }

func (o oneItem) Content(time.Time, time.Time, ContentFetchOpts) ([]Content, Usage, error) {
	return []Content{{Title: "item"}}, o.usage, nil
}

func producing(tts TTS, sections ...string) (*Episodes, *recordingEncoder, *recordingRepository, CastProfile) {
	script := Script{Title: "T"}
	for _, s := range sections {
		script.Sections = append(script.Sections, Section{Heading: "H" + s, Body: s})
	}
	encoder, repository := &recordingEncoder{}, &recordingRepository{}
	episodes := testEpisodesWith(&fakeWriter{script: script}, tts, encoder, repository, 500*time.Millisecond)
	profile := CastProfile{ID: "p", Name: "P", Subscriptions: []Subscription{{Repository: oneItem{}}}}
	return episodes, encoder, repository, profile
}

func TestSectionsAreJoinedWithPausesAndChaptersMarkTheirStarts(t *testing.T) {
	episodes, encoder, repository, profile := producing(lengthTTS{}, "1", "3", "2")

	_, err := episodes.Create(User{ID: "alex"}, profile)

	require.NoError(t, err)
	require.Len(t, repository.saved, 1)
	saved := repository.saved[0]
	// Sections of 1, 3 and 2 seconds with a pause of half a second in between.
	assert.Equal(t, []Chapter{{Title: "H1", Start: 0}, {Title: "H3", Start: 1500 * time.Millisecond}, {Title: "H2", Start: 5 * time.Second}}, saved.Chapters)
	assert.Equal(t, 7*time.Second, saved.Duration)
	assert.Equal(t, pcmDuration(len(encoder.speech.PCM), testSampleRate), saved.Duration, "the chapters and the duration describe the audio that was encoded")
	assert.Equal(t, saved.Chapters, encoder.chapters, "the encoder writes the chapters of the Episode into the file")
}

func TestOneFailingSectionFailsTheEpisodeAndKeepsNoAudio(t *testing.T) {
	episodes, _, repository, profile := producing(lengthTTS{failOn: "2"}, "1", "2", "3")

	_, err := episodes.Create(User{ID: "alex"}, profile)

	require.ErrorContains(t, err, "speech unavailable")
	assert.Empty(t, repository.saved, "no Episode with audio")
	require.Len(t, repository.failed, 1)
	assert.Contains(t, repository.failures[0], "speech unavailable")
	assert.Equal(t, "alex", repository.failed[0].UserID)
}

func TestAnEncoderThatFailsFailsTheEpisode(t *testing.T) {
	episodes, encoder, repository, profile := producing(lengthTTS{}, "1", "2")
	encoder.err = errors.New("tag chapters: no comment header")

	_, err := episodes.Create(User{ID: "alex"}, profile)

	require.ErrorContains(t, err, "encode audio")
	assert.Empty(t, repository.saved, "no Episode with audio")
	require.Len(t, repository.failed, 1)
}
