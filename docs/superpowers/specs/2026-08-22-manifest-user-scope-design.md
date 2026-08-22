# Manifest per-user scoping — design

**Goal:** the declarative path reaches where the code path does: a
manifest resource can declare per-user scoping, and its generated
store, actions and screens then enforce the same owner discipline
`examples/notes` hand-writes — a row that isn't yours is a row that
doesn't exist (404, never 403).

**Ruling this implements (Paul, 2026-08-22):** "Manifest resources
with per-user scoping — the declarative path should reach where the
code path does" (the site's own Pending list).

## The declaration

One new key on a manifest resource:

    scope = "user"

`rastrillo.Resource` gains `Scope ScopeKind` (`json:"scope"
toml:"scope"`), values `""` (unscoped — today's behavior, byte-exact)
and `"user"`. Validate rejects anything else and reserves the column
name `owner` (case-insensitively) alongside id/created_at/updated_at —
reserved unconditionally, not only when scoped, so flipping scope on
later never invalidates an existing manifest.

## Who owns a row

The owner is the session **Subject**, stored verbatim in a new
`owner TEXT NOT NULL` column (indexed). Subject, not a numeric user
id: the framework's default sign-in (magic-link auto-upgrading to
keymail) uses email subjects, and the password plugin uses numeric-
string subjects — a TEXT owner column serves both without asking
plugins to agree on a numeric type (the same reasoning as
sessions.Session.Subject itself).

## What generation changes when scoped

- **Store** (`gen/store/<name>/`): schema and migrations add
  `owner TEXT NOT NULL` plus an index; every query is owner-filtered —
  List/Count gain an always-on `owner = sqlc.arg(owner)` clause
  (before the optional search/filter clauses), Get/Update/Delete key
  on `id AND owner`, Create inserts the owner. Under sqlc's arity
  rules Get/Delete move from a bare id argument to a Params struct.
- **Actions** (`gen/actions/...`): every Handle opens with
  `sessions.Current(r)`; no session answers 403 ("signed out") —
  defense-in-depth under the documented contract that scoped routes
  mount behind `sessions.Require` (or `auth.RequireSession`). Every
  store call binds `Owner: sess.Subject`. A wrong-owner id hits the
  owner-filtered Get and answers the same 404 a missing row does.
- **Templates, locales, router:** unchanged — the owner column is
  never a declared field, so no screen shows it and no key names it.

An unscoped resource's generated output is byte-identical to before
this change (pinned by the existing goldens).

## The context seam fix that rides along

`auth.RequireSession`/`RequireFreshSession` stash identity under
auth's own context key, so `sessions.Current` — and therefore a
scoped generated action — is blind behind them (the uid-0 trap,
generalized). `sessions` exports `WithSession(r, sess) *http.Request`,
and auth's middlewares now stash both, so `sessions.Current` agrees
with `auth.From` everywhere. SKILL.md's trap note shrinks to the one
remaining case: `sessions.UserID` is still (0,false) for non-numeric
subjects.

## Proof

`examples/notes` becomes the mixed-paths example Paul's manifests
ruling describes: its hand-written, hand-scoped notes CRUD stays, and
one declared resource — `manifest/bookmarks.toml`, `scope = "user"`
(Title required, Link, Notes textarea; search on) — mounts inside the
same `sess.Require` group. The two-user isolation suite extends to the
generated screens: Bob's list never shows Alice's bookmark, and his
GET/edit/delete of her id answer 404. `rastrillo generate --check`
joins notes' CI the way it guards tickets.

`examples/tickets` stays unscoped on purpose: it is the regression
host proving the unscoped path never drifts.

## Out of scope

Team scoping (`scope = "team"`), owner columns with custom names, and
admin cross-owner views — all real, none needed to make the
declarative path reach the code path's owner discipline.
