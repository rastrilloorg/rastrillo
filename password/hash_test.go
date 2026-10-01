package password

import (
	"crypto/pbkdf2"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestHashVerifyRoundTrip(t *testing.T) {
	enc, err := Hash("s3cret")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if !Verify(enc, "s3cret") {
		t.Errorf("Verify(Hash(%q), %q) = false, want true", "s3cret", "s3cret")
	}
}

func TestVerifyWrongPassword(t *testing.T) {
	enc, err := Hash("s3cret")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if Verify(enc, "wrong") {
		t.Errorf("Verify with wrong password = true, want false")
	}
}

func TestHashSaltsDiffer(t *testing.T) {
	a, err := Hash("x")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	b, err := Hash("x")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if a == b {
		t.Errorf("two Hash(%q) calls produced identical output %q, want distinct salts", "x", a)
	}
}

func TestVerifyGarbageEncoded(t *testing.T) {
	cases := []string{"", "nonsense", "pbkdf2$sha256$abc$xx$yy", "argon2id$v=19$m=x,t=2,p=1$aa$bb", "argon2id$v=18$m=19456,t=2,p=1$c2FsdA$c2FsdA", "argon2id$v=19$m=19456,t=2,p=1$!!$bb"}
	for _, enc := range cases {
		if Verify(enc, "anything") {
			t.Errorf("Verify(%q, ...) = true, want false", enc)
		}
	}
}

func TestParamsPinned(t *testing.T) {
	enc, err := Hash("s3cret")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	const want = "argon2id$v=19$m=19456,t=2,p=1$"
	if len(enc) < len(want) || enc[:len(want)] != want {
		t.Errorf("Hash output = %q, want prefix %q", enc, want)
	}
}

// TestLegacyPBKDF2StillVerifiesAndNeedsRehash pins the upgrade path: a
// hash an earlier version stored keeps working, and is reported as
// wanting a rehash so the app replaces it on the next sign-in.
func TestLegacyPBKDF2StillVerifiesAndNeedsRehash(t *testing.T) {
	// "s3cret" under the old encoding, made with the old code.
	const legacy = "pbkdf2$sha256$600000$8c6b9a1f0e2d4c3b5a6978877665544332$"
	salt, _ := hex.DecodeString("8c6b9a1f0e2d4c3b5a6978877665544332")
	dk, _ := pbkdf2.Key(sha256.New, "s3cret", salt, 600000, 32)
	enc := legacy + hex.EncodeToString(dk)
	if !Verify(enc, "s3cret") {
		t.Fatal("a PBKDF2 hash from the earlier format no longer verifies")
	}
	if Verify(enc, "s3cret!") {
		t.Fatal("a PBKDF2 hash verified the wrong password")
	}
	if !NeedsRehash(enc) {
		t.Error("a PBKDF2 hash must be reported as needing a rehash")
	}
}

// TestDecoyHashInitialized guards the package-level decoyHash var:
// Hash("rastrillo-password-decoy") is computed at init via `var
// decoyHash, _ = Hash(...)`, silently discarding any error — this
// test turns a broken decoy (e.g. a future refactor that changes
// Hash's error behavior) into a red test instead of a silent timing
// leak in Signin's unknown-email path.
func TestDecoyHashInitialized(t *testing.T) {
	if !strings.HasPrefix(decoyHash, "argon2id$") {
		t.Errorf("decoyHash = %q, want an argon2id hash", decoyHash)
	}
}

func TestNeedsRehash(t *testing.T) {
	current, err := Hash("some-password")
	if err != nil {
		t.Fatal(err)
	}
	if NeedsRehash(current) {
		t.Errorf("a hash this package just made must not need a rehash")
	}

	old := "pbkdf2$sha256$100000$deadbeefdeadbeefdeadbeefdeadbeef$" + strings.Repeat("ab", 32)
	if !NeedsRehash(old) {
		t.Errorf("a PBKDF2 hash is the earlier format — must need a rehash")
	}
	weak := "argon2id$v=19$m=8192,t=1,p=1$c2FsdHNhbHRzYWx0c2Fs$" + strings.Repeat("A", 43)
	if !NeedsRehash(weak) {
		t.Errorf("argon2id below today's memory and time must need a rehash")
	}
	for _, garbage := range []string{"", "bcrypt$whatever", "pbkdf2$sha256$notanumber$aa$bb"} {
		if !NeedsRehash(garbage) {
			t.Errorf("NeedsRehash(%q) = false, want true for any format we no longer produce", garbage)
		}
	}
}
