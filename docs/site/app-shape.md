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

### Resolve and Serve, not Run

`rastrillo.Run` exists and is the right call for an app that lets the
framework own the database. **When your app opens its own database, use
`Resolve` and `Serve` instead.**

`Run` re-parses argv and repopulates `Options.DBPath`, so `Serve` would
open a second connection to the file `db.Open` already owns. `Resolve`
performs exactly the same activation-argv and `$STATE_DIRECTORY`
resolution without the serving, and hands you the resolved options.

Note the two lines that matter after `App` returns: set `opts.Mux`, and
blank `opts.DBPath` so `Serve` does not open the file again.

### What Resolve and Serve give you

The platform contract, in full, and none of it is yours to hand-roll:

- activation argv in both shapes the platform uses — `-socket`/`-addr`/`-db`
  flags for an agent exec child, a bare `serve` subcommand for a unit tenant
- `LISTEN_FDS` socket activation
- `$STATE_DIRECTORY` resolution for a relative database path
- `GET /healthz` and `GET /api/version`
- the SIGTERM drain
- baseline security headers — CSP and the rest, framework-owned and
  outermost, with your own `Set` winning

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

Four things about that order:

1. **`migrate.Apply` first**, before anything reads a table.
   [Migrations](/docs/migrations) explains `BootSchema`.
2. **`csrf.Protect(origin)` is mounted app-wide**, above the route
   groups, so a route added later is protected by default rather than by
   remembering. [Sessions](/docs/sessions) covers what it checks.
3. **Signed-in routes live in a `chi.Group` with `sess.Require`.** Sign-in
   and sign-up sit outside it, because a signed-out visitor has to reach
   them.
4. `App` returns a `*http.ServeMux` wrapping the chi router, which is
   what `main.go` hands to `Serve` as `opts.Mux`.

## handlers.go

Handlers hang off one struct holding the `*gorm.DB`:

```go
type app struct{ db *gorm.DB }

func (a *app) owned(r *http.Request) *gorm.DB {
	uid, _ := sessions.UserID(r)
	return scope.Owned(a.db, uid)
}
```

That `owned(r)` seam is the single most important line in the app.
[Scoping](/docs/scoping) explains what goes wrong without it, and the
one case where dropping the `ok` is a bug.

## render.go

One `*template.Template` per page — layout plus that page — rather than
one tree containing everything. Two pages can then both define
`"content"`, which they otherwise could not.

The render helper is also where `flash.Take(w, r)` is called, once per
page, so the layout can render a notice. [Templates](/docs/templates)
covers the component vocabulary available inside them.

## Before you call it done

1. Every handler on an owned model goes through the `owned(r)` seam.
2. Every update names its columns in `.Select(...)`.
3. `csrf.Protect(origin)` is mounted app-wide, above the route groups.
4. Signed-in routes, jobs' included, are in a group with `sess.Require`.
5. One `migrate.Apply` runs at boot, and `make ci` runs
   `rastrillo migration check`.
6. `opts.DBPath` is blanked before `Serve` when the app opened its own
   handle.
7. Not-found and not-yours both answer 404.
