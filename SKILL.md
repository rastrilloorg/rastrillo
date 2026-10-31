---
name: rastrillo
description: "Build a multi-user CARLOS app: GORM models or plain SQL, chi routes, sessions, owner-scoped queries."
---

# Rastrillo

CARLOS middle layer, not full-stack: you write GORM models (or plain
SQL), actions on a route table (`action`, over chi) and `html/template`
pages; it supplies the database opener, session store, identity
plugins, CSRF, owner scoping, form helpers. Module `amadan.net/rastrillo/rastrillo`; worked
reference `examples/notes` — in the repository, **not** in the
published module (Go excludes nested modules from a zip), so read it
there, never in your checkout. The topics below cover what it shows,
sign-in and tests included: read the example only for what they do
not. Rare traps get one sentence plus a page:
`docs/site/<page>.md`, or `curl -s https://rastrillo.org/docs/<page>.md`.

## Load a topic before its work

This file is what every task needs. The rest is in `skill/`, beside
this file (`$(go list -m -f '{{.Dir}}' amadan.net/rastrillo/rastrillo)/skill/`).
Read the topic before you start work it covers, not after the source:

| Before you... | read |
|---|---|
| write or change any Go code: names, function and file size, comments, an action's shape | `skill/code.md` |
| add password sign-in to an app that has none | `skill/add-signin.md` |
| use the session, magic links, a sign-in screen, 2FA | `skill/signin.md` |
| add a page or route, a flow, a test, or meet a perf/budget failure | `skill/screens.md` |
| call a `ui` partial: its keys, at a glance | `skill/partials.md` |
| style a page, format numbers, add a locale, a menu, a picture grid | `skill/design.md` |
| take money, dates, URLs or uploads in a form, or protect a public form | `skill/forms.md` |
| choose GORM or SQL, store files, write a migration, add teams or roles, serve a tenant under `/<slug>` | `skill/data.md` |
| run work in the background, poll for progress, schedule | `skill/background.md` |
| update parts of a page when data changes: live regions, forms that swap in place | `skill/live.md` |
| use the CLI beyond `new`, manifests, doctor, pins, native or PWA | `skill/tooling.md` |
| serve JSON at `/api/v1`, mint tokens, let an agent act for a person, write `Authorize`, hand-write an operation | `skill/api.md` |

## Start here

```sh
go install amadan.net/rastrillo/rastrillo/cmd/rastrillo@latest
rastrillo new notes && cd notes && go mod tidy && go test ./...
```

**People sign in? Start with `rastrillo new --auth=magic <name>`.** The
app then signs people in by emailed link from its first commit, scopes
owned rows (below) and its tests sign in for you; its
`AGENTS.md` says where each piece is. Without `--auth` the scaffold has
no accounts.

`rastrillo new` writes the whole app shape, its gate (`make ci`) and
its tests; it compiles, passes and serves before you write a line, so
never hand-write any of it. The scaffolded `AGENTS.md` is the source of
truth for that app's code; this file stays the framework's.

**Reach for a manifest before hand-writing.** A `manifest/*.toml`
resource generates CRUD screens (field kinds text, textarea, money;
`scope = "user"` owner-filters by session subject), correct by
construction; run `rastrillo generate`, commit `gen/` and `api/v1.json`,
and mount `gen.Router` in `Routes` (`manifest/README.md`).
docs/site/manifests.md

**Look before you build.** Most things an app needs already ship: a
Light/Dark/System switch in every shell, a picture grid (`media-grid`),
the API token page (`api-tokens`), job status pages, money fields,
error pages. Check the index first.

## App shape

`main.go`, in order: `rastrillo.Resolve(rastrillo.Options{DBPath:
"notes.db", ...})`; `db.Open(options.DBPath, logger)` (deferred
`Close`); `notes.New(database, origin, logger)`;
`notes.Configure(&options, mux, started)`; `options.DBPath = ""`;
`rastrillo.Serve(options)`. **Use `Resolve` + `Serve`, never `Run`,
when the app opens its own database:** `Run` re-parses argv and
repopulates `Options.DBPath`, so `Serve` opens a second connection to
the file `db.Open` owns. Resolve/Serve supply the platform contract
(activation, /healthz, SIGTERM drain, security headers); never
hand-roll it.

