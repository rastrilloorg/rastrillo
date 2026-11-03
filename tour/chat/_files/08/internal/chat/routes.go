package chat

import (
	"net/http"

	"amadan.net/rastrillo/rastrillo/action"
	"amadan.net/rastrillo/rastrillo/csrf"
)

const homePage action.Path = "/"
const accountPage action.Path = "/account"
const livePage action.Path = "/live"

// Routes registers everything the app serves. The API and MCP are outside
// the page group: a bearer token is their only credential, so they get no
// CSRF check and no page middleware.
func Routes(routes *action.Routes, app *App) {
	routes.Mount("/static", assets.Handler(), "static")
	routes.Mount("/api/v1/mcp", app.mcp, "mcp")
	routes.Mount("/api/v1", app.api, "api")
	// Every page and form goes in this group: it is what checks CSRF.
	routes.Group(func(guarded *action.Routes) {
		guarded.Guard("csrf", csrf.Protect(app.origin))
		guarded.Guard("pages", pages.Middleware)
		guarded.Guard("signedIn", app.signedIn)
		app.mountAuth(guarded)
		guarded.Get(homePage, app.home)
		// Owned pages and the stream: a visitor signs in first.
		guarded.Group(func(owned *action.Routes) {
			owned.Guard("session", app.auth.RequireSession)
			owned.Get(accountPage, app.account)
			owned.Group(func(admin *action.Routes) {
				admin.Guard("admin", app.requireAdmin)
				admin.Get(adminRoomsPage, app.adminRooms)
				admin.Post(adminRoomsPage, app.addRoom)
				admin.Get(adminRoomPage, app.adminRoom)
				admin.Post(adminRoomPage, app.renameRoomAt)
				admin.Get(adminRoomDeletePage, app.confirmDeleteRoom)
				admin.Post(adminRoomDeletePage, app.deleteRoomAt)
			})
			owned.Handle(http.MethodGet, livePage, app.live.Handler(),
				"live")
		})
	})
}
