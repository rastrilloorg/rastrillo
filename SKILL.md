---
name: rastrillo
description: "Build a multi-user CARLOS app: GORM models or plain SQL, chi routes, sessions, owner-scoped queries."
---

# Rastrillo

CARLOS middle layer, not full-stack: you write GORM models (or plain
SQL), `net/http` handlers on a chi router and `html/template`
pages; it supplies the database opener, session store, identity
plugins, CSRF, owner scoping, form helpers. Module `amadan.net/rastrillo/rastrillo`; worked
reference `examples/notes` — in the repository, **not** in the
published module (Go excludes nested modules from a zip), so read it
there, never in your checkout. The topics below hold what it shows,
sign-in and tests included, in a page each: read the topic first, and
the example only for what no topic covers. Rare traps get one sentence plus a page:
`docs/site/<page>.md`, or `curl -s https://rastrillo.org/docs/<page>.md`.

## Load a topic before its work

This file is what every task needs. The rest is in `skill/`, beside
this file (`$(go list -m -f '{{.Dir}}' amadan.net/rastrillo/rastrillo)/skill/`).
Read the topic before you start work it covers, not after the source:

| Before you... | read |
|---|---|
| add password sign-in to an app that has none | `skill/add-signin.md` |
| use the session, magic links, a sign-in screen, 2FA | `skill/signin.md` |
| add a page or route, a flow, a test, or meet a perf/budget failure | `skill/screens.md` |
| call a `ui` partial: its keys, at a glance | `skill/partials.md` |
| style a page, format numbers, add a locale, a menu, a picture grid | `skill/design.md` |
| take money, dates, URLs or uploads in a form, or protect a public form | `skill/forms.md` |
| choose GORM or SQL, store files, write a migration, add teams or roles | `skill/data.md` |
| run work in the background, poll for progress, schedule | `skill/background.md` |
| use the CLI beyond `new`, manifests, doctor, pins, native or PWA | `skill/tooling.md` |
| serve JSON at `/api/v1`, mint tokens, let an agent act for a person, write `Authorize`, hand-write an operation | `skill/api.md` |

## Start here

```sh
go install amadan.net/rastrillo/rastrillo/cmd/rastrillo@latest
rastrillo new notes && cd notes && go mod tidy && go test ./...
```

**People sign in? Start with `rastrillo new --auth=magic <name>`.** The
app then signs people in by emailed link from its first commit, its
owned rows go through `a.owned(r)`, and its tests sign in for you; its
`AGENTS.md` says where each piece is. Without `--auth` the scaffold has
no accounts.

`rastrillo new` writes the whole app shape, its gate (`make ci`) and
its tests; it compiles, passes and serves before you write a line, so
never hand-write any of it. The scaffolded `AGENTS.md` is the source of
truth for that app's code; this file stays the framework's.

**Reach for a manifest before hand-writing.** A `manifest/*.toml`
resource generates CRUD screens (field kinds text, textarea, money;
`scope = "user"` owner-filters by session subject), correct by
construction; run `rastrillo generate`, commit `gen/` and `api/v1.json`, and pass
`gen.Boot`'s result to `gen.Router`. docs/site/manifests.md

**Look before you build.** Most things an app needs already ship: a
Light/Dark/System switch in every shell, a picture grid (`media-grid`),
the API token page (`api-tokens`), job status pages, money fields,
error pages. Check the topic index
above before writing one.

## App shape

Five files plus `migrations.go`: `internal/<app>/models.go` (plain
GORM structs), `app.go` (wiring), `handlers.go` (owned CRUD),
`render.go` (embedded templates, page data), `cmd/<app>/main.go`.

