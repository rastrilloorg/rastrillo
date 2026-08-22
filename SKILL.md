---
name: rastrillo
description: Build a multi-user CARLOS app on Rastrillo: GORM models, chi routes, sessions, owner-scoped queries.
---

# Rastrillo

The CARLOS web framework. This file is the app story: read it instead of the
framework source. Module `github.com/carlosframework/rastrillo`; the worked
reference is `examples/notes` (~150 lines of domain, everything else plumbing).

Rastrillo is a middle layer, not a full-stack framework. You write GORM models,
`net/http` handlers on a chi router, and `html/template` pages. The framework
supplies the parts that are hard to get right twice: the database opener, the
session store, the identity plugins, CSRF, owner scoping, form helpers.

## 1. App shape

Five files. Copy the shape exactly.

```
internal/<app>/models.go     plain GORM structs
internal/<app>/app.go        AutoMigrate, sessions, identity plugin, chi router
internal/<app>/handlers.go   the owner-scoped CRUD (the part worth reading)
internal/<app>/render.go     embedded templates, flash/session-aware page data
cmd/<app>/main.go            Resolve -> db.Open -> App -> Serve
```

Imports are `github.com/carlosframework/rastrillo` plus its subpackages
(`.../db`, `.../scope`, `.../sessions`, `.../password`, `.../csrf`, `.../flash`,
`.../form`), `github.com/go-chi/chi/v5`, and `gorm.io/gorm`.

`cmd/<app>/main.go`:

```go
logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

opts, err := rastrillo.Resolve(rastrillo.Options{DBPath: "notes.db", Logger: logger})
if err != nil { logger.Error("resolve activation", "err", err); os.Exit(1) }

d, err := db.Open(opts.DBPath, logger)   // *db.DB; d.G is the *gorm.DB
if err != nil { logger.Error("open database", "err", err); os.Exit(1) }
defer d.Close()

mux, err := notes.App(d, origin, logger)
if err != nil { logger.Error("build app", "err", err); os.Exit(1) }

opts.Mux = mux
opts.DBPath = ""                          // Serve must not open a second handle
if err := rastrillo.Serve(opts); err != nil { logger.Error("serve failed", "err", err); os.Exit(1) }
```

**Use `rastrillo.Resolve` + `rastrillo.Serve`, not `rastrillo.Run`, whenever the
app opens its own database.** `Run` re-parses argv and repopulates
`Options.DBPath`, so `Serve` would open a second connection to the file
`db.Open` already owns. `Resolve` is the documented seam for an app that owns
its handle: it applies the same activation argv and `$STATE_DIRECTORY`
resolution, you blank `DBPath`, and `db.Open`'s eager ping keeps the boot
materialization duty satisfied (a hibernating route's activator replicates a
file that must exist).

The platform contract — activation argv, LISTEN_FDS, $STATE_DIRECTORY,
/healthz, /api/version, SIGTERM drain — is inherited from
rastrillo.Resolve/Serve; never hand-roll any of it.

`Options.Mux` is a `*http.ServeMux`, so the chi router mounts inside one:
`mux := http.NewServeMux(); mux.Handle("/", r)`.

`app.go` — the whole wiring, in order:

