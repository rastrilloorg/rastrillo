// recovery_test.go proves the recovery-code escape hatch: minting and
// replacement, redemption at the sign-in gate only, single use, the
// survival of the half-session across a wrong code, and the subject
// wall between one user's codes and another's half-session.
package passkey_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/carlosframework/rastrillo/sessions"
)

var codeShape = regexp.MustCompile(`^[a-z2-7]{5}-[a-z2-7]{5}$`)

func TestRegenerateMintsTenWellFormedCodes(t *testing.T) {
	e := newEnv(t)
	codes, err := e.h.RegenerateRecoveryCodes("alice@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(codes) != 10 {
		t.Fatalf("got %d codes, want 10", len(codes))
	}
	seen := map[string]bool{}
	for _, c := range codes {
		if !codeShape.MatchString(c) {
			t.Fatalf("malformed code %q", c)
		}
		if seen[c] {
			t.Fatalf("duplicate code %q", c)
		}
		seen[c] = true
	}
	if n, err := e.h.RecoveryCodesRemaining("alice@example.com"); err != nil || n != 10 {
		t.Fatalf("remaining = %d, %v; want 10", n, err)
	}
	// Another subject's count is untouched.
	if n, err := e.h.RecoveryCodesRemaining("bob@example.com"); err != nil || n != 0 {
		t.Fatalf("bob remaining = %d, %v; want 0", n, err)
	}
}

func TestRegenerateReplacesTheOldSet(t *testing.T) {
	e := newEnv(t)
	old, err := e.h.RegenerateRecoveryCodes("alice@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.h.RegenerateRecoveryCodes("alice@example.com"); err != nil {
		t.Fatal(err)
	}
	if n, _ := e.h.RecoveryCodesRemaining("alice@example.com"); n != 10 {
		t.Fatalf("remaining after regenerate = %d, want 10 (not 20)", n)
	}
	// The old set is gone from storage, not just outnumbered: its
	// hashes (of the normalized, dashless form) no longer exist.
	var cnt int
	if err := e.db.QueryRow(
		`SELECT COUNT(*) FROM passkey_recovery_codes WHERE code_hash = ?`,
		sessions.HashToken(strings.ReplaceAll(old[0], "-", ""))).Scan(&cnt); err != nil {
		t.Fatal(err)
	}
	if cnt != 0 {
		t.Fatal("an old code survived regeneration")
	}
}
