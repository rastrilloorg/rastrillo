// Package keyring owns the E2EE seed lifecycle the crypto package
// leaves to apps: one 32-byte seed per person, HKDF purpose derivation
// namespaced by Ring, the seed wrapped under a passkey's PRF output,
// content keys granted to members' box keys, and the wraps guard that
// keeps the last wrap unrevokable. Kass and messenger each re-solved
// this by hand; this package exists before a third app writes a third
// variant (issue #79).
//
// Two rulings bind everything here. No new cryptography: every
// operation composes crypto's existing, golden-vectored primitives —
// Derive, SealSym/OpenSym, Seal/Open — and testdata/golden.json pins
// the composition in both languages. The keyring adds names, formats
// and ceremonies, not ciphers. No storage: the keyring is pure
// functions plus wire formats, here and in the JS twin (see JS).
// Tables — wrapped seeds, grant rows, member public keys — belong to
// the app, or to the sealed store (#77).
//
// The one contract the package does not build but every consumer must
// honour: wrapped seeds live server-side, per credential. Store a
// wrapped seed keyed by credential ID; return it at sign-in; accept a
// new one at enrol. Kass's finishEnrol/sign-in-finish pair is the
// reference shape, and #77 is where a generated version of that
// surface belongs. Without those routes, device add and the RPID-move
// drill (see Ring.WrapSeed) have nowhere to put the new wrap.
package keyring

import (
	"fmt"

	"amadan.net/rastrillo/rastrillo/crypto"
)

// Ring namespaces every keyring operation for one app. Every context
// string is derived from Namespace — ns/prf/v1, ns/content/v1,
// ns/wrap/v1, ns/grant/v1 — so two apps on one keyring package can
// never collide, and kass's existing strings fall out as the
// Ring{"kass"} case.
//
// Compatibility promise, verified against kass: its deriveBytes is
// HKDF-SHA256 with a zero-length salt and a UTF-8 info string, and
// crypto.Derive passes a nil salt — identical output, because RFC 5869
// treats absent and zero-length salt the same (HMAC zero-pads either
// to the block size), and the Go↔JS agreement is already pinned by
// crypto's golden vectors. Kass's stored wrapped_seeds and
// seed-derived content keys are byte-identical under Ring{"kass"}.
type Ring struct {
	// Namespace is the app's name, e.g. "kass". It prefixes every
	// context string this Ring derives; changing it is a format break
	// for everything already wrapped or granted.
	Namespace string
}

// PRFSalt is the WebAuthn PRF-extension eval input for this ring:
// Namespace + "/prf/v1". The credential's PRF output evaluated under
// it is what WrapKey expects — one salt per app, so two apps sharing
// a passkey never share a wrap key.
func (r Ring) PRFSalt() string { return r.Namespace + "/prf/v1" }

// ContentKey derives the ring's content key from the seed:
// crypto.Derive(seed, Namespace+"/content/v1"). Deterministic — the
// seed is the durable secret, and the content key is a name for one
// of its purposes, not a second secret to store.
func (r Ring) ContentKey(seed []byte) []byte {
	return crypto.Derive(seed, r.Namespace+"/content/v1")
}

// BlobKey derives the sealing key for one named vault blob:
// crypto.Derive(seed, Namespace+"/blob/"+name+"/v1") — Woodstar's
// woodstar/blob/v1 generalised per name. Deterministic, like
// ContentKey: a blob key is a name for one of the seed's purposes,
// not a second secret. The name lands inside the context string, so
// callers must validate it first (the vault package's closed
// namespace restricts names to [a-z0-9-]{1,64} at construction);
// keyring derives what it is given.
func (r Ring) BlobKey(seed []byte, name string) []byte {
	return crypto.Derive(seed, r.Namespace+"/blob/"+name+"/v1")
}

// WrapKey derives the seed-wrapping key from a credential's PRF
// output: crypto.Derive(prf, Namespace+"/wrap/v1"). WrapSeed and
// UnwrapSeed compose it; it is exported because the JS twin's caller
// sometimes wants the key without the wrap (kass's unlock path).
func (r Ring) WrapKey(prf []byte) []byte {
	return crypto.Derive(prf, r.Namespace+"/wrap/v1")
}

// NewSeed mints a fresh 32-byte seed — the one per-person root every
// content key derives from. It is crypto.NewKey by another name: the
// seed is not itself a cipher key, and the separate name keeps call
// sites honest about which of the two they hold.
func NewSeed() ([]byte, error) {
	seed, err := crypto.NewKey()
	if err != nil {
		return nil, fmt.Errorf("rastrillo/keyring: new seed: %w", err)
	}
	return seed, nil
}
