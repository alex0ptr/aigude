package scripttoml_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/alex0ptr/aigude/business"
	"github.com/alex0ptr/aigude/pkg/scripttoml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixedLLM answers every prompt with the same reply and remembers the prompt.
type fixedLLM struct {
	reply  string
	err    error
	prompt string
	usage  business.Usage
}

func (f *fixedLLM) SinglePrompt(prompt string) (string, business.Usage, error) {
	f.prompt = prompt
	return f.reply, f.usage, f.err
}

const (
	system = "RULES-OF-THE-SYSTEM"
	style  = "STYLE-OF-THE-PROFILE"
)

func contents() []business.Content {
	return []business.Content{
		{Title: "First article", Source: "heise", Link: "https://example.org/1", Body: "Body one."},
		{Title: "Second article", Source: "tagesschau", Link: "https://example.org/2", Body: "Body two."},
	}
}

// scriptFrom writes a Script from a language model that answers with reply.
func scriptFrom(t *testing.T, reply string) (business.Script, error) {
	t.Helper()
	script, _, err := scripttoml.New(&fixedLLM{reply: reply}).WriteScript(system, style, contents())
	return script, err
}

const tomlScript = `title = '''Daily Tech News'''

[[section]]
heading = '''Introduction'''
sources = []
text = '''
Welcome to today's briefing.
'''

[[section]]
heading = '''Chips and Clouds'''
sources = [1, 2]
text = '''
First line.
Second line.
'''

[[section]]
heading = '''Closing'''
sources = []
text = '''
Goodbye!
'''
`

func TestScriptIsReadFromTOML(t *testing.T) {
	script, err := scriptFrom(t, tomlScript)

	require.NoError(t, err)
	assert.Equal(t, "Daily Tech News", script.Title)
	require.Len(t, script.Sections, 3)
	assert.Equal(t, "Introduction", script.Sections[0].Heading)
	assert.Equal(t, "Welcome to today's briefing.", script.Sections[0].Body)
	assert.Empty(t, script.Sections[0].Sources)
	assert.Equal(t, "Chips and Clouds", script.Sections[1].Heading)
	assert.Equal(t, "First line.\nSecond line.", script.Sections[1].Body, "line breaks inside the text stay")
	assert.Equal(t, []int{1, 2}, script.Sections[1].Sources)
	assert.Equal(t, "Closing", script.Sections[2].Heading)
}

func TestApostrophesAndQuotesNeedNoEscaping(t *testing.T) {
	reply := `title = '''Wie's weitergeht: "Der Hammer"'''

[[section]]
heading = '''Rock'n'Roll'''
sources = []
text = '''
Das ist 'ne Sache, die "zählt" - mit \ Backslash.
'''
`

	script, err := scriptFrom(t, reply)

	require.NoError(t, err)
	assert.Equal(t, `Wie's weitergeht: "Der Hammer"`, script.Title)
	assert.Equal(t, "Rock'n'Roll", script.Sections[0].Heading)
	assert.Equal(t, `Das ist 'ne Sache, die "zählt" - mit \ Backslash.`, script.Sections[0].Body)
}

func TestACodeFenceAroundTheScriptIsIgnored(t *testing.T) {
	script, err := scriptFrom(t, "```toml\n"+tomlScript+"```\n")

	require.NoError(t, err)
	assert.Equal(t, "Daily Tech News", script.Title)
	assert.Len(t, script.Sections, 3)
}

func TestAScriptThatIsNotValidIsRejectedWithAReason(t *testing.T) {
	tests := []struct {
		name    string
		reply   string
		message string
	}{
		{"not TOML at all", "Here is your script: good morning!", "TOML"},
		{"the old line format", "title: \"T\"\ntoc:\n- \"A\"\n---SECTION---\nText", "TOML"},
		{"no title", "[[section]]\nheading = '''A'''\ntext = '''B'''\n", "title"},
		{"no sections", "title = '''T'''\n", "section"},
		{"section without heading", "title = '''T'''\n[[section]]\ntext = '''B'''\n", "heading"},
		{"section without text", "title = '''T'''\n[[section]]\nheading = '''A'''\n", "text"},
		{"unknown key", "title = '''T'''\n[[section]]\nheading = '''A'''\ntext = '''B'''\nsource = [1]\n", "source"},
		{"sources of the wrong type", "title = '''T'''\n[[section]]\nheading = '''A'''\nsources = 'one'\ntext = '''B'''\n", "line 4"},
		{"cut off in the middle", "title = '''T'''\n[[section]]\nheading = '''A'''\ntext = '''Half a sen", "TOML"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := scriptFrom(t, tt.reply)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.message)
		})
	}
}

func TestThePromptCarriesRulesFormatStyleAndNumberedContents(t *testing.T) {
	llm := &fixedLLM{reply: tomlScript}

	_, _, err := scripttoml.New(llm).WriteScript(system, style, contents())

	require.NoError(t, err)
	order := []string{
		"RULES-OF-THE-SYSTEM", "[[section]]", "STYLE-OF-THE-PROFILE",
		"Content Item 1", "First article", "https://example.org/1", "Body one.",
		"Content Item 2", "Second article", "tagesschau", "Body two.",
	}
	last := -1
	for _, want := range order {
		i := strings.Index(llm.prompt, want)
		require.GreaterOrEqual(t, i, 0, "%q is missing from the prompt:\n%s", want, llm.prompt)
		require.Greater(t, i, last, "%q is out of order in the prompt:\n%s", want, llm.prompt)
		last = i
	}
}

func TestTheExampleInThePromptIsAValidScript(t *testing.T) {
	llm := &fixedLLM{reply: tomlScript}
	_, _, err := scripttoml.New(llm).WriteScript(system, style, contents())
	require.NoError(t, err)
	start := strings.Index(llm.prompt, "title = ")
	end := strings.Index(llm.prompt, "\n\nRules for the format:")
	require.True(t, start >= 0 && end > start, "example not found in the prompt:\n%s", llm.prompt)

	script, err := scriptFrom(t, llm.prompt[start:end])

	require.NoError(t, err, "the layout the model is shown must be the layout the writer reads")
	assert.NotEmpty(t, script.Title)
	assert.GreaterOrEqual(t, len(script.Sections), 2)
}

func TestUsageOfTheLanguageModelIsHandedOnAlsoWhenTheAnswerCannotBeRead(t *testing.T) {
	usage := business.Usage{{Kind: business.UsageScript, Unit: business.UnitInputTokens, Quantity: 12}}

	_, got, err := scripttoml.New(&fixedLLM{reply: tomlScript, usage: usage}).WriteScript(system, style, contents())
	require.NoError(t, err)
	assert.Equal(t, usage, got)

	_, got, err = scripttoml.New(&fixedLLM{reply: "not toml at all [", usage: usage}).WriteScript(system, style, contents())
	require.Error(t, err)
	assert.Equal(t, usage, got, "the tokens are gone even though the answer is unusable")

	_, got, err = scripttoml.New(&fixedLLM{err: errors.New("boom"), usage: usage}).WriteScript(system, style, contents())
	require.Error(t, err)
	assert.Equal(t, usage, got)
}
