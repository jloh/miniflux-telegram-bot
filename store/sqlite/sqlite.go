package sqlite

import (
	"database/sql"
	"log"
	"os"
	"time"

	_ "modernc.org/sqlite"

	"go.jloh.dev/miniflux-telegram-bot/models"
	"go.jloh.dev/miniflux-telegram-bot/store"
)

type db struct {
	ctx *sql.DB
}

func New() store.Store {
	dbDir := "data"
	if _, err := os.Stat(dbDir); os.IsNotExist(err) {
		if err := os.Mkdir(dbDir, os.ModePerm); err != nil {
			log.Printf("Error creating dir: %v\n", err)
		}
	}
	ctx, err := sql.Open("sqlite", dbDir+"/store.db")
	if err != nil {
		log.Fatalln(err)
	}
	_, err = ctx.Exec(`
	CREATE TABLE IF NOT EXISTS entries (
		id INTEGER PRIMARY KEY,
		telegram_id INTEGER,
		sent_time TEXT,
		updated TEXT
	)`)
	if err != nil {
		log.Fatalln(err)
	}
	return &db{
		ctx: ctx,
	}
}

func (d db) GetEntry(id int) (models.Message, error) {
	var msg models.Message
	stmt, err := d.ctx.Prepare("SELECT id, telegram_id, sent_time, updated FROM entries where id=?")
	if err != nil {
		return msg, err
	}
	defer stmt.Close()

	var sent_time, updated_time string
	err = stmt.QueryRow(id).Scan(&msg.ID, &msg.TelegramID, &sent_time, &updated_time)
	if err != nil {
		return msg, err
	}

	msg.SentTime, err = time.Parse(time.RFC3339, sent_time)
	if err != nil {
		return msg, err
	}

	msg.UpdatedTime, err = time.Parse(time.RFC3339, updated_time)
	if err != nil {
		return msg, err
	}

	return msg, nil
}

func (d db) InsertEntry(msg models.Message) error {
	_, err := d.ctx.Exec(`
	INSERT INTO entries(
		id,
		telegram_id,
		sent_time,
		updated
	)
	VALUES(?,?,?,?)`, msg.ID, msg.TelegramID, msg.SentTime.Format(time.RFC3339), msg.UpdatedTime.Format(time.RFC3339))
	return err
}

func (d db) UpdateEntryTime(id int64, updated time.Time) error {
	stmt, err := d.ctx.Prepare("UPDATE entries set updated=? where id=?")
	if err != nil {
		return err
	}

	defer stmt.Close()

	_, err = stmt.Exec(updated.Format(time.RFC3339), id)
	if err != nil {
		return err
	}

	return nil
}

func (d db) GetEntries() ([]models.Message, error) {
	results := make([]models.Message, 0)
	stmt, err := d.ctx.Prepare("SELECT id, telegram_id, sent_time, updated FROM entries")
	if err != nil {
		return nil, err
	}
	defer stmt.Close()

	res, err := stmt.Query()
	if err != nil {
		return results, err

	}

	for res.Next() {
		var msg models.Message
		var sent_time, updated_time string
		if err := res.Scan(&msg.ID, &msg.TelegramID, &sent_time, &updated_time); err != nil {
			continue
		}
		// Parse sent_time
		msg.SentTime, err = time.Parse(time.RFC3339, sent_time)
		if err != nil {
			continue
		}
		msg.UpdatedTime, err = time.Parse(time.RFC3339, updated_time)
		if err != nil {
			continue
		}
		results = append(results, msg)
	}

	return results, nil
}

func (d db) DeleteEntryByID(id int64) error {
	_, err := d.ctx.Exec(`
	DELETE from entries where id=?
	`, id)
	return err
}

func (d db) DeleteEntryByTelegramID(id int) error {
	_, err := d.ctx.Exec(`
	DELETE from entries where telegram_id=?
	`, id)
	return err
}
