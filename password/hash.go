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
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id is what Hash produces: the memory-hard function current
// guidance (OWASP, RFC 9106) puts first, at OWASP's first recommended
// setting — 19 MiB of memory, two passes, one lane — which lands near
// the quarter-second a verification should cost and stays polite to a
// small instance verifying several at once. The parameters are pinned
// in the encoded hash itself, so they can be raised later without
// breaking already-stored hashes: Verify reads the parameters a hash
// was made with, not the package's current defaults.
const (
	argonMemory  = 19 * 1024 // KiB
	argonTime    = 2
	argonThreads = 1
	saltLen      = 16
	keyLen       = 32

	// Ceilings on what Verify will accept from a stored hash's own
	// parameters — guards against a corrupted or hostile row pinning a
	// request in a derivation for an unbounded amount of time, far above
	// anything this package would ever encode.
	maxArgonMemory = 1024 * 1024 // 1 GiB
	maxArgonTime   = 64
	// Earlier versions of this package encoded PBKDF2-SHA256 at 600,000
	// iterations (the OWASP floor). Verify still honours those hashes,
	// reading the count from the hash itself under this ceiling;
	// NeedsRehash reports them, and an app upgrades each at the one
	// moment it holds the plaintext.
	maxIterations = 10_000_000
)

// Hash derives an Argon2id hash of password and encodes it as
// "argon2id$v=19$m=19456,t=2,p=1$<b64 salt>$<b64 key>" — the PHC
// string format, base64 without padding, so any other Argon2
// implementation reads it.
func Hash(password string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("rastrillo/password: generate salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, keyLen)
	return fmt.Sprintf("argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

// Verify reports whether password matches encoded — an Argon2id hash in
// Hash's format, or a PBKDF2-SHA256 hash from an earlier version of
// this package ("pbkdf2$sha256$<iter>$<hex salt>$<hex dk>"). Any
// malformed encoding — wrong field count, unknown algorithm,
// unparseable parameters, invalid encoding — is treated as a
// non-match: Verify never panics on garbage input. The key comparison
// uses subtle.ConstantTimeCompare so a mismatch takes the same time
// regardless of where the bytes diverge.
func Verify(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 5 {
		return false
	}
	switch parts[0] {
	case "argon2id":
		return verifyArgon(parts, password)
	case "pbkdf2":
		return verifyPBKDF2(parts, password)
	}
	return false
}

func verifyArgon(parts []string, password string) bool {
	if parts[1] != "v="+strconv.Itoa(argon2.Version) {
		return false
	}
	var m, t, p int
	for _, kv := range strings.Split(parts[2], ",") {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			return false
		}
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return false
		}
		switch k {
		case "m":
			m = n
		case "t":
			t = n
		case "p":
			p = n
		default:
			return false
		}
	}
	if m == 0 || t == 0 || p == 0 || m > maxArgonMemory || t > maxArgonTime || p > 16 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil || len(salt) == 0 {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(want) == 0 {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, uint32(t), uint32(m), uint8(p), uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

func verifyPBKDF2(parts []string, password string) bool {
	hashName, iterStr, saltHex, dkHex := parts[1], parts[2], parts[3], parts[4]
	if hashName != "sha256" {
		return false
	}
	iter, err := strconv.Atoi(iterStr)
	if err != nil || iter <= 0 || iter > maxIterations {
		return false
	}
	salt, err := hex.DecodeString(saltHex)
	if err != nil {
		return false
	}
	wantDK, err := hex.DecodeString(dkHex)
	if err != nil || len(wantDK) == 0 {
		return false
	}
	gotDK, err := pbkdf2.Key(sha256.New, password, salt, iter, len(wantDK))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(gotDK, wantDK) == 1
}

// NeedsRehash reports whether encoded was made with weaker parameters
// than the package currently uses — a PBKDF2 hash from an earlier
// version, an Argon2id hash below today's memory or time, or a format
// this package never produced. Old hashes keep verifying forever; this
// is how an app notices them and upgrades opportunistically, at the
// one moment it holds the plaintext:
//
//	if Verify(stored, submitted) && NeedsRehash(stored) {
//	    if h, err := Hash(submitted); err == nil { /* store h */ }
//	}
func NeedsRehash(encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 5 || parts[0] != "argon2id" {
		return true
	}
	var m, t int
	for _, kv := range strings.Split(parts[2], ",") {
		k, v, _ := strings.Cut(kv, "=")
		n, _ := strconv.Atoi(v)
		switch k {
		case "m":
			m = n
		case "t":
			t = n
		}
	}
	return m < argonMemory || t < argonTime
}

// decoyHash is verified against when Lookup finds no user, so an
// unknown email costs the same wall-clock as a wrong password — no
// enumeration oracle by timing.
var decoyHash, _ = Hash("rastrillo-password-decoy")
