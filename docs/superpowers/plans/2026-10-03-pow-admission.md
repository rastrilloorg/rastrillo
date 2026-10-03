# pow admission, and sign-in behind it by default: implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Grow `rastrillo/pow` into a front door Tito Go can move onto (admission that commits with the write, sealed scope and expiry, opt-in binding, a no-JavaScript tier, challenge-only recovery), and put `auth` and `password` sign-in and sign-up behind it by default.

**Architecture:** `pow` keeps its two halves in one module. The Go half gains a v2 seal, `Admit`/`Commit`/`Check`/`Verify`, an in-process attempt map and a `Form` value templates render; the browser half (`pow.js`) is rewritten around an explicit `init`, background solving and held submits. `auth` and `password` take an app-built `*pow.Guard` and refuse to boot without a decision. Nothing outside `pow/` imports it yet, so the API breaks freely.

**Tech Stack:** Go 1.25 stdlib (`crypto/hmac`, `encoding/binary`, `database/sql`), SQLite via `modernc.org/sqlite` v1.55.0 (bundles SQLite 3.53.3, so `unixepoch('subsec')`, which needs 3.42, is available), chromedp through `rastrillo/harness` for browser tests, plain ES modules in the browser.

**Spec:** `docs/superpowers/specs/2026-10-03-pow-admission-design.md`. Read it before starting a task; this plan argues from it and cites its sections as "spec §…".

## Global Constraints

- The gate, before every push: `GOFLAGS=-mod=mod go vet ./... && gofmt -l . && GOFLAGS=-mod=mod go test ./...` (`gofmt -l` prints nothing). `GOFLAGS=-mod=mod` is needed locally only (three scratch-module packages); CI does not set it.
- Browser tests: `TMPDIR="${TMPDIR:-/var/tmp}" go test -tags browser -p 1 ./pow/ ./auth/ ./ui/ -count=1`. They are part of `make ci` (`browser` target); a red browser test is a red gate.
- Examples are separate modules: test `examples/notes` from `examples/notes/` (`cd examples/notes && GOFLAGS=-mod=mod go test ./...`).
- Run git and `go run`-based tooling with the sandbox off in this checkout (sandboxed git sees phantom dotfiles; the module cache is read-only inside it).
- No new module dependencies.
- Comments say why, naming the failure they prevent (AGENTS.md). Commits: imperative subject, body says why. End every commit message with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- User-facing copy: no em dashes in UI strings. Every new string goes through Task 1's copy review before it is written into any file. Translations of approved English are not reviewed.
- SKILL.md has a 30,000-byte ceiling (`skillmd_test.go`, `skillBudget`); it is at 27,514 today. Trim redundant prose to fit; never delete a load-bearing fact.
- Scratch files under `$TMPDIR` or `~/.cache/pow-admission/`, never `/tmp`; delete them when done.
- Field names are fixed: `pow_scope`, `pow_nonce`, `pow_issued`, `pow_expires`, `pow_difficulty`, `pow_flags`, `pow_seal`, `pow_counter`, honeypot `hp`.
- Scopes are fixed: `rastrillo/auth/begin`, `rastrillo/password/signin`, `rastrillo/password/signup`.
- Defaults are fixed: `MinAge` 3s (`DefaultMinAge`), `MaxAge` 2h, `Attempts` 20, `Tracked` 100,000, sweep margin 10 minutes, sweep batch 500, `whenSolved` timeout 10s, auth/password guards in docs and the example use `MinAge: 500 * time.Millisecond`.

## Review Focus

The five inputs the spec implies but its test list does not pin, most likely first. Each has a test in the task named.

1. **Back button after a successful sign-in, then Continue again.** The restored page carries a spent token. Expected: a "check" problem and a recovery form that works, never a dead end. Test: Task 8, `TestBeginRecoversAReplayedForm`.
2. **Garbage or oversized challenge fields** (a 1 MB `pow_scope`, a non-numeric `pow_issued`, a negative or huge `pow_difficulty`). Expected: refused as `seal_invalid` before any hashing beyond the HMAC and before any database read. Test: Task 3, `TestReadChallengeRefusesMalformedFields`.
3. **The instance key rotated between render and submit.** Expected: refused, and the recovery form from the new Guard succeeds. Test: Task 4, `TestRotatedKeyRefusesThenRecovers`.
4. **Enter pressed in a field before the module has loaded.** Expected: nothing posts (the default button is disabled) and the status line is visible. Test: Task 6, `TestBrowserEnterBeforeReadyPostsNothing`.
5. **Two tabs of the same sign-in page, both submitted.** Expected: both succeed, because each render mints its own token. Test: Task 4, `TestTwoTabsBothSucceed`.

## Deviations from the spec, decided here

- **The status line's words come from the template, not from `Fields`.** `pow` has no locale, and the line must be in the visitor's language. `Form.StatusLine(text)` renders `<p data-pow-status>text</p>`; the ui partials pass `T "rastrillo.ui.pow_status"`. Behaviour is otherwise the spec's: visible from first paint, hidden by the module when ready, shown again on failure.
- **No reload link inside the status line.** The sentence itself says to reload; a link inside a translated sentence needs per-locale markup for no gain.
- **`Config.Bind` with `NoProof` is a boot error** (there is nothing to bind). The spec does not say; the alternative is a silently ignored setting.
- **`pow/powtest`** is a new test-support package (`Fill`, `FillBound`) so `auth`, `password` and `examples/notes` tests can submit a challenge the way the browser does. Shipping a solver in a package named for tests is the `httptest` pattern; the existing comment in `pow_test.go` that a Go solver must never ship is updated to say why this one is acceptable: hashcash is ten lines for anyone who wants it, so the risk was never secrecy, it was an app solving its own challenges in production, which a `…test` import path makes obvious in review.

## File structure

| File | Responsibility |
|---|---|
| `pow/migrations/0002_expires_ms.sql` | Recreate `pow_spent_nonces` with `expires_ms INTEGER` |
| `pow/store.go` | `Execer`, `NonceStore` (Spend with executor and database-clock expiry, Spent, Ready, bounded Sweep), `SQLNonces`, `MemoryNonces` |
| `pow/challenge.go` | `Challenge`, the v2 seal, field names, `readChallenge`, `Challenge.Fields`, `HoneypotStyleHash` |
| `pow/guard.go` | `Config`, `Guard`, `New`, `Reason`s, errors, `Want`, `Admission`, `Parent`, `Admit`, `Commit`, `Check`, `Verify`, `Trapped`, `Sweep` |
| `pow/attempts.go` | The bounded attempt map |
| `pow/form.go` | `Form`, `Issue`, `Form`, `FollowOn`, `Recovery`, `Attrs`, `Script`, `StatusLine`, `NeedsScript` |
| `pow/pow.go` | Package doc (rewritten), `Verify`, `normalize`, `DefaultDifficulty` |
| `pow/browser/pow.js` | Rewritten: `init`, background solve, holds, navigation, `whenSolved` |
| `pow/powtest/powtest.go` | Test support: fill a rendered challenge |
| `ui/busy.js` | Skip the hold after a `pow` release; cancel holds on `beforeunload` |
| `auth/auth.go`, `auth/handlers.go`, `auth/signin.go` | `Proof`/`ProofOff`, `Begin` checks first, `rec`/`force` carried, `SigninState.Proof`/`Force`, `ProblemCheck` |
| `ui/partials/signin.html`, `ui/partials/form-foot.html` | Render the challenge |
| `locales/*.toml` (12 files) | Three new keys |
| `password/handlers.go` | `Proof`/`ProofOff`, `Check`, `PageData.Proof`, `no-store` |
| `examples/notes/...` | End-to-end wiring, sweep loop, harness through `powtest` |
| `docs/site/reference/pow.md`, `docs/site/magic-links.md`, `docs/site/passwords.md`, `SKILL.md`, `CHANGELOG.md` | Docs |

---

### Task 1: Copy review of the new strings (controller and Paul)

This task is run by the controlling session with Paul, not by a subagent: the `copy-review` skill needs a person.

**Files:** none written until approval.

- [ ] **Step 1: Run the copy-review skill on these strings**

Invoke the `copy-review` skill with exactly these drafts and their context:

| Key / place | Draft | Where it shows |
|---|---|---|
| `rastrillo.ui.signin_problem_check` | `Your browser couldn't finish a security check. Try again.` | Callout on the sign-in screen after a refused proof |
| `rastrillo.ui.pow_status` | `If this form doesn't respond, reload the page.` | Small line beside a protected form's button; visible until the form is ready, and again if the check fails |
| `rastrillo.ui.pow_noscript` | `This form needs JavaScript to work.` | Inside `<noscript>` beside a protected form's button |
| `password` Go constant `checkFailed` | `Your browser couldn't finish a security check. Try again.` | Error above the password sign-in and sign-up forms after a refused proof |

- [ ] **Step 2: Record the approved strings**

Write the approved English, verbatim, into `~/.cache/pow-admission/approved-copy.md` (create the directory). Tasks 9 and 10 copy from that file. Do not start Task 9 or 10 before this file exists.

---

### Task 2: The spent-nonce store

**Files:**
- Create: `pow/migrations/0002_expires_ms.sql`
- Modify: `pow/store.go` (whole file)
- Test: `pow/store_test.go` (whole file replaced)

**Interfaces:**
- Produces:
  - `type Execer interface { ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) }`
  - `type NonceStore interface { Spend(ctx context.Context, ex Execer, nonce string, expires time.Time) (bool, error); Spent(ctx context.Context, nonce string) (bool, error); Ready(ctx context.Context) error; Sweep(now time.Time) error }`
  - `func SQLNonces(db *sql.DB) NonceStore`, `func MemoryNonces() NonceStore`
  - `var ErrNoSchema error`
  - unexported: `const sweepMargin = 10 * time.Minute`, `const sweepBatch = 500`

- [ ] **Step 1: Write the failing tests**

Replace `pow/store_test.go` with:

```go
package pow_test

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/db"
	"amadan.net/rastrillo/rastrillo/migrate"
	"amadan.net/rastrillo/rastrillo/pow"
)

func openTestDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "pow.db"), nil)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func newTestStore(t *testing.T) (pow.NonceStore, *db.DB) {
	t.Helper()
	d := openTestDB(t)
	if _, err := migrate.Apply(context.Background(), d, pow.Schema); err != nil {
		t.Fatalf("migrate.Apply: %v", err)
	}
	return pow.SQLNonces(d.Writer()), d
}

func TestSQLNoncesSpendsOnce(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	expires := time.Now().Add(time.Hour)
	if fresh, err := s.Spend(ctx, nil, "abc", expires); err != nil || !fresh {
		t.Fatalf("first spend = %v, %v; want fresh", fresh, err)
	}
	if fresh, err := s.Spend(ctx, nil, "abc", expires); err != nil || fresh {
		t.Fatalf("second spend = %v, %v; want not fresh", fresh, err)
	}
	if spent, err := s.Spent(ctx, "abc"); err != nil || !spent {
		t.Fatalf("Spent = %v, %v; want true", spent, err)
	}
}

func TestSQLNoncesSpendIsAtomicUnderConcurrency(t *testing.T) {
	// SELECT-then-INSERT would let two replays both see an empty table.
	s, _ := newTestStore(t)
	ctx := context.Background()
	expires := time.Now().Add(time.Hour)
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fresh, err := s.Spend(ctx, nil, "race", expires)
			if err != nil {
				t.Errorf("Spend: %v", err)
				return
			}
			if fresh {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("%d concurrent spends won, want exactly 1", wins)
	}
}

func TestSQLNoncesRefusesAnExpiredSpendOnTheDatabaseClock(t *testing.T) {
	// The expiry is checked by the INSERT itself against SQLite's clock.
	// A handler that checked in Go and then stalled past expiry and a
	// sweep would otherwise insert into a slot the sweep had emptied.
	s, _ := newTestStore(t)
	ctx := context.Background()
	if fresh, err := s.Spend(ctx, nil, "late", time.Now().Add(-time.Second)); err != nil || fresh {
		t.Fatalf("spend past expiry = %v, %v; want refused", fresh, err)
	}
}

func TestSQLNoncesSpendRidesTheCallersTransaction(t *testing.T) {
	s, d := newTestStore(t)
	ctx := context.Background()
	tx, err := d.Writer().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if fresh, err := s.Spend(ctx, tx, "tx", time.Now().Add(time.Hour)); err != nil || !fresh {
		t.Fatalf("spend in tx = %v, %v", fresh, err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if spent, _ := s.Spent(ctx, "tx"); spent {
		t.Fatal("a rolled-back spend stayed spent: the caller's retry would be refused")
	}
}

func TestSQLNoncesSweepIsBoundedAndKeepsTheMargin(t *testing.T) {
	s, d := newTestStore(t)
	ctx := context.Background()
	w := d.Writer()
	old := time.Now().Add(-time.Hour).UnixMilli()
	recent := time.Now().Add(-5 * time.Minute).UnixMilli() // inside the 10-minute margin
	for i := 0; i < 600; i++ {
		if _, err := w.ExecContext(ctx, `INSERT INTO pow_spent_nonces (nonce, expires_ms) VALUES (?, ?)`,
			"old"+strconv.Itoa(i), old); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := w.ExecContext(ctx, `INSERT INTO pow_spent_nonces (nonce, expires_ms) VALUES ('recent', ?)`, recent); err != nil {
		t.Fatal(err)
	}
	if err := s.Sweep(time.Now()); err != nil {
		t.Fatal(err)
	}
	var left int
	w.QueryRowContext(ctx, `SELECT count(*) FROM pow_spent_nonces WHERE nonce LIKE 'old%'`).Scan(&left)
	if left != 100 {
		t.Fatalf("one sweep left %d old rows, want 100 (a batch of 500)", left)
	}
	if spent, _ := s.Spent(ctx, "recent"); !spent {
		t.Fatal("a row inside the 10-minute margin was swept")
	}
}

func TestSQLNoncesReadyNamesAMissingTable(t *testing.T) {
	d := openTestDB(t)
	err := pow.SQLNonces(d.Writer()).Ready(context.Background())
	if !errors.Is(err, pow.ErrNoSchema) {
		t.Fatalf("Ready on an empty database = %v, want ErrNoSchema", err)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `cd /home/paulca/amadan.net/rastrillo/rastrillo-pow-admission && GOFLAGS=-mod=mod go test ./pow/ -run 'SQLNonces' -count=1`
Expected: compile failure (`Spend` has the old signature; `Spent`, `Ready`, `ErrNoSchema` undefined).

- [ ] **Step 3: Write the migration**

Create `pow/migrations/0002_expires_ms.sql`:

```sql
-- v2 seals an absolute expiry in milliseconds and the spend compares it
-- with SQLite's own clock inside the INSERT, so the column must be a
-- number SQL can compare; the RFC 3339 text it replaces cannot be.
-- No app had imported pow when this was written, so the table is
-- recreated rather than converted row by row.
DROP TABLE IF EXISTS pow_spent_nonces;

CREATE TABLE pow_spent_nonces (
  nonce      TEXT PRIMARY KEY,
  expires_ms INTEGER NOT NULL
);

CREATE INDEX pow_spent_nonces_expires_ms ON pow_spent_nonces (expires_ms);
```

- [ ] **Step 4: Rewrite `pow/store.go`**

```go
package pow

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"amadan.net/rastrillo/rastrillo/migrate"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Schema is pow's own migrations — one table of spent nonces. Merge it
// with the rest of your app's set and apply it BEFORE pow.New, which
// checks the table is there:
//
//	migrate.Apply(ctx, d, migrate.Merge(sessions.Schema, pow.Schema, app.Schema))
var Schema = migrate.MustFromFS(migrationFS, "pow")

// ErrNoSchema means pow_spent_nonces is missing or predates v2. New
// refuses to build a Guard over it: otherwise every submission is
// ReasonUnavailable and the first visitor finds out.
var ErrNoSchema = errors.New("rastrillo/pow: the pow_spent_nonces table is missing or out of date: merge pow.Schema into the set you migrate.Apply before pow.New")

// Execer is what Commit spends on: *sql.Tx, *sql.DB, *sql.Conn, or a
// GORM transaction's tx.Statement.ConnPool. It has no query method on
// purpose. Commit must never read through another handle while the
// caller's transaction holds the only connection: that read would wait
// for the transaction, which waits for Commit.
type Execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// NonceStore remembers which challenges have been spent.
//
// Spend records a nonce and reports whether it was fresh. It must be
// atomic, and it must refuse a nonce whose expires has passed on the
// store's own clock, inside the same statement: a check done before
// calling it can be overtaken by a stalled request and a sweep, and the
// same token would then be spent twice. ex is the caller's transaction,
// or nil for the store's own handle.
type NonceStore interface {
	Spend(ctx context.Context, ex Execer, nonce string, expires time.Time) (bool, error)
	Spent(ctx context.Context, nonce string) (bool, error)
	Ready(ctx context.Context) error
	Sweep(now time.Time) error
}

// sweepMargin is how long past its sealed expiry a spent row is kept.
// The spend refuses anything past expiry on the database clock, so any
// margin closes replay; ten minutes keeps a clock step or a slow sweep
// from mattering.
const sweepMargin = 10 * time.Minute

// sweepBatch bounds one Sweep. A sweep on wake that deleted everything
// at once would hold SQLite's single writer while the first visitor
// waits.
const sweepBatch = 500

// dbNowMillis is SQLite's clock in milliseconds. Spend and Sweep both
// read it, on the one writer, so they agree on what "now" is.
const dbNowMillis = `CAST(unixepoch('subsec') * 1000 AS INTEGER)`

// SQLNonces stores spent nonces in the app database. An in-memory store
// forgets them on restart, and a deploy is then a window in which every
// challenge minted in the last MaxAge is replayable.
func SQLNonces(db *sql.DB) NonceStore { return &sqlNonces{db: db} }

type sqlNonces struct{ db *sql.DB }

func (s *sqlNonces) Spend(ctx context.Context, ex Execer, nonce string, expires time.Time) (bool, error) {
	if ex == nil {
		ex = s.db
	}
	ms := expires.UnixMilli()
	res, err := ex.ExecContext(ctx,
		`INSERT OR IGNORE INTO pow_spent_nonces (nonce, expires_ms) SELECT ?, ? WHERE `+dbNowMillis+` <= ?`,
		nonce, ms, ms)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

func (s *sqlNonces) Spent(ctx context.Context, nonce string) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM pow_spent_nonces WHERE nonce = ?`, nonce).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// Ready selects the v2 columns. "no such table" and "no such column"
// are both a migration that did not run, and both become ErrNoSchema so
// the boot error names the fix; anything else is passed through.
func (s *sqlNonces) Ready(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT nonce, expires_ms FROM pow_spent_nonces LIMIT 0`)
	if err != nil {
		if strings.Contains(err.Error(), "no such") {
			return fmt.Errorf("%w (%v)", ErrNoSchema, err)
		}
		return err
	}
	return rows.Close()
}

func (s *sqlNonces) Sweep(time.Time) error {
	_, err := s.db.Exec(`DELETE FROM pow_spent_nonces WHERE rowid IN (
		SELECT rowid FROM pow_spent_nonces WHERE expires_ms < `+dbNowMillis+` - ? LIMIT ?)`,
		sweepMargin.Milliseconds(), sweepBatch)
	return err
}

// MemoryNonces keeps spent nonces in the process, under the same rules
// as SQLNonces. It ignores the caller's transaction, so a spend inside
// a transaction that rolls back stays spent: honest for tests and a
// single process that can afford to forget, wrong for anything else.
func MemoryNonces() NonceStore {
	return &memNonces{spent: make(map[string]time.Time), now: time.Now}
}

type memNonces struct {
	mu    sync.Mutex
	spent map[string]time.Time
	now   func() time.Time
}

func (m *memNonces) Spend(_ context.Context, _ Execer, nonce string, expires time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.now().After(expires) {
		return false, nil
	}
	if _, ok := m.spent[nonce]; ok {
		return false, nil
	}
	m.spent[nonce] = expires
	return true, nil
}

func (m *memNonces) Spent(_ context.Context, nonce string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.spent[nonce]
	return ok, nil
}

func (m *memNonces) Ready(context.Context) error { return nil }

func (m *memNonces) Sweep(now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for nonce, expires := range m.spent {
		if n == sweepBatch {
			break
		}
		if expires.Before(now.Add(-sweepMargin)) {
			delete(m.spent, nonce)
			n++
		}
	}
	return nil
}
```

- [ ] **Step 5: Run the store tests**

Run: `GOFLAGS=-mod=mod go vet ./pow/ 2>&1 | head`
Expected: errors only in `guard.go` and `pow_test.go`, which still call the old `Spend`; `store.go` itself must report nothing. Task 4 rewrites those files, and these tests run at the end of Task 4. `pow` does not compile between Tasks 2 and 4, so they land as one commit.

- [ ] **Step 6: No commit yet** (see Step 5). Tasks 2, 3 and 4 land as one commit because `pow` does not compile between them.

---

### Task 3: The v2 challenge and seal

**Files:**
- Modify: `pow/challenge.go` (whole file)
- Test: `pow/challenge_test.go` (new)

**Interfaces:**
- Consumes: nothing from Task 2.
- Produces:
  - `type Challenge struct { Scope, Nonce string; Issued, Expires time.Time; Difficulty int; Flags uint8; Seal string }`
  - `func (c Challenge) Fields() template.HTML`
  - unexported: `newChallenge(key []byte, issued time.Time, maxAge time.Duration, scope string, difficulty int, flags uint8) Challenge`, `sealOf(key []byte, c Challenge) string`, `sealOK(key []byte, c Challenge) bool`, `readChallenge(r *http.Request) (Challenge, Reason)`, `flagTrapOmitted`, `flagBound`, the `field*` constants, `maxScopeLen = 256`
  - kept: `DefaultMinAge`, `DefaultMaxAge`, `HoneypotStyleHash`, `honeypotStyle`

- [ ] **Step 1: Write the failing tests**

Create `pow/challenge_test.go`:

```go
package pow

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

