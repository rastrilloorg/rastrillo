---
name: rastrillo
description: Build a multi-user CARLOS app: GORM models, chi routes, sessions, owner-scoped queries.
---

# Rastrillo

The CARLOS web framework: a middle layer, not full-stack. You write GORM
models, `net/http` handlers on a chi router, and `html/template` pages; it
supplies what is hard to get right twice — database opener, session store,
identity plugins, CSRF, owner scoping, form helpers. Module
`github.com/carlosframework/rastrillo`; worked reference `examples/notes`.

This file alone covers the common path. Where a rare trap gets one
sentence, a doc line names its page: `docs/site/<page>.md` in this repo,
or `curl -s https://rastrillo.org/docs/<page>.md`.

## 1. App shape

Five files plus `migrations.go` beside `models.go` (§2):

```
internal/<app>/models.go     plain GORM structs
internal/<app>/app.go        migrate.Apply, sessions, identity plugin, router
internal/<app>/handlers.go   owner-scoped CRUD
internal/<app>/render.go     embedded templates, flash/session-aware page data
cmd/<app>/main.go            Resolve -> db.Open -> App -> Serve
```

`rastrillo new --theme=day|plain|signal --shell=column|topbar|sidebar <name>`
scaffolds all of it (also `--icons`, `--icon-delivery`, `--ux`): the theme
lands as `static/theme.css`, the shell as `templates/layout.html`, both
app-owned from that moment. docs/site/templates.md

Imports: `github.com/carlosframework/rastrillo` and subpackages (`db`,
`migrate`, `scope`, `sessions`, `password`, `csrf`, `flash`, `form`,
`jobs`), `github.com/go-chi/chi/v5`, `gorm.io/gorm`.

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

**Use `Resolve` + `Serve`, never `Run`, when the app opens its own
database.** `Run` re-parses argv and repopulates `Options.DBPath`, so
`Serve` opens a second connection to the file `db.Open` owns.

The platform contract — activation argv, LISTEN_FDS, $STATE_DIRECTORY,
/healthz, /api/version, SIGTERM drain, baseline security headers (your own
Set wins) — comes from Resolve/Serve. Never hand-roll any of it.

`app.go`, the whole wiring in order:

```go
func App(d *db.DB, origin string, logger *slog.Logger) (*http.ServeMux, error) {
	if _, err := migrate.Apply(context.Background(), d, BootSchema); err != nil { return nil, err }
	writer, err := d.G.DB() // sessions wants the writer *sql.DB
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
		r.Get("/", a.listNotes) // owned-model CRUD, §3/§4
	})
	mux := http.NewServeMux()
	mux.Handle("/", r)
	return mux, nil
}
```

Handlers hang off one struct holding `*gorm.DB`. `render.go` parses one
`*template.Template` per page (layout + page), so two pages can both
define `"content"`.

Locales: `Options.Locales`/`DefaultLocale`/`LocaleFS`, flat TOML per code.
The framework ships `rastrillo.ui.*` in en ga zh-Hans es hi pt bn ru ja
yue vi ar; any other locale must translate those keys or
`generate --check` fails. Switcher: put `rastrillo.LocaleItems(r)` in page
data, render `{{template "locale-menu" dict "Items" .Locales "Return"
.Path}}`; it POSTs `/_locale`, mounted by Serve. `rastrillo.Dir` gives
`<html dir>`.

## 2. Data

Models are plain GORM structs — no base type, no embedding:

```go
type Note struct {
	ID        int64
	UserID    int64 `gorm:"index"` // the owner column scope.Owned filters on
	Title     string
	Body      string
	UpdatedAt time.Time
}
```

`db.Open(path, logger)` returns `*db.DB`: `.G` is the `*gorm.DB`, wired
for SQLite (one file, WAL, one-connection writer pool, several readers,
routed per statement). `d.Close()` closes both pools; `d.G.DB()` returns
the writer `*sql.DB` for database/sql packages like sessions.

