package business

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// handingOut is an EpisodeRepository that hands out every Episode it holds to whoever asks, so that the rules of
// the EpisodeLibrary are tested without help from the repository.
type handingOut struct {
	EpisodeRepository
	episodes map[int64]Episode
	listened map[int64]bool
	deleted  []int64
}

func newHandingOut(episodes ...Episode) *handingOut {
	h := &handingOut{episodes: map[int64]Episode{}, listened: map[int64]bool{}}
	for _, e := range episodes {
		h.episodes[e.ID] = e
	}
	return h
}

func (h *handingOut) Get(_ string, id int64) (Episode, error) {
	e, ok := h.episodes[id]
	if !ok {
		return Episode{}, ErrEpisodeNotFound
	}
	return e, nil
}

func (h *handingOut) SetListened(_ string, id int64, listened bool) error {
	h.listened[id] = listened
	return nil
}

func (h *handingOut) Delete(_ string, id int64) error {
	h.deleted = append(h.deleted, id)
	return nil
}

// recordingAudio is an AudioStore that remembers which locations were opened.
type recordingAudio struct {
	AudioStore
	opened []string
}

func (r *recordingAudio) Open(location string) (AudioReader, time.Time, error) {
	r.opened = append(r.opened, location)
	return nil, modified, nil
}

var modified = time.Date(2026, time.October, 29, 12, 0, 0, 0, time.UTC)

var (
	ready  = Episode{ID: 1, UserID: "alex", AudioLocation: "1-alex.ogg"}
	failed = Episode{ID: 2, UserID: "alex", Failure: "boom"}
)

func TestAnEpisodeOfAnotherUserIsNotFoundEvenWhenTheRepositoryHandsItOut(t *testing.T) {
	library := NewEpisodeLibrary(newHandingOut(ready, failed), &recordingAudio{})

	for name, ask := range map[string]func() error{
		"Get":       func() error { _, err := library.Get("bea", ready.ID); return err },
		"GetReady":  func() error { _, err := library.GetReady("bea", ready.ID); return err },
		"GetFailed": func() error { _, err := library.GetFailed("bea", failed.ID); return err },
		"SetListened": func() error {
			_, err := library.SetListened("bea", ready.ID, true)
			return err
		},
		"Delete": func() error { return library.Delete("bea", ready.ID) },
	} {
		assert.ErrorIs(t, ask(), ErrEpisodeNotFound, name)
	}
}

func TestOnlyAReadyEpisodeCanBeOpenedAndOnlyAFailedOneCanBeRetried(t *testing.T) {
	library := NewEpisodeLibrary(newHandingOut(ready, failed), &recordingAudio{})

	opened, err := library.GetReady("alex", ready.ID)
	require.NoError(t, err)
	_, notReady := library.GetReady("alex", failed.ID)
	retried, err := library.GetFailed("alex", failed.ID)
	require.NoError(t, err)
	_, notFailed := library.GetFailed("alex", ready.ID)
	either, err := library.Get("alex", failed.ID)
	require.NoError(t, err)

	assert.Equal(t, ready.ID, opened.ID)
	assert.ErrorIs(t, notReady, ErrEpisodeNotFound, "a failed Episode has no audio to open")
	assert.Equal(t, failed.ID, retried.ID)
	assert.ErrorIs(t, notFailed, ErrEpisodeNotFound, "an Episode that did not fail is not retried")
	assert.Equal(t, failed.ID, either.ID, "Get does not mind how the Episode ended")
}

func TestListenedIsSetForAReadyEpisodeAndRefusedForAFailedOne(t *testing.T) {
	repository := newHandingOut(ready, failed)
	library := NewEpisodeLibrary(repository, &recordingAudio{})

	got, err := library.SetListened("alex", ready.ID, true)
	_, refused := library.SetListened("alex", failed.ID, true)

	require.NoError(t, err)
	assert.True(t, got.Listened, "the Episode comes back with the new state")
	assert.True(t, repository.listened[ready.ID])
	assert.True(t, errors.Is(refused, ErrEpisodeFailed))
	assert.NotContains(t, repository.listened, failed.ID, "nothing was written for the failed Episode")
}

func TestAnEpisodeOfAnotherUserIsNotDeleted(t *testing.T) {
	repository := newHandingOut(ready)
	library := NewEpisodeLibrary(repository, &recordingAudio{})

	_ = library.Delete("bea", ready.ID)
	require.NoError(t, library.Delete("alex", ready.ID))

	assert.Equal(t, []int64{ready.ID}, repository.deleted, "only the owner's request reached the repository")
}

func TestAudioIsOpenedOnlyForTheOwnerOfAReadyEpisode(t *testing.T) {
	for name, tc := range map[string]struct {
		user   string
		id     int64
		opened []string
		err    error
	}{
		"owner of a ready Episode":    {user: "alex", id: ready.ID, opened: []string{ready.AudioLocation}},
		"another User":                {user: "bea", id: ready.ID, err: ErrEpisodeNotFound},
		"Episode that failed":         {user: "alex", id: failed.ID, err: ErrEpisodeNotFound},
		"Episode that does not exist": {user: "alex", id: 99, err: ErrEpisodeNotFound},
	} {
		t.Run(name, func(t *testing.T) {
			store := &recordingAudio{}
			library := NewEpisodeLibrary(newHandingOut(ready, failed), store)

			_, at, err := library.OpenAudio(tc.user, tc.id)

			assert.ErrorIs(t, err, tc.err)
			assert.Equal(t, tc.opened, store.opened, "the store is asked only when the User may have the audio")
			if tc.err == nil {
				assert.Equal(t, modified, at)
			}
		})
	}
}
