package parse

import (
	"errors"
	"regexp"

	"go.jloh.dev/miniflux-telegram-bot/types"
)

// We limit the size to 15 characters as callback data can only be 1-64 bytes
var secretPattern = regexp.MustCompile("^[A-Za-z0-9]{1,15}$")

func TelegramSecret(secret string) (types.TelegramSecret, error) {
	if !secretPattern.MatchString(secret) {
		return "", errors.New("Invalid API key")

	}
	return types.TelegramSecret(secret), nil
}
