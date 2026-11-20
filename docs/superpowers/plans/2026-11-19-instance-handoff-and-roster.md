# Instance handoff and roster Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give every Rastrillo instance the core it needs to live under a
root app: knowing whether it is rooted or standalone, accepting a signed
handoff from the root and minting its own session, and accepting signed
roster snapshots that it applies to the app's own roster.

**Architecture:** Three new core packages and one test helper.
- `instance` reads the environment and decides rooted or standalone,
  refusing to start when neither is set.
- `handoff` is the instance side of the user-models §6.2 flow. It wraps
  the existing `assertion` verifier and keeps its replay store in the
  instance's SQLite.
- `roster` signs and verifies whole-roster snapshots. Its receiver diffs
  each snapshot against an app-supplied `Store`, so roles stay the app's
  own.
- `handofftest` plays the root in tests.

Everything is instance-side, apart from `roster.Sign`, which ships beside
its verifier so the format has one home. The root side (`/assert`,
roster delivery) is plaza's (roadmap Plan 3).

**Tech Stack:** Go 1.26, stdlib `net/http`, `database/sql` on SQLite,
Rastrillo's `assertion`, `crypto`, `sessions`, `migrate`, `basepath` and
`csrf`.

**Spec:** `docs/superpowers/specs/2026-11-19-multi-instance-apps-design.md`
(§5.2 standalone, §5.4 who signs in where, §6.2 handoff, §6.3 roster,
§7.3 session life on subdomains). Also user-models §6.2, read with
`git -C ../../carlos/pegamento show origin/design-user-models:docs/superpowers/specs/2026-09-27-user-models-design.md`.

## Approved copy

Reviewed by Luna on 2026-11-19 (Task 0). Use these strings verbatim, with straight apostrophes.

- **Handoff refusal page:**
  - title: `We couldn't verify that link`
  - sentence: `Go back and open your account again.`
  - link text (to the root origin): `Go to the main site`
- **Suspended account page:**
  - title: `This account is suspended`
  - sentence: `No one can sign in to this account right now. If this is your account, contact the person who runs the service.`
- **`instance.FromEnv` errors:**
  - neither mode set: `rastrillo/instance: set RASTRILLO_ROOT and RASTRILLO_ROOT_KEYS to use a root app, or set RASTRILLO_STANDALONE=1 to run on its own`
  - both set: `rastrillo/instance: RASTRILLO_STANDALONE=1 and RASTRILLO_ROOT are both set; set only one`
  - root without keys: `rastrillo/instance: RASTRILLO_ROOT is set but RASTRILLO_ROOT_KEYS is empty; set the root app's public keys`
  - keys without root: `rastrillo/instance: RASTRILLO_ROOT_KEYS is set but RASTRILLO_ROOT is empty; set the root app's origin`
  - bad origin: `rastrillo/instance: RASTRILLO_ROOT must be an HTTPS origin with no path, for example https://example.com`
  - bad keys: `rastrillo/instance: RASTRILLO_ROOT_KEYS: %w`, wrapping the parser's error.

## Global Constraints

- **No new module dependencies.** Use what `go.mod` already requires.
- **Code style:** follow `skill/code.md`. Names are spelled out (except
  `err`), lines stay within 80 columns, and comments say why, never what.
- **Wire-format checks:** each package's errors carry no submitted values,
  as `assertion` does.
- **One root key signs everything.** Root public keys use the format
  `sessions.ParsePublicKeys` reads (`<kid>.<base64url>` pairs), the same
  keys the hub's signed sessions use.
- **Routes:**
  - Handoff lives at `/_carlos/enter`: GET starts it and POST consumes
    it.
  - The roster receiver lives at `/_carlos/roster` and takes POST only.
  - Both answer only on the instance's own origin. A path-routed request
    (`basepath.PathRouted(r)`) gets a 404 from `handoff`, because path
    tenants use the hub session.
- **The handoff session's Method** is the string `"handoff"`.
- **Session life:** a handoff session renews after `MaxAge`, default
  `time.Hour` (spec §7.3).
- **Assertion lifetime:** at most 120 seconds, already enforced by
  `assertion`.
- **Roster snapshots:**
  - signing context `"rastrillo/roster/v1"`;
  - version `1`;
  - lifetime at most 300 seconds;
  - token at most 1 MiB.
