# 🤖 idear: roles and membership as an addon

Design, 2026-08-23. Approved by Paul the same evening.

A second CARLOS authoring bake-off graded five stacks building the same
team Kanban app. Rastrillo and `carrillo-chassis` **tied on the rubric** —
49/50 each, identical on all four axes, both a perfect 20/20 on the
weighted security axis with zero confirmed defects. The chassis was
ranked first on a metric the rubric records but does not score: builder
tokens, 165,944 against Rastrillo's 216,012.

The gap has one cause, and it is not framework friction. Rastrillo's
builder was *more* efficient per line — 61 output tokens per written line
against the chassis's 95. It simply had to write 1,781 more of them:
3,537 written lines against 1,756. That excess is almost exactly the
accounts-roles-invitations layer the chassis inherits and Rastrillo has
no answer for. The framework contributed nothing to the surface the
bake-off grades hardest, because it has no role concept: `scope.Owned`
is a per-user `WHERE user_id = ?`, and a team-global Kanban board is not
an owned row.

This builds that layer — **outside the framework**.

## 1. Why an addon, and not core

The obvious response to the bake-off is `rastrillo/team`. It is the wrong
one.

Two arguments for it are weak and worth discarding before they get
repeated. The `SKILL.md` budget is one: the ceiling has already been
raised twice (15k → 16k → 17k), and an addon still costs the same ~330-byte
pointer a core package's `Full treatment:` line would. "Not every app has
members" is the other — it applies equally to `passkey`, `jobs` and
manifests, all of which are core.

The real reasons are narrower and hold up. **Release cadence:** roles
will churn (reactivation, expiry policy, break-glass) at a rhythm that
should not drag the framework's tag along, and a v0.x framework already
asks enough of its consumers. **Gate honesty:**
`internal/docsite/symbols_test.go` requires every core package to carry a
reference page naming every exported symbol; that gate is valuable
exactly because it is expensive, and it should be spent on what every app
mounts.

