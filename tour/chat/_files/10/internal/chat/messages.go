package chat

import (
	"context"
	"slices"
	"strings"
	"unicode/utf8"

	"amadan.net/rastrillo/rastrillo/op"
)

// A message is one line of chat, not a letter.
const messageLimit = 500

// The room page shows this many of the newest messages.
const messagesShown = 100

func (app *App) recentMessages(
	ctx context.Context, roomID int64,
) ([]Message, error) {
	var messages []Message
	err := app.database.G.WithContext(ctx).Where("room_id = ?", roomID).
		Order("id desc").Limit(messagesShown).Find(&messages).Error
	slices.Reverse(messages) // oldest first, as a chat reads
	return messages, err
}

func (app *App) postMessage(
	ctx context.Context, roomID int64, sender, text string,
) error {
	text = strings.TrimSpace(text)
	if text == "" || utf8.RuneCountInString(text) > messageLimit {
		return op.Invalid{Fields: map[string]string{
			"Text": "Write a message of 1 to 500 characters.",
		}}
	}
	if _, err := app.findRoom(ctx, roomID); err != nil {
		return err
	}
	message := Message{RoomID: roomID, Sender: sender, Text: text}
	return app.database.G.WithContext(ctx).Create(&message).Error
}
