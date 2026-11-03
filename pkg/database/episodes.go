package database

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/alex0ptr/aigude/business"
)

// audioKey is the key of an Episode's audio in the audio store: the Episode number first, so a key always leads back
// to one Episode, then the Id of its User, so the audio of one User can be found by name.
func audioKey(id int64, userID string) string {
	return strconv.FormatInt(id, 10) + "-" + userID + ".ogg"
}

// EpisodeRepository implements business.EpisodeRepository: the Episodes in SQLite, their audio in an AudioStore.
// The database records only the opaque location the store returned.
type EpisodeRepository struct {
	db    *sql.DB
	audio business.AudioStore
}

var _ business.EpisodeRepository = (*EpisodeRepository)(nil)

// Save stores the Episode and hands its audio to the audio store. When either fails, neither is kept; a crash
// between storing the audio and committing the record can leave an orphan audio file.
func (r *EpisodeRepository) Save(episode business.Episode, audio []byte) (business.Episode, error) {
	slog.Info("Saving episode", "user", episode.UserID, "title", episode.Script.Title, "audioBytes", len(audio))
	if len(episode.Chapters) > len(episode.Script.Sections) {
		slog.Warn("Refusing to save episode with more chapters than sections", "user", episode.UserID,
			"chapters", len(episode.Chapters), "sections", len(episode.Script.Sections))
		return business.Episode{}, fmt.Errorf("save episode: %d chapters for %d sections, a chapter marks the start of a section",
			len(episode.Chapters), len(episode.Script.Sections))
	}
	tx, err := r.db.Begin()
	if err != nil {
		return business.Episode{}, fmt.Errorf("save episode: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.Exec(`INSERT INTO episodes
		(user_id, cast_profile_id, created_at, title)
		VALUES (?, ?, ?, ?)`,
		episode.UserID, episode.CastProfileID, episode.Date.UnixNano(), episode.Script.Title)
	if err != nil {
		return business.Episode{}, fmt.Errorf("save episode: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return business.Episode{}, fmt.Errorf("save episode: %w", err)
	}

	sectionIDs := make([]int64, len(episode.Script.Sections))
	for i, section := range episode.Script.Sections {
		sectionRes, err := tx.Exec(`INSERT INTO sections (episode_id, position, heading, body) VALUES (?, ?, ?, ?)`,
			id, i, section.Heading, section.Body)
		if err != nil {
			return business.Episode{}, fmt.Errorf("save section %d: %w", i, err)
		}
		if sectionIDs[i], err = sectionRes.LastInsertId(); err != nil {
			return business.Episode{}, fmt.Errorf("save section %d: %w", i, err)
		}
	}

	for _, identity := range episode.UsedContent {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO used_content_items (episode_id, identity) VALUES (?, ?)`, id, identity); err != nil {
			return business.Episode{}, fmt.Errorf("save used content %s: %w", identity, err)
		}
	}
	slog.Debug("Used content recorded", "id", id, "items", len(episode.UsedContent))
	if err := saveSources(tx, id, episode, sectionIDs); err != nil {
		return business.Episode{}, err
	}
	if err := saveUsage(tx, id, episode.Usage); err != nil {
		return business.Episode{}, err
	}

	slog.Debug("Storing episode audio", "id", id, "user", episode.UserID)
	location, err := r.audio.Store(audioKey(id, episode.UserID), audio)
	if err != nil {
		return business.Episode{}, fmt.Errorf("save episode audio: %w", err)
	}
	if err := r.saveAudioFile(tx, id, location, episode, sectionIDs); err != nil {
		r.discardAudio(id, location)
		return business.Episode{}, err
	}
	if err := tx.Commit(); err != nil {
		r.discardAudio(id, location)
		return business.Episode{}, fmt.Errorf("save episode: %w", err)
	}

	episode.ID = id
	episode.AudioLocation = location
	slog.Info("Episode saved", "id", id, "audio", location)
	return episode, nil
}

