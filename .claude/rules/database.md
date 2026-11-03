---
paths:
  - "pkg/database/**"
---

# Database schema

## Tables are plural and countable

A table is named after what one row is, in the plural. Uncountable nouns get a countable one.

```sql
-- bad
CREATE TABLE audio (...);
-- good
CREATE TABLE audio_files (...);
```

## The primary key is `<entity>_id`

The primary key is the singular name of the entity plus `_id`. A foreign key in another table has exactly the name of the key it points to, and joins use `USING`. A name that occurs in more than two joined tables is qualified in `WHERE` and `ORDER BY`.

```sql
-- bad: id here, episode_id there, ON to connect them
SELECT ... FROM episodes e JOIN sections s ON s.episode_id = e.id
-- good
SELECT ... FROM episodes JOIN sections USING (episode_id)
```

A table that only links two others has the two foreign keys as its composite primary key and no key of its own.

```sql
-- good: a Chapter is the link between an audio file and a Section
PRIMARY KEY (audio_file_id, section_id)
```

## Foreign keys name their target column

Every reference is declared with `REFERENCES table (column)`. A child that cannot outlive its parent has `ON DELETE CASCADE`. A foreign key column that does not start the primary key gets an index.

```sql
-- bad
episode_id INTEGER NOT NULL REFERENCES episodes
-- good
episode_id INTEGER NOT NULL REFERENCES episodes (episode_id) ON DELETE CASCADE
```

## The schema states the relationships

A one-to-one relationship is a `UNIQUE` foreign key. What cannot exist without something else refers to it, and a value lives with the thing it describes: the start of a Chapter with the audio file, the title of a Chapter with its Section. A value is copied only as a deliberate snapshot, such as a display name kept as text because the User Config lives in files.

## Units are in the comment

A column holding a quantity names its unit in a comment.

```sql
duration INTEGER NOT NULL -- nanoseconds
```
