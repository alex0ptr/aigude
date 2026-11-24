package business

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

// queueSize is how many requests may wait for the worker.
const queueSize = 64

// ErrQueueFull is returned when too many requests are waiting.
var ErrQueueFull = errors.New("too many Episodes are waiting to be produced")

// EpisodeMaker produces the Episode of a CastProfile of a User and stores it. Episodes is one.
type EpisodeMaker interface {
	Create(user User, profile CastProfile) (Episode, error)
}

// profileKey names a CastProfile of a User.
type profileKey struct {
	userID        string
	castProfileID string
}

// Productions produce Episodes one at a time: requests wait in a queue that a single worker empties. A CastProfile
// that is queued or being produced is not queued a second time, and Productions remember for which CastProfiles the
// latest production found nothing new.
type Productions struct {
	maker    EpisodeMaker
	profiles CastProfileSource
	clock    Clock
	queue    chan profileKey

	// pending holds the requests that wait in the queue or are being worked on, so that a CastProfile is not queued
	// a second time.
	mu      sync.Mutex
	pending map[profileKey]struct{}
	// nothingNew holds, for the CastProfiles whose latest production found no unused Content, when it looked.
	nothingNew map[profileKey]time.Time
}

// NewProductions creates the productions of the Episodes of the CastProfiles that profiles hands out.
func NewProductions(maker EpisodeMaker, profiles CastProfileSource, clock Clock) *Productions {
	return &Productions{
		maker: maker, profiles: profiles, clock: clock,
		queue: make(chan profileKey, queueSize), pending: make(map[profileKey]struct{}),
		nothingNew: make(map[profileKey]time.Time),
	}
}

// Generate puts a request for an Episode of the User's CastProfile in the queue. A CastProfile that is queued or
// being produced already is not queued again. A full queue is ErrQueueFull.
func (p *Productions) Generate(userID, castProfileID string) error {
	request := profileKey{userID: userID, castProfileID: castProfileID}
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, queued := p.pending[request]; queued {
		slog.Info("Episode requested although one is queued or being produced already", "user", userID, "profile", castProfileID)
		return nil
	}
	select {
	case p.queue <- request:
		p.pending[request] = struct{}{}
		slog.Info("Episode requested", "user", userID, "profile", castProfileID, "waiting", len(p.queue))
		return nil
	default:
		slog.Warn("Episode request rejected, the queue is full", "user", userID, "profile", castProfileID)
		return ErrQueueFull
	}
}

// Generating tells whether an Episode of the User's CastProfile is queued or being produced.
func (p *Productions) Generating(userID, castProfileID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, pending := p.pending[profileKey{userID: userID, castProfileID: castProfileID}]
	return pending
}

// NothingNew tells whether the latest production for the User's CastProfile found no unused Content in its Max
// Lookback, and when it looked. A later production that makes an Episode, or fails, clears it.
func (p *Productions) NothingNew(userID, castProfileID string) (time.Time, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	at, found := p.nothingNew[profileKey{userID: userID, castProfileID: castProfileID}]
	return at, found
}

// remember records the outcome of a production for NothingNew.
func (p *Productions) remember(r profileKey, nothingNew bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if nothingNew {
		p.nothingNew[r] = p.clock.Now()
		return
	}
	delete(p.nothingNew, r)
}

// release takes a request that was worked on out of the pending ones.
func (p *Productions) release(r profileKey) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.pending, r)
}

// Run works the queue until ctx is cancelled and returns a channel that is closed when the worker has stopped.
func (p *Productions) Run(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-ctx.Done():
				slog.Debug("Production worker stops")
				return
			case r := <-p.queue:
				p.handle(r)
			}
		}
	}()
	return done
}

// handle makes the Episode of one request from the CastProfile as the User defines it now. A request for a User or
// a CastProfile that is not there any more is skipped.
func (p *Productions) handle(r profileKey) {
	defer p.release(r)
	user, profile, ok := p.profiles.CastProfile(r.userID, r.castProfileID)
	if !ok {
		slog.Warn("Skipping request for a User or CastProfile that is not there", "user", r.userID, "profile", r.castProfileID)
		return
	}
	_, err := p.maker.Create(user, profile)
	p.remember(r, errors.Is(err, ErrNoNewContent))
}
