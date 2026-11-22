# Default Sign-in Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every scaffolded Rastrillo app signs people in by magic link
(or invisibly by Keymail), offers once to add a passkey, authenticator
app or passphrase as further ways in, and thereafter leads with the way
last used, with "Other ways to sign in" under it.

**Architecture:** Small exported seams in `auth`, `lastsignin` and
`totp`; two more ways on auth's existing one-way screen; a new `signin`
package that builds `passkey` and `totp`, stores passphrases, mounts
every route, gates sign-up steps and the offer, and renders through new
`ui` partials. The scaffold calls `signin.New`, `Mount` and `Gate`.

**Tech Stack:** Go, `database/sql` on SQLite, `html/template` partials
in `ui`, WebAuthn via `webauthn`/`passkey`, RFC 6238 via `totp`,
Argon2id via `password.Hash`/`Verify`.

**Spec:** `docs/superpowers/specs/2026-11-22-default-signin-design.md`

## Global Constraints

- Code by `skill/code.md`: spelled-out names (`response`, `request`;
  `err` and `ctx` allowed), 80 columns, non-test files ≤ 400 lines,
  non-test functions ≤ 40 lines (actions ≤ 15). `make code` checks it.
- Comments say why, in one or two full sentences.
- `auth` renders no HTML; every page is a `ui` partial.
- Every locale catalog carries every key: `TestBaseCatalogsShareOneKeySet`
  (basecatalog_test.go:49) fails otherwise. 12 catalogs: ar bn en es ga
  hi ja pt-PT ru vi yue zh-Hans.
- Unknown addresses get the same answer as wrong credentials, on every
  way.
- Authenticator and passphrase failures: 5 per account per 15 minutes,
  30 per client IP per 15 minutes, stored in SQL.
- Passphrase: 15 to 256 characters (counted in runes), no composition
  rules.
- Changing sign-in options needs a fresh, strong sign-in: verified
  within 10 minutes; `passkey-nouv` never counts; a `keymail` session
  counts only with a non-zero `AuthTime`.
- The email link can never be removed.
- Keymail: with no `KeymailServers` list, every server-side Keymail
  connection may only dial public unicast addresses.
- No user-facing string says "Keymail".
- Records mode, blind links and a `SecondFactor` hook are refused by
  `signin.New`.
- Run targeted tests locally (`GOFLAGS=-mod=mod go test ./pkg -run X`,
  sandbox off). Full suites run on CI, not on this machine.
