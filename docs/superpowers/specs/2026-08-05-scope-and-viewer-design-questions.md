# Scope and the viewer — design questions

**Date:** 2026-08-05
**Status:** questions only — nothing here is decided. This document is
deliberately not a spec: it records no decision, endorses no
mechanism, and no paragraph in it should be read as a recommendation.
It exists so the Scope/auth design cycle opens on written questions
instead of a blank page.
**Origin:** gleester's evaluation (James, 2026-08-04): "a wish list
isn't 'the rows in wishes', it's 'the rows this viewer may see', and
the app exists to enforce that. Generated CRUD would leak exactly what
the app hides."

`Options.Wrap`, shipped in this same branch, gives an app one seam for
sessions, CSRF and authorization at the HTTP edge. It says nothing
about what a manifest's generated store reads. That is the second of
gleester's two findings, and it is a larger problem than a seam —
which is why it gets questions here rather than a slice.

## What gleester needs concretely

In gleester every read is scoped to a viewer. A list of wish lists is
not "the rows in `wishes`" with a filter bolted on at the edge; the
set of rows a request may see is a property of the request. The same
holds for show, edit and delete — a row the viewer cannot see should
not become fetchable by guessing an id, and a row the viewer may read
but not change should not be writable.

A rastrillo manifest today generates `ListTicketTypes`,
`GetTicketType`, `UpdateTicketTypeBasics` and their siblings with no
viewer anywhere in their signatures
(`examples/tickets/gen/store/ticket_types/queries.sql`). For an app
like gleester that is not a missing feature; it is an information leak
wearing a feature's clothes. An app that adopted a manifest for its
wish lists would ship a list screen showing every user's rows on day
one.

The requirement that follows is stronger than "offer a way to scope".
Scoping has to be impossible to forget once a resource declares it. If
a developer can add a generated action, or a query, or a resource, and
have it come out unscoped by default, the mechanism has failed at the
only job it exists to do. A convention — remember to add the WHERE
clause — does not satisfy that.

## The dependency: where does the viewer come from?

rastrillo has no identity story. No sessions, no login, no user table,
nothing that turns an `*http.Request` into a person. That is friction
F7 in the family's log, deferred precisely because it needs a full
design cycle of its own rather than a slice.

Scope's signature depends on how F7 resolves. If the framework owns
identity, a scoped query takes a `viewer_id` that only exists once
sessions and auth exist, and Scope cannot land before them. If the
framework does not own identity, Scope could instead be designed
against something the app supplies — an abstract
`Viewer(r *http.Request) (string, bool)` on `Options`, whose
implementation is entirely the app's business — and auth could arrive
later without disturbing the scoping contract.

So: does Scope land together with auth in one cycle, or ahead of it
against an app-supplied viewer function? If ahead, what is the
viewer's type — an opaque string, a typed id, something richer that a
role check could also read? And what is a request with no viewer: does
a scoped resource show nothing, refuse the request, or is "no viewer"
a legitimate state that some resources accept?

## The prior question: is the database boundary the visibility boundary?

The platform runs a SQLite database per app instance, and nothing
stops a deployment from running an instance — and so a database — per
tenant. If the tenant boundary *is* the visibility boundary, unscoped
generated CRUD is safe by construction: `SELECT * FROM wishes` can
only return rows the activating viewer was allowed to reach, because
routing the request to that database already was the authorization
check. In that world the three mechanics below are a niche feature,
not a manifest prerequisite, and what remains of gleester's finding
is (a) identity — you cannot route to "my" database without knowing
who "me" is, so F7 is needed either way — and (b) whatever
`Options.Wrap` must hold at the edge to decide which databases a
session may activate.

So the design cycle's first question may not be "how does a viewer
thread through sqlc" but "what is a tenant, and when does the
database boundary stop being the visibility boundary?" Candidate
regimes, none endorsed:

- **Tenant = one viewer** (a personal tool, a single-team admin
  panel — examples/tickets is implicitly this). The DB boundary is
  the scope; nothing below applies.
- **Tenant = a group with internal roles** (a member sees their own
  drafts, an admin sees all). Partitioning shrinks the blast radius
  but reinstates in-database scoping for the within-tenant rules.
- **Tenant = the shared thing itself** (a database per wish list,
  activation gated by membership or share token). Row scoping
  disappears, but gleester's column-level rule — the owner must not
  see purchased flags on rows in their own list — is per-viewer
  visibility *within* one tenant database, so some viewer-awareness
  survives in actions and templates even under perfect partitioning.
