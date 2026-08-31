# 🤖 rastrillo

`amadan.net/rastrillo/rastrillo`

The root package: the process entrypoints, the platform contract,
localization, fingerprinted assets, the agent vocabulary, and the
manifest types.

[The shape of an app](/docs/app-shape) is the guide to the entrypoints,
and [Deploying](/docs/deploying) to what they implement.

## Entrypoints

```go
func Run(opts Options) error
func Resolve(opts Options) (Options, error)
func Serve(opts Options) error
func Handler(opts Options) (http.Handler, func() error, error)
```

`Run` resolves the platform's activation argv and calls `Serve`. Use it
when you let the framework own the database.

When your app opens its own database, use `Resolve` and `Serve` instead.
`Run` re-parses argv and repopulates `Options.DBPath`, so `Serve` would
open a second connection to the file `db.Open` already owns. `Resolve`
does the same argv and `$STATE_DIRECTORY` work without serving.

`Handler` is `Serve` minus the listener: the whole framework chrome as
an `http.Handler`, for test harnesses. Call the returned cleanup when
you are done.

`OpenDB` is the corrected SQLite opener, exported for tests. In a new
app use [`db.Open`](/docs/reference/db), which returns the two-pool
handle.

`BuildVersion` is the version string `GET /api/version` reports,
overridden at build time; it defaults to `"dev"`.

## Options

The one configuration struct. The fields worth knowing:

**`Mux`** or **`Router`** — exactly one must be set. `Router` builds the
mux *after* the database opens, and is handed the `*sql.DB` `Serve`
opened, which is how an app puts the framework-opened handle in its
per-request `Ctx`. `Serve` owns that handle and closes it when `Serve`
returns; do not retain it past that.

**`Wrap`** — where your middleware goes: sessions, CSRF, panic pages,
authorization. It runs inside the framework's chrome, so `GET /healthz`
and `GET /api/version` are answered outside it (platform probes never
traverse your middleware) and locale-prefix stripping happens before it
(your middleware sees the paths your routes match on). Returning nil is
a boot error.

**`DBPath`** — opens SQLite with the pragma ordering and connection
settings that have to be right. Blank it before `Serve` when your app
opened its own handle.

**`Migrations`** — applied in order at boot, idempotently. This is the
older additive-only mechanism. In a new app use
[`migrate`](/docs/reference/migrate) instead.

**`Socket`** and **`Addr`** — the platform's activation contract. Both
empty, `Serve` checks for a systemd-activated listener (`LISTEN_FDS`)
before falling back to `:8080`.

**`Locales`**, **`DefaultLocale`**, **`LocaleFS`** — the locale set and
its catalogs.

**`CSP`** — swaps the baseline content-security policy. The framework
sets baseline security headers outermost, and your own `Set` or `Del`
wins.

**`NextDue`** — answers the activator's `GET /api/next-due` scheduled-wake
poll. Unset, the route does not exist.

**`Sidecar`** — the app's sidecar pass, run in a loop when the platform
spawns `<binary> sidecar run`. See [Agents and tools](/docs/agents).

**`ErrorPage`** — your own error page, for a failure your app never saw.
The framework recovers a panicking handler outermost of all — outside
the security headers, so outside your middleware too — logs the stack
with a reference, and calls this to render the body at 500. Unset, the
response is a plain "Something went wrong.". `http.ErrAbortHandler`
goes straight back up, and a panic *after* the first byte still leaves a
broken page: the status is long gone by then, exactly as in `net/http`.

```go
type ErrorPageFunc func(w http.ResponseWriter, r *http.Request, status int, ref string)
func NewRef() string
```

Wire the same function to `Ctx.ErrorPage` and the 500 a handler answers
looks identical to the 500 a panic answers. `ui`'s `error-page` partial
is the body; the callback owns the status code as well, so it calls
`WriteHeader(status)` itself.

`ref` is what `NewRef` mints: six lowercase base32 characters over four
random bytes, shown on the page and logged beside the error. It is not
an id and nothing is stored under it — its whole job is to join what
the user saw to what you grep for. The alphabet has no `0`, `1`, `8` or
`9`, so a reference read down a phone line cannot be heard as an `O`, an
`l` or a `B`. `view.Fail` mints one too; `NewRef` is exported for a
hand-written handler doing the same job.

## Ctx and RenderFunc

```go
type Ctx struct {
	DB     *sql.DB
	Logger *slog.Logger
	Assets *Assets
	Actor  Actor
	Render RenderFunc

	ErrorPage ErrorPageFunc
}
```

Passed to every generated action: your own wiring, built once by your
ctx factory. Per-request state does not live here — identity is
`sessions.Current(r)`, locale is `LocaleFrom(r)`.

`Render` is the manifest system's seam. A generated action cannot call
an app-private helper, so it calls `ctx.Render` — and nil-checks it,
answering a logged 500 instead of a nil-pointer panic if you forget to
wire it.

`ErrorPage` is the same seam for the unhappy path: it is what
[`view.Fail`, `view.NotFound` and `view.Forbidden`](/docs/reference/view)
render through. Nil is legal, and the helpers answer plain text.

