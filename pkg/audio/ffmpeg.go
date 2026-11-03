// Package audio converts raw PCM audio (as returned by Gemini's native TTS)
// into the OGG/Opus format Episodes are published in.
//
// The conversion shells out to the ffmpeg binary on PATH.
package audio

import (
	"bytes"
	"fmt"
	"log/slog"
	"os/exec"
)

// Converter turns raw signed 16-bit little-endian PCM audio into an encoded
// audio format.
type Converter interface {
	// PCMToOGG encodes raw signed 16-bit little-endian PCM audio as OGG/Opus.
	PCMToOGG(pcm []byte, sampleRate, channels int) ([]byte, error)
}

// FFmpegConverter implements Converter by shelling out to an ffmpeg binary.
type FFmpegConverter struct {
	binPath string
}

// NewFFmpegConverter creates a Converter backed by the "ffmpeg" binary found on PATH.
func NewFFmpegConverter() *FFmpegConverter {
	return &FFmpegConverter{binPath: "ffmpeg"}
}

// Available reports whether ffmpeg can be found on PATH. Call this once at
// application startup to fail fast (or warn clearly) if audio conversion
// won't be possible at runtime.
func Available() bool {
	_, err := exec.LookPath("ffmpeg")
	return err == nil
}

// PCMToOGG converts raw signed 16-bit little-endian PCM audio to OGG/Opus by
// piping it through ffmpeg.
func (f *FFmpegConverter) PCMToOGG(pcm []byte, sampleRate, channels int) ([]byte, error) {
	slog.Debug("Converting PCM to OGG via ffmpeg", "bytes", len(pcm), "sampleRate", sampleRate, "channels", channels)

	cmd := exec.Command(f.binPath,
		"-hide_banner", "-loglevel", "error",
		"-f", "s16le",
		"-ar", fmt.Sprintf("%d", sampleRate),
		"-ac", fmt.Sprintf("%d", channels),
		"-i", "pipe:0",
		"-c:a", "libopus",
		"-f", "ogg",
		"pipe:1",
	)
	cmd.Stdin = bytes.NewReader(pcm)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		slog.Error("ffmpeg conversion failed", "error", err, "stderr", stderr.String())
		return nil, fmt.Errorf("ffmpeg conversion failed: %w: %s", err, stderr.String())
	}

	slog.Debug("PCM to OGG conversion succeeded", "outputBytes", stdout.Len())

	return stdout.Bytes(), nil
}
