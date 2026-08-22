# 2FA recovery codes — design

Date: 2026-08-22. Status: approved direction (sign-in only, per Paul);
clears the site's "2FA recovery codes" Pending item: an account whose
only passkey is lost no longer falls back to the operator.

## 1. Scope and trust boundary

Recovery codes are the passkey Gate's escape hatch, nothing more. A
code redeems **only at the sign-in gate**: it consumes the same pending
half-session a passkey assertion would, after the first factor already
verified. There is no recovery-based step-up — `sessions.RequireFresh`
is satisfied by a passkey assertion or a full re-sign-in, exactly as
today. (A session minted via recovery is fresh by AuthTime like any
new sign-in; "sign-in only" means no recovery StepUp endpoint exists.)

They live in `rastrillo/passkey`: same package, same DB handle, same
pending machinery, same Config. No new package, no new Config fields.

## 2. Codes

- A set is **10 codes**. Each is 10 characters from the lowercase RFC
  4648 base32 alphabet (`a–z2–7`), i.e. 50 bits of `crypto/rand`
  entropy, displayed grouped `xxxxx-xxxxx`.
- Stored **hashed** with `sessions.HashToken` (SHA-256). A fast hash is
  correct here because the input is 50 bits of machine entropy, not a
  human password; PBKDF2 would buy nothing but latency.
- **Single use**: redemption is `DELETE … RETURNING` on the hash, the
  same construction as challenges and the pending row.
- Codes do not expire and are untouched by `Sweep`; a set exists until
  regenerated or spent.

Schema (appended to `passkey.Migrations`, additive and idempotent):

```sql
CREATE TABLE IF NOT EXISTS passkey_recovery_codes (
  code_hash  TEXT PRIMARY KEY,
  subject    TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS passkey_recovery_codes_subject
  ON passkey_recovery_codes (subject);
```

## 3. API surface (all on the existing *Handlers)

- `RegenerateRecoveryCodes(subject string) ([]string, error)` — mints
  a fresh set of 10, **replacing** any previous set atomically (one
  transaction: delete the subject's rows, insert the new hashes), and
  returns the plaintexts — the only time they exist outside the
  caller's screen. The app calls this from its own settings/enrollment
  page, **mounted behind `sessions.RequireFresh`** (showing codes is a
  dangerous action), and renders them once with "save these now" copy.
- `RecoveryCodesRemaining(subject string) (int, error)` — for the
  app's settings page ("6 of 10 left") and for deciding whether the
  confirm page should offer the recovery link at all.
- `SignInRecovery(w, r)` — **POST, form-encoded, field `code`** — the
  no-JS path, deliberately: recovery is exactly the moment WebAuthn or
  JS isn't working, so a plain HTML form on the app's confirm page must
  suffice. Mount at `POST /passkey/signin/recovery`, behind
  `csrf.Protect` like every ceremony endpoint.

### SignInRecovery flow

1. `pendingFrom(r)` — no live half-session → `refuse` (403 "signed
   out"): without a verified first factor you shouldn't be here.
2. Normalize the posted code: lowercase, strip `-` and spaces.
3. Redeem: `DELETE FROM passkey_recovery_codes WHERE code_hash = ? AND
   subject = ? RETURNING subject` — hash **and** subject, so one
   user's code never redeems another's half-session.
4. Miss → 303 back to `ConfirmPath` with `?recovery=failed`, pending
   row NOT consumed (a typo must not burn the between-factors window;
   the app's confirm page reads the flag and shows its own, localized
   message). The generic-failure discipline holds: the response never
   says whether the code was wrong, spent, or never existed.
5. Hit → consume the pending row (`DELETE … RETURNING`, raced second
   finish loses), clear the pending cookie, `SignIn` with Method
   `p.method + "+recovery"` (e.g. `"magiclink+recovery"`) and AuthTime
   now, 303 to the stored `return_to`. Apps may detect `"+recovery"`
   and nudge enrolling a replacement passkey.

### Why no attempt counter

Guessing requires a live half-session, i.e. the first factor already
compromised — the Gate's standing threat model. The window is
`pendingTTL` (5 minutes); 10 valid codes at 2⁻⁵⁰ apiece put even a
thousand-guesses-per-second attacker at ~3×10⁻⁹ over the whole window.
The package doc states this math instead of shipping a counter that
would complicate the pending schema for no measurable gain.

## 4. What is deliberately not here

- No recovery step-up (Paul's ruling: sign-in only).
- No example-app wiring: notes does not mount passkey today; the Gate
  itself shipped in v0.11.0 with package-level proof (`signin_test.go`)
  and recovery follows that precedent.
- No operator/admin reset flow — regenerating requires a fresh
  session; a user with no factor at all remains an operator problem,
  now vanishingly rarer.
- No expiry on codes, no encryption-at-rest beyond hashing.

## 5. Testing

Package tests beside the existing gate suite, same harness (`newEnv`,
`gate`, authtest):

- Regenerate: 10 codes, format `^[a-z2-7]{5}-[a-z2-7]{5}$`, remaining
  10; regenerating invalidates the old set (old code no longer
  redeems) and remaining stays 10.
- Round trip: enroll → gate → POST recovery code → 303 to return_to,
  real session minted with Method "password+recovery" (harness signs
  in via password method string), pending consumed (replay refused),
  code burned (second redemption fails with ?recovery=failed).
- Wrong code: 303 to ConfirmPath?recovery=failed, half-session
  survives, a subsequent correct code still works.
- Normalization: `XXXXX-XXXXX` with dashes/spaces/uppercase redeems.
- Isolation: Bob's code does not redeem Alice's half-session.
- No pending cookie / expired pending → 403.
- GET → 405.

## 6. Docs and downstream

- Package doc gains a "# Recovery codes" section (trust boundary, the
  no-counter math, RequireFresh mounting rule, "+recovery" method).
- SKILL.md §5's 2FA bullet grows one sentence (byte budget applies).
- README's passkey bullet mentions recovery.
- Ships as v0.14.0 (additive, no breaking change). Site: Pending item
  removed, Posted passkey bullet extended. Skills repo rastrillo.md:
  safety-rules bullet updated.
