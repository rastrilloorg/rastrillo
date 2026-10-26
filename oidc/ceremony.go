package oidc

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"amadan.net/rastrillo/rastrillo/crypto"
)

// Start is the three unguessable values that bind one ceremony together,
// minted before the browser leaves and needed again when it comes back.
// Ported from ips's start.go.
type Start struct {
	// State is echoed by the provider and compared on return. It is what
	// ties the answer to a ceremony this browser began, and it is the
	// whole defence against somebody else's completed sign-in being
	// replayed into this browser.
	State string

	// Nonce travels inside the ID token's signature. State proves the
	// REQUEST was ours; the nonce proves the TOKEN was minted for it.
	Nonce string

	// Verifier is the PKCE secret. Only its hash leaves this process on
	// the way out, so an authorization code intercepted in a redirect
	// cannot be exchanged by whoever intercepted it.
	Verifier string
}

// NewStart mints one. An error here is refused rather than worked around:
// a ceremony whose randomness silently fell back to something weaker is
// worse than one that did not start, because nothing would say so.
func NewStart() (Start, error) {
	var s Start
	for _, into := range []*string{&s.State, &s.Nonce, &s.Verifier} {
		v, err := randomToken()
		if err != nil {
			return Start{}, err
		}
		*into = v
	}
	return s, nil
}

// Challenge is the S256 code challenge for this ceremony's verifier —
// what actually travels to the provider.
func (s Start) Challenge() string {
	sum := sha256.Sum256([]byte(s.Verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// Valid reports whether every part of this Start is present. A zero value
// read back from an expired or unreadable cookie must not be allowed to
// satisfy a comparison against another zero value.
func (s Start) Valid() bool {
	return s.State != "" && s.Nonce != "" && s.Verifier != ""
}

// ErrStateMismatch means the value the provider echoed is not the one
// this browser set out with.
var ErrStateMismatch = errors.New("oidc: the provider returned a different state from the one this sign-in began with")

// CheckState compares the returned state against this ceremony's, in
// constant time.
//
// Constant time is not theatre here even though the value is unguessable
// rather than secret: it costs one function call, and the alternative is
// a comparison somebody has to reason about again in five years. It also
// refuses an empty Start outright, so a cookie that expired mid-ceremony
// cannot be satisfied by a provider echoing nothing.
func (s Start) CheckState(returned string) error {
	if !s.Valid() || returned == "" {
		return ErrStateMismatch
	}
	if subtle.ConstantTimeCompare([]byte(s.State), []byte(returned)) != 1 {
		return ErrStateMismatch
	}
	return nil
}

// randomToken is 32 bytes of crypto/rand, base64url. It is the length
// PKCE's own specification asks of a verifier at its strongest, and it is
// used for all three Start values so there is one answer to "how long is
// this" rather than three.
func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// ── the sealed ceremony cookie (design D4) ─────────────────────────────

// ceremonyTTL bounds a ceremony from the inside, checked against the
// clock rather than trusted from the cookie's own Max-Age — ips's D88.5
// found the opposite the hard way: a cookie carrying only Max-Age makes
// the TTL a property of the browser, and a cookie exported from a
// profile, or a client that ignores the attribute, would present a
// year-old ceremony and be believed.
const ceremonyTTL = 10 * time.Minute

// ceremony is what the sealed cookie carries across the round trip to the
// provider and back.
type ceremony struct {
	Start
	// Key is which of an app's connections this ceremony is for — opaque
	// to this package, read once at Begin via Config.ConnectionKey and
	// never asked for again, so Return resolves the SAME tenant with
	// nothing taken from the provider's own callback request (review
	// finding 4).
	Key ConnectionKey
	// ReturnTo is validated same-site at Begin (sessions.SafeReturnTo)
	// and carried through sealed, because the provider's callback
	// carries nothing of the sort and a lost return_to would land every
	// deep link on SignedInPath (review finding 10).
	ReturnTo string
	// Reauth records that Begin sent prompt=login&max_age=0 because this
	// ceremony is satisfying a step-up (RequireFresh's ?reauth=1), so
	// Return knows to hold the resulting identity to freshWindow rather
	// than accepting any auth_time at all (design D6, review finding 8).
	Reauth  bool
	Expires time.Time
}

// ceremonyKey derives this provider kind's AEAD key from the app's
// instance key. Binding to the KIND ONLY — not to ConnectionKey, which
// lives inside the sealed plaintext instead — is what makes "a ceremony
// sealed for google cannot be opened on microsoft's return path" true by
// construction: GET /signin/oidc/microsoft/return derives a different key
// than GET /signin/oidc/google/return did, so OpenSym simply fails
// regardless of what either URL's own path parameter claims.
func ceremonyKey(instanceKey string, kind Kind) []byte {
	return crypto.Derive([]byte(instanceKey), "rastrillo/oidc/ceremony/v1:"+string(kind))
}

// sealCeremony seals c for cookie storage.
func sealCeremony(instanceKey string, kind Kind, c ceremony) (string, error) {
	plaintext, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	sealed, err := crypto.SealSym(ceremonyKey(instanceKey, kind), plaintext)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

// errCeremonyExpired/errCeremonyInvalid are not exported: every row in
// handlers.go's error-path table that can reach these shows the same
// generic "that sign-in took too long" page regardless of which one it
// was, by design — nothing about WHY a ceremony failed to open is ever
// disclosed to the caller, only logged.
var errCeremonyInvalid = errors.New("oidc: that sign-in's ceremony could not be opened")

// openCeremony reverses sealCeremony and checks the EXPIRY INSIDE THE
// BLOB against now — the actual bound; the cookie's own Max-Age is
// defence in depth only, never trusted alone (see ceremonyTTL's comment).
func openCeremony(instanceKey string, kind Kind, value string, now time.Time) (ceremony, error) {
	sealed, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return ceremony{}, fmt.Errorf("%w", errCeremonyInvalid)
	}
	plaintext, err := crypto.OpenSym(ceremonyKey(instanceKey, kind), sealed)
	if err != nil {
		return ceremony{}, fmt.Errorf("%w", errCeremonyInvalid)
	}
	var c ceremony
	if err := json.Unmarshal(plaintext, &c); err != nil {
		return ceremony{}, fmt.Errorf("%w", errCeremonyInvalid)
	}
	if !c.Start.Valid() || now.After(c.Expires) {
		return ceremony{}, fmt.Errorf("%w", errCeremonyInvalid)
	}
	return c, nil
}
