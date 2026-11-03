package userconfig_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alex0ptr/aigude/pkg/userconfig"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// watchedStore opens a Store on dir and follows it until the test ends.
func watchedStore(t *testing.T, dir string) *userconfig.Store {
	t.Helper()
	store, err := userconfig.Open(dir)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	done, err := store.Watch(ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return store
}

func writeFile(t *testing.T, dir, name, text string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644))
}

// expectSignal fails unless the channel signals soon.
func expectSignal(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("no change notification for %s", what)
	}
}

func TestOpenFailsOnAnInvalidFile(t *testing.T) {
	dir := writeFiles(t, map[string]string{"alex.toml": minimal + "colour = \"blue\"\n"})

	_, err := userconfig.Open(dir)

	var problems userconfig.Problems
	require.ErrorAs(t, err, &problems)
}

func TestAConfigurationChangeNotifiesSubscribers(t *testing.T) {
	dir := writeFiles(t, map[string]string{"alex.toml": minimal})
	store := watchedStore(t, dir)
	changes, cancel := store.Subscribe()
	defer cancel()

	writeFile(t, dir, "bea.toml", strings.Replace(minimal, "alex", "bea", 1))

	expectSignal(t, changes, "a new file")
	_, ok := store.Snapshot().User("bea")
	assert.True(t, ok, "the notification arrives after the new Snapshot is in force")
}

func TestBecomingInvalidAndBeingFixedNotifySubscribers(t *testing.T) {
	dir := writeFiles(t, map[string]string{"alex.toml": minimal})
	store := watchedStore(t, dir)
	changes, cancel := store.Subscribe()
	defer cancel()

	writeFile(t, dir, "alex.toml", minimal+"colour = \"blue\"\n")
	expectSignal(t, changes, "the file turning invalid")
	assert.Len(t, store.Snapshot().Problems(), 1)

	writeFile(t, dir, "alex.toml", minimal)
	expectSignal(t, changes, "the file being fixed")
	assert.Empty(t, store.Snapshot().Problems())
}

func TestRewritingAFileWithTheSameContentIsNoChange(t *testing.T) {
	dir := writeFiles(t, map[string]string{"alex.toml": minimal})
	store := watchedStore(t, dir)
	changes, cancel := store.Subscribe()
	defer cancel()

	writeFile(t, dir, "alex.toml", minimal)
	writeFile(t, dir, ".alex.toml.swp", "editor noise")
	store.Reload()

	select {
	case <-changes:
		t.Fatal("notified although nothing changed")
	case <-time.After(300 * time.Millisecond):
	}
}

func TestSeveralChangesBeforeTheReceiverLooksGiveOneSignal(t *testing.T) {
	dir := writeFiles(t, map[string]string{"alex.toml": minimal})
	store := watchedStore(t, dir)
	changes, cancel := store.Subscribe()
	defer cancel()

	for _, id := range []string{"bea", "carl", "dora"} {
		writeFile(t, dir, id+".toml", strings.Replace(minimal, "alex", id, 1))
		store.Reload()
	}

	expectSignal(t, changes, "the first change")
	assert.Len(t, store.Snapshot().Users(), 4, "the receiver finds the newest Snapshot")
	select {
	case <-changes:
		t.Fatal("more than one pending signal")
	default:
	}
}

func TestACancelledSubscriberIsNotNotified(t *testing.T) {
	dir := writeFiles(t, map[string]string{"alex.toml": minimal})
	store := watchedStore(t, dir)
	changes, cancel := store.Subscribe()
	cancel()

	writeFile(t, dir, "bea.toml", strings.Replace(minimal, "alex", "bea", 1))
	store.Reload()

	select {
	case <-changes:
		t.Fatal("notified after cancel")
	default:
	}
}

func TestASnapshotObtainedBeforeAChangeKeepsSeeingTheOldConfig(t *testing.T) {
	dir := writeFiles(t, map[string]string{"alex.toml": minimal})
	store := watchedStore(t, dir)
	changes, cancel := store.Subscribe()
	defer cancel()
	before := store.Snapshot()

	writeFile(t, dir, "alex.toml", strings.Replace(minimal, `"Alex"`, `"Alex Renamed"`, 1))
	writeFile(t, dir, "bea.toml", strings.Replace(minimal, "alex", "bea", 1))
	expectSignal(t, changes, "the edit")
	require.Eventually(t, func() bool { return len(store.Snapshot().Users()) == 2 }, 5*time.Second, 10*time.Millisecond)

	oldAlex, ok := before.User("alex")
	require.True(t, ok)
	assert.Equal(t, "Alex", oldAlex.DisplayName)
	assert.Len(t, before.Users(), 1)
	newAlex, _ := store.Snapshot().User("alex")
	assert.Equal(t, "Alex Renamed", newAlex.DisplayName)
}

func TestAnInvalidFileKeepsItsLastValidUser(t *testing.T) {
	dir := writeFiles(t, map[string]string{"alex.toml": minimal})
	store := watchedStore(t, dir)

	writeFile(t, dir, "alex.toml", "[user\n")
	store.Reload()

	alex, ok := store.Snapshot().User("alex")
	require.True(t, ok)
	assert.Equal(t, "Alex", alex.DisplayName)
	require.Len(t, store.Snapshot().Problems(), 1)
	assert.Equal(t, filepath.Join(dir, "alex.toml"), store.Snapshot().Problems()[0].File)
}

func TestARemovedInvalidFileTakesUserAndProblemWithIt(t *testing.T) {
	dir := writeFiles(t, map[string]string{"alex.toml": minimal})
	store := watchedStore(t, dir)
	writeFile(t, dir, "alex.toml", "[user\n")
	store.Reload()

	require.NoError(t, os.Remove(filepath.Join(dir, "alex.toml")))
	store.Reload()

	assert.Empty(t, store.Snapshot().Users())
	assert.Empty(t, store.Snapshot().Problems())
}

func TestANewFileThatSortsEarlierCannotPushOutTheUserWhoHasTheIdAlready(t *testing.T) {
	dir := writeFiles(t, map[string]string{"b.toml": minimal})
	store := watchedStore(t, dir)

	writeFile(t, dir, "a.toml", strings.Replace(minimal, `"Alex"`, `"Intruder"`, 1))
	store.Reload()

	alex, _ := store.Snapshot().User("alex")
	assert.Equal(t, "Alex", alex.DisplayName, "the file that held the Id first keeps it")
	require.Len(t, store.Snapshot().Problems(), 1)
	assert.Equal(t, filepath.Join(dir, "a.toml"), store.Snapshot().Problems()[0].File)
}

func TestReloadKeepsTheCurrentConfigWhenTheFolderCannotBeRead(t *testing.T) {
	dir := writeFiles(t, map[string]string{"alex.toml": minimal})
	store, err := userconfig.Open(dir)
	require.NoError(t, err)

	require.NoError(t, os.RemoveAll(dir))
	store.Reload()

	_, ok := store.Snapshot().User("alex")
	assert.True(t, ok)
}

func TestWatchFailsWhenTheFolderIsGone(t *testing.T) {
	dir := writeFiles(t, map[string]string{"alex.toml": minimal})
	store, err := userconfig.Open(dir)
	require.NoError(t, err)
	require.NoError(t, os.RemoveAll(dir))

	_, err = store.Watch(context.Background())

	require.Error(t, err)
}
