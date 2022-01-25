package main

import (
	"embed"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/spf13/viper"
	"go.jloh.dev/miniflux-telegram-bot/models"
	"go.jloh.dev/miniflux-telegram-bot/parse"
	"go.jloh.dev/miniflux-telegram-bot/store"
	"go.jloh.dev/miniflux-telegram-bot/store/sqlite"
	"go.jloh.dev/miniflux-telegram-bot/types"
	miniflux "miniflux.app/client"
)

const (
	markRead      string = "markRead"
	markUnread    string = "markUnread"
	deleteAndMark string = "deleteAndMark"
	deleteMessage string = "deleteMessage"
	star          string = "star"
)

var (
	version = "unknown"
	commit  = "00000000"
	date    = "unknown"
)

//go:embed migrations/*.sql
var embedMigrations embed.FS

func main() {
	// Get config
	viper.SetDefault("MINIFLUX_URL", "https://reader.miniflux.app")
	viper.SetDefault("MINIFLUX_SLEEP_TIME", 30)
	viper.SetDefault("TELEGRAM_CHAT_ID", 0)
	viper.SetDefault("TELEGRAM_POLL_TIMEOUT", 120)
	viper.SetDefault("TELEGRAM_SILENT_NOTIFICATION", true)
	viper.SetDefault("TELEGRAM_CLEANUP_MESSAGES", true)
	viper.SetDefault("TELEGRAM_SECRET", "")
	viper.AutomaticEnv() // read in environment variables that match

	// Pass migrations to storage
	sqlite.EmbedMigrations = embedMigrations

	// Set ChatID
	chatID := viper.GetInt64("TELEGRAM_CHAT_ID")
	if chatID == 0 {
		log.Fatalln("TELEGRAM_CHAT_ID is not set")
	}

	// Check secret is set and valid
	telegramSecret, err := parse.TelegramSecret(viper.GetString("TELEGRAM_SECRET"))
	if err != nil {
		log.Fatalf("TELEGRAM_SECRET setting is invalid, got: %v", viper.GetString("TELEGRAM_SECRET"))
	}

	// Setup RSS instance
	rss := miniflux.New(viper.GetString("MINIFLUX_URL"), viper.GetString("MINIFLUX_API_KEY"))

	// Get latest entry
	latestEntries, err := rss.Entries(&miniflux.Filter{Status: miniflux.EntryStatusUnread, Limit: 1, Direction: "desc", Order: "id"})
	if err != nil {
		log.Fatalf("Cannot find latest entry: %v", err)
	}

	// Set latest entry
	latestEntryID := latestEntries.Entries[0].ID

	// Get our DB going
	store := sqlite.New()

	// Initialise Telegram bot instance
	bot, err := tgbotapi.NewBotAPI(viper.GetString("TELEGRAM_BOT_TOKEN"))
	if err != nil {
		log.Fatalf("Error initialising Telegram: %v", err)
	}

	log.Printf("Starting Miniflux Bot %v built %v (Commit %s)", version, date, commit[:8])

	// Start listening for messages from Telegram
	go listenForMessages(bot, chatID, telegramSecret, rss, store)

	// Cleanup & update messages
	if viper.GetBool("TELEGRAM_CLEANUP_MESSAGES") {
		go updateMessages(bot, chatID, telegramSecret, rss, store)
	}

	// Loop checking for new Miniflux entries
	// TODO: If webhooks get added, change over to those
	for {
		entries, err := rss.Entries(&miniflux.Filter{Status: miniflux.EntryStatusUnread, Order: "id", AfterEntryID: latestEntryID})

		if err != nil {
			fmt.Printf("Error getting entries: %v", err)
		} else {
			if entries.Total != 0 {
				for _, entry := range entries.Entries {
					sendMsg(bot, chatID, telegramSecret, entry, viper.GetBool("TELEGRAM_SILENT_NOTIFICATION"), store)
					latestEntryID = entry.ID
				}
			}
		}
		time.Sleep(time.Duration(viper.GetInt64("MINIFLUX_SLEEP_TIME")) * time.Minute)
	}
}

