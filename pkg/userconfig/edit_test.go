package userconfig_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alex0ptr/aigude/pkg/userconfig"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const commented = minimal + "\n# my favourite show\n"

func TestFileOfAUserIsItsTextWithCommentsAndAHash(t *testing.T) {
	dir := writeFiles(t, map[string]string{"alex.toml": commented})
	store := watchedStore(t, dir)

	file, err := store.File("alex")

	require.NoError(t, err)
	assert.Equal(t, commented, file.Text)
	assert.Equal(t, "alex.toml", file.Name)
	assert.NotEmpty(t, file.Hash)
	_, err = store.File("nobody")
	assert.ErrorIs(t, err, userconfig.ErrUnknownUser)
}

func TestSavingValidTextWritesTheFileAndTakesEffectAtOnce(t *testing.T) {
	dir := writeFiles(t, map[string]string{"alex.toml": commented})
	store := watchedStore(t, dir)
	changes, cancel := store.Subscribe()
	defer cancel()
	file, err := store.File("alex")
	require.NoError(t, err)
	edited := strings.Replace(commented, `display_name = "Alex"`, `display_name = "Alex Renamed"`, 1) + "# another comment\n"
	require.NotEqual(t, commented, edited)

	require.NoError(t, store.Save("alex", edited, file.Hash))

	user, _ := store.Snapshot().User("alex")
	assert.Equal(t, "Alex Renamed", user.DisplayName, "in force when Save returns")
	onDisk, err := os.ReadFile(filepath.Join(dir, "alex.toml"))
	require.NoError(t, err)
	assert.Equal(t, edited, string(onDisk), "comments and order are kept")
	expectSignal(t, changes, "a saved file")
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, entries, 1, "no temporary file stays behind")
}

func TestSavingInvalidTextReturnsProblemsWithPositionsAndWritesNothing(t *testing.T) {
	dir := writeFiles(t, map[string]string{"alex.toml": commented})
	store := watchedStore(t, dir)
	file, err := store.File("alex")
	require.NoError(t, err)

	err = store.Save("alex", commented+"colour = \"blue\"\n", file.Hash)

	var problems userconfig.Problems
	require.ErrorAs(t, err, &problems)
	require.NotEmpty(t, problems)
	assert.NotZero(t, problems[0].Line)
	assert.NotZero(t, problems[0].Column)
	onDisk, _ := os.ReadFile(filepath.Join(dir, "alex.toml"))
	assert.Equal(t, commented, string(onDisk))
}

func TestSavingOverAChangeMadeByAnotherHandIsAConflict(t *testing.T) {
	dir := writeFiles(t, map[string]string{"alex.toml": commented})
	store := watchedStore(t, dir)
	file, err := store.File("alex")
	require.NoError(t, err)
	outside := commented + "# written by someone else\n"
	writeFile(t, dir, "alex.toml", outside)

	err = store.Save("alex", commented+"# my edit\n", file.Hash)

	assert.ErrorIs(t, err, userconfig.ErrConflict)
	onDisk, _ := os.ReadFile(filepath.Join(dir, "alex.toml"))
	assert.Equal(t, outside, string(onDisk))
}

func TestAUserIdCannotBeChangedOrTakenFromAnotherUser(t *testing.T) {
	dir := writeFiles(t, map[string]string{"alex.toml": minimal, "bea.toml": strings.Replace(minimal, "alex", "bea", 1)})
	store := watchedStore(t, dir)
	file, err := store.File("alex")
	require.NoError(t, err)

	renamed := store.Save("alex", strings.Replace(minimal, `id = "alex"`, `id = "carl"`, 1), file.Hash)
	taken := store.Save("alex", strings.Replace(minimal, `id = "alex"`, `id = "bea"`, 1), file.Hash)

	var problems userconfig.Problems
	assert.ErrorAs(t, renamed, &problems)
	assert.ErrorAs(t, taken, &problems)
	_, stillAlex := store.Snapshot().User("alex")
	assert.True(t, stillAlex)
}
