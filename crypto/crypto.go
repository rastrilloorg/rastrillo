// Package crypto is the family envelope (design doc §6): ECDH P-256
// ephemeral → HKDF-SHA256 → AES-256-GCM asymmetric sealing, ECDSA P-256
// signing with raw r‖s signatures, and the symmetric half (Derive,
// SealSym, OpenSym) — every operation domain-separated by a
// caller-supplied context string.
//
// Wire formats, byte-compatible with keymail's crypto.go, amadan's
// internal/envelope + internal/repokey, and seapointish's internal/seal
// (the three hand-rolled copies this package retires):
//
//	Seal/Open:       ephPub(65, uncompressed point) ‖ iv(12) ‖ AES-256-GCM ciphertext
//	Sign/Verify:     ECDSA P-256 over SHA-256(context ‖ 0x00 ‖ msg), raw r‖s (32+32, zero-padded)
//	SealSym/OpenSym: iv(12) ‖ AES-256-GCM ciphertext
//	Derive:          HKDF-SHA256, salt=nil, info=context, 32 bytes
//	DeriveInvite:    id/wrapKey/claimSecret = Derive(T, context+"-id"/"-wrap"/"-claim"),
//	                 claimHash = hex SHA-256(claimSecret); WrapKey/UnwrapKey = SealSym in base64url
//
// testdata/golden.json is amadan's pinned cross-implementation fixture
// (see amadan docs/superpowers/specs/2026-08-03-rastrillo-crypto-prompt.md);
// this package and its JS twin (js/crypto.mjs, see JS) must pass it, and
// any consumer replacing a local copy with this package keeps its own
// vectors as the proof of interchangeability.
//
// WrapKey/UnwrapKey and DeriveInvite (invite.go) waited until a
// consumer pinned their contract; eleven's messenger did
// (internal/e2ee, crypto.js deriveInvite), and testdata/invites.json
// carries its vectors verbatim — context "lchat-invite" reproduces its
// wire format byte for byte.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
)

// pubKeyLen is the length of an uncompressed P-256 point: 1 (0x04 prefix)
// + 32 (X) + 32 (Y).
const pubKeyLen = 65

// ivLen is the AES-GCM nonce length used throughout this package.
const ivLen = 12

// sigHalfLen is the length of each of r and s in a raw ECDSA signature.
const sigHalfLen = 32

// Keypair holds one party's dual keypair: an ECDSA P-256 signing key and
// an ECDH P-256 box (encryption) key.
type Keypair struct {
	SignPriv *ecdsa.PrivateKey // P-256
	BoxPriv  *ecdh.PrivateKey  // P-256
}

// Generate creates a fresh Keypair using crypto/rand.
func Generate() (*Keypair, error) {
	signPriv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("rastrillo/crypto: generate signing key: %w", err)
	}
	boxPriv, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("rastrillo/crypto: generate box key: %w", err)
	}
	return &Keypair{SignPriv: signPriv, BoxPriv: boxPriv}, nil
}

// SignPub returns the 65-byte uncompressed public signing key.
func (kp *Keypair) SignPub() []byte {
	return elliptic.Marshal(elliptic.P256(), kp.SignPriv.X, kp.SignPriv.Y)
}

// BoxPub returns the 65-byte uncompressed public box (ECDH) key.
func (kp *Keypair) BoxPub() []byte {
	return kp.BoxPriv.PublicKey().Bytes()
}