Schema changes: edit a model, run `rastrillo migration generate`, read the
SQL before committing. Migrations apply once at boot, ledgered — never
re-run, never reversed. `migrations.go` declares two `*migrate.Set`:
`Schema` (the app's own, what `generate`/`check` diff against `Models`)
and `BootSchema` = `migrate.Merge(sessions.Schema, Schema)` (argument
order is apply order) — everything `App()` applies. Add a subsystem to
`BootSchema`, never `Schema`, or `check` proposes dropping its table.
Never edit a shipped migration; add a new one. Renames are hand-written
(`rastrillo migration new rename_x`); destructive changes need
`--allow-destructive`.

**Recovering an old database.** Boot refuses on a structural diff.
Recovery is `migration baseline --db <path> --through <id>` FIRST, then
the missing migration by hand — bare `baseline` silently strands a
pending data migration. docs/site/migrations.md

**The first deploy of a version with migrations must be schema-neutral.**
Generate `0001_init` from the models as already deployed, ship it alone,
change a model only in a later release — otherwise boot refuses on the
new column, and `baseline` there strands it for good.

## 3. Scoping

Scoping separates users within one instance, never tenants: a CARLOS app
serves one team, and a multi-team product runs an instance per team (idle
instances hibernate free). Roles and membership are the
`amadan.net/rastrillo/idear` addon (Owner/Admin/Member, invitations, a
membership gate that mints no session), never core and never
`github.com/carlosframework/idear`. docs/site/addons.md

One seam, every read and write:

```go
func (a *app) owned(r *http.Request) *gorm.DB {
	uid, _ := sessions.UserID(r)
	return scope.Owned(a.db, uid)
}
```

`scope.Owned(g, owner)` adds `WHERE user_id = ?`; `scope.OwnedBy(g,
column, owner)` takes another owner column and panics unless it is plain
`lower_snake` (it is interpolated into SQL). Never
First/Find/Update/Delete an owned model without the filter — including
inside `d.G.Transaction`, where you scope the callback's `tx`, never
`d.G` (a `d.G` statement there hangs on the writer connection the
transaction already holds).

Scope the write, not just the read that loaded the row, so the SQL itself
carries `WHERE user_id = ? AND id = ?`:

```go
n, err := a.find(r) // a.owned(r).First(&n, id); 404 if err != nil
a.owned(r).Model(&n).Select("Title", "Body", "UpdatedAt").
	Updates(map[string]any{"Title": title, "Body": body})
```

A row that isn't yours doesn't exist: 404, never 403 — malformed `{id}`
too (`strconv.ParseInt` failure returns `gorm.ErrRecordNotFound`). A join
table is scoped through BOTH sides, checked explicitly; the stricter
reading wins. Creating takes the owner from the session, never the form:
`Note{UserID: uid, ...}` with `uid` from `sessions.UserID(r)`.

## 4. Forms and mass assignment

Never bind a request body onto a GORM model. Read each permitted field by
name (`r.PostFormValue("Title")`) and write through
`.Select("Title", "Body").Updates(...)` — `Select`'s strings are GORM
field names, the allowlist that keeps unexpected fields out of columns.

Validate with `form.Parse`, one declaration per field; re-render at 422
with values seeded back:

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

`p.Echo()` is the seed-back map for map-shaped views. Write the status
before rendering; the helper writes none. Money is `int64` cents:
`form.Money`, read `p.Cents`, parse `form.ParseCents` (no `$`, two
decimals max, `""` = zero), seed `form.FormatCentsPlain`, display
`form.FormatCents`.

`form.Date`/`Time`/`DateTime` parse `2006-01-02`, `15:04` and
`2006-01-02T15:04` exactly — nothing looser — in `Field.Location` (nil =
UTC; `Time` ignores it); read `p.Date`/`p.Time`/`p.DateTime`, and
`form.Range(p, "starts", "ends")` for the end-before-start check. An
empty optional date, an unparseable one and an undeclared name all read
back as the zero time, so ask `p.OK()`, never `IsZero`. Their error
values are `rastrillo.ui.*` **keys**, not sentences: render
`"Error" (T (index .Errors "Starts"))`, which is safe on every field
because `T` returns an unrecognised string verbatim, so the older kinds'
plain English passes through. The partials are `field-date`,
`field-time`, `field-datetime` and `field-daterange` (its two halves
need distinct `Name`s); `datetime.js` enhances them into a combobox that
reads "next fri 9am" in the page's own language — vocabulary from the
catalog on one JSON attribute, month names from `Intl`, no English
vocabulary in the file.

After a mutation: `flash.Set(w, "notice", "...")`,
then 303; the render helper calls `flash.Take(w, r)` once per page and
the layout renders it.

## 5. Sessions and identity

`sessions` owns signed-in state: SQLite rows (sign-out and revocation are
real), `__Host-` cookies on https, 30-day TTL. An identity plugin
verifies a credential and calls `SignIn`; that call is the whole contract.

- `csrf.Protect(origin)` mounts app-wide (`r.Use`): refuses cross-origin
  POST/PUT/PATCH/DELETE via `Sec-Fetch-Site`/`Origin`, no tokens.
- Signed-in routes live in a chi `Group` with `s.Require`: signed-out
  GET/HEAD redirects to `SigninPath` with a same-site `return_to`, the
  rest 403. `s.Middleware` only resolves, blocks nothing.
- `s.RequireFresh(maxAge)` adds step-up: past maxAge, GET/HEAD goes to
  `SigninPath?reauth=1`; re-signing-in or a `passkey` assertion rotates
  fresh (2FA: `Config.SecondFactor` = passkey's `Gate`). Lost passkey: a
  recovery code redeems at POST `/passkey/signin/recovery`, minted by
  `RegenerateRecoveryCodes` behind `RequireFresh`. docs/site/passkeys.md
- Read the viewer with `sessions.UserID(r)` (int64, ok) or
  `sessions.Current(r)` (Subject, Method, AuthTime, At). Past `Require`,
  `ok` holds only when the plugin's Subject is a numeric user id — see
  the `auth` warning. docs/site/magic-links.md
- Sign-in redirects go through `sessions.SafeReturn(r, "/")`, never a raw
  `return_to`. `s.Sweep(time.Now())` deletes expired rows.

**Password plugin.** `password.New` needs `Sessions`, `Lookup`,
`RenderSignin`; nil `Create` disables signup (its pages 404), and
`RenderSignup` is required whenever `Create` is set. `Lookup(ctx, email)
(id, hash, error)` returns `sql.ErrNoRows` for an unknown email, treated
as a wrong password over a decoy hash. Any `Create` error reads as a
duplicate email unless it wraps `password.ErrRefused` (via
`password.Refuse(msg)`), rendered at 403. `Signin`/`Signup`/`Signout` are
POST-only; Page variants take GET; 405 otherwise. Render callbacks take
`(w, r, password.PageData)` = `{Error, Email, ReturnTo}`; the plugin
writes the status (422/403/429), the callback must not.

**Rate limiting.** Signin and Signup share a per-email budget: 10
failures in 15 minutes answers 429 until one ages out; success resets;
in-memory (IP throttling belongs to deployment). `Refuse` refusals meter
on a second budget Signin never reads.

**Magic links** (`rastrillo/auth`: sign-in by emailed link, upgrading to
the keymail ceremony where the address has one): `auth.New` with
`Begin`/`Callback`/`Verify`/`Signout` and `RequireSession`, same
`sessions` core, same rate-limit shape. **Under `auth`, never
`sessions.UserID`:** the Subject is the verified email, so it returns
`(0, false)` and the §3 seam would scope every query to `user_id = 0`.
Use `auth.From(r)` or `sessions.Current(r)` and map the address to your
user row's id before scoping.

## 6. Background work

`jobs` runs observable goroutines in-memory: a restart kills them, fn's
ctx expires at 15 min (job turns Failed) — keep jobs idempotent, honor
ctx. `j := jobs.New(logger)` once; `j.Start(owner, name, location, fn)`
runs `fn(ctx, progress func(string)) error` and returns `(Job, error)` —
`ErrOwnerBusy` past 4 Running per owner (flash your own copy). 303 the
caller to `/jobs/`+job.ID. Owner is the session **Subject**, not
`sessions.UserID` — key job rows the same way. fn's error text reaches
the owner. `j.Get(id, owner)` 404s a foreign or unknown id.

`jobs.NewHandlers(jobs.Config{Jobs, Render, RenderFragment})` returns
`StatusPage`/`Fragment`/`Events` (SSE), erroring unless all three are
set. Mount them in the `sess.Require` group at `/jobs/{id}`,
`.../fragment`, `.../events`; each takes `jobs.PageData{Job,
FragmentPath, EventsPath, PollSeconds}`. Render draws the whole page,
RenderFragment the partial alone (the layout nests on the next poll
otherwise). Done+Location 303s from StatusPage; Fragment answers 204 +
`Rastrillo-Location`. Must work with scripts off: a `<noscript>` meta
refresh of `PollSeconds`, only while Running, or a failed page refreshes
forever.

The only JavaScript is app-owned `static/rastrillo.js`, inert until
markup opts in. `data-poll="URL"` + `data-poll-every="2"` swap the
element for the fetched fragment and repeat while the fragment still
carries `data-poll` (ui's `job-status` partial drops it once done); the
partial's `PushURL` (= `EventsPath`) emits `data-poll-push` and the shim
rides SSE, falling back to polling. Every submit button gets a spinner,
`aria-busy` and a double-submit guard by DEFAULT; `data-busy="false"`
(form or button) opts out, `data-busy-label` retitles. It is manners,
not idempotency — the server still has to refuse the second write.

Hibernation means a `time.Ticker` is not a scheduler. Declare recurring
work outside the app (`carlos schedule set -name sync -every 6h -path
/jobs/sync`); the platform wakes the instance and POSTs there. Guard the
handler with `carlos.Tick(r)` (bearer == `$CARLOS_ADMIN_TOKEN`,
constant-time, false with no token) and work inside the request —
202-plus-goroutine hibernates mid-job; 2xx done, 5xx retry, 4xx don't.
Delivery is at-least-once: dedupe on `carlos.TickOccurrence(r)`, stable
across retries, never on the clock. One-offs: `carlos.ScheduleAt(ctx,
name, at, path)` (upsert by name; `ErrNotOnCarlos` off-platform,
`ErrDeclaredSchedule`, `ErrTooManyTimers`) and `carlos.ScheduleCancel`.

## 7. What NOT to do

- **Manifests: declare what fits the vocabulary, hand-write the rest.** A
  `manifest/*.toml` resource generates CRUD screens — field kinds text,
  textarea, money; no relations; `scope = "user"` owner-filters by
  session subject. docs/site/manifests.md
- **Never import `github.com/glebarez/*` or `gorm.io/driver/sqlite`.**
  glebarez registers the `sqlite` driver name modernc already does, so
  the binary panics at init; the gorm.io one is cgo. `db.Open` already
  wires `gormlite` over modernc.
- **Never bind a form onto a model, query an owned model unscoped, or
  answer 403 where 404 is honest.** §3, §4.
- **UI: `rst-list`/`rst-card` hold rows only (unpadded by design).** A
  form, prose or links go in `rst-box` with a sibling `rst-box-head`.
  Screens stack vertically — never heading, paragraph and button in one
  flex row; a notice with a CTA is a `callout` ending in a link. Every
  menu is a `<details name="rst-menus">`, so opening one closes the rest
  and `rastrillo.js` closes any on an outside click or Escape; pass
  `MenuGroup` for another group, and a nested `rst-menu-group` MUST name
  a different one or it closes its parent. The full vocabulary is
  browsable at rastrillo.org/design-system; regenerate
  `docs/design-system` with `go generate ./...` after changing `ui`.
  docs/site/templates.md
- **Never hand-roll an error page.** `view.Fail`/`NotFound`/`Forbidden`
  render styled pages inside the shell; a 500 shows a ref matching the
  `ref` on the log line. Wire `opts.ErrorPage` (and `Ctx.ErrorPage`) to a
  `rastrillo.ErrorPageFunc` — `rastrillo new` scaffolds it as `render.go`'s
  `ErrorPage` over `templates/errors.html`, and panics recover to that
  same page. Unwired, errors are bare text. docs/site/templates.md
- **Never `git merge` to main**, even locally: every change is a PR,
  squash-merged.

## Checklist before you call an app done

1. Every handler on an owned model goes through the `owned(r)` seam.
2. Every update names its columns in `.Select(...)`.
3. `csrf.Protect(origin)` is mounted app-wide, above the route groups.
4. Signed-in routes, jobs' included, sit in a `Group` with `sess.Require`.
5. One `migrate.Apply` runs at boot; `make ci` runs `rastrillo migration check`.
6. `opts.DBPath` is blanked before `Serve` when the app opened its own handle.
7. Not-found and not-yours both answer 404.
