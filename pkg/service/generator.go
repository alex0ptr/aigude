package service

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/alex0ptr/aigude/business"
	"github.com/alex0ptr/aigude/pkg/userconfig"
)

// queueSize is how many requests may wait for the worker.
const queueSize = 64

// ErrQueueFull is returned when too many requests are waiting.
var ErrQueueFull = errors.New("too many Episodes are waiting to be produced")

// generationRequest asks for one Episode of a CastProfile.
type generationRequest struct {
	userID        string
	castProfileID string
}

// generator produces Episodes one at a time: requests wait in a queue that a single worker empties.
type generator struct {
	store    *userconfig.Store
	episodes *business.Episodes
	// crawler retrieves the pages that the items of the feeds link to.
	crawler business.Crawler
	clock   business.Clock
	queue   chan generationRequest

	// pending holds the requests that wait in the queue or are being worked on, so that a CastProfile is not queued
	// a second time.
	mu      sync.Mutex
	pending map[generationRequest]struct{}
	// nothingNew holds, for the CastProfiles whose latest production found no unused Content, when it looked.
	nothingNew map[generationRequest]time.Time
}

// newGenerator creates a generator that produces Episodes of the Users in store, retrieving pages through crawler.
func newGenerator(store *userconfig.Store, episodes *business.Episodes, crawler business.Crawler, clock business.Clock) *generator {
	return &generator{
		store: store, episodes: episodes, crawler: crawler, clock: clock,
		queue: make(chan generationRequest, queueSize), pending: make(map[generationRequest]struct{}),
		nothingNew: make(map[generationRequest]time.Time),
	}
}

// Generate puts a request for an Episode of the User's CastProfile in the queue. A CastProfile that is queued or
// being produced already is not queued again.
func (g *generator) Generate(userID, castProfileID string) error {
	request := generationRequest{userID: userID, castProfileID: castProfileID}
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, queued := g.pending[request]; queued {
		slog.Info("Episode requested although one is queued or being produced already", "user", userID, "profile", castProfileID)
		return nil
	}
	select {
	case g.queue <- request:
		g.pending[request] = struct{}{}
		slog.Info("Episode requested", "user", userID, "profile", castProfileID, "waiting", len(g.queue))
		return nil
	default:
		slog.Warn("Episode request rejected, the queue is full", "user", userID, "profile", castProfileID)
		return ErrQueueFull
	}
}

// Generating tells whether an Episode of the User's CastProfile is queued or being produced.
func (g *generator) Generating(userID, castProfileID string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	_, pending := g.pending[generationRequest{userID: userID, castProfileID: castProfileID}]
	return pending
}

// NothingNew tells whether the latest production for the User's CastProfile found no unused Content in its Max
// Lookback, and when it looked. A later production that makes an Episode, or fails, clears it.
func (g *generator) NothingNew(userID, castProfileID string) (time.Time, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	at, found := g.nothingNew[generationRequest{userID: userID, castProfileID: castProfileID}]
	return at, found
}

// remember records the outcome of a production for NothingNew.
func (g *generator) remember(r generationRequest, nothingNew bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if nothingNew {
		g.nothingNew[r] = g.clock.Now()
		return
	}
	delete(g.nothingNew, r)
}

// release takes a request that was worked on out of the pending ones.
func (g *generator) release(r generationRequest) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.pending, r)
}

// run works the queue until ctx is cancelled and returns a channel that is closed when the worker has stopped.
func (g *generator) run(ctx context.Context) <-chan struct{} {
	return background(func() {
		for {
			select {
			case <-ctx.Done():
				return
			case r := <-g.queue:
				g.handle(r)
			}
		}
	})
}

// handle makes the Episode of one request from the User Config in force now.
func (g *generator) handle(r generationRequest) {
	defer g.release(r)
	user, ok := g.store.Snapshot().User(r.userID)
	if !ok {
		slog.Warn("Skipping request of an unknown User", "user", r.userID)
		return
	}
	profile, ok := user.CastProfile(r.castProfileID)
	if !ok {
		slog.Warn("Skipping request of an unknown CastProfile", "user", r.userID, "profile", r.castProfileID)
		return
	}
	owner := business.User{ID: user.ID}
	_, err := g.episodes.Create(owner, castProfileOf(user, profile, g.crawler))
	g.remember(r, errors.Is(err, business.ErrNoNewContent))
}

// background runs work in a goroutine and returns a channel that is closed when it has returned.
func background(work func()) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		work()
	}()
	return done
}
