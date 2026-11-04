package chat

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"

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

func (app *App) findRoom(ctx context.Context, id int64) (Room, error) {
	var room Room
	err := app.database.G.WithContext(ctx).First(&room, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return room, op.NotFound{}
	}
	return room, err
}

func (app *App) renameRoom(ctx context.Context, id int64, name string) error {
	name = strings.TrimSpace(name)
	if err := checkRoomName(name); err != nil {
		return err
	}
	renamed := app.database.G.WithContext(ctx).Model(&Room{}).
		Where("id = ?", id).Update("name", name)
	if renamed.Error == nil && renamed.RowsAffected == 0 {
		return op.NotFound{}
	}
	return nameTaken(renamed.Error)
}

func (app *App) deleteRoom(ctx context.Context, id int64) error {
	deleted := app.database.G.WithContext(ctx).Delete(&Room{}, id)
	if deleted.Error == nil && deleted.RowsAffected == 0 {
		return op.NotFound{}
	}
	return deleted.Error
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
