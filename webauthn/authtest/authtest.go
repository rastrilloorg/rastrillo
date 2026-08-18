// Package authtest is a passkey authenticator for tests.
//
// It builds ceremonies to the spec rather than replaying fixtures nobody can
// regenerate, and every knob a test needs to break — the wrong relying party, a
// missing user-verified flag, a stale counter, a corrupted signature — is an
// option here rather than a hand-assembled byte slice at the call site.
//
// Test-only, and in its own package so none of it can reach a binary.
package authtest

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"fmt"
)

// Flags in authenticator data (WebAuthn §6.1).
const (
	FlagUserPresent  = 1 << 0
	FlagUserVerified = 1 << 2
	FlagAttestedData = 1 << 6

	AlgES256 = -7
)

// --- the smallest CBOR writer that can build an attestation object ---

func head(major byte, arg uint64) []byte {
	switch {
	case arg < 24:
		return []byte{major<<5 | byte(arg)}
	case arg < 1<<8:
		return []byte{major<<5 | 24, byte(arg)}
	case arg < 1<<16:
		b := []byte{major<<5 | 25, 0, 0}
		binary.BigEndian.PutUint16(b[1:], uint16(arg))
		return b
	default:
		b := []byte{major<<5 | 26, 0, 0, 0, 0}
		binary.BigEndian.PutUint32(b[1:], uint32(arg))
		return b
	}
}

// Int, Bytes, Text and Map are exported so a test can build a deliberately
// malformed structure without a second CBOR writer.
func Int(i int64) []byte {
	if i >= 0 {
		return head(0, uint64(i))
	}
	return head(1, uint64(-1-i))
}
func Bytes(b []byte) []byte { return append(head(2, uint64(len(b))), b...) }
func Text(s string) []byte  { return append(head(3, uint64(len(s))), s...) }
func Head(major byte, arg uint64) []byte {
	return head(major, arg)
}
func Map(pairs ...[]byte) []byte {
	out := head(5, uint64(len(pairs)/2))
	for _, p := range pairs {
		out = append(out, p...)
	}
	return out
}

// --- the authenticator ---

// Authenticator is one passkey: a key pair, a credential id, and a counter.
type Authenticator struct {
	Key    *ecdsa.PrivateKey
	CredID []byte
	Count  uint32
}

// New mints an authenticator with a fresh ES256 key.
func New() (*Authenticator, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	id := make([]byte, 32)
	if _, err := rand.Read(id); err != nil {
		return nil, err
	}
	return &Authenticator{Key: key, CredID: id}, nil
}

// Options are the knobs a test turns to produce a ceremony that should fail.
// The zero value is a well-formed ceremony for the given rpID and origin.
type Options struct {
	RPID          string
	Origin        string
	Flags         byte   // zero means present + verified (+ attested, on create)
	SignChallenge []byte // sign a different challenge than the server expects
	CorruptSig    bool
	Count         *uint32 // force the counter, e.g. to replay a stale one
	NoCredential  bool    // registration with the attested-data flag cleared
}

func (o Options) flags(create bool) byte {
	if o.Flags != 0 {
		return o.Flags
	}
	f := byte(FlagUserPresent | FlagUserVerified)
	if create && !o.NoCredential {
		f |= FlagAttestedData
	}
	return f
}

func (o Options) challenge(real []byte) []byte {
	if o.SignChallenge != nil {
		return o.SignChallenge
	}
	return real
}

// COSEKey is the credential public key as the authenticator reports it.
func (a *Authenticator) COSEKey() []byte {
	return Map(
		Int(1), Int(2), // kty: EC2
		Int(3), Int(AlgES256),
		Int(-1), Int(1), // crv: P-256
		Int(-2), Bytes(a.Key.X.FillBytes(make([]byte, 32))),
		Int(-3), Bytes(a.Key.Y.FillBytes(make([]byte, 32))),
	)
}

// AuthData assembles authenticator data, with credential data when asked.
func (a *Authenticator) AuthData(rpID string, flags byte, withCredential bool) []byte {
	h := sha256.Sum256([]byte(rpID))
	out := append([]byte{}, h[:]...)
	out = append(out, flags)
	out = binary.BigEndian.AppendUint32(out, a.Count)
	if withCredential {
		out = append(out, make([]byte, 16)...) // aaguid
		out = binary.BigEndian.AppendUint16(out, uint16(len(a.CredID)))
		out = append(out, a.CredID...)
		out = append(out, a.COSEKey()...)
	}
	return out
}

// ClientData builds a clientDataJSON document for a ceremony.
func ClientData(typ, origin string, challenge []byte) []byte {
	return []byte(fmt.Sprintf(`{"type":%q,"challenge":%q,"origin":%q,"crossOrigin":false}`,
		typ, base64.RawURLEncoding.EncodeToString(challenge), origin))
}

// Create runs a registration ceremony and returns what the browser would post.
func (a *Authenticator) Create(challenge []byte, o Options) (clientDataJSON, attestationObject []byte) {
	flags := o.flags(true)
	authData := a.AuthData(o.RPID, flags, flags&FlagAttestedData != 0)
	attestation := Map(
		Text("fmt"), Text("none"),
		Text("attStmt"), Map(),
		Text("authData"), Bytes(authData),
	)
	return ClientData("webauthn.create", o.Origin, o.challenge(challenge)), attestation
}

// Get runs an assertion ceremony and returns what the browser would post.
func (a *Authenticator) Get(challenge []byte, o Options) (clientDataJSON, authData, signature []byte, err error) {
	a.Count++
	if o.Count != nil {
		a.Count = *o.Count
	}
	authData = a.AuthData(o.RPID, o.flags(false), false)
	clientDataJSON = ClientData("webauthn.get", o.Origin, o.challenge(challenge))

	clientHash := sha256.Sum256(clientDataJSON)
	digest := sha256.Sum256(append(append([]byte{}, authData...), clientHash[:]...))
	signature, err = ecdsa.SignASN1(rand.Reader, a.Key, digest[:])
	if err != nil {
		return nil, nil, nil, err
	}
	if o.CorruptSig {
		signature[len(signature)-1] ^= 0xff
	}
	return clientDataJSON, authData, signature, nil
}
