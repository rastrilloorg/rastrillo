# Options.Wrap Middleware Seam Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give apps one seam to wrap their mux with middleware (sessions, CSRF, panic pages, authorization) — gleester's top friction.

**Architecture:** One new additive `Options` field, `Wrap func(http.Handler) http.Handler`, applied at the single point where `buildHandler` mounts the app mux on the framework's outer mux. `/healthz` and `/api/version` stay outside it; locale stripping stays around everything, so app middleware sees stripped paths.

**Tech Stack:** Go stdlib only (`net/http`, `httptest`, `fstest`). No new dependencies.

**Spec:** `docs/superpowers/specs/2026-08-05-middleware-seam-design.md` — decisions there are binding; if one proves wrong, stop and flag it in the PR, don't redesign.

## Global Constraints

- Additive only: no existing `Options` field changes type or meaning; existing tests must pass untouched.
- House comment style: the field comment carries the dated, attributed origin ("gleester's friction, James 2026-08-04").
- Tests call `buildHandler` directly (no sockets), matching `serve_test.go`.
- Run all sweeps before every commit: `go build ./...`, `go vet ./...`, `go test ./...`.

---

### Task 1: `Options.Wrap` field, `buildHandler` seam, core behavior tests

**Files:**
- Modify: `serve.go` (Options struct ~line 40-115; `buildHandler` ~line 186-208)
- Create: `wrap_test.go`

**Interfaces:**
- Produces: `Options.Wrap func(http.Handler) http.Handler` — nil means unwrapped; applied only around the app mux; `buildHandler` returns error `"rastrillo: Options.Wrap returned a nil handler"` when Wrap yields nil.

- [ ] **Step 1: Write the failing tests** — create `wrap_test.go`:

```go
package rastrillo

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// markerWrap tags every response that traversed the middleware, and
// refuses requests to paths containing "forbidden" without calling
// next — the short-circuit shape sessions/auth middleware actually
// have.
func markerWrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Wrapped", "yes")
		if strings.Contains(r.URL.Path, "forbidden") {
			http.Error(w, "no", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func TestWrapObservesAppRequests(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/orders", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("orders"))
	})
	handler, err := buildHandler(Options{Mux: mux, Wrap: markerWrap})
	if err != nil {
		t.Fatalf("buildHandler: %v", err)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/orders", nil))
	if rec.Header().Get("X-Wrapped") != "yes" {
		t.Error("app route response missing middleware marker")
	}
	if rec.Body.String() != "orders" {
		t.Errorf("body = %q, want %q", rec.Body.String(), "orders")
	}
}

func TestWrapShortCircuitSkipsAppHandler(t *testing.T) {
	called := false
	mux := http.NewServeMux()
	mux.HandleFunc("/forbidden/thing", func(http.ResponseWriter, *http.Request) {
		called = true
	})
	handler, err := buildHandler(Options{Mux: mux, Wrap: markerWrap})
	if err != nil {
		t.Fatalf("buildHandler: %v", err)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/forbidden/thing", nil))
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
	if called {
		t.Error("app handler ran despite middleware short-circuit")
	}
}

func TestWrapNeverTouchesFrameworkChrome(t *testing.T) {
	handler, err := buildHandler(Options{Mux: http.NewServeMux(), Wrap: markerWrap})
	if err != nil {
		t.Fatalf("buildHandler: %v", err)
	}
	for _, path := range []string{"/healthz", "/api/version"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Header().Get("X-Wrapped") == "yes" {
			t.Errorf("%s traversed app middleware; platform probes must not", path)
		}
		if rec.Code != http.StatusOK {
			t.Errorf("%s status = %d, want 200", path, rec.Code)
		}
	}
}

func TestWrapReturningNilIsABootError(t *testing.T) {
	_, err := buildHandler(Options{
		Mux:  http.NewServeMux(),
		Wrap: func(http.Handler) http.Handler { return nil },
	})
	if err == nil || !strings.Contains(err.Error(), "Options.Wrap returned a nil handler") {
		t.Errorf("err = %v, want nil-handler boot error", err)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test -run TestWrap ./...`
Expected: compile error — `Options` has no field `Wrap` (that IS the red state; all four tests are red for the same missing field).

- [ ] **Step 3: Implement.** In `serve.go`, add to `Options` (after the `Router` field, before `DBPath`):

