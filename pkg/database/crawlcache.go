package database

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/alex0ptr/aigude/business"
)

// CrawlCache implements business.CrawlCache in SQLite.
type CrawlCache struct {
	db *sql.DB
}

var _ business.CrawlCache = (*CrawlCache)(nil)

// Get returns the page stored for the address when it was retrieved at or after notBefore.
func (c *CrawlCache) Get(url string, notBefore time.Time) (business.Page, bool, error) {
	var page business.Page
	var retrieved int64
	err := c.db.QueryRow(`SELECT title, body, retrieved_at FROM crawled_pages WHERE url = ? AND retrieved_at >= ?`,
		url, notBefore.UnixNano()).Scan(&page.Title, &page.Text, &retrieved)
	if errors.Is(err, sql.ErrNoRows) {
		slog.Debug("Crawl cache has no page young enough", "url", url, "notBefore", notBefore.Format(time.DateOnly))
		return business.Page{}, false, nil
	}
	if err != nil {
		return business.Page{}, false, fmt.Errorf("read crawled page %s: %w", url, err)
	}
	page.RetrievedAt = time.Unix(0, retrieved).UTC()
	slog.Debug("Crawl cache holds the page", "url", url)
	return page, true, nil
}

// Put stores the page under its address, replacing an older copy.
func (c *CrawlCache) Put(url string, page business.Page, retrievedAt time.Time) error {
	slog.Debug("Storing crawled page", "url", url, "characters", len(page.Text))
	_, err := c.db.Exec(`INSERT INTO crawled_pages (url, title, body, retrieved_at) VALUES (?, ?, ?, ?)
		ON CONFLICT (url) DO UPDATE SET title = excluded.title, body = excluded.body, retrieved_at = excluded.retrieved_at`,
		url, page.Title, page.Text, retrievedAt.UnixNano())
	if err != nil {
		return fmt.Errorf("store crawled page %s: %w", url, err)
	}
	return nil
}

// DeleteBefore removes the pages retrieved before the time.
func (c *CrawlCache) DeleteBefore(t time.Time) (int, error) {
	res, err := c.db.Exec(`DELETE FROM crawled_pages WHERE retrieved_at < ?`, t.UnixNano())
	if err != nil {
		return 0, fmt.Errorf("delete expired crawled pages: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("delete expired crawled pages: %w", err)
	}
	slog.Debug("Deleted crawled pages", "before", t.Format(time.DateOnly), "deleted", n)
	return int(n), nil
}