// Seal encrypts plaintext for recipientBoxPub (a 65-byte uncompressed
// P-256 point) under an ephemeral ECDH keypair, domain-separated by
// context. The output is ephPub(65) ‖ iv(12) ‖ ciphertext.
func Seal(recipientBoxPub []byte, context string, plaintext []byte) ([]byte, error) {
	curve := ecdh.P256()

	recipientPub, err := curve.NewPublicKey(recipientBoxPub)
	if err != nil {
		return nil, fmt.Errorf("rastrillo/crypto: invalid recipient box key: %w", err)
	}

	ephPriv, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("rastrillo/crypto: generate ephemeral key: %w", err)
	}

	shared, err := ephPriv.ECDH(recipientPub)
	if err != nil {
		return nil, fmt.Errorf("rastrillo/crypto: ECDH: %w", err)
	}

	gcm, err := newGCM(Derive(shared, context))
	if err != nil {
		return nil, err
	}

	iv := make([]byte, ivLen)
	if _, err := rand.Read(iv); err != nil {
		return nil, fmt.Errorf("rastrillo/crypto: generate IV: %w", err)
	}

	ct := gcm.Seal(nil, iv, plaintext, nil)

	ephPub := ephPriv.PublicKey().Bytes()
	out := make([]byte, 0, len(ephPub)+len(iv)+len(ct))
	out = append(out, ephPub...)
	out = append(out, iv...)
	out = append(out, ct...)
	return out, nil
}

// Open decrypts a blob produced by Seal, using kp's box private key. It
// returns an error if the blob is malformed, the context does not match,
// the recipient is wrong, or the ciphertext has been tampered with.
func Open(kp *Keypair, context string, sealed []byte) ([]byte, error) {
	if len(sealed) < pubKeyLen+ivLen {
		return nil, errors.New("rastrillo/crypto: sealed input too short")
	}

	ephPubBytes := sealed[:pubKeyLen]
	iv := sealed[pubKeyLen : pubKeyLen+ivLen]
	ct := sealed[pubKeyLen+ivLen:]

	ephPub, err := ecdh.P256().NewPublicKey(ephPubBytes)
	if err != nil {
		return nil, fmt.Errorf("rastrillo/crypto: invalid ephemeral public key: %w", err)
	}

	shared, err := kp.BoxPriv.ECDH(ephPub)
	if err != nil {
		return nil, fmt.Errorf("rastrillo/crypto: ECDH: %w", err)
	}

	gcm, err := newGCM(Derive(shared, context))
	if err != nil {
		return nil, err
	}

	plaintext, err := gcm.Open(nil, iv, ct, nil)
	if err != nil {
		return nil, errors.New("rastrillo/crypto: decryption failed (wrong key, wrong context, or tampered ciphertext)")
	}
	return plaintext, nil
}

// Sign produces a raw 64-byte r‖s ECDSA P-256 signature over
// SHA-256(context ‖ 0x00 ‖ msg).
func Sign(kp *Keypair, context string, msg []byte) ([]byte, error) {
	digest := signDigest(context, msg)
	r, s, err := ecdsa.Sign(rand.Reader, kp.SignPriv, digest)
	if err != nil {
		return nil, fmt.Errorf("rastrillo/crypto: sign: %w", err)
	}
	sig := make([]byte, sigHalfLen*2)
	r.FillBytes(sig[:sigHalfLen])
	s.FillBytes(sig[sigHalfLen:])
	return sig, nil
}

// Verify reports whether sig is a valid signature over msg under
// signPub (a 65-byte uncompressed P-256 point) and context.
func Verify(signPub []byte, context string, msg, sig []byte) bool {
	if len(signPub) != pubKeyLen || len(sig) != sigHalfLen*2 {
		return false
	}
	x, y := elliptic.Unmarshal(elliptic.P256(), signPub)
	if x == nil {
		return false
	}
	pub := &ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y}

	r := new(big.Int).SetBytes(sig[:sigHalfLen])
	s := new(big.Int).SetBytes(sig[sigHalfLen:])

	digest := signDigest(context, msg)
	return ecdsa.Verify(pub, digest, r, s)
}

func signDigest(context string, msg []byte) []byte {
	h := sha256.New()
	h.Write([]byte(context))
	h.Write([]byte{0x00})
	h.Write(msg)
	return h.Sum(nil)
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("rastrillo/crypto: AES cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("rastrillo/crypto: GCM: %w", err)
	}
	return gcm, nil
}

// keypairJSON is the on-disk shape for MarshalKeypair/UnmarshalKeypair.
// Private scalars are stored hex-encoded and zero-padded to the curve's
// field size (32 bytes for P-256) — the same shape amadan pins in its
// golden vectors, so a stored amadan key round-trips through this
// package unchanged.
type keypairJSON struct {
	SignPrivD string `json:"sign_priv_d"`
	BoxPrivD  string `json:"box_priv_d"`
}

