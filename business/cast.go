package business

import (
	_ "embed"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

//go:embed prompt/system.txt
var systemPrompt string

//go:embed prompt/default.txt
var defaultUserPrompt string

// renderSystemPrompt fills the placeholders of the built-in system prompt with the episode language,
// the word budget and today's date.
func renderSystemPrompt(template, language string, wordBudget int, now time.Time) string {
	slog.Debug("Rendering system prompt", "language", language, "wordBudget", wordBudget, "date", now.Format(time.DateOnly))
	return strings.NewReplacer(
		"{language}", language,
		"{word_budget}", fmt.Sprint(wordBudget),
		"{date}", now.Format(time.DateOnly),
	).Replace(template)
}

// Script is the text of an Episode as written by the language model: a title and an ordered list of Sections.
type Script struct {
	Title    string
	Sections []Section
}

// Section is one thematic part of a Script, with a heading and the spoken text.
type Section struct {
	Heading string
	Body    string
	// Sources are the numbers (counting from 1, as in the contents given to ScriptWriter.WriteScript) of the Content items the writer says
	// it used for this Section. The introduction and the closing have none.
	Sources []int
}

// Chapter marks where a Section begins in the audio of an Episode, titled with the Section's heading.
type Chapter struct {
	Title string
	// Start is the offset from the beginning of the Episode's audio file.
	Start time.Duration
}

// Episode is one produced unit of listening: a single continuous audio file together with its Script and Chapters.
// It belongs to the User and CastProfile it was produced for, which it refers to by Id; their Display Names are
// looked up in the User Config when they are shown.
type Episode struct {
	// ID identifies the Episode; it is assigned by the EpisodeRepository.
	ID int64
	// Date is the creation time of the Episode.
	Date time.Time

	UserID        string
	CastProfileID string

	Script   Script
	Chapters []Chapter
	// Duration is the length of the audio file.
	Duration time.Duration
	// AudioLocation is where the single audio file of the Episode is kept. It is opaque: only the AudioStore that
	// issued it understands it. It is assigned by the EpisodeRepository and empty while the Episode has no audio.
	AudioLocation string
	// Failure says why producing the Episode failed; empty unless it did.
	Failure string
	// Sources are the Content items of the prompt in the order of the prompt, kept with a ready Episode so that they stay
	// readable when the feeds or the crawl cache change.
	Sources []Source
	// Usage is what producing the Episode consumed; a failed Episode keeps what had been consumed up to its failure.
	Usage Usage
	// UsedContent are the identities (Content.Identity) of all Content in the prompt of the Episode. The
	// EpisodeRepository keeps them for a ready Episode, so that the same Content is not used again.
	UsedContent []string
}

// Source is a Content item that went into the prompt of an Episode, kept as the language model saw it. The Section
// numbers of Section.Sources count in Episode.Sources from 1.
type Source struct {
	Title string
	// Link is the address of the original; empty when there is none.
	Link string
	// Name is where the item came from, such as the title of the feed.
	Name string
	// Text is the text handed to the language model, as Markdown.
	Text string
	// Retrieved is when the text was retrieved from its origin, or when the Episode read the item if that is not known.
	Retrieved time.Time
}

// ErrNoNewContent is returned by Create when the window holds no Content that no Episode of the CastProfile has used.
// No Episode is made then.
var ErrNoNewContent = errors.New("no unused content in the window")

// Episodes provides the central usecases for producing Episodes from the Subscriptions of a CastProfile.
type Episodes struct {
	writer     ScriptWriter
	tts        TTS
	encoder    AudioEncoder
	repository EpisodeRepository
	clock      Clock
	location   *time.Location
	voice      Voice
	wordBudget int
	charLimit  int
	pause      time.Duration
}

// NewEpisodes creates the usecases of producing Episodes. The writer writes the Script, tts speaks its Sections and
// the encoder turns the joined speech into the audio file with the Chapters in it, which the repository keeps. The
// clock tells today's date, taken in the location, for the prompt and the creation time of Episodes. The voice is the one a CastProfile without its own is spoken in, wordBudget the word budget handed to the
// language model, contentCharLimit the number of characters of one Content item that go into the prompt, and
// sectionPause the silence between two Sections.
func NewEpisodes(writer ScriptWriter, tts TTS, encoder AudioEncoder, repository EpisodeRepository, clock Clock,
	location *time.Location, voice Voice, wordBudget, contentCharLimit int, sectionPause time.Duration) *Episodes {
	return &Episodes{
		writer: writer, tts: tts, encoder: encoder, repository: repository, clock: clock, location: location,
		voice: voice, wordBudget: wordBudget, charLimit: contentCharLimit, pause: sectionPause,
	}
}

// Create produces a new Episode for the User and the CastProfile and stores it. When any step fails, no audio is
// kept; the Episode is stored as failed with the reason, and the error is returned.
func (e *Episodes) Create(user User, profile CastProfile) (Episode, error) {
	now := e.clock.Now()
	slog.Info("Creating episode", "user", user.ID, "profile", profile.ID, "maxLookback", profile.MaxLookback)
	episode := Episode{
		Date: now, UserID: user.ID, CastProfileID: profile.ID,
	}

	saved, err := e.build(episode, profile, now)
	if errors.Is(err, ErrNoNewContent) {
		slog.Info("No unused Content in the window, no Episode is made", "user", user.ID, "profile", profile.ID,
			"since", now.Add(-profile.MaxLookback))
		return Episode{}, err
	}
	if err != nil {
		slog.Error("Producing the episode failed", "user", user.ID, "profile", profile.ID, "error", err)
		slog.Info("Usage of the failed episode", "user", user.ID, "profile", profile.ID, "usage", saved.Usage)
		if _, saveErr := e.repository.SaveFailed(saved, err.Error()); saveErr != nil {
			slog.Error("Recording the failed episode failed", "user", user.ID, "profile", profile.ID, "error", saveErr)
		}
		return Episode{}, err
	}
	slog.Info("Episode ready", "user", user.ID, "id", saved.ID, "duration", saved.Duration, "usage", saved.Usage)
	return saved, nil
}

// build runs the steps of producing the Episode. On failure it returns what is known of the Episode so far.
func (e *Episodes) build(episode Episode, profile CastProfile, now time.Time) (Episode, error) {
	slog.Debug("Collecting the content", "profile", profile.ID)
	contents, usage, err := e.collect(episode.UserID, profile, now)
	episode.Usage = episode.Usage.Add(usage)
	if err != nil {
		return episode, err
	}
	slog.Debug("Noting the sources of the prompt", "profile", profile.ID, "items", len(contents))
	for _, content := range contents {
		if content.Identity != "" {
			episode.UsedContent = append(episode.UsedContent, content.Identity)
		}
		retrieved := content.Retrieved
		if retrieved.IsZero() {
			retrieved = now
		}
		episode.Sources = append(episode.Sources, Source{
			Title: content.Title, Link: content.Link, Name: content.Source, Text: content.Body, Retrieved: retrieved,
		})
	}

	script, usage, err := e.GenerateScript(profile, contents)
	episode.Usage = episode.Usage.Add(usage)
	if err != nil {
		return episode, err
	}
	episode.Script = script

	slog.Debug("Speaking the script", "profile", profile.ID, "sections", len(script.Sections))
	joined, usage, err := e.speak(script, profile)
	episode.Usage = episode.Usage.Add(usage)
	if err != nil {
		return episode, err
	}
	var chapters []Chapter
	for i, section := range script.Sections {
		chapters = append(chapters, Chapter{Title: section.Heading, Start: joined.sectionStarts[i]})
	}
	slog.Debug("Encoding the speech with its chapters", "duration", joined.speech.Duration(), "chapters", len(chapters))
	audio, err := e.encoder.Encode(joined.speech, chapters)
	if err != nil {
		return episode, fmt.Errorf("encode audio: %w", err)
	}
	episode.Duration = joined.speech.Duration()
	episode.Chapters = chapters

	slog.Debug("Saving the episode", "profile", profile.ID, "audioBytes", len(audio))
	return e.repository.Save(episode, audio)
}

// warnWhenOffBudget logs a warning when the spoken words of the Script are more than a tenth above or below the Word
// Budget. The Script is kept as it is.
func warnWhenOffBudget(script Script, profileID string, wordBudget int) {
	words := 0
	for _, section := range script.Sections {
		words += len(strings.Fields(section.Body))
	}
	slog.Debug("Counted the words of the script", "profile", profileID, "words", words, "budget", wordBudget)
	switch {
	case (words-wordBudget)*10 > wordBudget:
		slog.Warn("Script is longer than its word budget", "profile", profileID, "words", words, "budget", wordBudget)
	case (wordBudget-words)*10 > wordBudget:
		slog.Warn("Script is shorter than its word budget", "profile", profileID, "words", words, "budget", wordBudget)
	}
}

// collect gathers the Content of all Subscriptions of the CastProfile published within its Max Lookback up to now,
// leaving out what Episodes of the CastProfile have used. What an earlier Subscription of the CastProfile delivered
// is left out of the later ones, before anything is spent on it. It returns ErrNoNewContent when nothing is left.
func (e *Episodes) collect(userID string, profile CastProfile, now time.Time) ([]Content, Usage, error) {
	identities, err := e.repository.UsedContent(userID, profile.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("look up used content: %w", err)
	}
	used := make(map[string]bool, len(identities))
	for _, identity := range identities {
		used[identity] = true
	}
	slog.Debug("Content used by earlier Episodes", "profile", profile.ID, "items", len(used))
	delivered := make(map[string]bool)
	since, until := now.Add(-profile.MaxLookback), now
	opts := ContentFetchOpts{Skip: func(identity string) bool { return used[identity] || delivered[identity] }}
	var contents []Content
	var usage Usage
	for _, subscription := range profile.Subscriptions {
		slog.Info("Fetching content from repository", "profile", profile.ID, "since", since, "until", until)
		fetched, fetchedUsage, err := subscription.Repository.Content(since, until, opts)
		usage = usage.Add(fetchedUsage)
		if err != nil {
			return nil, usage, fmt.Errorf("fetch content: %w", err)
		}
		for _, content := range fetched {
			if content.Identity != "" && delivered[content.Identity] {
				slog.Debug("Leaving out an item that was delivered already", "profile", profile.ID, "title", content.Title)
				continue
			}
			if content.Identity != "" {
				delivered[content.Identity] = true
			}
			contents = append(contents, content)
		}
	}
	if len(contents) == 0 {
		return nil, usage, ErrNoNewContent
	}
	for i := range contents {
		contents[i].Body = CutText(contents[i].Body, e.charLimit)
	}
	slog.Debug("Collected content", "profile", profile.ID, "items", len(contents))
	return contents, usage, nil
}

// GenerateScript generates the Script of an Episode based on the provided CastProfile and contents, together with what
// writing it consumed.
func (e *Episodes) GenerateScript(profile CastProfile, contents []Content) (Script, Usage, error) {
	slog.Info("Generating script", "profile", profile.ID, "contentCount", len(contents))

	userPrompt := profile.UserPrompt
	if userPrompt == "" {
		slog.Debug("No user prompt provided, using default prompt")
		userPrompt = defaultUserPrompt
	}

	wordBudget := e.wordBudget
	if profile.WordBudget > 0 {
		wordBudget = profile.WordBudget
	}
	system := renderSystemPrompt(systemPrompt, languageName(profile.Voice.Or(e.voice).Language), wordBudget, e.clock.Now().In(e.location))
	slog.Debug("Asking the script writer", "systemLength", len(system), "styleLength", len(userPrompt))
	script, usage, err := e.writer.WriteScript(system, userPrompt, contents)
	if err != nil {
		return Script{}, usage, fmt.Errorf("failed to generate script: %w", err)
	}

	script = dropUnknownSources(script, len(contents))
	warnWhenOffBudget(script, profile.ID, wordBudget)

	slog.Info("Script generated successfully", "title", script.Title, "sectionCount", len(script.Sections))

	return script, usage, nil
}

// dropUnknownSources returns the Script without the source numbers outside 1 to itemCount, the number of Content
// items in the prompt, and warns about each; the Episode goes on.
func dropUnknownSources(script Script, itemCount int) Script {
	sections := make([]Section, len(script.Sections))
	for i, section := range script.Sections {
		var valid []int
		for _, n := range section.Sources {
			if n < 1 || n > itemCount {
				slog.Warn("Dropping source number of unknown Content item", "section", section.Heading, "source", n, "items", itemCount)
				continue
			}
			valid = append(valid, n)
		}
		section.Sources = valid
		sections[i] = section
	}
	script.Sections = sections
	return script
}
