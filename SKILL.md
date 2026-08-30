---
name: rastrillo
description: Build a multi-user CARLOS app: GORM models, chi routes, sessions, owner-scoped queries.
---

# Rastrillo

CARLOS middle layer, not full-stack: you write GORM models, `net/http`
handlers on a chi router and `html/template` pages; it supplies the
database opener, session store, identity plugins, CSRF, owner scoping,
form helpers. Module `github.com/carlosframework/rastrillo`; worked
reference `examples/notes`. Rare traps get one sentence plus a page:
`docs/site/<page>.md`, or `curl -s https://rastrillo.org/docs/<page>.md`.

## 1. App shape

Five files plus `migrations.go` beside `models.go` (§2):
`internal/<app>/models.go` (plain GORM structs), `app.go` (wiring:
migrate.Apply, sessions, identity plugin, router), `handlers.go`
(owner-scoped CRUD), `render.go` (embedded templates,
flash/session-aware page data), `cmd/<app>/main.go` (Resolve ->
db.Open -> App -> Serve).

`rastrillo new --theme=day|plain|signal --shell=column|topbar|sidebar <name>`
(also `--icons`, `--icon-delivery`, `--ux`) scaffolds it all: the theme
lands as `static/theme.css`, the shell as `templates/layout.html`, both
app-owned from then on. docs/site/templates.md

Imports: `github.com/carlosframework/rastrillo` and subpackages `db`,
`migrate`, `scope`, `sessions`, `password`, `csrf`, `flash`, `form`,
`jobs`; `github.com/go-chi/chi/v5`; `gorm.io/gorm`.

`cmd/<app>/main.go`, in order:
`opts, err := rastrillo.Resolve(rastrillo.Options{DBPath: "notes.db",
Logger: logger})`; `d, err := db.Open(opts.DBPath, logger)` (`*db.DB`,
§2); `defer d.Close()`; `mux, err := notes.App(d, origin, logger)`;
`opts.Mux = mux`; `opts.DBPath = ""`; `rastrillo.Serve(opts)`. On any
err: `logger.Error`, `os.Exit(1)`.

**Use `Resolve` + `Serve`, never `Run`, when the app opens its own
database:** `Run` re-parses argv and repopulates `Options.DBPath`, so
`Serve` opens a second connection to the file `db.Open` owns.

Resolve/Serve supply the platform contract — activation argv,
LISTEN_FDS, $STATE_DIRECTORY, /healthz, /api/version, SIGTERM drain,
baseline security headers (your own Set wins). Never hand-roll any of it.

`App(d *db.DB, origin string, logger *slog.Logger) (*http.ServeMux,
error)` in `app.go`, in order:
`migrate.Apply(context.Background(), d, BootSchema)`; writer `*sql.DB`
via `d.G.DB()` (sessions wants it);
`sessions.New(sessions.Config{DB: writer, Origin: origin, Logger:
logger})`; one handler struct holding `d.G`;
`ph, err := password.New(password.Config{...})` (§5); `r :=
chi.NewRouter()`; `r.Use(csrf.Protect(origin))`; `ph`'s handlers at
GET+POST `/signin`, `/signup` and POST `/signout` (§5); owned-model
CRUD (§3/§4) inside
`r.Group(func(r chi.Router) { r.Use(sess.Require); ... })`; return
`http.NewServeMux()` with `mux.Handle("/", r)`. `render.go` parses one
`*template.Template` per page (layout + page), so two pages can both
define `"content"`.

Locales: `Options.Locales`/`DefaultLocale`/`LocaleFS`, flat TOML per
code. Shipped `rastrillo.ui.*`: en ga zh-Hans es hi pt bn ru ja yue vi
ar; any other locale must translate those keys or `generate --check`
fails. Switcher: `rastrillo.LocaleItems(r)` in page data +
`{{template "locale-menu" dict "Items" .Locales "Return" .Path}}`;
POSTs `/_locale` (mounted by Serve). `rastrillo.Dir` gives `<html dir>`.

## 2. Data

Models are plain GORM structs — no base type, no embedding. Owner
column `UserID int64` tagged `gorm:"index"` is what `scope.Owned`
filters on.

`db.Open(path, logger)` returns `*db.DB`: `.G` is the `*gorm.DB`,
SQLite-wired (one file, WAL, one writer connection, several readers,
routed per statement). `d.Close()` closes both pools; `d.G.DB()` returns
the writer `*sql.DB` for database/sql packages like sessions.