`main.go`, in order: `opts, err := rastrillo.Resolve(rastrillo.Options{DBPath:
"notes.db", Logger: logger})`; `d, err := db.Open(opts.DBPath, logger)`;
`defer d.Close()`; `mux, err := notes.App(d, origin, logger)`;
`notes.Configure(&opts, mux, started)`; `opts.DBPath = ""`;
`rastrillo.Serve(opts)`. **Use `Resolve` + `Serve`, never `Run`, when
the app opens its own database:** `Run` re-parses argv and repopulates
`Options.DBPath`, so `Serve` opens a second connection to the file
`db.Open` owns. Resolve/Serve supply the platform contract (activation,
/healthz, SIGTERM drain, security headers); never hand-roll it.

`app.go`'s `Router`, in order: `migrate.Apply(ctx, d, BootSchema)`;
`r := chi.NewRouter()`; `r.Use(csrf.Protect(origin))` app-wide; public
routes; owned routes inside `r.Group(func(r chi.Router) {
r.Use(sess.Require); ... })` (sessions: `skill/signin.md`). `render.go`
builds `ui.Pages` once at boot; a new page goes in its list
(`skill/screens.md`).

## Owner scoping

One seam, every read and write (`a.db` is the scaffold's `*db.DB`;
`.G` is its `*gorm.DB`): `func (a *app) owned(r *http.Request) *gorm.DB
{ uid, _ := sessions.UserID(r); return scope.Owned(a.db.G, uid) }`.
`scope.Owned(g, owner)` adds `WHERE user_id = ?`; `scope.OwnedBy(g,
column, owner)` takes another owner column, panicking unless plain
`lower_snake` (it is interpolated into SQL). Never
First/Find/Update/Delete an owned model without the filter — in
`d.G.Transaction` too: scope the callback's `tx`, never `d.G` (a `d.G`
statement there hangs on the writer connection the transaction holds).

Scope the write, not just the read that loaded the row: after
`a.owned(r).First(&n, id)` (404 on err),
`a.owned(r).Model(&n).Select("Title", "Body", "UpdatedAt").Updates(...)`.
A row that isn't yours doesn't exist: 404, never 403 — malformed `{id}`
too. A join table is scoped through BOTH sides. Creating takes the
owner from the session, never the form: `Note{UserID: uid, ...}`.

Never bind a request body onto a model: read each permitted field by
name and write through `.Select(...)`, the allowlist keeping unexpected
fields out of columns. Validate with `form.Parse` (`skill/forms.md`).

## Sessions in one paragraph

`sessions` owns signed-in state: SQLite rows, cookies (`__Host-`
prefixed on https), 30-day TTL; an identity plugin verifies a credential and calls
`SignIn`. `csrf.Protect(origin)` refuses cross-origin writes with no
tokens. Signed-in routes sit in a `Group` with `sess.Require`. **Under
magic links (`auth`), never `sessions.UserID`:** the subject is an
email, so the seam above would scope to `user_id = 0`. Everything else:
`skill/signin.md`.

## Never

- Never import `github.com/glebarez/*` or `gorm.io/driver/sqlite`: the
  binary panics at init or needs cgo.
- Never write `style` attributes or `<style>` blocks (the CSP drops
  them silently), hand-roll a labelled control (use `field-*` partials
  in `<form rst-form>` closed by `form-foot`), an error page, a colour
  toggle or an image grid: they ship (`skill/design.md`).
- Never save bytes to disk or a column: the filesystem is ephemeral
  (`skill/data.md`).
- Never `git merge` to main, even locally: every change lands through
  your forge's review flow, on its own branch.

## Checklist before you call an app done

1. Every handler on an owned model goes through the `owned(r)` seam.
2. Every update names its columns in `.Select(...)`.
3. `csrf.Protect(origin)` mounted app-wide, above the route groups.
4. Signed-in routes, jobs' included, in a `Group` with `sess.Require`.
5. One `migrate.Apply` at boot; `make ci` runs `rastrillo migration check`.
6. `opts.DBPath` blanked before `Serve` when the app opened its own handle.
7. Not-found and not-yours both answer 404.
8. No screen carries two ways to start work; each action has its own page.
