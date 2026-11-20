# plaza, subdomains: Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `amadan.net/rastrillo/plaza`, the root app add-on for
the subdomain model. It covers signup that provisions an instance at
`<slug>.<parent>`, sign-in, `/assert`, the account switcher, people
pages with signed roster delivery, account and person suspension,
`/fleet` and a daily tick. It adds three small pieces to Rastrillo core
first.

**Architecture:** plaza is a library that a root app mounts, not a
framework. It keeps no database. Its records are `objrec` records in
the root app's store. It signs people in with `auth` in records mode
and creates instances with `provision`. It signs handoff assertions and
roster snapshots with one root key, the key whose public half every
instance holds in `RASTRILLO_ROOT_KEYS`. Pages are rendered by the
app: plaza ships named templates built on Rastrillo's `ui` partials and
calls the app's `Render` function, so an app can brand or replace any
page without forking. Every multi-step change is ordered and
repairable. The authoritative account record is written first, and the
indexes are written after. A daily `carlos schedule` tick repairs
indexes and re-sends undelivered rosters.

**Tech Stack:** Go 1.26, stdlib `net/http` (`ServeMux` patterns),
`html/template`. Rastrillo: `objrec`, `auth`, `sessions`, `provision`,
`provision/provisiontest`, `assertion`, `roster`, `handoff`,
`handoff/handofftest`, `instance`, `pow`, `clientip`, `csrf`, `mail`,
`carlos`, `ui`.

**Spec:** `docs/superpowers/specs/2026-11-19-multi-instance-apps-design.md`
(§3 constraints, §5 what people experience, §6.2 handoff, §6.3 roster
delivery, §7 the add-on, §8 testing). The roadmap is
`docs/superpowers/plans/2026-11-19-multi-instance-apps-roadmap.md`,
row 3. Also read user-models §5 (records), §6.2 (handoff) and §7
(provisioning):
`git -C /home/paulca/amadan.net/carlos/pegamento show origin/design-user-models:docs/superpowers/specs/2026-09-27-user-models-design.md`.
The instance side is Plan 1:
`docs/superpowers/plans/2026-11-19-instance-handoff-and-roster.md`.

**Prior art:** Tito Go, `/home/paulca/github.com/tito/titogo`:
`internal/carlos/signup_http.go`, `internal/carlos/fleet_http.go`,
`internal/accountslug/accountslug.go`, `internal/throwaway/`,
`internal/auth`. Port its lessons, not its code. Tito Go keeps
SQLite and proxies requests; plaza does neither.

## What was checked before planning

- **Instance state is readable with the provisioning credential.**
  `GET /api/cli/apps/{account}/{app}/instances` admits roles
  `instance` and `deploy` (platform `internal/console/routedecls.go:613`
  at origin/main 5b4c2f64). Each record carries `health`, one word from
  `listquery.go:25`: `Setting up`, `Running`, `Serving`, `Asleep`,
  `Failed`, `Not responding`, `No readings`, `Can't tell right now`.
  The single `GET .../instances/{host}` has no `health`
  (`apiinstances.go:1457-1473`). The list takes `q` (a substring of the
  host), `limit` (1 to 499), and `cursor`. It answers
  `{instances, truncated, total, counts, next_cursor}`, at most 500
  records. **No platform request is needed.** `/fleet` reads state
  from this list and never probes an instance.
- **The console rate limit is shared.** Every instance route,
  creates included, draws from 60 requests a minute per token
  (`authcli.go:45,100`). So `/fleet` makes one list call per page view
  at most, and caches the answer for 30 seconds.
- **Catch-alls.** `carlos domains attach --app <tenant> '*.<parent>'`
  serves each instance at `<label>.<parent>` (platform `AGENTS.md`
  "Instance catch-alls"). An instance therefore has two hosts:
  - the **platform host** `<slug>.<instance domain>`, which `Create`
    names, which `provision.Ready` probes, and which the instance
    names in `Rastrillo-Host`;
  - the **public host** `<slug>.<parent>`, which people see. This is
    the handoff and roster audience, and where roster snapshots are
    posted.
- **Core gaps this plan fills (Tasks 1 and 2):**
  - `auth.User` is a closed struct. A `suspended` field that plaza
    wrote into `users/<uid>.json` would be dropped the next time
    `EndSessions` or the blind-mode strip rewrote the record.
  - `auth` has no exported lookup by address. Invites and `/fleet`
    search need one.
  - `sessions.SigningKey` hides its `*crypto.Keypair`, which
    `assertion.Sign` and `roster.Sign` take.
  - `provision` has no list call, and `provisiontest` has no list
    route, no health and no catch-all.

## Milestones that need Paul

1. **Mockups (Task 0)** are approved before Task 14 starts. Tasks 1
   to 13 do not wait for them.
2. **Core lands and is released (after Task 2).** Tasks 1 and 2 go on
   rastrillo branch `plaza-core`. plaza can build against a
   pseudo-version of main once that branch lands. A plaza release
   needs a Rastrillo release that contains it.
3. **The repo `amadan.net/rastrillo/plaza` is created (before
   Task 3).** Do not create it yourself.

## Global Constraints

- **Addon rules** (`docs/site/addons.md`): plaza depends on Rastrillo,
  never the reverse. It ships its own `SKILL.md`. It does not
  re-implement core: sessions, CSRF, sign-in, proof of work and
  provisioning come from Rastrillo.
- **Stateless centre:** plaza opens no SQLite and writes no local
  files. Every record is in `objrec`. The test is to delete the
  process's `--db` path and confirm nothing is lost.
- **Conditional writes:**
  - Create-once records use `objrec.CreateJSON`. `ErrExists` means
    someone else won.
  - Updates use `objrec.Mutate`. The callback has no side effects
    because it may run more than once.
  - The authoritative record is written first and the index second.
- **Safety invariants (UM §8):**
  - The provisioning credential lives on the root app only.
  - A signature proves who, never what. The instance's roster is
    authoritative, and plaza's copy grants nothing.
  - Tokens never ride in URLs. The assertion is an auto-submitting
    `POST` form with a visible button.
- **Subjects and owners (idear, 2026-11-20):** every subject plaza
  sends is lowercase, and every snapshot carries exactly one member at
  `Roles[0]` unless `Config.ManyOwners` (Task 10c).
- **One root key signs everything:** sessions, assertions and
  rosters. It is one `sessions.SigningKey`, read from
  `PLAZA_SIGNING_KEY` in `ParseSigningKey` form. Its `Public()` value
  is what the tenant app's `RASTRILLO_ROOT_KEYS` holds.
- **Assertions:**
  - lifetime 60 seconds;
  - issuer = the root origin;
  - audience = `https://<public host>`;
  - posted to `<audience>/_carlos/enter` as the form field
    `assertion`.
- **Roster snapshots:**
  - version 1, lifetime 120 seconds;
  - posted to `<audience>/_carlos/roster` as the raw token body;
  - sequence = `max(previous + 1, now.UnixMicro())`, so a sequence
    keeps rising even after a lost account record.
  - **One sequence, one content.** `deliver` always signs the
    `Sequence` and `Members` it read together in one `Get`. Two
    deliveries of one sequence therefore carry the same roster, which
    matters because the receiver accepts a repeated sequence. The
    clock fallback covers a deleted record, not a clock set
    backwards. plaza never deletes an account record, so that case
    needs a person deleting one by hand, and `SKILL.md` says so.
- **Slugs:**
  - 3 to 32 characters;
  - match `^[a-z0-9][a-z0-9-]{1,30}[a-z0-9]$`;
  - no `--` at positions 3 and 4, to refuse punycode labels;
  - lowercased and trimmed before checking.
  
  32 is the `--private-storage` environment-name limit that
  `provision.MaxSlug` cites. With subdomains the label **is** the
  slug, so `provision.NewLabel` is not used.
