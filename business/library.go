package business

import (
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// ErrEpisodeFailed is returned when something is asked of an Episode that failed which only an Episode that is ready
// can do, such as being Listened.
var ErrEpisodeFailed = errors.New("episode failed")

// EpisodeLibrary is what a User does with the Episodes that were produced: look at the list, open one, say that they
// have heard it and delete it. It decides which Episodes belong to the User who asks; everything outside of business/
// reaches the stored Episodes only through it.
type EpisodeLibrary struct {
	repository EpisodeRepository
	audio      AudioStore
}

// NewEpisodeLibrary creates the library of the Episodes in the repository, whose audio is kept in the audio store.
func NewEpisodeLibrary(repository EpisodeRepository, audio AudioStore) *EpisodeLibrary {
	return &EpisodeLibrary{repository: repository, audio: audio}
}

// List returns the Episodes of the User, most recent first, as narrowed by opts.
func (l *EpisodeLibrary) List(userID string, opts ListOpts) ([]Episode, error) {
	return l.repository.List(userID, opts)
}

// Get returns the Episode of the User, whether it is ready or failed. An Episode of another User is
// ErrEpisodeNotFound.
func (l *EpisodeLibrary) Get(userID string, id int64) (Episode, error) {
	episode, err := l.repository.Get(userID, id)
	if err != nil {
		return Episode{}, err
	}
	if episode.UserID != userID {
		slog.Warn("Episode of another User asked for", "user", userID, "id", id, "owner", episode.UserID)
		return Episode{}, ErrEpisodeNotFound
	}
	return episode, nil
}

// GetReady returns the Episode of the User that has audio to open. One without audio, such as a failed one, is
// ErrEpisodeNotFound.
func (l *EpisodeLibrary) GetReady(userID string, id int64) (Episode, error) {
	episode, err := l.Get(userID, id)
	if err != nil {
		return Episode{}, err
	}
	if episode.AudioLocation == "" {
		slog.Debug("Episode has no audio, not found", "user", userID, "id", id)
		return Episode{}, ErrEpisodeNotFound
	}
	return episode, nil
}

// OpenAudio opens the audio of the ready Episode of the User, together with the time it was last modified. The caller
// closes it. The store is asked only for an Episode that GetReady returns, so audio of another User's Episode is
// ErrEpisodeNotFound like the Episode itself.
func (l *EpisodeLibrary) OpenAudio(userID string, id int64) (AudioReader, time.Time, error) {
	episode, err := l.GetReady(userID, id)
	if err != nil {
		return nil, time.Time{}, err
	}
	slog.Debug("Opening episode audio", "user", userID, "id", id, "location", episode.AudioLocation)
	audio, modified, err := l.audio.Open(episode.AudioLocation)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("open audio of episode %d: %w", id, err)
	}
	return audio, modified, nil
}

// GetFailed returns the Episode of the User that failed. One that did not fail is ErrEpisodeNotFound.
func (l *EpisodeLibrary) GetFailed(userID string, id int64) (Episode, error) {
	episode, err := l.Get(userID, id)
	if err != nil {
		return Episode{}, err
	}
	if episode.Failure == "" {
		slog.Debug("Episode did not fail, not found among the failed", "user", userID, "id", id)
		return Episode{}, ErrEpisodeNotFound
	}
	return episode, nil
}

// SetListened sets whether the Episode of the User is Listened and returns the Episode with that state. Setting the
// state it already has changes nothing. An Episode that failed has no such state: ErrEpisodeFailed.
func (l *EpisodeLibrary) SetListened(userID string, id int64, listened bool) (Episode, error) {
	episode, err := l.Get(userID, id)
	if err != nil {
		return Episode{}, err
	}
	if episode.Failure != "" {
		slog.Debug("Listened state of an Episode that failed refused", "user", userID, "id", id)
		return Episode{}, ErrEpisodeFailed
	}
	if err := l.repository.SetListened(userID, id, listened); err != nil {
		return Episode{}, err
	}
	episode.Listened = listened
	return episode, nil
}

// Delete removes the Episode of the User together with its audio.
func (l *EpisodeLibrary) Delete(userID string, id int64) error {
	if _, err := l.Get(userID, id); err != nil {
		return err
	}
	return l.repository.Delete(userID, id)
}

// DeleteBefore removes the Episodes of every User that were created before t, ready or failed, together with their
// audio, and returns how many it removed.
func (l *EpisodeLibrary) DeleteBefore(t time.Time) (int, error) {
	return l.repository.DeleteBefore(t)
}