- **Environment variables:**
  - `RASTRILLO_ROOT` is the root origin.
  - `RASTRILLO_ROOT_KEYS` holds the root's public keys.
  - `RASTRILLO_STANDALONE=1` selects standalone.
  - If neither mode is set, startup fails. If both are set, startup also
    fails.
- **User-facing copy:** every string goes through copy review (Task 0)
  before it is written into a file. No em dashes as separators.
- **Testing locally:** targeted runs only (`go test ./handoff/...
  -run X`). Set `GOFLAGS=-mod=mod`. CI runs the full gate.

## Review Focus

1. **The nonce cookie must come back on the root's cross-site POST.** A
   `SameSite=Lax` cookie is not sent on a cross-site form POST. The cookie
   must be `SameSite=None; Secure; HttpOnly`, or every real handoff fails,
   even though same-site tests pass. *Test:* Task 4,
   `TestEnterSetsANonceCookieTheRootsPostWillCarry`.
2. **An assertion replayed after sign-out.** The `jti` row must outlive
   the session. *Test:* Task 5, `TestConsumeRefusesAReplayAfterSignOut`.
3. **`next` as an open redirect** (`//evil.example`,
   `https://evil.example/`, `/\evil`) must land on `/`. *Test:* Task 4,
   `TestEnterKeepsOnlyALocalNext`.
4. **Snapshots arriving out of order.** An older sequence after a newer
   one is refused and changes nothing. The same sequence twice is
   accepted, and applying it again has no further effect. *Test:* Task 3,
   `TestReceiverRefusesAnOlderSequence` and
   `TestReceiverAcceptsTheSameSequenceTwice`.
5. **A suspended account.** Consume refuses it even with a valid
   assertion, and a later `active` snapshot lets the next handoff
   through. *Test:* Task 5, `TestConsumeRefusesASuspendedAccount`.

---

### Task 0: Copy review

**Files:** none until approved.

- [ ] **Step 1:** Extract every user-facing string this plan writes:
  - the handoff refusal page (title, one sentence, and a link back to
    the root);
  - the suspended-account refusal;
  - the `instance.FromEnv` error messages, which operators read.

  Run them through the copy-review skill and wait for Paul's edits.
- [ ] **Step 2:** Record the approved strings verbatim at the top of this
  plan, under "Approved copy". Later tasks use them exactly.

### Task 1: `instance`: rooted or standalone

**Files:**
- Create: `instance/instance.go`
- Test: `instance/instance_test.go` (`package instance_test`)

**Interfaces:**
- Produces:
  ```go
  type Mode int
  const (Standalone Mode = iota + 1; Rooted)
  type Setup struct {
      Mode Mode
      Root string             // root origin when Rooted, e.g. https://example.com
      Keys map[string][]byte  // root public keys by kid when Rooted
  }
  func FromEnv(getenv func(string) string) (Setup, error)
  ```

- [ ] **Step 1: Write the failing tests.** Each one builds `getenv` from
  a map:
  - `TestFromEnvRootedReadsTheRootAndItsKeys`: `RASTRILLO_ROOT=https://example.com`
    and `RASTRILLO_ROOT_KEYS` holding a key made with
    `sessions.NewSigningKey("k1")` and `.Public()`. Expect `Mode == Rooted`,
    `Root == "https://example.com"` and `len(Keys) == 1`.
  - `TestFromEnvStandalone`: `RASTRILLO_STANDALONE=1` gives
    `Mode == Standalone`, with `Root` and `Keys` empty.
  - `TestFromEnvRefusesNeither`: an empty environment gives an error
    whose message names all three variables.
  - `TestFromEnvRefusesBoth`: both modes set gives an error.
  - `TestFromEnvRefusesARootWithoutKeys`, and the reverse case.
  - `TestFromEnvRefusesARootThatIsNotAnHTTPSOrigin`: `http://`, a path,
    and a trailing slash each give an error.
- [ ] **Step 2:** Run `go test ./instance/ -run FromEnv`. Expect FAIL (no
  package).
- [ ] **Step 3: Implement `FromEnv`.** Keys go through
  `sessions.ParsePublicKeys`. The origin check accepts a scheme of
  `https` and a host only. Error copy comes from Task 0.
- [ ] **Step 4:** Run the same command. Expect PASS.
- [ ] **Step 5:** Commit: `instance: rooted or standalone, from the environment`.

