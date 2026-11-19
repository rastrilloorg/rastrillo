package chat

import (
	"net/http"
	"os"
	"slices"
	"strings"

	"amadan.net/rastrillo/rastrillo/action"
	"amadan.net/rastrillo/rastrillo/auth"
	"amadan.net/rastrillo/rastrillo/form"
)

const (
	adminRoomsPage      action.Path = "/admin/rooms"
	adminRoomPage       action.Path = "/admin/rooms/{id}"
	adminRoomDeletePage action.Path = "/admin/rooms/{id}/delete"
)

// requireAdmin lets through only the addresses listed in CHAT_ADMINS.
// Anyone else gets 404, so the admin pages do not show they exist.
func (app *App) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter,
		request *http.Request) {
		identity, _ := auth.From(request)
		admins := strings.Split(os.Getenv("CHAT_ADMINS"), ",")
		if identity.Address == "" ||
			!slices.Contains(admins, identity.Address) {
			ErrorPage(response, request, http.StatusNotFound, "")
			return
		}
		next.ServeHTTP(response, request)
	})
}

// newRoomForm is the admin page: the rooms, and the form that adds one.
type newRoomForm struct {
	app    *App
	parsed *form.Parsed
}

func (page newRoomForm) ShowAgain(
	visit *action.Visit, status int, errors form.Errors,
) {
	rooms, err := page.app.listRooms(visit.Context())
	if visit.Refused(err) {
		return
	}
	visit.Render("admin_rooms", status, map[string]any{
		"Rooms": rooms, "Echo": echoOf(page.parsed), "Errors": errors,
	})
}

func (app *App) adminRooms(visit *action.Visit) {
	newRoomForm{app: app}.ShowAgain(visit, http.StatusOK, nil)
}

func (app *App) addRoom(visit *action.Visit) {
	parsed, err := visit.ReadForm(form.Field{Name: "Name"})
	if visit.RefusedShowing(err, newRoomForm{app, parsed}) {
		return
	}
	room, err := app.createRoom(visit.Context(), parsed.String("Name"))
	if visit.RefusedShowing(err, newRoomForm{app, parsed}) {
		return
	}
	visit.Flash("Added " + room.Name + ".")
	visit.Redirect(string(adminRoomsPage))
}

// renameForm is one room's admin page.
type renameForm struct {
	room   Room
	parsed *form.Parsed
}

func (page renameForm) ShowAgain(
	visit *action.Visit, status int, errors form.Errors,
) {
	visit.Render("admin_room", status, map[string]any{
		"Room": page.room, "Echo": echoOf(page.parsed), "Errors": errors,
	})
}

func (app *App) adminRoom(visit *action.Visit) {
	room, err := app.roomAt(visit)
	if visit.Refused(err) {
		return
	}
	renameForm{room: room}.ShowAgain(visit, http.StatusOK, nil)
}

func (app *App) renameRoomAt(visit *action.Visit) {
	room, err := app.roomAt(visit)
	if visit.Refused(err) {
		return
	}
	parsed, err := visit.ReadForm(form.Field{Name: "Name"})
	if visit.RefusedShowing(err, renameForm{room, parsed}) {
		return
	}
	err = app.renameRoom(visit.Context(), room.ID, parsed.String("Name"))
	if visit.RefusedShowing(err, renameForm{room, parsed}) {
		return
	}
	visit.Flash("Room renamed.")
	visit.Redirect(string(adminRoomsPage))
}

func (app *App) confirmDeleteRoom(visit *action.Visit) {
	room, err := app.roomAt(visit)
	if visit.Refused(err) {
		return
	}
	visit.Render("admin_room_delete", http.StatusOK,
		map[string]any{"Room": room})
}

func (app *App) deleteRoomAt(visit *action.Visit) {
	room, err := app.roomAt(visit)
	if visit.Refused(err) {
		return
	}
	err = app.deleteRoom(visit.Context(), room.ID)
	if visit.Refused(err) {
		return
	}
	visit.Flash("Deleted " + room.Name + ".")
	visit.Redirect(string(adminRoomsPage))
}

// roomAt is the room the URL's {id} names.
func (app *App) roomAt(visit *action.Visit) (Room, error) {
	id, err := visit.PathID("id")
	if err != nil {
		return Room{}, err
	}
	return app.findRoom(visit.Context(), id)
}

// echoOf is what the person typed, to fill the form in again.
func echoOf(parsed *form.Parsed) map[string]string {
	if parsed == nil {
		return map[string]string{}
	}
	return parsed.Echo()
}