var testKey = []byte("0123456789abcdef0123456789abcdef")

func TestSealInputIsInjective(t *testing.T) {
	// v1 joined fields with NUL, so a scope ending in "\x00b" and a
	// nonce starting with "b\x00" signed the same bytes and a token
	// moved between scopes without forging anything.
	at := time.UnixMilli(1_700_000_000_000)
	a := Challenge{Scope: "a\x00b", Nonce: "n", Issued: at, Expires: at}
	b := Challenge{Scope: "a", Nonce: "b\x00n", Issued: at, Expires: at}
	if bytes.Equal(sealInput(a), sealInput(b)) {
		t.Fatal("two different (scope, nonce) pairs sign the same bytes")
	}
	c := Challenge{Scope: "\x01x", Nonce: "y", Issued: at, Expires: at}
	d := Challenge{Scope: "", Nonce: "\x01xy", Issued: at, Expires: at}
	if bytes.Equal(sealInput(c), sealInput(d)) {
		t.Fatal("a length-like scope byte collides")
	}
}

func TestNewChallengeIsMillisecondAndSealsExpiry(t *testing.T) {
	issued := time.Unix(1_700_000_000, 999_999_999) // late in a second
	c := newChallenge(testKey, issued, 2*time.Hour, "s", 10, 0)
	if c.Issued.UnixMilli() != issued.UnixMilli() {
		t.Fatalf("issued = %v, want millisecond %v", c.Issued.UnixMilli(), issued.UnixMilli())
	}
	if got := c.Expires.Sub(c.Issued); got != 2*time.Hour {
		t.Fatalf("sealed lifetime = %v, want 2h", got)
	}
	if !validNonce(c.Nonce) {
		t.Fatalf("minted nonce %q is not 32 lowercase hex", c.Nonce)
	}
	if !sealOK(testKey, c) {
		t.Fatal("a fresh challenge does not verify")
	}
}

func TestSealRefusesEveryEditedField(t *testing.T) {
	base := newChallenge(testKey, time.Now(), time.Hour, "s", 10, 0)
	edits := map[string]func(*Challenge){
		"scope":      func(c *Challenge) { c.Scope = "t" },
		"nonce":      func(c *Challenge) { c.Nonce = strings.Repeat("0", 32) },
		"issued":     func(c *Challenge) { c.Issued = c.Issued.Add(time.Millisecond) },
		"expires":    func(c *Challenge) { c.Expires = c.Expires.Add(time.Hour) },
		"difficulty": func(c *Challenge) { c.Difficulty = 1 },
		"flags":      func(c *Challenge) { c.Flags = flagTrapOmitted },
	}
	for name, edit := range edits {
		c := base
		edit(&c)
		if sealOK(testKey, c) {
			t.Errorf("editing %s kept the seal valid", name)
		}
	}
}

func postValues(v url.Values) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(v.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.ParseForm()
	return r
}

func valuesOf(c Challenge) url.Values {
	return url.Values{
		fieldScope:      {c.Scope},
		fieldNonce:      {c.Nonce},
		fieldIssued:     {strconv.FormatInt(c.Issued.UnixMilli(), 10)},
		fieldExpires:    {strconv.FormatInt(c.Expires.UnixMilli(), 10)},
		fieldDifficulty: {strconv.Itoa(c.Difficulty)},
		fieldFlags:      {strconv.Itoa(int(c.Flags))},
		fieldSeal:       {c.Seal},
	}
}

func TestReadChallengeRoundTripsFields(t *testing.T) {
	c := newChallenge(testKey, time.Now(), time.Hour, "rastrillo/auth/begin", 12, flagTrapOmitted)
	got, reason := readChallenge(postValues(valuesOf(c)))
	if reason != "" {
		t.Fatalf("readChallenge refused a good challenge: %s", reason)
	}
	if got != c {
		t.Fatalf("round trip = %+v, want %+v", got, c)
	}
}

func TestReadChallengeCallsAPostWithNoFieldsMissing(t *testing.T) {
	if _, reason := readChallenge(postValues(url.Values{"address": {"a@b.c"}})); reason != ReasonMissing {
		t.Fatalf("no challenge fields = %q, want missing", reason)
	}
}

func TestReadChallengeRefusesMalformedFields(t *testing.T) {
	// Review focus 2: garbage is refused before any hashing beyond the
	// HMAC and before the database, with one reason for all of it.
	good := newChallenge(testKey, time.Now(), time.Hour, "s", 10, 0)
	cases := map[string]func(url.Values){
		"huge scope":      func(v url.Values) { v.Set(fieldScope, strings.Repeat("x", 1<<20)) },
		"short nonce":     func(v url.Values) { v.Set(fieldNonce, "abc") },
		"upper nonce":     func(v url.Values) { v.Set(fieldNonce, strings.ToUpper(good.Nonce)) },
		"bad issued":      func(v url.Values) { v.Set(fieldIssued, "soon") },
		"bad expires":     func(v url.Values) { v.Set(fieldExpires, "") },
		"negative bits":   func(v url.Values) { v.Set(fieldDifficulty, "-1") },
		"huge bits":       func(v url.Values) { v.Set(fieldDifficulty, "100000") },
		"bad flags":       func(v url.Values) { v.Set(fieldFlags, "999") },
		"short seal":      func(v url.Values) { v.Set(fieldSeal, "00") },
	}
	for name, edit := range cases {
		v := valuesOf(good)
		edit(v)
		if _, reason := readChallenge(postValues(v)); reason != ReasonSealInvalid {
			t.Errorf("%s: reason %q, want seal_invalid", name, reason)
		}
	}
}

func TestAV1PostIsRefused(t *testing.T) {
	// v1 posted pow_issued_at and no scope or expiry. Its seal cannot
	// verify under v2, and its shape is refused before the HMAC.
	v1 := url.Values{
		"pow_nonce":      {strings.Repeat("ab", 16)},
		"pow_issued_at":  {"1800000000"},
		"pow_difficulty": {"18"},
		"pow_seal":       {strings.Repeat("0", 64)},
		"pow_counter":    {"12345"},
	}
	if _, reason := readChallenge(postValues(v1)); reason != ReasonSealInvalid {
		t.Fatalf("a v1 post = %q, want seal_invalid", reason)
	}
}

