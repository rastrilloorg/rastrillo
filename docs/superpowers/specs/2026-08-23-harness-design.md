# `harness/` — browser drive with a virtual authenticator

**Issue:** rastrilloorg/rastrillo#80 · **Date:** 2026-08-23 · **Status:** draft for review

## 0. The gap

The only honest way to test a passkey-wrapped seed's whole life — enrolment,
the PRF unwrap, opening the same sealed data on a second sign-in — is a real
browser with a virtual authenticator. Kass's `make harness`
(`web/scripts/ui-check.mjs` + `harness/rig.mjs`) proves the shape: one real
Chromium, a CDP virtual authenticator with `hasPrf: true`, loud on purpose,
every screen scanned for the bug class that renders perfectly and says
nothing. Every E2EE app on rastrillo needs exactly this.

## 1. Corrections to the issue, from the code

- Kass's rig is **Playwright**, not chromedp. Rastrillo's own precedent is
  `ui/browser_test.go` (chromedp, `//go:build browser`) — the harness is Go
  on chromedp, keeping the whole test suite one language and zero npm deps.
- The boot seam already exists and is documented for exactly this:
  `rastrillo.Handler(opts)` (serve.go:321, returning
  `(http.Handler, func() error, error)`) — "a harness is
  `httptest.NewServer` around this." The close func rides `t.Cleanup`, and
  the rig replicates the scaffold's wiring (app opens its own DB, passes
  `Mux`, leaves `DBPath` blank).
- **The CDP virtual authenticator cannot cover `prfByAssertion`.**
  `VirtualAuthenticatorOptions.HasPrf` is all-or-nothing; there is no knob to
  withhold PRF at creation while granting it at assertion (verified against
  cdproto at the pinned version, including `SetResponseOverrideBits`). The
  fallback gets covered by a page-level shim instead — §4.
