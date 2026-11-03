package database

import (
	"testing"
	"time"

	"github.com/alex0ptr/aigude/business"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openCache(t *testing.T) *CrawlCache {
	t.Helper()
	db, err := Open(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db.CrawlCache()
}

var noon = time.Date(2026, time.October, 29, 12, 0, 0, 0, time.UTC)

func TestCachedPageComesBackWhileItIsYoungEnough(t *testing.T) {
	cache := openCache(t)
	require.NoError(t, cache.Put("https://example.org/a", business.Page{Title: "T", Text: "Text"}, noon))

	page, found, err := cache.Get("https://example.org/a", noon.Add(-time.Hour))

	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, business.Page{Title: "T", Text: "Text", RetrievedAt: noon.UTC()}, page, "the page keeps the time it was retrieved")
}

func TestCachedPageOlderThanTheBoundCountsAsMissing(t *testing.T) {
	cache := openCache(t)
	require.NoError(t, cache.Put("https://example.org/a", business.Page{Text: "Text"}, noon))

	_, found, err := cache.Get("https://example.org/a", noon.Add(time.Second))

	require.NoError(t, err)
	assert.False(t, found)
}

func TestPuttingAPageAgainReplacesTheOldCopy(t *testing.T) {
	cache := openCache(t)
	require.NoError(t, cache.Put("https://example.org/a", business.Page{Text: "alt"}, noon))
	require.NoError(t, cache.Put("https://example.org/a", business.Page{Text: "neu"}, noon.Add(time.Hour)))

	page, found, err := cache.Get("https://example.org/a", noon.Add(30*time.Minute))

	require.NoError(t, err)
	require.True(t, found, "the new copy is as young as its retrieval")
	assert.Equal(t, "neu", page.Text)
}

func TestDeletingExpiredPagesKeepsTheYoungOnes(t *testing.T) {
	cache := openCache(t)
	require.NoError(t, cache.Put("https://example.org/old", business.Page{Text: "alt"}, noon.Add(-48*time.Hour)))
	require.NoError(t, cache.Put("https://example.org/also-old", business.Page{Text: "alt"}, noon.Add(-25*time.Hour)))
	require.NoError(t, cache.Put("https://example.org/young", business.Page{Text: "neu"}, noon.Add(-time.Hour)))

	deleted, err := cache.DeleteBefore(noon.Add(-24 * time.Hour))

	require.NoError(t, err)
	assert.Equal(t, 2, deleted)
	_, found, _ := cache.Get("https://example.org/young", noon.Add(-24*time.Hour))
	assert.True(t, found)
	_, found, _ = cache.Get("https://example.org/old", noon.Add(-72*time.Hour))
	assert.False(t, found)
}
