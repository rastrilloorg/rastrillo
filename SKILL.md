---
name: rastrillo
description: Build a multi-user CARLOS app on Rastrillo: GORM models, chi routes, sessions, owner-scoped queries.
---

# Rastrillo

The CARLOS web framework. This file is the app story: read it instead of the
source. Module `github.com/carlosframework/rastrillo`; the worked
reference is `examples/notes`.

Rastrillo is a middle layer, not a full-stack framework. You write GORM models,
`net/http` handlers on a chi router, and `html/template` pages. It supplies
what is hard to get right twice: the database opener, session store, identity
plugins, CSRF, owner scoping, form helpers.

## 1. App shape

Five files, copied exactly:

```
internal/<app>/models.go     plain GORM structs
internal/<app>/app.go        AutoMigrate, sessions, identity plugin, chi router
internal/<app>/handlers.go   the owner-scoped CRUD
internal/<app>/render.go     embedded templates, flash/session-aware page data
cmd/<app>/main.go            Resolve -> db.Open -> App -> Serve
```

Imports are `github.com/carlosframework/rastrillo` plus its subpackages
(`.../db`, `.../scope`, `.../sessions`, `.../password`, `.../csrf`, `.../flash`,
`.../form`, `.../jobs`), `github.com/go-chi/chi/v5`, and `gorm.io/gorm`.

`cmd/<app>/main.go`:

```go
logger := slog.Default()
opts, err := rastrillo.Resolve(rastrillo.Options{DBPath: "notes.db", Logger: logger})
if err != nil { logger.Error("resolve", "err", err); os.Exit(1) }

d, err := db.Open(opts.DBPath, logger)   // *db.DB; d.G is the *gorm.DB
if err != nil { logger.Error("open db", "err", err); os.Exit(1) }
defer d.Close()

mux, err := notes.App(d, origin, logger)
if err != nil { logger.Error("build", "err", err); os.Exit(1) }

opts.Mux = mux
opts.DBPath = ""                          // Serve must not open a second handle
if err := rastrillo.Serve(opts); err != nil { logger.Error("serve", "err", err); os.Exit(1) }
```

**Use `rastrillo.Resolve` + `rastrillo.Serve`, not `rastrillo.Run`, whenever the
app opens its own database.** `Run` re-parses argv and repopulates
`Options.DBPath`, so `Serve` opens a second connection to the file `db.Open`
owns. `Resolve` applies the same activation argv and `$STATE_DIRECTORY`
resolution; blank `DBPath`, and `db.Open`'s eager ping satisfies the boot
materialization duty.

The platform contract — activation argv, LISTEN_FDS, $STATE_DIRECTORY,
/healthz, /api/version, SIGTERM drain — comes from Resolve/Serve; never
hand-roll any of it.

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
	r.Get("/signin", ph.SigninPage); r.Post("/signin", ph.Signin)
	r.Get("/signup", ph.SignupPage); r.Post("/signup", ph.Signup)
	r.Post("/signout", ph.Signout)
	r.Group(func(r chi.Router) {
		r.Use(sess.Require)
		r.Get("/", a.listNotes)
		r.Get("/notes/new", a.newNote);        r.Post("/notes", a.createNote)
		r.Get("/notes/{id}/edit", a.editNote); r.Post("/notes/{id}", a.updateNote)
		r.Get("/notes/{id}", a.showNote)
		r.Post("/notes/{id}/delete", a.deleteNote)
	})
	mux := http.NewServeMux()
	mux.Handle("/", r)
	return mux, nil
}
```

Handlers hang off one struct holding `*gorm.DB`; `render.go` embeds templates
and parses one `*template.Template` per page (layout + that page) so two pages
can both define `"content"`.

## 2. Data

Models are plain GORM structs — no base type, no embedding:

```go
type User struct {
	ID           int64
	Email        string `gorm:"uniqueIndex"`
	PasswordHash string
}