func listenForMessages(bot *tgbotapi.BotAPI, chatID int64, secret types.TelegramSecret, rss *miniflux.Client, store store.Store) {
	poll := tgbotapi.NewUpdate(0)
	poll.Timeout = viper.GetInt("TELEGRAM_POLL_TIMEOUT")

	updates, err := bot.GetUpdatesChan(poll)
	if err != nil {
		fmt.Printf("Err: %v", err)
	}

	for update := range updates {
		// Check whether we've got a Callback Query
		if update.CallbackQuery != nil {

			// Double check if the callback is from our expected chat
			if update.CallbackQuery.From.ID != int(chatID) {
				fmt.Printf("Callback from unexpected chat ID %v, ignoring\n", update.CallbackQuery.From.ID)
				continue
			}

			var entryID int64
			// Split our string
			callback := strings.Split(update.CallbackQuery.Data, ":")

			// Check our secret
			if callback[0] != string(secret) {
				fmt.Println("Callback contained invalid secret, ignoring")
				continue
			}

			// If we've got an entry, try and get it
			if len(callback) == 3 {
				entryID, err = strconv.ParseInt(callback[2], 10, 64)
				if err != nil {
					fmt.Printf("Failed parsing entry ID: %v", err)
				}
			}

			switch callback[1] {
			case markRead:
				if err := rss.UpdateEntries([]int64{entryID}, "read"); err != nil {
					answerCallback(bot, update.CallbackQuery.ID, "Error marking entry as read")
				} else {
					go answerCallback(bot, update.CallbackQuery.ID, "Marked entry as read")
					go updateKeyboard(bot, chatID, secret, rss, update.CallbackQuery.Message.MessageID, entryID)
					go store.UpdateEntryTime(entryID, time.Now())
				}
			case markUnread:
				if err := rss.UpdateEntries([]int64{entryID}, "unread"); err != nil {
					answerCallback(bot, update.CallbackQuery.ID, "Error marking entry as unread")
				} else {
					go answerCallback(bot, update.CallbackQuery.ID, "Marked entry as unread")
					go updateKeyboard(bot, chatID, secret, rss, update.CallbackQuery.Message.MessageID, entryID)
					go store.UpdateEntryTime(entryID, time.Now())
				}
			case deleteAndMark:
				if err := rss.UpdateEntries([]int64{entryID}, "read"); err != nil {
					answerCallback(bot, update.CallbackQuery.ID, "Error marking entry as read")
				} else {
					bot.DeleteMessage(tgbotapi.NewDeleteMessage(chatID, update.CallbackQuery.Message.MessageID))
					store.DeleteEntryByID(entryID)
					answerCallback(bot, update.CallbackQuery.ID, "Deleted message & marked as read")
				}
			case deleteMessage:
				bot.DeleteMessage(tgbotapi.NewDeleteMessage(chatID, update.CallbackQuery.Message.MessageID))
				store.DeleteEntryByTelegramID(update.CallbackQuery.Message.MessageID)
				answerCallback(bot, update.CallbackQuery.ID, "Deleted message")
			case star:
				if err := rss.ToggleBookmark(entryID); err != nil {
					answerCallback(bot, update.CallbackQuery.ID, "Error updating Miniflux entry")
					fmt.Printf("Erorr talking to Miniflux: %v\n", err)
				} else {
					go answerCallback(bot, update.CallbackQuery.ID, "Updated entry")
					go updateKeyboard(bot, chatID, secret, rss, update.CallbackQuery.Message.MessageID, entryID)
					go store.UpdateEntryTime(entryID, time.Now())
				}
			}
		}
	}
}

func updateMessages(bot *tgbotapi.BotAPI, chatID int64, secret types.TelegramSecret, rss *miniflux.Client, store store.Store) {
	for {
		// Set current time so we know when messages are to old to remove
		currentTime := time.Now()

		// Get all our entries
		entries, err := store.GetEntries()
		if err != nil {
			fmt.Printf("Error getting saved entries: %v\n", err)
		}

		for _, entry := range entries {
			if currentTime.Sub(entry.SentTime).Hours() < 48 {
				// We can edit the message!
				minifluxEntry, err := rss.Entry(entry.ID)
				if err != nil {
					fmt.Printf("Error getting Miniflux entry: %v\n", err)
					continue
				}
				// If we're read and were updated > 2 hours ago, cleanup the message
				if (minifluxEntry.Status == "read") && (currentTime.Sub(minifluxEntry.ChangedAt).Hours() > 2) {
					_, err := bot.DeleteMessage(tgbotapi.NewDeleteMessage(chatID, entry.TelegramID))
					fmt.Printf("Deleting message for read entry %v\n", entry.ID)
					if err != nil {
						fmt.Printf("Error deleting message in Telegram: %v\n", err)
					}
					// Cleanup entry in DB
					err = store.DeleteEntryByID(entry.ID)
					if err != nil {
						fmt.Printf("Error deleting entry: %v\n", err)
					}
				} else if minifluxEntry.ChangedAt.Truncate(time.Second).After(entry.UpdatedTime) {
					// If entry has been updated in Miniflux (marked as read, starred etc) update Telegram keyboard
					// Note: We're required to truncate Miniflux's time since it stores it down to the millisecond which the bot doesn't
					// Without truncating it its always seen as "after" so we constantly update
					log.Printf("Updating keyboard for entry %v\n", entry.ID)
					updateKeyboard(bot, chatID, secret, rss, entry.TelegramID, entry.ID)
					err := store.UpdateEntryTime(entry.ID, minifluxEntry.ChangedAt)
					if err != nil {
						log.Printf("Failed updating entry in storage: %v\n", err)
					}
				}
			} else {
				// Cleanup the DB entry since there is nothing we can do with it
				if err := store.DeleteEntryByID(entry.ID); err != nil {
					fmt.Printf("Error deleting entry: %v\n", err)
				} else {
					fmt.Printf("Cleaned up entry for %v as older than 48 hours\n", entry.ID)
				}
			}
		}

		// Sleep for 10 minutes until we check again
		time.Sleep(time.Duration(10 * time.Minute))
	}
}

