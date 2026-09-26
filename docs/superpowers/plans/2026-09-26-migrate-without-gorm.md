# migrate without GORM — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `migrate` — and through it `pow`, `sessions`, `blobs`, `jobs`, `eventlog`, `auth`, `password`, `passkey`, `totp`, `secondfactor` and `vault` — importable without linking GORM, so a raw-SQL app (Tito Go first) can adopt them.

**Architecture:** GORM reaches those eleven packages only through `migrate`: `Migration.Fn` is `func(*gorm.DB) error`, `Apply` takes `*db.DB` and builds a pinned `*gorm.DB`, and the model-diff generator lives in the same package. Go migrations change to `func(context.Context, migrate.Tx) error` over the pinned connection; a `migrate/gormfn` adapter keeps GORM-bodied migrations one wrapper away; `Apply` takes any `Writer() *sql.DB`, which `*db.DB` already satisfies; the generator moves to `migrate/modeldiff`. `secondfactor`'s one Go migration is rewritten in plain SQL.

**Tech Stack:** Go 1.26, `database/sql`, `modernc.org/sqlite`; GORM only in `gormfn` and `modeldiff`.

**Spec:** the roadmap in Tito Go, `docs/superpowers/plans/2026-09-26-tito-rastrillo-two-way-roadmap.md` (slice R1). The adoption constraint behind it is the 2026-09-13 Tito plan's "Database/migrations" section: `migrate.Apply` required Rastrillo's `db.DB` wrapper and its schema checks relied on GORM models.

## Global Constraints

- `make ci` is the gate (`GOFLAGS=-mod=mod`, `CGO_ENABLED=0`); add to the Makefile and `.amadan/ci.d/` together.
- No change to the ledger table, to checksums, to adoption, or to the BEGIN IMMEDIATE / pinned-connection / foreign-key-rebuild behaviour. A database migrated by v0.27 must be identical, byte for byte in `rastrillo_migrations`, after the same set runs under this change.
- Every existing `migrate.Apply(ctx, d, set)` call with `d *db.DB` compiles unchanged (91 call sites across the amadan.net apps).
- Pre-1.0 breaking change is allowed but must be named in CHANGELOG.md: `Migration.Fn`'s type, and `Generate`/`Change` moving to `migrate/modeldiff`. No app outside this repo uses either (checked 2026-09-26 across oficina/*, comercio, correomona, dineraya, keymail, seapointish, pullup, contra).
- Comments say why, naming the failure prevented (AGENTS.md).

## Review Focus

1. A GORM-bodied migration wrapped by `gormfn.Fn` still runs inside the same BEGIN IMMEDIATE as its ledger row: a failure rolls both back (existing `TestApplyFnRollsBackOnError`-style guarantee must move with it).
2. `gorm.Create` inside a wrapped Fn must not issue a nested BEGIN (the `SkipDefaultTransaction` trap) — the existing `TestApplyFnMigrationUsesGormCreate` moves to `gormfn` and must still pass.
3. A plain `Tx` Fn cannot start its own transaction: `Tx` must not expose `BeginTx`, so a migration that tries fails to compile rather than deadlocking or nesting.
4. The ctx deadline reaches a Fn's statements (the timeout test moves with it).
5. The dependency fence: `go list -deps` of the eleven packages contains no `gorm.io/` path. Watch it fail by re-adding the import before trusting it.

---

### Task 1: `migrate.Tx` and the new `Fn` type

**Files:**
- Modify: `migrate/set.go` (Migration.Fn type, drop gorm import, new `Tx` interface)
- Modify: `migrate/apply.go` (runOne passes `conn`; drop the pinned `*gorm.DB`; drop `db`, `gormlite`, `gorm` imports; `Apply` takes `WriterSource`)
- Test: `migrate/apply_test.go`

**Interfaces:**
- Produces:
  ```go
  // Tx is what a Go migration gets: the pinned connection, already inside
  // BEGIN IMMEDIATE. No BeginTx — a migration that opened its own
  // transaction would nest inside Apply's and SQLite refuses that.
  type Tx interface {
      ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
      QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
      QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
      PrepareContext(ctx context.Context, query string) (*sql.Stmt, error)
  }
  // Migration.Fn
  Fn func(ctx context.Context, tx Tx) error `json:"-"`
  // WriterSource is satisfied by *db.DB; Pool adapts a bare *sql.DB.
  type WriterSource interface{ Writer() *sql.DB }
  func Pool(w *sql.DB) WriterSource
  func Apply(ctx context.Context, d WriterSource, s *Set) (Result, error)
  ```