// MarshalKeypair serializes kp as JSON for on-disk storage (mode 0600).
func MarshalKeypair(kp *Keypair) ([]byte, error) {
	signD := kp.SignPriv.D.FillBytes(make([]byte, 32))
	boxD := kp.BoxPriv.Bytes() // already 32 bytes for P-256

	out := keypairJSON{
		SignPrivD: hex.EncodeToString(signD),
		BoxPrivD:  hex.EncodeToString(boxD),
	}
	return json.Marshal(out)
}

// UnmarshalKeypair parses JSON produced by MarshalKeypair back into a
// Keypair with both private and (derived) public halves populated.
func UnmarshalKeypair(b []byte) (*Keypair, error) {
	var in keypairJSON
	if err := json.Unmarshal(b, &in); err != nil {
		return nil, fmt.Errorf("rastrillo/crypto: unmarshal keypair: %w", err)
	}

	signD, err := hex.DecodeString(in.SignPrivD)
	if err != nil {
		return nil, fmt.Errorf("rastrillo/crypto: decode sign_priv_d: %w", err)
	}
	boxD, err := hex.DecodeString(in.BoxPrivD)
	if err != nil {
		return nil, fmt.Errorf("rastrillo/crypto: decode box_priv_d: %w", err)
	}

	curve := elliptic.P256()
	d := new(big.Int).SetBytes(signD)
	x, y := curve.ScalarBaseMult(signD)
	signPriv := &ecdsa.PrivateKey{
		PublicKey: ecdsa.PublicKey{Curve: curve, X: x, Y: y},
		D:         d,
	}

	boxPriv, err := ecdh.P256().NewPrivateKey(boxD)
	if err != nil {
		return nil, fmt.Errorf("rastrillo/crypto: reconstruct box key: %w", err)
	}

	return &Keypair{SignPriv: signPriv, BoxPriv: boxPriv}, nil
}

// Derive expands key into a 32-byte subkey via HKDF-SHA256 (salt=nil,
// info=context). It is deterministic: the same (key, context) pair
// always yields the same subkey. Seal and Open use it to turn an ECDH
// shared secret into the AES key; apps use it to fan one root secret
// into per-purpose subkeys.
func Derive(key []byte, context string) []byte {
	out, err := hkdf.Key(sha256.New, key, nil, context, 32)
	if err != nil {
		// hkdf.Key only errors when the requested length exceeds the
		// hash's expand limit (255×hash size); 32 never does for
		// SHA-256. Treat it as unreachable rather than widen the API
		// with an error return every caller would have to check.
		panic(fmt.Sprintf("rastrillo/crypto: HKDF derive: %v", err))
	}
	return out
}

// NewKey generates a fresh 32-byte symmetric key using crypto/rand.
func NewKey() ([]byte, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("rastrillo/crypto: generate key: %w", err)
	}
	return key, nil
}

// SealSym encrypts plaintext under key (32 bytes, e.g. a Derive output)
// with a random 12-byte IV. Output is iv(12) ‖ AES-256-GCM ciphertext.
func SealSym(key, plaintext []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}

	iv := make([]byte, ivLen)
	if _, err := rand.Read(iv); err != nil {
		return nil, fmt.Errorf("rastrillo/crypto: generate IV: %w", err)
	}

	ct := gcm.Seal(nil, iv, plaintext, nil)

	out := make([]byte, 0, len(iv)+len(ct))
	out = append(out, iv...)
	out = append(out, ct...)
	return out, nil
}

// OpenSym decrypts a blob produced by SealSym under key.
func OpenSym(key, sealed []byte) ([]byte, error) {
	if len(sealed) < ivLen {
		return nil, errors.New("rastrillo/crypto: sealed input too short")
	}

	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}

	plaintext, err := gcm.Open(nil, sealed[:ivLen], sealed[ivLen:], nil)
	if err != nil {
		return nil, errors.New("rastrillo/crypto: decryption failed (wrong key or tampered ciphertext)")
	}
	return plaintext, nil
}
