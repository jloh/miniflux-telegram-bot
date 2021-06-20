package store

import (
	"time"

	"go.jloh.dev/miniflux-telegram-bot/models"
)

// Storage interface for storing a mapping of
// Miniflux IDs to Telegram messages
type Store interface {
	GetEntries() ([]models.Message, error)             // Get all entries in DB
	GetEntry(id int) (models.Message, error)           // Get a single entry in the DB
	InsertEntry(models.Message) error                  // Insert a new entry into the DB
	UpdateEntryTime(id int64, updated time.Time) error // Update the entry updated time
	DeleteEntryByID(id int64) error                    // Delete a entry in the DB by Miniflux ID
	DeleteEntryByTelegramID(id int) error              // Delete a entry in the DB by its Telegram ID
}
