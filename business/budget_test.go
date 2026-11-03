package business

import (
	"bytes"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// logged returns what the code under test logs for as long as the test runs.
func logged(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &buf
}

func TestTheWordBudgetOfTheShowInThePromptIsItsOwnOrTheServices(t *testing.T) {
	tests := []struct {
		name       string
		showBudget int
		inPrompt   int
	}{
		{"none takes the service's", 0, 4000},
		{"lower than the service's", 800, 800},
		{"higher than the service's", 9000, 9000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writer := &fakeWriter{script: Script{Title: "T"}}
			episodes := testEpisodes(writer)

			_, _, err := episodes.GenerateScript(CastProfile{Name: "Test", WordBudget: tt.showBudget}, twoItems())

			require.NoError(t, err)
			assert.Contains(t, writer.system, fmt.Sprintf("aim for about %d spoken words", tt.inPrompt))
			assert.Contains(t, writer.system, fmt.Sprintf("must not exceed %d words", tt.inPrompt))
		})
	}
}

func TestAScriptIsKeptAndWarnedAboutOnlyWhenItsWordsDeviateFromTheBudgetByMoreThanATenth(t *testing.T) {
	// The service's Word Budget is 100 words.
	tests := []struct {
		name       string
		words      int
		showBudget int
		warning    string
	}{
		{"exactly the budget", 100, 0, ""},
		{"a tenth over", 110, 0, ""},
		{"more than a tenth over", 111, 0, "longer than its word budget"},
		{"a tenth under", 90, 0, ""},
		{"more than a tenth under", 89, 0, "shorter than its word budget"},
		{"over the budget of the show", 100, 50, "longer than its word budget"},
		{"within a budget of the show above the service's", 190, 200, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := logged(t)
			script := Script{Title: "T", Sections: []Section{{Heading: "H", Body: strings.Repeat("word ", tt.words)}}}
			episodes := testEpisodes(&fakeWriter{script: script})
			episodes.wordBudget = 100

			got, _, err := episodes.GenerateScript(CastProfile{Name: "Test", WordBudget: tt.showBudget}, twoItems())

			require.NoError(t, err, "the Script is kept")
			assert.Equal(t, script, got)
			for _, warning := range []string{"longer than its word budget", "shorter than its word budget"} {
				assert.Equal(t, warning == tt.warning, strings.Contains(log.String(), warning), "%q in %s", warning, log.String())
			}
		})
	}
}