// SaveFailed stores an Episode whose production failed: its owner, title and the reason, without Sections, audio or
// Chapters.
func (r *EpisodeRepository) SaveFailed(episode business.Episode, failure string) (business.Episode, error) {
	slog.Info("Saving failed episode", "user", episode.UserID, "profile", episode.CastProfileID, "failure", failure)
	tx, err := r.db.Begin()
	if err != nil {
		return business.Episode{}, fmt.Errorf("save failed episode: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.Exec(`INSERT INTO episodes
		(user_id, cast_profile_id, created_at, title, failure)
		VALUES (?, ?, ?, ?, ?)`,
		episode.UserID, episode.CastProfileID, episode.Date.UnixNano(), episode.Script.Title, failure)
	if err != nil {
		return business.Episode{}, fmt.Errorf("save failed episode: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return business.Episode{}, fmt.Errorf("save failed episode: %w", err)
	}
	if err := saveUsage(tx, id, episode.Usage); err != nil {
		return business.Episode{}, err
	}
	if err := tx.Commit(); err != nil {
		return business.Episode{}, fmt.Errorf("save failed episode: %w", err)
	}
	episode.ID = id
	episode.Failure = failure
	return episode, nil
}

// List returns the User's Episodes, newest first.
func (r *EpisodeRepository) List(userID string, opts business.ListOpts) ([]business.Episode, error) {
	slog.Debug("Listing episodes", "user", userID, "limit", opts.Limit, "beforeID", opts.BeforeID, "castProfile", opts.CastProfileID)
	query := episodeSelect + ` WHERE user_id = ?`
	args := []any{userID}
	if opts.BeforeID > 0 {
		query += ` AND episode_id < ?`
		args = append(args, opts.BeforeID)
	}
	if opts.CastProfileID != "" {
		query += ` AND cast_profile_id = ?`
		args = append(args, opts.CastProfileID)
	}
	query += ` ORDER BY episode_id DESC`
	if opts.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, opts.Limit)
	}
	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list episodes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var episodes []business.Episode
	for rows.Next() {
		episode, err := r.scan(rows)
		if err != nil {
			return nil, fmt.Errorf("list episodes: %w", err)
		}
		episodes = append(episodes, episode)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list episodes: %w", err)
	}
	if err := r.addUsage(episodes); err != nil {
		return nil, fmt.Errorf("list episodes: %w", err)
	}
	slog.Debug("Listed episodes", "user", userID, "count", len(episodes))
	return episodes, nil
}

// UsedContent returns the identities of the Content that went into the ready Episodes of the CastProfile.
func (r *EpisodeRepository) UsedContent(userID, castProfileID string) ([]string, error) {
	rows, err := r.db.Query(`SELECT DISTINCT identity FROM used_content_items JOIN episodes USING (episode_id)
		WHERE user_id = ? AND cast_profile_id = ?`, userID, castProfileID)
	if err != nil {
		return nil, fmt.Errorf("list used content: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var identities []string
	for rows.Next() {
		var identity string
		if err := rows.Scan(&identity); err != nil {
			return nil, fmt.Errorf("list used content: %w", err)
		}
		identities = append(identities, identity)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list used content: %w", err)
	}
	slog.Debug("Listed used content", "user", userID, "profile", castProfileID, "items", len(identities))
	return identities, nil
}

// Get returns one Episode of the User with its Sections and Chapters.
func (r *EpisodeRepository) Get(userID string, id int64) (business.Episode, error) {
	slog.Debug("Reading episode", "user", userID, "id", id)
	episode, err := r.scan(r.db.QueryRow(episodeSelect+` WHERE user_id = ? AND episode_id = ?`, userID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return business.Episode{}, business.ErrEpisodeNotFound
	}
	if err != nil {
		return business.Episode{}, fmt.Errorf("get episode %d: %w", id, err)
	}

	if episode.Usage, err = r.usageOf(id); err != nil {
		return business.Episode{}, fmt.Errorf("get episode %d: %w", id, err)
	}

	sections, err := r.db.Query(`SELECT heading, body FROM sections WHERE episode_id = ? ORDER BY position`, id)
	if err != nil {
		return business.Episode{}, fmt.Errorf("get sections of episode %d: %w", id, err)
	}
	defer func() { _ = sections.Close() }()
	for sections.Next() {
		var section business.Section
		if err := sections.Scan(&section.Heading, &section.Body); err != nil {
			return business.Episode{}, fmt.Errorf("get sections of episode %d: %w", id, err)
		}
		episode.Script.Sections = append(episode.Script.Sections, section)
	}
	if err := sections.Err(); err != nil {
		return business.Episode{}, fmt.Errorf("get sections of episode %d: %w", id, err)
	}

	sources, perSection, err := r.sourcesOf(id)
	if err != nil {
		return business.Episode{}, err
	}
	episode.Sources = sources
	for i := range episode.Script.Sections {
		episode.Script.Sections[i].Sources = perSection[i]
	}

	chapters, err := r.db.Query(`SELECT heading, start
		FROM chapters JOIN sections USING (section_id) JOIN audio_files USING (audio_file_id)
		WHERE audio_files.episode_id = ? ORDER BY position`, id)
	if err != nil {
		return business.Episode{}, fmt.Errorf("get chapters of episode %d: %w", id, err)
	}
	defer func() { _ = chapters.Close() }()
	for chapters.Next() {
		var chapter business.Chapter
		var start int64
		if err := chapters.Scan(&chapter.Title, &start); err != nil {
			return business.Episode{}, fmt.Errorf("get chapters of episode %d: %w", id, err)
		}
		chapter.Start = time.Duration(start)
		episode.Chapters = append(episode.Chapters, chapter)
	}
	if err := chapters.Err(); err != nil {
		return business.Episode{}, fmt.Errorf("get chapters of episode %d: %w", id, err)
	}
	return episode, nil
}

// saveSources records the Content items of the prompt and, for each Section, which of them it is based on. A number
// that names no source is skipped.
func saveSources(tx *sql.Tx, id int64, episode business.Episode, sectionIDs []int64) error {
	sourceIDs := make([]int64, len(episode.Sources))
	for i, source := range episode.Sources {
		res, err := tx.Exec(`INSERT INTO sources (episode_id, position, title, link, name, body, retrieved_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			id, i+1, source.Title, source.Link, source.Name, source.Text, source.Retrieved.UnixNano())
		if err != nil {
			return fmt.Errorf("save source %d: %w", i+1, err)
		}
		if sourceIDs[i], err = res.LastInsertId(); err != nil {
			return fmt.Errorf("save source %d: %w", i+1, err)
		}
	}
	for i, section := range episode.Script.Sections {
		for _, number := range section.Sources {
			if number < 1 || number > len(sourceIDs) {
				slog.Warn("Skipping a source number that names no source", "id", id, "section", section.Heading, "source", number)
				continue
			}
			if _, err := tx.Exec(`INSERT OR IGNORE INTO section_sources (section_id, source_id) VALUES (?, ?)`,
				sectionIDs[i], sourceIDs[number-1]); err != nil {
				return fmt.Errorf("save sources of section %d: %w", i, err)
			}
		}
	}
	slog.Debug("Sources recorded", "id", id, "items", len(episode.Sources))
	return nil
}

// sourcesOf returns the sources of the Episode in prompt order and, for each Section by position, the numbers of its
// sources.
func (r *EpisodeRepository) sourcesOf(id int64) ([]business.Source, map[int][]int, error) {
	rows, err := r.db.Query(`SELECT title, link, name, body, retrieved_at FROM sources WHERE episode_id = ? ORDER BY position`, id)
	if err != nil {
		return nil, nil, fmt.Errorf("get sources of episode %d: %w", id, err)
	}
	var sources []business.Source
	for rows.Next() {
		var source business.Source
		var retrieved int64
		if err := rows.Scan(&source.Title, &source.Link, &source.Name, &source.Text, &retrieved); err != nil {
			_ = rows.Close()
			return nil, nil, fmt.Errorf("get sources of episode %d: %w", id, err)
		}
		source.Retrieved = time.Unix(0, retrieved).UTC()
		sources = append(sources, source)
	}
	if err := rows.Close(); err != nil {
		return nil, nil, fmt.Errorf("get sources of episode %d: %w", id, err)
	}

	links, err := r.db.Query(`SELECT sections.position, sources.position
		FROM section_sources JOIN sections USING (section_id) JOIN sources USING (source_id)
		WHERE sections.episode_id = ? ORDER BY sections.position, sources.position`, id)
	if err != nil {
		return nil, nil, fmt.Errorf("get sources of the sections of episode %d: %w", id, err)
	}
	defer func() { _ = links.Close() }()
	perSection := make(map[int][]int)
	for links.Next() {
		var section, source int
		if err := links.Scan(&section, &source); err != nil {
			return nil, nil, fmt.Errorf("get sources of the sections of episode %d: %w", id, err)
		}
		perSection[section] = append(perSection[section], source)
	}
	return sources, perSection, links.Err()
}

// saveUsage records what producing the Episode consumed.
func saveUsage(tx *sql.Tx, id int64, usage business.Usage) error {
	for _, item := range usage {
		if _, err := tx.Exec(`INSERT INTO usage_items (episode_id, kind, unit, quantity) VALUES (?, ?, ?, ?)
			ON CONFLICT (episode_id, kind, unit) DO UPDATE SET quantity = quantity + excluded.quantity`,
			id, string(item.Kind), string(item.Unit), item.Quantity); err != nil {
			return fmt.Errorf("save usage %s %s: %w", item.Kind, item.Unit, err)
		}
	}
	slog.Debug("Usage recorded", "id", id, "items", len(usage))
	return nil
}

// usageOf returns what producing the Episode consumed, ordered by kind and unit.
func (r *EpisodeRepository) usageOf(id int64) (business.Usage, error) {
	episodes := []business.Episode{{ID: id}}
	if err := r.addUsage(episodes); err != nil {
		return nil, err
	}
	return episodes[0].Usage, nil
}

// addUsage sets the Usage of all the Episodes with one query, ordered by kind and unit.
func (r *EpisodeRepository) addUsage(episodes []business.Episode) error {
	if len(episodes) == 0 {
		return nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(episodes)), ",")
	args := make([]any, len(episodes))
	position := make(map[int64]int, len(episodes))
	for i, e := range episodes {
		args[i], position[e.ID] = e.ID, i
	}
	rows, err := r.db.Query(`SELECT episode_id, kind, unit, quantity FROM usage_items
		WHERE episode_id IN (`+placeholders+`) ORDER BY episode_id, kind, unit`, args...)
	if err != nil {
		return fmt.Errorf("get usage of episodes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id int64
		var item business.UsageItem
		if err := rows.Scan(&id, &item.Kind, &item.Unit, &item.Quantity); err != nil {
			return fmt.Errorf("get usage of episodes: %w", err)
		}
		episodes[position[id]].Usage = append(episodes[position[id]].Usage, item)
	}
	return rows.Err()
}

// saveAudioFile records the audio file of the Episode at its location and one Chapter for each of the first Sections,
// which the Chapters mark the start of.
func (r *EpisodeRepository) saveAudioFile(tx *sql.Tx, id int64, location string, episode business.Episode, sectionIDs []int64) error {
	res, err := tx.Exec(`INSERT INTO audio_files (episode_id, location, duration) VALUES (?, ?, ?)`,
		id, location, int64(episode.Duration))
	if err != nil {
		return fmt.Errorf("save episode audio file: %w", err)
	}
	audioFileID, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("save episode audio file: %w", err)
	}
	slog.Debug("Saving chapters of the audio file", "episode", id, "audioFile", audioFileID, "chapters", len(episode.Chapters))
	for i, chapter := range episode.Chapters {
		if _, err := tx.Exec(`INSERT INTO chapters (audio_file_id, section_id, start) VALUES (?, ?, ?)`,
			audioFileID, sectionIDs[i], int64(chapter.Start)); err != nil {
			return fmt.Errorf("save chapter %d: %w", i, err)
		}
	}
	return nil
}

