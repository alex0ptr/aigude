// Package database is the SQLite database of AI Gude. It holds only recorded state: what was produced and
// retrieved, never configuration. Schema changes are goose migrations embedded in the binary and run on Open.
package database

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"path/filepath"

	"github.com/alex0ptr/aigude/business"
	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite" // pure Go driver, registers "sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// fileName is the database file inside the data folder.
const fileName = "aigude.db"

// Database is the opened and migrated SQLite database.
type Database struct {
	db *sql.DB
}

// Open opens the database in the data folder, creating it when it does not exist, and runs the migrations that have
// not been applied yet.
func Open(dataDir string) (*Database, error) {
	path := filepath.Join(dataDir, fileName)
	slog.Debug("Opening database with foreign keys enforced on every connection of the pool", "path", path)
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("open database %s: %w", path, err)
	}
	if err := migrate(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate database %s: %w", path, err)
	}
	return &Database{db: db}, nil
}

// migrate applies the embedded migrations that are still missing.
func migrate(db *sql.DB) error {
	migrations, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		return err
	}
	provider, err := goose.NewProvider(goose.DialectSQLite3, db, migrations)
	if err != nil {
		return err
	}
	results, err := provider.Up(context.Background())
	if err != nil {
		return err
	}
	slog.Info("Database migrated", "applied", len(results))
	return nil
}

// Close closes the database.
func (d *Database) Close() error {
	slog.Debug("Closing database")
	return d.db.Close()
}

// Episodes returns the repository of produced Episodes, which keeps their audio in the audio store.
func (d *Database) Episodes(audio business.AudioStore) *EpisodeRepository {
	return &EpisodeRepository{db: d.db, audio: audio}
}

// CrawlCache returns the cache of retrieved pages.
func (d *Database) CrawlCache() *CrawlCache {
	return &CrawlCache{db: d.db}
}