func TestFieldsCarryEverySealedFieldAndTheTrapUnlessOmitted(t *testing.T) {
	c := newChallenge(testKey, time.Now(), time.Hour, "s", 10, 0)
	html := string(c.Fields())
	for _, name := range []string{fieldScope, fieldNonce, fieldIssued, fieldExpires, fieldDifficulty, fieldFlags, fieldSeal, fieldCounter, fieldHoneypot} {
		if !strings.Contains(html, `name="`+name+`"`) {
			t.Errorf("Fields has no %s", name)
		}
	}
	trapless := newChallenge(testKey, time.Now(), time.Hour, "s", 10, flagTrapOmitted)
	if strings.Contains(string(trapless.Fields()), `name="`+fieldHoneypot+`"`) {
		t.Error("a trap-omitted challenge still renders the honeypot: a password manager that filled it once fills it again")
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `GOFLAGS=-mod=mod go test ./pow/ -run 'Seal|Challenge|Fields' -count=1`
Expected: compile failure (undefined `sealInput`, `validNonce`, `fieldScope`, …).

- [ ] **Step 3: Rewrite `pow/challenge.go`**

Keep the existing doc comments on `DefaultMinAge`, `DefaultMaxAge`, the honeypot field, `honeypotStyle` and `HoneypotStyleHash` verbatim; replace the rest with:

```go
package pow

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// A challenge is minted when a form renders and presented back when it
// is submitted. Nothing is written when one is minted: a row per render
// made every page view a serialised write against a single-writer
// SQLite file, so a crawler or a prefetcher was a denial of service
// against the one resource every submission needs. Nonces are recorded
// as spent on acceptance instead, where each row has already cost a
// solve.

const (
	DefaultMinAge = 3 * time.Second // keep the existing comment
	DefaultMaxAge = 2 * time.Hour   // keep the existing comment
)

// The form field names. Unexported because Fields renders every one and
// readChallenge reads every one, which is the only way the halves
// cannot drift.
const (
	fieldScope      = "pow_scope"
	fieldNonce      = "pow_nonce"
	fieldIssued     = "pow_issued"  // milliseconds
	fieldExpires    = "pow_expires" // milliseconds
	fieldDifficulty = "pow_difficulty"
	fieldFlags      = "pow_flags"
	fieldSeal       = "pow_seal"
	fieldCounter    = "pow_counter"
	fieldHoneypot   = "hp" // keep the existing comment on why this name
)

const (
	// flagTrapOmitted marks a recovery challenge: rendered without the
	// honeypot, so a password manager that filled it cannot fill it
	// again, and Admit skips the honeypot for this token only.
	flagTrapOmitted uint8 = 1 << 0
	// flagBound marks work bound to a submitted value. Sealed so a bound
	// challenge cannot be presented as unbound and verified without the
	// value it was bound to.
	flagBound uint8 = 1 << 1
)

// maxScopeLen bounds the one free-text sealed field. Scopes are short
// names chosen by the server; anything longer was sent by somebody
// probing, and is refused before it is hashed.
const maxScopeLen = 256

// maxDifficulty bounds the posted difficulty before it is used: SHA-256
// has 256 bits, and an int32 in the seal must not be fed a value that
// wraps.
const maxDifficulty = 256

// Challenge travels to the browser as hidden fields and back the same
// way. Every field is sealed; the seal is a signature, not encryption.
type Challenge struct {
	Scope      string
	Nonce      string
	Issued     time.Time // millisecond precision, the precision it is sealed at
	Expires    time.Time // absolute; fixed at issue so a later MaxAge cannot extend it
	Difficulty int       // 0 for a NoProof guard
	Flags      uint8
	Seal       string
}

// sealInput is the exact byte string the HMAC covers. Every variable
// length field is length-prefixed and every number fixed width, so no
// two different challenges share an input.
func sealInput(c Challenge) []byte {
	b := []byte("pow/v2")
	b = binary.AppendUvarint(b, uint64(len(c.Scope)))
	b = append(b, c.Scope...)
	b = binary.AppendUvarint(b, uint64(len(c.Nonce)))
	b = append(b, c.Nonce...)
	b = binary.BigEndian.AppendUint64(b, uint64(c.Issued.UnixMilli()))
	b = binary.BigEndian.AppendUint64(b, uint64(c.Expires.UnixMilli()))
	b = binary.BigEndian.AppendUint32(b, uint32(int32(c.Difficulty)))
	return append(b, c.Flags)
}

func sealOf(key []byte, c Challenge) string {
	m := hmac.New(sha256.New, key)
	m.Write(sealInput(c))
	return hex.EncodeToString(m.Sum(nil))
}

func sealOK(key []byte, c Challenge) bool {
	return hmac.Equal([]byte(sealOf(key, c)), []byte(c.Seal))
}

// validNonce is exactly what newChallenge mints. Checked before the
// HMAC, so the nonce is also a canonical identifier: one token, one
// string, and nothing else reaches the hash or the database.
func validNonce(s string) bool {
	if len(s) != 32 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func newChallenge(key []byte, issued time.Time, maxAge time.Duration, scope string, difficulty int, flags uint8) Challenge {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		// Every anti-abuse property here rests on the nonce being
		// unguessable; there is no degraded mode to fall back to.
		panic("rastrillo/pow: crypto/rand unavailable")
	}
	at := time.UnixMilli(issued.UnixMilli())
	c := Challenge{
		Scope:      scope,
		Nonce:      hex.EncodeToString(raw[:]),
		Issued:     at,
		Expires:    at.Add(maxAge),
		Difficulty: difficulty,
		Flags:      flags,
	}
	c.Seal = sealOf(key, c)
	return c
}

// readChallenge parses the posted fields. No seal and no nonce is
// ReasonMissing: the form was never wired, which an operator needs to
// tell apart from an attack. Anything present but malformed is
// ReasonSealInvalid, one reason for every shape, so a prober learns
// nothing from the difference.
func readChallenge(r *http.Request) (Challenge, Reason) {
	f := r.PostForm
	nonce, seal := f.Get(fieldNonce), f.Get(fieldSeal)
	if nonce == "" && seal == "" {
		return Challenge{}, ReasonMissing
	}
	scope := f.Get(fieldScope)
	if !validNonce(nonce) || len(seal) != 2*sha256.Size || len(scope) > maxScopeLen {
		return Challenge{}, ReasonSealInvalid
	}
	issued, err1 := strconv.ParseInt(f.Get(fieldIssued), 10, 64)
	expires, err2 := strconv.ParseInt(f.Get(fieldExpires), 10, 64)
	diff, err3 := strconv.Atoi(f.Get(fieldDifficulty))
	flags, err4 := strconv.ParseUint(f.Get(fieldFlags), 10, 8)
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil || diff < 0 || diff > maxDifficulty {
		return Challenge{}, ReasonSealInvalid
	}
	return Challenge{
		Scope: scope, Nonce: nonce,
		Issued: time.UnixMilli(issued), Expires: time.UnixMilli(expires),
		Difficulty: diff, Flags: uint8(flags), Seal: seal,
	}, ""
}

// Fields renders every sealed field, the empty input the solver writes
// into, and the honeypot unless this is a recovery challenge. Keep the
// existing comment about why it is one call and about the honeypot's
// accessibility contract.
func (c Challenge) Fields() template.HTML {
	var b strings.Builder
	hidden := func(name, value string) {
		fmt.Fprintf(&b, "<input type=\"hidden\" name=\"%s\" value=\"%s\">\n",
			name, template.HTMLEscapeString(value))
	}
	hidden(fieldScope, c.Scope)
	hidden(fieldNonce, c.Nonce)
	hidden(fieldIssued, strconv.FormatInt(c.Issued.UnixMilli(), 10))
	hidden(fieldExpires, strconv.FormatInt(c.Expires.UnixMilli(), 10))
	hidden(fieldDifficulty, strconv.Itoa(c.Difficulty))
	hidden(fieldFlags, strconv.Itoa(int(c.Flags)))
	hidden(fieldSeal, c.Seal)
	fmt.Fprintf(&b, "<input type=\"hidden\" name=\"%s\" value=\"\" data-pow-counter>\n", fieldCounter)
	if c.Flags&flagTrapOmitted == 0 {
		fmt.Fprintf(&b,
			"<div aria-hidden=\"true\" style=\"%s\">"+
				"<label for=\"%s\">Leave this field empty</label>"+
				"<input type=\"text\" id=\"%s\" name=\"%s\" tabindex=\"-1\" autocomplete=\"off\"></div>\n",
			honeypotStyle, fieldHoneypot, fieldHoneypot, fieldHoneypot)
	}
	return template.HTML(b.String())
}

// honeypotStyle and HoneypotStyleHash: keep both, and their comments,
// exactly as they are today. buildhandler_test.go checks the hash
// against serve.go's default policy.
```

Delete `FormAttrs`, `verifySeal` and `Challenge.expiry` (replaced by `Form.Attrs` in Task 5 and by the sealed `Expires`).

- [ ] **Step 4: Continue to Task 4** (the package compiles once `guard.go` is rewritten).

---
### Task 4: The Guard: admission, commit, check, verify, forms

**Files:**
- Modify: `pow/guard.go` (whole file), `pow/pow.go` (package doc only)
- Create: `pow/attempts.go`, `pow/form.go`
- Test: `pow/pow_test.go` (rewrite the Guard-level tests), `pow/guard_test.go` (new), `pow/store_test.go` (add the Guard-over-SQL tests)

**Interfaces:**
- Consumes: Task 2's `NonceStore`, `Execer`, `ErrNoSchema`; Task 3's `Challenge`, `newChallenge`, `sealOK`, `readChallenge`, flags, field names.
- Produces (later tasks rely on exactly these):
  - `const NoProof = -1`, `DefaultAttempts = 20`, `DefaultTracked = 100_000`
  - `type Config struct { InstanceKey string; Nonces NonceStore; Difficulty int; Bind bool; MinAge, MaxAge time.Duration; Attempts, Tracked int; ScriptURL, WorkerURL string }`
  - `func New(cfg Config) (*Guard, error)`; `func (g *Guard) Bound() bool`
  - Reasons: `ReasonHoneypot "honeypot"`, `ReasonSealInvalid "seal_invalid"`, `ReasonTooFast "too_fast"`, `ReasonTooOld "too_old"`, `ReasonShort "pow_short"`, `ReasonSpent "spent"`, `ReasonBounds "bounds"`, `ReasonUnavailable "unavailable"`, `ReasonMissing "missing"`, `ReasonAttempts "attempts"`, `ReasonBusy "busy"`
  - Errors: `ErrNoNonceStore`, `ErrEmptyInstanceKey`, `ErrNoAssets`, `ErrBindNeedsProof`, `ErrSpent`, `ErrNotAdmitted`
  - `type Want struct { Scope, Binding string }`
  - `type Admission struct { OK bool; Reason Reason; Also []Reason /* + unexported */ }`, `func (a Admission) Commit(ctx context.Context, ex Execer) error`, `func (a Admission) Recovered() bool`
  - `type Parent struct { Scope, Nonce string; Difficulty int; Issued time.Time }`
  - `func (g *Guard) Admit(r *http.Request, w Want) Admission`, `Check(r *http.Request, w Want) Admission`, `Verify(r *http.Request) (Parent, Reason, bool)`, `Sweep(now time.Time) error`
  - `func Trapped(r *http.Request) bool` (kept)
  - `type Form struct { Challenge /* + unexported */ }`; `func (g *Guard) Issue(now time.Time, scope string) Challenge`, `Form(now time.Time, scope string) Form`, `FollowOn(now time.Time, scope string) Form`, `Recovery(now time.Time, scope string) Form`
  - `func (f Form) NeedsScript() bool`, `Attrs() template.HTMLAttr`, `Script() template.HTML`, `StatusLine(text string) template.HTML` (and the promoted `Fields()`)

- [ ] **Step 1: Write the failing tests**

In `pow/pow_test.go`: keep `testDifficulty`, `solveFor`, `TestNormalizeOnlyFoldsASCII`, `TestVerifyAcceptsASolution`, `TestVerifyRefusesAnEmptyCounter`, `TestSolutionDoesNotTransferToAnotherBinding`, `TestSolutionDoesNotTransferToAnotherNonce`, `TestFieldsCarriesTheHoneypotContract`, `TestHoneypotStyleHashMatchesTheStyle`, `TestAssetsCarriesBothHalvesOfTheBrowserSide`. Delete every other test in it (they test `Check(r, binding)`, `FormAttrs`, `verifySeal` and the v1 seal, all removed); their properties are re-pinned below. Replace `newTestGuard` and add helpers:

```go
// t0 is a fixed clock: every Guard-level test sets g.now so ages are
// exact rather than racing the wall clock.
var t0 = time.UnixMilli(1_800_000_000_000)

func newTestGuard(t *testing.T, mut func(*Config)) *Guard {
	t.Helper()
	cfg := Config{
		InstanceKey: "test-instance-key",
		Nonces:      &memNonces{spent: map[string]time.Time{}, now: func() time.Time { return t0.Add(time.Minute) }},
		Difficulty:  testDifficulty,
		MinAge:      time.Second,
		ScriptURL:   "/pow/pow.js",
		WorkerURL:   "/pow/pow-worker.js",
	}
	if mut != nil {
		mut(&cfg)
	}
	g, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	g.now = func() time.Time { return t0.Add(time.Minute) }
	return g
}

// post builds the request a browser would send for f: every sealed
// field, a solved counter when f needs proof, and mut's edits.
func post(t *testing.T, f Form, binding string, mut func(url.Values)) *http.Request {
	t.Helper()
	v := valuesOf(f.Challenge)
	if f.Difficulty > 0 {
		v.Set(fieldCounter, solveFor(t, f.Nonce, binding, f.Difficulty))
	}
	if mut != nil {
		mut(v)
	}
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(v.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return r
}
```

Create `pow/guard_test.go`:

```go
package pow

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"
)

const scope = "test/scope"

func TestAdmitAcceptsAGoodSubmissionAndWritesNothing(t *testing.T) {
	g := newTestGuard(t, nil)
	f := g.Form(t0, scope)
	a := g.Admit(post(t, f, "", nil), Want{Scope: scope})
	if !a.OK {
		t.Fatalf("Admit refused a good submission: %s %v", a.Reason, a.Also)
	}
	if spent, _ := g.nonces.Spent(context.Background(), f.Nonce); spent {
		t.Fatal("Admit spent the nonce: a validation error afterwards would burn the visitor's solve")
	}
}

func TestCommitSpendsOnceAndAReplayIsRefused(t *testing.T) {
	g := newTestGuard(t, nil)
	f := g.Form(t0, scope)
	a := g.Admit(post(t, f, "", nil), Want{Scope: scope})
	if err := a.Commit(context.Background(), nil); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if err := a.Commit(context.Background(), nil); !errors.Is(err, ErrSpent) {
		t.Fatalf("second Commit = %v, want ErrSpent", err)
	}
	if b := g.Admit(post(t, f, "", nil), Want{Scope: scope}); b.OK || b.Reason != ReasonSpent {
		t.Fatalf("replay = %v %s, want refused as spent", b.OK, b.Reason)
	}
}

func TestCommitOfARefusalIsAnError(t *testing.T) {
	g := newTestGuard(t, nil)
	a := g.Admit(post(t, g.Form(t0, scope), "", nil), Want{Scope: "other"})
	if err := a.Commit(context.Background(), nil); !errors.Is(err, ErrNotAdmitted) {
		t.Fatalf("Commit of a refusal = %v, want ErrNotAdmitted", err)
	}
}

func TestScopeMismatchIsSealInvalid(t *testing.T) {
	g := newTestGuard(t, nil)
	a := g.Admit(post(t, g.Form(t0, "apply:41"), "", nil), Want{Scope: "apply:42"})
	if a.OK || a.Reason != ReasonSealInvalid {
		t.Fatalf("token for event 41 on event 42 = %v %s", a.OK, a.Reason)
	}
}

func TestMissingFieldsAreReasonMissing(t *testing.T) {
	g := newTestGuard(t, nil)
	r := post(t, g.Form(t0, scope), "", func(v url.Values) {
		for k := range v {
			if strings.HasPrefix(k, "pow_") {
				v.Del(k)
			}
		}
	})
	if a := g.Admit(r, Want{Scope: scope}); a.Reason != ReasonMissing {
		t.Fatalf("no challenge = %s, want missing", a.Reason)
	}
}

func TestHoneypotFirstAndCheapFailuresInAlso(t *testing.T) {
	g := newTestGuard(t, nil)
	g.now = func() time.Time { return t0 } // too fast as well
	r := post(t, g.Form(t0, scope), "", func(v url.Values) {
		v.Set(fieldHoneypot, "https://example.com")
		v.Set(fieldCounter, "") // an empty counter never verifies; "0" would, one time in 1024
	})
	a := g.Admit(r, Want{Scope: scope})
	if a.Reason != ReasonHoneypot {
		t.Fatalf("first reason = %s, want honeypot", a.Reason)
	}
	for _, want := range []Reason{ReasonTooFast, ReasonShort} {
		found := false
		for _, r := range a.Also {
			found = found || r == want
		}
		if !found {
			t.Errorf("Also = %v, missing %s", a.Also, want)
		}
	}
}

func TestMinimumAgeIsMilliseconds(t *testing.T) {
	g := newTestGuard(t, func(c *Config) { c.MinAge = 500 * time.Millisecond })
	issued := time.UnixMilli(t0.UnixMilli()/1000*1000 + 999) // late in a second
	f := g.Form(issued, scope)
	g.now = func() time.Time { return issued.Add(400 * time.Millisecond) }
	if a := g.Admit(post(t, f, "", nil), Want{Scope: scope}); a.Reason != ReasonTooFast {
		t.Fatalf("400ms after issue = %s, want too_fast", a.Reason)
	}
	g.now = func() time.Time { return issued.Add(600 * time.Millisecond) }
	if a := g.Admit(post(t, f, "", nil), Want{Scope: scope}); !a.OK {
		t.Fatalf("600ms after issue refused: %s", a.Reason)
	}
}

func TestExpiryIsSealedAndCanOnlyShorten(t *testing.T) {
	short := newTestGuard(t, func(c *Config) { c.MaxAge = time.Minute })
	f := short.Form(t0, scope)
	long := newTestGuard(t, func(c *Config) { c.MaxAge = 2 * time.Hour })
	long.now = func() time.Time { return t0.Add(10 * time.Minute) }
	if a := long.Admit(post(t, f, "", nil), Want{Scope: scope}); a.Reason != ReasonTooOld {
		t.Fatalf("a 1-minute token at 10 minutes under a 2h guard = %s, want too_old: a longer MaxAge revived it", a.Reason)
	}
	g := newTestGuard(t, func(c *Config) { c.MaxAge = 2 * time.Hour })
	tokenLong := g.Form(t0, scope)
	shortNow := newTestGuard(t, func(c *Config) { c.MaxAge = time.Minute })
	if a := shortNow.Admit(post(t, tokenLong, "", nil), Want{Scope: scope}); a.Reason != ReasonTooOld {
		t.Fatalf("a 2h token under a 1-minute guard = %s, want too_old", a.Reason)
	}
}

func TestALowerDifficultyTokenIsRefused(t *testing.T) {
	easy := newTestGuard(t, func(c *Config) { c.Difficulty = NoProof })
	hard := newTestGuard(t, nil)
	if a := hard.Admit(post(t, easy.Form(t0, scope), "", nil), Want{Scope: scope}); a.Reason != ReasonSealInvalid {
		t.Fatalf("NoProof token on a proof guard = %s, want seal_invalid", a.Reason)
	}
}

func TestBoundGuardsNeedTheBinding(t *testing.T) {
	g := newTestGuard(t, func(c *Config) { c.Bind = true })
	f := g.Form(t0, scope)
	if a := g.Admit(post(t, f, "a@example.com", nil), Want{Scope: scope, Binding: "A@Example.com "}); !a.OK {
		t.Fatalf("bound submission refused: %s", a.Reason)
	}
	f2 := g.Form(t0, scope)
	if a := g.Admit(post(t, f2, "a@example.com", nil), Want{Scope: scope, Binding: "b@example.com"}); a.Reason != ReasonShort {
		t.Fatalf("proof for another address = %s, want pow_short", a.Reason)
	}
	unbound := newTestGuard(t, nil)
	if a := unbound.Admit(post(t, f2, "a@example.com", nil), Want{Scope: scope}); a.Reason != ReasonSealInvalid {
		t.Fatalf("bound token on an unbound guard = %s, want seal_invalid", a.Reason)
	}
}

func TestAttemptsAreCappedAcrossRollbacks(t *testing.T) {
	g := newTestGuard(t, func(c *Config) { c.Attempts = 3 })
	f := g.Form(t0, scope)
	for i := 0; i < 3; i++ {
		a := g.Admit(post(t, f, "", nil), Want{Scope: scope})
		if !a.OK {
			t.Fatalf("attempt %d refused: %s", i+1, a.Reason)
		}
		// Admission spent and the caller's transaction rolled back:
		// MemoryNonces cannot roll back, so model it by not committing.
		// Either way the attempt entry must not be released.
	}
	if a := g.Admit(post(t, f, "", nil), Want{Scope: scope}); a.Reason != ReasonAttempts {
		t.Fatalf("fourth attempt = %s, want attempts", a.Reason)
	}
}

func TestCapacityRefusesNewTokensAndNeverEvicts(t *testing.T) {
	g := newTestGuard(t, func(c *Config) { c.Tracked = 2; c.Attempts = 5 })
	a, b, c := g.Form(t0, scope), g.Form(t0, scope), g.Form(t0, scope)
	for _, f := range []Form{a, b} {
		if adm := g.Admit(post(t, f, "", nil), Want{Scope: scope}); !adm.OK {
			t.Fatalf("filling: %s", adm.Reason)
		}
	}
	if adm := g.Admit(post(t, c, "", nil), Want{Scope: scope}); adm.Reason != ReasonBusy {
		t.Fatalf("third token at capacity = %s, want busy", adm.Reason)
	}
	if adm := g.Admit(post(t, a, "", nil), Want{Scope: scope}); !adm.OK {
		t.Fatalf("a tracked token was refused at capacity: %s", adm.Reason)
	}
}

func TestCheckBypassesTheAttemptMap(t *testing.T) {
	g := newTestGuard(t, func(c *Config) { c.Tracked = 1 })
	if adm := g.Admit(post(t, g.Form(t0, "other"), "", nil), Want{Scope: "other"}); !adm.OK {
		t.Fatal(adm.Reason)
	}
	f := g.Form(t0, scope)
	if adm := g.Check(post(t, f, "", nil), Want{Scope: scope}); !adm.OK {
		t.Fatalf("Check answered %s while another scope filled the map", adm.Reason)
	}
	if spent, _ := g.nonces.Spent(context.Background(), f.Nonce); !spent {
		t.Fatal("Check did not spend")
	}
	if adm := g.Check(post(t, f, "", nil), Want{Scope: scope}); adm.Reason != ReasonSpent {
		t.Fatalf("second Check = %s, want spent", adm.Reason)
	}
}

func TestRecoveryIsTraplessAndAdmissibleAtOnce(t *testing.T) {
	g := newTestGuard(t, func(c *Config) { c.Difficulty = NoProof; c.MinAge = 3 * time.Second })
	g.now = func() time.Time { return t0 }
	first := g.Form(t0, scope)
	refused := g.Admit(post(t, first, "", func(v url.Values) { v.Set(fieldHoneypot, "x") }), Want{Scope: scope})
	if refused.OK || refused.Recovered() {
		t.Fatal("an original token reported recovered")
	}
	rec := g.Recovery(t0, scope)
	if strings.Contains(string(rec.Fields()), `name="hp"`) {
		t.Fatal("Recovery rendered the trap")
	}
	a := g.Admit(post(t, rec, "", func(v url.Values) { v.Set(fieldHoneypot, "filled anyway") }), Want{Scope: scope})
	if !a.OK {
		t.Fatalf("recovery refused at once: %s %v", a.Reason, a.Also)
	}
	if !a.Recovered() {
		t.Fatal("Recovered() false on a recovery token")
	}
	refusedRec := g.Admit(post(t, g.Recovery(t0, scope), "", nil), Want{Scope: "other"})
	if refusedRec.Recovered() {
		t.Fatal("Recovered() true before the seal verified")
	}
}

func TestFollowOnIsAdmissibleAtOnceAndKeepsTheTrap(t *testing.T) {
	g := newTestGuard(t, func(c *Config) { c.MinAge = 3 * time.Second })
	g.now = func() time.Time { return t0 }
	f := g.FollowOn(t0, scope)
	if !strings.Contains(string(f.Fields()), `name="hp"`) {
		t.Fatal("FollowOn dropped the trap; only Recovery may")
	}
	if a := g.Admit(post(t, f, "", nil), Want{Scope: scope}); !a.OK {
		t.Fatalf("FollowOn refused at once: %s", a.Reason)
	}
}

func TestVerifyNeitherCountsNorSpendsAndReturnsTheScope(t *testing.T) {
	minter := newTestGuard(t, func(c *Config) { c.Difficulty = 12 })
	other := newTestGuard(t, func(c *Config) { c.Difficulty = NoProof; c.Tracked = 1 })
	f := minter.Form(t0, "apply:41")
	for i := 0; i < 3; i++ {
		p, reason, ok := other.Verify(post(t, f, "", nil))
		if !ok {
			t.Fatalf("Verify by another guard sharing the key refused: %s", reason)
		}
		if p.Scope != "apply:41" || p.Nonce != f.Nonce || p.Difficulty != 12 {
			t.Fatalf("Parent = %+v", p)
		}
	}
	if spent, _ := minter.nonces.Spent(context.Background(), f.Nonce); spent {
		t.Fatal("Verify spent the parent")
	}
	bound := newTestGuard(t, func(c *Config) { c.Bind = true })
	if _, reason, ok := other.Verify(post(t, bound.Form(t0, "x"), "a@b.c", nil)); ok || reason != ReasonSealInvalid {
		t.Fatalf("Verify of a bound token = %v %s, want seal_invalid", ok, reason)
	}
}

func TestRotatedKeyRefusesThenRecovers(t *testing.T) {
	// Review focus 3.
	old := newTestGuard(t, func(c *Config) { c.InstanceKey = "old" })
	rotated := newTestGuard(t, func(c *Config) { c.InstanceKey = "new" })
	if a := rotated.Admit(post(t, old.Form(t0, scope), "", nil), Want{Scope: scope}); a.Reason != ReasonSealInvalid {
		t.Fatalf("old key's token = %s, want seal_invalid", a.Reason)
	}
	if a := rotated.Admit(post(t, rotated.Recovery(t0.Add(time.Minute), scope), "", nil), Want{Scope: scope}); !a.OK {
		t.Fatalf("recovery under the new key refused: %s", a.Reason)
	}
}

func TestTwoTabsBothSucceed(t *testing.T) {
	// Review focus 5: each render mints its own token.
	g := newTestGuard(t, nil)
	a, b := g.Form(t0, scope), g.Form(t0, scope)
	for i, f := range []Form{a, b} {
		if adm := g.Check(post(t, f, "", nil), Want{Scope: scope}); !adm.OK {
			t.Fatalf("tab %d refused: %s", i+1, adm.Reason)
		}
	}
}

func TestNewRefusesWhatCannotWork(t *testing.T) {
	base := Config{InstanceKey: "k", Nonces: MemoryNonces(), ScriptURL: "/a.js", WorkerURL: "/w.js"}
	cases := map[string]struct {
		mut  func(*Config)
		want error
	}{
		"no key":          {func(c *Config) { c.InstanceKey = "" }, ErrEmptyInstanceKey},
		"no store":        {func(c *Config) { c.Nonces = nil }, ErrNoNonceStore},
		"no script":       {func(c *Config) { c.ScriptURL = "" }, ErrNoAssets},
		"no worker":       {func(c *Config) { c.WorkerURL = "" }, ErrNoAssets},
		"bind and noproof": {func(c *Config) { c.Bind = true; c.Difficulty = NoProof }, ErrBindNeedsProof},
	}
	for name, tc := range cases {
		cfg := base
		tc.mut(&cfg)
		if _, err := New(cfg); !errors.Is(err, tc.want) {
			t.Errorf("%s: New = %v, want %v", name, err, tc.want)
		}
	}
	np := base
	np.Difficulty, np.ScriptURL, np.WorkerURL = NoProof, "", ""
	if _, err := New(np); err != nil {
		t.Errorf("NoProof without assets refused: %v", err)
	}
}

func TestFormRendering(t *testing.T) {
	g := newTestGuard(t, func(c *Config) { c.MinAge = 3 * time.Second })
	f := g.Form(t0, scope)
	attrs := string(f.Attrs())
	for _, want := range []string{`data-pow-form`, `data-pow-nonce="` + f.Nonce + `"`, `data-pow-worker="/pow/pow-worker.js"`, `data-pow-min-age="3000"`} {
		if !strings.Contains(attrs, want) {
			t.Errorf("Attrs = %s, missing %s", attrs, want)
		}
	}
	if fo := g.FollowOn(t0, scope); !strings.Contains(string(fo.Attrs()), `data-pow-min-age="0"`) {
		t.Errorf("FollowOn Attrs = %s, want min age 0: a follow-on form must never be held", fo.Attrs())
	}
	if s := string(f.Script()); s != `<script type="module" src="/pow/pow.js"></script>` {
		t.Errorf("Script = %s", s)
	}
	if s := string(f.StatusLine("Reload <now>")); s != `<p data-pow-status>Reload &lt;now&gt;</p>` {
		t.Errorf("StatusLine = %s", s)
	}
	np := newTestGuard(t, func(c *Config) { c.Difficulty = NoProof })
	nf := np.Form(t0, scope)
	if nf.NeedsScript() || nf.Attrs() != "" || nf.Script() != "" || nf.StatusLine("x") != "" {
		t.Error("a NoProof form renders script machinery: its submit would wait for a module that never comes")
	}
	if !strings.Contains(string(nf.Fields()), `name="pow_seal"`) {
		t.Error("a NoProof form lost its sealed token")
	}
}
```

Append to `pow/store_test.go` (package `pow_test`, real SQLite, one writer connection):

```go
func newSQLGuard(t *testing.T, d *db.DB) *pow.Guard {
	t.Helper()
	g, err := pow.New(pow.Config{
		InstanceKey: "k", Nonces: pow.SQLNonces(d.Writer()), Difficulty: pow.NoProof,
		MinAge: time.Millisecond,
	})
	if err != nil {
		t.Fatalf("pow.New: %v", err)
	}
	return g
}

func formPost(f pow.Form) *http.Request {
	page := string(f.Fields())
	v := url.Values{}
	for _, m := range regexp.MustCompile(`name="([^"]+)" value="([^"]*)"`).FindAllStringSubmatch(page, -1) {
		v.Set(m[1], html.UnescapeString(m[2]))
	}
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(v.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return r
}

func TestNewRefusesAMissingTable(t *testing.T) {
	d := openTestDB(t)
	_, err := pow.New(pow.Config{InstanceKey: "k", Nonces: pow.SQLNonces(d.Writer()), Difficulty: pow.NoProof})
	if !errors.Is(err, pow.ErrNoSchema) {
		t.Fatalf("New over an unmigrated database = %v, want ErrNoSchema", err)
	}
}

func TestCommitRollsBackWithTheCallersTransaction(t *testing.T) {
	_, d := newTestStore(t)
	g := newSQLGuard(t, d)
	f := g.Form(time.Now().Add(-time.Second), "s")
	ctx := context.Background()
	a := g.Admit(formPost(f), pow.Want{Scope: "s"})
	tx, _ := d.Writer().BeginTx(ctx, nil)
	if err := a.Commit(ctx, tx); err != nil {
		t.Fatal(err)
	}
	tx.Rollback()
	b := g.Admit(formPost(f), pow.Want{Scope: "s"})
	if !b.OK {
		t.Fatalf("after a rollback the token was refused: %s", b.Reason)
	}
}

func TestConcurrentCommitsHaveOneWinner(t *testing.T) {
	_, d := newTestStore(t)
	g := newSQLGuard(t, d)
	f := g.Form(time.Now().Add(-time.Second), "s")
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a := g.Admit(formPost(f), pow.Want{Scope: "s"})
			if !a.OK {
				return
			}
			if a.Commit(context.Background(), nil) == nil {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("%d commits won, want 1", wins)
	}
}

func TestCommitThroughAGormTransaction(t *testing.T) {
	// GORM's *gorm.DB is not an Execer; its transaction's ConnPool is.
	// pow does not import GORM, so this is the documented way in.
	_, d := newTestStore(t)
	g := newSQLGuard(t, d)
	f := g.Form(time.Now().Add(-time.Second), "s")
	a := g.Admit(formPost(f), pow.Want{Scope: "s"})
	err := d.G.Transaction(func(tx *gorm.DB) error {
		return a.Commit(context.Background(), tx.Statement.ConnPool)
	})
	if err != nil {
		t.Fatalf("Commit through tx.Statement.ConnPool: %v", err)
	}
	if b := g.Admit(formPost(f), pow.Want{Scope: "s"}); b.Reason != pow.ReasonSpent {
		t.Fatalf("after a GORM commit the token = %s, want spent", b.Reason)
	}
}

func TestErrSpentInsideATransactionReturnsPromptly(t *testing.T) {
	// A lookup through another handle while this transaction holds the
	// only writer would wait on itself forever.
	_, d := newTestStore(t)
	g := newSQLGuard(t, d)
	f := g.Form(time.Now().Add(-time.Second), "s")
	ctx := context.Background()
	a := g.Admit(formPost(f), pow.Want{Scope: "s"})
	if err := a.Commit(ctx, nil); err != nil {
		t.Fatal(err)
	}
	tx, err := d.Writer().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	done := make(chan error, 1)
	go func() { done <- a.Commit(ctx, tx) }()
	select {
	case err := <-done:
		if !errors.Is(err, pow.ErrSpent) {
			t.Fatalf("Commit = %v, want ErrSpent", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Commit inside a transaction hung: it is waiting on its own connection")
	}
}
```

(Add `errors`, `html`, `net/http`, `net/http/httptest`, `net/url`, `regexp`, `strings` and `gorm.io/gorm` to that file's imports. `d.G` is the `*gorm.DB` that `db.Open` returns alongside the writer pool.)

- [ ] **Step 2: Run them to see them fail**

Run: `GOFLAGS=-mod=mod go test ./pow/ -count=1`
Expected: compile failure (`Admit`, `Form`, `Want`, … undefined).

- [ ] **Step 3: Write `pow/attempts.go`**

```go
package pow

import (
	"sync"
	"time"
)

// attempts counts admissions per token, in process. It bounds handler
// work, not replay: a solved token that keeps failing the handler's own
// validation would otherwise be free work for its whole lifetime.
// Replay is the durable ledger's job, so losing this on restart is fine.
type attempts struct {
	mu        sync.Mutex
	limit     int
	max       int
	m         map[string]attempt
	nextPrune time.Time
}

type attempt struct {
	n       int
	expires time.Time
}

func newAttempts(limit, max int) *attempts {
	return &attempts{limit: limit, max: max, m: make(map[string]attempt)}
}

// take counts one admission. Entries live until their token expires,
// never until a commit: the commit's insert succeeding says nothing
// about the caller's transaction, and releasing on it let admit, spend,
// business refusal, rollback repeat forever on one solve.
//
// At capacity a NEW token is refused and nothing live is evicted.
// Evicting the oldest would let an attacker cycling one more token than
// capacity reset every allowance without a new solve.
func (t *attempts) take(nonce string, expires, now time.Time) Reason {
	t.mu.Lock()
	defer t.mu.Unlock()
	// At most once a second: at capacity every new token would otherwise
	// pay a scan of the whole map.
	if !now.Before(t.nextPrune) {
		for k, e := range t.m {
			if now.After(e.expires) {
				delete(t.m, k)
			}
		}
		t.nextPrune = now.Add(time.Second)
	}
	e, ok := t.m[nonce]
	if !ok {
		if len(t.m) >= t.max {
			return ReasonBusy
		}
		e = attempt{expires: expires}
	}
	if e.n >= t.limit {
		return ReasonAttempts
	}
	e.n++
	t.m[nonce] = e
	return ""
}
```

- [ ] **Step 4: Rewrite `pow/guard.go`**

```go
package pow

import (
	"context"
	"crypto/sha256"
	"errors"
	"net/http"
	"strings"
	"time"
)

// Reason is why a submission was refused. The set is closed so a log
// of refusals stays countable and nothing a submitter typed reaches it.
type Reason string

const (
	ReasonHoneypot    Reason = "honeypot"
	ReasonSealInvalid Reason = "seal_invalid"
	ReasonTooFast     Reason = "too_fast"
	ReasonTooOld      Reason = "too_old"
	ReasonShort       Reason = "pow_short"
	ReasonSpent       Reason = "spent"
	// ReasonBounds: a body that would not parse as a form at all.
	ReasonBounds Reason = "bounds"
	// ReasonUnavailable: the nonce store failed. Still a refusal:
	// letting a submission through when replay protection is unreachable
	// turns a database outage into unlimited replay.
	ReasonUnavailable Reason = "unavailable"
	// ReasonMissing: no challenge was posted at all. The form was never
	// wired, or predates this release; an operator needs to tell that
	// apart from an attack, so it is not seal_invalid.
	ReasonMissing Reason = "missing"
	// ReasonAttempts: this token has been admitted Config.Attempts times.
	ReasonAttempts Reason = "attempts"
	// ReasonBusy: Config.Tracked tokens are already being counted.
	ReasonBusy Reason = "busy"
)

const (
	// NoProof is Config.Difficulty for a token-only form: a sealed,
	// single-use challenge and the honeypot, no proof of work, and no
	// JavaScript needed to submit it.
	NoProof          = -1
	DefaultAttempts  = 20
	DefaultTracked   = 100_000
)

var (
	ErrNoNonceStore     = errors.New("rastrillo/pow: Config.Nonces must not be nil") // keep the existing comment
	ErrEmptyInstanceKey = errors.New("rastrillo/pow: Config.InstanceKey must not be empty")
	// ErrNoAssets: a proof-of-work form the browser cannot complete must
	// not boot. A URL being set does not prove it is served; the status
	// line covers the visitor's side of that.
	ErrNoAssets = errors.New("rastrillo/pow: Config.ScriptURL and Config.WorkerURL are required unless Difficulty is NoProof")
	// ErrBindNeedsProof: NoProof has no work to bind, and a Bind that is
	// silently ignored is a setting somebody believes is protecting them.
	ErrBindNeedsProof = errors.New("rastrillo/pow: Config.Bind needs proof of work; it means nothing with NoProof")
	// ErrSpent: the token was spent by an identical request, or expired
	// before the spend. Refuse; never 500. Commit does not look further
	// to tell the two apart (see Execer).
	ErrSpent = errors.New("rastrillo/pow: challenge spent or expired")
	// ErrNotAdmitted: Commit of a refused admission. A handler that went
	// on past a refusal would otherwise create its row with nothing
	// consumed.
	ErrNotAdmitted = errors.New("rastrillo/pow: commit of a refused admission")
)

// Config configures New. InstanceKey and Nonces are required, and the
// asset URLs unless Difficulty is NoProof.
type Config struct {
	// InstanceKey seals challenges, derived under its own label so a
	// token minted by another subsystem never verifies here.
	InstanceKey string
	Nonces      NonceStore
	// Difficulty defaults to DefaultDifficulty; NoProof for token-only.
	Difficulty int
	// Bind ties the work to the [data-pow-binding] input. Off by
	// default: with single-use tokens one solve already buys one
	// submission, and binding means solving cannot start before submit.
	Bind bool
	// MinAge and MaxAge default to DefaultMinAge and DefaultMaxAge.
	// MaxAge is read once, at issue, and sealed into the token.
	MinAge, MaxAge time.Duration
	// Attempts and Tracked bound Admit's per-token counting; defaults
	// DefaultAttempts and DefaultTracked. Check never counts.
	Attempts, Tracked int
	// ScriptURL and WorkerURL are the fingerprinted URLs of pow.js and
	// pow-worker.js as the app serves pow.Assets().
	ScriptURL, WorkerURL string
}

// Guard is a front door. One Guard serves many forms: the scope passed
// to Form and Want keeps their tokens apart.
type Guard struct {
	key                  []byte
	nonces               NonceStore
	difficulty           int // 0: no proof
	bind                 bool
	minAge, maxAge       time.Duration
	scriptURL, workerURL string
	attempts             *attempts
	now                  func() time.Time
}

// New returns a Guard, or an error naming what is missing. It checks
// the store is ready, so apply pow.Schema before calling it.
func New(cfg Config) (*Guard, error) {
	if cfg.InstanceKey == "" {
		return nil, ErrEmptyInstanceKey
	}
	if cfg.Nonces == nil {
		return nil, ErrNoNonceStore
	}
	key := sha256.Sum256([]byte("rastrillo/pow/challenge\x00" + cfg.InstanceKey))
	g := &Guard{
		key: key[:], nonces: cfg.Nonces, bind: cfg.Bind,
		minAge: cfg.MinAge, maxAge: cfg.MaxAge,
		scriptURL: cfg.ScriptURL, workerURL: cfg.WorkerURL,
		now: time.Now,
	}
	switch {
	case cfg.Difficulty == NoProof:
		g.difficulty = 0
	case cfg.Difficulty <= 0:
		g.difficulty = DefaultDifficulty
	default:
		g.difficulty = cfg.Difficulty
	}
	if g.difficulty > 0 && (cfg.ScriptURL == "" || cfg.WorkerURL == "") {
		return nil, ErrNoAssets
	}
	if cfg.Bind && g.difficulty == 0 {
		return nil, ErrBindNeedsProof
	}
	if g.minAge <= 0 {
		g.minAge = DefaultMinAge
	}
	if g.maxAge <= 0 {
		g.maxAge = DefaultMaxAge
	}
	limit, max := cfg.Attempts, cfg.Tracked
	if limit <= 0 {
		limit = DefaultAttempts
	}
	if max <= 0 {
		max = DefaultTracked
	}
	g.attempts = newAttempts(limit, max)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := cfg.Nonces.Ready(ctx); err != nil {
		return nil, err
	}
	return g, nil
}

// Bound reports Config.Bind, for a package that renders forms it cannot
// bind (auth's sign-in) and must refuse a bound Guard at boot.
func (g *Guard) Bound() bool { return g.bind }

// Want is what the server expects of a submission: the scope it issued
// the form under, and, for a bound Guard, the submitted value.
type Want struct {
	Scope   string
	Binding string
}

// Admission is Admit's verdict, and the handle that spends it.
type Admission struct {
	OK     bool
	Reason Reason   // the first failure
	Also   []Reason // every other cheap failure, for the log
	g      *Guard
	c      Challenge
	sealed bool
}

func (a *Admission) fail(r Reason) {
	a.OK = false
	if a.Reason == "" {
		a.Reason = r
		return
	}
	a.Also = append(a.Also, r)
}

// Recovered reports that the posted token was a recovery token. Only
// after the seal verified: an unverified flag is a number the submitter
// chose. A handler that re-renders uses it to keep recovery sticky, or
// a trapless attempt that fails on a wrong password comes back with the
// trap and is refused again.
func (a Admission) Recovered() bool { return a.sealed && a.c.Flags&flagTrapOmitted != 0 }

// Parent is a token Verify authenticated, for a request made on a
// parent form's behalf.
type Parent struct {
	Scope      string
	Nonce      string // canonical: key allowances by it
	Difficulty int
	Issued     time.Time
}

// Admit decides and writes nothing; Commit spends, inside the caller's
// transaction. It reads the form, so bound the body with
// http.MaxBytesReader first.
//
// Order: honeypot, then the seal before anything it vouches for, then
// every cheap check (each failure recorded, so the log shows what a
// refusal was also wrong about), and only then, if all passed, the one
// database read and the attempt count.
func (g *Guard) Admit(r *http.Request, w Want) Admission { return g.admit(r, w, true) }

func (g *Guard) admit(r *http.Request, w Want, count bool) Admission {
	a := Admission{g: g}
	if err := r.ParseForm(); err != nil {
		a.fail(ReasonBounds)
		return a
	}
	c, reason := readChallenge(r)
	if reason != "" {
		a.fail(reason)
		return a
	}
	// The flag is unverified here; if it lies, the seal check below
	// refuses the request anyway.
	if c.Flags&flagTrapOmitted == 0 && Trapped(r) {
		a.fail(ReasonHoneypot)
	}
	if !sealOK(g.key, c) || c.Scope != w.Scope {
		a.fail(ReasonSealInvalid)
		return a
	}
	a.c, a.sealed = c, true
	now := g.now()
	if now.Sub(c.Issued) < g.minAge {
		a.fail(ReasonTooFast)
	}
	// Sealed expiry, and never longer than this Guard's MaxAge: a deploy
	// that raised MaxAge must not revive tokens already spent and swept.
	if now.After(c.Expires) || c.Expires.Sub(c.Issued) > g.maxAge {
		a.fail(ReasonTooOld)
	}
	if c.Difficulty < g.difficulty || (c.Flags&flagBound != 0) != g.bind {
		a.fail(ReasonSealInvalid)
	}
	if c.Difficulty > 0 {
		binding := ""
		if g.bind {
			binding = w.Binding
		}
		if !Verify(c.Nonce, binding, r.PostFormValue(fieldCounter), c.Difficulty) {
			a.fail(ReasonShort)
		}
	}
	if a.Reason != "" {
		return a
	}
	spent, err := g.nonces.Spent(r.Context(), c.Nonce)
	switch {
	case err != nil:
		a.fail(ReasonUnavailable)
		return a
	case spent:
		a.fail(ReasonSpent)
		return a
	}
	if count {
		if reason := g.attempts.take(c.Nonce, c.Expires, now); reason != "" {
			a.fail(reason)
			return a
		}
	}
	a.OK = true
	return a
}

// Commit spends the admitted token on ex: the caller's transaction, so
// a validation failure never reaches it and a rollback undoes it. nil
// spends on the store's own handle.
func (a Admission) Commit(ctx context.Context, ex Execer) error {
	if !a.OK || a.g == nil {
		return ErrNotAdmitted
	}
	fresh, err := a.g.nonces.Spend(ctx, ex, a.c.Nonce, a.c.Expires)
	if err != nil {
		return err
	}
	if !fresh {
		return ErrSpent
	}
	return nil
}

// Check is Admit and Commit at once, on the store's own handle, without
// the attempt count: it spends immediately, so there is nothing
// uncommitted to count, and a map filled through Admit by another form
// cannot make it answer busy. For a handler that redirects after every
// POST or has no transaction of its own, where the spend must land
// before mail or a probe does.
func (g *Guard) Check(r *http.Request, w Want) Admission {
	a := g.admit(r, w, false)
	if !a.OK {
		return a
	}
	fresh, err := g.nonces.Spend(r.Context(), nil, a.c.Nonce, a.c.Expires)
	switch {
	case err != nil:
		a.fail(ReasonUnavailable)
	case !fresh:
		a.fail(ReasonSpent)
	}
	return a
}

// Verify authenticates a parent token for a request made on its form's
// behalf (an email check, an upload): seal, expiry, proof at the
// sealed difficulty, not spent. It neither counts nor spends, and
// returns the authenticated scope for the caller to authorise. Any
// Guard sharing the instance key verifies any of the app's unbound
// tokens. A bound token is refused: its proof cannot be checked without
// the value it was bound to, which such a request does not carry.
func (g *Guard) Verify(r *http.Request) (Parent, Reason, bool) {
	if err := r.ParseForm(); err != nil {
		return Parent{}, ReasonBounds, false
	}
	c, reason := readChallenge(r)
	if reason != "" {
		return Parent{}, reason, false
	}
	if !sealOK(g.key, c) || c.Flags&flagBound != 0 {
		return Parent{}, ReasonSealInvalid, false
	}
	now := g.now()
	if now.After(c.Expires) || c.Expires.Sub(c.Issued) > g.maxAge {
		return Parent{}, ReasonTooOld, false
	}
	if c.Difficulty > 0 && !Verify(c.Nonce, "", r.PostFormValue(fieldCounter), c.Difficulty) {
		return Parent{}, ReasonShort, false
	}
	spent, err := g.nonces.Spent(r.Context(), c.Nonce)
	if err != nil {
		return Parent{}, ReasonUnavailable, false
	}
	if spent {
		return Parent{}, ReasonSpent, false
	}
	return Parent{Scope: c.Scope, Nonce: c.Nonce, Difficulty: c.Difficulty, Issued: c.Issued}, "", true
}

// Trapped reports whether the honeypot was filled.
func Trapped(r *http.Request) bool {
	return strings.TrimSpace(r.PostFormValue(fieldHoneypot)) != ""
}

// Sweep drops spent rows past their sealed expiry and the margin, at
// most a batch per call. Call it from a tick the app already has.
func (g *Guard) Sweep(now time.Time) error { return g.nonces.Sweep(now) }
```

- [ ] **Step 5: Write `pow/form.go`**

```go
package pow

import (
	"fmt"
	"html/template"
	"time"
)

// Form is what a template renders: a challenge plus how the browser
// should treat it. auth, password and apps share this one shape.
type Form struct {
	Challenge
	scriptURL, workerURL string
	bound                bool
	minAgeLeft           time.Duration
}

// Issue mints a challenge and writes nothing.
func (g *Guard) Issue(now time.Time, scope string) Challenge {
	return g.form(now, now, scope, 0).Challenge
}

// Form is an ordinary form.
func (g *Guard) Form(now time.Time, scope string) Form { return g.form(now, now, scope, 0) }

// FollowOn is for a form the visitor reaches with nothing left to type
// (a prefilled details form, a confirm screen, and their re-renders):
// issued MinAge ago, so it is usable the instant it renders. It keeps
// the trap.
func (g *Guard) FollowOn(now time.Time, scope string) Form {
	return g.form(now, now.Add(-g.minAge), scope, 0)
}

// Recovery is the form to re-render after a refusal: always trapless
// and always follow-on, whatever the reason, because varying it by
// reason let the fixes undo each other (a fast visitor whose password
// manager fills the trap alternated honeypot and too_fast forever).
// It recovers the challenge, never the submission: whether the page
// around it is refilled with what the visitor typed is the app's call,
// and needs the app's idempotency.
func (g *Guard) Recovery(now time.Time, scope string) Form {
	return g.form(now, now.Add(-g.minAge), scope, flagTrapOmitted)
}

func (g *Guard) form(now, issued time.Time, scope string, flags uint8) Form {
	if g.bind {
		flags |= flagBound
	}
	c := newChallenge(g.key, issued, g.maxAge, scope, g.difficulty, flags)
	left := g.minAge - now.Sub(c.Issued)
	if left < 0 {
		left = 0
	}
	return Form{Challenge: c, scriptURL: g.scriptURL, workerURL: g.workerURL, bound: g.bind, minAgeLeft: left}
}

// NeedsScript is false under NoProof: render the submit enabled, and
// no module, status line or noscript, because nothing will run.
func (f Form) NeedsScript() bool { return f.Difficulty > 0 }

// Attrs are the attributes pow.js reads off the <form>. data-pow-min-age
// is the age the token still lacks at render, so a FollowOn or Recovery
// form carries 0 and is never held.
func (f Form) Attrs() template.HTMLAttr {
	if !f.NeedsScript() {
		return ""
	}
	s := fmt.Sprintf(`data-pow-form data-pow-nonce="%s" data-pow-difficulty="%d" data-pow-worker="%s" data-pow-min-age="%d"`,
		template.HTMLEscapeString(f.Nonce), f.Difficulty, template.HTMLEscapeString(f.workerURL), f.minAgeLeft.Milliseconds())
	if f.bound {
		s += " data-pow-bound"
	}
	return template.HTMLAttr(s)
}

// Script loads the module. Render it once per page.
func (f Form) Script() template.HTML {
	if !f.NeedsScript() {
		return ""
	}
	return template.HTML(fmt.Sprintf(`<script type="module" src="%s"></script>`, template.HTMLEscapeString(f.scriptURL)))
}

// StatusLine is visible from first paint and hidden by the module when
// the form is ready, so a module that never runs (blocked, missing,
// thrown) leaves the visitor a reason instead of a dead button. Its
// words come from the caller because pow has no locale; they must be
// true whether or not the module ever runs. Render it inside the form.
func (f Form) StatusLine(text string) template.HTML {
	if !f.NeedsScript() {
		return ""
	}
	return template.HTML(`<p data-pow-status>` + template.HTMLEscapeString(text) + `</p>`)
}
```

- [ ] **Step 6: Rewrite the package doc in `pow/pow.go`**

Replace the comment above `package pow` (keep everything below it):

```go
// Package pow is the front door for a form anyone on the internet can
// post to: a sealed, single-use challenge, a honeypot, and a proof of
// work, with the browser half shipped alongside the Go half that
// verifies it.
//
// The browser half is here because the solver in the page and the
// verifier in Go must build a byte-identical preimage; nothing in a
// copied file enforces that, and a disagreement fails silently for
// some visitors. pow/browser_test.go runs the shipped solver in
// Chromium against this verifier.
//
// The shape:
//
//	g, err := pow.New(pow.Config{InstanceKey: key, Nonces: pow.SQLNonces(db),
//		ScriptURL: assets.Path("pow.js"), WorkerURL: assets.Path("pow-worker.js")})
//	f := g.Form(time.Now(), "apply:41")       // render f.Fields, f.Attrs, f.Script
//	adm := g.Admit(r, pow.Want{Scope: "apply:41"})
//	... validate, then in the handler's transaction:
//	err = adm.Commit(ctx, tx)                 // spends with the business write
//
// Check is Admit and Commit at once, for a handler that redirects after
// every POST. Never cache a page that carries a challenge: one token on
// a shared page is one token for every visitor.
//
// What this does not do: proof of work prices out scripted abuse and
// nothing more. Against a bulk attacker who pays for the solves the
// defence is a persisted budget on what the form spends.
```

- [ ] **Step 7: Run the package tests**

Run: `GOFLAGS=-mod=mod go test ./pow/ -count=1`
Expected: PASS. Then `GOFLAGS=-mod=mod go vet ./pow/ && gofmt -l pow/` prints nothing.

Also run: `GOFLAGS=-mod=mod go test . -run Honeypot -count=1` (root `buildhandler_test.go` still checks `pow.HoneypotStyleHash`). Expected: PASS.

- [ ] **Step 8: Commit Tasks 2 to 4**

```bash
git add pow/
git commit -m "pow: admission that commits with the write, sealed scope and expiry" -m "pow spent its nonce before the handler wrote anything, bound work to an address so solving waited for submit, had no token-only tier and sealed no scope, so Tito Go built its own instead. Admit now decides and writes nothing and Commit spends inside the caller's transaction; the v2 seal is length-prefixed and carries scope, millisecond issue and an absolute expiry; the spend enforces expiry on the database clock so a stalled request cannot outlive a sweep.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 5: `pow/powtest`, filling a challenge the way the browser does

**Files:**
- Create: `pow/powtest/powtest.go`, `pow/powtest/powtest_test.go`
- Modify: `pow/pow_test.go` (the `solveFor` comment only)

**Interfaces:**
- Consumes: `pow.Verify(nonce, binding, counter string, difficulty int) bool`, the field names.
- Produces: `func Fill(t testing.TB, page []byte, form url.Values) url.Values`, `func FillBound(t testing.TB, page []byte, form url.Values, binding string) url.Values`

- [ ] **Step 1: Write the failing test**

`pow/powtest/powtest_test.go`:

```go
package powtest_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/pow"
	"amadan.net/rastrillo/rastrillo/pow/powtest"
)

func TestFillProducesAnAdmissibleSubmission(t *testing.T) {
	g, err := pow.New(pow.Config{InstanceKey: "k", Nonces: pow.MemoryNonces(), Difficulty: 8,
		MinAge: time.Millisecond, ScriptURL: "/p.js", WorkerURL: "/w.js"})
	if err != nil {
		t.Fatal(err)
	}
	f := g.Form(time.Now().Add(-time.Second), "s")
	page := []byte(`<form ` + string(f.Attrs()) + `>` + string(f.Fields()) + `</form>`)
	v := powtest.Fill(t, page, url.Values{"email": {"a@b.c"}})
	if v.Get("email") != "a@b.c" {
		t.Fatal("Fill dropped the caller's fields")
	}
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(v.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if a := g.Check(r, pow.Want{Scope: "s"}); !a.OK {
		t.Fatalf("filled submission refused: %s %v", a.Reason, a.Also)
	}
	_ = context.Background()
}
```

- [ ] **Step 2: Run it to see it fail**

Run: `GOFLAGS=-mod=mod go test ./pow/powtest/ -count=1`
Expected: FAIL, package `powtest` does not exist.

- [ ] **Step 3: Write `pow/powtest/powtest.go`**

```go
// Package powtest fills a rendered pow challenge the way the shipped
// browser module would, for tests that drive a protected form over
// HTTP without a browser.
//
// It contains a solver. That is acceptable here and nowhere else: a
// hashcash solver is ten lines for anyone who wants one, so the risk
// was never secrecy; it was an app solving its own challenges in
// production, and an import path ending in powtest makes that obvious
// in review.
package powtest

import (
	"html"
	"net/url"
	"regexp"
	"strconv"
	"testing"

	"amadan.net/rastrillo/rastrillo/pow"
)

var hiddenField = regexp.MustCompile(`<input type="hidden" name="(pow_[a-z]+)" value="([^"]*)"`)

// Fill copies the first challenge in page into form and solves it
// unbound. The honeypot is left empty, as a person leaves it.
func Fill(t testing.TB, page []byte, form url.Values) url.Values {
	t.Helper()
	return FillBound(t, page, form, "")
}

// FillBound is Fill for a bound form: binding is the value the work is
// tied to, exactly as the [data-pow-binding] input would hold it.
func FillBound(t testing.TB, page []byte, form url.Values, binding string) url.Values {
	t.Helper()
	out := url.Values{}
	for k, v := range form {
		out[k] = append([]string(nil), v...)
	}
	seen := map[string]bool{}
	for _, m := range hiddenField.FindAllSubmatch(page, -1) {
		name := string(m[1])
		if seen[name] {
			break // the second form on the page starts here
		}
		seen[name] = true
		out.Set(name, html.UnescapeString(string(m[2])))
	}
	if !seen["pow_nonce"] {
		t.Fatalf("powtest: no pow challenge in the page")
	}
	bits, _ := strconv.Atoi(out.Get("pow_difficulty"))
	if bits > 0 {
		nonce := out.Get("pow_nonce")
		for i := 0; ; i++ {
			c := strconv.Itoa(i)
			if pow.Verify(nonce, binding, c, bits) {
				out.Set("pow_counter", c)
				break
			}
			if i > 1<<26 {
				t.Fatalf("powtest: no solution at %d bits", bits)
			}
		}
	}
	return out
}
```

- [ ] **Step 4: Run it**

Run: `GOFLAGS=-mod=mod go test ./pow/... -count=1`
Expected: PASS.

- [ ] **Step 5: Update the `solveFor` comment in `pow/pow_test.go`**

Replace its last two sentences with: `It is not exported. powtest ships one for other packages' tests; see that package's doc for why a test-named import path is the only acceptable home.`

- [ ] **Step 6: Commit**

```bash
git add pow/powtest pow/pow_test.go
git commit -m "pow/powtest: fill a challenge the way the browser does" -m "auth, password and the notes example drive protected forms over HTTP in their tests, and each would otherwise grow its own copy of the field list and a solver; a copy drifts from the fields Fields renders, and the tests would then pass against a contract the browser no longer meets.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---
### Task 6: The browser half: `pow.js`

**Files:**
- Modify: `pow/browser/pow.js` (whole file)
- Unchanged: `pow/browser/powcore.js`, `pow/browser/pow-worker.js`, `pow/browser/sha256.js`
- Test: `pow/browser_test.go` (append)

**Interfaces:**
- Consumes: Task 4's `Form.Attrs` attributes (`data-pow-form`, `data-pow-nonce`, `data-pow-difficulty`, `data-pow-worker`, `data-pow-min-age`, `data-pow-bound`), `[data-pow-counter]`, `[data-pow-status]`, `[data-pow-submit]`, `[data-pow-binding]`; `asciiLower` from `powcore.js`.
- Produces (module exports): `init(root = document)`, `whenSolved(form, { timeout = 10000 } = {}) → Promise<{ fields: Record<string,string> }>`. Events on the form: `pow:solved` (`detail.fields`), `pow:failed` (`detail.error`). Attributes it sets: `data-pow-ready` on a wired form, `data-pow-released` on the form for the duration of its own `requestSubmit` (Task 7's `busy.js` reads it).

- [ ] **Step 1: Write the failing browser tests**

Append to `pow/browser_test.go` (add imports `io`, `strings`, `sync/atomic`, `time`, `github.com/chromedp/cdproto/emulation`, `github.com/chromedp/cdproto/page`, `github.com/chromedp/chromedp/kb`):

```go
// formRig serves pow's assets, one page built from a real Guard, and a
// POST endpoint that Checks the submission and says what it decided.
type formRig struct {
	*harness.Rig
	g         *pow.Guard
	scriptURL string
	posts     atomic.Int32
	postedAt  atomic.Int64 // unix ms of the last POST
	last      atomic.Value // "ok" or the refusal reason
}

type rigOpts struct {
	cfg         func(*pow.Config)
	page        func(r *formRig) string
	csp         string
	delayModule time.Duration // serve pow.<hash>.js this late
}

func newFormRig(t *testing.T, o rigOpts) *formRig {
	t.Helper()
	fr := &formRig{}
	fr.Rig = harness.New(t, func(string) http.Handler {
		assets := rastrillo.NewAssets(pow.Assets())
		fr.scriptURL = "/pow" + assets.Path("pow.js")
		cfg := pow.Config{InstanceKey: "browser", Nonces: pow.MemoryNonces(), Difficulty: browserDifficulty,
			MinAge: 50 * time.Millisecond, ScriptURL: fr.scriptURL, WorkerURL: "/pow" + assets.Path("pow-worker.js")}
		if o.cfg != nil {
			o.cfg(&cfg)
		}
		g, err := pow.New(cfg)
		if err != nil {
			t.Fatalf("pow.New: %v", err)
		}
		fr.g = g
		mux := http.NewServeMux()
		powFiles := http.StripPrefix("/pow/", assets.Handler())
		mux.Handle("GET /pow/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if o.delayModule > 0 && r.URL.Path == fr.scriptURL {
				time.Sleep(o.delayModule)
			}
			powFiles.ServeHTTP(w, r)
		}))
		pageHandler := func(w http.ResponseWriter, r *http.Request) {
			if o.csp != "" {
				w.Header().Set("Content-Security-Policy", o.csp)
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			io.WriteString(w, o.page(fr))
		}
		mux.HandleFunc("GET /{$}", pageHandler)
		mux.HandleFunc("GET /again", pageHandler)
		mux.HandleFunc("POST /submit", func(w http.ResponseWriter, r *http.Request) {
			fr.posts.Add(1)
			fr.postedAt.Store(time.Now().UnixMilli())
			a := g.Check(r, pow.Want{Scope: "s", Binding: r.FormValue("email")})
			res := "ok"
			if !a.OK {
				res = string(a.Reason)
			}
			fr.last.Store(res)
			io.WriteString(w, "<!doctype html><title>done</title><p id=result>"+res+"</p>")
		})
		mux.HandleFunc("GET /slow", func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(3 * time.Second)
			io.WriteString(w, "<!doctype html><title>slow</title><p id=slow>slow</p>")
		})
		mux.HandleFunc("GET /bad-worker.js", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/javascript")
			io.WriteString(w, `throw new Error("a worker that cannot start");`)
		})
		mux.HandleFunc("GET /busy.js", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/javascript")
			w.Write(ui.BusyJS())
		})
		return mux
	})
	return fr
}

// powPage renders the ordinary protected form. bound adds the binding
// marker to the email field; outside moves the submit out of the form;
// extra is markup after the form.
func powPage(bound, outside bool, extra string) func(*formRig) string {
	return func(fr *formRig) string {
		f := fr.g.Form(time.Now(), "s")
		binding := ""
		if bound {
			binding = " data-pow-binding"
		}
		button := `<button id=go type=submit data-pow-submit disabled>Send</button>`
		inside, after := button, ""
		if outside {
			inside, after = "", `<button id=go type=submit form=f data-pow-submit disabled>Send</button>`
		}
		return `<!doctype html><title>pow</title><form id=f method=post action=/submit ` + string(f.Attrs()) + `>` +
			string(f.Fields()) + `<input name=email id=email value="a@example.com"` + binding + `>` + inside +
			string(f.StatusLine("If this form does not respond, reload the page.")) +
			`<noscript><p id=ns>This form needs JavaScript.</p></noscript></form>` + after + extra + string(f.Script())
	}
}

func (fr *formRig) waitReady(t *testing.T) {
	t.Helper()
	fr.Run(chromedp.Navigate(fr.Origin+"/"), chromedp.WaitReady(`form[data-pow-ready]`, chromedp.ByQuery))
}

func (fr *formRig) result(t *testing.T) string {
	t.Helper()
	var res string
	fr.Run(chromedp.WaitVisible(`#result`, chromedp.ByQuery), chromedp.Text(`#result`, &res, chromedp.ByQuery))
	return res
}

const whenSolvedJS = `(async () => {
	const m = await import(%q);
	try { return JSON.stringify(await m.whenSolved(document.getElementById("f"), %s)); }
	catch (e) { return "rejected: " + e.message; }
})()`

func TestBrowserUnboundSolvesBeforeSubmit(t *testing.T) {
	fr := newFormRig(t, rigOpts{page: powPage(false, false, "")})
	fr.waitReady(t)
	var got string
	fr.Run(chromedp.Evaluate(fmt.Sprintf(whenSolvedJS, fr.scriptURL, "{}"), &got, awaitPromise))
	if !strings.Contains(got, `"pow_counter":"`) || strings.Contains(got, `"pow_counter":""`) {
		t.Fatalf("whenSolved before any click = %s, want a counter", got)
	}
	fr.Run(chromedp.Click(`#go`, chromedp.ByQuery))
	if res := fr.result(t); res != "ok" {
		t.Fatalf("submission = %s", res)
	}
}

func TestBrowserFastClickIsHeldNotRefused(t *testing.T) {
	fr := newFormRig(t, rigOpts{
		cfg:  func(c *pow.Config) { c.MinAge = 1500 * time.Millisecond },
		page: powPage(false, false, ""),
	})
	fr.waitReady(t)
	fr.Run(chromedp.Click(`#go`, chromedp.ByQuery))
	if res := fr.result(t); res != "ok" {
		t.Fatalf("a click before the minimum age = %s, want held and then ok", res)
	}
}

func TestBrowserBoundFormResolvesAfterTheBindingChanges(t *testing.T) {
	fr := newFormRig(t, rigOpts{
		cfg:  func(c *pow.Config) { c.Bind = true; c.MinAge = 1500 * time.Millisecond },
		page: powPage(true, false, ""),
	})
	fr.waitReady(t)
	fr.Run(chromedp.Click(`#go`, chromedp.ByQuery),
		chromedp.Evaluate(`document.getElementById("email").value = "b@example.com"`, nil))
	if res := fr.result(t); res != "ok" {
		t.Fatalf("binding changed during the hold = %s, want re-solved and ok (a stale proof is pow_short)", res)
	}
}

func TestBrowserLeavingDuringAHoldNeverSubmits(t *testing.T) {
	fr := newFormRig(t, rigOpts{
		cfg:  func(c *pow.Config) { c.MinAge = 2 * time.Second },
		page: powPage(false, false, `<a id=slow href=/slow>elsewhere</a>`),
	})
	fr.waitReady(t)
	fr.Run(chromedp.Click(`#go`, chromedp.ByQuery), chromedp.Click(`#slow`, chromedp.ByQuery))
	time.Sleep(3500 * time.Millisecond)
	if n := fr.posts.Load(); n != 0 {
		t.Fatalf("%d POSTs after the visitor navigated away during the hold, want 0", n)
	}
}

func TestBrowserBackForwardRestoresTheForm(t *testing.T) {
	fr := newFormRig(t, rigOpts{
		cfg:  func(c *pow.Config) { c.MinAge = 2 * time.Second },
		page: powPage(false, false, ""),
	})
	fr.waitReady(t)
	fr.Run(chromedp.Click(`#go`, chromedp.ByQuery),
		chromedp.Evaluate(`dispatchEvent(new PageTransitionEvent("pagehide", {persisted: true}))`, nil))
	time.Sleep(2500 * time.Millisecond)
	if n := fr.posts.Load(); n != 0 {
		t.Fatalf("a held submit fired after pagehide: %d POSTs", n)
	}
	var enabled bool
	fr.Run(chromedp.Evaluate(`dispatchEvent(new PageTransitionEvent("pageshow", {persisted: true})); !document.getElementById("go").disabled`, &enabled))
	if !enabled {
		t.Fatal("pageshow from the back-forward cache left the submit disabled")
	}
	fr.Run(chromedp.Click(`#go`, chromedp.ByQuery))
	if res := fr.result(t); res != "ok" {
		t.Fatalf("after restore = %s", res)
	}
}

func TestBrowserInitAfterDocumentWrite(t *testing.T) {
	fr := newFormRig(t, rigOpts{page: powPage(false, false, "")})
	fr.waitReady(t)
	var ready bool
	fr.Run(chromedp.Evaluate(fmt.Sprintf(`(async () => {
		const html = await (await fetch("/again")).text();
		document.open(); document.write(html); document.close();
		const m = await import(%q);
		m.init(document);
		return document.querySelector("form").hasAttribute("data-pow-ready");
	})()`, fr.scriptURL), &ready, awaitPromise))
	if !ready {
		t.Fatal("init(document) after document.write did not wire the new form")
	}
	fr.Run(chromedp.Click(`#go`, chromedp.ByQuery))
	if res := fr.result(t); res != "ok" {
		t.Fatalf("after document.write = %s", res)
	}
}

func TestBrowserSubmitOutsideTheForm(t *testing.T) {
	fr := newFormRig(t, rigOpts{page: powPage(false, true, "")})
	fr.waitReady(t)
	fr.Run(chromedp.Click(`#go`, chromedp.ByQuery))
	if res := fr.result(t); res != "ok" {
		t.Fatalf("form= submit = %s", res)
	}
}

func TestBrowserFailureLeavesTheStatusLineShowing(t *testing.T) {
	cases := map[string]rigOpts{
		"missing module":   {cfg: func(c *pow.Config) { c.ScriptURL = "/pow/missing.js" }},
		"blocked by CSP":   {csp: "default-src 'self'; script-src 'none'"},
		"throwing worker":  {cfg: func(c *pow.Config) { c.WorkerURL = "/bad-worker.js" }},
	}
	for name, o := range cases {
		t.Run(name, func(t *testing.T) {
			o.page = powPage(false, false, "")
			fr := newFormRig(t, o)
			fr.Run(chromedp.Navigate(fr.Origin+"/"), chromedp.WaitVisible(`[data-pow-status]`, chromedp.ByQuery))
			fr.Run(chromedp.Click(`#go`, chromedp.ByQuery))
			time.Sleep(500 * time.Millisecond)
			var visible bool
			fr.Run(chromedp.Evaluate(`!document.querySelector("[data-pow-status]").hidden`, &visible))
			if !visible {
				t.Fatal("the status line is hidden although the form cannot work")
			}
			if n := fr.posts.Load(); n != 0 {
				t.Fatalf("%d POSTs from a form whose check could not run", n)
			}
		})
	}
}

func TestBrowserWhenSolvedContract(t *testing.T) {
	t.Run("NoProof resolves at once", func(t *testing.T) {
		fr := newFormRig(t, rigOpts{cfg: func(c *pow.Config) { c.Difficulty = pow.NoProof }, page: powPage(false, false, "")})
		fr.Run(chromedp.Navigate(fr.Origin + "/"))
		var got string
		fr.Run(chromedp.Evaluate(fmt.Sprintf(whenSolvedJS, fr.scriptURL, "{}"), &got, awaitPromise))
		if !strings.Contains(got, `"pow_seal"`) {
			t.Fatalf("whenSolved on a NoProof form = %s, want its fields", got)
		}
	})
	t.Run("bound rejects", func(t *testing.T) {
		fr := newFormRig(t, rigOpts{cfg: func(c *pow.Config) { c.Bind = true }, page: powPage(true, false, "")})
		fr.waitReady(t)
		var got string
		fr.Run(chromedp.Evaluate(fmt.Sprintf(whenSolvedJS, fr.scriptURL, "{}"), &got, awaitPromise))
		if !strings.HasPrefix(got, "rejected:") {
			t.Fatalf("whenSolved on a bound form = %s, want rejected", got)
		}
	})
	t.Run("timeout rejects", func(t *testing.T) {
		fr := newFormRig(t, rigOpts{cfg: func(c *pow.Config) { c.Difficulty = 40 }, page: powPage(false, false, "")})
		fr.waitReady(t)
		var got string
		fr.Run(chromedp.Evaluate(fmt.Sprintf(whenSolvedJS, fr.scriptURL, "{timeout: 200}"), &got, awaitPromise))
		if !strings.Contains(got, "timed out") {
			t.Fatalf("whenSolved past its timeout = %s, want rejected as timed out", got)
		}
	})
}

func TestBrowserNoProofWorksWithoutJavaScript(t *testing.T) {
	fr := newFormRig(t, rigOpts{
		cfg: func(c *pow.Config) { c.Difficulty = pow.NoProof; c.MinAge = 3 * time.Second },
		page: func(fr *formRig) string {
			f := fr.g.FollowOn(time.Now(), "s")
			return `<!doctype html><title>np</title><form id=f method=post action=/submit>` + string(f.Fields()) +
				`<button id=go type=submit>Send</button></form>`
		},
	})
	fr.Run(emulation.SetScriptExecutionDisabled(true), chromedp.Navigate(fr.Origin+"/"), chromedp.Click(`#go`, chromedp.ByQuery))
	if res := fr.result(t); res != "ok" {
		t.Fatalf("NoProof follow-on form without JavaScript = %s, want ok", res)
	}
}

func TestBrowserProofFormWithoutJavaScriptCannotSubmit(t *testing.T) {
	fr := newFormRig(t, rigOpts{page: powPage(false, false, "")})
	fr.Run(emulation.SetScriptExecutionDisabled(true), chromedp.Navigate(fr.Origin+"/"),
		chromedp.WaitVisible(`#ns`, chromedp.ByQuery), chromedp.Click(`#go`, chromedp.ByQuery))
	time.Sleep(500 * time.Millisecond)
	if n := fr.posts.Load(); n != 0 {
		t.Fatalf("%d POSTs from a disabled proof form with JavaScript off", n)
	}
}

func TestBrowserEnterBeforeReadyPostsNothing(t *testing.T) {
	// Review focus 4: implicit submission with the default button
	// disabled must do nothing, and the visitor sees why.
	fr := newFormRig(t, rigOpts{page: powPage(false, false, ""), delayModule: 2 * time.Second})
	fr.Run(chromedp.ActionFunc(func(ctx context.Context) error {
		_, _, _, _, err := page.Navigate(fr.Origin + "/").Do(ctx)
		return err
	}), chromedp.WaitVisible(`#email`, chromedp.ByQuery), chromedp.SendKeys(`#email`, kb.Enter, chromedp.ByQuery))
	time.Sleep(500 * time.Millisecond)
	if n := fr.posts.Load(); n != 0 {
		t.Fatalf("%d POSTs from Enter before the module loaded", n)
	}
	var visible bool
	fr.Run(chromedp.Evaluate(`!document.querySelector("[data-pow-status]").hidden`, &visible))
	if !visible {
		t.Fatal("the status line is not showing while the form is not ready")
	}
}
```

(Add `context` and `amadan.net/rastrillo/rastrillo/ui` to the imports.)

- [ ] **Step 2: Run them to see them fail**

Run: `cd /home/paulca/amadan.net/rastrillo/rastrillo-pow-admission && TMPDIR="${TMPDIR:-/var/tmp}" go test -tags browser -p 1 ./pow/ -run 'TestBrowser' -count=1`
Expected: the old `TestBrowser*` solver/honeypot tests pass; the new ones fail (no `init`/`whenSolved` exports, forms never get `data-pow-ready`).

- [ ] **Step 3: Rewrite `pow/browser/pow.js`**

```js
// Wires protected forms to the solver.
//
// The submit is rendered disabled, with a <noscript> line and a status
// line beside it, and this module enables it once the form is wired.
// That order is the only one that fails safe: JavaScript cannot enable
// a control inside <noscript>, and a module that is blocked, missing or
// throws leaves the honest disabled state and the status line, rather
// than a form that looks live and does nothing.
//
// Markup, which pow.Form renders:
//
//   form[data-pow-form]  nonce, difficulty, worker URL, min age, [bound]
//   [data-pow-counter]   the hidden input the solution is written into
//   [data-pow-submit]    submit controls, rendered disabled; found through
//                        form.elements, so form= controls outside count
//   [data-pow-status]    visible until ready; shown again on failure
//   [data-pow-binding]   bound forms only: the input the work is tied to
//
// The module does no hashing itself; the worker imports sha256.js.

import { asciiLower } from "./powcore.js";

// Every wired form's state. The page's own forms, so holding them is
// free; disconnected ones are dropped on the next init.
const states = new Map();

// init wires every protected form under root. Idempotent per form. It
// runs once when the module first evaluates; a page that replaces its
// document (document.open/write) must call it again, because a module
// URL that has already run is never evaluated a second time.
export function init(root = document) {
  for (const form of states.keys()) {
    if (!form.isConnected) states.delete(form);
  }
  for (const form of root.querySelectorAll("form[data-pow-form]")) setup(form);
}

// whenSolved resolves with the pow_* fields a request on this form's
// behalf must carry for Guard.Verify. It never stays pending: it
// rejects on worker failure, when the page is left, and after timeout.
// A form that is not a proof form resolves at once with its fields; a
// bound form rejects, because a bound proof cannot be verified without
// the value it is bound to.
export function whenSolved(form, { timeout = 10000 } = {}) {
  if (!form.hasAttribute("data-pow-form")) return Promise.resolve({ fields: fieldsFrom(form, null) });
  const st = states.get(form);
  if (!st) return Promise.reject(new Error("pow: this form is not initialised"));
  if (st.bound) return Promise.reject(new Error("pow: a bound proof cannot be verified on its own"));
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error("pow: timed out")), timeout);
    solve(st, "").then(
      () => { clearTimeout(timer); resolve({ fields: fieldsOf(st) }); },
      (err) => { clearTimeout(timer); reject(err); },
    );
  });
}