- The scaffold **already ships** an `internal/<pkg>test/harness_test.go` —
  the httptest HTTP harness, and "harness" already means that throughout the
  scaffold's prose. The browser rig therefore takes a different name
  everywhere: the scaffolded file is `browser_test.go`, the tag is
  `browser`, the Make-style invocation is "browser drive." (Deviation from
  the issue's `rastrillo harness` naming, forced by the collision.)

## 2. Shape: package `harness/`

Library imported from `_test.go` files — but its own files are build-tagged
`//go:build browser` with an untagged `doc.go`, because the README promises,
twice, that chromedp stays out of the ordinary build graph (`go list -deps
./...` pulls none of it); an untagged package importing chromedp would make
that sentence false the day it lands. The tag also keeps `go build ./...`
and `go vet ./...` happy via the doc file. One tag, `browser`, everywhere —
framework-internal and app-side alike (§3).

Core, shaped around the origin chicken-and-egg a passkey app forces: the app
needs its origin *before* building its handler (csrf, webauthn config), but
the port doesn't exist until the server starts. And `httptest.NewServer`'s
`srv.URL` is `http://127.0.0.1:PORT` — an IP is not a WebAuthn RP ID;
`http://localhost` is the trustworthy origin (kass boots with
`ORIGIN=http://localhost:PORT`, `RPID=localhost`, and its rig warns that
Chromium's treat-as-secure flag is inert headless). So:

```go
func New(t *testing.T, build func(origin string) http.Handler, opts ...Option) *Rig
```

`New` binds a listener first, computes `origin = "http://localhost:PORT"`,
calls `build(origin)`, serves on the listener — and the rig always navigates
the localhost origin, never `srv.URL`. Passkey apps set `RPID: "localhost"`
in test wiring. Then: discover Chromium, launch headless via chromedp,
enable the CDP WebAuthn domain, add a virtual authenticator with kass's
proven options verbatim: `ctap2`/`ctap2_1`, `transport: internal`,
`hasResidentKey`, `hasUserVerification`, `hasPrf`, `isUserVerified` — and
`automaticPresenceSimulation` set **explicitly** to true: the CDP default is
true but the Go zero value silently sends false, and every ceremony hangs
(the one trap in an otherwise clean cdproto mapping). Cleanup via
`t.Cleanup`, registered so LIFO tears the browser down before the server
(`chromedp.Cancel` for graceful shutdown).

- **Chromium discovery** moves from `ui/browser_test.go` into
  `harness.ChromePath()`: `RASTRILLO_CHROME` env → PATH names → the
  Playwright cache glob. `RASTRILLO_BROWSER_OPTIONAL` keeps its meaning ("a
  skip is not a pass" — missing browser fails unless explicitly opted out).
  `ui/browser_test.go` switches to the shared helper; behavior unchanged.
- **Loud-failure watchers** (kass's `watch`, ported to `ListenTarget`):
  console `error`/`assert`, thrown exceptions, failed requests, responses
  ≥ 400 — all accumulate into the rig's problem list.
  `rig.Allow(method, path, status)` registers expected probes (kass's one
  exemption: the signed-out `/api/me` 401). Three chromedp-specific facts
  the port must honor: a 4xx response also surfaces as a console-error
  *mirror*, which in CDP arrives via `log.EventEntryAdded` (not
  `runtime.EventConsoleAPICalled`) — `Allow` filters both, matching log
  entries by URL; the method of a response comes from correlating
  `network.EventRequestWillBeSent` by RequestID; and the browser's own
  `/favicon.ico` probe is pre-allowed so every app doesn't rediscover it.
- **The screen gate**:

```go
func (r *Rig) Screen(selector, note string)
```

  waits for `selector`, runs the junk scan, then flushes the problem list —
  any accumulated problem fails the test naming the screen it surfaced on.
  The scan root is the `selector` itself (kass hard-fails on a missing
  `#app`; rastrillo apps are server-rendered with no such convention, so the
  screen's own selector is the root and `"body"` is the whole-page case).
  The junk scan checks the root's `textContent`, every `input`/`textarea`
  value, and every `[aria-label]` for `"undefined"`, `"null"`,
  `"[object Object]"`, `"NaN"` (kass's full set; `ui/browser_test.go`'s scan
  gains the missing `"null"` when it moves — noting that substring `"null"`
  can false-positive on legitimate prose, so the failure message shows the
  surrounding text and `rig.AllowText(s)` exists for the rare deliberate
  case).
- **On failure**: capture the app root's `innerText` into the test log —
  a failure report always includes what was on screen.
- Flow scripting stays plain chromedp: `rig.Run(actions...)` wraps
  `chromedp.Run` with the rig's context. No DSL — kass's drive script is a
  top-to-bottom async function and that's the right amount of framework.

## 3. The scaffold and the tag

`rastrillo new` adds `internal/<pkg>test/browser_test.go` under
`//go:build browser` (the existing `harness_test.go` — the httptest HTTP
harness — keeps its name and its meaning): boots the app through
`harness.New` with the scaffold's own wiring, navigates `/`,
`Screen("body", "home")` — the minimal loud walk, growing with the app.
The scaffold has no Makefile today and this spec doesn't add one; the
README section it *does* add says the whole interface plainly:

```sh
go test -tags browser ./...   # real browser, loud on any console error
                              # — not part of the plain suite
```

One tag, `browser`, framework and apps alike — matching rastrillo's own
`ui/` precedent and the `-tags pins` posture: plain `go test ./...` never
half-runs a browser, and the tag name is the invocation. No new CLI verb —
a verb would only alias the go test line. (Deviation from the issue's
title, deliberate; revisit if flows ever need orchestration beyond a test
run.)

Who runs it: rastrillo's ci.yml does not pass `-tags browser` today, so
landing §4 without CI wiring would leave the "finally covered" branch still
never running. This spec includes adding a browser-tagged job (or step) to
rastrillo's CI with a pinned Chromium, `RASTRILLO_BROWSER_OPTIONAL` unset —
a skip is not a pass — so the framework's own browser checks, §4 included,
run on every PR. Apps make their own CI call; the scaffolded README says
so.

## 4. Covering `prfByAssertion`

The fallback triggers when `prfSalt` was requested and
`getClientExtensionResults().prf.results.first` is absent after `create()`
(webauthn.mjs:71-76). Since the virtual authenticator can't withhold PRF at
creation, the framework test forces the condition one level up, and the
mechanism matters — two tempting shapes fail:

- patching `PublicKeyCredential.prototype.getClientExtensionResults` strips
  PRF from *assertions too* — the fallback's own
  `assertion.getClientExtensionResults()` goes through the same prototype
  method, so the patch breaks the very path under test;
- a naive Proxy around the credential throws "Illegal invocation" on the
  brand-checked `response`/`rawId` getters.

The working shape, which `register()`'s access pattern permits (it reads
only `rawId`, `response.*`, and calls `getClientExtensionResults()` on the
instance): a script registered via `Page.addScriptToEvaluateOnNewDocument`
— main world, before `Navigate` — wraps `navigator.credentials.create` and
defines an **own property** `getClientExtensionResults: () => ({})` on the
returned credential. Creation succeeds, the extension result is empty,
`register()` falls back to an immediate assertion — `credentials.get` is
untouched, so the virtual authenticator serves real PRF there — and since
PRF (hmac-secret) is deterministic per credential+salt, the test asserts
the ceremony completes with PRF bytes equal to a straight assertion's. The
test first asserts the *unshimmed* create does return PRF (the virtual
authenticator's expected behavior — without that baseline the shim proves
nothing). Lives in `webauthn/` under `//go:build browser`, built on the
`harness` package — one test upstream covers the branch for every consumer,
which shipped from kass untested.

The shim ships as a rig option (`harness.WithoutPRFAtCreation()`) rather
than a private fixture, so an app can rehearse the two-prompt path too.

## 5. Testing the harness itself

- The webauthn PRF test (§4) is the harness's own end-to-end proof: real
  ceremonies, both PRF paths, against a minimal in-repo fixture app.
- Junk-scan and watcher unit tests against static handlers (a page printing
  `undefined`, a handler answering 500) — browser-tagged.
- `new_test.go` gains assertions that the scaffolded `browser_test.go` and
  the README section exist and parse (the `readScaffold` pattern); the
  scaffolded file compiles under `-tags browser` in the existing
  scaffold-build test if cheap enough, else parse-checked only.

## 6. Out of scope

- The RPID-move executable drill — a scenario built on this rig once #79's
  documentation lands; tracked there.
- Screenshot/diff tooling, multi-browser matrices, parallel rigs: not the
  problem this solves.
