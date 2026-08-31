# F4 DB Hand-Back Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close friction-log F4 (examples/blog/README.md): apps that put the DB in `Ctx` can now receive the framework-opened `*sql.DB` instead of hand-copying the pragma DSN.

**Architecture:** Two additive API pieces. (1) `Options.Router func(*sql.DB) (*http.ServeMux, error)` — when set, `Serve` opens `DBPath` first (pragmas, eager Ping, migrations) and calls `Router` with the handle; the returned mux serves. Exactly one of `Mux`/`Router` must be set. (2) `rastrillo.OpenDB(path, migrations)` — the existing unexported `openDB`, exported, so tests and non-Serve contexts get the corrected opener instead of reproducing the DSN. The blog then drops its hand-copied DSN entirely and goes back to plain `Run`.

**Tech Stack:** Go stdlib + modernc.org/sqlite (already dependencies). No new dependencies.

## Global Constraints

- All changes are ADDITIVE — `Options.Mux`-only apps must work unchanged (wire-shape rule from the parent platform repo).
- `gofmt -l` clean on every touched file; `go build ./... && go vet ./... && go test ./... -count=1` clean in the root module AND in `examples/blog` AND in `examples/helloworld` before every commit.
- Sandbox: go commands need `GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod`; retry with sandbox disabled only on "read-only file system" errors.
- Comment style: match the repo — comments state constraints and reasons, never narrate the diff.

---

### Task 1: `Options.Router` + exported `OpenDB` in the framework

**Files:**
- Modify: `serve.go` (Options struct ~line 39-83; Serve ~line 89-142; openDB ~line 199)
- Test: `serve_router_test.go` (create)

**Interfaces:**
- Produces: `Options.Router func(db *sql.DB) (*http.ServeMux, error)` field; `func OpenDB(path string, migrations []string) (*sql.DB, error)`; unexported `func buildMux(opts Options, db *sql.DB) (*http.ServeMux, error)`. Task 2 relies on all three names exactly.

- [ ] **Step 1: Write the failing tests** — create `serve_router_test.go`:

```go
package rastrillo

import (
	"database/sql"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// Exactly one of Mux and Router: Serve must refuse both-set and
// neither-set before it touches a listener or a database.
func TestServeRequiresExactlyOneOfMuxAndRouter(t *testing.T) {
	both := Options{
		Mux:    http.NewServeMux(),
		Router: func(*sql.DB) (*http.ServeMux, error) { return http.NewServeMux(), nil },
	}
	if err := Serve(both); err == nil {
		t.Error("Serve accepted both Mux and Router")
	}
	if err := Serve(Options{}); err == nil {
		t.Error("Serve accepted neither Mux nor Router")
	}
}

func TestBuildMuxCallsRouterWithTheHandle(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "x.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var got *sql.DB
	mux, err := buildMux(Options{Router: func(d *sql.DB) (*http.ServeMux, error) {
		got = d
		return http.NewServeMux(), nil
	}}, db)
	if err != nil {
		t.Fatal(err)
	}
	if mux == nil {
		t.Fatal("buildMux returned a nil mux")
	}
	if got != db {
		t.Error("Router did not receive the opened handle")
	}
}

func TestBuildMuxPropagatesRouterError(t *testing.T) {
	boom := errors.New("boom")
	_, err := buildMux(Options{Router: func(*sql.DB) (*http.ServeMux, error) {
		return nil, boom
	}}, nil)
	if err == nil || !errors.Is(err, boom) {
		t.Errorf("want the Router error wrapped, got %v", err)
	}
}

func TestBuildMuxRefusesANilMuxFromRouter(t *testing.T) {
	_, err := buildMux(Options{Router: func(*sql.DB) (*http.ServeMux, error) {
		return nil, nil
	}}, nil)
	if err == nil {
		t.Error("buildMux accepted a nil mux with a nil error")
	}
}

func TestBuildMuxPassesThroughAPlainMux(t *testing.T) {
	m := http.NewServeMux()
	mux, err := buildMux(Options{Mux: m}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if mux != m {
		t.Error("buildMux did not return Options.Mux unchanged")
	}
}

// OpenDB is openDB exported: same pragmas, same eager materialization,
// same idempotent migrations. The file-exists check is the hibernate
// contract (the activator replicates the path from boot).
func TestOpenDBMaterializesAndMigrates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.db")
	db, err := OpenDB(path, []string{
		`CREATE TABLE IF NOT EXISTS t (id INTEGER PRIMARY KEY, name TEXT)`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("database file not materialized at boot: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO t (name) VALUES ('a')`); err != nil {
		t.Errorf("migrated table not usable: %v", err)
	}
	db.Close()

	// Re-open: additive migrations must be idempotent.
	again, err := OpenDB(path, []string{
		`CREATE TABLE IF NOT EXISTS t (id INTEGER PRIMARY KEY, name TEXT)`,
		`ALTER TABLE t ADD COLUMN name TEXT`, // duplicate column: tolerated
	})
	if err != nil {
		t.Fatalf("re-open with idempotent migrations failed: %v", err)
	}
	again.Close()
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod go test -run 'TestServeRequires|TestBuildMux|TestOpenDB' . -count=1`
Expected: FAIL — `undefined: OpenDB`, `unknown field Router`, `undefined: buildMux`.

- [ ] **Step 3: Implement.** In `serve.go`:

(a) Add the field to `Options`, directly after the `Mux` field:

```go
	// Router, if set, builds the app's mux after the database opens:
	// Serve calls it with the *sql.DB opened from DBPath — pragmas,
	// eager ping, and Migrations already applied — and serves the mux
	// it returns. This is how an app puts the framework-opened handle
	// in its per-request Ctx without hand-copying the DSN (the blog's
	// friction log, F4):
	//
	//	Router: func(db *sql.DB) (*http.ServeMux, error) {
	//		return gen.Router(func(*http.Request) *rastrillo.Ctx {
	//			return &rastrillo.Ctx{DB: db, Logger: logger}
	//		}), nil
	//	},
	//
	// Exactly one of Mux and Router must be set. With DBPath empty,
	// Router is called with a nil db — an app without a database can
	// still defer its mux construction.
	Router func(db *sql.DB) (*http.ServeMux, error)