function setup(form) {
  if (states.has(form)) return;
  const d = form.dataset;
  const bound = form.hasAttribute("data-pow-bound");
  const st = {
    form,
    bound,
    nonce: d.powNonce,
    difficulty: parseInt(d.powDifficulty, 10),
    workerURL: d.powWorker,
    readyAt: performance.now() + (parseInt(d.powMinAge || "0", 10) || 0),
    counter: form.querySelector("[data-pow-counter]"),
    status: form.querySelector("[data-pow-status]"),
    binding: bound ? form.querySelector("[data-pow-binding]") : null,
    submits: Array.from(form.elements).filter((el) => el.matches("[data-pow-submit]")),
    worker: null,
    solving: null,
    solution: null,
    held: null,
    releasing: false,
  };
  // A form missing a piece is a wiring bug. Enabling its submit would
  // post something the server is certain to refuse; leave it disabled,
  // with the status line showing, and say so where a developer looks.
  if (!st.nonce || !st.workerURL || !st.counter || !(st.difficulty > 0) ||
      st.submits.length === 0 || (bound && !st.binding)) {
    console.error("pow.js: form is missing a required element or attribute", form);
    return;
  }
  states.set(form, st);
  form.addEventListener("submit", (e) => onSubmit(st, e));
  for (const b of st.submits) b.disabled = false;
  if (st.status) st.status.hidden = true;
  form.setAttribute("data-pow-ready", "");
  // Unbound work can start now, while the visitor reads and types, so
  // most never wait at all.
  if (!bound) solve(st, "").catch(() => {});
}