### Task 2: `roster` token: sign and verify

**Files:**
- Create: `roster/token.go`
- Create: `roster/testdata/golden.json`
- Test: `roster/token_test.go` (`package roster`, internal, as
  `assertion` is, for the vectors)

**Interfaces:**
- Consumes: `crypto.Keypair`, `crypto.Sign`, `crypto.Verify`.
- Produces:
  ```go
  type State string
  const (Active State = "active"; Suspended State = "suspended")
  type Member struct {
      Subject string `json:"sub"`
      Email   string `json:"email"`
      Name    string `json:"name"`
      Role    string `json:"role"`
  }
  type Snapshot struct {
      Version   int64    `json:"v"`
      Issuer    string   `json:"iss"`
      Audience  string   `json:"aud"`
      KeyID     string   `json:"kid"`
      Sequence  int64    `json:"seq"`
      IssuedAt  int64    `json:"iat"`
      ExpiresAt int64    `json:"exp"`
      State     State    `json:"state"`
      Members   []Member `json:"members"`
  }
  func Sign(key *crypto.Keypair, snapshot Snapshot) (string, error)
  type Verifier struct{ /* issuer, audience, keys */ }
  func NewVerifier(issuer, audience string, keys map[string][]byte) (*Verifier, error)
  func (v *Verifier) Verify(token string, now time.Time) (Snapshot, error)
  var ErrMalformed, ErrVersion, ErrUnknownKey, ErrSignature, ErrIssuer,
      ErrAudience, ErrLifetime, ErrExpired, ErrClaims, ErrConfig error
  ```
  The token is `base64url(payload) "." base64url(signature)`, the same
  shape as an assertion, but under its own signing context so neither
  token can be replayed as the other.

- [ ] **Step 1: Write the failing tests.**
  - `TestSignedSnapshotVerifies`: a round trip in which every field
    survives.
  - `TestVerifyRefuses`: table-driven, one row per error, each asserted
    with `errors.Is`. The rows cover:
    - a wrong issuer, a wrong audience, or an unknown kid;
    - a flipped signature byte;
    - `exp-iat > 300`, or `exp <= now`;
    - `Version != 1`;
    - `Sequence < 1`;
    - a `State` that is neither value;
    - a duplicate `Subject` among `Members`;
    - an empty `Subject` or `Role`;
    - a token over 1 MiB.
  - `TestAnAssertionIsNotASnapshot`: a token from `assertion.Sign` with
    the same key fails `Verify` with `ErrSignature`.
  - `TestGoldenVector`: `testdata/golden.json` holds a fixed key, a
    snapshot and its token. `Verify` at the vector's `now` returns the
    snapshot. Generate the vector once with a fixed key and commit it.
    The test pins the format, it doesn't regenerate it.
- [ ] **Step 2:** Run `go test ./roster/ -run 'Signed|Verify|Assertion|Golden'`.
  Expect FAIL.
- [ ] **Step 3: Implement.** Mirror `assertion`'s structure:
  - strict JSON with unknown fields refused;
  - `KeyID` from the token;
  - `crypto.Sign` and `crypto.Verify` with context `"rastrillo/roster/v1"`.
- [ ] **Step 4:** Run the same command. Expect PASS.
- [ ] **Step 5:** Commit: `roster: signed whole-roster snapshots`.

### Task 3: `roster` receiver

**Files:**
- Create: `roster/receiver.go`
- Create: `roster/migrations/0001_roster_state.sql`: table
  `roster_state(id INTEGER PRIMARY KEY CHECK (id = 1), sequence INTEGER
  NOT NULL, state TEXT NOT NULL)`.
- Test: `roster/receiver_test.go` (`package roster_test`), using
  `dbtest` for SQLite with `Schema` applied.

**Interfaces:**
- Consumes: Task 2.
- Produces:
  ```go
  const Path = "/_carlos/roster"
  var Schema = migrate.MustFromFS(migrationFS, "roster")
  type Store interface {
      Members(ctx context.Context) ([]Member, error)
      Put(ctx context.Context, member Member) error
      Remove(ctx context.Context, subject string) error
  }
  type Config struct {
      Verifier *Verifier
      Store    Store
      DB       *sql.DB
      Roles    []string // the app's roles, highest first
      Now      func() time.Time
      Logger   *slog.Logger
  }
  func New(config Config) (*Receiver, error)
  func (r *Receiver) ServeHTTP(w http.ResponseWriter, req *http.Request)
  func (r *Receiver) Suspended(ctx context.Context) (bool, error)
  ```

