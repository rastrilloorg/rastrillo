# Known-Libraries Middle Layer

**Date:** 2026-08-21
**Status:** Approved in discussion; this document is the written record.
**Inputs:** James's framework review ("Rastrillo, Reviewed", v0.6.1),
the CARLOS authoring bake-off ("The CARLOS Bake-off", carril@493d83a,
source at github.com/carlosframework/carlos-bakeoff), and a build
probe run during this design session (see §5).

## 1. Problem

Rastrillo's app-shape layer — manifest codegen over an anemic core —
has the wrong cost curve for its real goal. Inside the manifest
vocabulary the leverage is real (~14 lines of TOML → ~2,100 lines of
working CRUD), but the vocabulary cannot express relations, booleans,
dates, or *who is asking*, and one step outside it an app falls back
to bare net/http against a core that supplies nothing in the middle
(the Gleester port: 8,925 lines of Go for a 1,524-line Rails app).
Meanwhile ~65–75% of the generated lines are the same seven helpers
stamped privately into every action package.

The bake-off measured the alternatives on one identical multi-tenant
spec built by the same model: the bespoke-framework arm (rastrillo)
was the most expensive Go arm and its CRUD generator was unusable for
row-level auth; the winning arm (carrillo, 48.5/50, cheapest, only
perfect security score) was **known libraries the model already has
priors for, on a thin, well-documented CARLOS-citizen chassis**.

## 2. Goals and non-goals

Goals, in order:

1. A frontier model can one-shot a production-quality multi-user
   CARLOS app cheaply, and a human can read the result and believe it.
2. Humans can read a Rastrillo app and understand what's going on.
3. Humans who don't use LLMs can build Rastrillo apps directly.

Non-goals for this phase: new manifest vocabulary; a bespoke
controller/resource framework (the bake-off dissolved it — the winner
had models, route groups, handler files, and a skill doc); replacing
the platform-citizenship layer or the golden-vectored satellite
libraries (crypto, webauthn, blobs, eventlog, mail, ui), which are
kept as-is.

## 3. Decisions of record

- **Evolve Rastrillo in place.** No adoption of the carril/carrillo
  repos; their evidence and conventions are folded in instead.
- **Known libraries over bespoke abstractions** wherever the library
  is in the model's (and Go developers') priors: GORM for data,
  chi for app routing, dbresolver for the writer/reader pool split.
