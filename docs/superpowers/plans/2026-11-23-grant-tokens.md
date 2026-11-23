# Grant tokens (access B): implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Any Rastrillo site can accept a grant token signed by its root, for one audience, until it expires or the root's latest roster snapshot stops listing the grant. Each request then runs as "agent:<name> for user:<subject> grant:<id>".

**Architecture:**
- **A new package, `granttoken`,** with three parts:
  - signing and verifying (`Sign`, `Verifier`), under the same discipline as `roster` and `assertion`, sharing `internal/wire`;
  - `Door`, the `api.Verifier` and `api.Rechecker` that adds the roster check;
  - `Either`, which sends grant tokens to the `Door` and every other token to `apitoken`.
- **`roster` gains `Member.Grants`.** The receiver keeps it in the row it already records (no migration), and answers `GrantActive` from it.
- **Revocation needs no new code.** A revoked grant is absent from the next snapshot. `GrantActive` then answers false, and the gate's existing recheck cuts any answer still going out.
- **Nothing in `oauth` or `apitoken` changes, and this plan does not connect them to grants.** `granttoken.Grants` is the site-side seam: a site passes its receiver, and the root will pass its own grant records (step 2). OAuth at the root cannot issue grant tokens yet: `oauth.Server` mints access tokens only through `*apitoken.Store` (`oauth/server.go`, `oauth/token.go`). Its actors also carry a resource-path audience (`<origin>/api/v1` or `/api/v1/mcp`) and scopes, where a grant token carries an origin audience and no scopes. Three things wait for the grants spec named in oauth spec §5.10:
  - the issuance adapter that has OAuth mint grant tokens;
  - the mapping from a connection to a grant record;
  - the mapping of audience and scopes between the two.

**Tech Stack:** Go; `crypto` (ECDSA P-256 with a signing context); `internal/wire`; `github.com/go-json-experiment/json`; `api`, `roster`, `handoff/handofftest`; SQLite through the receiver's existing `roster_state` row.

**Spec:** `docs/superpowers/specs/2026-11-22-agent-parity-design.md` §5.3 (grants), with §5.8 (human-only) and §6 step 1. Its seam with OAuth is `docs/superpowers/specs/2026-11-22-oauth-design.md` §5.10. Read them with this plan. Every § below is the agent-parity spec's.

## Global Constraints

- **Package:** `amadan.net/rastrillo/rastrillo/granttoken`.
- **Signing context:** `rastrillo/grant/v1`, used by nothing else. A payload signed under `rastrillo/roster/v1` or `rastrillo/assertion/v1` must fail as `ErrSignature`.
- **Wire form:** `rastrillo_grant_` followed by `wire.Join(payload, signature)`. The token after the prefix is at most 4096 bytes. `Prefix` is exported so that `Either` and `Redact` can recognise the form.
- **Claims (JSON names exact):**
  - `v`: always 1;
  - `iss`: the root's origin;
  - `aud`: one origin, the site's platform host;
  - `kid`: the signing key's ID;
  - `sub`: the person's root subject;
  - `gid`: the grant ID;
  - `agent`: the agent's name;
  - `access`: `read` or `write`;
  - `iat`, `exp`: Unix seconds.

  Parsing is strict: an unknown member, a duplicate member or a wrong JSON type is refused (`ErrMalformed` for a duplicate, `ErrClaims` for the rest). Every one of the ten members is required and none may be `null`. `wire.StrictObject` and `json.RejectUnknownMembers` do not catch a missing or `null` member, because unmarshalling turns both into a zero value. So before unmarshalling into `Claims`, the strict parse reads the object as `map[string]jsontext.Value` and refuses, with `ErrClaims`, any required name that is absent or whose value is `null`, as `assertion.parse` does.
- **Claim rules:**
  - `iss` and `aud` pass `wire.Origin`, and `iss` may equal `aud`, which is the root's own door (the dashboard);
  - `kid` is `wire.Identifier(…, 64)`, `sub` is `wire.Identifier(…, 128)` and `gid` is `wire.Identifier(…, 64)`;
  - `agent` passes `apitoken.ValidAgentName` and equals its trimmed form;
  - the trust set has 1 to 8 keys (`wire.TrustSet`).
