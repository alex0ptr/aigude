package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeSettingsFile writes a settings file into a temporary folder and returns its path.
func writeSettingsFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "aigude.toml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestFileOverridesDefaults(t *testing.T) {
	path := writeSettingsFile(t, `
listen_addr = "0.0.0.0:9000"
word_budget = 2500
scheduler_max_sleep = "15m"
`)

	got, err := Load(LoadOptions{File: path})
	require.NoError(t, err)

	assert.Equal(t, "0.0.0.0:9000", got.ListenAddr)
	assert.Equal(t, 2500, got.WordBudget)
	assert.Equal(t, 15*time.Minute, got.SchedulerMaxSleep)
	assert.Equal(t, "Achernar", got.DefaultVoice, "settings absent from the file keep their default")
}

func TestEnvironmentOverridesFile(t *testing.T) {
	path := writeSettingsFile(t, `listen_addr = "0.0.0.0:9000"`)
	t.Setenv("AIGUDE_LISTEN_ADDR", "0.0.0.0:9100")
	t.Setenv("AIGUDE_SECTION_PAUSE", "2s")

	got, err := Load(LoadOptions{File: path})
	require.NoError(t, err)

	assert.Equal(t, "0.0.0.0:9100", got.ListenAddr)
	assert.Equal(t, 2*time.Second, got.SectionPause, "environment also sets keys the file does not mention")
}

func TestFlagsOverrideEnvironment(t *testing.T) {
	path := writeSettingsFile(t, `listen_addr = "0.0.0.0:9000"`)
	t.Setenv("AIGUDE_LISTEN_ADDR", "0.0.0.0:9100")
	flags := NewFlagSet()
	require.NoError(t, flags.Parse([]string{"--listen-addr=0.0.0.0:9200"}))

	got, err := Load(LoadOptions{File: path, Flags: flags})
	require.NoError(t, err)

	assert.Equal(t, "0.0.0.0:9200", got.ListenAddr)
}

func TestFlagsThatWereNotGivenDoNotOverrideAnything(t *testing.T) {
	path := writeSettingsFile(t, `word_budget = 2500`)
	flags := NewFlagSet()
	require.NoError(t, flags.Parse(nil))

	got, err := Load(LoadOptions{File: path, Flags: flags})
	require.NoError(t, err)

	assert.Equal(t, 2500, got.WordBudget, "a flag's own default must not beat the file")
}

func TestUnknownKeyInFileStopsLoadingWithAClearMessage(t *testing.T) {
	path := writeSettingsFile(t, `
listen_adr = "0.0.0.0:9000"
word_budget = 2500
extra = true
`)

	_, err := Load(LoadOptions{File: path})

	require.Error(t, err)
	assert.Contains(t, err.Error(), path)
	assert.Contains(t, err.Error(), "listen_adr")
	assert.Contains(t, err.Error(), "extra")
	assert.NotContains(t, err.Error(), "word_budget", "known keys are not reported")
}

func TestApiKeysInTheSettingsFileAreRejectedAsUnknownKeys(t *testing.T) {
	path := writeSettingsFile(t, `gemini_api_key = "secret-value"`)

	_, err := Load(LoadOptions{File: path})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "gemini_api_key")
	assert.NotContains(t, err.Error(), "secret-value", "a rejected secret is never echoed")
}

func TestForeignFlagsInTheSameFlagSetAreNotUnknownKeys(t *testing.T) {
	flags := NewFlagSet()
	flags.String("config", "", "settings file")
	require.NoError(t, flags.Parse([]string{"--config=x.toml"}))

	_, err := Load(LoadOptions{Flags: flags})

	require.NoError(t, err)
}

func TestInvalidValuesAreRejectedWithTheNameOfTheSetting(t *testing.T) {
	tests := map[string]string{
		"time_zone":           `time_zone = "Mars/Olympus"`,
		"time_zone local":     `time_zone = "Local"`,
		"listen_addr":         `listen_addr = "no-port"`,
		"word_budget":         `word_budget = 0`,
		"article_char_limit":  `article_char_limit = -1`,
		"crawl_cache_ttl":     `crawl_cache_ttl = "0s"`,
		"scheduler_max_sleep": `scheduler_max_sleep = "0s"`,
		"section_pause":       `section_pause = "-1s"`,
		"episode_retention":   `episode_retention = "-1h"`,
		"default_voice":       `default_voice = ""`,
		"default_language":    `default_language = ""`,
		"firecrawl_url empty": `firecrawl_url = ""`,
		"firecrawl_url ftp":   `firecrawl_url = "ftp://firecrawl.lan"`,
		"firecrawl_url bare":  `firecrawl_url = "localhost:3002"`,
		"firecrawl_url host":  `firecrawl_url = "https://"`,
		"firecrawl_url port":  `firecrawl_url = "http://:3002"`,
		"script_model":        `script_model = ""`,
		"tts_model":           `tts_model = ""`,
	}
	for key, line := range tests {
		t.Run(key, func(t *testing.T) {
			_, err := Load(LoadOptions{File: writeSettingsFile(t, line)})

			require.Error(t, err)
			assert.Contains(t, err.Error(), strings.Fields(key)[0])
		})
	}
}

func TestZeroRetentionIsValidAndSwitchesTheCleanUpOff(t *testing.T) {
	got, err := Load(LoadOptions{File: writeSettingsFile(t, `episode_retention = "0s"`)})

	require.NoError(t, err)
	assert.Zero(t, got.EpisodeRetention)
}

func TestSampleSettingsFileIsValidAndShowsTheDefaults(t *testing.T) {
	got, err := Load(LoadOptions{File: "../../hack/aigude.toml"})

	require.NoError(t, err)
	assert.Equal(t, Defaults(), got, "the settings file in hack documents the built-in defaults")
}
