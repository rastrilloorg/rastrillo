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

```go
func ProbeConfig(opts Options)
```

With `RASTRILLO_CHECK_CONFIG=1` set, `ProbeConfig` checks the locale
setup in `opts`, prints a JSON report and exits, without opening or
serving anything. It exits 0 for a good setup, 1 for problems, and 2
for any value but `1`. Unset, it returns. `rastrillo doctor` runs your
app this way.

`Run` and `Resolve` call it first. If your `main` calls neither, make
`ProbeConfig` its first line: while the variable is set, `Serve`,
`Handler`, `OpenDB` and `db.Open` refuse to open the database.

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

**`CSP`** — swaps the baseline content-security policy. The baseline
allows no inline styles or scripts. The framework sets baseline security
headers outermost, and your own `Set` or `Del` wins.

**`NextDue`** — answers the activator's `GET /api/next-due` scheduled-wake
poll. Unset, the route does not exist.

**`Sidecar`** — the app's sidecar pass, run in a loop when the platform
spawns `<binary> sidecar run`. See [Agents](/docs/agents).

**`Background`** — your app's
[`background.Group`](/docs/reference/background). `Serve` stops it after
the last request finishes and before the database closes, so work still
running does not lose its database.

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

**`NoSpeculationRules`**: turns off prerendering. By default every
response names `SpeculationRulesPath`, where `Serve` answers with rules
that prerender the navigation in this release's sidebar and console
layouts. A content-security policy does not turn it off; this field
does, or deleting the `Speculation-Rules` header in a handler.

```go
const SpeculationRulesPath = "/_speculation-rules"
```

**`NoSchemeSwitch`**: leaves `POST /_theme` to your app. By default
`Serve` mounts the colour-scheme switch there unless your mux already
has a handler at that path, in which case yours wins. A router mounted
under `/` cannot be seen through the mux, so an app that routes
`/_theme` itself through chi or the like sets this.

**`NameInstance`**: adds the `Rastrillo-Host` header, carrying `$CARLOS_HOST`, to `GET /api/version`, so a hub can tell which instance answered. Set it on every app `provision` creates, and on the hub. Anyone can read the header, so it shows the platform hostname behind your domain.

```go
NameInstance bool
```

**`BasePath`**: serves your whole app under `/<slug>` on a shared product origin, such as `https://example.com/acme`. The framework's own routes, `/healthz` and `/api/version` among them, answer under it too. The zero value is off and costs nothing. Fill it with `basepath.FromEnv`, and set `DirectHost` in code: it is your app's choice. With `ServeDirect`, the instance's own hostname is read-only. `Handler` refuses a value `basepath` cannot serve. See [`basepath`](/docs/reference/basepath).

```go
BasePath basepath.Config
```

## Ctx and RenderFunc

```go
type Ctx struct {
	DB     *sql.DB
	Logger *slog.Logger
	Assets *Assets
	Actor  Actor
	Render RenderFunc

	Authorize func(ctx context.Context, a Actor, o Operation) error

	ErrorPage ErrorPageFunc
}
```

Passed to every generated action: your own wiring, built once by your
ctx factory. `Authorize` is the app's one policy for who may do what;
nil means the default policy. `Actor` is the one per-request value. The
router copies it from the request context (`WithActor`). If there is
none and your factory left `Actor` empty, the router uses the signed-in
person (`sessions.ActorFrom`). The signed-in user is
`sessions.Current(r)` and locale is `LocaleFrom(r)`.

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
func LocalePrefix(r *http.Request) string
```

```go
func SchemeFrom(r *http.Request) string
func SchemeSwitchHandler() http.Handler
const SchemeCookie = "rastrillo_scheme"
const SchemeSwitchPath = "/_theme"
```

```go
func FormatLocale(r *http.Request) string
func (l *Locales) Negotiate(r *http.Request) string
func RetagOf(code string) (string, bool)
func Retagged() map[string]string
```

`FormatLocale` is the CLDR locale a request's amounts are written in.
Pass it to `currency.Format`, as the generated actions do. It is not
`LocaleFrom(r)`: an app offering `zh-TW` formats money as `zh-Hant`, and
a one-language app's requests carry no language and format in its
default.

`Locales.Negotiate` is the language `Middleware` would pick for a
request (path prefix, cookie, `Accept-Language`, default) without
changing it. Use it where the middleware has not run, such as an error
page after a panic.

`RetagOf` and `Retagged` are the codes the framework moved: today only
`pt`, now `pt-PT`. They are exported for the framework's own tools; see
[Localization](/docs/localization#portuguese-is-pt-pt).

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
`Options.Locales` is set, one locale or twelve. `LocalePrefix` is the
locale segment `Middleware` stripped from this request's path, `/fr`,
or `""` when the URL carried none; `ui.ForRequest` puts it back on
`Return`, so a switch that redirects back lands where the reader was.

`SchemeFrom` is the colour scheme a request's `SchemeCookie` chose:
`light`, `dark`, or `""` for System and for anything the framework did
not write. The shells render it as `data-theme` on `<html>`.
`SchemeSwitchHandler` is the `POST /_theme` route the `scheme-menu`
partial posts to: it stores `light` or `dark`, clears the cookie for
`system`, refuses anything else, and redirects to the form's return
path through the same guard `/_locale` uses. `Serve` mounts it unless
your app already handles the path; see `Options.NoSchemeSwitch`. The
cookie is a display preference and nothing else reads it.

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
type Access int
const ( Read Access = 1; Write Access = 2 )
func (a Access) String() string
func (a Access) Valid() bool

type Operation struct {
	ID, Resource string
	Scoped       bool
	Access       Access
}
```