- [ ] **Step 1: Write the failing tests.** Each uses an in-memory fake
  `Store` and tokens from `roster.Sign`.
  - `TestReceiverAppliesASnapshot`: the store starts with `{a: owner,
    b: member}`, and the snapshot is `{a: owner, c: editor}`, seq 1.
    Expect 204. The store ends as `{a, c}`: `b` removed, `c` put, and
    `a` not rewritten, because unchanged members cause no Put.
  - `TestReceiverRefusesAnOlderSequence`: seq 2 then seq 1. Expect 409,
    with the store and state unchanged.
  - `TestReceiverAcceptsTheSameSequenceTwice`: seq 3 twice. Both return
    204, and the second causes no Put or Remove.
  - `TestReceiverRefusesAnUnknownRole`: expect 422 with nothing applied.
  - `TestReceiverRecordsSuspension`: a `Suspended` snapshot makes
    `Suspended` true, and a later `Active` one makes it false.
  - `TestReceiverRefusesABadToken`: a wrong audience gives 403, and a
    GET gives 405.
  - `TestReceiverRetriesAfterAStoreFailure`: the store fails on Put, so
    the response is 500 and the stored sequence is unchanged. The same
    token then succeeds.
  - `TestSuspendedIsFalseBeforeAnySnapshot`.
- [ ] **Step 2:** Run `go test ./roster/ -run 'Receiver|Suspended'`.
  Expect FAIL.
- [ ] **Step 3: Implement.**
  - **Request:** the body is the raw token, read through
    `http.MaxBytesReader` at 1 MiB.
  - **Order:** verify, then check the sequence, then validate roles,
    then diff and apply, then write the state.
  - **One at a time:** a `sync.Mutex` serialises applies in-process. The
    state write is `INSERT ... ON CONFLICT(id) DO UPDATE SET sequence =
    excluded.sequence, state = excluded.state WHERE excluded.sequence >=
    roster_state.sequence`, so a second process can't move the sequence
    backwards.
  - **Retries:** the state is written only after every Put and Remove
    succeeds, so a failed apply leaves the sequence where it was and a
    retry re-applies the snapshot.
- [ ] **Step 4:** Run the same command. Expect PASS.
- [ ] **Step 5:** Commit: `roster: receiver that applies snapshots to the app's store`.

### Task 4: `handoff`: New and Enter

**Files:**
- Create: `handoff/handoff.go`
- Create: `handoff/enter.go`
- Create: `handoff/migrations/0001_handoff_used.sql`: table
  `handoff_used(jti TEXT PRIMARY KEY, expires_at INTEGER NOT NULL)`.
- Test: `handoff/enter_test.go` (`package handoff_test`).

**Interfaces:**
- Consumes: `instance.Setup` (Task 1), `sessions.Sessions`.
- Produces:
  ```go
  const EnterPath = "/_carlos/enter"
  const Method = "handoff"
  var Schema = migrate.MustFromFS(migrationFS, "handoff")
  type Config struct {
      Root      string            // instance.Setup.Root
      Keys      map[string][]byte // instance.Setup.Keys
      Origin    string            // this instance's origin
      Sessions  *sessions.Sessions
      DB        *sql.DB
      MaxAge    time.Duration     // default time.Hour
      Suspended func(context.Context) (bool, error) // optional: Receiver.Suspended
      Now       func() time.Time
      Logger    *slog.Logger
  }
  func New(config Config) (*Handoff, error)
  func (h *Handoff) Enter(w http.ResponseWriter, r *http.Request)
  ```
  The nonce cookie is called `__Host-carlos-enter`. Its value is
  `<nonce>.<base64url(next)>`, and it carries `Path=/`, `Max-Age=300`,
  `Secure`, `HttpOnly` and `SameSite=None`.
  Enter answers 303 to
  `<Root>/assert?aud=<url-escaped Origin>&nonce=<nonce>&next=<url-escaped next>`.

