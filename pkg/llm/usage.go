package llm

import (
	"log/slog"

	"github.com/alex0ptr/aigude/business"
	"google.golang.org/genai"
)

// scriptUsage is what an answer that wrote a Script consumed: the tokens of the prompt, and the tokens of the answer
// together with the thinking tokens, which Google bills as output. An answer without usage metadata reports nothing.
func scriptUsage(resp *genai.GenerateContentResponse) business.Usage {
	meta := resp.UsageMetadata
	if meta == nil {
		slog.Warn("Gemini answered without usage metadata, the Script reports no Usage")
		return nil
	}
	return nonZero(
		business.UsageItem{Kind: business.UsageScript, Unit: business.UnitInputTokens, Quantity: int64(meta.PromptTokenCount)},
		business.UsageItem{Kind: business.UsageScript, Unit: business.UnitOutputTokens, Quantity: int64(meta.CandidatesTokenCount) + int64(meta.ThoughtsTokenCount)},
	)
}

// speechUsage is what an answer that spoke a text consumed: the tokens of the prompt and the tokens of the audio
// modality of the answer. An answer without usage metadata, or without the break-down by modality, reports nothing
// for what it cannot tell.
func speechUsage(resp *genai.GenerateContentResponse) business.Usage {
	meta := resp.UsageMetadata
	if meta == nil {
		slog.Warn("Gemini answered without usage metadata, the speech reports no Usage")
		return nil
	}
	var audio int64
	for _, detail := range meta.CandidatesTokensDetails {
		if detail != nil && detail.Modality == genai.MediaModalityAudio {
			audio += int64(detail.TokenCount)
		}
	}
	if audio == 0 {
		slog.Warn("Gemini answered without audio tokens in its usage metadata, the speech reports none", "candidatesTokenCount", meta.CandidatesTokenCount)
	}
	return nonZero(
		business.UsageItem{Kind: business.UsageSpeech, Unit: business.UnitInputTokens, Quantity: int64(meta.PromptTokenCount)},
		business.UsageItem{Kind: business.UsageSpeech, Unit: business.UnitAudioTokens, Quantity: audio},
	)
}

// nonZero is the Usage of the items that consumed something.
func nonZero(items ...business.UsageItem) business.Usage {
	var usage business.Usage
	for _, item := range items {
		if item.Quantity > 0 {
			usage = append(usage, item)
		}
	}
	return usage
}
