package main

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/spf13/viper"
	"miniflux.app/client"
	miniflux "miniflux.app/client"
)

const (
	markRead      = "markRead"
	markUnread    = "markUnread"
	deleteAndMark = "deleteAndMark"
	deleteMessage = "deleteMessage"
	star          = "star"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
	builtBy = "unknown"
)

func main() {
	// Get config
	viper.SetDefault("MINIFLUX_URL", "https://reader.miniflux.app")
	viper.SetDefault("TELEGRAM_CHAT_ID", 0)
	viper.AutomaticEnv() // read in environment variables that match

	// Set ChatID
	chatID := viper.GetInt64("TELEGRAM_CHAT_ID")
	if chatID == 0 {
		log.Fatalln("TELEGRAM_CHAT_ID is not set")
	}

	// Setup RSS instance
	rss := miniflux.New(viper.GetString("MINIFLUX_URL"), viper.GetString("MINIFLUX_API_KEY"))

	// Get latest entry
	latestEntries, err := rss.Entries(&miniflux.Filter{Status: miniflux.EntryStatusUnread, Limit: 1, Direction: "desc", Order: "id"})
	if err != nil {
		log.Fatalf("Cannot find latest entry: %v", err)
	}

	// Get latest entry ID
	latestEntryID := latestEntries.Entries[0].ID

	bot, err := tgbotapi.NewBotAPI(viper.GetString("TELEGRAM_BOT_TOKEN"))
	if err != nil {
		log.Panic(err)
	}

	go listenForMessages(bot, chatID, rss)

	for {
		entries, err := rss.Entries(&miniflux.Filter{Status: miniflux.EntryStatusUnread, Order: "id", AfterEntryID: latestEntryID})

		if err != nil {
			fmt.Printf("Error getting entries: %v", err)
		} else {
			if entries.Total != 0 {
				fmt.Println("Found new entries:")
				for _, entry := range entries.Entries {
					fmt.Printf("%v by %v\n", entry.Title, entry.Feed.Title)
					sendMsg(bot, chatID, entry, true)
					latestEntryID = entry.ID
				}
			}
		}
		time.Sleep(1 * time.Minute)
	}
}

func listenForMessages(bot *tgbotapi.BotAPI, chatID int64, rss *client.Client) {
	poll := tgbotapi.NewUpdate(0)
	poll.Timeout = 120

	updates, err := bot.GetUpdatesChan(poll)
	if err != nil {
		fmt.Printf("Err: %v", err)
	}

	for update := range updates {
		// Check whether we've got a Callback Query
		if update.CallbackQuery != nil {
			var entryID int64
			// Split our string
			callback := strings.Split(update.CallbackQuery.Data, ":")

			// If we've got an entry, try and get it
			if len(callback) == 2 {
				entryID, err = strconv.ParseInt(callback[1], 10, 64)
				if err != nil {
					fmt.Printf("Err: %v", err)
				}
			}

			switch callback[0] {
			case markRead:
				if err := rss.UpdateEntries([]int64{entryID}, "read"); err != nil {
					answerCallback(bot, update.CallbackQuery.ID, "Error marking entry as read")
				} else {
					answerCallback(bot, update.CallbackQuery.ID, "Marked entry as read")
					updateKeyboard(bot, chatID, rss, update.CallbackQuery.Message.MessageID, entryID)
				}
			case markUnread:
				if err := rss.UpdateEntries([]int64{entryID}, "unread"); err != nil {
					answerCallback(bot, update.CallbackQuery.ID, "Error marking entry as unread")
				} else {
					answerCallback(bot, update.CallbackQuery.ID, "Marked entry as unread")
					updateKeyboard(bot, chatID, rss, update.CallbackQuery.Message.MessageID, entryID)
				}
			case deleteAndMark:
				if err := rss.UpdateEntries([]int64{entryID}, "read"); err != nil {
					answerCallback(bot, update.CallbackQuery.ID, "Error marking entry as read")
				} else {
					bot.DeleteMessage(tgbotapi.NewDeleteMessage(chatID, update.CallbackQuery.Message.MessageID))
					answerCallback(bot, update.CallbackQuery.ID, "Deleted message & marked as read")
				}
			case deleteMessage:
				bot.DeleteMessage(tgbotapi.NewDeleteMessage(chatID, update.CallbackQuery.Message.MessageID))
				answerCallback(bot, update.CallbackQuery.ID, "Deleted message")
			case star:
				if err := rss.ToggleBookmark(entryID); err != nil {
					answerCallback(bot, update.CallbackQuery.ID, "Error updating Miniflux entry")
					fmt.Printf("Erorr talking to Miniflux: %v\n", err)
				} else {
					go answerCallback(bot, update.CallbackQuery.ID, "Updated entry")
					go updateKeyboard(bot, chatID, rss, update.CallbackQuery.Message.MessageID, entryID)
				}
			}
		}
	}
}

func sendMsg(bot *tgbotapi.BotAPI, chatID int64, entry *client.Entry, silentMessage bool) {
	msg := tgbotapi.NewMessage(chatID, escapeText("ModeMarkdownV2", fmt.Sprintf("*%s*\n%s in %s\n%s", entry.Title, entry.Feed.Title, entry.Feed.Category.Title, entry.URL)))
	msg.ReplyMarkup = generateKeyboard(entry)
	msg.ParseMode = "MarkdownV2"
	msg.DisableNotification = silentMessage
	bot.Send(msg)
}

func escapeText(parseMode string, text string) string {
	var replacer *strings.Replacer

	if parseMode == "ModeHTML" {
		replacer = strings.NewReplacer("<", "&lt;", ">", "&gt;", "&", "&amp;")
	} else if parseMode == "ModeMarkdown" {
		replacer = strings.NewReplacer("_", "\\_", "*", "\\*", "`", "\\`", "[", "\\[")
	} else if parseMode == "ModeMarkdownV2" {
		replacer = strings.NewReplacer(
			"_", "\\_", "[", "\\[", "]", "\\]", "(",
			"\\(", ")", "\\)", "~", "\\~", "`", "\\`", ">", "\\>",
			"#", "\\#", "+", "\\+", "-", "\\-", "=", "\\=", "|",
			"\\|", "{", "\\{", "}", "\\}", ".", "\\.", "!", "\\!",
		)
	} else {
		return ""
	}

	return replacer.Replace(text)
}

func generateKeyboard(entry *client.Entry) tgbotapi.InlineKeyboardMarkup {
	buttons := make(map[string]string)

	if entry.Starred {
		buttons["star"] = "Unstar"
	} else {
		buttons["star"] = "Star"
	}
	buttons["star_data"] = fmt.Sprintf("%v:%v", star, entry.ID)

	if entry.Status == "unread" {
		buttons["unread"] = "Mark as read"
		buttons["unread_data"] = fmt.Sprintf("%v:%v", markRead, entry.ID)
	} else {
		buttons["unread"] = "Mark as unread"
		buttons["unread_data"] = fmt.Sprintf("%v:%v", markUnread, entry.ID)
	}

	buttons["deleteAndMark"] = fmt.Sprintf("%v:%v", deleteAndMark, entry.ID)

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

func updateKeyboard(bot *tgbotapi.BotAPI, chatID int64, rss *client.Client, messageID int, entry int64) {
	// Get latest entry data
	entryData, err := rss.Entry(entry)
	if err != nil {
		fmt.Printf("Err: %v", err)
	}

	// Generate new keyboard data
	msg := tgbotapi.NewEditMessageReplyMarkup(
		chatID,
		messageID,
		generateKeyboard(entryData),
	)
	bot.Send(msg)
}
