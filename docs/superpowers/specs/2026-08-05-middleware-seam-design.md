# Options.Wrap: the middleware seam — design

**Date:** 2026-08-05
**Status:** approved (Paul, 2026-08-04 evening, batched design
questions; overnight build authorized)
**Origin:** gleester's evaluation (James, 2026-08-04): "Options takes
`*http.ServeMux`, not `http.Handler`. Because it's a concrete type,
you cannot wrap the router — middleware is impossible." Verified
against `serve.go`: the app mux is mounted on the framework's outer
mux and handed to `http.Server` with no app-reachable seam. Sessions,
CSRF, panic pages, and authorization all need one.

## Decisions recorded

1. **`Wrap func(http.Handler) http.Handler` is a NEW `Options` field,
   additive.** `Mux` and `Router` keep their concrete `*http.ServeMux`
   types — no existing app or scaffold changes. Rejected: widening
   `Router` to return `http.Handler` (breaks every caller for no
   capability `Wrap` doesn't give); a `[]Middleware` slice (an app
   composes its own chain in one function; the framework needs exactly
   one seam).
2. **`Wrap` applies around the app mux only**, at the single mount
   point in `buildHandler`: `mux.Handle("/", opts.Wrap(appMux))`.
   Consequences, both deliberate:
   - `GET /healthz` and `GET /api/version` are registered on the
     framework's outer mux and are **never wrapped** — the platform's
     probes must not traverse app sessions, auth, or redirects.
   - Locale middleware wraps the outer mux (unchanged), so app
     middleware runs **inside** locale stripping: it sees the same
     stripped paths the app's routes match on (`/orders`, never
     `/fr/orders`), and `rastrillo.T(r, …)` works inside middleware
     because the translator already rides the request context.
3. **Nil `Wrap` is the zero value and means today's behavior
   exactly.** A `Wrap` that returns nil is a boot error
   (`rastrillo: Options.Wrap returned a nil handler`), matching
   `Router`'s nil-mux check — fail loudly at boot, not with a panic on
   first request.
4. **`Wrap` composes with both `Mux` and `Router`** — it applies after
   `buildMux` resolves the choice, so the F4 pattern (Router closes
   over the framework-opened `*sql.DB`) and `Wrap` are orthogonal.
5. **No scaffold or example adoption in this slice.** `rastrillo new`
   output is unchanged; no stock app currently needs middleware
   (YAGNI). The README's Serve bullet gains the seam; the field
   comment carries the dated origin per house style.

## The change (serve.go, complete)

```go
// Wrap, if set, wraps the app's mux — the one seam for app
// middleware: sessions, CSRF, panic pages, authorization
// (gleester's friction, James 2026-08-04). It runs inside the
// framework's chrome: GET /healthz and GET /api/version are
// answered outside it (platform probes never traverse app
// middleware), and locale-prefix stripping happens before it, so
// middleware sees the same paths routes match on. Nil means no
// wrapping. Returning nil is a boot error.
Wrap func(http.Handler) http.Handler
```

`buildHandler` change: resolve `appHandler := http.Handler(opts.Mux)`;
if `opts.Wrap != nil`, `appHandler = opts.Wrap(opts.Mux)` with the nil
check; `mux.Handle("/", appHandler)`. Nothing else moves.

## Tests (serve_router_test.go / serve_test.go conventions)

1. **Wrap observes app requests**: a marker-header middleware wraps a
   route; response carries the marker.
2. **Chrome is unwrapped**: same middleware; `GET /healthz` and
   `GET /api/version` responses provably lack the marker.
3. **Composes with Router**: `Options.Router` + `Wrap` — middleware
   runs and the handler still sees the DB-backed Ctx.
4. **Inside locale stripping**: app declares locales; middleware
   records `r.URL.Path` for `/fr/orders`; sees `/orders`, and `T(r,…)`
   resolves French inside the middleware.
5. **Nil Wrap unchanged**: existing suite passes untouched.
6. **Wrap returning nil**: Serve/buildHandler returns the named boot
   error.
7. **Short-circuit works**: middleware that 302s without calling
   `next` — the app handler is not invoked (the actual sessions/auth
   use case).

## Companion doc in the same branch (not this spec's scope)

`2026-08-05-scope-and-viewer-design-questions.md` — pre-design
questions for the Scope-hook/auth cycle (gleester's second finding).
Explicitly **not** a spec: no decisions are recorded there; it exists
so the design cycle with Paul (and James's input) starts from written
questions instead of a blank page.
