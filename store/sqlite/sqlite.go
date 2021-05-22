package sqlite

import (
	"database/sql"
	"log"
	"os"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"go.jloh.dev/miniflux-telegram-bot/models"
	"go.jloh.dev/miniflux-telegram-bot/store"
)

type db struct {
	ctx *sql.DB
}

func New() store.Store {
	dbDir := "data"
	if _, err := os.Stat(dbDir); os.IsNotExist(err) {
		os.Mkdir(dbDir, os.ModePerm)
	}
	ctx, err := sql.Open("sqlite3", dbDir+"/store.db")
	if err != nil {
		log.Fatalln(err)
	}
	_, err = ctx.Exec(`
	CREATE TABLE IF NOT EXISTS entries (
		id INTEGER PRIMARY KEY,
		telegram_id INTEGER,
		sent_time TEXT
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
	stmt, err := d.ctx.Prepare("SELECT telegram_id, sent_time FROM entries where id=?")
	if err != nil {
		return msg, err
	}
	defer stmt.Close()

	var sent_time string
	err = stmt.QueryRow(id).Scan(&msg.TelegramID, &sent_time)
	if err != nil {
		return msg, err
	}

	msg.SentTime, err = time.Parse(time.RFC3339, sent_time)
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
		sent_time
	)
	VALUES(?,?,?)`, msg.ID, msg.TelegramID, msg.SentTime.Format(time.RFC3339))
	return err
}

func (d db) GetEntries() ([]models.Message, error) {
	results := make([]models.Message, 0)
	stmt, err := d.ctx.Prepare("SELECT id, telegram_id, sent_time FROM entries")
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
		var sent_time string
		if err := res.Scan(&msg.ID, &msg.TelegramID, &sent_time); err != nil {
			continue
		}
		// Parse sent_time
		msg.SentTime, err = time.Parse(time.RFC3339, sent_time)
		if err != nil {
			continue
		}
		results = append(results, msg)
	}

	return results, nil
}

func (d db) DeleteEntry(id int) error {
	_, err := d.ctx.Exec(`
	DELETE from entries where id=?
	`, id)
	return err
}