type Note struct {
	ID        int64
	UserID    int64 `gorm:"index"`   // the owner column scope.Owned filters on
	Title     string
	Body      string
	UpdatedAt time.Time
}
```

`db.Open(path, logger)` returns a `*db.DB` whose `G` is the `*gorm.DB`, wired
for SQLite: one file, a one-connection writer pool, a reader pool of several,
routed per statement by `dbresolver`. The DSN
sets `busy_timeout` before `journal_mode=WAL` (the reverse order crashes on
concurrent open) plus `foreign_keys(1)`. `d.Close()` closes both
pools; `d.G.DB()` returns the writer `*sql.DB` for database/sql packages like
sessions.

AutoMigrate changes are additive-only: never rename or drop a column — add a
new one and migrate data in code. `sessions.Migrations` is raw SQL for
`d.G.Exec` beside that call, never a GORM model — keep it out of AutoMigrate.

## 3. Scoping

One seam, for every read and write:

```go
func (a *app) owned(r *http.Request) *gorm.DB {
	uid, _ := sessions.UserID(r)
	return scope.Owned(a.db, uid)
}
```

Every query on an owned model goes through `scope.Owned(d.G, uid)` (or
`scope.OwnedBy` for team-owned rows): never First/Find/Update/Delete without
the owner filter — including inside transactions, where a `d.G.Transaction`
callback applies `scope.Owned` to every statement, the same as outside.

(Scope the callback's `tx`, never `d.G`: a `d.G` statement runs outside the
transaction, whose one writer connection it already holds, so it hangs instead
of erroring.)

`scope.Owned(g, owner int64)` adds `WHERE user_id = ?` — the convention column.
`scope.OwnedBy(g, column string, owner any)` takes any owner column and
**panics** unless the column is a plain `lower_snake` identifier: it is
interpolated into SQL, so a bad one fails loudly.

Scope the *write*, not just the read that loaded the row: the SQL then carries
`WHERE user_id = ? AND id = ?` itself, so a later refactor cannot silently turn
an update into an IDOR.

```go
n, err := a.find(r)                     // find() = a.owned(r).First(&n, id)
if err != nil { http.NotFound(w, r); return }
...
update := map[string]any{"Title": title, "Body": body}
a.owned(r).Model(&n).Select("Title", "Body", "UpdatedAt").Updates(update)
a.owned(r).Delete(&n)
```

A row that isn't yours is a row that doesn't exist: answer 404, never 403.

A malformed `{id}` too: `strconv.ParseInt` failing returns
`gorm.ErrRecordNotFound` and 404s.

A join table is scoped through BOTH sides: a membership row needs the caller
authorized on each side it links, checked explicitly.

Creating is the one place the owner comes from the session, not a filter:
`n := Note{UserID: uid, Title: title, Body: body}`, `uid` from
`sessions.UserID(r)` — never from the form.

## 4. Forms and mass assignment

Never bind a request body onto a GORM model — no reflection binding, no
PostForm loops. Read each permitted field by name (`r.PostFormValue("Title")`)
and write through `.Select("Title", "Body").Updates(...)` so an unexpected
field can never reach a column.

(`PostFormValue` takes the input's name; `Select`'s
strings are GORM field names — the allowlist that matters.)

Validate with `form.Parse` — one declaration per field — and re-render at 422
with values seeded back so nothing is retyped:

```go
p := form.Parse(r, form.Field{Name: "title", Required: true},
	form.Field{Name: "body", Kind: form.Textarea})