## Localization

```go
func NewLocales(codes []string, def string, base Catalog, fsys fs.FS) (*Locales, error)
func T(r *http.Request, key string) string
func Tf(r *http.Request, key string, args ...any) string
func LocaleFrom(r *http.Request) string
func BaseCatalog() Catalog
func BaseCatalogs() map[string]Catalog
func BaseLocales() []string
func BaseKeys() []string
func IsBaseKey(key string) bool
func Dir(locale string) string
func LocaleItems(r *http.Request) []LocaleItem
type LocaleItem struct {
	Code    string
	Name    string
	Href    string
	Current bool
}
const LocaleCookie = "rastrillo_locale"
const LocaleSwitchPath = "/_locale"
```

`T` and `Tf` are the request-scoped lookups actions call; `Tf`
interpolates `{name}` placeholders. Lookup falls back through the
requested locale, the default locale, the framework's catalog for that
locale, the framework's English, and finally the key itself, so a
missing translation stays visible on the page instead of blank.

`Catalog` is a flat `map[string]string`. `BaseCatalog` carries
`rastrillo/ui`'s own English strings and is wired into every served app
automatically; your entry for the same key wins. `BaseCatalogs`,
`BaseLocales` and `BaseKeys` are the twelve shipped catalogs, their
codes, and the `rastrillo.ui.*` key set an unshipped locale has to
translate before `generate --check` passes; `IsBaseKey` reports whether
one key is the framework's. `Dir` is the `<html dir>` value for a
locale, `"rtl"` or `"ltr"`.

`LocaleItems` builds the language switcher's data for a request — one
`LocaleItem` per declared locale, empty for a one-locale app — and the
`locale-menu` partial renders it as a form posting to `LocaleSwitchPath`.

`Locales` is the resolved set: `Locales.Codes`, `Locales.Default`,
`Locales.Has`, `Locales.FrameworkHas`, `Locales.T`, `Locales.Tf`,
`Locales.Middleware`, which strips the path prefix before the app's mux
sees it, and `Locales.SwitchHandler`, the `POST /_locale` route that
writes `LocaleCookie` and redirects — `Serve` mounts it whenever
`Options.Locales` is set, one locale or twelve.

[Localization](/docs/localization) has the resolution order and the
caveats.

## Assets

```go
func NewAssets(fsys fs.FS) *Assets
func (a *Assets) Path(name string) string
func (a *Assets) Handler() http.Handler
```

Fingerprints static files so they cache forever and still update on an
ordinary reload. `Path` maps a file to a URL carrying its content hash;
`Handler` serves it with an immutable `Cache-Control`. Mount at
`GET /static/` with no `StripPrefix`. See [Assets](/docs/assets).

## Icons

```go
func Icon(slug string) template.HTML
func IconSlugs() []string
```

`Icon` renders a vendored icon by slug, and an unknown slug renders
nothing instead of panicking a page mid-response. `IconSlugs` is the
eleven-slug vocabulary, which is Rastrillo's own rather than any
vendor's — [Icons](/docs/icons) explains why that matters.

## Agents

```go
type Tool struct{ /* Description, Access, Args, Confirm */ }
type ToolDef struct{ /* the registry entry generate emits */ }
type Access int
const ( ToolRead Access = iota; ToolWrite )
```

An action opts in with `var Tool = rastrillo.Tool{...}`. `ToolRead`
observes and never changes state. `ToolWrite` changes state, so it
requires a confirm sentence and an explicitly confirmed call.
`Access.String` is `"read"` or `"write"`.

```go
type Actor struct {
	Human bool
	Name  string
}
func WithActor(r *http.Request, a Actor) *http.Request
func ActorFromContext(ctx context.Context) (Actor, bool)
```

Every action's caller is attributed, never anonymous, so an audit trail
can say who did what. `Actor.String` is the audit form, `"human"` or
`"agent:<name>"`, and it is what `eventlog` stores on every appended
event — so a stream says who did what without importing this package.

[Agents and tools](/docs/agents) is the guide.

## Manifest types

The Go mirror of a `manifest/*.toml` resource, for tooling that builds
one programmatically. Declaring in TOML is the normal path —
[Manifests](/docs/manifests) is the guide.

```go
type Resource struct{ /* Name, Route, Store, Scope, List, Form */ }
func (r Resource) Validate() error
```

`Resource.Validate` refuses a field colliding with the fixed columns
every generated store emits — `id`, `created_at`, `updated_at`, and
`owner` for a scoped resource — instead of producing a confusing table.

`List` holds `Column` values and `Filter` values; `Form` holds `Field`
values.

The enumerations:

| Type | Values |
|---|---|
| `Kind` | `Text`, `Textarea`, `Money` |
| `StoreKind` | `Exclusive` (one SQL table), `Mergeable` (an eventlog stream per record) |
| `ScopeKind` | `Unscoped` (the zero value), `UserScoped` (owner-filtered by session subject) |
