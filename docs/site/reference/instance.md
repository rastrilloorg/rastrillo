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
