# GORM or plain SQL: what a Rastrillo app gains and pays for each

Status: record, 2026-09-26. Source: Tito Go's GORM experiment
(titogo `docs/superpowers/specs/2026-09-26-gorm-experiment-findings.md`,
PR "docs: record the GORM experiment and settle data access on SQL").
Tito settled on `database/sql` with hand-written SQL. This record keeps
every number and hazard from that experiment and restates them from a
Rastrillo app's side: the framework now supports both data layers, and
this is the evidence an implementor chooses with. `SKILL.md` §2 carries
the short version.

## The two choices

- **GORM, the scaffold's default.** `db.Open` (two pools over one SQLite
  file, routed per statement), models, `rastrillo migration generate`
  (`migrate/modeldiff`) diffing models into SQL migrations, Go
  migrations through `migrate/gormfn`, and owner scoping through
  `scope`.
- **Plain `database/sql`, the way Tito Go does it.** The app opens its
  own writer (one connection) and readers, applies migrations with
  `migrate.Apply(ctx, migrate.Pool(writer), set)`, writes Go migrations
  against `migrate.Tx`, tests against `dbtest.FromSet`, and uses only
  the packages in the Makefile's `make gorm-free` fence (`GORM_FREE`:
  on 2026-09-26 `migrate`, `pow`, `sessions`, `blobs`, `jobs`,
  `eventlog`, `auth`, `password`, `passkey`, `totp`, `secondfactor`,
  `vault`, `csrf`, `mail`, `carlos`, `crypto`, `flash`, `form`,
  `dbtest`, `clientip` and `nodetest`; the Makefile is the live list).
  No `db`, no `scope`, no `gormlite`.

Since R1 (`migrate` without GORM) nothing in the framework's subsystems
requires the ORM. The choice is only about the app's own data access.

## How it was measured

Tito converted `internal/partners` (7k product lines, 6k test lines, raw
SQL over `modernc.org/sqlite`) to idiomatic GORM on Rastrillo's pure-Go
dialector `gormlite`, in two separately measured slices:

- **A: plain CRUD.** `store.go`, `partners.go`, `members.go`,
  `customers.go`, `referrals.go`, `lines.go`, about 1.8k lines.
- **B: money.** `settle.go`, 1,029 lines of settlement logic with
  transactions, locking writes and aggregates.

The schema was unchanged; models mapped onto existing tables with
explicit names. The existing tests were the oracle, and all passed after
the conversion (one test line changed where a test reached an internal
helper). Tito's wider codebase, for scale: 6,065 `.Scan(` calls across
427 product files, 199 `ON CONFLICT` upserts and 62 `RETURNING`
clauses.

## What GORM gains an app

**Less CRUD code: about 12–16%.**

| | A: CRUD | B: settle.go |
|---|---|---|
| Product lines, code only | 1,726 → 1,514 (−12%; −16% excluding the model file) | 804 → 700 (−13%) |
| Product lines, raw | 2,247 → 2,079 (−7%) | 1,029 → 932 (−9%) |

Per file, the store grew (359 → 398 code lines) because it became the
plumbing between GORM and raw transactions; the rest shrank:
`partners.go` 343 → 228, `members.go` 118 → 71, `referrals.go`
217 → 165, `lines.go` 208 → 164, `customers.go` 187 → 182.

**Plain CRUD reads better.** `RevokeMember` went from 34 lines to 16
inside `Transaction(func(tx *gorm.DB) error {...})`. Settlement was
neutral: a 12-placeholder INSERT became a struct literal plus a guarded
batch `Create`.

**Model-driven migrations.** `rastrillo migration generate` diffs the
models against the migrations and writes the SQL; with plain SQL there
are no models to diff, and every migration is written by hand.

**`scope`.** `scope.Owned`/`OwnedBy` is the one seam an owned query goes
through. A plain-SQL app writes the `WHERE user_id = ?` itself, or its
own guard.

**The same SQL, when it matters.** Across 14 operations captured at the
driver, the statements per operation were identical, and `EXPLAIN QUERY
PLAN` was identical on every statement. No N+1 queries were introduced.
With `SkipDefaultTransaction: true` there were no hidden transactions.
With GORM's default, every standalone write became `BEGIN`/write/
`COMMIT`: one statement became three. `db.Open` keeps GORM's default,
so under Rastrillo's GORM that is what a standalone write costs today.

What changed in the SQL: `SELECT *` replaced explicit column lists;
`First` adds `ORDER BY id LIMIT 1` (same plan); inserts gain `RETURNING
id`; map updates list columns alphabetically.

## What GORM costs an app

**Reads cost about twice as much.** Micro-benchmarks, three runs each;
allocation counts are deterministic, times indicative.

| Operation | SQL | GORM |
|---|---|---|
| Agreement by id | 28–51 µs, 78 allocs | 45–79 µs, 134 allocs |
| All lines (200 rows) | 0.73–1.06 ms, 4,060 allocs | 1.3–3.1 ms, 7,092 allocs |
| Create partner | 119–141 µs, 12 allocs | 57–113 µs, 59 allocs |
| Compute (200 lines) | 7–14 ms, 14.1k allocs, 0.86 MB | 10–21 ms, 26.6k allocs, 1.62 MB |

Reads took roughly twice the time and 1.7–1.9× the allocations. Create
was faster under GORM, probably from how the two versions open a write
rather than GORM's own cost; not investigated.

