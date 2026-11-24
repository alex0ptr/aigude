package business

import (
	"errors"
	"log/slog"
)

// ErrCastProfileNotFound is returned when the User has no CastProfile with the Id, because there never was one or
// because it was removed from the User Config.
var ErrCastProfileNotFound = errors.New("cast profile not found")

// ErrAlreadyProducing is returned when an Episode of the CastProfile is queued or being produced already.
var ErrAlreadyProducing = errors.New("episode is being produced")

// ProductionQueue queues the production of Episodes, which are produced one at a time when their turn comes. Productions
// is one.
type ProductionQueue interface {
	// Generate asks for an Episode of the User's CastProfile.
	Generate(userID, castProfileID string) error
	// Generating tells whether an Episode of the User's CastProfile is queued or being produced.
	Generating(userID, castProfileID string) bool
}

// EpisodeRequests is how a User asks for Episodes: for a new one of a CastProfile, or for another try after one
// failed. It decides whether the request is allowed; the production itself is the business of the ProductionQueue.
type EpisodeRequests struct {
	queue    ProductionQueue
	profiles CastProfiles
	library  *EpisodeLibrary
}

// NewEpisodeRequests creates the requests for the CastProfiles, queueing their production and finding the failed
// Episodes in the library.
func NewEpisodeRequests(queue ProductionQueue, profiles CastProfiles, library *EpisodeLibrary) *EpisodeRequests {
	return &EpisodeRequests{queue: queue, profiles: profiles, library: library}
}

// Request asks for an Episode of the User's CastProfile. A CastProfile the User does not have is
// ErrCastProfileNotFound. What the queue answers, such as that it is full, is returned as it is.
func (r *EpisodeRequests) Request(userID, castProfileID string) error {
	if !r.profiles.Exists(userID, castProfileID) {
		slog.Debug("Episode requested for a CastProfile the User does not have", "user", userID, "profile", castProfileID)
		return ErrCastProfileNotFound
	}
	return r.queue.Generate(userID, castProfileID)
}

// Retry asks for a new Episode in place of the failed Episode of the User. Only once the request is queued is the
// failed Episode removed, so that the new attempt takes its place; if that cannot be done it is logged and the request
// stands. An Episode of another User, or one that did not fail, is ErrEpisodeNotFound; a CastProfile that was removed
// since is ErrCastProfileNotFound and one that is being produced right now is ErrAlreadyProducing. Nothing is
// removed in any of these cases.
func (r *EpisodeRequests) Retry(userID string, episodeID int64) error {
	episode, err := r.library.GetFailed(userID, episodeID)
	if err != nil {
		return err
	}
	if !r.profiles.Exists(userID, episode.CastProfileID) {
		slog.Warn("Retry of an Episode whose CastProfile is gone", "user", userID, "id", episodeID, "profile", episode.CastProfileID)
		return ErrCastProfileNotFound
	}
	if r.queue.Generating(userID, episode.CastProfileID) {
		slog.Info("Retry refused, an Episode of the CastProfile is being produced", "user", userID, "id", episodeID)
		return ErrAlreadyProducing
	}
	if err := r.queue.Generate(userID, episode.CastProfileID); err != nil {
		slog.Error("Retry request failed", "user", userID, "id", episodeID, "error", err)
		return err
	}
	if err := r.library.Delete(userID, episodeID); err != nil {
		slog.Error("Removing the failed Episode after a retry failed", "user", userID, "id", episodeID, "error", err)
	}
	return nil
}