What the split genuinely costs, stated so nobody rediscovers it later:
MVS version skew (idear pins a rastrillo; a `sessions` or `migrate`
change can block an app's upgrade until idear catches up), a second
frozen-checksum and CI regime, and the vanity-path risk of §9. Those are
accepted, not waved off.

So idear ships as its own module, versioned separately, and the
dependency arrow points one way: **idear imports rastrillo; rastrillo
never imports idear.** Inside this repo the change is a documentation
page, its nav entry, a pointer in `SKILL.md`, and one small seam in
`password` that §5 forces and §8 pays for.

### The tenancy ruling survives intact

The 2026-08-22 ruling — instance-per-team, team tenancy is APP-level, no
membership package — was about *cross-team isolation*, and it stands
unchanged. `carrillo-chassis` agrees with it completely; its own skill
doc opens "the instance IS the tenant… there is no tenant field, no
tenant scope."

Owner > Admin > Member *inside* one instance is a different question, and
the ruling never answered it. idear is the intra-instance half. Nothing
here reintroduces a tenant column, a tenant scope, or `/t/{slug}`
routing.

## 2. The seam this fills already exists

`auth.Config.Authorize` is documented, in the framework, today:

> Authorize is the admission gate: given a verified address, may it have
> a session? Nil admits every verified address. **Membership models
> (tables, roles, admin bootstrap) are app policy layered on this hook.**

That hook was left open for precisely this. idear fills it rather than
inventing a parallel one — which is the difference between an addon and
a fork.

## 3. What idear is

The **roster** for a Rastrillo instance: who is in it, at what role, and
who may change that.

It is not an identity provider. It never mints a session, never hashes a
password, never renders a sign-in form. It sits on top of `sessions` and
whichever identity plugin the app already chose, so an app using keymail
or passkeys keeps them.

Module path `amadan.net/rastrillo/idear`, repository
`https://amadan.net/rastrillo/idear`. It depends on
`rastrillo/{sessions,migrate,form,flash}` and `gorm.io/gorm`. It does
**not** depend on `rastrillo/ui`: the example styles its pages, the
library does not own a look.

## 4. The data model

Two tables and one migration set, following the `sessions` / `auth` /
`blobs` convention exactly.

```go
type Member struct {
	ID            int64
	Subject       string `gorm:"uniqueIndex"`
	Email         string `gorm:"index"`
	Name          string
	Role          Role `gorm:"not null;index"`
	DeactivatedAt *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type Invitation struct {
	ID         int64
	Email      string `gorm:"index"`
	Role       Role   `gorm:"not null"`
	TokenHash  string `gorm:"uniqueIndex"`
	InvitedBy  int64
	CreatedAt  time.Time
	ExpiresAt  time.Time
	AcceptedAt *time.Time
	RevokedAt  *time.Time
}

var Schema = migrate.MustFromFS(migrationFS, "idear")
```

Four decisions worth stating, because each has an alternative that looks
better until you try it.

**`Subject` is a string, and it is the join.** idear does not own email
and password — the app keeps its own `User` row. The key between them is
the session `Subject`, because it is the only identifier both identity
plugins produce: the password plugin's Subject is a numeric user id,
keymail's is a verified email address. `sessions.UserID` returns
`(0, false)` under keymail, and a membership layer keyed on it would
resolve every keymail viewer to member zero. `jobs` already set this
precedent — its owner is the session Subject, for the same reason.

**The token is hashed at rest, and it expires.** `TokenHash` stores a
SHA-256 digest; the plaintext token exists only in the emitted link.
`sessions` already holds nothing but `HashToken` digests, and an addon has
no business being laxer than the core it rides on. `ExpiresAt` defaults to
seven days. Round 1 of the bake-off penalised an arm for immortal,
anonymously-fetchable invitation tokens that disclosed team and email;
this is that finding, applied before it is earned a second time. Tokens
are 32 bytes of `crypto/rand`, and `GET /invitations/{token}` does not
echo the full invited address.

**Removal is `DeactivatedAt`, never a delete.** A deleted row dangles
every `AuthorID` in the app's own tables. The corollary is that
reactivation must exist as a first-class operation (§5) — `Subject` is
unique, so without it a removed person can never be readmitted by any
path: keymail's `Authorize` sees the inactive row and refuses, password
re-signup hits the app's duplicate-email check, and a fresh invitation's
Member insert collides with the dead row.

**Apps merge `idear.Schema` into `BootSchema`, never into `Schema`** —
`migrate.Merge(sessions.Schema, idear.Schema, Schema)`. Merging it into
the app's own `Schema` makes `rastrillo migration check` propose dropping
tables that `Models` does not know about. This is the documented rule for
every subsystem and idear is not an exception to it.

## 5. The API

Mirrors `jobs`: a core built at boot, then handlers that error unless
their renderers are set.

```go
r, err := idear.New(idear.Config{DB: d.G, OpenSignUp: false})
h, err := idear.NewHandlers(idear.HandlerConfig{
	Roster:           r,
	RenderMembers:    renderMembers,
	RenderInvitation: renderInvitation,
	NotFound:         renderNotFound,
})
```

`Config.Subject` defaults to reading `sessions.Current(r).Subject` and
exists as an override for an app whose viewer arrives another way.

### Middleware — the membership gate

- `r.Require` — signed in *and* an active member. A non-member is
  answered by `HandlerConfig.NotFound`, which **must be the same renderer
  the app gives chi's own `NotFound`**. "Byte-identical 404" is otherwise
  unimplementable: an app with a custom 404 page makes idear's default
  `http.NotFound` distinguishable, and that delta *is* the membership
  oracle `SKILL.md` §3 forbids. The default is `http.NotFound`; an app
  with its own 404 page and no `NotFound` hook is the one misconfiguration
  idear cannot detect, so the skill doc leads with it.
- `r.RequireRole(min)` — **403**, because a member can legitimately see
  the page and merely may not act. It **stacks inside** `Require`; mounted
  bare it would 403 a non-member and break the 404 rule.
- `Config.Subject` reads a session the caller must already have resolved:
  mount `Require` *inside* a `sess.Require` (or `auth.RequireSession`)
  group. Mounted outside one, the Subject is empty and every request 404s
  — silently, and identically to a real refusal. Signed-out requests are
  the upstream middleware's business; idear does not redirect.
- `idear.From(req) *Member` — the viewer, following `auth.From`.

### Policy, as pure functions

`Role.AtLeast`, `ParseRole` (which accepts only the three known roles —
anything else is not a role, not a default), and:

```go
func MayActOn(actor, target *Member) error
```

Admins manage Members only; nobody acts on themselves; the target's rank
must be strictly below the actor's. Keeping this a pure function is what
lets the role matrix be tested exhaustively without an HTTP server.

### Store operations

Each is one transaction and each enforces its own invariant, because an
invariant checked outside the transaction that maintains it is not an
invariant:

`Claim` · `Invite` · `Revoke` · `Accept` · `SetRole` · `Deactivate` ·
`Reactivate` · `Transfer`.

Three carry invariants that a naive implementation loses:

**`Accept` consumes the invitation by CAS, not by lookup.** The update is
`SET accepted_at = ? WHERE id = ? AND accepted_at IS NULL AND revoked_at
IS NULL AND expires_at > ?`, with rows-affected checked, **inside** the
same transaction as the Member write. Anything softer lets `Revoke` race
acceptance and admits a revoked invitation. Single-use cannot be delegated
to the app's unique-email index — idear can neither see nor enforce that
index, so the CAS is the invariant.

**`Transfer` checks the target is active inside its own transaction.**
Otherwise a transfer racing a `Deactivate` of the same target produces a
deactivated Owner: an instance with no one able to administer it and no
one able to be promoted.

**`Claim` means zero rows, not zero active rows.** A roster whose members
have all been deactivated must not reopen the claim — that would hand a
stranger Owner of an instance full of dormant data.

### Admission: the token is the credential

This is the part the first draft got wrong, and it was wrong in the one
place that mattered.

The rule is **possession of the invitation token, plus an email match** —
not an email match alone. The chassis resolves the invitation *from the
token* and then checks `inv.Email == email`
(`carrillo-chassis/handlers_auth.go:115-129`). Email-match alone is
exploitable under the password plugin, which never verifies an address:
anyone who learns that `admin@corp.com` was invited registers that address
with their own password first and lands at the invited role.

`password.Config.Create` is `func(ctx, email, hash) (int64, error)` — it
receives no `*http.Request`, so the token cannot be read from the form
inside it. It does receive `r.Context()`. So idear supplies a middleware:

```go
r.Post("/signup", idear.CarryToken(ph.Signup))
```

`CarryToken` reads the `invite` field from the posted form and stashes it
in the request context; `Admitting` reads it back. Mounting it is
**mandatory** on the password path — without it every invited signup is
refused, which is loud and safe rather than quiet and permissive.

Admission then decides the role **before reading anything else the form
says**, in this order: **claim** if the roster has zero rows; otherwise a
**valid token** — unexpired, unrevoked, unaccepted — whose `Email` equals
the submitted address; otherwise `OpenSignUp` at `RoleMember`; otherwise
refuse. The role is never read from the form on any path.

### Reconciliation: the signed-in half of the invitation routes

`Admitting` cannot enlist the app's opaque `Create` into its transaction,
so a failure between "app user created" and "Member written" leaves a
user row with no membership. The first draft called this an honest edge
and claimed a retry heals it. **It does not:** the retry's `Create` fails
on the now-duplicate email, and `password.Signup` renders that as 422
without ever reaching the Member write. The orphan can sign *in* and 404s
forever.

The same terminal state is reachable with no failure at all. Two
concurrent first signups both observe an empty roster; one wins the
`Claim`, and the `ErrOwnerExists` loser is an orphan.

So reconciliation is a designed path, not a footnote, and it is what the
public invitation routes are *for*:

- `GET /invitations/{token}` — renders the invitation to anyone holding a
  valid token, naming the instance and the role but not the full address.
- `POST /invitations/{token}` — **for a signed-in viewer with no Member
  row**: given a valid token matching their session, or a roster with zero
  rows, it writes the Member from the live session `Subject`. This heals
  the orphan, resolves the `Claim`-race loser, and is the only path by
  which a Member row is created from an already-live session.

Both public routes are rate-limited; `GET` is an unauthenticated lookup
of a secret and must not be a free oracle.

### The two identity adapters

```go
// keymail — fills the hook auth already documents as the app's
auth.New(auth.Config{Sessions: sess, Authorize: r.Authorize})

// password — wraps the app's own Create
password.New(password.Config{Sessions: sess, Create: r.Admitting(createUser(d.G))})
```

Two asymmetries between them must be documented rather than smoothed
over, because a reader will otherwise assume the guarantee is uniform:

**Refusals need a channel.** `password.Config.Create`'s contract is that
*any* error means duplicate-email, so an uninvited visitor would be told
their address is already registered — false, and an enumeration oracle of
exactly the kind PRs #69–#73 closed. This design therefore adds
`password.ErrRefused` to the **core** package: `Signup` checks
`errors.Is(err, password.ErrRefused)` and renders the plugin's message at
403. That makes §8 more than the three files the first draft promised,
and the seam is worth it — anything gating signup needs it.

**Deactivation is enforced per request, not at sign-in — under password.**
`password.Signin` runs Lookup → Verify → mint with no idear involvement;
only keymail's admission consults `Authorize`. So a deactivated member or
an orphan can still *mint a session* under the password plugin; what stops
them is `Require` on every route. The example must therefore gate `/`
itself, and the skill doc must say that an ungated landing page is the
one place this design leaks. The chassis refuses at sign-in because it
owns the credential check; idear does not, and should not claim to.

**`Authorize` returns a bool with no error channel** (`func(address
string) bool`, verified in `auth/auth.go`). A database failure during
admission is therefore indistinguishable from a policy denial to the
visitor. idear logs the distinction even though it cannot render it.
`Authorize` also runs *before* `SecondFactor`, so a Member row can be
written for a sign-in that a 2FA gate never completes — self-healing on
the next attempt, and stated so nobody reads it as a bug.

### Handlers

Paths belong to the app; these are the defaults the example mounts.

```
GET  /members
POST /members/invitations
POST /members/invitations/{id}/revoke
POST /members/{id}/role
POST /members/{id}/remove
POST /members/{id}/restore
POST /members/transfer
GET  /invitations/{token}      (public)
POST /invitations/{token}      (public, signed-in reconciliation)
```

Rendering goes through callbacks the app supplies, following
`password.Config.RenderSignin`. idear owns the flows, where the role
rules actually get enforced; the app owns its shell.

## 6. Repository shape

```
role.go          Role, AtLeast, ParseRole, Title
member.go        Member, Invitation
roster.go        Config, New, the store operations
policy.go        MayActOn — pure, no net/http
middleware.go    Require, RequireRole, From
admit.go         Authorize, Admitting
handlers.go      NewHandlers, the eight handlers, the PageData types
migrations/0001_init.sql
example/         a working app on rastrillo + idear
SKILL.md         idear's own authoring doc
Makefile, .amadan/ci, .amadan/ci.d/
```

idear ships its **own `SKILL.md`** on the same contract as Rastrillo's —
an agent loads it instead of the source. That mechanism, not the code, is
what made the chassis arm cheap: a 361-line skill doc carrying a
~3,110-line platform layer the builder mostly did not have to read. (Not
"never read" — round 2 records the carrillo builder's context as including
"the chassis Go studied to use it." The saving is real and it is partial.)

### How an agent gets it

This is the part that decides whether the addon thesis holds at all, and
it does not come for free the way the framework's does. Rastrillo's
`SKILL.md` sits at the repo root the scaffold points to. idear's would
land in a versioned module-cache directory nobody names, and
`docs/addons` is a directory page, not a skill.

So idear's `SKILL.md` is **served at a stable URL**,
`https://amadan.net/rastrillo/idear/SKILL.md`, and `docs/site/addons.md`
carries the exact `curl` line for it — matching the convention Rastrillo's
own `SKILL.md` already uses for `curl -s
https://rastrillo.org/docs/<page>.md`. No new machinery, and it
generalises to every future addon: the directory page's job is to hand an
agent a fetchable skill, not to describe one.

An addon whose skill doc an agent cannot find saves an app the typing and
none of the reading — which is the whole cost argument, lost.

## 7. Testing

Tests drive the HTTP surface with a cookie jar and real CSRF tokens
scraped from rendered pages — the path a browser and an attacker both
take. The authorization suite is the deliverable, not a supporting
artifact:

1. a non-member is refused **read and write on every route**, with
   byte-identical 404s, including deeply nested ids;
2. a Member is refused every management action;
3. an Admin cannot change, demote or deactivate an Admin or the Owner;
4. a posted `role=owner` never lands, on any path, for any actor;
5. the single-owner invariant holds **across six concurrent transfers**,
   run as an actual race — round 1 of the bake-off found exactly this bug
   in the hand-rolled version;
6. migration checksums frozen, matching `migrate/frozen_checksums_test.go`.

Six more, each pinning a defect this design was revised to close:

7. an invited address cannot be claimed **without the token** — registering
   `admin@corp.com` on an invite-only instance with no `invite` field
   admits at no role, not the invited one;
8. `Revoke` racing `Accept` never admits: the CAS is exercised
   concurrently, not asserted about;
9. an expired invitation is refused, and an accepted one cannot be
   replayed;
10. an orphaned user — created, then failed before the Member write — is
    healed by `POST /invitations/{token}` while signed in, and 404s on
    every route until they are;
11. `Transfer` racing `Deactivate` of the same target never yields a
    deactivated Owner;
12. a deactivated member can be reactivated and regains exactly their
    prior access, and no more.

**One anti-pattern designed out.** Round 1's auditor found a test written
to whitelist the very payload it listed, so a green suite passed over a
live open redirect. Allow-list tests here derive their payloads from the
shared list under test; they never restate it. A test that quotes the
implementation proves the implementation equals itself.

## 8. The changes in this repository

More than the three files the first draft promised, because
`password.ErrRefused` (§5) is a core change and has to be paid for
honestly:

**`password/handlers.go`** gains an exported `ErrRefused` sentinel and one
branch in `Signup`: `errors.Is(err, password.ErrRefused)` renders the
wrapped message at 403 instead of the duplicate-email copy at 422. With
it come its tests and a line on `docs/site/reference/password.md`, which
`symbols_test.go` requires for any new exported symbol. This is a seam,
not a feature — anything gating signup needs it, and idear is simply the
first.

**`docs/site/addons.md`** plus its `nav.json` section. Written as a
directory that scales past one entry: what an addon is — versioned
separately, depends on rastrillo and never the reverse, ships its own
namespaced `migrate.Set` and its own `SKILL.md` — then the idear entry
with its module path, what it does, what it deliberately does not, and
the wiring. Every Go fence must parse; that is a gate, not a style note.

Living in `docs/site/` rather than the website repo means it inherits all
six docsite gates and rides the existing vendoring. The URL is
`rastrillo.org/docs/addons`.

The addons page also carries the `curl` line for idear's own `SKILL.md`
(§6) — without it the directory describes a skill instead of delivering
one.

**`SKILL.md`**, roughly 330 bytes into §3 — where the reader has just
been told scoping separates users and not tenants, and immediately
wonders how roles work:

> **Roles and membership are an addon, not core.** Rastrillo has no role
> concept: who is *in* this instance and at what rank is
> `amadan.net/rastrillo/idear` — Owner/Admin/Member, invitations, and
> the members UI, over `sessions` and either identity plugin.
> Full treatment: docs/site/addons.md — rastrillo.org/docs/addons

That lands the file near 16.4 KB against the 17 KB budget, buying the
pointer without a trim, in the existing `Full treatment:` convention.

## 9. Sequencing and open risk

The in-repo documentation ships first, as its own pull request: it states
the doctrine and is independently useful before any of idear exists.
idear follows as its own plan against the amadan repository.

**Open risk: the vanity import path.** `amadan.net/rastrillo/idear`
requires amadan.net to serve a `go-import` meta tag at
`/rastrillo/idear?go-get=1`. Paul believes it already does. Believing is
not verifying, and the whole premise of the addon is that an unattended
agent can `go get` it, so a smoke test against a throwaway module is task
zero of the idear plan — before anything depends on the path.

## What this does not claim

idear closes a **cost** gap, not a correctness one. Both arms scored
20/20 on security with zero confirmed defects; the hand-rolled gate held
against live adversarial probing. Nothing here says Rastrillo was unsafe.

And the chassis's own caveat transfers wholesale: **a library buys the
mechanism, not the discipline.** idear makes the safe call the short
call. It does not remove the engineer at the seams it cannot cover.
