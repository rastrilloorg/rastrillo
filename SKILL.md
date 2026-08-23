---
name: rastrillo
description: Build a multi-user CARLOS app: GORM models, chi routes, sessions, owner-scoped queries.
---

# Rastrillo

The CARLOS web framework. This file is the app story: read it instead of the
source. Module `github.com/carlosframework/rastrillo`; the worked
reference is `examples/notes`.

This file covers the common path completely — build a standard app from it
alone. Where a rare trap is compressed to a sentence here, a **Full
treatment** line points at the page that unpacks it. Each page stands
alone, so read only the one you need: `docs/site/<page>.md` in this repo,
or `curl -s https://rastrillo.org/docs/<page>.md`.

A middle layer, not a full-stack framework: you write GORM models,
`net/http` handlers on a chi router, and `html/template` pages. It supplies
what is hard to get right twice: the database opener, session store, identity
plugins, CSRF, owner scoping, form helpers.

## 1. App shape

Five files, copied exactly, plus `migrations.go` beside `models.go` (§2):

```
internal/<app>/models.go     plain GORM structs
internal/<app>/app.go        migrate.Apply, sessions, identity plugin, router
internal/<app>/handlers.go   the owner-scoped CRUD
internal/<app>/render.go     embedded templates, flash/session-aware page data
cmd/<app>/main.go            Resolve -> db.Open -> App -> Serve
```

Imports: `github.com/carlosframework/rastrillo` and its subpackages
(`.../db`, `.../migrate`, `.../scope`, `.../sessions`, `.../password`,
`.../csrf`, `.../flash`, `.../form`, `.../jobs`), `github.com/go-chi/chi/v5`,
`gorm.io/gorm`.

`cmd/<app>/main.go`:

```go
logger := slog.Default()
opts, err := rastrillo.Resolve(rastrillo.Options{DBPath: "notes.db", Logger: logger})
if err != nil { logger.Error("resolve", "err", err); os.Exit(1) }

d, err := db.Open(opts.DBPath, logger) // *db.DB (§2)
if err != nil { logger.Error("open db", "err", err); os.Exit(1) }
defer d.Close()

mux, err := notes.App(d, origin, logger)
if err != nil { logger.Error("build", "err", err); os.Exit(1) }

opts.Mux = mux
opts.DBPath = ""
if err := rastrillo.Serve(opts); err != nil { logger.Error("serve", "err", err); os.Exit(1) }
```

**Use `rastrillo.Resolve` + `rastrillo.Serve`, not `rastrillo.Run`, whenever
the app opens its own database.** `Run` re-parses argv and repopulates
`Options.DBPath`, so `Serve` opens a second connection to the file `db.Open`
owns. `Resolve` applies the same activation argv and `$STATE_DIRECTORY`
resolution.

The platform contract — activation argv, LISTEN_FDS, $STATE_DIRECTORY,
/healthz, /api/version, SIGTERM drain, baseline security headers (CSP
et al; your own Set wins) — comes from Resolve/Serve; never hand-roll any
of it.

`app.go` — the whole wiring, in order:

```go
func App(d *db.DB, origin string, logger *slog.Logger) (*http.ServeMux, error) {
	if _, err := migrate.Apply(context.Background(), d, BootSchema); err != nil { return nil, err }
	writer, err := d.G.DB()                             // sessions wants the writer *sql.DB
	if err != nil { return nil, err }
	sess, err := sessions.New(sessions.Config{DB: writer, Origin: origin, Logger: logger})
	if err != nil { return nil, err }

	a := &app{db: d.G}
	ph, err := password.New(password.Config{Sessions: sess, Lookup: lookupUser(d.G),
		Create: createUser(d.G), RenderSignin: renderSignin, RenderSignup: renderSignup})
	if err != nil { return nil, err }

	r := chi.NewRouter()
	r.Use(csrf.Protect(origin))
	r.Get("/signin", ph.SigninPage); r.Post("/signin", ph.Signin)
	r.Get("/signup", ph.SignupPage); r.Post("/signup", ph.Signup)
	r.Post("/signout", ph.Signout)
	r.Group(func(r chi.Router) {
		r.Use(sess.Require)
		r.Get("/", a.listNotes) // the rest of the owned-model CRUD, §3/§4
	})
	mux := http.NewServeMux()
	mux.Handle("/", r)
	return mux, nil
}
```

