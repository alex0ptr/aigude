package business

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// bytesPerSample is the width of one sample of Speech.
const bytesPerSample = 2

// pcmDuration is the length of the given number of PCM bytes at the sample rate.
func pcmDuration(bytes, sampleRate int) time.Duration {
	return time.Duration(bytes/bytesPerSample) * time.Second / time.Duration(sampleRate)
}

// joinedSpeech is the speech of all Sections of a Script as one stretch of audio.
type joinedSpeech struct {
	speech Speech
	// sectionStarts are the offsets at which the Sections begin in speech, one for each Section.
	sectionStarts []time.Duration
}

// speak speaks every Section in its own call and joins the speech with the pause between the Sections. The offset
// of each Section follows exactly from the lengths of what precedes it. The Usage is what speaking consumed, also
// when a call failed.
func (e *Episodes) speak(script Script, profile CastProfile) (joinedSpeech, Usage, error) {
	voice := profile.Voice.Or(e.voice)
	var joined joinedSpeech
	var usage Usage
	for i, section := range script.Sections {
		slog.Debug("Speaking section", "heading", section.Heading, "language", voice.Language, "voice", voice.Name)
		speech, spoken, err := e.synthesize(section.Body, voice)
		usage = usage.Add(spoken)
		if err != nil {
			return joinedSpeech{}, usage, fmt.Errorf("speak section %d (%s): %w", i+1, section.Heading, err)
		}
		if i == 0 {
			joined.speech.SampleRate = speech.SampleRate
		} else {
			if speech.SampleRate != joined.speech.SampleRate {
				return joinedSpeech{}, usage, fmt.Errorf("speak section %d (%s): sample rate %d differs from %d",
					i+1, section.Heading, speech.SampleRate, joined.speech.SampleRate)
			}
			joined.speech.PCM = append(joined.speech.PCM, silence(e.pause, speech.SampleRate)...)
		}
		joined.sectionStarts = append(joined.sectionStarts, joined.speech.Duration())
		joined.speech.PCM = append(joined.speech.PCM, speech.PCM...)
	}
	return joined, usage, nil
}

// speechAttempts is how often a Section is spoken before the Episode fails with a transient error, and speechRetryWait
// the wait after the first failure; every further wait is twice as long as the one before.
const (
	speechAttempts  = 3
	speechRetryWait = time.Second
)

// synthesize speaks the text, trying again after a wait when the speech service fails with a transient error. The
// Usage is what every attempt consumed.
func (e *Episodes) synthesize(text string, voice Voice) (Speech, Usage, error) {
	var usage Usage
	wait := speechRetryWait
	for attempt := 1; ; attempt++ {
		speech, spoken, err := e.tts.Synthesize(text, voice)
		usage = usage.Add(spoken)
		if err == nil || !errors.Is(err, ErrTransient) || attempt == speechAttempts {
			return speech, usage, err
		}
		slog.Warn("Speech failed, trying again", "attempt", attempt, "of", speechAttempts, "wait", wait, "error", err)
		e.clock.Sleep(wait)
		wait *= 2
	}
}

// silence is PCM of the given length that makes no sound.
func silence(d time.Duration, sampleRate int) []byte {
	samples := int(d * time.Duration(sampleRate) / time.Second)
	return make([]byte, samples*bytesPerSample)
}

// languageNames are the English names the prompt uses for the base languages of BCP-47 codes.
var languageNames = map[string]string{
	"de": "German", "en": "English", "fr": "French", "es": "Spanish", "it": "Italian", "nl": "Dutch",
	"pt": "Portuguese", "pl": "Polish", "sv": "Swedish", "da": "Danish", "nb": "Norwegian", "fi": "Finnish",
	"cs": "Czech", "tr": "Turkish", "ja": "Japanese", "ko": "Korean", "zh": "Chinese",
}

// languageName is how the prompt names a language given as a BCP-47 code such as de-DE; a code it does not know is
// used as it is.
func languageName(code string) string {
	base, _, _ := strings.Cut(code, "-")
	if name, ok := languageNames[strings.ToLower(base)]; ok {
		return name
	}
	return code
}