Schema changes: edit a model, `rastrillo migration generate`, read the
SQL before committing. Migrations apply once at boot, ledgered — never
re-run, never reversed. `migrations.go` declares two `*migrate.Set`:
`Schema` (the app's own; `generate`/`check` diff it against `Models`)
and `BootSchema = migrate.Merge(sessions.Schema, Schema)` (argument
order = apply order) — everything `App()` applies. Subsystems go in
`BootSchema`, never `Schema`, or `check` proposes dropping their
tables. Never edit a shipped migration; add a new one. Renames are
hand-written (`rastrillo migration new rename_x`); destructive changes
need `--allow-destructive`.

**Old database.** Boot refuses on a structural diff. Run
`migration baseline --db <path> --through <id>` FIRST, then the missing
migration by hand — bare `baseline` silently strands a pending data
migration. docs/site/migrations.md

**The first deploy of a version with migrations must be
schema-neutral.** Generate `0001_init` from the models as already
deployed, ship it alone, change a model only in a later release — else
boot refuses on the new column, and `baseline` there strands it for
good.

## 3. Scoping

Scoping separates users, never tenants: a CARLOS app serves one team; a
multi-team product runs an instance per team (idle instances hibernate
free). Roles/membership are the
`amadan.net/rastrillo/idear` addon (Owner/Admin/Member, invitations, a
membership gate that mints no session) — never core, never
`github.com/carlosframework/idear`. docs/site/addons.md

One seam, every read and write:
`func (a *app) owned(r *http.Request) *gorm.DB { uid, _ :=
sessions.UserID(r); return scope.Owned(a.db, uid) }`.
`scope.Owned(g, owner)` adds `WHERE user_id = ?`; `scope.OwnedBy(g,
column, owner)` takes another owner column, panicking unless plain
`lower_snake` (it is interpolated into SQL). Never
First/Find/Update/Delete an owned model without the filter — in
`d.G.Transaction` too: scope the callback's `tx`, never `d.G` (a `d.G`
statement there hangs on the writer connection the transaction holds).

Scope the write, not just the read that loaded the row — the SQL must
carry `WHERE user_id = ? AND id = ?`: after
`a.owned(r).First(&n, id)` (404 on err),
`a.owned(r).Model(&n).Select("Title", "Body", "UpdatedAt").Updates(...)`.

A row that isn't yours doesn't exist: 404, never 403 — malformed `{id}`
too (`strconv.ParseInt` failure returns `gorm.ErrRecordNotFound`). A
join table is scoped through BOTH sides, checked explicitly; the
stricter reading wins. Creating takes the owner from the session, never
the form: `Note{UserID: uid, ...}`, `uid` from `sessions.UserID(r)`.

## 4. Forms and mass assignment

Never bind a request body onto a GORM model. Read each permitted field
by name (`r.PostFormValue("Title")`), write through
`.Select("Title", "Body").Updates(...)` — `Select`'s strings are GORM
field names, the allowlist keeping unexpected fields out of columns.

Validate with `form.Parse(r, form.Field{Name: "title", Required: true},
form.Field{Name: "body", Kind: form.Textarea})` — one declaration per
field. On `!p.OK()`: `w.WriteHeader(422)`, re-render seeding values
back (`p.String("title")`; `p.Echo()` is the seed-back map for
map-shaped views). Write the status before rendering; the render helper
writes none. Money is `int64` cents: `form.Money`, read `p.Cents`,
parse `form.ParseCents` (no `$`, two decimals max, `""` = zero), seed
`form.FormatCentsPlain`, display `form.FormatCents`.

`form.Date`/`Time`/`DateTime` parse `2006-01-02`, `15:04` and
`2006-01-02T15:04` exactly — nothing looser — in `Field.Location` (nil
= UTC; `Time` ignores it); read `p.Date`/`p.Time`/`p.DateTime`;
`form.Range(p, "starts", "ends")` checks end-before-start. Empty
optional dates, unparseable ones and undeclared names all read back as
the zero time: ask `p.OK()`, never `IsZero`. Their error values are
`rastrillo.ui.*` **keys**, not sentences: render
`"Error" (T (index .Errors "Starts"))`, safe on every field — `T`
returns an unrecognised string verbatim, so the older kinds' plain
English passes through. Partials: `field-date`, `field-time`,
`field-datetime`, `field-daterange` (its two halves need distinct
`Name`s); `datetime.js` enhances them into a combobox reading
"next fri 9am" in the page's own language — vocabulary from the catalog
on one JSON attribute, month names from `Intl`, no English vocabulary
in the file.

