# Known-Libraries Middle Layer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace Rastrillo's stamped-helper codegen middle with runtime packages built on known libraries (GORM, chi, dbresolver) plus a sessions/CSRF/flash core extracted from `auth/`, so a multi-user app is ~150 lines of domain code.

**Architecture:** Bottom platform layer (Run/Serve/OpenDB) untouched. New primitives: an owned GORM dialector over modernc (`gormlite/`), a pooled GORM opener (`db/`), `sessions/` + `csrf/` + `flash/` extracted/generalized from `auth/`, thin `form/`/`view/`/`scope/` helpers. `auth/` becomes the keymail identity plugin on the sessions core; `password/` is the second plugin. The manifest emitter is retargeted to compose `form`/`view` instead of stamping private copies. `examples/notes` (accounts + owner-scoped CRUD) is the new front-door example; `SKILL.md` is a deliverable.

**Tech Stack:** Go 1.25, modernc.org/sqlite v1.55.0, gorm.io/gorm v1.31.x, gorm.io/plugin/dbresolver v1.6.x, github.com/go-chi/chi/v5 v5.3.x, stdlib `crypto/pbkdf2`.

**Spec:** `docs/superpowers/specs/2026-08-21-known-libraries-middle-layer-design.md`

## Global Constraints

- `CGO_ENABLED=0 go build ./...` must stay green in the root module and both example modules — the static-binary property is load-bearing (spec §12.3).
- Allowed new dependencies, exactly: `gorm.io/gorm`, `gorm.io/plugin/dbresolver`, `github.com/go-chi/chi/v5`. **Never** `github.com/glebarez/*` (its `go-sqlite` registers driver name `"sqlite"`, clashing with modernc's registration → init panic; that is why `gormlite/` exists) and **never** `gorm.io/driver/sqlite` (cgo).
- `modernc.org/sqlite` stays at v1.55.0. Do not let any step downgrade it.
- Migrations are additive-only and idempotent (`CREATE TABLE IF NOT EXISTS`, `INSERT OR IGNORE`); never drop or rewrite an existing table.
- Doc comments explain constraints and provenance, matching house style (see `serve.go`); commit messages use the repo's `pkg: lowercase summary` style, ending with the Claude co-author trailer.
- Never merge to main; all work stays on the current branch (`criticism`), PR at the end.
- Scratch/temp files go to the session scratchpad, never the repo.
- Deviations from spec already agreed in design review: CSRF is the family-proven **origin check** (extracted from `auth/csrf.go`), not token-based — example tests assert cross-origin rejection instead of scraping tokens; `flash/` is cookie-based and depends on nothing; the password plugin renders through app callbacks ("the signin page stays the app's", same philosophy as keymail).

---

### Task 1: `gormlite/` — owned GORM dialector over modernc

**Files:**
- Create: `gormlite/sqlite.go`, `gormlite/ddlmod.go`, `gormlite/migrator.go`, `gormlite/errors.go`, `gormlite/LICENSE` (copied from upstream, MIT)
- Create: `gormlite/ddlmod_test.go`, `gormlite/sqlite_test.go`, `gormlite/noclash_test.go`
- Modify: `go.mod` (add gorm.io/gorm, gorm.io/plugin/dbresolver, github.com/go-chi/chi/v5)

**Interfaces:**
- Consumes: `modernc.org/sqlite` (already a dependency; registers driver `"sqlite"`).
- Produces: `package gormlite` with `func Open(dsn string) gorm.Dialector` and `type Dialector struct { DriverName, DSN string; Conn gorm.ConnPool }` (carried verbatim from the fork). Task 2 uses `gormlite.Dialector{Conn: pool}`.

- [ ] **Step 1: Add the dependencies**

```bash
go get gorm.io/gorm@v1.31.2 gorm.io/plugin/dbresolver@v1.6.2 github.com/go-chi/chi/v5@v5.3.2
```

Then confirm `grep modernc.org/sqlite go.mod` still says v1.55.0.

- [ ] **Step 2: Vendor the fork source**

```bash
git clone --depth 1 --branch v1.11.0 https://github.com/glebarez/sqlite "$SCRATCHPAD/glebarez-sqlite"
mkdir gormlite
cp "$SCRATCHPAD/glebarez-sqlite"/{sqlite.go,ddlmod.go,migrator.go,errors.go,LICENSE} gormlite/
cp "$SCRATCHPAD/glebarez-sqlite"/{ddlmod_test.go,sqlite_test.go} gormlite/
```

- [ ] **Step 3: Adapt the package**

In every copied `.go` file: change `package sqlite` → `package gormlite`. In `gormlite/sqlite.go` and `gormlite/sqlite_test.go`: change the import `gosqlite "github.com/glebarez/go-sqlite"` → `gosqlite "modernc.org/sqlite"`. The only code reference is the error-translation type switch (`case *gosqlite.Error:` around upstream line 240); modernc's `*sqlite.Error` also has `.Code() int`, so it compiles unchanged — if any other identifier differs, adapt it minimally and note it in the doc comment. Add a provenance header to `sqlite.go`:

```go
// Package gormlite is a GORM SQLite dialector over modernc.org/sqlite.
//
// It is a minimal fork of github.com/glebarez/sqlite v1.11.0 (MIT, see
// LICENSE in this directory) with one change: the driver import points
// at modernc.org/sqlite directly instead of glebarez/go-sqlite.
// glebarez pins a 2023-vintage repackage of modernc and registers the
// same driver name "sqlite" modernc registers, so a binary importing
// both panics at init — this fork keeps Rastrillo on current modernc
// (v1.55.0) with one driver registration.
package gormlite
```

- [ ] **Step 4: Run the carried tests, expect fail-or-pass honestly**

```bash
CGO_ENABLED=0 go test ./gormlite/
```

Expected: PASS. If a carried test depends on a glebarez-specific error string, fix the test's expectation to modernc's actual value (run once to see it) and comment why.

- [ ] **Step 5: Write the no-clash regression test**

`gormlite/noclash_test.go` — this is the whole reason the fork exists, so it gets a named guard:

```go
package gormlite

import (
	"testing"

	_ "modernc.org/sqlite" // must coexist with this package's driver use
	"gorm.io/gorm"
)

// TestNoDriverClash proves a binary can import modernc.org/sqlite (as
// OpenDB does) and this dialector together. With glebarez/sqlite this
// exact arrangement panics at init: sql: Register called twice for
// driver sqlite.
func TestNoDriverClash(t *testing.T) {
	g, err := gorm.Open(Open("file:noclash?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("gorm.Open: %v", err)
	}
	var one int
	if err := g.Raw("SELECT 1").Scan(&one).Error; err != nil || one != 1 {
		t.Fatalf("SELECT 1 = %d, %v", one, err)
	}
}
```

- [ ] **Step 6: Run all gormlite tests + vet**

```bash
CGO_ENABLED=0 go test ./gormlite/ && go vet ./gormlite/
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add go.mod go.sum gormlite/
git commit -m "gormlite: GORM dialector over modernc (glebarez/sqlite fork)"
```

---

### Task 2: `db/` — pooled GORM opener

**Files:**
- Create: `db/db.go`, `db/db_test.go`

**Interfaces:**
- Consumes: `gormlite.Dialector{Conn: ...}` (Task 1), `gorm.io/plugin/dbresolver`.
- Produces: `type DB struct { G *gorm.DB }` (unexported writer/reader fields), `func Open(path string, log *slog.Logger) (*DB, error)`, `func (d *DB) Close() error`. Tasks 10 and 12 consume `*db.DB` and `d.G`.

- [ ] **Step 1: Write the failing tests**

`db/db_test.go`:

```go
package db

import (
	"path/filepath"
	"testing"
)

func TestOpenWALAndPing(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "app.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	var jm string
	if err := d.G.Raw("PRAGMA journal_mode").Scan(&jm).Error; err != nil {
		t.Fatal(err)
	}
	if jm != "wal" {
		t.Fatalf("journal_mode = %q, want wal", jm)
	}
}

// TestReadDuringOpenRows is the single-connection-deadlock regression:
// with one shared connection, an open *sql.Rows plus any second query
// hangs forever. The reader pool must allow a second concurrent read.
func TestReadDuringOpenRows(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "app.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := d.G.Exec("CREATE TABLE t (n INTEGER)").Error; err != nil {
		t.Fatal(err)
	}
	if err := d.G.Exec("INSERT INTO t VALUES (1), (2)").Error; err != nil {
		t.Fatal(err)
	}
	rows, err := d.reader.Query("SELECT n FROM t")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	rows.Next() // hold the first result set open...
	var n int
	// ...and issue a second read; this must not hang.
	if err := d.G.Raw("SELECT count(*) FROM t").Scan(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("count = %d, want 2", n)
	}
}

func TestWriterSerialized(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "app.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if got := d.writer.Stats().MaxOpenConnections; got != 1 {
		t.Fatalf("writer MaxOpenConnections = %d, want 1", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
CGO_ENABLED=0 go test ./db/
```

Expected: FAIL — package does not exist.

- [ ] **Step 3: Implement `db/db.go`**

```go
// Package db opens the application's SQLite database the way a CARLOS
// app needs it, exposed as one *gorm.DB.
//
// One file, two pools: writes go through a pool capped at one
// connection (SQLite allows one writer; queueing in the pool beats
// SQLITE_BUSY at the call site), reads through a pool of several (WAL
// supports many readers, and serialising them behind one connection
// turns an open *sql.Rows plus any second query into a silent
// deadlock). gorm.io/plugin/dbresolver routes each statement, so app
// code never picks a pool.
//
// The DSN keeps OpenDB's hard-won pragma order: busy_timeout before
// journal_mode=WAL (the reverse crashes under concurrent open), plus
// foreign_keys(1). The eager Ping keeps hibernation replication happy:
// the file exists on disk from boot.
package db

import (
	"database/sql"
	"fmt"
	"log/slog"
	"runtime"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/plugin/dbresolver"

	"amadan.net/rastrillo/rastrillo/gormlite"
)

type DB struct {
	G *gorm.DB

	writer *sql.DB
	reader *sql.DB
}

func Open(path string, log *slog.Logger) (*DB, error) {
	if log == nil {
		log = slog.Default()
	}
	dsn := path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"

	w, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	w.SetMaxOpenConns(1)
	if err := w.Ping(); err != nil {
		w.Close()
		return nil, fmt.Errorf("rastrillo/db: ping %s: %w", path, err)
	}

	r, err := sql.Open("sqlite", dsn)
	if err != nil {
		w.Close()
		return nil, err
	}
	readers := runtime.NumCPU()
	if readers < 4 {
		readers = 4
	}
	r.SetMaxOpenConns(readers)
	if err := r.Ping(); err != nil {
		w.Close()
		r.Close()
		return nil, fmt.Errorf("rastrillo/db: ping reader %s: %w", path, err)
	}

	g, err := gorm.Open(gormlite.Dialector{Conn: w}, &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
		NowFunc: func() time.Time { return time.Now().UTC() },
	})
	if err != nil {
		w.Close()
		r.Close()
		return nil, err
	}
	if err := g.Use(dbresolver.Register(dbresolver.Config{
		Replicas: []gorm.Dialector{gormlite.Dialector{Conn: r}},
	})); err != nil {
		w.Close()
		r.Close()
		return nil, err
	}
	return &DB{G: g, writer: w, reader: r}, nil
}

func (d *DB) Close() error {
	rerr := d.reader.Close()
	if werr := d.writer.Close(); werr != nil {
		return werr
	}
	return rerr
}
```

Note: the driver name `"sqlite"` is registered by `modernc.org/sqlite`, which arrives through gormlite's import — no blank import needed here (the compiler will tell you if that changes).

- [ ] **Step 4: Run tests to verify they pass**

```bash
CGO_ENABLED=0 go test ./db/ && go vet ./db/
```

Expected: PASS, and TestReadDuringOpenRows completes instantly (a hang here means dbresolver isn't routing reads to the reader pool — check the Replicas registration).

- [ ] **Step 5: Commit**

```bash
git add db/
git commit -m "db: pooled GORM opener (writer-1/reader-N via dbresolver)"
```

---

### Task 3: `sessions/` — the session core

**Files:**
- Create: `sessions/sessions.go`, `sessions/sessions_test.go`

**Interfaces:**
- Consumes: `database/sql` only (raw SQL like today's `auth/store.go` — the sessions table predates GORM apps and must serve non-GORM apps too).
- Produces (Tasks 8, 9, 12 consume these — exact signatures):

```go
type Session struct { Subject, Method string; AuthTime, At time.Time }
type Config struct { DB *sql.DB; Origin string; TTL time.Duration; SigninPath string; Logger *slog.Logger }
func New(cfg Config) (*Sessions, error)
var Migrations []string
func (s *Sessions) SignIn(w http.ResponseWriter, r *http.Request, sess Session) error
func (s *Sessions) SignOut(w http.ResponseWriter, r *http.Request)
func (s *Sessions) From(r *http.Request) (Session, bool)
func (s *Sessions) Middleware(next http.Handler) http.Handler
func (s *Sessions) Require(next http.Handler) http.Handler
func (s *Sessions) CookieName() string
func Current(r *http.Request) (Session, bool)
func UserID(r *http.Request) (int64, bool)
func SafeReturn(r *http.Request, fallback string) string
func NewToken() (token, hash string, err error)
func HashToken(token string) string
```

- [ ] **Step 1: Write the failing tests**

`sessions/sessions_test.go` (open the DB with `sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(),"s.db")+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")`, blank-import `modernc.org/sqlite`, apply `Migrations` with `db.Exec` in a helper). Test cases, each its own function:

```go
// newTestSessions is the shared helper: temp DB, Migrations applied,
// New(Config{DB: db, Origin: origin}). Origin defaults "http://app.test".

func TestSignInSetsCookieAndStoresHash(t *testing.T) {
	// SignIn via httptest.ResponseRecorder; assert a Set-Cookie for
	// CookieName() exists, HttpOnly, SameSite=Lax, Path=/; assert the
	// sessions table has exactly one row and its token_hash is NOT the
	// cookie value (only the hash is stored).
}

func TestFromRoundTrip(t *testing.T) {
	// SignIn with Session{Subject: "42", Method: "password"}; build a
	// request carrying the minted cookie; From must return the session
	// with Subject "42" and ok=true.
}

func TestSignOutRevokesServerSide(t *testing.T) {
	// SignIn, then SignOut with the cookie; a request still carrying
	// the old cookie must get ok=false from From (row deleted — real
	// revocation, not just a cleared cookie).
}

func TestExpiredSessionRejected(t *testing.T) {
	// New with TTL: time.Millisecond; SignIn; sleep 5ms; From → false.
}

func TestSignInRotatesToken(t *testing.T) {
	// SignIn once, capture cookie A. Build a request carrying A and
	// SignIn again (privilege change): capture cookie B. A != B, and a
	// request carrying A must now fail From — the old row is deleted.
}

func TestHostPrefixOnHTTPS(t *testing.T) {
	// Origin "https://app.example.com" → CookieName() ==
	// "__Host-rastrillo_session"; "http://localhost:8080" → plain
	// "rastrillo_session". (Same rule as auth today: the __Host-
	// prefix requires Secure.)
}

func TestRequireRedirectsWithReturnTo(t *testing.T) {
	// Require(next) with no cookie: GET /notes/7 → 303 to
	// "/signin?return_to=%2Fnotes%2F7". POST → 403, no redirect.
}

func TestRequireStashesCurrent(t *testing.T) {
	// With a valid cookie, the wrapped next sees Current(r) ok=true
	// and UserID(r) parses a numeric Subject.
}

func TestSafeReturnRejectsAbsoluteAndSchemeless(t *testing.T) {
	// Table test over form value "return_to":
	//  "/notes/7"                 → "/notes/7"
	//  "https://evil.example"     → fallback
	//  "//evil.example"           → fallback
	//  "/ok\\evil"                → fallback
	//  ""                         → fallback
	// This is the open-redirect guard: only a same-site absolute path
	// survives. (The bake-off's rastrillo arm shipped an open redirect
	// through exactly this hole.)
}
```

Write these as real tests (the comments above are the behavior contract; the bodies are yours to write following them exactly).

- [ ] **Step 2: Run tests to verify they fail**

```bash
CGO_ENABLED=0 go test ./sessions/
```

Expected: FAIL — package does not exist.

- [ ] **Step 3: Implement `sessions/sessions.go`**

Port the mechanics from `auth/auth.go` + `auth/store.go` (they are the proven code — lift, don't reinvent): `NewToken`/`HashToken` move here verbatim (auth re-exports later, Task 8). Table and store:

```go
// Package sessions maintains signed-in sessions: SQLite-backed rows
// (so sign-out and admin revocation are real — a deleted row is dead
// even if the cookie lives on), __Host- cookies on https origins, and
// the request-context surface (Current, UserID) the rest of an app
// reads. It deliberately does not know how a session is EARNED —
// identity plugins (auth's keymail flow, password) verify a credential
// and call SignIn; that one call is the whole plugin contract.
var Migrations = []string{
	`CREATE TABLE IF NOT EXISTS sessions (
	  token_hash TEXT PRIMARY KEY,
	  subject    TEXT NOT NULL,
	  method     TEXT NOT NULL DEFAULT '',
	  auth_time  TEXT NOT NULL DEFAULT '',
	  created_at TEXT NOT NULL,
	  expires_at TEXT NOT NULL
	);`,
}
```

Config validation in `New` (loud, matching house style): `DB` required; `Origin` must be absolute (`http://`/`https://` prefix — same check as `auth.New`); defaults `TTL: 30 * 24 * time.Hour`, `SigninPath: "/signin"`, `Logger: slog.Default()`. Cookie base name `"rastrillo_session"`, `__Host-` prefix iff origin is https (port `secure()`/`cookieName`/`setCookie`/`clearCookie` from `auth/auth.go`). Store methods are unexported (`create`, `lookup`, `revoke`) with the same RFC3339 formats as `auth/store.go` so the Task 8 copy-migration is column-compatible.

`SignIn`: if the request carries a valid session cookie, `revoke` its hash first (rotation); then `NewToken`, `create` row with `Subject`/`Method`/`AuthTime` (empty string when zero) and computed expiry, `setCookie`. `SignOut`: revoke presented hash (log lookup errors, don't fail the request), clear cookie. `From`: cookie → `lookup`, expiry-checked in SQL-side comparison exactly like `lookupSession` does today. `Middleware`: resolve once, stash via unexported ctx key, always call next. `Require`: no session → GET/HEAD 303 to `SigninPath + "?return_to=" + url.QueryEscape(r.URL.RequestURI())`, others 403 `"signed out"`; valid → stash + next. `Current`: ctx read. `UserID`: `Current` + `strconv.ParseInt(s.Subject, 10, 64)`. `SafeReturn`:

```go
// SafeReturn returns r.FormValue("return_to") when it is a same-site
// absolute path — starts with exactly one "/", no scheme, no
// backslash — and fallback otherwise. Anything laxer is an open
// redirect on a sign-in endpoint.
func SafeReturn(r *http.Request, fallback string) string {
	to := r.FormValue("return_to")
	if to == "" || !strings.HasPrefix(to, "/") ||
		strings.HasPrefix(to, "//") || strings.ContainsAny(to, "\\") {
		return fallback
	}
	return to
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
CGO_ENABLED=0 go test ./sessions/ && go vet ./sessions/
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add sessions/
git commit -m "sessions: SQLite-backed session core with pluggable identity seam"
```

---

### Task 4: `csrf/` — same-origin protection, app-wide

**Files:**
- Create: `csrf/csrf.go`, `csrf/csrf_test.go`
- Modify: `auth/csrf.go` (delegate), `auth/auth_test.go` only if an origin test references the unexported method directly

**Interfaces:**
- Consumes: nothing (stdlib only).
- Produces: `func SameOrigin(r *http.Request, origin string) bool`, `func Protect(origin string) func(http.Handler) http.Handler`. Task 12 mounts `Protect`; Task 8's auth keeps its behavior through `SameOrigin`.

- [ ] **Step 1: Write the failing tests**

`csrf/csrf_test.go` — table-driven over the four evidence tiers, ported from the doc comment in `auth/csrf.go` (Sec-Fetch-Site same-origin/none pass, cross-site fails; Origin exact match; Referer origin match; none-of-three refuse), plus `Protect` method gating:

```go
func TestSameOrigin(t *testing.T) {
	const origin = "https://app.example.com"
	cases := []struct {
		name    string
		headers map[string]string
		want    bool
	}{
		{"sec-fetch same-origin", map[string]string{"Sec-Fetch-Site": "same-origin"}, true},
		{"sec-fetch none (address bar)", map[string]string{"Sec-Fetch-Site": "none"}, true},
		{"sec-fetch cross-site", map[string]string{"Sec-Fetch-Site": "cross-site"}, false},
		{"origin exact", map[string]string{"Origin": origin}, true},
		{"origin mismatch", map[string]string{"Origin": "https://evil.example"}, false},
		{"referer same origin", map[string]string{"Referer": origin + "/form"}, true},
		{"referer other origin", map[string]string{"Referer": "https://evil.example/form"}, false},
		{"no evidence refused", map[string]string{}, false},
	}
	// build POST requests, apply headers, assert SameOrigin(r, origin) == want
}

func TestProtectGatesMutatingMethodsOnly(t *testing.T) {
	// GET with no headers passes through (200 from next).
	// POST with no headers → 403, next not called.
	// POST with Origin matching → 200.
	// Same for PUT, PATCH, DELETE (403 without evidence).
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
CGO_ENABLED=0 go test ./csrf/
```

Expected: FAIL — package does not exist.

- [ ] **Step 3: Implement**

Move the body of `(*Auth).sameOrigin` from `auth/csrf.go` into `csrf.SameOrigin(r, origin)` unchanged, keeping its full doc comment (including the vitogo history — that comment is why the package exists). Add:

```go
// Protect refuses state-changing cross-origin requests: POST, PUT,
// PATCH and DELETE must carry browser evidence of same-origin
// submission (see SameOrigin); GET/HEAD/OPTIONS pass untouched. This
// is the family's CSRF defense — origin-checking, not tokens: every
// current browser sends Sec-Fetch-Site or Origin on form POSTs, there
// is no token to mint, store, or forget to check, and it covers
// generated and hand-written handlers alike when mounted app-wide.
func Protect(origin string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
				if !SameOrigin(r, origin) {
					http.Error(w, "cross-origin form submission refused", http.StatusForbidden)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
```

Rewrite `auth/csrf.go` to a two-line delegate: `func (a *Auth) sameOrigin(r *http.Request) bool { return csrf.SameOrigin(r, a.cfg.Origin) }` with a pointer comment. Import path: `amadan.net/rastrillo/rastrillo/csrf`.

- [ ] **Step 4: Run tests to verify they pass — including auth's**

```bash
CGO_ENABLED=0 go test ./csrf/ ./auth/ && go vet ./csrf/ ./auth/
```

Expected: PASS (auth's existing origin-behavior tests prove the delegation preserved semantics).

- [ ] **Step 5: Commit**

```bash
git add csrf/ auth/csrf.go
git commit -m "csrf: extract the proven same-origin check as app-wide middleware"
```

---

### Task 5: `flash/` — one-shot notices

**Files:**
- Create: `flash/flash.go`, `flash/flash_test.go`

**Interfaces:**
- Consumes: nothing (stdlib only).
- Produces: `type Flash struct { Kind, Message string }`, `func Set(w http.ResponseWriter, kind, message string)`, `func Take(w http.ResponseWriter, r *http.Request) (Flash, bool)`. Task 12's layout template calls `Take` per request.

- [ ] **Step 1: Write the failing tests**

```go
func TestSetThenTake(t *testing.T) {
	// Set(w, "notice", "Note created.") on a recorder; carry the
	// Set-Cookie into a new request; Take returns
	// Flash{Kind: "notice", Message: "Note created."}, ok=true, and
	// writes a clearing Set-Cookie (MaxAge < 0) — one-shot.
}

func TestTakeWithoutFlash(t *testing.T) {
	// No cookie → ok=false, and no Set-Cookie written.
}

func TestTakeGarbageCookie(t *testing.T) {
	// Cookie value "not!base64" → ok=false, cookie still cleared.
}

func TestMessageSurvivesEncoding(t *testing.T) {
	// Message with spaces, unicode and a newline round-trips intact
	// (cookie values can't carry these raw — encoding is the point).
}
```

- [ ] **Step 2: Run to verify FAIL**, `CGO_ENABLED=0 go test ./flash/` — package does not exist.

- [ ] **Step 3: Implement `flash/flash.go`**

Cookie `"rastrillo_flash"`, value `base64.RawURLEncoding` of `kind + "\x00" + message`, `Path: "/"`, `HttpOnly: true`, `SameSite: Lax`, `MaxAge: 60` (a flash that isn't read within a minute is stale by definition). `Take`: read cookie; on any decode/split failure return `false` but still clear; on success clear (`MaxAge: -1`) and return the flash. Doc comment: cookie-based rather than DB-backed because a flash is display state, not a record — losing one to a cleared cookie costs a notice, not data.

- [ ] **Step 4: Run to verify PASS**, `CGO_ENABLED=0 go test ./flash/ && go vet ./flash/`.

- [ ] **Step 5: Commit** — `git add flash/ && git commit -m "flash: cookie-based one-shot notices"`

---

### Task 6: `form/` and `view/` — the helpers the emitter stamps today

**Files:**
- Create: `form/money.go`, `form/errors.go`, `form/money_test.go`
- Create: `view/view.go`, `view/view_test.go`

**Interfaces:**
- Consumes: `view` imports the root `rastrillo` package for `*rastrillo.Ctx` (no cycle: root does not import view).
- Produces: `form.ParseCents(s string) (int64, error)`, `form.FormatCents(cents int64) string`, `form.FormatCentsPlain(cents int64) string`, `type form.Errors map[string]string` with `func (e Errors) Any() bool`; `view.Render(ctx *rastrillo.Ctx, w http.ResponseWriter, page string, status int, data any)`, `view.Fail(ctx *rastrillo.Ctx, w http.ResponseWriter, what string, err error)`, `view.ParseID(r *http.Request) (int64, bool)`. Task 7's emitter and Task 12's handlers call all of these.

- [ ] **Step 1: Move the money helpers with their tests first**

The implementations move **verbatim** from the emitter's stamped block (source of truth: `internal/generate/actions.go` lines ~379 onward, or any generated copy like `examples/tickets/gen/actions/admin/ticket_types/index_post/index.POST.go:110-203`) — including every doc comment (the sign-mangling history, the resubmit round-trip rule, the strconv leniency guard). Only the names change: `parseCents` → `ParseCents`, etc.; `isDigits` stays unexported.

`form/money_test.go` — port the behavior the comments document into table tests:

```go
func TestParseCents(t *testing.T) {
	cases := []struct {
		in      string
		want    int64
		wantErr bool
	}{
		{"12.34", 1234, false},
		{"12", 1200, false},
		{"12.3", 1230, false},
		{".5", 50, false},
		{"", 0, false},          // blank = zero; Required rejects earlier on raw text
		{"12.345", 0, true},     // >2 decimal places
		{"-1.50", 0, true},      // sign refused outright (v1: no negative money)
		{"12.-5", 0, true},      // the strconv leniency hole, closed
		{"12.+5", 0, true},
		{"$12.34", 0, true},     // ParseCents rejects "$": FormatCentsPlain seeds forms
		{"abc", 0, true},
	}
	// ...
}

func TestFormatCentsNegative(t *testing.T) {
	// FormatCents(-150) == "-$1.50" (not "$-1.-50" — the truncation
	// mangling the stamped comment documents). FormatCents(150) == "$1.50".
	// FormatCentsPlain(150) == "1.50" and ParseCents(FormatCentsPlain(150))
	// round-trips to 150 — the resubmit rule.
}
```

- [ ] **Step 2: Run to verify FAIL**, `CGO_ENABLED=0 go test ./form/`.

- [ ] **Step 3: Implement `form/`** (moved code + `Errors` with `Any() bool` returning `len(e) > 0`). Package doc: these helpers used to be stamped privately into every generated action file; they live here once now, serving generated and hand-written handlers alike.

- [ ] **Step 4: Run to verify PASS**, then write `view/` the same way: failing tests first (`Render` with nil `ctx.Render` → 500 + logged line via a test slog handler; `Render` with a wired fake render fn receives page/status/data; `Fail` → 500 with body "Something went wrong." and logged what+err; `ParseID` table: "7"→7 ok, "0"→!ok, "-3"→!ok, "abc"→!ok, ""→!ok), then move the implementations from the stamped block (keep the 404-not-400 comment on ParseID and the nil-Render rationale on Render).

- [ ] **Step 5: Run both**, `CGO_ENABLED=0 go test ./form/ ./view/ && go vet ./form/ ./view/` — PASS.

- [ ] **Step 6: Commit**

```bash
git add form/ view/
git commit -m "form, view: the once-stamped helpers, now shared packages"
```

---

### Task 7: Retarget the manifest emitter onto `form`/`view`

**Files:**
- Modify: `internal/generate/actions.go` (delete the stamped helper block; emit imports + qualified calls)
- Modify: `internal/generate/actions_test.go` (golden expectations)
- Modify: `examples/tickets/gen/**` (regenerated output)

**Interfaces:**
- Consumes: `form.ParseCents`/`FormatCents`/`FormatCentsPlain`, `view.Render`/`Fail`/`ParseID` (Task 6 — exact names).
- Produces: generated action files ~50-70 lines each instead of ~160-320; the `Handle` bodies and route/store emission are byte-identical in behavior.

- [ ] **Step 1: Read before cutting**

Read `internal/generate/actions.go` fully. Identify: (a) the helper-block emission (the raw string starting at the comment around line 379 that stamps `fail`/`render`/`parseID`/`formatCents`/`formatCentsPlain`/`parseCents`/`isDigits`); (b) the import-list builder for emitted files; (c) `moneyFmt` (line ~269) which picks `"formatCents"` vs `"formatCentsPlain"` per context.

- [ ] **Step 2: Make the change**

- Delete the helper-block emission entirely.
- Add `"amadan.net/rastrillo/rastrillo/form"` and `"amadan.net/rastrillo/rastrillo/view"` to emitted imports **only when the emitted body references them** (an action with no money field must not import `form` unused — emitted code must pass vet).
- Rewrite emitted call sites: `fail(ctx, w, ...)` → `view.Fail(ctx, w, ...)` (the `what` string keeps its `"<resource>: "` prefix, now built into the argument by the emitter); `render(` → `view.Render(`; `parseID(` → `view.ParseID(`; `parseCents(` → `form.ParseCents(`; `moneyFmt` returns `"form.FormatCents"` / `"form.FormatCentsPlain"`.
- `errs := map[string]string{}` may stay as-is (churn-free) — do not switch generated code to `form.Errors` in this task.

- [ ] **Step 3: Run the emitter tests, update goldens deliberately**

```bash
go test ./internal/generate/ 2>&1 | head -50
```

Expected: FAIL with diffs showing exactly the deletion + qualified calls. Read each diff; update the expectations in `actions_test.go` to the new output only after confirming no `Handle`-body logic changed.

- [ ] **Step 4: Regenerate the tickets example and run its suite**

```bash
(cd examples/tickets && go run amadan.net/rastrillo/rastrillo/cmd generate && go test ./... && go vet ./...)
```

(If the CLI invocation differs, `examples/tickets/internal/ticketstest/generatecheck_test.go` shows the canonical command — mirror it.) Expected: regeneration rewrites `gen/actions/**`, tests PASS including the roundtrip/required/filter/delete suites. Run `wc -l examples/tickets/gen/actions/**/*.go` and record the before/after total in the commit message (expected: ~1,800 → ~600).

- [ ] **Step 5: Commit**

```bash
git add internal/generate/ examples/tickets/
git commit -m "generate: emit form/view calls instead of stamping private helpers"
```

---

### Task 8: `auth/` becomes the keymail identity plugin on the sessions core

**Files:**
- Modify: `auth/auth.go` (build a `*sessions.Sessions` in `New`; cookie/token fns delegate)
- Modify: `auth/store.go` (delete `createSession`/`lookupSession`/`deleteSession`; keep `linkStore`; add copy migration)
- Modify: `auth/handlers.go` (`admit`/`Signout`/`SessionFrom` via the core)
- Test: existing `auth/auth_test.go` (must pass unmodified except where it queried `auth_sessions` directly — those queries move to `sessions`)

**Interfaces:**
- Consumes: `sessions.New/Config`, `(*Sessions).SignIn/SignOut/From/CookieName`, `sessions.Session`, `sessions.NewToken/HashToken` (Task 3).
- Produces: unchanged public API — `auth.New(Config)`, `Begin/Callback/Verify/Signout`, `RequireSession`, `From`, `SessionFrom`, `SessionCookie()`, `auth.Migrations`, `NewToken`/`HashToken` (now thin aliases: `func NewToken() (string, string, error) { return sessions.NewToken() }`).

- [ ] **Step 1: Run the existing auth suite first** — `go test ./auth/` must be green before touching anything (it is the behavior contract for this refactor).

- [ ] **Step 2: Make the swap**

- `Auth` gains an unexported `sessions *sessions.Sessions`; `New` builds it from `Config`: `sessions.New(sessions.Config{DB: cfg.DB, Origin: cfg.Origin, TTL: cfg.SessionTTL, SigninPath: cfg.SigninPath, Logger: cfg.Logger})`.
- `admit` becomes: authorize gate (unchanged), then `a.sessions.SignIn(w, r, sessions.Session{Subject: id.Address, Method: string(id.Method), AuthTime: id.AuthTime})`, then redirect. Delete the hand-rolled token/cookie/store steps.
- `SessionFrom` maps `a.sessions.From(r)` → `Identity{Address: s.Subject, Method: signin.Method(s.Method), AuthTime: s.AuthTime, At: s.At}`. `RequireSession`'s redirect-vs-403 behavior stays exactly as written (it deliberately has no return_to today; keeping it byte-compatible for family apps beats consistency).
- `Signout` keeps the sameOrigin gate, then `a.sessions.SignOut(w, r)` + redirect.
- `SessionCookie()` returns `a.sessions.CookieName()`. Delete `secure`/`cookieName`/`setCookie`/`clearCookie` from auth **only if** nothing else in the package uses them (the pending cookie in `Begin`/`Callback` still needs them — keep exactly what the pending flow uses, delete the rest).
- `auth.Migrations` = links table + `sessions.Migrations...` + the upgrade copy (a family app upgrading must not sign everyone out):

```go
// Copy any live auth_sessions rows into the shared sessions table —
// additive and idempotent (OR IGNORE), so upgrading does not sign the
// family out. The old table stays, abandoned, per the additive-only rule.
`INSERT OR IGNORE INTO sessions (token_hash, subject, method, auth_time, created_at, expires_at)
   SELECT token_hash, address, method, auth_time, created_at, expires_at FROM auth_sessions;`,
```

Note ordering: the copy statement must come **after** `sessions.Migrations` in the slice, and `Sweep` now sweeps `sessions` (and still sweeps `auth_links`).

- [ ] **Step 3: Run tests to verify they pass**

```bash
CGO_ENABLED=0 go test ./auth/ ./sessions/ && go vet ./auth/
```

Expected: PASS. Any auth test that asserted against the `auth_sessions` table directly should now assert against `sessions` — change the table name in the test, nothing else.

- [ ] **Step 4: Commit**

```bash
git add auth/
git commit -m "auth: ride the sessions core; keymail becomes an identity plugin"
```

---

### Task 9: `password/` — the second identity plugin

**Files:**
- Create: `password/hash.go`, `password/handlers.go`, `password/hash_test.go`, `password/handlers_test.go`

**Interfaces:**
- Consumes: `sessions.SignIn`, `sessions.SafeReturn` (Task 3); stdlib `crypto/pbkdf2`, `crypto/sha256`, `crypto/rand`, `crypto/subtle`.
- Produces (Task 12 consumes):

```go
func Hash(password string) (string, error)        // "pbkdf2$sha256$600000$<hexsalt>$<hexdk>"
func Verify(encoded, password string) bool
type Config struct {
	Sessions     *sessions.Sessions
	Lookup       func(ctx context.Context, email string) (id int64, hash string, err error) // sql.ErrNoRows = unknown
	Create       func(ctx context.Context, email, hash string) (int64, error)               // nil disables signup
	SignedInPath string                                                                      // default "/"
	RenderSignin func(w http.ResponseWriter, r *http.Request, d PageData)
	RenderSignup func(w http.ResponseWriter, r *http.Request, d PageData)
}
type PageData struct { Error, Email, ReturnTo string }
func New(cfg Config) (*Handlers, error)
// Handlers methods, all http.HandlerFunc-shaped:
//   SigninPage, Signin, SignupPage, Signup, Signout
```

- [ ] **Step 1: Write the failing hash tests**

```go
func TestHashVerifyRoundTrip(t *testing.T)      // Verify(Hash("s3cret"), "s3cret") == true
func TestVerifyWrongPassword(t *testing.T)      // == false
func TestHashSaltsDiffer(t *testing.T)          // two Hash("x") calls produce different strings
func TestVerifyGarbageEncoded(t *testing.T)     // "", "nonsense", "pbkdf2$sha256$abc$xx$yy" all false, no panic
func TestParamsPinned(t *testing.T)             // Hash output HasPrefix "pbkdf2$sha256$600000$"
```

- [ ] **Step 2: Run to verify FAIL**, `CGO_ENABLED=0 go test ./password/`.

- [ ] **Step 3: Implement `password/hash.go`**

stdlib `crypto/pbkdf2` (Go 1.24+): `pbkdf2.Key(sha256.New, password, salt, 600_000, 32)`. 16-byte salt from `crypto/rand`. Compare with `subtle.ConstantTimeCompare`. Format `pbkdf2$sha256$600000$<hex salt>$<hex dk>` — parameters ride the string so they can be raised later without breaking stored hashes. Doc comment: PBKDF2-SHA256 at 600k iterations is the current OWASP floor; chosen over argon2 to stay stdlib-only.

Also add the package-level decoy for timing equalization:

```go
// decoyHash is verified against when Lookup finds no user, so an
// unknown email costs the same wall-clock as a wrong password —
// no enumeration oracle by timing.
var decoyHash, _ = Hash("rastrillo-password-decoy")
```

- [ ] **Step 4: Run hash tests to verify PASS.**

- [ ] **Step 5: Write the failing handler tests**

`password/handlers_test.go` builds a `Handlers` over an in-memory user map (fake `Lookup`/`Create`) and a real `*sessions.Sessions` on a temp DB; `Render*` callbacks record their `PageData`:

```go
func TestSigninSuccessMintsSession(t *testing.T)
	// POST email+password of a known user → 303 to SignedInPath,
	// Set-Cookie present; sessions.From on a follow-up request
	// resolves Subject == strconv.FormatInt(id, 10), Method "password".
func TestSigninHonorsReturnTo(t *testing.T)
	// form return_to=/notes/7 → 303 to /notes/7;
	// return_to=https://evil.example → 303 to SignedInPath (SafeReturn).
func TestSigninWrongPasswordRerenders(t *testing.T)
	// RenderSignin called with PageData{Error: "Wrong email or password.",
	// Email: submitted}, status 422 written by the handler, no cookie.
func TestSigninUnknownEmailSameMessage(t *testing.T)
	// Unknown email → byte-identical PageData.Error as wrong-password
	// (one message, no enumeration oracle).
func TestSignupCreatesAndSignsIn(t *testing.T)
	// POST /signup with fresh email → Create called with a Hash-format
	// hash (never plaintext), 303, session minted.
func TestSignupNilCreateDisabled(t *testing.T)
	// Config.Create nil → New still succeeds; Signup answers 404.
func TestSignoutRevokes(t *testing.T)
	// Signed-in cookie + POST Signout → cookie cleared, row revoked.
```

- [ ] **Step 6: Run to verify FAIL, then implement `password/handlers.go`**

`New` validates: `Sessions`, `Lookup`, `RenderSignin` required; default `SignedInPath: "/"`. `Signin`: `ParseForm`; `Lookup(ctx, strings.ToLower(strings.TrimSpace(email)))`; on `sql.ErrNoRows` → `Verify(decoyHash, password)` then fail; on found → `Verify(hash, password)`; failure re-renders via `RenderSignin` with **one** error string `"Wrong email or password."` at `http.StatusUnprocessableEntity`; success → `h.cfg.Sessions.SignIn(w, r, sessions.Session{Subject: strconv.FormatInt(id, 10), Method: "password", AuthTime: time.Now()})` then 303 to `sessions.SafeReturn(r, h.cfg.SignedInPath)`. `Signup`: 404 when `Create` nil; validate email non-empty/contains "@" and password ≥ 8 bytes (re-render with specific error otherwise); `Hash` then `Create`; duplicate-email error from `Create` re-renders `"That email is already registered."`; success signs in like `Signin`. `SigninPage`/`SignupPage` call the render callbacks with `PageData{ReturnTo: r.URL.Query().Get("return_to")}`. `Signout`: `h.cfg.Sessions.SignOut(w, r)` + 303 to `/`. (CSRF is NOT this package's job — `csrf.Protect` is mounted app-wide, Task 12.)

- [ ] **Step 7: Run to verify PASS**, `CGO_ENABLED=0 go test ./password/ && go vet ./password/`.

- [ ] **Step 8: Commit** — `git add password/ && git commit -m "password: email+password identity plugin on the sessions core"`

---

### Task 10: `scope/` — owner-scoped queries

**Files:**
- Create: `scope/scope.go`, `scope/scope_test.go`

**Interfaces:**
- Consumes: `*gorm.DB`; tests use `gormlite.Open` in-memory.
- Produces: `func Owned(g *gorm.DB, owner int64) *gorm.DB` (convention column `user_id`), `func OwnedBy(g *gorm.DB, column string, owner any) *gorm.DB`. Task 12's handlers use `Owned`.

- [ ] **Step 1: Write the failing tests**

```go
func TestOwnedFiltersByUserID(t *testing.T) {
	// gorm.Open(gormlite.Open("file:scope?mode=memory&cache=shared")),
	// AutoMigrate a local `type note struct{ ID int64; UserID int64 }`,
	// create rows for owners 1 and 2.
	// Owned(g, 2).First(&n, 1) with a note ID belonging to owner 1
	// → gorm.ErrRecordNotFound (the 404-not-403 shape).
	// Owned(g, 1).Find(&all) → only owner 1's rows.
}

func TestOwnedByCustomColumn(t *testing.T)   // team_id filtering works
func TestOwnedByRejectsBadColumn(t *testing.T) {
	// OwnedBy(g, `user_id = 1 OR ""=`, 1) must panic — the column is
	// an identifier, not SQL. assert with recover().
}
```

- [ ] **Step 2: Run to verify FAIL.**

- [ ] **Step 3: Implement**

```go
// Package scope makes the right query the short query: every model
// owned by a user (or team) is read through its owner filter, and a
// row that isn't yours is a row that doesn't exist — handlers answer
// 404, never 403 (matching view.ParseID's rule: a URL that was never
// yours was never a URL).
package scope

var identifier = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// Owned scopes g to rows whose user_id is owner — the convention
// column. For another owner column, OwnedBy.
func Owned(g *gorm.DB, owner int64) *gorm.DB {
	return g.Where("user_id = ?", owner)
}

// OwnedBy scopes g by an arbitrary owner column. The column must be a
// plain lower_snake identifier — it is interpolated into SQL, so
// anything else panics loudly at development time rather than parsing
// as SQL.
func OwnedBy(g *gorm.DB, column string, owner any) *gorm.DB {
	if !identifier.MatchString(column) {
		panic("rastrillo/scope: OwnedBy column must be a plain identifier, got " + strconv.Quote(column))
	}
	return g.Where(column+" = ?", owner)
}
```

- [ ] **Step 4: Run to verify PASS**, `CGO_ENABLED=0 go test ./scope/ && go vet ./scope/`.

- [ ] **Step 5: Commit** — `git add scope/ && git commit -m "scope: owner-scoped queries, 404-not-403"`

---

### Task 11: Ctx cleanup and README reframe

**Files:**
- Modify: `ctx.go` (delete `Scope` and `Locale` fields)
- Modify: `README.md` (manifest reframe + two review nits)
- Test: `go build ./...` across all modules (nothing outside examples/generate references the fields — verified during planning)

- [ ] **Step 1: Delete the fields**

Remove `Scope any` and `Locale string` from `Ctx` with their comments (the `Scope` comment cites `_middleware.go`, which never existed — the reference dies with the field). Extend `Ctx`'s type comment: per-request identity now lives in `sessions.Current(r)`; locale was always `rastrillo.LocaleFrom(r)`.

- [ ] **Step 2: Verify nothing breaks**

```bash
CGO_ENABLED=0 go build ./... && go test ./... 2>&1 | tail -20
(cd examples/tickets && go build ./... && go test ./...)
```

Expected: PASS. If an example or test references the deleted fields, delete that reference (planning-time grep found none outside generated code already regenerated in Task 7).

- [ ] **Step 3: README**

- Find the manifest section's "Generates no delete action" paragraph (`grep -n "no delete" README.md`) and delete/correct it — the delete flow shipped in v0.6.x.
- Reframe the manifest section's opening honestly: manifests are the **admin-panel generator** — one flat resource, three field kinds, no relations, no per-user scoping — and the app story for multi-user apps is the primitives + SKILL.md (link it). Keep the section's mechanics intact.
- Add the new packages to the README's package list with one-liners each (`gormlite`, `db`, `sessions`, `csrf`, `flash`, `form`, `view`, `scope`, `password`), and update the not-built list (sessions/CSRF/flash are built now; remove them if listed).

- [ ] **Step 4: Commit** — `git add ctx.go README.md && git commit -m "ctx, README: drop unread Scope/Locale; reframe manifests as admin add-on"`

---

### Task 12: `examples/notes` — the front-door example

**Files:**
- Create: `examples/notes/go.mod` (mirror `examples/tickets/go.mod`'s replace-directive shape, module name `notes`)
- Create: `examples/notes/cmd/notes/main.go`
- Create: `examples/notes/internal/notes/models.go`, `app.go`, `handlers.go`, `render.go`
- Create: `examples/notes/internal/notes/templates/{layout,signin,signup,index,show,new,edit}.html`
- Create: `examples/notes/internal/notestest/harness_test.go`, `crud_test.go`, `isolation_test.go`, `auth_test.go`

**Interfaces:**
- Consumes: `db.Open`, `sessions.*`, `csrf.Protect`, `flash.*`, `password.*`, `scope.Owned`, `form.Errors`, chi v5.
- Produces: the app the SKILL.md describes; the domain surface must stay ≈150 lines (`models.go` + `handlers.go`; `app.go`/`render.go`/main are wiring).

- [ ] **Step 1: Write the models and app shell**

`models.go`:

```go
package notes

import "time"

type User struct {
	ID           int64
	Email        string `gorm:"uniqueIndex"`
	PasswordHash string
	CreatedAt    time.Time
}

type Note struct {
	ID        int64
	UserID    int64 `gorm:"index"`
	Title     string
	Body      string
	CreatedAt time.Time
	UpdatedAt time.Time
}
```

`app.go` — `func App(d *db.DB, origin string, logger *slog.Logger) (*http.ServeMux, error)` (a ServeMux, not a bare handler, because `rastrillo.Options.Mux` is typed `*http.ServeMux` — the chi router mounts inside it):

1. `d.G.AutoMigrate(&User{}, &Note{})`; apply `sessions.Migrations` via `d.G.Exec` (raw strings — the sessions table is not a GORM model).
2. `sessions.New(sessions.Config{DB: sqlDBFromGorm, Origin: origin, Logger: logger})` — get the writer `*sql.DB` via `d.G.DB()` (dbresolver returns the source pool, which is what sessions wants: its statements are mixed read/write).
3. `password.New(password.Config{Sessions: s, Lookup: ..., Create: ..., RenderSignin: ..., RenderSignup: ...})` — `Lookup` is `scope`-free (`d.G.Where("email = ?", email).First(&u)`, translating `gorm.ErrRecordNotFound` → `sql.ErrNoRows`); `Create` inserts a `User` and translates the unique-violation into a plain error.
4. chi router:

```go
r := chi.NewRouter()
r.Use(csrf.Protect(origin))
r.Get("/signin", ph.SigninPage)
r.Post("/signin", ph.Signin)
r.Get("/signup", ph.SignupPage)
r.Post("/signup", ph.Signup)
r.Post("/signout", ph.Signout)
r.Group(func(r chi.Router) {
	r.Use(s.Require)
	r.Get("/", listNotes)
	r.Get("/notes/new", newNote)
	r.Post("/notes", createNote)
	r.Get("/notes/{id}", showNote)
	r.Get("/notes/{id}/edit", editNote)
	r.Post("/notes/{id}", updateNote)
	r.Post("/notes/{id}/delete", deleteNote)
})
mux := http.NewServeMux()
mux.Handle("/", r)
return mux, nil
```

- [ ] **Step 2: Write the handlers (the ~110-line file the review demands)**

Every read/write goes through `scope.Owned(d.G, uid)` where `uid, _ := sessions.UserID(r)`. `showNote`/`editNote`/`updateNote`/`deleteNote`: `scope.Owned(...).First(&n, id)`; `gorm.ErrRecordNotFound` → `http.NotFound` (Bob force-updating Alice's note gets the same 404 as a random ID). `createNote`/`updateNote`: bind ONLY `Title`+`Body` from `r.PostFormValue` onto explicit fields (never a struct bind — the SKILL.md rule, demonstrated); blank title → re-render the form at 422 with `form.Errors{"Title": "Title is required"}` and the submitted values seeded back. Set `flash.Set(w, "notice", "Note created.")` (and updated/deleted) before each redirect. Updates persist via `d.G.Model(&n).Select("Title", "Body", "UpdatedAt").Updates(...)`.

`render.go`: `//go:embed templates`, layout + per-page `template.ExecuteTemplate`; layout takes `{Flash flash.Flash; HasFlash bool; SignedIn bool; Content any}` — call `flash.Take(w, r)` once per render. Templates are minimal semantic HTML (this example demonstrates the middle layer, not the ui package).

`cmd/notes/main.go` (~40 lines): flags/env per Serve's contract — open `db.Open(dbPath, logger)` **itself** (Options.DBPath stays empty; the app owns the GORM handle), build `App`, then

```go
rastrillo.Run(rastrillo.Options{
	Mux:    mux,
	Socket: socket, Addr: addr,
})
```

Mirror `examples/tickets/cmd/tickets/main.go` for the argv/activation handling — copy its shape, swap the app construction. Origin for dev: `http://localhost:8080` unless `NOTES_ORIGIN` is set.

- [ ] **Step 3: Write the test harness + suites (failing first, app second is fine within the step — but run them red before wiring the app)**

`harness_test.go`: boot `App` on a temp `db.Open`, wrap in `httptest.NewServer`, origin = server URL (so same-origin POSTs pass `csrf.Protect` when the test sets `Origin: ts.URL`). Client helper: `http.Client` with `cookiejar.New`, a `postForm(path string, vals url.Values)` that sets the `Origin` header, and `signup(email, password)` composing the real HTTP flow.

`auth_test.go`:

```go
func TestSignupSigninSignout(t *testing.T)       // full flow through real forms
func TestRequireRedirectsAnonymous(t *testing.T) // GET / anonymous → lands on /signin?return_to=%2F
func TestReturnToAfterSignin(t *testing.T)       // GET /notes/new anonymous → signin → POST signin with return_to → back at /notes/new
func TestCrossOriginPostRefused(t *testing.T)    // POST /notes with Origin: https://evil.example → 403
func TestSignoutRevokesServerSide(t *testing.T)  // after signout, replaying the old cookie → redirected to signin
```

`crud_test.go`:

```go
func TestNoteCRUDThroughForms(t *testing.T)      // create → list shows it → edit → show → delete → gone
func TestValidationRerendersAt422(t *testing.T)  // blank title → 422, body contains the error AND the submitted Body value (input preserved)
func TestFlashShownOnce(t *testing.T)            // after create, the redirect target shows "Note created." once; reloading shows it zero times
```

`isolation_test.go` — the permanent regression guard:

```go
func TestTwoUserIsolation(t *testing.T) {
	// Alice signs up, creates a note, captures its ID from the redirect.
	// Bob signs up (separate cookie jar).
	// Bob GET  /notes/{aliceID}        → 404
	// Bob GET  /notes/{aliceID}/edit   → 404
	// Bob POST /notes/{aliceID}        → 404 (the force-update probe)
	// Bob POST /notes/{aliceID}/delete → 404, and Alice's note still exists
	// Bob GET  /                       → Alice's title absent from the body
}
```

- [ ] **Step 4: Run the whole example suite**

```bash
(cd examples/notes && CGO_ENABLED=0 go test ./... && go vet ./... && CGO_ENABLED=0 go build ./...)
```

Expected: PASS. Then measure the claim: `wc -l internal/notes/models.go internal/notes/handlers.go` — the domain surface; record the number for the PR description (target ≈150; if it lands over 200, look for wiring that leaked into handlers.go before accepting it).

- [ ] **Step 5: Commit**

```bash
git add examples/notes/
git commit -m "examples/notes: accounts + owner-scoped CRUD on the middle layer"
```

---

### Task 13: `SKILL.md`

**Files:**
- Create: `SKILL.md` (repo root)

**Interfaces:**
- Consumes: every package built above (its examples must compile conceptually against their real signatures — copy signatures from the packages, don't paraphrase).
- Produces: the doc an LLM loads instead of framework source; budget ≤ 15 KB (`wc -c SKILL.md`).

- [ ] **Step 1: Write it**

Frontmatter name `rastrillo`, description "Build a multi-user CARLOS app on Rastrillo: GORM models, chi routes, sessions, owner-scoped queries." Sections, with the **normative rules verbatim as written here**:

1. **App shape** — the examples/notes layout (models.go / app.go / handlers.go / render.go / cmd), the `db.Open` + `App(...)` + `rastrillo.Run(Options{Mux:...})` wiring, and the sentence: *"The platform contract — activation argv, LISTEN_FDS, $STATE_DIRECTORY, /healthz, /api/version, SIGTERM drain — is inherited from rastrillo.Run/Serve; never hand-roll any of it."*
2. **Data** — models are plain GORM structs; `db.Open` gives writer-1/reader-N pools automatically. *"Schema changes go through AutoMigrate and are additive-only: never rename or drop a column on an existing table — add a new column and migrate data in code."*
3. **Scoping** — *"Every query on an owned model goes through `scope.Owned(d.G, uid)` (or `scope.OwnedBy` for team-owned rows). Never call First/Find/Update/Delete on an owned model without the owner filter — including inside transactions: a `d.G.Transaction` callback must apply `scope.Owned` to every statement in it, the same as outside."* And: *"A row that isn't yours is a row that doesn't exist: answer 404, never 403."* And the join-table stance: *"A join table is scoped through BOTH sides: reading or writing a membership row requires the caller to be authorized on each side it links, checked explicitly — the stricter reading always wins."*
4. **Forms and mass assignment** — *"Never bind a request body onto a GORM model — no reflection binding, no loops over PostForm. Read each permitted field by name (`r.PostFormValue("Title")`) and write updates through `.Select("Title", "Body").Updates(...)` so an unexpected form field can never reach a column."* Validation: check, collect `form.Errors`, re-render at 422 with submitted values seeded back. Money uses `form.ParseCents`/`FormatCentsPlain` (forms) / `form.FormatCents` (display).
5. **Sessions & identity** — mount `csrf.Protect(origin)` app-wide; guard signed-in routes with a chi `Group` + `s.Require`; read the viewer with `sessions.UserID(r)`; sign-in redirect targets go through `sessions.SafeReturn` (never a raw `return_to`). Password plugin wiring (the app.go recipe); keymail plugin exists for family apps.
6. **What NOT to do** — the manifest generator is for standalone admin tables only (no relations, no scoping — never use it for user-owned data); never import `glebarez/*` or `gorm.io/driver/sqlite` (driver clash / cgo); never `git merge` to main.

- [ ] **Step 2: Verify budget and accuracy**

```bash
wc -c SKILL.md   # must print ≤ 15000
```

Cross-check every code identifier in the doc against the real packages (`grep` each function name); a skill doc that names a function that doesn't exist poisons every generation that reads it.

- [ ] **Step 3: Commit** — `git add SKILL.md && git commit -m "SKILL.md: the app story as a loadable skill"`

---

### Task 14: Acceptance sweep and PR

**Files:** none new — this is the spec §12 gate.

- [ ] **Step 1: The full matrix**

```bash
CGO_ENABLED=0 go build ./... && go vet ./... && CGO_ENABLED=0 go test ./...
(cd examples/tickets && CGO_ENABLED=0 go build ./... && go vet ./... && CGO_ENABLED=0 go test ./...)
(cd examples/notes   && CGO_ENABLED=0 go build ./... && go vet ./... && CGO_ENABLED=0 go test ./...)
wc -c SKILL.md
grep -rn 'glebarez' go.mod examples/*/go.mod && echo "FAIL: glebarez leaked" || echo "OK"
grep -n 'modernc.org/sqlite v1.55.0' go.mod
```

Every command must succeed; fix forward anything that doesn't, then re-run the whole block from the top.

- [ ] **Step 2: Push and open the PR**

```bash
git push -u origin criticism
gh pr create --title "Known-libraries middle layer" --body "..."
```

PR body: the one-paragraph story (review + bake-off → primitives on known libraries), the before/after generated-line count from Task 7, the notes domain-line count from Task 12, links to the spec and this plan, and the deviation notes from Global Constraints. Do NOT merge — the PR is the deliverable.