- Commits: imperative subject under 80 characters, body says why,
  ending `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
  This checkout's `.git/config.lock` blocks config writes: commit with
  `GIT_AUTHOR_NAME=claude GIT_AUTHOR_EMAIL='claude~paul@keymail.dev'`
  and the same two `GIT_COMMITTER_*` variables.

## Review Focus

1. **A remembered authenticator or passphrase way whose credential was
   since removed.** The one-tap must still render and a submit must fail
   like a wrong code, offering the email link, never a 500. Test in
   Task 8.
2. **The gate on non-navigation requests.** A signed-in person with an
   unfinished step posting a form, fetching JSON or loading an asset
   must not be redirected. Test in Task 9.
3. **`return_to` through steps, the offer and enrolment.** A deep link
   (`/notes/42?tab=x`) must survive sign-in, two steps and the offer;
   an off-site `return_to` must fall back to `/`. Test in Task 9.
4. **Two tabs enrolling the authenticator at once.** The second
   `Begin` must not silently replace the secret the first tab is
   confirming. Test in Task 10.
5. **Removing the last added way.** The person falls back to the email
   link, and since they again have none of the added ways and dismissed
   the offer, they never see the offer again. Test in Task 11.

---

## Copy

These keys are drafts. Task 1 puts them through review and translation
before any other task uses them. All keys are `rastrillo.ui.<key>`.

| Key | Draft English |
|---|---|
| `signin_way_authenticator` | Authenticator app |
| `signin_way_passphrase` | Passphrase |
| `signin_code` | Code from your authenticator app |
| `signin_passphrase` | Passphrase |
| `signin_code_submit` | Sign in |
| `signin_problem_code` | That code didn't work. Try the newest one, or email yourself a link. |
| `signin_problem_passphrase` | That email and passphrase don't match. Try again, or email yourself a link. |
| `signin_problem_paused` | Too many tries. Email yourself a link to sign in now. |
| `signin_email_instead` | Email me a link instead |
| `signin_offer_title` | Sign in faster next time to {name} |
| `signin_offer_heading` | Sign in faster next time |
| `signin_offer_body` | Add another way in, so you don't need an email every time. |
| `signin_offer_recommended` | Recommended |
| `signin_offer_passkey` | Use a passkey |
| `signin_offer_passkey_detail` | Your face, fingerprint or screen lock. |
| `signin_offer_authenticator` | Use an authenticator app |
| `signin_offer_authenticator_detail` | A six-digit code from an app on your phone. |
| `signin_offer_passphrase` | Set a passphrase |
| `signin_offer_passphrase_detail` | A long phrase only you know. |
| `signin_offer_later` | Not now |
| `signin_add_passkey_heading` | Add a passkey |
| `signin_add_passkey_body` | Your device will ask for your face, fingerprint or screen lock. |
| `signin_add_passkey_submit` | Create a passkey |
| `signin_add_passkey_failed` | That didn't work. Try again, or pick another way. |
| `signin_add_authenticator_heading` | Add an authenticator app |
| `signin_add_authenticator_body` | Scan this with your authenticator app, then type the code it shows. |
| `signin_add_authenticator_key` | Can't scan it? Type this key instead: |
| `signin_add_authenticator_submit` | Turn it on |
| `signin_add_authenticator_wrong` | That code didn't match. Type the newest code the app shows. |
| `signin_add_passphrase_heading` | Set a passphrase |
| `signin_add_passphrase_body` | Use at least 15 characters. A few unrelated words work well. |
| `signin_add_passphrase_short` | Use at least 15 characters. |
| `signin_add_passphrase_long` | Use 256 characters or fewer. |
| `signin_add_passphrase_submit` | Save passphrase |
| `signin_added` | Done. Next time you can sign in with it. |
| `signin_added_continue` | Continue |
| `signin_options_title` | Sign-in options for {name} |
| `signin_options_heading` | Sign-in options |
| `signin_options_email` | Email link |
| `signin_options_email_detail` | Always on. We email a link to {address}. |
| `signin_options_add` | Add |
| `signin_options_remove` | Remove |
| `signin_options_removed` | Removed. |

Changed keys (Task 2):

| Key | Today | Draft |
|---|---|---|
| `signin_problem_keymail` | Keymail couldn't confirm it's you. | We couldn't confirm it's you. Try again, or email yourself a link. |
| `signin_to_keymail` | Continue to Keymail | Continue |
| `signin_continue_heading` | Taking you to Keymail | Signing you in |
| `signin_continue_title` | Taking you to Keymail to sign in to {name} | Signing in to {name} |
| `signin_continue_body` | Taking you to Keymail to confirm it's you. | Taking you to confirm it's you. |

And the hard-coded string at auth/handlers.go:337 becomes "That sign-in
came back for a different address. Try again."

---

### Task 1: Review and translate the copy

**Files:**
- Modify: `locales/*.toml` (all 12)
- Create: `docs/superpowers/plans/2026-11-22-default-signin-copy.md`
  (the review record)

**Interfaces:**
- Produces: every key in the Copy tables, in all 12 catalogs, final
  wording in `en.toml`.

- [ ] **Step 1: Run Luna on the strings.** Write a prompt with both Copy
  tables, the AGENTS.md "User-facing copy" section and the spec's "What
  a person sees". Run
  `codex exec -m gpt-6-luna -c model_reasoning_effort=high -s read-only --skip-git-repo-check -o <out> - < <prompt>`
  with the Bash sandbox off. Apply each correction unless it is factually
  wrong. Record the prompt, the whole reply and what was applied in the
  copy record file.
- [ ] **Step 2: Add the new keys and change the five existing ones in
  `en.toml`**, beside the other `signin_` keys (en.toml:105-140).
- [ ] **Step 3: Translate into the other 11 catalogs.** Keep `{name}`
  and `{address}` placeholders verbatim.
- [ ] **Step 4: Run** `GOFLAGS=-mod=mod go test . -run 'TestBaseCatalog'`.
  Expected: PASS.
- [ ] **Step 5: Commit** "Add the default sign-in's copy in every locale".

### Task 2: Keymail stops saying its name

**Files:**
- Modify: `ui/partials/signin.html` (the classic keymail one-tap at
  :71-75, the one-way one at :147-152)
- Modify: `auth/handlers.go:337`
- Test: `auth/screen_test.go`, `auth/oneway_test.go` (whichever assert
  the old strings; grep for `Keymail` in `auth/*_test.go`, `ui/*_test.go`)

**Interfaces:**
- Consumes: Task 1's changed keys.

- [ ] **Step 1: Write the failing test** `TestNoKeymailInWhatPeopleRead`
  in `auth/screen_test.go`: render every door (`ask`, `keymail`, `link`,
  `continue`, `sent`, the `keymail` problem) on the classic and one-way
  screens, and the mismatch response from `Callback`; assert no body
  contains `Keymail` or `keymail` outside attribute values and hidden
  `expect` inputs. Also assert the remembered-Keymail one-tap reads
  `Continue as ` followed by the address.
- [ ] **Step 2: Run it.** Expected: FAIL naming "Continue to Keymail".
- [ ] **Step 3: Change both keymail one-taps** to render
  `signin_continue_as` with the address (keep the hidden
  `expect=keymail`, which Begin still uses), drop the now-redundant
  `rst-signin-remembered` line, and change the mismatch message.
- [ ] **Step 4: Fix existing tests** that pinned the old strings, then
  run `GOFLAGS=-mod=mod go test ./auth ./ui -run 'Signin|Keymail|OneWay|Screen'`.
  Expected: PASS.
- [ ] **Step 5: Commit** "Keep Keymail invisible on the sign-in screen".

### Task 3: Keymail lookups dial public addresses only

**Files:**
- Create: `auth/keymaildial.go`
- Modify: `auth/auth.go:537-558`
- Test: `auth/keymaildial_test.go`

**Interfaces:**
- Produces: `func publicUnicast(address netip.Addr) bool`;
  `func publicOnlyClient(timeout time.Duration) *http.Client`.

- [ ] **Step 1: Write the failing tests.**
  `TestPublicUnicast` table: `127.0.0.1`, `::1`, `10.1.2.3`,
  `172.16.0.1`, `192.168.1.1`, `169.254.1.1`, `fe80::1`, `fc00::1`,
  `100.64.0.1`, `100.127.255.254`, `0.0.0.0`, `::`, `224.0.0.1`,
  `ff02::1`, `::ffff:127.0.0.1` → false; `1.1.1.1`,
  `2606:4700::1111`, `100.128.0.1` → true.
  `TestDefaultKeymailClientRefusesLoopback`: build `auth.New` with
  `KeymailServers` nil; point `a.flow.Classifier.HTTP` at nothing new,
  then `a.flow.Classifier.HTTP.Get(httptestServer.URL)` (a 127.0.0.1
  server) must return an error and the server must record zero hits; do
  the same with `a.exchangeHTTP`.
  `TestListedServersKeepTheGuard`: with `KeymailServers` set, the
  clients are still `hostGuard`'s (no regression).
- [ ] **Step 2: Run** `GOFLAGS=-mod=mod go test ./auth -run 'PublicUnicast|KeymailClient|ListedServers'`.
  Expected: FAIL (undefined).
- [ ] **Step 3: Implement.** `publicOnlyClient` clones
  `http.DefaultTransport`, sets its `DialContext` to a `net.Dialer`
  whose `Control` parses the dialed `host:port` with
  `netip.ParseAddrPort`, unmaps it, and refuses it unless
  `publicUnicast`. Checking in `Control` (after resolution) is what
  defeats DNS rebinding. In `auth.New`, when `servers == nil`, set
  `classifier.HTTP` and `a.exchangeHTTP` to it with `classifyTimeout`
  and `exchangeTimeout`. `wireKeymail` in tests already replaces the
  clients outright, so the existing fakes are unaffected.
- [ ] **Step 4: Run** the step-2 command, then
  `GOFLAGS=-mod=mod go test ./auth -run Keymail`. Expected: PASS.
- [ ] **Step 5: Commit** "Let Keymail lookups dial public addresses only".

### Task 4: auth exports admission and subject lookup

**Files:**
- Modify: `auth/handlers.go:485-548` (admit), `auth/subject.go`
- Create: `auth/admit.go`
- Test: `auth/admit_test.go`

**Interfaces:**
- Produces:
  - `func (a *Auth) Admit(response http.ResponseWriter, request *http.Request, identity Identity, subject string)`:
    today's `admit`; when `subject` is non-empty and `subjectFor`
    resolves a different one, it logs and answers like
    `refuseAdmission`. `admit` becomes `a.Admit(w, r, id, "")`.
  - `func (a *Auth) Subject(ctx context.Context, address string) (string, error)`:
    the address in rows mode without `SubjectFor`;
    `ErrNeedsLookup` when `SubjectFor`, `Records` or `BlindLinks` is set.
    It never creates anything.
  - `var ErrNeedsLookup = errors.New("rastrillo/auth: this configuration resolves subjects by creating them; give signin a Lookup")`
  - `func (a *Auth) Assembles() error`: nil, or an error naming the
    first of `Records`, `BlindLinks`, `SecondFactor` that is set.

- [ ] **Step 1: Write the failing tests** in `auth/admit_test.go`:
  `TestAdmitSignsInAndRemembers` (an Identity with Method `totp` lands
  on `SignedInPath`, mints a session with Method `totp`, and writes a
  remembered record — read it back with `RememberJar().Read`);
  `TestAdmitRefusesAnotherSubject` (SubjectFor maps the address to
  `"7"`, Admit is called with `"8"`: 403, no session cookie);
  `TestAdmitHonoursAuthorize`; `TestSubjectIsTheAddress`;
  `TestSubjectNeedsLookupWithSubjectFor` (and with Records);
  `TestAssemblesRefusesSecondFactor`.
- [ ] **Step 2: Run** `GOFLAGS=-mod=mod go test ./auth -run 'Admit|Subject|Assembles'`.
  Expected: FAIL.
- [ ] **Step 3: Implement** in `auth/admit.go`. Move the body of
  `admit` there unchanged except for the subject comparison; leave
  `handlers.go`'s callers calling `a.Admit(…, "")`.
- [ ] **Step 4: Run** the step-2 command, then
  `GOFLAGS=-mod=mod go test ./auth -run 'Callback|Verify|Refused|ReturnTo'`.
  Expected: PASS.
- [ ] **Step 5: Commit** "Export auth's admission for other ways in".

### Task 5: lastsignin remembers two more methods

**Files:**
- Modify: `lastsignin/lastsignin.go:47-53`, `:251-260`
- Test: `lastsignin/lastsignin_test.go`

**Interfaces:**
- Produces: `MethodTOTP = "totp"`, `MethodPassphrase = "passphrase"`;
  both valid only with a non-empty valid address and no provider.

- [ ] **Step 1: Write the failing test** `TestRemembersTOTPAndPassphrase`:
  each method with an address round-trips through `Remember`/`Read` as
  `Valid`; each with an empty address or a provider writes nothing.
- [ ] **Step 2: Run** `GOFLAGS=-mod=mod go test ./lastsignin -run TOTPAndPassphrase`.
  Expected: FAIL.
- [ ] **Step 3: Add the constants and the `validRecord` case**, and
  extend the package comment's list of methods.
- [ ] **Step 4: Run** `GOFLAGS=-mod=mod go test ./lastsignin`. Expected: PASS.
- [ ] **Step 5: Commit** "Remember authenticator and passphrase sign-ins".

### Task 6: totp checks a code for a caller that signs in

**Files:**
- Modify: `totp/handlers.go` (beside `Confirm`, :176), `totp/totp.go:1-12`
- Test: `totp/handlers_test.go` (or the package's existing test file)

**Interfaces:**
- Produces: `func (h *Handlers) Check(subject, submitted string) (bool, error)`:
  `verify(subject, submitted, true)`, true when a step was spent.

- [ ] **Step 1: Write the failing tests** `TestCheckSpendsTheStep`
  (a confirmed secret's current code is true once, false the second
  time); `TestCheckRefusesUnconfirmed`; `TestCheckUnknownSubject`
  (false, nil).
- [ ] **Step 2: Run** `GOFLAGS=-mod=mod go test ./totp -run Check`. Expected: FAIL.
- [ ] **Step 3: Implement `Check`**, and change the package comment's
  "never signs anybody in from nothing" to say that `signin` does, by
  address and code, behind its own limiter.
- [ ] **Step 4: Run** `GOFLAGS=-mod=mod go test ./totp`. Expected: PASS.
- [ ] **Step 5: Commit** "Let totp check a code for a sign-in by address".

### Task 7: The one-way screen draws two more ways

**Files:**
- Modify: `auth/oneway.go` (`SigninWay` consts :27-30, `ParseWay`,
  `offers`, `rememberedWay`, `Ways`), `auth/signin.go` (`SigninState`,
  `Door`)
- Modify: `ui/partials/signin.html` (the `signin-one-way` define)
- Modify: `internal/designsystem/screens.go` (sample states)
- Test: `auth/oneway_test.go`, `ui/ui_test.go`

**Interfaces:**
- Consumes: Task 5's methods; Task 1's keys.
- Produces:
  - `WayAuthenticator SigninWay = "authenticator"`,
    `WayPassphrase SigninWay = "passphrase"`
  - `type SecretDoor struct{ Path string }`: where the form posts.
  - `SigninState.Authenticator *SecretDoor`,
    `SigninState.Passphrase *SecretDoor`: set by the page (as
    `Passkey` is); nil means the app does not offer that way.
  - `Door()` returns `"authenticator"` or `"passphrase"` for those ways.

- [ ] **Step 1: Write the failing tests** in `auth/oneway_test.go`:
  `TestRememberedTOTPLeadsWithTheCodeDoor` (record `totp` +
  address, `Authenticator` set → Door `authenticator`, `Address` is
  the remembered one); `TestRememberedTOTPWithoutTheDoorFallsToEmail`;
  `TestChooserOrder` (all four doors set, shown way email → rows
  passkey, authenticator, passphrase, in that order; shown way
  passphrase → email, passkey, authenticator);
  `TestParseWayKnowsTheNewWays`; `TestReauthOffersAllLocalWays`.
  In `ui/ui_test.go`, `TestSecretDoorsRender`: the authenticator door
  has an email field (prefilled, `autocomplete="username"`), a code
  field (`inputmode="numeric"`, `autocomplete="one-time-code"`,
  `name="code"`), posts to `Path`, carries `return_to` and `reauth`;
  the passphrase door the same with `type="password"`,
  `autocomplete="current-password"`, `name="passphrase"`. Both offer
  "Email me a link instead" (`signin_email_instead`) linking to the
  email way, and "Other ways to sign in".
- [ ] **Step 2: Run** `GOFLAGS=-mod=mod go test ./auth ./ui -run 'TOTP|Chooser|ParseWay|Reauth|SecretDoors'`.
  Expected: FAIL.
- [ ] **Step 3: Implement** the ways in `oneway.go`, the fields and
  `Door` cases in `signin.go`, one shared `signin-secret-door` define
  in the partial drawn for both (it takes the field's name, type,
  autocomplete and label), the chooser rows with the `key`-style glyphs
  (add `"shield"` for authenticator and `"lock"` for passphrase, from
  Lucide, inline as the others are), and problem callouts for
  `?err=code`, `?err=passphrase`, `?err=paused` mapped in
  `SigninState` to new `SigninProblem`s `ProblemCode`,
  `ProblemPassphrase`, `ProblemPaused`. Unlike the email problems, these
  three keep the secret door (the visitor retypes the code, not the
  address). Add each door as a sample state on the Screens page.
- [ ] **Step 4: Run** the step-2 command, then
  `GOFLAGS=-mod=mod go test ./auth ./ui ./internal/designsystem -run 'OneWay|Signin|Screens'`.
  Expected: PASS.
- [ ] **Step 5: Commit** "Draw authenticator and passphrase ways on the one-way screen".

### Task 8: The `signin` package and its two sign-ins

**Files:**
- Create: `signin/signin.go` (Config, New, Signins), `signin/schema.go`
  + `signin/migrations/0001_init.sql`, `signin/limit.go`,
  `signin/secret.go` (the two POST handlers), `signin/passphrase.go`
  (store)
- Test: `signin/signin_test.go`, `signin/secret_test.go`,
  `signin/limit_test.go`

**Interfaces:**
- Consumes: Task 4 (`Admit`, `Subject`, `ErrNeedsLookup`, `Assembles`),
  Task 6 (`Check`), Task 7 (`SecretDoor`).
- Produces:

```go
type Ways uint8
const (
    Passkey Ways = 1 << iota
    Authenticator
    Passphrase
)

type Config struct {
    Auth     *auth.Auth
    Sessions *sessions.Sessions
    DB       *sql.DB
    Origin   string
    Key      [32]byte // seals TOTP secrets
    Issuer   string   // the authenticator app's account label
    Ways     Ways
    Steps    []Step   // Task 9
    Lookup   func(ctx context.Context, address string) (subject string, ok bool, err error)
    Proof    *pow.Guard // the Guard auth has; nil only with ProofOff
    ProofOff bool
    Render   func(response http.ResponseWriter, request *http.Request, view View) // Task 10
    TrustedProxyHops *int
    Logger   *slog.Logger
    Now      func() time.Time
}

func New(config Config) (*Signins, error)
var Schema = migrate.MustFromFS(migrationFS, "signin") // as totp/handlers.go:26
const (
    CodePath       = "/signin/code"
    PassphrasePath = "/signin/passphrase"
)
func (signins *Signins) SignInWithCode(response http.ResponseWriter, request *http.Request)
func (signins *Signins) SignInWithPassphrase(response http.ResponseWriter, request *http.Request)
func (signins *Signins) Fill(state *auth.SigninState) // sets Passkey, Authenticator, Passphrase doors
```

  Tables: `signin_passphrases(subject TEXT PRIMARY KEY, hash TEXT NOT NULL, created_at TEXT NOT NULL)`,
  `signin_offers(subject TEXT PRIMARY KEY, dismissed_at TEXT NOT NULL)`,
  `signin_failures(bucket TEXT NOT NULL, at TEXT NOT NULL)` with an
  index on `(bucket, at)`. Buckets: `account:` + hex SHA-256 of the
  lowercased address, and `ip:` + the client IP from `clientip`.

- [ ] **Step 1: Write the failing tests.**
  `signin_test.go`: `TestNewRefusesRecordsBlindAndSecondFactor`
  (each `Assembles` error surfaces); `TestNewNeedsLookupWithSubjectFor`;
  `TestNewRefusesNoRender`.
  `limit_test.go`: `TestFiveMissesPauseTheAccount` (sixth attempt
  within 15 minutes is paused even with the right code; at 15 minutes
  + 1 second it is open); `TestThirtyMissesPauseTheIP` across 30
  different addresses; `TestPausesSurviveANewSigns` (a second `New` on
  the same DB sees the earlier misses).
  `secret_test.go`: `TestCodeSignsIn` (enrolled subject, current code
  → 303 to `/`, session Method `totp`, remembered record `totp` with the
  address); `TestPassphraseSignsIn` (Method `passphrase`);
  `TestUnknownAddressAnswersLikeAWrongCode` and the passphrase twin:
  the status, `Location` and `Set-Cookie` names are identical;
  `TestPausedAnswersErrPaused` (`Location` has `err=paused`);
  `TestRemovedCredentialFailsLikeAWrongOne` (Review Focus 1: remembered
  `totp` record, secret disabled → `err=code`, never 5xx);
  `TestSecretSignInKeepsReturnTo` (`return_to=/notes/42` lands there;
  `return_to=https://evil.example/` lands on `/`);
  `TestUnsolvedProofIsRefusedBeforeAnyCheck` (no failure recorded, no
  `Check` or `Verify` call).
- [ ] **Step 2: Run** `GOFLAGS=-mod=mod go test ./signin`. Expected: FAIL
  (package does not exist).
- [ ] **Step 3: Implement.** (`TestNewRefusesNoRender` also covers:
  `New` refuses neither `Proof` nor `ProofOff`, like `password.New`.)
  `New` builds `totp.New` (no Gate) and
  `passkey.New` (with `Remember: config.Auth.RememberJar()`, no Gate)
  when their ways are on; computes a decoy passphrase hash once with
  `password.Hash` of random bytes. Each POST: first
  `config.Proof.Check(request, pow.Want{Scope: auth.ProofScope})`, the
  same scope as the screen's form, so the `State.Proof` the partial
  already renders on the secret doors (Task 7) is the one checked; a
  refusal redirects with `err=check&rec=1` as `Begin` does
  (auth/handlers.go:122-160). Then read `address`; refuse when either bucket is paused (`err=paused`);
  resolve the subject through `Lookup` or `auth.Subject`; check the
  credential (decoy `password.Verify` for an unknown passphrase
  subject); on a miss record a failure in both buckets and redirect to
  the sign-in page with `way=<way>&err=<code|passphrase>` plus
  `return_to`; on a hit call `config.Auth.Admit` with
  `Identity{Address, Method, AuthTime: now, At: now}` and the subject.
  Sweep failures older than 15 minutes from the same background loop
  that sweeps auth (expose `func (signins *Signins) Sweep(now time.Time) error`).
- [ ] **Step 4: Run** `GOFLAGS=-mod=mod go test ./signin`. Expected: PASS.
- [ ] **Step 5: Commit** "Add signin: authenticator and passphrase as ways in".

### Task 9: Sign-up steps and the gate

**Files:**
- Create: `signin/gate.go`
- Test: `signin/gate_test.go`

**Interfaces:**
- Consumes: Task 8's `Signins`, `Config.Steps`.
- Produces:

```go
type Step struct {
    Path   action.Path
    Needed func(ctx context.Context, subject string) (bool, error)
}
func (signins *Signins) Gate(next http.Handler) http.Handler
func (signins *Signins) Next(response http.ResponseWriter, request *http.Request)
```

  The gate acts only on GET with `Sec-Fetch-Mode: navigate`, or without
  that header and with `Accept` containing `text/html`; never on the
  steps' paths, the offer and enrolment paths, `/signin*`, `/auth/*`,
  `/passkey/*`, `/signout`. Offer eligibility is Task 10's
  `func (signins *Signins) offerDue(ctx context.Context, session sessions.Session) (bool, error)`;
  stub it to false here.

- [ ] **Step 1: Write the failing tests:**
  `TestGateSendsToTheFirstNeededStep` (two steps, first not needed →
  303 to the second with `return_to` the requested URL);
  `TestGateLetsThroughWhenNothingIsNeeded`;
  `TestGateIgnoresNonNavigation` (Review Focus 2: POST, a GET with
  `Sec-Fetch-Mode: cors`, a GET with `Accept: application/json`, a GET
  of `/assets/app.css` → all reach `next`);
  `TestGateIgnoresSignedOut`; `TestGateNeverLoopsOnAStepPath`;
  `TestNextGoesToTheNextStepThenReturnTo` and
  `TestReturnToSurvivesTwoStepsAndRefusesOffSite` (Review Focus 3);
  `TestNeededErrorIs500AndLogged`.
- [ ] **Step 2: Run** `GOFLAGS=-mod=mod go test ./signin -run 'Gate|Next|ReturnTo|Needed'`.
  Expected: FAIL.
- [ ] **Step 3: Implement.** `return_to` goes through
  `sessions.SafeReturnTo`. `Next` reads `return_to` from the form and
  calls the same chooser the gate uses.
- [ ] **Step 4: Run** the step-2 command. Expected: PASS.
- [ ] **Step 5: Commit** "Gate signed-in pages on the app's sign-up steps".

### Task 10: The offer and adding a way

**Files:**
- Create: `signin/offer.go`, `signin/add.go`, `signin/view.go`,
  `passkey/js/register.mjs`, `ui/partials/signin-more.html`
- Modify: `passkey/js.go` (embed and export `RegisterJS()`),
  `signin/gate.go` (real `offerDue`), `internal/designsystem/screens.go`
- Test: `signin/offer_test.go`, `signin/add_test.go`,
  `ui/ui_test.go`, `passkey/registerjs_test.go` (node, as
  `signinjs_test.go` is)

**Interfaces:**
- Consumes: Tasks 8 and 9; `passkey.Handlers.RegisterBegin/Finish`;
  `totp.Handlers.Begin/Pending/Confirm`; `password.Hash`.
- Produces:

```go
type View struct {
    Status  int
    Signin  *auth.SigninState // the sign-in screen, for RenderConfirm and the page
    Offer   *Offer
    Add     *Add
    Options *Options          // Task 11
}
type Offer struct{ Ways Ways; LaterPath, ReturnTo string }
type Add struct {
    Way      Ways
    Action   string            // where the form posts
    ReturnTo string
    QR       template.HTML     // authenticator only
    Key      string            // authenticator only
    Problem  string            // locale key, "" for none
    Done     bool
    Passkey  *auth.PasskeyDoor // passkey only: begin/finish and module URLs
}
func (signins *Signins) offerDue(ctx context.Context, session sessions.Session) (bool, error)
func (signins *Signins) strong(session sessions.Session) bool
```

  `offerDue`: Method is `magiclink`, at least one way is on, the
  subject has none of the on ways (`passkey.Enrolled`, `totp.Enrolled`,
  a passphrase row), and no `signin_offers` row. `strong`: verified at
  `AuthTime`, or at `At` when `AuthTime` is zero, except Method
  `keymail` (needs non-zero `AuthTime`) and `passkey-nouv` (never);
  within 10 minutes of `Now()`. Every add handler requires `strong`,
  else 303 to the sign-in page with `reauth=1&return_to=<this page>`,
  plus `force=1` when the session is `keymail` with a zero `AuthTime`,
  so `Begin` sends an email link instead of repeating the Keymail
  round trip that could not prove a fresh gesture.
  The ui partials `signin-offer`, `signin-add` and `signin-options` are
  defined in `signin-more.html`, and one `signin-view` define picks
  between them and the existing `signin` partial, so an app's page
  template is a single `{{template "signin-view" …}}`.

- [ ] **Step 1: Write the failing tests:**
  `TestOfferDueOnlyForLinkSignInsWithNoWay` (table over methods
  `magiclink`, `keymail`, `passkey`, `totp`; with and without an
  enrolled way; with a dismissal row); `TestLaterRecordsAndNeverAsksAgain`;
  `TestGateShowsTheOfferAfterTheSteps`;
  `TestStrong` (table: magiclink with `At` 5 min ago → true, 11 min →
  false; keymail with zero `AuthTime` and fresh `At` → false; keymail
  with `AuthTime` 1 min ago → true; `passkey-nouv` fresh → false;
  `totp` fresh → true);
  `TestAddAuthenticatorConfirmsOneCode` (QR and key shown; wrong code
  → `signin_add_authenticator_wrong`, not enrolled; right code →
  enrolled, `Done`);
  `TestTwoTabsAddingTheAuthenticator` (Review Focus 4: tab A begins,
  tab B begins, tab A confirms with a code for A's secret → fails with
  the wrong-code problem, and tab B's secret confirms; i.e. `Pending`
  is reused rather than replaced, so both tabs show the same secret —
  assert the second `Begin` returns the same key);
  `TestAddPassphraseRules` (14 runes → short problem; 257 → long; 15
  emoji runes → accepted; stored hash verifies with `password.Verify`);
  `TestAddPasskeyRegistersWithAuthtest` (use `webauthn/authtest` to
  answer `RegisterBegin`; credential listed afterwards);
  `TestAddNeedsAStrongSession` (stale magiclink → 303 with `reauth=1`;
  keymail with zero `AuthTime` → 303 with `reauth=1&force=1`).
  `ui`: `TestOfferRendersPasskeyFirstAndRecommended`;
  `TestAddAuthenticatorFieldIsANumericOneTimeCode`;
  `TestAddPassphraseFieldIsANewPassword` (`autocomplete="new-password"`,
  `minlength="15"`, `maxlength="256"`, a hidden `username` field with
  the address for password managers).
  `passkey/registerjs_test.go`: the module posts begin, calls
  `register`, posts finish, then navigates to the answer's `to`.
- [ ] **Step 2: Run** `GOFLAGS=-mod=mod go test ./signin ./ui ./passkey -run 'Offer|Later|Strong|Add|TwoTabs|RegisterJS'`.
  Expected: FAIL.
- [ ] **Step 3: Implement** the handlers, `View`, the partials (one
  card on the stage shell like `signin`; passkey first with the
  `signin_offer_recommended` badge; "Not now" is a POST), and
  `register.mjs` over `webauthn.mjs`'s `register` and `signin.mjs`'s
  `postJSON`/`safeNext`. Add each view as a sample on the Screens page.
- [ ] **Step 4: Run** the step-2 command. Expected: PASS.
- [ ] **Step 5: Commit** "Offer another way in once, and add it".

### Task 11: Sign-in options

**Files:**
- Create: `signin/options.go`
- Modify: `ui/partials/signin-more.html` (`signin-options`)
- Test: `signin/options_test.go`, `ui/ui_test.go`

**Interfaces:**
- Consumes: Task 10's `strong`, `View`, add handlers.
- Produces: `type Options struct{ Address string; Ways []OptionRow; RemovePath string }`,
  `type OptionRow struct{ Way Ways; Have bool; AddPath string; Label string }`
  (one row per enabled way; passkey rows per credential use
  `passkey.Info.Label`).

- [ ] **Step 1: Write the failing tests:** `TestOptionsListsEmailAlways`
  (no remove control for email); `TestOptionsAddsAndRemoves` (remove
  authenticator → `totp.Enrolled` false; remove a passphrase → row
  gone; remove one passkey of two → one left);
  `TestRemovingNeedsAStrongSession`;
  `TestRemovingTheLastWayDoesNotBringTheOfferBack` (Review Focus 5);
  `TestOptionsRefusesSignedOut` (303 to sign-in with `return_to`).
- [ ] **Step 2: Run** `GOFLAGS=-mod=mod go test ./signin ./ui -run Options`.
  Expected: FAIL.
- [ ] **Step 3: Implement.** Viewing the page needs only a session;
  adding and removing need `strong`.
- [ ] **Step 4: Run** the step-2 command. Expected: PASS.
- [ ] **Step 5: Commit** "Add the sign-in options page".

### Task 12: Mount every route

**Files:**
- Create: `signin/mount.go`
- Test: `signin/mount_test.go`

**Interfaces:**
- Produces: `func (signins *Signins) Mount(routes *action.Routes)`.
  Routes (names in quotes):
  `POST /signin/code` "signInWithCode",
  `POST /signin/passphrase` "signInWithPassphrase",
  `GET /signin/faster` "showOffer", `POST /signin/faster/later` "laterOffer",
  `GET|POST /signin/add/passkey`, `/signin/add/authenticator`,
  `/signin/add/passphrase` ("showAddX"/"addX"),
  `POST /passkey/signin/begin|finish`, `POST /passkey/register/begin|finish`,
  `GET /account/signin` "signinOptions",
  `POST /account/signin/remove` "removeSigninWay",
  `GET /signin/js/*` (webauthn, passkey sign-in and register modules,
  served with `text/javascript`).

- [ ] **Step 1: Write the failing test** `TestMountRegistersEveryRoute`:
  mount on a fresh `action.Routes`, list it, compare with the table
  above; routes for a way that is off are absent.
  `TestModulesServeAsJavaScript`.
- [ ] **Step 2: Run** `GOFLAGS=-mod=mod go test ./signin -run 'Mount|Modules'`.
  Expected: FAIL.
- [ ] **Step 3: Implement.** Use Task 8's path constants; move any
  path `Fill` hard-coded into constants beside them.
- [ ] **Step 4: Run** `GOFLAGS=-mod=mod go test ./signin`. Expected: PASS.
- [ ] **Step 5: Commit** "Mount the sign-in routes in one call".

### Task 13: The scaffold turns it on

**Files:**
- Modify: `cmd/rastrillo/scaffold/auth.go.tmpl`,
  `cmd/rastrillo/scaffold/signin.html`, `cmd/rastrillo/new_auth.go`
  (if it lists files or routes), the account page template the scaffold
  writes (link to `/account/signin`), `cmd/rastrillo/scaffold/signin_test.go.tmpl`
- Modify: `.rastrillo/budgets.txt` (`size cmd/rastrillo`, with the
  reason), `.rastrillo/code.txt` if a record moves
- Modify: `Makefile` `BROWSER_PKGS` only if Task 14 adds a browser test
  outside `auth`/`internal/scaffoldcheck`, and then `.amadan/ci.d/50-suite@N`
- Test: `internal/scaffoldcheck` (existing), `cmd/rastrillo` new-app tests

**Interfaces:**
- Consumes: everything above.

- [ ] **Step 1: Write the failing test** in the scaffold's generated
  `signin_test.go.tmpl`: `TestSigninOffersEveryWay` (the chooser of a
  fresh app lists email, passkey, authenticator app and passphrase);
  `TestFirstLinkSignInShowsTheOffer`. Extend the `cmd/rastrillo`
  test that checks the magic-auth app's `auth.Config` to assert
  `SigninOneWay: true` and no `KeymailServers`.
- [ ] **Step 2: Run** `GOFLAGS=-mod=mod go test ./cmd/rastrillo -run 'NewAuth|Magic'`.
  Expected: FAIL.
- [ ] **Step 3: Change the template.** `auth.Config` gains
  `SigninOneWay: true` and loses `KeymailServers` and its comment;
  `startAuth` builds `signin.New` with all three ways, the app's
  instance key for `Key` (derive 32 bytes with SHA-256 of a labelled
  key, as auth does for its pending key), `Render` pointing at
  `signinScreen`; the sweep loop calls `signins.Sweep`; `mountAuth`
  calls `signins.Mount(routes)`; the app's handler is wrapped in
  `signins.Gate`; `signinScreen` builds the `View`, calls
  `signins.Fill` on the state; `signin.html` renders `signin-view`.
  Keep every function under 40 lines and the file under 400.
- [ ] **Step 4: Run** the step-2 command and
  `GOFLAGS=-mod=mod go test ./internal/scaffoldcheck -run Magic -short`.
  Expected: PASS. Then `go run ./cmd/rastrillo budget size` and raise
  `size cmd/rastrillo` by exactly the measured growth, with a reason
  naming this branch.
- [ ] **Step 5: Commit** "Scaffold the default sign-in".

### Task 14: Docs, skill and one browser walk

**Files:**
- Create: `docs/site/signin.md` (the assembly: what a new app gets,
  adding a sign-up step with "your name" as the example, turning ways
  off)
- Modify: `docs/site/magic-links.md` (Keymail now on by default and the
  public-address rule), `docs/site/passkeys.md`,
  `docs/site/reference/lastsignin.md`, `docs/site/reference/totp.md`,
  `skill/signin.md`, `skill/add-signin.md`, the docs nav
- Test: `auth/oneway_browser_test.go` (extend), `skillmd_test.go`
  (byte budgets)

- [ ] **Step 1: Write the browser test**
  `TestBrowserAuthenticatorWayRemembered`: sign in by code through the
  one-way screen, sign out, reload: the page leads with the code door
  with the address prefilled and "Other ways to sign in" below.
- [ ] **Step 2: Run** `GOFLAGS=-mod=mod go test -tags browser ./auth -run AuthenticatorWayRemembered`.
  Expected: PASS once (it exercises Tasks 7–8; if it fails, the bug is
  there, not here).
- [ ] **Step 3: Write the docs** with the humane-docs voice; run the
  docs' new strings past Luna as in Task 1 only if they are user-facing
  UI, not prose docs.
- [ ] **Step 4: Run** `GOFLAGS=-mod=mod go test . -run 'Skill|Docs'` and
  `make code`. Expected: PASS.
- [ ] **Step 5: Commit** "Document the default sign-in".

### Task 15: Hand-off

- [ ] **Step 1:** `make mirror-check`, merge `origin/main` into the
  branch if it moved (memory: main moves under a branch), resolve, and
  push.
- [ ] **Step 2:** `amadan branch describe rastrillo/rastrillo default-signin`
  with the model and reasoning setting, the spec and plan paths, and the
  before/after test runs for each new test (CONTRIBUTING.md).
- [ ] **Step 3:** Cross-provider review of the whole branch with Astra
  (`codex exec -m gpt-6-astra -c model_reasoning_effort=high …`, sandbox
  off); post the prompt, the whole reply and what was done about each
  finding as a branch discussion.
- [ ] **Step 4:** `amadan ci status rastrillo/rastrillo -branch default-signin`
  until it passes; stop for Paul before `amadan branch merge`.
