package database

import (
	"errors"
	"testing"
	"time"

	"github.com/alex0ptr/aigude/business"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// memoryStore keeps audio in memory; with fail set it cannot store anything.
type memoryStore struct{ fail bool }

func (m memoryStore) Store(key string, _ []byte) (string, error) {
	if m.fail {
		return "", errors.New("disk full")
	}
	return key, nil
}

func (memoryStore) Open(string) (business.AudioReader, time.Time, error) {
	return nil, time.Time{}, errors.New("not needed")
}

func (memoryStore) Delete(string) error { return nil }

func openEpisodes(t *testing.T, store memoryStore) *EpisodeRepository {
	t.Helper()
	db, err := Open(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db.Episodes(store)
}

func episodeUsing(userID, castProfileID string, identities ...string) business.Episode {
	return business.Episode{
		Date: noon, UserID: userID, CastProfileID: castProfileID,
		Script: business.Script{Title: "T"}, UsedContent: identities,
	}
}

func TestAReadyEpisodeUsesUpItsContentForItsCastProfileOnly(t *testing.T) {
	repo := openEpisodes(t, memoryStore{})
	_, err := repo.Save(episodeUsing("alex", "morning", "https://example.org/a", "https://example.org/b"), []byte("a"))
	require.NoError(t, err)

	used, err := repo.UsedContent("alex", "morning")
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"https://example.org/a", "https://example.org/b"}, used)
	other, err := repo.UsedContent("alex", "evening")
	require.NoError(t, err)
	assert.Empty(t, other, "another CastProfile of the User has used nothing")
	anotherUser, err := repo.UsedContent("bea", "morning")
	require.NoError(t, err)
	assert.Empty(t, anotherUser, "the CastProfile of another User with the same Id has used nothing")
}

func TestAFailedEpisodeUsesUpNothing(t *testing.T) {
	repo := openEpisodes(t, memoryStore{})
	_, err := repo.SaveFailed(episodeUsing("alex", "morning", "https://example.org/a"), "model unreachable")
	require.NoError(t, err)

	used, err := repo.UsedContent("alex", "morning")

	require.NoError(t, err)
	assert.Empty(t, used)
}

func TestDeletingAnEpisodeGivesItsContentBack(t *testing.T) {
	repo := openEpisodes(t, memoryStore{})
	first, err := repo.Save(episodeUsing("alex", "morning", "https://example.org/a"), []byte("a"))
	require.NoError(t, err)
	_, err = repo.Save(episodeUsing("alex", "morning", "https://example.org/b"), []byte("a"))
	require.NoError(t, err)

	require.NoError(t, repo.Delete("alex", first.ID))

	used, err := repo.UsedContent("alex", "morning")
	require.NoError(t, err)
	assert.Equal(t, []string{"https://example.org/b"}, used)
}

func TestAnEpisodeWhoseAudioCannotBeStoredUsesUpNothing(t *testing.T) {
	repo := openEpisodes(t, memoryStore{fail: true})

	_, err := repo.Save(episodeUsing("alex", "morning", "https://example.org/a"), []byte("a"))

	require.Error(t, err)
	used, err := repo.UsedContent("alex", "morning")
	require.NoError(t, err)
	assert.Empty(t, used, "the record is rolled back as a whole")
	episodes, err := repo.List("alex", business.ListOpts{})
	require.NoError(t, err)
	assert.Empty(t, episodes)
}

var usageOfAnEpisode = business.Usage{
	{Kind: business.UsageCrawl, Unit: business.UnitPages, Quantity: 14},
	{Kind: business.UsageScript, Unit: business.UnitInputTokens, Quantity: 45120},
	{Kind: business.UsageScript, Unit: business.UnitOutputTokens, Quantity: 2810},
	{Kind: business.UsageSpeech, Unit: business.UnitAudioTokens, Quantity: 14900},
}

func TestUsageIsKeptWithAReadyAndAFailedEpisode(t *testing.T) {
	repo := openEpisodes(t, memoryStore{})
	ready := episodeUsing("alex", "morning")
	ready.Usage = usageOfAnEpisode
	failed := episodeUsing("alex", "morning")
	failed.Usage = usageOfAnEpisode[:1]
	savedReady, err := repo.Save(ready, []byte("a"))
	require.NoError(t, err)
	savedFailed, err := repo.SaveFailed(failed, "speech unavailable")
	require.NoError(t, err)

	gotReady, err := repo.Get("alex", savedReady.ID)
	require.NoError(t, err)
	gotFailed, err := repo.Get("alex", savedFailed.ID)
	require.NoError(t, err)
	listed, err := repo.List("alex", business.ListOpts{})
	require.NoError(t, err)

	assert.ElementsMatch(t, usageOfAnEpisode, gotReady.Usage)
	assert.ElementsMatch(t, usageOfAnEpisode[:1], gotFailed.Usage)
	require.Len(t, listed, 2)
	assert.ElementsMatch(t, usageOfAnEpisode[:1], listed[0].Usage, "the list carries the Usage as well")
	assert.ElementsMatch(t, usageOfAnEpisode, listed[1].Usage)
}

func TestDeletingAnEpisodeRemovesItsUsage(t *testing.T) {
	repo := openEpisodes(t, memoryStore{})
	episode := episodeUsing("alex", "morning")
	episode.Usage = usageOfAnEpisode
	saved, err := repo.Save(episode, []byte("a"))
	require.NoError(t, err)

	require.NoError(t, repo.Delete("alex", saved.ID))

	var rows int
	require.NoError(t, repo.db.QueryRow(`SELECT COUNT(*) FROM usage_items`).Scan(&rows))
	assert.Zero(t, rows)
}

