package service_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/alex0ptr/aigude/business"
	"github.com/alex0ptr/aigude/pkg/service/servicetest"
	"github.com/stretchr/testify/assert"
)

func usageOf(kind business.UsageKind, unit business.UsageUnit, quantity int64) business.UsageItem {
	return business.UsageItem{Kind: kind, Unit: unit, Quantity: quantity}
}

// reportingHarness is crawlingHarness whose Script writer and speech report fixed Usage.
func reportingHarness(t *testing.T) *servicetest.Harness {
	h := crawlingHarness(t)
	h.ScriptWriter.Usage = business.Usage{usageOf(business.UsageScript, business.UnitInputTokens, 500), usageOf(business.UsageScript, business.UnitOutputTokens, 80)}
	h.TTS.Usage = business.Usage{usageOf(business.UsageSpeech, business.UnitInputTokens, 10), usageOf(business.UsageSpeech, business.UnitAudioTokens, 100)}
	return h
}

func TestReadyEpisodeHasTheUsageOfAllItsStepsSummedOverItsSections(t *testing.T) {
	h := reportingHarness(t)

	episodes := generateAndWaitFor(t, h, 1, 1)

	stored := h.Episode(t, "alex", episodes[0].ID)
	assert.ElementsMatch(t, business.Usage{
		usageOf(business.UsageCrawl, business.UnitPages, 1),
		usageOf(business.UsageScript, business.UnitInputTokens, 500),
		usageOf(business.UsageScript, business.UnitOutputTokens, 80),
		usageOf(business.UsageSpeech, business.UnitInputTokens, 30),
		usageOf(business.UsageSpeech, business.UnitAudioTokens, 300),
	}, stored.Usage, "three Sections are spoken in three calls")
}

func TestUsageIsLoggedOncePerEpisode(t *testing.T) {
	h := reportingHarness(t)
	generateAndWaitFor(t, h, 1, 1)

	var lines []string
	for _, line := range h.Logs.Lines() {
		if strings.Contains(line, "Episode ready") && strings.Contains(line, "script.input_tokens=500") {
			lines = append(lines, line)
		}
	}
	assert.Len(t, lines, 1, fmt.Sprint(h.Logs.Lines()))
}