- **Bespoke only where it buys a real property**: SQLite-backed
  sessions (revocation — cookie stores can't revoke), the platform
  layer (already proven), and a ~1,000-line owned GORM dialector
  (see §5, driver clash).
- **Manifests are demoted** to a maintenance-only admin-panel add-on.
- **Sessions core + pluggable identity**: the framework owns session
  lifecycle/CSRF/flash/Require; identity acquisition is a plugin seam
  with keymail (existing `auth/`) and password as the two shipped
  implementations.
- **The skill doc is a deliverable**, reviewed like code, budgeted
  small (~12–15 KB; the chassis's SKILL.md is 12.8 KB vs the manifest
  system's ~144 KB context tax).

## 4. Layering

```
Platform citizenship (unchanged):  run.go, serve.go, OpenDB, assets, sidecar
Primitives (this phase):           db (GORM dialector + pools)
                                   sessions/  csrf/  flash/
                                   form/ (thin)  view/ (thin)  scope/ (thin)
                                   auth/ = keymail identity plugin on sessions/
                                   password/ = second identity plugin
App story:                         GORM models + chi route groups + handler
                                   files + SKILL.md conventions
Add-on (retargeted, frozen):       manifest codegen emitting thin compositions
Libraries (unchanged):             crypto/ webauthn/ blobs/ eventlog/ mail/ ui/
```

Every primitives package is importable alone, tested alone, and
validates loudly. The manifest emitter and hand-written apps are both
consumers of the primitives; nothing in the primitives knows about
either.

### Ctx shrinks

`Ctx` keeps `DB`, `Logger`, `Assets`, `Render` (real, populated
fields). `Scope any` and the always-empty `Locale` are **deleted**
along with their doc references to the unimplemented `_middleware.go`.
Per-request state moves to typed accessors where it is resolved:
`sessions.User(r)` for the current user, `rastrillo.LocaleFrom(r)`
(already exists) for locale.

## 5. Data layer: GORM on the existing stack

Verified by a build probe during this session (scratchpad, throwaway):

- `gorm.io/gorm` v1.31.2 + `github.com/glebarez/sqlite` v1.11.0
  builds with `CGO_ENABLED=0`.
- OpenDB's pragma order survives: the DSN carries
  `busy_timeout(5000)` before `journal_mode(WAL)` before
  `foreign_keys(1)`; runtime `PRAGMA journal_mode` returned `wal`.
- The dialector accepts a pre-opened `*sql.DB` via
  `sqlite.Dialector{Conn: db}` — **OpenDB remains the pool owner**.
- Owner-scoped queries behave: `Where("user_id = ?", viewer)` on a
  by-ID fetch of another user's row returns `ErrRecordNotFound`;
  association-scoped finds work.

**Driver clash (probe-confirmed):** `glebarez/sqlite` transitively
imports `glebarez/go-sqlite`, which registers driver name `"sqlite"` —
the same name `modernc.org/sqlite` registers — so a binary importing
both **panics at init** (`sql: Register called twice`). The chassis
dodged this by adopting glebarez's stack wholesale, pinning embedded
SQLite at a ~2023 vintage (go-sqlite v1.21.2 / modernc v1.23-era).
Rastrillo deliberately tracks current modernc (v1.55.0), and glebarez
looks semi-dormant.

**Decision: own the dialector.** Fork `glebarez/sqlite` (MIT; ~1,000
lines: sqlite.go 275, ddlmod.go 289, migrator.go 430, errors.go 7)
into a Rastrillo package (working name `db/gormlite`) that imports
`modernc.org/sqlite` directly for driver and error translation. Carry
over glebarez's own test files, run against modernc v1.55.0. Preserve
the fork's provenance and license text.

**Pool split:** OpenDB grows the writer-1/reader-N shape (writer pool
`SetMaxOpenConns(1)`; reader pool N) routed by
`gorm.io/plugin/dbresolver` — closing the review's "single-connection
silent deadlock" finding with a known library. The existing
single-pool OpenDB signature stays for compatibility; the GORM entry
point is new API.

**Migrations:** GORM `AutoMigrate` for v1. Its limits (no destructive
changes, no renames) are stated in SKILL.md. Existing schema-tolerance
machinery (F4 handback) is unchanged.

## 6. Routing: chi for apps, stdlib for the platform

`github.com/go-chi/chi/v5` becomes the app-routing story: route
groups with middleware are exactly what `sessions.Require` needs
(`r.Group(func(r chi.Router) { r.Use(sessions.Require); … })`), the
dependency is tiny and stable, and both winning bake-off arms used it.

The platform endpoints (`/healthz`, `/api/version`) remain on the
outer stdlib mux in serve.go, **outside** app middleware — unchanged.
The generated (manifest) router keeps emitting a stdlib
`*http.ServeMux`; serve.go mounts either without caring.

## 7. Sessions core and identity plugins

Split `auth/` at the natural joint: *maintaining* a session vs
*earning* one.

**`sessions`** (extracted from `auth/`): SQLite-backed store,
`__Host-` cookie handling, and:

```go
sessions.Middleware(db)         // resolves the cookie once per request
sessions.User(r) (UserID, bool)
sessions.Require(next)          // redirect to sign-in, capturing return-to
sessions.SignIn(w, r, userID)   // rotates session ID on privilege change
sessions.SignOut(w, r)          // deletes the server row: real revocation
```

**`csrf`** and **`flash`**: own packages beside it (both depend on
sessions, nothing else). CSRF becomes app-wide: token issued into
every form via a `ui` partial + template func, checked by middleware
on every mutating method.

**Identity plugins** answer "which user is this?" and their entire
contract with the core is calling `sessions.SignIn`. `auth/` (keymail)
becomes the first plugin, keeping single-use links via
`DELETE…RETURNING` and revocation, shedding its private session/CSRF
copies. `password/` (classic email+password on the existing `crypto`
package) is the second. Each plugin owns its handlers and templates;
an app mounts one or both in main.go with one call each.

Sessions, CSRF, and Require are defaults an app opts *out* of, not
machinery it assembles.

## 8. Thin form/view helpers (no DSL)

There is **no** form-binding DSL. What ships:

- `form`: the money helpers (`ParseCents`, `FormatCents`,
  `FormatCentsPlain` with their hard-won comments, moved to the
  library once), an `Errors map[string]string` type, and the
  documented 422 re-render convention (validation fails → re-render
  form with raw input seeded back, status 422).
- `view`: `Render` (nil-Render logged-500 behavior), `Fail` (logged
  plain 500), `ParseID` (404-not-400 rule). These exist so neither
  generated nor hand-written code re-declares them again.

**Mass-assignment rule (SKILL.md, with teeth):** never bind a request
onto a GORM model. Updates go through explicitly `Select`-ed fields or
dedicated form structs. (carril-thin's best-in-field security came
from having no binding path at all; carrillo's perfect score required
hand discipline — the rule is the paved road for that discipline.)

## 9. Scoping conventions

- Every model owned by a user (or team) is queried through its
  association or an explicit `Where` on the owner column — never a
  bare `First(&x, id)`.
- `scope.Owned(g *gorm.DB, owner UserID) *gorm.DB` makes the right
  thing the short thing; **404-not-403** is the documented contract
  (matching ParseID's rule: a URL that was never yours doesn't exist).
- Scoping rules apply **inside transactions** identically — stated
  explicitly (a bake-off doc-gap finding).
- Join tables get one unambiguous stance in SKILL.md (the other
  bake-off doc-gap finding): the stricter reading wins — rows in a
  join table are scoped through *both* sides' owners.

No middleware magic. Convention plus helper, checked by example tests.

## 10. SKILL.md

A repo-root skill doc, ~12–15 KB budget, reviewed like code. It
carries: the app shape (models/routes/handlers layout), the scoping
conventions (§9) including transactions and join tables, the
mass-assignment rule (§8), AutoMigrate limits (§5), the sessions/CSRF
defaults and identity-plugin mounting (§7), and the platform contract
an app inherits for free. It is what an LLM loads instead of framework
source, and what a human reads first.

## 11. Manifests and README

- Emitter retargeted to compose `form`/`view` helpers instead of
  stamping private copies (~65–75% fewer generated lines). No
  vocabulary changes; the subsystem is maintenance-only.
- README reframes manifests as the admin-panel generator, and fixes
  the two review nits: the stale "generates no delete action"
  paragraph, and `Ctx.Scope`'s reference to `_middleware.go` (the
  field is deleted; the reference dies with it).

## 12. Examples and acceptance

- `examples/tickets` stays as the manifest add-on's example
  (regenerated with the thin emitter).
- A **new front-door example** (working name `examples/notes`):
  accounts, sessions, CSRF, flash, and one owner-scoped resource in
  the new shape. Test style: cookie jar + scraped CSRF tokens driving
  the real HTTP surface; a two-user isolation suite (Bob loading or
  force-updating Alice's note → 404) as the permanent regression
  guard; validation re-render at 422 preserving input; login
  return-to.

**Acceptance for the implementation PR:**

1. All existing tests green.
2. New example's suite green, including the isolation tests.
3. `CGO_ENABLED=0 go build` across the tree (static binary preserved).
4. `db/gormlite` passes its carried-over dialector tests against
   modernc v1.55.0.
5. `go vet` clean.
6. SKILL.md exists, within budget, covering §8–§10's rules.

## 13. Out of scope / deferred

- Stage-2 resource abstractions (dissolved; revisit only if the
  conventions prove insufficient on a real port).
- New manifest vocabulary (frozen).
- Re-porting Gleester (the natural next acceptance test after this
  lands, mirroring the bake-off's "second and third app" next move).
- Password-plugin extras (reset flows, lockout policy) beyond the
  basic email+password + session-rotation shape.

## 14. Sequencing note

This phase is sized for an overnight autonomous session executing a
written implementation plan, landing as a PR (never a direct merge to
main, per workflow). The implementation plan is the next artifact
after this spec is approved.