`Read` observes and never changes state. `Write` changes state. The zero
`Access` is invalid: set one explicitly. `Access.String` is `"read"`,
`"write"` or `"invalid"`, and `Access.Valid` is true for `Read` and
`Write`. An `Operation` is what `Ctx.Authorize` is asked about.

```go
type Actor struct {
	Subject, Agent, Credential string
	Access                     Access
	Session, Harness           string
}
func (a Actor) String() string
func (a Actor) LogAttrs() []slog.Attr
func (a Actor) IsToken() bool
func WithActor(r *http.Request, a Actor) *http.Request
func ActorFromContext(ctx context.Context) (Actor, bool)
func ActorPhrase(a Actor, name func(subject string) string, t func(key string, args ...any) string) string
```

Every action's caller is attributed, never anonymous, so an audit trail
can say who did what. `Subject` is the person, `Agent` is the agent
acting for them (empty when they act directly), and `Credential` is the
token's public ID (empty for a browser session). The zero `Actor` is the
app itself. `Actor.String` is the audit form: `app`, `user:<subject>`,
`user:<subject> token:<id>` or `agent:<name> for user:<subject>
token:<id>`. It is what `eventlog` stores on every appended event, so a
stream says who did what without importing this package.
`Actor.LogAttrs` returns the same as `slog` attributes, plus
`agent_session` and `agent_harness` when the client sent them.
`Actor.IsToken` is true when `Credential` is set.

`ActorPhrase` is the words a page shows for an actor: nothing for the
app, the person's name, or `Claude for Paul` for an agent. `name` turns
a subject into a name; if it is nil or finds no name, the page shows the
subject. `t` is the page's translator, such as `Locales.Tf` for the
reader's language. In a template, use `ui`'s `actor` function instead.

[Agents](/docs/agents) is the guide.

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

```go
type Resource struct{ /* ..., Currency string, Legacy *Legacy */ }
type Legacy struct {
	Currency string
	Scale    *int
}
func (r *Resource) HasMoney() bool
```

`Resource.Currency` is the default currency for new records, and
`Legacy` is the `[legacy]` table: what integers stored before money had
a currency mean. `Scale` is a pointer because 0 is a real scale, whole
units, and a missing scale must not read as one. `HasMoney` reports
whether any column or field is money, and so whether the resource needs
a `Currency`. See [Manifests](/docs/manifests#money).

```go
type Resource struct{ /* ..., API API */ }
type API struct {
	Expose *bool
}
func (r Resource) Exposed() bool
```

`Resource.API` is the `[api]` table. `Exposed` reports whether the
resource is on the JSON API. A `scope = "user"` resource is on it and an
unscoped one is not, unless `API.Expose` says otherwise. Leave `Expose`
nil to keep the default for the scope.

`Resource.Validate` also refuses a filter on a field whose wire name is
`q`, `after` or `limit`. API lists already use those query parameters
for search and paging.

The enumerations:

| Type | Values |
|---|---|
| `Kind` | `Text`, `Textarea`, `Money` |
| `StoreKind` | `Exclusive` (one SQL table), `Mergeable` (an eventlog stream per record) |
| `ScopeKind` | `Unscoped` (the zero value), `UserScoped` (owner-filtered by session subject) |
