// Package scripttoml writes the Script of an Episode with any text-only language model.
//
// The model is asked to answer in a TOML layout that this package defines (format.txt) and reads back, so
// the business layer and the model adapters know nothing about it. A model that returns structured data
// implements business.ScriptWriter directly.
package scripttoml

import (
	_ "embed"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/alex0ptr/aigude/business"
	"github.com/pelletier/go-toml/v2"
)

//go:embed format.txt
var format string

// Writer implements business.ScriptWriter on top of a business.LLM.
type Writer struct {
	llm business.LLM
}

// New creates a Writer that asks the given language model.
func New(llm business.LLM) *Writer {
	return &Writer{llm: llm}
}

// WriteScript builds the prompt, asks the language model and reads the answer back into a Script. The answer is
// read strictly: an unknown key, a missing title, heading or text, or no Section at all is an error that says
// what is wrong. The result is all or nothing: a complete Script or an error, also for a Script that is cut off or
// has a syntax error.
func (w *Writer) WriteScript(system, style string, contents []business.Content) (business.Script, business.Usage, error) {
	prompt := buildPrompt(system, style, contents)
	slog.Info("Asking the language model for the script", "promptLength", len(prompt), "contentCount", len(contents))
	response, usage, err := w.llm.SinglePrompt(prompt)
	if err != nil {
		slog.Error("Language model failed", "error", err)
		return business.Script{}, usage, fmt.Errorf("language model: %w", err)
	}
	slog.Debug("Received the language model's answer", "responseLength", len(response))

	script, err := parse(response)
	if err != nil {
		slog.Error("Could not read the script", "error", err)
		return business.Script{}, usage, err
	}
	slog.Info("Script read", "title", script.Title, "sectionCount", len(script.Sections))
	return script, usage, nil
}

// buildPrompt puts the rules, the layout of the answer, the style guide and the numbered Content items into
// one prompt.
func buildPrompt(system, style string, contents []business.Content) string {
	var builder strings.Builder

	builder.WriteString("System Instructions:\n")
	builder.WriteString(system)
	builder.WriteString("\n\nOutput Format:\n")
	builder.WriteString(format)
	builder.WriteString("\n\nStyle Guidelines:\n")
	builder.WriteString(style)
	builder.WriteString("\n\nContent to process:\n\n")
	for i, content := range contents {
		fmt.Fprintf(&builder, "--- Content Item %d ---\n", i+1)
		fmt.Fprintf(&builder, "Title: %s\n", content.Title)
		fmt.Fprintf(&builder, "Source: %s\n", content.Source)
		fmt.Fprintf(&builder, "Created: %s\n", content.Created.Format(time.DateTime))
		fmt.Fprintf(&builder, "Link: %s\n", content.Link)
		fmt.Fprintf(&builder, "Body:\n%s\n\n", content.Body)
	}

	return builder.String()
}

// rawScript mirrors the TOML the language model is asked to write (see format.txt).
type rawScript struct {
	Title    string       `toml:"title"`
	Sections []rawSection `toml:"section"`
}

type rawSection struct {
	Heading string `toml:"heading"`
	Sources []int  `toml:"sources"`
	Text    string `toml:"text"`
}

// parse reads the TOML answer of the language model. A code fence around it is tolerated, because models like to
// add one.
func parse(response string) (business.Script, error) {
	var raw rawScript
	err := toml.NewDecoder(strings.NewReader(stripCodeFence(response))).DisallowUnknownFields().Decode(&raw)
	if err != nil {
		var strictErr *toml.StrictMissingError
		if errors.As(err, &strictErr) {
			return business.Script{}, fmt.Errorf("answer has keys the format does not know: %s", strictErr.String())
		}
		var decodeErr *toml.DecodeError
		if errors.As(err, &decodeErr) {
			row, column := decodeErr.Position()
			return business.Script{}, fmt.Errorf("answer is not the expected TOML (line %d, column %d): %s", row, column, decodeErr.Error())
		}
		return business.Script{}, fmt.Errorf("answer is not the expected TOML: %w", err)
	}

	if strings.TrimSpace(raw.Title) == "" {
		return business.Script{}, errors.New("answer has no title")
	}
	if len(raw.Sections) == 0 {
		return business.Script{}, errors.New("answer has no section")
	}
	sections := make([]business.Section, len(raw.Sections))
	for i, rs := range raw.Sections {
		if strings.TrimSpace(rs.Heading) == "" {
			return business.Script{}, fmt.Errorf("section %d has no heading", i+1)
		}
		if strings.TrimSpace(rs.Text) == "" {
			return business.Script{}, fmt.Errorf("section %d (%q) has no text", i+1, rs.Heading)
		}
		sections[i] = business.Section{Heading: strings.TrimSpace(rs.Heading), Body: strings.TrimSpace(rs.Text), Sources: rs.Sources}
	}
	slog.Debug("Parsed script", "title", raw.Title, "sections", len(sections))
	return business.Script{Title: strings.TrimSpace(raw.Title), Sections: sections}, nil
}

// stripCodeFence removes a Markdown code fence (```toml ... ```) around the answer, if there is one.
func stripCodeFence(response string) string {
	trimmed := strings.TrimSpace(response)
	if !strings.HasPrefix(trimmed, "```") {
		return response
	}
	slog.Debug("Removing code fence around the script")
	_, afterOpening, found := strings.Cut(trimmed, "\n")
	if !found {
		return response
	}
	return strings.TrimSuffix(strings.TrimSpace(afterOpening), "```")
}
