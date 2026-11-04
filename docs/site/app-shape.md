# 🤖 The shape of an app

Before you change a new app, read these files in order.

```text
cmd/<app>/main.go              Resolve, db.Open, New, Serve
internal/<app>/app.go          App, New and Router
internal/<app>/routes.go       Routes: every path the app serves
internal/<app>/handlers.go     the actions
internal/<app>/render.go       the pages, contextFor and ErrorPage
internal/<app>/api.go          the JSON API at /api/v1
internal/<app>/models.go       plain GORM structs
internal/<app>/migrations.go   Schema and BootSchema
```

An app made with `--auth=magic` also has `auth.go`, which signs people
in by emailed link and holds the `owned` seam.

## main.go

```go
started := time.Now()
logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
options, err := rastrillo.Resolve(rastrillo.Options{
	DBPath:        "notes.db",
	Logger:        logger,
	Locales:       genlocales.Locales,
	DefaultLocale: genlocales.DefaultLocale,
})
exitOnError(logger, "resolve activation", err)
database, err := db.Open(options.DBPath, logger)
exitOnError(logger, "open database", err)
defer database.Close()
origin := rastrillo.Origin("NOTES_ORIGIN", logger)
// ... the base path, from RASTRILLO_SLUG
mux, hub, err := notes.New(database, origin, logger)
exitOnError(logger, "build app", err)
notes.Configure(&options, mux, hub, started)
options.DBPath = ""
exitOnError(logger, "serve", rastrillo.Serve(options))
```

Keep these two lines: `Configure` sets `options.Mux` to the app, and
`options.Live` to its [live pages](/docs/live) hub, so shutdown ends
the open streams instead of waiting for them.
`options.DBPath = ""` stops `Serve` from opening the database file a
second time.

### Use Resolve and Serve

Use `Resolve` and `Serve` because this app opens its database with
`db.Open`.

`Run` re-parses argv and fills in `Options.DBPath` again, so `Serve`
would open a second connection to the file opened by `db.Open`. Use
`Resolve` to do the activation-argv and `$STATE_DIRECTORY` work without
serving; it returns the resolved options.

### What you get from Resolve and Serve

Use the framework's start-up support for activation argv in both shapes
the platform uses, `LISTEN_FDS` socket activation, `$STATE_DIRECTORY`
resolution for a relative database path, `GET /healthz` and
`GET /api/version`, the SIGTERM drain, and baseline security headers.
Your own `Set` wins.

[Deploying](/docs/deploying) covers what the platform does with all of
that.

## app.go

`App` holds what the actions need. Its fields are unexported.

```go
type App struct {
	database *db.DB
	logger   *slog.Logger
	origin   string
	api      http.Handler
	mcp      http.Handler
}
```

`New` builds the app and returns the `*http.ServeMux` that `Configure`
hands to `Serve`. It calls `Router`, which does the work:

```go
migrated, err := migrate.Apply(context.Background(), database, BootSchema)
// ...
api, mcp, err := newDoors(database, logger)
// ...
app := &App{database: database, logger: logger, origin: origin,
	api: api, mcp: mcp}
router := chi.NewRouter()
Routes(action.NewRoutes(router, app.contextFor), app)
```

`migrate.Apply` runs before anything reads a table.
[Migrations](/docs/migrations) explains `BootSchema`. The perf test
calls `Router` too, so build anything the app needs at boot there.

## routes.go

```go
const homePage action.Path = "/"

func Routes(routes *action.Routes, app *App) {
	routes.Mount("/static", assets.Handler(), "static")
	routes.Mount("/api/v1/mcp", app.mcp, "mcp")
	routes.Mount("/api/v1", app.api, "api")
	routes.Group(func(guarded *action.Routes) {
		guarded.Guard("csrf", csrf.Protect(app.origin))
		guarded.Guard("pages", pages.Middleware)
		guarded.Get(homePage, app.home)
	})
}
```

`Routes` registers everything the app serves. Put every page and form
in the guarded group: its `csrf` guard refuses a write from another
origin. Keep the static files, API and MCP endpoints outside it. A
bearer token is the only credential the API and MCP accept, so neither
gets a CSRF check.

`Routes` must only register routes. `rastrillo routes` calls it with an
empty `App` to list them, and `make routes` fails for an action that no
route mounts. See [CLI](/docs/cli#rastrillo-routes).

In an `--auth=magic` app, put sign-in in the guarded group and pages
that need a session in their own group. A signed-out visitor who opens
one of those pages is sent to sign in.

```go
guarded.Group(func(owned *action.Routes) {
	owned.Guard("session", app.auth.RequireSession)
	owned.Get(accountPage, app.account)
})
```

## handlers.go

Each page is an action: a method on `App` that takes one
`*action.Visit`.

```go
func (app *App) home(visit *action.Visit) {
	visit.Render("index", http.StatusOK, nil)
}
```

An action reads its input, runs one operation, answers a refusal, and
says what success looks like. [action](/docs/reference/action) shows
an action with a form.

In an `--auth=magic` app, every query on an owned row goes through
`app.owned`, in `auth.go`:

```go
func (app *App) owned(request *http.Request) *gorm.DB {
	return scope.OwnedBy(app.database.G, "owner", ownerOf(request))
}
```

Use this method to scope owned rows. [Scoping](/docs/scoping) explains
what goes wrong without it.

## render.go

`render.go` parses every page once, at boot. Add a new page's name to
the list in `newPages`.

`contextFor` builds each request's `Ctx`. Its `Render` draws through
that request, so `visit.Render` uses the request's language and base
path.

`ErrorPage` draws `templates/errors.html` for a refusal and, through
`Options.ErrorPage`, for a panic. A 500 from an action looks the same
as a 500 from a panic. [Templates](/docs/templates) covers what a page
can use.

## api.go

`newDoors` builds the JSON API at `/api/v1` and MCP at `/api/v1/mcp`
from one config, so both apply the same checks. `authorize` is the one
policy for pages, the API and MCP. The API and MCP endpoints cannot see
page middleware, so add page rules to `authorize` too. [JSON API](/docs/api)
and [MCP](/docs/mcp) cover the rest.

## Before you call it done

1. Every action on an owned model goes through `app.owned`.
2. Every update names its columns in `.Select(...)`.
3. Every page and form is in the guarded group in `Routes`.
4. Signed-in pages are behind the session guard.
5. One `migrate.Apply` runs at boot, and `make ci` runs
   `rastrillo migration check`.
6. `options.DBPath` is blanked before `Serve`.
7. Not-found and not-yours both answer 404.
8. `make ci` passes, `make code` and `make routes` included.