- [ ] **Step 1: Rewrite the Fn tests in apply_test.go against the new type** — `0002_seed` becomes `Fn: func(ctx context.Context, tx migrate.Tx) error { _, err := tx.ExecContext(ctx, "INSERT INTO t (n) VALUES (1)"); return err }`; the failing-Fn rollback test returns `errors.New("boom")` after an insert and asserts neither the row nor the ledger entry exists; add `TestApplyAcceptsABareSQLDB` calling `migrate.Apply(ctx, migrate.Pool(sqldb), set)` on a `sql.Open("sqlite", ...)` pool capped at one connection.
- [ ] **Step 2: Run `go test ./migrate/` — expect compile failure** (Tx, Pool undefined).
- [ ] **Step 3: Implement** — in runOne, `case m.Fn != nil: if err := m.Fn(ctx, conn); err != nil { return false, err }`; delete the `gorm.Open` block and the `g` parameter; `Apply` signature takes `WriterSource`; `type pool struct{ w *sql.DB }; func (p pool) Writer() *sql.DB { return p.w }`.
- [ ] **Step 4: `go test ./migrate/` passes** (the GORM-specific tests are moved in Task 2; delete them here in the same commit only after Task 2's copies exist — so Tasks 1 and 2 share one commit).

### Task 2: `migrate/gormfn` adapter

**Files:**
- Create: `migrate/gormfn/gormfn.go`, `migrate/gormfn/gormfn_test.go`

**Interfaces:**
- Consumes: `migrate.Tx`, `gormlite.Dialector{Conn gorm.ConnPool}` (`*sql.Conn` and `migrate.Tx` both satisfy `gorm.ConnPool`).
- Produces: `func Fn(f func(*gorm.DB) error) func(context.Context, migrate.Tx) error`

- [ ] **Step 1: Move the three GORM tests** (`TestApplyFnMigrationCompletesWithinTimeout`, `TestApplyFnMigrationUsesGormCreate`, the rollback case) into `gormfn_test.go`, wrapping each body as `gormfn.Fn(func(g *gorm.DB) error {...})`.
- [ ] **Step 2: Run — expect compile failure.**
- [ ] **Step 3: Implement** — `gorm.Open(gormlite.Dialector{Conn: tx}, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent), SkipDefaultTransaction: true})`, then `f(g.WithContext(ctx))`. Carry the SkipDefaultTransaction and pinned-connection comments over from apply.go verbatim — they name the failures.
- [ ] **Step 4: `go test ./migrate/...` passes. Commit Tasks 1+2:** `migrate: Go migrations take the pinned connection, GORM is an adapter`.

### Task 3: secondfactor's migration in plain SQL

**Files:** Modify `secondfactor/secondfactor.go:87-111`; test `secondfactor/secondfactor_test.go` (existing adoption tests must pass unchanged).

- [ ] **Step 1:** Rewrite `adoptPasskeyTables(ctx context.Context, tx migrate.Tx) error` with `tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='passkey_recovery_codes'").Scan(&n)` and `tx.ExecContext` for the INSERT OR IGNORE, the DROP and the DROP IF EXISTS. Same statements, same order.
- [ ] **Step 2:** `go test ./secondfactor/ ./passkey/` passes; `go list -f '{{.Imports}}' ./secondfactor` has no gorm.
- [ ] **Step 3: Commit** `secondfactor: its adoption migration is plain SQL`.

### Task 4: generator to `migrate/modeldiff`

**Files:** Create `migrate/modeldiff/modeldiff.go` (Generate, Change, recorder, gormOn, dropChanges, recorded, isDDL, ensureSemicolon, local index/cols/idxs helpers); move `migrate/diff_test.go` → `migrate/modeldiff/modeldiff_test.go`; create `migrate/schemasql.go` holding `SchemaSQL` (no GORM); modify `cmd/rastrillo/migration.go` and its test to import `modeldiff`.

- [ ] **Step 1:** Move the tests, change the package, run — expect compile failure.
- [ ] **Step 2:** Move the code; `go build ./... && go test ./migrate/... ./cmd/...` passes; `examples/*` (separate modules) build from their own directories.
- [ ] **Step 3: Commit** `migrate: the model generator moves to modeldiff`.

### Task 5: the dependency fence

**Files:** Create `migrate/nogorm_test.go`.

- [ ] **Step 1: Write the test** — run `go list -deps` (via `exec.Command("go", "list", "-deps", pkgs...)`, with `GOFLAGS=-mod=mod`) over `./migrate ./pow ./sessions ./blobs ./jobs ./eventlog ./auth ./password ./passkey ./totp ./secondfactor ./vault ./money ./csrf ./mail ./carlos`; fail naming any line with prefix `gorm.io/`. Comment: a raw-SQL app imports these to avoid a second persistence layer, and one stray import puts it back invisibly.
- [ ] **Step 2: Watch it fail** — temporarily add `_ "gorm.io/gorm"` to `pow/store.go`; run; expect FAIL naming pow. Remove it.
- [ ] **Step 3:** Pass. Commit `migrate: fence the raw-SQL packages against GORM`.

### Task 6: changelog, SKILL.md, gate

- [ ] CHANGELOG.md: breaking — `Migration.Fn` signature (wrap old bodies with `gormfn.Fn`), `Generate`/`Change` → `migrate/modeldiff`; new — `migrate.Tx`, `migrate.Pool`.
- [ ] SKILL.md: update any line naming `Fn func(*gorm.DB)` (check `skillmd_test.go` budget).
- [ ] `make ci` green; push; `amadan branch describe`; advance tasks.
