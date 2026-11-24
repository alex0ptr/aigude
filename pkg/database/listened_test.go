package database

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/alex0ptr/aigude/business"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTheMigrationMarksEveryReadyEpisodeThatExistsAsListenedAndLeavesFailedOnesAlone(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), fileName))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	migrations, err := fs.Sub(migrationsFS, "migrations")
	require.NoError(t, err)
	provider, err := goose.NewProvider(goose.DialectSQLite3, db, migrations)
	require.NoError(t, err)
	_, err = provider.UpTo(context.Background(), 1)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO episodes (episode_id, user_id, cast_profile_id, created_at, title, failure) VALUES
		(1, 'alex', 'morning', 1, 'Ready', NULL), (2, 'alex', 'morning', 2, '', 'boom')`)
	require.NoError(t, err)

	require.NoError(t, migrate(db))

	listened := map[int64]bool{}
	rows, err := db.Query(`SELECT episode_id, listened FROM episodes`)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id int64
		var value bool
		require.NoError(t, rows.Scan(&id, &value))
		listened[id] = value
	}
	assert.Equal(t, map[int64]bool{1: true, 2: false}, listened)
}

func TestANewEpisodeIsUnlistenedAndTheStateIsKeptPerEpisodeAndUser(t *testing.T) {
	repo := openEpisodes(t, memoryStore{})
	mine, err := repo.Save(episodeUsing("alex", "morning"), []byte("a"))
	require.NoError(t, err)
	other, err := repo.Save(episodeUsing("alex", "morning"), []byte("b"))
	require.NoError(t, err)
	failed, err := repo.SaveFailed(episodeUsing("alex", "morning"), "boom")
	require.NoError(t, err)

	require.NoError(t, repo.SetListened("alex", mine.ID, true))

	all, err := repo.List("alex", business.ListOpts{})
	require.NoError(t, err)
	assert.Equal(t, map[int64]bool{mine.ID: true, other.ID: false, failed.ID: false}, listenedByID(all))
	unlistened, err := repo.List("alex", business.ListOpts{UnlistenedOnly: true})
	require.NoError(t, err)
	assert.Equal(t, map[int64]bool{other.ID: false, failed.ID: false}, listenedByID(unlistened), "failed Episodes stay in the narrowed list")
	got, err := repo.Get("alex", mine.ID)
	require.NoError(t, err)
	assert.True(t, got.Listened)
	assert.ErrorIs(t, repo.SetListened("bea", mine.ID, false), business.ErrEpisodeNotFound, "another User cannot reach it")
	assert.ErrorIs(t, repo.SetListened("alex", 999, true), business.ErrEpisodeNotFound)
	require.NoError(t, repo.SetListened("alex", mine.ID, true), "the state it has is not an error")
}

func listenedByID(episodes []business.Episode) map[int64]bool {
	states := make(map[int64]bool, len(episodes))
	for _, e := range episodes {
		states[e.ID] = e.Listened
	}
	return states
}