Handlers hang off one struct holding `*gorm.DB`; `render.go` embeds and
parses one `*template.Template` per page (layout + that page) so two pages
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
for SQLite: one file, WAL, a one-connection writer pool, a reader pool of
several, routed per statement. `d.Close()` closes both pools; `d.G.DB()`
returns the writer `*sql.DB` for database/sql packages like sessions.

Schema changes go through `rastrillo migration generate`: edit a model, run
it, read the SQL before committing. Migrations apply once each, at boot,
recorded in a ledger — never re-run, never reversed.

`migrations.go` declares two `*migrate.Set`: `Schema`, the app's own
migrations (what `generate`/`check` diff against `Models`), and
`BootSchema` — `migrate.Merge(sessions.Schema, Schema)`, argument order is
apply order — everything `App()` applies. Add a subsystem to `BootSchema`,
never `Schema`, or `check` proposes dropping a table `Models` doesn't know.
Never edit a shipped migration; add a new one (`generate` may now emit a
full rebuild, not just `ADD COLUMN`). Renames are hand-written
(`rastrillo migration new rename_x`; drop+add is indistinguishable to any
tool); destructive changes need `--allow-destructive`.

**Recovering an old database.** Boot refuses on a structural diff (a
database predating a subsystem's migrations; a manifest resource
reshaped after first boot, so its regenerated SQL no longer matches the
ledger's checksum). Recovery is `migration baseline --db <path> --through
<id>` **first**, *then* the missing migration by hand — that order is
load-bearing, and bare `baseline` silently strands a pending data
migration.
Full treatment: docs/site/migrations.md — rastrillo.org/docs/migrations

**The first deploy of a version with migrations must be schema-neutral.**
Generate `0001_init` from the models *as already deployed* and ship it
alone; change a model only in a later release. Otherwise boot refuses on
the new column, and `baseline` there would strand it for good.

## 3. Scoping

Scoping separates *users* within one instance, never tenants: a CARLOS app
serves one team. A product with many teams gives each team its own instance
(instances hibernate — idle ones cost nothing); isolation is the platform's
process-and-file boundary, not a WHERE clause.

One seam, for every read and write:

```go
func (a *app) owned(r *http.Request) *gorm.DB {
	uid, _ := sessions.UserID(r)
	return scope.Owned(a.db, uid)
}
```

Every query on an owned model goes through `scope.Owned(d.G, uid)` (or
`scope.OwnedBy` for another owner column): never First/Find/Update/Delete
without the owner filter — including inside a `d.G.Transaction` callback,
which must scope its own `tx`, never `d.G` (a `d.G` statement there runs
outside the transaction, whose one writer connection it already holds, so it
hangs instead of erroring).

`scope.Owned(g, owner int64)` adds `WHERE user_id = ?`. `scope.OwnedBy(g,
column string, owner any)` takes any owner column and **panics** unless it's
a plain `lower_snake` identifier — it's interpolated into SQL, so a bad one
fails loudly.

Scope the *write*, not just the read that loaded the row: the SQL then
carries `WHERE user_id = ? AND id = ?` itself, so a later refactor cannot
silently turn an update into an IDOR.

```go
n, err := a.find(r) // a.owned(r).First(&n, id); 404 if err != nil
update := map[string]any{"Title": title, "Body": body}
a.owned(r).Model(&n).Select("Title", "Body", "UpdatedAt").Updates(update)
```

A row that isn't yours is a row that doesn't exist: answer 404, never 403 —
a malformed `{id}` too (`strconv.ParseInt` failing returns
`gorm.ErrRecordNotFound`, also a 404). A join table is scoped through BOTH
sides: a membership row needs the caller authorized on each side it links,
checked explicitly — the stricter reading wins.

Creating is the one place the owner comes from the session, not a filter:
`n := Note{UserID: uid, Title: title, Body: body}`, `uid` from
`sessions.UserID(r)` — never from the form.

## 4. Forms and mass assignment

Never bind a request body onto a GORM model — no reflection binding, no
PostForm loops. Read each permitted field by name (`r.PostFormValue("Title")`,
the HTML input's name) and write through `.Select("Title", "Body").Updates(...)`
(`Select`'s strings are GORM field names, the allowlist that matters) so an
unexpected field can never reach a column.

Validate with `form.Parse` — one declaration per field — and re-render at
422 with values seeded back so nothing is retyped:

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

Money is `int64` cents (`form.Money`, read with `p.Cents`, parsed via
`form.ParseCents` — no `$`/sign, at most two decimals, `""` = zero). Seed
with `form.FormatCentsPlain(cents)`; display with `form.FormatCents(cents)`
(adds the `$`).

After a successful mutation: `flash.Set(w, "notice", "Note created.")`, then
`http.Redirect(w, r, "/notes/…", http.StatusSeeOther)`. The render helper
calls `flash.Take(w, r)` once per page; the layout renders it.

## 5. Sessions and identity

`sessions` owns the signed-in state: SQLite-backed rows (so sign-out and
revocation are real), `__Host-` cookies on https origins, 30-day default
TTL. An identity plugin verifies a credential and calls `SignIn`; that call
is the whole contract.

- `csrf.Protect(origin)` mounts app-wide (`r.Use`): refuses cross-origin
  POST/PUT/PATCH/DELETE via `Sec-Fetch-Site`/`Origin`, no tokens to mint.
- Guard signed-in routes with a chi `Group` + `s.Require`: signed-out
  GET/HEAD redirects to `SigninPath` with a same-site `return_to`; anything
  else 403s.
- `s.Middleware` is softer: resolves a session when there is one, blocks
  nothing.
- `s.RequireFresh(maxAge)` is Require plus step-up: past maxAge, GET/HEAD
  goes to `SigninPath?reauth=1`; re-signing-in or a `passkey` assertion
  rotates fresh (2FA: `Config.SecondFactor` = `passkey`'s `Gate`). Lost
  passkey: a recovery code redeems at POST `/passkey/signin/recovery`,
  minted by `RegenerateRecoveryCodes` behind `RequireFresh`.
  Full treatment: docs/site/passkeys.md — rastrillo.org/docs/passkeys
- Read the viewer with `sessions.UserID(r)` (int64, ok) or
  `sessions.Current(r)` (the `Session`: Subject, Method, AuthTime, At). Past
  `Require` the `ok` holds only for a plugin whose Subject is a numeric user
  id — see the `auth` warning below.
  Full treatment: docs/site/magic-links.md — rastrillo.org/docs/magic-links
- Sign-in redirect targets go through `sessions.SafeReturn(r, "/")` — never
  a raw `return_to`: only a same-site absolute path (one leading `/`, no
  scheme or backslash) passes.
- `s.Sweep(time.Now())` deletes expired rows.

**Password plugin.** `password.New(password.Config{...})` needs `Sessions`,
`Lookup`, `RenderSignin`; `Create` disables signup when nil (SignupPage and
Signup 404), and `RenderSignup` is **required whenever Create is set** — New
errors otherwise. `Lookup(ctx, email) (id, hash, error)` returns
`sql.ErrNoRows` for an unknown email, treated like a wrong password (a
decoy hash flattens timing). Any error from `Create` reads as a duplicate
email, unless it wraps `password.ErrRefused` (use `password.Refuse(msg)`),
which renders that message at 403.
`Signin`/`Signup`/`Signout` are **POST-only** — Page variants on GET, the
rest on POST, 405 to anything else. Render callbacks take
`(w, r, password.PageData)` = `{Error, Email, ReturnTo}`;
password writes the 422 itself, so the callback must not.

**Rate limiting:** Signin and Signup share a per-email budget — 10 failures in
15 minutes answers 429 until one ages out, success resets it, in-memory (IP
throttling is deployment's). The magic-link plugin (`rastrillo/auth`: sign-in
by emailed link, upgrading itself to the keymail ceremony for the few
addresses that have one) rate-limits likewise: `auth.New(auth.Config{...})`
with `Begin`/`Callback`/`Verify`/`Signout` and `RequireSession`, over the same
`sessions` core. **Under `auth`, do not use `sessions.UserID`:** its Subject
is the verified *email* on both paths, so it returns `(0, false)`, and the §3
seam, dropping that `ok`, would scope every query to `user_id = 0`. Read the viewer with `auth.From(r)` or `sessions.Current(r)`
(`RequireSession` stashes both) and map the address to your user row's id
before scoping.

## 6. Background work

`jobs` runs observable goroutines, in-memory: a restart kills them, and fn's
ctx expires at 15 min (the job turns Failed) — keep jobs idempotent and
honor ctx. `j := jobs.New(logger)` once; `j.Start(owner, name, location,
fn)` runs `fn(ctx, progress func(string)) error`, returning `(Job, error)`:
`ErrOwnerBusy` past 4 Running jobs per owner — flash your own copy. 303 the
caller to `/jobs/`+job.ID. Owner is the session **Subject**, not
`sessions.UserID` — key job rows the same way; fn's error text reaches the
owner. `j.Get(id, owner)` 404s a foreign/unknown id.

`jobs.NewHandlers(jobs.Config{Jobs, Render, RenderFragment})` returns
`StatusPage`/`Fragment`/`Events` (SSE), erroring unless all three set; mount
them in the `sess.Require` group at `/jobs/{id}`, `/jobs/{id}/fragment` and
`/jobs/{id}/events`, each taking `jobs.PageData{Job, FragmentPath,
EventsPath, PollSeconds}`: Render draws a whole page, RenderFragment the
partial *alone*, or the layout nests on the next poll.
Done+Location 303s from StatusPage; Fragment answers 204 +
`Rastrillo-Location`. **Must work with scripts off:** a `<noscript>` meta
refresh of `PollSeconds`, only while Running, or a failed page refreshes
forever.

The only JavaScript is app-owned `static/rastrillo.js`, inert until markup
opts in: `data-poll="URL"` + `data-poll-every="2"` swap the element for the
fetched fragment and repeat while the new fragment still carries
`data-poll` (ui's `job-status` partial drops it once done, stopping the
loop); the partial's `PushURL` (= `EventsPath`) emits `data-poll-push`, and
the shim rides SSE, falling back to polling itself.
`data-busy`/`data-busy-label` disable/retitle a submit button.

## 7. What NOT to do

- **Manifests: declare what fits the vocabulary, hand-write the rest.** A
  `manifest/*.toml` resource generates CRUD screens — three field kinds
  (text, textarea, money), no relations. `store = "exclusive"` (default) is
  one SQL table, `"mergeable"` an `eventlog` stream per record.
  `scope = "user"` owner-filters either by session subject (someone else's
  row 404s); mount behind `sessions.Require`/`auth.RequireSession`.
  Full treatment: docs/site/manifests.md — rastrillo.org/docs/manifests
- **Never import `github.com/glebarez/*` or `gorm.io/driver/sqlite`.**
  `glebarez/sqlite` registers the driver name `sqlite` that
  `modernc.org/sqlite` already does, so a binary with it and
  `rastrillo/gormlite` panics at init. `gorm.io/driver/sqlite` is the cgo
  one; `db.Open` already wires `gormlite` over modernc.
- **Never bind a form onto a model, query an owned model unscoped, or answer
  403 where 404 is honest.** See §3, §4.
- **Never `git merge` to main**, not even locally: every change is a PR,
  squash-merged.

## Checklist before you call an app done

1. Every handler on an owned model goes through the `owned(r)` seam.
2. Every update names its columns in `.Select(...)`.
3. `csrf.Protect(origin)` is mounted app-wide, above the route groups.
4. Signed-in routes, jobs' included, are in a `Group` with `sess.Require`.
5. One `migrate.Apply` runs at boot; `make ci` runs `rastrillo migration check`.
6. `opts.DBPath` is blanked before `Serve` when the app opened its own handle.
7. Not-found and not-yours both answer 404.
