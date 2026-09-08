// recovery.go is the escape hatch: saved single-use codes that redeem
// a pending half-session when the factor that should have is lost —
// whichever factor that was. Sign-in only, by design: there is no
// recovery step-up, and sessions.RequireFresh stays satisfiable by a
// real proof or a full re-sign-in and nothing else.

package secondfactor

import (
	"crypto/rand"
	"net/http"
	"strings"
	"time"

	"amadan.net/rastrillo/rastrillo/sessions"
)

// recoveryCodesPerSet is a set's size: enough that losing a few pieces
// of paper still leaves a way in, few enough to print on one line each.
const recoveryCodesPerSet = 10

// recoveryAlphabet is lowercase RFC 4648 base32: 32 characters, so a
// random byte mod 32 is unbiased, and no 0/1/8/9 to misread as o/l/b/g.
const recoveryAlphabet = "abcdefghijklmnopqrstuvwxyz234567"

// newRecoveryCode mints one code: 10 alphabet characters (50 bits of
// crypto/rand entropy), displayed grouped xxxxx-xxxxx.
func newRecoveryCode() (string, error) {
	b := make([]byte, 10)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	for i := range b {
		b[i] = recoveryAlphabet[int(b[i])%len(recoveryAlphabet)]
	}
	return string(b[:5]) + "-" + string(b[5:]), nil
}

// normalizeRecoveryCode is what gets hashed and what a posted code is
// reduced to before lookup: lowercase, grouping dashes and spaces
// stripped — a code survives being retyped from paper in any casing.
func normalizeRecoveryCode(code string) string {
	code = strings.ToLower(code)
	code = strings.ReplaceAll(code, "-", "")
	return strings.ReplaceAll(code, " ", "")
}

// RegenerateRecoveryCodes mints a fresh set for subject, atomically
// replacing any previous set, and returns the plaintexts — the only
// moment they exist outside the caller's screen; only hashes (of the
// normalized form, via sessions.HashToken — a fast hash is right for
// 50 bits of machine entropy, where PBKDF2 would buy latency and
// nothing else) reach the database. Call it from a page mounted behind
// sessions.RequireFresh: showing sign-in-grade secrets is exactly the
// dangerous action step-up exists for.
func (g *Gate) RegenerateRecoveryCodes(subject string) ([]string, error) {
	tx, err := g.cfg.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM secondfactor_recovery_codes WHERE subject = ?`, subject); err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	codes := make([]string, 0, recoveryCodesPerSet)
	for i := 0; i < recoveryCodesPerSet; i++ {
		code, err := newRecoveryCode()
		if err != nil {
			return nil, err
		}
		if _, err := tx.Exec(
			`INSERT INTO secondfactor_recovery_codes (code_hash, subject, created_at) VALUES (?, ?, ?)`,
			sessions.HashToken(normalizeRecoveryCode(code)), subject, now); err != nil {
			return nil, err
		}
		codes = append(codes, code)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return codes, nil
}

// RecoveryCodesRemaining reports how many unspent codes subject holds —
// for a settings page's "6 of 10 left", and for deciding whether the
// confirm page should offer the recovery form at all.
func (g *Gate) RecoveryCodesRemaining(subject string) (int, error) {
	var n int
	err := g.cfg.DB.QueryRow(
		`SELECT COUNT(*) FROM secondfactor_recovery_codes WHERE subject = ?`, subject).Scan(&n)
	return n, err
}

// SignInRecovery is POST /signin/recovery: redeem a recovery code
// against the pending half-session — a plain form POST (field
// "code"), deliberately, because recovery is exactly the moment the
// authenticator, WebAuthn or JavaScript isn't working. Mount it
// behind csrf.Protect like every ceremony endpoint.
//
// A miss 303s back to ConfirmPath with ?recovery=failed and leaves the
// half-session alive (a typo must not burn the between-factors
// window); which check missed is never said. A hit burns the code,
// consumes the half-session (a raced second finish loses), and mints
// the real session as the original first-factor method plus
// "+recovery" — an app can spot that marker and nudge enrolling a
// replacement factor.
//
// There is no attempt counter, on the math rather than an oversight:
// redemption requires a live half-session, so the guesser has already
// verified the first factor, and holds it for pendingTTL at most —
// 10 valid codes at 2^-50 apiece leave even a thousand guesses a
// second at odds near 3×10⁻⁹ across the whole window.
func (g *Gate) SignInRecovery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	p, ok := g.Pending(r)
	if !ok {
		http.Error(w, "signed out", http.StatusForbidden)
		return
	}
	failedTo := g.cfg.ConfirmPath + "?recovery=failed"
	code := normalizeRecoveryCode(r.PostFormValue("code"))
	if code == "" {
		http.Redirect(w, r, failedTo, http.StatusSeeOther)
		return
	}
	var redeemed string
	if err := g.cfg.DB.QueryRow(
		`DELETE FROM secondfactor_recovery_codes WHERE code_hash = ? AND subject = ? RETURNING subject`,
		sessions.HashToken(code), p.Subject).Scan(&redeemed); err != nil {
		g.cfg.Logger.Warn("rastrillo/secondfactor: recovery code refused")
		http.Redirect(w, r, failedTo, http.StatusSeeOther)
		return
	}
	if err := g.Complete(w, r, p, "recovery"); err != nil {
		if err == ErrConsumed {
			http.Redirect(w, r, failedTo, http.StatusSeeOther)
			return
		}
		g.cfg.Logger.Error("rastrillo/secondfactor: mint session", "err", err)
		http.Error(w, "something went wrong", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, p.ReturnTo, http.StatusSeeOther)
}