// A solution belongs to one binding value, normalised exactly as
// powcore does. Autofill or a correction while the worker runs changes
// the value; releasing a proof for the old one would be refused as
// pow_short with nothing the visitor can read.
function keyOf(value) {
  return asciiLower(value.trim());
}

function solve(st, value) {
  const key = keyOf(value);
  if (st.solution && st.solution.key === key) return Promise.resolve(st.solution.counter);
  if (st.solving && st.solving.key === key) return st.solving.promise;
  stopWorker(st, new Error("pow: superseded"));
  let settle;
  const promise = new Promise((resolve, reject) => { settle = { resolve, reject }; });
  st.solving = { key, promise, settle };
  try {
    const worker = new Worker(st.workerURL, { type: "module" });
    st.worker = worker;
    worker.onmessage = (e) => {
      if (!e.data.done) return;
      const s = st.solving;
      stopWorker(st, null);
      st.solution = { key, counter: e.data.counter };
      s.settle.resolve(e.data.counter);
      st.form.dispatchEvent(new CustomEvent("pow:solved", { detail: { fields: fieldsOf(st) } }));
    };
    worker.onerror = (e) => {
      e.preventDefault();
      failed(st, new Error("pow: the worker failed"));
    };
    worker.postMessage({ nonce: st.nonce, binding: value, difficulty: st.difficulty });
  } catch (err) {
    // The constructor itself can throw (a CSP worker-src, a bad URL).
    failed(st, err);
  }
  return promise;
}

