# 🤖 carlos

`github.com/carlosframework/rastrillo/carlos`

Work that has to happen at a particular time, on a platform where your
process is usually asleep. The platform keeps the clock and POSTs to
your app when something is due. This package is your side of that: one
call to check the POST really came from the platform, and one to ask
for a POST later on.

Recurring work is declared from outside the app, with the platform CLI:

```
carlos schedule set -name sync -every 6h -path /jobs/sync
```

Your app's whole part is a handler on `/jobs/sync`.

## Tick

```go
func Tick(r *http.Request) bool
```

A tick arrives on a public route, so a tick nobody authenticated is an
internet request to that path. `Tick` is the constant-time comparison
that tells them apart: the platform presents your instance's
`$CARLOS_ADMIN_TOKEN` as a bearer, and nothing else about the request
proves anything.

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

Do the work inside the request, as above. The platform holds the
instance awake for as long as the request is open and starts the idle
clock the moment you return, so returning 202 and finishing in a
goroutine gets that goroutine hibernated mid-job. Your status is the
whole reply: 2xx means done, 5xx asks for a retry with backoff, 4xx
means don't bother.

With no token in the environment `Tick` is always false. An app the
platform never handed the secret to cannot tell a tick from a stranger,
and the `X-Carlos-Schedule` headers are not evidence — anyone can set
those.

`Tick` looks at the bearer and nothing else. That is what lets the same
handler serve a "Sync now" button behind your own auth. The scheme is
matched case-insensitively, so `bearer` from a runbook `curl` works too;
the token itself is compared in constant time.

## TickOccurrence

```go
func TickOccurrence(r *http.Request) (string, bool)
```

Delivery is at-least-once, and you will see the same occurrence twice:
a handler that outruns the platform's timeout, a box that reboots
mid-tick, a deploy that cuts the instance out from under a long job.
`TickOccurrence` returns the key that stays the same across all of
those. It is the instant the delivery is *for*, not the moment it
arrived.

```go
if !carlos.Tick(r) {
	http.Error(w, "forbidden", http.StatusForbidden)
	return
}
if occ, ok := carlos.TickOccurrence(r); ok && alreadyDone(occ) {
	w.WriteHeader(http.StatusNoContent)
	return
}
```

Record it when the work succeeds and skip it when it comes round again.
Never key on the wall clock: a retry twenty minutes later is the same
occurrence, and treating it as a new one is how a job runs twice.

Keep `Tick` as the guard, the way the snippet does. `TickOccurrence`
refuses everything `Tick` refuses, but it also refuses a request with no
occurrence header — and that is exactly what your own "Sync now" call
looks like. Guard on `TickOccurrence` alone and the manual path `Tick`
was written to allow gets a 403 over a header nobody has heard of. A
missing occurrence means "no dedupe key, run it".

Treat the key as opaque. It is unique within one schedule but not
across schedules, so if a single handler serves several, key on the
`X-Carlos-Schedule` name header alongside it.

## ScheduleAt

```go
func ScheduleAt(ctx context.Context, name string, at time.Time, path string) error
```

For work with no rhythm: one reminder, one expiry, one thing at 8am on
the first. Ask for it and the platform wakes your instance at `at` and
POSTs to `path`, which is the same tick your handler already guards.

```go
err := carlos.ScheduleAt(ctx, "remind-"+id, when, "/jobs/remind")
```

`name` is your handle on the timer and matches
`^[a-z0-9][a-z0-9-]{0,31}$`. Registering the same name again replaces
the timer rather than adding one, so re-asserting everything you are
expecting at boot is safe. The control socket is bound before your
process starts, so that works from the first line of `main`. You
mostly won't need it: timers live in the box's registry, not in your
process, and a restart loses none of them.

`at` has to be set, and no more than `MaxAhead` (400 days) out. Both
bounds are checked here before anything is sent. The zero time is the
one worth knowing about: the platform would accept it, because an `at`
in the past is legal and fires on the next sweep, so a field nobody
filled in would reach someone as a reminder at boot rather than as an
error you can see. A deliberately past `at` is still fine — pass a real
one.

The call goes to the agent over a unix socket. That is quick, but it is
still I/O, so pass a context you are willing to wait on. One with no
deadline of its own gets a ten-second one.

## ScheduleCancel

```go
func ScheduleCancel(ctx context.Context, name string) error
```

Drops a timer `ScheduleAt` registered. Cancelling something that
already fired, or never existed, succeeds, so a cleanup path doesn't
have to know which.

## The errors

```go
var (
	ErrNotOnCarlos      error
	ErrUnauthorized     error
	ErrDeclaredSchedule error
	ErrTooManyTimers    error
)
```

All four are sentinels; compare with `errors.Is`.

`ErrNotOnCarlos` means there is no control socket in the environment,
which is what your laptop and your tests look like. Boot code that
registers timers should treat it as "skip", not as a failure.

`ErrUnauthorized` means no `$CARLOS_ADMIN_TOKEN`, or one the agent
would not take. It is what an instance sees if it was already running
when its box first learned these verbs, because the token is minted
into the environment at spawn; restarting the instance clears it.

`ErrDeclaredSchedule` means the name belongs to a recurring schedule
somebody declared with `carlos schedule set`. Both kinds share a
namespace, and an app cannot quietly overwrite or cancel work its
operator set up. Pick another name.

`ErrTooManyTimers` means the box is holding its limit of 1000 pending
one-shot timers for this app. Cancel finished ones, or if you are
registering the same shape of work over and over, that is a recurring
schedule wearing a disguise.

## StatusError

```go
type StatusError struct {
	Status int
	Name   string
	Body   string
}

func (e *StatusError) Error() string
```

Anything the agent refused that has no sentinel comes back as this —
a rejected `path`, a malformed body. Pull it out with `errors.As` when
you need to decide what to do next, because the two directions are
opposite: a 5xx is the agent's registry having a bad moment and is
worth retrying, a 4xx is a permanent complaint about this exact request
and retrying it is a loop. `Body` is the agent's own message, which is
usually the only place the real reason is written down.