```

(b) In `Serve`, replace the `if opts.Mux == nil` guard and insert the `buildMux` call after the database opens (the handler must be built from the resolved mux):

```go
	if (opts.Mux == nil) == (opts.Router == nil) {
		return errors.New("rastrillo: exactly one of Options.Mux and Options.Router must be set")
	}
```

and after the `openDB` block (keep `defer db.Close()` where it is):

```go
	opts.Mux, err = buildMux(opts, db)
	if err != nil {
		return err
	}
```

Note `err` is first declared by the openDB block's `var err error`; hoist a single `var err error` above the db block if the compiler complains — keep one declaration, no shadowing.

(c) Add `buildMux` after `buildHandler`:

```go
// buildMux resolves the Mux/Router choice. Router runs after the
// database opens so the app can close over the framework-opened handle
// — the entire point of the seam (F4).
func buildMux(opts Options, db *sql.DB) (*http.ServeMux, error) {
	if opts.Router == nil {
		return opts.Mux, nil
	}
	mux, err := opts.Router(db)
	if err != nil {
		return nil, fmt.Errorf("rastrillo: build router: %w", err)
	}
	if mux == nil {
		return nil, errors.New("rastrillo: Options.Router returned a nil mux")
	}
	return mux, nil
}
```

(d) Export the opener. Rename `openDB` to `OpenDB`, update its doc comment to start `// OpenDB applies the SQLite convention ...` and append one sentence: `// Exported so tests and non-Serve contexts get the corrected opener instead of reproducing the DSN by hand (the blog's F4).` Update the one call site in `Serve` (`db, err = OpenDB(...)`). Grep for any other `openDB` references (doc comments in `serve.go` mention it — update them).

- [ ] **Step 4: Run tests to verify they pass**

Run: `GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod go test . -count=1` (whole root package — the existing suite must stay green too)
Expected: PASS.

- [ ] **Step 5: Root sweep and commit**

Run: `GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod go build ./... && go vet ./... && go test ./... -count=1` and `gofmt -l .`
Expected: all clean.

```bash
git add serve.go serve_router_test.go
git commit -m "serve: Options.Router hands the opened DB to the app; export OpenDB (F4)"
```

---

### Task 2: Blog drops the hand-copied DSN

**Files:**
- Modify: `examples/blog/internal/blog/store.go` (the `migration` const ~line 20 and `Open` ~line 48)
- Modify: `examples/blog/cmd/blog/main.go` (whole `main` function)
- Modify: `examples/blog/README.md` (F4 friction-log entry, ~line 130-145 — find `**F4 —`)
- Modify: `run.go` (Resolve doc comment ~line 40-55)
- Modify: `README.md` (root; the `rastrillo.Serve` bullet and the Resolve sentence in the `rastrillo.Run` bullet)

**Interfaces:**
- Consumes: `rastrillo.OpenDB`, `Options.Router` from Task 1 (exact signatures there).
- Produces: `blog.Migration` (exported const, the schema SQL); `blog.Open` retained as a thin wrapper for the tests.

- [ ] **Step 1: store.go.** Rename the `migration` const to `Migration` (exported; doc: `// Migration is the app's whole schema: one additive, idempotent statement. main.go hands it to rastrillo via Options.Migrations; Open applies it for tests.`). Replace `Open` with:

