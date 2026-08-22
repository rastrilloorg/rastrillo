package crypto

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// Invite is DeriveInvite's result: three independent values fanned out
// of one link-fragment secret, each HKDF-domain-separated, so a server
// that stores only ID and ClaimHash can neither claim the invite nor
// unwrap anything sealed under WrapKey.
//
// The scheme is eleven's (messenger internal/e2ee, crypto.js
// deriveInvite), generalized by the context string the rest of this
// package already uses — eleven is context "lchat-invite", and
// testdata/invites.json pins byte-compatibility with its vectors.
type Invite struct {
	// ID is the server-side lookup key (base64url).
	ID string
	// WrapKey is the 32-byte AES key invite payloads are sealed under
	// (WrapKey/UnwrapKey below, or SealSym directly).
	WrapKey []byte
	// ClaimSecret is presented when claiming; knowing it proves
	// knowledge of the invite secret (base64url).
	ClaimSecret string
	// ClaimHash is what the server stores: lowercase hex SHA-256 of
	// ClaimSecret. Comparing hashes lets the server verify a claim
	// without ever holding the secret.
	ClaimHash string
}

// NewInviteSecret mints a fresh 128-bit invite secret, base64url —
// sized to ride a URL fragment, which is the point: the fragment never
// reaches the server.
func NewInviteSecret() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("rastrillo/crypto: generate invite secret: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// DeriveInvite fans secret (base64url, padding tolerated) into an
// Invite. Everything is deterministic: the same (context, secret) pair
// always yields the same Invite, which is what lets the inviter and
// the claimer derive identical values from a shared link fragment.
// The HKDF info strings are context+"-id", context+"-wrap" and
// context+"-claim" (RFC 5869, salt nil, as Derive).
func DeriveInvite(context, secret string) (Invite, error) {
	t, err := decodeB64url(secret)
	if err != nil {
		return Invite{}, fmt.Errorf("rastrillo/crypto: invite secret: %w", err)
	}
	id := Derive(t, context+"-id")
	wrapKey := Derive(t, context+"-wrap")
	claim := Derive(t, context+"-claim")

	claimSecret := base64.RawURLEncoding.EncodeToString(claim)
	sum := sha256.Sum256([]byte(claimSecret))
	return Invite{
		ID:          base64.RawURLEncoding.EncodeToString(id),
		WrapKey:     wrapKey,
		ClaimSecret: claimSecret,
		ClaimHash:   hex.EncodeToString(sum[:]),
	}, nil
}

// WrapKey seals key under kek and returns it base64url — SealSym's
// wire format (iv(12) ‖ AES-256-GCM ciphertext) in the encoding invite
// payloads travel in. kek is typically an Invite's WrapKey.
func WrapKey(kek, key []byte) (string, error) {
	sealed, err := SealSym(kek, key)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

// UnwrapKey reverses WrapKey: decode, open, return the raw key. A
// wrong kek or tampered blob fails the way OpenSym fails.
func UnwrapKey(kek []byte, wrapped string) ([]byte, error) {
	sealed, err := decodeB64url(wrapped)
	if err != nil {
		return nil, fmt.Errorf("rastrillo/crypto: wrapped key: %w", err)
	}
	return OpenSym(kek, sealed)
}

// decodeB64url decodes base64url with or without padding — the JS side
// strips padding (bytesToB64url), and tolerating it here means a value
// that passed through a padder still parses.
func decodeB64url(s string) ([]byte, error) {
	for len(s)%4 != 0 {
		s += "="
	}
	return base64.URLEncoding.DecodeString(s)
}
