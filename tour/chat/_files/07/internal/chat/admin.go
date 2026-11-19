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

const adminRoomsPage action.Path = "/admin/rooms"

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

// echoOf is what the person typed, to fill the form in again.
func echoOf(parsed *form.Parsed) map[string]string {
	if parsed == nil {
		return map[string]string{}
	}
	return parsed.Echo()
}
