package models

import "time"

// Message is used to contain entries inserted into storage
type Message struct {
	ID         int64     // ID taken Miniflux's entry ID
	TelegramID int       // The message ID from Telegram
	SentTime   time.Time // The time the message was sent
}
