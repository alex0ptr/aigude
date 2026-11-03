package oggchapters

import (
	"fmt"
	"log/slog"

	"github.com/alex0ptr/aigude/business"
)

// PCMEncoder encodes raw signed 16-bit little-endian PCM as OGG/Opus.
type PCMEncoder interface {
	PCMToOGG(pcm []byte, sampleRate, channels int) ([]byte, error)
}

// Encoder implements business.AudioEncoder: the given PCMEncoder makes the OGG/Opus file, and the Tagger writes the
// Chapters into it.
type Encoder struct {
	pcm PCMEncoder
}

var _ business.AudioEncoder = Encoder{}

// NewEncoder creates an Encoder that lets pcm encode the speech.
func NewEncoder(pcm PCMEncoder) Encoder { return Encoder{pcm: pcm} }

// Encode returns the speech, which is mono, as OGG/Opus audio with the Chapters as chapter tags.
func (e Encoder) Encode(speech business.Speech, chapters []business.Chapter) ([]byte, error) {
	slog.Debug("Encoding the speech", "bytes", len(speech.PCM), "sampleRate", speech.SampleRate)
	ogg, err := e.pcm.PCMToOGG(speech.PCM, speech.SampleRate, 1)
	if err != nil {
		return nil, err
	}
	tagged, err := New().TagChapters(ogg, chapters)
	if err != nil {
		return nil, fmt.Errorf("tag chapters: %w", err)
	}
	return tagged, nil
}
