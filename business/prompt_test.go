package business

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRenderSystemPromptFillsAllPlaceholders(t *testing.T) {
	now := time.Date(2026, time.October, 29, 8, 0, 0, 0, time.UTC)

	rendered := renderSystemPrompt(systemPrompt, "German", 1400, now)

	assert.NotContains(t, rendered, "{language}", "language placeholder must be filled")
	assert.NotContains(t, rendered, "{word_budget}", "word budget placeholder must be filled")
	assert.NotContains(t, rendered, "{date}", "date placeholder must be filled")
	assert.Contains(t, rendered, "German")
	assert.Contains(t, rendered, "2026-10-29")
}

func TestTheLengthIsInstructedOnlyOnceAndNotByTheStyleGuide(t *testing.T) {
	assert.Equal(t, 1, strings.Count(systemPrompt, "Length:"), "the system prompt states the length once")
	assert.NotContains(t, defaultUserPrompt, "Length:", "the style guide leaves the length to the Word Budget")
	assert.NotContains(t, strings.ToLower(defaultUserPrompt), "spoken words", "the style guide names no number of words")
}
