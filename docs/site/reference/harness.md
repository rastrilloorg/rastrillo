# 🤖 harness

`github.com/carlosframework/rastrillo/harness`

A browser-drive rig: a real app on a real localhost origin, driven by a
real Chromium with a CDP virtual authenticator attached, for the tests
that a Go-only suite cannot reach — a passkey ceremony, a JS-enhanced
form, anything that needs an actual browser to be honest about.

Every file but this package's doc comment is built only under
`-tags browser`, so `chromedp` and its CDP dependencies stay out of the
plain build graph: `go build ./...` and `go test ./...` never pull them
in, and only `go test -tags browser ./...` does.

```
go test -tags browser ./...
```

## The origin chicken-and-egg

A passkey app needs its origin before it can even build its handler —
CSRF and WebAuthn config both read it — but the origin doesn't exist
until a port is bound. `New` resolves the order: bind a localhost
listener first, compute `http://localhost:PORT` from it, hand that
origin to your `build` function, serve on the listener, and only then
launch Chromium.

The origin is always `localhost`, never a bare IP: WebAuthn's relying
party ID has to be a registrable domain, and an IP address is not one.
Navigate `Rig.Origin`, not the listener's address.

```go
type Rig struct {
	Origin string
}

func New(t *testing.T, build func(origin string) http.Handler, opts ...Option) *Rig
```

`New` also attaches a CDP virtual authenticator to the browser before
handing back the rig, so any WebAuthn ceremony the app drives has
somewhere to land — resident keys, user verification and PRF are all
enabled on it. Cleanups are registered browser-after-server, so
`t.Cleanup`'s LIFO order tears the browser down first, before the
server it was talking to.

## Options

```go
type Option func(*config)
```

`New` takes a variadic list of `Option` values to adjust what it builds.
None are exported by this package yet — a later change adds the first
one.

## Finding Chromium

```go
func ChromePath(t *testing.T) string
```

`ChromePath` locates the Chromium a browser-tagged test drives, in
order: the `RASTRILLO_CHROME` environment variable, then `chromium`,
`chromium-browser`, `google-chrome` and `google-chrome-stable` on
`PATH`, then the Playwright cache
(`~/.cache/ms-playwright/chromium-*/chrome-linux64/chrome`).

A skip is not a pass: with no browser found, `ChromePath` fails the
test outright, unless `RASTRILLO_BROWSER_OPTIONAL` is set — which turns
that failure into a deliberate, visible skip instead of a silent gap in
coverage. `New` calls `ChromePath` itself, so a rig-based test gets
this behavior for free.

## Driving the browser

```go
func (r *Rig) Context() context.Context
func (r *Rig) Run(actions ...chromedp.Action)
```

`Run` executes a script of plain `chromedp` actions against the rig's
browser — no extra DSL on top of chromedp's own. On failure it fails
the test immediately, and its message includes whatever was on screen
at the time: a failing drive always reports what a person looking at
the browser would have seen, not just the error chromedp returned.

`Context` exposes the rig's underlying `chromedp` context directly, for
the rare drive that needs a tighter deadline than the test binary's
own timeout affords.

## The loud-failure watchers

```go
func (r *Rig) Allow(method, path string, status int)
func (r *Rig) Screen(selector, note string)
```

`New` wires up a set of watchers before it hands back the rig, so a
silent failure is impossible: a console error or assertion, an
uncaught exception, a failed request, and any response with status
`>= 400` are all recorded as problems. A 4xx/5xx response also shows up
a second time as Chromium's own console-error mirror of it — that
mirror arrives over the CDP log domain rather than the console-API
event, and carries only a URL, not a method or status.

`Screen(selector, note)` is the gate a drive passes at every screen
boundary: it waits for `selector` to become visible, then flushes the
accumulated problems, failing the test — naming `note` and whatever
was on screen — if any turned up. `"body"` is the whole-page case for
rastrillo's server-rendered apps, which have no `#app` convention to
hard-fail on.

Some probes are expected — a signed-out boot asking `/api/me` and
being told 401 is how an app finds out to show the sign-in screen.
`Allow(method, path, status)` excuses exactly that response, matched
by path, and its console-error mirror along with it, matched by path
alone since the mirror carries no method or status. `New` calls
`Allow(http.MethodGet, "/favicon.ico", http.StatusNotFound)` itself,
so the browser's own favicon probe never needs rediscovering by every
app that uses the rig.