func TestSourcesAndTheirSectionsAreKeptWithTheEpisode(t *testing.T) {
	repo := openEpisodes(t, memoryStore{})
	episode := episodeUsing("alex", "morning")
	episode.Script.Sections = []business.Section{
		{Heading: "Intro", Body: "hi"},
		{Heading: "Topic", Body: "news", Sources: []int{2, 1}},
	}
	episode.Chapters = []business.Chapter{{Title: "Intro"}, {Title: "Topic", Start: time.Second}}
	episode.Sources = []business.Source{
		{Title: "A", Link: "https://example.org/a", Name: "feed", Text: "text a", Retrieved: noon},
		{Title: "B", Text: "text b", Retrieved: noon},
	}
	saved, err := repo.Save(episode, []byte("a"))
	require.NoError(t, err)

	got, err := repo.Get("alex", saved.ID)

	require.NoError(t, err)
	assert.Equal(t, episode.Sources, got.Sources)
	assert.Empty(t, got.Script.Sections[0].Sources)
	assert.Equal(t, []int{1, 2}, got.Script.Sections[1].Sources, "numbers count in the sources, in order")
}

func TestDeletingTheEpisodeRemovesItsSources(t *testing.T) {
	repo := openEpisodes(t, memoryStore{})
	episode := episodeUsing("alex", "morning")
	episode.Script.Sections = []business.Section{{Heading: "Topic", Body: "news", Sources: []int{1}}}
	episode.Sources = []business.Source{{Title: "A", Text: "a", Retrieved: noon}}
	saved, err := repo.Save(episode, []byte("a"))
	require.NoError(t, err)

	require.NoError(t, repo.Delete("alex", saved.ID))

	var sources, links int
	require.NoError(t, repo.db.QueryRow(`SELECT (SELECT COUNT(*) FROM sources), (SELECT COUNT(*) FROM section_sources)`).Scan(&sources, &links))
	assert.Zero(t, sources+links)
}

func TestMigrationsRunOnANewDatabaseAndOnAnAlreadyMigratedOne(t *testing.T) {
	dir := t.TempDir()
	first, err := Open(dir)
	require.NoError(t, err, "a new database is migrated")
	_, err = first.Episodes(memoryStore{}).Save(episodeUsing("alex", "morning"), []byte("a"))
	require.NoError(t, err)
	require.NoError(t, first.Close())

	second, err := Open(dir)
	require.NoError(t, err, "an already migrated database is accepted")
	t.Cleanup(func() { _ = second.Close() })

	episodes, err := second.Episodes(memoryStore{}).List("alex", business.ListOpts{})
	require.NoError(t, err)
	assert.Len(t, episodes, 1)
}

func TestAnEpisodeWithMoreChaptersThanSectionsIsNotSaved(t *testing.T) {
	repo := openEpisodes(t, memoryStore{})
	episode := episodeUsing("alex", "morning")
	episode.Script.Sections = []business.Section{{Heading: "Only section"}}
	episode.Chapters = []business.Chapter{{Title: "Only section"}, {Title: "No section", Start: time.Second}}

	_, err := repo.Save(episode, []byte("a"))

	require.Error(t, err)
	episodes, err := repo.List("alex", business.ListOpts{})
	require.NoError(t, err)
	assert.Empty(t, episodes, "nothing of the Episode is kept")
}

// recordingStore remembers which audio was deleted.
type recordingStore struct {
	memoryStore
	deleted []string
}

func (r *recordingStore) Delete(location string) error {
	r.deleted = append(r.deleted, location)
	return nil
}

func TestDeletingBeforeATimeRemovesEveryOlderEpisodeOfEveryUserWithItsAudio(t *testing.T) {
	store := &recordingStore{}
	db, err := Open(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := db.Episodes(store)
	at := func(episode business.Episode, created time.Time) business.Episode {
		episode.Date = created
		return episode
	}
	oldReady, err := repo.Save(at(episodeUsing("alex", "morning", "https://example.org/a"), noon.Add(-48*time.Hour)), []byte("a"))
	require.NoError(t, err)
	_, err = repo.SaveFailed(at(episodeUsing("alex", "morning"), noon.Add(-48*time.Hour)), "model unreachable")
	require.NoError(t, err)
	oldOfAnother, err := repo.Save(at(episodeUsing("gone", "morning"), noon.Add(-72*time.Hour)), []byte("a"))
	require.NoError(t, err)
	young, err := repo.Save(at(episodeUsing("alex", "morning", "https://example.org/b"), noon.Add(-time.Hour)), []byte("a"))
	require.NoError(t, err)

	deleted, err := repo.DeleteBefore(noon.Add(-24 * time.Hour))

	require.NoError(t, err)
	assert.Equal(t, 3, deleted)
	left, err := repo.List("alex", business.ListOpts{})
	require.NoError(t, err)
	require.Len(t, left, 1)
	assert.Equal(t, young.ID, left[0].ID)
	gone, err := repo.List("gone", business.ListOpts{})
	require.NoError(t, err)
	assert.Empty(t, gone)
	assert.ElementsMatch(t, []string{oldReady.AudioLocation, oldOfAnother.AudioLocation}, store.deleted,
		"the audio of the deleted Episodes goes, the failed one has none")
	used, err := repo.UsedContent("alex", "morning")
	require.NoError(t, err)
	assert.Equal(t, []string{"https://example.org/b"}, used, "the Content of a deleted Episode counts as unused again")
}

func TestDeletingBeforeATimeWithNothingOlderDeletesNothing(t *testing.T) {
	repo := openEpisodes(t, memoryStore{})
	_, err := repo.Save(episodeUsing("alex", "morning"), []byte("a"))
	require.NoError(t, err)

	deleted, err := repo.DeleteBefore(noon)

	require.NoError(t, err)
	assert.Zero(t, deleted, "an Episode created exactly at the bound is not older")
}