**A bigger binary.** A static (`CGO_ENABLED=0`) binary importing the
package grew from 11.66 MB to 16.47 MB (+41%, about +4.8 MB); stripped,
7.85 → 11.24 MB. The first slice pays all of it: GORM,
`golang.org/x/text` and `encoding/gob`.

**Escape hatches for what GORM does not say well.** Slice A needed 7
(raw SQL, `OnConflict`, `Returning`, `gorm.Expr`, aggregates) and 3 raw
`*sql.Tx` hand-offs to unconverted code; slice B needed 5 and 1.
Claim-and-lock queries read worse: `UPDATE … WHERE id=(SELECT … LIMIT 1)
AND state='queued' RETURNING …` became a subquery builder with
`Model(&slice)` and `clause.Returning{}`, and a locking first write
became `Update("updated_at", gorm.Expr("updated_at"))`.

**Silent behaviour changes — the finding that decided it for Tito.**
Five of these changed behaviour silently or hung rather than failing
loudly, and the existing tests caught only the first two:

1. **Rebinding a session moved the main handle.** Binding a GORM session
   to a transaction by assigning its connection pointed the store's
   main handle at an already-committed transaction, because `Session`
   shares its parent's statement. 20+ tests failed. Fix: pass a
   `Context` so `Session` copies.
2. **Half-migrated code deadlocked.** Unconverted files built
   transaction views from the raw reader only; their GORM handle stayed
   on the single-connection writer pool the open transaction already
   held, and the suite hung for 10 minutes. Fix: route every view
   through one function. (SKILL.md §3's "scope the callback's `tx`,
   never `d.G`" is the same trap.)
3. **`Updates(struct)` skips zero and nil fields**, and wrote a stored
   document's key over the real one. Fix: an explicit `Select` column
   list — which SKILL.md §4 already requires for mass assignment.
4. **Automatic timestamps.** GORM stamps `CreatedAt`/`UpdatedAt` int64
   fields with the wall clock, which fired an audit trigger with an
   invented time. Fix: `autoCreateTime:false` / `autoUpdateTime:false`.
5. **`Model(&T{ID: 0})` refuses the update** where the original was a
   harmless 0-row `UPDATE`; with `AllowGlobalUpdate` set it would
   rewrite every row. Fix: an explicit `Where("id = ?")`.

And six louder ones:

6. **A column name guessed wrong:** `SignedPDFSHA256` became
   `signed_pdfsha256`. Fix: a column tag, plus a fence checking every
   model field against the real table.
7. **Unique-violation errors lose detail.** `TranslateError` (which
   `db.Open` turns on) returns a sentinel that no longer names the
   constraint.
8. **Empty batch inserts fail.** `Create` on an empty slice errors.
9. **A transaction closure cannot roll back quietly**; it takes a
   sentinel error.
10. **A name collided:** a package's `schema` constant clashes with
    `gorm.io/gorm/schema`.
11. **`[]` instead of `null`.** `Find` returns an empty slice where raw
    code returned nil, which serialises to JSON as `[]` rather than
    `null`.

Money itself was fine: integer cents map directly, and `sum()` reads
into `sql.NullInt64`.

## What plain SQL gains and costs an app

It gains explicit transactions and explicit plans: the SQL in the file
is the SQL that runs, and a claim-and-lock or upsert is written the way
SQLite spells it. It is smaller (no ORM in the binary) and faster to
read. It costs the scan boilerplate GORM removes — the 12–16% above —
and it has no model diff: every migration is hand-written, and owner
scoping is the app's own discipline rather than `scope`'s. `rastrillo
migration generate` and `migration check` both load the app's GORM
`Models`, so a plain-SQL app leaves them out: the scaffold runs
`migration-check` twice, in `make ci` and as its own runner step
`.amadan/ci.d/40-migration-check`, and both go; `migrate`'s
own checksum refusal (an applied migration whose SQL changed) still
guards the ledger at boot.

Tito's plan for the boilerplate is a generic query-and-scan helper
(its 2026-09-23 duplication review sized it at about 2k lines saved)
and a query guard that makes forgetting tenant scoping hard. Neither is
in Rastrillo yet; both would be candidates to give back.

**Test wall time** could not be compared: the box was at load 10–26, and
one variant ranged from 116 s to 196 s.

## Choosing, and switching later

- A CRUD-heavy app with a small team, many simple owned resources and
  no money: GORM. The line savings are real there, `scope` and the
  model diff carry weight, and the hazards above are avoidable with
  SKILL.md's rules (`Select` lists, scope the callback's `tx`).
- Money, fulfilment, or anything whose correctness is a transaction's
  exact shape; hot read paths; a binary size that matters; a team that
  reads SQL more easily than GORM's builder: plain SQL.
- Mixed is possible but is where hazard 2 lives: if one package's code
  uses both, route every transaction view through one function.

Switching later is per package, not per app, because the schema is the
same either way. From GORM to SQL: keep `migrate` (its ledger does not
care who wrote the SQL), stop generating, replace `scope.Owned` with an
explicit `WHERE`, and drop `db`/`scope` imports when the last caller
goes; Go migrations already written with `gormfn.Fn` keep running. From
SQL to GORM: open with `db.Open`, add models that match the existing
tables with explicit column tags (and a fence checking them, hazard 6),
set `autoCreateTime:false`/`autoUpdateTime:false` where a trigger or an
audit depends on the time, and run `rastrillo migration check` until it
reports models and migrations agree before generating anything.
