# 🤖 Testing

`rastrillo new` ships an app whose tests pass before you have written
anything, and a `make ci` that defines the gate once.

## The harness

The scaffold writes `internal/<app>test/` — a harness plus example
tests. It builds your app the way `main.go` does and drives it over
HTTP, so what a test exercises is what a request exercises: the router,
the middleware, the session store, CSRF.

Its `post()` sends an `Origin` header, because
[`csrf.Protect`](/docs/sessions) is mounted from day one and a test that
forgot would 403 confusingly.

## Serving without a listener

```go
handler, cleanup, err := rastrillo.Handler(opts)
```

`Handler` is `Serve` minus the listener: the whole framework chrome —
`/healthz`, `/api/version`, locale-prefix stripping, `Options.Wrap` — as
an `http.Handler` you can hand to `httptest.NewServer`. Call the
returned cleanup when you are done.

`rastrillo.OpenDB` is the same corrected SQLite opener `Serve` uses,
exported so a test can open a database with the right pragma order
instead of an approximation of it.

## make ci

```sh
make ci
```

is `vet` + `fmt` + `test` + `rastrillo migration check`, and it is the
one gate definition. `.amadan/ci` and `.amadan/ci.d/` are executable
steps that delegate to it, so the CI runner and your terminal cannot
disagree about what passing means.

Keeping [`migration check`](/docs/migrations) in that gate is what stops
models and migrations drifting apart between deploys. It touches no
database, so it runs anywhere.

## Testing a two-user rule

The rule most worth a test is the one in [scoping](/docs/scoping):
another user's row must answer 404. Sign in as A, create a row, sign in
as B, request it by id, assert 404. `examples/notes` proves both its
declared and hand-written halves with one two-user suite.

Write that test once per owned resource. It is the cheapest insurance
against the most expensive bug.

## Passkey ceremonies

`webauthn/authtest` is a fake authenticator, public so your tests can
drive a full registration and assertion without hardware. See
[Passkeys](/docs/passkeys).

## The pins test

```sh
go test -tags pins ./internal/iconsets/
```

Build-tagged and deliberately outside the ordinary suite, because it
reaches jsdelivr and the npm registry. A check that fails when someone
else's CDN has a bad afternoon teaches people to ignore failures. Run it
at release — [Icons](/docs/icons) explains what it verifies.
