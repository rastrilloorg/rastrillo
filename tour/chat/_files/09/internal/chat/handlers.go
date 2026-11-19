package chat

import (
	"net/http"

	"amadan.net/rastrillo/rastrillo/action"
)

const roomPage action.Path = "/rooms/{id}"

func (app *App) home(visit *action.Visit) {
	rooms, err := app.listRooms(visit.Context())
	if visit.Refused(err) {
		return
	}
	visit.Render("index", http.StatusOK, map[string]any{"Rooms": rooms})
}

func (app *App) room(visit *action.Visit) {
	room, err := app.roomAt(visit)
	if visit.Refused(err) {
		return
	}
	visit.Render("room", http.StatusOK, map[string]any{"Room": room})
}
