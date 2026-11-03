package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alex0ptr/aigude/pkg/audio"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServeRefusesToStartWithoutFfmpeg(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	cmd := newRootCommand()
	cmd.SetArgs([]string{"serve"})

	err := cmd.Execute()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "ffmpeg")
	assert.Contains(t, err.Error(), "PATH")
}

func TestServeNeedsTheFirecrawlKeyForTheHostedAddress(t *testing.T) {
	tests := map[string][]string{
		"by default":                   {"serve"},
		"however the address is spelt": {"serve", "--firecrawl-url", "https://API.firecrawl.dev/"},
	}
	for name, args := range tests {
		t.Run(name, func(t *testing.T) {
			if !audio.Available() {
				t.Skip("ffmpeg not found on PATH")
			}
			t.Chdir(t.TempDir())
			t.Setenv("GEMINI_API_KEY", "a-gemini-key")
			t.Setenv("FIRECRAWL_API_KEY", "")
			cmd := newRootCommand()
			cmd.SetArgs(args)

			err := cmd.Execute()

			require.Error(t, err)
			assert.Contains(t, err.Error(), "FIRECRAWL_API_KEY")
		})
	}
}

func TestServeNeedsNoFirecrawlKeyForAnotherAddress(t *testing.T) {
	if !audio.Available() {
		t.Skip("ffmpeg not found on PATH")
	}
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("GEMINI_API_KEY", "a-gemini-key")
	t.Setenv("FIRECRAWL_API_KEY", "")
	blocker := filepath.Join(dir, "blocker")
	require.NoError(t, os.WriteFile(blocker, nil, 0o600))
	cmd := newRootCommand()
	cmd.SetArgs([]string{"serve", "--firecrawl-url", "http://localhost:3002", "--data-dir", filepath.Join(blocker, "data")})

	err := cmd.Execute()

	require.Error(t, err, "the data folder cannot be made, which ends the start after the keys were checked")
	assert.Contains(t, err.Error(), "data folder")
	assert.NotContains(t, err.Error(), "FIRECRAWL_API_KEY")
}
