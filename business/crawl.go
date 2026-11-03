package business

import (
	"log/slog"
	"time"
)

// CrawlCache keeps the pages a Crawler retrieved by their address. It is shared by all Users: a page costs
// something to retrieve, whoever wants it.
type CrawlCache interface {
	// Get returns the page stored for the address if it was retrieved at or after notBefore.
	Get(url string, notBefore time.Time) (page Page, found bool, err error)
	// Put stores the page, replacing the one stored for the address before.
	Put(url string, page Page, retrievedAt time.Time) error
	// DeleteBefore removes the pages retrieved before the time and returns how many there were.
	DeleteBefore(t time.Time) (int, error)
}

// CachingCrawler is a Crawler that retrieves a page through another Crawler only when the cache holds no copy that is
// younger than the time to live. A cache that fails costs a retrieval, never the Episode.
type CachingCrawler struct {
	crawler Crawler
	cache   CrawlCache
	clock   Clock
	ttl     time.Duration
}

var _ Crawler = (*CachingCrawler)(nil)

// NewCachingCrawler creates a Crawler that serves pages from the cache for as long as they are younger than ttl.
func NewCachingCrawler(crawler Crawler, cache CrawlCache, clock Clock, ttl time.Duration) *CachingCrawler {
	return &CachingCrawler{crawler: crawler, cache: cache, clock: clock, ttl: ttl}
}

// Crawl returns the page from the cache, or retrieves it and keeps it.
func (c *CachingCrawler) Crawl(url string) (Page, Usage, error) {
	now := c.clock.Now()
	page, found, err := c.cache.Get(url, now.Add(-c.ttl))
	if err != nil {
		slog.Warn("Crawl cache could not be read, retrieving the page", "url", url, "error", err)
	}
	if found {
		slog.Debug("Page served from the crawl cache", "url", url)
		return page, nil, nil
	}

	page, usage, err := c.crawler.Crawl(url)
	if err != nil {
		return Page{}, usage, err
	}
	page.RetrievedAt = now
	if err := c.cache.Put(url, page, now); err != nil {
		slog.Warn("Crawl cache could not keep the page", "url", url, "error", err)
	}
	return page, usage, nil
}

// DeleteExpired removes the pages that are older than the time to live from the cache.
func (c *CachingCrawler) DeleteExpired() error {
	deleted, err := c.cache.DeleteBefore(c.clock.Now().Add(-c.ttl))
	if err != nil {
		return err
	}
	slog.Info("Expired pages deleted from the crawl cache", "deleted", deleted)
	return nil
}
