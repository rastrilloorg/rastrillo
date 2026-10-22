# 🤖 csrf

`amadan.net/rastrillo/rastrillo/csrf`

Same-origin enforcement for state-changing requests. No tokens, nothing
to thread through a template.

## Protect

```go
func Protect(origin string, opts ...Option) func(http.Handler) http.Handler
```

Middleware refusing cross-origin `POST`, `PUT`, `PATCH` and `DELETE`.
Mount it app-wide, above your route groups:

```go
r := chi.NewRouter()
r.Use(csrf.Protect(origin))
```

Mounting it once at the top is what makes a route you add six months
from now protected by default, instead of protected by someone
remembering.

Safe methods pass through. `GET` and `HEAD` are not state-changing, and
an app that mutates on `GET` has a different problem this package cannot
fix.

To let a machine through on one path, pass [`Exempt`](#exempt).

## Option

```go
type Option func(*options)
```

A setting for `Protect`. `Exempt` is the only one today. Existing calls
to `Protect(origin)` need no change.

## Exempt

```go
func Exempt(paths ...string) Option
```

Lets `POST`, `PUT`, `PATCH` and `DELETE` on the named paths through
without browser evidence. Use it for a path that a server calls, such as
a payment provider's webhook or a `carlos schedule` tick. A server sends
no `Sec-Fetch-Site`, `Origin` or `Referer`, so `Protect` would refuse
every call.

```go
r.Use(csrf.Protect(origin, csrf.Exempt("/stripe/webhook", "/jobs/sync")))
```

**An exempt path is not checked by `Protect` at all.** Its handler must
check its own credential first and refuse the request if the check
fails. Without that check, any site on the internet can post to it.

A webhook checks the provider's signature. This one uses Stripe's
`webhook` package:

```go
func stripeWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	event, err := webhook.ConstructEvent(body, r.Header.Get("Stripe-Signature"), endpointSecret)
	if err != nil {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	// event is genuine. Act on it.
}
```

A schedule tick checks the bearer token with [`carlos.Tick`](/docs/reference/carlos#tick):

```go
func syncTick(w http.ResponseWriter, r *http.Request) {
	if !carlos.Tick(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	// The platform called this. Do the work.
}
```

A path matches only when it is exactly `r.URL.Path`. `"/hook"` does not
exempt `/hook/`, `/HOOK` or `/hook/x`, so a route you add later under it
stays protected. `Exempt` panics if a path is empty or does not start
with `/`, so a typo stops the app at start instead of leaving the
webhook answering `403`.

## SameOrigin

```go
func SameOrigin(r *http.Request, origin string) bool
```

The predicate `Protect` applies, exported for a handler that needs the
same judgement outside the middleware.

Evidence is checked in order of quality. `Sec-Fetch-Site` first: the
browser sets it, page script cannot forge it, and it is unambiguous.
Then `Origin`, sent on cross-origin state-changing requests and reliable
when present. Then `Referer`, for the shrinking set of clients sending
neither.

## Why no tokens

A synchroniser token has to be minted, stored, embedded in every form
and validated — four places to get it wrong, and the failure mode is a
form that mysteriously rejects a legitimate submission.

The header check needs none of that and is stronger against the attacks
that matter, because `Sec-Fetch-Site` cannot be set by the page mounting
the attack. The cost is that a client sending none of the three headers
on a state-changing request is refused, which for an app served to
browsers is the right answer anyway.