```go
func App(d *db.DB, origin string, logger *slog.Logger) (*http.ServeMux, error) {
	if err := d.G.AutoMigrate(&User{}, &Note{}); err != nil { return nil, err }
	for _, stmt := range sessions.Migrations {          // raw SQL, not a GORM model
		if err := d.G.Exec(stmt).Error; err != nil { return nil, err }
	}
	writer, err := d.G.DB()                             // sessions wants the writer *sql.DB
	if err != nil { return nil, err }
	sess, err := sessions.New(sessions.Config{DB: writer, Origin: origin, Logger: logger})
	if err != nil { return nil, err }

	a := &app{db: d.G}
	ph, err := password.New(password.Config{
		Sessions: sess, Lookup: lookupUser(d.G), Create: createUser(d.G),
		RenderSignin: renderSignin, RenderSignup: renderSignup,
	})
	if err != nil { return nil, err }

	r := chi.NewRouter()
	r.Use(csrf.Protect(origin))
	r.Get("/signin", ph.SigninPage);  r.Post("/signin", ph.Signin)
	r.Get("/signup", ph.SignupPage);  r.Post("/signup", ph.Signup)
	r.Post("/signout", ph.Signout)
	r.Group(func(r chi.Router) {
		r.Use(sess.Require)
		r.Get("/", a.listNotes)
		r.Get("/notes/new", a.newNote)
		r.Post("/notes", a.createNote)
		r.Get("/notes/{id}", a.showNote)
		r.Get("/notes/{id}/edit", a.editNote)
		r.Post("/notes/{id}", a.updateNote)
		r.Post("/notes/{id}/delete", a.deleteNote)
	})
	mux := http.NewServeMux()
	mux.Handle("/", r)
	return mux, nil
}
```

