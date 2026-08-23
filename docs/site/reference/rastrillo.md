# 🤖 rastrillo

`github.com/carlosframework/rastrillo`

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

`Run` resolves the platform's activation argv and calls `Serve`. It is
the right call for an app that lets the framework own the database.

**When your app opens its own database, use `Resolve` and `Serve`
instead.** `Run` re-parses argv and repopulates `Options.DBPath`, so
`Serve` would open a second connection to the file `db.Open` already
owns. `Resolve` performs the same argv and `$STATE_DIRECTORY`
resolution without serving.

`Handler` is `Serve` minus the listener — the whole framework chrome as
an `http.Handler`, for test harnesses. Call the returned cleanup when
done.

`OpenDB` is the corrected SQLite opener exported for tests. New apps
should prefer [`db.Open`](/docs/reference/db), which returns the
two-pool handle.

`BuildVersion` is the version string `GET /api/version` reports,
overridden at build time; it defaults to `"dev"`.

## Options

The one configuration struct. The fields worth knowing:

**`Mux`** or **`Router`** — exactly one must be set. `Router` builds the
mux *after* the database opens, and is handed the `*sql.DB` `Serve`
opened, which is how an app puts the framework-opened handle in its
per-request `Ctx`. `Serve` owns that handle and closes it when `Serve`
returns; do not retain it past that.

**`Wrap`** — the one seam for app middleware: sessions, CSRF, panic
pages, authorization. It runs **inside** the framework's chrome, so
`GET /healthz` and `GET /api/version` are answered outside it (platform
probes never traverse app middleware) and locale-prefix stripping
happens before it (middleware sees the paths routes match on).
Returning nil is a boot error.

**`DBPath`** — opens SQLite with the pragma ordering and connection
settings that have to be right. Blank it before `Serve` when your app
opened its own handle.

**`Migrations`** — applied in order at boot, idempotently. This is the
older additive-only mechanism; new apps use
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

## Ctx and RenderFunc

```go
type Ctx struct {
	DB     *sql.DB
	Logger *slog.Logger
	Assets *Assets
	Actor  Actor
	Render RenderFunc
}
```

Passed to every generated action: the app's own wiring, built once by
its ctx factory. **Per-request state does not live here** — identity is
`sessions.Current(r)`, locale is `LocaleFrom(r)`.

`Render` is the manifest system's seam. A generated action cannot call
an app-private helper, so it calls `ctx.Render`; a generated action
nil-checks it and answers a logged 500 rather than a nil-pointer panic
when an app forgets to wire it.

## Localization

```go
func NewLocales(codes []string, def string, base Catalog, fsys fs.FS) (*Locales, error)
func T(r *http.Request, key string) string
func Tf(r *http.Request, key string, args ...any) string
func LocaleFrom(r *http.Request) string
func BaseCatalog() Catalog
const LocaleCookie = "rastrillo_locale"
```

`T` and `Tf` are the request-scoped lookups actions call; `Tf`
interpolates `{name}` placeholders. Lookup falls back through the
requested locale, the default locale, the framework base catalog, and
finally the key itself — a missing translation stays visible, never
blank.

`Catalog` is a flat `map[string]string`. `BaseCatalog` carries
`rastrillo/ui`'s own strings and is wired into every served app
automatically; an app entry for the same key wins.

`Locales` is the resolved set: `Locales.Codes`, `Locales.Default`,
`Locales.Has`, `Locales.T`, `Locales.Tf`, and `Locales.Middleware`,
which strips the path prefix before the app's mux sees it.

Nothing writes `LocaleCookie` yet — persisting a user's choice is the
app's job for now. [Localization](/docs/localization) has the
resolution order and the two caveats.

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

`Icon` renders a vendored icon by slug; an unknown slug renders
**nothing** rather than panicking a page mid-response. `IconSlugs` is
the eleven-slug vocabulary, which is Rastrillo's own rather than any
vendor's — [Icons](/docs/icons) explains why that matters.

## Agents

```go
type Tool struct{ /* Description, Access, Args, Confirm */ }
type ToolDef struct{ /* the registry entry generate emits */ }
type Access int
const ( ToolRead Access = iota; ToolWrite )
```

An action opts in with `var Tool = rastrillo.Tool{...}`. `ToolRead`
observes and never changes state; `ToolWrite` changes state and
therefore requires a confirm sentence and an explicitly confirmed call.
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
can say who did what. `Actor.String` is the audit form — `"human"` or
`"agent:<name>"` — which is what `eventlog` stores on every appended
event, so a stream says who did what without importing this package.

[Agents and tools](/docs/agents) is the guide.

## Manifest types

The Go mirror of a `manifest/*.toml` resource, for tooling that builds
one programmatically. [Manifests](/docs/manifests) is the guide, and
declaring in TOML is the normal path.

```go
type Resource struct{ /* Name, Route, Store, Scope, List, Form */ }
func (r Resource) Validate() error
```

`Resource.Validate` refuses a field colliding with the fixed columns
every generated store emits — `id`, `created_at`, `updated_at`, and
`owner` for a scoped resource — rather than producing a confusing table.

`List` holds `Column` values and `Filter` values; `Form` holds `Field`
values.

Three enumerations:

| Type | Values |
|---|---|
| `Kind` | `Text`, `Textarea`, `Money` |
| `StoreKind` | `Exclusive` (one SQL table), `Mergeable` (an eventlog stream per record) |
| `ScopeKind` | `Unscoped` (the zero value), `UserScoped` (owner-filtered by session subject) |
