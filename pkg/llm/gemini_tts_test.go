package llm

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

// wavOf wraps 16-bit mono PCM in a WAV file with the canonical 44 byte header.
func wavOf(pcm []byte, sampleRate int) []byte {
	header := make([]byte, 44)
	copy(header[0:], "RIFF")
	binary.LittleEndian.PutUint32(header[4:], uint32(36+len(pcm)))
	copy(header[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(header[16:], 16)
	binary.LittleEndian.PutUint16(header[20:], 1) // PCM
	binary.LittleEndian.PutUint16(header[22:], 1) // mono
	binary.LittleEndian.PutUint32(header[24:], uint32(sampleRate))
	binary.LittleEndian.PutUint32(header[28:], uint32(sampleRate*2))
	binary.LittleEndian.PutUint16(header[32:], 2)
	binary.LittleEndian.PutUint16(header[34:], 16)
	copy(header[36:], "data")
	binary.LittleEndian.PutUint32(header[40:], uint32(len(pcm)))
	return append(header, pcm...)
}

func responseWith(mimeType string, data []byte) *genai.GenerateContentResponse {
	return &genai.GenerateContentResponse{Candidates: []*genai.Candidate{{
		Content: &genai.Content{Parts: []*genai.Part{{InlineData: &genai.Blob{MIMEType: mimeType, Data: data}}}},
	}}}
}

func TestSpeechFromResponseDropsTheWavHeader(t *testing.T) {
	samples := []byte{1, 0, 2, 0, 3, 0, 4, 0}

	pcm, rate, err := speechFromResponse(responseWith("audio/wav", wavOf(samples, 24000)))

	require.NoError(t, err)
	assert.Equal(t, samples, pcm, "the header is not audio")
	assert.Equal(t, 24000, rate)
}

func TestSpeechFromResponseTakesTheRateFromTheWavHeader(t *testing.T) {
	_, rate, err := speechFromResponse(responseWith("audio/wav", wavOf([]byte{1, 0}, 16000)))

	require.NoError(t, err)
	assert.Equal(t, 16000, rate)
}

func TestSpeechFromResponseKeepsRawPCM(t *testing.T) {
	samples := []byte{1, 0, 2, 0}

	pcm, rate, err := speechFromResponse(responseWith("audio/L16;codec=pcm;rate=22050", samples))

	require.NoError(t, err)
	assert.Equal(t, samples, pcm)
	assert.Equal(t, 22050, rate)
}

func TestSpeechFromResponseFailsWithoutAudio(t *testing.T) {
	_, _, err := speechFromResponse(&genai.GenerateContentResponse{})

	require.Error(t, err)
}