- [ ] **Step 1: Write the failing tests.**
  - `TestNewRefusesABadConfig`: covers a missing Root, Keys, Origin,
    Sessions or DB, `Root == Origin`, and a Sessions in signed mode.
    Signed mode means a path tenant, which never uses handoff.
  - `TestEnterSetsANonceCookieTheRootsPostWillCarry`: asserts the
    cookie's name, `SameSite == http.SameSiteNoneMode`, `Secure`,
    `HttpOnly`, `Path == "/"` and `MaxAge == 300`.
  - `TestEnterRedirectsToTheRootsAssert`: the redirect's scheme and host
    are the root's, its path is `/assert`, and `aud`, `nonce` and `next`
    round-trip. The nonce in the URL equals the nonce in the cookie.
  - `TestEnterKeepsOnlyALocalNext`: `next` values of `//evil.example`,
    `https://evil.example/` and `/\evil` become `/`. `/events/1?x=2` is
    kept.
  - `TestEnterIsNotFoundUnderABasePath`: the handler is served through
    `basepath.Config{Slug: "acme", Origin: <hub origin>}.Handler` and
    requested at `/acme/_carlos/enter`. Expect 404 and no cookie.
- [ ] **Step 2:** Run `go test ./handoff/ -run 'New|Enter'`. Expect FAIL.
- [ ] **Step 3: Implement.**
  - The nonce is `assertion.RandomID()`.
  - `next` is sanitised with `sessions.SafeReturnTo(next, "/")`.
- [ ] **Step 4:** Run the same command. Expect PASS.
- [ ] **Step 5:** Commit: `handoff: start a handoff at the root`.

### Task 5: `handoff`: Consume

**Files:**
- Create: `handoff/consume.go`
- Test: `handoff/consume_test.go`

**Interfaces:**
- Produces: `func (h *Handoff) Consume(w http.ResponseWriter, r *http.Request)`,
  mounted at `POST EnterPath`. The app exempts `EnterPath` from CSRF with
  `csrf.Exempt(handoff.EnterPath)`. This POST is cross-site by design,
  and the nonce and signature are its protection. The package doc says so.

- [ ] **Step 1: Write the failing tests.** Each runs Enter first to get
  the nonce cookie, then signs an assertion with `assertion.Sign`. The
  claims are: `iss` the root, `aud` the instance, `sub "u1"`, and
  `nonce` the cookie's.
  - `TestConsumeSignsThePersonIn`: expect a 303 to the stored `next`.
    The response sets the session cookie, and the session has
    `Subject == "u1"` and `Method == "handoff"`. The nonce cookie is
    cleared.
  - `TestConsumeRefusesAMismatchedNonce`, `TestConsumeRefusesWithoutTheNonceCookie`:
    expect 403 with the refusal page and no session.
  - `TestConsumeRefusesAReplayAfterSignOut`: consume, sign out, then post
    the same assertion again with a fresh nonce cookie. Expect 403.
  - `TestConsumeRefusesAWrongAudience`, `TestConsumeRefusesAnExpiredAssertion`.
  - `TestConsumeRefusesASuspendedAccount`: `Suspended` returns true, so
    expect 403 with the suspended copy. Once it returns false, a fresh
    handoff succeeds.
  - `TestConsumeIsNotFoundUnderABasePath`: set up the same way as the
    Enter case in Task 4.
- [ ] **Step 2:** Run `go test ./handoff/ -run Consume`. Expect FAIL.
- [ ] **Step 3: Implement.** The order is:
  1. Parse the form, limited to 16 KiB.
  2. Verify the assertion.
  3. Compare the nonces with `subtle.ConstantTimeCompare`.
  4. Check suspension.
  5. Insert the `jti`. A primary-key conflict is a replay.
  6. Call `Sessions.SignIn` with `Session{Subject, Method, AuthTime:
     time.Unix(claims.AuthTime, 0)}`.
  7. Clear the nonce cookie.
  8. Answer 303.

  Refusals log the `assertion` error, which carries no submitted
  values, and show the Task 0 page.
- [ ] **Step 4:** Run the same command. Expect PASS.
- [ ] **Step 5:** Commit: `handoff: consume the root's assertion and sign in`.

### Task 6: `handoff`: Renew and Sweep

**Files:**
- Create: `handoff/renew.go`
- Test: `handoff/renew_test.go`

**Interfaces:**
- Produces:
  ```go
  func (h *Handoff) Renew(next http.Handler) http.Handler
  func (h *Handoff) Sweep(now time.Time) error
  ```
  Renew wraps the app's signed-in routes, inside `sessions.Middleware`.

