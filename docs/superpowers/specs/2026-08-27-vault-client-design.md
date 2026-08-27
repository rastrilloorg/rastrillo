# The vault client — rastrillo's half of the Pegamento vault

**Date:** 2026-08-27 · **Status:** DRAFT for review ·
**Upstream:** `amadan.net/carlos/pegamento` — `docs/doctrine.md` and
`docs/superpowers/specs/2026-08-27-vault-v1-design.md` (the wire this
client speaks). **Depends on:** #79 keyring (shipped).

## 0. What this is, and the boundary

Pegamento's vault facet gives one person one set of named sealed blobs
and one seed wrapped per sign-in method, on a home service the app's
operator may not run. This spec is **rastrillo's client half only**: the
derivations, the wire client, the enrol/restore handoff, and the two
small `sessions` additions that let a vault entry admit its owner. The
server is Pegamento's; the ceremonies (passkey PRF) are the `passkey`
package's; nothing here builds either.

Two rulings bind everything, both inherited: **strictly additive** — an
app that configures no home origin constructs none of this and makes no
request, ever; and **closed namespace** — blob names are declared at
client construction, and an undeclared name is a programming error the
client refuses locally, before any request.

## 1. What the vault carries for a rastrillo app

**A session token — not a new credential type.** This is the spec's one
real decision, so the reasoning in full:

Eleven's vault carries each instance's live device token; Tito Go's
model has no instance credential at all. Rastrillo already has exactly
one admission primitive — a session row, revocable, TTL-bound, hashed
at rest — and the whole plugin doctrine is "verify a credential, call
`SignIn`." Inventing a second long-lived credential type (a "vault
token" table) would mean a second revocation surface, a second sweep,
and a second thing an admin must reason about. Instead:

- **Enrol** mints a *second, separate* session row with
  `Method: "vault"`, and that token — never the browser's own cookie
  token — is what the client seals into the vault's `servers` blob.
  Browser cookie and vault credential are independent rows: signing out
  of the browser does not strand the vault entry, revoking the vault
  row does not sign out the browser, and administrative revocation
  already works on both because both are ordinary rows.
- **Restore** presents the vault token to the instance, which *adopts*
  it: verify the hash, check expiry, set the cookie on this browser.
  No new row; the vault row simply gains a cookie pointing at it.
- **Expiry is the fallback, not a failure.** A vault entry idle past
  the sessions TTL (default 30 days) is dead like any other row;
  restore then answers "sign in normally", the app's ordinary plugins
  run, and enrol replaces the entry. No rotation machinery in v1 —
  rotation-on-restore (revoke presented, mint fresh, write back) is
  noted for v2 only if stale-entry churn proves real.

## 2. Package layout

| Piece | Where | Why there |
|---|---|---|
| `Ring.BlobKey(seed, name)` | `keyring` (addition) | a derivation is a wire format; keyring owns those, with the JS twin and golden vectors to pin it |
| `sessions.Mint`, `sessions.Adopt` | `sessions` (additions) | they touch the sessions table and cookie policy; nothing else may |
| everything else | **new package `vault`** | the HTTP client, envelope policy, handoff handlers, and JS twin compose `crypto` + `keyring` + `sessions` without reaching into any of them |

### 2.1 `keyring` addition

```go
// BlobKey derives the sealing key for one named vault blob:
// crypto.Derive(seed, Namespace+"/blob/"+name+"/v1").
func (r Ring) BlobKey(seed []byte, name string) []byte
```

Woodstar's `woodstar/blob/v1` generalised per name. Golden vectors gain
a `blobKey` case in both languages; `name` is restricted to
`[a-z0-9-]{1,64}` (the closed namespace makes this checkable at
construction, and a hostile name can't smuggle a `/v2` suffix into the
context string).

### 2.2 `sessions` additions

```go
// Mint creates a session row and returns its token without touching
// any cookie — for credentials that live somewhere other than this
// browser (the vault's copy). Same TTL, same table, same Sweep.
func (s *Sessions) Mint(sess Session) (token string, err error)

// Adopt verifies token against the store and, if the row is alive,
// sets the session cookie and returns the session. It mints nothing.
func (s *Sessions) Adopt(w http.ResponseWriter, r *http.Request, token string) (Session, bool)
```

Both are thin compositions of the package's existing `create`/`lookup`/
`setCookie`; neither adds a column or a migration.

### 2.3 The `vault` package