Handlers hang off one small struct holding `*gorm.DB`; templates live in
`render.go` behind `//go:embed templates`, parsed one `*template.Template` per
page (layout + that page's file) so two pages can both define `"content"`.

## 2. Data

Models are plain GORM structs — no framework base type, no embedding:

```go
type User struct {
	ID           int64
	Email        string `gorm:"uniqueIndex"`
	PasswordHash string
	CreatedAt    time.Time
}

type Note struct {
	ID        int64
	UserID    int64 `gorm:"index"`   // the owner column scope.Owned filters on
	Title     string
	Body      string
	CreatedAt time.Time
	UpdatedAt time.Time
}
```

`db.Open(path, logger)` returns a `*db.DB` whose `G` is a `*gorm.DB` wired for
SQLite the way a CARLOS app needs it: one file, writer pool capped at one
connection, reader pool of several, routed per statement by `dbresolver`. App
code never picks a pool. The DSN sets `busy_timeout` before
`journal_mode=WAL` (the reverse order crashes on concurrent open) plus
`foreign_keys(1)`. `d.Close()` closes both pools; `d.G.DB()` hands back the
writer `*sql.DB` for packages that want database/sql (sessions does).

Schema changes go through AutoMigrate and are additive-only: never rename or
drop a column on an existing table — add a new column and migrate data in code.

`sessions.Migrations` is a `[]string` of raw SQL, applied with `d.G.Exec` next
to your `AutoMigrate` call — it is not a GORM model and must not be managed by
AutoMigrate.

## 3. Scoping

One seam, used by every read and every write:

```go
func (a *app) owned(r *http.Request) *gorm.DB {
	uid, _ := sessions.UserID(r)
	return scope.Owned(a.db, uid)
}
```

Every query on an owned model goes through `scope.Owned(d.G, uid)` (or
`scope.OwnedBy` for team-owned rows). Never call First/Find/Update/Delete on an
owned model without the owner filter — including inside transactions: a
`d.G.Transaction` callback must apply `scope.Owned` to every statement in it,
the same as outside.

(Inside the callback, scope the callback's `tx` — `scope.Owned(tx, uid)` —
never `d.G`: a `d.G` statement runs outside the transaction, and the writer
pool's one connection is already held by it, so it hangs instead of erroring.)

`scope.Owned(g, owner int64)` adds `WHERE user_id = ?` — the convention column.
`scope.OwnedBy(g, column string, owner any)` takes any owner column and
**panics** if the column is not a plain `lower_snake` identifier (it is
interpolated into SQL, so a non-identifier fails loudly at development time
rather than parsing as SQL).

Scope the *write*, not just the read that loaded the row: the SQL then carries
`WHERE user_id = ? AND id = ?` itself, so a later refactor of the lookup helper
cannot silently turn an update into an IDOR.

```go
n, err := a.find(r)                     // find() = a.owned(r).First(&n, id)
if err != nil { http.NotFound(w, r); return }
...
update := map[string]any{"Title": title, "Body": body}
a.owned(r).Model(&n).Select("Title", "Body", "UpdatedAt").Updates(update)
a.owned(r).Delete(&n)
```

A row that isn't yours is a row that doesn't exist: answer 404, never 403.

That covers a malformed `{id}` too — a URL that was never yours was never a
URL, so `strconv.ParseInt` failing returns `gorm.ErrRecordNotFound` and 404s.

A join table is scoped through BOTH sides: reading or writing a membership row
requires the caller to be authorized on each side it links, checked explicitly
— the stricter reading always wins.

Creating is the one place the owner comes from the session rather than a
filter: `n := Note{UserID: uid, Title: title, Body: body}` with `uid` from
`sessions.UserID(r)` — never from the form.

## 4. Forms and mass assignment

Never bind a request body onto a GORM model — no reflection binding, no loops
over PostForm. Read each permitted field by name (`r.PostFormValue("Title")`)
and write updates through `.Select("Title", "Body").Updates(...)` so an
unexpected form field can never reach a column.

(The string you pass `PostFormValue` is the HTML input's name — the example's
inputs are `title`/`body`. The strings in `Select` are GORM field names, and
they are the allowlist that matters.)

Validate by hand, collect a `form.Errors` (a `map[string]string`, with
`.Any()`), re-render at 422 with the submitted values seeded back so nothing is
retyped:

```go
title, body := r.PostFormValue("title"), r.PostFormValue("body")
if title == "" {
	w.WriteHeader(http.StatusUnprocessableEntity)
	renderContent(w, r, "new", formView{
		Note:   Note{Title: title, Body: body},
		Errors: form.Errors{"Title": "Title is required"},
	})
	return
}
```

Write the status before rendering; the render helper writes none of its own.

Money is stored as `int64` cents. Parse with `form.ParseCents(s)` (rejects more
than two decimals, a leading `$`, or any sign; `""` parses to zero). Seed a form
field with `form.FormatCentsPlain(cents)` — exactly what ParseCents accepts back
— and display with `form.FormatCents(cents)`, which adds the `$`.

After a successful mutation: `flash.Set(w, "notice", "Note created.")` then
`http.Redirect(w, r, "/notes/…", http.StatusSeeOther)`. The render helper calls
`flash.Take(w, r)` exactly once per page and the layout renders it.

## 5. Sessions and identity

`sessions` owns the signed-in state: SQLite-backed rows (so sign-out and
revocation are real), `__Host-` cookies on https origins, 30-day default TTL.
It deliberately does not know how a session is *earned* — an identity plugin
verifies a credential and calls `SignIn`; that one call is the whole contract.

- `csrf.Protect(origin)` mounts app-wide (`r.Use`). It refuses cross-origin
  POST/PUT/PATCH/DELETE by checking `Sec-Fetch-Site`/`Origin`; there are no
  tokens to mint. GET/HEAD/OPTIONS pass untouched.
- Guard signed-in routes with a chi `Group` + `s.Require`: signed-out GET/HEAD
  redirects to `SigninPath` with a same-site `return_to`, anything else is 403.
- `s.Middleware` is the softer variant: it resolves a session onto the request
  context when there is one and blocks nothing — for pages that merely look
  different when signed in.
- `s.RequireFresh(maxAge)` is Require plus step-up: the credential must be
  verified within maxAge; stale GET/HEAD goes to `SigninPath` with `reauth=1`,
  and re-signing-in rotates fresh.
- Read the viewer with `sessions.UserID(r)` (int64, ok) or `sessions.Current(r)`
  (the `Session`: Subject, Method, AuthTime, At). Past a `Require` boundary the
  `ok` is guaranteed only for a plugin whose Subject is a numeric user id, as
  password's is — see the keymail warning below.
- Sign-in redirect targets go through `sessions.SafeReturn(r, "/")` — never a
  raw `return_to`: only a same-site absolute path (one leading `/`, no scheme,
  no backslash) passes; anything else gets the fallback.
- `s.Sweep(time.Now())` deletes expired rows — optional; lookup checks
  expiry itself.

**Password plugin.** `password.New(password.Config{...})` needs `Sessions`,
`Lookup`, and `RenderSignin`; `Create` is optional and disables signup when nil
(SignupPage and Signup 404), and `RenderSignup` is **required whenever Create is
set** — New returns an error otherwise. `Lookup(ctx, email) (id, hash, error)`
returns `sql.ErrNoRows` for an unknown email, which Signin treats identically to
a wrong password (one message; a decoy hash keeps the timing flat). Any error
from `Create(ctx, email, hash) (id,
error)` is reported as a duplicate email. `Signin`, `Signup` and `Signout` are
**POST-only** — they answer 405 to anything else — so mount `SigninPage`/
`SignupPage` on GET and the rest on POST, exactly as in §1. The Render callbacks
take `(w, r, password.PageData)` where PageData is `{Error, Email, ReturnTo}`;
password writes the 422 status itself before re-rendering, so the callback must
not write one.

**Rate limiting:** Signin throttles failed attempts per email — 10 failures in
15 minutes answers 429 until the oldest ages out; success resets the budget.
In-memory, per email; IP throttling stays a deployment concern.
The keymail plugin (`rastrillo/auth`) — the family default: magic-link email
auto-upgrading to keymail — rate-limits via signin: `auth.New(auth.Config{...})` with
`Begin`/`Callback`/`Verify`/`Signout` handlers and `RequireSession`, over the
same `sessions` core. **With keymail, do not use `sessions.UserID`.** Its
Subject is the verified *email*, and `RequireSession` stores the `Identity`
under auth's own context key — so `sessions.UserID(r)` returns `(0, false)`
there, and the §3 seam, which drops that `ok`, would scope every query to
`user_id = 0`: everyone reading everyone. Read the viewer with `auth.From(r)`
and map `Identity.Address` to your user row's id before scoping.

## 6. What NOT to do

- **Do not use the manifest generator for user-owned data.** `rastrillo
  generate` compiles `manifest/` resources into CRUD screens for *standalone
  admin tables only*: one table per resource, three field kinds (text, textarea,
  money), no relations between resources, and **no per-user scoping at all**.
  Anything a user owns gets hand-written handlers over the packages above. (The
  `view` package — `view.Render`, `view.Fail`, `view.ParseID` — is the helper
  set for those *generated* actions, which run against a `*rastrillo.Ctx`; a
  hand-written app like `examples/notes` uses its own render helper instead.)
- **Never import `github.com/glebarez/*` or `gorm.io/driver/sqlite`.**
  `glebarez/sqlite` registers the same driver name `sqlite` that
  `modernc.org/sqlite` does, so a binary holding it and `rastrillo/gormlite`
  panics at init — `sql: Register called twice for driver sqlite`.
  `gorm.io/driver/sqlite` is the cgo one (mattn), which loses the pure-Go build
  and puts a second, differently-configured SQLite in the process. `db.Open`
  already wires `gormlite` over modernc; you should not need a driver import.
- **Never bind a form onto a model, never query an owned model unscoped, never
  answer 403 where 404 is the honest answer.** See §3 and §4.
- **Never `git merge` to main** — not even locally. Every change lands as a pull
  request, squash-merged.

## Checklist before you call an app done

1. Every handler touching an owned model goes through the `owned(r)` seam.
2. Every update names its columns in `.Select(...)`.
3. `csrf.Protect(origin)` is mounted app-wide, above the route groups.
4. Signed-in routes are inside a `Group` with `sess.Require`.
5. `AutoMigrate` plus `sessions.Migrations` both run at boot.
6. `opts.DBPath` is blanked before `Serve` when the app opened its own handle.
7. Not-found and not-yours both answer 404.