```go
// Open opens the app's SQLite database with rastrillo's corrected
// opener and applies the migration. The serving path doesn't use this —
// main.go lets Serve open the database and hand the *sql.DB back via
// Options.Router — but the tests still want a one-call migrated handle.
func Open(path string) (*sql.DB, error) {
	return rastrillo.OpenDB(path, []string{Migration})
}
```

Add `"amadan.net/rastrillo/rastrillo"` to imports; drop the now-unused `_ "modernc.org/sqlite"` blank import (OpenDB's package pulls the driver) and remove the stale `// the app opens its own handle; see Open` comment. Grep `store.go` for other uses of `migration` (lowercase) and update.

- [ ] **Step 2: main.go.** Replace the whole file body after the package doc comment (keep the doc comment as-is) with:

```go
package main

import (
	"database/sql"
	"log/slog"
	"net/http"
	"os"

	"amadan.net/rastrillo/rastrillo"

	"blog/gen"
	"blog/internal/blog"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	// Run end to end: rastrillo resolves the activation argv, opens the
	// database (pragmas, eager ping, the schema migration), and hands
	// the *sql.DB back through Router — the F4 seam. No hand-copied
	// DSN, no Resolve dance, no double-open to avoid.
	err := rastrillo.Run(rastrillo.Options{
		DBPath:     "blog.db",
		Migrations: []string{blog.Migration},
		Logger:     logger,
		Router: func(db *sql.DB) (*http.ServeMux, error) {
			// A fresh Ctx per request. Actor.Human is true and
			// Actor.Name empty: honest for an app with no auth, and
			// the one line a real deployment would replace with a
			// session lookup.
			mux := gen.Router(func(*http.Request) *rastrillo.Ctx {
				return &rastrillo.Ctx{DB: db, Logger: logger, Actor: rastrillo.Actor{Human: true}}
			})

			// The app serves its own static files — the framework
			// never does. "GET /static/" is a longer pattern than
			// "GET /", so the stdlib mux prefers it and no ordering
			// care is needed.
			mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
			return mux, nil
		},
	})
	if err != nil {
		logger.Error("serve failed", "err", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 3: Blog sweep, then boot smoke**

Run in `examples/blog`: `GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod go build ./... && go vet ./... && go test ./... -count=1`
Expected: all clean (blogtest's `blog.Open` call sites still compile — Open remains).

Boot smoke: `go build -o /tmp/claude-1001/blogbin2 ./cmd/blog && cd examples/blog && /tmp/claude-1001/blogbin2 -addr 127.0.0.1:8197 -db /tmp/claude-1001/f4smoke.db & sleep 1; curl -sf http://127.0.0.1:8197/healthz && curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:8197/admin/posts && ls -la /tmp/claude-1001/f4smoke.db; kill %1`
Expected: `ok`, `200`, and the db file exists (Serve's eager Ping materializes it again — the duty moved back to the framework).

- [ ] **Step 4: Docs.**

(a) `examples/blog/README.md`, F4 entry: after the existing `*Eased, not fixed:*` paragraph, append:

```markdown
*Fixed:* `Options.Router` now receives the `*sql.DB` that `Serve`
opened — pragmas, eager ping, and `Options.Migrations` applied — so
`cmd/blog/main.go` is back to plain `Run` and the hand-copied DSN is
gone. `rastrillo.OpenDB` is the same opener exported, which is what
`blog.Open` (kept for the tests) now wraps.
```

(b) `run.go`, Resolve doc: replace the sentence beginning `The motivating case is an app that opens its own database handle` through the end of that paragraph (ending `...before Serve is called.`) with:

```
// The original motivating case — an app that opens its own database
// before building its mux — is better served by Options.Router now,
// which hands back the *sql.DB Serve opened; Resolve remains for apps
// that need the resolved paths themselves. If you do open your own
// handle and blank DBPath, the boot-materialization duty transfers
// with it: touch the driver (a Ping, or a migration) before Serve, or
// a hibernate route's activator replicates a file that does not exist.
```

(c) Root `README.md`: in the `rastrillo.Serve` bullet, after the sentence about the activation contract, add: `` An app that keeps its database in `Ctx` sets `Options.Router` instead of `Options.Mux` and is handed the `*sql.DB` Serve opened; `rastrillo.OpenDB` is the same corrected opener exported for tests. `` In the `rastrillo.Run` bullet, the Resolve sentence stays (still true).

- [ ] **Step 5: Full sweep everywhere and commit**

Run: root `go build ./... && go vet ./... && go test ./... -count=1 && gofmt -l .`; same in `examples/blog`; `go build ./... && go vet ./...` in `examples/helloworld`.
Expected: all clean.

```bash
git add examples/blog run.go README.md
git commit -m "blog: Options.Router replaces the hand-copied DSN — F4 fixed"
```