`app.go`'s `New` wraps `Router`, which the perf test walks:
`migrate.Apply(ctx, database, BootSchema)`, then `App`, then
`Routes(action.NewRoutes(router, app.contextFor), app)`. `routes.go`
holds named paths and `func Routes(routes *action.Routes, app *App)`,
which registers everything: `/static` and
`routes.Mount("/api/v1", app.api, "api")` unguarded (a bearer token is
the API's only credential), then every page in a `routes.Group`
starting `Guard("csrf", csrf.Protect(app.origin))` and
`Guard("pages", pages.Middleware)`. Owned pages go in a `Group` within,
behind the session guard (`app.auth.RequireSession` under
`--auth=magic`). An action, `func (app *App) home(visit
*action.Visit)`, answers with `visit.Render("index", http.StatusOK,
data)`; its page also goes in `render.go`'s list.

## Owner scoping

One seam, every read and write (`app.database.G` is a
`*gorm.DB`; in an action, `request` is `visit.Request()`):
`func (app *App) owned(request *http.Request) *gorm.DB { userID, _ :=
sessions.UserID(request); return scope.Owned(app.database.G, userID) }`.
An `--auth=magic` app has it already (`auth.go`), over an `Owner`
address column: create rows with `Owner: ownerOf(request)`, never
`UserID`. `scope.Owned(g, owner)` adds `WHERE user_id = ?`; `scope.OwnedBy(g,
column, owner)` takes another owner column, panicking unless plain
`lower_snake` (it is interpolated into SQL). Never
First/Find/Update/Delete an owned model without the filter — in a
transaction too: scope the callback's `tx`, never `app.database.G` (a
statement on it there hangs on the writer connection `tx` holds).

Scope the write, not just the read that loaded the row: after
`app.owned(request).First(&note, id)` (map `gorm.ErrRecordNotFound`
to `op.NotFound{}`), `app.owned(request).Model(&note).Select("Title",
"Body", "UpdatedAt").Updates(...)`. A row that isn't yours doesn't
exist: 404, never 403 — malformed `{id}` too. A join table is scoped
through BOTH sides. Creating takes the owner from the session, never
the form: `Note{UserID: userID, ...}`.

Never bind a request body onto a model: write each permitted field
through `.Select(...)` (`skill/forms.md`).

## Sessions in one paragraph

`sessions` owns signed-in state; an identity plugin verifies a
credential and calls `SignIn`. `csrf.Protect(origin)` refuses
cross-origin writes with no tokens. **Under magic links (`auth`),
never `sessions.UserID`:** the subject is an email, so the seam above
would scope to `user_id = 0`.
Everything else: `skill/signin.md`.

## Write code that reads like a sentence

`rastrillo budget code` holds Go to four limits. An app's `make ci`
runs it as `make code`, and `make routes` fails an action no route
mounts:

- lines at most 80 columns, a tab reaching the next stop of 4;
- non-test files at most 400 lines;
- non-test functions at most 40 lines; an action,
  `func(*action.Visit)`, 15;
- no one-letter names but a capital type parameter and the testing
  types' `t`, `b`, `f`.

1. Spell names out, except those, `err` and `ctx` (`context` is the
   package): `request`, not `r`.
2. Name things for what the person is doing (`entries.Publish`), not
   how the code does it (`saveEntry`).
3. One route, one intent: two buttons that mean two things post to two
   routes, never to one handler with an `isNew` switch.
4. An action has four beats: read the input, run one operation, answer
   a refusal, say what success looks like.
5. Operations hold the rules and the database work, return `op`
   refusals and never touch HTTP.
6. A helper answers refusals; success stays written out in the action.
7. Comments say why, in one or two full sentences, never what the code
   does.

The other seven, an action's shape and what each gate message means:
`skill/code.md`.

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

1. Every action on an owned model goes through the `owned` seam.
2. Every update names its columns in `.Select(...)`.
3. Pages and forms only inside `Routes`' `csrf.Protect` group.
4. Signed-in routes, jobs' included, behind the session guard.
5. One `migrate.Apply` at boot; `make ci` runs `rastrillo migration check`.
6. `options.DBPath` blanked before `Serve` when the app opened its own handle.
7. Not-found and not-yours both answer 404.
8. No screen carries two ways to start work; each action has its own page.