- **Data replicated per viewer's database.** Scoping trades for
  sync/merge — which is what the design doc's deferred `Mergeable`
  store shape looks like it exists for. A different problem, not a
  smaller one.

Questions this adds: does rastrillo take a stance ("apps are
per-tenant by default; multiple viewers in one database is the
exception you opt into"), and if so where is that stance visible — a
manifest declaration, an Options field, documentation only? Is
gleester's sharing graph naturally tenant-per-wishlist, or dense
enough (friends-of-friends, aggregate views across lists) that
partitioning by database was never viable? And what does the platform
owe here — is "a database per tenant" a deployment shape the
activator already supports, or new platform work this design would be
gated on?

## Three candidate mechanics for threading scope through generated sqlc queries

The constraint that makes this genuinely hard: generation emits
`schema.sql` and `queries.sql` per resource and runs `go tool sqlc
generate` over them. The output is static SQL with typed parameters.
There is no runtime query builder to hang a scope onto — scoping is
present in the SQL text at generation time, or it is not present at
all.

(a) **Column convention.** Every scoped resource carries a `viewer_id`
column; generated queries append `WHERE viewer_id =
sqlc.arg(viewer_id)`. Simplest, and it composes with the existing
search, filter and pagination predicates. Failure mode: it models
ownership, not visibility — a wish list shared with three friends has
no single owner column, and shared many-to-many visibility is exactly
gleester's case.

(b) **Generated WHERE-fragment hook.** The manifest declares
`scope = "<SQL>"`, joined into every generated query. Expressive
enough for a join against a membership table. Failure mode: SQL
embedded in TOML, unvalidated where it is declared, and sqlc types
parameters by static analysis — an arbitrary fragment carrying
arbitrary parameters is not something the generator can check before
sqlc chokes on it.

(c) **Query ejection per resource.** Generation emits the queries and
the app takes over the scoped ones. The ejection story today covers
templates and actions, not `gen/store/<name>/queries.sql`, so the
store would have to join it first. Failure mode: hand-scoping is the
forgettable convention section 2 rules out, and ejection is per file —
a resource's unejected queries keep regenerating unscoped beside it.

None of the three is endorsed here.

## Questions for the design cycle

1. One Scope shape for a resource, or per-action shapes? A list scope
   and a show scope are the same predicate in the simple case and
   different in gleester's ("everyone I share with can read, only I
   can edit").
2. What is the 404-vs-403 story for generated screens? A 403 on an
   out-of-scope row confirms the row exists; a blanket 404 hides
   existence but makes a genuine permission error indistinguishable
   from a typo. Does the framework pick one, or does the resource?
3. Does `gen/manifest.json` carry scope declarations? Its evolution is
   additive-only, so whatever shape lands there is a contract with any
   future renderer or tool — which argues for settling it in the same
   cycle rather than after.
4. What do Eleven, Keymail and Woodstar do by hand today for exactly
   this, and does the proposed mechanism cover their call sites? A
   scoping design the family's own existing apps could not adopt is
   not finished.
5. Does scope apply to writes as well as reads, and is a scoped delete
   a different question again given no delete action is generated
   today?
6. What does `rastrillo generate --check` owe here — can it prove a
   declared-scoped resource generated no unscoped query?
7. Does an unscoped resource stay legal, and how loud is that choice?
8. Before any of 1–7: is the tenant/database boundary the visibility
   boundary (see "The prior question" above)? If the answer is "often
   yes", the cycle should decide which regime rastrillo speaks for
   before designing query mechanics for the regimes it doesn't.

## Input we want from James

Two things, concretely.

First: which of the three candidates — if any — fits gleester's
wish-list sharing model? The sharing shape is the part we are guessing
at from the outside. Whether a wish list's visibility is a membership
table, a share token, a friendship graph, or something else decides
whether a `viewer_id` column is merely insufficient or actively the
wrong model, and it decides how much SQL a fragment hook would need to
express.

Second: what do the call sites of gleester's hand-rolled
authorization layer actually look like? Not the policy — the shape.
Where does the viewer enter, what does a handler hold by the time it
queries, how many distinct scoping predicates does the app really
have, and how often does one read need a scope that no other read
uses? A framework mechanism that made gleester's existing code longer
would be a failure of the same kind as the leak, and the honest way to
find that out before designing is to read the code that exists.

Anything gleester does today that a generated store would make harder
rather than easier is worth writing down here too, even where it is
not about scoping.
