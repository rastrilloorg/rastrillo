# 🤖 instance

Use `instance` to decide, at startup, whether your app runs under a root app or on its own. A root app provides one instance per customer account and signs in that account's members. If the settings are invalid, the app refuses to start.

`amadan.net/rastrillo/rastrillo/instance`

Set the variables once, on the instance:

```sh
carlos env set --app <tenant-app> RASTRILLO_ROOT=https://example.com RASTRILLO_ROOT_KEYS=<kid>.<base64url>
```

To run with no root, set `RASTRILLO_STANDALONE=1` instead.

## FromEnv

```go
type Mode int

const (
	Standalone Mode = iota + 1
	Rooted
)

type Setup struct {
	Mode Mode
	Root string
	Keys map[string][]byte
}

func FromEnv(getenv func(string) string) (Setup, error)
```

`FromEnv(os.Getenv)` reads three variables and returns a `Setup`.

- `RASTRILLO_ROOT` is the root's origin: `https://example.com`, with HTTPS, no path and no trailing slash.
- `RASTRILLO_ROOT_KEYS` is the root's public keys, as `sessions.ParsePublicKeys` reads them: one or two comma-separated `<kid>.<base64url>` values with distinct kids. Two is for rotating keys. They are the same keys the hub's signed sessions use.
- `RASTRILLO_STANDALONE=1` selects standalone.

A rooted `Setup` has `Mode` `Rooted`, plus `Root` and `Keys`. A standalone one has `Mode` `Standalone` and nothing else.

Exactly one mode must be chosen. Neither set is an error, so the app refuses to start. Both set (`RASTRILLO_STANDALONE=1` with `RASTRILLO_ROOT`) is an error too. So is a root without keys, keys without a root, a root that is not an HTTPS origin, and keys that do not parse. Each error names the variable to fix and carries none of the values.

Call `FromEnv` once in `main`, and stop on its error. Hand a rooted `Setup` to [`handoff`](/docs/reference/handoff) and [`roster`](/docs/reference/roster). A standalone app mounts neither.

## Serving on a custom domain

```go
func ServingOrigin(ctx context.Context, db *sql.DB, fallback string) (string, error)
func RedirectToOrigin(origin string, next http.Handler) (http.Handler, error)
```

An account can serve its instance on its own domain. The root sends that origin in the roster snapshot, and the instance stores it (see [`roster`](/docs/reference/roster)). Wire it like this:

1. After migrations, and before you build handlers, call `ServingOrigin(ctx, db, fallback)`. It returns the stored origin, or `fallback` when there is none. Pass as `fallback` the public origin the root sends members to, such as `https://<slug>.<parent>` under a catch-all. That is `https://` and `carlos.Host()` only when the instance has no parent. Use the result as the app's origin, for handoff, sessions, CSRF and emailed links.
2. Wrap the app's handler in `RedirectToOrigin(origin, handler)`. Requests on any other host go to the same path and query on `origin`. The platform's own paths are answered on every host: `/_carlos/*`, `/api/version` and `/healthz`. An empty `origin` redirects nothing. Any other `origin` must be `https://` and a lower-case host with no path, or `RedirectToOrigin` returns an error. In local development, pass a real `https://` origin, or `""` to leave every host alone. For a path-routed tenant, wrap `RedirectToOrigin` inside `basepath`'s handler.
3. Build the roster verifier with the platform host, `https://` and `carlos.Host()`, as its audience, never the serving origin. Roster delivery then keeps working when the custom domain does not.
4. Set `roster.Config.OnOrigin` to shut the server down gracefully, so the platform starts the app again on the new origin. Start the shutdown in its own goroutine: `OnOrigin` runs inside the roster request, which the shutdown waits for.

The redirect is a 307 with `Cache-Control: no-store`. A 301 or 308 would stay in visitors' browsers and keep sending them to the custom domain after the owner goes back to the default address. A 307 also keeps a POST's method and body. Sessions do not move with the origin, so people sign in again on the new one.

Deploy this before any root sends an origin: an older instance refuses a snapshot that carries one.