// discardAudio removes audio of an Episode that was not recorded and logs when that fails.
func (r *EpisodeRepository) discardAudio(id int64, location string) {
	slog.Warn("Removing audio of an Episode that was not saved", "id", id, "location", location)
	if err := r.audio.Delete(location); err != nil {
		slog.Error("Audio of an unsaved Episode stays behind", "id", id, "location", location, "error", err)
	}
}

// Delete removes the Episode, its Sections and Chapters, and its audio. The record is the truth about what
// exists: once it is gone the Episode is deleted, and audio that cannot be removed is logged as an orphan.
func (r *EpisodeRepository) Delete(userID string, id int64) error {
	slog.Info("Deleting episode", "user", userID, "id", id)
	var location string
	err := r.db.QueryRow(`SELECT COALESCE(location, '') FROM episodes LEFT JOIN audio_files USING (episode_id)
		WHERE user_id = ? AND episode_id = ?`, userID, id).Scan(&location)
	if errors.Is(err, sql.ErrNoRows) {
		return business.ErrEpisodeNotFound
	}
	if err != nil {
		return fmt.Errorf("delete episode %d: %w", id, err)
	}
	res, err := r.db.Exec(`DELETE FROM episodes WHERE user_id = ? AND episode_id = ?`, userID, id)
	if err != nil {
		return fmt.Errorf("delete episode %d: %w", id, err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		slog.Debug("Episode was deleted in the meantime", "user", userID, "id", id)
		return business.ErrEpisodeNotFound
	}
	if location == "" {
		slog.Debug("Episode had no audio", "id", id)
		return nil
	}
	slog.Debug("Removing episode audio", "id", id, "location", location)
	if err := r.audio.Delete(location); err != nil {
		slog.Error("Episode deleted but its audio stays behind", "id", id, "location", location, "error", err)
	}
	return nil
}

// DeleteBefore removes the Episodes created before the time, of every User, with their audio. As in Delete the
// record decides: audio that cannot be removed afterwards is logged as an orphan.
func (r *EpisodeRepository) DeleteBefore(t time.Time) (int, error) {
	slog.Debug("Deleting episodes created before", "before", t.Format(time.DateOnly))
	locations, err := r.deleteRecordsBefore(t)
	if err != nil {
		return 0, err
	}
	for id, location := range locations {
		if location == "" {
			continue
		}
		slog.Debug("Removing audio of an expired episode", "id", id, "location", location)
		if err := r.audio.Delete(location); err != nil {
			slog.Error("Expired episode deleted but its audio stays behind", "id", id, "location", location, "error", err)
		}
	}
	return len(locations), nil
}

// deleteRecordsBefore deletes the records of the Episodes created before the time in one transaction and returns the
// audio location of each deleted Episode by its ID, empty for an Episode without audio.
func (r *EpisodeRepository) deleteRecordsBefore(t time.Time) (map[int64]string, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("delete expired episodes: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.Query(`SELECT episode_id, COALESCE(location, '') FROM episodes LEFT JOIN audio_files USING (episode_id)
		WHERE created_at < ?`, t.UnixNano())
	if err != nil {
		return nil, fmt.Errorf("find expired episodes: %w", err)
	}
	locations := make(map[int64]string)
	for rows.Next() {
		var id int64
		var location string
		if err := rows.Scan(&id, &location); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("find expired episodes: %w", err)
		}
		locations[id] = location
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("find expired episodes: %w", err)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("find expired episodes: %w", err)
	}
	for id := range locations {
		if _, err := tx.Exec(`DELETE FROM episodes WHERE episode_id = ?`, id); err != nil {
			return nil, fmt.Errorf("delete expired episode %d: %w", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("delete expired episodes: %w", err)
	}
	slog.Debug("Records of expired episodes deleted", "before", t.Format(time.DateOnly), "episodes", len(locations))
	return locations, nil
}

// episodeSelect reads the columns scan expects.
const episodeSelect = `SELECT episode_id, user_id, cast_profile_id,
	created_at, title, COALESCE(location, ''), COALESCE(duration, 0), COALESCE(failure, '')
	FROM episodes LEFT JOIN audio_files USING (episode_id)`

// scanner is what *sql.Row and *sql.Rows have in common.
type scanner interface{ Scan(dest ...any) error }

// scan reads one row of episodeSelect; Sections and Chapters are left empty.
func (r *EpisodeRepository) scan(row scanner) (business.Episode, error) {
	var episode business.Episode
	var created, duration int64
	var location string
	err := row.Scan(&episode.ID, &episode.UserID, &episode.CastProfileID,
		&created, &episode.Script.Title, &location, &duration, &episode.Failure)
	if err != nil {
		return business.Episode{}, err
	}
	episode.Date = time.Unix(0, created).UTC()
	episode.Duration = time.Duration(duration)
	episode.AudioLocation = location
	return episode, nil
}
