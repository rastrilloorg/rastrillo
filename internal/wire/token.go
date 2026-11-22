package wire

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/base64"
	"strings"
)

// encodedSignature is the length of a 64-byte P-256 signature in unpadded
// base64url. Checking it before decoding refuses a padded or oversized
// signature without allocating for it.
const encodedSignature = 86

// Join is a token's wire form: the payload and the signature in unpadded
// base64url, joined by one dot.
func Join(payload, signature []byte) string {
	return base64.RawURLEncoding.EncodeToString(payload) + "." +
		base64.RawURLEncoding.EncodeToString(signature)
}

// Split undoes Join for a token of at most max bytes. It refuses a second
// dot, a non-canonical base64url spelling, and a signature that is not 64
// bytes, so two spellings of one token can never both verify.
func Split(token string, max int) (payload, signature []byte, ok bool) {
	if len(token) > max || strings.Count(token, ".") != 1 {
		return nil, nil, false
	}
	encodedPayload, encoded, _ := strings.Cut(token, ".")
	if len(encoded) != encodedSignature {
		return nil, nil, false
	}
	payload, ok = Decode(encodedPayload, max)
	if !ok {
		return nil, nil, false
	}
	signature, ok = Decode(encoded, max)
	if !ok || len(signature) != 64 {
		return nil, nil, false
	}
	return payload, signature, true
}

// TrustSet copies one to eight public keys by key id, refusing an id that
// is not an Identifier and a key that is not an uncompressed P-256 point.
// The copy means a caller that later changes its map changes nothing here.
func TrustSet(keys map[string][]byte) (map[string][]byte, bool) {
	if len(keys) < 1 || len(keys) > 8 {
		return nil, false
	}
	trusted := make(map[string][]byte, len(keys))
	for keyID, public := range keys {
		if !Identifier(keyID, 64) || len(public) != 65 {
			return nil, false
		}
		_, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), public)
		if err != nil {
			return nil, false
		}
		trusted[keyID] = append([]byte(nil), public...)
	}
	return trusted, true
}
