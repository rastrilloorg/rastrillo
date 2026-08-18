// Package webauthn verifies passkey registrations and assertions — the
// extraction design doc §7 names: kass and slopbox carried duplicate
// copies of this package (kass's internal/webauthn is the source lifted
// here, tests and all), and each thing it leaves out is a thing that
// cannot be got wrong.
//
// Deliberately narrow: ES256 (COSE alg -7) credentials, no attestation
// statement checking, no extensions the server has to understand. That covers
// every platform authenticator we care about. Anything outside the subset is
// rejected rather than waved through. The CBOR reader next door covers
// exactly the subset WebAuthn uses, hand-rolled because a dependency on the
// authentication path is a far larger surface to audit than eighty lines.
//
// The passkey does two jobs, and only one of them is here. This package is
// the *identity* half — proving the person is who they say — while sealing
// *content* is rastrillo/crypto's job. In an E2EE app the PRF extension
// output stays in the browser, unwrapping keys the server never holds: a
// total compromise of this package yields sessions, never content. Ceremony
// challenges are yours to mint (32 random bytes) and to keep across the
// round trip — server-side in the app's store, or sealed into a cookie with
// crypto.SealSym. The browser half ships alongside as an embedded, reusable
// ES module (js/webauthn.mjs, exported via JS) — the acknowledged JS
// dependency, vendored once instead of hand-copied per app.
//
// The authtest sub-package is the matching test authenticator: it builds
// real ceremonies to the spec, with a knob for every way one should fail.
package webauthn

import (
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
)

// Authenticator data flags (WebAuthn §6.1).
const (
	flagUserPresent  = 1 << 0
	flagUserVerified = 1 << 2
	flagAttestedData = 1 << 6
)

const coseAlgES256 = -7

// Credential is what the server stores for a passkey. None of it is secret:
// the public key verifies signatures and nothing else, and losing the whole
// table costs sessions, not content.
type Credential struct {
	ID        []byte `json:"id"`
	PublicKey []byte `json:"public_key"` // uncompressed EC point: 0x04 ‖ X ‖ Y
	SignCount uint32 `json:"sign_count"`
}

// Config pins the things every ceremony is checked against.
type Config struct {
	RPID   string // e.g. "app.example.com"
	Origin string // e.g. "https://app.example.com"

	// LegacyRPID is a relying party this instance used to be, accepted for
	// assertions and never for registration. It exists to move an instance to a
	// new hostname without stranding anybody's data.
	//
	// A passkey is bound to its relying party. Change the hostname and the
	// old credential no longer matches — which in an E2EE app does not merely
	// lock somebody out of an account: content keys wrapped under the old
	// passkey's PRF output become unreachable, because the server holds only
	// bytes it cannot open. Accepting the old relying party for sign-in
	// leaves a door open long enough for those keys to be re-wrapped under a
	// new passkey on the new name.
	//
	// Registration deliberately does not accept it: every new credential is
	// minted under the name we are moving to, so the crossover drains rather
	// than filling back up.
	LegacyRPID string
}

var (
	ErrChallenge   = errors.New("challenge does not match")
	ErrOrigin      = errors.New("origin does not match")
	ErrRPID        = errors.New("relying party does not match")
	ErrNotVerified = errors.New("the authenticator did not verify the user")
	ErrSignature   = errors.New("signature does not verify")
	ErrCounter     = errors.New("signature counter went backwards")
)

// clientData is the subset of clientDataJSON we check. Unknown fields are
// ignored by design — the signature covers the whole document either way.
type clientData struct {
	Type      string `json:"type"`
	Challenge string `json:"challenge"`
	Origin    string `json:"origin"`
}

func (c Config) checkClientData(raw []byte, wantType string, challenge []byte) error {
	var cd clientData
	if err := json.Unmarshal(raw, &cd); err != nil {
		return fmt.Errorf("clientDataJSON is not JSON: %w", err)
	}
	if cd.Type != wantType {
		return fmt.Errorf("expected a %q ceremony, got %q", wantType, cd.Type)
	}
	want := base64.RawURLEncoding.EncodeToString(challenge)
	if subtle.ConstantTimeCompare([]byte(cd.Challenge), []byte(want)) != 1 {
		return ErrChallenge
	}
	if cd.Origin != c.Origin {
		return fmt.Errorf("%w: %q", ErrOrigin, cd.Origin)
	}
	return nil
}

// checkAuthData verifies the parts of authenticator data that do not depend on
// the ceremony: which relying party it was for, and that a human was actually
// there and verified.
//
// allowLegacy is true only for assertions. See Config.LegacyRPID.
func (c Config) checkAuthData(authData []byte, allowLegacy bool) error {
	if len(authData) < 37 {
		return errors.New("authenticator data is too short")
	}
	if !c.rpMatches(authData[:32], allowLegacy) {
		return ErrRPID
	}
	flags := authData[32]
	if flags&flagUserPresent == 0 {
		return fmt.Errorf("%w: nobody was present", ErrNotVerified)
	}
	if flags&flagUserVerified == 0 {
		return ErrNotVerified
	}
	return nil
}

