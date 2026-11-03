package llm

import (
	"testing"

	"github.com/alex0ptr/aigude/business"
	"github.com/stretchr/testify/assert"
	"google.golang.org/genai"
)

func TestScriptUsageCountsThinkingTokensAsOutput(t *testing.T) {
	resp := &genai.GenerateContentResponse{UsageMetadata: &genai.GenerateContentResponseUsageMetadata{
		PromptTokenCount: 45120, CandidatesTokenCount: 2000, ThoughtsTokenCount: 810,
	}}

	assert.Equal(t, business.Usage{
		{Kind: business.UsageScript, Unit: business.UnitInputTokens, Quantity: 45120},
		{Kind: business.UsageScript, Unit: business.UnitOutputTokens, Quantity: 2810},
	}, scriptUsage(resp))
}

func TestSpeechUsageCountsTheAudioModalityOfTheAnswer(t *testing.T) {
	resp := &genai.GenerateContentResponse{UsageMetadata: &genai.GenerateContentResponseUsageMetadata{
		PromptTokenCount: 300, CandidatesTokenCount: 15000,
		CandidatesTokensDetails: []*genai.ModalityTokenCount{
			{Modality: genai.MediaModalityText, TokenCount: 100},
			{Modality: genai.MediaModalityAudio, TokenCount: 14900},
		},
	}}

	assert.Equal(t, business.Usage{
		{Kind: business.UsageSpeech, Unit: business.UnitInputTokens, Quantity: 300},
		{Kind: business.UsageSpeech, Unit: business.UnitAudioTokens, Quantity: 14900},
	}, speechUsage(resp))
}

func TestAnswerWithoutUsageMetadataReportsNothing(t *testing.T) {
	resp := &genai.GenerateContentResponse{}

	assert.Empty(t, scriptUsage(resp))
	assert.Empty(t, speechUsage(resp))
}

func TestSpeechWithoutAudioBreakDownReportsOnlyWhatItKnows(t *testing.T) {
	resp := &genai.GenerateContentResponse{UsageMetadata: &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 300, CandidatesTokenCount: 15000}}

	assert.Equal(t, business.Usage{{Kind: business.UsageSpeech, Unit: business.UnitInputTokens, Quantity: 300}}, speechUsage(resp))
}
