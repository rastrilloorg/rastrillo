# 🤖 Deploying on CARLOS

Rastrillo builds apps; the CARLOS platform runs them. The contract
between the two is what `rastrillo.Resolve` and `rastrillo.Serve`
implement, and none of it is yours to hand-roll.

## What the framework answers

Activation argv in both shapes the platform uses: `-socket`/`-addr`/`-db`
flags for an agent exec child on a hibernating route, or a bare `serve`
subcommand for a `carlos-app@.service` unit tenant. `LISTEN_FDS` socket
activation, before falling back to `Addr`. `$STATE_DIRECTORY`, so a
relative `-db` resolves inside it when systemd provides one — a unit
tenant's working directory is not its state directory. `GET /healthz`
and `GET /api/version`. The SIGTERM drain, which fits inside the
activator's SIGKILL budget. And baseline security headers, CSP included,
framework-owned and outermost, with your own `Set` or `Del` winning and
`Options.CSP` swapping the policy wholesale.

Between `Serve` and `Run`, every route kind the platform runs —
always-on instance, hibernating exec child, unit tenant — boots the same
scaffolded app.

## Hibernation needs nothing from you

The activator owns the restore/replicate cycle. Your app does not
participate in it, does not know it is happening, and needs no hook.

Two consequences worth designing around. A wake can be cut short at any
moment, which is why [migrations](/docs/migrations) apply one
transaction at a time and a killed wake rolls back cleanly and retries.
And background work does not survive: a [job](/docs/jobs) is a
goroutine, and hibernation ends it, so keep jobs idempotent.

## Work on a clock

Since hibernation ends goroutines, a timer inside your process is not a
scheduler. The platform keeps the clock instead: you declare a schedule
from outside the app with `carlos schedule set -name sync -every 6h
-path /jobs/sync`, and when it comes due the platform wakes the instance
and POSTs to that path.

Your side is an ordinary handler, guarded so that a stranger POSTing the
same path gets nothing:

```go
func handleSync(w http.ResponseWriter, r *http.Request) {
	if !carlos.Tick(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if err := syncer.RunOnce(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
```

Do the work inside the request. The instance stays awake while it is
open, and the idle clock starts when you return.

One-off work — a reminder, an expiry — you ask for yourself with
`carlos.ScheduleAt`, and it arrives as the same kind of tick.
[`carlos`](/docs/reference/carlos) is the package, and the reason
`TickOccurrence` is there: delivery is at-least-once, so a retry brings
the same occurrence back.

## The middleware seam

`Options.Wrap` is where your app middleware goes — sessions, CSRF, panic
pages, authorization:

```go
opts.Wrap = func(next http.Handler) http.Handler {
	return sess.Middleware(next)
}
```

It runs inside the framework's chrome. `GET /healthz` and
`GET /api/version` are answered outside it, so platform probes never
traverse your middleware and a broken authorization layer cannot make
your app look dead. Locale-prefix stripping happens before it, so your
middleware sees the same paths your routes match on.

Returning nil from `Wrap` is a boot error, not a silent pass-through.

`rastrillo.Handler` is `Serve` minus the listener, for test harnesses —
see [Testing](/docs/testing).

## Your first deploy with migrations

It has to be schema-neutral, and this is the rule most likely to bite
you.

If your app is already deployed and you are introducing migrations for
the first time, generate `0001_init` from the models as already deployed
and ship it alone. Change a model only in a later release.

Otherwise boot refuses on the new column, and `baseline` — the tool you
would reach for under pressure — would strand that migration for good.
[Migrations](/docs/migrations#the-first-deploy-of-a-version-with-migrations-must-be-schema-neutral)
has the detail.

## Shipping

`examples/helloworld` is a real scaffolded app, checked in, proven to
ship, promote and serve through the actual `carlos` binary.
`hack/local-deploy-demo.sh` in the framework repo runs the sequence.

The platform's own documentation owns the `ship`/`promote` commands and
their flags. What matters from this side is that a Rastrillo binary is
an ordinary CARLOS app: no wrapper, no init container, no
platform-specific build.