if !p.OK() {
	w.WriteHeader(http.StatusUnprocessableEntity)
	renderContent(w, r, "new", formView{Errors: p.Errors(),
		Note: Note{Title: p.String("title"), Body: p.String("body")}})
	return
}
```

`p.Echo()` is the seed-back map for map-shaped views. Write the status before
rendering; the helper writes none.

Money is `int64` cents: a `form.Money` field, read with `p.Cents`, parses via
`form.ParseCents` — no `$` or sign, at most two decimals; `""` is zero. Seed
with `form.FormatCentsPlain(cents)`; display with `form.FormatCents(cents)`,
(adds the `$`).

After a successful mutation: `flash.Set(w, "notice", "Note created.")` then
`http.Redirect(w, r, "/notes/…", http.StatusSeeOther)`. The render helper calls
`flash.Take(w, r)` exactly once per page and the layout renders it.

## 5. Sessions and identity

`sessions` owns the signed-in state: SQLite-backed rows (so sign-out and
revocation are real), `__Host-` cookies on https origins, 30-day default TTL.
An identity plugin verifies a
credential and calls `SignIn`; that call is the whole contract.

- `csrf.Protect(origin)` mounts app-wide (`r.Use`). It refuses cross-origin
  POST/PUT/PATCH/DELETE via `Sec-Fetch-Site`/`Origin`; no tokens to mint.
- Guard signed-in routes with a chi `Group` + `s.Require`: signed-out GET/HEAD
  redirects to `SigninPath` with a same-site `return_to`; anything else 403s.
- `s.Middleware` is softer: resolves a session when there is one, blocks
  nothing.
- `s.RequireFresh(maxAge)` is Require plus step-up: the credential must be
  verified within maxAge; stale GET/HEAD goes to `SigninPath` with `reauth=1`;
  re-signing-in or a `passkey` assertion rotates fresh. Sign-in-time
  2FA: the plugin's `Config.SecondFactor` takes `passkey`'s `Gate`. Lost
  passkey: a recovery code redeems at POST `/passkey/signin/recovery`;
  mint via `RegenerateRecoveryCodes` behind `RequireFresh`.
- Read the viewer with `sessions.UserID(r)` (int64, ok) or `sessions.Current(r)`
  (the `Session`: Subject, Method, AuthTime, At). Past `Require` the `ok` holds
  only for a plugin whose Subject is a numeric user id, as password's is — see
  the keymail warning below.
- Sign-in redirect targets go through `sessions.SafeReturn(r, "/")` — never a
  raw `return_to`: only a same-site absolute path (one leading `/`, no scheme
  or backslash) passes.
- `s.Sweep(time.Now())` deletes expired rows.

**Password plugin.** `password.New(password.Config{...})` needs `Sessions`,
`Lookup`, `RenderSignin`; `Create` is optional and disables signup when nil
(SignupPage and Signup 404), and `RenderSignup` is **required whenever Create
is set** — New errors otherwise. `Lookup(ctx, email) (id, hash, error)` returns
`sql.ErrNoRows` for an unknown email, which Signin treats like a wrong password
(one message; a decoy hash flattens timing). Any error from
`Create(ctx, email, hash) (id, error)` reads as a duplicate email. `Signin`,
`Signup`, `Signout` are **POST-only** — 405 to anything else — so mount
`SigninPage`/`SignupPage` on GET and the rest on POST, as in §1. Render
callbacks take `(w, r, password.PageData)` = `{Error, Email, ReturnTo}`;
password writes the 422 itself before re-rendering, so the callback must not.

**Rate limiting:** Signin throttles failures per email — 10 in 15 minutes
answers 429 until one ages out; success resets it. In-memory; IP throttling is
a deployment concern.
The keymail plugin (`rastrillo/auth`) — the family default: magic-link email
auto-upgrading to keymail — rate-limits via `signin`:
`auth.New(auth.Config{...})` with `Begin`/`Callback`/`Verify`/`Signout`
handlers and `RequireSession`, over the same `sessions` core. **With keymail, do not use `sessions.UserID`.** Its
Subject is the verified *email*, so it returns `(0, false)` — and the §3 seam,
which drops that `ok`, would scope every query to `user_id = 0`.
Read the viewer with `auth.From(r)` or `sessions.Current(r)`
(`RequireSession` stashes both) and map the address to your user row's id
before scoping.

## 6. Background work

`jobs` runs observable goroutines, in-memory: a restart kills them, and fn's
ctx expires at 15 min (the job turns Failed) — keep jobs idempotent and honor
ctx. `j := jobs.New(logger)` once, then
`j.Start(owner, name, location, fn)` runs `fn(ctx, progress func(string))
error`, returning `(Job, error)`: `ErrOwnerBusy` past 4 Running jobs per owner
— flash your own copy. 303 the caller to `/jobs/`+job.ID. Owner is
the session **Subject**, not `sessions.UserID` — key rows the job writes the
same way; fn's error text reaches the owner. `j.Get(id, owner)` answers only
the owner, so foreign/unknown ids 404.

`jobs.NewHandlers(jobs.Config{Jobs, Render, RenderFragment})` returns
`StatusPage`, `Fragment` and `Events` (SSE), erroring unless all three are
set; mount them in the `sess.Require` group at `/jobs/{id}`,
`/jobs/{id}/fragment` and `/jobs/{id}/events`. Handlers take
`jobs.PageData{Job, FragmentPath, EventsPath, PollSeconds}`: Render draws a
page,
RenderFragment the partial *alone*.
Done+Location 303s from StatusPage; Fragment answers 204 +
`Rastrillo-Location`. **The page must work with scripts off:** emit a
`<noscript>` meta refresh of `PollSeconds` *only* while Running, or a failed
page refreshes forever.

The only JavaScript is `static/rastrillo.js` — app-owned, scaffolded, inert
until markup opts in: `data-poll="URL"` +
`data-poll-every="2"` swap the element for the fetched fragment and repeat
while the *new* fragment still carries `data-poll`; ui's `job-status` partial
emits it only while running, which is how polling stops. The partial's `PushURL` (= `EventsPath`) emits `data-poll-push`: the shim
rides SSE, falling back to polling itself. `data-busy` on a form
disables its submit buttons, `data-busy-label` retitles them.

## 7. What NOT to do

- **Manifests: declare what fits the vocabulary, hand-write the rest.** A
  `manifest/*.toml` resource generates CRUD screens — three field kinds
  (text, textarea, money), no relations. `store = "exclusive"` (default) is
  one SQL table; `store = "mergeable"` keeps each record as an `eventlog`
  stream — derived reads, tombstone deletes.
  `scope = "user"` owner-filters either store by the session subject
  (someone else's row 404s); mount those routes behind
  `sessions.Require`/`auth.RequireSession`.
- **Never import `github.com/glebarez/*` or `gorm.io/driver/sqlite`.**
  `glebarez/sqlite` registers the same driver name `sqlite` that
  `modernc.org/sqlite` does, so a binary with it and `rastrillo/gormlite`
  panics at init. `gorm.io/driver/sqlite` is the cgo one; `db.Open`
  already wires `gormlite` over modernc.
- **Never bind a form onto a model, query an owned model unscoped, or answer
  403 where 404 is honest.** See §3 and §4.
- **Never `git merge` to main**, not even locally: every change lands as a
  pull request, squash-merged.

## Checklist before you call an app done

1. Every handler on an owned model goes through the `owned(r)` seam.
2. Every update names its columns in `.Select(...)`.
3. `csrf.Protect(origin)` is mounted app-wide, above the route groups.
4. Signed-in routes, jobs' included, are in a `Group` with `sess.Require`.
5. `AutoMigrate` plus `sessions.Migrations` both run at boot.
6. `opts.DBPath` is blanked before `Serve` when the app opened its own handle.
7. Not-found and not-yours both answer 404.