```go
	// Wrap, if set, wraps the app's mux — the one seam for app
	// middleware: sessions, CSRF, panic pages, authorization
	// (gleester's friction, James 2026-08-04). It runs inside the
	// framework's chrome: GET /healthz and GET /api/version are
	// answered outside it (platform probes never traverse app
	// middleware), and locale-prefix stripping happens before it,
	// so middleware sees the same paths routes match on. Nil means
	// no wrapping. Returning nil is a boot error.
	Wrap func(http.Handler) http.Handler
```

In `buildHandler`, replace `mux.Handle("/", opts.Mux)` with:

```go
	app := http.Handler(opts.Mux)
	if opts.Wrap != nil {
		if app = opts.Wrap(opts.Mux); app == nil {
			return nil, errors.New("rastrillo: Options.Wrap returned a nil handler")
		}
	}
	mux.Handle("/", app)
```

(`errors` is already imported in serve.go.) Update `buildHandler`'s doc comment: after "the app mux", add "— wrapped by Options.Wrap when set —".

- [ ] **Step 4: Run the full sweeps**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: all pass, including the four new tests and the untouched existing suite.

- [ ] **Step 5: Commit**

```bash
git add serve.go wrap_test.go
git commit -m "Options.Wrap: the app-middleware seam (gleester friction) 🤖

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 2: composition tests — Wrap with Router, Wrap inside locale stripping

**Files:**
- Modify: `wrap_test.go` (append)

**Interfaces:**
- Consumes: `Options.Wrap` from Task 1; `buildMux(opts, db)` (serve.go, resolves Mux/Router); `Serve`'s wiring order (`opts.Mux, _ = buildMux(...)` then `buildHandler(opts)`); `T(r, key)` request-scoped translation; `markerWrap` from Task 1's `wrap_test.go`.
- Produces: nothing new — proof only.

- [ ] **Step 1: Write the failing-or-passing tests** (they may pass immediately — that is fine; they pin the two contracts the spec promises):

```go
func TestWrapComposesWithRouter(t *testing.T) {
	opts := Options{
		Router: func(db *sql.DB) (*http.ServeMux, error) {
			mux := http.NewServeMux()
			mux.HandleFunc("/orders", func(w http.ResponseWriter, _ *http.Request) {
				w.Write([]byte("via router"))
			})
			return mux, nil
		},
		Wrap: markerWrap,
	}
	// Serve's own order: resolve the mux, then assemble the handler.
	var err error
	opts.Mux, err = buildMux(opts, nil)
	if err != nil {
		t.Fatalf("buildMux: %v", err)
	}
	handler, err := buildHandler(opts)
	if err != nil {
		t.Fatalf("buildHandler: %v", err)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/orders", nil))
	if rec.Header().Get("X-Wrapped") != "yes" || rec.Body.String() != "via router" {
		t.Errorf("marker=%q body=%q; Wrap and Router must be orthogonal",
			rec.Header().Get("X-Wrapped"), rec.Body.String())
	}
}

func TestWrapRunsInsideLocaleStripping(t *testing.T) {
	fsys := fstest.MapFS{
		"locales/en.toml": {Data: []byte("greet = \"hello\"\n")},
		"locales/fr.toml": {Data: []byte("greet = \"bonjour\"\n")},
	}
	var sawPath, sawGreet string
	spy := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sawPath = r.URL.Path
			sawGreet = T(r, "greet")
			next.ServeHTTP(w, r)
		})
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/orders", func(http.ResponseWriter, *http.Request) {})
	handler, err := buildHandler(Options{
		Mux: mux, Wrap: spy,
		Locales: []string{"en", "fr"}, LocaleFS: fsys,
	})
	if err != nil {
		t.Fatalf("buildHandler: %v", err)
	}
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/fr/orders", nil))
	if sawPath != "/orders" {
		t.Errorf("middleware saw path %q, want %q (stripped)", sawPath, "/orders")
	}
	if sawGreet != "bonjour" {
		t.Errorf("middleware saw T(greet)=%q, want %q (translator must already ride the context)", sawGreet, "bonjour")
	}
}
```

Add `"database/sql"` and `"testing/fstest"` to `wrap_test.go`'s imports.

- [ ] **Step 2: Run**

Run: `go test -run TestWrap ./...`
Expected: PASS (Task 1's implementation already provides both properties; these tests pin them).

- [ ] **Step 3: Full sweeps**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: all pass.

- [ ] **Step 4: Commit**

```bash
git add wrap_test.go
git commit -m "wrap: pin Router orthogonality and inside-locale ordering 🤖

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 3: README — the seam in the Serve bullet

