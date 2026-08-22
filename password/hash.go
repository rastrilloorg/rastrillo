// Package password is an email+password identity plugin on the
// sessions core: it verifies a submitted credential and calls
// sessions.SignIn — the same one-call contract auth's keymail flow
// honors — while leaving user storage, page rendering, and CSRF to
// the app (csrf.Protect is mounted app-wide, not this package's job).
package password

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

// iterations and keyLen are pinned in the encoded hash itself (see
// Hash) rather than only in code, so they can be raised later without
// breaking already-stored hashes: Verify reads the parameters a hash
// was made with, not the package's current defaults.
const (
	iterations = 600_000
	saltLen    = 16
	keyLen     = 32

	// maxIterations caps what Verify will accept from a stored hash's
	// own iter field — see Verify's comment.
	maxIterations = 10_000_000
)

// Hash derives a PBKDF2-SHA256 hash of password and encodes it as
// "pbkdf2$sha256$600000$<hex salt>$<hex dk>". PBKDF2-SHA256 at 600k
// iterations is the current OWASP floor; it's chosen over argon2 here
// to stay stdlib-only (Go's crypto/pbkdf2, added in 1.24).
func Hash(password string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("rastrillo/password: generate salt: %w", err)
	}
	dk, err := pbkdf2.Key(sha256.New, password, salt, iterations, keyLen)
	if err != nil {
		return "", fmt.Errorf("rastrillo/password: derive key: %w", err)
	}
	return fmt.Sprintf("pbkdf2$sha256$%d$%s$%s", iterations, hex.EncodeToString(salt), hex.EncodeToString(dk)), nil
}

// Verify reports whether password matches the PBKDF2 hash encoded in
// encoded (Hash's format). Any malformed encoding — wrong field
// count, unknown algorithm, unparseable iteration count, invalid hex
// — is treated as a non-match: Verify never panics on garbage input.
// The derived key comparison uses subtle.ConstantTimeCompare so a
// mismatch takes the same time regardless of where the bytes diverge.
func Verify(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 5 {
		return false
	}
	algo, hashName, iterStr, saltHex, dkHex := parts[0], parts[1], parts[2], parts[3], parts[4]
	if algo != "pbkdf2" || hashName != "sha256" {
		return false
	}
	iter, err := strconv.Atoi(iterStr)
	// The upper bound isn't a real-world iteration count — it's a
	// guard against a corrupted or hostile stored row (a huge iter
	// pinning a request thread in PBKDF2 for an unbounded amount of
	// time); maxIterations is far above any value this package would
	// ever encode.
	if err != nil || iter <= 0 || iter > maxIterations {
		return false
	}
	salt, err := hex.DecodeString(saltHex)
	if err != nil {
		return false
	}
	wantDK, err := hex.DecodeString(dkHex)
	if err != nil {
		return false
	}
	if len(wantDK) == 0 {
		// Belt-and-suspenders: an empty key would make pbkdf2.Key's
		// keyLength argument 0, which happens to also be a non-match by
		// construction — spelled out explicitly rather than relying on
		// that stdlib behavior.
		return false
	}
	gotDK, err := pbkdf2.Key(sha256.New, password, salt, iter, len(wantDK))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(gotDK, wantDK) == 1
}

// NeedsRehash reports whether encoded was made with weaker parameters
// than the package currently uses — an older iteration count, or a
// format this package no longer produces. The parameters are pinned in
// each hash (see Hash), so old hashes keep verifying forever; this is
// how an app notices them and upgrades opportunistically, at the one
// moment it holds the plaintext:
//
//	if Verify(stored, submitted) && NeedsRehash(stored) {
//	    if h, err := Hash(submitted); err == nil { /* store h */ }
//	}
func NeedsRehash(encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 5 || parts[0] != "pbkdf2" || parts[1] != "sha256" {
		return true
	}
	iter, err := strconv.Atoi(parts[2])
	return err != nil || iter < iterations
}

// decoyHash is verified against when Lookup finds no user, so an
// unknown email costs the same wall-clock as a wrong password — no
// enumeration oracle by timing.
var decoyHash, _ = Hash("rastrillo-password-decoy")
