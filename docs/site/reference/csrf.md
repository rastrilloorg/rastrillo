# 🤖 csrf

`github.com/carlosframework/rastrillo/csrf`

Same-origin enforcement for state-changing requests. Two symbols, no
tokens, nothing to thread through a template.

## Protect

```go
func Protect(origin string) func(http.Handler) http.Handler
```

Middleware refusing cross-origin `POST`, `PUT`, `PATCH` and `DELETE`.
Mount it **app-wide**, above the route groups:

```go
r := chi.NewRouter()
r.Use(csrf.Protect(origin))
```

Mounting it once at the top is what makes a route added six months from
now protected by default rather than protected by someone remembering.

Safe methods pass through. `GET` and `HEAD` are not state-changing, and
an app that mutates on `GET` has a different problem this package
cannot fix.

## SameOrigin

```go
func SameOrigin(r *http.Request, origin string) bool
```

The predicate `Protect` applies, exported for a handler that needs to
make the same judgement outside the middleware.

Evidence is checked in order of quality:

1. **`Sec-Fetch-Site`** — set by the browser, unforgeable by page
   script, and unambiguous. Trusted first wherever present.
2. **`Origin`** — sent on cross-origin state-changing requests, and
   reliable when present.
3. **`Referer`** — the fallback, for the shrinking set of clients
   sending neither of the above.

## Why no tokens

A synchroniser token has to be minted, stored, embedded in every form,
and validated — four places to get it wrong, and the failure mode is a
form that mysteriously rejects a legitimate submission.

The header-based check needs none of that and is strictly stronger
against the attacks that matter, because `Sec-Fetch-Site` cannot be set
by the page mounting the attack. The cost is that a client sending none
of the three headers on a state-changing request is refused — which, for
an app served to browsers, is the correct answer anyway.
