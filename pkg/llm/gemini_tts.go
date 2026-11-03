package llm

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/alex0ptr/aigude/business"
	"google.golang.org/genai"
)

// DefaultGeminiTTSModel is the stable Gemini 3.8 Flash text-to-speech model.
const DefaultGeminiTTSModel = "gemini-3.8-flash-tts"

// defaultPCMSampleRate is the rate assumed when Gemini's response carries none.
const defaultPCMSampleRate = 24000

// GeminiTTS implements business.TTS using Gemini's native audio output. Gemini's generateContent API answers an
// AUDIO response modality with 16-bit mono audio, 24kHz, wrapped in a WAV header. GeminiTTS hands out the bare PCM
// (the encoding to OGG lives in pkg/audio).
//
// Example usage:
//
//	tts, err := llm.NewGeminiTTS(apiKey, llm.DefaultGeminiTTSModel)
//	speech, err := tts.Synthesize("Hallo, willkommen zur Folge!", business.Voice{Language: "de-DE", Name: "Achernar"})
type GeminiTTS struct {
	client *genai.Client
	ctx    context.Context
	model  string
}

var _ business.TTS = (*GeminiTTS)(nil)

// NewGeminiTTS creates a new Gemini TTS client.
// apiKey is the Google AI API key.
// model is the TTS model to use, e.g. DefaultGeminiTTSModel.
func NewGeminiTTS(apiKey, model string) (*GeminiTTS, error) {
	slog.Debug("Creating Gemini TTS client", "model", model)

	ctx := context.Background()
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		slog.Error("Failed to create Gemini TTS client", "error", err)
		return nil, fmt.Errorf("failed to create Gemini TTS client: %w", err)
	}

	return &GeminiTTS{client: client, ctx: ctx, model: model}, nil
}

// Synthesize speaks the text in one Gemini call. The Voice names a BCP-47 language (e.g. "de-DE") and one of
// Gemini's prebuilt voices (e.g. "Achernar"). The model takes the text as a verbatim transcript, so nothing but what
// is to be spoken goes into it.
func (g *GeminiTTS) Synthesize(text string, voice business.Voice) (business.Speech, business.Usage, error) {
	slog.Debug("Synthesizing speech", "textLength", len(text), "language", voice.Language, "voice", voice.Name)

	resp, err := g.client.Models.GenerateContent(g.ctx, g.model, genai.Text(text), &genai.GenerateContentConfig{
		ResponseModalities: []string{"AUDIO"},
		SpeechConfig: &genai.SpeechConfig{
			LanguageCode: voice.Language,
			VoiceConfig: &genai.VoiceConfig{
				PrebuiltVoiceConfig: &genai.PrebuiltVoiceConfig{
					VoiceName: voice.Name,
				},
			},
		},
	})
	if err != nil {
		slog.Error("Failed to call Gemini TTS API", "error", err)
		return business.Speech{}, nil, fmt.Errorf("failed to synthesize speech: %w", classify(err))
	}

	usage := speechUsage(resp)
	pcm, sampleRate, err := speechFromResponse(resp)
	if err != nil {
		return business.Speech{}, usage, err
	}
	slog.Debug("Received speech from Gemini", "bytes", len(pcm), "sampleRate", sampleRate)
	return business.Speech{PCM: pcm, SampleRate: sampleRate}, usage, nil
}

// speechFromResponse pulls the bare PCM and its sample rate out of the first inline audio of the response. Audio
// wrapped in a WAV header is unwrapped, raw PCM is taken as it is.
func speechFromResponse(resp *genai.GenerateContentResponse) ([]byte, int, error) {
	data, mimeType, err := extractAudio(resp)
	if err != nil {
		return nil, 0, err
	}
	if pcm, rate, ok := unwrapWAV(data); ok {
		slog.Debug("Unwrapped WAV audio", "mimeType", mimeType, "sampleRate", rate)
		return pcm, rate, nil
	}
	return data, parsePCMSampleRate(mimeType), nil
}

// unwrapWAV returns the samples and the sample rate of a WAV file; ok is false when data is not one.
func unwrapWAV(data []byte) (pcm []byte, sampleRate int, ok bool) {
	if len(data) < 12 || !bytes.Equal(data[0:4], []byte("RIFF")) || !bytes.Equal(data[8:12], []byte("WAVE")) {
		return nil, 0, false
	}
	for pos := 12; pos+8 <= len(data); {
		id, size := string(data[pos:pos+4]), int(binary.LittleEndian.Uint32(data[pos+4:]))
		body := pos + 8
		slog.Debug("Reading WAV chunk", "chunk", id, "size", size)
		switch id {
		case "fmt ":
			if body+8 <= len(data) {
				sampleRate = int(binary.LittleEndian.Uint32(data[body+4:]))
			}
		case "data":
			end := min(body+size, len(data))
			return data[body:end], sampleRate, sampleRate > 0
		}
		pos = body + size + size%2
	}
	slog.Warn("WAV audio has no usable data chunk", "bytes", len(data))
	return nil, 0, false
}

// extractAudio pulls the raw audio bytes and MIME type out of the first
// candidate's first inline-data part.
func extractAudio(resp *genai.GenerateContentResponse) ([]byte, string, error) {
	if len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil {
		return nil, "", fmt.Errorf("no candidates returned")
	}
	for _, part := range resp.Candidates[0].Content.Parts {
		if part.InlineData != nil && len(part.InlineData.Data) > 0 {
			return part.InlineData.Data, part.InlineData.MIMEType, nil
		}
	}
	return nil, "", fmt.Errorf("no audio data in response")
}

// parsePCMSampleRate extracts the sample rate from a MIME type like
// "audio/pcm;rate=24000", falling back to defaultPCMSampleRate.
func parsePCMSampleRate(mimeType string) int {
	for _, part := range strings.Split(mimeType, ";") {
		if rate, ok := strings.CutPrefix(strings.TrimSpace(part), "rate="); ok {
			if n, err := strconv.Atoi(rate); err == nil {
				return n
			}
		}
	}
	return defaultPCMSampleRate
}
