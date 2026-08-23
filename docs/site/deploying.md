# 🤖 Deploying on CARLOS

Rastrillo builds apps; the CARLOS platform runs them. The contract
between the two is what `rastrillo.Resolve` and `rastrillo.Serve`
implement, and **none of it is yours to hand-roll**.

## What the framework answers

- **Activation argv, in both shapes.** `-socket`/`-addr`/`-db` flags for
  an agent exec child (a hibernating route), or a bare `serve`
  subcommand with no flags for a `carlos-app@.service` unit tenant.
- **`LISTEN_FDS`** socket activation, before falling back to `Addr`.
- **`$STATE_DIRECTORY`.** A relative `-db` or `Options.DBPath` is
  resolved inside it when systemd provides one — a unit tenant's working
  directory is not its state directory.
- **`GET /healthz` and `GET /api/version`**, answered automatically.
- **The SIGTERM drain**, which fits inside the activator's SIGKILL
  budget.
- **Baseline security headers** — CSP and the rest, framework-owned and
  outermost. Your own `Set` or `Del` wins, and `Options.CSP` swaps the
  policy wholesale.

Between `Serve` and `Run`, every route kind the platform runs — always-on
instance, hibernating exec child, unit tenant — boots the same scaffolded
app.

## Hibernation needs nothing from you

The activator owns the restore/replicate cycle. Your app does not
participate in it, does not know it is happening, and does not need a
hook.

Two things follow that are worth designing around:

- **A wake can be cut short at any moment.** This is why
  [migrations](/docs/migrations) apply one transaction at a time and why
  a killed wake rolls back cleanly and retries.
- **Background work does not survive.** A [job](/docs/jobs) is a
  goroutine, and hibernation ends it. Keep jobs idempotent.

## The middleware seam

`Options.Wrap` is the one place app middleware goes — sessions, CSRF,
panic pages, authorization:

```go
opts.Wrap = func(next http.Handler) http.Handler {
	return sess.Middleware(next)
}
```

It runs **inside** the framework's chrome. `GET /healthz` and
`GET /api/version` are answered outside it, so platform probes never
traverse app middleware and a broken authorization layer cannot make an
app look dead. Locale-prefix stripping happens before it, so middleware
sees the same paths routes match on.

Returning nil from `Wrap` is a boot error, not a silent pass-through.

`rastrillo.Handler` is `Serve` minus the listener, for test harnesses —
see [Testing](/docs/testing).

## The one deploy rule that will bite you

**The first deploy of a version with migrations must be
schema-neutral.**

If your app is already deployed and you are introducing migrations for
the first time, generate `0001_init` from the models *as already
deployed* and ship it alone. Change a model only in a later release.

Otherwise boot refuses on the new column, and `baseline` — the tool you
would reach for under pressure — would strand that migration for good.
[Migrations](/docs/migrations#the-first-deploy-of-a-version-with-migrations-must-be-schema-neutral)
has the detail.

## Shipping

`examples/helloworld` is a real scaffolded app, checked in, proven to
ship, promote and serve through the actual `carlos` binary —
`hack/local-deploy-demo.sh` in the framework repo runs the sequence.

The platform's own documentation owns the `ship`/`promote` commands and
their flags. What matters from this side is that a Rastrillo binary is
an ordinary CARLOS app: it needs no wrapper, no init container and no
platform-specific build.
