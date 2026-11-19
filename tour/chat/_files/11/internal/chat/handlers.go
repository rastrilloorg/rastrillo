package chat

import (
	"net/http"

	"amadan.net/rastrillo/rastrillo/action"
	"amadan.net/rastrillo/rastrillo/form"
)

const (
	roomPage     action.Path = "/rooms/{id}"
	messagesPage action.Path = "/rooms/{id}/messages"
)

func (app *App) home(visit *action.Visit) {
	rooms, err := app.listRooms(visit.Context())
	if visit.Refused(err) {
		return
	}
	visit.Render("index", http.StatusOK, map[string]any{"Rooms": rooms})
}

// roomForm is a room's page: its messages, and the form to post one.
type roomForm struct {
	app    *App
	room   Room
	parsed *form.Parsed
}

func (page roomForm) ShowAgain(
	visit *action.Visit, status int, errors form.Errors,
) {
	messages, err := page.app.recentMessages(visit.Context(), page.room.ID)
	if visit.Refused(err) {
		return
	}
	visit.Render("room", status, map[string]any{
		"Room": page.room, "Messages": messages,
		"Topic": roomTopic(page.room.ID),
		"Echo":  echoOf(page.parsed), "Errors": errors,
	})
}

func (app *App) room(visit *action.Visit) {
	room, err := app.roomAt(visit)
	if visit.Refused(err) {
		return
	}
	roomForm{app: app, room: room}.ShowAgain(visit, http.StatusOK, nil)
}

func (app *App) postMessageAt(visit *action.Visit) {
	room, err := app.roomAt(visit)
	if visit.Refused(err) {
		return
	}
	parsed, err := visit.ReadForm(form.Field{Name: "Text"})
	if visit.RefusedShowing(err, roomForm{app, room, parsed}) {
		return
	}
	err = app.postMessage(visit.Context(), room.ID,
		ownerOf(visit.Request()), parsed.String("Text"))
	if visit.RefusedShowing(err, roomForm{app, room, parsed}) {
		return
	}
	app.live.Notify(roomTopic(room.ID))
	visit.Redirect(roomPage.For(room.ID))
}
