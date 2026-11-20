# Multi-instance apps as the easy default

**Date:** 2026-11-19
**Status:** approved by Paul 2026-11-19; amended 2026-11-20 (custom
domains, §7.4)
**Builds on:** the user-models design, approved 2026-09-28
(`carlos/pegamento`, branch `design-user-models`,
`docs/superpowers/specs/2026-09-27-user-models-design.md`, called "UM"
below) and its roadmap (`docs/superpowers/plans/2026-09-28-user-models-roadmap.md`);
the scaffold-shapes design (`origin/scaffold-shapes`,
`docs/superpowers/specs/2026-10-25-scaffold-shapes-design.md`, approved,
plan written, no code yet).

## 1. Why

Paul is building Seamus, a CMS for events, and has built Patrix, a
general-purpose blogging platform. Both want Tito Go's user model:

- people and permissions live at the root domain, in object storage with
  conditional writes;
- "create account" provisions a new instance automatically;
- each account lives at a subdomain (`acme.example.com`) or a
  subdirectory (`example.com/acme`);
- it behaves like ordinary SaaS, but each account is its own process and
  its own database, so it scales and isolates far better.

Agents building on Rastrillo don't reach for this today. They take the
"simpler" one-instance shape because that is what `rastrillo new` writes
and what the skill shows first, even though CARLOS does the heavy lifting
(creating instances, routing, hibernation). The user-models design
already chose the models and most of the mechanism. Half of it is built
(`objrec`, stateless sign-in, `provision`, `basepath`). What is missing
is the part that makes it a choice an agent actually makes: the
scaffolds, the root app itself, an admin panel, and a skill that asks
the question.

**Success:** an agent asked to build "an events CMS where each organiser
signs up and gets their own space" asks one-or-many, picks many, runs one
`rastrillo new` command, and has signup → provisioned instance → signed-in
handoff working and tested before it writes a line of product code. A
superadmin can see every person and account at `/fleet`.

## 2. Goals, non-goals, anti-goals

**Goals**

1. When an app has accounts that belong to separate customers, teams or
   organisations, the agent is steered to one instance per account, and
   the path to it is one command.
2. Both addresses work: subdomains (UM model B, "home") and
   subdirectories (UM model C, "hub").
3. Every multi-instance app also runs standalone: one instance with its
   own sign-in and no root app (Paul, 2026-11-19, option 1). Local
   development and self-hosting use this.
4. The root app comes with an admin panel at `/fleet`, modelled on Tito
   Go's, for superadmins only.
5. Seamus and Patrix can adopt it without forking it.

**Non-goals (this design)**

