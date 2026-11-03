package service_test

import (
	"testing"

	"github.com/alex0ptr/aigude/pkg/service/servicetest"
	"github.com/alex0ptr/aigude/pkg/settings"
	"github.com/stretchr/testify/assert"
)

func TestTheConfiguredWordBudgetIsInThePrompt(t *testing.T) {
	feed := servicetest.ServeFeed(t, feedWith("News", "https://example.org/a", "Text"))
	h := servicetest.Start(t,
		servicetest.WithUserConfigFile("alex.toml", generatingFile(feed, "")),
		servicetest.WithSettings(func(s *settings.Settings) { s.WordBudget = 1234 }))
	h.ScriptWriter.Script = threeSections()

	generateAndWaitFor(t, h, 1, 1)

	system := h.ScriptWriter.Requests()[0].System
	assert.Contains(t, system, "1234 words")
	assert.NotContains(t, system, "4000")
}

func TestTheWordBudgetOfACastProfileInTheUserConfigIsInItsPrompt(t *testing.T) {
	feed := servicetest.ServeFeed(t, feedWith("News", "https://example.org/a", "Text"))
	h := servicetest.Start(t,
		servicetest.WithUserConfigFile("alex.toml", generatingFile(feed, "word_budget = 500")),
		servicetest.WithSettings(func(s *settings.Settings) { s.WordBudget = 1234 }))
	h.ScriptWriter.Script = threeSections()

	generateAndWaitFor(t, h, 1, 1)

	system := h.ScriptWriter.Requests()[0].System
	assert.Contains(t, system, "about 500 spoken words")
	assert.NotContains(t, system, "1234")
}
