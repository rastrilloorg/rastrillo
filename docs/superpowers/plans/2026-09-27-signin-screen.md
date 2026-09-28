# A shipped sign-in screen Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship a sign-in screen — `ui`'s `signin`/`signin-title` partials in a new `stage` shell, fed by `auth.SigninState` — with a keymail continuation that keeps `form-action 'self'`, a remembered one-tap, a passkey door, and an optional keymail-server allowlist, all behind one opt-in switch that leaves apps which do not adopt it byte-for-byte unchanged.

**Architecture:** `auth` stays HTML-free: it gains `SigninScreen`, sealed attempt and continuation cookies, `SigninState` (read-only) and `PrepareSigninResponse` (the only writer), `Forget` and `AnswerAsSent`. A new leaf package `lastsignin` owns the remembered cookie and the one "attempt is over" seam that `auth.admit` and `passkey.DiscoverFinish` both call. `ui` renders: new partials, a `stage` layout, a `stageArt` backdrop func, two helpers (`opt`, `Tbdi`). The passkey door's script is its own module, `passkey.JS()`, which the partial loads only when it renders the door. One shared envelope (`internal/sealedcookie`) seals all three cookies.

**Tech Stack:** Go 1.26, `html/template`, `github.com/keymaildev/signin` v0.1.1, `amadan.net/rastrillo/rastrillo/crypto` (HKDF + AES-256-GCM), chromedp harness (`-tags browser`), Node for the JS twin tests, axe-core (vendored) for a11y.

**Spec:** `docs/superpowers/specs/2026-09-27-signin-screen-design.md` (four review rounds; round 4's corrections #28 and #29 applied). Read it beside this plan: the plan argues from it, and section references below (§1.3 etc.) are to it.

## Global Constraints

- **Where commands run:** from the worktree root `/home/paulca/amadan.net/rastrillo/rastrillo/.claude/worktrees/signin-screen`, with the Bash sandbox disabled (`dangerouslyDisableSandbox: true`) for every `git`, `go` and `node` command, and `GOFLAGS=-mod=mod` exported (three scratch-module packages fail on go.sum without it).
- **The gate:** AGENTS.md's gate is `make ci` (it carries `GOFLAGS=-mod=mod` itself); Task 15 runs it whole. Every other task ends with its fast core green: `GOFLAGS=-mod=mod go vet ./... && gofmt -l . && GOFLAGS=-mod=mod go test ./...`, where `gofmt -l .` must print nothing. Code blocks in this plan are not guaranteed column-aligned: run `gofmt -w` on every Go file you write before the gate.
- **Browser tests:** `-tags browser`, `RASTRILLO_CHROME=/usr/bin/chromium`, `TMPDIR=/var/tmp`. Never set `RASTRILLO_BROWSER_OPTIONAL` — a skip is not a pass.
- **Node tests:** run with `RASTRILLO_TEST_REQUIRE_NODE=1` so a missing node fails instead of skipping.
- **Examples are separate modules.** Any task that edits `ui/tokens.css` re-copies it byte for byte: `cp ui/tokens.css examples/blog/static/tokens.css && cp ui/tokens.css examples/tickets/static/tokens.css`, then runs `(cd examples/blog && GOFLAGS=-mod=mod go test ./...) && (cd examples/tickets && GOFLAGS=-mod=mod go test ./...)`.
- **Class/attribute twins:** every `rst-*` rule in `tokens.css` names both spellings in the same rule — `.rst-signin__brand, [rst-signin-brand]` — per `internal/markup`'s grammar (`__` part → `-`, `--variant` → `~="variant"`). `ui/markup_v3_test.go` enforces it both ways.
- **No inline styles** in `ui/partials/*` or `ui/layouts/*` (`TestPartialsAndLayoutsEmitNoInlineStyles`); `stageArt`'s generated SVG carries no `style`, `<style`, colour literal or external reference.
- **Catalogs:** the 12 files in `locales/` share one key set (`TestBaseCatalogsShareOneKeySet`); every key is `rastrillo.ui.*`.
- **Struct callers stay working:** a new optional key read by a shipped partial goes through `opt` (Task 8), never `.Key` inline — an inline read of a field a caller's struct lacks is an Execute error (the `menuGroup` precedent, `ui/funcs.go`).
- **`ui/rastrillo.js` is not touched.** It is at its 16 KiB cap and every page loads it; the passkey door is `passkey/js/signin.mjs` (Task 9), and it must contain no `http://`, `https://`, `eval(`, `new Function` or `innerHTML` (its own test holds that).
- **SKILL.md:** ≤ 30,000 bytes (`skillmd_test.go`). It is LLM-facing and is not copy-reviewed, but it is reviewed like code: no inaccurate line.
- **Screen off changes nothing:** with `SigninScreen` false and `KeymailServers` unset, `Begin`, `Callback`, `admit` and passkey discovery write exactly today's cookies and redirects, whatever `Remember` says. `auth` renders no HTML.
- **Copy:** no user-facing English is written into a tracked file before the operator has reviewed it (spec §2; the `copy-review` skill). Batch 1 (Task 10) is every string the screen shows and every gallery sentence about it: approved, written to `locales/en.toml`, and translated into the other eleven once; the gallery's approved strings wait in the `.gitignore`d `copy-review/result.json` for Tasks 11–12, which write them and translate them once — in `internal/designsystem` the English is also the translation key (AGENTS.md, "User-facing copy"). Batch 2 (Task 14) is the docs and the CHANGELOG. English shown in Tasks 11–14 is the draft that went to review; the approved text replaces it. Tests reference catalog keys (`defaultT("rastrillo.ui.signin_…")`), never the English.
- **Comments** say why and name the failure prevented; a comment that restates the code is not written. **Commits:** imperative subject; body says why; last line `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Commit per task, then `git push origin signin-screen`: on amadan the branch is the PR, and its page should never lag the worktree.

## Review Focus

The inputs the spec implies but no spec test names, most likely first. Each has its test in the task that owns the code.

1. **Hostile or unusual text shown back to the visitor** — markup, `{placeholder}`-looking text, RTL script, a 300-byte string — in the prefill, the Sent address and the one-tap label renders escaped, isolated in `<bdi>`, never re-substituted; an over-long address is dropped from the cookie, not truncated. *Tests: Task 3 (`TestBeginWritesTheAttempt`, over-long), Task 8 (`TestTbdiEscapesAndSubstitutesOnce`), Task 12 (`TestSigninEscapesWhatAVisitorTyped`).*
2. **A remembered passkey with no passkey door wired** (the app removed passkeys, or forgot `State.Passkey`) renders the Ask form with the email field focused — never an empty door column. *Test: Task 6 (`TestDoorAndFocusFollowTheMatrix`), Task 12 (`TestAReturningPasskeyWithNoDoorAsksForAnAddress`).*
3. **A rotated `InstanceKey` or changed `Origin`**: every sealed cookie from before is ignored and cleared on the next sign-in page — no 500, no error callout, no stale one-tap. *Test: Task 6 (`TestCookiesFromAnotherKeyAreIgnoredAndCleared`).*
4. **A keymail approval that fails** (`?force=1&err=keymail`) keeps the address the visitor typed, so the "Send me a link instead" form is prefilled. *Test: Task 6 (`TestAKeymailProblemKeepsTheAddressForTheEscapeHatch`).*
5. **A misspelt `KeymailServers` entry** (`https://keymail.dev`, `keymail.dev/`) is refused at `New` instead of silently matching nothing and sending every keymail user a link. *Test: Task 4 (`TestKeymailServersRefusesWhatIsNotAHost`).*

## Where the code forced a choice the spec did not make

Each is decided below; a reviewer who disagrees should say so before the task that owns it.

- **The passkey door is its own module** (Task 9), not code in `rastrillo.js`, which is at its 16 KiB cap and loads on every page. The spec's §1.6 delivery decision is amended to match in the same commit as this plan.
- **Two new template helpers.** `field`'s `QuietError` and `callout`'s `ID`/`Focus` cannot be read inline without breaking struct callers (the `menuGroup` precedent), so Task 8 adds `opt`. An address inside a translated sentence needs escaping and `<bdi>` isolation, which `Tf` cannot give, so Task 8 adds `Tbdi`. `Funcs` goes from 9 to 12 entries with `stageArt`.
- **The partials and the gallery page that claims them land together** (Task 12): `buildFamilies` fails on any partial no page claims, and every partial must be marked on some page.
- **`Preview` also shows the passkey button** unhidden and inert, loading no script, so the gallery's passkey state is visible; the spec defined `Preview` only for the Continue state.
- **A valid attempt with an empty address** (over 254 bytes, dropped) still wins the prefill, leaving the field empty rather than falling back to the remembered address — a prefill must be what this browser last typed or nothing.
- **A remembered passkey with no door wired** shows the Ask form (Review Focus 2).
- **`KeymailServers` entries are validated at `New`** as whole authorities (Review Focus 5).
- **The screen-off advisory is once per process**, as the spec words it: a package-level `sync.Once`, replaceable in tests.
- **The stage shell loads only `rastrillo.js`**, not `select.js`, `calendar.js` or `datetime.js`: a sign-in page has no enhanced fields.
- **The art's colours** are `color-mix()` of accent into background, which the token-pair contrast gate cannot evaluate; Task 11 adds a small sRGB mixer to the test and keeps the card's border at ≥ 3:1 over the art (10% lines, 8% glow; measured worst case 3.17:1).
- **Only the first gallery frame draws the art**, and the gallery shows ten states, to keep the Screens page under its 128 KiB budget; the full state matrix is scanned on test-only pages (Task 13).

---

## File Structure

Created:

| File | Responsibility |
|---|---|
| `internal/sealedcookie/sealedcookie.go` (+ `_test.go`) | The `v1.` envelope: `Seal`, `Open` (strict), `Fresh` (origin + times). |
| `lastsignin/lastsignin.go` (+ `_test.go`) | The remembered-method cookie and `EndAttempt`; `Mode` Off/Forgetting/On. |
| `auth/attempt.go` | The attempt cookie: payload, seal/open, `noteAttempt`, `answerSent`. |
| `auth/keymail.go` | `callbackPath`, `KeymailServers` parsing, `hostGuard` transport. |
| `auth/continuation.go` | The continuation cookie, `continuationFor`, `validAuthorizeURL`. |
| `auth/signin.go` | `SigninStep`/`SigninProblem`, `SigninState` + `Door`/`Focus`, `PrepareSigninResponse`, `Forget`, the advisory. |
| `auth/screen_test.go`, `auth/keymailfake_test.go`, `auth/keymail_test.go`, `auth/continuation_test.go`, `auth/signinstate_test.go` | Unit tests; the fake keymail server and a cookie-jar helper. |
| `auth/signinscreen_browser_test.go` | The browser drive of the whole screen (`-tags browser`). |
| `passkey/signinscreen_test.go` | Discovery with a jar; the auth + passkey integration. |
| `ui/partials/signin.html` | `signin` and `signin-title`. |
| `ui/layouts/stage.html` | The `stage` shell. |
| `ui/stageart.go` (+ `_test.go`) | `stageArt(seed)`. |
| `ui/signin_test.go` | The partial's tests. |
| `passkey/js/signin.mjs`, `passkey/js.go` | The passkey door's module and `passkey.JS()`. |
| `passkey/js/signin_node.mjs`, `passkey/signinjs_test.go` | Node drives of the door's ceremony and destination guard. |
| `auth/signinpage_test.go` | Rendered Sent-page parity for an admission wrapper. |
| `docs/site/reference/lastsignin.md` | Reference page (Task 14, after copy review). |

Modified: `auth/auth.go`, `auth/handlers.go`, `auth/auth_test.go` (migration set), `passkey/passkey.go`, `ui/funcs.go`, `ui/funcs_test.go`, `ui/ui.go`, `ui/ui_test.go`, `ui/contrast_test.go`, `ui/partials/field.html`, `ui/partials/callout.html`, `ui/tokens.css`, `locales/*.toml` (12), `internal/designsystem/{screens.go,page.go,prose.go,designsystem.go,a11y_test.go}`, `Makefile` (browser + gorm-free lists), `examples/{blog,tickets}/static/tokens.css`, `docs/site/{magic-links,passkeys,templates}.md`, `docs/site/reference/{auth,passkey,ui}.md`, `docs/site/nav.json`, `SKILL.md`, `CHANGELOG.md`.

---
### Task 1: The sealed-cookie envelope

**Files:**
- Create: `internal/sealedcookie/sealedcookie.go`
- Test: `internal/sealedcookie/sealedcookie_test.go`

**Interfaces:**
- Consumes: `crypto.SealSym(key, plaintext []byte) ([]byte, error)`, `crypto.OpenSym(key, sealed []byte) ([]byte, error)`, `crypto.Derive(key []byte, context string) []byte` (crypto/crypto.go:275-320).
- Produces:
  - `func Seal(key []byte, payload any) (string, error)` — `"v1." + base64url(SealSym(key, json(payload)))`.
  - `func Open(key []byte, value string, payload any) error` — strict decode; `ErrInvalid` on any failure.
  - `func Fresh(origin, want string, iat, exp int64, now time.Time, max time.Duration) bool`.
  - `var ErrInvalid error`; `const FutureSkew = time.Minute`.

- [ ] **Step 1: Write the failing test**

`internal/sealedcookie/sealedcookie_test.go`:

```go
package sealedcookie

import (
	"bytes"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/crypto"
)

type payload struct {
	O string `json:"o"`
	A string `json:"a"`
}

var (
	keyA = crypto.Derive([]byte("instance"), "rastrillo/test/a")
	keyB = crypto.Derive([]byte("instance"), "rastrillo/test/b")
)

func TestSealThenOpenRoundTrips(t *testing.T) {
	v, err := Seal(keyA, payload{O: "https://app.test", A: "ada@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(v, "v1.") {
		t.Fatalf("value %q lacks the v1. prefix", v)
	}
	// Decoded, not the base64 text: random ciphertext can spell a short
	// word by chance, but not fifteen plaintext bytes.
	raw, _ := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(v, "v1."))
	if bytes.Contains(raw, []byte("ada@example.com")) {
		t.Fatal("the payload is readable in the sealed value")
	}
	var got payload
	if err := Open(keyA, v, &got); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got != (payload{O: "https://app.test", A: "ada@example.com"}) {
		t.Fatalf("round trip gave %+v", got)
	}
}

func TestOpenRefusesAnythingItDidNotSeal(t *testing.T) {
	good, err := Seal(keyA, payload{O: "o", A: "a"})
	if err != nil {
		t.Fatal(err)
	}
	body := strings.TrimPrefix(good, "v1.")
	raw, _ := base64.RawURLEncoding.DecodeString(body)
	raw[len(raw)/2] ^= 0x01
	tampered := "v1." + base64.RawURLEncoding.EncodeToString(raw)

	otherKey, _ := Seal(keyB, payload{O: "o", A: "a"})
	extra, _ := Seal(keyA, struct {
		O string `json:"o"`
		A string `json:"a"`
		Z string `json:"z"`
	}{"o", "a", "z"})
	two, _ := crypto.SealSym(keyA, []byte(`{"o":"o","a":"a"} {"o":"x"}`))
	trailing := "v1." + base64.RawURLEncoding.EncodeToString(two)

	for name, v := range map[string]string{
		"tampered":        tampered,
		"another key":     otherKey,
		"unknown version": "v2." + body,
		"no version":      body,
		"not base64":      "v1.!!!",
		"unknown field":   extra,
		"trailing data":   trailing,
		"empty":           "",
	} {
		var got payload
		if err := Open(keyA, v, &got); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: Open = %v, want ErrInvalid", name, err)
		}
	}
}

func TestFreshJudgesOriginAndTime(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	n := now.Unix()
	for _, c := range []struct {
		name     string
		origin   string
		iat, exp int64
		want     bool
	}{
		{"current", "https://app.test", n - 60, n + 60, true},
		{"another origin", "https://other.test", n - 60, n + 60, false},
		{"expiring exactly now", "https://app.test", n - 600, n, false},
		{"a minute ahead is clock drift", "https://app.test", n + 60, n + 120, true},
		{"further ahead is not", "https://app.test", n + 61, n + 120, false},
		{"exactly the cap", "https://app.test", n - 10, n - 10 + 900, true},
		{"longer than the cap", "https://app.test", n - 10, n - 10 + 901, false},
		{"no issue time", "https://app.test", 0, n + 60, false},
		{"expiring before issue", "https://app.test", n, n - 1, false},
	} {
		if got := Fresh(c.origin, "https://app.test", c.iat, c.exp, now, 15*time.Minute); got != c.want {
			t.Errorf("%s: Fresh = %v, want %v", c.name, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `GOFLAGS=-mod=mod go test ./internal/sealedcookie/ -count=1`
Expected: FAIL — `undefined: Seal`, `undefined: Open`, `undefined: Fresh`, `undefined: ErrInvalid`.

- [ ] **Step 3: Write the implementation**

`internal/sealedcookie/sealedcookie.go`:

```go
// Package sealedcookie is the one envelope the sign-in screen's cookies
// share: "v1." + base64url(AES-256-GCM(JSON)).
//
// auth's attempt and continuation cookies and lastsignin's remembered
// cookie each carry something the browser must neither read nor edit —
// an address, an authorize URL — and each must expire on a clock the
// browser cannot move. Three hand-written envelopes would agree until
// the day one of them was edited; this is the one they share.
//
// crypto.SealSym checks nothing about time, so expiry lives in the
// payload and Fresh is the one place those times are judged. A cookie's
// Max-Age is a courtesy to the browser and is never trusted: a copied
// value presented after its Max-Age must still pass Fresh.
package sealedcookie

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"amadan.net/rastrillo/rastrillo/crypto"
)

// version prefixes every value. A value without it — a future v2, or a
// cookie older code wrote under the same name — is refused before any
// decryption, so a format change can never be misread as this one.
const version = "v1."

// ErrInvalid is every way a value can fail to open. One error on
// purpose: the caller's only decision is "treat it as absent and clear
// it", and a caller able to tell tampering from expiry would be tempted
// to say which to the browser.
var ErrInvalid = errors.New("sealedcookie: invalid value")

// FutureSkew is how far ahead of this server's clock an issue time may
// sit and still be believed. A minute absorbs drift between instances
// behind one origin; more, and a value sealed by a fast clock would
// outlive its cap by the difference.
const FutureSkew = time.Minute

// Seal encrypts payload's JSON under key (32 bytes, a crypto.Derive
// output, one context per cookie so no cookie can open another's).
func Seal(key []byte, payload any) (string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sealed, err := crypto.SealSym(key, raw)
	if err != nil {
		return "", err
	}
	return version + base64.RawURLEncoding.EncodeToString(sealed), nil
}

// Open decrypts value into payload. Decoding is strict — an unknown
// field or trailing data is refused — so a payload some other code or
// version wrote can never be half-read as this one.
func Open(key []byte, value string, payload any) error {
	body, ok := strings.CutPrefix(value, version)
	if !ok {
		return ErrInvalid
	}
	sealed, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return ErrInvalid
	}
	raw, err := crypto.OpenSym(key, sealed)
	if err != nil {
		return ErrInvalid
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(payload); err != nil {
		return ErrInvalid
	}
	if _, err := dec.Token(); err != io.EOF {
		return ErrInvalid
	}
	return nil
}

// Fresh reports whether a payload's origin and times can be trusted at
// now: sealed for this origin (so an InstanceKey shared across
// instances cannot move a cookie between them), not expired, not issued
// further in the future than FutureSkew, and living no longer than max —
// so a value sealed with a longer life, by a bug or an older version,
// is refused rather than honoured.
func Fresh(origin, want string, iat, exp int64, now time.Time, max time.Duration) bool {
	n := now.Unix()
	return origin == want &&
		iat > 0 && exp > iat &&
		n < exp &&
		iat <= n+int64(FutureSkew/time.Second) &&
		exp-iat <= int64(max/time.Second)
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `GOFLAGS=-mod=mod go test ./internal/sealedcookie/ -count=1 -v`
Expected: PASS, three tests.

- [ ] **Step 5: Run the gate and commit**

Run: `GOFLAGS=-mod=mod go vet ./... && gofmt -l . && GOFLAGS=-mod=mod go test ./... -count=1`
Expected: no gofmt output; all packages `ok`.

```bash
git add internal/sealedcookie
git commit -F - <<'EOF'
Add the sealed-cookie envelope the sign-in screen's cookies share

The attempt, continuation and remembered-method cookies all carry an
address or an authorize URL the browser must not read or edit, and all
must expire on a clock the browser cannot move. crypto.SealSym checks
no time, so expiry has to live in the payload; three envelopes written
separately would agree until one was edited. Strict decoding refuses
unknown fields and trailing data so one cookie's payload can never be
half-read as another's.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
```

---

### Task 2: The `lastsignin` jar

**Files:**
- Create: `lastsignin/lastsignin.go`
- Test: `lastsignin/lastsignin_test.go`
- Modify: `Makefile` (add `./lastsignin` to `GORM_FREE`)

**Interfaces:**
- Consumes: `sealedcookie.Seal/Open/Fresh` (Task 1), `crypto.Derive`.
- Produces (spec §1.5):
  - `type Mode int` with `Off`, `Forgetting`, `On`.
  - `type Config struct{ Origin, InstanceKey, AttemptCookie string; Mode Mode; Now func() time.Time }`.
  - `type Record struct{ Method, Address string }`; constants `MethodKeymail = "keymail"`, `MethodMagicLink = "magiclink"`, `MethodPasskey = "passkey"`.
  - `type ReadResult int` with `Absent`, `Valid`, `Invalid`.
  - `func New(Config) (*Jar, error)`; `(*Jar) Mode() Mode`; `(*Jar) CookieName() string`; `(*Jar) EndAttempt(http.ResponseWriter)`; `(*Jar) Remember(http.ResponseWriter, Record)`; `(*Jar) Read(*http.Request) (Record, ReadResult)`; `(*Jar) Clear(http.ResponseWriter)`.

- [ ] **Step 1: Write the failing test**

`lastsignin/lastsignin_test.go` (internal package, so it can seal a payload the public API would refuse to write):

```go
package lastsignin

import (
	"bytes"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/internal/sealedcookie"
)

const origin = "https://app.test"

func newJar(t *testing.T, mode Mode, now func() time.Time) *Jar {
	t.Helper()
	j, err := New(Config{Origin: origin, InstanceKey: "k", AttemptCookie: "__Host-rastrillo_attempt", Mode: mode, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	return j
}

// carrying is a request bearing every cookie w set, deletions dropped —
// what the browser sends back on its next request.
func carrying(w *httptest.ResponseRecorder) *http.Request {
	r := httptest.NewRequest(http.MethodGet, origin+"/signin", nil)
	for _, c := range w.Result().Cookies() {
		if c.MaxAge >= 0 {
			r.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value})
		}
	}
	return r
}

func TestRememberThenReadRoundTrips(t *testing.T) {
	j := newJar(t, On, nil)
	for _, rec := range []Record{
		{MethodKeymail, "kay@example.org"},
		{MethodMagicLink, "ada@example.com"},
		{MethodPasskey, ""},
	} {
		w := httptest.NewRecorder()
		j.Remember(w, rec)
		if got, res := j.Read(carrying(w)); res != Valid || got != rec {
			t.Errorf("Remember(%+v) then Read = %+v, %v", rec, got, res)
		}
	}
}

func TestTheCookieIsHostOnlyHttpOnlyAndLastsFourHundredDays(t *testing.T) {
	w := httptest.NewRecorder()
	newJar(t, On, nil).Remember(w, Record{MethodMagicLink, "ada@example.com"})
	cs := w.Result().Cookies()
	if len(cs) != 1 {
		t.Fatalf("%d cookies, want 1", len(cs))
	}
	c := cs[0]
	if c.Name != "__Host-rastrillo_last_signin" || !c.Secure || !c.HttpOnly ||
		c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.MaxAge != 400*24*3600 {
		t.Fatalf("cookie = %+v", c)
	}
	raw, _ := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(c.Value, "v1."))
	if bytes.Contains(raw, []byte("ada@example.com")) {
		t.Fatal("the address is readable in the cookie")
	}
}

func TestAPlainHTTPOriginGetsTheUnprefixedName(t *testing.T) {
	j, err := New(Config{Origin: "http://app.test", InstanceKey: "k", AttemptCookie: "rastrillo_attempt", Mode: On})
	if err != nil {
		t.Fatal(err)
	}
	if j.CookieName() != "rastrillo_last_signin" {
		t.Fatalf("CookieName = %q; __Host- requires Secure, which a plain-http origin cannot set", j.CookieName())
	}
}

func TestOffWritesReadsAndDeletesNothing(t *testing.T) {
	w := httptest.NewRecorder()
	newJar(t, On, nil).Remember(w, Record{MethodMagicLink, "ada@example.com"})
	r := carrying(w)
	r.AddCookie(&http.Cookie{Name: "__Host-rastrillo_attempt", Value: "x"})

	off := newJar(t, Off, nil)
	w = httptest.NewRecorder()
	off.Remember(w, Record{MethodMagicLink, "ada@example.com"})
	off.EndAttempt(w)
	off.Clear(w)
	if h := w.Header().Values("Set-Cookie"); len(h) != 0 {
		t.Fatalf("Off wrote %v; an app without the screen must see no cookie traffic", h)
	}
	if _, res := off.Read(r); res != Absent {
		t.Fatalf("Off read %v from a valid cookie, want Absent", res)
	}
}

func TestForgettingDeletesAndNeverReads(t *testing.T) {
	w := httptest.NewRecorder()
	newJar(t, On, nil).Remember(w, Record{MethodMagicLink, "ada@example.com"})
	r := carrying(w)

	j := newJar(t, Forgetting, nil)
	if _, res := j.Read(r); res != Invalid {
		t.Fatalf("Forgetting read %v, want Invalid so the screen clears it", res)
	}
	w = httptest.NewRecorder()
	j.Remember(w, Record{MethodMagicLink, "ada@example.com"})
	j.EndAttempt(w)
	got := map[string]int{}
	for _, c := range w.Result().Cookies() {
		got[c.Name] = c.MaxAge
	}
	if got["__Host-rastrillo_last_signin"] >= 0 || got["__Host-rastrillo_attempt"] >= 0 || len(got) != 2 {
		t.Fatalf("Forgetting wrote %v, want both cookies deleted and nothing else", got)
	}
}

func TestRememberRefusesARecordItCouldNotReadBack(t *testing.T) {
	j := newJar(t, On, nil)
	for _, rec := range []Record{
		{MethodPasskey, "ada@example.com"},
		{MethodKeymail, ""},
		{MethodMagicLink, "no-at-sign"},
		{MethodMagicLink, "a@b@c.example"},
		{MethodMagicLink, strings.Repeat("a", 250) + "@x.example"},
		{MethodMagicLink, "ada\n@example.com"},
		{"password", "ada@example.com"},
	} {
		w := httptest.NewRecorder()
		j.Remember(w, rec)
		if h := w.Header().Values("Set-Cookie"); len(h) != 0 {
			t.Errorf("Remember(%+v) wrote %v", rec, h)
		}
	}
}

func TestReadRefusesWhatWasNotSealedForThisOriginAndTime(t *testing.T) {
	clock := time.Unix(1_800_000_000, 0)
	at := func(d time.Duration) func() time.Time { return func() time.Time { return clock.Add(d) } }
	j := newJar(t, On, at(0))
	w := httptest.NewRecorder()
	j.Remember(w, Record{MethodMagicLink, "ada@example.com"})
	r := carrying(w)

	other, _ := New(Config{Origin: "https://other.test", InstanceKey: "k", AttemptCookie: "a", Mode: On, Now: at(0)})
	rotated, _ := New(Config{Origin: origin, InstanceKey: "rotated", AttemptCookie: "a", Mode: On, Now: at(0)})
	for name, reader := range map[string]*Jar{
		"another origin":        other,
		"a rotated key":         rotated,
		"after 400 days":        newJar(t, On, at(400*24*time.Hour)),
		"issued in the future": newJar(t, On, at(-2*time.Minute)),
	} {
		// other.test's jar reads the same __Host- name, so it sees the cookie.
		if _, res := reader.Read(r); res != Invalid {
			t.Errorf("%s: Read = %v, want Invalid", name, res)
		}
	}

	now := clock.Unix()
	for name, p := range map[string]payload{
		"an unknown method":       {O: origin, M: "password", A: "ada@example.com", IAT: now, EXP: now + 3600},
		"a passkey with an address": {O: origin, M: MethodPasskey, A: "ada@example.com", IAT: now, EXP: now + 3600},
		"longer than 400 days":    {O: origin, M: MethodPasskey, IAT: now, EXP: now + 401*24*3600},
	} {
		v, err := sealedcookie.Seal(j.key, p)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(http.MethodGet, origin+"/", nil)
		r.AddCookie(&http.Cookie{Name: j.CookieName(), Value: v})
		if _, res := j.Read(r); res != Invalid {
			t.Errorf("%s: Read = %v, want Invalid", name, res)
		}
	}
}

func TestNewRefusesAConfigItCannotHonour(t *testing.T) {
	for name, cfg := range map[string]Config{
		"a relative origin":        {Origin: "app.test", InstanceKey: "k", AttemptCookie: "a", Mode: On},
		"no instance key":          {Origin: origin, AttemptCookie: "a", Mode: On},
		"On with no attempt name":  {Origin: origin, InstanceKey: "k", Mode: On},
		"Forgetting with no name": {Origin: origin, InstanceKey: "k", Mode: Forgetting},
	} {
		if _, err := New(cfg); err == nil {
			t.Errorf("New(%s) succeeded", name)
		}
	}
	if _, err := New(Config{Origin: origin, InstanceKey: "k", Mode: Off}); err != nil {
		t.Errorf("Off needs no attempt cookie name: %v", err)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `GOFLAGS=-mod=mod go test ./lastsignin/ -count=1`
Expected: FAIL — `undefined: Mode`, `undefined: New`, …

- [ ] **Step 3: Write the implementation**

`lastsignin/lastsignin.go`:

```go
// Package lastsignin remembers, in this browser only, which way in it
// last used — keymail, a magic link or a passkey — and the address for
// the two that have one, so the sign-in screen can offer a one-tap.
//
// It also owns the one seam every sign-in path calls when an attempt
// ends, EndAttempt, so auth and passkey clear the screen's attempt
// cookie the same way — and passkey never needs auth's InstanceKey or
// its keymail dependency to do it.
//
// What the remembered cookie is never evidence of: identity, admission,
// freshness or a second factor. Nothing reads it but the sign-in screen
// and this jar; a one-tap posts its address to auth.Begin exactly as if
// it had been typed, where it is classified, rate-limited and proved
// afresh.
package lastsignin

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"amadan.net/rastrillo/rastrillo/crypto"
	"amadan.net/rastrillo/rastrillo/internal/sealedcookie"
)

// Mode is what the jar is allowed to do, decided once from auth's
// configuration.
type Mode int

const (
	// Off: the app does not render the shipped screen. Nothing is
	// written, read or deleted — not one Set-Cookie header — which is
	// what "an app that leaves SigninScreen off sees no change" needs.
	Off Mode = iota
	// Forgetting: the screen is on and remembering is switched off (a
	// shared kiosk). Remember deletes what an earlier configuration
	// wrote, and Read reports any cookie as Invalid so the sign-in page
	// clears it too — turning remembering off forgets, it does not just
	// stop writing.
	Forgetting
	// On: the screen is on and remembers the way in.
	On
)

// The methods a record may name — the Identity methods auth mints, and
// passkey discovery's.
const (
	MethodKeymail   = "keymail"
	MethodMagicLink = "magiclink"
	MethodPasskey   = "passkey"
)

// lifetime is 400 days because that is the longest Max-Age browsers
// honour (RFC 6265bis clamps anything longer); every sign-in rewrites
// the cookie, so the 400 days run from the latest one.
const lifetime = 400 * 24 * time.Hour

// Config configures New.
type Config struct {
	// Origin decides Secure and the __Host- prefix, and is sealed into
	// the value so a key shared across instances cannot move a cookie
	// between them.
	Origin string
	// InstanceKey is auth's; the jar derives its own key from it.
	InstanceKey string
	// AttemptCookie is the attempt cookie's resolved name. auth passes
	// it in so EndAttempt can delete that cookie without knowing its
	// format. Required unless Mode is Off.
	AttemptCookie string
	Mode          Mode
	// Now is the clock values are sealed and judged by; time.Now if nil.
	Now func() time.Time
}

// Record is one remembered way in. Address is empty exactly when Method
// is MethodPasskey: discovery knows a subject, not an address.
type Record struct {
	Method  string
	Address string
}

// ReadResult says what Read found.
type ReadResult int

const (
	// Absent: no cookie, or the jar is Off.
	Absent ReadResult = iota
	// Valid: a record this jar sealed, for this origin, still in date.
	Valid
	// Invalid: a cookie is present and must not be believed — tampered,
	// expired, sealed elsewhere, or present while remembering is off.
	// The caller clears it.
	Invalid
)

// Jar is the remembered cookie and the attempt's end. Build one per
// Auth (auth.New does) and share it with passkey.
type Jar struct {
	cfg Config
	key []byte
}

// payload is the sealed value. Field names are short because every byte
// rides every request to this origin for 400 days.
type payload struct {
	O   string `json:"o"`
	M   string `json:"m"`
	A   string `json:"a,omitempty"`
	IAT int64  `json:"iat"`
	EXP int64  `json:"exp"`
}

// New validates cfg and derives the jar's key.
func New(cfg Config) (*Jar, error) {
	if !strings.HasPrefix(cfg.Origin, "https://") && !strings.HasPrefix(cfg.Origin, "http://") {
		return nil, errors.New("rastrillo/lastsignin: Config.Origin must be an absolute origin like https://app.example.com")
	}
	if cfg.InstanceKey == "" {
		return nil, errors.New("rastrillo/lastsignin: Config.InstanceKey must not be empty")
	}
	if cfg.Mode != Off && cfg.AttemptCookie == "" {
		return nil, errors.New("rastrillo/lastsignin: Config.AttemptCookie is required unless Mode is Off")
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Jar{cfg: cfg, key: crypto.Derive([]byte(cfg.InstanceKey), "rastrillo/lastsignin/v1")}, nil
}

// Mode is the mode the jar was built with.
func (j *Jar) Mode() Mode { return j.cfg.Mode }

func (j *Jar) secure() bool { return strings.HasPrefix(j.cfg.Origin, "https://") }

// CookieName is the remembered cookie's name for this origin: __Host-
// on https, where the prefix pins it to this host and path; unprefixed
// on a plain-http dev origin, because __Host- requires Secure.
func (j *Jar) CookieName() string {
	if j.secure() {
		return "__Host-rastrillo_last_signin"
	}
	return "rastrillo_last_signin"
}

func (j *Jar) set(w http.ResponseWriter, name, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: value, Path: "/",
		MaxAge: maxAge, HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Secure: j.secure(),
	})
}

// EndAttempt deletes the sign-in screen's attempt cookie. Every path
// that verifies a first factor calls it: an attempt left behind keeps
// prefilling the form with an address the visitor has already proved —
// or, after a passkey sign-in, somebody else's.
func (j *Jar) EndAttempt(w http.ResponseWriter) {
	if j.cfg.Mode == Off {
		return
	}
	j.set(w, j.cfg.AttemptCookie, "", -1)
}

// Remember records the way in this browser just used. A record Read
// would refuse is not written at all, so a bad caller can never leave a
// cookie behind that only ever reads as Invalid.
func (j *Jar) Remember(w http.ResponseWriter, rec Record) {
	switch j.cfg.Mode {
	case Off:
		return
	case Forgetting:
		j.set(w, j.CookieName(), "", -1)
		return
	}
	if !validRecord(rec) {
		return
	}
	now := j.cfg.Now().Unix()
	v, err := sealedcookie.Seal(j.key, payload{
		O: j.cfg.Origin, M: rec.Method, A: rec.Address,
		IAT: now, EXP: now + int64(lifetime/time.Second),
	})
	if err != nil {
		// Only crypto/rand can fail here. Remembering is a convenience;
		// failing a verified sign-in over it would be the wrong trade.
		return
	}
	j.set(w, j.CookieName(), v, int(lifetime/time.Second))
}

// Read returns the remembered record. It is never Valid unless the jar
// is On.
func (j *Jar) Read(r *http.Request) (Record, ReadResult) {
	if j.cfg.Mode == Off {
		return Record{}, Absent
	}
	c, err := r.Cookie(j.CookieName())
	if err != nil {
		return Record{}, Absent
	}
	if j.cfg.Mode == Forgetting {
		return Record{}, Invalid
	}
	var p payload
	if sealedcookie.Open(j.key, c.Value, &p) != nil ||
		!sealedcookie.Fresh(p.O, j.cfg.Origin, p.IAT, p.EXP, j.cfg.Now(), lifetime) {
		return Record{}, Invalid
	}
	rec := Record{Method: p.M, Address: p.A}
	if !validRecord(rec) {
		return Record{}, Invalid
	}
	return rec, Valid
}

// Clear deletes the remembered cookie: "Use a different email".
func (j *Jar) Clear(w http.ResponseWriter) {
	if j.cfg.Mode == Off {
		return
	}
	j.set(w, j.CookieName(), "", -1)
}

func validRecord(rec Record) bool {
	switch rec.Method {
	case MethodPasskey:
		return rec.Address == ""
	case MethodKeymail, MethodMagicLink:
		return validAddress(rec.Address)
	}
	return false
}

// validAddress is the shape a remembered address must have: at most 254
// bytes (the longest address SMTP carries), exactly one @, and no
// control bytes — the address is shown back on the screen and posted
// back to Begin, and a CR or LF in either place is an injection waiting
// for a careless consumer.
func validAddress(a string) bool {
	return a != "" && len(a) <= 254 && strings.Count(a, "@") == 1 &&
		!strings.ContainsFunc(a, func(r rune) bool { return r < 0x20 || r == 0x7f })
}
```

In `Makefile`, extend `GORM_FREE` (the line ending `./perf`):

```make
            ./xlsx ./table \
            ./perf ./lastsignin
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `GOFLAGS=-mod=mod go test ./lastsignin/ -count=1 -v && GOFLAGS=-mod=mod make gorm-free`
Expected: PASS for all tests; `gorm-free` prints nothing and exits 0.

- [ ] **Step 5: Run the gate and commit**

Run: `GOFLAGS=-mod=mod go vet ./... && gofmt -l . && GOFLAGS=-mod=mod go test ./... -count=1`

```bash
git add lastsignin Makefile
git commit -F - <<'EOF'
Add lastsignin: the remembered way in, and the attempt's one end

auth and passkey both have to clear the sign-in screen's attempt cookie
when a first factor verifies, and both have to remember the way in the
same way. Putting that in a leaf package keeps passkey from needing
auth's InstanceKey or its keymail dependency. The Off mode writes, reads
and deletes nothing, which is what lets an app that never renders the
screen see no cookie traffic at all; Forgetting deletes what an earlier
configuration remembered instead of only ceasing to write.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
```

---
### Task 3: `auth` — the switch, the attempt cookie, `AnswerAsSent`, and `admit`'s seam

**Files:**
- Create: `auth/attempt.go`, `auth/screen_test.go`
- Modify: `auth/auth.go` (Config fields, `Auth` fields, `New`, `RememberJar`), `auth/handlers.go` (`Begin`, new `AnswerAsSent`, `admit`), `auth/auth_test.go:32-55` (`newTestAuth` also migrates `secondfactor.Schema`)

**Interfaces:**
- Consumes: `lastsignin.New/Config/Mode/Record` (Task 2); `sealedcookie.Seal/Open/Fresh` (Task 1); `crypto.Derive`.
- Produces:
  - `auth.Config` fields `SigninScreen bool`, `BeginPath string`, `ForgetPath string`, `Remember *bool` (spec §1.1). Defaults `BeginPath="/signin"`, `ForgetPath="/signin/forget"`.
  - `func (a *Auth) AnswerAsSent(w http.ResponseWriter, r *http.Request)`.
  - `func (a *Auth) RememberJar() *lastsignin.Jar`.
  - Unexported, used by Tasks 5–6: `type attempt struct{ O, ID string; IAT, EXP int64; K, A string; X bool }` (json `o,id,iat,exp,k,a,x`); consts `attemptLink="link"`, `attemptProblem="problem"`, `attemptKeymail="keymail"`, `attemptTTL = 15*time.Minute`; `type cookieState int` with `cookieAbsent`, `cookieValid`, `cookieInvalid`; `(a *Auth) attemptCookie() string`; `(a *Auth) openAttempt(*http.Request) (attempt, cookieState)`; `(a *Auth) noteAttempt(w, kind, address string, expectedKeymail bool) string`; `(a *Auth) answerSent(w, r, address string)`; `newID() (string, error)`; fields `a.jar *lastsignin.Jar`, `a.attemptKey []byte`, `a.now func() time.Time`.
  - Test helpers (package `auth`, `screen_test.go`), used by every later auth task: `newScreenAuth(t, mut)`, `type browser`, `newBrowser()`, `(*browser) request/do/apply/clone/cookie`, `setCookies(w) []string`, `cookieShape(w) []string`, `redirectShape(w) string`, `holdingGate(t, a) *secondfactor.Gate`, `ptr[T]`, `failingMailer`.

- [ ] **Step 1: Widen the test migration set**

`auth/auth_test.go`, in `newTestAuth` — the held-sign-in tests need the second-factor tables in the same database:

```go
	// secondfactor.Schema as well: the screen's tests hold sign-ins at a
	// real secondfactor.Gate, whose half-session table must exist.
	if _, err := migrate.Apply(context.Background(), d, migrate.Merge(sessions.Schema, Schema, secondfactor.Schema)); err != nil {
		t.Fatalf("migrate.Apply: %v", err)
	}
```

Add `"amadan.net/rastrillo/rastrillo/secondfactor"` to its imports. Run `GOFLAGS=-mod=mod go test ./auth/ -count=1` — expected PASS (no behaviour change).

- [ ] **Step 2: Write the failing tests**

`auth/screen_test.go`:

```go
package auth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/crypto"
	"amadan.net/rastrillo/rastrillo/internal/sealedcookie"
	"amadan.net/rastrillo/rastrillo/lastsignin"
	"amadan.net/rastrillo/rastrillo/secondfactor"
	"amadan.net/rastrillo/rastrillo/sessions"
)

func ptr[T any](v T) *T { return &v }

// newScreenAuth is newTestAuth with the shipped screen switched on.
func newScreenAuth(t *testing.T, mut func(*Config)) (*Auth, *captureMailer) {
	t.Helper()
	return newTestAuth(t, func(c *Config) {
		c.SigninScreen = true
		if mut != nil {
			mut(c)
		}
	})
}

// browser is one cookie jar applied the way a browser applies
// Set-Cookie: a later value wins, a deletion removes. The two-tab tests
// stage "the pair a browser installed last" by copying cookies from one
// jar into another, which is exactly the state the spec reasons about.
type browser struct{ jar map[string]*http.Cookie }

func newBrowser() *browser { return &browser{jar: map[string]*http.Cookie{}} }

func (b *browser) apply(w *httptest.ResponseRecorder) {
	for _, c := range w.Result().Cookies() {
		if c.MaxAge < 0 {
			delete(b.jar, c.Name)
			continue
		}
		b.jar[c.Name] = c
	}
}

// request builds one request from this browser: its cookies, and — for
// a POST — the same-origin evidence a real form submission carries.
func (b *browser) request(method, target string, form url.Values) *http.Request {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	r := httptest.NewRequest(method, "http://app.test"+target, body)
	if form != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if method == http.MethodPost {
		r.Header.Set("Sec-Fetch-Site", "same-origin")
	}
	for _, c := range b.jar {
		r.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value})
	}
	return r
}

func (b *browser) do(h http.HandlerFunc, method, target string, form url.Values) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h(w, b.request(method, target, form))
	b.apply(w)
	return w
}

func (b *browser) clone() *browser {
	c := newBrowser()
	for k, v := range b.jar {
		c.jar[k] = v
	}
	return c
}

func (b *browser) cookie(name string) *http.Cookie { return b.jar[name] }

// setCookies names every cookie a response writes, a deletion as
// "name:clear", sorted — the shape "exactly today's cookies" is
// asserted in.
func setCookies(w *httptest.ResponseRecorder) []string {
	var out []string
	for _, c := range w.Result().Cookies() {
		name := c.Name
		if c.MaxAge < 0 {
			name += ":clear"
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// cookieShape is every attribute of every Set-Cookie except its value —
// what two answers must share to be indistinguishable to a watcher.
func cookieShape(w *httptest.ResponseRecorder) []string {
	var out []string
	for _, c := range w.Result().Cookies() {
		out = append(out, fmt.Sprintf("%s path=%s httponly=%v samesite=%v secure=%v maxage=%d",
			c.Name, c.Path, c.HttpOnly, c.SameSite, c.Secure, c.MaxAge))
	}
	sort.Strings(out)
	return out
}

var attemptParam = regexp.MustCompile(`attempt=[A-Za-z0-9_-]+`)

// redirectShape is the Location with the random attempt id blanked.
func redirectShape(w *httptest.ResponseRecorder) string {
	return attemptParam.ReplaceAllString(w.Header().Get("Location"), "attempt=ID")
}

type alwaysEnrolled struct{}

func (alwaysEnrolled) Enrolled(string) (bool, error) { return true, nil }

// holdingGate is a real secondfactor.Gate that holds every sign-in, so
// a test can see the cookies the held path writes today.
func holdingGate(t *testing.T, a *Auth) *secondfactor.Gate {
	t.Helper()
	sess, err := sessions.New(sessions.Config{DB: a.cfg.DB, Origin: a.cfg.Origin})
	if err != nil {
		t.Fatal(err)
	}
	g, err := secondfactor.New(secondfactor.Config{Sessions: sess, DB: a.cfg.DB, Origin: a.cfg.Origin})
	if err != nil {
		t.Fatal(err)
	}
	g.Add(alwaysEnrolled{})
	return g
}

type failingMailer struct{}

func (failingMailer) Send(context.Context, string, string, string) error {
	return errors.New("the mail server is down")
}

func pathOf(link string) string { return strings.TrimPrefix(link, "http://app.test") }

// TestScreenOffIsToday holds the switch's promise on every path this
// task touches: with SigninScreen off and KeymailServers unset, whatever
// Remember says, every answer and every cookie is what it was before
// the screen existed — including the second-factor cookie a held
// sign-in writes (round 4, finding 29). The keymail branch's rows are
// in TestScreenOffKeymailIsToday (Task 5).
func TestScreenOffIsToday(t *testing.T) {
	for _, remember := range []*bool{nil, ptr(true), ptr(false)} {
		name := "nil"
		if remember != nil {
			name = strconv.FormatBool(*remember)
		}
		t.Run("Remember="+name, func(t *testing.T) {
			a, m := newTestAuth(t, func(c *Config) { c.Remember = remember })
			b := newBrowser()
			post := func(h http.HandlerFunc, address string, extra url.Values) *httptest.ResponseRecorder {
				form := url.Values{"address": {address}, "force": {"1"}}
				for k, v := range extra {
					form[k] = v
				}
				return b.do(h, http.MethodPost, "/signin", form)
			}
			want := func(what string, w *httptest.ResponseRecorder, loc string, cookies []string) {
				t.Helper()
				if w.Code != http.StatusSeeOther || w.Header().Get("Location") != loc {
					t.Errorf("%s: %d → %q, want 303 → %q", what, w.Code, w.Header().Get("Location"), loc)
				}
				if got := setCookies(w); !reflect.DeepEqual(got, cookies) {
					t.Errorf("%s: Set-Cookie %v, want %v", what, got, cookies)
				}
			}

			want("a link", post(a.Begin, "ada@example.com", url.Values{"expect": {"keymail"}}), "/signin?sent=1", nil)
			adaLink := linkRE.FindString(m.body)
			want("a bad address", post(a.Begin, "not an address", nil), "/signin?err=address", nil)
			for i := 0; i < 5; i++ {
				post(a.Begin, "bea@example.com", nil)
			}
			want("over budget", post(a.Begin, "bea@example.com", nil), "/signin?err=rate", nil)
			want("AnswerAsSent", post(a.AnswerAsSent, "cal@example.com", nil), "/signin?sent=1", nil)

			want("admit", b.do(a.Verify, http.MethodGet, pathOf(adaLink), nil), "/", []string{"rastrillo_session"})

			a.cfg.SecondFactor = holdingGate(t, a).Hold
			post(a.Begin, "dee@example.com", nil)
			want("admit, held", b.do(a.Verify, http.MethodGet, pathOf(linkRE.FindString(m.body)), nil),
				"/signin/confirm", []string{"rastrillo_secondfactor"})

			a.flow.Mailer = failingMailer{}
			want("a failure", post(a.Begin, "eve@example.com", nil), "/signin?err=1", nil)
		})
	}
}

func TestBeginWritesTheAttempt(t *testing.T) {
	a, _ := newScreenAuth(t, nil)
	b := newBrowser()
	current := func() attempt {
		t.Helper()
		at, st := a.openAttempt(b.request(http.MethodGet, "/signin", nil))
		if st != cookieValid {
			t.Fatalf("attempt cookie is %v, want valid", st)
		}
		return at
	}
	post := func(form url.Values) *httptest.ResponseRecorder {
		return b.do(a.Begin, http.MethodPost, "/signin", form)
	}

	w := post(url.Values{"address": {"  ada@example.com "}, "force": {"1"}})
	id, ok := strings.CutPrefix(w.Header().Get("Location"), "/signin?sent=1&attempt=")
	if !ok || id == "" {
		t.Fatalf("a sent link redirected to %q", w.Header().Get("Location"))
	}
	if at := current(); at.ID != id || at.K != attemptLink || at.A != "ada@example.com" || at.X {
		t.Fatalf("attempt = %+v, want the link just sent, trimmed, with no expect", at)
	}
	var c *http.Cookie
	for _, sc := range w.Result().Cookies() {
		if sc.Name == "rastrillo_attempt" {
			c = sc
		}
	}
	if c == nil || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.MaxAge != 900 {
		t.Fatalf("attempt cookie = %+v", c)
	}

	post(url.Values{"address": {"ada@example.com"}, "force": {"1"}, "expect": {"keymail"}})
	if at := current(); !at.X {
		t.Fatal("expect=keymail with a link sent did not record the surprise")
	}

	if w := post(url.Values{"address": {"not an address"}, "force": {"1"}}); w.Header().Get("Location") != "/signin?err=address" {
		t.Fatalf("bad address → %q", w.Header().Get("Location"))
	}
	if at := current(); at.K != attemptProblem || at.A != "not an address" {
		t.Fatalf("a refused address left attempt %+v, want kind problem with what was typed", at)
	}

	long := strings.Repeat("a", 250) + "@example.com"
	post(url.Values{"address": {long}, "force": {"1"}})
	if at := current(); at.A != "" {
		t.Fatalf("a %d-byte address was kept as %q; over 254 bytes it is dropped, not truncated", len(long), at.A)
	}

	var rated *httptest.ResponseRecorder
	for i := 0; i < 6; i++ {
		rated = post(url.Values{"address": {"fay@example.com"}, "force": {"1"}})
	}
	if rated.Header().Get("Location") != "/signin?err=rate" {
		t.Fatalf("sixth try → %q, want ?err=rate", rated.Header().Get("Location"))
	}
	if at := current(); at.K != attemptProblem || at.A != "fay@example.com" {
		t.Fatalf("rate-limited attempt = %+v", at)
	}
}

func TestAnAttemptOpensOnlyWhereItWasSealed(t *testing.T) {
	a, _ := newScreenAuth(t, nil)
	now := a.now().Unix()
	good := attempt{O: a.cfg.Origin, ID: "id", IAT: now, EXP: now + 900, K: attemptLink, A: "ada@example.com"}
	seal := func(key []byte, p any) string {
		v, err := sealedcookie.Seal(key, p)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	check := func(name, value string, want cookieState) {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "http://app.test/signin", nil)
		if value != "" {
			r.AddCookie(&http.Cookie{Name: a.attemptCookie(), Value: value})
		}
		if _, got := a.openAttempt(r); got != want {
			t.Errorf("%s: %v, want %v", name, got, want)
		}
	}
	other, expired, future, long := good, good, good, good
	other.O = "http://other.test"
	expired.IAT, expired.EXP = now-901, now-1
	future.IAT, future.EXP = now+120, now+900
	long.EXP = now + 901
	sealed := seal(a.attemptKey, good)

	check("absent", "", cookieAbsent)
	check("sealed here", sealed, cookieValid)
	check("another origin", seal(a.attemptKey, other), cookieInvalid)
	check("expired", seal(a.attemptKey, expired), cookieInvalid)
	check("issued in the future", seal(a.attemptKey, future), cookieInvalid)
	check("longer than 15 minutes", seal(a.attemptKey, long), cookieInvalid)
	check("under the continuation's key", seal(crypto.Derive([]byte("test-instance-key"), "rastrillo/auth/continue/v1"), good), cookieInvalid)
	check("unknown version", "v2."+strings.TrimPrefix(sealed, "v1."), cookieInvalid)
	check("unknown field", seal(a.attemptKey, struct {
		attempt
		Z string `json:"z"`
	}{good, "z"}), cookieInvalid)
}

func TestAnswerAsSentAnswersLikeASentLink(t *testing.T) {
	for _, screen := range []bool{false, true} {
		t.Run("SigninScreen="+strconv.FormatBool(screen), func(t *testing.T) {
			a, m := newTestAuth(t, func(c *Config) { c.SigninScreen = screen })
			form := url.Values{"address": {"ada@example.com"}, "force": {"1"}, "expect": {"keymail"}}
			sent := newBrowser().do(a.Begin, http.MethodPost, "/signin", form)
			m.to = ""
			refused := newBrowser().do(a.AnswerAsSent, http.MethodPost, "/signin", form)
			if m.to != "" {
				t.Fatal("AnswerAsSent sent mail")
			}
			if sent.Code != refused.Code || redirectShape(sent) != redirectShape(refused) ||
				!reflect.DeepEqual(cookieShape(sent), cookieShape(refused)) {
				t.Fatalf("a sent link and a refusal differ:\n sent    %d %q %v\n refused %d %q %v",
					sent.Code, redirectShape(sent), cookieShape(sent),
					refused.Code, redirectShape(refused), cookieShape(refused))
			}
			r := newBrowser().request(http.MethodPost, "/signin", form)
			r.Header.Set("Sec-Fetch-Site", "cross-site")
			w := httptest.NewRecorder()
			a.AnswerAsSent(w, r)
			if w.Code != http.StatusForbidden {
				t.Fatalf("a cross-site AnswerAsSent got %d, want 403", w.Code)
			}
		})
	}
}

func TestAdmitRemembersTheWayInAndEndsTheAttempt(t *testing.T) {
	signIn := func(t *testing.T, a *Auth, m *captureMailer, b *browser, address string) *httptest.ResponseRecorder {
		t.Helper()
		b.do(a.Begin, http.MethodPost, "/signin", url.Values{"address": {address}, "force": {"1"}})
		return b.do(a.Verify, http.MethodGet, pathOf(linkRE.FindString(m.body)), nil)
	}
	remembered := func(a *Auth, b *browser) (lastsignin.Record, lastsignin.ReadResult) {
		return a.jar.Read(b.request(http.MethodGet, "/signin", nil))
	}
	has := func(w *httptest.ResponseRecorder, name string) bool {
		for _, c := range setCookies(w) {
			if c == name {
				return true
			}
		}
		return false
	}

	t.Run("a magic link is remembered by address, not subject", func(t *testing.T) {
		a, m := newScreenAuth(t, func(c *Config) {
			c.SubjectFor = func(string) (string, error) { return "ref_1", nil }
		})
		b := newBrowser()
		w := signIn(t, a, m, b, "ada@example.com")
		if w.Header().Get("Location") != "/" {
			t.Fatalf("verify → %q", w.Header().Get("Location"))
		}
		if rec, res := remembered(a, b); res != lastsignin.Valid || rec != (lastsignin.Record{Method: "magiclink", Address: "ada@example.com"}) {
			t.Fatalf("remembered %+v, %v", rec, res)
		}
		if !has(w, "rastrillo_attempt:clear") || b.cookie("rastrillo_attempt") != nil {
			t.Fatal("the verified sign-in left the attempt cookie behind")
		}
	})
	t.Run("a sign-in held for a second factor is remembered too", func(t *testing.T) {
		a, m := newScreenAuth(t, nil)
		a.cfg.SecondFactor = holdingGate(t, a).Hold
		b := newBrowser()
		if w := signIn(t, a, m, b, "ada@example.com"); w.Header().Get("Location") != "/signin/confirm" {
			t.Fatalf("held verify → %q", w.Header().Get("Location"))
		}
		if _, res := remembered(a, b); res != lastsignin.Valid {
			t.Fatalf("a held sign-in was not remembered: %v", res)
		}
	})
	t.Run("refused by Authorize: the attempt ends and nothing is remembered", func(t *testing.T) {
		a, m := newScreenAuth(t, func(c *Config) { c.Authorize = func(string) bool { return false } })
		b := newBrowser()
		w := signIn(t, a, m, b, "ada@example.com")
		if w.Code != http.StatusForbidden || !has(w, "rastrillo_attempt:clear") || has(w, "rastrillo_last_signin") {
			t.Fatalf("refused sign-in: %d, Set-Cookie %v", w.Code, setCookies(w))
		}
	})
	t.Run("SubjectFor fails: nothing is remembered", func(t *testing.T) {
		a, m := newScreenAuth(t, func(c *Config) {
			c.SubjectFor = func(string) (string, error) { return "", errors.New("down") }
		})
		b := newBrowser()
		if w := signIn(t, a, m, b, "ada@example.com"); w.Code != http.StatusInternalServerError || has(w, "rastrillo_last_signin") {
			t.Fatalf("failed SubjectFor: %d, Set-Cookie %v", w.Code, setCookies(w))
		}
	})
	t.Run("Remember=false deletes what was remembered before", func(t *testing.T) {
		on, onMail := newScreenAuth(t, nil)
		b := newBrowser()
		signIn(t, on, onMail, b, "ada@example.com")
		off, offMail := newScreenAuth(t, func(c *Config) { c.Remember = ptr(false) })
		w := signIn(t, off, offMail, b, "ada@example.com")
		if !has(w, "rastrillo_last_signin:clear") || b.cookie("rastrillo_last_signin") != nil {
			t.Fatalf("Remember=false left the old cookie: Set-Cookie %v", setCookies(w))
		}
	})
	t.Run("signing out forgets nothing", func(t *testing.T) {
		a, m := newScreenAuth(t, nil)
		b := newBrowser()
		signIn(t, a, m, b, "ada@example.com")
		w := b.do(a.Signout, http.MethodPost, "/signout", nil)
		if has(w, "rastrillo_last_signin:clear") {
			t.Fatal("Signout deleted the remembered way in")
		}
		if _, res := remembered(a, b); res != lastsignin.Valid {
			t.Fatalf("after signout: %v", res)
		}
	})
	t.Run("a remembered cookie is not a session", func(t *testing.T) {
		a, m := newScreenAuth(t, nil)
		b := newBrowser()
		signIn(t, a, m, b, "ada@example.com")
		only := newBrowser()
		only.jar["rastrillo_last_signin"] = b.cookie("rastrillo_last_signin")
		w := httptest.NewRecorder()
		a.RequireSession(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			t.Fatal("a remembered-method cookie reached a guarded handler")
		})).ServeHTTP(w, only.request(http.MethodGet, "/private", nil))
		if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/signin" {
			t.Fatalf("guarded GET with only a remembered cookie: %d → %q", w.Code, w.Header().Get("Location"))
		}
	})
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `GOFLAGS=-mod=mod go test ./auth/ -count=1`
Expected: FAIL to compile — `unknown field SigninScreen`, `a.openAttempt undefined`, `a.AnswerAsSent undefined`, `a.jar undefined`, …

- [ ] **Step 4: Add the configuration and the jar**

`auth/auth.go` — in `Config`, after `SessionTTL`:

```go
	// SigninScreen says this app renders the shipped sign-in screen (ui's
	// signin partial, fed by SigninState). It turns on everything the
	// screen needs from auth at once: the attempt cookie that lets the
	// screen prefill an address and say where a link went, the keymail
	// continuation that keeps form-action 'self' strict, and remembering
	// the way in. Default false, and false is exactly the behaviour
	// before the screen existed: no new cookie is written, read or
	// deleted, and Begin and Callback answer as they always did. One
	// switch rather than three because they are one feature — an app
	// remembering the way in without the screen would be writing a
	// cookie nothing reads.
	SigninScreen bool

	// BeginPath and ForgetPath are where the app mounted Begin (POST) and
	// Forget (POST), for the screen's form actions. auth mounts nothing
	// itself, and neither can be inferred from SigninPath: an app may
	// mount Begin at /auth/begin. Defaults "/signin" and
	// "/signin/forget".
	BeginPath  string
	ForgetPath string

	// Remember switches off one part of SigninScreen: a pointer to false
	// keeps the screen and stops remembering the way in (a shared
	// kiosk), and deletes what was remembered before. Nil or true leaves
	// it on. With SigninScreen off it has no effect — nothing is
	// remembered to begin with. A pointer because an explicit false must
	// be told apart from unset.
	Remember *bool
```

In `Auth`, after `hops`:

```go
	// jar holds the remembered way in and ends an attempt; passkey gets
	// the same one through RememberJar.
	jar *lastsignin.Jar
	// attemptKey seals the attempt cookie. Its own derivation so it
	// opens nothing else and nothing else opens it.
	attemptKey []byte
	// now is the clock the screen's cookies are sealed and judged by.
	// time.Now outside tests.
	now func() time.Time
```

In `New`, with the other defaults:

```go
	if cfg.BeginPath == "" {
		cfg.BeginPath = "/signin"
	}
	if cfg.ForgetPath == "" {
		cfg.ForgetPath = "/signin/forget"
	}
```

Replace `a := &Auth{…}` and add the jar before `a.flow = …`:

```go
	a := &Auth{
		cfg: cfg, sessions: sess, hops: trustedHops(cfg.TrustedProxyHops, carlos.Running()),
		now:        time.Now,
		attemptKey: crypto.Derive([]byte(cfg.InstanceKey), "rastrillo/auth/attempt/v1"),
	}
	jar, err := lastsignin.New(lastsignin.Config{
		Origin: cfg.Origin, InstanceKey: cfg.InstanceKey,
		AttemptCookie: a.attemptCookie(), Mode: rememberMode(cfg),
		// Through a.now, not time.Now, so a test that moves auth's clock
		// moves the jar's too and the two never disagree about expiry.
		Now: func() time.Time { return a.now() },
	})
	if err != nil {
		return nil, err
	}
	a.jar = jar
```

After `New`:

```go
// rememberMode is the jar's mode, from the one switch: nothing at all
// without the screen, whatever Remember says; with it, Remember=false
// forgets and anything else remembers.
func rememberMode(cfg Config) lastsignin.Mode {
	switch {
	case !cfg.SigninScreen:
		return lastsignin.Off
	case cfg.Remember != nil && !*cfg.Remember:
		return lastsignin.Forgetting
	default:
		return lastsignin.On
	}
}

// RememberJar is the jar holding this browser's remembered way in, and
// the seam that ends a sign-in attempt. Give it to
// passkey.Config.Remember so a passkey sign-in clears the screen's
// typed address and is remembered like the other two ways in. With
// SigninScreen off the jar is inert, so wiring it is always safe.
func (a *Auth) RememberJar() *lastsignin.Jar { return a.jar }
```

Imports to add in `auth/auth.go`: `"amadan.net/rastrillo/rastrillo/crypto"`, `"amadan.net/rastrillo/rastrillo/lastsignin"`.

- [ ] **Step 5: Add the attempt cookie**

`auth/attempt.go`:

```go
package auth

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"strings"
	"time"

	"amadan.net/rastrillo/rastrillo/internal/sealedcookie"
)

// attemptTTL is the magic link's own lifetime (signin's LinkTTL
// default): the Sent page may name where a link went for exactly as
// long as that link can work, and no longer.
const attemptTTL = 15 * time.Minute

// The kinds of attempt: a link went out, Begin answered with a problem,
// or a keymail round trip started.
const (
	attemptLink    = "link"
	attemptProblem = "problem"
	attemptKeymail = "keymail"
)

// attempt is the latest thing this browser submitted to Begin, kept for
// the screen: the prefill after a problem, and the address on the Sent
// page. It decides nothing about signing in.
type attempt struct {
	O   string `json:"o"`
	ID  string `json:"id"`
	IAT int64  `json:"iat"`
	EXP int64  `json:"exp"`
	K   string `json:"k"`
	A   string `json:"a,omitempty"`
	// X: this link went out where the remembered-Keymail one-tap
	// promised Keymail. It changes one line of this browser's own Sent
	// page and nothing else.
	X bool `json:"x,omitempty"`
}

type cookieState int

const (
	cookieAbsent cookieState = iota
	cookieValid
	cookieInvalid
)

func (a *Auth) attemptCookie() string { return a.cookieName("rastrillo_attempt") }

// newID is 16 random bytes, base64url: an attempt or continuation id.
func newID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// attemptAddress is what of a submitted address the cookie may keep:
// trimmed, and only if it is at most 254 bytes with no control bytes.
// Anything longer is dropped rather than truncated — a truncated
// address shown back on the Sent page would name somebody else.
func attemptAddress(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 254 || strings.ContainsFunc(s, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return ""
	}
	return s
}

// noteAttempt records what this browser just submitted and returns the
// attempt's id, or "" when nothing was written: the screen is off, or
// crypto/rand failed — in which case the answer falls back to today's
// unbound one rather than failing a sign-in over a screen nicety.
func (a *Auth) noteAttempt(w http.ResponseWriter, kind, address string, expectedKeymail bool) string {
	if !a.cfg.SigninScreen {
		return ""
	}
	id, err := newID()
	if err != nil {
		a.cfg.Logger.Error("rastrillo/auth: attempt id", "err", err)
		return ""
	}
	now := a.now().Unix()
	v, err := sealedcookie.Seal(a.attemptKey, attempt{
		O: a.cfg.Origin, ID: id, IAT: now, EXP: now + int64(attemptTTL/time.Second),
		K: kind, A: attemptAddress(address), X: kind == attemptLink && expectedKeymail,
	})
	if err != nil {
		a.cfg.Logger.Error("rastrillo/auth: seal attempt", "err", err)
		return ""
	}
	a.setCookie(w, a.attemptCookie(), v, int(attemptTTL/time.Second))
	return id
}

// openAttempt reads this browser's attempt. Invalid means present and
// not to be believed; SigninState marks it for deletion.
func (a *Auth) openAttempt(r *http.Request) (attempt, cookieState) {
	c, err := r.Cookie(a.attemptCookie())
	if err != nil {
		return attempt{}, cookieAbsent
	}
	var p attempt
	if sealedcookie.Open(a.attemptKey, c.Value, &p) != nil ||
		!sealedcookie.Fresh(p.O, a.cfg.Origin, p.IAT, p.EXP, a.now(), attemptTTL) {
		return attempt{}, cookieInvalid
	}
	return p, cookieValid
}

// answerSent is the magic-link answer, shared by Begin and AnswerAsSent
// so the two cannot drift: an address an admission wrapper refused and
// one that got a link must look the same down to the cookie, or the
// difference is a membership oracle (round 2, finding 17).
func (a *Auth) answerSent(w http.ResponseWriter, r *http.Request, address string) {
	id := a.noteAttempt(w, attemptLink, address, r.FormValue("expect") == "keymail")
	if id == "" {
		a.redirect(w, r, a.cfg.SigninPath+"?sent=1")
		return
	}
	a.redirect(w, r, a.cfg.SigninPath+"?sent=1&attempt="+id)
}
```

- [ ] **Step 6: Route `Begin`, add `AnswerAsSent`, and give `admit` its seam**

`auth/handlers.go` — `Begin`'s error cases each note a problem attempt before redirecting, and the magic-link tail uses `answerSent`:

```go
	switch {
	case errors.Is(err, signin.ErrRateLimited):
		a.noteAttempt(w, attemptProblem, address, false)
		a.redirect(w, r, a.cfg.SigninPath+"?err=rate")
		return
	case errors.Is(err, signin.ErrBadAddress):
		a.noteAttempt(w, attemptProblem, address, false)
		a.redirect(w, r, a.cfg.SigninPath+"?err=address")
		return
	case err != nil:
		a.cfg.Logger.Error("rastrillo/auth: begin sign-in", "err", err)
		a.noteAttempt(w, attemptProblem, address, false)
		a.redirect(w, r, a.cfg.SigninPath+"?err=1")
		return
	}
```

and replace the final `a.redirect(w, r, a.cfg.SigninPath+"?sent=1")` with `a.answerSent(w, r, address)`. Extend `Begin`'s doc comment:

```go
// With Config.SigninScreen on, every answer also records this attempt
// for the screen (the attempt cookie), a sent link lands on
// ?sent=1&attempt=<id> so the Sent page can name the address, and an
// expect=keymail field — which only the remembered-Keymail one-tap
// sends — marks a link that went out where Keymail was promised. expect
// never chooses a path.
```

After `Begin`, add:

```go
// AnswerAsSent is Begin's magic-link answer without the magic link, for
// an admission wrapper that refuses an address before Begin can
// classify it. A wrapper that answered a refusal with a plain ?sent=1
// would give a refused address no attempt cookie and no attempt= while
// an admitted one got both: a membership oracle on the first try. This
// answers exactly as Begin does for a sent link, down to the cookie,
// and sends nothing. With SigninScreen off that is today's plain
// ?sent=1 and no cookie.
//
// It does not hide what classification and the per-address rate limit
// reveal; docs/site/magic-links.md says what does.
func (a *Auth) AnswerAsSent(w http.ResponseWriter, r *http.Request) {
	if !a.sameOrigin(r) {
		http.Error(w, "cross-origin form submission refused", http.StatusForbidden)
		return
	}
	a.answerSent(w, r, r.FormValue("address"))
}
```

`admit` — first line, and the `Remember` call after `SubjectFor`:

```go
func (a *Auth) admit(w http.ResponseWriter, r *http.Request, id Identity) {
	// A first factor has verified, so this browser's attempt is over
	// whether or not the address is admitted. Left behind, it would keep
	// prefilling the form with an address already proved (round 3,
	// finding 25).
	a.jar.EndAttempt(w)
	if a.cfg.Authorize != nil && !a.cfg.Authorize(id.Address) {
```

and, immediately after `subject = s` / the SubjectFor block and before `sess := sessions.Session{…}`:

```go
	// The address, never the subject: SubjectFor may make subjects
	// opaque, and the screen shows this address back. Before the
	// SecondFactor hook because nothing after it knows the address —
	// Gate.Complete holds only the subject — so a sign-in held for a
	// second factor is remembered too. The record says which door this
	// browser used, never that the person got in.
	a.jar.Remember(w, lastsignin.Record{Method: string(id.Method), Address: id.Address})
```

Add `"amadan.net/rastrillo/rastrillo/lastsignin"` to `handlers.go`'s imports.

- [ ] **Step 7: Run the tests to verify they pass**

Run: `GOFLAGS=-mod=mod go test ./auth/ -count=1 -run 'TestScreenOffIsToday|TestBeginWritesTheAttempt|TestAnAttemptOpensOnlyWhereItWasSealed|TestAnswerAsSentAnswersLikeASentLink|TestAdmitRemembersTheWayInAndEndsTheAttempt' -v`
Expected: PASS. Then `GOFLAGS=-mod=mod go test ./auth/ -count=1` — the pre-existing tests still PASS.

- [ ] **Step 8: Run the gate and commit**

Run: `GOFLAGS=-mod=mod go vet ./... && gofmt -l . && GOFLAGS=-mod=mod go test ./... -count=1`

```bash
git add auth
git commit -F - <<'EOF'
auth: SigninScreen, the attempt cookie, AnswerAsSent, and admit's seam

The screen needs to prefill an address after a problem and to name,
on the Sent page, the address this browser's link went to — without
the query ever supplying it. A sealed attempt cookie carries that, and
only with SigninScreen on: off, not one new cookie is written, which
TestScreenOffIsToday holds on every path including the held one.

AnswerAsSent exists because an admission wrapper answering a refusal
with a bare ?sent=1 would, once Begin writes an attempt cookie, differ
from a sent link on the first try. Both now go through one answerSent.

admit ends the attempt as soon as a first factor verifies, and
remembers the address (never the subject) before the second-factor
hook, which is the last point that knows it.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
```

---
### Task 4: `auth` — `KeymailServers`, the guarded transport, and a fake keymail

**Files:**
- Create: `auth/keymail.go`, `auth/keymailfake_test.go`, `auth/keymail_test.go`
- Modify: `auth/auth.go` (`Config.KeymailServers`, `Auth` fields, the flow's classifier and `Keymail` func in `New`)

**Interfaces:**
- Consumes: `signin.Classifier{HTTP, LookupTXT}` (K/classify.go:33-49), `signin.Keymail{Base, Origin, RedirectPath, HTTP}` (K/keymail.go:10-20).
- Produces:
  - `auth.Config.KeymailServers []string`.
  - Unexported: `const callbackPath = "/auth/callback"`; `const classifyTimeout = 5*time.Second`, `exchangeTimeout = 15*time.Second`; `type hostGuard struct{ allow map[string]bool; base http.RoundTripper }` implementing `http.RoundTripper`; `func keymailServers([]string) (map[string]bool, error)`; `Auth` fields `servers map[string]bool` (nil = any server), `guard *hostGuard` (nil without a list), `exchangeHTTP *http.Client` (nil without a list — today's default client).
  - Test helpers (package `auth`): `type keymailFake` with fields `servers`, `claimed`, `delegate`, `address`, `redirect`, `seen`; `newKeymailFake()`, `kayFake()`, `(*keymailFake) RoundTrip`, `lookupTXT`, `hits(host, path string) int`; `wireKeymail(a *Auth, f *keymailFake)`; `stateOf(t, rawURL) string`.

- [ ] **Step 1: Write the fake keymail server**

`auth/keymailfake_test.go`:

```go
package auth

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// keymailFake is a keymail federation server in-process: an
// http.RoundTripper answering the endpoints auth's two clients call —
// the well-known document and the federation lookup for
// classification, the token endpoint for the exchange — for the hosts
// it is told are servers, and 404 for any other host. It records every
// request that reaches it, which is how the allowlist tests prove a
// host was never contacted: a request the guard refused never arrives.
// Nothing here opens a socket or resolves a name.
type keymailFake struct {
	mu       sync.Mutex
	servers  map[string]bool   // hosts that answer as keymail servers
	claimed  map[string]bool   // lowercased addresses whose lookup is a 200
	delegate map[string]string // domain -> the host its _keymail TXT names
	address  string            // what the token endpoint says was verified
	redirect map[string]string // path -> Location: a server bouncing the request
	// status is the redirect's code: 302 by default; 307 and 308 keep
	// the method and body, so a bounced token exchange would carry the
	// code and verifier to wherever Location points.
	status int
	seen   []string // "host path" of every request that arrived
}

func newKeymailFake() *keymailFake {
	return &keymailFake{
		servers: map[string]bool{}, claimed: map[string]bool{},
		delegate: map[string]string{}, redirect: map[string]string{},
	}
}

// kayFake is the common fixture: kay@example.org delegates to
// keymail.test, which answers for her and verifies her.
func kayFake() *keymailFake {
	f := newKeymailFake()
	f.servers["keymail.test"] = true
	f.delegate["example.org"] = "keymail.test"
	f.claimed["kay@example.org"] = true
	f.address = "kay@example.org"
	return f
}

func (f *keymailFake) RoundTrip(r *http.Request) (*http.Response, error) {
	f.mu.Lock()
	f.seen = append(f.seen, r.URL.Host+" "+r.URL.Path)
	server, to := f.servers[r.URL.Host], f.redirect[r.URL.Path]
	claimed, address, status := f.claimed[r.URL.Query().Get("addr")], f.address, f.status
	f.mu.Unlock()
	if r.Body != nil {
		r.Body.Close()
	}
	rec := httptest.NewRecorder()
	switch {
	case !server:
		rec.WriteHeader(http.StatusNotFound)
	case to != "":
		if status == 0 {
			status = http.StatusFound
		}
		rec.Header().Set("Location", to)
		rec.WriteHeader(status)
	case r.URL.Path == "/.well-known/keymail":
		rec.WriteString(`{"version":"1"}`)
	case r.URL.Path == "/api/federation/lookup" && claimed:
		rec.WriteString(`{}`)
	case r.URL.Path == "/api/oauth/token":
		fmt.Fprintf(rec, `{"access_token":"t","token_type":"bearer","address":%q}`, address)
	default:
		rec.WriteHeader(http.StatusNotFound)
	}
	res := rec.Result()
	res.Request = r
	return res, nil
}

func (f *keymailFake) lookupTXT(_ context.Context, name string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if host, ok := f.delegate[strings.TrimPrefix(name, "_keymail.")]; ok {
		return []string{"v=1 host=" + host}, nil
	}
	return nil, &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
}

// hits counts the requests that reached host — at path, or anywhere
// when path is "".
func (f *keymailFake) hits(host, path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, s := range f.seen {
		h, p, _ := strings.Cut(s, " ")
		if h == host && (path == "" || p == path) {
			n++
		}
	}
	return n
}

// wireKeymail points a's keymail clients at f. Without KeymailServers
// the fake replaces the default clients outright; with them it sits
// UNDER the guard, which is the arrangement the allowlist tests need.
func wireKeymail(a *Auth, f *keymailFake) {
	a.flow.Classifier.LookupTXT = f.lookupTXT
	if a.guard != nil {
		a.guard.base = f
		return
	}
	a.flow.Classifier.HTTP = &http.Client{Transport: f}
	a.exchangeHTTP = &http.Client{Transport: f}
}

// stateOf is the state parameter of an authorize URL.
func stateOf(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parsing %q: %v", raw, err)
	}
	return u.Query().Get("state")
}
```

- [ ] **Step 2: Write the failing tests**

`auth/keymail_test.go`:

```go
package auth

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func listed(servers ...string) func(*Config) {
	return func(c *Config) { c.KeymailServers = servers }
}

// beginKeymail posts kay's address and returns the answer, unforced so
// it is classified.
func beginKeymail(a *Auth, b *browser, address string) *http.Response {
	return b.do(a.Begin, http.MethodPost, "/signin", url.Values{"address": {address}}).Result()
}

func TestKeymailServersRefusesWhatIsNotAHost(t *testing.T) {
	for _, bad := range []string{
		"https://keymail.dev", "keymail.dev/", "keymail.dev/x", "", "key mail.dev", "user@keymail.dev",
		"keymail.test:banana", "keymail.test:99999", "keymail.test:0", "keymail.test:", "[::1", "keymail\x00.test", "keymail.test?x",
	} {
		d := newTestAuthDB(t)
		if _, err := New(Config{DB: d, Origin: "http://app.test", InstanceKey: "k", Mailer: &captureMailer{}, KeymailServers: []string{bad}}); err == nil {
			t.Errorf("KeymailServers %q was accepted; it can never match a host, so every keymail user would silently get a link", bad)
		}
	}
}

func TestWithoutKeymailServersBothClientsAreToday(t *testing.T) {
	a, _ := newTestAuth(t, nil)
	if a.guard != nil || a.flow.Classifier.HTTP != nil || a.exchangeHTTP != nil {
		t.Fatal("with no list, auth must leave both keymail clients as the library's defaults")
	}
	if k := a.flow.Keymail("keymail.test"); k.RedirectPath != callbackPath || k.HTTP != nil {
		t.Fatalf("Keymail = %+v; RedirectPath must be callbackPath, set explicitly", k)
	}
}

func TestKeymailServersKeepBothTimeouts(t *testing.T) {
	a, _ := newTestAuth(t, listed("keymail.test"))
	if a.flow.Classifier.HTTP == nil || a.flow.Classifier.HTTP.Timeout != 5*time.Second {
		t.Fatalf("classifier client = %+v, want the library's 5s probe timeout kept", a.flow.Classifier.HTTP)
	}
	if k := a.flow.Keymail("keymail.test"); k.HTTP == nil || k.HTTP.Timeout != 15*time.Second {
		t.Fatalf("exchange client = %+v, want the library's 15s exchange timeout kept", k.HTTP)
	}
}

func TestAnUnlistedServerIsNeverProbed(t *testing.T) {
	a, m := newTestAuth(t, listed("keymail.test"))
	f := newKeymailFake()
	f.servers["rogue.test"] = true
	f.delegate["example.net"] = "rogue.test"
	f.claimed["ron@example.net"] = true
	wireKeymail(a, f)
	res := beginKeymail(a, newBrowser(), "ron@example.net")
	if loc := res.Header.Get("Location"); loc != "/signin?sent=1" {
		t.Fatalf("an unlisted server's address → %q, want a magic link", loc)
	}
	if n := f.hits("rogue.test", ""); n != 0 {
		t.Fatalf("rogue.test received %d requests; an unlisted server must never be contacted", n)
	}
	if m.to != "ron@example.net" {
		t.Fatalf("the link went to %q", m.to)
	}
}

func TestAListedServerGetsTheCeremony(t *testing.T) {
	a, _ := newTestAuth(t, listed("KEYMAIL.test"))
	f := kayFake()
	wireKeymail(a, f)
	res := beginKeymail(a, newBrowser(), "kay@example.org")
	if loc := res.Header.Get("Location"); !strings.HasPrefix(loc, "https://keymail.test/oauth/authorize?") {
		t.Fatalf("a listed server's address → %q, want the authorize URL (the list compares case-insensitively)", loc)
	}
}

// Both probes — the well-known document and the federation lookup —
// and every redirect code a client follows.
func TestAProbeCannotLeaveTheListByRedirect(t *testing.T) {
	for _, path := range []string{"/.well-known/keymail", "/api/federation/lookup"} {
		for _, status := range []int{http.StatusFound, http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
			for name, to := range map[string]string{
				"to an unlisted host": "https://rogue.test" + path,
				"down to plain http":  "http://keymail.test" + path,
			} {
				t.Run(fmt.Sprintf("%s %d %s", path, status, name), func(t *testing.T) {
					a, _ := newTestAuth(t, listed("keymail.test"))
					f := kayFake()
					f.redirect[path], f.status = to, status
					wireKeymail(a, f)
					if loc := beginKeymail(a, newBrowser(), "kay@example.org").Header.Get("Location"); loc != "/signin?sent=1" {
						t.Fatalf("a redirected probe → %q, want the magic link a failed probe means", loc)
					}
					if n := f.hits("rogue.test", ""); n != 0 {
						t.Fatalf("rogue.test received %d requests", n)
					}
					if n := f.hits("keymail.test", path); n != 1 {
						t.Fatalf("keymail.test%s was requested %d times, want 1: the https request, and the refused hop never arriving", path, n)
					}
				})
			}
		}
	}
}

// 307 and 308 are the redirects that re-send a POST with its body: the
// code and the PKCE verifier. Neither an unlisted host nor the listed
// host over plain http may receive them.
func TestAnExchangeCannotLeaveTheListByRedirect(t *testing.T) {
	for _, status := range []int{http.StatusFound, http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		for name, to := range map[string]string{
			"to an unlisted host": "https://rogue.test/api/oauth/token",
			"down to plain http":  "http://keymail.test/api/oauth/token",
		} {
			t.Run(fmt.Sprintf("%d %s", status, name), func(t *testing.T) {
				a, _ := newTestAuth(t, listed("keymail.test"))
				f := kayFake()
				wireKeymail(a, f)
				b := newBrowser()
				state := stateOf(t, beginKeymail(a, b, "kay@example.org").Header.Get("Location"))
				f.redirect["/api/oauth/token"], f.status = to, status
				w := b.do(a.Callback, http.MethodGet, "/auth/callback?code=c&state="+state, nil)
				if loc := w.Header().Get("Location"); loc != "/signin?force=1&err=keymail" {
					t.Fatalf("a redirected exchange → %q, want the escape hatch", loc)
				}
				if n := f.hits("rogue.test", ""); n != 0 {
					t.Fatalf("the code and verifier reached rogue.test (%d requests)", n)
				}
				if n := f.hits("keymail.test", "/api/oauth/token"); n != 1 {
					t.Fatalf("keymail.test's token endpoint saw %d requests, want 1: the refused hop must never be sent", n)
				}
			})
		}
	}
}

func TestAPendingCookieForAServerNoLongerListed(t *testing.T) {
	before, _ := newTestAuth(t, listed("keymail.test"))
	f := kayFake()
	wireKeymail(before, f)
	b := newBrowser()
	state := stateOf(t, beginKeymail(before, b, "kay@example.org").Header.Get("Location"))

	after, _ := newTestAuth(t, listed("other.test"))
	wireKeymail(after, f)
	w := b.do(after.Callback, http.MethodGet, "/auth/callback?code=c&state="+state, nil)
	if loc := w.Header().Get("Location"); loc != "/signin?force=1&err=keymail" {
		t.Fatalf("a pending cookie for a delisted server → %q, want the escape hatch, not a dead end", loc)
	}
	if n := f.hits("keymail.test", "/api/oauth/token"); n != 0 {
		t.Fatalf("the delisted server received %d token exchanges", n)
	}
}
```

Add to `auth/auth_test.go` (used above to build a Config by hand):

```go
// newTestAuthDB is a migrated database for a test that calls New itself.
func newTestAuthDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "auth.db"), nil)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	if _, err := migrate.Apply(context.Background(), d, migrate.Merge(sessions.Schema, Schema, secondfactor.Schema)); err != nil {
		t.Fatalf("migrate.Apply: %v", err)
	}
	return d.Writer()
}
```

(add `"database/sql"` to `auth_test.go`'s imports.)

- [ ] **Step 3: Run the tests to verify they fail**

Run: `GOFLAGS=-mod=mod go test ./auth/ -count=1 -run 'Keymail|Listed|Unlisted|Probe|Exchange|Pending'`
Expected: FAIL to compile — `unknown field KeymailServers`, `a.guard undefined`, `a.exchangeHTTP undefined`, `undefined: callbackPath`.

- [ ] **Step 4: Write the guard**

`auth/keymail.go`:

```go
package auth

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// callbackPath is where keymail sends the browser back. auth sets it on
// every signin.Keymail explicitly rather than inheriting the library's
// default, because the authorize-URL predicate checks redirect_uri
// against this same constant: one value built, the same value checked,
// so the two cannot drift apart on a library upgrade.
const callbackPath = "/auth/callback"

// The library's own client timeouts (K/classify.go:68,
// K/keymail.go:105-108), kept when auth replaces the clients to guard
// them: a guard that dropped them would let a slow keymail server hold
// a sign-in request open indefinitely.
const (
	classifyTimeout = 5 * time.Second
	exchangeTimeout = 15 * time.Second
)

// keymailServers parses Config.KeymailServers into a lowercased set; nil
// means any delegated server. An entry that could never equal a URL's
// host — a scheme, a path, userinfo, a port that is not a port, an
// unclosed IPv6 bracket, a control character — is refused here, because
// accepted it would match nothing and every keymail user would quietly
// be sent a link instead.
func keymailServers(list []string) (map[string]bool, error) {
	if len(list) == 0 {
		return nil, nil
	}
	set := make(map[string]bool, len(list))
	for _, s := range list {
		h := strings.ToLower(strings.TrimSpace(s))
		if !validAuthority(h) {
			return nil, fmt.Errorf("rastrillo/auth: KeymailServers entry %q is not a host or host:port", s)
		}
		set[h] = true
	}
	return set, nil
}

// validAuthority is a host with an optional port, exactly as a URL's
// Host carries it: parsing it as one and demanding it come back
// unchanged is what makes "a string that can equal some URL's host"
// the rule, rather than a list of characters someone thought of.
func validAuthority(h string) bool {
	if h == "" || strings.ContainsFunc(h, func(r rune) bool { return r <= ' ' || r == 0x7f }) {
		return false
	}
	u, err := url.Parse("https://" + h)
	if err != nil || u.Host != h || u.User != nil || u.Path != "" || u.RawQuery != "" ||
		u.Fragment != "" || u.Hostname() == "" || strings.HasSuffix(h, ":") {
		return false
	}
	if p := u.Port(); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			return false
		}
	}
	return true
}

// hostGuard is the transport under both keymail clients when
// KeymailServers is set. It refuses any request that is not https to a
// listed host — on every hop, because http.Client calls its transport
// again for each redirect it follows, so a listed server cannot bounce
// a probe, or a token exchange carrying the code and verifier, to a
// host nobody listed or down to plain http.
type hostGuard struct {
	allow map[string]bool
	// base carries what the guard lets through; http.DefaultTransport
	// when nil. A field so tests can put an in-process keymail server
	// under the guard.
	base http.RoundTripper
}

func (g *hostGuard) RoundTrip(r *http.Request) (*http.Response, error) {
	host := strings.ToLower(r.URL.Host)
	if r.URL.Scheme != "https" || !g.allow[host] {
		// A RoundTripper owns the body even when it refuses.
		if r.Body != nil {
			r.Body.Close()
		}
		// Host only: the probe's query carries the address being signed in.
		return nil, fmt.Errorf("rastrillo/auth: %s://%s is not a listed keymail server", r.URL.Scheme, host)
	}
	base := g.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(r)
}
```

`auth/auth.go` — in `Config`, after `Remember`:

```go
	// KeymailServers, when set, is the closed set of keymail servers
	// (host or host:port, compared case-insensitively) this app will
	// classify against or exchange a code with. Empty means any server
	// an address's own _keymail delegation names — which is keymail's
	// protocol: the domain's owner chooses its server, the same party
	// that controls its MX and could receive a magic link anyway, and a
	// server cannot vouch for anyone else's address because Callback
	// compares the address it returns with the one the flow started for.
	// An address whose server is not listed gets a magic link and its
	// server is never contacted. Copied at New; changing it means a
	// restart, which also empties the classifier's caches. It applies
	// whether or not SigninScreen is on.
	KeymailServers []string
```

In `Auth`, after `now`:

```go
	// servers is KeymailServers as a set, nil for "any server"; guard
	// enforces it on both clients, and the authorize-URL predicate
	// checks it again.
	servers map[string]bool
	guard   *hostGuard
	// exchangeHTTP is the token-exchange client: nil — the library's own
	// default — unless KeymailServers asked for a guard.
	exchangeHTTP *http.Client
```

In `New`, before `a.flow = …`:

```go
	servers, err := keymailServers(cfg.KeymailServers)
	if err != nil {
		return nil, err
	}
	a.servers = servers
	classifier := &signin.Classifier{}
	if servers != nil {
		a.guard = &hostGuard{allow: servers}
		classifier.HTTP = &http.Client{Transport: a.guard, Timeout: classifyTimeout}
		a.exchangeHTTP = &http.Client{Transport: a.guard, Timeout: exchangeTimeout}
	}
```

and in the flow literal:

```go
		Classifier: classifier,
		Keymail: func(server string) *signin.Keymail {
			return &signin.Keymail{
				Base: "https://" + server, Origin: cfg.Origin,
				RedirectPath: callbackPath, HTTP: a.exchangeHTTP,
			}
		},
```

(`net/http` is already imported by `auth.go`.)

- [ ] **Step 5: Run the tests to verify they pass**

Run: `GOFLAGS=-mod=mod go test ./auth/ -count=1 -v -run 'Keymail|Listed|Unlisted|Probe|Exchange|Pending'`
Expected: PASS. Then `GOFLAGS=-mod=mod go test ./auth/ -count=1` — all PASS.

- [ ] **Step 6: Run the gate and commit**

Run: `GOFLAGS=-mod=mod go vet ./... && gofmt -l . && GOFLAGS=-mod=mod go test ./... -count=1`

```bash
git add auth
git commit -F - <<'EOF'
auth: KeymailServers, enforced on both keymail clients at every hop

The library talks to a keymail server twice — to classify an address
and to exchange the code — and offers no hook between classifying and
acting on the answer. The one seam before the result exists is the
HTTP client, so an allowlist has to be a transport, and it has to sit
under the exchange client too or a pending cookie from before the list
tightened still reaches a delisted server with the code and verifier.
http.Client calls the transport per redirect hop, so a listed server
cannot bounce either request elsewhere or down to http.

A malformed entry is refused at New: accepted, it would match nothing
and send every keymail user a link with no error anywhere.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
```

---
### Task 5: `auth` — the continuation, the authorize-URL predicate, and `Callback`'s guard

**Files:**
- Create: `auth/continuation.go`, `auth/continuation_test.go`
- Modify: `auth/auth.go` (`continueKey` field, derived in `New`), `auth/handlers.go` (`Begin`'s keymail branch → `continueKeymail`; `Callback`)

**Interfaces:**
- Consumes: `newID`, `noteAttempt`, `attemptKeymail`, `cookieState` (Task 3); `callbackPath`, `a.servers`, `kayFake`, `wireKeymail`, `stateOf` (Task 4); `sealedcookie` (Task 1).
- Produces (unexported, used by Task 6):
  - `type continuation struct{ O, ID string; IAT, EXP int64; U, ST, PH string }` (json `o,id,iat,exp,u,st,ph`); `const continuationTTL = pendingTTL`.
  - `(a *Auth) continueCookie() string`; `(a *Auth) sealContinuation(authorizeURL, pending string) (id, value string, err error)`; `(a *Auth) openContinuation(*http.Request) (continuation, cookieState)`; `(a *Auth) continuationFor(r *http.Request, id string) (string, bool)`; `(a *Auth) validAuthorizeURL(raw string) bool`; `digest(s string) string` (base64url SHA-256); field `a.continueKey []byte`.

- [ ] **Step 1: Write the failing tests**

`auth/continuation_test.go`:

```go
package auth

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/keymaildev/signin"

	"amadan.net/rastrillo/rastrillo/internal/sealedcookie"
	"amadan.net/rastrillo/rastrillo/lastsignin"
)

// flipByte tampers with a sealed value the only reliable way: decode it,
// flip one bit of the ciphertext, re-encode. Editing the base64 text can
// leave the decoded bytes unchanged.
func flipByte(t *testing.T, v string) string {
	t.Helper()
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(v, "v1."))
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)/2] ^= 0x01
	return "v1." + base64.RawURLEncoding.EncodeToString(raw)
}

// newKeymailScreen is the screen with kay's keymail server wired in.
func newKeymailScreen(t *testing.T, mut func(*Config)) (*Auth, *keymailFake) {
	t.Helper()
	a, _ := newScreenAuth(t, mut)
	f := kayFake()
	wireKeymail(a, f)
	return a, f
}

// startKeymail begins kay's sign-in in b and returns the continuation
// id and the authorize URL's state.
func startKeymail(t *testing.T, a *Auth, b *browser) (id, state string) {
	t.Helper()
	w := b.do(a.Begin, http.MethodPost, "/signin", url.Values{"address": {"kay@example.org"}})
	id, ok := strings.CutPrefix(w.Header().Get("Location"), "/signin?continue=")
	if !ok || id == "" {
		t.Fatalf("keymail Begin → %q, want /signin?continue=<id>", w.Header().Get("Location"))
	}
	u, ok := a.continuationFor(b.request(http.MethodGet, "/signin?continue="+id, nil), id)
	if !ok {
		t.Fatal("the continuation Begin just wrote does not resolve")
	}
	return id, stateOf(t, u)
}

func callback(a *Auth, b *browser, state string) *httptest.ResponseRecorder {
	return b.do(a.Callback, http.MethodGet, "/auth/callback?code=c&state="+url.QueryEscape(state), nil)
}

// install copies the named cookies from one browser into another: how a
// test stages "the pair this browser installed last".
func install(to, from *browser, names ...string) {
	for _, n := range names {
		to.jar[n] = from.jar[n]
	}
}

var pair = []string{"rastrillo_pending", "rastrillo_continue"}

func TestAuthorizeURLPredicate(t *testing.T) {
	a, _ := newTestAuth(t, nil)
	build := func(origin, redirectPath string, stepUp bool) string {
		k := &signin.Keymail{Base: "https://keymail.test", Origin: origin, RedirectPath: redirectPath}
		return k.AuthorizeURL(strings.Repeat("s", 43), strings.Repeat("c", 43), stepUp)
	}
	good := build(a.cfg.Origin, callbackPath, false)
	for _, c := range []struct {
		name string
		url  string
		want bool
	}{
		{"what the library builds", good, true},
		{"with prompt=login", build(a.cfg.Origin, callbackPath, true), true},
		{"prompt=none", good + "&prompt=none", false},
		{"a duplicated state", good + "&state=" + strings.Repeat("s", 43), false},
		{"an extra parameter", good + "&next=%2F", false},
		{"userinfo", strings.Replace(good, "https://", "https://u@", 1), false},
		{"a fragment", good + "#x", false},
		{"an encoded path", strings.Replace(good, "/oauth/authorize", "/oauth%2Fauthorize", 1), false},
		{"another path", strings.Replace(good, "/oauth/authorize", "/oauth/authorise", 1), false},
		{"plain http", strings.Replace(good, "https://", "http://", 1), false},
		{"no host", strings.Replace(good, "keymail.test", "", 1), false},
		{"another redirect_uri", build(a.cfg.Origin, "/elsewhere", false), false},
		{"another client_id", build("http://evil.test", callbackPath, false), false},
		{"another scope", strings.Replace(good, "scope=identify", "scope=email", 1), false},
		{"plain PKCE", strings.Replace(good, "code_challenge_method=S256", "code_challenge_method=plain", 1), false},
		{"a short state", strings.Replace(good, "state="+strings.Repeat("s", 43), "state=sss", 1), false},
		{"a state that is not base64url", strings.Replace(good, "state="+strings.Repeat("s", 43), "state="+strings.Repeat("s", 42)+"%2B", 1), false},
		{"not a URL", "https://%zz", false},
		{"a query that does not decode", good + "&x=%zz", false},
		{"a query with a semicolon", good + ";x=1", false},
		{"no state", strings.Replace(good, "state="+strings.Repeat("s", 43), "", 1), false},
		{"an empty state", strings.Replace(good, "state="+strings.Repeat("s", 43), "state=", 1), false},
		{"no client_id", regexp.MustCompile(`client_id=[^&]*&?`).ReplaceAllString(good, ""), false},
		{"an empty code_challenge", strings.Replace(good, "code_challenge="+strings.Repeat("c", 43), "code_challenge=", 1), false},
		{"a short code_challenge", strings.Replace(good, "code_challenge="+strings.Repeat("c", 43), "code_challenge="+strings.Repeat("c", 42), 1), false},
		{"a code_challenge that is not base64url", strings.Replace(good, "code_challenge="+strings.Repeat("c", 43), "code_challenge="+strings.Repeat("c", 42)+"%2F", 1), false},
		{"a duplicated prompt", build(a.cfg.Origin, callbackPath, true) + "&prompt=login", false},
		{"an empty prompt", good + "&prompt=", false},
	} {
		if got := a.validAuthorizeURL(c.url); got != c.want {
			t.Errorf("%s: %v, want %v (%s)", c.name, got, c.want, c.url)
		}
	}

	l, _ := newTestAuth(t, listed("keymail.test"))
	if !l.validAuthorizeURL(good) {
		t.Error("the list refused a listed host")
	}
	if l.validAuthorizeURL(strings.Replace(good, "keymail.test", "rogue.test", 1)) {
		t.Error("the list admitted an unlisted host")
	}
}

func TestAContinuationOpensOnlyWhereItWasSealed(t *testing.T) {
	a, _ := newScreenAuth(t, nil)
	good := (&signin.Keymail{Base: "https://keymail.test", Origin: a.cfg.Origin, RedirectPath: callbackPath}).
		AuthorizeURL(strings.Repeat("s", 43), strings.Repeat("c", 43), false)
	id, value, err := a.sealContinuation(good, "pending-value")
	if err != nil {
		t.Fatal(err)
	}
	open := func(v string) cookieState {
		r := httptest.NewRequest(http.MethodGet, "http://app.test/signin", nil)
		r.AddCookie(&http.Cookie{Name: a.continueCookie(), Value: v})
		_, st := a.openContinuation(r)
		return st
	}
	if st := open(value); st != cookieValid {
		t.Fatalf("a fresh continuation is %v", st)
	}
	now := a.now().Unix()
	p := continuation{O: a.cfg.Origin, ID: id, IAT: now, EXP: now + 600, U: good, ST: digest("s"), PH: digest("p")}
	seal := func(key []byte, v any) string {
		s, err := sealedcookie.Seal(key, v)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	other, expired, future, long := p, p, p, p
	other.O = "http://other.test"
	expired.IAT, expired.EXP = now-601, now-1
	future.IAT, future.EXP = now+120, now+600
	long.EXP = now + 601
	for name, v := range map[string]string{
		"another origin":           seal(a.continueKey, other),
		"expired":                  seal(a.continueKey, expired),
		"issued in the future":     seal(a.continueKey, future),
		"longer than ten minutes":  seal(a.continueKey, long),
		"under the attempt's key":  seal(a.attemptKey, p),
		"unknown version":          "v2." + strings.TrimPrefix(value, "v1."),
		"an attempt, not this":     seal(a.continueKey, attempt{O: a.cfg.Origin, ID: "x", IAT: now, EXP: now + 60, K: attemptLink}),
		"tampered":                 flipByte(t, value),
	} {
		if st := open(v); st != cookieInvalid {
			t.Errorf("%s: %v, want invalid", name, st)
		}
	}
}

func TestBeginContinuesOnTheSigninPage(t *testing.T) {
	a, _ := newKeymailScreen(t, nil)
	b := newBrowser()
	w := b.do(a.Begin, http.MethodPost, "/signin", url.Values{"address": {"kay@example.org"}})
	if w.Code != http.StatusSeeOther || !strings.HasPrefix(w.Header().Get("Location"), "/signin?continue=") {
		t.Fatalf("keymail Begin with the screen on → %d %q; the form must stay on this origin so form-action 'self' allows it",
			w.Code, w.Header().Get("Location"))
	}
	if got, want := setCookies(w), []string{"rastrillo_attempt", "rastrillo_continue", "rastrillo_pending"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Set-Cookie %v, want %v", got, want)
	}
	if at, _ := a.openAttempt(b.request(http.MethodGet, "/signin", nil)); at.K != attemptKeymail || at.A != "kay@example.org" {
		t.Fatalf("attempt = %+v, want kind keymail with the address", at)
	}
}

func TestBeginRefusesAnAuthorizeURLThatFailsThePredicate(t *testing.T) {
	a, _ := newKeymailScreen(t, nil)
	a.flow.Keymail = func(server string) *signin.Keymail {
		return &signin.Keymail{Base: "https://" + server + "/evil", Origin: a.cfg.Origin, RedirectPath: callbackPath}
	}
	w := newBrowser().do(a.Begin, http.MethodPost, "/signin", url.Values{"address": {"kay@example.org"}})
	if w.Header().Get("Location") != "/signin?err=1" || len(setCookies(w)) != 0 {
		t.Fatalf("a bad authorize URL → %q with %v; want ?err=1 and no cookie at all", w.Header().Get("Location"), setCookies(w))
	}
}

func TestAContinuationResolvesOnlyForThisBrowser(t *testing.T) {
	a, _ := newKeymailScreen(t, nil)
	b := newBrowser()
	id, _ := startKeymail(t, a, b)
	resolves := func(b *browser, id string) bool {
		_, ok := a.continuationFor(b.request(http.MethodGet, "/signin?continue="+url.QueryEscape(id), nil), id)
		return ok
	}
	if resolves(b, "https://evil.test/") {
		t.Error("a URL in ?continue= resolved; the URL must only ever come from the cookie")
	}
	if resolves(b, "another-id") {
		t.Error("a wrong id resolved")
	}
	noPending := b.clone()
	delete(noPending.jar, "rastrillo_pending")
	if resolves(noPending, id) {
		t.Error("a continuation resolved with no pending cookie")
	}
	otherPending := b.clone()
	otherPending.jar["rastrillo_pending"] = &http.Cookie{Name: "rastrillo_pending", Value: "someone-else"}
	if resolves(otherPending, id) {
		t.Error("a continuation resolved against a different pending cookie")
	}
	a.now = func() time.Time { return time.Now().Add(11 * time.Minute) }
	if resolves(b, id) {
		t.Error("a continuation resolved after its ten minutes")
	}
	a.now = time.Now

	bad, value, err := a.sealContinuation("https://keymail.test/evil/oauth/authorize?x=1", b.cookie("rastrillo_pending").Value)
	if err != nil {
		t.Fatal(err)
	}
	b.jar["rastrillo_continue"] = &http.Cookie{Name: "rastrillo_continue", Value: value}
	if resolves(b, bad) {
		t.Error("a sealed URL failing the predicate resolved; it is checked again on the way out")
	}
}

// TestTwoTabsTheInstalledPairWins is §1.3's guarantee: whichever pair
// of cookies the browser installed last is the only attempt that can
// continue or complete, and a callback for any other attempt ends at
// Expired without touching that pair.
func TestTwoTabsTheInstalledPairWins(t *testing.T) {
	t.Run("A then B: A is stale", func(t *testing.T) {
		a, _ := newKeymailScreen(t, nil)
		b := newBrowser()
		idA, stateA := startKeymail(t, a, b)
		_, stateB := startKeymail(t, a, b)
		if _, ok := a.continuationFor(b.request(http.MethodGet, "/signin?continue="+idA, nil), idA); ok {
			t.Fatal("tab A's continuation still resolves after B's pair was installed")
		}
		w := callback(a, b, stateA)
		if w.Header().Get("Location") != "/signin?err=expired" || len(setCookies(w)) != 0 {
			t.Fatalf("A's late callback → %q with %v; want Expired, B's pair untouched", w.Header().Get("Location"), setCookies(w))
		}
		if w := callback(a, b, stateB); w.Header().Get("Location") != "/" {
			t.Fatalf("B's callback → %q, want signed in", w.Header().Get("Location"))
		}
	})
	t.Run("B's pair then A's, out of order: B is stale", func(t *testing.T) {
		a, _ := newKeymailScreen(t, nil)
		b := newBrowser()
		_, stateA := startKeymail(t, a, b)
		pairA := b.clone()
		_, stateB := startKeymail(t, a, b)
		install(b, pairA, pair...)
		if w := callback(a, b, stateB); w.Header().Get("Location") != "/signin?err=expired" || len(setCookies(w)) != 0 {
			t.Fatalf("B's callback against A's pair → %q with %v", w.Header().Get("Location"), setCookies(w))
		}
		if w := callback(a, b, stateA); w.Header().Get("Location") != "/" {
			t.Fatalf("A's callback → %q, want signed in", w.Header().Get("Location"))
		}
	})
	t.Run("keymail A, keymail B, then a magic link: A still cannot spoil B", func(t *testing.T) {
		a, _ := newKeymailScreen(t, nil)
		b := newBrowser()
		_, stateA := startKeymail(t, a, b)
		_, stateB := startKeymail(t, a, b)
		b.do(a.Begin, http.MethodPost, "/signin", url.Values{"address": {"ada@example.com"}, "force": {"1"}})
		if w := callback(a, b, stateA); w.Header().Get("Location") != "/signin?err=expired" || len(setCookies(w)) != 0 {
			t.Fatalf("A's callback after a magic link → %q with %v", w.Header().Get("Location"), setCookies(w))
		}
		if w := callback(a, b, stateB); w.Header().Get("Location") != "/" {
			t.Fatalf("B's callback → %q", w.Header().Get("Location"))
		}
	})
	t.Run("a continuation that describes another pending cookie does not block", func(t *testing.T) {
		a, _ := newKeymailScreen(t, nil)
		b := newBrowser()
		startKeymail(t, a, b)
		pairA := b.clone()
		_, stateB := startKeymail(t, a, b)
		install(b, pairA, "rastrillo_continue")
		w := callback(a, b, stateB)
		if w.Header().Get("Location") != "/" {
			t.Fatalf("B's callback beside A's leftover continuation → %q, want today's path", w.Header().Get("Location"))
		}
		got := strings.Join(setCookies(w), " ")
		if !strings.Contains(got, "rastrillo_pending:clear") || !strings.Contains(got, "rastrillo_continue:clear") {
			t.Fatalf("today's path clears pending and continuation; got %s", got)
		}
	})
	t.Run("a third party's callback URL leaves the attempt alone", func(t *testing.T) {
		a, _ := newKeymailScreen(t, nil)
		b := newBrowser()
		startKeymail(t, a, b)
		if w := callback(a, b, "forged"); w.Header().Get("Location") != "/signin?err=expired" || len(setCookies(w)) != 0 {
			t.Fatalf("a forged callback → %q with %v; today it throws the visitor's attempt away", w.Header().Get("Location"), setCookies(w))
		}
	})
}

// A keymail sign-in with the screen on ends like a magic link does:
// the attempt is over, the way in is remembered with the address, and
// the continuation goes with its pending cookie — on the held path too.
func TestAKeymailSignInIsRememberedAndEndsTheAttempt(t *testing.T) {
	for _, held := range []bool{false, true} {
		a, _ := newKeymailScreen(t, nil)
		want, dest := []string{"rastrillo_attempt:clear", "rastrillo_continue:clear", "rastrillo_last_signin", "rastrillo_pending:clear", "rastrillo_session"}, "/"
		if held {
			a.cfg.SecondFactor = holdingGate(t, a).Hold
			want, dest = []string{"rastrillo_attempt:clear", "rastrillo_continue:clear", "rastrillo_last_signin", "rastrillo_pending:clear", "rastrillo_secondfactor"}, "/signin/confirm"
		}
		b := newBrowser()
		_, state := startKeymail(t, a, b)
		w := callback(a, b, state)
		if w.Header().Get("Location") != dest || !reflect.DeepEqual(setCookies(w), want) {
			t.Fatalf("held=%v: → %q with %v, want %q with %v", held, w.Header().Get("Location"), setCookies(w), dest, want)
		}
		if rec, res := a.jar.Read(b.request(http.MethodGet, "/signin", nil)); res != lastsignin.Valid || rec != (lastsignin.Record{Method: "keymail", Address: "kay@example.org"}) {
			t.Fatalf("held=%v: remembered %+v, %v", held, rec, res)
		}
	}
}

// TestScreenOffKeymailIsToday is TestScreenOffIsToday's keymail half.
func TestScreenOffKeymailIsToday(t *testing.T) {
	for _, remember := range []*bool{nil, ptr(true), ptr(false)} {
		name := "nil"
		if remember != nil {
			name = strconv.FormatBool(*remember)
		}
		t.Run("Remember="+name, func(t *testing.T) {
			a, _ := newTestAuth(t, func(c *Config) { c.Remember = remember })
			wireKeymail(a, kayFake())
			b := newBrowser()
			begin := func() string {
				w := b.do(a.Begin, http.MethodPost, "/signin", url.Values{"address": {"kay@example.org"}})
				loc := w.Header().Get("Location")
				if !strings.HasPrefix(loc, "https://keymail.test/oauth/authorize?") || !reflect.DeepEqual(setCookies(w), []string{"rastrillo_pending"}) {
					t.Fatalf("keymail Begin, screen off → %q with %v; want today's 303 and pending cookie", loc, setCookies(w))
				}
				return stateOf(t, loc)
			}
			check := func(what string, w *httptest.ResponseRecorder, loc string, cookies []string) {
				t.Helper()
				if w.Header().Get("Location") != loc || !reflect.DeepEqual(setCookies(w), cookies) {
					t.Errorf("%s → %q with %v, want %q with %v", what, w.Header().Get("Location"), setCookies(w), loc, cookies)
				}
			}
			begin()
			check("a callback for no attempt", callback(a, b, "forged"), "/signin?err=expired", []string{"rastrillo_pending:clear"})
			check("the callback", callback(a, b, begin()), "/", []string{"rastrillo_pending:clear", "rastrillo_session"})

			// A continuation cookie left by a screen-on configuration with
			// the same key — describing this very pending cookie, for some
			// other state — is not read with the screen off: the callback
			// completes as today and the cookie is not touched.
			state := begin()
			on, _ := newScreenAuth(t, nil)
			other := (&signin.Keymail{Base: "https://keymail.test", Origin: a.cfg.Origin, RedirectPath: callbackPath}).
				AuthorizeURL(strings.Repeat("o", 43), strings.Repeat("c", 43), false)
			_, leftover, err := on.sealContinuation(other, b.cookie("rastrillo_pending").Value)
			if err != nil {
				t.Fatal(err)
			}
			b.jar["rastrillo_continue"] = &http.Cookie{Name: "rastrillo_continue", Value: leftover}
			check("the callback beside a leftover continuation", callback(a, b, state), "/", []string{"rastrillo_pending:clear", "rastrillo_session"})
			a.cfg.SecondFactor = holdingGate(t, a).Hold
			check("the callback, held", callback(a, b, begin()), "/signin/confirm", []string{"rastrillo_pending:clear", "rastrillo_secondfactor"})
		})
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `GOFLAGS=-mod=mod go test ./auth/ -count=1 -run 'Authorize|Continuation|Continues|TwoTabs|ScreenOffKeymail'`
Expected: FAIL to compile — `a.continuationFor undefined`, `a.validAuthorizeURL undefined`, `a.sealContinuation undefined`, …

- [ ] **Step 3: Write the continuation and the predicate**

`auth/continuation.go`:

```go
package auth

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"
	"time"

	"amadan.net/rastrillo/rastrillo/internal/sealedcookie"
)

// continuationTTL is the pending cookie's own life: a continuation
// describes one pending round trip, and outliving it would only offer
// an authorize URL for an attempt that is already dead.
const continuationTTL = pendingTTL

// continuation is the pending cookie's sidecar: the authorize URL the
// sign-in page navigates to, and what binds a callback to it. Separate
// from the attempt cookie because the two live differently — a magic
// link submitted after a keymail one replaces the attempt, while the
// keymail round trip is still out and its callback must still be
// recognised (round 2, finding 19).
type continuation struct {
	O   string `json:"o"`
	ID  string `json:"id"`
	IAT int64  `json:"iat"`
	EXP int64  `json:"exp"`
	U   string `json:"u"`
	// ST is the digest of the URL's state, so Callback can tell a
	// callback for this attempt from one for any other.
	ST string `json:"st"`
	// PH is the digest of the pending cookie written beside it, so a
	// continuation dies with its pending cookie and never outlives the
	// attempt it describes.
	PH string `json:"ph"`
}

func (a *Auth) continueCookie() string { return a.cookieName("rastrillo_continue") }

// digest is base64url SHA-256: enough to compare, and nothing a reader
// of the cookie could replay.
func digest(s string) string {
	sum := sha256.Sum256([]byte(s))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func (a *Auth) sealContinuation(authorizeURL, pending string) (id, value string, err error) {
	u, err := url.Parse(authorizeURL)
	if err != nil {
		return "", "", err
	}
	if id, err = newID(); err != nil {
		return "", "", err
	}
	now := a.now().Unix()
	value, err = sealedcookie.Seal(a.continueKey, continuation{
		O: a.cfg.Origin, ID: id, IAT: now, EXP: now + int64(continuationTTL/time.Second),
		U: authorizeURL, ST: digest(u.Query().Get("state")), PH: digest(pending),
	})
	return id, value, err
}

func (a *Auth) openContinuation(r *http.Request) (continuation, cookieState) {
	c, err := r.Cookie(a.continueCookie())
	if err != nil {
		return continuation{}, cookieAbsent
	}
	var p continuation
	if sealedcookie.Open(a.continueKey, c.Value, &p) != nil ||
		!sealedcookie.Fresh(p.O, a.cfg.Origin, p.IAT, p.EXP, a.now(), continuationTTL) {
		return continuation{}, cookieInvalid
	}
	return p, cookieValid
}

// continuationFor is the authorize URL this browser may be sent to for
// ?continue=id. The query carries only an id that means nothing without
// this browser's own cookie, so ?continue=https://evil can never become
// a link. It resolves only while the continuation is in date, names
// this id, belongs to the pending cookie the browser holds now — so it
// dies with that cookie, and going back after the callback gives
// Expired — and its URL still passes the predicate.
func (a *Auth) continuationFor(r *http.Request, id string) (string, bool) {
	p, st := a.openContinuation(r)
	if st != cookieValid || id == "" || p.ID != id {
		return "", false
	}
	pending, err := r.Cookie(a.pendingCookie())
	if err != nil || digest(pending.Value) != p.PH {
		return "", false
	}
	if !a.validAuthorizeURL(p.U) {
		return "", false
	}
	return p.U, true
}

// authorizeParams is every parameter keymail's authorize URL may carry
// (K/keymail.go:44-57). Anything else is not a URL auth built.
var authorizeParams = map[string]bool{
	"client_id": true, "redirect_uri": true, "scope": true, "state": true,
	"code_challenge": true, "code_challenge_method": true, "prompt": true,
}

// validAuthorizeURL is fichas' predicate (F/auth_navigation.go:79-99)
// made stricter, every expected value taken from the configuration that
// built the URL. It runs before the URL is sealed and again before it
// becomes a link, because this is the one place auth turns a URL into
// something the browser navigates to: the one place an open redirect
// could be born.
func (a *Auth) validAuthorizeURL(raw string) bool {
	// Checked on the raw string: url.Parse drops an empty fragment, and
	// any '#' at all is not something the library writes.
	if strings.ContainsRune(raw, '#') {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Opaque != "" ||
		u.Path != "/oauth/authorize" || u.RawPath != "" {
		return false
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return false
	}
	for k, v := range q {
		if !authorizeParams[k] || len(v) != 1 || v[0] == "" {
			return false
		}
	}
	for _, k := range []string{"client_id", "redirect_uri", "scope", "state", "code_challenge", "code_challenge_method"} {
		if _, ok := q[k]; !ok {
			return false
		}
	}
	if p, ok := q["prompt"]; ok && p[0] != "login" {
		return false
	}
	origin := strings.TrimSuffix(a.cfg.Origin, "/")
	if q.Get("client_id") != origin || q.Get("redirect_uri") != origin+callbackPath ||
		q.Get("scope") != "identify" || q.Get("code_challenge_method") != "S256" ||
		!pkceValue(q.Get("state")) || !pkceValue(q.Get("code_challenge")) {
		return false
	}
	return a.servers == nil || a.servers[strings.ToLower(u.Host)]
}

// pkceValue is 43 base64url characters: what signin.NewVerifier and
// ChallengeS256 produce (K/keymail.go:133-145).
func pkceValue(s string) bool {
	if len(s) != 43 {
		return false
	}
	for _, c := range s {
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}
```

`auth/auth.go` — `Auth` gains `continueKey []byte` beside `attemptKey` (comment: "seals the continuation cookie; its own derivation for the same reason as attemptKey"), and `New` sets it in the `&Auth{…}` literal:

```go
		continueKey: crypto.Derive([]byte(cfg.InstanceKey), "rastrillo/auth/continue/v1"),
```

- [ ] **Step 4: Route `Begin` and guard `Callback`**

`auth/handlers.go` — `Begin`'s keymail branch:

```go
	if next.Kind == signin.NextKeymail {
		if a.cfg.SigninScreen {
			a.continueKeymail(w, r, address, next)
			return
		}
		a.setCookie(w, a.pendingCookie(), next.Pending, int(pendingTTL.Seconds()))
		// next.Redirect points at a host the submitted address chose
		// (via its DNS delegation) — untrusted output, sent as a
		// redirect the browser follows, never echoed into a page.
		a.redirect(w, r, next.Redirect)
		return
	}
```

and after `Begin`:

```go
// continueKeymail is the keymail answer with SigninScreen on. A 303
// straight to the provider is refused by the default CSP, whose
// form-action 'self' covers a form's whole redirect chain; so this
// stays on the origin — the pending cookie as today, a continuation
// cookie holding the authorize URL, and a redirect to the sign-in page,
// whose Continue state navigates on by itself.
func (a *Auth) continueKeymail(w http.ResponseWriter, r *http.Request, address string, next signin.Next) {
	if !a.validAuthorizeURL(next.Redirect) {
		// A correct library never builds one. If one appears it is not
		// turned into a link, and nothing is left half-set.
		a.cfg.Logger.Error("rastrillo/auth: the keymail authorize URL failed the predicate; not continuing")
		a.redirect(w, r, a.cfg.SigninPath+"?err=1")
		return
	}
	id, value, err := a.sealContinuation(next.Redirect, next.Pending)
	if err != nil {
		a.cfg.Logger.Error("rastrillo/auth: seal continuation", "err", err)
		a.redirect(w, r, a.cfg.SigninPath+"?err=1")
		return
	}
	a.setCookie(w, a.pendingCookie(), next.Pending, int(pendingTTL.Seconds()))
	a.setCookie(w, a.continueCookie(), value, int(continuationTTL.Seconds()))
	a.noteAttempt(w, attemptKeymail, address, false)
	a.redirect(w, r, a.cfg.SigninPath+"?continue="+id)
}
```

`Callback` — replace the single `a.clearCookie(w, a.pendingCookie())` line with:

```go
	if a.cfg.SigninScreen {
		// Today the pending cookie is cleared the moment it is read and
		// the library then finds a state mismatch — so a late callback
		// from tab A, or a callback URL a third party makes the browser
		// open, throws away tab B's newer attempt. When the continuation
		// describes the pending cookie just read and this callback's
		// state is not that attempt's, the callback belongs to some other
		// attempt: answer Expired and leave the pair alone. The pending
		// blob's own authenticated expiry still bounds it; nothing is
		// refreshed. Any other case — no continuation, or one left from
		// an earlier attempt or a screen-off Begin — is today's path.
		if p, st := a.openContinuation(r); st == cookieValid && p.PH == digest(c.Value) &&
			p.ST != digest(r.URL.Query().Get("state")) {
			a.redirect(w, r, a.cfg.SigninPath+"?err=expired")
			return
		}
		a.clearCookie(w, a.continueCookie())
	}
	a.clearCookie(w, a.pendingCookie())
```

Update `Callback`'s doc comment: append "With SigninScreen on, a callback whose state is not the attempt the browser's continuation describes answers Expired without clearing anything (see the comment inside)."

- [ ] **Step 5: Run the tests to verify they pass**

Run: `GOFLAGS=-mod=mod go test ./auth/ -count=1 -v -run 'Authorize|Continuation|Continues|TwoTabs|ScreenOffKeymail'`
Expected: PASS. Then `GOFLAGS=-mod=mod go test ./auth/ -count=1` — all PASS.

- [ ] **Step 6: Run the gate and commit**

Run: `GOFLAGS=-mod=mod go vet ./... && gofmt -l . && GOFLAGS=-mod=mod go test ./... -count=1`

```bash
git add auth
git commit -F - <<'EOF'
auth: continue keymail on the sign-in page; guard Callback per attempt

The default CSP's form-action 'self' covers a form's whole redirect
chain, so Begin's 303 to a keymail server is refused. With the screen
on, Begin now stays on the origin and the sign-in page navigates on by
itself, which keeps form-action strict instead of widening it (the
SKILL.md advice until now). The URL lives in a sealed continuation
cookie, never the query, and passes the predicate before it is sealed
and again before it becomes a link.

The continuation also binds a callback to its attempt: a late callback
from another tab, or one a third party makes the browser open, now
ends at Expired without throwing away the attempt the browser actually
installed. Screen off, Callback is untouched; TestScreenOffKeymailIsToday
holds that.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
```

---
### Task 6: `auth` — `SigninState`, `PrepareSigninResponse`, `Forget`

**Files:**
- Create: `auth/signin.go`, `auth/signinstate_test.go`
- Modify: `auth/auth.go` (package doc paragraph about the screen)

**Interfaces:**
- Consumes: `openAttempt`, `attempt`, `cookieState` (Task 3); `openContinuation`, `continuationFor`, `continueCookie` (Task 5); `a.jar` (Task 3); test helpers from Tasks 3–5.
- Produces (spec §1.2) — the exact names Tasks 7, 12 and 13 use:

```go
type SigninStep string    // StepAsk "ask", StepReturning "returning", StepSent "sent", StepContinue "continue"
type SigninProblem string // ProblemNone "", ProblemRate "rate", ProblemAddress "address", ProblemExpired "expired",
                          // ProblemKeymail "keymail", ProblemGeneric "generic", ProblemReauth "reauth"
type SigninState struct {
	Step        SigninStep
	Problem     SigninProblem
	Address     string
	SentTo      string
	SentInstead bool
	Remembered  *Remembered
	ContinueURL string
	BeginPath   string
	ForgetPath  string
	Passkey     *PasskeyDoor
	// unexported: clear []string
}
type Remembered struct{ Method, Address string }
type PasskeyDoor struct{ BeginPath, FinishPath, ModuleURL, ScriptURL, LegacyRPID string }
func (a *Auth) SigninState(r *http.Request) SigninState
func (a *Auth) PrepareSigninResponse(w http.ResponseWriter, st SigninState)
func (a *Auth) Forget(w http.ResponseWriter, r *http.Request)
func (s SigninState) Door() string  // "ask" | "keymail" | "link" | "passkey" | "sent" | "continue"
func (s SigninState) Focus() string // "field" | "callout" | "onetap" | ""
```

`SigninProblem` values double as catalog-key suffixes: the partial shows `rastrillo.ui.signin_problem_<problem>`.

- [ ] **Step 1: Write the failing tests**

`auth/signinstate_test.go`:

```go
package auth

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/crypto"
	"amadan.net/rastrillo/rastrillo/internal/sealedcookie"
	"amadan.net/rastrillo/rastrillo/lastsignin"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// rememberAs gives b a remembered-method cookie from a's jar.
func rememberAs(a *Auth, b *browser, method, address string) {
	w := httptest.NewRecorder()
	a.jar.Remember(w, lastsignin.Record{Method: method, Address: address})
	b.apply(w)
}

func stateAt(a *Auth, b *browser, target string) SigninState {
	return a.SigninState(b.request(http.MethodGet, target, nil))
}

func TestSigninStateMapsTheQuery(t *testing.T) {
	a, _ := newScreenAuth(t, nil)
	for _, c := range []struct {
		query   string
		step    SigninStep
		problem SigninProblem
	}{
		{"", StepAsk, ProblemNone},
		{"?sent=1", StepSent, ProblemNone},
		{"?err=rate", StepAsk, ProblemRate},
		{"?err=address", StepAsk, ProblemAddress},
		{"?err=expired", StepAsk, ProblemExpired},
		{"?force=1&err=keymail", StepAsk, ProblemKeymail},
		{"?err=1", StepAsk, ProblemGeneric},
		{"?err=nonsense", StepAsk, ProblemNone},
		{"?reauth=1", StepAsk, ProblemReauth},
		{"?continue=x", StepAsk, ProblemExpired},
		{"?continue=", StepAsk, ProblemExpired},
		{"?continue=x&sent=1", StepAsk, ProblemExpired},
		{"?sent=1&err=rate", StepSent, ProblemNone},
		{"?err=rate&reauth=1", StepAsk, ProblemRate},
		{"?err=nonsense&reauth=1", StepAsk, ProblemReauth},
	} {
		st := stateAt(a, newBrowser(), "/signin"+c.query)
		if st.Step != c.step || st.Problem != c.problem {
			t.Errorf("%q → %s/%q, want %s/%q", c.query, st.Step, st.Problem, c.step, c.problem)
		}
		if st.BeginPath != "/signin" || st.ForgetPath != "/signin/forget" {
			t.Errorf("%q: paths %q %q", c.query, st.BeginPath, st.ForgetPath)
		}
	}

	b := newBrowser()
	rememberAs(a, b, "magiclink", "ada@example.com")
	for _, c := range []struct {
		query   string
		step    SigninStep
		problem SigninProblem
	}{
		{"", StepReturning, ProblemNone},
		{"?reauth=1", StepReturning, ProblemReauth},
		{"?err=rate", StepAsk, ProblemRate},
	} {
		if st := stateAt(a, b, "/signin"+c.query); st.Step != c.step || st.Problem != c.problem ||
			st.Remembered == nil || *st.Remembered != (Remembered{"magiclink", "ada@example.com"}) {
			t.Errorf("remembered, %q → %+v", c.query, st)
		}
	}
}

func TestContinueComesFromTheCookieOnly(t *testing.T) {
	a, _ := newKeymailScreen(t, nil)
	b := newBrowser()
	id, _ := startKeymail(t, a, b)
	st := stateAt(a, b, "/signin?continue="+id)
	if st.Step != StepContinue || !strings.HasPrefix(st.ContinueURL, "https://keymail.test/oauth/authorize?") {
		t.Fatalf("a live continuation → %+v", st)
	}
	if st := stateAt(a, b, "/signin?continue="+url.QueryEscape("https://evil.test/")); st.Step != StepAsk || st.Problem != ProblemExpired || st.ContinueURL != "" {
		t.Fatalf("?continue=<a URL> → %+v", st)
	}
}

func TestTheAddressIsNeverFromTheQuery(t *testing.T) {
	a, _ := newScreenAuth(t, nil)
	b := newBrowser()
	if st := stateAt(a, b, "/signin?address=mallory@example.com&err=address"); st.Address != "" {
		t.Fatalf("Address %q came from the query", st.Address)
	}
	rememberAs(a, b, "magiclink", "ada@example.com")
	if st := stateAt(a, b, "/signin"); st.Address != "ada@example.com" {
		t.Fatalf("with only a remembered address, Address = %q", st.Address)
	}
	b.do(a.Begin, http.MethodPost, "/signin", url.Values{"address": {"not an address"}, "force": {"1"}})
	if st := stateAt(a, b, "/signin?err=address"); st.Address != "not an address" {
		t.Fatalf("the attempt did not win over the remembered address: %q", st.Address)
	}
}

func TestSentNamesOnlyTheBoundAttempt(t *testing.T) {
	a, _ := newScreenAuth(t, nil)
	b := newBrowser()
	rememberAs(a, b, "magiclink", "remembered@example.com")
	w := b.do(a.Begin, http.MethodPost, "/signin", url.Values{"address": {"ada@example.com"}, "force": {"1"}, "expect": {"keymail"}})
	loc := w.Header().Get("Location")
	if st := stateAt(a, b, loc); st.Step != StepSent || st.SentTo != "ada@example.com" || !st.SentInstead {
		t.Fatalf("bound Sent → %+v", st)
	}
	if st := stateAt(a, b, "/signin?sent=1&attempt=someone-elses"); st.SentTo != "" || st.SentInstead {
		t.Fatalf("an unbound Sent named %q; Sent never guesses", st.SentTo)
	}
	if st := stateAt(a, b, "/signin?sent=1"); st.SentTo != "" {
		t.Fatalf("a Sent page with no attempt named %q — the remembered address is no evidence of where this link went", st.SentTo)
	}
	b.do(a.Begin, http.MethodPost, "/signin", url.Values{"address": {"not an address"}, "force": {"1"}})
	if st := stateAt(a, b, loc); st.SentTo != "" {
		t.Fatalf("a problem attempt bound a Sent page: %q", st.SentTo)
	}
}

// TestSentConsultsNothing is the enumeration argument (§2): the Sent
// page is computed from the query and this browser's own cookies, so it
// cannot depend on whether an address is known, admitted or keymail.
func TestSentConsultsNothing(t *testing.T) {
	a, _ := newScreenAuth(t, func(c *Config) {
		c.Authorize = func(string) bool { t.Error("SigninState called Authorize"); return false }
	})
	a.flow.Classifier.HTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		t.Errorf("SigninState reached the classifier: %s", r.URL)
		return nil, errors.New("no")
	})}
	a.flow.Mailer = failingMailer{}
	b := newBrowser()
	w := b.do(a.AnswerAsSent, http.MethodPost, "/signin", url.Values{"address": {"ada@example.com"}})
	a.cfg.DB.Close()
	if st := stateAt(a, b, w.Header().Get("Location")); st.Step != StepSent || st.SentTo != "ada@example.com" {
		t.Fatalf("Sent with nothing behind it → %+v", st)
	}
}

// freshAdvisory gives a test its own process-wide once, and puts the
// real one back after: other tests in this binary call SigninState with
// the screen off and would otherwise have spent it already.
func freshAdvisory(t *testing.T) {
	t.Helper()
	saved := advisoryOnce
	advisoryOnce = new(sync.Once)
	t.Cleanup(func() { advisoryOnce = saved })
}

func TestTheAdvisoryFiresOnceAndOnlyWithTheScreenOff(t *testing.T) {
	freshAdvisory(t)
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	off, _ := newTestAuth(t, func(c *Config) { c.Logger = logger })
	another, _ := newTestAuth(t, func(c *Config) { c.Logger = logger })
	off.SigninState(newBrowser().request(http.MethodGet, "/signin", nil))
	off.SigninState(newBrowser().request(http.MethodGet, "/signin", nil))
	another.SigninState(newBrowser().request(http.MethodGet, "/signin", nil))
	if n := strings.Count(logs.String(), "SigninScreen is off"); n != 1 {
		t.Fatalf("the advisory logged %d times across two Auths, want once per process:\n%s", n, logs.String())
	}
	if strings.Contains(logs.String(), "misconfigured") {
		t.Fatal("the advisory must state a condition, never call the app misconfigured: it cannot know")
	}
	logs.Reset()
	freshAdvisory(t)
	on, _ := newScreenAuth(t, func(c *Config) { c.Logger = logger })
	on.SigninState(newBrowser().request(http.MethodGet, "/signin", nil))
	if strings.Contains(logs.String(), "SigninScreen is off") {
		t.Fatal("the advisory fired with the screen on")
	}
}

func TestWithTheScreenOffCookiesAreIgnoredNotCleared(t *testing.T) {
	on, _ := newScreenAuth(t, nil)
	b := newBrowser()
	b.do(on.Begin, http.MethodPost, "/signin", url.Values{"address": {"ada@example.com"}, "force": {"1"}})
	rememberAs(on, b, "magiclink", "ada@example.com")
	off, _ := newTestAuth(t, nil)
	st := stateAt(off, b, "/signin?sent=1")
	if st.Address != "" || st.SentTo != "" || st.Remembered != nil || len(st.clear) != 0 {
		t.Fatalf("screen off read the screen's cookies: %+v", st)
	}
	w := httptest.NewRecorder()
	off.PrepareSigninResponse(w, st)
	if len(setCookies(w)) != 0 {
		t.Fatalf("screen off wrote %v", setCookies(w))
	}
}

func TestPrepareSigninResponse(t *testing.T) {
	a, _ := newScreenAuth(t, nil)
	for _, step := range []SigninStep{StepAsk, StepReturning, StepSent, StepContinue} {
		w := httptest.NewRecorder()
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		a.PrepareSigninResponse(w, SigninState{Step: step})
		if got := w.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("%s: Cache-Control %q; a page carrying an address must not be cached", step, got)
		}
		want := "strict-origin-when-cross-origin"
		if step == StepContinue {
			want = "no-referrer"
		}
		if got := w.Header().Get("Referrer-Policy"); got != want {
			t.Errorf("%s: Referrer-Policy %q, want %q", step, got, want)
		}
	}
}

// TestCookiesFromAnotherKeyAreIgnoredAndCleared is Review Focus 3: a
// rotated InstanceKey turns every screen cookie into noise, and noise
// is cleared quietly — no 500, no error callout, no stale one-tap.
func TestCookiesFromAnotherKeyAreIgnoredAndCleared(t *testing.T) {
	old, _ := newKeymailScreen(t, func(c *Config) { c.InstanceKey = "the-old-key" })
	b := newBrowser()
	startKeymail(t, old, b)
	rememberAs(old, b, "magiclink", "ada@example.com")

	a, _ := newScreenAuth(t, nil)
	st := stateAt(a, b, "/signin")
	if st.Step != StepAsk || st.Problem != ProblemNone || st.Remembered != nil || st.Address != "" {
		t.Fatalf("after a key rotation → %+v, want a plain Ask", st)
	}
	w := httptest.NewRecorder()
	a.PrepareSigninResponse(w, st)
	got := strings.Join(setCookies(w), " ")
	for _, name := range []string{"rastrillo_attempt:clear", "rastrillo_continue:clear", "rastrillo_last_signin:clear"} {
		if !strings.Contains(got, name) {
			t.Errorf("PrepareSigninResponse wrote %q, missing %s", got, name)
		}
	}
	if strings.Contains(got, "rastrillo_pending") || strings.Contains(got, "rastrillo_session") {
		t.Errorf("PrepareSigninResponse touched a cookie it does not own: %s", got)
	}
}

// Each way a remembered cookie can be unbelievable, through the page
// path the app actually runs: SigninState ignores it — no Returning, no
// prefill — and PrepareSigninResponse deletes it and nothing else.
func TestABadRememberedCookieIsIgnoredAndCleared(t *testing.T) {
	a, _ := newScreenAuth(t, nil)
	now := time.Now()
	seal := func(p any) string {
		v, err := sealedcookie.Seal(crypto.Derive([]byte("test-instance-key"), "rastrillo/lastsignin/v1"), p)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	// The jar's payload shape, restated: lastsignin keeps its own
	// unexported, and a test that could only use the public API could
	// not seal a well-formed record for the wrong origin or method.
	type payload struct {
		O   string `json:"o"`
		M   string `json:"m"`
		A   string `json:"a,omitempty"`
		IAT int64  `json:"iat"`
		EXP int64  `json:"exp"`
	}
	good := payload{O: a.cfg.Origin, M: "magiclink", A: "ada@example.com", IAT: now.Unix(), EXP: now.Unix() + 3600}
	expired, wrongOrigin, future, unknown := good, good, good, good
	expired.IAT, expired.EXP = now.Unix()-7200, now.Unix()-3600
	wrongOrigin.O = "http://other.test"
	future.IAT = now.Unix() + 600
	unknown.M = "password"
	for name, value := range map[string]string{
		"tampered":             flipByte(t, seal(good)),
		"expired":              seal(expired),
		"for another origin":   seal(wrongOrigin),
		"issued in the future": seal(future),
		"an unknown method":    seal(unknown),
		"not an envelope":      "remember-me",
	} {
		b := newBrowser()
		b.jar["rastrillo_last_signin"] = &http.Cookie{Name: "rastrillo_last_signin", Value: value}
		st := stateAt(a, b, "/signin")
		if st.Step != StepAsk || st.Remembered != nil || st.Address != "" {
			t.Errorf("%s: → %+v, want a plain Ask", name, st)
		}
		w := httptest.NewRecorder()
		a.PrepareSigninResponse(w, st)
		if got := setCookies(w); len(got) != 1 || got[0] != "rastrillo_last_signin:clear" {
			t.Errorf("%s: Set-Cookie %v, want exactly the remembered cookie deleted", name, got)
		}
	}
	// The control: a good one reads as Returning and is left alone.
	b := newBrowser()
	b.jar["rastrillo_last_signin"] = &http.Cookie{Name: "rastrillo_last_signin", Value: seal(good)}
	st := stateAt(a, b, "/signin")
	w := httptest.NewRecorder()
	a.PrepareSigninResponse(w, st)
	if st.Step != StepReturning || len(setCookies(w)) != 0 {
		t.Fatalf("a good remembered cookie → %+v, Set-Cookie %v", st, setCookies(w))
	}
}

func TestRememberFalseClearsOnTheSigninPage(t *testing.T) {
	on, _ := newScreenAuth(t, nil)
	b := newBrowser()
	rememberAs(on, b, "magiclink", "ada@example.com")
	off, _ := newScreenAuth(t, func(c *Config) { c.Remember = ptr(false) })
	st := stateAt(off, b, "/signin")
	if st.Remembered != nil || st.Step != StepAsk {
		t.Fatalf("Remember=false read a remembered cookie: %+v", st)
	}
	w := httptest.NewRecorder()
	off.PrepareSigninResponse(w, st)
	if got := setCookies(w); len(got) != 1 || got[0] != "rastrillo_last_signin:clear" {
		t.Fatalf("Set-Cookie %v, want the remembered cookie deleted", got)
	}
}

func TestForget(t *testing.T) {
	a, _ := newScreenAuth(t, nil)
	b := newBrowser()
	b.do(a.Begin, http.MethodPost, "/signin", url.Values{"address": {"ada@example.com"}, "force": {"1"}})
	rememberAs(a, b, "magiclink", "ada@example.com")

	w := b.do(a.Forget, http.MethodGet, "/signin/forget", nil)
	if w.Code != http.StatusMethodNotAllowed || w.Header().Get("Allow") != http.MethodPost || len(setCookies(w)) != 0 {
		t.Fatalf("GET Forget → %d, Allow %q, %v; a state change on GET is prefetchable", w.Code, w.Header().Get("Allow"), setCookies(w))
	}
	r := b.request(http.MethodPost, "/signin/forget", url.Values{})
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	cw := httptest.NewRecorder()
	a.Forget(cw, r)
	if cw.Code != http.StatusForbidden {
		t.Fatalf("cross-site Forget → %d", cw.Code)
	}
	w = b.do(a.Forget, http.MethodPost, "/signin/forget", url.Values{})
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/signin" {
		t.Fatalf("Forget → %d %q", w.Code, w.Header().Get("Location"))
	}
	if b.cookie("rastrillo_last_signin") != nil || b.cookie("rastrillo_attempt") != nil {
		t.Fatal("Forget left a cookie behind")
	}
}

// TestAKeymailOneTapThatEndsInALinkSaysSo is §1.3's "honest surprise"
// with round 4's correction: the Sent line states the outcome — a link
// went out this time — never a cause the screen cannot know.
func TestAKeymailOneTapThatEndsInALinkSaysSo(t *testing.T) {
	oneTap := url.Values{"address": {"kay@example.org"}, "expect": {"keymail"}}
	sent := func(t *testing.T, a *Auth, b *browser, w *httptest.ResponseRecorder) SigninState {
		t.Helper()
		loc := w.Header().Get("Location")
		if !strings.HasPrefix(loc, "/signin?sent=1&attempt=") {
			t.Fatalf("→ %q, want a bound Sent page", loc)
		}
		return stateAt(a, b, loc)
	}
	t.Run("the server stopped answering", func(t *testing.T) {
		a, f := newKeymailScreen(t, nil)
		f.servers["keymail.test"] = false
		b := newBrowser()
		if st := sent(t, a, b, b.do(a.Begin, http.MethodPost, "/signin", oneTap)); !st.SentInstead || st.SentTo != "kay@example.org" {
			t.Fatalf("%+v", st)
		}
	})
	t.Run("the list now excludes it", func(t *testing.T) {
		a, _ := newKeymailScreen(t, listed("other.test"))
		b := newBrowser()
		if st := sent(t, a, b, b.do(a.Begin, http.MethodPost, "/signin", oneTap)); !st.SentInstead {
			t.Fatalf("%+v", st)
		}
	})
	t.Run("an admission wrapper refused it", func(t *testing.T) {
		a, _ := newKeymailScreen(t, nil)
		b := newBrowser()
		if st := sent(t, a, b, b.do(a.AnswerAsSent, http.MethodPost, "/signin", oneTap)); !st.SentInstead {
			t.Fatalf("%+v; the wrapper's refusal must read exactly like Begin's link", st)
		}
	})
	t.Run("typed, with no expect", func(t *testing.T) {
		a, f := newKeymailScreen(t, nil)
		f.servers["keymail.test"] = false
		b := newBrowser()
		if st := sent(t, a, b, b.do(a.Begin, http.MethodPost, "/signin", url.Values{"address": {"kay@example.org"}})); st.SentInstead {
			t.Fatalf("%+v", st)
		}
	})
	t.Run("expect never chooses the path", func(t *testing.T) {
		a, _ := newKeymailScreen(t, nil)
		w := newBrowser().do(a.Begin, http.MethodPost, "/signin", oneTap)
		if !strings.HasPrefix(w.Header().Get("Location"), "/signin?continue=") {
			t.Fatalf("a keymail address with expect → %q, want keymail", w.Header().Get("Location"))
		}
	})
}

// TestAnAdmissionWrapperIsNoOracle composes a wrapper the way fichas
// does and checks what a visitor sees is identical, page included, for
// an admitted and a refused address, in both modes.
func TestAnAdmissionWrapperIsNoOracle(t *testing.T) {
	for _, screen := range []bool{false, true} {
		a, m := newTestAuth(t, func(c *Config) { c.SigninScreen = screen })
		f := newKeymailFake()
		wireKeymail(a, f)
		wrapper := func(admit bool) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				if !admit {
					a.AnswerAsSent(w, r)
					return
				}
				a.Begin(w, r)
			}
		}
		form := url.Values{"address": {"ada@example.com"}, "expect": {"keymail"}}
		ab, rb := newBrowser(), newBrowser()
		admitted := ab.do(wrapper(true), http.MethodPost, "/signin", form)
		m.to, f.seen = "", nil
		refused := rb.do(wrapper(false), http.MethodPost, "/signin", form)
		if m.to != "" || len(f.seen) != 0 {
			t.Fatalf("screen=%v: AnswerAsSent sent mail (%q) or classified (%v)", screen, m.to, f.seen)
		}
		sa := stateAt(a, ab, admitted.Header().Get("Location"))
		sr := stateAt(a, rb, refused.Header().Get("Location"))
		if admitted.Code != refused.Code || redirectShape(admitted) != redirectShape(refused) ||
			strings.Join(cookieShape(admitted), "|") != strings.Join(cookieShape(refused), "|") ||
			sa.Step != sr.Step || sa.SentTo != sr.SentTo || sa.SentInstead != sr.SentInstead {
			t.Fatalf("screen=%v: admitted %+v / %q vs refused %+v / %q", screen, sa, redirectShape(admitted), sr, redirectShape(refused))
		}
		if screen && sa.SentTo != "ada@example.com" {
			t.Fatalf("screen on: SentTo %q", sa.SentTo)
		}
		if !screen && (sa.SentTo != "" || admitted.Header().Get("Location") != "/signin?sent=1") {
			t.Fatalf("screen off: %+v %q, want today's plain ?sent=1", sa, admitted.Header().Get("Location"))
		}
	}
}

// TestAKeymailProblemKeepsTheAddressForTheEscapeHatch is Review Focus 4.
func TestAKeymailProblemKeepsTheAddressForTheEscapeHatch(t *testing.T) {
	a, f := newKeymailScreen(t, nil)
	b := newBrowser()
	_, state := startKeymail(t, a, b)
	f.servers["keymail.test"] = false
	w := callback(a, b, state)
	loc := w.Header().Get("Location")
	if loc != "/signin?force=1&err=keymail" {
		t.Fatalf("a failed exchange → %q", loc)
	}
	st := stateAt(a, b, loc)
	if st.Problem != ProblemKeymail || st.Address != "kay@example.org" || st.Door() != "ask" || st.Focus() != "callout" {
		t.Fatalf("after a failed keymail approval → %+v (door %s, focus %s)", st, st.Door(), st.Focus())
	}
}

func TestDoorAndFocusFollowTheMatrix(t *testing.T) {
	door := &PasskeyDoor{BeginPath: "/b", FinishPath: "/f", ModuleURL: "/m.mjs"}
	kay := &Remembered{"keymail", "kay@example.org"}
	ada := &Remembered{"magiclink", "ada@example.com"}
	pk := &Remembered{"passkey", ""}
	for _, c := range []struct {
		name         string
		st           SigninState
		door, focus string
	}{
		{"ask", SigninState{Step: StepAsk}, "ask", "field"},
		{"ask, passkey door", SigninState{Step: StepAsk, Passkey: door}, "ask", "field"},
		{"returning keymail", SigninState{Step: StepReturning, Remembered: kay}, "keymail", "onetap"},
		{"returning link", SigninState{Step: StepReturning, Remembered: ada}, "link", "onetap"},
		{"returning passkey", SigninState{Step: StepReturning, Remembered: pk, Passkey: door}, "passkey", ""},
		{"returning passkey, no door", SigninState{Step: StepReturning, Remembered: pk}, "ask", "field"},
		{"sent", SigninState{Step: StepSent}, "sent", ""},
		{"continue", SigninState{Step: StepContinue}, "continue", ""},
		{"address", SigninState{Step: StepAsk, Problem: ProblemAddress}, "ask", "field"},
		{"rate", SigninState{Step: StepAsk, Problem: ProblemRate}, "ask", "callout"},
		{"expired", SigninState{Step: StepAsk, Problem: ProblemExpired}, "ask", "callout"},
		{"keymail", SigninState{Step: StepAsk, Problem: ProblemKeymail}, "ask", "callout"},
		{"generic", SigninState{Step: StepAsk, Problem: ProblemGeneric}, "ask", "callout"},
		{"reauth, ask", SigninState{Step: StepAsk, Problem: ProblemReauth}, "ask", "field"},
		{"reauth, returning keymail", SigninState{Step: StepReturning, Problem: ProblemReauth, Remembered: kay}, "keymail", "onetap"},
		{"reauth, returning passkey", SigninState{Step: StepReturning, Problem: ProblemReauth, Remembered: pk, Passkey: door}, "passkey", ""},
	} {
		if d, f := c.st.Door(), c.st.Focus(); d != c.door || f != c.focus {
			t.Errorf("%s: door %q focus %q, want %q %q", c.name, d, f, c.door, c.focus)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `GOFLAGS=-mod=mod go test ./auth/ -count=1 -run 'Signin|Continue|Address|Sent|Advisory|Prepare|Cookies|Remember|Forget|OneTap|Oracle|Problem|Door'`
Expected: FAIL to compile — `undefined: SigninStep`, `a.SigninState undefined`, `a.Forget undefined`, …

- [ ] **Step 3: Write `auth/signin.go`**

```go
package auth

import (
	"net/http"
	"net/url"
	"sync"

	"amadan.net/rastrillo/rastrillo/lastsignin"
)

// SigninStep is which screen the sign-in page shows. A string, not an
// int, because ui's signin partial compares it by name.
type SigninStep string

const (
	StepAsk       SigninStep = "ask"
	StepReturning SigninStep = "returning"
	StepSent      SigninStep = "sent"
	StepContinue  SigninStep = "continue"
)

// SigninProblem is what went wrong on the way to the page, if anything.
// Each value is also the suffix of its catalog key
// (rastrillo.ui.signin_problem_<value>), so a problem added here without
// its string fails the partial's unresolved-key check.
type SigninProblem string

const (
	ProblemNone    SigninProblem = ""
	ProblemRate    SigninProblem = "rate"
	ProblemAddress SigninProblem = "address"
	ProblemExpired SigninProblem = "expired"
	ProblemKeymail SigninProblem = "keymail"
	ProblemGeneric SigninProblem = "generic"
	// ProblemReauth is not an error: RequireFreshSession wants a fresh
	// sign-in, and the page says so above whichever door it would
	// otherwise show.
	ProblemReauth SigninProblem = "reauth"
)

// SigninState is everything ui's signin partial needs, as plain data.
// Build it with SigninState, set Passkey if the app mounted passkey
// discovery, call PrepareSigninResponse, then render.
type SigninState struct {
	Step    SigninStep
	Problem SigninProblem
	// Address prefills the email field: this browser's latest attempt's
	// address, else the remembered one. Never from the query.
	Address string
	// SentTo is the address this attempt's link went to, on Sent only,
	// and only when the page's attempt= matches this browser's attempt
	// cookie; otherwise the page says "your inbox" and guesses nothing.
	SentTo string
	// SentInstead: a link went out where the remembered-Keymail one-tap
	// promised Keymail. Bound like SentTo, and false whenever it is "".
	SentInstead bool
	// Remembered is this browser's remembered way in; nil when nothing
	// valid is remembered or remembering is off.
	Remembered *Remembered
	// ContinueURL is the keymail authorize URL, on Continue only. It
	// comes from the continuation cookie, never the query.
	ContinueURL string
	BeginPath   string
	ForgetPath  string
	// Passkey is set by the app, which knows whether and where it
	// mounted passkey discovery; auth cannot. Nil means no passkey door.
	Passkey *PasskeyDoor

	// clear names cookies SigninState found present and unbelievable:
	// how the read-only half tells PrepareSigninResponse what to delete.
	clear []string
}

// Remembered is the way in this browser used last. Address is "" for a
// passkey.
type Remembered struct {
	Method  string
	Address string
}

// PasskeyDoor is where the app mounted passkey discovery (BeginPath,
// FinishPath), where it serves webauthn.JS() (ModuleURL, the WebAuthn
// helper) and passkey.JS() (ScriptURL, the door's own script, which the
// partial loads only when it renders the door), and
// passkey.Config.LegacyRPID, if any.
type PasskeyDoor struct {
	BeginPath, FinishPath string
	ModuleURL             string
	ScriptURL             string
	LegacyRPID            string
}

// advisoryOnce makes the screen-off advisory fire once per process, as
// the spec and the docs say — package-level rather than a field on Auth,
// because a process that built two Auths would otherwise warn twice. A
// pointer so a test can replace it with a fresh one and restore it.
var advisoryOnce = new(sync.Once)

// advisoryText is worded as a condition rather than a diagnosis on
// purpose: auth cannot see whether the app widened form-action or wraps
// Begin itself as fichas does, so it must never say the app is wrong.
const advisoryText = "rastrillo/auth: SigninScreen is off: under the default CSP, with no continuation of your own, " +
	"a keymail address cannot leave the sign-in form, and nothing is remembered"

// SigninState reads the query and this browser's four cookies (attempt,
// continuation, pending, remembered) and returns what the page shows.
// It writes nothing and consults nothing else — no database, no
// classifier, no Authorize, no mailer — which is why the screen adds no
// membership oracle: every state is a function of what this browser
// already holds.
//
// With SigninScreen off it reads only the query, marks nothing, and
// logs the advisory once per process.
func (a *Auth) SigninState(r *http.Request) SigninState {
	st := SigninState{BeginPath: a.cfg.BeginPath, ForgetPath: a.cfg.ForgetPath}
	q := r.URL.Query()
	if !a.cfg.SigninScreen {
		advisoryOnce.Do(func() { a.cfg.Logger.Warn(advisoryText) })
		st.Step, st.Problem = outcome(q, false)
		return st
	}

	rec, res := a.jar.Read(r)
	switch res {
	case lastsignin.Valid:
		st.Remembered = &Remembered{Method: rec.Method, Address: rec.Address}
	case lastsignin.Invalid:
		st.clear = append(st.clear, a.jar.CookieName())
	}
	at, atState := a.openAttempt(r)
	if atState == cookieInvalid {
		st.clear = append(st.clear, a.attemptCookie())
	}
	if _, cs := a.openContinuation(r); cs == cookieInvalid {
		st.clear = append(st.clear, a.continueCookie())
	}

	if q.Has("continue") {
		if u, ok := a.continuationFor(r, q.Get("continue")); ok {
			st.Step, st.ContinueURL = StepContinue, u
			return st
		}
	}
	st.Step, st.Problem = outcome(q, st.Remembered != nil)

	// A prefill is the visitor's own input and stays editable, so the
	// latest attempt wins even if another tab made it.
	switch {
	case atState == cookieValid:
		st.Address = at.A
	case st.Remembered != nil:
		st.Address = st.Remembered.Address
	}
	if st.Step == StepSent && atState == cookieValid && at.K == attemptLink &&
		at.ID != "" && at.ID == q.Get("attempt") {
		st.SentTo, st.SentInstead = at.A, at.A != "" && at.X
	}
	return st
}

// outcome maps the query to a step and a problem, in §1.2's order: the
// first row that applies wins. A ?continue= that did not resolve is
// Expired; an unknown err is no problem at all.
func outcome(q url.Values, remembered bool) (SigninStep, SigninProblem) {
	if q.Has("continue") {
		return StepAsk, ProblemExpired
	}
	if q.Get("sent") == "1" {
		return StepSent, ProblemNone
	}
	switch q.Get("err") {
	case "rate":
		return StepAsk, ProblemRate
	case "address":
		return StepAsk, ProblemAddress
	case "expired":
		return StepAsk, ProblemExpired
	case "keymail":
		return StepAsk, ProblemKeymail
	case "1":
		return StepAsk, ProblemGeneric
	}
	step := StepAsk
	if remembered {
		step = StepReturning
	}
	if q.Get("reauth") == "1" {
		return step, ProblemReauth
	}
	return step, ProblemNone
}

// PrepareSigninResponse is the only writer: the app calls it before
// rendering any state.
//
// no-store on every state, because the Returning and Sent pages carry an
// address and a shared cache or the back-forward history keeps a page,
// not a cookie. no-referrer on Continue only: that page navigates to a
// URL carrying state and the PKCE challenge. Not on the others, on
// purpose — under no-referrer a form POST sends Origin: null, and a
// browser without Sec-Fetch-Site would then fail csrf.SameOrigin, so
// every state with a form keeps the app's policy.
func (a *Auth) PrepareSigninResponse(w http.ResponseWriter, st SigninState) {
	h := w.Header()
	h.Set("Cache-Control", "no-store")
	if st.Step == StepContinue {
		h.Set("Referrer-Policy", "no-referrer")
	}
	for _, name := range st.clear {
		a.clearCookie(w, name)
	}
}

// Forget is POST at Config.ForgetPath — "Use a different email": it
// forgets the remembered way in and ends the attempt. POST only, because
// a state change on GET is one a link prefetcher can make; same-origin
// only, as Begin is.
func (a *Auth) Forget(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !a.sameOrigin(r) {
		http.Error(w, "cross-origin form submission refused", http.StatusForbidden)
		return
	}
	a.jar.Clear(w)
	a.jar.EndAttempt(w)
	a.redirect(w, r, a.cfg.SigninPath)
}

// Door is which way in the page offers, derived here rather than in the
// template so the rules are Go and tested: "sent" and "continue" for
// those steps; "ask" for any error problem, so the visitor can correct
// what they typed; for Returning, the remembered method's one-tap —
// "keymail", "link", or "passkey" when the app wired a passkey door —
// and "ask" otherwise.
func (s SigninState) Door() string {
	switch s.Step {
	case StepSent:
		return "sent"
	case StepContinue:
		return "continue"
	}
	if s.Problem != ProblemNone && s.Problem != ProblemReauth {
		return "ask"
	}
	if s.Step == StepReturning && s.Remembered != nil {
		switch s.Remembered.Method {
		case lastsignin.MethodKeymail:
			return "keymail"
		case lastsignin.MethodMagicLink:
			return "link"
		case lastsignin.MethodPasskey:
			// A remembered passkey with no door wired is not a dead end:
			// it is the email form.
			if s.Passkey != nil {
				return "passkey"
			}
		}
	}
	return "ask"
}

// Focus is where focus starts on an ordinary load (§2's matrix): the
// email field for an address problem, whose error it carries; the
// callout for any other error; the one-tap for a keymail or link
// Returning door; the email field for Ask; nothing for the passkey door,
// Sent and Continue, whose new title is the announcement.
func (s SigninState) Focus() string {
	switch {
	case s.Problem == ProblemAddress:
		return "field"
	case s.Problem != ProblemNone && s.Problem != ProblemReauth:
		return "callout"
	}
	switch s.Door() {
	case "keymail", "link":
		return "onetap"
	case "ask":
		return "field"
	}
	return ""
}
```

`auth/auth.go` — in the package doc before "The decision tree", a paragraph:

```go
// The shipped sign-in screen (ui's signin partial) is opt-in:
// Config.SigninScreen. With it on, SigninState reads what the page
// shows, PrepareSigninResponse writes the headers and cookie deletions
// that go with it, Forget is "Use a different email", and the page
// renders the rest. With it off nothing about Begin or Callback changes.
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `GOFLAGS=-mod=mod go test ./auth/ -count=1 -v -run 'Signin|Continue|Address|Sent|Advisory|Prepare|Cookies|Remember|Forget|OneTap|Oracle|Problem|Door'`
Expected: PASS. Then `GOFLAGS=-mod=mod go test ./auth/ -count=1` — all PASS.

- [ ] **Step 5: Run the gate and commit**

Run: `GOFLAGS=-mod=mod go vet ./... && gofmt -l . && GOFLAGS=-mod=mod go test ./... -count=1`

```bash
git add auth
git commit -F - <<'EOF'
auth: SigninState, PrepareSigninResponse and Forget

The screen needs to know which door to show without auth rendering
HTML, so SigninState returns plain data and consults nothing but the
query and this browser's cookies: the Sent page cannot depend on
whether an address is known, which is the enumeration argument.
Cookie cleanup needs a writer, so a separate PrepareSigninResponse
takes the deletions SigninState marked, sets no-store on every state,
and no-referrer only on Continue, since no-referrer would break the
same-origin check on every page with a form.

Door and Focus put §2's matrix in Go where it is tested, including a
remembered passkey with no door wired, which falls back to the email
form. The screen-off advisory fires once and states a condition,
because auth cannot see whether the app handled keymail itself.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
```

---
### Task 7: `passkey` — discovery ends the attempt and remembers the passkey

**Files:**
- Modify: `passkey/passkey.go` (`Config.Remember`, `DiscoverFinish`, package doc)
- Test: `passkey/signinscreen_test.go`

**Interfaces:**
- Consumes: `lastsignin.Jar`, `lastsignin.Record`, `lastsignin.MethodPasskey`, `lastsignin.New`, `lastsignin.On/Off` (Task 2); `auth.New`, `auth.Config.SigninScreen`, `(*auth.Auth).RememberJar`, `(*auth.Auth).SigninState`, `(*auth.Auth).Begin`, `auth.Schema`, `auth.Remembered` (Tasks 3, 6); existing test helpers `newEnvWith`, `enroll`, `challengeFrom`, `postJSON`, `b64`, `testOrigin`, `testRPID`, `sessionCookie` (passkey/passkey_test.go, passkey/discover_test.go).
- Produces: `passkey.Config.Remember *lastsignin.Jar`.

- [ ] **Step 1: Write the failing tests**

`passkey/signinscreen_test.go`:

```go
package passkey_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/auth"
	"amadan.net/rastrillo/rastrillo/db"
	"amadan.net/rastrillo/rastrillo/lastsignin"
	"amadan.net/rastrillo/rastrillo/migrate"
	"amadan.net/rastrillo/rastrillo/passkey"
	"amadan.net/rastrillo/rastrillo/secondfactor"
	"amadan.net/rastrillo/rastrillo/sessions"
	"amadan.net/rastrillo/rastrillo/webauthn/authtest"
)

func jarFor(t *testing.T, mode lastsignin.Mode) *lastsignin.Jar {
	t.Helper()
	j, err := lastsignin.New(lastsignin.Config{Origin: testOrigin, InstanceKey: "test-instance-key", AttemptCookie: "rastrillo_attempt", Mode: mode})
	if err != nil {
		t.Fatal(err)
	}
	return j
}

func rememberedCookie(t *testing.T, j *lastsignin.Jar, rec lastsignin.Record) *http.Cookie {
	t.Helper()
	w := httptest.NewRecorder()
	j.Remember(w, rec)
	cs := w.Result().Cookies()
	if len(cs) != 1 {
		t.Fatalf("Remember wrote %d cookies", len(cs))
	}
	return cs[0]
}

// discoverCarrying is discover with cookies on the finish request — the
// attempt and remembered cookies a browser on the sign-in screen sends.
func discoverCarrying(t *testing.T, e env, a *authtest.Authenticator, opts authtest.Options, cookies []*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	challenge := challengeFrom(t, postJSON(t, e.h.DiscoverBegin, nil, nil))
	if opts.RPID == "" {
		opts.RPID, opts.Origin = testRPID, testOrigin
	}
	clientData, authData, sig, err := a.Get(challenge, opts)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{
		"id": b64(a.CredID), "clientDataJSON": b64(clientData), "authenticatorData": b64(authData), "signature": b64(sig),
	})
	r := httptest.NewRequest(http.MethodPost, testOrigin+"/passkey/discover/finish", bytes.NewReader(body))
	for _, c := range cookies {
		r.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value})
	}
	w := httptest.NewRecorder()
	e.h.DiscoverFinish(w, r)
	return w
}

// written names every cookie a response sets, a deletion as "name:clear".
func written(w *httptest.ResponseRecorder) map[string]*http.Cookie {
	out := map[string]*http.Cookie{}
	for _, c := range w.Result().Cookies() {
		name := c.Name
		if c.MaxAge < 0 {
			name += ":clear"
		}
		out[name] = c
	}
	return out
}

func names(m map[string]*http.Cookie) []string {
	var out []string
	for n := range m {
		out = append(out, n)
	}
	return out
}

func TestDiscoverWithAJarEndsTheAttemptAndRemembersThePasskey(t *testing.T) {
	for _, held := range []bool{false, true} {
		j := jarFor(t, lastsignin.On)
		e := newEnvWith(t, func(c *passkey.Config) {
			c.Remember = j
			if held {
				c.OtherFactor = func(string) (bool, error) { return true, nil }
			}
		})
		a, _ := authtest.New()
		enroll(t, e, e.signIn(t, "alice"), a)
		cookies := []*http.Cookie{
			{Name: "rastrillo_attempt", Value: "an earlier typed address"},
			rememberedCookie(t, j, lastsignin.Record{Method: "magiclink", Address: "alice@example.com"}),
		}
		opts := authtest.Options{}
		if held {
			opts.Flags = authtest.FlagUserPresent
		}
		w := discoverCarrying(t, e, a, opts, cookies)
		if w.Code != http.StatusOK {
			t.Fatalf("held=%v: discover finish %d %s", held, w.Code, w.Body.String())
		}
		got := written(w)
		if got["rastrillo_attempt:clear"] == nil {
			t.Errorf("held=%v: the attempt cookie was not ended; wrote %v", held, names(got))
		}
		last := got["rastrillo_last_signin"]
		if last == nil {
			t.Fatalf("held=%v: nothing remembered; wrote %v", held, names(got))
		}
		r := httptest.NewRequest(http.MethodGet, testOrigin+"/signin", nil)
		r.AddCookie(&http.Cookie{Name: last.Name, Value: last.Value})
		if rec, res := j.Read(r); res != lastsignin.Valid || rec != (lastsignin.Record{Method: "passkey"}) {
			t.Errorf("held=%v: remembered %+v, %v; a passkey record carries no address", held, rec, res)
		}
		if held && got["rastrillo_secondfactor"] == nil {
			t.Errorf("held path wrote %v, want the half-session too", names(got))
		}
	}
}

func TestAFailedAssertionTouchesNeitherCookie(t *testing.T) {
	j := jarFor(t, lastsignin.On)
	e := newEnvWith(t, func(c *passkey.Config) { c.Remember = j })
	a, _ := authtest.New()
	enroll(t, e, e.signIn(t, "alice"), a)
	stranger, _ := authtest.New()
	w := discoverCarrying(t, e, stranger, authtest.Options{}, []*http.Cookie{{Name: "rastrillo_attempt", Value: "x"}})
	if w.Code != http.StatusBadRequest || len(w.Result().Cookies()) != 0 {
		t.Fatalf("a failed assertion → %d, wrote %v", w.Code, names(written(w)))
	}
}

func TestAnUnadmittedPasskeyEndsTheAttemptAndRemembersNothing(t *testing.T) {
	j := jarFor(t, lastsignin.On)
	e := newEnvWith(t, func(c *passkey.Config) {
		c.Remember = j
		c.Authorize = func(string) bool { return false }
	})
	a, _ := authtest.New()
	enroll(t, e, e.signIn(t, "alice"), a)
	got := written(discoverCarrying(t, e, a, authtest.Options{}, nil))
	if got["rastrillo_attempt:clear"] == nil || got["rastrillo_last_signin"] != nil {
		t.Fatalf("an unadmitted passkey wrote %v", names(got))
	}
}

// TestDiscoverWithoutTheScreenIsToday: no jar, or the screen's jar with
// the screen off, sets nothing beyond today's session cookie — or, on
// the held path, today's half-session cookie (round 4, finding 29).
func TestDiscoverWithoutTheScreenIsToday(t *testing.T) {
	for name, jar := range map[string]*lastsignin.Jar{"no jar": nil, "an Off jar": jarFor(t, lastsignin.Off)} {
		for _, held := range []bool{false, true} {
			e := newEnvWith(t, func(c *passkey.Config) {
				c.Remember = jar
				if held {
					c.OtherFactor = func(string) (bool, error) { return true, nil }
				}
			})
			a, _ := authtest.New()
			enroll(t, e, e.signIn(t, "alice"), a)
			opts := authtest.Options{}
			want := "rastrillo_session"
			if held {
				opts.Flags, want = authtest.FlagUserPresent, "rastrillo_secondfactor"
			}
			got := written(discoverCarrying(t, e, a, opts, nil))
			if len(got) != 1 || got[want] == nil {
				t.Errorf("%s, held=%v: wrote %v, want only %s", name, held, names(got), want)
			}
		}
	}
}

type discardMailer struct{ body string }

func (m *discardMailer) Send(_ context.Context, _, _, body string) error { m.body = body; return nil }

// TestAPasskeySignInLeavesNoTypedAddressBehind is round 3's finding 25
// end to end: this browser typed Alice's address and was remembered as
// her; Bob then signs in with his passkey on it. The next sign-in page
// must offer neither Alice's address nor her method.
func TestAPasskeySignInLeavesNoTypedAddressBehind(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "screen.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if _, err := migrate.Apply(context.Background(), d, migrate.Merge(sessions.Schema, auth.Schema, passkey.Schema, secondfactor.Schema)); err != nil {
		t.Fatal(err)
	}
	au, err := auth.New(auth.Config{DB: d.Writer(), Origin: testOrigin, InstanceKey: "test-instance-key", Mailer: &discardMailer{}, SigninScreen: true})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := sessions.New(sessions.Config{DB: d.Writer(), Origin: testOrigin})
	if err != nil {
		t.Fatal(err)
	}
	h, err := passkey.New(passkey.Config{Sessions: sess, DB: d.Writer(), Origin: testOrigin, Remember: au.RememberJar()})
	if err != nil {
		t.Fatal(err)
	}
	e := env{h: h, sess: sess, db: d.Writer()}
	bob, _ := authtest.New()
	enroll(t, e, e.signIn(t, "bob"), bob)

	jar := map[string]*http.Cookie{}
	keep := func(w *httptest.ResponseRecorder) {
		for _, c := range w.Result().Cookies() {
			if c.MaxAge < 0 {
				delete(jar, c.Name)
			} else {
				jar[c.Name] = c
			}
		}
	}
	form := url.Values{"address": {"alice@example.com"}, "force": {"1"}}
	r := httptest.NewRequest(http.MethodPost, testOrigin+"/signin", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	w := httptest.NewRecorder()
	au.Begin(w, r)
	keep(w)
	w = httptest.NewRecorder()
	au.RememberJar().Remember(w, lastsignin.Record{Method: "magiclink", Address: "alice@example.com"})
	keep(w)

	var cookies []*http.Cookie
	for _, c := range jar {
		cookies = append(cookies, c)
	}
	keep(discoverCarrying(t, e, bob, authtest.Options{}, cookies))

	page := httptest.NewRequest(http.MethodGet, testOrigin+"/signin", nil)
	for _, c := range jar {
		page.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value})
	}
	st := au.SigninState(page)
	if st.Address != "" {
		t.Fatalf("after Bob's passkey sign-in the form is prefilled with %q", st.Address)
	}
	if st.Remembered == nil || *st.Remembered != (auth.Remembered{Method: "passkey"}) {
		t.Fatalf("remembered %+v, want a passkey with no address", st.Remembered)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `GOFLAGS=-mod=mod go test ./passkey/ -count=1 -run 'Jar|Failed|Unadmitted|WithoutTheScreen|TypedAddress'`
Expected: FAIL to compile — `unknown field Remember in struct literal of type passkey.Config`.

- [ ] **Step 3: Write the implementation**

`passkey/passkey.go` — in `Config`, after `Refused`:

```go
	// Remember is the sign-in screen's jar (auth's RememberJar). With it,
	// a verified discover assertion ends the screen's sign-in attempt —
	// so an address typed before the passkey was used stops prefilling
	// the form — and records "passkey" as this browser's way in, with no
	// address: discovery knows a subject, not an address, and keeping
	// the old address would label one person's passkey sign-in with
	// another's name. Nil does neither, and a previously typed or
	// remembered address stays on the screen after a passkey sign-in.
	// An auth with SigninScreen off hands out an inert jar, so wiring
	// this is always safe.
	Remember *lastsignin.Jar
```

`DiscoverFinish` — after the `assert` call succeeds, and around the Authorize check:

```go
	subject, a, ok := h.assert(w, r, "", "discover")
	if !ok {
		return
	}
	// The first factor is proved, so the screen's attempt is over
	// whether or not the subject is admitted — the same rule auth's
	// admit follows, through the same seam.
	if h.cfg.Remember != nil {
		h.cfg.Remember.EndAttempt(w)
	}
	if h.cfg.Authorize != nil && !h.cfg.Authorize(subject) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "not admitted"})
		return
	}
	// Before Hold or SignIn, so the held path and the completed path are
	// remembered alike; heldResponse passes Set-Cookie through.
	if h.cfg.Remember != nil {
		h.cfg.Remember.Remember(w, lastsignin.Record{Method: lastsignin.MethodPasskey})
	}
```

Add `"amadan.net/rastrillo/rastrillo/lastsignin"` to imports. In the package doc, replace the endpoint table's discover lines' neighbourhood with one added paragraph after the "# Sign-in-time 2FA" section's end:

```go
// # The sign-in screen
//
// On auth's shipped sign-in screen the discover pair is the passkey
// door. Set Config.Remember to auth's RememberJar so a passkey sign-in
// ends the screen's attempt and is remembered like the other ways in.
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `GOFLAGS=-mod=mod go test ./passkey/ -count=1 -v -run 'Jar|Failed|Unadmitted|WithoutTheScreen|TypedAddress'`
Expected: PASS. Then `GOFLAGS=-mod=mod go test ./passkey/ ./auth/ -count=1` — all PASS.

- [ ] **Step 5: Run the gate and commit**

Run: `GOFLAGS=-mod=mod go vet ./... && gofmt -l . && GOFLAGS=-mod=mod go test ./... -count=1`

```bash
git add passkey
git commit -F - <<'EOF'
passkey: discovery ends the screen's attempt and remembers the passkey

Without this, a browser where Alice typed her address and Bob then
signed in with his passkey kept offering Alice's address on the next
sign-in page, because the screen prefers the attempt's address (round
3, finding 25). Discovery now calls the same EndAttempt seam auth's
admit does, and records a passkey with no address — discovery knows a
subject, and SubjectFor may make subjects opaque. Both run before Hold
or SignIn so the held path behaves like the completed one; a failed
assertion touches neither cookie.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
```

---
### Task 8: `ui` — `opt`, `Tbdi`, the field's `QuietError`, the callout's `ID`/`Focus`, hidden buttons

**Files:**
- Modify: `ui/funcs.go` (`Funcs` registers `opt` and `Tbdi`; their implementations; doc comment), `ui/funcs_test.go` (the exact-set tests), `ui/partials/field.html`, `ui/partials/callout.html`, `ui/tokens.css`, `ui/ui_test.go`
- Re-copy: `examples/blog/static/tokens.css`, `examples/tickets/static/tokens.css`

**Interfaces:**
- Consumes: `optKey`, `deref` (ui/funcs.go:295-334), `defaultT`.
- Produces:
  - Template func `opt(data any, key string) any` — the value of an optional key on a dict or a struct, or nil.
  - Template func `Tbdi(key string, args ...any) template.HTML` — the catalog string HTML-escaped, each `{name}` replaced once by its escaped value wrapped in `<bdi>`. Bound to the same translator as `T`.
  - `field` partial: optional `QuietError bool` (omits `role="alert"`).
  - `callout` partial: optional `ID string`, `Focus bool` (emits `tabindex="-1" autofocus`).
  - CSS: `.rst-btn[hidden], [rst-btn][hidden] { display: none; }`.

- [ ] **Step 1: Write the failing tests**

In `ui/funcs_test.go`, update both exact-set tests to the new list (the count moves 9 → 11) and rename the first:

```go
func TestFuncsRegistersExactlyTheDocumentedHelpers(t *testing.T) {
	f := Funcs()
	for _, name := range []string{"dict", "list", "menuGroup", "searchClear", "icon", "iconAssets", "T", "Tf", "dateWords", "opt", "Tbdi"} {
		if _, ok := f[name]; !ok {
			t.Errorf("Funcs() is missing %q", name)
		}
	}
	// Exactly these: an accidental extra is a helper the shipped partials
	// do not document and an app cannot rely on.
	if len(f) != 11 {
		t.Errorf("Funcs() has %d entries, want exactly 11", len(f))
	}
}
```

and in `TestFuncsWithReplacesOnlyTAndTf` the same list and `len(f) != 11`. Append to `ui/funcs_test.go`:

```go
func TestOptReadsADictOrAStruct(t *testing.T) {
	type withKey struct {
		ID    string
		Focus bool
		priv  string
	}
	type without struct{ Body string }
	for _, c := range []struct {
		name string
		data any
		key  string
		want any
	}{
		{"a dict key", map[string]any{"ID": "x"}, "ID", "x"},
		{"a missing dict key", map[string]any{}, "ID", nil},
		{"a struct field", withKey{ID: "x"}, "ID", "x"},
		{"a false bool", withKey{}, "Focus", false},
		{"a pointer to a struct", &withKey{ID: "p"}, "ID", "p"},
		{"a struct without the field", without{Body: "b"}, "ID", nil},
		{"an unexported field", withKey{priv: "no"}, "priv", nil},
		{"nil data", nil, "ID", nil},
	} {
		if got := opt(c.data, c.key); got != c.want {
			t.Errorf("%s: opt = %#v, want %#v", c.name, got, c.want)
		}
	}
}

func TestTbdiEscapesAndSubstitutesOnce(t *testing.T) {
	f := tbdi(func(key string, _ ...any) string {
		return map[string]string{
			"k":       "Sent to {address} & kept",
			"missing": "Hello {name}",
		}[key]
	})
	if got := string(f("k", "address", "<b>ada</b>@example.com")); got != "Sent to <bdi>&lt;b&gt;ada&lt;/b&gt;@example.com</bdi> &amp; kept" {
		t.Errorf("escaping: %s", got)
	}
	// A visitor-typed value that looks like a placeholder is text, not a
	// second substitution: sequential replacement would expand it.
	if got := string(f("k", "address", "{address}")); got != "Sent to <bdi>{address}</bdi> &amp; kept" {
		t.Errorf("substituted twice: %s", got)
	}
	if got := string(f("missing")); got != "Hello {name}" {
		t.Errorf("an unmatched placeholder must stay visible: %s", got)
	}
	rtl := string(f("k", "address", "مرحبا@example.com"))
	if !strings.Contains(rtl, "<bdi>مرحبا@example.com</bdi>") {
		t.Errorf("RTL text is not isolated: %s", rtl)
	}
}

func TestTbdiFollowsARebindOfT(t *testing.T) {
	fn := FuncsWith(func(key string, _ ...any) string { return "X {address}" })["Tbdi"].(func(string, ...any) template.HTML)
	if got := string(fn("any", "address", "a@b.c")); got != "X <bdi>a@b.c</bdi>" {
		t.Fatalf("Tbdi ignored the rebound T: %s", got)
	}
}
```

Append to `ui/ui_test.go`:

```go
func TestFieldQuietErrorDropsOnlyTheAlertRole(t *testing.T) {
	quiet := render(t, "field", map[string]any{"ID": "f1", "Name": "n", "Label": "L", "Error": "bad", "QuietError": true})
	if strings.Contains(quiet, `role="alert"`) {
		t.Errorf("QuietError still emits role=alert: %s", quiet)
	}
	for _, want := range []string{`aria-invalid="true"`, `aria-describedby="f1-error"`, `id="f1-error"`} {
		if !strings.Contains(quiet, want) {
			t.Errorf("QuietError lost %s: %s", want, quiet)
		}
	}
}

func TestCalloutIDAndFocus(t *testing.T) {
	got := render(t, "callout", map[string]any{"Body": "b", "ID": "c1", "Focus": true})
	if !strings.Contains(got, `id="c1"`) || !strings.Contains(got, `tabindex="-1" autofocus`) {
		t.Errorf("ID/Focus not emitted: %s", got)
	}
	plain := render(t, "callout", map[string]any{"Body": "b"})
	if strings.Contains(plain, "id=") || strings.Contains(plain, "tabindex") || strings.Contains(plain, "autofocus") {
		t.Errorf("a plain callout grew attributes: %s", plain)
	}
}

// A struct caller written before QuietError, ID and Focus existed must
// still render: reading those keys inline would make its screen a 500.
func TestFieldAndCalloutStillTakeAnOlderStruct(t *testing.T) {
	type field struct {
		ID, Name, Label, Type, Value, Placeholder, Autocomplete, Maxlength, Min, Max, Pattern, Hint, Help, Error string
		Required, Short, Primary, Autofocus                                                                   bool
	}
	type callout struct {
		Body, Title, Tone string
		Alert             bool
	}
	if got := render(t, "field", field{ID: "f", Name: "n", Label: "L", Error: "bad"}); !strings.Contains(got, `role="alert"`) {
		t.Errorf("an older struct lost the default alert role: %s", got)
	}
	if got := render(t, "callout", callout{Body: "b"}); strings.Contains(got, "tabindex") {
		t.Errorf("an older struct grew a tabindex: %s", got)
	}
}

// A button's own display rule would otherwise beat the user agent's
// [hidden] rule, and the passkey door would show before its script
// decided it can work.
func TestHiddenButtonsStayHidden(t *testing.T) {
	if !strings.Contains(string(TokensCSS()), `.rst-btn[hidden], [rst-btn][hidden] { display: none; }`) {
		t.Fatal("tokens.css does not keep a [hidden] button hidden")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `GOFLAGS=-mod=mod go test ./ui/ -count=1 -run 'Funcs|Opt|Tbdi|QuietError|CalloutID|OlderStruct|HiddenButtons'`
Expected: FAIL — `undefined: opt`, `undefined: tbdi`, and the rendering assertions.

- [ ] **Step 3: Implement the helpers**

`ui/funcs.go` — in `Funcs`, the returned map becomes:

```go
	return template.FuncMap{
		"dict": dict, "list": list, "menuGroup": menuGroup, "searchClear": searchClear,
		"icon": c.icon, "iconAssets": c.assets, "T": c.t, "Tf": c.tf,
		"dateWords": dateWords(c.t),
		"opt":       opt, "Tbdi": tbdi(c.t),
	}
```

Extend `Funcs`' doc comment before "An app is free to add…":

```go
// opt reads an optional key off a partial's data — a dict or a Go
// struct — and gives nil when it is absent: the one way a shipped
// partial reads a key added after struct callers existed, because a
// template reading .Key off a struct without that field is an Execute
// error. Tbdi is Tf for a sentence that carries text a visitor typed:
// the catalog string is HTML-escaped and each {name} becomes its value,
// escaped and isolated in <bdi> so an address in a right-to-left script
// cannot reorder the sentence around it.
```

and change "it must not drop these nine" to "it must not drop these eleven".

Add after `optPairs`:

```go
// opt is optKey for templates: the value, or nil for an absent key, an
// absent or unexported field, or a nil.
func opt(data any, key string) any {
	v := optKey(data, key)
	if !v.IsValid() || !v.CanInterface() {
		return nil
	}
	return v.Interface()
}

// tbdi returns the {{Tbdi}} helper bound to one translator, like T and
// dateWords. It walks the catalog string once, left to right, so a
// value that itself contains {name} — an address a visitor typed — is
// printed rather than substituted into, which sequential ReplaceAll
// calls would do. The result is template.HTML because it carries <bdi>;
// every piece of text in it has been escaped here.
func tbdi(t func(key string, args ...any) string) func(key string, args ...any) template.HTML {
	return func(key string, args ...any) template.HTML {
		vals := map[string]string{}
		for i := 0; i+1 < len(args); i += 2 {
			if name, ok := args[i].(string); ok {
				vals[name] = fmt.Sprint(args[i+1])
			}
		}
		src := t(key)
		var b strings.Builder
		for {
			open := strings.IndexByte(src, '{')
			if open < 0 {
				b.WriteString(template.HTMLEscapeString(src))
				break
			}
			end := strings.IndexByte(src[open:], '}')
			if end < 0 {
				b.WriteString(template.HTMLEscapeString(src))
				break
			}
			b.WriteString(template.HTMLEscapeString(src[:open]))
			if v, ok := vals[src[open+1:open+end]]; ok {
				b.WriteString("<bdi>" + template.HTMLEscapeString(v) + "</bdi>")
			} else {
				b.WriteString(template.HTMLEscapeString(src[open : open+end+1]))
			}
			src = src[open+end+1:]
		}
		return template.HTML(b.String())
	}
}
```

- [ ] **Step 4: Extend the two partials**

`ui/partials/field.html` — add to the Keys list in its comment:

```
       QuietError bool, optional — the error without role="alert", for a
                 page where focus lands on this field at load: the focus
                 already announces the message, and a live alert would
                 race it
```

and change line 22's opening tag to:

```html
{{if .Error}}<p rst-field-error id="{{.ID}}-error"{{if not (opt . "QuietError")}} role="alert"{{end}}>{{.Error}}</p>
```

`ui/partials/callout.html` — add to its Keys comment:

```
       ID     string, optional — for aria-describedby or a skip target
       Focus  bool, optional — tabindex="-1" autofocus: focus starts on
              the callout, so a problem shown at load is read first.
              Not role="alert": focus already announces it
```

and change its opening `<div>` to:

```html
{{define "callout"}}<div rst-callout{{with opt . "ID"}} id="{{.}}"{{end}} rst-tone="{{or .Tone "info"}}"{{if .Alert}} role="alert"{{end}}{{if opt . "Focus"}} tabindex="-1" autofocus{{end}}>
```

(the rest of the line unchanged).

- [ ] **Step 5: Keep hidden buttons hidden**

`ui/tokens.css` — immediately after the `.rst-btn--block, [rst-btn~="block"] { … }` rule:

```css
/* The UA's [hidden] { display: none } loses to the display this file
   gives every button, so a button marked hidden would show anyway. The
   sign-in screen's passkey door ships hidden and is revealed only where
   a ceremony can work; without this it would show everywhere. */
.rst-btn[hidden], [rst-btn][hidden] { display: none; }
```

Re-copy: `cp ui/tokens.css examples/blog/static/tokens.css && cp ui/tokens.css examples/tickets/static/tokens.css`.

- [ ] **Step 6: Run the tests to verify they pass**

Run: `GOFLAGS=-mod=mod go test ./ui/ -count=1 && (cd examples/blog && GOFLAGS=-mod=mod go test ./...) && (cd examples/tickets && GOFLAGS=-mod=mod go test ./...)`
Expected: PASS everywhere (the twin, inline-style and existing field/callout tests included).

- [ ] **Step 7: Run the gate and commit**

Run: `GOFLAGS=-mod=mod go vet ./... && gofmt -l . && GOFLAGS=-mod=mod go test ./... -count=1`

```bash
git add ui examples/blog/static/tokens.css examples/tickets/static/tokens.css
git commit -F - <<'EOF'
ui: opt and Tbdi; QuietError on field; ID and Focus on callout

The sign-in screen focuses the field or the callout that carries a
problem at load, and a role="alert" there would race the focus
announcement, so both partials need a quiet form. They gain the keys
through opt rather than inline reads: a struct caller written before
the keys existed would otherwise turn into an Execute error.

Tbdi exists because the screen shows an address the visitor typed
inside a translated sentence. It has to be escaped, isolated so an RTL
address cannot reorder the sentence, and substituted once so typed text
that looks like a placeholder stays text.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
```

---
### Task 9: `passkey` — the passkey door's own module, `passkey.JS()`

The spec first put the door's script in `ui/rastrillo.js`. That file is at 16,364 of its 16,384-byte cap, and its size test says to split past 16 KiB. The door ships instead as its own ES module, embedded in `passkey` and loaded by the `signin` partial with `<script type="module" src>` only when it renders a passkey door (spec §1.6, as amended with this plan). No other page of an app ever requests it, `rastrillo.js` is untouched, and the default CSP allows it unchanged: it has no `script-src`, so scripts fall back to `default-src 'self'`, which admits a same-origin module.

**Files:**
- Create: `passkey/js/signin.mjs`, `passkey/js.go`, `passkey/js/signin_node.mjs`, `passkey/signinjs_test.go`

**Interfaces:**
- Consumes: the markup Task 12's partial writes — `<button data-rst-passkey hidden data-rst-passkey-begin data-rst-passkey-finish data-rst-passkey-module [data-rst-passkey-legacy-rpid] data-rst-passkey-cancelled data-rst-passkey-failed aria-describedby="rst-signin-passkey-msg">`, `<p id="rst-signin-passkey-msg" aria-live="polite">`, then `<script type="module" src="<ScriptURL>">`; `webauthn.mjs`'s `available()` and `authenticate({challenge, rpId, legacyRpId})` → `{credentialId, clientDataJSON, authenticatorData, signature, prf}` (webauthn/js/webauthn.mjs:34-36, 96-117); discover begin → `{"challenge"}`, finish ← `{id, clientDataJSON, authenticatorData, signature}` → `{"ok": true, "to", ["pending"]}` (passkey/passkey.go:424-455).
- Produces: `func passkey.JS() []byte`; the module's exports `localPath(to)`, `safeNext(to, loc) string`, `postJSON(url, body)`, `ceremony({post, authenticate, begin, finish, rpId, legacyRpId, loc}) → Promise<{navigate} | {message: "cancelled"|"failed"}>`. Apps serve `JS()` with a JavaScript content type at the URL they put in `auth.PasskeyDoor.ScriptURL` (Task 6's field).

- [ ] **Step 1: Write the failing tests**

`passkey/js/signin_node.mjs`:

```js
// signin_node — drives signin.mjs's ceremony and destination guard in
// plain Node, with the network and the authenticator replaced. Nothing
// here touches a page: signin.mjs keeps every DOM step behind its own
// `document` guard, which Node never passes.
//
// passkey/signinjs_test.go is the only caller. It pipes {origin,
// destinations} on stdin and reads back each scenario's outcome, the
// keys each scenario's finish request carried, and where each
// destination would send the page. This file sits beside signin.mjs so
// `go test` finds it by path; it is never embedded.
import { ceremony, safeNext } from "./signin.mjs";

const answer = (body) => async () => body;
const fail = (err) => async () => { throw err; };
const assertion = () => ({
  credentialId: "id", clientDataJSON: "cd", authenticatorData: "ad", signature: "sig",
  prf: new Uint8Array([1, 2, 3]),
});

const scenarios = {
  "signed in, local destination": { begin: answer({ challenge: "c" }), auth: answer(assertion()), finish: answer({ ok: true, to: "/home" }) },
  "held for a second factor": { begin: answer({ challenge: "c" }), auth: answer(assertion()), finish: answer({ ok: true, to: "/signin/confirm", pending: true }) },
  "signed in, destination off-site": { begin: answer({ challenge: "c" }), auth: answer(assertion()), finish: answer({ ok: true, to: "//evil.example/x" }) },
  "signed in, no destination": { begin: answer({ challenge: "c" }), auth: answer(assertion()), finish: answer({ ok: true }) },
  "finish says ok false": { begin: answer({ challenge: "c" }), auth: answer(assertion()), finish: answer({ ok: false, to: "/home" }) },
  "finish has no ok": { begin: answer({ challenge: "c" }), auth: answer(assertion()), finish: answer({ to: "/home" }) },
  "finish answers null": { begin: answer({ challenge: "c" }), auth: answer(assertion()), finish: answer(null) },
  "begin not 2xx": { begin: fail(new Error("status 500")), auth: answer(assertion()), finish: answer({ ok: true, to: "/" }) },
  "begin without a challenge": { begin: answer({}), auth: answer(assertion()), finish: answer({ ok: true, to: "/" }) },
  "finish not 2xx": { begin: answer({ challenge: "c" }), auth: answer(assertion()), finish: fail(new Error("status 400")) },
  "the network is down": { begin: fail(new TypeError("Failed to fetch")), auth: answer(assertion()), finish: answer({ ok: true, to: "/" }) },
  "dismissed, or no passkey": { begin: answer({ challenge: "c" }), auth: fail(new Error("no passkey was offered")), finish: answer({ ok: true, to: "/" }) },
  "the authenticator failed": { begin: answer({ challenge: "c" }), auth: fail(new Error("NotReadableError")), finish: answer({ ok: true, to: "/" }) },
};

let raw = "";
for await (const chunk of process.stdin) raw += chunk;
const { origin, destinations } = JSON.parse(raw);
const loc = { href: origin + "/signin", origin };
const out = { outcomes: {}, finishKeys: {}, destinations: destinations.map((to) => safeNext(to, loc)) };
for (const [name, sc] of Object.entries(scenarios)) {
  let sent = null;
  const post = (url, body) => {
    if (url === "/finish") {
      sent = body;
      return sc.finish();
    }
    return sc.begin();
  };
  out.outcomes[name] = await ceremony({ post, authenticate: sc.auth, begin: "/begin", finish: "/finish", rpId: "app.test", loc });
  out.finishKeys[name] = sent ? Object.keys(sent).sort() : null;
}
process.stdout.write(JSON.stringify(out));
```

`passkey/signinjs_test.go`:

```go
package passkey_test

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"amadan.net/rastrillo/rastrillo/nodetest"
	"amadan.net/rastrillo/rastrillo/passkey"
)

type outcome struct {
	Navigate string `json:"navigate,omitempty"`
	Message  string `json:"message,omitempty"`
}

func runSigninJS(t *testing.T, destinations []any) (map[string]outcome, map[string][]string, []string) {
	t.Helper()
	in, err := json.Marshal(map[string]any{"origin": "https://app.test", "destinations": destinations})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Outcomes     map[string]outcome  `json:"outcomes"`
		FinishKeys   map[string][]string `json:"finishKeys"`
		Destinations []string            `json:"destinations"`
	}
	if err := json.Unmarshal(nodetest.Run(t, nodetest.Cmd{Args: []string{"js/signin_node.mjs"}, Stdin: in}), &got); err != nil {
		t.Fatal(err)
	}
	return got.Outcomes, got.FinishKeys, got.Destinations
}

// Every branch a real ceremony can end in. The rule the table holds: the
// page navigates only when finish answered ok:true, and then only to a
// safe destination; everything else is a message and focus stays put.
func TestThePasskeyDoorNavigatesOnlyAfterASuccess(t *testing.T) {
	outcomes, keys, _ := runSigninJS(t, nil)
	want := map[string]outcome{
		"signed in, local destination":    {Navigate: "/home"},
		"held for a second factor":        {Navigate: "/signin/confirm"},
		"signed in, destination off-site": {Navigate: "/"},
		"signed in, no destination":       {Navigate: "/"},
		"finish says ok false":            {Message: "failed"},
		"finish has no ok":                {Message: "failed"},
		"finish answers null":             {Message: "failed"},
		"begin not 2xx":                   {Message: "failed"},
		"begin without a challenge":       {Message: "failed"},
		"finish not 2xx":                  {Message: "failed"},
		"the network is down":             {Message: "failed"},
		"dismissed, or no passkey":        {Message: "cancelled"},
		"the authenticator failed":        {Message: "failed"},
	}
	if !reflect.DeepEqual(outcomes, want) {
		t.Fatalf("outcomes:\n got %v\nwant %v", outcomes, want)
	}
	if got := keys["signed in, local destination"]; !reflect.DeepEqual(got, []string{"authenticatorData", "clientDataJSON", "id", "signature"}) {
		t.Fatalf("finish carried %v; it sends the assertion as id and never the PRF output, which is a client-side secret", got)
	}
}

// §1.6 step 4: the server's "to" is followed only when it is a local
// absolute path by sessions.SafeReturn's rule AND resolves to this
// origin. A same-origin absolute URL is refused too: the server never
// sends one. ui/rastrillo.js keeps its own copy of the local-path rule
// for Rastrillo-Location; its contract test pins that copy's text.
func TestThePasskeyDoorNavigatesOnlyToALocalPath(t *testing.T) {
	cases := []any{
		"//evil.example/x", "/\t/evil.example", "/\n/evil.example", "/\\evil.example",
		"https://evil.example/x", "https://app.test/home",
		"/", "/home", "/confirm?x=1",
		nil, 42, "",
	}
	want := []string{"/", "/", "/", "/", "/", "/", "/", "/home", "/confirm?x=1", "/", "/", "/"}
	if _, _, got := runSigninJS(t, cases); !reflect.DeepEqual(got, want) {
		t.Fatalf("safeNext:\n got %q\nwant %q", got, want)
	}
}

func TestJSIsTheModuleAndSelfContained(t *testing.T) {
	file, err := os.ReadFile("js/signin.mjs")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(passkey.JS(), file) {
		t.Fatal("passkey.JS() is not js/signin.mjs")
	}
	for _, bad := range []string{"http://", "https://", "eval(", "new Function", "innerHTML"} {
		if bytes.Contains(file, []byte(bad)) {
			t.Errorf("signin.mjs contains %q; it must run under the default CSP and write only text", bad)
		}
	}
	for _, want := range []string{"export function ceremony", "export function safeNext", `typeof document !== "undefined"`, "aria-disabled", "pageshow"} {
		if !bytes.Contains(file, []byte(want)) {
			t.Errorf("signin.mjs lacks %q", want)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `RASTRILLO_TEST_REQUIRE_NODE=1 GOFLAGS=-mod=mod go test ./passkey/ -count=1 -run 'PasskeyDoor|JSIsTheModule'`
Expected: FAIL to compile — `undefined: passkey.JS`.

- [ ] **Step 3: Write the module and its accessor**

`passkey/js/signin.mjs`:

```js
// signin.mjs — the sign-in screen's passkey door. ui's signin partial
// loads it with <script type="module"> only when it renders a passkey
// door, so no other page of an app ever requests it. The app serves it
// from passkey.JS() at the URL it gives auth.PasskeyDoor.ScriptURL.
//
// The door ships hidden. It is revealed only where a ceremony can run —
// PublicKeyCredential exists, the app's webauthn module loads, and the
// module says credentials are available — because a passkey button that
// cannot work is a dead end on the one screen that must never have one.
// The email form beside it works with or without any of this.
//
// Busy is drawn as rastrillo.js's busy rule draws it (aria-busy and an
// rst-spin child, which tokens.css keys the spinner on) plus
// aria-disabled, and never with disabled: a disabled button drops
// focus, and a failure is announced to someone whose focus has to still
// be on the button to try again.

// localPath is sessions.SafeReturn's rule: exactly one leading "/", no
// backslash, no control character — browsers strip tab/CR/LF before
// parsing, so "/\t/evil.example" would otherwise resolve off-site.
export function localPath(to) {
  return typeof to === "string" && to.charAt(0) === "/" && to.charAt(1) !== "/" &&
    to.indexOf("\\") === -1 && !/[\u0000-\u001f\u007f]/.test(to);
}

// safeNext is where a successful sign-in may send the page: the
// server's "to" when it is a local path AND parses to this origin, "/"
// otherwise. The server never sends an absolute URL, so accepting even
// a same-origin one would only widen what a bad answer could do; the
// parsed-origin check is the second line, for whatever a future edit to
// localPath lets through.
export function safeNext(to, loc) {
  if (!localPath(to)) return "/";
  try {
    return new URL(to, loc.href).origin === loc.origin ? to : "/";
  } catch (e) {
    return "/";
  }
}

// postJSON is a same-origin POST, so csrf.Protect sees
// Sec-Fetch-Site: same-origin. A non-2xx answer is an error, never a
// body to act on.
export async function postJSON(url, body) {
  const res = await fetch(url, {
    method: "POST",
    credentials: "same-origin",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body || {}),
  });
  if (!res.ok) throw new Error("status " + res.status);
  return res.json();
}

// ceremony runs one passkey sign-in with its effects passed in, so Node
// can drive every branch. It never throws: it resolves to {navigate} —
// only when finish answered ok:true — or to {message}, "cancelled" or
// "failed". A failed answer must never navigate as if it had worked.
export async function ceremony({ post, authenticate, begin, finish, rpId, legacyRpId, loc }) {
  let a;
  try {
    const b = await post(begin, {});
    if (!b || typeof b.challenge !== "string") return { message: "failed" };
    a = await authenticate({ challenge: b.challenge, rpId, legacyRpId });
  } catch (err) {
    // "no passkey was offered" is webauthn.mjs's one message for a
    // dismissed prompt and for no passkey at all — the same on purpose,
    // so a site cannot probe for credentials — and it gets the gentler
    // words.
    return { message: err && err.message === "no passkey was offered" ? "cancelled" : "failed" };
  }
  try {
    // authenticate() names the credential credentialId; the server reads
    // it as id. prf is never sent: it is a client-side secret.
    const done = await post(finish, {
      id: a.credentialId,
      clientDataJSON: a.clientDataJSON,
      authenticatorData: a.authenticatorData,
      signature: a.signature,
    });
    if (!done || done.ok !== true) return { message: "failed" };
    return { navigate: safeNext(done.to, loc) };
  } catch (err) {
    return { message: "failed" };
  }
}

function busy(btn, on) {
  let spin = btn.querySelector("[rst-spin]");
  if (!on) {
    btn.removeAttribute("aria-busy");
    btn.removeAttribute("aria-disabled");
    if (spin) spin.remove();
    return;
  }
  btn.setAttribute("aria-busy", "true");
  btn.setAttribute("aria-disabled", "true");
  if (!spin) {
    spin = document.createElement("span");
    spin.setAttribute("rst-spin", "");
    spin.setAttribute("aria-hidden", "true");
    btn.insertBefore(spin, btn.firstChild);
  }
}

function door(btn) {
  if (!window.PublicKeyCredential) return;
  const msg = document.getElementById(btn.getAttribute("aria-describedby") || "");
  let mod = null;
  import(btn.getAttribute("data-rst-passkey-module")).then((m) => {
    if (m && m.available && m.available()) {
      mod = m;
      btn.hidden = false; // revealing never moves focus
    }
  }, () => {});
  btn.addEventListener("click", async () => {
    // A second click while one ceremony runs would start another, and
    // the first one's answer would land on a page that has moved on.
    if (!mod || btn.getAttribute("aria-busy") === "true") return;
    busy(btn, true);
    if (msg) msg.textContent = "";
    const out = await ceremony({
      post: postJSON,
      authenticate: mod.authenticate,
      begin: btn.getAttribute("data-rst-passkey-begin"),
      finish: btn.getAttribute("data-rst-passkey-finish"),
      rpId: location.hostname,
      legacyRpId: btn.getAttribute("data-rst-passkey-legacy-rpid") || undefined,
      loc: location,
    });
    if (out.navigate) {
      location.assign(out.navigate);
      return;
    }
    busy(btn, false);
    if (msg) msg.textContent = btn.getAttribute("data-rst-passkey-" + out.message) || "";
  });
}

// A module runs after the document is parsed, so the door is already
// there. Node has no document and stops here.
if (typeof document !== "undefined") {
  document.querySelectorAll("[data-rst-passkey]").forEach(door);
  // A sign-in navigates away mid-busy; the back-forward cache restores
  // the page exactly as it was left, spinner and all.
  window.addEventListener("pageshow", (e) => {
    if (e.persisted) document.querySelectorAll("[data-rst-passkey][aria-busy]").forEach((b) => busy(b, false));
  });
}
```

`passkey/js.go`:

```go
package passkey

import _ "embed"

//go:embed js/signin.mjs
var signinJS []byte

// JS is the sign-in screen's passkey door: an ES module that reveals
// the signin partial's passkey button where a ceremony can run, runs
// the discover pair, and navigates only after a successful answer.
// Serve it with a JavaScript content type at the URL you set as
// auth.PasskeyDoor.ScriptURL, beside webauthn.JS(). The partial loads
// it only on a page that shows the passkey door.
func JS() []byte { return signinJS }
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `RASTRILLO_TEST_REQUIRE_NODE=1 GOFLAGS=-mod=mod go test ./passkey/ -count=1 -v -run 'PasskeyDoor|JSIsTheModule'`
Expected: PASS, three tests. `git diff --stat ui/rastrillo.js` prints nothing: the shim is untouched.

- [ ] **Step 5: Run the gate and commit**

Run: `GOFLAGS=-mod=mod go vet ./... && gofmt -l . && RASTRILLO_TEST_REQUIRE_NODE=1 GOFLAGS=-mod=mod go test ./... -count=1`

```bash
git add passkey/js passkey/js.go passkey/signinjs_test.go
git commit -F - <<'EOF'
passkey: the sign-in screen's passkey door as its own module

rastrillo.js is at its size cap, and every page of every app loads it;
the passkey door exists on one page. As an embedded module the signin
partial loads only when it renders the door, it costs no other page a
byte, needs no CSP change, and leaves the shim alone.

The ceremony takes its network and authenticator as arguments, so Node
drives every ending: the page navigates only when finish answers
ok:true and only to a local path on this origin; a failed, malformed or
unreachable answer is a message with focus left on the button.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
git push origin signin-screen
```

---

### Task 10: Copy review, batch 1 — the screen's strings and the gallery's

The spec (§2) and the copy-review skill both say the operator reviews user-facing English before it is written into any file. This batch is every string the screen shows and every sentence the gallery will say about it; batch 2 (Task 14) is the docs and the CHANGELOG. The catalog strings are written here, approved, and translated once. The gallery's are written by Tasks 11 and 12 from the approved text — in `internal/designsystem` the English is also the translation key, so it must be final before its eleven translations are drafted (AGENTS.md, "User-facing copy").

**Files:**
- Create (untracked, `.gitignore`d): `copy-review/strings.json`, and the skill's `copy-review/result.json`
- Modify: `locales/*.toml` (all 12)

**Interfaces:**
- Consumes: the catalog keys Task 12's partial uses and the gallery strings Tasks 11–12 write (listed in the index below).
- Produces: the 29 `rastrillo.ui.signin_*` keys in all 12 catalogs, approved in English and translated; `copy-review/result.json` holding the approved `gallery.*` strings, which Tasks 11 and 12 read. Leave `copy-review/` in place until Task 12 deletes it.

- [ ] **Step 1: Write the string index**

Write `copy-review/strings.json` exactly as below, then validate it: `python3 -m json.tool copy-review/strings.json > /dev/null` prints nothing.

```json
[
  {"id": "rastrillo.ui.signin_heading", "section": "Sign-in screen: asking", "label": "Heading", "text": "Sign in to {name}", "context": "The heading on the sign-in card when it asks for an email address, and above any problem message. {name} is the app's name.", "notes": "Keep {name}."},
  {"id": "rastrillo.ui.signin_title", "section": "Sign-in screen: asking", "label": "Browser tab title", "text": "Sign in — {name}", "context": "The browser tab's title on the same page. A screen reader announces it when the page opens.", "notes": "Keep {name}."},
  {"id": "rastrillo.ui.signin_problem_title", "section": "Sign-in screen: asking", "label": "Tab title when something went wrong", "text": "Problem: sign in — {name}", "context": "The tab title when the page is showing a problem (too many tries, a used link, and so on), so someone listening hears there is a problem before the page content.", "notes": "Keep {name}."},
  {"id": "rastrillo.ui.signin_email", "section": "Sign-in screen: asking", "label": "Email field label", "text": "Email", "context": "The label on the one field the sign-in card asks for."},
  {"id": "rastrillo.ui.signin_submit", "section": "Sign-in screen: asking", "label": "Main button", "text": "Continue", "context": "The button under the email field. It cannot say what happens next, because that depends on the address: most people get a link by email, some go to Keymail."},
  {"id": "rastrillo.ui.signin_passkey", "section": "Sign-in screen: asking", "label": "Passkey button", "text": "Sign in with a passkey", "context": "A second button under the email form, shown only in apps with passkeys and only on devices that can use them."},
  {"id": "rastrillo.ui.signin_passkey_cancelled", "section": "Sign-in screen: asking", "label": "Passkey not used", "text": "No passkey was used. Try again, or use your email.", "context": "Appears under the passkey button when the person closed the passkey prompt, or had no passkey for this site. The browser does not say which, on purpose, so the words cannot either."},
  {"id": "rastrillo.ui.signin_passkey_failed", "section": "Sign-in screen: asking", "label": "Passkey failed", "text": "That didn't work. Try again, or use your email.", "context": "Appears under the passkey button when the passkey sign-in failed for any other reason."},
  {"id": "rastrillo.ui.signin_problem_rate", "section": "Sign-in screen: problems", "label": "Too many tries", "text": "Too many tries. Try again in a few minutes.", "context": "A message above the email form after too many sign-in attempts in a short time."},
  {"id": "rastrillo.ui.signin_problem_address", "section": "Sign-in screen: problems", "label": "Not an address", "text": "That doesn't look like an email address.", "context": "An error under the email field, which still holds what they typed, when it is not an address."},
  {"id": "rastrillo.ui.signin_problem_expired", "section": "Sign-in screen: problems", "label": "Link used or expired", "text": "That link has expired or was already used. Send a new one.", "context": "Above the email form when someone opened a sign-in link that no longer works. Links work once and last 15 minutes."},
  {"id": "rastrillo.ui.signin_problem_keymail", "section": "Sign-in screen: problems", "label": "Keymail could not confirm", "text": "Keymail couldn't confirm it's you.", "context": "Above the email form after the Keymail step failed. The button under the form changes to the next string, so they can get a link instead."},
  {"id": "rastrillo.ui.signin_send_link_instead", "section": "Sign-in screen: problems", "label": "Button after a Keymail failure", "text": "Send me a link instead", "context": "Replaces the main button only after Keymail could not confirm the person, and sends an email link without trying Keymail again."},
  {"id": "rastrillo.ui.signin_problem_generic", "section": "Sign-in screen: problems", "label": "Something else went wrong", "text": "Something went wrong. Try again.", "context": "Above the email form when sign-in failed for a reason the page cannot name, such as the mail server being down."},
  {"id": "rastrillo.ui.signin_problem_reauth", "section": "Sign-in screen: problems", "label": "Sign in again", "text": "Sign in again to continue.", "context": "Not an error. Shown when the person is signed in but the page they asked for needs a fresh sign-in first, such as changing account settings."},
  {"id": "rastrillo.ui.signin_to_keymail", "section": "Sign-in screen: coming back", "label": "One-tap for a Keymail user", "text": "Continue to Keymail", "context": "The one button offered to someone whose last sign-in on this browser was through Keymail. Their address is shown under it (next string). Also the link on the page that moves on to Keymail by itself."},
  {"id": "rastrillo.ui.signin_remembered_as", "section": "Sign-in screen: coming back", "label": "Whose Keymail", "text": "as {address}", "context": "The line under Continue to Keymail, naming the remembered address, so the button reads as Continue to Keymail, as ada@example.com.", "notes": "Keep {address}. It is shown isolated, so a right-to-left address cannot reorder the sentence."},
  {"id": "rastrillo.ui.signin_continue_as", "section": "Sign-in screen: coming back", "label": "One-tap for an email-link user", "text": "Continue as {address}", "context": "The one button offered to someone whose last sign-in on this browser was an email link. One tap sends them a new link.", "notes": "Keep {address}."},
  {"id": "rastrillo.ui.signin_passkey_remembered", "section": "Sign-in screen: coming back", "label": "One-tap for a passkey user", "text": "Sign in with your passkey", "context": "The passkey button, first on the card, for someone whose last sign-in on this browser was a passkey. The email form stays underneath."},
  {"id": "rastrillo.ui.signin_different", "section": "Sign-in screen: coming back", "label": "Forget me", "text": "Use a different email", "context": "Under a one-tap, and on the page after a link was sent. It forgets the remembered address on this browser and shows the empty form."},
  {"id": "rastrillo.ui.signin_sent_heading", "section": "Sign-in screen: link sent", "label": "Heading", "text": "Check your email", "context": "The heading after a sign-in link was emailed."},
  {"id": "rastrillo.ui.signin_sent_title", "section": "Sign-in screen: link sent", "label": "Tab title", "text": "Check your email — {name}", "context": "The tab title on the same page.", "notes": "Keep {name}."},
  {"id": "rastrillo.ui.signin_sent_instead", "section": "Sign-in screen: link sent", "label": "A link instead of Keymail", "text": "We sent you a sign-in link this time.", "context": "The first line on that page only when the person tapped Continue to Keymail and got an email link instead. The page cannot know why (the server may be down, or the app may not trust it), so the line says what happened, not why."},
  {"id": "rastrillo.ui.signin_sent_to", "section": "Sign-in screen: link sent", "label": "Where the link went", "text": "We sent a link to {address}.", "context": "Names the address the link went to, so a typo can be noticed now rather than after waiting for an email that will not come. Followed by the next string in the same paragraph.", "notes": "Keep {address}."},
  {"id": "rastrillo.ui.signin_sent_inbox", "section": "Sign-in screen: link sent", "label": "Where the link went, unknown address", "text": "We sent a link to your inbox.", "context": "Used instead of the previous string when this browser did not send the address itself, for example a link opened in another browser. The page never guesses an address."},
  {"id": "rastrillo.ui.signin_sent_once", "section": "Sign-in screen: link sent", "label": "How the link works", "text": "The link works once and expires soon.", "context": "Follows the previous line in the same paragraph. Links last 15 minutes."},
  {"id": "rastrillo.ui.signin_continue_heading", "section": "Sign-in screen: going to Keymail", "label": "Heading", "text": "Taking you to Keymail", "context": "The heading on a page that moves on to Keymail by itself, straight away, after someone entered an address that uses Keymail."},
  {"id": "rastrillo.ui.signin_continue_title", "section": "Sign-in screen: going to Keymail", "label": "Tab title", "text": "Taking you to Keymail — {name}", "context": "The tab title on the same page.", "notes": "Keep {name}."},
  {"id": "rastrillo.ui.signin_continue_body", "section": "Sign-in screen: going to Keymail", "label": "Body", "text": "Taking you to Keymail to confirm it's you.", "context": "The one sentence on that page, above a link that does the same thing in case the page does not move on."},
  {"id": "gallery.screens.lead", "section": "Design-system gallery: Screens page", "label": "Page lead", "text": "The sign-in screens are the shipped signin partial, shown in its states. The last two are examples to copy.", "context": "The first line of the gallery's Screens page, for developers. Most screens are now the real component; the last two (a password screen and third-party buttons) are markup to copy.", "notes": "signin is a code name; keep it."},
  {"id": "gallery.screens.ask.name", "section": "Design-system gallery: Screens page", "label": "Screen name", "text": "Asking for an address", "context": "Name of the first sign-in screen shown: the empty form."},
  {"id": "gallery.screens.returning-keymail.name", "section": "Design-system gallery: Screens page", "label": "Screen name", "text": "Coming back with Keymail", "context": "Name of the screen offered to someone whose last sign-in here was Keymail."},
  {"id": "gallery.screens.returning-keymail.blurb", "section": "Design-system gallery: Screens page", "label": "Screen note", "text": "The browser remembers how it got in last time. One tap sends the remembered address, which is checked again from scratch.", "context": "The note under that screen, for developers deciding whether the remembered one-tap is safe."},
  {"id": "gallery.screens.returning-link.name", "section": "Design-system gallery: Screens page", "label": "Screen name", "text": "Coming back with an email link", "context": "Name of the screen for someone whose last sign-in here was an email link."},
  {"id": "gallery.screens.returning-link.blurb", "section": "Design-system gallery: Screens page", "label": "Screen note", "text": "The address is on the button, so there is nothing to type.", "context": "The note under that screen."},
  {"id": "gallery.screens.sent-unbound.name", "section": "Design-system gallery: Screens page", "label": "Screen name", "text": "After the link is sent, in another browser", "context": "Name of the link-sent screen when this browser did not send the address."},
  {"id": "gallery.screens.sent-unbound.blurb", "section": "Design-system gallery: Screens page", "label": "Screen note", "text": "When this browser did not send the address, the page does not guess it.", "context": "The note under that screen."},
  {"id": "gallery.screens.sent-instead.name", "section": "Design-system gallery: Screens page", "label": "Screen name", "text": "When Keymail was offered and a link went out", "context": "Name of the link-sent screen after a Continue to Keymail tap ended in an email link."},
  {"id": "gallery.screens.sent-instead.blurb", "section": "Design-system gallery: Screens page", "label": "Screen note", "text": "One line says a link was sent this time. It does not guess why.", "context": "The note under that screen."},
  {"id": "gallery.screens.continue.name", "section": "Design-system gallery: Screens page", "label": "Screen name", "text": "On the way to Keymail", "context": "Name of the page that moves on to Keymail by itself."},
  {"id": "gallery.screens.continue.blurb", "section": "Design-system gallery: Screens page", "label": "Screen note", "text": "The page moves on by itself, with a link in case it does not.", "context": "The note under that screen."},
  {"id": "gallery.screens.problem-address.name", "section": "Design-system gallery: Screens page", "label": "Screen name", "text": "An address that does not look right", "context": "Name of the screen showing the not-an-address error."},
  {"id": "gallery.screens.problem-address.blurb", "section": "Design-system gallery: Screens page", "label": "Screen note", "text": "The message belongs to the field, and that is where focus starts.", "context": "The note under that screen, for developers who care about screen readers."},
  {"id": "gallery.screens.problem-keymail.name", "section": "Design-system gallery: Screens page", "label": "Screen name", "text": "When Keymail could not confirm it", "context": "Name of the screen after a failed Keymail step."},
  {"id": "gallery.screens.problem-keymail.blurb", "section": "Design-system gallery: Screens page", "label": "Screen note", "text": "The button changes to send a link instead, so nobody is stuck.", "context": "The note under that screen."},
  {"id": "gallery.screens.password.warning", "section": "Design-system gallery: Screens page", "label": "Password warning", "text": "Rastrillo does not ship this screen. People reuse passwords, they leak, and you inherit the job of storing them safely. Use a link in your email plus a passkey, or passkeys on their own. The markup is here because some products still need it.", "context": "A warning box above the example password screen, under the title We do not recommend passwords."},
  {"id": "gallery.shells.stage.blurb", "section": "Design-system gallery: Shells page", "label": "Stage shell note", "text": "One card in the middle of a full-page backdrop, for a screen that stands alone. The sign-in screen is what it is for.", "context": "The note beside the new stage page frame on the gallery's Shells page."},
  {"id": "gallery.shells.demo.sentence", "section": "Design-system gallery: Shells page", "label": "Shell demo paragraph", "text": "This is the {shell} shell, one of the shells ui.Layout ships. A screen is a column: a page header, then a section heading and its card, then the next one. Everything you see here is the shell, tokens.css and two partials.", "context": "The paragraph in the middle of each full-page shell demo. It used to say one of the four; there are five now.", "notes": "Keep {shell}, ui.Layout and tokens.css."}
]
```

- [ ] **Step 2: Run the review**

Invoke the `copy-review` skill (Skill tool, `skill: "copy-review"`) and follow it exactly: launch `serve.sh copy-review/strings.json` with the Bash sandbox off (inside the sandbox it exits 3), give the operator the one URL it prints, and poll for `copy-review/result.json`. On `"action": "reroll"`, rewrite as the skill says — the operator's edited strings carried over unchanged, their demonstrated edits applied to the rest — and relaunch. Loop until `"action": "approve"`.

- [ ] **Step 3: Write the approved English**

Append to `locales/en.toml` one line per `rastrillo.ui.signin_*` id, in the index's order, `key = "<approved text>"`, byte for byte — no fixed typos, no changed capitalisation. If a string would break (a lost `{name}` or `{address}`), stop and ask the operator; never repair it silently.

- [ ] **Step 4: Translate, once**

Not reviewed — the operator reviews the source language only. Append the same 29 keys to each of `ga`, `zh-Hans`, `es`, `hi`, `pt`, `bn`, `ru`, `ja`, `yue`, `vi`, `ar`, each value your translation of the approved English, keeping every `{name}`/`{address}` and the file's register (its header says how it was drafted). "Keymail" is a product name and stays as written.

- [ ] **Step 5: Run the catalog gates**

Run: `GOFLAGS=-mod=mod go test . -count=1 -run 'BaseCatalog|IsBaseKey'`
Expected: PASS — one key set in all twelve, none empty.

- [ ] **Step 6: Run the gate and commit**

Run: `GOFLAGS=-mod=mod go vet ./... && gofmt -l . && GOFLAGS=-mod=mod go test ./... -count=1`

```bash
git add locales
git commit -F - <<'EOF'
Catalogs: the sign-in screen's strings, as approved in copy review

Every string the shipped sign-in screen shows went through the
operator's copy review before being written, as the spec requires; the
approved English is applied verbatim and the eleven translations are
drafted once, from it. Nothing renders these keys yet — the partial
lands next — so they change no page today.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
git push origin signin-screen
```

---

### Task 11: `ui` — the `stage` shell and the `stageArt` backdrop

**Files:**
- Create: `ui/layouts/stage.html`, `ui/stageart.go`, `ui/stageart_test.go`
- Modify: `ui/ui.go` (`layoutNames`, the package doc's shell paragraph, `Layout`'s doc), `ui/funcs.go` (register `stageArt`; doc), `ui/funcs_test.go` (11 → 12), `ui/ui_test.go` (`TestLayoutsParseAndRender`, `TestTheShellsKeepTheirOverridableBlockNames`), `ui/contrast_test.go` (the card edge over the art), `ui/tokens.css`
- Modify (gallery): `internal/designsystem/page.go` (the stage blurb, `shell-stage`'s height, the shell demo's "one of the four" sentence), `internal/designsystem/prose.go`
- Re-copy: `examples/blog/static/tokens.css`, `examples/tickets/static/tokens.css`

**Interfaces:**
- Consumes: the approved `gallery.shells.stage.blurb` and `gallery.shells.demo.sentence` in `copy-review/result.json` (Task 10); `fnv1a64(s string) uint64` (ui/colour.go:949), `parseHex`, `hexOf`, `ContrastRatio`, `themeTokens` (ui/contrast_test.go:154).
- Produces:
  - Template func `stageArt(seed string) template.HTML` — `<svg rst-stage-art aria-hidden="true" focusable="false" viewBox="0 0 1200 800" preserveAspectRatio="xMidYMid slice"><g rst-stage-art-glow>…</g><g rst-stage-art-lines transform="rotate(…)">…</g></svg>`.
  - `ui.LayoutNames()` → `["column", "topbar", "sidebar", "console", "stage"]`; `ui.Layout("stage")`.
  - Stage blocks, in source order: `lang`, `dir`, `title`, `head`, `backdrop`, `content`, `foot`.
  - Attributes: `rst-stage` (on `<body>`, or any wrapper), `rst-stage-scene`, `rst-stage-art`, `rst-stage-art-lines`, `rst-stage-art-glow`, `rst-stage-foot`.

- [ ] **Step 1: Write the failing tests**

`ui/stageart_test.go`:

```go
package ui

import (
	"regexp"
	"strings"
	"testing"
)

func TestStageArtIsDeterministicPerSeed(t *testing.T) {
	if stageArt("fichas") != stageArt("fichas") {
		t.Fatal("one seed drew two pictures; an app's backdrop must not change between loads")
	}
	if stageArt("fichas") == stageArt("harbour") {
		t.Fatal("two seeds drew the same picture; two apps should differ")
	}
	if stageArt("") != stageArt("") {
		t.Fatal("the empty seed is not constant")
	}
}

// The rendered output, not the template source: the inline-style test
// reads partials and layouts as source, and would never see what this
// function generates.
func TestStageArtIsSmallDecorativeAndPaintedOnlyByCSS(t *testing.T) {
	forbidden := regexp.MustCompile(`(?i)style|#[0-9a-f]{3,8}\b|rgba?\(|hsla?\(|url\(|href|https?:|<script|<image|<use`)
	for i := 0; i < 200; i++ {
		seed := "seed-" + strings.Repeat("x", i%7) + string(rune('a'+i%26))
		out := string(stageArt(seed))
		if len(out) >= 4096 {
			t.Errorf("seed %q: %d bytes, want under 4 KB", seed, len(out))
		}
		if m := forbidden.FindString(out); m != "" {
			t.Errorf("seed %q: output carries %q; colour comes from tokens.css so the art follows the theme and scheme", seed, m)
		}
		for _, want := range []string{`<svg rst-stage-art aria-hidden="true" focusable="false"`, "<g rst-stage-art-glow>", "<g rst-stage-art-lines "} {
			if !strings.Contains(out, want) {
				t.Errorf("seed %q: missing %s", seed, want)
			}
		}
	}
}

func TestStageShellCarriesTheBackdropSlot(t *testing.T) {
	src, ok := Layout("stage")
	if !ok {
		t.Fatal("no stage shell")
	}
	for _, want := range []string{
		`<body rst-stage>`,
		`<div rst-stage-scene aria-hidden="true">{{block "backdrop" .}}{{stageArt "rastrillo"}}{{end}}</div>`,
		`<main rst-page id="main">`,
	} {
		if !strings.Contains(string(src), want) {
			t.Errorf("layouts/stage.html lacks %s", want)
		}
	}
}
```

In `ui/ui_test.go`, `TestLayoutsParseAndRender`'s first line becomes:

```go
	if got := LayoutNames(); !reflect.DeepEqual(got, []string{"column", "topbar", "sidebar", "console", "stage"}) {
```

and `TestTheShellsKeepTheirOverridableBlockNames`' map gains:

```go
		// stage has no chrome to override. backdrop is the picture
		// behind the card, foot an optional line under it.
		"stage": {"lang", "dir", "title", "head", "backdrop", "content", "foot"},
```

In `ui/funcs_test.go`, both exact-set lists gain `"stageArt"` and both counts become 12.

Append to `ui/contrast_test.go`:

```go
// stageArtMix reads the percentage of accent tokens.css mixes into the
// background for one part of the stage art, so the check below measures
// what the stylesheet actually says rather than a copy of it.
func stageArtMix(t *testing.T, part string) float64 {
	t.Helper()
	re := regexp.MustCompile(`\[rst-stage-art-` + part + `\] \{[^}]*color-mix\(in srgb, var\(--rst-accent\) (\d+)%, var\(--rst-bg\)\)`)
	m := re.FindStringSubmatch(string(TokensCSS()))
	if m == nil {
		t.Fatalf("tokens.css has no color-mix(in srgb, var(--rst-accent) N%%, var(--rst-bg)) for [rst-stage-art-%s]", part)
	}
	p, _ := strconv.Atoi(m[1])
	return float64(p) / 100
}

// mixSRGB is CSS color-mix(in srgb, a p, b): a per-channel linear blend
// of the two sRGB values.
func mixSRGB(t *testing.T, a, b string, p float64) string {
	t.Helper()
	ar, ag, ab, err := parseHex(a)
	if err != nil {
		t.Fatal(err)
	}
	br, bg, bb, err := parseHex(b)
	if err != nil {
		t.Fatal(err)
	}
	mix := func(x, y uint8) uint8 { return uint8(math.Round(float64(x)*p + float64(y)*(1-p))) }
	return hexOf([3]uint8{mix(ar, br), mix(ag, bg), mix(ab, bb)})
}

// The sign-in card's border is its boundary against the stage art (WCAG
// 1.4.11). The art is drawn by mixing a little accent into the page
// background, so the border must clear 3:1 against the darkest (or, in
// dark schemes, lightest) thing the art paints — in every theme and
// scheme. If this fails, lower the art's percentage in tokens.css; never
// the floor.
func TestTheSigninCardStandsOutFromTheStageArt(t *testing.T) {
	for _, theme := range ThemeNames() {
		for _, scheme := range []string{"light", "dark"} {
			tok := themeTokens(t, theme)[scheme]
			for _, part := range []string{"lines", "glow"} {
				behind := mixSRGB(t, tok["--rst-accent"], tok["--rst-bg"], stageArtMix(t, part))
				ratio, err := ContrastRatio(tok["--rst-line-strong"], behind)
				if err != nil {
					t.Fatal(err)
				}
				if ratio < 3.0 {
					t.Errorf("%s/%s: the card border on the art's %s is %.2f:1, want >= 3:1", theme, scheme, part, ratio)
				}
			}
		}
	}
}
```

(add `"math"` and `"strconv"` to `contrast_test.go`'s imports).

- [ ] **Step 2: Run the tests to verify they fail**

Run: `GOFLAGS=-mod=mod go test ./ui/ -count=1 -run 'StageArt|StageShell|Layouts|Blocks|Funcs|SigninCard'`
Expected: FAIL — `undefined: stageArt`, `LayoutNames = [column topbar sidebar console]`.

- [ ] **Step 3: Write `stageArt`**

`ui/stageart.go`:

```go
package ui

import (
	"fmt"
	"html/template"
	"strings"
)

// stageArt draws the stage shell's default backdrop: fine wavy lines
// over a soft glow, in the spirit of keymail's sign-in page, so no app
// ships a blank page behind its sign-in card.
//
// It is deterministic in seed — line count, wavelength, amplitude,
// angle and the glow's position are picked from bounded ranges by a
// generator seeded from FNV-1a of the seed — so two apps differ and one
// app never changes between loads. The shell passes a constant seed,
// because no shell block may read the page's data; an app redefines the
// backdrop block as {{stageArt "its-name"}} for its own pattern, or with
// its own picture.
//
// The output is only numbers and fixed markup: the seed is never
// echoed, so returning template.HTML is safe. It carries no colour, no
// style and no reference to anything outside the page: tokens.css
// paints it through [rst-stage-art-lines] and [rst-stage-art-glow] with
// the theme's tokens, so it follows theme and scheme, and hides it
// under forced colours and in print.
func stageArt(seed string) template.HTML {
	r := splitmix{s: fnv1a64(seed)}
	lines := r.between(8, 12)
	half := r.between(100, 200) // half a wavelength, in viewBox units
	amp := r.between(12, 40)
	angle := r.between(-12, 12)
	cx, cy := r.between(240, 960), r.between(120, 480)

	var b strings.Builder
	b.WriteString(`<svg rst-stage-art aria-hidden="true" focusable="false" viewBox="0 0 1200 800" preserveAspectRatio="xMidYMid slice">`)
	// Concentric circles at low opacity rather than a gradient: a
	// gradient needs an id and a url() reference, and ids in an inline
	// SVG collide with the page's.
	b.WriteString(`<g rst-stage-art-glow>`)
	for i := 6; i >= 1; i-- {
		fmt.Fprintf(&b, `<circle cx="%d" cy="%d" r="%d"/>`, cx, cy, i*70)
	}
	b.WriteString(`</g>`)
	fmt.Fprintf(&b, `<g rst-stage-art-lines transform="rotate(%d 600 400)">`, angle)
	gap := 800 / (lines + 1)
	for i := 1; i <= lines; i++ {
		// One quadratic curve, then smooth continuations: each "t"
		// reflects the last control point, which is what makes a wave.
		// Started a wavelength off the left edge so the rotation never
		// shows a line's end.
		fmt.Fprintf(&b, `<path d="M%d %dq%d %d %d 0`, -2*half, i*gap, half/2, -amp, half)
		for x := -half; x < 1400; x += half {
			fmt.Fprintf(&b, "t%d 0", half)
		}
		b.WriteString(`"/>`)
	}
	b.WriteString(`</g></svg>`)
	return template.HTML(b.String())
}

// splitmix is SplitMix64: a few lines, no allocation, and a good spread
// from a 64-bit hash — all a picture needs. Not math/rand, whose
// generators are not promised to stay the same across Go releases,
// which would change every app's backdrop on an upgrade.
type splitmix struct{ s uint64 }

func (m *splitmix) next() uint64 {
	m.s += 0x9e3779b97f4a7c15
	z := m.s
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

// between is uniform over [lo, hi].
func (m *splitmix) between(lo, hi int) int {
	return lo + int(m.next()%uint64(hi-lo+1))
}
```

`ui/funcs.go` — `Funcs`' map gains `"stageArt": stageArt,`; the doc comment gains: "stageArt draws the stage shell's default backdrop from a seed (see its own comment)." and "these eleven" becomes "these twelve".

- [ ] **Step 4: Write the stage shell**

`ui/layouts/stage.html`:

```html
{{define "layout"}}<!doctype html>
<html lang="{{block "lang" .}}en{{end}}" dir="{{block "dir" .}}ltr{{end}}">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width">
<title>{{block "title" .}}Hello{{end}}</title>
<link rel="stylesheet" href="{{asset "static/tokens.css"}}">
<link rel="stylesheet" href="{{asset "static/theme.css"}}">
<script defer src="{{asset "static/rastrillo.js"}}"></script>
{{iconAssets}}
{{/* stage — one card centred on a full-page backdrop, for a screen that
     stands alone: the sign-in page above all. No select.js, calendar.js
     or datetime.js: a stage page has no enhanced fields, and each would
     be a request in front of the one screen every visitor meets first.
     head is the app's slot, last so its CSS wins the ties it should. */}}
{{block "head" .}}{{end}}
</head>
<body rst-stage>
<a rst-skip href="#main">{{T "rastrillo.ui.shell_skip"}}</a>
{{/* backdrop is the picture behind the card. The default is generated
     from a constant seed because no block may read the page's data;
     redefine it as {{stageArt "your-app"}} for a pattern of your own,
     or with an <img> or your own SVG. aria-hidden: it is decoration. */}}
<div rst-stage-scene aria-hidden="true">{{block "backdrop" .}}{{stageArt "rastrillo"}}{{end}}</div>
<main rst-page id="main">
{{template "content" .}}
</main>
{{/* foot is empty by default. Define it as
     <footer rst-stage-foot>…</footer> for legal links — the element is
     yours so an empty default leaves no empty landmark behind. */}}
{{block "foot" .}}{{end}}
</body>
</html>
{{end}}
```

`ui/ui.go`:

```go
var layoutNames = []string{"column", "topbar", "sidebar", "console", "stage"}
```

Update the `layoutNames` comment ("…the three chrome shells are the ones an app opts into, and stage is the one-card page a sign-in screen sits in"); in `Layout`'s doc replace "title, lang, dir and head in all four" with "title, lang, dir and head in all five" and add "stage has backdrop — the picture behind its card — and foot instead of chrome"; in the package doc replace "All four carry rst-skip" with "All five carry rst-skip" and "Layout ships the four shells" with "Layout ships the five shells".

- [ ] **Step 5: Style the stage**

`ui/tokens.css` — after the `rst-signin` section from Task 10:

```css
/* ── rst-stage: the stage shell (layouts/stage.html) ─────────────────
   One card centred on a full-page backdrop. [rst-stage] is <body> in
   the shell and a plain wrapper in the design-system gallery's frames,
   so nothing here assumes which element carries it. The scene is
   absolutely positioned inside it rather than fixed, so a page taller
   than the window scrolls the picture with it instead of sliding the
   card over a still image. */
.rst-stage, [rst-stage] { box-sizing: border-box; display: grid; grid-template-rows: 1fr auto; isolation: isolate; min-block-size: 100dvh; position: relative; }
.rst-stage > .rst-page, [rst-stage] > [rst-page] { align-self: center; box-sizing: border-box; display: flex; justify-content: center; max-width: none; position: relative; width: 100%; z-index: 1; }
.rst-stage__scene, [rst-stage-scene] { inset: 0; overflow: hidden; pointer-events: none; position: absolute; z-index: 0; }
.rst-stage__scene > *, [rst-stage-scene] > * { block-size: 100%; display: block; inline-size: 100%; object-fit: cover; }
.rst-stage__foot, [rst-stage-foot] { color: var(--rst-text-muted); padding: var(--rst-sp-4); position: relative; text-align: center; z-index: 1; }
/* The art's two colours: a little accent mixed into the page
   background, faint enough that the sign-in card's border still clears
   3:1 against it in every theme and scheme — ui/contrast_test.go reads
   these two percentages and checks exactly that. Lower them rather than
   loosen the test. */
.rst-stage-art__lines, [rst-stage-art-lines] { fill: none; stroke: color-mix(in srgb, var(--rst-accent) 10%, var(--rst-bg)); stroke-width: 1.5; }
.rst-stage-art__glow, [rst-stage-art-glow] { fill: color-mix(in srgb, var(--rst-accent) 8%, var(--rst-bg)); fill-opacity: 0.2; }
/* Forced colours would paint the lines in the system's text colour at
   full strength, and a printout does not need a picture of waves. */
@media (forced-colors: active) {
  .rst-stage-art, [rst-stage-art] { display: none; }
}
@media print {
  .rst-stage-art, [rst-stage-art] { display: none; }
}
```

Re-copy: `cp ui/tokens.css examples/blog/static/tokens.css && cp ui/tokens.css examples/tickets/static/tokens.css`.

- [ ] **Step 6: Run the tests to verify they pass**

Run: `GOFLAGS=-mod=mod go test ./ui/ -count=1 && (cd examples/blog && GOFLAGS=-mod=mod go test ./...) && (cd examples/tickets && GOFLAGS=-mod=mod go test ./...)`
Expected: PASS — including `TestEveryEmbeddedThemeAndLayoutIsNamed`, `TestLayoutClassesAreStyled`, `TestEveryShellLeavesTheAppASlotInTheHead`, `TestPartialsAndLayoutsEmitNoInlineStyles`, the twin tests and `TestTokensCSSHasNoColourLiterals`. If `TestTheSigninCardStandsOutFromTheStageArt` fails for a theme, lower that part's percentage in the rule and re-copy; the measured worst case at 10% lines / 8% glow across day, plain and signal is 3.17:1.

- [ ] **Step 7: Give the gallery its fifth shell**

`internal/designsystem` renders every shell in `ui.LayoutNames()`, so `stage` joins its Shells page now, and its words have to arrive with it: a shell with no blurb, and a demo sentence that still says "one of the four", would be this task shipping a wrong page. Use the approved text from `copy-review/result.json` (Task 10) for `gallery.shells.stage.blurb` and `gallery.shells.demo.sentence`, byte for byte; the drafts are shown here.

In `internal/designsystem/page.go`: add `"shell-stage": 780,` to `previewHeights` beside the other shells (Task 13's browser run measures it); add to `shellViews`' `blurbs` map

```go
		"stage":   "One card in the middle of a full-page backdrop, for a screen that stands alone. The sign-in screen is what it is for.",
```

and change `shellTemplate`'s sentence to `{{P "This is the {shell} shell, one of the shells ui.Layout ships. A screen is a column: a page header, then a section heading and its card, then the next one. Everything you see here is the shell, tokens.css and two partials." "shell" .Name}}`. Until Task 12 gives it the sign-in card, the stage demo frames the same generic content as the others, which that sentence describes truthfully.

In `internal/designsystem/prose.go`: delete the row keyed by the old sentence (`This is the {shell} shell, one of the four ui.Layout ships. …`) — `TestEveryProseKeyIsTranslated` fails on a stale row — and add one row for each of the two approved strings, with all eleven translations, in the register the file's header sets, identifiers untranslated and `{shell}` kept. The shape (each `…` is your translation of the key into that language):

```go
	`One card in the middle of a full-page backdrop, for a screen that stands alone. The sign-in screen is what it is for.`: {
		`ga`:      `…`,
		`zh-Hans`: `…`,
		`es`:      `…`,
		`hi`:      `…`,
		`pt`:      `…`,
		`bn`:      `…`,
		`ru`:      `…`,
		`ja`:      `…`,
		`yue`:     `…`,
		`vi`:      `…`,
		`ar`:      `…`,
	},
```

In `internal/designsystem/designsystem.go`'s package comment, "a full-page demo of each of the four shells" becomes "…of each of the five shells".

Run: `GOFLAGS=-mod=mod go test ./internal/designsystem/ -count=1`
Expected: PASS — `TestEveryProseKeyIsTranslated` (no missing, no stale row), `TestNoEnglishProseReachesATranslatedPage`, `TestTreeShapeIsComplete` (`shells/stage.html` in every theme × locale), `TestEveryPageIsAWholeLocalisedDocument`.

- [ ] **Step 8: Run the gate and commit**

Run: `GOFLAGS=-mod=mod go vet ./... && gofmt -l . && GOFLAGS=-mod=mod go test ./... -count=1`

```bash
git add ui internal/designsystem examples/blog/static/tokens.css examples/tickets/static/tokens.css
git commit -F - <<'EOF'
ui: the stage shell and its generated backdrop

A sign-in card needs a page of its own that is not a column of app
chrome, and no app should ship a blank page behind it. stage is that
page: one centred card over a backdrop block, whose default is a
seeded, token-coloured SVG. It is generated from numbers only, carries
no colour or style of its own, and follows theme and scheme through
tokens.css; a test holds the card's border at 3:1 against it in every
theme and scheme, because the art mixes accent into the background
that border was measured against.

The gallery's Shells page gains the stage with its copy-reviewed blurb,
and its shell demos stop saying there are four.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
```

---
### Task 12: The `signin` partials, their CSS, and the gallery's Screens page built on them

One task, because the gallery refuses a partial no page claims (`buildFamilies`, internal/designsystem/page.go:990-1014) and requires every partial's marker on some page (`TestEveryPartialAppearsAcrossThePages`): the partials and the page that documents them have to land together for the gate to stay green.

**Files:**
- Create: `ui/partials/signin.html`, `ui/signin_test.go`, `auth/signinpage_test.go`
- Modify: `ui/tokens.css`, `ui/ui_test.go` (`allPartials`, `TestAllPartialsAreDefined`), `internal/designsystem/screens.go` (the docs table and `buildScreens`), `internal/designsystem/page.go` (`renderGallery` parses the frame; `buildFamilies`' orphan sweep; `previewHeights`; `srcdoc`'s padding rule; `renderShell` + `shellData` + a stage override template), `internal/designsystem/prose.go`, `internal/designsystem/designsystem_test.go`
- Re-copy: `examples/blog/static/tokens.css`, `examples/tickets/static/tokens.css`

**Interfaces:**
- Consumes: `auth.SigninState`, `Door()`, `Focus()`, `auth.Step*`, `auth.Problem*`, `auth.Remembered`, `auth.PasskeyDoor` with `ScriptURL` (Task 6); `opt`, `Tbdi`, `field`'s `QuietError`, `callout`'s `ID`/`Focus` (Task 8); the door markup contract of `passkey/js/signin.mjs` (Task 9); the `rastrillo.ui.signin_*` keys (Task 10); the approved `gallery.screens.*` strings in `copy-review/result.json` (Task 10); `stageArt`, the `rst-stage` CSS, `ui.Layout("stage")` (Task 11); existing `newPreview`, `previewTitle`, `marker`, `proseIn`, `anchorID`, `galleryFuncs`, `renderShell`, `shellData`.
- Produces:
  - `{{template "signin" (dict "State" <auth.SigninState> "Brand" <dict|struct with Name, optional Pitch, Mark> ["Preview" true])}}` and `{{template "signin-title" (dict "State" … "Brand" …)}}`.
  - Element ids the browser drive (Task 13) and the gallery rely on: `rst-signin-heading`, `rst-signin-email`, `rst-signin-problem`, `rst-signin-remembered`, `rst-signin-passkey-msg`.
  - Attributes: `rst-signin`, `rst-signin-brand`, `rst-signin-mark`, `rst-signin-name`, `rst-signin-pitch`, `rst-signin-door`, `rst-signin-form`, `rst-signin-remembered`, `rst-signin-passkey-msg`.
  - The screen frame template `ds-screen-stage` and `var screenPartials = []string{"signin", "signin-title"}` in `internal/designsystem`; screen keys `signin-ask`, `signin-returning-keymail`, `signin-returning-link`, `signin-returning-passkey`, `signin-sent`, `signin-sent-unbound`, `signin-sent-instead`, `signin-continue`, `signin-problem-address`, `signin-problem-keymail`, `signin-social`, `signin-password` (anchor ids `screen-<key>`), used by Task 13.

**Gallery English below is the draft that went to review.** Wherever `copy-review/result.json` holds an approved `text` for the matching `gallery.screens.*` id that differs, write the approved text, byte for byte: in `internal/designsystem` the English is the prose key, so what you write in Go is also what `prose.go` is keyed by. The ids map as: `gallery.screens.lead` → `screensBody`'s lead; `gallery.screens.<key>.name`/`.blurb` → the `screenDoc` with `Key: "signin-<key>"`; `gallery.screens.password.warning` → the password screen's `WarningBody`.

- [ ] **Step 1: Write the partial's failing tests**

`ui/signin_test.go`:

```go
package ui

import (
	"html"
	"regexp"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/auth"
)

const keymailURL = "https://keymail.test/oauth/authorize?client_id=https%3A%2F%2Fapp.test&code_challenge=cccc&code_challenge_method=S256&redirect_uri=https%3A%2F%2Fapp.test%2Fauth%2Fcallback&scope=identify&state=ssss"

var signinDoor = &auth.PasskeyDoor{BeginPath: "/passkey/discover/begin", FinishPath: "/passkey/discover/finish", ModuleURL: "/static/webauthn.mjs", ScriptURL: "/static/passkey-signin.mjs"}

func signinData(st auth.SigninState) map[string]any {
	if st.BeginPath == "" {
		st.BeginPath = "/signin"
	}
	if st.ForgetPath == "" {
		st.ForgetPath = "/signin/forget"
	}
	return map[string]any{"State": st, "Brand": map[string]any{"Name": "Harbour"}}
}

func remembered(method, address string) *auth.Remembered {
	return &auth.Remembered{Method: method, Address: address}
}

// signinStates is every step × problem × remembered method the screen
// can be in, named, with the element focus must start on (§2's matrix):
// "field", "callout", "onetap" or "".
func signinStates() []struct {
	name  string
	st    auth.SigninState
	focus string
} {
	kay := remembered("keymail", "kay@example.org")
	return []struct {
		name  string
		st    auth.SigninState
		focus string
	}{
		{"ask", auth.SigninState{Step: auth.StepAsk}, "field"},
		{"ask with a passkey door", auth.SigninState{Step: auth.StepAsk, Passkey: signinDoor}, "field"},
		{"returning keymail", auth.SigninState{Step: auth.StepReturning, Remembered: kay, Address: "kay@example.org"}, "onetap"},
		{"returning magic link", auth.SigninState{Step: auth.StepReturning, Remembered: remembered("magiclink", "ada@example.com")}, "onetap"},
		{"returning passkey", auth.SigninState{Step: auth.StepReturning, Remembered: remembered("passkey", ""), Passkey: signinDoor}, ""},
		{"returning passkey, no door", auth.SigninState{Step: auth.StepReturning, Remembered: remembered("passkey", "")}, "field"},
		{"sent, bound", auth.SigninState{Step: auth.StepSent, SentTo: "ada@example.com"}, ""},
		{"sent, unbound", auth.SigninState{Step: auth.StepSent}, ""},
		{"sent instead of keymail", auth.SigninState{Step: auth.StepSent, SentTo: "kay@example.org", SentInstead: true}, ""},
		{"continue", auth.SigninState{Step: auth.StepContinue, ContinueURL: keymailURL}, ""},
		{"rate", auth.SigninState{Step: auth.StepAsk, Problem: auth.ProblemRate, Address: "ada@example.com"}, "callout"},
		{"address", auth.SigninState{Step: auth.StepAsk, Problem: auth.ProblemAddress, Address: "ada@example"}, "field"},
		{"expired", auth.SigninState{Step: auth.StepAsk, Problem: auth.ProblemExpired}, "callout"},
		{"keymail", auth.SigninState{Step: auth.StepAsk, Problem: auth.ProblemKeymail, Address: "kay@example.org"}, "callout"},
		{"generic", auth.SigninState{Step: auth.StepAsk, Problem: auth.ProblemGeneric}, "callout"},
		{"reauth, ask", auth.SigninState{Step: auth.StepAsk, Problem: auth.ProblemReauth}, "field"},
		{"reauth, returning keymail", auth.SigninState{Step: auth.StepReturning, Problem: auth.ProblemReauth, Remembered: kay}, "onetap"},
	}
}

var (
	h1Pattern        = regexp.MustCompile(`(?s)<h1 id="rst-signin-heading">(.*?)</h1>`)
	autofocusPattern = regexp.MustCompile(`<[a-z]+\b[^>]*\sautofocus[\s>][^>]*>?`)
	tagText          = regexp.MustCompile(`<[^>]*>`)
)

func text(s string) string { return strings.TrimSpace(html.UnescapeString(tagText.ReplaceAllString(s, ""))) }

func TestSigninHeadingAndTitleFollowTheState(t *testing.T) {
	name := "Harbour"
	for _, c := range signinStates() {
		out := render(t, "signin", signinData(c.st))
		m := h1Pattern.FindAllStringSubmatch(out, -1)
		if len(m) != 1 {
			t.Errorf("%s: %d <h1>s, want exactly one", c.name, len(m))
			continue
		}
		var heading, title string
		switch {
		case c.st.Step == auth.StepSent:
			heading, title = defaultT("rastrillo.ui.signin_sent_heading"), defaultTf("rastrillo.ui.signin_sent_title", "name", name)
		case c.st.Step == auth.StepContinue:
			heading, title = defaultT("rastrillo.ui.signin_continue_heading"), defaultTf("rastrillo.ui.signin_continue_title", "name", name)
		case c.st.Problem != auth.ProblemNone && c.st.Problem != auth.ProblemReauth:
			heading, title = defaultTf("rastrillo.ui.signin_heading", "name", name), defaultTf("rastrillo.ui.signin_problem_title", "name", name)
		default:
			heading, title = defaultTf("rastrillo.ui.signin_heading", "name", name), defaultTf("rastrillo.ui.signin_title", "name", name)
		}
		if got := text(m[0][1]); got != heading {
			t.Errorf("%s: <h1> %q, want %q", c.name, got, heading)
		}
		if got := text(render(t, "signin-title", signinData(c.st))); got != title {
			t.Errorf("%s: title %q, want %q", c.name, got, title)
		}
	}
}

func TestSigninFocusFollowsTheMatrix(t *testing.T) {
	for _, c := range signinStates() {
		out := render(t, "signin", signinData(c.st))
		tags := autofocusPattern.FindAllString(out, -1)
		if len(tags) > 1 {
			t.Errorf("%s: %d autofocus attributes; at most one element may ask for focus", c.name, len(tags))
			continue
		}
		var got string
		if len(tags) == 1 {
			switch tag := tags[0]; {
			case strings.Contains(tag, `id="rst-signin-email"`):
				got = "field"
			case strings.Contains(tag, `id="rst-signin-problem"`) && strings.Contains(tag, `tabindex="-1"`):
				got = "callout"
			case strings.HasPrefix(tag, "<button"):
				got = "onetap"
			default:
				got = "unexpected " + tag
			}
		}
		if got != c.focus {
			t.Errorf("%s: focus starts on %q, want %q", c.name, got, c.focus)
		}
	}
}

// A message present at load that focus lands on is announced by the
// focus; role="alert" would race it (§2). The only live region on the
// screen is the passkey message.
func TestSigninNeverUsesRoleAlert(t *testing.T) {
	for _, c := range signinStates() {
		if out := render(t, "signin", signinData(c.st)); strings.Contains(out, `role="alert"`) {
			t.Errorf("%s renders role=alert", c.name)
		}
	}
}

func TestTheOneTapNamesTheRememberedMethod(t *testing.T) {
	byName := map[string]auth.SigninState{}
	for _, c := range signinStates() {
		byName[c.name] = c.st
	}
	km := render(t, "signin", signinData(byName["returning keymail"]))
	for _, want := range []string{
		`<input type="hidden" name="address" value="kay@example.org">`,
		`<input type="hidden" name="expect" value="keymail">`,
		`aria-describedby="rst-signin-remembered"`,
		`id="rst-signin-remembered"`,
		`<bdi>kay@example.org</bdi>`,
		html.EscapeString(defaultT("rastrillo.ui.signin_to_keymail")),
	} {
		if !strings.Contains(km, want) {
			t.Errorf("keymail one-tap lacks %s:\n%s", want, km)
		}
	}

	ml := render(t, "signin", signinData(byName["returning magic link"]))
	button := regexp.MustCompile(`(?s)<button rst-btn="primary block" type="submit"[^>]*>(.*?)</button>`).FindStringSubmatch(ml)
	if button == nil || !strings.Contains(button[1], "<bdi>ada@example.com</bdi>") || strings.Contains(button[0], "aria-describedby") {
		t.Errorf("the magic-link one-tap must carry the address in its label and nothing else: %v", button)
	}
	if strings.Contains(ml, `name="expect"`) {
		t.Error("the magic-link one-tap posts expect; only the Keymail one does")
	}

	pk := render(t, "signin", signinData(byName["returning passkey"]))
	if !strings.Contains(pk, "data-rst-passkey ") || !strings.Contains(pk, html.EscapeString(defaultT("rastrillo.ui.signin_passkey_remembered"))) {
		t.Errorf("the passkey door is missing or mislabelled:\n%s", pk)
	}
	if !strings.Contains(pk, `id="rst-signin-email"`) {
		t.Error("the passkey door is on its own; the email form must be beside it (§1.6 step 6)")
	}
}

// Review Focus 2.
func TestAReturningPasskeyWithNoDoorAsksForAnAddress(t *testing.T) {
	out := render(t, "signin", signinData(auth.SigninState{Step: auth.StepReturning, Remembered: remembered("passkey", "")}))
	if strings.Contains(out, "data-rst-passkey") || !strings.Contains(out, `id="rst-signin-email"`) {
		t.Fatalf("a remembered passkey with no door wired must be the email form:\n%s", out)
	}
}

func TestTheKeymailProblemOffersTheEscapeHatch(t *testing.T) {
	for _, address := range []string{"kay@example.org", ""} {
		out := render(t, "signin", signinData(auth.SigninState{Step: auth.StepAsk, Problem: auth.ProblemKeymail, Address: address}))
		if !strings.Contains(out, `<input type="hidden" name="force" value="1">`) ||
			!strings.Contains(out, html.EscapeString(defaultT("rastrillo.ui.signin_send_link_instead"))) {
			t.Errorf("address %q: no escape hatch:\n%s", address, out)
		}
	}
}

func TestContinueIsANavigationNotAForm(t *testing.T) {
	st := auth.SigninState{Step: auth.StepContinue, ContinueURL: keymailURL}
	out := render(t, "signin", signinData(st))
	if strings.Contains(out, "<form") {
		t.Error("Continue renders a form; it has no controls but its link")
	}
	meta := regexp.MustCompile(`<meta http-equiv="refresh" content="0;url=([^"]*)">`).FindStringSubmatch(out)
	link := regexp.MustCompile(`<a rst-btn="primary block" href="([^"]*)">`).FindStringSubmatch(out)
	if meta == nil || html.UnescapeString(meta[1]) != keymailURL {
		t.Errorf("meta refresh %v, want a zero-delay refresh to the continuation", meta)
	}
	if link == nil || html.UnescapeString(link[1]) != keymailURL {
		t.Errorf("fallback link %v, want the same URL", link)
	}

	data := signinData(st)
	data["Preview"] = true
	preview := render(t, "signin", data)
	if strings.Contains(preview, "http-equiv") || !strings.Contains(preview, `href="#"`) || strings.Contains(preview, "keymail.test") {
		t.Errorf("a Preview must not navigate anywhere:\n%s", preview)
	}
}

func TestSentNamesOnlyABoundAddress(t *testing.T) {
	bound := render(t, "signin", signinData(auth.SigninState{Step: auth.StepSent, SentTo: "ada@example.com"}))
	if !strings.Contains(bound, "<bdi>ada@example.com</bdi>") {
		t.Errorf("bound Sent does not name the address:\n%s", bound)
	}
	unbound := render(t, "signin", signinData(auth.SigninState{Step: auth.StepSent}))
	if strings.Contains(unbound, "@") || !strings.Contains(unbound, html.EscapeString(defaultT("rastrillo.ui.signin_sent_inbox"))) {
		t.Errorf("unbound Sent must say your inbox and no address:\n%s", unbound)
	}
	instead := render(t, "signin", signinData(auth.SigninState{Step: auth.StepSent, SentTo: "kay@example.org", SentInstead: true}))
	i, j := strings.Index(instead, html.EscapeString(defaultT("rastrillo.ui.signin_sent_instead"))), strings.Index(instead, "<bdi>kay@example.org</bdi>")
	if i < 0 || j < 0 || i > j {
		t.Errorf("the one-time line must come first, before where the link went:\n%s", instead)
	}
}

// Review Focus 1.
func TestSigninEscapesWhatAVisitorTyped(t *testing.T) {
	hostile := `"><script>alert(1)</script>{address}`
	for name, st := range map[string]auth.SigninState{
		"a prefill":      {Step: auth.StepAsk, Problem: auth.ProblemAddress, Address: hostile},
		"a Sent address": {Step: auth.StepSent, SentTo: hostile},
		"a one-tap":      {Step: auth.StepReturning, Remembered: remembered("magiclink", hostile)},
	} {
		out := render(t, "signin", signinData(st))
		if strings.Contains(out, "<script>") {
			t.Errorf("%s: markup a visitor typed reached the page:\n%s", name, out)
		}
	}
	rtl := render(t, "signin", signinData(auth.SigninState{Step: auth.StepSent, SentTo: "مرحبا@example.com"}))
	if !strings.Contains(rtl, "<bdi>مرحبا@example.com</bdi>") {
		t.Errorf("an RTL address is not isolated:\n%s", rtl)
	}
}

func TestThePasskeyButtonWaitsForItsScript(t *testing.T) {
	st := auth.SigninState{Step: auth.StepAsk, Passkey: &auth.PasskeyDoor{BeginPath: "/b", FinishPath: "/f", ModuleURL: "/m.mjs", ScriptURL: "/door.mjs", LegacyRPID: "old.example"}}
	out := render(t, "signin", signinData(st))
	for _, want := range []string{
		" hidden", `data-rst-passkey-begin="/b"`, `data-rst-passkey-finish="/f"`, `data-rst-passkey-module="/m.mjs"`,
		`<script type="module" src="/door.mjs"></script>`,
		`data-rst-passkey-legacy-rpid="old.example"`, `aria-describedby="rst-signin-passkey-msg"`,
		`id="rst-signin-passkey-msg"`, `aria-live="polite"`,
		`data-rst-passkey-cancelled="` + html.EscapeString(defaultT("rastrillo.ui.signin_passkey_cancelled")) + `"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the passkey door lacks %s:\n%s", want, out)
		}
	}
	data := signinData(st)
	data["Preview"] = true
	if preview := render(t, "signin", data); strings.Contains(preview, " hidden") || strings.Contains(preview, "data-rst-passkey") || strings.Contains(preview, "<script") {
		t.Errorf("a Preview shows the door as the enhanced page would, loading nothing and with nothing for a script to find:\n%s", preview)
	}
}

// The door's script is the one request the screen adds, and only a page
// that renders the door makes it: an email-only screen, a Sent or
// Continue page, a one-tap for keymail or a link, and every Preview
// load nothing. ui's shells never name it either.
func TestOnlyAPasskeyDoorLoadsItsScript(t *testing.T) {
	for _, c := range signinStates() {
		out := render(t, "signin", signinData(c.st))
		n := strings.Count(out, "<script")
		door := c.st.Passkey != nil && (c.st.Door() == "ask" || c.st.Door() == "passkey")
		switch {
		case door && (n != 1 || !strings.Contains(out, `<script type="module" src="/static/passkey-signin.mjs"></script>`)):
			t.Errorf("%s renders a passkey door and %d scripts; want exactly its module", c.name, n)
		case !door && n != 0:
			t.Errorf("%s renders no passkey door but loads %d scripts", c.name, n)
		}
		data := signinData(c.st)
		data["Preview"] = true
		if strings.Contains(render(t, "signin", data), "<script") {
			t.Errorf("%s: a Preview loads a script", c.name)
		}
	}
	for _, name := range LayoutNames() {
		src, _ := Layout(name)
		if strings.Contains(string(src), "passkey") {
			t.Errorf("layouts/%s.html names the passkey door; only the partial may load it", name)
		}
	}
}

func TestSigninControlsAreNamedAndIDsUnique(t *testing.T) {
	inputs := regexp.MustCompile(`<input\b[^>]*>`)
	buttons := regexp.MustCompile(`(?s)<button\b[^>]*>(.*?)</button>`)
	for _, c := range signinStates() {
		out := render(t, "signin", signinData(c.st))
		for _, in := range inputs.FindAllString(out, -1) {
			if strings.Contains(in, `type="hidden"`) {
				continue
			}
			id := regexp.MustCompile(`id="([^"]*)"`).FindStringSubmatch(in)
			if id == nil || !strings.Contains(out, `for="`+id[1]+`"`) {
				t.Errorf("%s: an input with no label: %s", c.name, in)
			}
		}
		for _, b := range buttons.FindAllStringSubmatch(out, -1) {
			if text(b[1]) == "" {
				t.Errorf("%s: a button with no text: %s", c.name, b[0])
			}
		}
		seen := map[string]bool{}
		for _, m := range idAttr.FindAllStringSubmatch(out, -1) {
			if seen[m[1]] {
				t.Errorf("%s: id %q twice", c.name, m[1])
			}
			seen[m[1]] = true
		}
	}
}

func TestSigninClassesAreStyled(t *testing.T) {
	css := string(TokensCSS())
	for _, c := range signinStates() {
		for name := range rstVocabulary(render(t, "signin", signinData(c.st))) {
			if !qualifiedOnly[name] && !tokensStyle(css, name) {
				t.Errorf("%s: tokens.css has no selector for %q", c.name, name)
			}
		}
	}
}

func TestSigninTakesABrandStruct(t *testing.T) {
	type brand struct{ Name string }
	out := render(t, "signin", map[string]any{"State": auth.SigninState{Step: auth.StepAsk, BeginPath: "/signin"}, "Brand": brand{Name: "Harbour"}})
	if !strings.Contains(out, "Harbour") {
		t.Fatalf("a Brand struct with only Name must render:\n%s", out)
	}
}
```

In `ui/ui_test.go`: add to `allPartials()` (at the end of the slice):

```go
		{"signin", map[string]any{
			"State": auth.SigninState{Step: auth.StepAsk, BeginPath: "/signin", ForgetPath: "/signin/forget", Address: "grace@example.com"},
			"Brand": map[string]any{
				"Name": "Harbour", "Pitch": "Moorings and berths, booked in a minute.",
				"Mark": template.HTML(`<svg viewBox="0 0 24 24" aria-hidden="true" focusable="false"><circle cx="12" cy="12" r="9"/></svg>`),
			},
		}},
		{"signin-title", map[string]any{"State": auth.SigninState{Step: auth.StepSent}, "Brand": map[string]any{"Name": "Harbour"}}},
```

(import `"amadan.net/rastrillo/rastrillo/auth"`), and in `TestAllPartialsAreDefined` append `"signin", "signin-title"` to `want` and change the count check to 36 ("the shipped set is 36 partials").

- [ ] **Step 2: Write the gallery's failing test**

Append to `internal/designsystem/designsystem_test.go`:

```go
// The sign-in screens are the shipped partial, not hand-written markup:
// a gallery screen that is not what the framework renders teaches the
// wrong thing. Each state's frame must carry the partial's own markers,
// and the hand-written screens that stay are the two ui does not ship.
func TestTheSigninScreensAreThePartial(t *testing.T) {
	files := render(t)
	page := string(files[RootTheme()+"/en/"+fileOf("screens")])
	for _, key := range []string{
		"signin-ask", "signin-returning-keymail", "signin-returning-link", "signin-returning-passkey",
		"signin-sent", "signin-sent-unbound", "signin-sent-instead", "signin-continue",
		"signin-problem-address", "signin-problem-keymail",
	} {
		i := strings.Index(page, `id="`+anchorID("screen", key)+`"`)
		if i < 0 {
			t.Errorf("no screen %s on the Screens page", key)
			continue
		}
		section := page[i:]
		if end := strings.Index(section[1:], `<article class="ds-partial"`); end > 0 {
			section = section[:end+1]
		}
		if !strings.Contains(section, "rst-signin") || !strings.Contains(section, "rst-stage") {
			t.Errorf("screen %s is not the signin partial in a stage frame", key)
		}
	}
	// Previews load nothing: the door's module is for a live page.
	for name, body := range files {
		if strings.Contains(string(body), "passkey-signin.mjs") {
			t.Errorf("%s names the passkey door's module; a Preview must not load it", name)
		}
	}
	for _, gone := range []string{"/signin/other", "/signin/reset"} {
		if strings.Contains(page, gone) {
			t.Errorf("the Screens page still links %s, which nothing serves", gone)
		}
	}
	if !strings.Contains(page, anchorID("screen", "signin-password")) || !strings.Contains(page, anchorID("screen", "signin-social")) {
		t.Error("the two screens ui does not ship (password, social) are gone; they stay as examples to copy")
	}
}
```

- [ ] **Step 3: Write the rendered-page parity test**

`auth/signinpage_test.go` — the admission-wrapper parity of Task 6, carried to the rendered page (spec §5: "the same rendered Sent page"):

```go
package auth

import (
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/ui"
)

// renderSignin is the page body and title an app renders for st.
func renderSignin(t *testing.T, st SigninState) string {
	t.Helper()
	tmpl := template.Must(template.New("").Funcs(ui.Funcs()).ParseFS(ui.Templates(), "*.html"))
	var b strings.Builder
	data := map[string]any{"State": st, "Brand": map[string]any{"Name": "Harbour"}}
	for _, name := range []string{"signin-title", "signin"} {
		if err := tmpl.ExecuteTemplate(&b, name, data); err != nil {
			t.Fatal(err)
		}
	}
	return b.String()
}

// An address an admission wrapper refused with AnswerAsSent and one
// Begin sent a link to must render byte-identical pages, in both modes
// and with or without the Keymail one-tap's expect.
func TestAnAdmissionWrapperRendersTheSameSentPage(t *testing.T) {
	for _, screen := range []bool{false, true} {
		for _, expect := range []string{"", "keymail"} {
			t.Run("SigninScreen="+strconv.FormatBool(screen)+" expect="+expect, func(t *testing.T) {
				a, _ := newTestAuth(t, func(c *Config) { c.SigninScreen = screen })
				wireKeymail(a, newKeymailFake())
				form := url.Values{"address": {"ada@example.com"}}
				if expect != "" {
					form.Set("expect", expect)
				}
				ab, rb := newBrowser(), newBrowser()
				sent := ab.do(a.Begin, http.MethodPost, "/signin", form)
				refused := rb.do(a.AnswerAsSent, http.MethodPost, "/signin", form)
				pa := renderSignin(t, stateAt(a, ab, sent.Header().Get("Location")))
				pr := renderSignin(t, stateAt(a, rb, refused.Header().Get("Location")))
				if pa != pr {
					t.Fatalf("a sent link and a refusal render different pages:\n sent:    %s\n refused: %s", pa, pr)
				}
				if screen && !strings.Contains(pa, "<bdi>ada@example.com</bdi>") {
					t.Fatalf("screen on, the Sent page does not name the address:\n%s", pa)
				}
			})
		}
	}
}
```
- [ ] **Step 4: Run the tests to verify they fail**

Run: `GOFLAGS=-mod=mod go test ./ui/ ./auth/ ./internal/designsystem/ -count=1 -run 'Signin|OneTap|Passkey|Keymail|Continue|Sent|AllPartials|AdmissionWrapperRenders'`
Expected: FAIL — `html/template: no such template "signin"`, and the gallery test's `no screen signin-ask on the Screens page`.

- [ ] **Step 5: Write the partials**

`ui/partials/signin.html`:

```html
{{/* signin — the shipped sign-in screen: one card, brand at the start
     and the way in at the end. Everything it shows comes from
     auth.SigninState; the page calls auth's PrepareSigninResponse
     before rendering it. Put it in the stage shell (ui.Layout("stage"))
     or any other.

     The auth side must have Config.SigninScreen on: that is what writes
     the cookies this screen reads, keeps a keymail sign-in on the page
     under the default CSP, and remembers the way in.

     Keys:
       State    auth.SigninState, required
       Brand    required: Name (string); optional Pitch (string, one
                line) and Mark (template.HTML — an <img> or inline SVG
                the app owns)
       Preview  bool, optional — gallery use only: no meta refresh, the
                Continue link goes to #, and the passkey door shows as
                the enhanced page would, with nothing for a script to find
                and no script loaded

     The passkey door loads its own module, State.Passkey.ScriptURL
     (passkey.JS(), served by the app), and only when it renders: every
     other page of the app, and every other state of this one, requests
     nothing.

     No role="alert" anywhere: every message present at load is where
     focus starts (State.Focus), and a live alert would race the focus
     announcement. The only live region is the passkey message. */}}
{{define "signin"}}{{$s := .State}}{{$door := $s.Door}}{{$focus := $s.Focus}}{{$name := .Brand.Name}}{{$preview := opt . "Preview"}}<section rst-signin aria-labelledby="rst-signin-heading">
<div rst-signin-brand>{{with opt .Brand "Mark"}}<div rst-signin-mark>{{.}}</div>{{end}}<p rst-signin-name>{{$name}}</p>{{with opt .Brand "Pitch"}}<p rst-signin-pitch>{{.}}</p>{{end}}</div>
<div rst-signin-door>
{{if eq $door "continue"}}{{/* In the card, not <head>: browsers apply a refresh wherever the element
     is, the page's layout owns <head>, and this is how fichas runs in
     production. A document navigating is not the form's redirect chain,
     so form-action 'self' never sees it. */}}{{if not $preview}}<meta http-equiv="refresh" content="0;url={{$s.ContinueURL}}">{{end}}
<h1 id="rst-signin-heading">{{T "rastrillo.ui.signin_continue_heading"}}</h1>
<p>{{T "rastrillo.ui.signin_continue_body"}}</p>
<a rst-btn="primary block" href="{{if $preview}}#{{else}}{{$s.ContinueURL}}{{end}}">{{T "rastrillo.ui.signin_to_keymail"}}</a>
{{else if eq $door "sent"}}<h1 id="rst-signin-heading">{{T "rastrillo.ui.signin_sent_heading"}}</h1>
{{if $s.SentInstead}}<p>{{T "rastrillo.ui.signin_sent_instead"}}</p>
{{end}}<p>{{if $s.SentTo}}{{Tbdi "rastrillo.ui.signin_sent_to" "address" $s.SentTo}}{{else}}{{T "rastrillo.ui.signin_sent_inbox"}}{{end}} {{T "rastrillo.ui.signin_sent_once"}}</p>
<form rst-signin-form method="post" action="{{$s.ForgetPath}}"><button rst-btn="ghost block" type="submit">{{T "rastrillo.ui.signin_different"}}</button></form>
{{else}}<h1 id="rst-signin-heading">{{Tf "rastrillo.ui.signin_heading" "name" $name}}</h1>
{{if and $s.Problem (ne $s.Problem "address")}}{{$tone := "negative"}}{{if eq $s.Problem "reauth"}}{{$tone = "info"}}{{end}}{{template "callout" (dict "ID" "rst-signin-problem" "Tone" $tone "Body" (T (printf "rastrillo.ui.signin_problem_%s" $s.Problem)) "Focus" (eq $focus "callout"))}}
{{end}}{{if eq $door "keymail"}}<form rst-signin-form method="post" action="{{$s.BeginPath}}">
<input type="hidden" name="address" value="{{$s.Remembered.Address}}">
<input type="hidden" name="expect" value="keymail">
<button rst-btn="primary block" type="submit" aria-describedby="rst-signin-remembered"{{if eq $focus "onetap"}} autofocus{{end}}>{{T "rastrillo.ui.signin_to_keymail"}}</button>
<p rst-signin-remembered id="rst-signin-remembered">{{Tbdi "rastrillo.ui.signin_remembered_as" "address" $s.Remembered.Address}}</p>
</form>
{{else if eq $door "link"}}<form rst-signin-form method="post" action="{{$s.BeginPath}}">
<input type="hidden" name="address" value="{{$s.Remembered.Address}}">
<button rst-btn="primary block" type="submit"{{if eq $focus "onetap"}} autofocus{{end}}>{{Tbdi "rastrillo.ui.signin_continue_as" "address" $s.Remembered.Address}}</button>
</form>
{{else}}{{/* The ask door, and the passkey door with the email form beside it
     (the email form is always on the screen where a passkey door is:
     a passkey is never the only way in). The passkey button is written
     once, in whichever slot this door puts it: first for a remembered
     passkey, after the form otherwise. */}}{{$pkSlot := "after"}}{{if eq $door "passkey"}}{{$pkSlot = "before"}}{{end}}{{$fieldErr := ""}}{{if eq $s.Problem "address"}}{{$fieldErr = T "rastrillo.ui.signin_problem_address"}}{{end}}{{range list "before" "form" "after"}}{{if eq . "form"}}<form rst-signin-form method="post" action="{{$s.BeginPath}}">
{{template "field" (dict "ID" "rst-signin-email" "Name" "address" "Label" (T "rastrillo.ui.signin_email") "Type" "email" "Autocomplete" "email" "Required" true "Value" $s.Address "Autofocus" (eq $focus "field") "Error" $fieldErr "QuietError" true)}}
{{if eq $s.Problem "keymail"}}<input type="hidden" name="force" value="1">
{{end}}<button rst-btn="primary block" type="submit">{{if eq $s.Problem "keymail"}}{{T "rastrillo.ui.signin_send_link_instead"}}{{else}}{{T "rastrillo.ui.signin_submit"}}{{end}}</button>
</form>
{{else if and $s.Passkey (eq . $pkSlot)}}<button rst-btn="{{if eq $door "passkey"}}primary block{{else}}block{{end}}" type="button"{{if not $preview}} hidden data-rst-passkey data-rst-passkey-begin="{{$s.Passkey.BeginPath}}" data-rst-passkey-finish="{{$s.Passkey.FinishPath}}" data-rst-passkey-module="{{$s.Passkey.ModuleURL}}"{{with $s.Passkey.LegacyRPID}} data-rst-passkey-legacy-rpid="{{.}}"{{end}} data-rst-passkey-cancelled="{{T "rastrillo.ui.signin_passkey_cancelled"}}" data-rst-passkey-failed="{{T "rastrillo.ui.signin_passkey_failed"}}"{{end}} aria-describedby="rst-signin-passkey-msg">{{if eq $door "passkey"}}{{T "rastrillo.ui.signin_passkey_remembered"}}{{else}}{{T "rastrillo.ui.signin_passkey"}}{{end}}</button>
<p rst-signin-passkey-msg id="rst-signin-passkey-msg" aria-live="polite"></p>
{{if not $preview}}<script type="module" src="{{$s.Passkey.ScriptURL}}"></script>
{{end}}{{end}}{{end}}{{end}}{{if ne $door "ask"}}<form rst-signin-form method="post" action="{{$s.ForgetPath}}"><button rst-btn="ghost block" type="submit">{{T "rastrillo.ui.signin_different"}}</button></form>
{{end}}{{end}}</div>
</section>{{end}}
{{/* signin-title — the document title for the same state, for the
     page's {{define "title"}}: every state has one heading and a title
     that says the same thing, so a screen-reader user who lands on the
     page hears where they are. Keys: State, Brand (as signin). */}}
{{define "signin-title"}}{{$s := .State}}{{$n := .Brand.Name}}{{if eq $s.Step "sent"}}{{Tf "rastrillo.ui.signin_sent_title" "name" $n}}{{else if eq $s.Step "continue"}}{{Tf "rastrillo.ui.signin_continue_title" "name" $n}}{{else if and $s.Problem (ne $s.Problem "reauth")}}{{Tf "rastrillo.ui.signin_problem_title" "name" $n}}{{else}}{{Tf "rastrillo.ui.signin_title" "name" $n}}{{end}}{{end}}
```

Note the `ne $door "ask"` clause: the "Use a different email" form appears on the keymail, link and passkey doors (the Sent door has its own above) and never on Ask.

- [ ] **Step 6: Style the card**

`ui/tokens.css` — a new section after the callout rules (after the `[rst-callout][rst-tone~="negative"] > [rst-callout-ic]` line):

```css
/* ── rst-signin: the shipped sign-in card (partials/signin.html) ─────
   Brand at the start and the way in at the end, side by side from 48rem
   and stacked below it, so a phone reads the brand and then the door.
   The card paints its own surface and border because it usually sits
   on the stage shell's backdrop: whatever picture an app puts there,
   the controls stay on a known ground and their contrast is the
   theme's, not the picture's. overflow-wrap on the door is for the one
   long string it shows back — an address, which has no spaces to break
   at and would otherwise push a 320px screen sideways. */
.rst-signin, [rst-signin] { background: var(--rst-surface); border: 1px solid var(--rst-line-strong); border-radius: var(--rst-radius); box-shadow: var(--rst-shadow-pop); box-sizing: border-box; display: grid; gap: var(--rst-sp-5); inline-size: 100%; max-inline-size: 56rem; padding: var(--rst-sp-5); }
@media (min-width: 48rem) {
  .rst-signin, [rst-signin] { align-items: center; gap: var(--rst-sp-6); grid-template-columns: minmax(0, 1fr) minmax(0, 1.25fr); padding: var(--rst-sp-6); }
}
.rst-signin__brand, [rst-signin-brand] { display: flex; flex-direction: column; gap: var(--rst-sp-2); min-inline-size: 0; }
.rst-signin__mark, [rst-signin-mark] { block-size: 3rem; inline-size: 3rem; }
.rst-signin__mark > *, [rst-signin-mark] > * { block-size: 100%; display: block; inline-size: 100%; }
.rst-signin__name, [rst-signin-name] { font-size: var(--rst-fs-lg); font-weight: 600; margin: 0; }
.rst-signin__pitch, [rst-signin-pitch] { color: var(--rst-text-muted); margin: 0; }
.rst-signin__door, [rst-signin-door] { display: flex; flex-direction: column; gap: var(--rst-sp-4); min-inline-size: 0; overflow-wrap: anywhere; }
.rst-signin__door h1, [rst-signin-door] h1 { color: var(--rst-text); font-size: 1.375rem; font-weight: 600; letter-spacing: -0.015em; line-height: 1.25; margin: 0; }
.rst-signin__door > p, [rst-signin-door] > p { margin: 0; }
/* The door spaces its children with gap; a callout's and a field's own
   block margins would double it. */
.rst-signin__door > .rst-callout, [rst-signin-door] > [rst-callout] { margin: 0; }
.rst-signin__form, [rst-signin-form] { display: flex; flex-direction: column; gap: var(--rst-sp-3); margin: 0; }
.rst-signin__form > .rst-field, [rst-signin-form] > [rst-field] { margin: 0; }
.rst-signin__remembered, [rst-signin-remembered] { color: var(--rst-text-muted); margin: 0; }
.rst-signin__passkey-msg, [rst-signin-passkey-msg] { color: var(--rst-text-muted); margin: 0; }
```

Re-copy: `cp ui/tokens.css examples/blog/static/tokens.css && cp ui/tokens.css examples/tickets/static/tokens.css`.

- [ ] **Step 7: Rebuild `screens.go` on the partial**

Replace `screenDoc`, `screenDocs` and `buildScreens` in `internal/designsystem/screens.go` (keep the file's header comment, changing its last paragraph to: "Sign-in is the first group because it is the screen every app needs on its first day. Most of it is now the shipped signin partial, rendered in its states; the two it does not ship — a password screen and third-party buttons — stay as examples to copy."), and add the import `"amadan.net/rastrillo/rastrillo/auth"`:

```go
// screenDoc is one screen's source, before it is rendered.
type screenDoc struct {
	// Key is the anchor id's suffix and previewHeights' key.
	Key string
	// Name and Blurb are English, and therefore prose keys.
	Name  string
	Blurb string
	// WarningTitle and WarningBody render a callout above the screen.
	WarningTitle string
	WarningBody  string
	// Markup is a hand-written screen: complete HTML with no template
	// actions, on the same footing as ui.Styleguide's samples.
	Markup string
	// Signin is a state of the shipped signin partial. A screen with one
	// is rendered from the partial itself, in a stage frame, so this page
	// cannot show a sign-in screen the framework does not produce.
	Signin *auth.SigninState
}

// screenPartials are the partials the Screens page documents. A family
// IS a page and buildFamilies refuses a partial no page claims; the
// sign-in partials belong here, on the page about whole screens, and
// this is that claim. The first sign-in screen carries their markers.
var screenPartials = []string{"signin", "signin-title"}

// galleryPasskey is a passkey door for the samples. In a Preview the
// partial shows the button as the enhanced page would and writes none
// of these paths, so nothing here needs to exist.
var galleryPasskey = &auth.PasskeyDoor{BeginPath: "/passkey/discover/begin", FinishPath: "/passkey/discover/finish", ModuleURL: "/static/webauthn.mjs", ScriptURL: "/static/passkey-signin.mjs"}

// galleryBrand is sample data, not the page's voice: it stays English on
// every page, like every other sample's names.
var galleryBrand = map[string]any{"Name": "Harbour"}

const graceAddress = "grace@example.com"

func signinScreen(mut func(*auth.SigninState)) *auth.SigninState {
	st := auth.SigninState{Step: auth.StepAsk, BeginPath: "/signin", ForgetPath: "/signin/forget"}
	mut(&st)
	return &st
}

// signinSource is the Code tab for every sign-in state: the two
// template calls an app writes, not the rendered markup. The markup is
// the partial's to produce, and a copied snapshot of it would freeze a
// screen the module keeps improving.
const signinSource = `{{define "title"}}{{template "signin-title" (dict "State" .Signin "Brand" .Brand)}}{{end}}
{{define "content"}}{{template "signin" (dict "State" .Signin "Brand" .Brand)}}{{end}}`

// screenFrame is the stage shell in miniature for a preview frame: the
// same attributes layouts/stage.html writes on <body>, on a wrapper.
// Only the first frame draws the backdrop: it is the same picture in
// every frame, and ten escaped copies of it would spend a fifth of the
// page's byte budget saying nothing new.
const screenFrame = `{{define "ds-screen-stage"}}<div rst-stage><div rst-stage-scene aria-hidden="true">{{if .Art}}{{stageArt "rastrillo"}}{{end}}</div><div rst-page>{{template "signin" .}}</div></div>{{end}}`

// screenDocs is the page's content, in page order.
func screenDocs() []screenDoc {
	kay := &auth.Remembered{Method: "keymail", Address: "kay@example.org"}
	return []screenDoc{
		{Key: "signin-ask", Name: "Asking for an address",
			Blurb:  "One field and one button. Nothing to remember and nothing to steal. Rastrillo does this out of the box.",
			Signin: signinScreen(func(s *auth.SigninState) { s.Passkey = galleryPasskey })},
		{Key: "signin-returning-keymail", Name: "Coming back with Keymail",
			Blurb: "The browser remembers how it got in last time. One tap sends the remembered address, which is checked again from scratch.",
			Signin: signinScreen(func(s *auth.SigninState) {
				s.Step, s.Remembered, s.Address = auth.StepReturning, kay, kay.Address
			})},
		{Key: "signin-returning-link", Name: "Coming back with an email link",
			Blurb: "The address is on the button, so there is nothing to type.",
			Signin: signinScreen(func(s *auth.SigninState) {
				s.Step, s.Remembered = auth.StepReturning, &auth.Remembered{Method: "magiclink", Address: graceAddress}
			})},
		{Key: "signin-returning-passkey", Name: "A passkey",
			Blurb: "The fastest way in for someone who has one, and nothing to type. Offer a second way as well: people lose devices, and a passkey that will not work is a locked door.",
			Signin: signinScreen(func(s *auth.SigninState) {
				s.Step, s.Remembered, s.Passkey = auth.StepReturning, &auth.Remembered{Method: "passkey"}, galleryPasskey
			})},
		{Key: "signin-sent", Name: "After the link is sent",
			Blurb:  "Repeat the address back. It is the only chance to notice a typo before waiting for an email that will never come.",
			Signin: signinScreen(func(s *auth.SigninState) { s.Step, s.SentTo = auth.StepSent, graceAddress })},
		{Key: "signin-sent-unbound", Name: "After the link is sent, in another browser",
			Blurb:  "When this browser did not send the address, the page does not guess it.",
			Signin: signinScreen(func(s *auth.SigninState) { s.Step = auth.StepSent })},
		{Key: "signin-sent-instead", Name: "When Keymail was offered and a link went out",
			Blurb: "One line says a link was sent this time. It does not guess why.",
			Signin: signinScreen(func(s *auth.SigninState) {
				s.Step, s.SentTo, s.SentInstead = auth.StepSent, kay.Address, true
			})},
		{Key: "signin-continue", Name: "On the way to Keymail",
			Blurb: "The page moves on by itself, with a link in case it does not.",
			Signin: signinScreen(func(s *auth.SigninState) {
				s.Step, s.ContinueURL = auth.StepContinue, "https://keymail.example/oauth/authorize"
			})},
		{Key: "signin-problem-address", Name: "An address that does not look right",
			Blurb: "The message belongs to the field, and that is where focus starts.",
			Signin: signinScreen(func(s *auth.SigninState) {
				s.Problem, s.Address = auth.ProblemAddress, "grace@example"
			})},
		{Key: "signin-problem-keymail", Name: "When Keymail could not confirm it",
			Blurb: "The button changes to send a link instead, so nobody is stuck.",
			Signin: signinScreen(func(s *auth.SigninState) {
				s.Problem, s.Address = auth.ProblemKeymail, kay.Address
			})},
		{
			Key:   "signin-social",
			Name:  "Google, Apple and the rest",
			Blurb: "We give you the buttons, drawn the way each company requires. We do not give you the sign-in itself — you wire that up to whichever provider you use.",
			Markup: `<section rst-box>
  <h1>Sign in</h1>
  <form rst-form method="post" action="/auth/google"><button rst-btn type="submit">Continue with Google</button></form>
  <form rst-form method="post" action="/auth/apple"><button rst-btn type="submit">Continue with Apple</button></form>
  <p><a href="/signin">Email me a link instead</a></p>
</section>`,
		},
		{
			Key:          "signin-password",
			Name:         "Email and password",
			Blurb:        "If you do use passwords, use the same one-way-in layout, and put a way to reset directly under the button rather than hiding it.",
			WarningTitle: "We do not recommend passwords",
			WarningBody:  "Rastrillo does not ship this screen. People reuse passwords, they leak, and you inherit the job of storing them safely. Use a link in your email plus a passkey, or passkeys on their own. The markup is here because some products still need it.",
			Markup: `<section rst-box>
  <h1>Sign in</h1>
  <form rst-form method="post" action="/signin">
    <div rst-field>
      <label rst-field-label for="pw-email">Email</label>
      <input rst-input id="pw-email" name="email" type="email" autocomplete="email" required>
    </div>
    <div rst-field>
      <label rst-field-label for="pw-pass">Password</label>
      <input rst-input id="pw-pass" name="password" type="password" autocomplete="current-password" required>
    </div>
    <button rst-btn="primary" type="submit">Continue</button>
  </form>
</section>`,
		},
	}
}

// buildScreens renders every screen for one theme and locale.
func buildScreens(mount, theme, locale string, tmpl *template.Template) ([]screenView, error) {
	docs := screenDocs()
	out := make([]screenView, 0, len(docs))
	first := true
	for _, doc := range docs {
		id := anchorID("screen", doc.Key)
		view := screenView{
			Name:   proseIn(locale, doc.Name),
			ID:     id,
			Marker: marker("screen", doc.Key),
			Blurb:  proseIn(locale, doc.Blurb),
		}
		if doc.WarningBody != "" {
			var buf strings.Builder
			err := tmpl.ExecuteTemplate(&buf, "callout", map[string]any{
				"Tone":  "warning",
				"Title": proseIn(locale, doc.WarningTitle),
				"Body":  proseIn(locale, doc.WarningBody),
			})
			if err != nil {
				return nil, fmt.Errorf("screen %s warning: %w", doc.Key, err)
			}
			view.Warning = template.HTML(buf.String())
		}
		title := previewTitle(locale, doc.Key, "Screens")
		if doc.Signin == nil {
			view.Preview = newPreview(mount, theme, locale, id+"-0", title, doc.Markup, id)
			out = append(out, view)
			continue
		}
		var buf strings.Builder
		err := tmpl.ExecuteTemplate(&buf, "ds-screen-stage", map[string]any{
			"State": *doc.Signin, "Brand": galleryBrand, "Preview": true, "Art": first,
		})
		if err != nil {
			return nil, fmt.Errorf("screen %s: %w", doc.Key, err)
		}
		if first {
			for _, name := range screenPartials {
				view.Marker += marker("partial", name)
			}
			first = false
		}
		view.Preview = newPreview(mount, theme, locale, id+"-0", title, buf.String(), id)
		view.Preview.Source = signinSource
		out = append(out, view)
	}
	return out, nil
}
```

Change `screensBody`'s lead paragraph to `{{P "The sign-in screens are the shipped signin partial, shown in its states. The last two are examples to copy."}}`.

The password example keeps `name="email"`: it posts to the password plugin, whose handler reads `email` (password/handlers.go:186). Only the dead reset link goes. `address` is the magic-link form's name, which the shipped partial now writes itself.

- [ ] **Step 8: Wire the frame, the claim, the heights, the padding and the stage demo into `page.go`**

In `renderGallery`, right after `parseRawSamples`:

```go
	if _, err := tmpl.Parse(screenFrame); err != nil {
		return nil, fmt.Errorf("parsing the screen frame: %w", err)
	}
```

In `buildFamilies`, before `defined, err := partialNames()`:

```go
	// The Screens page is a page too, and it documents the sign-in
	// partials; see screenPartials.
	for _, name := range screenPartials {
		claimed[name] = true
	}
```

In `previewHeights`, replace the five `screen-signin-*` entries with:

```go
	// The sign-in screens: a stage frame is at least as tall as its
	// frame (100dvh), so these are the card plus its margin, measured.
	"screen-signin-ask":                460,
	"screen-signin-returning-keymail":  420,
	"screen-signin-returning-link":     400,
	"screen-signin-returning-passkey":  520,
	"screen-signin-sent":               400,
	"screen-signin-sent-unbound":       400,
	"screen-signin-sent-instead":       440,
	"screen-signin-continue":           400,
	"screen-signin-problem-address":    480,
	"screen-signin-problem-keymail":    520,
	"screen-signin-social":             290,
	"screen-signin-password":           420,
```

These are starting values; Task 13's browser run of `TestPreviewFrameHeightsFitTheirContent` measures them and says which to change.

In `srcdoc`, the zero-padding rule becomes:

```go
	b.WriteString("body:has(> [rst-shell-topbar], > [rst-shell-sidebar], > [rst-backdrop], > [rst-stage]) { padding: 0; }</style>\n")
```

(and its comment gains "a stage frame" in the list of whole-page samples).

`shellData` gains a field:

```go
	// Signin is the stage demo's card: the Ask state, with nothing
	// behind it.
	Signin auth.SigninState
```

In `renderShell`, replace the single `tmpl.Parse(shellTemplate)` with:

```go
	overrides := shellTemplate
	if shell == "stage" {
		// stage has no chrome to fill and one card to show; the generic
		// overrides would put a Posts list in the middle of a sign-in
		// frame.
		overrides = stageShellTemplate
	}
	if _, err := tmpl.Parse(overrides); err != nil {
		return nil, fmt.Errorf("parsing the shell overrides: %w", err)
	}
```

and set `Signin: auth.SigninState{Step: auth.StepAsk, BeginPath: "/signin", ForgetPath: "/signin/forget"}` in the `shellData` literal. Add after `shellTemplate`:

```go
// stageShellTemplate fills the stage shell's blocks for its demo: the
// sign-in card in its Ask state, and a foot with the way back. The
// backdrop block is left alone, so the demo shows the default art.
const stageShellTemplate = `
{{define "head"}}<script src="{{.Mount}}/gallery.js"></script>{{end}}
{{define "lang"}}{{.Locale}}{{end}}
{{define "dir"}}{{.Dir}}{{end}}
{{define "title"}}{{.Title}}{{end}}
{{define "foot"}}<footer rst-stage-foot><a href="{{.Index}}">{{P "Back to the design system"}}</a></footer>{{end}}
{{define "content"}}{{template "signin" (dict "State" .Signin "Brand" (dict "Name" "Harbour") "Preview" true)}}{{end}}
`
```

The comment above `shellTemplate` — "fills every block the four shells leave open … one override set covers all four" — becomes "…the chrome shells leave open … one override set covers all four of them; stage has its own, below". `page.go` imports `"amadan.net/rastrillo/rastrillo/auth"`.

- [ ] **Step 9: Bring `prose.go` in line**

Delete the rows whose keys the page no longer says — `TestEveryProseKeyIsTranslated` fails on a stale row:
- `A link in your email`
- `People reuse them, they leak, and you inherit the job of storing them safely. Use a link in your email plus a passkey, or passkeys on their own, or sign-in with an account people already have. The screen below is here because some products still need it, not because it is a good default.`
- `The screens here are examples to copy rather than components.`

Add one row per new key, each with all eleven translations (`ga`, `zh-Hans`, `es`, `hi`, `pt`, `bn`, `ru`, `ja`, `yue`, `vi`, `ar`) in the register the file's header sets, `{shell}` kept verbatim where present, in this shape (each `…` is your translation of the key into that language; the file's rules hold: identifiers untranslated, `{placeholders}` kept):

```go
	`Asking for an address`: {
		`ga`:      `…`,
		`zh-Hans`: `…`,
		`es`:      `…`,
		`hi`:      `…`,
		`pt`:      `…`,
		`bn`:      `…`,
		`ru`:      `…`,
		`ja`:      `…`,
		`yue`:     `…`,
		`vi`:      `…`,
		`ar`:      `…`,
	},
```

The keys (drafts; use the approved English):
- `Asking for an address`
- `Coming back with Keymail`
- `The browser remembers how it got in last time. One tap sends the remembered address, which is checked again from scratch.`
- `Coming back with an email link`
- `The address is on the button, so there is nothing to type.`
- `After the link is sent, in another browser`
- `When this browser did not send the address, the page does not guess it.`
- `When Keymail was offered and a link went out`
- `One line says a link was sent this time. It does not guess why.`
- `On the way to Keymail`
- `The page moves on by itself, with a link in case it does not.`
- `An address that does not look right`
- `The message belongs to the field, and that is where focus starts.`
- `When Keymail could not confirm it`
- `The button changes to send a link instead, so nobody is stuck.`
- `Rastrillo does not ship this screen. People reuse passwords, they leak, and you inherit the job of storing them safely. Use a link in your email plus a passkey, or passkeys on their own. The markup is here because some products still need it.`
- `The sign-in screens are the shipped signin partial, shown in its states. The last two are examples to copy.`

Kept and reused unchanged: `A passkey`, `After the link is sent`, `One field and one button. …`, `Repeat the address back. …`, `The fastest way in for someone who has one, …`, `Google, Apple and the rest`, `Email and password`, `If you do use passwords, …`, `We do not recommend passwords`.

- [ ] **Step 10: Run everything this task touches**

Run: `GOFLAGS=-mod=mod go test ./ui/ ./auth/ ./internal/designsystem/ . -count=1 && (cd examples/blog && GOFLAGS=-mod=mod go test ./...) && (cd examples/tickets && GOFLAGS=-mod=mod go test ./...)`
Expected: PASS — the new tests; `TestRenderedPartialsAreSelfContained`, `TestRenderEverythingSmoke` (balanced tags, unique ids across all fixtures; the fixture is the Ask state with no passkey door, so it loads no script), the twin tests, `TestPartialsAndLayoutsEmitNoInlineStyles`; and the gallery's `TestEveryPartialAppearsAcrossThePages` (signin and signin-title marked once per theme × locale), `TestEveryProseKeyIsTranslated` (no missing, no stale), `TestNoEnglishProseReachesATranslatedPage`, `TestSampleLinksAndFormsAreDeadInThePreviews`, `TestEveryPageStaysUnderItsBudget`, `TestRenderIsDeterministic`, `TestEveryFrameTitleIsUniqueOnThePage`. If the Screens page is over its 128 KiB budget, read which locale and by how much before changing anything; the art-free frames are the lever, not a debt entry.

Then `rm -rf copy-review`: every batch-1 string has now been applied, and Task 14's batch 2 starts a fresh index.

- [ ] **Step 11: Run the gate and commit**

Run: `GOFLAGS=-mod=mod go vet ./... && gofmt -l . && RASTRILLO_TEST_REQUIRE_NODE=1 GOFLAGS=-mod=mod go test ./... -count=1`

```bash
git add ui auth/signinpage_test.go internal/designsystem examples/blog/static/tokens.css examples/tickets/static/tokens.css
git commit -F - <<'EOF'
ui: the signin partials, and the gallery's sign-in screens built on them

Every app needs a sign-in screen on its first day and until now had to
improvise one. This renders every state SigninState can be in — Ask,
the three remembered one-taps, Sent bound or unbound, the Keymail
continuation and each problem — with focus starting where §2's matrix
says and no role="alert" to race it. The passkey door loads its module
only when it renders, so no other page requests it.

The gallery's hand-written sign-in markup had two bugs (a field name
the handler does not read, two links nothing serves); its Screens page
now renders the partial itself in a stage frame, with the reviewed
English and its translations. The partials and the page that claims
them land together because the gallery refuses an unclaimed partial.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
git push origin signin-screen
```

---

### Task 13: The browser drives, and accessibility on every state

**Files:**
- Create: `auth/signinscreen_browser_test.go` (`//go:build browser`)
- Modify: `internal/designsystem/a11y_test.go` (`previewPageKinds`, `pickPreviewFrames`, `a11yTargets`, `TestA11yReflowsAt320`'s page list, and a new `TestA11yScansEverySigninState`), `internal/designsystem/page.go` (`previewHeights`, `previewMobileHeights` — measured values), `Makefile` (browser target gains `./auth/`)

**Interfaces:**
- Consumes: everything from Tasks 3–12, and `passkey.JS()` (Task 9): `New`, `Config.SigninScreen`, `SigninState`, `PrepareSigninResponse`, `PasskeyDoor`, `Begin`, `Callback`, `Verify`, `Signout`, `Forget`, `RememberJar`, `validAuthorizeURL`, `kayFake`, `wireKeymail`, `captureMailer` (auth); `passkey.New/Config.Remember` and its handlers; `ui.Funcs`, `ui.Templates`, `ui.Layout("stage")`, `ui.VendoredAssets`; `webauthn.JS()`; `rastrillo.Handler`, `rastrillo.BaseCatalog`; `harness.New`, `(*Rig).Run/Screen/Context/Origin`.
- Produces: nothing other tasks use.

- [ ] **Step 1: Write the drive**

`auth/signinscreen_browser_test.go`:

```go
//go:build browser

package auth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/csrf"
	"amadan.net/rastrillo/rastrillo/db"
	"amadan.net/rastrillo/rastrillo/harness"
	"amadan.net/rastrillo/rastrillo/migrate"
	"amadan.net/rastrillo/rastrillo/passkey"
	"amadan.net/rastrillo/rastrillo/secondfactor"
	"amadan.net/rastrillo/rastrillo/sessions"
	"amadan.net/rastrillo/rastrillo/ui"
	"amadan.net/rastrillo/rastrillo/webauthn"
)

// screenApp is a whole app on the shipped screen: auth with
// SigninScreen, passkey discovery wired to its jar, the stage shell and
// the vendored assets — served through rastrillo.Handler, so every page
// carries the framework's real CSP, form-action 'self' included.
// Classification and the token exchange go to the in-process fake;
// the browser's own navigation to keymail.test is caught by CDP's Fetch
// domain (interceptKeymail). Nothing real is contacted.
type screenApp struct {
	auth *Auth
	mail *captureMailer
}

func newScreenApp(t *testing.T) (*screenApp, func(origin string) http.Handler) {
	app := &screenApp{mail: &captureMailer{}}
	return app, func(origin string) http.Handler {
		d, err := db.Open(filepath.Join(t.TempDir(), "screen.db"), nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { d.Close() })
		if _, err := migrate.Apply(context.Background(), d, migrate.Merge(sessions.Schema, Schema, passkey.Schema, secondfactor.Schema)); err != nil {
			t.Fatal(err)
		}
		a, err := New(Config{DB: d.Writer(), Origin: origin, InstanceKey: "browser-instance-key", Mailer: app.mail, SigninScreen: true})
		if err != nil {
			t.Fatal(err)
		}
		wireKeymail(a, kayFake())
		app.auth = a
		sess, err := sessions.New(sessions.Config{DB: d.Writer(), Origin: origin})
		if err != nil {
			t.Fatal(err)
		}
		pk, err := passkey.New(passkey.Config{Sessions: sess, DB: d.Writer(), Origin: origin, Remember: a.RememberJar()})
		if err != nil {
			t.Fatal(err)
		}

		funcs := ui.Funcs()
		funcs["asset"] = func(p string) string { return "/" + p }
		stage, _ := ui.Layout("stage")
		tmpl := template.Must(template.New("layout").Funcs(funcs).ParseFS(ui.Templates(), "*.html"))
		template.Must(tmpl.Parse(string(stage)))
		template.Must(tmpl.Parse(`{{define "title"}}{{template "signin-title" (dict "State" .Signin "Brand" .Brand)}}{{end}}` +
			`{{define "content"}}{{template "signin" (dict "State" .Signin "Brand" .Brand)}}{{end}}`))

		mux := http.NewServeMux()
		assets, _ := ui.VendoredAssets("day")
		for name, body := range assets {
			ct := "text/css; charset=utf-8"
			if strings.HasSuffix(name, ".js") {
				ct = "text/javascript; charset=utf-8"
			}
			mux.HandleFunc("GET /static/"+name, serveBytes(body, ct))
		}
		mux.HandleFunc("GET /static/webauthn.mjs", serveBytes(webauthn.JS(), "text/javascript; charset=utf-8"))
		mux.HandleFunc("GET /static/passkey-signin.mjs", serveBytes(passkey.JS(), "text/javascript; charset=utf-8"))
		signinPage := func(door bool) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				st := a.SigninState(r)
				if door {
					st.Passkey = &PasskeyDoor{
						BeginPath: "/passkey/discover/begin", FinishPath: "/passkey/discover/finish",
						ModuleURL: "/static/webauthn.mjs", ScriptURL: "/static/passkey-signin.mjs",
					}
				}
				a.PrepareSigninResponse(w, st)
				var buf bytes.Buffer
				if err := tmpl.ExecuteTemplate(&buf, "layout", map[string]any{"Signin": st, "Brand": map[string]any{"Name": "Harbour"}}); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.Write(buf.Bytes())
			}
		}
		mux.HandleFunc("GET /signin", signinPage(true))
		// The same screen in an app with no passkeys: email only.
		mux.HandleFunc("GET /signin-plain", signinPage(false))
		// An ordinary page, to show the door's module is not everyone's.
		mux.HandleFunc("GET /about", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, `<!doctype html><html lang="en"><title>About</title><main><h1 id="about">About</h1></main></html>`)
		})
		mux.HandleFunc("POST /signin", a.Begin)
		mux.HandleFunc("POST /signin/forget", a.Forget)
		mux.HandleFunc("GET /auth/callback", a.Callback)
		mux.HandleFunc("GET /auth/verify", a.Verify)
		mux.HandleFunc("POST /signout", a.Signout)
		mux.HandleFunc("POST /passkey/discover/begin", pk.DiscoverBegin)
		mux.HandleFunc("POST /passkey/discover/finish", pk.DiscoverFinish)
		mux.HandleFunc("POST /passkey/register/begin", pk.RegisterBegin)
		mux.HandleFunc("POST /passkey/register/finish", pk.RegisterFinish)
		// Home names who is signed in, so a drive proves where a sign-in
		// landed and as whom, not just that some page has an h1.
		mux.Handle("GET /{$}", a.RequireSession(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id, _ := From(r)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprintf(w, `<!doctype html><html lang="en"><title>Home</title><main><h1 id="home">%s</h1></main></html>`, template.HTMLEscapeString(id.Address))
		})))
		// csrf.Protect as an app mounts it, so the drive's passkey and form
		// POSTs pass the same check they would in production.
		h, closeAll, err := rastrillo.Handler(rastrillo.Options{Mux: mux, Wrap: csrf.Protect(origin)})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { closeAll() })
		return h
	}
}

func serveBytes(body []byte, contentType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentType)
		w.Write(body)
	}
}

// provider is the drive's view of everything that leaves the app: it
// answers every request to keymail.test with a stand-in page, reports
// only the authorize-document navigations (a provider page's own
// favicon must not look like a second sign-in), and — while holdRefresh
// is set — serves the continuation page without its meta refresh, so
// the fallback link can be clicked on the real page under its real CSP.
type provider struct {
	authorize   chan string
	holdRefresh atomic.Bool
}

var metaRefresh = regexp.MustCompile(`<meta http-equiv="refresh"[^>]*>`)

func interceptProvider(rig *harness.Rig) *provider {
	p := &provider{authorize: make(chan string, 8)}
	ctx := rig.Context()
	chromedp.ListenTarget(ctx, func(ev any) {
		e, ok := ev.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		// Answered from a goroutine: a CDP call made inside a listener
		// deadlocks the event loop it is waiting on.
		go func() {
			ectx := cdp.WithExecutor(ctx, chromedp.FromContext(ctx).Target)
			if strings.Contains(e.Request.URL, "/signin?continue=") {
				if !p.holdRefresh.Load() {
					_ = fetch.ContinueRequest(e.RequestID).Do(ectx)
					return
				}
				body, err := fetch.GetResponseBody(e.RequestID).Do(ectx)
				if err != nil {
					_ = fetch.ContinueRequest(e.RequestID).Do(ectx)
					return
				}
				_ = fetch.FulfillRequest(e.RequestID, e.ResponseStatusCode).
					WithResponseHeaders(e.ResponseHeaders).
					WithBody(base64.StdEncoding.EncodeToString(metaRefresh.ReplaceAll(body, nil))).Do(ectx)
				return
			}
			if e.ResourceType == network.ResourceTypeDocument && strings.Contains(e.Request.URL, "/oauth/authorize") {
				p.authorize <- e.Request.URL
			}
			body := base64.StdEncoding.EncodeToString([]byte(`<!doctype html><html lang="en"><title>Keymail</title><main><h1 id="provider">Keymail stand-in</h1></main></html>`))
			_ = fetch.FulfillRequest(e.RequestID, http.StatusOK).
				WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).
				WithBody(body).Do(ectx)
		}()
	})
	rig.Run(fetch.Enable().WithPatterns([]*fetch.RequestPattern{
		{URLPattern: "https://keymail.test/*"},
		{URLPattern: "*/signin?continue=*", RequestStage: fetch.RequestStageResponse},
	}))
	return p
}

// requestsFor counts the requests the page makes whose URL path is path.
func requestsFor(rig *harness.Rig, path string) func() int {
	var mu sync.Mutex
	n := 0
	chromedp.ListenTarget(rig.Context(), func(ev any) {
		if e, ok := ev.(*network.EventRequestWillBeSent); ok {
			if u, err := url.Parse(e.Request.URL); err == nil && u.Path == path {
				mu.Lock()
				n++
				mu.Unlock()
			}
		}
	})
	return func() int {
		mu.Lock()
		defer mu.Unlock()
		return n
	}
}

// continuations records every /signin?continue= document the browser
// loaded, with its Content-Security-Policy.
type continuations struct {
	mu   sync.Mutex
	seen [][2]string
}

func watchContinuations(rig *harness.Rig) *continuations {
	c := &continuations{}
	chromedp.ListenTarget(rig.Context(), func(ev any) {
		e, ok := ev.(*network.EventResponseReceived)
		if !ok || e.Type != network.ResourceTypeDocument || !strings.Contains(e.Response.URL, "/signin?continue=") {
			return
		}
		csp, _ := e.Response.Headers["Content-Security-Policy"].(string)
		c.mu.Lock()
		c.seen = append(c.seen, [2]string{e.Response.URL, csp})
		c.mu.Unlock()
	})
	return c
}

func (c *continuations) last(t *testing.T) (id, csp string) {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.seen) == 0 {
		t.Fatal("no /signin?continue= document was loaded")
	}
	u, err := url.Parse(c.seen[len(c.seen)-1][0])
	if err != nil {
		t.Fatal(err)
	}
	return u.Query().Get("continue"), c.seen[len(c.seen)-1][1]
}

func awaitURL(t *testing.T, p *provider) string {
	t.Helper()
	select {
	case u := <-p.authorize:
		return u
	case <-time.After(15 * time.Second):
		t.Fatal("the continuation page never navigated to keymail")
		return ""
	}
}

func eval(rig *harness.Rig, expr string, out any) {
	rig.Run(chromedp.Evaluate(expr, out, func(p *runtime.EvaluateParams) *runtime.EvaluateParams {
		return p.WithAwaitPromise(true)
	}))
}

var verifyLink = regexp.MustCompile(`http://localhost:\d+/auth/verify\?token=[A-Za-z0-9_-]+`)

const registerPasskey = `(async () => {
  const m = await import("/static/webauthn.mjs");
  const post = (u, b) => fetch(u, {method: "POST", headers: {"Content-Type": "application/json"}, body: JSON.stringify(b || {})});
  const begin = await (await post("/passkey/register/begin")).json();
  const r = await m.register({challenge: begin.challenge, rpId: location.hostname, userId: "YWRh", userName: "ada@example.com"});
  return (await post("/passkey/register/finish", {clientDataJSON: r.clientDataJSON, attestationObject: r.attestationObject, label: "drive"})).status;
})()`

func TestSigninScreenInTheBrowser(t *testing.T) {
	app, build := newScreenApp(t)
	rig := harness.New(t, build)
	keymail := interceptProvider(rig)
	conts := watchContinuations(rig)
	moduleLoads := requestsFor(rig, "/static/passkey-signin.mjs")
	catalog := rastrillo.BaseCatalog()
	var s string
	var ok bool

	// Ordinary pages and an email-only sign-in screen never ask for the
	// passkey door's module; the screen with a door does.
	rig.Run(chromedp.Navigate(rig.Origin+"/about"), chromedp.WaitVisible("#about", chromedp.ByQuery))
	rig.Run(chromedp.Navigate(rig.Origin+"/signin-plain"), chromedp.WaitVisible("#rst-signin-email", chromedp.ByQuery))
	if n := moduleLoads(); n != 0 {
		t.Fatalf("%d requests for the passkey module from pages with no passkey door", n)
	}

	// Ask: focus starts in the field, and Tab reaches Continue, then the
	// passkey door, which its module revealed.
	rig.Run(chromedp.Navigate(rig.Origin+"/signin"), chromedp.WaitVisible(`[data-rst-passkey]`, chromedp.ByQuery))
	if moduleLoads() == 0 {
		t.Fatal("the screen with a passkey door never loaded its module")
	}
	rig.Screen("[rst-signin]", "the Ask screen")
	eval(rig, `document.activeElement.id`, &s)
	if s != "rst-signin-email" {
		t.Fatalf("focus starts on %q, want the email field", s)
	}
	rig.Run(chromedp.KeyEvent(kb.Tab))
	eval(rig, `document.activeElement.type + " " + document.activeElement.closest("form")?.getAttribute("action")`, &s)
	if s != "submit /signin" {
		t.Fatalf("Tab from the field reached %q, want Continue", s)
	}
	rig.Run(chromedp.KeyEvent(kb.Tab))
	eval(rig, `document.activeElement.hasAttribute("data-rst-passkey")`, &ok)
	if !ok {
		t.Fatal("the next Tab stop is not the passkey door")
	}

	// A typed address: the Sent page names it.
	rig.Run(
		chromedp.SendKeys("#rst-signin-email", "ada@example.com", chromedp.ByQuery),
		chromedp.Click(`form[action="/signin"] button[type="submit"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`[rst-signin-door] bdi`, chromedp.ByQuery),
	)
	rig.Screen("[rst-signin]", "the Sent screen")
	eval(rig, `document.querySelector("[rst-signin-door] bdi").textContent`, &s)
	if s != "ada@example.com" {
		t.Fatalf("Sent names %q", s)
	}

	// The link signs Ada in; she enrols a passkey and signs out.
	rig.Run(chromedp.Navigate(verifyLink.FindString(app.mail.body)), chromedp.WaitVisible("#home", chromedp.ByQuery))
	var status float64
	eval(rig, registerPasskey, &status)
	if status != http.StatusOK {
		t.Fatalf("registering a passkey answered %v", status)
	}
	eval(rig, `fetch("/signout", {method: "POST"}).then(r => r.status)`, &status)

	// The next visit offers the one-tap, focused, naming Ada; it posts
	// the remembered address.
	rig.Run(chromedp.Navigate(rig.Origin+"/signin"), chromedp.WaitVisible(`button[autofocus]`, chromedp.ByQuery))
	eval(rig, `(() => { const b = document.activeElement; return b.tagName + " " + (b.querySelector("bdi")?.textContent ?? ""); })()`, &s)
	if s != "BUTTON ada@example.com" {
		t.Fatalf("Returning focuses %q, want the one-tap naming Ada", s)
	}
	rig.Run(chromedp.Click(`form[action="/signin"] button[type="submit"]`, chromedp.ByQuery), chromedp.WaitVisible(`[rst-signin-door] bdi`, chromedp.ByQuery))
	eval(rig, `document.querySelector("[rst-signin-door] bdi").textContent`, &s)
	if s != "ada@example.com" {
		t.Fatalf("the one-tap's Sent page names %q", s)
	}

	// "Use a different email" returns to an empty Ask.
	rig.Run(chromedp.Navigate(rig.Origin+"/signin"), chromedp.WaitVisible(`form[action="/signin/forget"] button`, chromedp.ByQuery),
		chromedp.Click(`form[action="/signin/forget"] button`, chromedp.ByQuery), chromedp.WaitVisible("#rst-signin-email", chromedp.ByQuery))
	eval(rig, `document.getElementById("rst-signin-email").value`, &s)
	if s != "" {
		t.Fatalf("after Use a different email the field holds %q", s)
	}

	// Keymail, scripts on: POST → 303 → the continuation page → the
	// provider, with the page's CSP still form-action 'self'. A CSP
	// violation would be a console error, which the rig fails on.
	rig.Run(
		chromedp.SendKeys("#rst-signin-email", "kay@example.org", chromedp.ByQuery),
		chromedp.Click(`form[action="/signin"] button[type="submit"]`, chromedp.ByQuery),
	)
	went := awaitURL(t, keymail)
	if !strings.HasPrefix(went, "https://keymail.test/oauth/authorize?") || !app.auth.validAuthorizeURL(went) {
		t.Fatalf("the continuation went to %q", went)
	}
	id, csp := conts.last(t)
	if !strings.Contains(csp, "form-action 'self'") || strings.Contains(csp, "keymail.test") {
		t.Fatalf("the continuation page's CSP is %q; the point is that form-action stays 'self'", csp)
	}
	// The fallback link, clicked: the same continuation page with its
	// refresh held back, so the link is what navigates — and it reaches
	// the same URL the refresh did.
	keymail.holdRefresh.Store(true)
	rig.Run(chromedp.Navigate(rig.Origin+"/signin?continue="+id), chromedp.WaitVisible(`[rst-signin-door] a[rst-btn]`, chromedp.ByQuery))
	rig.Screen("[rst-signin]", "the Continue screen")
	rig.Run(chromedp.Click(`[rst-signin-door] a[rst-btn]`, chromedp.ByQuery))
	if byLink := awaitURL(t, keymail); byLink != went {
		t.Fatalf("the fallback link went to %q; the refresh went to %q", byLink, went)
	}
	keymail.holdRefresh.Store(false)

	// Scripts off: the passkey door stays hidden, an address still gets
	// its Sent page, and the keymail continuation still moves on — the
	// meta refresh needs no script.
	rig.Run(emulation.SetScriptExecutionDisabled(true), chromedp.Navigate(rig.Origin+"/signin"), chromedp.WaitVisible("#rst-signin-email", chromedp.ByQuery))
	eval(rig, `document.querySelector("[data-rst-passkey]").hidden`, &ok)
	if !ok {
		t.Fatal("with scripts off the passkey door is showing")
	}
	rig.Run(
		chromedp.SetValue("#rst-signin-email", "sam@example.com", chromedp.ByQuery),
		chromedp.Click(`form[action="/signin"] button[type="submit"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`[rst-signin-door] bdi`, chromedp.ByQuery),
	)
	eval(rig, `document.querySelector("[rst-signin-door] bdi").textContent`, &s)
	if s != "sam@example.com" {
		t.Fatalf("scripts off, Sent names %q", s)
	}
	rig.Run(chromedp.Navigate(rig.Origin+"/signin"), chromedp.WaitVisible("#rst-signin-email", chromedp.ByQuery))
	rig.Run(
		chromedp.SetValue("#rst-signin-email", "kay@example.org", chromedp.ByQuery),
		chromedp.Click(`form[action="/signin"] button[type="submit"]`, chromedp.ByQuery),
	)
	if went := awaitURL(t, keymail); !app.auth.validAuthorizeURL(went) {
		t.Fatalf("scripts off, the continuation went to %q", went)
	}
	rig.Run(emulation.SetScriptExecutionDisabled(false))

	// A passkey signs in from nothing, and the next visit is Returning
	// (passkey) with the typed address gone — kay's attempt cookie from
	// the keymail leg above is still set until the passkey ends it.
	rig.Run(chromedp.Navigate(rig.Origin+"/signin"), chromedp.WaitVisible(`[data-rst-passkey]`, chromedp.ByQuery))
	eval(rig, `document.getElementById("rst-signin-email").value`, &s)
	if s != "kay@example.org" {
		t.Fatalf("before the passkey the field holds %q, want kay's typed address", s)
	}
	// Wait for home itself — the sign-in page has an h1 too — and check
	// the URL and who it says is signed in before leaving it, so the
	// ceremony's cookies are set before the next navigation.
	var here string
	rig.Run(chromedp.Click(`[data-rst-passkey]`, chromedp.ByQuery), chromedp.WaitVisible("#home", chromedp.ByQuery), chromedp.Location(&here))
	eval(rig, `document.getElementById("home").textContent`, &s)
	if here != rig.Origin+"/" || s != "ada@example.com" {
		t.Fatalf("the passkey sign-in landed on %q as %q, want / as ada@example.com", here, s)
	}
	rig.Run(chromedp.Navigate(rig.Origin+"/signin"), chromedp.WaitVisible(`[data-rst-passkey]`, chromedp.ByQuery))
	eval(rig, `document.getElementById("rst-signin-email").value + "|" + document.querySelector("[data-rst-passkey]").textContent.trim()`, &s)
	if want := "|" + catalog["rastrillo.ui.signin_passkey_remembered"]; s != want {
		t.Fatalf("after a passkey sign-in: %q, want %q", s, want)
	}

	// 320px: nothing scrolls sideways, even with a long address shown back.
	rig.Run(chromedp.EmulateViewport(320, 640), chromedp.Navigate(rig.Origin+"/signin"), chromedp.WaitVisible("#rst-signin-email", chromedp.ByQuery),
		chromedp.SetValue("#rst-signin-email", "an-unusually-long-local-part-for-reflow@a-rather-long-domain.example", chromedp.ByQuery),
		chromedp.Click(`form[action="/signin"] button[type="submit"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`[rst-signin-door] bdi`, chromedp.ByQuery))
	eval(rig, `document.documentElement.scrollWidth <= document.documentElement.clientWidth`, &ok)
	if !ok {
		t.Fatal("the Sent page scrolls sideways at 320px")
	}
	rig.Run(chromedp.EmulateViewport(1280, 800))
}

// The passkey door's failure and busy paths, with navigator.credentials
// replaced before any page script runs: a prompt the person dismissed
// (the same NotAllowedError as "no passkey here"), and one that never
// answers, to prove a second click starts nothing.
const passkeyStandIn = `(() => {
  const real = window.fetch.bind(window);
  window.__begins = 0;
  window.fetch = (u, o) => { if (String(u).includes("/passkey/discover/begin")) window.__begins++; return real(u, o); };
  navigator.credentials.get = () => window.__hold
    ? new Promise(() => {})
    : Promise.reject(new DOMException("dismissed", "NotAllowedError"));
})()`

func TestThePasskeyDoorFailsQuietlyAndOnce(t *testing.T) {
	_, build := newScreenApp(t)
	rig := harness.New(t, build)
	rig.Run(chromedp.ActionFunc(func(ctx context.Context) error {
		_, err := page.AddScriptToEvaluateOnNewDocument(passkeyStandIn).Do(ctx)
		return err
	}))
	rig.Run(chromedp.Navigate(rig.Origin+"/signin"), chromedp.WaitVisible(`[data-rst-passkey]`, chromedp.ByQuery))

	var raw string
	eval(rig, `(async () => {
	  const b = document.querySelector("[data-rst-passkey]");
	  const msg = document.getElementById("rst-signin-passkey-msg");
	  b.focus(); b.click();
	  for (let i = 0; i < 100 && !msg.textContent; i++) await new Promise(r => setTimeout(r, 50));
	  return JSON.stringify({msg: msg.textContent, live: msg.getAttribute("aria-live"), want: b.getAttribute("data-rst-passkey-cancelled"),
	    focused: document.activeElement === b, disabled: b.disabled, busy: b.getAttribute("aria-busy")});
	})()`, &raw)
	var got struct {
		Msg, Live, Want string
		Focused, Disabled bool
		Busy             *string
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatal(err)
	}
	if got.Msg == "" || got.Msg != got.Want || got.Live != "polite" || !got.Focused || got.Disabled || got.Busy != nil {
		t.Fatalf("after a dismissed prompt: %+v; want the cancelled words in the live region, focus still on the button, never disabled, no longer busy", got)
	}

	eval(rig, `(async () => {
	  const b = document.querySelector("[data-rst-passkey]");
	  window.__hold = true;
	  const before = window.__begins;
	  b.click(); await new Promise(r => setTimeout(r, 300));
	  b.click(); await new Promise(r => setTimeout(r, 300));
	  return JSON.stringify({started: window.__begins - before, busy: b.getAttribute("aria-busy"), disabled: b.disabled});
	})()`, &raw)
	var busy struct {
		Started  int
		Busy     string
		Disabled bool
	}
	if err := json.Unmarshal([]byte(raw), &busy); err != nil {
		t.Fatal(err)
	}
	if busy.Started != 1 || busy.Busy != "true" || busy.Disabled {
		t.Fatalf("two clicks while busy: %+v; want one ceremony, busy, and the button never disabled", busy)
	}
}
```

- [ ] **Step 2: Run the drives**

Run: `TMPDIR=/var/tmp RASTRILLO_CHROME=/usr/bin/chromium GOFLAGS=-mod=mod go test -tags browser -p 1 ./auth/ -count=1 -v -run 'TestSigninScreenInTheBrowser|TestThePasskeyDoorFailsQuietlyAndOnce'`
Expected: PASS. A failure prints what was on screen; fix the code under test, not the drive, unless the drive misreads a correct page (then say why in the fix's commit).

- [ ] **Step 3: Put the Screens frames and the stage shell under the a11y gates**

`internal/designsystem/a11y_test.go`:

`previewPageKinds` — append `"screens"` after `"primitives"`:

```go
	return append(out, "primitives", "screens")
```

`pickPreviewFrames` — before the component-page branch:

```go
	if kind == "screens" {
		// Every sign-in state, not the first: §5 asks for axe on every
		// screen state in every theme and scheme, and the states differ
		// in exactly what axe checks — a focused callout, a field in
		// error, a one-tap whose name carries an address.
		if len(got.Frames) < 10 {
			t.Fatalf("expected the ten sign-in states and two examples, picked %d", len(got.Frames))
		}
		return got.Frames
	}
```

`a11yTargets` — the `day/en shells` entry's reason becomes "the five page frames, each framed at full page size", the `day/en screens` entry's reason becomes "the sign-in screens: the shipped partial in ten states in stage frames, and the two examples it does not ship — the only page carrying a password field", and add after the console shell's entry:

```go
		{"day/en stage shell", shellHref(mountPath, "day", "en", "stage"), "the sign-in shell: a generated backdrop behind one card, no navigation at all — the page every visitor meets first, and the only one whose main landmark sits over a decorative picture"},
```

`TestA11yReflowsAt320` — append to `pages`:

```go
		struct{ name, href string }{"day/en stage shell", shellHref(mountPath, "day", "en", "stage")},
```

**Every state, not the gallery's ten.** The Screens page shows ten states to stay under its byte budget, so `TestA11yScansEverySigninState` renders the whole matrix on pages that exist only for the scan, through the gallery's own frame template, and scans each in every theme and both schemes. Append to `internal/designsystem/a11y_test.go` (it is `//go:build browser`; add `"amadan.net/rastrillo/rastrillo/auth"` and `"strings"` to its imports if absent):

```go
// signinMatrix is every state the signin partial can be in — each step,
// each problem, each remembered method, with and without a passkey
// door, and Reauth over every door — for the axe scan. The Screens page
// shows ten; the rest are rendered here only.
func signinMatrix() map[string]auth.SigninState {
	door := galleryPasskey
	kay := &auth.Remembered{Method: "keymail", Address: "kay@example.org"}
	ada := &auth.Remembered{Method: "magiclink", Address: graceAddress}
	pk := &auth.Remembered{Method: "passkey"}
	s := func(mut func(*auth.SigninState)) auth.SigninState { return *signinScreen(mut) }
	return map[string]auth.SigninState{
		"ask":                        s(func(st *auth.SigninState) {}),
		"ask-door":                   s(func(st *auth.SigninState) { st.Passkey = door }),
		"returning-keymail":          s(func(st *auth.SigninState) { st.Step, st.Remembered = auth.StepReturning, kay }),
		"returning-link":             s(func(st *auth.SigninState) { st.Step, st.Remembered = auth.StepReturning, ada }),
		"returning-passkey":          s(func(st *auth.SigninState) { st.Step, st.Remembered, st.Passkey = auth.StepReturning, pk, door }),
		"returning-passkey-no-door":  s(func(st *auth.SigninState) { st.Step, st.Remembered = auth.StepReturning, pk }),
		"sent-bound":                 s(func(st *auth.SigninState) { st.Step, st.SentTo = auth.StepSent, graceAddress }),
		"sent-unbound":               s(func(st *auth.SigninState) { st.Step = auth.StepSent }),
		"sent-instead":               s(func(st *auth.SigninState) { st.Step, st.SentTo, st.SentInstead = auth.StepSent, kay.Address, true }),
		"continue":                   s(func(st *auth.SigninState) { st.Step, st.ContinueURL = auth.StepContinue, "https://keymail.example/oauth/authorize" }),
		"problem-rate":               s(func(st *auth.SigninState) { st.Problem, st.Address = auth.ProblemRate, graceAddress }),
		"problem-address":            s(func(st *auth.SigninState) { st.Problem, st.Address = auth.ProblemAddress, "grace@example" }),
		"problem-expired":            s(func(st *auth.SigninState) { st.Problem = auth.ProblemExpired }),
		"problem-keymail":            s(func(st *auth.SigninState) { st.Problem, st.Address = auth.ProblemKeymail, kay.Address }),
		"problem-generic":            s(func(st *auth.SigninState) { st.Problem, st.Passkey = auth.ProblemGeneric, door }),
		"reauth-ask":                 s(func(st *auth.SigninState) { st.Problem = auth.ProblemReauth }),
		"reauth-returning-keymail":   s(func(st *auth.SigninState) { st.Step, st.Problem, st.Remembered = auth.StepReturning, auth.ProblemReauth, kay }),
		"reauth-returning-link":      s(func(st *auth.SigninState) { st.Step, st.Problem, st.Remembered = auth.StepReturning, auth.ProblemReauth, ada }),
		"reauth-returning-passkey":   s(func(st *auth.SigninState) { st.Step, st.Problem, st.Remembered, st.Passkey = auth.StepReturning, auth.ProblemReauth, pk, door }),
	}
}

// TestA11yScansEverySigninState is §5's "axe WCAG 2.2 AA on every
// screen state in every theme × scheme". The pages are Preview renders
// (so the passkey door shows as it would once revealed) in the same
// stage frame the gallery uses, served beside the gallery tree so they
// load its tokens.css and themes.
func TestA11yScansEverySigninState(t *testing.T) {
	tmpl, err := partialTree("en")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tmpl.Parse(screenFrame); err != nil {
		t.Fatal(err)
	}
	pages := map[string][]byte{}
	for _, theme := range ui.ThemeNames() {
		for name, st := range signinMatrix() {
			var b strings.Builder
			if err := tmpl.ExecuteTemplate(&b, "ds-screen-stage", map[string]any{"State": st, "Brand": galleryBrand, "Preview": true, "Art": true}); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			pages["/signin-matrix/"+theme+"/"+name+".html"] = []byte(srcdoc(mountPath, theme, "en", "Sign-in state "+name, b.String()))
		}
	}
	rig := harness.New(t, func(string) http.Handler {
		tree := treeHandler(t)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if body, ok := pages[r.URL.Path]; ok {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.Write(body)
				return
			}
			tree.ServeHTTP(w, r)
		})
	})
	ctx, cancel := context.WithTimeout(rig.Context(), 900*time.Second)
	defer cancel()
	axeJS := axeSource(t)
	total := 0
	for href := range pages {
		for _, scheme := range a11ySchemes {
			where := href + " (" + scheme + ")"
			if err := chromedp.Run(ctx, chromedp.Navigate(rig.Origin+href), chromedp.WaitReady("body"), chromedp.Evaluate(axeJS, nil)); err != nil {
				t.Fatalf("%s: loading: %v", where, err)
			}
			paint(t, ctx, scheme)
			total += report(t, where, scan(t, ctx, where, "window.axe", "document", "false"))
		}
	}
	if total == 0 {
		t.Logf("clean: %d sign-in states × %d themes × %d schemes", len(signinMatrix()), len(ui.ThemeNames()), len(a11ySchemes))
	}
}
```

`Makefile` — the browser target's package list becomes `./harness/ ./webauthn/ ./ui/ ./pow/ ./internal/designsystem/ ./auth/`, and its comment gains "and the sign-in screen's whole journey".

- [ ] **Step 4: Measure the preview heights**

Run: `TMPDIR=/var/tmp RASTRILLO_CHROME=/usr/bin/chromium GOFLAGS=-mod=mod go test -tags browser -p 1 ./internal/designsystem/ -count=1 -v -run 'TestPreviewFrameHeightsFitTheirContent'`
Expected: PASS, with slack logged per frame. For any `screen-signin-*` or `shell-stage` frame it reports as too small, set `previewHeights` (desktop) or add a `previewMobileHeights` entry (the stacked phone layout is the taller one) to the measured height rounded up to the next 10 px plus 20, and re-run until it passes.

- [ ] **Step 5: Run the gallery's and ui's browser suites**

Run: `TMPDIR=/var/tmp RASTRILLO_CHROME=/usr/bin/chromium GOFLAGS=-mod=mod go test -tags browser -p 1 ./internal/designsystem/ ./ui/ -count=1`

This includes `TestA11yScansEverySigninState` — nineteen states × three themes × two schemes.
Expected: PASS — `TestA11yScansTheGallery`, `TestA11yScansThePreviewDocuments` (now including every sign-in frame in day, plain and signal, light and dark, and day/ar), `TestA11yReflowsAt320`, `TestA11yWalksTheKeyboard`, and ui's shell drives with `stage` among `LayoutNames()`. An axe violation in a sign-in frame is a defect in the partial or its CSS: fix it there.

- [ ] **Step 6: Run the gate and commit**

Run: `GOFLAGS=-mod=mod go vet ./... && gofmt -l . && RASTRILLO_TEST_REQUIRE_NODE=1 GOFLAGS=-mod=mod go test ./... -count=1`

```bash
git add auth/signinscreen_browser_test.go internal/designsystem Makefile
git commit -F - <<'EOF'
Browser drives for the sign-in screen, and axe on every state

The screen's promises are about a real browser: that a keymail sign-in
leaves the form under form-action 'self' and still reaches the
provider, with scripts on and off; that the one-tap posts the
remembered address; that a passkey sign-in clears a typed address; that
a dismissed passkey prompt is announced with focus still on the button,
and a second click while busy starts nothing. The drive holds all of
it against the framework's own CSP, with the provider intercepted so
nothing real is contacted.

The gallery's accessibility scan now covers every sign-in state in
every theme and scheme, and the stage shell at full page and at 320px.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
```

---
### Task 14: Copy review, batch 2 — the docs and the CHANGELOG; SKILL.md

The docs and the CHANGELOG go to the operator before they are written, as batch 1's strings did (Task 10). They are a separate batch because they describe the finished feature, so they are drafted last; nothing in this batch is a translation key, so nothing is translated. SKILL.md is read by an LLM, not a person, so it is not reviewed; it is written here because it describes the same feature.

**Files:**
- Create (untracked, `.gitignore`d): `copy-review/strings.json`; `docs/site/reference/lastsignin.md`
- Modify: `docs/site/magic-links.md`, `docs/site/passkeys.md`, `docs/site/templates.md`, `docs/site/reference/{auth,passkey,ui}.md`, `docs/site/nav.json`, `CHANGELOG.md`, `SKILL.md`

**Interfaces:**
- Consumes: the API names from Tasks 2–12 the docs describe. Task 12 deleted batch 1's `copy-review/`, so this batch starts a fresh index.
- Produces: nothing code depends on.

- [ ] **Step 1: Write the string index**

Write `copy-review/strings.json` exactly as below. Each `id` names where the approved text goes (the table in Step 4). Order is the order a reader meets them.

```json
[
  {"id": "docs/site/magic-links.md#intro", "section": "Docs: Magic links", "label": "Opening paragraph, second sentence", "text": "It is the framework's turnkey option: you get the whole flow — link minting, single-use redemption, rate limiting, sessions, CSRF — and a sign-in screen to use as it is or replace with your own.", "context": "The second paragraph of the Magic links guide. It used to end and you keep your own sign-in page, which is no longer the only option."},
  {"id": "docs/site/magic-links.md#screen-heading", "section": "Docs: Magic links", "label": "Section heading", "text": "Use the shipped screen, or your own", "context": "Heading of the section that replaces The sign-in page stays yours. Other pages link to it by its slug.", "notes": "If reworded, the link targets #use-the-shipped-screen-or-your-own in reference/auth.md, reference/ui.md and the CHANGELOG change with it."},
  {"id": "docs/site/magic-links.md#screen-what", "section": "Docs: Magic links", "label": "What the screen is", "text": "Turn on `SigninScreen` and render ui's `signin` partial, and your app has a sign-in page on its first day: one field for an address, the right thing happening next whether that address gets a link or Keymail, and a one-tap for someone coming back.", "context": "First paragraph of the section, before a code sample showing the configuration and the page handler.", "notes": "Markdown; keep the backticked names."},
  {"id": "docs/site/magic-links.md#screen-turns-on", "section": "Docs: Magic links", "label": "What the switch turns on", "text": "`SigninScreen` turns on everything the screen needs from `auth`, together: a short-lived cookie that remembers what was typed, so the page can refill the field after a problem and name the address a link went to; a keymail sign-in that stays on your page instead of leaving the form, so the default CSP's `form-action 'self'` needs no widening; and a cookie that remembers which way this browser got in last. Leave it off and nothing changes: no new cookie, and `Begin` and `Callback` answer exactly as before.", "context": "After the code sample. Developers deciding whether to turn it on need to know what they are agreeing to.", "notes": "Markdown."},
  {"id": "docs/site/magic-links.md#screen-remember", "section": "Docs: Magic links", "label": "Turning remembering off", "text": "Set `Remember` to a pointer to `false` to keep the screen and stop remembering — for a kiosk or a shared computer. It also deletes anything remembered before. The remembered cookie is a hint for the screen and nothing else: it signs nobody in, and a one-tap is checked from scratch like a typed address.", "context": "Next paragraph.", "notes": "Markdown."},
  {"id": "docs/site/magic-links.md#screen-passkeys", "section": "Docs: Magic links", "label": "With passkeys", "text": "If you use passkeys, serve `passkey.JS()` beside `webauthn.JS()` and set both addresses on the page's `Passkey` door, as above; the screen loads the passkey script only when it shows the door. Pass `a.RememberJar()` as `passkey.Config.Remember`: a passkey sign-in then clears an address typed earlier in the same browser, and the next visit offers the passkey first.", "context": "Next paragraph.", "notes": "Markdown."},
  {"id": "docs/site/magic-links.md#screen-stage", "section": "Docs: Magic links", "label": "The stage shell", "text": "The `stage` shell is made for this page: one card over a generated backdrop. Redefine its `backdrop` block for a pattern or a picture of your own.", "context": "Last paragraph before the table of outcome addresses.", "notes": "Markdown."},
  {"id": "docs/site/magic-links.md#outcomes-lead", "section": "Docs: Magic links", "label": "Outcome table lead", "text": "`Begin` and the completion handlers report outcomes by redirecting to `Config.SigninPath`. The shipped screen reads these for you; a page of your own renders them:", "context": "Introduces the table of query strings the sign-in page receives.", "notes": "Markdown."},
  {"id": "docs/site/magic-links.md#outcomes-table", "section": "Docs: Magic links", "label": "Outcome table", "text": "| Query | Meaning |\n|---|---|\n| `?sent=1` | a link was emailed — or the address was refused; the page cannot tell which |\n| `?sent=1&attempt=<id>` | the same, with the screen on: the id lets the page name the address |\n| `?continue=<id>` | a keymail sign-in is under way; the screen moves on by itself |\n| `?err=rate` | rate limited |\n| `?err=address` | the address was rejected |\n| `?err=expired` | the link had expired or was already used |\n| `?err=1` | something else went wrong |\n| `?reauth=1` | a fresh sign-in is needed to continue |", "context": "The table itself. Only the Meaning column is prose.", "notes": "Markdown table; keep the first column exactly."},
  {"id": "docs/site/magic-links.md#wrapper-heading", "section": "Docs: Magic links", "label": "Sub-heading", "text": "An admission check in front of Begin", "context": "A sub-heading in the same section, for apps that refuse non-members before any mail goes out. reference/auth.md links to it by slug.", "notes": "If reworded, update the #an-admission-check-in-front-of-begin link in reference/auth.md."},
  {"id": "docs/site/magic-links.md#wrapper", "section": "Docs: Magic links", "label": "Admission checks", "text": "If you put an admission check in front of `Begin` — refusing addresses that are not members before any mail goes out — answer a refusal with `a.AnswerAsSent(w, r)`, not a redirect of your own. With the screen on, a sent link leaves a cookie and an `attempt=` behind; a plain `?sent=1` for a refusal would look different on the very first try, and anyone could learn who is a member. `AnswerAsSent` answers exactly as a sent link does and sends nothing, so the page and the cookies give nothing away. It does not stop the per-address rate limit or keymail classification from revealing something about an address; that is separate work, and this does not do it.", "context": "The paragraph under that sub-heading. Written for the developer of an invite-only app.", "notes": "Markdown."},
  {"id": "docs/site/magic-links.md#keymail-servers", "section": "Docs: Magic links", "label": "Which keymail servers are trusted", "text": "By default any keymail server an address's own domain names is trusted. That is keymail's design: whoever controls a domain chooses its server, the same person who controls its mail and could receive a link anyway, and a server cannot vouch for anyone else's address, because the address it returns must match the one that was typed. To trust a closed set instead, list them in `KeymailServers` (host or host:port). An address whose server is not listed gets an ordinary link, and its server is never contacted — not to check the address, and not to finish a sign-in. The list applies with the screen on or off.", "context": "A new last paragraph of the Aside: the keymail upgrade section.", "notes": "Markdown."},
  {"id": "docs/site/passkeys.md#allowed-1", "section": "Docs: Passkeys", "label": "What a passkey may do, first paragraph", "text": "A passkey can sign someone in on its own, through the discover endpoints. A passkey that checked the person with a PIN, a fingerprint or a face has already proved who they are and what they hold, and asking for an emailed link as well adds work and no safety.", "context": "Replaces A passkey never signs anybody in from nothing, which stopped being true when discovery shipped."},
  {"id": "docs/site/passkeys.md#allowed-2", "section": "Docs: Passkeys", "label": "What a passkey may do, second paragraph", "text": "It also refreshes an existing session at step-up, and completes a sign-in whose first factor already checked out. A passkey that only found somebody present, without checking who, is the weaker proof: where the account has another factor, discover holds the sign-in for it.", "context": "Replaces the paragraph that followed."},
  {"id": "docs/site/passkeys.md#remember", "section": "Docs: Passkeys", "label": "On the sign-in screen", "text": "On `auth`'s shipped sign-in screen, the discover pair is the passkey button. Set `Config.Remember` to `auth`'s `RememberJar()`: a passkey sign-in then clears any address typed earlier in the same browser, and the next visit offers the passkey first. Without it, an address someone typed before using their passkey stays in the form.", "context": "A new paragraph after the list of endpoints in Wiring it.", "notes": "Markdown."},
  {"id": "docs/site/templates.md#funcs", "section": "Docs: Templates", "label": "Template functions list", "text": "`ui.Funcs()` registers `dict`, `list`, `menuGroup`, `searchClear`, `icon`, `iconAssets`, `T`, `Tf`, `dateWords`, `opt`, `Tbdi` and `stageArt`.", "context": "The first line under Template functions.", "notes": "Markdown."},
  {"id": "docs/site/templates.md#blocks", "section": "Docs: Templates", "label": "Shell blocks", "text": "The blocks are `title`, `lang`, `dir` and `head` in all five shells, plus `brand`, `nav`, `account` and `locale` in `topbar`, `sidebar` and `console`, `foot` in `topbar`, `console` and `stage`, and `backdrop` in `stage`. None of them reads a field off the data, so a shell renders whether your handler passes a struct, a `dict`-built map or nil — a shell can never break because a page's view model changed shape.", "context": "Replaces the paragraph that lists the shells' blocks for four shells.", "notes": "Markdown."},
  {"id": "docs/site/templates.md#stage", "section": "Docs: Templates", "label": "The stage shell", "text": "`stage` has no chrome. It centres one card — the sign-in screen, usually — over a full-page backdrop, drawn by `{{stageArt \"rastrillo\"}}` unless you redefine `backdrop`: `{{define \"backdrop\"}}{{stageArt \"your-app\"}}{{end}}` draws a pattern of your own from any word, and an `<img>` or your own SVG replaces it outright. Its attributes are `rst-stage`, `rst-stage-scene`, `rst-stage-art` and `rst-stage-foot`, and it carries `rst-skip` like the others.", "context": "A new paragraph after the one listing the chrome attributes (which ends with the skip link that all shells carry).", "notes": "Markdown; keep the template code exactly."},
  {"id": "docs/site/reference/auth.md#screen-config", "section": "Docs: auth reference", "label": "The screen's settings", "text": "`SigninScreen` turns on the shipped sign-in screen's side of `auth`: the attempt and continuation cookies, the keymail continuation, and remembering the way in. Off, nothing about `Begin` or `Callback` changes. `BeginPath` and `ForgetPath` (default `/signin` and `/signin/forget`) are where you mounted `Begin` and `Forget`, for the screen's forms. `Remember` set to `false` keeps the screen and stops remembering.", "context": "After the Config code block, before the InstanceKey paragraph.", "notes": "Markdown."},
  {"id": "docs/site/reference/auth.md#keymail-servers", "section": "Docs: auth reference", "label": "KeymailServers", "text": "`KeymailServers` limits keymail to the servers you list, both when an address is checked and when a sign-in finishes; an unlisted server's addresses get a link. See [Magic links](/docs/magic-links#aside-the-keymail-upgrade).", "context": "After the TrustedProxyHops paragraph.", "notes": "Markdown."},
  {"id": "docs/site/reference/auth.md#handlers", "section": "Docs: auth reference", "label": "What the handlers report", "text": "These handlers report outcomes by redirecting to `SigninPath`: `?sent=1`, `?err=rate|address|expired|1`, `?err=keymail` with `?force=1` after a failed keymail approval, and — with `SigninScreen` on — `?sent=1&attempt=<id>` and `?continue=<id>`. The shipped screen reads them through `SigninState`; a page of your own renders them itself.", "context": "Replaces The sign-in page stays yours paragraph under The handlers.", "notes": "Markdown."},
  {"id": "docs/site/reference/auth.md#screen-heading", "section": "Docs: auth reference", "label": "Section heading", "text": "The sign-in screen", "context": "A new section heading after The handlers."},
  {"id": "docs/site/reference/auth.md#state", "section": "Docs: auth reference", "label": "SigninState and PrepareSigninResponse", "text": "`SigninState` reads the query and this browser's own cookies and returns what the page shows, as plain data. It consults nothing else, so the page cannot reveal whether an address is known. Set `Passkey` on the result if you mounted passkey discovery. `PrepareSigninResponse` writes what goes with it: `Cache-Control: no-store`, `Referrer-Policy: no-referrer` on the page that moves on to Keymail, and deletions for any cookie that could not be trusted. Call it before rendering. With `SigninScreen` off, `SigninState` reads only the query and logs one warning per process.", "context": "After a code block of the new signatures.", "notes": "Markdown."},
  {"id": "docs/site/reference/auth.md#forget", "section": "Docs: auth reference", "label": "Forget, AnswerAsSent, RememberJar", "text": "`Forget` is the Use a different email button: POST only, same-origin only, it forgets the remembered way in and redirects to `SigninPath`. `AnswerAsSent` is `Begin`'s answer for a sent link, without the link, for an admission check in front of `Begin` — see [Magic links](/docs/magic-links#an-admission-check-in-front-of-begin). `RememberJar` is the jar that remembers the way in; give it to `passkey.Config.Remember`.", "context": "Last paragraph of that section.", "notes": "Markdown."},
  {"id": "docs/site/reference/passkey.md#remember", "section": "Docs: passkey reference", "label": "Config.Remember", "text": "`Config.Remember` takes `auth`'s `RememberJar()`. With it, a verified discover assertion ends the sign-in screen's attempt and remembers passkey as this browser's way in, with no address.", "context": "A new last paragraph of Discover: the passkey as the front door.", "notes": "Markdown."},
  {"id": "docs/site/reference/ui.md#funcs", "section": "Docs: ui reference", "label": "Funcs list and the three new ones", "text": "Registers `dict`, `list`, `menuGroup`, `searchClear`, `icon`, `iconAssets`, `T`, `Tf`, `dateWords`, `opt`, `Tbdi` and `stageArt`.\n\n`opt` reads an optional key off a partial's data, whether it is a map or a struct, and gives nil when it is missing. `Tbdi` is `Tf` for a sentence that shows back something a visitor typed: each value is escaped and wrapped in `<bdi>`. `stageArt` draws the stage shell's backdrop from a word; the same word always draws the same picture.", "context": "Replaces the one-line list under Funcs.", "notes": "Markdown; two paragraphs."},
  {"id": "docs/site/reference/ui.md#shells", "section": "Docs: ui reference", "label": "The stage shell", "text": "`stage` is one card centred over a full-page backdrop, for a page that stands alone — the sign-in screen above all. Its blocks are `title`, `lang`, `dir`, `head`, `backdrop` and `foot`.", "context": "A new paragraph in the Shells section, after the one describing the other four. That section's first words become The five shipped page frames.", "notes": "Markdown."},
  {"id": "docs/site/reference/ui.md#signin", "section": "Docs: ui reference", "label": "The signin partial", "text": "`signin` renders the whole sign-in card from an `auth.SigninState`, and `signin-title` the matching tab title. Pass `Brand` with a `Name`, and optionally a one-line `Pitch` and a `Mark` (an `<img>` or inline SVG as `template.HTML`). Every string comes from the base catalogs under `rastrillo.ui.signin_*`, in all twelve languages. The passkey button loads `passkey.JS()` from the `ScriptURL` you set, and only on a page that shows it; without that script it stays hidden, and the email form works as ever. See [Magic links](/docs/magic-links#use-the-shipped-screen-or-your-own).", "context": "A new section, The sign-in screen, before Styleguide.", "notes": "Markdown."},
  {"id": "docs/site/reference/lastsignin.md#lead", "section": "Docs: lastsignin reference (new page)", "label": "Lead", "text": "Remembers, in one browser, which way it last used to sign in — keymail, an emailed link or a passkey — and the address for the first two, so the sign-in screen can offer a one-tap. It is a hint for the screen and nothing more: it signs nobody in, and a one-tap is checked from scratch.", "context": "The opening paragraph of a new reference page for a small package developers rarely call directly."},
  {"id": "docs/site/reference/lastsignin.md#usually", "section": "Docs: lastsignin reference (new page)", "label": "You rarely build one", "text": "You rarely build one yourself. `auth.New` builds the jar from its own configuration, and `auth.RememberJar` hands it to `passkey.Config.Remember`.", "context": "Second paragraph, before the API listing.", "notes": "Markdown."},
  {"id": "docs/site/reference/lastsignin.md#modes", "section": "Docs: lastsignin reference (new page)", "label": "Modes", "text": "`Off` writes, reads and deletes nothing — an app without the shipped screen sees no cookie at all. `Forgetting` deletes what was remembered and never reads it. `On` remembers. `EndAttempt` deletes the screen's attempt cookie; every sign-in path calls it once a first factor checks out.", "context": "After the API listing.", "notes": "Markdown."},
  {"id": "docs/site/reference/lastsignin.md#cookie", "section": "Docs: lastsignin reference (new page)", "label": "The cookie", "text": "The cookie is sealed, HttpOnly, and lasts 400 days, the longest browsers allow; each sign-in renews it. Signing out does not delete it.", "context": "Last paragraph."},
  {"id": "docs/site/nav.json#lastsignin", "section": "Docs: lastsignin reference (new page)", "label": "Docs index blurb", "text": "The sign-in screen's memory of how this browser got in last, and the seam that ends a sign-in attempt.", "context": "The one-line description of the new page on the docs index."},
  {"id": "CHANGELOG.md#added-heading", "section": "CHANGELOG", "label": "Entry heading", "text": "Added — a sign-in screen, and a browser that remembers how you got in; re-vendor `tokens.css`", "context": "A new entry at the top of Unreleased. Headings in this file start with Added, Changed or Fixed and name the one thing to do if there is one.", "notes": "Written after ### ."},
  {"id": "CHANGELOG.md#added-1", "section": "CHANGELOG", "label": "What it is", "text": "`ui` ships a sign-in screen: the `signin` and `signin-title` partials, and a `stage` shell to put them in, with a generated backdrop (`stageArt`) you can replace. It asks for an address and then does the right thing for it — an emailed link, or Keymail — and offers a one-tap to someone coming back, a passkey button where you have passkeys, and plain words for every problem. Every string is in all twelve languages.", "context": "First paragraph of the entry.", "notes": "Markdown."},
  {"id": "CHANGELOG.md#added-2", "section": "CHANGELOG", "label": "How to use it", "text": "Turn it on with `auth.Config.SigninScreen`, render the page from `auth.SigninState` and `PrepareSigninResponse`, and mount `auth.Forget`. With it on, a keymail sign-in stays on your page, so the default CSP's `form-action 'self'` needs no widening. See [Magic links](/docs/magic-links#use-the-shipped-screen-or-your-own).", "context": "Second paragraph.", "notes": "Markdown."},
  {"id": "CHANGELOG.md#added-3", "section": "CHANGELOG", "label": "Everything else new", "text": "Also new: `auth.AnswerAsSent`, for an admission check in front of `Begin`; `auth.Config.KeymailServers`, to trust only the keymail servers you list; `BeginPath`, `ForgetPath` and `Remember` on `auth.Config`; `passkey.Config.Remember` and `passkey.JS()`, the passkey button's script; the `lastsignin` package; `QuietError` on the `field` partial; `ID` and `Focus` on the `callout` partial; and the `opt` and `Tbdi` template functions.", "context": "Third paragraph: the full list, for someone scanning for a name.", "notes": "Markdown."},
  {"id": "CHANGELOG.md#added-4", "section": "CHANGELOG", "label": "What changes for existing apps", "text": "An app that leaves `SigninScreen` off sees no change: no new cookie, and `Begin` and `Callback` answer as before. `KeymailServers`, if you set it, applies either way. Turning the screen on turns on the keymail continuation and remembering together; set `Remember` to false to keep the screen and stop remembering.", "context": "Fourth paragraph. The file's rule: anything that could change an app's behaviour is said plainly.", "notes": "Markdown."},
  {"id": "CHANGELOG.md#added-5", "section": "CHANGELOG", "label": "Re-vendoring", "text": "The screen's styles are in `tokens.css`, which your app has its own copy of. Run `rastrillo doctor --fix` to take the new one.", "context": "Last paragraph, the action the heading promises.", "notes": "Markdown."}
]
```

Validate it: `python3 -m json.tool copy-review/strings.json > /dev/null` — no output means valid.

- [ ] **Step 2: Run the review**

Invoke the `copy-review` skill (Skill tool, `skill: "copy-review"`) and follow it exactly: launch `serve.sh copy-review/strings.json` with the Bash sandbox off (it exits 3 inside the sandbox), give the operator the one URL it prints, and poll for `copy-review/result.json`. On `"action": "reroll"`, rewrite as the skill says — carrying the operator's edited strings over unchanged and applying their demonstrated edits to the rest — and relaunch. Loop until `"action": "approve"`. Do not write any of these strings into a tracked file before then.

- [ ] **Step 3: Apply the approved strings, verbatim**

For every entry in `result.json`, the approved `text` goes where its `id` says, byte for byte — no fixed typos, no changed capitalisation. If a string would break markup (an unbalanced backtick, a lost `{name}`), stop and ask the operator; never repair it silently.

| `id` | Destination |
|---|---|
| `docs/site/magic-links.md#intro` | replaces the paragraph beginning "It is the framework's turnkey option" |
| `docs/site/magic-links.md#screen-*`, `#outcomes-*`, `#wrapper*` | the new section, in this order, replacing the whole `### The sign-in page stays yours` section: `### <#screen-heading>`, `#screen-what`, the two code blocks in Step 4, `#screen-turns-on`, `#screen-remember`, `#screen-passkeys`, `#screen-stage`, `#outcomes-lead`, `#outcomes-table`, `#### <#wrapper-heading>`, `#wrapper` |
| `docs/site/magic-links.md#keymail-servers` | a new last paragraph of `## Aside: the keymail upgrade` |
| `docs/site/passkeys.md#allowed-1`, `#allowed-2` | replace the two paragraphs under `## What a passkey is allowed to do` |
| `docs/site/passkeys.md#remember` | after the endpoint list in `## Wiring it`, which gains the two discover lines in Step 4 |
| `docs/site/templates.md#funcs` | replaces line 24–25's sentence |
| `docs/site/templates.md#blocks` | replaces the paragraph beginning "The blocks are `title`, `lang`, `dir` and `head` in all four shells" |
| `docs/site/templates.md#stage` | a new paragraph after the one ending "like every other idiom here." (the chrome-attributes paragraph), whose "which all four shells carry — `column` included" becomes "which all five shells carry — `column` and `stage` included" |
| `docs/site/reference/auth.md#…` | as each entry's context says; the Config block and signatures are in Step 4 |
| `docs/site/reference/passkey.md#remember` | new last paragraph of `## Discover: the passkey as the front door` |
| `docs/site/reference/ui.md#funcs` | replaces the "Registers `dict`, …" line under `## Funcs` |
| `docs/site/reference/ui.md#shells` | a new paragraph in `## Shells`; "The four shipped page frames" becomes "The five shipped page frames" and "in all four" becomes "in all five" |
| `docs/site/reference/ui.md#signin` | a new `## The sign-in screen` section before `## Styleguide` |
| `docs/site/reference/lastsignin.md#…` | the new page, laid out in Step 4 |
| `docs/site/nav.json#lastsignin` | the `blurb` of the new nav entry in Step 4 |
| `CHANGELOG.md#…` | a new entry at the top of `## Unreleased`: `### <#added-heading>` then `#added-1` … `#added-5` as paragraphs |

If an approved docs heading differs from the draft, change every link that targets its slug (the table's notes name them).

- [ ] **Step 4: Write the code blocks and structure around the approved prose**

These are code, not copy, and go in exactly as written.

`docs/site/magic-links.md`, after `#screen-what`:

````markdown
```go
a, err := auth.New(auth.Config{
	DB:           writer,
	Origin:       origin,
	InstanceKey:  instanceKey,
	Mailer:       mailer,
	SigninScreen: true,
})

r.Get("/signin", func(w http.ResponseWriter, r *http.Request) {
	st := a.SigninState(r)
	st.Passkey = &auth.PasskeyDoor{ // only if you mounted passkey discovery
		BeginPath:  "/passkey/discover/begin",
		FinishPath: "/passkey/discover/finish",
		ModuleURL:  "/passkey/webauthn.mjs", // serves webauthn.JS()
		ScriptURL:  "/passkey/signin.mjs",   // serves passkey.JS()
	}
	a.PrepareSigninResponse(w, st)
	render(w, "signin", map[string]any{"Signin": st, "Brand": map[string]any{"Name": "Harbour"}})
})
r.Post("/signin/forget", a.Forget)
r.Get("/passkey/webauthn.mjs", serveJS(webauthn.JS()))
r.Get("/passkey/signin.mjs", serveJS(passkey.JS()))

// serveJS serves an embedded module with the type a browser requires.
func serveJS(b []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Write(b)
	}
}
```

`templates/signin.html`, in a page set built on the `stage` shell instead of `layout.html`:

```html
{{define "title"}}{{template "signin-title" (dict "State" .Signin "Brand" .Brand)}}{{end}}
{{define "content"}}{{template "signin" (dict "State" .Signin "Brand" .Brand)}}{{end}}
```

```go
stage, _ := ui.Layout("stage")
t := template.Must(template.New("layout").
	Funcs(ui.Funcs(ui.WithIcons(icons.Icon, icons.Assets))).
	Funcs(template.FuncMap{"asset": assets.Path}).
	ParseFS(ui.Templates(), "*.html"))
t = template.Must(t.Parse(string(stage)))
pages["signin"] = template.Must(t.ParseFS(appFS, "templates/signin.html"))
```
````

`docs/site/passkeys.md` — the endpoint list in `## Wiring it` gains:

```text
POST /passkey/discover/begin    -> {"challenge": ...}
POST /passkey/discover/finish   <- authenticate()'s result -> {"to": ...}
```

`docs/site/reference/auth.md` — the `Config` block gains, after `TrustedProxyHops *int`:

```go
	SigninScreen     bool
	BeginPath        string
	ForgetPath       string
	Remember         *bool
	KeymailServers   []string
```

the handler table gains `POST /signin/forget -> Auth.Forget`, and the new `## <#screen-heading>` section opens with:

```go
func (a *Auth) SigninState(r *http.Request) SigninState
func (a *Auth) PrepareSigninResponse(w http.ResponseWriter, st SigninState)
func (a *Auth) Forget(w http.ResponseWriter, r *http.Request)
func (a *Auth) AnswerAsSent(w http.ResponseWriter, r *http.Request)
func (a *Auth) RememberJar() *lastsignin.Jar
```

`docs/site/reference/lastsignin.md`:

````markdown
# 🤖 lastsignin

`amadan.net/rastrillo/rastrillo/lastsignin`

<#lead>

<#usually>

```go
func New(cfg Config) (*Jar, error)
type Config struct {
	Origin, InstanceKey, AttemptCookie string
	Mode                               Mode // Off, Forgetting, On
	Now                                func() time.Time
}
func (j *Jar) Remember(w http.ResponseWriter, rec Record)
func (j *Jar) Read(r *http.Request) (Record, ReadResult) // Absent, Valid, Invalid
func (j *Jar) Clear(w http.ResponseWriter)
func (j *Jar) EndAttempt(w http.ResponseWriter)
type Record struct{ Method, Address string }
```

<#modes>

<#cookie>
````

where each `<#…>` is that entry's approved text.

`docs/site/nav.json` — after the `reference/keyring` entry:

```json
        {
          "slug": "reference/lastsignin",
          "label": "lastsignin",
          "blurb": "<nav.json#lastsignin>"
        },
```

- [ ] **Step 5: SKILL.md (not reviewed; LLM-facing)**

Line 32: `--shell=column|topbar|sidebar|console` becomes `--shell=column|topbar|sidebar|console|stage`.

Replace the `**CSP:**` paragraph (lines 287–295) with:

```markdown
**Sign-in screen:** set `auth.Config.SigninScreen: true` and render
`{{template "signin" (dict "State" .Signin "Brand" .Brand)}}` (title:
`signin-title`) in the `stage` shell, from `st := a.SigninState(r)` —
set `st.Passkey` if passkey discovery is mounted — then
`a.PrepareSigninResponse(w, st)` before rendering. Mount
`POST /signin/forget` → `a.Forget`; with passkeys, serve `passkey.JS()`
beside `webauthn.JS()`, set both URLs (`ScriptURL`, `ModuleURL`) on
`st.Passkey`, and wire `passkey.Config.Remember: a.RememberJar()`. An admission check in front
of `Begin` answers refusals with `a.AnswerAsSent(w, r)`, never its own
`?sent=1`.

**CSP:** `form-action 'self'` covers a form's whole redirect chain, so
`Begin`'s 303 to a keymail server is refused. With `SigninScreen` on,
keymail continues on your own page and `form-action` stays `'self'`.
Only a hand-built sign-in page with the screen off restates
`Options.CSP` (it replaces the policy wholesale) with the keymail
origin appended: `default-src 'self'; style-src 'self' 'unsafe-hashes'
'sha256-yJxAE4rjdcckohdlnvecSporPcqS9xOaA4hJxi87LMc='; img-src 'self'
data:; frame-ancestors 'none'; base-uri 'self'; form-action 'self'
https://keymail.dev`. `auth.Config.KeymailServers` limits which keymail
servers are trusted; other addresses get a link.
```

Then `grep -n "four shells\|dateWords" SKILL.md` and bring any hit in line with this branch (five shells; the helper list ending `opt`, `Tbdi`, `stageArt`). Check the budget: `wc -c SKILL.md` must stay ≤ 30000; if it does not, trim genuinely redundant prose elsewhere in the file rather than cutting a fact (AGENTS.md).

- [ ] **Step 6: Run everything the words touch**

Run: `GOFLAGS=-mod=mod go test . ./ui/ ./internal/designsystem/ ./internal/docsite/ -count=1`
Expected: PASS — `TestBaseCatalogsShareOneKeySet`, `TestSkillMDStaysWithinBudget`, the CSS-floor statement test, and the docsite nav tests (the new page is reachable, has a label and a blurb). Then `rm -rf copy-review`.

- [ ] **Step 7: Run the gate and commit**

Run: `GOFLAGS=-mod=mod go vet ./... && gofmt -l . && RASTRILLO_TEST_REQUIRE_NODE=1 GOFLAGS=-mod=mod go test ./... -count=1`

```bash
git add docs/site CHANGELOG.md SKILL.md
git commit -F - <<'EOF'
Docs and CHANGELOG for the sign-in screen, as approved in copy review

The guides and the changelog went through the operator's copy review
before being written, and the approved text is applied verbatim, as the
screen's own strings were in the first batch.

magic-links.md no longer says the sign-in page stays yours, passkeys.md
no longer says a passkey never signs anybody in from nothing, and
SKILL.md stops recommending a wider form-action for apps on the shipped
screen, where it is no longer needed.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
```

---

### Task 15: The whole gate, the push, and what only a person can check

**Files:** none new.

- [ ] **Step 1: Run the repository's full gate**

Run: `GOFLAGS=-mod=mod make ci`
Expected: every target passes — gofmt, money, root (with node required), chromedp-graph, gorm-free, race, the four examples, generate-check, scaffold-smoke, and browser (now including `./auth/`). A failure is fixed in the task that owns the code, as a new commit on this branch.

- [ ] **Step 2: Try the scaffold with the new shell**

Run, from the worktree root, as one command — the repository path is captured before any `cd`, and the destination is made explicitly rather than trusted to an inherited `$TMPDIR`:

```bash
repo=$(pwd) && dest=$(mktemp -d)/stageapp && \
GOFLAGS=-mod=mod go run ./cmd/rastrillo new --shell=stage "$dest" && \
cd "$dest" && GOFLAGS=-mod=mod go mod edit -replace "amadan.net/rastrillo/rastrillo=$repo" && \
GOFLAGS=-mod=mod go mod tidy && GOFLAGS=-mod=mod go test ./...
```

Expected: the app builds and its tests pass on the `stage` shell (the page renders the index inside the card area).

- [ ] **Step 3: Push**

```bash
git push origin signin-screen
```

- [ ] **Step 4: Record the checks a machine cannot do**

Add this list, unticked, to the branch description — on amadan the branch is the PR and its description is the PR body (`amadan branch describe signin-screen -body -` replaces the whole description, so pipe in the current text from the branch page with the list appended). The spec (§5, "By hand before merge") requires these and none can be automated here:

- The keymail continuation in Firefox and in Safari (Safari enforces `form-action` across intermediate GETs, F/auth_navigation.go:13-18): type a keymail address, land on the provider, and see no CSP error in the console.
- VoiceOver and NVDA on: Ask; an error problem (`?err=rate`) — the callout is read first; Returning (keymail) — the button and the address it describes; and the passkey failure message.

Do not merge; merging is the operator's call after those checks, and it goes through `amadan branch merge`, never a squash (AGENTS.md).