function stopWorker(st, reason) {
  if (st.worker) {
    st.worker.terminate();
    st.worker = null;
  }
  const s = st.solving;
  st.solving = null;
  if (s && reason) s.settle.reject(reason);
}

function failed(st, err) {
  stopWorker(st, err);
  endHold(st);
  if (st.status) st.status.hidden = false;
  st.form.dispatchEvent(new CustomEvent("pow:failed", { detail: { error: err } }));
}

function onSubmit(st, e) {
  if (st.releasing) return; // our own requestSubmit
  if (st.held) { e.preventDefault(); return; } // a second click while held changes nothing
  const value = st.bound ? st.binding.value : "";
  if (st.bound && !value.trim()) {
    e.preventDefault();
    st.binding.reportValidity();
    return;
  }
  if (st.solution && st.solution.key === keyOf(value) && performance.now() >= st.readyAt) {
    st.counter.value = st.solution.counter;
    return; // nothing to wait for: an ordinary submit
  }
  e.preventDefault();
  hold(st, e.submitter || null);
}

// hold keeps a submit until the solve and the minimum age are both done.
// A beforeunload listener exists only while holding: pagehide fires
// only once a navigation commits, and until then a solve finishing
// would submit over the navigation the visitor chose. Only while
// holding, so ordinary pages stay eligible for the back-forward cache.
function hold(st, submitter) {
  const onLeave = () => endHold(st);
  st.held = { submitter, onLeave, label: submitter ? submitter.textContent : null };
  addEventListener("beforeunload", onLeave);
  if (submitter) {
    submitter.setAttribute("aria-busy", "true");
    if (submitter.dataset.workingLabel) submitter.textContent = submitter.dataset.workingLabel;
  }
  wait(st);
}

function wait(st) {
  const value = st.bound ? st.binding.value : "";
  const delay = Math.max(0, st.readyAt - performance.now());
  Promise.all([solve(st, value), new Promise((r) => setTimeout(r, delay))])
    .then(() => release(st), () => {});
}

function release(st) {
  const h = st.held;
  if (!h) return; // left, or failed, while waiting
  const value = st.bound ? st.binding.value : "";
  if (!st.solution || st.solution.key !== keyOf(value)) {
    wait(st); // the binding changed while the worker ran: solve again
    return;
  }
  endHold(st);
  st.counter.value = st.solution.counter;
  const sub = h.submitter && h.submitter.isConnected && h.submitter.form === st.form ? h.submitter : undefined;
  const wasDisabled = sub ? sub.disabled : false;
  if (sub) sub.disabled = false;
  // busy.js reads this during the submit event requestSubmit fires
  // synchronously, and does not hold a submit the visitor has already
  // watched wait.
  st.form.setAttribute("data-pow-released", "");
  st.releasing = true;
  try {
    st.form.requestSubmit(sub);
  } finally {
    st.releasing = false;
    st.form.removeAttribute("data-pow-released");
    if (sub) sub.disabled = wasDisabled;
  }
}

function endHold(st) {
  const h = st.held;
  if (!h) return;
  st.held = null;
  removeEventListener("beforeunload", h.onLeave);
  if (h.submitter) {
    h.submitter.removeAttribute("aria-busy");
    if (h.submitter.dataset.workingLabel && h.label !== null) h.submitter.textContent = h.label;
  }
}

function fieldsFrom(form, counter) {
  const out = {};
  for (const el of form.querySelectorAll('input[type="hidden"][name^="pow_"]')) out[el.name] = el.value;
  if (counter !== null) out.pow_counter = counter;
  return out;
}

function fieldsOf(st) {
  return fieldsFrom(st.form, st.solution ? st.solution.counter : null);
}

// Leaving the page ends every hold and stops every worker; a timer or a
// solve surviving into the back-forward cache would otherwise submit
// on the visitor's return.
addEventListener("pagehide", () => {
  for (const st of states.values()) {
    endHold(st);
    stopWorker(st, new Error("pow: the page was left"));
  }
});

// The cache restores the DOM as it was left. Hand every form back and
// restart unfinished unbound work.
addEventListener("pageshow", (e) => {
  if (!e.persisted) return;
  for (const st of states.values()) {
    for (const b of st.submits) b.disabled = false;
    if (!st.bound && !st.solution) solve(st, "").catch(() => {});
  }
});

init(document);
```

- [ ] **Step 4: Run the browser tests**

Run: `TMPDIR="${TMPDIR:-/var/tmp}" go test -tags browser -p 1 ./pow/ -count=1`
Expected: PASS. If `TestBrowserEnterBeforeReadyPostsNothing` posts, check the button is still rendered `disabled` in `powPage` (implicit submission is blocked only when the default button is disabled).

- [ ] **Step 5: Commit**

```bash
git add pow/browser/pow.js pow/browser_test.go
git commit -m "pow.js: solve in the background, hold fast submits, survive navigation" -m "Bound work could not start before submit, a fast click met too_fast, a hold kept running into a navigation the visitor chose or into the back-forward cache, a document replaced from a POST never re-ran the module, and a module that failed with JavaScript on left a dead button with no word of why. Each is now handled and pinned in Chromium.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 7: `busy.js` and `pow` together

**Files:**
- Modify: `ui/busy.js` (the hold section, lines 150-252 today)
- Test: `pow/browser_test.go` (append; it already serves `/busy.js`)

**Interfaces:**
- Consumes: `data-pow-released` (Task 6).

- [ ] **Step 1: Write the failing tests**

Append to `pow/browser_test.go`:

```go
func TestBrowserBusyDoesNotHoldAPowRelease(t *testing.T) {
	// The visitor already watched pow's hold; busy.js adding its own
	// 650ms after it is a second wait for nothing.
	fr := newFormRig(t, rigOpts{
		cfg:  func(c *pow.Config) { c.MinAge = time.Second },
		page: powPage(false, false, `<script defer src="/busy.js"></script>`),
	})
	fr.waitReady(t)
	clicked := time.Now()
	fr.Run(chromedp.Click(`#go`, chromedp.ByQuery))
	if res := fr.result(t); res != "ok" {
		t.Fatalf("with busy.js loaded = %s", res)
	}
	if n := fr.posts.Load(); n != 1 {
		t.Fatalf("%d POSTs, want 1", n)
	}
	if d := time.UnixMilli(fr.postedAt.Load()).Sub(clicked); d > 1500*time.Millisecond {
		t.Fatalf("POST %v after the click: busy.js held a submit pow had already held", d)
	}
}

