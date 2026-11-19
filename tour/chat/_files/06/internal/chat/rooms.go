package chat

import (
	"context"
	"strings"
	"unicode/utf8"

	"amadan.net/rastrillo/rastrillo/op"
)

// A name longer than this would not fit the room list on a phone.
const roomNameLimit = 40

func (app *App) listRooms(ctx context.Context) ([]Room, error) {
	var rooms []Room
	err := app.database.G.WithContext(ctx).Order("name").Find(&rooms).Error
	return rooms, err
}

func (app *App) createRoom(ctx context.Context, name string) (Room, error) {
	room := Room{Name: strings.TrimSpace(name)}
	if err := checkRoomName(room.Name); err != nil {
		return room, err
	}
	err := app.database.G.WithContext(ctx).Create(&room).Error
	return room, nameTaken(err)
}

func checkRoomName(name string) error {
	if name == "" || utf8.RuneCountInString(name) > roomNameLimit {
		return op.Invalid{Fields: map[string]string{
			"Name": "Give the room a name of 1 to 40 characters.",
		}}
	}
	return nil
}

// nameTaken turns the unique index's refusal into a message the form
// can show beside the name.
func nameTaken(err error) error {
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return op.Invalid{Fields: map[string]string{
			"Name": "There is already a room with that name.",
		}}
	}
	return err
}
