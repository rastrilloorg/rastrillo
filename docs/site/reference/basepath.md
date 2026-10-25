# 🤖 basepath

Use `basepath` to serve your app under a path on another app's domain, such as `https://example.com/acme`. A hub product does this: the hub is at the root, and each tenant is an instance at `/<slug>`.

Set two variables on the tenant app, once:

```sh
carlos env set --app tenant TENANT_ORIGIN=https://example.com RASTRILLO_SLUG_FROM_HOST=1
```

Each instance then reads its slug from its own hostname, `acme-x7k2pq.<account>.oncarlos.com`, and serves everything under `/acme`. A slug read this way is at most 25 characters (`provision.MaxSlug`); for a longer one, set `RASTRILLO_SLUG` instead.

In templates, write links with `path`: `<a href="{{path $ "/orders"}}">`. In Go, use `basepath.Path(r, "/orders")` for anything that goes into a page. Redirect as usual, with `http.Redirect(w, r, "/orders", http.StatusSeeOther)`: the base is added for you. To send someone to the hub, use a full URL.

The instance's own hostname sends every visitor to `https://example.com/acme/…`. To show a public page there instead, with nobody signed in and no forms that change anything, set `opts.BasePath.DirectHost = basepath.ServeDirect`.

To try it locally, run `RASTRILLO_SLUG=acme rastrillo dev` and open `http://localhost:8080/acme`. `http://127.0.0.1:8080` is then your direct host.

Sign-in does not run under a base path. The hub signs people in.

`amadan.net/rastrillo/rastrillo/basepath`

## Config

```go
type Config struct {
	Origin     string
	Slug       string
	DirectHost DirectHost
}

type DirectHost int

const (
	RedirectDirect DirectHost = iota
	ServeDirect
)

func (c Config) Enabled() bool
func (c Config) Validate() error
func FromEnv(origin string, getenv func(string) string) (Config, error)
func SlugFromHost(host string) (string, error)
```

`Origin` is the product origin, a scheme and host with no path: `https://example.com`. `Slug` is the one path segment the app lives under. The zero `Config` is off, and `Enabled` reports whether a `Config` asks for a base path at all.

`Validate` checks that `Origin` is an `http` or `https` origin with nothing after the host, and that `Slug` is 1 to 56 lower-case letters, digits and inner hyphens.

`FromEnv` reads `RASTRILLO_SLUG`, or, when `RASTRILLO_SLUG_FROM_HOST` is `1`, the slug in the instance's label in `CARLOS_HOST`. With neither set, it returns the zero `Config`. Setting both is an error. Reading the slug from the host is opt-in, so an app whose hostname only looks like an instance label stays at the root.

`SlugFromHost` reads the slug from a label in `provision.NewLabel`'s shape, `<slug>-<6 letters>`. That shape cuts a slug to `provision.MaxSlug` (25) characters, so a path-routed hub keeps its slugs to 25 characters, or sets `RASTRILLO_SLUG` on the tenant.

`RedirectDirect` and `ServeDirect` say what the instance's own hostname does: send the visitor to the product path, or serve the app there at the root.

## Handler

```go
func (c Config) Handler(next http.Handler) http.Handler
```

On the product origin, `Handler` answers only for `/<slug>` and below. Anything else there is a 404. An unclean path, such as `/acme//x` or `//acme/x`, gets a 301 to the clean one, or a 308 for a method other than GET and HEAD.

`next` sees the path without the base: `/acme/notes` arrives as `/notes`. `r.RequestURI` still reads `/acme/notes`.

A `Location` or `Rastrillo-Location` header that starts with one `/` gets the base added once, as the response goes out. That covers `http.Redirect` and the mux's own redirects. A full URL, a relative path and every other header are left as they are. One gap: the short HTML link `http.Redirect` writes in the body has no base. Browsers follow the header.

## Paths

```go
func Join(base, p string) string
func Path(r *http.Request, p string) string
func Base(r *http.Request) string
func Original(r *http.Request) string
```

`Join` holds the prefix rule, and is the only place it is written. It adds `base` to `p` only when `p` starts with exactly one `/`. `//host/x` and `/\host` are links to another site, so they are left alone, along with full URLs, relative paths and an empty `p`. `/` gives the base itself, and `/?q` gives the base and the query.

`Path` is `Join(Base(r), p)`. Use it for every root-absolute URL in a body. Never pass its result to `http.Redirect`: the base would appear twice.

`Base` is the base the request was served under, such as `/acme`, or `""`. `Original` is the request target as the browser sent it, base included.

## Direct host

```go
func OnDirectHost(r *http.Request) bool
func PathRouted(r *http.Request) bool
func PlatformCall(r *http.Request) bool
func ProductURL(r *http.Request) string
```

`OnDirectHost` reports whether a request came in on a host other than the product origin's. `PathRouted` reports whether it is served under a base path or on the direct host. `PlatformCall` reports whether it is the platform's own call, such as a wake check or a health probe. `ProductURL` is the page's full URL on the product origin.

Outside `Handler`, `Base` is `""`, `Path` returns `p`, `Original` is the request's own target, and the rest are false or `""`.