**Files:**
- Modify: `README.md` (the `rastrillo.Serve` bullet, ~line 55-60 near "`rastrillo.OpenDB` is the same corrected opener exported for tests.")

**Interfaces:**
- Consumes: the shipped `Options.Wrap` semantics from Task 1.

- [ ] **Step 1: Add one sentence** to the `rastrillo.Serve` bullet, directly after the sentence ending "`rastrillo.OpenDB` is the same corrected opener exported for tests.":

```markdown
  `Options.Wrap` is the app-middleware seam: it wraps the app's mux
  (sessions, CSRF, panic pages, authorization) inside the framework's
  chrome — `/healthz`, `/api/version`, and locale-prefix stripping
  stay outside it, so probes never traverse app middleware and
  middleware sees the same paths routes match on.
```

- [ ] **Step 2: Sweeps** (README-only, but the rule is every commit)

Run: `go build ./... && go vet ./... && go test ./...`
Expected: all pass.

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "README: document the Options.Wrap middleware seam 🤖

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 4: Scope/viewer design-questions doc

**Files:**
- Create: `docs/superpowers/specs/2026-08-05-scope-and-viewer-design-questions.md`

**Interfaces:**
- Consumes: nothing from earlier tasks (doc-only; parallel-safe).
- Produces: the pre-design doc the Scope/auth issue links to.

**Note for the dispatcher:** this task is judgment-heavy prose, not
mechanical transcription — dispatch at high capability or write it in
the orchestrator session.

- [ ] **Step 1: Write the doc** with exactly these sections, in this order, each 100-250 words, no decisions recorded anywhere in it:

1. **Header** — Date 2026-08-05; Status: "questions only — nothing here is decided"; Origin: gleester's finding quoted verbatim: "a wish list isn't 'the rows in wishes', it's 'the rows this viewer may see', and the app exists to enforce that. Generated CRUD would leak exactly what the app hides."
2. **What gleester needs concretely** — every list/show/edit/delete read scoped to a viewer; unscoped generated CRUD is not a missing feature but an information leak; therefore scoping must be impossible to forget once declared, not a convention.
3. **The dependency: where does the viewer come from?** — rastrillo has no identity story (friction F7, deferred to a design cycle). Scope's signature depends on it: a `viewer_id` only exists after sessions+auth exist. Question: does Scope land with auth in one cycle, or can Scope be designed against an abstract `Viewer(r *http.Request) (string, bool)` the app supplies, letting auth arrive later?
4. **Three candidate mechanics for threading scope through generated sqlc queries** — (a) column convention: every scoped resource carries `viewer_id`; generated queries append `WHERE viewer_id = ?`; simplest, but wrong for shared/many-to-many visibility like wish lists. (b) generated WHERE-fragment hook: manifest declares `scope = "..."` SQL joined into every query; expressive, but SQL-in-TOML and sqlc's static analysis limit it. (c) query ejection per resource: generation emits the queries, the app ejects and rewrites the scoped ones; works today, but scoping-by-hand is exactly the forgettable convention section 2 forbids. Each listed with its failure mode; none endorsed.
5. **Questions for the design cycle** — numbered, including at minimum: one Scope shape or per-action shapes? What does the generated 404-vs-403 story look like (leaking existence)? Does `gen/manifest.json` carry scope declarations (additive-only contract)? What do Eleven/Keymail/Woodstar do today by hand that this must match?
6. **Input we want from James** — which candidate fits gleester's wish-list sharing model, and what his hand-rolled authorization layer's call sites actually look like.

- [ ] **Step 2: Self-check** — the doc records zero decisions (grep it for "we will", "must be", "decided": section 2's "must" statements describe the *requirement*, not a chosen mechanism — that one is fine); every section from the list above is present.

- [ ] **Step 3: Commit**

```bash
git add docs/superpowers/specs/2026-08-05-scope-and-viewer-design-questions.md
git commit -m "docs: Scope/viewer pre-design questions (gleester finding 2) 🤖

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```
