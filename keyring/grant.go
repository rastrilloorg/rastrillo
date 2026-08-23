package keyring

import (
	"crypto/ecdh"
	"fmt"

	"github.com/carlosframework/rastrillo/crypto"
)

// Grant wraps contentKey to a member's 65-byte uncompressed box public
// point: crypto.Seal(memberBoxPub, Namespace+"/grant/v1", contentKey)
// → ephPub(65) ‖ iv(12) ‖ ciphertext. One key, one instance, never the
// seed — a member holds what they were granted, not the root it came
// from.
//
// History, because the bytes almost lied: kass's coach grants fed raw
// ECDH bits straight into AES-GCM; crypto.Seal inserts HKDF with a
// context string — same outer layout, byte-incompatible keys. The
// keyring standardises on the HKDF envelope and carries no legacy
// decode path (ruled by Paul, 2026-08-23); kass re-wraps its grants
// client-side at next unlock, a ceremony it needs anyway.
//
// Revocation is the server deleting the grant row — nothing
// cryptographic here (kass's rule: "taking it back is deleting one
// row"). Re-keying after revocation, where an app wants it, is mint a
// new content key and re-grant to the remaining members; v1 adds no
// machinery for that ceremony.
func (r Ring) Grant(memberBoxPub, contentKey []byte) ([]byte, error) {
	return crypto.Seal(memberBoxPub, r.Namespace+"/grant/v1", contentKey)
}

// OpenGrant opens a Grant blob with the member's raw box private key —
// an ECDH P-256 private scalar, 32 bytes, the shape crypto's
// BoxPriv.Bytes() returns. It takes the box half only, not a full
// crypto.Keypair: kass member identities are ECDH-only pairs stored as
// pkcs8, and forcing a dummy sign half on them would make the
// migration path a lie. (The JS twin accepts the pkcs8/JWK import the
// app already holds.) crypto.Open reads only the box half, so a
// box-only Keypair is sound here, not a hack that happens to work.
func (r Ring) OpenGrant(memberBoxPriv, sealed []byte) ([]byte, error) {
	priv, err := ecdh.P256().NewPrivateKey(memberBoxPriv)
	if err != nil {
		return nil, fmt.Errorf("rastrillo/keyring: member box key: %w", err)
	}
	return crypto.Open(&crypto.Keypair{BoxPriv: priv}, r.Namespace+"/grant/v1", sealed)
}
