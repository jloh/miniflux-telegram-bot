-- +goose Up
-- +goose StatementBegin
ALTER TABLE entries add delete_read BOOLEAN DEFAULT true NOT NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE entries RENAME TO _entries_old;
CREATE TABLE entries (
	id INTEGER PRIMARY KEY NOT NULL,
	telegram_id INTEGER NOT NULL,
	sent_time TEXT NOT NULL,
	updated TEXT NOT NULL
);
INSERT INTO entries (id, telegram_id, sent_time, updated) SELECT `id`, telegram_id, sent_time, updated from _entries_old;
DROP TABLE _entries_old;
-- +goose StatementEnd
