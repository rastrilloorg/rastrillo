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

func WithoutPRFAtCreation() Option
```

`New` takes a variadic list of `Option` values to adjust what it builds.

`WithoutPRFAtCreation` rehearses the browsers that refuse to return PRF
(hmac-secret) output during creation, forcing `webauthn.mjs`'s
two-prompt fallback: `register()` gets an empty extension result from
`create()` and runs an immediate assertion to fetch PRF instead. The
CDP virtual authenticator can't withhold PRF at creation on its own —
`HasPrf` is all-or-nothing — so the rig fakes the condition one level
up: a script registered on new documents, in the main world, before
`New` ever navigates, wraps `navigator.credentials.create` and defines
an *own* property `getClientExtensionResults: () => ({})` on the
credential it returns. `credentials.get` is left untouched, so the
fallback assertion still gets real PRF from the authenticator.

The own-property shape matters. Patching
`PublicKeyCredential.prototype.getClientExtensionResults` would strip
PRF from the fallback's own assertion too, since assertions read the
same prototype method. Wrapping the credential in a `Proxy` trips its
brand-checked `response`/`rawId` getters with "Illegal invocation".

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
browser. There is no extra DSL on top of chromedp's own.

On failure it fails the test immediately, and the message includes
whatever was on screen at the time. A failing drive reports what a
person looking at the browser would have seen, which is usually more
useful than the error chromedp returned.

`Context` exposes the rig's underlying `chromedp` context directly, for
the rare drive that needs a tighter deadline than the test binary's
own timeout affords.

## The loud-failure watchers

```go
func (r *Rig) Allow(method, path string, status int)
func (r *Rig) Screen(selector, note string)
```

`New` wires up watchers before it hands back the rig, so a silent
failure is impossible. A console error or assertion, an uncaught
exception, a failed request, and any response with status `>= 400` are
all recorded as problems. A 4xx/5xx response also shows up
a second time as Chromium's own console-error mirror of it — that
mirror arrives over the CDP log domain rather than the console-API
event, and carries only a URL, not a method or status.

`Screen(selector, note)` is the gate a drive passes at every screen
boundary: it waits for `selector` to become visible, then flushes the
accumulated problems, failing the test — naming `note` and whatever
was on screen — if any turned up. `"body"` is the whole-page case for
rastrillo's server-rendered apps, which have no `#app` convention to
hard-fail on.

Some probes are expected. A signed-out boot asking `/api/me` and being
told 401 is how your app finds out to show the sign-in screen.
`Allow(method, path, status)` excuses exactly that response, matched by
path, and its console-error mirror along with it — matched by path
alone, since the mirror carries no method or status. `New` calls
`Allow(http.MethodGet, "/favicon.ico", http.StatusNotFound)` itself,
so the browser's own favicon probe never needs rediscovering by every
app that uses the rig.

## The junk scan

```go
func (r *Rig) AllowText(s string)
```

`Screen` doesn't just wait for its selector and flush the problem
list — between the two it scans the screen for the values that render
perfectly and say nothing: `"undefined"`, `"null"`, `"[object
Object]"` and `"NaN"`, wherever a template silently dropped a field or
mishandled a shape. The scan reads the screen the way a person would
(the root's `textContent`, rooted at `selector`) plus the places a
person cannot see — every `input`/`textarea` value and every
`[aria-label]` a screen reader would announce. Any hit fails the test,
naming the note and quoting the surrounding text so a substring like
`"null"` is legible in context rather than reported bare.

That context matters because `"null"` is also honest English —
"this contract is null and void" — so `AllowText(s)` exempts one exact
string from the scan for the rest of that test. The allowance is the
surrounding text, not the junk value: everything else on the screen,
including other occurrences of `"null"`, is still scanned.
