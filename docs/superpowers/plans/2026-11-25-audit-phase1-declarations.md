# Audit phase 1: table declarations Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every table in a Rastrillo app's composed schema is declared (what it holds, whose it is, whether it is audited, how it is erased), a GORM-independent check enforces that in every app's tests, and core, the examples and the scaffold all pass it.

**Architecture:** Declarations ride on `migrate.Set` beside the migrations that create the tables, so `Merge` composes them in the same order. A checker applies the composed set to an in-memory database with `migrate.Apply` (Go migrations included) and compares the resulting tables with the declarations. `privacy.Identity` and `auth.(*Auth).Identify` resolve a subject to every identifier that stands for that person. Boot reports problems but does not refuse yet. Strict mode arrives in phase 2.

**Tech Stack:** Go 1.26, modernc.org/sqlite, the existing `migrate` package and its `migrate/dump` program, the `rastrillo` CLI.

**Spec:** `docs/superpowers/specs/2026-11-25-audit-and-personal-data-design.md` (§1 is this phase; §6 explains why boot only reports).

## Global Constraints

- Names differ from the spec's sketch. `migrate.Table` and `migrate.Column` already exist as schema-snapshot types (`migrate/schema.go`), so the declaration is `migrate.Data`, a personal column is `migrate.Field`, and the method is `(*Set).Declare`.
- Nothing in this phase may make an existing app fail to boot. Problems found at boot are returned on `migrate.Result`, not as an error.
- `migrate` must not import GORM (it is in the Makefile's `GORM_FREE` list). The new `privacy` and `audit` packages join `GORM_FREE`.
- Code style: 80 columns, comments explain why, Go naming conventions. `make gofmt code budget` must pass after every task. Raise a budget only with a reason line, as `.rastrillo/budgets.txt` does.
- Local tests are targeted (`go test ./pkg/ -run X`). The full suite runs on the fleet. Use `GOFLAGS=-mod=mod` where scratch modules need it.
- User-facing prose (docs pages, CHANGELOG, CLI output) gets Luna's copy review (`codex exec -m gpt-6-luna`) before it lands, and the strings are posted on the branch.

## Review Focus

1. **A table that only a Go migration creates or drops** (e.g. `secondfactor`'s `0002_adopt_passkey`). The checker must see the schema `Apply` produces, Go migrations included. A table that is declared but dropped again by a later migration is a problem; a table a Go migration creates is not "undeclared" by accident. Pinned in Task 2.
2. **Virtual-table shadow tables** (FTS5 creates `<name>_data`, `<name>_idx`, `<name>_content`, `<name>_docsize`, `<name>_config`). A declared virtual table covers its shadows; they must not be reported as undeclared. Pinned in Task 2.
3. **Tables without a primary key** (`signin_failures`) and composite keys (`eventlog`). `Key: []string{"rowid"}` is valid for a rowid table, and a composite key whose columns all exist passes. Pinned in Tasks 1 and 2.
4. **The same table declared twice** (two sets, or one set twice through `Merge`). It must be reported once, naming both namespaces. Pinned in Task 2.
5. **Framework-owned tables** (`rastrillo_migrations`, `sqlite_*`, `_litestream_*`). They must never be reported undeclared. Pinned in Task 2.

---

## File Structure

- `migrate/declare.go` (new): `Data`, `Field`, `Owner`, the enums and `(*Set).Declare`/`Declarations`.
- `migrate/declcheck.go` (new): `CheckDeclarations` and `Problem`.
- `migrate/set.go`: `Set` gains `declarations`; `Merge` and `All`'s siblings carry them.
- `migrate/apply.go`: `Result` gains `Problems []Problem`.
- `audit/check.go` (new, package `audit`): `CheckSchema(t, set)` test helper and `Exemptions(set)`.
- `privacy/identity.go` (new, package `privacy`): `Identity`, `Identifier`, `ErrUnidentifiable`.
- `auth/identify.go` (new): `(*Auth).Identify`.
- One `declare.go` per core package with a `Schema` (14 packages).
- `internal/generate`: generated stores emit declarations.
- `cmd/rastrillo`: the scaffold's `audit_test.go` template, its declarations, and `rastrillo audit exemptions`.
- `migrate/dump`: emits declarations for the CLI.
- Docs: `docs/site/audit.md` (new), `skill/data.md`, `CHANGELOG.md`.

---

### Task 1: Declaration types on `migrate.Set`

**Files:**
- Create: `migrate/declare.go`, `migrate/declare_test.go`
- Modify: `migrate/set.go` (`Set`, `Merge`)

**Interfaces:**
- Produces:

```go
type Kind int
const ( Personal Kind = iota + 1; Shared; System )

type Holds int
const ( HoldsSubject Holds = iota + 1; HoldsAddress; HoldsUserID; HoldsFreeText )

type Treatment int // how erasure treats a value
const ( EraseRow Treatment = iota + 1; EraseNull; EraseTombstone; EraseKeep )

type Owner struct {
    Column string // column on this table, or the FK column for Via
    Holds  Holds
    Parent string // non-empty for Via: the parent table owning the row
}
func Subject(column string) Owner
func Address(column string) Owner
func UserID(column string) Owner
func Via(column, parent string) Owner

type Field struct { Name string; Holds Holds; Erase Treatment }

type Data struct {
    Name      string
    Key       []string // "rowid" alone for a table without a primary key
    Kind      Kind
    Owner     Owner    // zero when the table has no owner
    Fields    []Field
    Recorded  []string
    NotAudited string  // reason; "" means audited
    Erase     Treatment
    KeepReason string  // required when Erase == EraseKeep
    Export    bool
    Namespace string   // set by Declare from the Set; not by callers
}

func (s *Set) Declare(d Data) *Set
func (s *Set) Declarations() []Data // composed order, Namespace filled
```

- [ ] **Step 1: Write the failing tests** in `migrate/declare_test.go`:
  - `TestDeclareCarriesNamespace`: `MustFromFS(fs, "tickets").Declare(Data{Name: "tickets"})` returns one declaration whose `Namespace` is `"tickets"`.
  - `TestMergeKeepsDeclarationOrder`: `Merge(a, nil, b).Declarations()` lists a's then b's, each with its own namespace.
  - `TestOwnerConstructors`: `Via("ticket_id", "tickets")` gives `Owner{Column: "ticket_id", Parent: "tickets"}` with `Holds` zero; `Subject("owner").Holds == HoldsSubject`.
- [ ] **Step 2: Run** `go test ./migrate/ -run 'Declare|MergeKeepsDeclaration|OwnerConstructors'`. Expected: FAIL (undefined).
- [ ] **Step 3: Implement** in `migrate/declare.go` and add `declarations []Data` to `Set`. `Merge` appends each input's declarations with that input's namespace (already filled by `Declare`), mirroring how it qualifies migration IDs.
- [ ] **Step 4: Run** the same tests. Expected: PASS. Also `go test ./migrate/` passes unchanged.
- [ ] **Step 5: Commit** `migrate: declare what each table holds, beside its migrations`.

### Task 2: `CheckDeclarations` and boot reporting

**Files:**
- Create: `migrate/declcheck.go`, `migrate/declcheck_test.go`
- Modify: `migrate/apply.go` (`Result.Problems`, call the check after applying)

**Interfaces:**
- Consumes: Task 1's types; `Apply`, `Pool`, `Memory`, `Read`.
- Produces:

```go
type Problem struct {
    Table   string
    Message string // one sentence, names what to fix
}
func CheckDeclarations(ctx context.Context, s *Set) ([]Problem, error)
func checkAgainst(snap Snapshot, virtual map[string]bool, ds []Data) []Problem // shared with Apply
```

`CheckDeclarations` opens `Memory()`, runs `Apply(ctx, Pool(d), s)`, reads the snapshot and the virtual tables (`sqlite_master` rows whose `sql` starts `CREATE VIRTUAL TABLE`), and calls `checkAgainst`. `Apply` calls `checkAgainst` on the real database after migrating and puts the result on `Result.Problems`. It never returns them as an error.

Rules, each one problem with the message in quotes:
- a table in the snapshot with no declaration: `"no declaration; add one with Declare next to the migration that creates it"`;
- a declaration naming no table: `"declared but no migration creates it"`;
- a table declared more than once: `"declared by <ns1> and <ns2>"`;
- a key column that does not exist (`rowid` always exists on a table without `WITHOUT ROWID`): `"key column <c> does not exist"`;
- an owner column or field that does not exist: `"column <c> does not exist"`;
- `Via` naming a parent that is not declared: `"owner parent <p> is not declared"`;
- `Kind == Personal` with `Erase == 0`: `"personal table needs an Erase"`;
- `Erase == EraseRow` (or any `Field` treatment) with a zero `Owner` on a `Personal` table: `"erasing rows needs an Owner"`;
- `EraseKeep` with an empty `KeepReason`: `"Keep needs a reason"`;
- a `Recorded` column that is also a `Field`: `"recorded column <c> holds personal data"`.

Never reported: `rastrillo_migrations`, names starting `sqlite_` or `_litestream_`, and shadow tables of a declared virtual table (name starts with `<virtual>_` and is one of FTS5's `_data`, `_idx`, `_content`, `_docsize`, `_config`).

- [ ] **Step 1: Write the failing tests** in `migrate/declcheck_test.go`, one per rule above plus:
  - `TestGoMigrationTablesAreSeen`: a set whose `0002` Go migration (`Fn`) creates `made_in_go`. Undeclared, it is reported; declared, it passes.
  - `TestDroppedTableIsNotThere`: a table created in `0001` and dropped in `0002`, still declared, is reported `"declared but no migration creates it"`.
  - `TestShadowTablesAreCovered`: `CREATE VIRTUAL TABLE notes_fts USING fts5(body)` declared as `notes_fts` gives no problems.
  - `TestRowidKeyAndCompositeKey`: `Key: []string{"rowid"}` on a table without a primary key passes; `Key: []string{"stream", "writer", "seq"}` passes on a table with those columns; `Key: []string{"stream", "nope"}` reports `nope`.
  - `TestFrameworkTablesIgnored`: an empty set with no declarations gives no problems (the ledger exists after `Apply`).
  - `TestApplyReportsWithoutFailing`: `Apply` on a set with an undeclared table returns `err == nil` and one entry in `Result.Problems`.
- [ ] **Step 2: Run** `go test ./migrate/ -run 'Declaration|Declared|GoMigrationTables|DroppedTable|ShadowTables|RowidKey|FrameworkTables|ApplyReports'`. Expected: FAIL.
- [ ] **Step 3: Implement** `CheckDeclarations`, `checkAgainst` and the `Apply` change. Sort problems by table, then message, so output is stable.
- [ ] **Step 4: Run** the same tests, then `go test ./migrate/...`. Expected: PASS.
- [ ] **Step 5: Commit** `migrate: check declarations against the applied schema`.

### Task 3: `audit.CheckSchema` and the exemptions report

**Files:**
- Create: `audit/check.go`, `audit/check_test.go`, `audit/doc.go`
- Modify: `Makefile` (`GORM_FREE` += `./audit ./privacy`)

**Interfaces:**
- Consumes: `migrate.CheckDeclarations`, `(*Set).Declarations`.
- Produces:

```go
func CheckSchema(t testing.TB, s *migrate.Set) // t.Errorf per problem, prefixed with the table
type Exemption struct{ Table, Namespace, What, Reason string } // What: "not audited" | "kept on erasure"
func Exemptions(s *migrate.Set) []Exemption // sorted by namespace, table, what
```

`doc.go` states the package's purpose (the audit log, of which this phase ships only the schema check) and points to the spec.

- [ ] **Step 1: Write the failing tests:** `TestCheckSchemaReportsEachProblem` (use a `testing.TB` recorder, as other packages' tests do, to capture `Errorf` calls: two problems give two calls); `TestExemptionsListsReasons` (one `NotAudited` and one `EraseKeep` give two exemptions with their reasons, in order).
- [ ] **Step 2: Run** `go test ./audit/`. Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** `go test ./audit/` and `make gorm-free`. Expected: PASS.
- [ ] **Step 5: Commit** `audit: a schema check every app's tests run`.

### Task 4: Core declares its tables

**Files:**
- Create: `<pkg>/declare.go` for each of `apitoken auth blobs eventlog handoff oauth passkey pow rootcall roster secondfactor sessions signin totp`, each chaining `Declare` onto its existing `Schema` (e.g. `var Schema = migrate.MustFromFS(...).Declare(...)`, moved or wrapped so the chain stays in one place).
- Create: `audit/core_test.go`

**Interfaces:**
- Consumes: Tasks 1–3.

The declarations are this table, exactly. Reasons are the strings to use. "Audited" means `NotAudited == ""`.

| Table | Key | Kind | Owner | Erase | Audit |
|---|---|---|---|---|---|
| api_tokens | id | Personal | Subject(subject) | EraseRow | audited; Recorded `access` |
| auth_links | hash | Personal | Address(address) | EraseRow | "one-time sign-in links; issuing and using one are security events" |
| auth_sessions | token_hash | Personal | Address(address) | EraseRow | audited (spec §7 may change this) |
| blobs | hash | Shared | | | audited |
| eventlog | stream, writer, seq | Personal | | EraseKeep "append-only and copied to other machines; erasure is the follow-up spec's" | "eventlog records actor, kind and time for every append itself" |
| eventlog_writer | id | System | | | "one row naming this machine's writer" |
| handoff_used | jti | System | | | "replay guard of spent one-time IDs" |
| oauth_clients | id | Shared | | | audited |
| oauth_codes | hash | Personal | Via(connection, oauth_connections) | EraseRow | "single-use codes that live for minutes; the connection is audited" |
| oauth_connections | id | Personal | Subject(subject) | EraseRow | audited; Recorded `access` |
| oauth_pending | id | Personal | Subject(subject) | EraseRow | "consent-screen state that lives for minutes; the grant is audited on oauth_connections" |
| oauth_refresh | hash | Personal | Via(connection, oauth_connections) | EraseRow | "rotated on every refresh; the connection is audited" |
| passkey_challenges | challenge | Personal | Subject(subject) | EraseRow | "ceremony state that lives for minutes" |
| passkey_credentials | id | Personal | Subject(subject) | EraseRow | audited |
| passkey_pending | token_hash | Personal | Subject(subject) | EraseRow | "ceremony state that lives for minutes" |
| passkey_recovery_codes | code_hash | Personal | Subject(subject) | EraseRow | audited |
| pow_spent_nonces | nonce | System | | | "replay guard of spent one-time IDs" |
| rootcall_used | jti | System | | | "replay guard of spent one-time IDs" |
| roster_state | id | Personal | | EraseKeep "the root's member sync rewrites it; erasure through the root is the follow-up spec's" | audited |
| secondfactor_pending | token_hash | Personal | Subject(subject) | EraseRow | "ceremony state that lives for minutes" |
| secondfactor_recovery_codes | code_hash | Personal | Subject(subject) | EraseRow | audited |
| sessions | token_hash | Personal | Subject(subject) | EraseRow | audited (spec §7 may change this) |
| signin_failures | rowid | Personal | | EraseKeep "rate-limit windows of minutes, pruned as they expire" | "rate-limit buckets; failed sign-ins are security events" |
| signin_offers | subject | Personal | Subject(subject) | EraseRow | audited |
| signin_passphrases | subject | Personal | Subject(subject) | EraseRow | audited |
| totp_secrets | subject | Personal | Subject(subject) | EraseRow | audited |

Every `Personal` table with `EraseRow` sets `Export: true`, except tables marked "lives for minutes". Record any table that `0002`-style Go migrations create or drop but that this table misses as a problem in the commit message, and declare it the same way.

- [ ] **Step 1: Write the failing test** `TestCoreSchemasAreDeclared` in `audit/core_test.go`: `CheckSchema(t, migrate.Merge(sessions.Schema, auth.Schema, apitoken.Schema, blobs.Schema, eventlog.Schema, handoff.Schema, oauth.Schema, passkey.Schema, pow.Schema, rootcall.Schema, roster.Schema, secondfactor.Schema, signin.Schema, totp.Schema))`. Add `TestCoreExemptionsHaveReasons`: every `Exemptions` entry has a non-empty `Reason`.
- [ ] **Step 2: Run** `go test ./audit/ -run Core`. Expected: FAIL with one problem per undeclared table (26).
- [ ] **Step 3: Implement** the 14 `declare.go` files from the table.
- [ ] **Step 4: Run** `go test ./audit/ -run Core` and each touched package's own tests. Expected: PASS. Check that the `make gorm-free` packages are still GORM-free.
- [ ] **Step 5: Commit** `core: declare every table's data, owner and erasure`.

### Task 5: `privacy.Identity` and `auth.Identify`

**Files:**
- Create: `privacy/identity.go`, `privacy/identity_test.go`, `auth/identify.go`, `auth/identify_test.go`
- Modify: `auth/auth.go` (`Config.AddressesFor`)

**Interfaces:**
- Produces:

```go
package privacy
type Identity struct {
    Subject   string
    Addresses []string // lower-cased, trimmed, sorted, deduplicated
    UserID    int64    // 0 unless Subject is a positive decimal integer
}
type Identifier interface {
    Identify(ctx context.Context, subject string) (Identity, error)
}
var ErrUnidentifiable = errors.New("privacy: this app cannot list a person's addresses")
func (i Identity) Matches(h migrate.Holds, value string) bool // HoldsSubject: == Subject; HoldsAddress: in Addresses (normalised); HoldsUserID: == decimal UserID
```

```go
package auth
// AddressesFor lists the addresses a subject signs in with, for an app
// whose SubjectFor is not reversible. Nil with SubjectFor set makes
// Identify answer privacy.ErrUnidentifiable.
AddressesFor func(subject string) ([]string, error) // new Config field
func (auth *Auth) Identify(ctx context.Context, subject string) (privacy.Identity, error)
```

How `Identify` finds addresses depends on the auth mode:
- **Default** (no records, no `SubjectFor`, no blind links): the subject is the address. `Addresses = [subject]`.
- **Records mode, not blind:** `auth.User(ctx, subject)`, then its `Email`.
- **Blind links or blind records:** addresses are not stored in the clear. Return `Addresses` empty and no error. Note in the doc comment that blind modes keep only hashes, so matching works on subjects alone.
- **`SubjectFor` set:** call `AddressesFor` if set. If it isn't, return `ErrUnidentifiable`.

- [ ] **Step 1: Write the failing tests:**
  - `privacy`: `TestMatchesNormalisesAddresses`, where `" Ana@Example.org "` matches `ana@example.org`; `TestUserIDOnlyForDecimalSubjects`, where `"42"` gives 42 and both `"ana@x"` and `"-3"` give 0.
  - `auth`, one test per mode above, using the package's existing test constructors for each mode.
- [ ] **Step 2: Run** `go test ./privacy/ ./auth/ -run 'Identify|Matches|UserID'`. Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** the same tests. Expected: PASS.
- [ ] **Step 5: Commit** `privacy, auth: resolve a subject to every identifier for that person`.

### Task 6: Generated stores, examples and the scaffold declare their tables

**Files:**
- Modify: `internal/generate` store emitters, so each generated store's migration set declares its table. An owner-scoped store gets `Kind: Personal, Owner: Subject("owner"), Erase: EraseRow, Export: true`. One without an owner gets `Kind: Shared`. Refresh every affected golden file.
- Modify: `cmd/rastrillo/scaffold/` templates. Add `audit_test.go.tmpl`, which calls `audit.CheckSchema(t, app.BootSchema)`, and add declarations for any table the scaffold itself creates. This must cover every scaffold shape (single, `--auth=signin`, and the multi-instance shapes `internal/scaffoldcheck` builds).
- Modify: `examples/*`. Each example's composed boot schema passes `CheckSchema` in a test, and the hand-written tables (notes, blog) carry hand-written declarations.

**Interfaces:**
- Consumes: Tasks 1–4.

- [ ] **Step 1: Write the failing test.** Add an `audit_test.go` to each example and run `go test ./examples/...`. Expected: FAIL, listing the undeclared app tables.
- [ ] **Step 2: Implement** the generator change, refresh goldens with the package's existing update flag, and add the hand-written declarations.
- [ ] **Step 3: Run** `go test ./internal/generate/... ./examples/...` and `go test ./cmd/rastrillo/ -run Scaffold`. Expected: PASS. The scaffold byte caps are at zero headroom (memory: scaffold shapes). Fit the template within them, or raise a cap with its reason line.
- [ ] **Step 4: Run** `make generate-check`. Expected: PASS.
- [ ] **Step 5: Commit** `generate, scaffold, examples: declare app tables and check them in tests`.

### Task 7: `rastrillo audit exemptions`

**Files:**
- Modify: `migrate/dump`, so it also prints the composed declarations as JSON under a new flag.
- Modify: `cmd/rastrillo/main.go`, and add `cmd/rastrillo/audit.go` for the `audit exemptions` subcommand.
- Test: `cmd/rastrillo/audit_test.go`

**Interfaces:**
- Consumes: `audit.Exemptions`, and the dump program the `migration` subcommand already runs against an app.

Output is one line per exemption: `<namespace>  <table>  <not audited|kept on erasure>  <reason>`, in `Exemptions` order. If there are none, it prints `No exemptions: every table is audited and erased.` (strings go through copy review).

- [ ] **Step 1: Write the failing test** `TestAuditExemptionsOnTickets`. It runs the subcommand against `examples/tickets` the way the `migration` subcommand's tests run theirs, and checks that the output includes core's `eventlog` line with its reason.
- [ ] **Step 2: Run** `go test ./cmd/rastrillo/ -run AuditExemptions`. Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** it again. Expected: PASS. Also add the subcommand to `docs/site/cli.md`.
- [ ] **Step 5: Commit** `cli: rastrillo audit exemptions prints every exemption and its reason`.

### Task 8: Docs, skill and changelog

**Files:**
- Create: `docs/site/audit.md`. It covers declaring a table, what each field means, the check in tests, the exemptions report, and what phase 1 does not do yet (no log, no erasure).
- Modify: `skill/data.md`, adding a short "Declare every table" section that points to the page; `docs/site/addons.md`, saying addons declare their tables alongside their migrations; `CHANGELOG.md` under Unreleased.

- [ ] **Step 1: Draft** the prose. Run Luna's copy review, post the strings and Luna's reply as a branch discussion, and apply the edits.
- [ ] **Step 2: Run** the docs tests (`go test . -run 'Docsite|Markup|Changelog'`) and the skill budget test. Expected: PASS. The `skill/` byte budget may need its reason line raised.
- [ ] **Step 3: Commit** `docs: declaring tables for the audit log and personal data`.

---

## Self-review notes

- **Spec coverage:** this phase builds §1 (declarations, owners, fields, recorded columns, exemptions, the gate check), "Who is a person" (Task 5), and core declarations (Task 4). The boot check only reports here (spec §6: strict mode comes with phase 2). `recursive_triggers` moves to phase 2, where triggers arrive. Addon declarations are phase 5.
- **Deferred to the follow-up spec**, and noted in the docs (Task 8): auth's records-mode people store and plaza's accounts live in the object store, so declarations cannot cover them.