```go
type Config struct {
    Home   string        // home origin, scheme included; empty = the package is unusable, by design
    Ring   keyring.Ring  // the app's namespace
    Blobs  []string      // the closed namespace: every name this app may touch
    Client *http.Client  // default http.DefaultClient with a 10s timeout
}

func New(cfg Config) (*Client, error)  // refuses empty Home, invalid blob names
```

Wire methods mirror Pegamento's v1 sketch exactly — `Methods`,
`Wrapped`, `Enrol`, `RemoveMethod`, `Blobs`, `Get`, `Put`, `Delete` —
with the three contracts pinned as types, not prose:

- **Versions are opaque strings.** `vault.Create` (= `"0"`) is the
  create-only sentinel; `Put` requires the version and a mismatch
  returns `ErrStale{Current string}` for the caller's merge-and-retry
  loop. The client never parses, increments, or compares versions
  beyond equality.
- **Sealing is local and mandatory.** `Get` returns plaintext opened
  with `BlobKey`; `Put` accepts plaintext and seals. Ciphertext never
  crosses the package boundary in either direction, so a caller cannot
  accidentally store plaintext. Padding is per-blob policy:
  `PutPadded(name, plaintext, size)` for blobs whose length leaks
  (Eleven's 16 KiB server-list case).
- **Undeclared names refuse locally** with `ErrUndeclared` before any
  request — the closed namespace enforced at both ends.

### 2.4 The handoff (enrol/restore)

Mirrors Eleven's shape: everything secret travels in URL fragments and
comes back wrapped to an ephemeral key; the decision logic is pure
functions; the handlers are thin.

- **Pure helpers** (Go + JS twin, golden-vectored): build/validate the
  enrol and restore payloads — nonce, `ret` same-origin check,
  ephemeral-key wrap/unwrap via `crypto.Seal`/`Open`. The validators
  are exported and tested standalone, Eleven's `restore-payload.js`
  lesson.
- **Two handlers**, mounted by the app only when it constructs the
  package:
  - `POST /vault/enrol` — signed-in only (`sess.Require` above it):
    `sessions.Mint` a `Method: "vault"` row, answer the payload the
    browser carries to the home's enrol page. The instance never talks
    to the home.
  - `POST /vault/restore` — signed-out: same-origin (the `csrf`
    middleware already covers it), body carries the token the browser
    unwrapped from the home's return fragment; `sessions.Adopt`, then
    redirect via `sessions.SafeReturn`. A dead token answers the
    signin page, never an error page.

Instance-side footprint beyond the two handlers, copied from Eleven's
proof that three references suffice: the home origin enters the CSP
`connect-src`, reaches templates as one variable, and nothing else. No
`/api/me` equivalent in v1 — the home-side ownership check that needs
it has no rastrillo consumer yet.

## 3. Error handling

The home being down, slow, or gone is **normal operation**, not an
error path: every failure of the vault client degrades to "the app
works exactly as it does today, without cross-instance convenience."
`Get`/`Put` return errors the caller shows as "couldn't reach your
home" copy; the handoff handlers time-box home interactions on the
browser side (the instance never waits on the home at all); `Adopt`
failing falls through to ordinary signin. Nothing retries in a loop;
nothing blocks boot; nothing logs above `Warn`.

## 4. Testing

- **Golden vectors**: `blobKey` and the handoff payload wrap in
  `keyring`/`vault` testdata, both languages, via the existing #78
  vector machinery.
- **A fake home** (`httptest`, in-package): the wire contract — CAS
  409s, `"0"` create, If-None-Match 304, closed-namespace 404 — tested
  against the same suite the real Pegamento server will import, so the
  two halves cannot drift (Eleven's backend-agnostic `store_test.go`
  pattern, applied across the repo boundary).
- **Handler tests**: enrol requires a session; restore adopts a live
  token, refuses a revoked one, and falls back to signin on expiry;
  a tampered fragment payload fails closed.
- **The strictly-additive test**: an app built with no `vault.Config`
  makes zero requests — asserted with a transport that fails the test
  on any dial.

## 5. Out of scope, named

The Pegamento server and its store; the registry facet's client (v2 —
enrolment presumes a home account exists, and creating one is
home-side); ceremonies and PRF plumbing (`passkey` owns them); vault
UI beyond the handoff redirects (app territory); rotation-on-restore
(v2, evidence first); any change to existing identity plugins —
passwords, magic links and passkeys are untouched, still calling
`SignIn`, still the standalone path every instance keeps.
