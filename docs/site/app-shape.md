# 🤖 The shape of an app

A Rastrillo app is five files plus `migrations.go`. `rastrillo new`
writes all of them, and they are worth reading in order before you
change anything.

```text
internal/<app>/models.go       plain GORM structs
internal/<app>/migrations.go   Schema and BootSchema
internal/<app>/app.go          migrate.Apply, sessions, identity plugin, router
internal/<app>/handlers.go     the owner-scoped CRUD
internal/<app>/render.go       embedded templates, flash/session-aware page data
cmd/<app>/main.go              Resolve -> db.Open -> App -> Serve
```

`examples/notes` in the framework repo is the worked reference.

## main.go

```go
logger := slog.Default()
opts, err := rastrillo.Resolve(rastrillo.Options{DBPath: "notes.db", Logger: logger})
if err != nil {
	logger.Error("resolve", "err", err)
	os.Exit(1)
}

d, err := db.Open(opts.DBPath, logger)
if err != nil {
	logger.Error("open db", "err", err)
	os.Exit(1)
}
defer d.Close()

mux, err := notes.App(d, origin, logger)
if err != nil {
	logger.Error("build", "err", err)
	os.Exit(1)
}

opts.Mux = mux
opts.DBPath = ""
if err := rastrillo.Serve(opts); err != nil {
	logger.Error("serve", "err", err)
	os.Exit(1)
}
```

Two lines there are easy to miss: set `opts.Mux` after `App` returns,
and blank `opts.DBPath` so `Serve` does not open the database file a
second time.

### Use Resolve and Serve, not Run

`rastrillo.Run` is the right call when you let the framework own the
database. Your scaffolded app does not — it calls `db.Open` itself — so
it uses `Resolve` and `Serve` instead.

The reason is small and annoying. `Run` re-parses argv and repopulates
`Options.DBPath`, so `Serve` would open a second connection to the file
`db.Open` already owns. `Resolve` does the same activation-argv and
`$STATE_DIRECTORY` work without the serving, and hands you the resolved
options.

### What you get from Resolve and Serve

The whole platform contract, and you should never hand-roll any of it:
activation argv in both shapes the platform uses, `LISTEN_FDS` socket
activation, `$STATE_DIRECTORY` resolution for a relative database path,
`GET /healthz` and `GET /api/version`, the SIGTERM drain, and baseline
security headers with your own `Set` winning.

[Deploying](/docs/deploying) covers what the platform does with all of
that.

## app.go

The whole wiring, in order:

```go
func App(d *db.DB, origin string, logger *slog.Logger) (*http.ServeMux, error) {
	if _, err := migrate.Apply(context.Background(), d, BootSchema); err != nil {
		return nil, err
	}
	writer, err := d.G.DB()
	if err != nil {
		return nil, err
	}
	sess, err := sessions.New(sessions.Config{DB: writer, Origin: origin, Logger: logger})
	if err != nil {
		return nil, err
	}

	a := &app{db: d.G}
	ph, err := password.New(password.Config{
		Sessions: sess, Lookup: lookupUser(d.G), Create: createUser(d.G),
		RenderSignin: renderSignin, RenderSignup: renderSignup,
	})
	if err != nil {
		return nil, err
	}

	r := chi.NewRouter()
	r.Use(csrf.Protect(origin))
	r.Get("/signin", ph.SigninPage)
	r.Post("/signin", ph.Signin)
	r.Get("/signup", ph.SignupPage)
	r.Post("/signup", ph.Signup)
	r.Post("/signout", ph.Signout)
	r.Group(func(r chi.Router) {
		r.Use(sess.Require)
		r.Get("/", a.listNotes)
	})
	mux := http.NewServeMux()
	mux.Handle("/", r)
	return mux, nil
}
```

The order matters in a few places. `migrate.Apply` runs before anything
reads a table — [Migrations](/docs/migrations) explains `BootSchema`.
`csrf.Protect(origin)` goes on app-wide, above the route groups, so a
route you add in six months is protected without you remembering
anything. Signed-in routes live in a `chi.Group` with `sess.Require`,
while sign-in and sign-up sit outside it, since a signed-out visitor has
to be able to reach them.

`App` returns the `*http.ServeMux` that `main.go` hands to `Serve` as
`opts.Mux`.

## handlers.go

Handlers hang off one struct holding the `*gorm.DB`:

```go
type app struct{ db *gorm.DB }

func (a *app) owned(r *http.Request) *gorm.DB {
	uid, _ := sessions.UserID(r)
	return scope.Owned(a.db, uid)
}
```

That `owned(r)` method is the most important line in the app.
[Scoping](/docs/scoping) explains what goes wrong without it, and the
one identity plugin where dropping the `ok` is a bug.

## render.go

Parse one `*template.Template` per page — layout plus that page —
instead of one tree containing everything. Two pages can then both
define `"content"`, which they otherwise could not.

This is also where `flash.Take(w, r)` gets called, once per page, so the
layout can render a notice. [Templates](/docs/templates) covers what is
available inside them.

`render.go` also holds `ErrorPage`, a `rastrillo.ErrorPageFunc` that
renders `templates/errors.html` — `ui`'s `error-page` partial inside
your own layout. `main.go` points `opts.ErrorPage` at it so a panic gets
a real page; wire it to `Ctx.ErrorPage` too and the 500 a handler
answers looks the same as the 500 a panic answers.

## Before you call it done

1. Every handler on an owned model goes through the `owned(r)` method.
2. Every update names its columns in `.Select(...)`.
3. `csrf.Protect(origin)` is mounted app-wide, above the route groups.
4. Signed-in routes, jobs' included, are in a group with `sess.Require`.
5. One `migrate.Apply` runs at boot, and `make ci` runs
   `rastrillo migration check`.
6. `opts.DBPath` is blanked before `Serve` when your app opened its own
   handle.
7. Not-found and not-yours both answer 404.
