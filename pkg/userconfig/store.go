package userconfig

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
)

// reloadDelay is how long the Store waits after the last file event before it
// reads the folder again. Editors and tools write a file in several steps
// (temp file, rename, chmod); this folds them into one reload.
const reloadDelay = 50 * time.Millisecond

// Store holds the User Config of the running service. It starts from a
// strictly valid folder and then follows the folder: the current Snapshot is
// replaced as a whole whenever the files change, so anyone who fetched a
// Snapshot keeps working with it unchanged.
//
// A file that turns invalid leaves its User in place with the last valid setup
// of that file; the problem is reported through Snapshot.Problems. A file that disappears takes its User with it.
type Store struct {
	dir     string
	current atomic.Pointer[Snapshot]

	mu          sync.Mutex
	lastValid   map[string]User
	subscribers map[int]chan struct{}
	nextSubID   int
}

// Open reads dir like LoadDir does and fails with Problems when any file is
// invalid. This is the strict start; after it, reloads are forgiving.
func Open(dir string) (*Store, error) {
	snapshot, userByFile, err := loadStrict(dir)
	if err != nil {
		return nil, err
	}
	s := &Store{dir: dir, lastValid: userByFile, subscribers: map[int]chan struct{}{}}
	s.current.Store(snapshot)
	slog.Info("User Config loaded", "dir", dir, "users", len(snapshot.Users()))
	return s, nil
}

// Snapshot returns the User Config in force. Fetch it once per piece of
// work: a Snapshot is immutable, a later call may return a newer one.
func (s *Store) Snapshot() *Snapshot {
	return s.current.Load()
}

// Subscribe returns a channel that receives a signal whenever the User Config
// changes, and a function to stop listening. Signals coalesce: if several changes happen before the receiver looks,
// it gets one signal and finds the newest Snapshot.
func (s *Store) Subscribe() (changes <-chan struct{}, cancel func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.nextSubID
	s.nextSubID++
	ch := make(chan struct{}, 1)
	s.subscribers[id] = ch
	slog.Debug("User Config subscriber added", "subscribers", len(s.subscribers))
	return ch, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		delete(s.subscribers, id)
	}
}

// Reload reads the folder again and swaps in the result. When the folder
// cannot be read at all, the current Snapshot stays. Subscribers are told
// only when something actually differs.
func (s *Store) Reload() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reloadLocked()
}

// reloadLocked is Reload for a caller that holds the lock.
func (s *Store) reloadLocked() {
	results, err := readFiles(s.dir)
	if err != nil {
		slog.Error("Reloading User Config failed, keeping the current one", "error", err)
		return
	}
	users, problems, userByFile := assemble(results, s.lastValid)
	next := newSnapshot(users, problems)
	s.lastValid = userByFile

	if prev := s.current.Load(); prev != nil && sameContent(prev, next) {
		slog.Debug("User Config unchanged after reload")
		return
	}
	s.current.Store(next)
	slog.Info("User Config reloaded", "users", len(users), "problems", len(problems))
	for _, p := range problems {
		slog.Warn("User Config problem", "problem", p.String())
	}
	for _, ch := range s.subscribers {
		select {
		case ch <- struct{}{}:
		default:
			slog.Debug("Subscriber has a signal pending already")
		}
	}
}

// sameContent says whether two Snapshots hold the same Users and problems.
func sameContent(a, b *Snapshot) bool {
	return reflect.DeepEqual(a.users, b.users) && reflect.DeepEqual(a.problems, b.problems)
}

// Watch follows the folder until ctx is cancelled and reloads on every change
// to a config file, whatever editor made it. It returns once the folder is
// being watched, so changes made after it returns are not missed; the channel
// it returns is closed when watching has ended.
func (s *Store) Watch(ctx context.Context) (done <-chan struct{}, err error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("watch users folder: %w", err)
	}
	if err := w.Add(s.dir); err != nil {
		_ = w.Close()
		return nil, fmt.Errorf("watch users folder %s: %w", s.dir, err)
	}
	slog.Info("Watching users folder", "dir", s.dir)
	slog.Debug("Catching up on changes made before the watch started")
	s.Reload()

	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		defer func() { _ = w.Close() }()
		s.follow(ctx, w)
	}()
	return stopped, nil
}

// follow turns file events into reloads, one reload per burst of events.
func (s *Store) follow(ctx context.Context, w *fsnotify.Watcher) {
	timer := time.NewTimer(reloadDelay)
	timer.Stop()
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			slog.Info("Stopped watching users folder")
			return
		case ev, ok := <-w.Events:
			if !ok {
				slog.Error("File events of the users folder ended, changes are no longer followed")
				return
			}
			if s.isConfigEvent(ev) {
				slog.Debug("User Config file event", "event", ev.String())
				timer.Reset(reloadDelay)
			}
		case err, ok := <-w.Errors:
			if !ok {
				slog.Error("File errors of the users folder ended, changes are no longer followed")
				return
			}
			slog.Error("Watching users folder failed", "error", err)
		case <-timer.C:
			s.Reload()
		}
	}
}

// isConfigEvent says whether a file event can change the User Config: a
// change to a config file, or the folder itself going away.
func (s *Store) isConfigEvent(ev fsnotify.Event) bool {
	if ev.Op == fsnotify.Chmod {
		return false
	}
	if ev.Name == s.dir {
		slog.Warn("Users folder was removed or renamed", "dir", s.dir, "event", ev.String())
		return true
	}
	name := filepath.Base(ev.Name)
	return strings.HasSuffix(name, ".toml") && !strings.HasPrefix(name, ".")
}