func sendMsg(bot *tgbotapi.BotAPI, chatID int64, secret types.TelegramSecret, entry *miniflux.Entry, silentMessage bool, store store.Store) {
	msg := tgbotapi.NewMessage(chatID,
		fmt.Sprintf("*%s*\n%s in %s\n%s",
			escapeText("ModeMarkdownV2", entry.Title),
			escapeText("ModeMarkdownV2", entry.Feed.Title),
			escapeText("ModeMarkdownV2", entry.Feed.Category.Title),
			escapeText("ModeMarkdownV2", entry.URL),
		),
	)
	msg.ReplyMarkup = generateKeyboard(entry, secret)
	msg.ParseMode = "MarkdownV2"
	msg.DisableNotification = silentMessage
	message, err := bot.Send(msg)
	if err != nil {
		fmt.Printf("Error sending message: %v\n", err)
		return
	}

	// Save our message
	var messageEntry models.Message
	messageEntry.ID = entry.ID
	messageEntry.TelegramID = message.MessageID
	messageEntry.SentTime = message.Time()
	err = store.InsertEntry(messageEntry)
	if err != nil {
		fmt.Printf("Error storing message: %v\n", err)
	}
}

func generateKeyboard(entry *miniflux.Entry, secret types.TelegramSecret) tgbotapi.InlineKeyboardMarkup {
	buttons := make(map[string]string)

	if entry.Starred {
		buttons["star"] = "Unstar"
	} else {
		buttons["star"] = "Star"
	}
	buttons["star_data"] = fmt.Sprintf("%s:%v:%v", secret, star, entry.ID)

	if entry.Status == "unread" {
		buttons["unread"] = "Mark as read"
		buttons["unread_data"] = fmt.Sprintf("%s:%v:%v", secret, markRead, entry.ID)
	} else {
		buttons["unread"] = "Mark as unread"
		buttons["unread_data"] = fmt.Sprintf("%s:%v:%v", secret, markUnread, entry.ID)
	}

	buttons["deleteAndMark"] = fmt.Sprintf("%s:%v:%v", secret, deleteAndMark, entry.ID)

	markup := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(buttons["unread"], buttons["unread_data"]),
			tgbotapi.NewInlineKeyboardButtonData(buttons["star"], buttons["star_data"]),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Delete message", deleteMessage),
			tgbotapi.NewInlineKeyboardButtonData("Delete & mark as read", buttons["deleteAndMark"]),
		),
	)
	return markup
}

func answerCallback(bot *tgbotapi.BotAPI, queryID string, reply string) {
	bot.AnswerCallbackQuery(
		tgbotapi.CallbackConfig{
			CallbackQueryID: queryID,
			Text:            reply,
		},
	)
}

func updateKeyboard(bot *tgbotapi.BotAPI, chatID int64, secret types.TelegramSecret, rss *miniflux.Client, messageID int, entry int64) {
	// Get latest entry data
	entryData, err := rss.Entry(entry)
	if err != nil {
		fmt.Printf("Err: %v", err)
	}

	// Generate new keyboard data
	msg := tgbotapi.NewEditMessageReplyMarkup(
		chatID,
		messageID,
		generateKeyboard(entryData, secret),
	)
	bot.Send(msg)
}

// EscapeText takes an input text and escape Telegram markup symbols.
// In this way we can send a text without being afraid of having to escape the characters manually.
// Note that you don't have to include the formatting style in the input text, or it will be escaped too.
// If there is an error, an empty string will be returned.
//
// parseMode is the text formatting mode (ModeMarkdown, ModeMarkdownV2 or ModeHTML)
// text is the input string that will be escaped
func escapeText(parseMode string, text string) string {
	var replacer *strings.Replacer

	if parseMode == "ModeHTML" {
		replacer = strings.NewReplacer("<", "&lt;", ">", "&gt;", "&", "&amp;")
	} else if parseMode == "ModeMarkdown" {
		replacer = strings.NewReplacer("_", "\\_", "*", "\\*", "`", "\\`", "[", "\\[")
	} else if parseMode == "ModeMarkdownV2" {
		replacer = strings.NewReplacer(
			"_", "\\_", "*", "\\*", "[", "\\[", "]", "\\]", "(",
			"\\(", ")", "\\)", "~", "\\~", "`", "\\`", ">", "\\>",
			"#", "\\#", "+", "\\+", "-", "\\-", "=", "\\=", "|",
			"\\|", "{", "\\{", "}", "\\}", ".", "\\.", "!", "\\!",
		)
	} else {
		return ""
	}

	return replacer.Replace(text)
}