func TestBrowserBusyHoldIsCancelledByNavigation(t *testing.T) {
	// busy.js on its own had the same race: its 650ms hold cancelled only
	// on pagehide, which fires after a slow destination commits.
	fr := newFormRig(t, rigOpts{page: func(*formRig) string {
		return `<!doctype html><title>busy</title><form method=post action=/submit>` +
			`<button id=go type=submit>Send</button></form><a id=slow href=/slow>elsewhere</a>` +
			`<script defer src="/busy.js"></script>`
	}})
	fr.Run(chromedp.Navigate(fr.Origin+"/"), chromedp.Click(`#go`, chromedp.ByQuery), chromedp.Click(`#slow`, chromedp.ByQuery))
	time.Sleep(3500 * time.Millisecond)
	if n := fr.posts.Load(); n != 0 {
		t.Fatalf("%d POSTs after navigating away during busy.js's hold, want 0", n)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `TMPDIR="${TMPDIR:-/var/tmp}" go test -tags browser -p 1 ./pow/ -run 'Busy' -count=1`
Expected: both FAIL (a POST about 650ms later than pow's release; one POST after navigation started).

- [ ] **Step 3: Change `ui/busy.js`**

In the window `submit` listener, directly after `if (form === releasing) { releasing = null; return; }`, add:

```js
    // pow.js has already held this submit while it solved, with the
    // working state showing; a second hold here is a second wait for
    // nothing. It marks its own release for exactly this check.
    if (form.hasAttribute("data-pow-released")) return;
```

Replace `pending.set(form, setTimeout(function () { release(form, btn); }, HOLD_MS));` with:

```js
    pending.set(form, setTimeout(function () { release(form, btn); }, HOLD_MS));
    // pagehide fires only once a navigation commits, and a slow
    // destination leaves the hold running until then: a held submit
    // released meanwhile replaces the navigation the visitor chose.
    // beforeunload fires as the navigation starts. Attached only while
    // something is held, so it keeps no page out of the back-forward
    // cache.
    if (pending.size === 1) window.addEventListener("beforeunload", dropHeld);
```

At the start of `release(form, btn)`, after `pending.delete(form);`, add:

```js
    if (pending.size === 0) window.removeEventListener("beforeunload", dropHeld);
```

Add this function after `release`:

```js
  // A navigation started while a submit was held: drop every held
  // submit and hand the forms back, so a navigation that is then
  // cancelled (a download, a beforeunload prompt) leaves no form
  // spinning.
  function dropHeld() {
    pending.forEach(function (timer, form) {
      clearTimeout(timer);
      busyOff(form);
    });
    pending.clear();
    window.removeEventListener("beforeunload", dropHeld);
  }
```

Replace the `pagehide` listener's body with `dropHeld();`.

In the header comment, after "a submit still being held when the page is left is dropped rather than sent later.", add: "Left means a navigation has started, not that it has finished. A submit pow.js has already held is not held again."

- [ ] **Step 4: Run them**

Run: `TMPDIR="${TMPDIR:-/var/tmp}" go test -tags browser -p 1 ./pow/ ./ui/ -count=1`
Expected: PASS, including ui's existing busy tests.

- [ ] **Step 5: Commit**

```bash
git add ui/busy.js pow/browser_test.go
git commit -m "busy.js: drop a held submit when navigation starts, and never re-hold pow" -m "busy.js cancelled its hold on pagehide, which fires only once a slow destination commits, so a held submit could replace the navigation the visitor had chosen; pow.js closed the same race and busy.js reopened it with its own hold after every pow release.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---
### Task 8: `auth`: the front door on `Begin`

**Files:**
- Modify: `auth/auth.go` (Config fields, errors, `New`), `auth/handlers.go` (`Begin`, new `problemURL`), `auth/signin.go` (`ProblemCheck`, `outcome`, `SigninState`)
- Modify (tests): `auth/auth_test.go` (`newTestAuthDB` merges `pow.Schema`; `newTestAuth` defaults `ProofOff: true`; `TestNewValidatesConfig` success cases add `ProofOff: true`), `auth/keymail_test.go:44,53` (add `ProofOff: true`)
- Create: `auth/proof_test.go`

**Interfaces:**
- Consumes: `pow.Guard`, `pow.Want`, `pow.Form`, `(*pow.Guard).Check/Form/Recovery/Bound`, `pow.Admission.Recovered`, `powtest.Fill`.
- Produces: `auth.Config.Proof *pow.Guard`, `auth.Config.ProofOff bool`, `auth.ErrProofUnset`, `auth.ErrProofMode`, `const auth.ProofScope = "rastrillo/auth/begin"`, `auth.ProblemCheck SigninProblem = "check"`, `SigninState.Force bool`, `SigninState.Proof *pow.Form`.

- [ ] **Step 1: Inventory the constructors**

Run: `grep -rn 'auth\.New(\|password\.New(\|\bNew(Config{' --include=*.go . | grep -v '/\.claude/'`
Expected, and the complete list this plan updates: `auth/auth_test.go` (`newTestAuth`, `TestNewValidatesConfig`), `auth/keymail_test.go:44,53`, `auth/signinscreen_browser_test.go:67` (Task 9), `passkey/signinscreen_test.go:201` (Task 11), `password/handlers_test.go:169` (Task 10), `examples/notes/internal/notes/app.go:65` (Task 11). If the grep shows any other, add it to the task that owns its package.

- [ ] **Step 2: Write the failing tests**

`auth/proof_test.go`:

```go
package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/pow"
	"amadan.net/rastrillo/rastrillo/pow/powtest"
)

func newProofAuth(t *testing.T, mut func(*Config)) (*Auth, *captureMailer) {
	t.Helper()
	return newTestAuth(t, func(c *Config) {
		g, err := pow.New(pow.Config{InstanceKey: "test-instance-key", Nonces: pow.SQLNonces(c.DB),
			Difficulty: 8, MinAge: time.Millisecond, ScriptURL: "/pow/pow.js", WorkerURL: "/pow/pow-worker.js"})
		if err != nil {
			t.Fatalf("pow.New: %v", err)
		}
		c.Proof, c.ProofOff = g, false
		if mut != nil {
			mut(c)
		}
	})
}

// filled is a Begin form as the browser would post it after loading
// /signin<query>: the challenge SigninState hands the page, solved.
func filled(t *testing.T, a *Auth, query string, form url.Values) url.Values {
	t.Helper()
	st := a.SigninState(httptest.NewRequest(http.MethodGet, "/signin"+query, nil))
	if st.Proof == nil {
		t.Fatal("SigninState carries no challenge although Config.Proof is set")
	}
	time.Sleep(2 * time.Millisecond) // past MinAge, as a person always is
	return powtest.Fill(t, []byte(st.Proof.Fields()), form)
}

func location(t *testing.T, w *httptest.ResponseRecorder) url.Values {
	t.Helper()
	u, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	return u.Query()
}

func TestNewRequiresAProofDecision(t *testing.T) {
	d := newTestAuthDB(t)
	base := Config{DB: d, Origin: "http://app.test", InstanceKey: "k", Mailer: &captureMailer{}}
	if _, err := New(base); !errors.Is(err, ErrProofUnset) {
		t.Fatalf("no decision = %v, want ErrProofUnset", err)
	}
	bound, err := pow.New(pow.Config{InstanceKey: "k", Nonces: pow.MemoryNonces(), Bind: true, ScriptURL: "/p", WorkerURL: "/w"})
	if err != nil {
		t.Fatal(err)
	}
	c := base
	c.Proof = bound
	if _, err := New(c); !errors.Is(err, ErrProofMode) {
		t.Fatalf("bound guard = %v, want ErrProofMode", err)
	}
	unbound, _ := pow.New(pow.Config{InstanceKey: "k", Nonces: pow.MemoryNonces(), ScriptURL: "/p", WorkerURL: "/w"})
	c = base
	c.Proof, c.ProofOff = unbound, true
	if _, err := New(c); err == nil {
		t.Fatal("Proof and ProofOff both set was accepted")
	}
	c = base
	c.ProofOff = true
	if _, err := New(c); err != nil {
		t.Fatalf("ProofOff = %v", err)
	}
}

func TestBeginRefusesWithoutAChallengeBeforeAnythingElse(t *testing.T) {
	a, m := newProofAuth(t, nil)
	lookups := 0
	a.flow.Classifier.LookupTXT = func(context.Context, string) ([]string, error) {
		lookups++
		return nil, nil
	}
	b := newBrowser()
	for i := 0; i < 30; i++ {
		w := b.do(a.Begin, http.MethodPost, "/signin", url.Values{"address": {"amy@example.com"}})
		if q := location(t, w); q.Get("err") != "check" || q.Get("rec") != "1" {
			t.Fatalf("Begin without a challenge → %s, want err=check&rec=1", w.Header().Get("Location"))
		}
	}
	if m.lastTo != "" || lookups != 0 {
		t.Fatalf("a refused Begin sent mail to %q or made %d DNS lookups", m.lastTo, lookups)
	}
	// Thirty refusals and the per-IP budget is twenty: if refusals spent
	// it, this would be err=rate.
	w := b.do(a.Begin, http.MethodPost, "/signin", filled(t, a, "", url.Values{"address": {"amy@example.com"}, "force": {"1"}}))
	if q := location(t, w); q.Get("err") != "" {
		t.Fatalf("a good Begin after refusals → %s: refusals spent the rate budget", w.Header().Get("Location"))
	}
	if m.lastTo != "amy@example.com" {
		t.Fatalf("mail went to %q", m.lastTo)
	}
}

func TestBeginRecoversAReplayedForm(t *testing.T) {
	// Review focus 1: Back after a successful sign-in restores a page
	// whose token is spent. Continue again must lead somewhere.
	a, m := newProofAuth(t, nil)
	b := newBrowser()
	form := filled(t, a, "", url.Values{"address": {"amy@example.com"}, "force": {"1"}})
	if w := b.do(a.Begin, http.MethodPost, "/signin", form); location(t, w).Get("err") != "" {
		t.Fatal("first submission refused")
	}
	w := b.do(a.Begin, http.MethodPost, "/signin", form)
	q := location(t, w)
	if q.Get("err") != "check" || q.Get("rec") != "1" || q.Get("force") != "1" {
		t.Fatalf("replayed form → %s, want err=check&force=1&rec=1", w.Header().Get("Location"))
	}
	m.lastTo = ""
	if st := a.SigninState(httptest.NewRequest(http.MethodGet, "/signin?err=check&force=1&rec=1", nil)); strings.Contains(string(st.Proof.Fields()), `name="hp"`) {
		t.Fatal("the recovery form carries the honeypot")
	}
	again := filled(t, a, "?err=check&force=1&rec=1", url.Values{"address": {"amy@example.com"}, "force": {"1"}})
	if w := b.do(a.Begin, http.MethodPost, "/signin", again); location(t, w).Get("err") != "" {
		t.Fatalf("recovery submission → %s", w.Header().Get("Location"))
	}
	if m.lastTo != "amy@example.com" {
		t.Fatal("the recovery submission sent no link")
	}
}

func TestBeginKeepsRecAndForceOnLaterErrors(t *testing.T) {
	a, _ := newProofAuth(t, nil)
	b := newBrowser()
	form := filled(t, a, "?rec=1", url.Values{"address": {"not an address"}, "force": {"1"}})
	w := b.do(a.Begin, http.MethodPost, "/signin", form)
	q := location(t, w)
	if q.Get("err") != "address" || q.Get("rec") != "1" || q.Get("force") != "1" {
		t.Fatalf("recovered token, bad address → %s, want err=address with rec and force kept", w.Header().Get("Location"))
	}
}

func TestBeginHoneypotRefusalLeadsToATraplessFormThatWorks(t *testing.T) {
	a, m := newProofAuth(t, nil)
	b := newBrowser()
	form := filled(t, a, "", url.Values{"address": {"amy@example.com"}, "force": {"1"}})
	form.Set("hp", "filled by a password manager")
	w := b.do(a.Begin, http.MethodPost, "/signin", form)
	if q := location(t, w); q.Get("err") != "check" || q.Get("rec") != "1" {
		t.Fatalf("honeypot → %s", w.Header().Get("Location"))
	}
	if m.lastTo != "" {
		t.Fatal("a trapped Begin sent mail")
	}
	again := filled(t, a, "?err=check&rec=1&force=1", url.Values{"address": {"amy@example.com"}, "force": {"1"}})
	if w := b.do(a.Begin, http.MethodPost, "/signin", again); location(t, w).Get("err") != "" {
		t.Fatalf("trapless retry → %s", w.Header().Get("Location"))
	}
}

func TestSigninStateCarriesProofAndForce(t *testing.T) {
	for _, screen := range []bool{false, true} {
		a, _ := newProofAuth(t, func(c *Config) { c.SigninScreen = screen })
		st := a.SigninState(httptest.NewRequest(http.MethodGet, "/signin?err=keymail&force=1", nil))
		if st.Proof == nil || !st.Force {
			t.Fatalf("screen=%v: Proof=%v Force=%v", screen, st.Proof != nil, st.Force)
		}
		rec := a.SigninState(httptest.NewRequest(http.MethodGet, "/signin?err=check&rec=1", nil))
		if rec.Problem != ProblemCheck {
			t.Fatalf("screen=%v: problem = %q, want check", screen, rec.Problem)
		}
	}
	off, _ := newTestAuth(t, nil)
	if st := off.SigninState(httptest.NewRequest(http.MethodGet, "/signin", nil)); st.Proof != nil {
		t.Fatal("ProofOff still renders a challenge")
	}
}
```

- [ ] **Step 3: Run them to see them fail**

Run: `GOFLAGS=-mod=mod go test ./auth/ -count=1`
Expected: compile failure (`Config.Proof`, `ProofScope`, `ProblemCheck`, `SigninState.Proof` undefined).

- [ ] **Step 4: Implement**

`auth/auth.go`: add `"amadan.net/rastrillo/rastrillo/pow"` to the imports, these fields to `Config` after `InstanceKey`:

```go
	// Proof is sign-in's front door: Begin runs it before the rate
	// limiter, classification and any mail, so an anonymous visitor
	// cannot make the server probe a domain of their choosing, send a
	// link, or spend another address's budget without solving. Build one
	// pow.Guard and share it with password; scope keeps their tokens
	// apart. Required unless ProofOff: an app that upgraded without
	// wiring it must fail at boot, not refuse every visitor in
	// production.
	Proof *pow.Guard
	// ProofOff runs sign-in without the front door.
	ProofOff bool
```

these errors beside `ErrEmptyInstanceKey`:

```go
// ErrProofUnset: Config has neither Proof nor ProofOff.
var ErrProofUnset = errors.New("rastrillo/auth: Config.Proof is unset: build a pow.Guard (serve pow.Assets(), apply pow.Schema) and set Config.Proof, or set Config.ProofOff")

// ErrProofMode: the Guard binds its work to an input, and the sign-in
// forms carry the address in three different shapes (two hidden
// inputs and a field), none of them bound.
var ErrProofMode = errors.New("rastrillo/auth: Config.Proof is a bound pow.Guard; sign-in needs an unbound one")

// ProofScope is the scope Begin's challenges are issued and checked
// under. A Guard shared with other forms keeps their tokens apart by
// it.
const ProofScope = "rastrillo/auth/begin"
```

and in `New`, directly after the `cfg.DB == nil` check:

```go
	switch {
	case cfg.Proof == nil && !cfg.ProofOff:
		return nil, ErrProofUnset
	case cfg.Proof != nil && cfg.ProofOff:
		return nil, errors.New("rastrillo/auth: Config.Proof and Config.ProofOff are both set; choose one")
	case cfg.Proof != nil && cfg.Proof.Bound():
		return nil, ErrProofMode
	}
```

`auth/handlers.go`: replace the start of `Begin` through the error `switch` with:

```go
func (a *Auth) Begin(w http.ResponseWriter, r *http.Request) {
	if !a.sameOrigin(r) {
		http.Error(w, "cross-origin form submission refused", http.StatusForbidden)
		return
	}
	force := r.FormValue("force") != ""
	recovered := false
	if a.cfg.Proof != nil {
		// First, and with Check rather than Admit: every outcome below
		// redirects, the next GET mints a new challenge, and the spend
		// must land before a probe or a mail does.
		adm := a.cfg.Proof.Check(r, pow.Want{Scope: ProofScope})
		if !adm.OK {
			a.cfg.Logger.Debug("rastrillo/auth: sign-in refused at the front door", "reason", adm.Reason, "also", adm.Also)
			a.noteAttempt(w, attemptProblem, r.FormValue("address"), false)
			a.redirect(w, r, a.problemURL("check", true, force))
			return
		}
		recovered = adm.Recovered()
	}
	address := r.FormValue("address")
	var method signin.Method
	if force {
		method = signin.MethodMagicLink
	}

	next, err := a.flow.Begin(r.Context(), address, clientip.From(r, a.hops), method)
	switch {
	case errors.Is(err, signin.ErrRateLimited):
		a.noteAttempt(w, attemptProblem, address, false)
		a.redirect(w, r, a.problemURL("rate", recovered, force))
		return
	case errors.Is(err, signin.ErrBadAddress):
		a.noteAttempt(w, attemptProblem, address, false)
		a.redirect(w, r, a.problemURL("address", recovered, force))
		return
	case err != nil:
		a.cfg.Logger.Error("rastrillo/auth: begin sign-in", "err", err)
		a.noteAttempt(w, attemptProblem, address, false)
		a.redirect(w, r, a.problemURL("1", recovered, force))
		return
	}
```

(the rest of `Begin` is unchanged) and add after `Begin`:

```go
// problemURL is the sign-in page for a problem. rec keeps recovery
// sticky: a visitor whose password manager fills the honeypot was
// given a trapless form, and an ordinary form after their next typo
// would trap them again. force keeps the send-a-link-instead choice a
// failed keymail exchange offered: without it, a refusal would drop it
// and Begin would classify the address back to the failing provider.
// Both are attacker-controllable and select nothing a script can use:
// a recovery form costs the same proof.
func (a *Auth) problemURL(problem string, rec, force bool) string {
	q := url.Values{"err": {problem}}
	if rec {
		q.Set("rec", "1")
	}
	if force {
		q.Set("force", "1")
	}
	return a.cfg.SigninPath + "?" + q.Encode()
}
```

(add `net/url` and the `pow` import to `handlers.go`.)

`auth/signin.go`: add to the `SigninProblem` constants:

```go
	// ProblemCheck: the front door refused the submission. The page
	// shows it on the ask step with a recovery challenge.
	ProblemCheck SigninProblem = "check"
```

add to `SigninState`, after `ForgetPath`:

```go
	// Force carries force=1 from the query: the partial renders the
	// send-a-link-instead input from it rather than from the problem,
	// because a refusal replaces the keymail problem and the visitor
	// would otherwise loop back to a provider that just failed them.
	Force bool
	// Proof is the challenge the Begin form carries; nil with
	// Config.ProofOff. A recovery challenge when the query has rec=1.
	// Minting writes nothing, so SigninState still touches no database.
	Proof *pow.Form
```

in `SigninState`, replace the first two lines with:

```go
	q := r.URL.Query()
	st := SigninState{BeginPath: a.cfg.BeginPath, ForgetPath: a.cfg.ForgetPath, Force: q.Get("force") == "1"}
	if a.cfg.Proof != nil {
		now := a.now()
		f := a.cfg.Proof.Form(now, ProofScope)
		if q.Get("rec") == "1" {
			f = a.cfg.Proof.Recovery(now, ProofScope)
		}
		st.Proof = &f
	}
```

and in `outcome`'s `switch q.Get("err")` add `case "check": return StepAsk, ProblemCheck`.

Update the tests that build an `Auth` directly: `newTestAuth`'s base config gains `ProofOff: true` (a `mut` that sets `Proof` must also clear it, as `newProofAuth` does); `newTestAuthDB` merges `pow.Schema` (`migrate.Merge(sessions.Schema, Schema, secondfactor.Schema, pow.Schema)`); the success cases in `TestNewValidatesConfig` and both `New(Config{...})` calls in `keymail_test.go` gain `ProofOff: true`.

- [ ] **Step 5: Run the package**

Run: `GOFLAGS=-mod=mod go test ./auth/ -count=1`
Expected: PASS (existing tests unchanged in behaviour: they run with `ProofOff`, and their `err=` redirects carry neither `rec` nor `force` unless they posted `force`; any test that asserted the exact Location `?err=rate` while posting `force=1` now sees `?err=rate&force=1` — update those assertions to parse the query, and say why in the test: the fallback choice now survives an error).

- [ ] **Step 6: Commit**

```bash
git add auth/
git commit -m "auth: Begin checks a pow challenge first, and refuses to boot without a decision" -m "An anonymous visitor could make Begin resolve and probe a domain of their choosing, send mail and spend another address's rate budget with one POST. The front door now runs before all of it; refusals carry rec and force so a trapped password manager and a failed keymail provider each lead somewhere. An app that has not chosen Proof or ProofOff fails at boot rather than refusing every visitor after an upgrade.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 9: The sign-in partial, `form-foot`, and the catalogs

**Files:**
- Modify: `ui/partials/signin.html`, `ui/partials/form-foot.html`, `locales/*.toml` (all 12), `auth/signinscreen_browser_test.go` (wire a Guard and serve `pow.Assets()`)
- Test: `ui/signin_test.go` (append), `ui/ui_test.go` (append)

**Interfaces:**
- Consumes: `SigninState.Proof *pow.Form`, `SigninState.Force`, `Form.Fields/Attrs/Script/StatusLine/NeedsScript`, the approved strings in `~/.cache/pow-admission/approved-copy.md` (Task 1).

- [ ] **Step 1: Add the three catalog keys**

In `locales/en.toml`, beside the other `rastrillo.ui.signin_problem_*` keys and in the same style, add the approved English for `rastrillo.ui.signin_problem_check`, `rastrillo.ui.pow_status` and `rastrillo.ui.pow_noscript` verbatim from `approved-copy.md`. Translate each into the other 11 locales (`ls locales/`), placed beside the same neighbours. No em dashes in any of them.

- [ ] **Step 2: Write the failing partial tests**

Append to `ui/signin_test.go` (it already has `signinData`, `render`, `signinStates`):

```go
func proofGuard(t *testing.T, difficulty int) *pow.Guard {
	t.Helper()
	g, err := pow.New(pow.Config{InstanceKey: "ui", Nonces: pow.MemoryNonces(), Difficulty: difficulty,
		ScriptURL: "/pow/pow.js", WorkerURL: "/pow/pow-worker.js"})
	if err != nil {
		t.Fatal(err)
	}
	return g
}

var (
	beginForm  = regexp.MustCompile(`(?s)<form rst-signin-form method="post" action="/signin"[^>]*>.*?</form>`)
	forgetFull = regexp.MustCompile(`(?s)<form rst-signin-form method="post" action="/signin/forget">.*?</form>`)
)

func TestSigninRendersTheChallengeOnTheOneBeginForm(t *testing.T) {
	g := proofGuard(t, 12)
	for _, c := range signinStates() {
		st := c.st
		f := g.Form(time.Now(), auth.ProofScope)
		st.Proof = &f
		out := render(t, "signin", signinData(st))
		forms := beginForm.FindAllString(out, -1)
		switch st.Door() {
		case "sent", "continue":
			if len(forms) != 0 || strings.Contains(out, "pow_seal") {
				t.Errorf("%s: a %s page carries a challenge", c.name, st.Door())
			}
			continue
		}
		if len(forms) != 1 {
			t.Fatalf("%s: %d Begin forms, want exactly 1", c.name, len(forms))
		}
		form := forms[0]
		for _, want := range []string{`data-pow-form`, `name="pow_seal"`, `data-pow-submit`, `disabled`, `data-pow-status`, `<noscript>`} {
			if !strings.Contains(form, want) {
				t.Errorf("%s: Begin form lacks %s", c.name, want)
			}
		}
		if strings.Count(out, `<script type="module" src="/pow/pow.js">`) != 1 {
			t.Errorf("%s: pow.js is not loaded exactly once", c.name)
		}
		if forget := forgetFull.FindString(out); strings.Contains(forget, "pow_") {
			t.Errorf("%s: the Forget form carries a challenge", c.name)
		}
	}
}

func TestSigninNoProofRendersAnEnabledSubmit(t *testing.T) {
	g := proofGuard(t, pow.NoProof)
	f := g.Form(time.Now(), auth.ProofScope)
	out := render(t, "signin", signinData(auth.SigninState{Step: auth.StepAsk, Proof: &f}))
	form := beginForm.FindString(out)
	if strings.Contains(form, "disabled") || strings.Contains(out, "pow.js") {
		t.Fatal("a NoProof sign-in form renders a disabled submit or loads a module it does not need")
	}
	if !strings.Contains(form, `name="pow_seal"`) {
		t.Fatal("a NoProof sign-in form lost its token")
	}
}

func TestSigninForceComesFromState(t *testing.T) {
	g := proofGuard(t, 12)
	f := g.Recovery(time.Now(), auth.ProofScope)
	out := render(t, "signin", signinData(auth.SigninState{Step: auth.StepAsk, Problem: auth.ProblemCheck, Force: true, Proof: &f}))
	if !strings.Contains(beginForm.FindString(out), `name="force" value="1"`) {
		t.Fatal("a check refusal after a keymail failure dropped the send-a-link-instead choice")
	}
}
```

(Add `time` and `amadan.net/rastrillo/rastrillo/pow` to the imports.)

For `form-foot`, append to `ui/ui_test.go`:

```go
func TestFormFootRendersTheProofSubmit(t *testing.T) {
	g := proofGuard(t, 12)
	f := g.Form(time.Now(), "s")
	out := render(t, "form-foot", map[string]any{"Submit": "Sign in", "Proof": &f})
	for _, want := range []string{`data-pow-submit`, `disabled`, `data-pow-status`, `<noscript>`} {
		if !strings.Contains(out, want) {
			t.Errorf("form-foot with Proof lacks %s", want)
		}
	}
	if plain := render(t, "form-foot", map[string]any{"Submit": "Save"}); strings.Contains(plain, "disabled") {
		t.Error("form-foot without Proof renders a disabled submit")
	}
}
```

- [ ] **Step 3: Run them to see them fail**

Run: `GOFLAGS=-mod=mod go test ./ui/ -run 'Signin|FormFoot' -count=1`
Expected: FAIL (no challenge rendered).

- [ ] **Step 4: Change `ui/partials/signin.html`**

In the header comment's Keys block, add: `State.Proof, when set, is rendered on the one Begin form this state shows: its fields, its attributes, a disabled submit the module enables, the status line and a <noscript> line, and the module once. The Forget form carries none of it.`

At the top of the `signin` define, after `{{$preview := opt . "Preview"}}`, add `{{$proof := $s.Proof}}{{$gated := and $proof $proof.NeedsScript}}`.

On each of the three Begin forms (keymail one-tap, link one-tap, ask), change the opening tag to end `action="{{$s.BeginPath}}"{{with $proof}} {{.Attrs}}{{end}}>` and insert `{{with $proof}}{{.Fields}}{{end}}` directly after it. On each of their submit buttons add `{{if $gated}} disabled data-pow-submit{{end}}` before the closing `>`. After each of those buttons add:

```
{{if $gated}}{{$proof.StatusLine (T "rastrillo.ui.pow_status")}}<noscript><p>{{T "rastrillo.ui.pow_noscript"}}</p></noscript>
{{end}}
```

Change the ask form's force condition from `{{if eq $s.Problem "keymail"}}<input type="hidden" name="force" value="1">` to `{{if or $s.Force (eq $s.Problem "keymail")}}<input type="hidden" name="force" value="1">` (the button label condition stays on the problem).

Before the final `</div>\n</section>{{end}}` of the `signin` define, add `{{if and $gated (ne $door "sent") (ne $door "continue") (not $preview)}}{{$proof.Script}}{{end}}`.

- [ ] **Step 5: Change `ui/partials/form-foot.html`**

Add to its Keys comment: `Proof  *pow.Form, optional: when it needs a script, the submit is rendered disabled for pow.js to enable, with the status line and a <noscript> line. Render .Proof.Fields and .Proof.Attrs on the form and .Proof.Script once on the page yourself.` Replace the define with:

```
{{define "form-foot"}}{{$p := opt . "Proof"}}{{$gated := and $p $p.NeedsScript}}<div rst-form-foot>
  <button rst-btn="primary lg" type="submit"{{if $gated}} disabled data-pow-submit{{end}}>{{.Submit}}</button>
  {{- if .CancelHref}}
  <a rst-btn="lg" href="{{.CancelHref}}">{{.CancelLabel}}</a>
  {{- end}}
</div>{{if $gated}}
{{$p.StatusLine (T "rastrillo.ui.pow_status")}}<noscript><p>{{T "rastrillo.ui.pow_noscript"}}</p></noscript>{{end}}{{end}}
```

- [ ] **Step 6: Wire the auth browser test**

In `auth/signinscreen_browser_test.go` around line 67, build a Guard over the same database before `New` and serve the assets:

```go
		powAssets := rastrillo.NewAssets(pow.Assets())
		g, err := pow.New(pow.Config{InstanceKey: "browser-instance-key", Nonces: pow.SQLNonces(d.Writer()),
			Difficulty: 10, MinAge: 100 * time.Millisecond,
			ScriptURL: "/pow" + powAssets.Path("pow.js"), WorkerURL: "/pow" + powAssets.Path("pow-worker.js")})
		if err != nil {
			t.Fatalf("pow.New: %v", err)
		}
		mux.Handle("GET /pow/", http.StripPrefix("/pow/", powAssets.Handler()))
		a, err := New(Config{DB: d.Writer(), Origin: origin, InstanceKey: "browser-instance-key", Mailer: app.mail, SigninScreen: true, Proof: g})
```

(apply `pow.Schema` in that test's migration set; name the mux variable whatever that function already uses). The existing browser journeys then exercise the real module: a journey that clicks Continue now waits for the solve, which at 10 bits is milliseconds.

- [ ] **Step 7: Run**

Run: `GOFLAGS=-mod=mod go test ./ui/ ./auth/ -count=1 && TMPDIR="${TMPDIR:-/var/tmp}" go test -tags browser -p 1 ./auth/ ./ui/ -count=1`
Expected: PASS, including ui's unresolved-catalog-key check for the three new keys in every locale.

- [ ] **Step 8: Commit**

```bash
git add ui/partials/signin.html ui/partials/form-foot.html locales/ ui/ auth/signinscreen_browser_test.go
git commit -m "ui: render the sign-in challenge, with a status line that survives a failed module" -m "The challenge has to be on whichever single Begin form a state shows and never on Forget; the submit is disabled until pow.js enables it, and the status line and noscript line say why when it cannot. The send-a-link-instead input now follows SigninState.Force, so a refusal no longer drops it.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 10: `password`: Check before the limiter

**Files:**
- Modify: `password/handlers.go`
- Test: `password/handlers_test.go` (`newTestEnv` defaults `ProofOff: true`), `password/proof_test.go` (new)

**Interfaces:**
- Consumes: `pow.Guard.Check/Form/Recovery/Bound`, `pow.Admission.Recovered`, `powtest.Fill`.
- Produces: `password.Config.Proof *pow.Guard`, `Config.ProofOff bool`, `password.ErrProofUnset`, `password.ErrProofMode`, `password.ScopeSignin = "rastrillo/password/signin"`, `password.ScopeSignup = "rastrillo/password/signup"`, `PageData.Proof *pow.Form`.

- [ ] **Step 1: Write the failing tests**

`password/proof_test.go`:

```go
package password_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/password"
	"amadan.net/rastrillo/rastrillo/pow"
	"amadan.net/rastrillo/rastrillo/pow/powtest"
)

func newProofEnv(t *testing.T) testEnv {
	t.Helper()
	g, err := pow.New(pow.Config{InstanceKey: "k", Nonces: pow.MemoryNonces(), Difficulty: 8,
		MinAge: time.Millisecond, ScriptURL: "/pow/pow.js", WorkerURL: "/pow/pow-worker.js"})
	if err != nil {
		t.Fatal(err)
	}
	return newTestEnv(t, func(c *password.Config) { c.Proof, c.ProofOff = g, false })
}

func lastPage(t *testing.T, r *renderRecorder) recordedPage {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.calls) == 0 {
		t.Fatal("nothing rendered")
	}
	return r.calls[len(r.calls)-1]
}

// page GETs path and fills its challenge into form, as a browser would.
func page(t *testing.T, env testEnv, h http.HandlerFunc, rec *renderRecorder, form url.Values) url.Values {
	t.Helper()
	w := httptest.NewRecorder()
	h(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("Cache-Control = %q on a page carrying a single-use token, want no-store", cc)
	}
	d := lastPage(t, rec).data
	if d.Proof == nil {
		t.Fatal("PageData carries no challenge although Config.Proof is set")
	}
	time.Sleep(2 * time.Millisecond)
	return powtest.Fill(t, []byte(d.Proof.Fields()), form)
}

func postTo(h http.HandlerFunc, form url.Values) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h(w, r)
	return w
}

func TestNewRequiresAProofDecision(t *testing.T) {
	base := password.Config{Sessions: newTestSessions(t), Lookup: newUserStore().lookup, RenderSignin: (&renderRecorder{}).render}
	if _, err := password.New(base); !errors.Is(err, password.ErrProofUnset) {
		t.Fatalf("no decision = %v, want ErrProofUnset", err)
	}
	bound, _ := pow.New(pow.Config{InstanceKey: "k", Nonces: pow.MemoryNonces(), Bind: true, ScriptURL: "/p", WorkerURL: "/w"})
	c := base
	c.Proof = bound
	if _, err := password.New(c); !errors.Is(err, password.ErrProofMode) {
		t.Fatalf("bound = %v, want ErrProofMode", err)
	}
}

func TestSignupWithoutAChallengeCreatesNoRow(t *testing.T) {
	env := newProofEnv(t)
	w := postTo(env.h.Signup, url.Values{"email": {"new@example.com"}, "password": {"long enough pw"}})
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422", w.Code)
	}
	if _, _, err := env.store.lookup(context.Background(), "new@example.com"); err == nil {
		t.Fatal("a refused signup created a user")
	}
	if p := lastPage(t, env.signup); p.data.Proof == nil || strings.Contains(string(p.data.Proof.Fields()), `name="hp"`) {
		t.Fatal("a refused signup re-rendered without a trapless recovery challenge")
	}
}

func TestSignupWithAChallengeSucceeds(t *testing.T) {
	env := newProofEnv(t)
	form := page(t, env, env.h.SignupPage, env.signup, url.Values{"email": {"new@example.com"}, "password": {"long enough pw"}})
	if w := postTo(env.h.Signup, form); w.Code != http.StatusSeeOther {
		t.Fatalf("status %d, want 303", w.Code)
	}
}

