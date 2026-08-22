# 2FA Recovery Codes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Sign-in-only recovery codes for the passkey second factor — an account whose only passkey is lost signs in with a saved code instead of falling back to the operator.

**Architecture:** Everything lands in the existing `passkey` package: one new table in `passkey.Migrations`, three methods on `*Handlers` (`RegenerateRecoveryCodes`, `RecoveryCodesRemaining`, `SignInRecovery`), reusing the pending half-session, `sessions.HashToken`, and the DELETE…RETURNING single-use idiom throughout. `SignInRecovery` is a plain form POST — the no-JS path, deliberately.

**Tech Stack:** Go stdlib (`crypto/rand`, `database/sql`), existing `sessions` helpers, existing passkey test harness (`newEnv`, `gate`, authtest).

**Spec:** docs/superpowers/specs/2026-08-22-recovery-codes-design.md

## Global Constraints

- Sign-in only: no recovery step-up endpoint of any kind.
- Codes: 10 per set, 10 chars of lowercase base32 (`a–z2–7`, 50 bits crypto/rand), displayed `xxxxx-xxxxx`; hash the normalized (dashless) form with `sessions.HashToken`.
- Single use via `DELETE … RETURNING`; a failed redemption must NOT consume the pending half-session; the pending row is consumed last, on success only.
- Redemption keys on `code_hash AND subject`.
- Failure answers 303 to `ConfirmPath + "?recovery=failed"` — never which check failed.
- Success mints `p.method + "+recovery"`, AuthTime now, 303 to stored return_to.
- No attempt counter (spec §3 documents the entropy math in the package doc instead).
- `CGO_ENABLED=0 go test ./passkey/`; commits carry the Claude trailer; PR-only workflow.

---

### Task 1: Codes core — mint, count, migrations

**Files:** Modify `passkey/passkey.go` (Migrations + new methods), Create `passkey/recovery.go` + `passkey/recovery_test.go`.

- [ ] Failing tests: TestRegenerateMintsTenWellFormedCodes (format `^[a-z2-7]{5}-[a-z2-7]{5}$`, all distinct, remaining=10), TestRegenerateReplacesTheOldSet (old code's hash gone; remaining still 10).
- [ ] Append `passkey_recovery_codes` table + subject index to Migrations (spec §2).
- [ ] Implement `newRecoveryCode()` (5+5 base32 chars, crypto/rand), `normalizeRecoveryCode` (lowercase, strip `-` and spaces), `RegenerateRecoveryCodes` (one transaction: DELETE subject's rows, INSERT 10 hashes of normalized forms, return display forms), `RecoveryCodesRemaining` (COUNT).
- [ ] `CGO_ENABLED=0 go test ./passkey/ -count=1` green; commit.

### Task 2: SignInRecovery — the form POST

**Files:** Modify `passkey/recovery.go`, `passkey/recovery_test.go`.

**Interfaces:** Consumes `pendingFrom`, `setPendingCookie`, `refuse`, `h.cfg.Sessions.SignIn`, `h.cfg.ConfirmPath` — all existing.

- [ ] Failing tests per spec §5: round trip (gate → POST code → 303 return_to → Method "password+recovery" session; replayed pending refused; burned code fails), wrong code (303 `?recovery=failed`, half-session survives, correct code still redeems after), normalization (uppercase/dashes/spaces), cross-subject isolation, no/expired pending → 403, GET → 405.
- [ ] Implement `SignInRecovery` in spec §3's order: method check (405) → pendingFrom (refuse) → normalize → `DELETE … WHERE code_hash = ? AND subject = ? RETURNING subject` → miss: 303 ConfirmPath?recovery=failed → hit: consume pending (`DELETE … RETURNING`, raced loser 303s to ?recovery=failed too), clear cookie, SignIn `p.method+"+recovery"`, 303 return_to.
- [ ] Green incl. `-race`; commit.

### Task 3: Docs and doc-tests

**Files:** Modify `passkey/passkey.go` (package doc), `SKILL.md`, `README.md`.

- [ ] Package doc: "# Recovery codes" section — trust boundary (sign-in only), the no-counter math, RequireFresh rule for the regenerate page, `+recovery` method marker, endpoint listing updated.
- [ ] SKILL.md §5 2FA bullet: one added sentence (recovery codes redeem at the gate; regenerate behind RequireFresh); stay ≤15,000 bytes (root test enforces).
- [ ] README passkey bullet: recovery-codes sentence.
- [ ] Full tree: root build/vet/test (GOFLAGS=-mod=mod, CGO_ENABLED=0) + `go test -race ./passkey/ ./jobs/ ./sessions/`; commit.

Then: PR → CI → squash-merge → release PR (fallback v0.14.0) → tag → site (Pending −1, Posted bullet, counts unchanged — no example change) → skills repo.