After a mutation: `flash.Set(w, "notice", "...")`, then 303; the render
helper calls `flash.Take(w, r)` once per page and the layout renders it.

## 5. Sessions and identity

`sessions` owns signed-in state: SQLite rows (sign-out/revocation are
real), `__Host-` cookies on https, 30-day TTL. An identity plugin
verifies a credential and calls `SignIn` — the whole contract.

- `csrf.Protect(origin)` mounts app-wide (`r.Use`): refuses cross-origin
  POST/PUT/PATCH/DELETE via `Sec-Fetch-Site`/`Origin`, no tokens.
- Signed-in routes: chi `Group` with `s.Require` — signed-out GET/HEAD
  redirects to `SigninPath` with a same-site `return_to`; the rest 403.
  `s.Middleware` only resolves, blocks nothing.
- `s.RequireFresh(maxAge)` step-up: past maxAge, GET/HEAD goes to
  `SigninPath?reauth=1`; re-signing-in or a `passkey` assertion rotates
  fresh (2FA: `Config.SecondFactor` = passkey's `Gate`). Lost passkey: a
  recovery code redeems at POST `/passkey/signin/recovery`, minted by
  `RegenerateRecoveryCodes` behind `RequireFresh`. docs/site/passkeys.md
- Viewer: `sessions.UserID(r)` (int64, ok) or `sessions.Current(r)`
  (Subject, Method, AuthTime, At). Past `Require`, `ok` holds only when
  the plugin's Subject is a numeric user id — see the `auth` warning.
  docs/site/magic-links.md
- Sign-in redirects go through `sessions.SafeReturn(r, "/")`, never a
  raw `return_to`. `s.Sweep(time.Now())` deletes expired rows.

**Password plugin.** `password.New` needs `Sessions`, `Lookup`,
`RenderSignin`; nil `Create` disables signup (its pages 404);
`RenderSignup` is required whenever `Create` is set. `Lookup(ctx, email)
(id, hash, error)`: unknown email = `sql.ErrNoRows`, treated as a wrong
password over a decoy hash. Any `Create` error reads as a duplicate
email unless it wraps `password.ErrRefused` (via
`password.Refuse(msg)`), rendered at 403. `Signin`/`Signup`/`Signout`
POST-only; Page variants GET; 405 otherwise. Render callbacks take
`(w, r, password.PageData)` = `{Error, Email, ReturnTo}`; the plugin
writes the status (422/403/429), the callback must not.

**Rate limiting.** Signin and Signup share a per-email budget: 10
failures in 15 minutes answers 429 until one ages out; success resets;
in-memory (IP throttling belongs to deployment). `Refuse` refusals
meter on a second budget Signin never reads.

**Magic links** (`rastrillo/auth`: sign-in by emailed link, upgrading
to the keymail ceremony where the address has one): `auth.New` with
`Begin`/`Callback`/`Verify`/`Signout` and `RequireSession`, same
`sessions` core, same rate-limit shape. **Under `auth`, never
`sessions.UserID`:** the Subject is the verified email, so it returns
`(0, false)` and the §3 seam would scope every query to `user_id = 0`.
Use `auth.From(r)` or `sessions.Current(r)` and map the address to your
user row's id before scoping.

## 6. Background work

`jobs` runs observable in-memory goroutines: a restart kills them, fn's
ctx expires at 15 min (job turns Failed) — keep jobs idempotent, honor
ctx. `j := jobs.New(logger)` once; `j.Start(owner, name, location, fn)`
runs `fn(ctx, progress func(string)) error`, returns `(Job, error)`;
`ErrOwnerBusy` past 4 Running per owner (flash your own copy). 303 the
caller to `/jobs/`+job.ID. Owner is the session **Subject**, not
`sessions.UserID` — key job rows the same way. fn's error text reaches
the owner. `j.Get(id, owner)` 404s a foreign or unknown id.

`jobs.NewHandlers(jobs.Config{Jobs, Render, RenderFragment})` returns
`StatusPage`/`Fragment`/`Events` (SSE), erroring unless all three are
set. Mount in the `sess.Require` group at `/jobs/{id}`, `.../fragment`,
`.../events`; each takes `jobs.PageData{Job, FragmentPath, EventsPath,
PollSeconds}`. Render draws the whole page, RenderFragment the partial
alone (else the layout nests on the next poll). Done+Location 303s from
StatusPage; Fragment answers 204 + `Rastrillo-Location`. Must work with
scripts off: a `<noscript>` meta refresh of `PollSeconds`, only while
Running, or a failed page refreshes forever.

The only JavaScript is app-owned `static/rastrillo.js`, inert until
markup opts in. `data-poll="URL"` + `data-poll-every="2"` swap the
element for the fetched fragment, repeating while the fragment still
carries `data-poll` (ui's `job-status` partial drops it once done); the
partial's `PushURL` (= `EventsPath`) emits `data-poll-push` and the
shim rides SSE, falling back to polling. Every submit button gets a
spinner, `aria-busy` and a double-submit guard by DEFAULT;
`data-busy="false"` (form or button) opts out, `data-busy-label`
retitles. Manners, not idempotency — the server still refuses the
second write.

Hibernation means a `time.Ticker` is not a scheduler. Declare recurring
work outside the app (`carlos schedule set -name sync -every 6h -path
/jobs/sync`); the platform wakes the instance and POSTs there. Guard
with `carlos.Tick(r)` (bearer == `$CARLOS_ADMIN_TOKEN`, constant-time,
false with no token) and work inside the request — 202-plus-goroutine
hibernates mid-job; 2xx done, 5xx retry, 4xx don't. At-least-once
delivery: dedupe on `carlos.TickOccurrence(r)` (stable across retries),
never the clock. One-offs: `carlos.ScheduleAt(ctx,
name, at, path)` (upsert by name; `ErrNotOnCarlos` off-platform,
`ErrDeclaredSchedule`, `ErrTooManyTimers`) and `carlos.ScheduleCancel`.

## 7. What NOT to do

- **Manifests: declare what fits the vocabulary, hand-write the rest.**
  A `manifest/*.toml` resource generates CRUD screens — field kinds
  text, textarea, money; no relations; `scope = "user"` owner-filters by
  session subject. docs/site/manifests.md
- **Never import `github.com/glebarez/*` or `gorm.io/driver/sqlite`:**
  glebarez re-registers modernc's `sqlite` driver name, so the binary
  panics at init; the gorm.io one is cgo. `db.Open` already wires
  `gormlite` over modernc.
- **Your app's components stay in your app.** `ui` is rastrillo's
  vocabulary, not yours. Keep a component app-local; the day one app
  copies another's, extract it to a shared module that week and delete
  the copy. A copy is the trigger — "a second consumer appears" never
  fires.
- **UI: `rst-list`/`rst-card` hold rows only (unpadded by design).**
  Forms, prose, links go in `rst-box` with a sibling `rst-box-head`.
  Screens stack vertically — never heading, paragraph and button in one
  flex row; a notice with a CTA is a `callout` ending in a link. State
  is never colour alone: a `status-pill`'s label and a `meter`'s
  fraction are always visible text; `Alert` (`role="alert"`) is for
  live problems, not ambient notes. Every menu is a
  `<details name="rst-menus">` (opening one closes the rest);
  `rastrillo.js` closes any on outside click or Escape; `MenuGroup`
  names another group, and a nested `rst-menu-group` MUST name a
  different one or it closes its parent. Full vocabulary:
  rastrillo.org/design-system (built from `ui` by `cmd/dsgen`, not
  committed); `go generate ./...` renders a local copy into
  `.design-system/`. docs/site/templates.md
- **Never hand-roll an error page.** `view.Fail`/`NotFound`/`Forbidden`
  render styled pages inside the shell; a 500 shows a ref matching the
  log line's `ref`. Wire `opts.ErrorPage` (and `Ctx.ErrorPage`) to a
  `rastrillo.ErrorPageFunc` — `rastrillo new` scaffolds it as
  `render.go`'s `ErrorPage` over `templates/errors.html`; panics recover
  to that same page. Unwired, errors are bare text.
  docs/site/templates.md
- **Never `git merge` to main**, even locally: every change is a PR,
  squash-merged.

## Checklist before you call an app done

1. Every handler on an owned model goes through the `owned(r)` seam.
2. Every update names its columns in `.Select(...)`.
3. `csrf.Protect(origin)` mounted app-wide, above the route groups.
4. Signed-in routes, jobs' included, in a `Group` with `sess.Require`.
5. One `migrate.Apply` at boot; `make ci` runs `rastrillo migration check`.
6. `opts.DBPath` blanked before `Serve` when the app opened its own handle.
7. Not-found and not-yours both answer 404.
