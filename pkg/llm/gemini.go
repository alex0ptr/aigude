package llm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/alex0ptr/aigude/business"
	"google.golang.org/genai"
)

// DefaultGeminiModel is Google's "latest Flash" alias. Google hot-swaps it
// whenever a new Flash version ships, so it follows the newest model with
// every release. An explicit version (e.g. "gemini-3.8-flash") gives
// reproducible behavior across model upgrades.
const DefaultGeminiModel = "gemini-flash-latest"

// Gemini implements business.LLM using Google's Gemini API via the unified
// google.golang.org/genai SDK.
type Gemini struct {
	client *genai.Client
	model  string
	ctx    context.Context
}

// NewGemini creates a new Gemini LLM client.
// apiKey is the Google AI API key (get one at https://aistudio.google.com/apikey).
// model is the model name, e.g. DefaultGeminiModel.
func NewGemini(apiKey string, model string) (*Gemini, error) {
	slog.Debug("Creating Gemini client", "model", model)

	ctx := context.Background()
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		slog.Error("Failed to create Gemini client", "error", err)
		return nil, fmt.Errorf("failed to create Gemini client: %w", err)
	}

	return &Gemini{
		client: client,
		model:  model,
		ctx:    ctx,
	}, nil
}

// SinglePrompt sends a single prompt to Gemini and returns the response text and what the answer consumed.
func (g *Gemini) SinglePrompt(prompt string) (string, business.Usage, error) {
	slog.Debug("Sending prompt to Gemini", "model", g.model, "promptLength", len(prompt))

	resp, err := g.client.Models.GenerateContent(g.ctx, g.model, genai.Text(prompt), nil)
	if err != nil {
		slog.Error("Failed to generate content from Gemini", "error", err)
		return "", nil, fmt.Errorf("failed to generate content: %w", err)
	}

	usage := scriptUsage(resp)
	result := resp.Text()
	if result == "" {
		slog.Warn("Empty response text from Gemini")
		return "", usage, errors.New("empty response content")
	}

	slog.Debug("Received response from Gemini", "responseLength", len(result))

	return result, usage, nil
}
