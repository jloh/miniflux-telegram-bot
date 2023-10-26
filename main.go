package main

import (
	"embed"
	"fmt"
	"log/slog"
	"math/rand"
	"os"
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
	viper.SetDefault("TELEGRAM_ALLOWED_USERNAME", "")
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath("/etc/miniflux_bot/")
	viper.AddConfigPath(".")
	viper.AutomaticEnv()

	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err != nil {
		slog.Warn("Error reading in config file", "error", err)
	}

	// Pass migrations to storage
	sqlite.EmbedMigrations = embedMigrations

	// Set ChatID
	chatID := viper.GetInt64("TELEGRAM_CHAT_ID")
	if chatID == 0 {
		slog.Error("TELEGRAM_CHAT_ID is not set")
		os.Exit(1)
	}

	// Check secret is set and valid
	telegramSecret, err := parse.TelegramSecret(viper.GetString("TELEGRAM_SECRET"))
	if err != nil {
		slog.Error("TELEGRAM_SECRET setting is invalid", "error", err)
		os.Exit(1)
	}

	// Setup RSS instance
	rss := miniflux.New(viper.GetString("MINIFLUX_URL"), viper.GetString("MINIFLUX_API_KEY"))

	// Get latest entry
	latestEntries, err := rss.Entries(&miniflux.Filter{Status: miniflux.EntryStatusUnread, Limit: 1, Direction: "desc", Order: "id"})
	if err != nil {
		slog.Error("Cannot find latest entry", "error", err)
		os.Exit(1)
	}

	// Set latest entry
	latestEntryID := latestEntries.Entries[0].ID

	// Get our DB going
	store := sqlite.New()

	// Initialise Telegram bot instance
	bot, err := tgbotapi.NewBotAPI(viper.GetString("TELEGRAM_BOT_TOKEN"))
	if err != nil {
		slog.Error("Failed initialising Telegram", "error", err)
		os.Exit(1)
	}

	slog.Info("Starting Miniflux Bot", "version", version, "built", date, "commit", commit[:8])

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
			slog.Error("Failed getting entries", "error", err)
		} else {
			if entries.Total != 0 {
				for _, entry := range entries.Entries {
					latestEntryID = entry.ID
					if ignoredCategoryID(entry.Feed.Category.ID) {
						slog.Info("Skipping entry as it's in an ignored category", "entry", entry.ID)
						continue
					} else {
						if err := sendMsg(bot, chatID, telegramSecret, entry, viper.GetBool("TELEGRAM_SILENT_NOTIFICATION"), true, store); err != nil {
							slog.Error("Failed sending message", "error", err, "entry", entry.ID)
						} else {
							slog.Info("Message sent for entry", "entry", entry.ID)
						}
					}
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
		slog.Error("Failed getting Telegram updates", "error", err)
	}

	allowed_username := viper.GetString("TELEGRAM_ALLOWED_USERNAME")

	for update := range updates {
		// Check whether we're a command
		if update.Message != nil && update.Message.IsCommand() {
			if allowed_username != "" && update.Message.From.UserName != allowed_username {
				slog.Error("Received command from invalid user", slog.String("user", update.Message.From.UserName))
				continue
			}
			switch update.Message.Command() {
			case "randomunread":
				unreadEntries, err := rss.Entries(&miniflux.Filter{Status: miniflux.EntryStatusUnread})
				if err != nil {
					slog.Error("Failed getting unread entries from Miniflux", "error", err)
				}

				// Select a random entry from the list

				if err := sendMsg(bot, chatID, secret, unreadEntries.Entries[rand.Intn(len(unreadEntries.Entries))], false, false, store); err != nil {
					slog.Error("Failed sending message for random entry", "error", err)
				}
			case "start":
				startMessage := fmt.Sprintf("Your Miniflux Bot is online! Running %v built %v (Commit %s)", version, date, commit[:8])
				if err = sendText(bot, chatID, startMessage, false); err != nil {
					slog.Error("Failed sending message for start command", "error", err)
				}
			}
		}
		// Check whether we've got a Callback Query
		if update.CallbackQuery != nil {

			// Double check if the callback is from our expected chat
			if update.CallbackQuery.From.ID != int(chatID) {
				slog.Warn("Callback from unexpected chat ID, ignoring", "chat_id", update.CallbackQuery.From.ID)
				continue
			}

			var entryID int64
			// Split our string
			callback := strings.Split(update.CallbackQuery.Data, ":")

			// Check our secret
			if callback[0] != string(secret) {
				slog.Warn("Callback contained invalid secret, ignoring", "callback", callback)
				continue
			}

			// If we've got an entry, try and get it
			if len(callback) == 3 {
				entryID, err = strconv.ParseInt(callback[2], 10, 64)
				if err != nil {
					slog.Error("Failed parsing entry ID", "error", err)
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
			slog.Error("Failed getting saved entries", "error", err)
		}

		for _, entry := range entries {
			if currentTime.Sub(entry.SentTime).Hours() < 48 {

				// We can edit the message!
				minifluxEntry, err := rss.Entry(entry.ID)
				if err != nil {
					slog.Error("Failed getting Miniflux entry", "error", err)
					continue
				}

				// If we're read, were updated > 2 hours ago and set to delete it, cleanup the message
				if (minifluxEntry.Status == "read") && (currentTime.Sub(minifluxEntry.ChangedAt).Hours() > 2) && entry.DeleteRead {
					_, err := bot.DeleteMessage(tgbotapi.NewDeleteMessage(chatID, entry.TelegramID))
					slog.Info("Deleting message for read entry", "entry", entry.ID)
					if err != nil {
						slog.Error("Failed deleting message in Telegram", "error", err)
					}
					// Cleanup entry in DB
					err = store.DeleteEntryByID(entry.ID)
					if err != nil {
						slog.Error("Error deleting entry in storage", "error", err)
					}
				} else if minifluxEntry.ChangedAt.Truncate(time.Second).After(entry.UpdatedTime) {
					// If entry has been updated in Miniflux (marked as read, starred etc) update Telegram keyboard
					// Note: We're required to truncate Miniflux's time since it stores it down to the millisecond which the bot doesn't
					// Without truncating it its always seen as "after" so we constantly update
					slog.Info("Updating keyboard for entry", "entry", entry.ID)
					updateKeyboard(bot, chatID, secret, rss, entry.TelegramID, entry.ID)
					err := store.UpdateEntryTime(entry.ID, minifluxEntry.ChangedAt)
					if err != nil {
						slog.Error("Failed updating entry in storage", "error", err)
					}
				}
			} else {
				// Cleanup the DB entry since there is nothing we can do with it
				if err := store.DeleteEntryByID(entry.ID); err != nil {
					slog.Error("Error deleting entry in storage", "error", err)
				} else {
					slog.Info("Cleaned up old entry in storage", "entry", entry.ID)
				}
			}
		}

		// Sleep for 10 minutes until we check again
		time.Sleep(time.Duration(10 * time.Minute))
	}
}

func sendText(bot *tgbotapi.BotAPI, chatID int64, msgStr string, silentMessage bool) error {
	msg := tgbotapi.NewMessage(chatID, msgStr)
	msg.DisableNotification = silentMessage
	_, err := bot.Send(msg)
	return err
}

func sendMsg(bot *tgbotapi.BotAPI, chatID int64, secret types.TelegramSecret, entry *miniflux.Entry, silentMessage bool, deleteRead bool, store store.Store) error {
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
		return err
	}

	// Save our message
	var messageEntry models.Message
	messageEntry.ID = entry.ID
	messageEntry.TelegramID = message.MessageID
	messageEntry.SentTime = message.Time()
	messageEntry.UpdatedTime = entry.ChangedAt
	messageEntry.DeleteRead = deleteRead
	if err := store.InsertEntry(messageEntry); err != nil {
		return err
	}

	return nil
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
			tgbotapi.NewInlineKeyboardButtonData("Delete message", fmt.Sprintf("%s:%v", secret, deleteMessage)),
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
		slog.Error("Error updating keyboard", "error", err)
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

// Check whether the CategoryID is in our ignored list
func ignoredCategoryID(categoryID int64) bool {
	ignoredCategories := viper.GetStringSlice("MINIFLUX_IGNORED_CATEGORIES")
	for _, ignoredCategory := range ignoredCategories {
		if strconv.FormatInt(categoryID, 10) == ignoredCategory {
			return true
		}
	}
	return false
}