- [ ] **Step 1: Write the failing tests.**
  - `TestRenewPassesAFreshHandoffSession`: a session with `At` 59
    minutes ago passes.
  - `TestRenewSendsAStaleGETThroughTheRoot`: with `At` 61 minutes ago,
    a GET to `/events/1?x=2` gets a 303 to
    `/_carlos/enter?next=%2Fevents%2F1%3Fx%3D2`.
  - `TestRenewRefusesAStalePOST`: expect 401, and the handler is not
    called.
  - `TestRenewIgnoresOtherMethods`: a 2-day-old session with `Method
    "magic"` passes, because public visitors' sessions are not handoff
    sessions (spec §5.4).
  - `TestSweepDeletesExpiredReplayRows`: only the rows with `expires_at`
    before now are removed.
- [ ] **Step 2:** Run `go test ./handoff/ -run 'Renew|Sweep'`. Expect FAIL.
- [ ] **Step 3: Implement.** Read the session with `sessions.Current(r)`.
  The age is `now - Session.At`.
- [ ] **Step 4:** Run the same command. Expect PASS.
- [ ] **Step 5:** Commit: `handoff: renew handoff sessions through the root`.

### Task 7: `handofftest`: a fake root, and the whole flow

**Files:**
- Create: `handoff/handofftest/handofftest.go`
- Test: `handoff/handofftest/flow_test.go`

**Interfaces:**
- Produces:
  ```go
  type Root struct { Origin string; Key *crypto.Keypair; KeyID string }
  func New(t testing.TB) *Root                       // Origin "https://root.test"
  func (r *Root) Setup() instance.Setup               // Mode Rooted, Root, Keys
  func (r *Root) Assert(t testing.TB, enter *http.Response, subject string) url.Values
      // reads aud and nonce from Enter's redirect; returns the form to POST
  func (r *Root) Roster(t testing.TB, audience string, sequence int64,
      state roster.State, members ...roster.Member) string
  ```
  Scaffold tests (roadmap Plan 4) and plaza's tests drive the instance
  through this helper, so keep it the only place tests hand-sign tokens.

- [ ] **Step 1: Write the failing test.** `TestTheWholeFlow` mounts
  `Enter`, `Consume`, `Renew` and the roster `Receiver` on one mux, under
  `csrf.Protect` with the exemption. Then it checks, in order:
  1. A roster snapshot adds `u1` as owner, and the receiver returns 204.
  2. GET `/_carlos/enter?next=/dash`.
  3. The form from `Assert` is POSTed, and GET `/dash` returns 200 for
     `u1`.
  4. The clock moves 61 minutes on, and GET `/dash` redirects through
     enter.
  5. A `Suspended` snapshot arrives, and the next consume gets 403.
- [ ] **Step 2:** Run `go test ./handoff/... -run TheWholeFlow`. Expect
  FAIL.
- [ ] **Step 3: Implement the helper.**
- [ ] **Step 4:** Run the same command. Expect PASS.
- [ ] **Step 5:** Commit: `handofftest: a fake root and the whole flow`.

### Task 8: Docs, changelog and the gate

**Files:**
- Create: `docs/site/reference/instance.md`, `handoff.md`, `roster.md`
  and `handofftest.md`, in the shape of `basepath.md`.
- Modify: `CHANGELOG.md` (Unreleased).
- Modify: `skill/data.md`. The home/hub paragraph gains one sentence:
  an instance under a root uses `instance.FromEnv`, `handoff` and
  `roster`, and the reference pages are named. The full skill chapter is
  roadmap Plan 5.

- [ ] **Step 1:** Write the pages. They must say:
  - the app exempts `EnterPath` from CSRF;
  - the app merges `handoff.Schema` and `roster.Schema` into
    `BootSchema`;
  - `Renew` wraps signed-in routes;
  - the app calls `Sweep` from its daily tick.

  Any new user-facing prose goes through the copy-review skill first.
- [ ] **Step 2:** Run the repo-wide checks that list packages and docs.
  The command is `go test . -run 'Packs|Adoption|Seams|SkillMD|Reference'`.
  If one of them enumerates packages, register the new ones and re-run
  until it passes.
- [ ] **Step 3:** Commit: `docs: instance, handoff and roster reference`.
- [ ] **Step 4:** Merge `origin/main` if it has moved, push, and let CI
  run the full gate on the fleet.
