-- +goose Up
CREATE TABLE episodes (
    episode_id      INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id         TEXT    NOT NULL,
    cast_profile_id TEXT    NOT NULL,
    created_at      INTEGER NOT NULL, -- unix nanoseconds
    title           TEXT    NOT NULL, -- empty while no Script was written
    failure         TEXT              -- why producing the Episode failed; NULL unless it did
);

CREATE INDEX episodes_by_user ON episodes (user_id, episode_id DESC);

CREATE TABLE sections (
    section_id INTEGER PRIMARY KEY,
    episode_id INTEGER NOT NULL REFERENCES episodes (episode_id) ON DELETE CASCADE,
    position   INTEGER NOT NULL,
    heading    TEXT    NOT NULL,
    body       TEXT    NOT NULL,
    UNIQUE (episode_id, position)
);

-- The single audio file of an Episode; an Episode has none until its audio is produced.
CREATE TABLE audio_files (
    audio_file_id INTEGER PRIMARY KEY,
    episode_id    INTEGER NOT NULL UNIQUE REFERENCES episodes (episode_id) ON DELETE CASCADE,
    location      TEXT    NOT NULL, -- opaque location returned by the audio store
    duration      INTEGER NOT NULL  -- nanoseconds
);

-- A marker in the audio file where a Section begins; its title is the Section's heading.
-- Replacing the audio file removes its Chapters, removing the Section removes its Chapter.
CREATE TABLE chapters (
    audio_file_id INTEGER NOT NULL REFERENCES audio_files (audio_file_id) ON DELETE CASCADE,
    section_id    INTEGER NOT NULL REFERENCES sections (section_id) ON DELETE CASCADE,
    start         INTEGER NOT NULL, -- nanoseconds from the beginning of the audio
    PRIMARY KEY (audio_file_id, section_id)
);

CREATE INDEX chapters_by_section ON chapters (section_id);

-- The Content that went into the prompt of a ready Episode, by identity: the link without its fragment, or the GUID
-- of the feed when there is no link. It goes with the Episode: deleting the Episode gives its Content back.
CREATE TABLE used_content_items (
    episode_id INTEGER NOT NULL REFERENCES episodes (episode_id) ON DELETE CASCADE,
    identity   TEXT    NOT NULL,
    PRIMARY KEY (episode_id, identity)
);

-- A Content item that went into the prompt of a ready Episode, kept as the language model saw it, so that it stays
-- readable when the feeds or the crawl cache change. The position is the number the Script writer cites.
CREATE TABLE sources (
    source_id    INTEGER PRIMARY KEY,
    episode_id   INTEGER NOT NULL REFERENCES episodes (episode_id) ON DELETE CASCADE,
    position     INTEGER NOT NULL, -- counting from 1, as in the prompt
    title        TEXT    NOT NULL,
    link         TEXT    NOT NULL, -- address of the original; empty when there is none
    name         TEXT    NOT NULL, -- where the item came from, such as the title of the feed
    body         TEXT    NOT NULL, -- Markdown text as handed to the language model
    retrieved_at INTEGER NOT NULL, -- unix nanoseconds
    UNIQUE (episode_id, position)
);

-- The sources the Script writer says a Section is based on.
CREATE TABLE section_sources (
    section_id INTEGER NOT NULL REFERENCES sections (section_id) ON DELETE CASCADE,
    source_id  INTEGER NOT NULL REFERENCES sources (source_id) ON DELETE CASCADE,
    PRIMARY KEY (section_id, source_id)
);

CREATE INDEX section_sources_by_source ON section_sources (source_id);

-- What producing an Episode consumed, per kind of work and unit. A failed Episode keeps it: the resources are gone.
CREATE TABLE usage_items (
    episode_id INTEGER NOT NULL REFERENCES episodes (episode_id) ON DELETE CASCADE,
    kind       TEXT    NOT NULL, -- script, speech or crawl
    unit       TEXT    NOT NULL, -- input_tokens, output_tokens, audio_tokens or pages
    quantity   INTEGER NOT NULL, -- count of the unit
    PRIMARY KEY (episode_id, kind, unit)
);

-- A page a Crawler retrieved, shared by all Users. A page older than the time to live counts as missing and is
-- replaced by a new retrieval.
CREATE TABLE crawled_pages (
    crawled_page_id INTEGER PRIMARY KEY,
    url             TEXT    NOT NULL UNIQUE, -- address of the page
    title           TEXT    NOT NULL,
    body            TEXT    NOT NULL,        -- text as the Crawler returned it, before cleaning
    retrieved_at    INTEGER NOT NULL         -- unix nanoseconds
);

CREATE INDEX crawled_pages_by_retrieved_at ON crawled_pages (retrieved_at);

-- +goose Down
DROP TABLE crawled_pages;
DROP TABLE usage_items;
DROP TABLE section_sources;
DROP TABLE sources;
DROP TABLE used_content_items;
DROP TABLE chapters;
DROP TABLE audio_files;
DROP TABLE sections;
DROP TABLE episodes;
