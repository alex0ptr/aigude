package business

import (
	"errors"
	"github.com/stretchr/testify/require"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestUsageAddSumsEqualKindAndUnit(t *testing.T) {
	a := Usage{{UsageSpeech, UnitInputTokens, 10}, {UsageSpeech, UnitAudioTokens, 100}}
	b := Usage{{UsageSpeech, UnitAudioTokens, 50}, {UsageCrawl, UnitPages, 2}}

	got := a.Add(b)

	assert.Equal(t, Usage{{UsageSpeech, UnitInputTokens, 10}, {UsageSpeech, UnitAudioTokens, 150}, {UsageCrawl, UnitPages, 2}}, got)
	assert.Equal(t, Usage{{UsageSpeech, UnitInputTokens, 10}, {UsageSpeech, UnitAudioTokens, 100}}, a, "the operands stay as they were")
}

func TestEpisodeKeepsTheUsageOfEveryStepSummedOverItsSections(t *testing.T) {
	speech := Usage{{UsageSpeech, UnitInputTokens, 10}, {UsageSpeech, UnitAudioTokens, 100}}
	episodes, _, repository, profile := producing(lengthTTS{usage: speech}, "1", "2", "3")
	profile.Subscriptions = []Subscription{{Repository: oneItem{usage: Usage{{UsageCrawl, UnitPages, 2}}}}}
	episodes.writer = &fakeWriter{script: Script{Title: "T", Sections: []Section{{Heading: "a", Body: "1"}, {Heading: "b", Body: "2"}, {Heading: "c", Body: "3"}}},
		usage: Usage{{UsageScript, UnitInputTokens, 500}, {UsageScript, UnitOutputTokens, 80}}}

	_, err := episodes.Create(User{ID: "alex"}, profile)

	require.NoError(t, err)
	require.Len(t, repository.saved, 1)
	assert.ElementsMatch(t, Usage{
		{UsageCrawl, UnitPages, 2},
		{UsageScript, UnitInputTokens, 500}, {UsageScript, UnitOutputTokens, 80},
		{UsageSpeech, UnitInputTokens, 30}, {UsageSpeech, UnitAudioTokens, 300},
	}, repository.saved[0].Usage)
}

func TestFailedEpisodeKeepsWhatWasConsumedUpToTheFailure(t *testing.T) {
	speech := Usage{{UsageSpeech, UnitAudioTokens, 100}}
	episodes, _, repository, profile := producing(lengthTTS{failOn: "2", usage: speech}, "1", "2")
	profile.Subscriptions = []Subscription{{Repository: oneItem{usage: Usage{{UsageCrawl, UnitPages, 1}}}}}

	_, err := episodes.Create(User{ID: "alex"}, profile)

	require.Error(t, err)
	require.Len(t, repository.failed, 1)
	assert.ElementsMatch(t, Usage{{UsageCrawl, UnitPages, 1}, {UsageSpeech, UnitAudioTokens, 200}}, repository.failed[0].Usage,
		"the first Section and the call that failed both consumed something")
}

func TestScriptThatFailsStillContributesItsUsage(t *testing.T) {
	episodes, _, repository, profile := producing(lengthTTS{}, "1")
	episodes.writer = &fakeWriter{err: errors.New("model unavailable"), usage: Usage{{UsageScript, UnitInputTokens, 7}}}

	_, err := episodes.Create(User{ID: "alex"}, profile)

	require.Error(t, err)
	assert.Equal(t, Usage{{UsageScript, UnitInputTokens, 7}}, repository.failed[0].Usage)
}

func TestCachedPageAddsNoUsage(t *testing.T) {
	cache := &memoryCache{pages: map[string]Page{"https://example.org/a": {Text: "cached"}}}
	crawler := NewCachingCrawler(countingCrawler{}, cache, fixedClock{}, time.Hour)

	_, hit, err := crawler.Crawl("https://example.org/a")
	require.NoError(t, err)
	_, miss, err := crawler.Crawl("https://example.org/b")
	require.NoError(t, err)

	assert.Empty(t, hit)
	assert.Equal(t, Usage{{UsageCrawl, UnitPages, 1}}, miss)
}

type countingCrawler struct{}

func (countingCrawler) Crawl(string) (Page, Usage, error) {
	return Page{Text: "fresh"}, Usage{{UsageCrawl, UnitPages, 1}}, nil
}

type memoryCache struct{ pages map[string]Page }

func (m *memoryCache) Get(url string, _ time.Time) (Page, bool, error) {
	p, ok := m.pages[url]
	return p, ok, nil
}
func (m *memoryCache) Put(url string, p Page, _ time.Time) error { m.pages[url] = p; return nil }
func (m *memoryCache) DeleteBefore(time.Time) (int, error)       { return 0, nil }

func TestEpisodeKeepsTheContentOfItsPromptAsSources(t *testing.T) {
	episodes, _, repository, profile := producing(lengthTTS{}, "1")
	profile.Subscriptions = []Subscription{{Repository: stubContent{{Title: "A", Body: "text a", Link: "https://example.org/a", Source: "feed"}, {Title: "B", Body: "text b"}}}}

	_, err := episodes.Create(User{ID: "alex"}, profile)

	require.NoError(t, err)
	assert.Equal(t, []Source{
		{Title: "A", Link: "https://example.org/a", Name: "feed", Text: "text a", Retrieved: episodes.clock.Now()},
		{Title: "B", Text: "text b", Retrieved: episodes.clock.Now()},
	}, repository.saved[0].Sources, "in the order of the prompt, so that the numbers of Section.Sources count in it")
}

type stubContent []Content

func (s stubContent) Content(time.Time, time.Time, ContentFetchOpts) ([]Content, Usage, error) {
	return s, nil, nil
}