- **Time.** Three rules are static: `Sign` and `Verify` both check them, with no clock.
  - `iat > 0` and `exp > 0`;
  - `0 < exp - iat <= 31_622_400` (366 days; the spec's "at most 1 year" must still hold in a leap year).

  Two rules need the verifier's clock, so only `Verify` checks them:
  - expired when `exp <= now`;
  - refused when `iat - 30 > now`.

  Issuer, audience and trusted keys are the verifier's too. `Sign` checks only that `iss` and `aud` are well-formed origins.

  The 90-day default and the grant record's expiry are the root's to set (step 2). A token's `exp` is its grant's expiry.
- **Roster field:** `Member.Grants []string` with the tag `json:"grants,omitempty"`. Each ID is `wire.Identifier(…, 64)`, a member has at most 64, and no ID appears twice in one snapshot, even under two members.
- **Site check, in this order:** one `Authorization: Bearer` header, the signature, `iss`, `aud`, the claims, expiry, then `GrantActive(sub, gid)`. `GrantActive` is false before any snapshot, when the latest snapshot suspends the instance, and when it lists no such grant for that subject.
- **Actor:**
  - `Subject` = `sub`, `Agent` = `agent`, `Credential` = `gid`, and `Access` = `Read` or `Write`;
  - `Grant` = `&rastrillo.Grant{ID: gid, Issuer: iss, IdentityIssuer: iss, Audience: aud}`;
  - no `Session`, so a write still needs an `Agent-Session` header when `RequireAgentSession` is set (§5.3);
  - `String()` is `agent:<agent> for user:<sub> grant:<gid>`.
- **Errors:** token and claim failures, and an inactive grant, wrap `api.ErrUnauthorized` (401). A `GrantActive` error wraps `api.ErrUnavailable` (503). Error values hold no submitted text.
- **No migration.** `roster_state.members` already stores JSON, and `migrate/frozen_checksums_test.go` must pass unchanged.
- **Rollout (§5.3):**
  1. Release Rastrillo v0.47.0;
  2. deploy `patrix-sites` on it, one release for every hosted site;
  3. only then may plaza put grant IDs in snapshots.

  An older receiver refuses a snapshot that holds `grants`, and plaza never retries a refused snapshot (`plaza/deliver.go`, `Refused`). A snapshot with no grants stays byte-for-byte what v0.46.0 signs, so it remains deliverable to old sites.
- **House rules:**
  - 80 columns, and Go naming;
  - `go run ./cmd/rastrillo budget code` refuses single-letter names, files over 400 lines and functions over 40 lines, test files included;
  - `make budget` caps a directory at 5,000 source and 8,000 test lines;
  - comments only say why;
  - docs go in `docs/site/reference`, kept honest by the docsite tests;
  - the CHANGELOG entry goes under `## Unreleased`;
  - every user-facing string (docs prose, CHANGELOG) goes through Luna.
- **Gates:**
  - targeted tests locally, with `GOFLAGS=-mod=mod`;
  - full CI on the fleet;
  - git and `go run` tooling run with the sandbox off.

## Review Focus

1. **Grants wrong after the receiver repairs from its own row, or records roles only.** `RecordRolesOnly` writes `roleOnly`, and a 409 repair re-applies the recorded row. If either drops `grants`, every grant token on that site dies silently. If a stale replay brought a grant back, a revoked token, or a removed member's, would work again. Tests: Task 2, `TestGrantsSurviveRolesOnlyAndRepair`, `TestRevokedGrantStaysRevokedAfterStaleReplay` and `TestRemovedMemberGrantStaysRevokedAfterStaleReplay`.
2. **A grant listed under another member.** A token for subject A naming grant `g1` must fail when the roster lists `g1` only under B. Test: Task 2, `TestGrantActiveIsPerSubject`. Task 3, `TestDoorAsksForTheTokensSubject`, checks that the door passes the token's own subject.
3. **A suspended site.** The roster still lists the members and their grants, but the site is suspended, so a token must be refused. Test: Task 2, `TestGrantActiveFalseWhenSuspended`.
4. **A refused grant token tried as an `apitoken`.** A grant-shaped token that the door refuses, for example one whose audience is the dashboard and not this site, must get 401 from the door. It must never reach the fallback verifier. Test: Task 4, `TestEitherNeverFallsThrough`.
5. **Grants seen as a change to the member.** `Member` stops being comparable, and the app's `Store` does not hold grants. If `syncMembers` compared grants, every snapshot would call `Put` for every member with a grant, and Patrix's `Put` rewrites the person's row. Test: Task 2, `TestGrantsAloneDoNotRewriteStore`.

---

Work in the worktree `../rastrillo-grants` on branch `grant-tokens`, cut from `origin/main` 3b13cb06 (v0.46.0).

### Task 1: Sign and verify grant tokens

**Files:**
- Create: `granttoken/token.go`, `granttoken/token_test.go`, `granttoken/testdata/golden.json`

**Interfaces:**
- Produces:

```go
const Prefix = "rastrillo_grant_"

// Access values on the wire.
const Read, Write = "read", "write"

type Claims struct {
	Version   int64  `json:"v"`
	Issuer    string `json:"iss"`
	Audience  string `json:"aud"`
	KeyID     string `json:"kid"`
	Subject   string `json:"sub"`
	Grant     string `json:"gid"`
	Agent     string `json:"agent"`
	Access    string `json:"access"`
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
}

var ErrMalformed, ErrVersion, ErrUnknownKey, ErrSignature, ErrIssuer,
	ErrAudience, ErrLifetime, ErrExpired, ErrClaims, ErrConfig,
	ErrSigning error // "granttoken: <word>", as roster's

func Sign(key *crypto.Keypair, claims Claims) (string, error)
func NewVerifier(
	issuer, audience string, keys map[string][]byte,
) (*Verifier, error)
func (verifier *Verifier) Verify(token string, now time.Time) (Claims, error)
func Redact(text string) string
```

- [ ] **Step 1: Write the failing tests** in `token_test.go`, package `granttoken`. Build signed-but-invalid payloads with `crypto.Sign` and `wire.Join`, as `roster/token_test.go`'s `rawToken` does.
  - `TestSignedTokenVerifies`: the token starts with `Prefix`, and `Verify` returns claims equal to those signed.
  - `invalidClaims`, one shared table of static claim faults, each with its error. Each row is a signed payload, built raw where `Claims` cannot express it:
    - `"v":2` → `ErrVersion`;
    - each of the ten members missing, one row per member → `ErrClaims`;
    - each of the ten members `null`, one row per member → `ErrClaims`;
    - `"aud":["https://site.test","https://other.test"]` → `ErrClaims`;
    - `"iat":"1700000000"`, a string → `ErrClaims`;
    - an extra `"scope":"x"` member → `ErrClaims`;
    - `"iss":"http://root.test"` → `ErrClaims`;
    - `"access":"admin"` → `ErrClaims`;
    - `"agent":"a:b"` → `ErrClaims`;
    - `"agent":" Claude"` → `ErrClaims`;
    - an empty `gid` or one of 65 characters → `ErrClaims`;
    - a 129-character `sub` → `ErrClaims`;
    - `iat` of 0, and of -1 → `ErrLifetime`;
    - `exp <= iat` → `ErrLifetime`;
    - `exp - iat == 31_622_401` → `ErrLifetime`.
  - `TestVerifyRefusesInvalidClaims`: every `invalidClaims` row, signed and checked at a `now` inside its lifetime, gives `errors.Is(err, want)` and zero `Claims`.
  - `TestVerifyRefuses`, the verifier's own checks, one case per row, each asserting `errors.Is(err, want)` and zero `Claims`:
    - no prefix, a second dot, padded base64 → `ErrMalformed`;
    - 4097 bytes after the prefix → `ErrMalformed`;
    - a duplicate `"sub"` member → `ErrMalformed`;
    - an unknown `kid` → `ErrUnknownKey`;
    - signed by another key → `ErrSignature`;
    - the same payload signed under `rastrillo/roster/v1` → `ErrSignature`;
    - the same payload signed under `rastrillo/assertion/v1` → `ErrSignature`;
    - another `iss` → `ErrIssuer`;
    - `aud` of `https://other.test` → `ErrAudience`;
    - `exp == now` → `ErrExpired`;
    - `iat == now+31` → `ErrLifetime`, and `iat == now+30` verifies.
  - `TestSignRefusesInvalidClaims`: `Sign` returns the row's error for every `invalidClaims` row that a `Claims` value can express, and `ErrSigning` for a nil key. It consults no clock: an expired token, a token with `iat` in the future and one for any well-formed audience all sign.
  - `TestNewVerifierRefusesBadConfiguration`, each → `ErrConfig`:
    - an `http://` issuer;
    - zero keys, and nine keys;
    - a 64-byte public key.
  - `TestNewVerifierAllowsTheRootsOwnDoor`: `iss == aud` builds, and a token for it verifies.
  - `TestGoldenVector`: `testdata/golden.json` (`publicKey`, `now`, `claims`, `token`) verifies to `claims`, so the wire form cannot drift.
  - `TestRedactHidesGrantTokens`:
    - `"Authorization: Bearer rastrillo_grant_eyJ2.abc-_"` becomes `"Authorization: Bearer rastrillo_grant_[redacted]"`;
    - text with no `rastrillo_grant_` comes back unchanged.
- [ ] **Step 2: Run them and see them fail:** `GOFLAGS=-mod=mod go test ./granttoken -run 'Signed|VerifyRefuses|SignRefuses|NewVerifier|Golden|Redact'`. They fail to compile: the package doesn't exist yet.
- [ ] **Step 3: Implement** in `token.go`. Copy the shape of `roster/token.go`:
  - `checkSignature` parses leniently, only for `v` and `kid`;
  - after the signature has passed, the strict parse:
    1. `wire.StrictObject`;
    2. the required-and-not-null check over `map[string]jsontext.Value`;
    3. unmarshalling with `json.RejectUnknownMembers(true)`;
  - then `validate`, the static rules that `Sign` shares;
  - then `checkTrust`: issuer, audience and the clock.

  `Sign` runs `validate`, marshals, re-runs the strict parse on its own bytes, and signs. `Redact` replaces `rastrillo_grant_[A-Za-z0-9._-]+` with `rastrillo_grant_[redacted]`. Generate `golden.json` once, with a throwaway key and `iat` 1700000000, and commit it.
- [ ] **Step 4: Run them and see them pass,** then `go vet ./granttoken` and `go run ./cmd/rastrillo budget code`.
- [ ] **Step 5: Commit:** `granttoken: sign and verify grant tokens (rastrillo/grant/v1)`.

### Task 2: The roster carries each member's active grants

**Files:**
- Modify: `roster/token.go` (`Member`, `validate`), `roster/receiver.go` (`syncMembers`), `roster/record.go` (`roleOnly`)
- Create: `roster/grants.go`, `roster/grants_test.go`, `roster/compat_test.go`
- Modify (tests): `roster/token_test.go`, `roster/receiver_test.go`, `roster/record_test.go`

**Interfaces:**
- Produces:

```go
type Member struct {
	Subject string   `json:"sub"`
	Email   string   `json:"email"`
	Name    string   `json:"name"`
	Role    string   `json:"role"`
	Grants  []string `json:"grants,omitempty"`
}

// GrantActive reports whether the latest applied snapshot lists grant
// under subject on an active instance.
func (receiver *Receiver) GrantActive(
	ctx context.Context, subject, grant string,
) (bool, error)
```

- `Member` stops being comparable with `==`. `Store.Put` receives `Grants` and may ignore them: the receiver keeps them in its own row.

- [ ] **Step 1: Write the failing tests.** Use `receiver_test.go`'s `newFixture` and `fakeStore`. Make `fakeStore.Members` return members without `Grants`, as Patrix's store does, and count calls to `Put`.
  - `TestSignedSnapshotCarriesGrants`: a member with `Grants: []string{"g1","g2"}` round-trips through `Sign` and `Verify`.
  - `TestVerifyRefusesBadGrants`, each → `ErrClaims` from both `Sign` and `Verify`:
    - an empty ID;
    - a 65-character ID;
    - `"g 1"`;
    - `g1` twice under one member;
    - `g1` under two members;
    - 65 grants.
  - `TestGrantActive`:
    - before any snapshot, `(A, g1)` is false;
    - after seq 1 lists A with `[g1]`, `(A, g1)` is true and `(A, g2)` is false;
    - after seq 2 lists A with no grants, `(A, g1)` is false.
  - `TestGrantActiveIsPerSubject`: seq 1 lists A with `[]` and B with `[g1]`. `(A, g1)` is false, and `(B, g1)` is true.
  - `TestGrantActiveFalseWhenSuspended`: seq 1 is `Suspended` and lists A with `[g1]`. `(A, g1)` is false.
  - `TestGrantsSurviveRolesOnlyAndRepair`. With `RecordRolesOnly: true`:
    1. seq 2 lists A with `[g1]`, and `(A, g1)` is true;
    2. posting seq 1 answers 409 and runs the repair;
    3. `(A, g1)` is still true.

    Repeat the whole case with `RecordRolesOnly: false`.
  - `TestRevokedGrantStaysRevokedAfterStaleReplay`. In both recording modes:
    1. seq 2 lists A with `[g1]`;
    2. seq 3 lists A with no grants;
    3. replaying seq 2 answers 409 and the receiver repairs from its row;
    4. `(A, g1)` is false;
    5. a new `Receiver` built over the same database also answers false for `(A, g1)`.
  - `TestRemovedMemberGrantStaysRevokedAfterStaleReplay`: the same five steps, except that seq 3 leaves A out entirely. After the replay, `(A, g1)` is false on both receivers, and the store does not hold A.
  - `TestGrantsAloneDoNotRewriteStore`: seq 1 lists A with `[g1]`, then seq 2 lists A with `[g1, g2]` and the same email, name and role. The second apply calls `Put` zero times.
  - `compat_test.go`:
    - `TestSnapshotWithoutGrantsSignsTheSameBytes`: `json.Marshal` of `testdata/golden.json`'s snapshot equals the payload bytes decoded from its token.
    - `TestOldReceiverRefusesGrants` keeps a frozen copy of v0.46.0's `Member` and `Snapshot` (no `Grants`) and strict-parses with `json.RejectUnknownMembers(true)`. A payload with `"grants"` is refused, and the same payload without it parses. The test's comment names the rollout order.
    - `TestNewReceiverAcceptsOldSnapshot`: the golden token applies, and `GrantActive` is false for every member.
  - **Update the existing tests so that they compile once `Member` holds a slice.** Leave each assertion's meaning as it is:
    - `roster/token_test.go` `goodSnapshot`: the two positional `Member` literals become keyed (`Subject:`, `Email:`, `Name:`, `Role:`);
    - `roster/receiver_test.go`, lines 219-220, 238, 253, 349 and 515, and `roster/record_test.go`, lines 70, 134, 153, 168 and 176: each `==` or `!=` on a `roster.Member` becomes `reflect.DeepEqual`. That keeps comparing `Grants`, so a test that sets grants also checks them.
- [ ] **Step 2: Run them and see them fail:** `GOFLAGS=-mod=mod go test ./roster -run 'Grant|Compat|OldReceiver|NewReceiver|WithoutGrants|Revoked|Removed'`. The build fails on `Grants` and `GrantActive`.
- [ ] **Step 3: Implement:**
  - `Member.Grants`, and its rules in `validate`: one snapshot-wide seen set, and `maxGrants = 64` per member.
  - `sameMember(existing, member Member) bool` in `receiver.go`, comparing `Subject`, `Email`, `Name` and `Role`, in place of `existing == member`.
  - Find every other `==` on `Member`, in tests too: `GOFLAGS=-mod=mod go test ./... -run '^$'` must compile every package. Fix each with `reflect.DeepEqual`, or with `sameMember` where the comparison means "the store's copy is current".
  - `roleOnly` gains `Grants []string` with the tag `json:"grants,omitempty"`.
  - `GrantActive` in `grants.go`: one `SELECT state, members FROM roster_state WHERE id = 1`. No row, or `Suspended`, answers false. Otherwise, unmarshal into a struct with only `sub` and `grants`, and look for the subject's grant.
  - Update the package comment's "carries no credentials". Grant IDs are not credentials: only a signed grant token uses one.
- [ ] **Step 4: Run them and see them pass,** plus `go test ./roster ./handoff/...`, `go test ./migrate -run Frozen` and `GOFLAGS=-mod=mod go test ./... -run '^$'`.
- [ ] **Step 5: Commit:** `roster: each member's active grant IDs, and GrantActive`.

### Task 3: The door, and the actor it names

**Files:**
- Create: `granttoken/door.go`, `granttoken/door_test.go`
- Modify: `ctx.go` (`Actor.String`), `ctx_test.go`, and `api/tree.go` (the `Verifier` comment names `granttoken.Door`)

**Interfaces:**
- Consumes: `Verifier`, `Claims`, `Read`, `Write` and `Prefix` from Task 1. `*roster.Receiver` satisfies `Grants` (Task 2).
- Produces:

```go
// Grants says whether subject still holds grant. A site passes its
// *roster.Receiver; the root passes its own grant records.
type Grants interface {
	GrantActive(ctx context.Context, subject, grant string) (bool, error)
}

type DoorConfig struct {
	Verifier *Verifier
	Grants   Grants
	Now      func() time.Time // nil means time.Now
}

var ErrRevoked = errors.New("granttoken: grant not active")

func NewDoor(config DoorConfig) (*Door, error) // ErrConfig when a part is nil
func (door *Door) Verify(
	ctx context.Context, request *http.Request,
) (rastrillo.Actor, error)
func (door *Door) Recheck(
	ctx context.Context, request *http.Request, was rastrillo.Actor,
) (rastrillo.Actor, error)
```

- `Actor.String` leaves out ` token:<credential>` when `Grant != nil && Grant.ID == Credential`. An OAuth actor's credential is its token's public ID, so its string is unchanged.

- [ ] **Step 1: Write the failing tests** in `door_test.go`. Use a `fakeGrants` that records each `(subject, grant)` and answers from a map or an error.
  - `TestDoorVerifiesAGrantToken`: a write token for `person_1`, `g1`, `Claude` and `https://site.test` gives exactly:
    - `Actor{Subject: "person_1", Agent: "Claude", Credential: "g1", Access: rastrillo.Write}`;
    - `Grant{ID: "g1", Issuer: "https://root.test", IdentityIssuer: "https://root.test", Audience: "https://site.test"}`;
    - empty `Session` and `Harness`;
    - `String() == "agent:Claude for user:person_1 grant:g1"`.
  - `TestDoorReadGrantIsRead`: `access: read` gives `rastrillo.Read`.
  - `TestDoorRefusesRevokedGrant`: `fakeGrants` answers false. Both `Verify` and `Recheck` return an error that `errors.Is` `api.ErrUnauthorized` and `ErrRevoked`.
  - `TestDoorAsksForTheTokensSubject`: `fakeGrants` was asked exactly `("person_1", "g1")`.
  - `TestDoorChecksSignatureFirst`: a token signed by a stranger's key gets `api.ErrUnauthorized`, and `fakeGrants` records no call.
  - `TestDoorUnavailable`: `fakeGrants` errors. The result wraps `api.ErrUnavailable` and not `api.ErrUnauthorized`.
  - `TestDoorReadsOnlyTheAuthorizationHeader`, each → `api.ErrUnauthorized`:
    - the token in a cookie;
    - the token in `?access_token=`;
    - two `Authorization` headers;
    - `Basic` scheme.

    The lowercase scheme `bearer` passes.
  - `TestDoorExpiresWhileInUse`: `Verify` passes. After `Now` moves to `exp`, `Recheck` returns `api.ErrUnauthorized`.
  - `TestNewDoorRefusesMissingParts`.
  - In `ctx_test.go`, `TestActorStringGrantToken`: `Credential == Grant.ID` gives `agent:Claude for user:s grant:g1`. `TestActorStringWithGrant` must pass unchanged.
- [ ] **Step 2: Run them and see them fail:** `GOFLAGS=-mod=mod go test ./granttoken -run Door` and `go test . -run TestActorString`.
- [ ] **Step 3: Implement.**
  - `Verify` and `Recheck` share one side-effect-free `check(ctx, request)`, so a rule added to one cannot be missed by the other, as `apitoken.lookup` does.
  - `bearer(request) (string, error)` follows `apitoken`'s `credential` rule: exactly one `Authorization` header, with the scheme compared case-insensitively.
  - Map errors as Global Constraints say.
- [ ] **Step 4: Run them and see them pass,** plus `go test ./api -run 'Gate|Emit'`.
- [ ] **Step 5: Commit:** `granttoken: Door, the api.Verifier for grant tokens`.

### Task 4: One verifier for grant tokens and personal tokens

A hosted Patrix site serves its team with grant tokens and its readers with per-site tokens (§5.4) on the same doors. `api.Config` and `api.Gate` take a single `Verify`, and `Redact` is a single function.

**Files:**
- Create: `granttoken/either.go`, `granttoken/either_test.go`

**Interfaces:**
- Consumes: `Door` (Task 3), `Prefix` and `Redact` (Task 1).
- Produces:

```go
// Fallback is what *apitoken.Store already is.
type Fallback interface {
	api.Verifier
	api.Rechecker
	Redact(text string) string
}

func (door *Door) Or(others Fallback) *Either
func (either *Either) Verify(
	ctx context.Context, request *http.Request,
) (rastrillo.Actor, error)
func (either *Either) Recheck(
	ctx context.Context, request *http.Request, was rastrillo.Actor,
) (rastrillo.Actor, error)
func (either *Either) Redact(text string) string
```

- **The rule:** a request with exactly one `Authorization` header, whose Bearer token starts with `Prefix`, goes to the door. Every other request goes to `others`, which refuses a malformed one itself. `Redact` is `others.Redact(Redact(text))`.

- [ ] **Step 1: Write the failing tests** with a `fakeFallback` that counts calls.
  - `TestEitherSendsGrantTokensToTheDoor`: a good grant token verifies through the door, and the fallback is never called, for both `Verify` and `Recheck`.
  - `TestEitherSendsOtherTokensOn`: `Bearer tickets_…` reaches the fallback, and its actor is returned unchanged.
  - `TestEitherNeverFallsThrough`. Each of these returns the door's 401, and the fallback records no call:
    - a grant token for the dashboard's audience, presented to a site;
    - a revoked grant;
    - `rastrillo_grant_garbage`.
  - `TestEitherRedactsBoth`: text holding a grant token and a fallback-shaped secret comes back with both redacted.
  - `var _ api.Rechecker = (*Either)(nil)`, and the same assertion for `*apitoken.Store` as a `Fallback`.
- [ ] **Step 2: Run them and see them fail:** `GOFLAGS=-mod=mod go test ./granttoken -run Either`.
- [ ] **Step 3: Implement** in `either.go`.
- [ ] **Step 4: Run them and see them pass.**
- [ ] **Step 5: Commit:** `granttoken: Either serves grant and personal tokens on one door`.

### Task 5: End to end: a snapshot revokes, mid-answer too

**Files:**
- Modify: `handoff/handofftest/handofftest.go` (`GrantToken`)
- Create: `granttoken/flow_test.go`, package `granttoken_test`. It must be an external test package, because `handofftest` imports `granttoken`.

**Interfaces:**
- Consumes: every earlier task's interfaces, plus `api.New`, `op.DefaultPolicy` and `roster.New`.
- Produces:

```go
// GrantToken signs claims as this root, filling Version, Issuer, KeyID
// and IssuedAt, and ExpiresAt as an hour from now when it is zero.
func (root *Root) GrantToken(
	t testing.TB, claims granttoken.Claims,
) string
```

- **The fixture:**
  - `handofftest.New(t)` as the root, with the site at `https://site.test`;
  - a `roster.Receiver` over a test database, as `roster/receiver_test.go` builds one;
  - a `Door` whose `Grants` is that receiver;
  - an `api.New` tree with `RequireAgentSession: true` and `Authorize: op.DefaultPolicy`, served by `httptest.NewServer`, because the recheck needs a real connection for its write deadlines. Build its routes the way `api/tree_test.go` does:
    - `GET /api/v1/things`, a read answering 200 KiB;
    - `POST /api/v1/things`, a write;
    - `POST /api/v1/grants/widen`, a write marked `HumanOnly`.
  - Seq 1 lists `owneruid` as `owner` with `Grants: ["g1"]`, posted to `roster.Path`.

- [ ] **Step 1: Write the failing tests:**
  - `TestFlowGrantTokenReadsAndWrites`:
    - GET answers 200;
    - POST with `Agent-Session: s1` succeeds;
    - the handler's `rastrillo.ActorFromContext` gives `agent:Claude for user:owneruid grant:g1`, with `Session == "s1"`.
  - `TestFlowRevokedBySnapshot`: after seq 2 drops `g1`, the same GET answers 401.
  - `TestFlowRevokedDuringTheHandler`: the GET handler posts seq 2 to the receiver before writing. The client gets 401 and none of the 200 KiB.
  - `TestFlowRevokedMidAnswer`. The `Grants` wraps the receiver. Once it has answered twice (`Verify`, then the recheck before the headers), it posts seq 2, so the next recheck finds the grant gone. Assert, as `api/emit_test.go`'s `TestEmitRecheckMidBody` does:
    - status 200;
    - `Content-Length` equal to 204800;
    - a read error of `io.ErrUnexpectedEOF`;
    - at most 64 KiB of body.
  - `TestFlowReadGrantCannotWrite`: POST with a read token answers 403.
  - `TestFlowWriteNeedsAgentSession`: POST with no `Agent-Session` answers 422.
  - `TestFlowHumanOnlyRefused`: the human-only route answers 403 with the `op.HumanOnly` refusal, even for a write grant.
  - `TestFlowOtherSitesTokenRefused`: `aud` `https://other.test` answers 401.
  - `TestFlowSuspendedSite`: after a seq that is `Suspended` and still lists `g1`, the GET answers 401.
- [ ] **Step 2: Run them and see them fail:** `GOFLAGS=-mod=mod go test ./granttoken -run Flow`. The build fails on `GrantToken`.
- [ ] **Step 3: Implement `GrantToken`.** Any other failure is a bug in an earlier task's file. Fix it there, with a test there.
- [ ] **Step 4: Run them and see them pass,** plus `go test ./handoff/...`.
- [ ] **Step 5: Commit:** `granttoken: end-to-end revocation by roster snapshot`.

### Task 6: Docs and changelog, through Luna

**Files:**
- Create: `docs/site/reference/granttoken.md`
- Modify:
  - `internal/docsite/symbols_test.go`: add `"granttoken": "reference/granttoken"`;
  - `docs/site/nav.json`: add `granttoken` between `gormlite` and `handoff`, in alphabetical order, with a one-line blurb;
  - `docs/site/reference/roster.md`: `Member.Grants`, `GrantActive`, the "carries no credentials" sentence and the upgrade order;
  - `docs/site/reference/handofftest.md`: `GrantToken`;
  - `docs/site/reference/api.md`: `Verifier` implementations;
  - `CHANGELOG.md`.

- [ ] **Step 1: Make the docs test fail:** add the `symbols_test.go` entry, then run `go test ./internal/docsite -run 'TestExportedSymbolsAreDocumented|TestEveryPackageHasAReferencePage|Nav'`. It reports that there is no `reference/granttoken` page.
- [ ] **Step 2: Draft the pages.** `granttoken.md` covers:
  - what a grant token is;
  - mounting the door on a hosted site:
    - `granttoken.NewVerifier(setup.Root, platformHost, setup.Keys)`;
    - `NewDoor` with the receiver as `Grants`;
    - `Verify: door.Or(tokens)` and `Redact: door.Or(tokens).Redact` on both `api.Config` and the MCP gate;
  - the checks, in order;
  - the actor;
  - the 401 and 503 answers;
  - what a read grant and the human-only operations refuse;
  - the root's own door, where `iss == aud`.

  Under `## Unreleased`, the CHANGELOG has `### Added: grant tokens`. Ahead of it come two call-outs:
  - **Upgrade order:** sites first, then the root sends grants;
  - **`roster.Member` is no longer comparable with `==`.**
- [ ] **Step 3: Luna review.** Gather every new or changed prose line into one prompt. Run `codex exec -m gpt-6-luna -c model_reasoning_effort=high -s read-only --skip-git-repo-check -o <out> - < <prompt>` with the sandbox off. Apply every correction that is not factually wrong. Post the strings, the whole reply and what was applied in a discussion on the branch.
- [ ] **Step 4: Run** the Step 1 command and `go test ./internal/docsite`. They pass.
- [ ] **Step 5: Commit:** `docs: granttoken reference, roster grants, changelog`.

### Task 7: Gate, review, land, release v0.47.0

- [ ] **Step 1: Run the local gate:**
  - `gofmt -l .` prints nothing;
  - `go vet ./...`;
  - `go run ./cmd/rastrillo budget code`;
  - `make budget`;
  - `GOFLAGS=-mod=mod go test ./granttoken/... ./roster/... ./api/... ./apitoken/... ./mcp/... ./handoff/... ./internal/docsite/... ./migrate/...`;
  - `go test . -run 'TestActor'`;
  - `GOFLAGS=-mod=mod go test ./... -run '^$'`, which compiles every test.
- [ ] **Step 2: Show each test failing before the change.** CONTRIBUTING §3: in a worktree at 3b13cb06, with stubs where a test cannot compile, run Tasks 1 to 5's tests, and post the command and both outputs on the branch.
- [ ] **Step 3: Get a cross-provider review.** Astra (`codex exec -m gpt-6-astra`) reviews the full diff and this plan. Post the prompt, the whole reply, and each finding with what was done about it.
- [ ] **Step 4: Run fleet CI.** Push `grant-tokens` and get `make ci` green on the fleet. If `origin/main` has moved, merge it in and run CI again.
- [ ] **Step 5: Land.** Run `amadan branch merge rastrillo/rastrillo grant-tokens -squash -expect <full sha> -message` with the subject `granttoken: grant tokens a site verifies against the roster` (under 80 characters), a body, and the `Co-Authored-By` trailer.
- [ ] **Step 6: Release v0.47.0** by the release recipe:
  1. a `release-v0.47.0` branch that moves `## Unreleased` to `## v0.47.0`;
  2. squash-land it;
  3. an annotated tag on the squash;
  4. `make release TAG=v0.47.0` from a detached worktree at the tag;
  5. `make mirror`;
  6. the site sync.
- [ ] **Step 7: Hand off to steps 2 and 3:**
  - **plaza (step 2):** add a switch, off by default, that puts grant IDs in snapshots. Turn it on only after `patrix-sites` runs v0.47.0. Mint grant IDs with `assertion.RandomID`, and mirror the new roster rules in `checkRoster`.
  - **Patrix (step 3):** mount the door for hosted sites with `door.Or(tokens)`.
