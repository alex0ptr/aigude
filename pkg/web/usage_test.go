package web

import (
	"testing"

	"github.com/alex0ptr/aigude/business"
	"github.com/stretchr/testify/assert"
)

func TestUsageIsOneSortedLinePerKindAndUnitWithThousandsSeparated(t *testing.T) {
	usage := business.Usage{
		{Kind: business.UsageSpeech, Unit: business.UnitAudioTokens, Quantity: 14900},
		{Kind: business.UsageScript, Unit: business.UnitOutputTokens, Quantity: 2810},
		{Kind: business.UsageCrawl, Unit: business.UnitPages, Quantity: 14},
		{Kind: business.UsageScript, Unit: business.UnitInputTokens, Quantity: 45120},
		{Kind: business.UsageSpeech, Unit: business.UnitInputTokens, Quantity: 1234567},
	}

	assert.Equal(t, []string{
		"Crawl pages: 14",
		"Script input tokens: 45,120",
		"Script output tokens: 2,810",
		"Speech audio tokens: 14,900",
		"Speech input tokens: 1,234,567",
	}, usageLines(usage))
}

func TestANewKindOrUnitIsOneMoreLine(t *testing.T) {
	usage := business.Usage{{Kind: "image", Unit: "megapixels", Quantity: 3}}

	assert.Equal(t, []string{"Image megapixels: 3"}, usageLines(usage))
}