func TestWrongPasswordRerendersAFreshChallengeAndKeepsRecovery(t *testing.T) {
	env := newProofEnv(t)
	hash, err := password.Hash("right password")
	if err != nil {
		t.Fatal(err)
	}
	env.store.create(context.Background(), "amy@example.com", hash)
	form := page(t, env, env.h.SigninPage, env.signin, url.Values{"email": {"amy@example.com"}, "password": {"wrong"}})
	w := postTo(env.h.Signin, form)
	p := lastPage(t, env.signin)
	if w.Code != http.StatusUnprocessableEntity || p.data.Proof == nil || p.data.Proof.Nonce == form.Get("pow_nonce") {
		t.Fatalf("wrong password: status %d, fresh challenge %v", w.Code, p.data.Proof != nil)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("a re-render carrying a token is cacheable")
	}
	// A refusal hands back a recovery form; a wrong password on THAT
	// form must hand back a recovery form again.
	postTo(env.h.Signin, url.Values{"email": {"amy@example.com"}, "password": {"x"}})
	rec := lastPage(t, env.signin).data.Proof
	again := powtest.Fill(t, []byte(rec.Fields()), url.Values{"email": {"amy@example.com"}, "password": {"wrong again"}})
	postTo(env.h.Signin, again)
	if strings.Contains(string(lastPage(t, env.signin).data.Proof.Fields()), `name="hp"`) {
		t.Fatal("a wrong password after a recovery brought the trap back")
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `GOFLAGS=-mod=mod go test ./password/ -count=1`
Expected: compile failure.

- [ ] **Step 3: Implement in `password/handlers.go`**

Imports: add `"amadan.net/rastrillo/rastrillo/pow"`.

Constants and errors near `wrongCredentials`:

```go
// checkFailed is the message for a submission the front door refused.
// (Approved copy: see Task 1.)
const checkFailed = "<approved password checkFailed string>"

// The scopes password's challenges are issued and checked under, so a
// Guard shared with auth or the app's own forms keeps tokens apart.
const (
	ScopeSignin = "rastrillo/password/signin"
	ScopeSignup = "rastrillo/password/signup"
)

var (
	ErrProofUnset = errors.New("rastrillo/password: Config.Proof is unset: build a pow.Guard (serve pow.Assets(), apply pow.Schema) and set Config.Proof, or set Config.ProofOff")
	ErrProofMode  = errors.New("rastrillo/password: Config.Proof is a bound pow.Guard; password forms need an unbound one")
)
```

`PageData` gains:

```go
	// Proof is the challenge the form must carry; nil with ProofOff.
	// Render .Proof.Fields and .Proof.Attrs on the form, .Proof.Script
	// once on the page, and pass it to form-foot as "Proof".
	Proof *pow.Form
```

`Config` gains (with the same comments as auth's, adjusted):

```go
	Proof    *pow.Guard
	ProofOff bool
```

`New` gains, after the existing required-field checks:

```go
	switch {
	case cfg.Proof == nil && !cfg.ProofOff:
		return nil, ErrProofUnset
	case cfg.Proof != nil && cfg.ProofOff:
		return nil, errors.New("rastrillo/password: Config.Proof and Config.ProofOff are both set; choose one")
	case cfg.Proof != nil && cfg.Proof.Bound():
		return nil, ErrProofMode
	}
```

Add the render seam every page goes through:

```go
// show renders one of the two pages. no-store always: the page carries
// a single-use token, and a cached copy is one token for every visitor
// who loads it, spent by the first. recovered picks the recovery
// challenge, which is trapless; once a visitor has one, every re-render
// for them keeps it, or a password manager that filled the trap fills
// it again on the next wrong password.
func (h *Handlers) show(w http.ResponseWriter, r *http.Request, render func(http.ResponseWriter, *http.Request, PageData),
	scope string, status int, recovered bool, d PageData) {
	w.Header().Set("Cache-Control", "no-store")
	if h.cfg.Proof != nil {
		now := time.Now()
		f := h.cfg.Proof.Form(now, scope)
		if recovered {
			f = h.cfg.Proof.Recovery(now, scope)
		}
		d.Proof = &f
	}
	if status != 0 {
		w.WriteHeader(status)
	}
	render(w, r, d)
}

// gate runs the front door. On refusal it has already answered.
func (h *Handlers) gate(w http.ResponseWriter, r *http.Request, render func(http.ResponseWriter, *http.Request, PageData),
	scope, email string) (recovered, ok bool) {
	if h.cfg.Proof == nil {
		return false, true
	}
	adm := h.cfg.Proof.Check(r, pow.Want{Scope: scope})
	if !adm.OK {
		h.cfg.Logger.Debug("rastrillo/password: refused at the front door", "scope", scope, "reason", adm.Reason, "also", adm.Also)
		h.show(w, r, render, scope, http.StatusUnprocessableEntity, true,
			PageData{Error: checkFailed, Email: email, ReturnTo: r.FormValue("return_to")})
		return false, false
	}
	return adm.Recovered(), true
}
```

Then route every existing render through `show`:
- `SigninPage`: `h.show(w, r, h.cfg.RenderSignin, ScopeSignin, 0, false, PageData{ReturnTo: r.URL.Query().Get("return_to")})`
- `SignupPage`: the same with `RenderSignup`, `ScopeSignup`.
- `Signin`: after `email`/`submitted` are read, `recovered, ok := h.gate(w, r, h.cfg.RenderSignin, ScopeSignin, email); if !ok { return }`. The 429 branch becomes `h.show(w, r, h.cfg.RenderSignin, ScopeSignin, http.StatusTooManyRequests, recovered, PageData{...})`. `rerenderSignin` gains a `recovered bool` parameter and becomes `h.show(w, r, h.cfg.RenderSignin, ScopeSignin, http.StatusUnprocessableEntity, recovered, PageData{...})`; pass `recovered` at both call sites.
- `Signup`: the same: gate after `email`, the 429 and 403 branches through `show` with `recovered`, and `rerenderSignup(w, r, msg, email, recovered)`.

The gate runs before the limiter in both, as the spec requires: a blocked attempt still costs a solve, and a refusal costs no limiter unit.

Fill `checkFailed` with the approved string from `~/.cache/pow-admission/approved-copy.md`.

In `password/handlers_test.go`, `newTestEnv`'s base config gains `ProofOff: true`.

- [ ] **Step 4: Run**

Run: `GOFLAGS=-mod=mod go test ./password/ -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add password/
git commit -m "password: sign-in and sign-up check a pow challenge before the limiter" -m "Signup created a user row for every fresh address and counted only failures, so a script with a list of addresses was unlimited. Both forms now run the front door first, re-render with a fresh or recovery challenge, and are never cacheable, because a cached page is one single-use token shared by every visitor.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---
### Task 11: The notes example end to end, and the passkey test

**Files:**
- Modify: `examples/notes/internal/notes/app.go`, `examples/notes/internal/notes/migrations.go`, `examples/notes/internal/notes/templates/signin.html`, `examples/notes/internal/notes/templates/signup.html`, `examples/notes/cmd/notes/main.go`, `examples/notes/internal/notestest/harness_test.go`, `passkey/signinscreen_test.go:201`

**Interfaces:**
- Consumes: `pow.New`, `pow.Assets`, `pow.SQLNonces`, `pow.Schema`, `(*pow.Guard).Sweep`, `password.Config.Proof`, `PageData.Proof`, `form-foot`'s `Proof` key, `powtest.Fill`, `rastrillo.NewAssets`, `(*background.Group).Loop`.
- Produces: `notes.App(d *db.DB, origin, instanceKey string, bg *background.Group, logger *slog.Logger) (*http.ServeMux, error)`.

- [ ] **Step 1: The passkey test**

`passkey/signinscreen_test.go:201` tests the passkey door, not Begin: add `ProofOff: true` to its `auth.Config`. Run `GOFLAGS=-mod=mod go test ./passkey/ -count=1`; expected PASS.

- [ ] **Step 2: Make the notes harness drive the real form (failing first)**

In `examples/notes/internal/notestest/harness_test.go`:

`newApp` passes the new arguments and stops the group at cleanup:

```go
	bg := &background.Group{}
	t.Cleanup(bg.Stop)
	mux, err := notes.App(d, origin, "notes-test-instance-key", bg, slog.New(slog.NewTextHandler(io.Discard, nil)))
```

Add, beside `postForm`:

```go
// filled GETs path and returns form with the page's challenge solved,
// as the browser would post it. It waits out the example's 500ms
// minimum age first; a person always has.
func (cl *client) filled(path string, form url.Values) url.Values {
	cl.t.Helper()
	resp, err := cl.c.Get(cl.ts.URL + path)
	if err != nil {
		cl.t.Fatalf("GET %s: %v", path, err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	time.Sleep(600 * time.Millisecond)
	return powtest.Fill(cl.t, body, form)
}
```

and change `signup` and `signin` to post `cl.filled("/signup", …)` / `cl.filled("/signin", …)` instead of the bare values. Add one test:

```go
func TestSignupWithoutTheChallengeIsRefused(t *testing.T) {
	ts := newApp(t)
	cl := newClient(t, ts)
	resp := cl.postForm("/signup", url.Values{"email": {"bot@example.com"}, "password": {"long enough pw"}})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("signup without a challenge: %d, want 422", resp.StatusCode)
	}
}
```

(use whatever the file's client constructor is called; imports `time`, `amadan.net/rastrillo/rastrillo/background`, `amadan.net/rastrillo/rastrillo/pow/powtest`.)

Run: `cd examples/notes && GOFLAGS=-mod=mod go test ./... -count=1`
Expected: compile failure (`notes.App` arity).

- [ ] **Step 3: Wire the example**

`migrations.go`: `var BootSchema = migrate.Merge(sessions.Schema, pow.Schema, genSchema, Schema)`, and extend its comment: pow's spent-nonce table is applied here because `pow.New` checks it exists.

`app.go`: change the signature to `func App(d *db.DB, origin, instanceKey string, bg *background.Group, logger *slog.Logger) (*http.ServeMux, error)`, and after `sess` is built add:

```go
	// The front door for sign-in and sign-up. The Guard is built after
	// migrate.Apply above, because New checks pow's table exists; the
	// app owns it, so the app sweeps it.
	powAssets := rastrillo.NewAssets(pow.Assets())
	guard, err := pow.New(pow.Config{
		InstanceKey: instanceKey,
		Nonces:      pow.SQLNonces(writer),
		MinAge:      500 * time.Millisecond,
		ScriptURL:   "/pow" + powAssets.Path("pow.js"),
		WorkerURL:   "/pow" + powAssets.Path("pow-worker.js"),
	})
	if err != nil {
		return nil, err
	}
	bg.Loop(context.Background(), 10*time.Minute, func() {
		if err := guard.Sweep(time.Now()); err != nil {
			logger.Warn("sweep spent challenges", "err", err)
		}
	})
```

add `Proof: guard,` to `password.Config`, and mount the assets beside the other static routes: `r.Handle("/pow/*", http.StripPrefix("/pow/", powAssets.Handler()))`. Use `Difficulty` from Task 12's measurement if it differs from the default; until then leave it unset.

`templates/signin.html` (and `signup.html` the same way, with its own action and Submit):

```
{{define "content"}}
{{template "page-header" dict "Title" "Sign in"}}
{{with .Content}}
{{template "form-error" .Error}}
<form rst-form method="post" action="/signin"{{with .Proof}} {{.Attrs}}{{end}}>
{{with .Proof}}{{.Fields}}{{end}}
<input type="hidden" name="return_to" value="{{.ReturnTo}}">
{{template "field-text" dict "Name" "email" "Label" "Email" "Type" "email" "Value" .Email "Required" true "Autofocus" true "Autocomplete" "email"}}
{{template "field-text" dict "Name" "password" "Label" "Password" "Type" "password" "Required" true "Autocomplete" "current-password"}}
{{template "form-foot" dict "Submit" "Sign in" "Proof" .Proof}}
</form>
{{with .Proof}}{{.Script}}{{end}}
{{end}}
<p><a href="/signup">Need an account? Sign up</a></p>
{{end}}
```

`cmd/notes/main.go`: read the key and own the group:

```go
	instanceKey := os.Getenv("NOTES_INSTANCE_KEY")
	if instanceKey == "" {
		// Loud for the same reason as the origin: the key seals every
		// sign-in challenge, and a default shared by every copy of this
		// example is a key anybody can read.
		instanceKey = "notes-development-only-instance-key"
		logger.Warn("NOTES_INSTANCE_KEY not set; using a development-only key")
	}
```

and `bg := &background.Group{}` before `notes.App(d, origin, instanceKey, bg, logger)`, then `opts.Background = bg` before `rastrillo.Serve(opts)` (Serve stops it after draining requests and before the deferred `d.Close()`).

- [ ] **Step 4: Run**

Run: `cd examples/notes && GOFLAGS=-mod=mod go test ./... -count=1 && go vet ./...`
Expected: PASS. Then from the repo root: `make example-notes` if that target runs more than the tests; expected PASS.

- [ ] **Step 5: Commit**

```bash
git add examples/notes passkey/signinscreen_test.go
git commit -m "examples/notes: wire the pow front door end to end" -m "The example is what an app copies, and password now refuses to boot without a Proof decision. It shows the order that matters (migrate pow.Schema before pow.New), one Guard swept from the app's background group, the challenge on both forms through form-foot, and a harness that drives the real form instead of posting around it.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 12: Measure the difficulty (controller, then Paul)

The spec leaves sign-in's difficulty open until measured. A subagent can run the throttled-Chromium measurement; only Paul can run it on a real phone.

**Files:**
- Test: `pow/measure_browser_test.go` (new, `//go:build browser`, skipped unless `POW_MEASURE=1`)

- [ ] **Step 1: Write the measurement**

```go
//go:build browser

package pow_test

import (
	"fmt"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
)

// TestMeasureSolveTimes times the shipped solver in Chromium with the
// CPU throttled, to choose a difficulty from measurement rather than a
// guess. Solve time is geometric, so it reports p95 and p99, not the
// mean: calibrating on the mean ships a form that hangs for one visitor
// in a hundred. Opt-in: it takes minutes.
//
//	POW_MEASURE=1 TMPDIR=/var/tmp go test -tags browser -run Measure -v ./pow/
func TestMeasureSolveTimes(t *testing.T) {
	if os.Getenv("POW_MEASURE") == "" {
		t.Skip("set POW_MEASURE=1 to measure")
	}
	rig := powRig(t)
	rig.Run(chromedp.Navigate(rig.Origin + "/"))
	for _, rate := range []float64{1, 4, 6} {
		rig.Run(emulation.SetCPUThrottlingRate(rate))
		for bits := 12; bits <= 18; bits++ {
			samples := 60
			if bits >= 17 {
				samples = 30
			}
			var ms []float64
			for i := 0; i < samples; i++ {
				nonce := fmt.Sprintf("%032x", time.Now().UnixNano()+int64(i))
				var took float64
				rig.Run(chromedp.Evaluate(fmt.Sprintf(`(async () => {
					const m = await import("/pow/powcore.js");
					const t0 = performance.now();
					m.solve(%q, "", %d, null);
					return performance.now() - t0;
				})()`, nonce, bits), &took, awaitPromise))
				ms = append(ms, took)
			}
			sort.Float64s(ms)
			q := func(p float64) float64 { return ms[int(p*float64(len(ms)-1))] }
			t.Logf("throttle %gx  %2d bits  p50 %6.0fms  p95 %6.0fms  p99 %6.0fms", rate, bits, q(0.5), q(0.95), q(0.99))
		}
	}
}
```

- [ ] **Step 2: Run it and record the table**

Run: `POW_MEASURE=1 TMPDIR="${TMPDIR:-/var/tmp}" go test -tags browser -run Measure -v -timeout 60m ./pow/`
Copy the logged table into `~/.cache/pow-admission/measurements.md` with the machine it ran on.

- [ ] **Step 3: Pick the recommendation by this rule, and show Paul**

The recommended sign-in difficulty is the largest number of bits whose **p95 at 6x throttle is at most 1000ms** (the one-tap returning visitor waits the whole solve; everyone else solves while typing). Present the table and the pick to Paul with the question: "Run it on a real mid-range Android too (open the notes example on the phone; the plan's Task 13 doc says how), or accept the throttled measurement?" Record his answer in `measurements.md`.

- [ ] **Step 4: Apply the number**

If the pick is not 18: set `Difficulty: <pick>` in `examples/notes/internal/notes/app.go`'s `pow.Config` with a comment citing the measurement, and use the same number in Task 13's docs. Leave `DefaultDifficulty` alone unless Paul says otherwise; its comment already says to measure before relying on it.

- [ ] **Step 5: Commit**

```bash
git add pow/measure_browser_test.go examples/notes/internal/notes/app.go
git commit -m "pow: measure solve times in throttled Chromium before recommending a difficulty" -m "DefaultDifficulty was an estimate. Sign-in's one-tap visitor waits for a whole solve, so the recommendation is the largest difficulty whose p95 stays under a second at 6x CPU throttle, taken from a measurement kept in the repo rather than a number nobody can rerun.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 13: Docs, SKILL.md and the CHANGELOG

**Files:**
- Modify: `docs/site/reference/pow.md` (rewrite), `docs/site/magic-links.md`, `docs/site/passwords.md`, `SKILL.md` (the auth, password and "Public forms" paragraphs), `CHANGELOG.md`

- [ ] **Step 1: Draft the public prose in scratch, then copy-review it**

Use the `humane-docs` skill's voice. Draft in `~/.cache/pow-admission/docs-draft.md`:

1. `docs/site/reference/pow.md`, rewritten around these sections, every claim taken from the spec and the code as built:
   - *What it is*: sealed single-use challenge, honeypot, proof of work, both halves shipped together; what it does not do (bulk attackers who pay for solves; persisted budgets).
   - *Wiring it up*: `migrate.Apply` with `pow.Schema` **before** `pow.New` (it checks the table, `ErrNoSchema`); `Config` fields and defaults; `NoProof`; `Bind` (off by default, and why); serving `pow.Assets()` behind `rastrillo.NewAssets`; the app owns the Guard and sweeps it from a `background.Group` loop.
   - *Rendering a form*: `Form`, `.Fields`, `.Attrs`, `.Script` once per page, `.StatusLine`, the disabled submit with `data-pow-submit`, `<noscript>`, `form-foot`'s `Proof` key; **never cache a page carrying a challenge**; `FollowOn` for prefilled and confirm screens; `Recovery` after a refusal (trapless, follow-on, sticky via `Recovered()`), and that refilling answers is the app's decision and needs the business write's idempotency (the traps: spec review findings 35 and 37 to 40, described in plain words).
   - *Checking a submission*: `Admit` then `Commit(ctx, tx)` in the handler's transaction (GORM: `tx.Statement.ConnPool`); `ErrSpent` means refuse, never 500; `Check` for redirect-after-POST handlers; the attempt allowance and `ReasonBusy`, with the stated trade-off.
   - *Requests on a form's behalf*: `Verify`, `Parent`, `whenSolved` and its rejection contract.
   - *Reasons*: a table of every `Reason` and what a visitor should be shown.
   - *Choosing a difficulty*: Task 12's table and rule, and how to rerun it.
2. `docs/site/magic-links.md`: a "The front door" section: `Config.Proof` / `ProofOff`, `ErrProofUnset`, `ErrProofMode`, the `?err=check&rec=1` problem, `force` surviving refusals. Replace lines 212-215's description of the limiter with what the code does: it counts every `Begin`, per IP (20) and per address (5) in a 15-minute fixed window, in memory, and a success does not reset it.
3. `docs/site/passwords.md`: the same section for `password` (`ScopeSignin`, `ScopeSignup`, `PageData.Proof`, `no-store`).
4. `CHANGELOG.md`: one entry under the next version, marked breaking, with upgrade steps: merge `pow.Schema` and apply it before building a Guard; build one Guard and pass it as `auth.Config.Proof` and `password.Config.Proof` (or set `ProofOff`); render `.Proof` in your password templates; refresh your vendored `busy.js`; `pow`'s API changed (`Check(r, binding)` → `Admit`/`Commit`/`Check(r, Want)`; `FormAttrs` → `Form.Attrs`; field names).

Then invoke the `copy-review` skill on the draft and apply Paul's edits verbatim.

- [ ] **Step 2: Write the reviewed prose into the files**

- [ ] **Step 3: Update SKILL.md**

SKILL.md is read by models instead of the source; keep it exact. Replace the "Public forms" paragraph (around `SKILL.md:315`) and add one sentence each to the auth and password paragraphs. Load-bearing facts, all of which must be present: default on, `ErrProofUnset` and its two fixes, `ErrProofMode`; apply `pow.Schema` before `pow.New` (`ErrNoSchema`); the app owns the Guard, shares it between auth and password, and sweeps it; `Admit` then `Commit(ctx, tx)` in the handler's transaction, `Check` for redirect-after-POST, GORM via `tx.Statement.ConnPool`; scope is sealed and namespaced; never cache a challenge page; `NoProof` needs no JavaScript, proof forms do; `Recovery` recovers the challenge, refilling answers is the app's risk; serve the JS from `pow.Assets()`, never vendor it.

Run: `GOFLAGS=-mod=mod go test -run SkillMD -count=1 .` (or whatever `skillmd_test.go`'s tests are called: `grep -n '^func Test' skillmd_test.go`). Expected: PASS within the 30,000-byte ceiling. If over, trim redundant prose elsewhere in SKILL.md; never drop a fact listed above. If the facts genuinely cannot fit, raise `skillBudget` and say why in its comment, as AGENTS.md requires.

- [ ] **Step 4: Commit**

```bash
git add docs/site SKILL.md CHANGELOG.md
git commit -m "docs: pow's new contract, the sign-in front door, and the upgrade steps" -m "An app upgrading now fails at boot until it chooses Proof or ProofOff, so the docs have to say exactly what to wire and in what order; SKILL.md carries the same facts for a model that reads it instead of the source. The magic-links page also described the rate limiter as counting failures and resetting on success, which the code has never done.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 14: The whole gate, review, and push

- [ ] **Step 1: Run the gate**

```bash
cd /home/paulca/amadan.net/rastrillo/rastrillo-pow-admission
GOFLAGS=-mod=mod go vet ./... && gofmt -l . && GOFLAGS=-mod=mod go test ./... -count=1
TMPDIR="${TMPDIR:-/var/tmp}" go test -tags browser -p 1 ./harness/ ./webauthn/ ./ui/ ./pow/ ./internal/designsystem/ ./auth/ -count=1
(cd examples/notes && GOFLAGS=-mod=mod go test ./... -count=1)
```

Expected: all PASS, `gofmt -l` prints nothing. Also run `make ci` if time allows; it is what the forge runs.

- [ ] **Step 2: Whole-branch review by Astra**

This session runs on Opus, so the review goes to Astra through `codex exec` (the invocation in memory `codex-exec-needs-sandbox-off`): read-only, `--add-dir` the Tito checkout and the module cache, prompt asking for numbered findings with severity, file:line evidence and a resolution, against the spec and this plan. Fix what holds up; re-run the gate.

- [ ] **Step 3: Push and describe**

```bash
git push origin pow-admission
amadan branch describe rastrillo/rastrillo pow-admission -summary "pow grows admission that commits with the write, a v2 seal with scope and expiry, opt-in binding, a no-JavaScript tier and challenge-only recovery; auth and password sign-in and sign-up run it by default and refuse to boot without a decision." -notes "Spec and plan in docs/superpowers. Breaking for any app using auth or password: see CHANGELOG. Tito Go's migration is a separate titogo plan."
amadan ci status rastrillo/rastrillo -branch pow-admission
```

Expected: CI passes. Landing (`amadan branch merge … -squash -expect <full sha> -message …`, then `make mirror`) is Paul's call; ask.

- [ ] **Step 4: Clean up scratch**

Delete `~/.cache/pow-admission/` and `~/.cache/pow-admission-review/` once Paul has what he needs from them.
