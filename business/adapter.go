package business

import (
	"errors"
	"io"
	"time"
)

// LLM turns a prompt into text and reports what that consumed (UsageScript), also together with an error.
type LLM interface {
	SinglePrompt(prompt string) (string, Usage, error)
}

// ScriptWriter writes the Script of an Episode: a title and Sections with heading, spoken text and the numbers of
// the Content items each Section is based on. How it gets there is up to the implementation. One that wraps a
// text-only LLM has to ask for a layout it can read back and does the reading; another might use a model's
// structured output instead.
type ScriptWriter interface {
	// WriteScript writes the Script from the built-in rules for its content (language, word budget, date, writing
	// for the ear), the style guide of the CastProfile and the Contents it is based on. The numbers in
	// Section.Sources count from 1 in contents. The layout of the answer is the ScriptWriter's business. The Usage
	// is what writing consumed, also when it fails.
	WriteScript(system, style string, contents []Content) (Script, Usage, error)
}

// ErrTransient marks an error of a step that may go away when the step is tried again, such as a rate limit or a
// server error of a speech service. An adapter wraps its error with it; the production retries such a step.
var ErrTransient = errors.New("transient error")

// ErrEpisodeNotFound is returned by an EpisodeRepository when the User has no Episode with the given ID.
var ErrEpisodeNotFound = errors.New("episode not found")

// ListOpts narrows the Episodes returned by an EpisodeRepository.
type ListOpts struct {
	// Limit is the maximum number of Episodes returned; zero means no limit.
	Limit int
	// BeforeID excludes Episodes that are not older than the Episode with this ID; zero means no bound. Passing the
	// ID of the last Episode of a list returns the next older ones.
	BeforeID int64
	// CastProfileID leaves out the Episodes of other CastProfiles; empty means all.
	CastProfileID string
}

// EpisodeRepository persists produced Episodes. An Episode always belongs to a User; another User's Episodes are
// invisible to all operations.
type EpisodeRepository interface {
	// SaveFailed stores an Episode whose production failed, with the reason and without audio, and returns it with its
	// ID set. Of the Script only the title is kept; no Content counts as used.
	SaveFailed(episode Episode, failure string) (Episode, error)
	// Save stores the Episode together with its single audio file and the identities of the Content it was made
	// from, and returns it with its ID and AudioLocation set. The AudioLocation of the provided Episode is ignored;
	// the repository hands the audio to its AudioStore.
	Save(episode Episode, audio []byte) (Episode, error)
	// List returns the Episodes of the User, most recent first, with their AudioLocation set.
	List(userID string, opts ListOpts) ([]Episode, error)
	// Get returns one Episode of the User, or ErrEpisodeNotFound.
	Get(userID string, id int64) (Episode, error)
	// Delete removes the Episode of the User together with its audio file, or returns ErrEpisodeNotFound.
	Delete(userID string, id int64) error
	// DeleteBefore removes every Episode of every User that was created before the time, ready or failed, together
	// with its audio, and returns how many it removed.
	DeleteBefore(t time.Time) (int, error)
	// UsedContent returns the identities of the Content that went into Episodes of the CastProfile (Episode.UsedContent).
	// An Episode that is deleted takes its Content with it.
	UsedContent(userID, castProfileID string) ([]string, error)
}

// AudioReader is audio opened for reading. It can be read, seeked and must be closed.
type AudioReader interface {
	io.ReadSeeker
	io.Closer
}

// AudioStore keeps the bytes of audio under a key and hands out a location that only it understands.
type AudioStore interface {
	// Store keeps the audio under the key, a file-name-safe string that carries its own extension, and returns the
	// opaque location to open or delete it later.
	Store(key string, audio []byte) (location string, err error)
	// Open opens the audio at a location returned by Store, together with the time it was last modified. The
	// caller closes it.
	Open(location string) (AudioReader, time.Time, error)
	// Delete removes the audio at a location returned by Store. Audio that is already gone is not an error.
	Delete(location string) error
}

// Page is a web page as a Crawler retrieved it.
type Page struct {
	Title string
	// Text is the readable text of the page, as Markdown.
	Text string
	// RetrievedAt is when the page was retrieved from its origin; the zero time when that is not known. A page that a
	// CachingCrawler serves from its cache keeps the time of the retrieval that filled the cache.
	RetrievedAt time.Time
}

// Crawler retrieves the readable text of a web page.
type Crawler interface {
	// Crawl retrieves the page at the address. The Usage is what retrieving consumed (UsageCrawl), also when it fails.
	Crawl(url string) (Page, Usage, error)
}

// Speech is spoken audio as raw PCM: signed 16-bit little-endian samples, one channel, no header.
type Speech struct {
	PCM        []byte
	SampleRate int
}

// Duration is the length of the speech.
func (s Speech) Duration() time.Duration {
	return pcmDuration(len(s.PCM), s.SampleRate)
}

// TTS turns text into speech.
type TTS interface {
	// Synthesize speaks the text in the Voice. The Usage is what speaking consumed (UsageSpeech), also when it fails.
	Synthesize(text string, voice Voice) (Speech, Usage, error)
}

// AudioEncoder turns the speech of an Episode into the audio file that is kept and played.
type AudioEncoder interface {
	// Encode returns the speech as OGG/Opus audio with the Chapters written into it, where ordinary players show them.
	Encode(speech Speech, chapters []Chapter) ([]byte, error)
}