- **Environment (read by `plaza.FromEnv`):**
  - `PLAZA_SIGNING_KEY`;
  - `PLAZA_PEPPER` (base64url, at least 32 bytes);
  - `PLAZA_SUPERADMINS` (comma-separated addresses);
  - `PLAZA_PROVISION_TOKEN`, `PLAZA_CONSOLE`, `PLAZA_ACCOUNT` (sqid)
    and `PLAZA_TENANT_APP`;
  - `PLAZA_INSTANCE_DOMAIN` (the tenant app's instance domain);
  - `PLAZA_PARENT` (the catch-all's parent; empty means the public
    host is the platform host).
- **Root routes:**
  - `/signin`, `/auth/callback`, `/auth/verify`, `/signout`,
    `/_carlos/refresh`;
  - `/signup` and `/signup/{slug}`;
  - `/accounts` (the switcher);
  - `/{slug}/settings/{general,people,domain,danger}` (Task 13c moves
    Tasks 10, 10b and 13b's `/accounts/{slug}/...` routes here);
  - `/invitations/{slug}/{id}` (Task 10b);
  - `/assert`;
  - `/fleet...`;
  - `POST /_plaza/tick`.
  
  The app keeps `/` and everything else.
- **Code style:** `skill/code.md`. Names are spelled out (except
  `err`), lines stay within 80 columns, and comments explain why.
  `GOFLAGS=-mod=mod go run amadan.net/rastrillo/rastrillo/cmd/rastrillo budget code`
  stays clean.
- **No copy without review.** Every user-facing string goes through
  Luna (Task 14) before it ships. Tasks 3 to 13 use placeholder copy
  that is plain and accurate, and the tests assert page names and
  data, never sentences.
- **No em dashes as separators** in copy, docs or comments.
- **Tests:** targeted `go test ./... -run X` locally. The full suite
  and `-race` run on the amadan fleet.

## Review Focus

1. **Two people sign up for the same slug at once.** Exactly one
   account record wins. The loser sees "taken", their rate-limit
   budget is refunded, and no second instance is created. Pinned by
   `TestRacingSignupsOneWins` (Task 7).
2. **A person closes the tab mid-provisioning and comes back an hour
   later**, or two tabs refresh the waiting page together. The flow
   resumes from the record and never creates a second instance:
   every `Create` after the first passes `Resume`. Pinned by
   `TestWaitResumesAfterLostAnswer` and `TestTwoTabsCreateOnce`
   (Task 8).
3. **A roster change meets a busy instance (503) or a newer delivery
   (409).** The change is never lost. A 503 retries, then is left
   pending for the tick. A 409 is never read as success: plaza
   delivers its newest reservation, or reserves a fresh one once.
   Indexes follow only what the instance accepted.
   Pinned by `TestDeliverRetriesBusy`, `TestDeliverSupersededIsDone`
   and `TestTickRedeliversPending` (Tasks 6 and 13).
4. **A suspended person still holds an instance session.** They
   cannot sign in at the root. `/assert` refuses them, so the
   instance's hourly renewal ends the session. Pinned by
   `TestAssertRefusesSuspendedPerson` (Task 11) and auth's
   `TestSuspendedUserCannotSignIn` (Task 1).
5. **The platform's list is down, slow or rate-limited while a
   superadmin opens `/fleet`.** The page still renders from records,
   with state "can't tell", and nothing probes an instance. Pinned by
   `TestFleetWithoutPlatformList` (Task 12).

---

## Task 0: Mockups for Paul (no code)

Runs alongside Tasks 1 to 13. Task 14 does not start until Paul has
approved the mockups.

- [ ] **Step 1:** Draw static HTML mockups of these pages, built from
  Rastrillo's `ui` partials and design tokens, with real Lucide icons:
  - signup (email step and account step);
  - the "getting your space ready" page, and its failed state with a
    way out;
  - the switcher, with one account and with several (suspended and
    creating shown as unlinked rows);
  - the account people page, with an invite form and a pending
    delivery;
  - the `/assert` interstitial;
  - the refusal page;
  - the `/fleet` dashboard, account list, account page (with the
    delete command), people search, person page and superadmins.
  
  Follow `DESIGN.md`: dropdowns sit inline in a row, never alone above
  a title.
- [ ] **Step 1b (added 2026-11-20):** "Your own domain": add a
  hostname, the records to add with their states, connected, and
  removing it (Task 13b).
- [ ] **Step 2:** Serve them on Tailscale (`tailscale serve`, not
  localhost). Send the link to the managing session for Paul. Record
  his decisions in `docs/plaza-mockups.md` in the plaza repo, or on
  this branch if the repo does not exist yet.

---

## Task 1 (rastrillo, branch `plaza-core`): person suspension and lookups in `auth`

**Files:**
- Modify: `auth/users.go`, `auth/signin.go` (the records-mode mint
  path), `auth/auth.go` (records-mode epoch).
- Test: `auth/records_test.go`.
- Docs: `docs/site/reference/` page for auth (records mode section),
  `CHANGELOG.md`.

**Interfaces:**
- Produces:
  - `User.Suspended bool` with json `"suspended,omitempty"`.
  - `func (a *Auth) SetSuspended(ctx context.Context, uid string, suspended bool) error`.
    It uses `objrec.Mutate`. Suspending also bumps `Epoch`. It keeps
    the blind-mode email strip, as `EndSessions` does. An unknown uid
    returns `sessions.ErrNoUser`.
  - `func (a *Auth) UserByEmail(ctx context.Context, address string) (User, error)`
    reads the claim, then the user. When there is none it returns an
    error wrapping `objrec.ErrNotFound`. It never creates anything.
  - `func (a *Auth) EnsureUser(ctx context.Context, address string) (User, error)`
    is `users.ensure` with the same address normalisation sign-in
    uses. Invites need it.
  - `func (a *Auth) Users(ctx context.Context, cursor string, limit int) ([]User, string, error)`
    pages through `users/<uid>.json` in key order, skipping keys that
    are not users, as `stripAll` does.
  - `func (a *Auth) SessionCurrent(r *http.Request) (Identity, error)`
    is `SessionFrom` plus one read of `users/<uid>.json`. It refuses
    a session whose epoch is no longer the user's, or whose user is
    suspended. A signed cookie is otherwise trusted for up to 15
    minutes without a read. Anything that mints a longer-lived
    credential (plaza's `/assert`) or acts as a superadmin must
    therefore check this, so a reset or a suspension holds at once.
  - `Config.Now func() time.Time`, default `time.Now`, is the clock
    for links, signed sessions and refresh. Today the clock is a
    private field that only `auth`'s own tests can reach, and plaza's
    harness needs to move it.
  - All of these return `errNoRecords` on a database-mode `Auth`.

- [ ] **Step 1: Write the failing tests** in `auth/records_test.go`,
  on the records-mode harness `stateless_e2e_test.go` already uses:
  - `TestSuspendedUserCannotSignIn`: `EnsureUser("ana@example.test")`,
    then `SetSuspended(uid, true)`, then a full magic-link sign-in.
    Expect `Config.Refused` called with the address and no session
    cookie set.
  - `TestSuspendedUserCannotRefresh`: sign in, then `SetSuspended`,
    then advance the clock past the signed cookie's life and `GET`
    the refresh route. Expect the session cookie deleted, the same
    outcome as after `EndSessions`.
  - `TestResumedUserSignsIn`: suspend, resume, then sign in. Expect a
    session.
  - `TestSuspendKeepsOtherFields`: the user's `Email`, `EH` and
    `Created` are unchanged, and `Epoch` rose by exactly one.
  - `TestUserByEmailNeverCreates`: an unknown address gives
    `errors.Is(err, objrec.ErrNotFound)`, and `List("emails/")` is
    still empty.
  - `TestUsersPages`: with 3 users and limit 2, the first page has 2
    users and a cursor, and the second page has 1 user and `""`. A
    stray `users/<uid>/instances/x.json` key is skipped.
  - `TestSessionCurrentRefusesOldEpoch`: sign in, then
    `EndSessions`. Expect `SessionFrom` to still answer, and
    `SessionCurrent` to refuse. The same holds after
    `SetSuspended(true)`.
  - `TestConfigNowDrivesSessions`: with `Config.Now` set to a fake
    clock advanced by 16 minutes, the signed cookie is treated as
    expired and sent to refresh.
- [ ] **Step 2:** Run `GOFLAGS=-mod=mod go test ./auth -run 'Suspend|Resumed|UserByEmail|UsersPages|SessionCurrent|ConfigNow'`.
  Expect FAIL, since the methods are undefined.
- [ ] **Step 3: Implement.**
  - **Refusing sign-in:** where records mode mints after `ensure`, a
    suspended user takes the same path as an address `Authorize`
    refused.
  - **Refusing refresh:** `users.epoch` returns `sessions.ErrNoUser`
    for a suspended user, so refresh ends the session.
- [ ] **Step 4:** Run the same command. Expect PASS. Also run
  `go test ./auth -run 'Records|Stateless'` to confirm nothing else
  moved.
- [ ] **Step 5:** Commit with the subject
  `auth: suspend a person, look one up by address`.

## Task 2 (rastrillo, branch `plaza-core`): instance list, health, catch-all, keypair

**Files:**
- Modify: `provision/provision.go` (types), and create
  `provision/list.go`.
- Modify: `provision/provisiontest/*.go` (list route, health,
  catch-all).
- Modify: `sessions/signed.go` (`Keypair`).
- Test: `provision/list_test.go`,
  `provision/provisiontest/list_test.go`, `sessions/signed_test.go`.
- Docs: `docs/site/reference/provision.md`, `CHANGELOG.md`.

**Interfaces:**
- Produces in `sessions`:
  `func (k *SigningKey) Keypair() *crypto.Keypair`.
- Produces in `provision`:
  ```go
  type ListRequest struct {
  	Query  string // substring of the host, the console's q
  	Cursor string
  	Limit  int // 1..499; 0 lets the console choose
  }
  type Listed struct {
  	Instance
  	PublicHost string `json:"public_host"`
  	Health     string `json:"health"`
  }
  type ListPage struct {
  	Instances []Listed
  	Total     int
  	Next      string // "" on the last page
  	Truncated bool
  }
  func (c *Console) List(ctx context.Context, request ListRequest) (ListPage, error)

  const (
  	HealthSettingUp     = "Setting up"
  	HealthRunning       = "Running"
  	HealthServing       = "Serving"
  	HealthAsleep        = "Asleep"
  	HealthFailed        = "Failed"
  	HealthNotResponding = "Not responding"
  	HealthNoReadings    = "No readings"
  	HealthUnknown       = "Can't tell right now"
  )
  ```
  - `List` sends `GET /api/cli/apps/{account}/{app}/instances` with
    `q`, `cursor` and `limit`.
  - A 429, a 5xx or no answer is a `*RetryError`, with `After` taken
    from `Retry-After`.
  - A 403 is `ErrForbidden`.
  - It never retries by itself: the caller decides, and the rate
    budget is shared.
  - It reads the body through its own limit of 4 MiB, not the
    console helper's 64 KiB (`provision.go:257`), which a page of 499
    records can exceed. A body over the limit is an error, never a
    silently truncated page.
- Produces in `provisiontest`:
  - The list route, admitting roles `instance` and `deploy`. `health`
    is `Setting up` while an instance is still `Unready`, and
    `Running` after that.
  - `func (p *Platform) Sleep(host string)` makes the health `Asleep`.
    The instance still answers if requested, because the real edge
    wakes it.
  - `func (p *Platform) Fail(host string)` makes the health `Failed`
    and makes edge requests to that host answer 502.
  - `func (p *Platform) CatchAll(account, app, parent string)` makes
    the edge serve `<label>.<parent>` for each of the app's
    instances, where the label is the first label of the instance
    host. The app's handler is called with the **instance** host.
  - `ScriptList(replies ...Reply)` queues answers for list calls, as
    `Script` does for creates.
  - `func (p *Platform) MakeReady(host string)` ends an existing
    instance's pending answers at once. `Unready` only affects
    instances created after it is called.

- [ ] **Step 1: Write the failing tests:**
  - `TestKeypairSignsForPublic` (sessions): sign an
    `assertion.Claims` with `key.Keypair()`. Expect it to verify with
    `assertion.NewVerifier(issuer, audience, ParsePublicKeys(key.Public()))`.
  - `TestListReadsHealth`: create two instances, one `Unready(1)`.
    Expect `List` to return both, with health `Setting up` and
    `Running`. After `Sleep`, `Asleep`.
  - `TestListPages`: with 3 instances and `Limit: 2`, expect 2 records
    and `Next != ""`, then 1 record and `Next == ""`.
  - `TestListQuery`: `Query: "acme"` returns only hosts containing
    `acme`.
  - `TestListRetryable`: `ScriptList(Reply{Status: 429, RetryAfter: "7"})`
    gives a `*RetryError` with `After == 7*time.Second`. A 503 is also
    a `*RetryError`.
  - `TestListForbiddenForOtherRoles`: an `operate` credential gives
    `ErrForbidden`.
  - `TestListReadsLargePage`: 499 instances with long hosts, a body
    over 64 KiB, all decode. A body over 4 MiB is an error.
  - `TestMakeReady`: an instance created after `Unready(1000)` reads
    ready after `MakeReady(host)`.
  - `TestCatchAllServesLabel`: after
    `CatchAll("bdf", "tenant", "example.test")`,
    `GET https://acme.example.test/api/version` answers 200 with
    `Rastrillo-Host: acme.tenant.oncarlos.test`. A label with no
    instance gives 404.
- [ ] **Step 2:** Run
  `GOFLAGS=-mod=mod go test ./sessions ./provision/... -run 'Keypair|List|CatchAll|MakeReady'`.
  Expect FAIL.
- [ ] **Step 3: Implement.**
  - In the provisiontest package comment's citation table, cite
    platform origin/main 5b4c2f64 for each new rule:
    - `routedecls.go:613` for the role gate;
    - `apiinstances.go:1252-1400` for list, `q`, `limit` and cursor;
    - `listquery.go:25` for the words;
    - `AGENTS.md` "Instance catch-alls" for the alias.
  - Bump `provisiontest`'s cited commit.
- [ ] **Step 4:** Run the same command. Expect PASS.
- [ ] **Step 5:** Commit with the subject
  `provision: list instances with health; test catch-alls`.
- [ ] **Step 6:**
  - Push `plaza-core`, describe it, and wait for green CI.
  - Ask the managing session for Paul's go to land it and cut a
    release.
  - Record the release (or the pseudo-version of the merge) for
    Task 3's `go.mod`.

## Task 2b (rastrillo, branch `plaza-domains-core`): the instance's origin rides the roster; domains in provision

Spec §7.4 (custom domains, Paul 2026-11-20). Branch from `plaza-core`
until that lands, then rebase onto main.

**Files:**
- Modify: `roster/token.go` (Snapshot), `roster/receiver.go`, a new
  `roster/migrations/` step, `handoff/handofftest/handofftest.go`.
- Create: `provision/domains.go`; extend `provision/provisiontest`.
- Test: `roster/origin_test.go`, `provision/domains_test.go`,
  `provision/provisiontest/domains_test.go`.
- Docs: the roster and provision reference pages, `CHANGELOG.md`.

**Interfaces:**
- `roster.Snapshot` gains `Origin string` (`json:"origin,omitempty"`):
  the account's custom origin, `https://` and a host with no path, or
  empty. `Sign` and `Verify` refuse anything else. Check how strictly
  `Verify` decodes: if it refuses unknown fields, say so in the
  CHANGELOG (an older receiver refuses a snapshot that sets it).
- The receiver stores the origin with the sequence it applied (one new
  column on its state row). New API:
  ```go
  func (receiver *Receiver) Origin(ctx context.Context) (string, error)
  func StoredOrigin(ctx context.Context, db *sql.DB) (string, error) // for boot, before handlers exist
  // Config gains: OnOrigin func(previous, next string)
  ```
  `OnOrigin` runs after a snapshot that changes the origin has been
  applied and answered (an app exits so the platform restarts it on
  the new origin). A snapshot that repeats the origin does not call it.
- `handofftest.Root` gains `RosterOrigin(t, audience, sequence, state,
  origin, members...)`.
- **Wiring for real instances** (the Plan 4 scaffold uses it):
  `func instance.ServingOrigin(ctx context.Context, db *sql.DB,
  fallback string) (string, error)` returns the stored origin, else
  `fallback` (the public host); call it after migrations and before
  building handlers. `func instance.RedirectToOrigin(origin string,
  next http.Handler) http.Handler` sends every other host to `origin`
  with path and query, except `/_carlos/*`, `/api/version` and
  `/healthz`. The roster verifier's audience is the platform host
  (`https://` + `carlos.Host()`), never the serving origin. `OnOrigin`
  in the scaffold shuts the server down gracefully (draining requests)
  so the platform restarts it. **Upgrade order:** instances run this
  core before any root sends `Origin`.
- `provision`:
  ```go
  // DNSRecord is one record the platform says to publish, as the
  // per-host GET's `domain.records` carries it (TTL, alternatives and
  // whether it is required included).
  type DNSRecord struct {
  	Type, Name, Value string
  	TTL               int
  	Required          bool
  	Alternatives      []DNSRecord
  }
  type Attached struct{ Host string; Verified bool; Verify *DNSRecord }
  type Domain struct {
  	Host, Route string
  	Verified    bool
  	State       string    // the platform's derived state ("live", ...)
  	Reason      string
  	CheckedAt   time.Time
  	DNS         string
  	CNAMETarget string
  	Guidance    string
  	Records     []DNSRecord
  	CertExpiry  time.Time // zero until a certificate exists
  }
  func (c *Console) AttachDomain(ctx context.Context, host, route string) (Attached, error)
  func (c *Console) Domain(ctx context.Context, host string) (Domain, error)
  func (c *Console) DetachDomain(ctx context.Context, host string) error
  ```
  Read the field names and states from platform origin/main
  (`internal/console/apidomains.go`: attachAnswer, the per-host GET)
  and cite them. Errors follow `List`'s rules (409 → `ErrConflict`,
  429/5xx/no answer → `*RetryError`).
- `provisiontest`: the three domain doors for roles `instance` and
  `publish`, with the 409 for a name another app holds and the
  same-app re-attach that moves the claim (as the platform does);
  `DomainProof(on bool)` (attach answers a TXT challenge, a repeat
  attach after `ProveDomain(host)` verifies it); `DNSReady(host)` and
  `CertLive(host, expiry)` for GET; the edge serves an attached,
  verified host as the route it names.

- [ ] **Step 1: Write the failing tests:** `TestSnapshotCarriesOrigin`,
  `TestSnapshotRefusesBadOrigin` (a path, `http://`, userinfo),
  `TestReceiverStoresOriginAndCallsOnOrigin` (called once on change,
  not on repeat), `TestStoredOriginAtBoot`, `TestAttachDomainWithProof`,
  `TestDomainReportsDNSAndCert` (the full GET envelope: state, reason,
  checked_at, records, guidance), `TestDetachDomain`,
  `TestAttachTakenElsewhereConflicts`, `TestServingOriginFallsBack`,
  `TestRedirectToOriginSparesPlatformPaths`.
- [ ] **Step 2:** Run them. Expect FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** Run them, then `go test ./roster ./handoff/... ./provision/... ./internal/docsite` and `budget size`/`budget code`. Expect PASS.
- [ ] **Step 5:** Commit with the subject
  `roster carries the custom origin; provision attaches domains`.

---

## Task 3: module, `Config`, records and slugs

**Prerequisite:** the repo exists (Milestone 3).

**Files:**
- Create in the plaza repo:
  - `go.mod` (`module amadan.net/rastrillo/plaza`, go 1.26, requiring
    Rastrillo at Task 2's version);
  - `Makefile` and `.amadan/` (copy aviso v0.1.0's shape: `ci: vet
    fmt-check test race`);
  - `.gitignore`;
  - `plaza.go`, `records.go`, `slug.go`, `env.go`.
- Test: `records_test.go`, `slug_test.go`, `env_test.go`.

**Interfaces:**
- Produces in `plaza.go`:
  ```go
  type Config struct {
  	Origin         string // root origin, https://example.com
  	Parent         string // "example.com"; "" = public host is the platform host
  	InstanceDomain string // the tenant app's instance domain
  	TenantApp      string // for the delete command /fleet shows
  	Region         string // optional create region
  	Store          objrec.Store
  	Console        *provision.Console
  	HTTP           *http.Client // probes and deliveries; nil = default with 10 s timeout
  	Key            *sessions.SigningKey
  	Pepper         []byte
  	InstanceKey    string // auth's sealing key
  	Proof          *pow.Guard // built by the app with pow.RecordNonces(Store); shared by sign-in and signup
  	Mailer         mail.Sender
  	Roles          []string // the app's roles, highest first; Roles[0] is the signer-up's
  	ManageFrom     string   // lowest role that may change people; default Roles[0]
  	Superadmins    []string // lowercased addresses
  	Reserved       []string // slugs refused on top of DefaultReserved
  	DailyCreates   int      // fleet-wide signups per UTC day; default 50
  	CreateTimeout  time.Duration // one waiting-page create call; default 25 s
  	Render         func(w http.ResponseWriter, r *http.Request, page string, status int, data any)
  	AssetPath      func(name string) string // where the app serves Assets(); nil = "/static/plaza/"+name
  	Brand          Brand
  	SigninSubject  string                   // auth.Config.Subject; "" = auth's default
  	SigninBody     func(link string) string // auth.Config.Body; nil = auth's default
  	InviteMail     func(account Account, signinURL string) (subject, body string) // nil = plaza's reviewed copy
  	TrustedProxyHops *int
  	Now            func() time.Time
  	Logger         *slog.Logger
  }
  type Brand struct{ Name string } // shown in titles and mail; Origin rides in Page
  type Plaza struct{ /* unexported */ }
  func New(config Config) (*Plaza, error)
  ```
  `New` refuses:
  - a non-HTTPS `Origin`;
  - an empty `InstanceDomain`;
  - a nil `Store`, `Console`, `Key`, `Proof` or `Render`;
  - a `Pepper` under 32 bytes;
  - empty or duplicate `Roles`;
  - a `ManageFrom` that is not in `Roles`.
- Produces in `records.go`:
  ```go
  type Phase string // "creating", "live", "failed"
  type Member struct {
  	UID   string `json:"uid"`
  	Email string `json:"email"`
  	Role  string `json:"role"`
  }
  type Account struct {
  	Slug, Name       string
  	Host             string    // platform host
  	PublicHost       string
  	Owner            string    // uid of the signer-up
  	Phase            Phase
  	Suspended        bool
  	Error            string    // last problem, for /fleet; never shown raw to people
  	// Creation (Task 8). Only the holder of an unexpired lease calls
  	// Create, and its result is written only while it still holds it.
  	Lease            string    // random id of the caller creating now; "" = none
  	LeaseUntil       time.Time
  	MayHaveLanded    bool      // an attempt of ours ended without a definite answer
  	Created          bool      // the platform answered 201 or 202, or Resume found our host
  	HostTaken        bool      // the first create met another creation's instance; final
  	Ready            bool      // provision.Ready answered true once
  	Started, CreatedAt time.Time
  	// The roster (Task 6).
  	Members          []Member  // desired: what the next snapshot carries
  	Sequence         int64     // last reserved
  	Accepted         []Member  // exactly the snapshot the instance last accepted
  	Delivered        int64     // that snapshot's sequence
  	DeliveryError    string
  }
  type Membership struct{ Slug, Name, PublicHost, Role string } // users/<uid>/instances/<slug>.json
  func accountKey(slug string) string     // "accounts/<slug>.json"
  func membershipKey(uid, slug string) string
  func (plaza *Plaza) Account(ctx context.Context, slug string) (Account, objrec.Version, error)
  func (plaza *Plaza) Memberships(ctx context.Context, uid string) ([]Membership, error)
  ```
  Use exact json tags in snake_case (`public_host`, `seq`,
  `delivered`, and so on). Fix them in this task; later tasks read
  them.
- Produces in `slug.go`:
  - `func NormalizeSlug(raw string) string` lowercases and trims.
  - `func ValidSlug(slug string) error` returns `ErrSlugShape` or
    `ErrSlugReserved`.
  - `var DefaultReserved []string` holds:
    - Tito Go's `reserved` list (`accountslug.go:86-96`);
    - plus `fleet`, `signup`, `signin`, `assert`, `accounts`, `auth`,
      `static`, `api` and `www`.
    
    Do not copy its 200 marketing words: an app adds its own through
    `Config.Reserved`.
- Produces in `env.go`:
  `func FromEnv(getenv func(string) string) (Settings, error)`.
  `Settings` holds the parsed key, pepper, superadmins, console
  `provision.Config` fields, instance domain and parent. Every
  missing required variable is named in one error.

- [ ] **Step 1: Write the failing tests:**
  - `TestValidSlug` (table):
    - accepted: `acme`, `a1b`, `dublin-fringe`, 32 characters;
    - `ErrSlugShape`: `ab`, 33 characters, `-ab`, `ab-`, `a_b`,
      `xn--abc`, `ACME` (before normalising);
    - `ErrSlugReserved`: `fleet`, `www`.
  - `TestNormalizeSlug`: `"  Acme "` gives `"acme"`.
  - `TestNewRefusesBadConfig` (table, one row per refusal above).
  - `TestAccountRoundTrip`: create an account with `objrec.CreateJSON`
    on `objrec.Dir(t.TempDir())`, then read it back with
    `plaza.Account`. Expect it equal. A missing slug gives
    `objrec.ErrNotFound`.
  - `TestFromEnv`: a full environment parses. With an empty
    environment, the error names all seven required variables.
    `PLAZA_SUPERADMINS=" Ana@x.test, bo@y.test "` gives
    `[ana@x.test bo@y.test]`.
- [ ] **Step 2:** Run `go test ./... -run 'Slug|NewRefuses|AccountRoundTrip|FromEnv'`.
  Expect FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** Run it again. Expect PASS. Also run `make vet fmt-check`.
- [ ] **Step 5:** Commit with the subject
  `plaza: config, records and slug rules`.

## Task 4: `plazatest`, the harness every later test uses

**Files:**
- Create: `plazatest/plazatest.go`, `plazatest/tenant.go`,
  `plazatest/browser.go`.
- Test: `plazatest/plazatest_test.go`.

**Interfaces:**
- Consumes: `provisiontest`, `handoff`, `roster`, `sessions`,
  `dbtest`, and Task 3's `Config`.
- Produces:
  ```go
  type Harness struct {
  	Platform *provisiontest.Platform
  	Store    *objrec.DirStore
  	Mail     *Mailbox         // captures every message
  	Root     *plaza.Plaza
  	Pages    *Pages           // records every Render call: page, status, data
  	Clock    *Clock           // shared by root and every fake instance
  }
  func New(t testing.TB, options ...Option) *Harness
  func (h *Harness) Browser() *Browser // cookie jar over root and instance hosts
  func (h *Harness) Tenant(publicHost string) *Tenant
  type Tenant struct{ /* the fake instance at one host */ }
  func (t *Tenant) Members() []roster.Member
  func (t *Tenant) Suspended() bool
  func (t *Tenant) Busy(n int)  // the next n roster posts answer 503 with Retry-After: 1
  func (b *Browser) Get(t testing.TB, url string) *http.Response
  func (b *Browser) Post(t testing.TB, url string, form url.Values) *http.Response
  func (b *Browser) Follow(t testing.TB, response *http.Response) *http.Response // redirects and auto-submit forms
  func (b *Browser) SignIn(t testing.TB, address string) // magic link via Mailbox
  ```
- **Setup it performs:**
  - `Platform.Enable("bdf", "tenant", "tenant.oncarlos.test")`;
  - `CatchAll("bdf", "tenant", "example.test")`;
  - a credential with role `instance`;
  - root origin `https://example.test`;
  - roles `owner, admin, member`;
  - `Config.Proof`, a `pow.Guard` with `pow.RecordNonces(Store)`,
    difficulty 1 and `MinAge` 1 ns. The browser renders the recorded
    `pow.Form` fields to HTML and fills them with `powtest.Fill`.
- **The fake tenant** enforces what a real rooted instance enforces,
  or the tests would prove nothing. Per host it serves, `Serve` builds:
  - `provisiontest.Tenant("")` for `/api/version`;
  - `handoff` at `/_carlos/enter`, with `Suspended: receiver.Suspended`
    and `Now` from the shared clock;
  - `roster.Receiver` at `/_carlos/roster`, over an in-memory
    `roster.Store` with a `dbtest` SQLite per host;
  - `GET /whoami`, wrapped in `sessions.Middleware` and
    `handoff.Renew`. It answers the session subject only when that
    subject is in the tenant's roster and the tenant is not suspended.
    Otherwise it answers 403, or 401 with no session.
- **The browser's transport:** requests to the root origin go to the
  root's handler. Every other host goes to `Platform`.

- [ ] **Step 1: Write the failing test:**
  `TestHarnessTenantAcceptsRoster`. Create an instance through the
  platform and post a roster that `handofftest`-style code signs with
  the root key. Expect 204, and `Tenant(...).Members()` to have one
  member.
  `TestHarnessTenantChecksRoster`: a handoff for a subject not in
  the roster gives `/whoami` 403. After a suspended roster, a member
  gets 403 too.
- [ ] **Step 2:** Run `go test ./plazatest -run Harness`. Expect FAIL.
- [ ] **Step 3:** Implement. Keep the harness free of plaza logic: it
  only plays the platform, the instance and the browser.
- [ ] **Step 4:** Run it again. Expect PASS.
- [ ] **Step 5:** Commit with the subject
  `plazatest: fake platform, instances and browser`.

## Task 5: sign-in on `auth` records mode, `Mount` and the render seam

**Files:**
- Create: `signin.go`, `pages.go`, `mount.go`.
- Test: `signin_test.go`.

**Interfaces:**
- Consumes: Task 1's `auth` methods and Task 4's harness.
- Produces:
  - `func (plaza *Plaza) Mount(mux *http.ServeMux)` registers every
    root route in Global Constraints. Each is wrapped in plaza's
    session middleware and `csrf.Protect(Origin, csrf.Exempt("/_plaza/tick"))`.
  - `type Page struct{ Brand Brand; Origin string; Person *Person; Superadmin bool }`
    is embedded in every page's data.
  - `type Person struct{ UID, Email string }`.
  - `func (plaza *Plaza) person(r *http.Request) (Person, bool)` reads
    the session.
  - **Naming:** a page's name is a string constant `Page<X>`
    (`PageSignin = "plaza/signin"`), and its data is the struct
    `<X>Data` (`SigninData`). Each later task adds its own pair.
  - `plaza.auth` is built here: `auth.New` with
    - `Records`, `Signed{Name: "plaza", Key: Key}` and `Pepper`;
    - `SigninScreen: true`;
    - `SignedInPath: "/accounts"`;
    - `Proof: Config.Proof`, shared with signup;
    - `KeymailServers: []string{}` unless the app sets them;
    - `RenderConfirm`, which renders `PageSignin`;
    - `Now: Config.Now`, so one clock drives the root.

- [ ] **Step 1: Write the failing tests:**
  - `TestSignInLandsOnAccounts`: `Browser.SignIn("ana@example.test")`
    ends at `/accounts` with a session.
  - `TestRefreshKeepsSession`: advance the clock by 16 minutes, then
    `GET /accounts`. The browser follows `/_carlos/refresh` and gets
    200.
  - `TestSignoutEndsSession`.
  - `TestPostWithoutOriginRefused`: a cross-site `POST /signup` gives
    403 from csrf.
  - `TestNoLocalFiles`: run a sign-in. Expect the process wrote
    nothing outside `Store`'s directory (check that `t.TempDir()`
    siblings stay empty).
- [ ] **Step 2:** Run `go test ./ -run 'SignIn|Refresh|Signout|WithoutOrigin|NoLocalFiles'`.
  Expect FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** Run it again. Expect PASS.
- [ ] **Step 5:** Commit with the subject
  `plaza: sign-in on records, routes and page seam`.

## Task 6: roster delivery

**Files:**
- Create: `deliver.go`.
- Test: `deliver_test.go`.

**Interfaces:**
- Consumes: `roster.Sign`, `Key.Keypair()`, and Task 3's `Account`.
- Produces:
  ```go
  // change runs inside objrec.Mutate: no side effects.
  func (plaza *Plaza) changeRoster(ctx context.Context, slug string,
  	change func(*Account) error) (Changed, error)
  type Delivery int // Delivered, Pending, Refused
  type Changed struct {
  	Account  Account
  	Delivery Delivery
  }
  func (plaza *Plaza) deliver(ctx context.Context, slug string) (Delivery, error)
  func (plaza *Plaza) syncMemberships(ctx context.Context, slug string) error
  ```
- **`changeRoster`** applies `change`, then reserves the next sequence
  (`max(Sequence + 1, now.UnixMicro())`) in the same `Mutate`, then
  calls `deliver`.
- **`deliver`** repeats the steps below at most 3 times:
  1. Read the account in one `Get`. If `Delivered >= Sequence`, it is
     done. Otherwise keep `seq = Sequence` and `members = Members`
     from that same read.
  2. Sign `members` as roster members, with `Subject = UID`. Use
     `State` `suspended` when `Suspended` is true, otherwise
     `active`. Use `Sequence = seq`.
  3. POST to `https://<PublicHost>/_carlos/roster`. What it does next
     depends on the answer:

     | Answer | What `deliver` does |
     |---|---|
     | 204 | `Mutate`: if `seq > Delivered`, set `Delivered = seq` and `Accepted = members`, and clear `DeliveryError`. Then `syncMemberships`. If `Sequence` has moved past `seq` in the meantime, go round again for the newer one. |
     | 409 | The instance holds a newer sequence. A 409 says nothing about whose, so it is never read as success. Read again. If `Sequence > seq`, go round again and deliver the newer reservation. Otherwise the instance holds a sequence plaza never issued: reserve a fresh one (`Mutate`) and go round once more. A 409 on that is `Refused`, with `DeliveryError` set. |
     | 503 | Wait for `Retry-After` (capped at 2 s), at most 3 attempts. Then return `Pending`, with `DeliveryError` set. |
     | No answer or another 5xx | `Pending`. |
     | 403 or 422 | `Refused`, with the status in `DeliveryError`. |

     The delivery client never follows redirects.
- **`syncMemberships`** is the only writer of membership records apart
  from signup's owner record. It reads the account, then writes a
  membership for each of `Accepted` and deletes the memberships of
  people not in `Accepted`. It derives everything from the `Accepted`
  it reads just before writing, never from a snapshot an earlier step
  held, so a slow writer can only write the latest accepted state.
  The tick calls it too, and repairs any interleaving that remains.
- **`Accepted` is the only "who is in" a page trusts:** the switcher,
  memberships and `/fleet`'s member counts all read it. `Members` is
  what is on its way, and the people page shows the difference as
  pending.

- [ ] **Step 1: Write the failing tests** (harness tenant):
  - `TestDeliverAddsMember`: an account with an owner gets a member
    added. Expect the tenant's members to be both, `Delivered ==
    Sequence`, and `users/<uid>/instances/<slug>.json` to exist.
  - `TestDeliverRetriesBusy`: with `Busy(2)`, expect `Delivered`
    after the 3rd attempt. With `Busy(5)`, expect `Pending`, the
    record's `Delivered < Sequence`, and no membership written.
  - `TestDeliverSupersededIsDone`: reserve two changes with
    `Busy(10)` so neither lands, then `Busy(0)`. Deliver the second
    directly, then the first, which meets a 409. Expect `Delivered`
    with `Delivered == Sequence`, and the tenant and `Accepted` both
    holding both changes.
  - `TestDeliverForeignSequence`: post a snapshot with sequence
    `now + 10^12` to the tenant directly, then change the roster.
    Expect one fresh reservation, then `Refused` with `DeliveryError`
    set. `Delivered` and `Accepted` are unchanged.
  - `TestPendingRemovalKeepsMembership`: with `Busy(10)`, remove a
    member. Their membership stays, and `Accepted` still lists them.
    After `Busy(0)` and `deliver`, the membership is gone.
  - `TestAcceptedNotDesired`: deliver change 1, then reserve change 2
    while `Busy(10)`. `syncMemberships` writes change 1's people
    only.
  - `TestSequenceSurvivesLostRecord`: deliver, then delete the
    account record. Recreate it with a different member, `Sequence 0`
    and `Delivered 0`, and call `changeRoster` with a no-op change.
    The reservation comes from the clock, so the tenant accepts it
    and holds the new member.
  - `TestSequenceRisesOnFrozenClock`: with the clock frozen, three
    reservations give three rising sequences.
- [ ] **Step 2:** Run `go test ./ -run 'Deliver|Sequence|PendingRemoval|AcceptedNotDesired'`.
  Expect FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** Run it again. Expect PASS.
- [ ] **Step 5:** Commit with the subject
  `plaza: deliver signed rosters before writing indexes`.

## Task 7: signup with abuse guards

**Files:**
- Create: `signup.go`, `guard.go`, `throwaway.go`, `throwaway.txt`.
- Test: `signup_test.go`, `guard_test.go`.

**Interfaces:**
- Consumes: the `pow.Guard` from Task 5, `clientip.From`,
  `auth.User`, and Task 3's records.
- Produces:
  - `GET /signup`:
    - anonymous: renders `PageSignupEmail`, which posts to auth's
      `/signin` with return to `/signup`;
    - signed in: renders `PageSignupAccount` with
      `SignupAccountData{Page, Form{Name, Slug}, Challenge pow.Form, Problem string}`.
  - `POST /signup` runs its guards cheapest first, as Tito Go does:
    1. session;
    2. `pow` admission (scope `"plaza/signup"`);
    3. slug shape and reserved;
    4. the throwaway domain of the person's address;
    5. the per-IP limiter (20 per 15 minutes) and the per-person
       limiter (5 per 15 minutes);
    6. the daily cap: list `created/<yyyy-mm-dd UTC>/`, and refuse at
       `DailyCreates` keys.
    
    Then it creates `accounts/<slug>.json` with:
    - `Phase creating`;
    - the owner as the only member of `Members`;
    - `Sequence` reserved, and `Delivered 0`.
    
    `ErrExists` means "taken": refund both limiter budgets and write
    nothing else. Only the winner then writes the marker
    `created/<day>/<slug>` (create-once) and the owner's membership,
    and redirects 303 to `/signup/{slug}`.
    
    **The cap is a brake, not a quota.** Signups in flight at the same
    moment can each pass the listing, so the cap can be overshot by at
    most the number of concurrent signups, which the per-IP and
    per-person limits already bound. A duplicate slug never counts,
    because only the winner writes a marker. A crash between the
    account and its marker under-counts by one. The tick deletes
    markers older than 2 days.
  - `func isThrowaway(address string) bool` matches the domain or any
    parent domain against the embedded list.
  - `type limiter` is a fixed-window counter in memory, keyed on
    strings. It is in memory because a root under attack is kept
    awake by that attack, so hibernation cannot reset it.

- [ ] **Step 1: Write the failing tests:**
  - `TestSignupCreatesAccount`: sign in, then POST name "Acme" and
    slug "acme". Expect a 303 to `/signup/acme`. The record has
    `Phase creating`, `Owner == uid` and `Members == [{uid, email, owner}]`.
    The owner's membership exists. No console call has been made yet:
    `Platform.Calls()` is empty.
  - `TestRacingSignupsOneWins`: 10 goroutines, 10 signed-in people,
    one slug. Expect exactly one 303. The other nine re-render
    `PageSignupAccount` with `Problem == "taken"`. `List("accounts/")`
    has one key, and `Platform.Instances` is empty.
  - `TestTakenRefundsBudget`: 5 taken attempts by one person, then a
    free slug, succeeds.
  - `TestSignupGuards` (table):
    - a reserved slug gives `taken` (the same answer as a real
      duplicate, as Tito Go learned);
    - a bad shape gives `shape`;
    - `x@mailinator.com` gives `throwaway`;
    - no proof gives `proof`;
    - the 21st attempt from one IP gives `rate`;
    - the 6th from one person gives `rate`;
    - the daily cap reached gives `closed`.
  - `TestDuplicateNeverCounts`: with `DailyCreates: 2`, sign up `a`,
    then make 5 attempts at `a`, then sign up `b`. Expect `b`
    accepted, and `created/<day>/` to hold exactly `a` and `b`.
  - `TestThrowawayMatchesParents`: `a@sub.mailinator.com` is true,
    and `a@example.com` is false.
- [ ] **Step 2:** Run `go test ./ -run 'Signup|Throwaway'`. Expect FAIL.
- [ ] **Step 3: Implement.** Copy `throwaway.txt` verbatim from Tito
  Go's `internal/throwaway/domains.txt`. Give it a header naming its
  upstream (github.com/disposable-email-domains, commit `fa96007`)
  and that project's licence. Check the licence permits the copy; if
  it does not, stop and report.
- [ ] **Step 4:** Run it again. Expect PASS.
- [ ] **Step 5:** Commit with the subject
  `plaza: signup with proof, limits and name rules`.

## Task 8: provision, wait, first roster, `/assert` and the first handoff

**Files:**
- Create: `wait.go`, `assert.go`.
- Test: `wait_test.go`, `assert_test.go`, `flow_test.go`.

**Interfaces:**
- Consumes: `provision.Console.Create`, `provision.Ready`,
  `assertion.Sign`, `deliver` (Task 6), and `auth.SessionCurrent`
  (Task 1).
- Produces:
  - `GET /signup/{slug}` is the owner's waiting page. Anyone else gets
    a 404. Each request reads the account, takes **one** of the steps
    below, then renders or redirects. Every write is a `Mutate`, and a
    write that finds the phase changed under it (a retry, a tick, a
    `/fleet` action) re-reads and renders instead of overwriting.
    1. **Deadline.** `creating` and `now - Started > 15m`: set
       `Phase failed`, `Error "not ready after 15 minutes"`. This
       covers every sub-step: the create, the readiness wait and the
       first roster.
    2. **Create** (`!Created`, `!HostTaken`):
       - Take the lease, unless another caller holds an unexpired one;
         in that case, render the waiting page. Taking it sets `Lease`
         to a fresh random id and `LeaseUntil = now + 60s`, records
         `resume := MayHaveLanded`, then sets `MayHaveLanded = true`.
       - Call `Create(ctx, CreateRequest{Host, Region, Resume: resume})`,
         with a `CreateTimeout` context (default 25 s, inside the
         60-second lease).
       - Write the outcome only if `Lease` is still this caller's id,
         and clear the lease in the same write. A late result whose
         lease has passed to someone else is dropped. Because
         `MayHaveLanded` stayed true, the next holder reconciles with
         `Resume`.

       | Outcome | Result |
       |---|---|
       | nil error (a 201, `Accepted` or `Landed`) | `Created = true` |
       | `ErrHostTaken` | `HostTaken = true`, `Phase failed`, `Error "address taken"`. This is final: retry refuses it |
       | `*RetryError`, or the context's own error | Stay creating, with `Error` set. `MayHaveLanded` stays true |
       | Any other `*APIError` (4xx) | `Phase failed`, `MayHaveLanded = false` |

       **Why `Resume` only follows our own unknown outcome.** `Resume`
       reads any existing host as `Landed` (`provision/create.go:123`).
       Passing it on a first attempt, or after `ErrHostTaken`, would
       adopt someone else's instance and send it our owner's roster.
       After a create of ours with no definite answer, the host is
       ours: only lease holders create, the slug is create-once, and
       the catch-all reserves the label. A person creating that exact
       label by hand in that window is the one gap, and `SKILL.md`
       names it.
    3. **Ready** (`Created`, `!Ready`): one
       `provision.Ready(ctx, HTTP, Host)`. If it is true, set
       `Ready = true`.
    4. **First roster** (`Ready`, `Delivered < Sequence`): `deliver`.
       - `Delivered` sets `Phase live`.
       - `Pending` renders the waiting page again.
       - `Refused` sets `Phase failed` with
         `Error "the instance refused its first roster"`. That needs a
         person: its keys or roles are misconfigured.
    5. **Live:** 303 to `https://<PublicHost>/_carlos/enter?next=/`.
    
    It renders `PageWait` with
    `WaitData{Page, Account, RefreshSeconds, Failed}`, refreshing with
    `<meta http-equiv="refresh">` at 5 s for the first minute, 10 s up
    to 5 minutes, then 30 s. It sends `Cache-Control: no-store`. A
    failed account shows two ways out: retry (unless `HostTaken`) and
    the switcher.
  - `POST /signup/{slug}/retry` (owner): `Mutate` sets `Phase
    creating`, `Started now` and `Error ""`, keeping `Created`,
    `MayHaveLanded` and `Ready`. It refuses when `HostTaken`. Then 303
    to `/signup/{slug}`.
    `func (plaza *Plaza) retry(ctx, slug string) error` is shared with
    `/fleet`.
  - `GET /assert?aud=&nonce=`:
    - It needs a session that `auth.SessionCurrent` accepts: the
      epoch is current and the person is not suspended. A cookie the
      root would still honour for up to 15 minutes after a reset is
      not enough to mint an hour-long instance session. When there is
      no session, it redirects to `/signin` with return to the full
      `/assert` URL; the nonce cookie lives 300 s. When the session is
      stale, it refuses with `suspended-person` or sends the person to
      sign in again.
    - `aud` must be exactly `https://<slug>.<Parent>` (or
      `<slug>.<InstanceDomain>` when `Parent` is empty).
    - The account exists, is `live` and is not `Suspended`.
    - The person is in `Members`. A pending removal already refuses
      here, before the instance hears of it.
    - The `nonce` has the shape `assertion.Sign` demands: canonical
      unpadded base64url of exactly 16 bytes, so 22 characters.
      Anything else is `bad-request` before signing.
    
    It then signs `assertion.Claims{Version 1, Issuer Origin, Audience
    aud, Subject uid, KeyID, ID RandomID(), Nonce, IssuedAt now,
    ExpiresAt now+60, AuthTime}`. All times are `.Unix()`. `AuthTime`
    is the identity's `AuthTime`, or its `At` when that is zero, which
    it is for a magic link (`sessions/sessions.go:35`); `assertion`
    refuses a zero. It is clamped to at most `IssuedAt`. It then
    renders `PageAssert` with
    `AssertData{Page, Action: aud + "/_carlos/enter", Assertion: token}`.
    The response has:
    - `Cache-Control: no-store`;
    - `Referrer-Policy: no-referrer`;
    - a CSP whose `form-action` allows the audience origin;
    - an auto-submit script served as a static asset, not inline.
    
    Any refusal renders `PageRefused` with `RefusedData{Page, Reason}`
    and 403. `Reason`
    is one of `not-member`, `suspended-account`, `suspended-person`,
    `not-ready` or `bad-request`.

- [ ] **Step 1: Write the failing tests:**
  - `TestSignupToSignedIn` (`flow_test.go`, the spec's end-to-end).
    The sign-in is a real magic link, so `AuthTime` is zero and the
    `At` fallback is exercised.
    1. sign in, sign up `acme`;
    2. `Platform.Unready(2)`;
    3. `Follow` the waiting page until it redirects;
    4. the instance's enter redirects to `/assert`;
    5. the assertion form posts back.
    
    Expect `GET https://acme.example.test/whoami` to answer the uid.
    The tenant's roster is `[owner]`, and the record is `live` with
    `Delivered == Sequence`.
  - `TestWaitResumesAfterLostAnswer`:
    `Platform.Script(Reply{Hang: true, Record: true})`, so the first
    page load's create context ends (the harness sets
    `Config.CreateTimeout` to 50 ms). The instance is recorded, and
    the outcome is the context's error. Refresh. Expect the second call to pass `Resume`,
    find the host (`Landed`), and leave exactly one instance in
    `Platform.Instances`.
  - `TestTwoTabsCreateOnce`: two browsers of the same person refresh
    the waiting page concurrently 5 times, with the first create held
    by `Hang` until the second tab has loaded. Expect one console
    `POST`, and the second tab rendering the waiting page without
    calling `Create`.
  - `TestLateResultDropped`: the lease expires while a create hangs,
    and a second caller completes. The first caller's late outcome
    leaves the record as the second wrote it.
  - `TestWaitFailsAfterDeadline`: `Unready(1000)` and advance the
    clock by 16 minutes. Expect `Phase failed`, and that the page
    offers retry. Retry, then `MakeReady(host)`, and expect `live`.
  - `TestHostTakenIsFinal`: an instance `acme` was created out of
    band. Expect `Phase failed`, `HostTaken`, and
    `Error "address taken"`. Retry is refused, and no create with
    `Resume` is ever sent.
  - `TestFirstRosterRefusedFails`: the tenant's roles lack `owner`
    (it answers 422). Expect `Phase failed`.
  - `TestAssertNeedsCurrentSession`: hand off once, then
    `ResetSignin`. Within the root cookie's 15 minutes, `/assert`
    refuses.
  - `TestWaitIsOwnersOnly`: another signed-in person gets 404.
  - `TestAssertRefuses` (table): wrong `aud` host, `http://` aud, aud
    with a path, unknown slug, creating account, suspended account,
    not a member, a missing nonce, a 21-character nonce, and a
    22-character nonce that is not canonical base64url. Each gives 403
    and the right `Reason`.
  - `TestAssertTokenNotInURL`: no response `Location` or URL
    anywhere in the flow contains the token.
- [ ] **Step 2:** Run `go test ./ -run 'SignupToSignedIn|Wait|TwoTabs|LateResult|HostTaken|FirstRoster|Assert'`.
  Expect FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** Run it again. Expect PASS.
- [ ] **Step 5:** Commit with the subject
  `plaza: provision, wait and hand off the first owner`.

## Task 9: the switcher

**Files:**
- Create: `switcher.go`.
- Test: `switcher_test.go`.

**Interfaces:**
- Produces: `GET /accounts` renders `PageAccounts` with
  `AccountsData{Page, Accounts []AccountRow}`, where
  `AccountRow{Slug, Name, PublicHost, Role, Open string, State string}`.
  `State` is `ready`, `creating`, `failed` or `suspended`.
  - Rows come from `Memberships(uid)`. Each is checked against its
    account record. A membership counts when the person is in
    `Accepted`, or is the `Owner` of an account still `creating` or
    `failed`.
  - A membership whose account is gone, or that does not count, is
    dropped from the page and deleted. This is Tito Go's repair. A
    person in `Members` but not yet in `Accepted` has no membership
    yet, so a pending invite never shows a row.
  - `syncMemberships` (Task 6) follows the same rule, so it keeps the
    owner's signup membership while the account is being created.
  - `Open` is `https://<PublicHost>/_carlos/enter` for `ready` rows.
    A `creating` or `failed` row links to `/signup/{slug}` for its
    owner. A suspended row has no link.
  - Exactly one `ready` row and no others: 303 straight to its
    `Open`.

- [ ] **Step 1: Write the failing tests:**
  - `TestSwitcherOneAccountGoesStraightIn`.
  - `TestSwitcherListsSeveral`: rows are sorted by name.
  - `TestSwitcherRepairsStaleIndex`: delete the person from
    `Members` and `Accepted` by hand. Expect the row gone and the
    membership key deleted.
  - `TestSwitcherShowsCreatingToOwner`: straight after signup, the
    owner sees one `creating` row linking to `/signup/{slug}`, and
    nobody is sent anywhere.
  - `TestSwitcherShowsSuspendedUnlinked`.
- [ ] **Step 2:** Run `go test ./ -run Switcher`. Expect FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** Run it again. Expect PASS.
- [ ] **Step 5:** Commit with the subject
  `plaza: the account switcher`.

## Task 10: account people pages

**Files:**
- Create: `people.go`.
- Test: `people_test.go`.

**Interfaces:**
- Consumes: `changeRoster` (Task 6) and `auth.EnsureUser` (Task 1).
- Produces:
  - `GET /accounts/{slug}/people` is for members only; anyone else
    gets 404. "Member" here means in `Accepted`. It renders
    `PagePeople` with
    `PeopleData{Page, Account, Members []MemberRow, CanManage bool, Roles []string, Pending bool, Problem string}`.
    `Members` lists the desired roster, with each row marked
    `Pending` when it differs from `Accepted`. `Pending` is
    `Delivered < Sequence`.
  - `POST /accounts/{slug}/people` (`email`, `role`) invites
    someone: `EnsureUser`, then `changeRoster` adds them. It then
    sends one plain email through `Mailer` naming the account and
    the root's `/signin` link.
  - `POST /accounts/{slug}/people/{uid}` (`role`) changes a role.
  - `POST /accounts/{slug}/people/{uid}/remove` removes someone.
- **Rules:**
  - Only a member at `ManageFrom` or above may change people.
  - Nobody grants a role above their own.
  - The last member at `Roles[0]` cannot be removed or demoted.
  - A role not in `Roles` is refused.
  - A suspended account refuses every change.
  - Every POST checks the actor with `auth.SessionCurrent`.
  - After a change: `Delivered` gives 303 back to the page. `Pending`
    gives 303 with `?pending=1`, and the page says the change will
    arrive. `Refused` re-renders with `Problem "refused"`.

- [ ] **Step 1: Write the failing tests:**
  - `TestInviteDeliversThenIndexes`: the owner invites bo as
    `admin`. Expect the tenant to have bo, bo's membership to exist,
    and one mail to bo.
  - `TestInvitedPersonGetsIn`: bo signs in at the root, then is sent
    to the switcher, then to the instance. Expect `/whoami` to answer
    bo's uid.
  - `TestRoleRules` (table):
    - a member cannot invite;
    - an admin cannot grant owner;
    - the last owner cannot be demoted or removed;
    - an unknown role gives 400.
  - `TestPeoplePendingWhenBusy`: with `Busy(10)`, expect the invite to
    give `?pending=1`. bo's membership is not written, and the
    record's `Members` includes bo.
  - `TestRemovedMemberLosesSwitcherRow`.
- [ ] **Step 2:** Run `go test ./ -run 'Invite|RoleRules|PeoplePending|RemovedMember'`.
  Expect FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** Run it again. Expect PASS.
- [ ] **Step 5:** Commit with the subject
  `plaza: people pages deliver to the instance first`.

## Task 10b: invitations a person accepts (Paul, 2026-11-20)

Paul's ruling on the mockups: adding someone defaults to an invitation
they accept, and an app can switch that off to add people straight away
(Task 10's behaviour).

**Files:**
- Create: `invitations.go`.
- Modify: `plaza.go` (Config), `people.go` (the invite POST and page
  data), `mount.go`, `tick.go` (expiry sweep).
- Test: `invitations_test.go`.

**Interfaces:**
- Consumes: `changeRoster` (Task 6), the roster rules in
  `peoplerules.go` (Task 10), `auth.SessionCurrent` and `auth.User`.
- Produces:
  ```go
  type InviteMode int // InviteAccept (default, the zero value), InviteDirect
  // Config gains:  Invitations InviteMode
  //                InvitationLife time.Duration // default 14 days
  type Invitation struct {
  	ID        string    `json:"id"`         // random 128-bit, base32
  	Slug      string    `json:"slug"`
  	Email     string    `json:"email"`      // lowercased
  	Role      string    `json:"role"`
  	InvitedBy string    `json:"invited_by"` // uid
  	Created   time.Time `json:"created"`
  	Expires   time.Time `json:"expires"`
  }
  func invitationKey(slug, id string) string // "invitations/<slug>/<id>.json"
  ```
- **Inviting** (`POST /accounts/{slug}/people` when `Invitations` is
  `InviteAccept`): the same manager and role rules as Task 10, checked
  against the current record. No `auth.EnsureUser`: nobody is created
  until they sign in. If a pending invitation for that address exists
  on this account, it is replaced (delete then create), so there is at
  most one per address. Create the record, then mail the link
  `Origin + "/invitations/" + slug + "/" + id`. A person already in
  `Members` is refused with problem `member`. Answers 303 to the
  people page with `?invited=1`.
- **The people page** lists pending invitations (`Invitations
  []InvitationRow{ID, Email, Role, Created}`) beside the members, with
  `POST /accounts/{slug}/invitations/{id}/cancel` (delete) and
  `.../resend` (mail the same link again) for managers.
- **`GET /invitations/{slug}/{id}`**: renders `PageInvitation` with
  `InvitationData{Page, Account name, Inviter email, Role, State}`.
  `State` is one of `accept` (signed in as the invited address),
  `other-address` (signed in as someone else), `expired` or `gone`
  (no such record). Anonymous visitors go to `/signin` with return to
  this URL. The ID is a locator, not a credential: accepting needs a
  current session whose address is the invited one.
- **`POST /invitations/{slug}/{id}/accept`**: `auth.SessionCurrent`;
  the address must equal the invitation's; not expired; the account is
  not suspended. Then `changeRoster` adds the person at the invited
  role, re-checking inside the callback that the inviter could still
  grant it (`InvitedBy` is in `Accepted` and `Members` at `ManageFrom`
  or above, and the role is not above theirs) and that the person is
  not already a member. Delete the invitation only after the change is
  reserved. `Delivered` goes to the instance's enter;
  `Pending` goes to the switcher. A refused rule answers 403 with
  problem `withdrawn`.
- **`POST /invitations/{slug}/{id}/decline`**: a current session with
  the invited address deletes the record and renders `PageInvitation`
  with `State` `declined`.
- **Tick** (Task 13): delete invitations past `Expires`.
- With `InviteDirect`, Task 10's direct add is unchanged and no
  invitation records are written.

- [ ] **Step 1: Write the failing tests:**
  - `TestInviteCreatesInvitationNotMember`: no roster change, no user
    created, one mail with the link; the people page lists it.
  - `TestAcceptAddsMember`: the invited person signs in, accepts, and
    `/whoami` on the instance answers their uid; the invitation is gone.
  - `TestAcceptNeedsTheInvitedAddress`: another signed-in person sees
    `other-address`, and their accept POST is 403 with nothing changed.
  - `TestDeclineDeletes`, `TestCancelDeletes`, `TestResendMailsAgain`.
  - `TestExpiredInvitation`: past `Expires`, the page shows `expired`
    and accept is refused; the tick deletes it.
  - `TestInviterLostRightsWithdraws`: the inviter is removed before
    acceptance; accept is 403 `withdrawn`.
  - `TestReinviteReplaces`: inviting the same address twice leaves one
    invitation with the newer role.
  - `TestDirectModeAddsAtOnce`: with `InviteDirect`, Task 10's tests'
    behaviour holds.
- [ ] **Step 2:** Run `go test ./ -run 'Invit|Accept|Decline|Cancel|Resend|Expired|Reinvite|DirectMode'`. Expect FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** Run it again, then `go test ./...` and `-race`. Expect PASS.
- [ ] **Step 5:** Commit with the subject
  `plaza: invitations people accept, by default`.

## Task 10c: lowercase subjects and one owner per snapshot (idear, 2026-11-20)

Two requirements from idear's roster-store work, which implements
`roster.Store` for the default scaffold:

- **Every subject plaza sends is lowercase:** `roster.Member.Subject`
  in every snapshot and `Subject` in every assertion.
- **Every snapshot carries exactly one owner:** exactly one member at
  `Roles[0]`.

The approved spec (§6.3) says roles are the app's, and Patrix keeps
several owners. Ruling: one owner is the default, because idear is the
scaffold's roster, and `Config.ManyOwners bool` lets an app with its own
roster (Patrix) allow several. Cost if wrong: a config flag to flip.

**Files:**
- Modify: `plaza.go` (Config.ManyOwners), `peoplerules.go`,
  `people.go`, `invitations.go`, `peopleinvite.go`, `deliver.go`,
  `assert.go`.
- Test: `owners_test.go`.

**Behaviour with one owner (the default):**
- Granting `Roles[0]` to a member is a **transfer**: only the current
  owner may do it, and in the same `changeRoster` the old owner becomes
  `Roles[1]`. There is no second owner at any sequence.
- The owner cannot be removed or demoted except by a transfer (the
  existing last-owner rule already refuses both).
- Inviting someone at `Roles[0]`, by invitation or direct add, is
  refused with problem `one-owner`. Ownership moves only to an existing
  member.
- `Config` refuses `ManyOwners == false` with fewer than two roles,
  since a transfer needs `Roles[1]`.

**Guards in `deliver` (both modes):** before signing, check every
member's `Subject` equals `strings.ToLower(Subject)`, and in one-owner
mode that exactly one member is at `Roles[0]`. A snapshot that fails is
not sent: the delivery is `Refused` with `DeliveryError` naming the
rule, so a bug shows on `/fleet` instead of reaching an instance.
`/assert` refuses to sign a subject that is not lowercase (500, logged).
uids are lowercase base32 already, so these guards should never fire.

- [ ] **Step 1: Write the failing tests:**
  - `TestMakeOwnerTransfers`: the owner makes bo owner; the tenant's
    roster has bo at owner and ana at admin, at one sequence.
  - `TestOnlyTheOwnerTransfers`: an admin making someone owner is 403
    `forbidden-role`.
  - `TestInviteAsOwnerRefused`: invitation and direct add at owner are
    refused with `one-owner`.
  - `TestManyOwnersAllowsSeveral`: with `ManyOwners`, Task 10's
    behaviour holds (a second owner can be added).
  - `TestDeliverRefusesTwoOwners` and `TestDeliverRefusesUpperCaseSubject`:
    a record edited by hand to break each rule is not sent.
  - `TestEverySubjectIsLowercase`: across the signup-to-signed-in flow,
    every snapshot and assertion the tenant receives has lowercase
    subjects.
- [ ] **Step 2:** Run `go test ./ -run 'Owner|Subject|Lowercase'`. Expect FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** Run it again, then `go test ./...` and `-race`. Expect PASS.
- [ ] **Step 5:** Commit with the subject
  `plaza: one owner per roster by default; lowercase subjects`.

## Task 11: suspend and resume, for accounts and people

**Files:**
- Create: `suspend.go`.
- Test: `suspend_test.go`.

**Interfaces:**
- Consumes: `changeRoster`, and `auth.SetSuspended`/`EndSessions`.
- Produces:
  ```go
  func (plaza *Plaza) SuspendAccount(ctx context.Context, slug string) (Delivery, error)
  func (plaza *Plaza) ResumeAccount(ctx context.Context, slug string) (Delivery, error)
  func (plaza *Plaza) SuspendPerson(ctx context.Context, uid string) error
  func (plaza *Plaza) ResumePerson(ctx context.Context, uid string) error
  func (plaza *Plaza) ResetSignin(ctx context.Context, uid string) error // EndSessions
  ```
  - **Account suspension** is a `changeRoster` that sets `Suspended`
    and delivers `State suspended`. The root refuses `/assert` at
    once, whether or not the delivery landed.
  - **Person suspension** is `auth.SetSuspended`. It touches no
    instance, and the hourly renewal does the rest (spec §7.3).

- [ ] **Step 1: Write the failing tests:**
  - `TestSuspendAccountReachesInstance`: expect `Tenant.Suspended()`
    to be true, `/assert` to give `suspended-account`, and the
    instance's enter to refuse.
  - `TestResumeAccount`.
  - `TestSuspendAccountWhileInstanceBusy`: with `Busy(10)`, expect
    `Pending`, `/assert` refused anyway, and the record `Suspended`.
  - `TestAssertRefusesSuspendedPerson`: hand off, then
    `SuspendPerson`, then at once start a new handoff. `/assert`
    refuses, although the root cookie is still inside its 15 minutes.
  - `TestSuspendedPersonLosesInstanceWithinTheHour`: hand off, then
    `SuspendPerson`, then advance the clock by 61 minutes. `/whoami`
    on the instance redirects through renewal to `/assert`. The root
    cookie has expired, refresh refuses the suspended person, and the
    browser lands on `/signin`. Signing in again is refused, so
    `/whoami` never answers 200 again.
  - `TestResetSigninEndsRootSession`: within 15 minutes of
    `ResetSignin`, `/assert` and every `/fleet` or people POST refuse
    the old cookie. After the cookie's life, refresh signs it out.
- [ ] **Step 2:** Run `go test ./ -run 'Suspend|Resume|ResetSignin'`.
  Expect FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** Run it again. Expect PASS.
- [ ] **Step 5:** Commit with the subject
  `plaza: suspend and resume accounts and people`.

## Task 12: `/fleet`

**Files:**
- Create: `fleet.go`, `fleet_accounts.go`, `fleet_people.go`,
  `platformstate.go`.
- Test: `fleet_test.go`.

**Interfaces:**
- Consumes: `provision.Console.List` (Task 2), and Tasks 8 and 11.
- Produces:
  - **The gate.** Anonymous: 303 to `/signin` with return. Signed in
    but `Email` not in `Superadmins`: 404, identical to an unknown
    path. Every page's `Page.Superadmin` is true. Every `/fleet`
    request checks the session with `auth.SessionCurrent`, so a reset
    or suspended superadmin loses access at once.
  - **`platformState`:**
    `func (plaza *Plaza) platformState(ctx) (map[string]string, bool)`
    maps the platform host to a plaza state:
    - `Running` or `Serving`: `live`;
    - `Asleep`: `sleeping`;
    - `Failed`: `failed`;
    - `Setting up`: `starting`;
    - anything else: `unknown`.
    
    It makes at most 4 list pages of 499 per call and caches the map
    for 30 s. On a `*RetryError`, `ErrForbidden` or a timeout it
    returns `(nil, false)`, and the pages show "can't tell".
  - **`GET /fleet`** renders `PageFleet` with
    `FleetData{Page, Accounts, People int, Attention []AccountRow, Newest []AccountRow}`.
    Member counts come from `Accepted`.
    `Attention` holds accounts that are `failed`, that are
    `creating` past 15 minutes, that have `Delivered < Sequence`, or
    whose platform state is `failed`. `Newest` is the 10 most
    recently created. It reads every account record. That is fine to
    a few thousand accounts; a summary record kept by the tick is the
    follow-up if it is not.
  - **`GET /fleet/accounts?q=&cursor=`:** `q` matches slugs from the
    key listing (no reads). The page reads 50 records.
  - **`GET /fleet/accounts/{slug}`:** `PageFleetAccount`
    (`FleetAccountData`) with the
    members, state, `Error`, `DeliveryError`, platform state, and
    `DeleteCommand` =
    `carlos instances delete --app <TenantApp> --host <Host>`.
  - **Account actions:** `POST /fleet/accounts/{slug}/retry`,
    `/suspend` and `/resume`.
  - **`GET /fleet/people?email=&cursor=`:** an exact address uses
    `auth.UserByEmail`. Otherwise it pages with `auth.Users` (key
    order), 50 per page.
  - **`GET /fleet/people/{uid}`:** `PageFleetPerson`
    (`FleetPersonData`) with the
    memberships and roles, and whether suspended.
  - **Person actions:** `POST /fleet/people/{uid}/reset`, `/suspend`
    and `/resume`.
  - **`GET /fleet/superadmins`:** the list from `Config.Superadmins`.
  - There is no delete route anywhere.

- [ ] **Step 1: Write the failing tests:**
  - `TestFleetGate`: anonymous gives 303 to signin, a non-superadmin
    gets 404 with the body of a plain unknown path, and a superadmin
    gets 200.
  - `TestFleetShowsPlatformState`: with two accounts, one `Sleep`
    and one `Fail`, expect the states `sleeping` and `failed`, the
    failed one in `Attention`, and `Platform.Calls()` to hold no edge
    request to either instance during the page view.
  - `TestFleetWithoutPlatformList`: `ScriptList(Reply{Status: 429})`.
    Expect 200, every state `unknown`, and no edge request.
  - `TestFleetCachesList`: three page views within 30 s make one list
    call.
  - `TestFleetRetryFailedSignup`.
  - `TestFleetSuspendResumeAccount` and `TestFleetPersonActions`.
  - `TestFleetPeopleExactSearch`.
  - `TestFleetHasNoDelete`: no route in `Mount` matches
    `DELETE`/`POST .../delete`. The account page's `DeleteCommand`
    equals the exact string.
- [ ] **Step 2:** Run `go test ./ -run Fleet`. Expect FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** Run it again. Expect PASS.
- [ ] **Step 5:** Commit with the subject
  `plaza: /fleet from records and platform state`.

## Task 13: the daily tick

**Files:**
- Create: `tick.go`.
- Test: `tick_test.go`.

**Interfaces:**
- Produces: `POST /_plaza/tick`.
  - **Authentication:** `carlos.Tick(r)`, or 404. It is exempt from
    CSRF.
  - **Bound and progress:** each run stops at a 4-minute deadline and
    answers 200 with counts. It is idempotent, so a repeated occurrence
    is harmless. It keeps its place in `tick/state.json` (the last
    account slug and the last membership key it finished, written with
    `Mutate` after each item). The next run resumes there and wraps
    round at the end, so slow deliveries at the front of the list
    cannot starve the rest. One `deliver` per account per run, each
    with a 30-second context.
  - **What it does, in order:**
    1. `auth.Sweep(now)` (expired links) and `Proof.Sweep(now)` (spent
       proof nonces). Neither takes a context, so they run before the
       deadline clock starts. Each is bounded by what one day can
       write: links live 15 minutes and nonces their challenge's
       `MaxAge`. Context-aware sweeps in core are a follow-up, if the
       daily volume ever makes these slow.
    2. Delete `created/<day>/` markers older than 2 days.
    3. From the saved place, for each `accounts/` record:
       - `syncMemberships` (Task 6), which writes from `Accepted`;
       - if `Delivered < Sequence` and the phase is `live`, run
         `deliver`. This wakes only those instances, which have a
         change waiting;
       - if the phase is `creating` and `Started` is over 15 minutes
         ago, set the phase to `failed`.
    4. From the saved place, for each membership key, delete it if its
       account is gone or the person does not count (Task 9's rule).
  - It never calls `Create`, and never contacts an instance that has
    nothing to deliver.
  - Documented schedule:
    `carlos schedule set -name plaza-daily -every 24h -path /_plaza/tick`.

- [ ] **Step 1: Write the failing tests:**
  - `TestTickNeedsToken`: without the tick token, 404.
  - `TestTickRedeliversPending`: invite while `Busy(10)`, then
    `Busy(0)`, then tick. Expect the tenant to have the member and
    the membership to be written.
  - `TestTickRepairsIndexes`: delete one membership and add a stray
    one. After the tick, both are corrected.
  - `TestTickWakesNothingIdle`: three live accounts with nothing
    pending. After the tick, `Platform.Calls()` holds no edge request.
  - `TestTickFailsStaleCreating`.
  - `TestTickResumesWhereItStopped`: 5 accounts pending, the first 2
    hanging, and a deadline that allows 2 items. The second run starts
    at account 3, and after three runs all 5 have been tried.
- [ ] **Step 2:** Run `go test ./ -run Tick`. Expect FAIL.
- [ ] **Step 3:** Implement. Set the tick token in tests with the
  environment variable `carlos.Tick` reads.
- [ ] **Step 4:** Run it again. Expect PASS.
- [ ] **Step 5:** Commit with the subject
  `plaza: daily tick repairs indexes and resends`.

## Task 13b: connect your domain (spec §7.4)

Needs Task 2b. Spec §7.4 holds the rulings: the custom domain becomes
the instance's origin, and the current origin is the one in the last
accepted snapshot. Roster delivery moves to the platform host. The
feature is off unless `Config.CustomDomains` is set.

**Files:**
- Create: `domains.go`, `domainrecords.go`, `domains_test.go`.
- Modify:
  - `records.go`: Account gains `Domain string` (desired host),
    `DomainLive bool` (desired) and `AcceptedOrigin string` (from the
    accepted snapshot);
  - `signroster.go` (Origin);
  - `deliver.go`: audience and URL become `https://` + `Account.Host`,
    the platform host, and `markDelivered` records `AcceptedOrigin`;
  - `assert.go`, `switcher.go`, `tick.go`, `fleet*.go`, `mount.go`;
  - `plazatest`: the fake tenant serves the stored origin and restarts
    on `OnOrigin`.

**Interfaces:**
- `func (account Account) Origin() string` returns `AcceptedOrigin`
  when it is set, else `https://` + `PublicHost`. It never returns the
  desired origin before the instance has accepted it. These all use it:
  `/assert`, the switcher's Open, the waiting page's redirect and the
  invitation accept's redirect.
- `/assert` takes `aud` as either `https://<slug>.<Parent>` or a custom
  host. A custom host is resolved through `domains/<host>.json` to a
  slug, and the account's `Origin()` must equal `aud`. Unknown, pending,
  removing and reassigned hosts are `bad-request`.
- **The hostname record** `domains/<host>.json`:
  ```go
  type HostRecord struct {
  	Host, Slug string
  	State      string // "reserving", "attached", "removing"
  	Generation int64  // bumped on every state change
  	Created, Expires time.Time // Expires: reserving/unproved, +7 days
  	CheckedAt, NextCheck time.Time
  	PlatformState, Reason string
  	CertExpiry time.Time
  }
  ```
  It is created once. Every later write is a `Mutate` that checks the
  generation it expects. One custom domain per account (my call).
- **Routes** (Owner only: `Roles[0]` in both rosters and
  `SessionCurrent`; 404 when `CustomDomains` is off):
  - `GET /accounts/{slug}/domain` renders `PageDomain` with
    `DomainData{Page, Account, Host, Records, State, Reason, CertExpiry,
    Problem}`. `State` is one of `none`, `proving`, `waiting-dns`,
    `waiting-cert`, `live` or `not-working`.
  - `POST /accounts/{slug}/domain` adds a host.
  - `POST .../domain/check`.
  - `POST .../domain/remove` (after a confirm page), which also serves
    as "use the default address".
- **Adding:**
  1. Validate the host: lowercase, two or more labels, at most 253
     characters. Refuse it if it is the root's host or sits under
     `Parent` or `InstanceDomain`.
  2. Create the record as `reserving`. `ErrExists` held by another slug
     is `taken`; an expired `reserving` record held by another slug is
     released conditionally, then retried once.
  3. `Domain(host)`: if the platform already has a claim plaza has no
     record of, mark the record released and answer `taken`.
  4. `AttachDomain(host, Account.Host)`.
  5. Mutate to `attached` and set `Account.Domain`.

  An ambiguous attach (no answer, 5xx, or the context ending) is never
  undone blind. It stays `reserving`, and the next check reads
  `Domain(host)` to decide: attached to our route means `attached`;
  absent means release.
- **Checking** (the check route, page views and the tick):
  - While unverified, repeat the attach; then `Domain(host)`.
  - Store `PlatformState`, `Reason`, `CheckedAt`, `CertExpiry` and
    `NextCheck`.
  - **Live** means: platform `state` is `live`, `Route` equals
    `Account.Host`, `CertExpiry` is in the future, and `CheckedAt` is
    within the last hour.
  - On live, `changeRoster` sets `DomainLive`. The snapshot then
    carries `Origin`, and `Origin()` switches when it is accepted.
  - **Not working:** a live domain that fails the live test on three
    daily checks in a row shows as not working. Nothing switches back
    automatically.
- **Console budget:**
  - Domain calls go through one in-process token bucket: 10 a minute,
    shared by page views, checks and the tick. This leaves the rest of
    the console's 60 a minute for provisioning.
  - Concurrent checks of one host are coalesced.
  - The tick visits only records whose `NextCheck` is due, at most 20
    console calls per run, resuming by key like the other tick passes.
  - `NextCheck` is every 10 minutes while proving or waiting (for the
    first day, then daily), daily when live, and every 6 hours within
    14 days of certificate expiry.
- **Removing,** which is also "use the default address":
  1. Mutate the record to `removing`, bumping the generation.
  2. `changeRoster` clears `DomainLive`. Wait for `Delivered` with
     `AcceptedOrigin` empty; on `Pending`, show problem `pending` and
     let the tick finish it.
  3. Read `Domain(host)`, and `DetachDomain` only when its route is
     `Account.Host`.
  4. Delete the record with a generation check, and clear
     `Account.Domain`.
- **Expiry:** a `reserving` record past `Expires` is released by the
  tick (generation check), and the platform lapses its side.
  Superadmins can release any record from `/fleet`.
- **Suspension:** a suspended account keeps its domain, and the
  instance refuses sign-in as before.

- [ ] **Step 1: Write the failing tests:**
  - `TestCustomDomainsOffByDefault`: 404.
  - `TestAddDomainShowsPlatformRecords`: the page shows the GET's
    records and guidance, and the attach route is the platform host.
  - `TestDomainGoesLive`: once live and accepted, `Origin()` is the
    custom origin, `/assert` signs for it, and the switcher opens it.
  - `TestOriginWaitsForAcceptance`: with the tenant `Busy`, `Origin()`
    stays the old one and `/assert` still signs for the old one.
  - `TestDomainNotLiveUntilPlatformSaysLive` (expired certificate,
    wrong route, stale check), `TestDomainNotWorkingAfterThreeChecks`.
  - `TestDomainTakenByAnotherAccount`, `TestDomainHeldOutsidePlaza`,
    `TestDomainUnderParentRefused`.
  - `TestAmbiguousAttachReconciles`, `TestRemoveRacingAddKeepsTheNewClaim`
    (a slow second removal never detaches a host another account now
    holds), `TestReservationLapses`.
  - `TestRemoveDomain`: the origin is cleared and accepted first, then
    detached.
  - `TestAssertResolvesCustomAudience` (unknown, pending, removing and
    reassigned hosts refused).
  - `TestDomainBudgetIsShared`, `TestOnlyOwnerManagesDomain`,
    `TestRosterGoesToPlatformHost`.
- [ ] **Step 2:** Run `go test ./ -run 'Domain|Origin|RosterGoesToPlatformHost|AssertResolves|Reservation'`. Expect FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** Run it again, then `go test ./...` and `-race`. Expect PASS.
- [ ] **Step 5:** Commit with the subject
  `plaza: connect your own domain`.

## Task 13c: the account settings section (Paul, 2026-11-20)

Paul's second round on the mockups: an account's settings are full
pages at `<root>/<slug>/settings/<page>`, in a settings layout with a
left-hand menu (General, People, Your domain, Danger zone). They are not
the centred chooser column. Waits for Paul's approval of the mockups.

**Files:**
- Create: `settings.go`.
- Modify: `people.go`, `peopleinvite.go`, `invitations.go`,
  `domains.go`, `suspend.go`, `mount.go` and the tests that name the
  old paths.

**Interfaces:**
- **Routes move:**
  - `/accounts/{slug}/people...` becomes `/{slug}/settings/people...`.
  - `/accounts/{slug}/invitations/{id}/...` becomes
    `/{slug}/settings/people/invitations/{id}/...`.
  - `/accounts/{slug}/domain...` becomes `/{slug}/settings/domain...`.
  
  The invitation link (`/invitations/{slug}/{id}`) and the switcher
  (`/accounts`) stay. `{slug}` is shape-checked and must name an
  existing account, or the answer is 404. A first path segment that is
  a reserved slug never reaches these routes.
- **The settings frame:** every settings page's data embeds
  `SettingsData{Page, Account, Section string, CanManage, IsOwner bool,
  Open string}`. `Open` is `Account.Origin()` + `handoff.EnterPath`,
  the header's "Open site" link. Viewing needs `Accepted` membership
  (404 otherwise), as People did.
- `GET /{slug}/settings` redirects to `/{slug}/settings/general`.
- **General:** `GET` shows the name and address.
  `POST /{slug}/settings/general` renames the account (`Name`, the same
  rules as signup's name) for `ManageFrom` and above. The name reaches
  memberships at the next `syncMemberships`.
- **People:**
  - One table, `PeopleData.Rows []PersonRow{Kind ("member" or
    "invitation"), UID or ID, Name, Email, Role, Status, CanChangeRole,
    CanRemove, IsYou}`. Members and pending invitations sit in the same
    table.
  - `Status` is one of `active`, `adding` (desired, not yet accepted),
    `removing`, or `invited` (with the invitation's `Created`).
  - The owner's own row has `IsYou` and no actions.
  - The role menu in each row posts to the existing role route. The
    row menu has remove (members), and resend and revoke (invitations).
- **Invite is a modal route:** `GET /{slug}/settings/people/invite`
  renders `PageInvite` with `InviteData{PeopleData, Form, Problem}`.
  The template draws the people page under Fiesta's inert backdrop and
  the dialog over it, and closing is a link back. The dialog posts to
  the existing invite handler. On a problem it re-renders the modal
  route, not the page.
- **Danger zone:**
  - `POST /{slug}/settings/danger/suspend` and `.../resume` are
    Owner only. They call `SuspendAccount` and `ResumeAccount`, with a
    confirm page for suspend.
  - `POST .../leave` is for a non-owner member: it is a `changeRoster`
    removing the actor, then 303 to `/accounts`. The owner is refused
    with problem `owner-must-transfer`.
  - There is no delete (spec §7.3).
- **`/fleet`** keeps its sidebar layout with wide tables (Task 12),
  unchanged.

- [ ] **Step 1: Write the failing tests:**
  - `TestSettingsRoutesMoved`: the new paths answer, and the old ones
    404.
  - `TestSettingsNeedsMembership`.
  - `TestRenameAccount` (and refused below `ManageFrom`).
  - `TestPeopleTableMergesInvitations`.
  - `TestOwnerRowHasNoActions`.
  - `TestInviteModalRoute` (and a problem re-renders the modal).
  - `TestOwnerSuspendsAndResumes`.
  - `TestMemberLeaves`.
  - `TestOwnerCannotLeave`.
- [ ] **Step 2:** Run `go test ./ -run 'Settings|Rename|PeopleTable|OwnerRow|InviteModal|OwnerSuspends|Leave'`. Expect FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** Run it again, then `go test ./...` and `-race`. Expect PASS.
- [ ] **Step 5:** Commit with the subject
  `plaza: account settings section at /<slug>/settings`.

## Task 14: default templates and copy (after Task 0's approval)

**Amended after the mockups (Paul, 2026-11-20, two rounds):**
- The word for an account is the app's. Default templates say "account"
  and "accounts" through `Config.Nouns{One, Many}` (default
  `{"account", "accounts"}`); Seamus's mockups say "space".
- **Layouts:**
  - The switcher (`/accounts`) is a centred single column: Tito Go's
    chooser, about 29rem, with a centred heading and account rows as
    cards.
  - Signup, the waiting page, `/assert`, refusals and the invitation
    pages use the stage shell.
  - The account settings section (Task 13c) and `/fleet` use the
    sidebar shell: a left-hand menu, a page header and wide content.
- **The settings header** shows the account name, its address, and an
  "Open site" link with Lucide `external-link`. People adds the primary
  "Invite" button (Lucide `user-plus`).
- **People** is one table: person (name or email), role, status, and a
  row menu.
  - The role menu is the `dropdown` partial, inline in the row (Paul's
    rule: dropdowns sit inline, never on their own line).
  - Pending invitations are rows with an "Invited <when>" status.
  - The owner's row reads "Owner (you)" with no menus.
- **Invite** is Fiesta's modal primitive: inert backdrop, `<dialog
  rst-modal-panel open>`, and a close link with Lucide `x`. Use a small
  panel; if Fiesta has no small variant, ask Fiesta for one rather than
  overriding its CSS in plaza.
- **Your domain and Danger zone** pages follow the approved mockups.
- **Buttons on stage pages** (invitation, refusals, decisions) are
  default size with short labels ("Accept", "Decline") in
  `rst-form-actions`, not `form-foot`'s large buttons (Paul, 2026-11-20).
- **Instance menu:** each instance's own menu gets a "Settings" item
  linking to `<root>/<slug>/settings/general`. That is the scaffold's
  job (roadmap Plan 4), recorded here so Plan 4 picks it up.

**Files:**
- Create: `templates/*.html` (one `{{define "plaza/<page>"}}` per page
  constant), `templates.go`, `static/assert.js`.
- Test: `templates_test.go`, `browser_test.go`.

**Interfaces:**
- Produces:
  - `func Templates() fs.FS` holds plaza's page templates. An app
    parses them after `ui.Templates()` and before its own, so that
    redefining a `plaza/...` name in the app's tree overrides that
    page.
  - `func Assets() fs.FS` holds `assert.js`. The app serves it, and
    `Config.AssetPath` names it in `PageAssert`.
  - **The override points,** documented in `SKILL.md`:
    - `Brand.Name`;
    - every `plaza/<page>` template;
    - the app's own layout, because the app renders;
    - `SigninSubject`, `SigninBody` and `InviteMail` on `Config`.

- [ ] **Step 1:** Build each page from the approved mockups, using
  `ui` partials only. Write no new CSS beyond the design tokens.
- [ ] **Step 2: Copy review.**
  - Extract every user-facing string: templates, the invite mail, the
    refusal reasons and the guard problems.
  - Run them through Luna with
    `codex exec -m gpt-6-luna -c model_reasoning_effort=high -s read-only --skip-git-repo-check -o <out> - < <prompt>`.
  - Apply its edits unless one is factually wrong.
  - Post the strings, the reply and what was applied with
    `amadan discuss new rastrillo/plaza -branch <b>`.
- [ ] **Step 3: Write the tests:**
  - `TestEveryPageHasATemplate`: each page constant is defined in
    `Templates()`, and executes with zero-value data plus `Page`.
  - `TestAppOverridesAPage`: an app tree that redefines
    `plaza/accounts` renders the app's version.
  - `TestWaitPageRefreshesWithoutJS`: the `<meta http-equiv="refresh">`
    content matches `RefreshSeconds`.
  - `TestAssertPageWorksWithoutJS` (chromedp, JS off): clicking the
    visible button completes the handoff.
- [ ] **Step 4:** Run `go test ./ -run 'Template|Overrides|RefreshesWithoutJS'`.
  Expect PASS. The browser test runs on the fleet.
- [ ] **Step 5:** Commit with the subject
  `plaza: default pages on Rastrillo components`.

## Task 15: `SKILL.md`, README and example

**Files:**
- Create: `SKILL.md`, `README.md`, `skillmd_test.go` (copy aviso's
  shape), and `example/main.go` (a root app that mounts plaza, about
  60 lines, built in CI).

- [ ] **Step 1:** Write `SKILL.md` for an agent wiring plaza. Cover:
  - what plaza is and is not (no delete, no billing, no support
    access);
  - every `PLAZA_*` variable;
  - the `carlos` commands the root app needs (`services create --role
    instance`, `secrets set`, `env set`, `schedule set`);
  - the leak profile (UM §8.5 and §9, model B), in three parts:
    - **Publishing the store** shows every person's email address, the
      accounts each belongs to with their roles, each account's name,
      address and state, and the record of signups per day.
    - **Compromising the signing key** lets the holder sign in as
      anyone to every instance that trusts the root key, and push any
      roster to them. The defence is rotating the key with two `kid`s.
    - **Compromising the provisioning token**, until carlos/platform
      #185 ships, means the app-wide `instance` role, which can
      create, delete or move every instance.
  - the two residual gaps the plan accepts: a person hand-creating an
    instance with a signup's exact label during a create with no
    definite answer, and hand-deleting an account record (which can
    reuse a roster sequence if the clock has gone backwards);
  - the override points;
  - `plazatest` for the app's own end-to-end tests.
- [ ] **Step 2:** `TestSkillCommandsExist`: every `PLAZA_*` name and
  route in `SKILL.md` exists in code.
- [ ] **Step 3:** Run `go test ./ -run Skill` and `go build ./example`.
  Expect PASS.
- [ ] **Step 4:** Commit with the subject
  `plaza: skill, readme and example`, then push.
- [ ] **Step 5:** Run a whole-branch review by Astra (Paul's rule), and
  apply what it finds. Then write the handoff. No tag: v0.1.0 needs
  Paul's go.
