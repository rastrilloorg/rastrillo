# Audit log, personal data, and plain SQL by default

Status: draft 2, for Paul's approval, 2026-11-25. Covers amadan
rastrillo/rastrillo #412 (audit log, per-person export and erasure) and
#415 (plain SQL with sqlc as the default data layer, with a build-time
owner-scoping check). One spec because all three rest on one
declaration: which tables hold a person's data, through which columns,
and what happens to them.

Draft 2 folds in a builder check (8 questions) and Astra's adversarial
review (22 findings, one blocker). Where a finding changed the design,
the section says so in a line.

## Why

Paul (2026-11-25): "we want any Rastrillo app to come out as easy to
audit as possible for the likes of ISO27001 without folks having to
realise too late and backport auditing."

Success is an app scaffolded today that, two years from now, faces its
first ISO 27001 audit or its first erasure request and needs no
backporting: the activity log already covers every write, tampering
with it is detectable, the app can show it is being watched, and a
person's data can be exported and erased with a record that it was.

The trigger was an outside review of Rastrillo (Tony's agents).
Payments (Abono), OpenTelemetry and webhooks (Tony's addons, #413,
#414) are out of scope.

## What an auditor asks for, mapped to this spec

ISO 27001:2022 Annex A, as this spec reads it (not legal advice):

| Control | Evidence an auditor wants | Where it comes from |
|---|---|---|
| A.8.15 Logging | Who did what, when, from where; protected; retained; analysed | The audit log (§2–3), verification and health signals (§3.8) |
| A.8.16 Monitoring | Anomalies are noticed and acted on | Health signals and alerts (§3.8); streaming to the org's own tools is the OpenTelemetry addon's (#413) |
| A.8.17 Clock sync | Timestamps can be trusted | The platform's clock discipline, stated in the operator notes (§3.8) |
| A.5.15–5.18, A.8.2 Access | Who has access at which role, who changed it and to what | Recorded values for declared access columns (§1) and typed security events (§3.6) |
| A.8.5 Authentication | Sign-ins, failures, second-factor changes | Typed security events, committed even when the request fails (§3.6) |
| A.8.10, A.5.34 Deletion, privacy | A person's data can be exported and erased; erasure is recorded | §4 |

## Goals

1. **On by default.** `rastrillo new` produces an app whose every write
   to its own tables, and every security event in core, lands in an
   audit log, with no app code beyond what the scaffold writes.
2. **Nothing escapes silently.** A write that would not be attributed
   fails loudly. A table that is not audited says so, with a reason, in
   one report an auditor can read.
3. **Tamper-evident, honestly bounded.** Entries are hash-chained and
   identities are bound into the chain. History up to the last
   checkpoint held outside the database is verifiable; the tail after
   it is reported as unanchored, not claimed.
4. **Erasure-compatible.** Erasing a person never breaks the chain.
5. **Export and erasure for any person,** across core, addons and the
   app, self-service and admin-initiated, that in-flight work cannot
   undo.
6. **Plain SQL with sqlc is the default for new apps,** and a query on
   an owned table that does not filter by owner fails the build.

## Non-goals

- **Before-and-after values in general.** The log records who, which
  row, which action and which columns changed. The only values it
  records are those of columns declared `Recorded` (§1), such as a
  role, which carry no personal data.
- **Shipping logs to a SIEM** (the OpenTelemetry addon, #413).
- **Removing GORM.** It stays supported.
- **Legal judgement** about what an app must retain.
- **Data outside the app's own database** (Paul, 2026-11-25: one
  database first). This version covers each app database on its own.
  A follow-up spec covers:
  - erasure at a root reaching its instances (today the root's member
    sync would restore a locally erased member);
  - an audit log for hubs that keep accounts in the object store
    (plaza), where triggers cannot see writes;
  - eventlog: its events copy to other machines and carry the actor
    string, so local erasure would be undone by the next ingest.

  Until then, those tables are declared `Keep` with that reason, the
  exemptions report lists them, and the docs say erasure must be
  requested in each database and does not yet reach these stores.

## Anti-goals

- A write path that is not audited and nobody notices.
- Personal data in the log that erasure cannot reach.
- App authors having to remember to call anything for ordinary writes.
- A noticeable slowdown: measured and budgeted (§7).
- An existing app breaking on upgrade (§6).

## Approaches considered

| | Capture | Verdict |
|---|---|---|
| **A. SQLite triggers on every audited table**, with each writing transaction first recording who is acting and why | Complete: sqlc, GORM, raw SQL, jobs and migrations all go through SQLite | **Chosen (Paul, 2026-11-25).** Completeness is what an auditor tests. |
| B. Wrap the database handle and infer tables and rows from SQL text | Pure Go | Fragile parsing; writes that bypass the wrapper vanish silently. |
| C. Record at the operation layer only | Readable intent, cheap | Misses handlers and jobs that write directly, and nothing can check for that reliably. |

A carries C's information: each transaction names its operation, so an
entry reads as intent ("tickets.close by user:ana@…") and as rows.

## 1. The table declaration

Every table in an app's composed schema (`BootSchema`: core, addons,
the app) is declared alongside the migrations that create it. A
`migrate.Set` gains declarations, so they merge like migrations and an
addon's travel with its set.

```go
set.Declare(migrate.Table{
    Name: "tickets",
    Key:  []string{"id"},                 // composite keys allowed
    Kind: migrate.Personal,               // Personal | Shared | System
    Owner: migrate.Subject("owner"),      // or Address("address"),
                                          // UserID("user_id"),
                                          // Via("ticket_id", "tickets")
    Columns: []migrate.Column{            // other columns holding personal data
        {Name: "contact_email", Holds: migrate.Address, Erase: migrate.Tombstone},
        {Name: "notes", Holds: migrate.FreeText, Erase: migrate.Null},
    },
    Recorded: []string{"status"},          // values kept in the log (no personal data)
    Audit: migrate.Audited,                // or NotAudited("reason")
    Erase: migrate.Delete,                 // Delete | Anonymise | Keep("reason")
    Export: true,
})
```

- **Key** is the logical row identity, composite where the table's is
  (eventlog's is `(stream, writer, seq)`). (Astra 9.)
- **Owner** says which column ties a row to a person and what that
  column holds: a subject, an email address, a numeric user ID, or
  ownership through a parent row (`Via`, for child tables such as
  comments on a ticket). (Builder 2, 7.)
- **Columns** lists other personal columns, what they hold, and how
  erasure treats each: `Null`, `Tombstone` (a unique per-row marker, so
  unique and check constraints hold), or deletion of the row. An
  author column in a `Shared` table is declared here. (Astra 11, 12.)
- **Recorded** lists columns whose before and after values the log
  keeps, for access-right evidence: a role, a status, a grant's scope.
  The gate refuses a `Recorded` column that is also in `Columns`.
  (Astra 19.)
- **Kind**, **Audit** and **Erase** as before; `NotAudited` and `Keep`
  require a reason.
- A package whose personal data no declaration can describe (blobs in
  the object store, eventlog payloads, encrypted fields) registers an
  `Exporter`/`Eraser` instead, and the gate requires one or the other
  for each `Personal` table.

### Who is a person

A person is a subject. `privacy.Identify(subject)` returns every
identifier that stands for them in this app: the subject, the email
addresses `auth` maps to it (through `SubjectFor`), and the numeric ID
for GORM apps. Core provides the default from `auth`'s tables; an app
with its own identity scheme supplies its own. Export, erasure and the
log's per-person filter all go through it, and `Owner` and `Columns`
say which identifier each column holds. (Builder 2, Astra 11.)

### The gate

There is no GORM-independent schema check today: the scaffold's
`migrations_test.go` compares `Schema` with GORM `Models` and skips
`BootSchema`, and plain-SQL apps are told to delete it. So this adds
one. (Builder 1, Astra 16.)

`audit.CheckSchema(t, app.BootSchema)`, in a test the scaffold writes
(`audit_test.go`), builds the composed schema in memory and fails when:

- a table has no declaration, or a declaration names no table;
- a `Personal` table has no `Owner` and no `Eraser`, or no `Erase`;
- a declared column or key column does not exist;
- `recursive_triggers` is off on the writer (§3.2).

The same checks run at boot, where a failure refuses to start the app
in strict mode (§6). `rastrillo audit exemptions` prints every
`NotAudited`, `Keep` and owner-check exemption with its reason.

Core and the first-party addons (idear, aviso, plaza) declare their
tables in the release that ships phase 1.

## 2. Storage

The `audit` package's own migration set creates:

- **`audit_log`**: `seq INTEGER PRIMARY KEY AUTOINCREMENT` (never
  reused, Astra 21), `at` (UTC, ms), `actor_ref`, `subject_ref` (whose
  data the row belongs to: its owner at the time of the write, Builder
  4, Astra 8), `source_ref` (client IP), `operation`, `request`, `tbl`,
  `row_key` (canonical encoding of the declared key), `action`,
  `columns`, `recorded` (JSON of before and after values of `Recorded`
  columns), `detail` (typed fields for security events, §3.6), the
  staging columns below, `prev`, `hash`.
- **`audit_refs`**: `id`, `commitment` (SHA-256 of `salt ‖ value`),
  `salt`, `value`, `person` (the subject ref this value belongs to, if
  any), `erased`. Every actor string, subject, email, IP and personal
  row key is stored here once. The chain covers the commitment, so a
  value cannot be swapped without detection while it exists; erasure
  deletes `salt` and `value` and keeps the commitment, which cannot be
  reversed without the salt. (Astra 1, the blocker.)
- **`audit_who`**: one row, present only inside a writing transaction
  (§3.1).

**Staging.** Triggers cannot call Go, so they write raw values (actor
string, owner value, IP, personal row key) into staging columns on the
new entry. The sealer (§3.4) moves each into `audit_refs`, clears the
staging column, and only then hashes the entry. Raw values live only
in unsealed entries, for at most one sealing interval, and erasure runs
the sealer first.

## 3. Capture

### 3.1 Who is writing

A writing transaction begins by inserting the `audit_who` row (actor,
subject, source, operation, request) and ends by deleting it, inside
the same transaction. Readers never see it; Astra confirmed this holds
under the configured isolation.

The primitive is connection-aware, so it works on pinned connections
as well as pools (Astra 6):

- `audit.Begin(tx, who)` and `audit.End(tx)` take any executor already
  inside a transaction: a `*sql.Tx`, `migrate.Tx`, or eventlog's pinned
  connection.
- `audit.Write(ctx, pool, who, fn)` is the convenience for the common
  case: it opens the transaction, calls `Begin`, runs `fn`, calls `End`
  and commits.
- `migrate.Apply` and `eventlog` call `Begin`/`End` on their own pinned
  connections.

**`who` is explicit, never a silent default** (Astra 7):

- Every entry point stamps the actor on the request context: the
  generated routers (which already compute `Ctx.Actor` from the
  session but do not put it on the context), the API tree, MCP and
  the op doors. `audit.WhoFrom(ctx)` returns an error when a request
  context has no actor, so a forgotten stamp fails, rather than
  recording a signed-in write as `app`.
- An anonymous request is its own actor, `anonymous`, with its IP as
  source; it is never `app`.
- Jobs, background work and boot tasks build `audit.Who{Actor:
  rastrillo.Actor{}, Operation: "job:<name>"}` themselves; an empty
  operation is refused.
- The framework gives every request an ID (honouring the platform's
  request ID header when present) and stamps it on the context.

### 3.2 Triggers

Triggers are **derived objects, not migrations.** After applying
migrations, `migrate.Apply` drops and recreates every audit trigger
from the declarations in the same transaction. Nothing goes into
migration files, so checksums and adoption of older databases are
untouched (Astra 17), and a migration that adds a column cannot leave a
stale trigger (Builder 1).

For each audited table:

- `BEFORE INSERT/UPDATE/DELETE`: abort when `audit_who` is empty (in
  strict mode; §6).
- `AFTER INSERT/UPDATE/DELETE`: one `audit_log` entry with the row key,
  the owner value, changed column names and `Recorded` values. An
  update that changes nothing writes no entry.

The writer's DSN sets `recursive_triggers=ON`, and boot verifies it,
because SQLite fires delete triggers for `REPLACE` conflicts only with
it on, and core already uses `INSERT OR REPLACE` (auth/store.go:83).
(Astra 4.)

### 3.3 Protecting the log

Triggers on `audit_log` refuse `UPDATE` and `DELETE`, refuse an
`INSERT` that names a `seq` (which blocks `INSERT OR REPLACE`
overwriting an entry), and refuse any insert outside an `audit_who`
transaction. The sealer and retention identify themselves through
reserved operation names that `Begin` refuses from callers.

Anyone with the database file can drop triggers. The chain and the
checkpoints (§3.4) make that detectable; the triggers stop accidents
and ordinary app code.

### 3.4 Sealing, checkpoints and what is guaranteed

**One sealer** seals the whole pending prefix, in `seq` order, under
the write lock (`BEGIN IMMEDIATE`), so commit order and chain order
cannot diverge (Astra 2). It runs after each audited commit (signalled,
not awaited), on a `background` loop, and at boot. It moves staging
values into refs, then sets `prev` and `hash = SHA-256(prev ‖
canonical(entry fields, with ref commitments))`.

**Checkpoints** (Astra 3). Recomputing hashes only proves the chain is
consistent; an attacker who can write the file can recompute it.
Detection needs a copy of the chain head held where the app cannot
rewrite it. So:

- The app writes a checkpoint (`seq`, `hash`, time) hourly and at
  shutdown to a checkpoint sink outside the database.
- The first sink is the platform's logs, at info level, in a fixed
  format `rastrillo audit verify --checkpoints <file>` reads.
- An append-only checkpoint store is requested from the CARLOS
  platform (carloku), since the platform is where an app-independent
  copy belongs. The sink is an interface, so it plugs in when it
  exists.
- **The guarantee is stated as bounded:** history up to the latest
  verified checkpoint is tamper-evident; entries after it are reported
  as "unanchored" by verify, the page and the health signals. Boot
  sealing never re-seals an entry already covered by a checkpoint.

### 3.5 Retention

Default 13 months, configurable. Pruning runs in one transaction: it
writes a `pruned` entry carrying the hash and `seq` of the last pruned
entry, then deletes the entries before it. `AUTOINCREMENT` keeps `seq`
from restarting. Refs no longer referenced are deleted with them.

### 3.6 Security events

Some events change no audited row, or happen in a request that fails.
Core records them as typed events with `detail` fields per event
(personal values through refs):

- sign-in success and failure (way, outcome), sign-out;
- second factor added or removed;
- API token minted or revoked; OAuth grant given, refused or revoked;
- role changes (from, to, by whom) where idear's row capture is not
  enough;
- authorization refused (operation, reason);
- export requested; erasure requested, cancelled, carried out.

Failures go through **a separate committed path**, `audit.Fail(who,
event)`, in its own short transaction on a context detached from the
request, so a rolled-back or cancelled request still leaves its
failure on record. If recording fails, the event is written to the
app's error log with its fields and counted as a capture failure
(§3.8); the request is not blocked by it. (Astra 18.)

Whether `sessions` is audited row by row or only through these events
is decided by measurement (§7).

### 3.7 Reading the log

- A page, the API and MCP operations, all behind **one explicit
  audit-administrator policy** the app sets (default: idear's Owner and
  Admin roles; an app without idear has no audit reader until it sets
  one). Read access alone does not grant it. (Astra 20.)
- A separate, subject-restricted operation lets anyone see and export
  their own entries.
- Export as CSV and JSON for a date range, with the checkpoints that
  cover it, so an auditor can verify the extract.
- Erased refs show as `erased`, never their former value.

### 3.8 Health signals and operator notes

An auditor tests that the log is watched, not only that it exists
(Astra 22). The framework provides:

- a daily scheduled verify (chain and checkpoints), its result on the
  page and in the logs at error level when it fails;
- signals for: unsealed backlog age, unanchored tail length, capture
  failures (§3.6), unattributed writes in recording mode (§6),
  `recursive_triggers` off;
- all of them on the audit page and in a JSON health endpoint the
  platform or the OpenTelemetry addon can poll.

And a page of operator notes in the docs, written to drop into an
ISMS: who reviews the log and how often, what to do on a failed
verify, how long logs and backups are kept, and that timestamps rely
on the platform's clock discipline (A.8.17).

## 4. Export and erasure

### 4.1 Contract

Driven by the declarations plus `Identify`. For each `Personal` table,
rows whose owner (directly or `Via` a parent) is one of the person's
identifiers; for each declared personal column elsewhere, matching
values. `Exporter`/`Eraser` pairs cover what declarations cannot.

### 4.2 Export

A signed-in person downloads everything held about them as a zip of
JSON files, one per table, plus their own audit entries (as actor and
as subject). An audit administrator can export for a member. Every
export is a security event.

### 4.3 Erasure

1. **Request.** The person or an administrator requests erasure and
   confirms. A dry run of the erasure plan runs at once in a rolled-back
   transaction, so constraint problems surface now, not at the end of
   the grace period. (Astra 12.)
2. **Grace period** (default 14 days), during which they can cancel.
   Signing in shows the pending erasure.
3. **Carry out**, as persisted, idempotent steps (Astra 13, 14):
   1. Mark the person erased. From this moment `audit.Begin` refuses
      any `who` whose actor or subject is that person, so a request or
      job already holding their actor cannot write after erasure.
   2. Revoke every session, token and grant they hold.
   3. Run the sealer, so no raw values remain in staging.
   4. In one transaction, apply each declaration (`Delete`, `Null`,
      `Tombstone`, `Keep`) and erase their refs' values and salts.
   5. Run each `Eraser` for external stores (blobs and the like), with
      retries; each records completion.
   6. Record `erasure carried out`, with no personal value, only when
      every step has completed.
4. **Physical removal.** Apps run with `secure_delete=ON`, so deleted
   values are overwritten in the main file, not left in free pages
   (Astra 15; `skill/data.md` already names the setting). The cost is
   measured in §7. The docs state what remains out of reach: WAL frames
   until checkpoint, and Litestream backups until they age out under
   the platform's retention.

Refused: erasing the last Owner of an instance until ownership moves.

## 5. Plain SQL with sqlc as the default (#415)

- `rastrillo new` scaffolds plain SQL: migrations in SQL, queries in
  `queries.sql`, sqlc-generated Go, the `go tool sqlc` directive,
  `audit.Write` around writes, and the declarations. Within the
  scaffold's byte budgets.
- GORM stays supported. `skill/data.md` leads with sqlc.
- **Owner-scoping check**, in the gate, over the app's own sqlc
  queries (Builder 6): every query that reads, updates or deletes an
  `Owner`-declared table must constrain the owner column (or the `Via`
  parent's) to a parameter, or carry `-- rastrillo:unscoped <reason>`.
  Inserts must set the owner column. The check reads sqlc's parsed
  catalog or a SQLite parser, never regular expressions. Core and
  addon packages are out of its scope (their own tests cover them),
  and so is SQL written in Go strings, which the docs say plainly.
- `examples/notes` and the tour's chat app move to sqlc, or the plan
  says why they stay on GORM.
- A migration guide for an existing GORM app; porting stays optional.

## 6. Modes and existing apps

Two modes, stored in the app's config and visible to the gate, boot
and the audit page:

- **Strict** (new apps): a write outside `audit.Begin` aborts; boot
  refuses to start if the schema check fails.
- **Recording**: triggers record a missing `who` as `unattributed`
  instead of aborting, the sealer still seals, and the health signals
  count unattributed writes. Boot warns instead of refusing.

The audit tables' own migration set is additive and new, so adoption
of an older ledgerless database is not affected; the plan must prove
that against `migrate/adopt.go`'s rules. (Astra 17.)

## 7. Measurement

Before the plan fixes defaults, measure on `examples/tickets` and the
perf harness: write latency and throughput with and without triggers,
`recursive_triggers` and `secure_delete`; log growth per day under the
bench's load; sealing cost and backlog under load. The results set the
budget the gate holds and decide the `sessions` question (§3.6).

## 8. Order of work

Each phase is its own plan and branch, reviewed cross-provider:

1. Declarations, `Identify`, the schema check, core's declarations,
   the exemptions report.
2. The audit log: storage, `Begin`/`Write`, context stamping, triggers,
   sealing, checkpoints, retention, security events, `Fail`, the page,
   API, MCP, export, health signals, operator notes, modes.
3. Export and erasure.
4. sqlc as the default and the owner-scoping check (depends only on
   phase 1).
5. Addon follow-ups: idear, aviso and plaza declare their tables, with
   `Exporter`/`Eraser` pairs where needed.

## Decided

- Approach A, carrying C's information (Paul, 2026-11-25).
- On by default, per-table exemptions with reasons, no app-wide off
  switch (Paul, 2026-11-25).
- Every write to audited tables, no general values (Paul, 2026-11-25).
- Apps without sign-in covered; hash chain; 13-month retention; page
  plus API, MCP and export (proposed 2026-11-25, unopposed).
- One database at a time; instances, hubs and eventlog in a follow-up
  spec (Paul, 2026-11-25).

## Calls made in this draft (Paul to check)

- Declarations live on `migrate.Set`.
- Personal values go through salted commitments in `audit_refs`.
- Triggers are derived objects rebuilt at boot, not migrations.
- Checkpoints go to the platform's logs first; an append-only
  checkpoint store is requested from the platform.
- `Recorded` columns keep values for access-right evidence.
- Failure events commit separately and never block a request.
- Audit readers need an explicit administrator policy.
- `secure_delete=ON` for apps, subject to §7's measurement.
- Erasure grace period 14 days; last Owner cannot be erased.
- The owner-scoping check covers the app's sqlc queries only.

## Left to the builder

- Exact trigger SQL, the canonical encodings (row key, hash input).
- How the op doors pass the operation name into `who`.
- How the owner-scoping check reads queries.
- The checkpoint log line format.
- Page layout, within the design system.
