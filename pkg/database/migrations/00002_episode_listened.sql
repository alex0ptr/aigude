-- +goose Up
-- Whether the User has heard the Episode far enough. A new Episode starts as 0. Every ready Episode that exists when this
-- migration runs counts as heard; a failed Episode has no audio to hear and stays at 0.
ALTER TABLE episodes ADD COLUMN listened INTEGER NOT NULL DEFAULT 0 CHECK (listened IN (0, 1)); -- 1 when listened
UPDATE episodes SET listened = 1 WHERE failure IS NULL;

-- +goose Down
ALTER TABLE episodes DROP COLUMN listened;
