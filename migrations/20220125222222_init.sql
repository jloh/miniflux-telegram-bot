-- +goose Up
CREATE TABLE IF NOT EXISTS entries (
	id INTEGER PRIMARY KEY NOT NULL,
	telegram_id INTEGER NOT NULL,
	sent_time TEXT NOT NULL,
	updated TEXT NOT NULL
);

-- +goose Down
DROP TABLE entries;
