package store

import "go.jloh.dev/miniflux-telegram-bot/models"

// Storage interface for storing a mapping of
// Miniflux IDs to Telegram messages
type Store interface {
	GetEntries() ([]models.Message, error)            // Get all entries in DB
	GetEntry(miniflux_id int) (models.Message, error) // Get a single entry in the DB
	InsertEntry(models.Message) error                 // Insert a new entry into the DB
	DeleteEntry(id int) error                         // Delete a entry in the DB by ID
}
