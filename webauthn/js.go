package webauthn

import (
	"crypto/rand"
	_ "embed"
)

//go:embed js/webauthn.mjs
var jsHelper []byte

// JS returns the raw bytes of the browser half (js/webauthn.mjs): the
// navigator.credentials wrappers producing exactly what Register and
// Verify check, for apps to serve as a static asset.
func JS() []byte { return jsHelper }

// NewChallenge mints a ceremony challenge: 32 random bytes. Keep it
// across the round trip — in the app's store, or sealed with
// rastrillo/crypto's SealSym — and hand the same bytes to Register or
// Verify; the browser sends it back inside clientDataJSON base64url'd,
// which those functions check for you.
func NewChallenge() ([]byte, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return b, nil
}