- Support access (a superadmin entering a customer's account). It will be
  its own add-on later, and depends on the CARLOS trust boundary
  (Paul, 2026-11-19).
- Billing, region moves, takedowns, imports.
- Migrating Tito Go or Eleven onto it.
- Blind (end-to-end encrypted) homes: UM model D is unchanged.

**Anti-goals: failure even if it technically works**

- The single-instance scaffold stays the path of least resistance, so
  agents still pick it for multi-customer apps.
- A proxy in front of the platform's routing. Routing is CARLOS's job.
  Platform gaps are filed as requests, never built around.
- Tenancy by `WHERE` clause: one instance, a `tenant_id` column, every
  customer in one file.
- A stateful centre: a root app with its own SQLite.
- A multi-instance app that cannot run without its root app.

## 3. Constraints

- **UM rulings stand**, and this design does not reopen them: stateless
  centre (records in the app's store via `objrec`); handoff by signed
  assertion for subdomains; a shared hub-signed session for
  subdirectories; provisioning credential on the central app only; the
  five safety invariants (UM §8), in particular **"an instance decides
  access from its own roster; a signature proves who, never what."**
- **The platform routes both ways.** Hostnames, with catch-alls
  (`carlos domains attach '*.<parent>'`), and path prefixes
  (`PUT /api/cli/apps/{account}/{app}/domains/{host}/paths`, up to 10,000
  prefixes per hostname). `provision.PublishPaths` already drives the
  second.
- **Open platform requests.** carlos/platform #185 (create-only
  provisioning role with a quota) and #186 (path targets on another box)
  are both open. Without #186 every subdirectory account would live on
  the root app's box. **Subdirectory accounts wait for #186 rather than
  ship one box big** (Paul, 2026-11-19); #186 is being designed on
  `carlos/platform` branch `path-routes-across-boxes`. The subdomain
  model has no such limit and goes first. Until #185 ships, the root app
  holds the app-wide `instance` role and lists that as a published leak
  (UM §7.1).
- **Never wake a sleeping instance to look at it.** Anything the root
  app shows about instance state comes from the platform, not from
  probing the instance.
- **Addon rules** (`docs/site/addons.md`): depends on Rastrillo, never
  the reverse; ships its own `SKILL.md`; does not re-implement core.
- `scaffold-shapes` must land first: it adds the `--scaffold` flag and
  reserves `home` and `hub`.
- Rastrillo's `SKILL.md` has a size budget enforced by `skillmd_test.go`.

## 4. Approaches considered

**A. All in Rastrillo core.** Handoff, signup, the root app and `/fleet`
in core; `rastrillo new` generates it all. One module and found by
default, but core has stayed building blocks, and every app would carry
an admin UI and signup flow.

**B. Building blocks in core, the root app as an add-on (chosen, Paul
2026-11-19).** Core gets the instance-side pieces every multi-instance
or standalone app needs. A new add-on module holds the root app: signup
that provisions, sign-in, the account switcher, members pages, roster
delivery and `/fleet`. `rastrillo new` asks the question and, for a
multi-instance answer, pulls the add-on in and wires it. Cost: two
modules to keep in step, and agents find the add-on only if core points
at it, so the routing in `SKILL.md` and `rastrillo new` is what makes or
breaks this.

**C. Extend idear.** Rejected: idear lives inside one instance and keeps
its tables in that instance's SQLite. The root app has no database.
Mixing them blurs the line idear was built around. idear does gain the
instance side of roster delivery (§6.3), which is squarely its job.

Paul also chose **handoff over Tito Go's router** (2026-11-19): the root
app passes a signed person to the instance and the platform routes
requests straight to it, rather than the root app proxying every
request with signed `X-Tito-Auth-*` headers. The root app can sleep, and
nothing sits in front of CARLOS's routing.

## 5. What an agent experiences

### 5.1 The question

The root `SKILL.md`'s "Start here" opens with the question, before any
`rastrillo new` command:

> **One instance, or one per account?** If the app has accounts that
> belong to separate customers, teams or organisations (an organiser's
> events, a company's blog), build one instance per account. CARLOS
> creates, routes and sleeps them; Rastrillo writes signup, sign-in and
> the admin panel. Ask the person which address they want: subdomains
> (`acme.example.com`, `--scaffold home`) or subdirectories
> (`example.com/acme`, `--scaffold hub`). One instance (`single`,
> `accounts`) is for an app one team or one person uses together.

with a pointer to `skill/instances.md` for the trade-offs.

`rastrillo new` **requires** `--scaffold` (my call, amending
scaffold-shapes' `single` default). Without it, it exits non-zero and
prints the menu, one line per shape, with `home` and `hub` first under
"accounts for separate customers". An agent that skipped the skill still
meets the question. Existing docs and tests that run plain
`rastrillo new <name>` change to `--scaffold single`.

### 5.2 What `--scaffold home|hub` writes

One module, one binary, two CARLOS apps (my call; UM §10 said two
binaries; Tito Go's single binary with operator subcommands is the
precedent this follows):

- `cmd/<name>/main.go`: serves **an instance** by default. The
  platform starts every instance with a fixed command line and passes no
  arguments of an app's choosing, so the root app is selected by an
  environment variable set on the root CARLOS app (`<NAME>_ROLE=root`,
  exact name the builder's), not by a subcommand. Operator subcommands
  run by a person stay subcommands.
- `internal/<app>/`: the product, written as an `accounts`-shape app
  (sign-in, idear roster, owner-scoped rows) plus the handoff entry
  (§6.2) and the roster receiver (§6.3).
- `internal/root/`: a few lines wiring the add-on (§7) with the app's
  name, store, provisioning credential and branding.
- An `AGENTS.md` section and a `DEPLOY.md` naming the exact `carlos`
  commands: create the two apps, enable instances on the tenant app,
  create the store, mint the `--role instance` service credential and set
  it **on the root app only**, set the root's public key on the tenant
  app, attach the catch-all (home) or the product hostname (hub).
- Tests that drive signup → provision → wait → handoff → signed-in page
  end to end against `provisiontest` and a directory `objrec` store, and
  a test that the instance serves standalone with no root configured.

**Standalone** is chosen explicitly (`<NAME>_STANDALONE=1`, set by
`make dev` and documented for self-hosting): the instance signs people in
itself (emailed link, its own roster, first person becomes Owner), at `/`
rather than under a base path. A tenant instance with neither a root
public key nor the standalone setting **refuses to start**, so a
misconfigured provisioned instance can never hand Owner to its first
visitor.

### 5.3 What a person experiences

- **Signup** at the root: email, account name, address
  (`acme` → `acme.example.com` or `example.com/acme`). A "getting your
  space ready" page that refreshes itself (no JavaScript needed), then
  they land in their account already signed in. Abuse guards follow Tito
  Go's signup (proof of work, rate limit, reserved and throwaway names);
  the builder takes Rastrillo's `pow` and `clientip` for these.
- **Sign-in** at the root, then a list of their accounts ("switcher").
  With one account they go straight in.
- **Members** are invited from the account's people page. The change
  goes to the instance first (§6.3).

### 5.4 Who signs in where (Paul, 2026-11-19)

- **Members** (the people who run an account: organisers, editors)
  reach an instance that has a root **only through the root**: the
  handoff for subdomains, the hub session for subdirectories. The
  instance offers members no sign-in of its own in that mode. So one
  place decides who gets in, suspending or resetting someone at the root
  actually works, and a person has one identity across their accounts.
  The cost: while the root is down members can't start a new session;
  existing instance sessions last up to an hour. This replaces UM model
  B's "each instance also accepts the ordinary sign-in on its own
  origin" for members.
- **Public visitors** (attendees, readers, commenters) are the
  account's own data and sign in **on the instance**, with the
  instance's own sign-in, kept apart from members. They have no root
  identity and never appear in `/fleet`. One identity for a visitor
  across accounts would be a different product decision and is not in
  this design.
- **Standalone** instances sign everyone in themselves (§5.2).

## 6. Core (rastrillo)

### 6.1 Already built

`objrec`, stateless `auth` records mode and signed-cookie sessions with
refresh (UM Plan 2), `provision` with `provisiontest` (Plan 4),
`basepath` (Plan 5), the `assertion` sign/verify core with golden
vectors.

### 6.2 The handoff (UM Plan 3, subdomains)

The enter / assert / consume flow of UM §6.2 around the existing
`assertion` package:

1. The root links to `https://<instance>/_carlos/enter`.
2. The instance sets a short browser-bound nonce cookie and redirects to
   the root's `/assert?aud=<instance origin>&nonce=<n>`.
3. The root checks the person owns or belongs to that account, signs an
   assertion (issuer root origin, audience instance origin, at most 120
   seconds) and returns it in an auto-submitting `POST` form with a
   visible button. Tokens never ride in URLs.
4. The instance verifies, compares the nonce, records the `jti` in its
   own SQLite (single use), and mints its own session with `sessions`.

The instance side is core (every home-model instance needs it); the root
side (`/assert`) is in the add-on. Subdirectory accounts don't use this:
they share the hub's origin and verify the hub's signed session cookie,
which is already built.

### 6.3 Roster delivery

Changes to who is in an account and at what role are made at the root
and delivered to the instance first, as UM §6.4 describes: a root-signed
`POST /_carlos/roster` (audience the instance, monotonic sequence
number). Only after the instance accepts does the root write its
directory copy. **The instance's roster is authoritative**; the root's
copy answers "which accounts might I have" for the switcher and grants
nothing. The same signed channel carries account state (`suspended`,
`active`); a suspended account refuses every sign-in and handoff.

**Roles are the app's, not a fixed set** (my call, from the builder
check). Patrix keeps its own roster with six roles and several owners,
not idear's three. So the receiver is a small core piece: it verifies
the signature, audience and sequence, then calls an interface the app
supplies (apply a member's role, remove a member, set account state).
idear ships an implementation of that interface in a new release; an app
with its own roster, like Patrix, implements it itself. The app declares
its role names and order once, and the add-on's people pages and
`/fleet` show those.

**When an instance has a root, members are edited only at the root**
(follows from §5.4). The instance's own people pages are off in that mode, so the
two copies cannot drift through local edits, and there is no daily
reconcile that reads every instance (which would wake them all).

### 6.4 The skill

- Root `SKILL.md`: the question (§5.1) and a table row.
- `skill/instances.md` (new): UM §3's models with who each suits,
  benefits and costs, the one-box limit for subdirectories, the leak
  profiles (UM §9), the safety invariants (UM §8), and "read the
  add-on's SKILL.md next" with the `go list -m` command.
- `skill/data.md`'s home/hub lines point to `skill/instances.md`.
- `docs/site/addons.md`: a directory entry for the add-on.
- The CARLOS skills chapter (UM Plans 8 and 9) names
  `rastrillo new --scaffold home|hub` as the first step of each
  recommended model. Per house practice it ships with a
  carlosframework.com sync.

## 7. The add-on: the root app

A new module, **`amadan.net/rastrillo/plaza`** (name my call: the
square every account opens onto; rename freely). It is a library the
scaffold's `internal/root/` wires, not a framework: the app passes its
store, credential, keys, branding and templates overrides.

### 7.1 Records (on `objrec`, UM §5)

Shared: `users/<uid>.json`, `emails/<eh>.json`, link records. Home:
`users/<uid>/instances/<label>.json`. Hub: `tenants/<slug>.json`
(authoritative), `members/<slug>/<uid>.json` (directory copy),
`users/<uid>/tenants/<slug>` (index). Added here:
`accounts/<slug>.json` state for suspension, written create-once then
`If-Match`, and person suspension on `users/<uid>.json`. Indexes are
repaired by a daily `carlos schedule` tick, which does not keep the
process awake.

The account's address is `<slug>.<root>` or `<root>/<slug>`, and the slug
is unique by its create-once record. With subdomains the platform label
**is** the slug: a catch-all serves `<label>.<parent>` and has no mapping
to a nicer name, so `provision.NewLabel`'s random suffix is not used
there.

### 7.2 What it serves

Signup, sign-in (via `auth` records mode), `/assert` (home), session
refresh (hub), the switcher, account people pages and roster delivery,
path-map republish after each hub signup with the fall-through check
(UM §7.2 step 6), and `/fleet`.

### 7.3 `/fleet`

Superadmins only; everyone else signed in gets a 404, anonymous visitors
go to sign-in. **Superadmins are named in an environment variable on the
root app** (`PLAZA_SUPERADMINS`, set with `carlos env set`), the way
Patrix names owners (my call: the platform's own operator channel, no
store credentials on a laptop, and changes are recorded by the platform;
Tito Go's CLI-written list needs store access from wherever the command
runs).

```
/fleet                                       Accounts 128 · People 412
Needs attention
  dublin-fringe   failed to start 09:12                  [Retry]
Newest accounts
  galway-arts     /galway-arts     3 people   created today
People   [search by email]
  ana@example.ie  2 accounts  owner, editor  last sign-in 2h ago
```

- **Accounts:** searchable list with address, member count, created date
  and state as the platform reports it (live, sleeping, failed). Retry a
  failed signup. Suspend and resume (§6.3; the hub also drops a suspended
  account's path prefix).
- **People:** search by email; a person's page lists their accounts and
  roles. Reset sign-in and suspend and resume, both by bumping the
  person's `epoch`. With subdirectories that ends their sessions within
  15 minutes (the hub cookie's life). With subdomains each instance
  issues its own session, so instance sessions last at most an hour and
  renew by a silent handoff through the root, which refuses a suspended
  or reset person; their sessions end within the hour (my call: nothing
  has to wake every instance the person belongs to).
- **Superadmins:** the list, read from the environment.
- **Delete is not a button.** UM §7.5 keeps deletion a person's
  `carlos instances delete`, so a bug in the centre can't destroy data,
  and the create-only credential of #185 couldn't delete anyway. The
  account page shows the exact command.

It uses Rastrillo's design system and components, and goes through copy
review like any user-facing text.

### 7.4 Custom domains (Paul, 2026-11-20)

**Ruling (Paul, 2026-11-20):** every account can use its own domain,
for example a Patrix site at `blog.asymco.com` instead of
`asymco.patrix.cc`. This reverses the non-goal this design first had.

**What the platform gives.** The tenant app's `--role instance`
credential may attach, read and detach hostnames
(`POST`, `GET` and `DELETE /api/cli/apps/{account}/{app}/domains` and
`domains/{host}`; platform `internal/console/routedecls.go:856-888`).
- Attach takes the route to serve, here the instance's platform host.
- On a deployment with `CONSOLE_DOMAIN_PROOF`, attach answers with a
  TXT challenge at `_carlos-challenge.<host>`. Repeating the attach
  verifies it; there is no separate verify door.
- The per-host `GET` reports the records to publish, DNS state, a
  derived `state` with its reason and check time, and certificate
  expiry. Certificates are automatic.
- An exact claim wins over the catch-all.

**The flow.** An account's Owner adds a hostname at the root. The root
shows the records the platform says to publish, checks on request and
on the daily tick, and shows the state: proving, waiting for DNS,
waiting for the certificate, live, or not working. The Owner can remove
it, and can always go back to the default address from the root, which
does not depend on the custom domain working.

**Ownership.** The root keeps one record per hostname, naming the
account, with an operation state (`reserving`, `attached`, `removing`)
and a generation. Every console call is reconciled against that record,
and an ambiguous answer is settled by reading the platform before
anything is undone. An unproved reservation lapses after seven days,
as the platform's does. The root refuses hostnames under the product's
parent, the instance domain and its own host.

**Ruling: the custom domain becomes the instance's origin** (my call,
flagged to Paul). The instance must answer handoff, sessions, emailed
links and public pages on one origin, and Rastrillo fixes an app's
origin at boot. The other option, handoff on the platform host and
public pages on the custom domain, still needs the instance to learn
the domain (for its links and canonical URLs). It also gives visitors
a sign-in on a different host from the pages they read.
- **Delivery.** The root sends the account's custom origin in the
  signed roster snapshot (§6.3) once the platform reports the domain
  `live`. Roster delivery always goes to the instance's **platform
  host**: the receiver's audience is bound to it, and it never
  redirects. A broken custom domain therefore cannot cut the root off.
- **The instance.** It stores the origin with the roster state, loads
  it before building its handlers at boot, and restarts gracefully on a
  change. Its other hosts redirect to the origin, except the platform
  paths: `/_carlos/*`, `/api/version` and `/healthz`.
- **Which origin is current.** It is the one in the last snapshot the
  instance **accepted**, not the one the root intends. `/assert` signs
  for it, and resolves a custom audience through the hostname record.
- **Sessions do not move.** Members enter the new origin through the
  root as usual, and visitors sign in again. Old-host links redirect
  with their path and query. No new credential is minted, and none is
  carried by a redirect the root makes.
- **When a live domain stops working,** the root shows it as not
  working (to the Owner, to members in the switcher, and on `/fleet`).
  It does not switch back on its own, because DNS can flap. The Owner's
  "use the default address" sends a snapshot without the origin, waits
  for it to be accepted, then detaches.

**Ships off until the platform closes two gaps** (`Config.CustomDomains`,
default false). The platform's TXT challenge is derived from the
platform account and the hostname (`domainpolicy.go:285`). Every plaza
account shares one platform account, so account B could reuse a record
account A left behind. And plaza cannot tell whether proof is on. An
operator may turn the feature on knowing both.

**Platform requests** (filed as `carlos/platform` discussions, not
worked around):
1. **Proof bound to the claim.** A fresh challenge per attach (per claim
   generation), so a leftover TXT record proves nothing for the next
   claimant.
2. **Proof is knowable.** Report whether `CONSOLE_DOMAIN_PROOF` is on,
   for example in the attach answer.
3. **Conditional attach and detach.** Re-attaching a hostname the same
   app holds moves it to the new route, and detach takes no expected
   claim. Both should accept an expected route or claim revision and
   refuse on a mismatch, so concurrent writers in one app cannot move or
   remove each other's claims.
4. **Domains for the instance role.** The `instance` role reaches the
   domains doors "for compatibility" (`routedecls.go:850-855`). Plaza
   depends on it, so it should be documented as part of that role, or
   of #185's create-only role.

## 8. Testing

- Core and idear pieces: unit tests for replay, wrong audience, nonce
  mismatch, expiry, stale roster sequence, suspended account.
- The add-on: tests against `provisiontest` and a directory store,
  including racing signups for one slug (exactly one wins) and a path-map
  fall-through.
- Scaffolds: `rastrillo new --scaffold home` and `hub` each produce an
  app whose own tests pass, end to end, plus the standalone test.
- The skill, by fresh-agent test (UM §11): before and after, an agent
  planning "an events CMS where organisers sign up" must pick one
  instance per account and run the right `rastrillo new`; an agent
  planning "a family's shared shopping list" must still pick one
  instance. Measured with skilleval.
- One live run on Carloku in the `demo` account (signup, provisioning,
  handoff, a second member, suspend, clean up), with the scoped approval
  AGENTS.md requires.

## 9. Order of work

1. Land `scaffold-shapes` (plan already reviewed).
2. Core handoff (UM Plan 3) and idear roster delivery, in parallel.
3. The add-on, subdomains first; subdirectories once #186 lands on the
   platform.
4. `--scaffold home`, then `--scaffold hub` (UM Plans 6 and 7), and
   `rastrillo new` requiring a shape.
5. The skill pieces (§6.4) and the fresh-agent test.
6. Seamus and Patrix adopt, each on its own branch in its own repo.

## 10. Left to the builder

- Package and type names inside core and the add-on.
- Template override points in the add-on, as long as an app can brand
  signup and `/fleet` without forking.
- How the platform reports instance state to `/fleet`; if the
  `--role instance` credential can't read it, file a platform request
  rather than probing instances.
