package parse

import (
	"strings"
	"testing"
)

func TestAPIKey(t *testing.T) {
	var tests = []struct {
		explanation   string
		key           string
		validExpected bool
	}{
		{
			"15 character secret is valid",
			strings.Repeat("a", 15),
			true,
		}, {
			"Colon character is invalid",
			strings.Repeat("a", 20) + ":",
			false,
		}, {
			"Long string is invalid",
			strings.Repeat("a", 50),
			false,
		}, {
			"Randomly generated string is valid",
			"oohue2Oob3leyah",
			true,
		},
	}

	for _, tt := range tests {
		_, err := TelegramSecret(tt.key)
		if (err == nil) != tt.validExpected {
			t.Errorf("%s: input [%s], got %v, want %v", tt.explanation, tt.key, err, tt.validExpected)
		}
	}
}