// rpMatches compares the relying party hash the authenticator signed against
// the one this instance is, and — for assertions only — the one it used to be.
// Constant time throughout, and both are always compared, so a mismatch cannot
// be told from a legacy match by how long the answer took.
func (c Config) rpMatches(got []byte, allowLegacy bool) bool {
	current := sha256.Sum256([]byte(c.RPID))
	ok := subtle.ConstantTimeCompare(got, current[:]) == 1

	if allowLegacy && c.LegacyRPID != "" {
		legacy := sha256.Sum256([]byte(c.LegacyRPID))
		if subtle.ConstantTimeCompare(got, legacy[:]) == 1 {
			ok = true
		}
	}
	return ok
}

// Register verifies a creation ceremony and returns the credential to store.
//
// The attestation statement is deliberately not examined. We are not trying to
// establish what *kind* of authenticator this is — only that a passkey now
// exists and here is the key that will speak for it.
func (c Config) Register(challenge, clientDataJSON, attestationObject []byte) (Credential, error) {
	if err := c.checkClientData(clientDataJSON, "webauthn.create", challenge); err != nil {
		return Credential{}, err
	}

	att, err := cborMap(attestationObject)
	if err != nil {
		return Credential{}, fmt.Errorf("attestation object: %w", err)
	}
	authData, ok := mapBytes(att, "authData")
	if !ok {
		return Credential{}, errors.New("attestation object has no authenticator data")
	}
	// Registration is strict: a new credential is always minted under the name
	// this instance is now, never the one it used to be.
	if err := c.checkAuthData(authData, false); err != nil {
		return Credential{}, err
	}
	if authData[32]&flagAttestedData == 0 {
		return Credential{}, errors.New("registration carried no credential")
	}

	// attestedCredentialData: aaguid(16) ‖ idLen(2) ‖ id ‖ COSE key
	rest := authData[37:]
	if len(rest) < 18 {
		return Credential{}, errors.New("credential data is truncated")
	}
	idLen := int(rest[16])<<8 | int(rest[17])
	rest = rest[18:]
	if idLen == 0 || idLen > len(rest) {
		return Credential{}, errors.New("credential id length is out of range")
	}
	credID := make([]byte, idLen)
	copy(credID, rest[:idLen])

	pub, err := parseCOSEKey(rest[idLen:])
	if err != nil {
		return Credential{}, err
	}
	return Credential{ID: credID, PublicKey: pub, SignCount: signCount(authData)}, nil
}

// Verify checks an assertion against a stored credential and returns the new
// signature counter to persist.
func (c Config) Verify(cred Credential, challenge, clientDataJSON, authData, signature []byte) (uint32, error) {
	if err := c.checkClientData(clientDataJSON, "webauthn.get", challenge); err != nil {
		return 0, err
	}
	if err := c.checkAuthData(authData, true); err != nil {
		return 0, err
	}

	pub, err := publicKey(cred.PublicKey)
	if err != nil {
		return 0, err
	}
	clientHash := sha256.Sum256(clientDataJSON)
	signed := append(append([]byte{}, authData...), clientHash[:]...)
	digest := sha256.Sum256(signed)
	if !ecdsa.VerifyASN1(pub, digest[:], signature) {
		return 0, ErrSignature
	}

	// A counter that goes backwards means the credential has been cloned. Many
	// authenticators never implement it and report zero forever; that is not a
	// signal, so only a *moving* counter is held to the rule.
	next := signCount(authData)
	if cred.SignCount != 0 || next != 0 {
		if next <= cred.SignCount {
			return 0, ErrCounter
		}
	}
	return next, nil
}

func signCount(authData []byte) uint32 {
	return uint32(authData[33])<<24 | uint32(authData[34])<<16 | uint32(authData[35])<<8 | uint32(authData[36])
}

// parseCOSEKey pulls an uncompressed P-256 point out of a COSE_Key, rejecting
// anything that is not an ES256 key on that curve.
func parseCOSEKey(b []byte) ([]byte, error) {
	m, err := cborMap(b)
	if err != nil {
		return nil, fmt.Errorf("credential public key: %w", err)
	}
	if kty, _ := mapInt(m, int64(1)); kty != 2 {
		return nil, fmt.Errorf("credential key type %d is not EC2", kty)
	}
	if alg, _ := mapInt(m, int64(3)); alg != coseAlgES256 {
		return nil, fmt.Errorf("credential algorithm %d is not ES256", alg)
	}
	if crv, _ := mapInt(m, int64(-1)); crv != 1 {
		return nil, fmt.Errorf("credential curve %d is not P-256", crv)
	}
	x, okX := mapBytes(m, int64(-2))
	y, okY := mapBytes(m, int64(-3))
	if !okX || !okY || len(x) != 32 || len(y) != 32 {
		return nil, errors.New("credential public key is malformed")
	}
	point := append([]byte{4}, append(x, y...)...)
	if _, err := publicKey(point); err != nil {
		return nil, err
	}
	return point, nil
}

// publicKey turns a stored point back into a verifier, rejecting anything not
// actually on the curve — ecdh does that check for us.
func publicKey(point []byte) (*ecdsa.PublicKey, error) {
	if _, err := ecdh.P256().NewPublicKey(point); err != nil {
		return nil, fmt.Errorf("public key is not a point on P-256: %w", err)
	}
	return &ecdsa.PublicKey{
		Curve: elliptic.P256(),
		X:     new(big.Int).SetBytes(point[1:33]),
		Y:     new(big.Int).SetBytes(point[33:65]),
	}, nil
}
