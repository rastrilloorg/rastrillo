// Package wire holds the token-format checks that assertion and roster must
// apply identically, because both guard the same root key. Fix them here once.
package wire

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/base64"
	"net"
	"strconv"
	"strings"
	"unicode/utf8"

	"amadan.net/rastrillo/rastrillo/crypto"
	json "github.com/go-json-experiment/json"
)

// ValidSigningKey reports whether key is a usable P-256 signing key.
func ValidSigningKey(key *crypto.Keypair) bool {
	if key == nil || key.SignPriv == nil {
		return false
	}
	private := key.SignPriv
	curve := elliptic.P256()
	//lint:ignore SA1019 Bytes would panic on a nil field
	unset := private.D == nil || private.X == nil || private.Y == nil
	if private.Curve != curve || unset {
		return false
	}
	// PrivateKey.Bytes refuses a scalar outside 1..N-1 and PublicKey.Bytes
	// a point that is not on the curve, and ParseRawPrivateKey re-derives
	// the public half from the scalar. Comparing the two encodings is the
	// on-curve check and the private/public consistency check at once -
	// what IsOnCurve plus ScalarBaseMult did.
	scalar, err := private.Bytes()
	if err != nil {
		return false
	}
	declared, err := private.PublicKey.Bytes()
	if err != nil {
		return false
	}
	derived, err := ecdsa.ParseRawPrivateKey(curve, scalar)
	if err != nil {
		return false
	}
	actual, err := derived.PublicKey.Bytes()
	if err != nil {
		return false
	}
	return bytes.Equal(declared, actual)
}

// Identifier reports whether text is 1..max bytes of letters, digits, - or _.
func Identifier(text string, max int) bool {
	if len(text) == 0 || len(text) > max {
		return false
	}
	for index := 0; index < len(text); index++ {
		char := text[index]
		if !alphaNum(char) && char != '-' && char != '_' {
			return false
		}
	}
	return true
}
func alphaNum(char byte) bool {
	return char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' ||
		char >= '0' && char <= '9'
}

// Decode accepts only the canonical unpadded base64url spelling.
func Decode(text string, max int) ([]byte, bool) {
	if !Identifier(text, max) {
		return nil, false
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(text)
	return raw, err == nil && base64.RawURLEncoding.EncodeToString(raw) == text
}

// Origin reports whether text is an exact HTTPS origin with a DNS name.
func Origin(text string) bool {
	if !strings.HasPrefix(text, "https://") {
		return false
	}
	host := strings.TrimPrefix(text, "https://")
	if name, port, found := strings.Cut(host, ":"); found {
		if port == "" || port[0] == '0' || port == "443" {
			return false
		}
		for index := 0; index < len(port); index++ {
			if port[index] < '0' || port[index] > '9' {
				return false
			}
		}
		number, err := strconv.ParseUint(port, 10, 16)
		if err != nil || number == 0 {
			return false
		}
		host = name
	}
	if len(host) == 0 || len(host) > 253 || net.ParseIP(host) != nil {
		return false
	}
	// Numeric final labels can be interpreted as noncanonical IPv4 by browser
	// URL parsers (for example 127.1); require a DNS suffix with a letter.
	labels := strings.Split(host, ".")
	last := labels[len(labels)-1]
	hasLetter := false
	for _, char := range []byte(last) {
		if char >= 'a' && char <= 'z' {
			hasLetter = true
		}
	}
	if strings.HasPrefix(last, "0x") {
		_, err := strconv.ParseUint(last[2:], 16, 64)
		if last == "0x" || err == nil {
			return false
		}
	}
	if !hasLetter {
		return false
	}
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' ||
			label[len(label)-1] == '-' {
			return false
		}
		for _, char := range []byte(label) {
			if !(char >= 'a' && char <= 'z' || char >= '0' && char <= '9' ||
				char == '-') {
				return false
			}
		}
	}
	return true
}

// Strict v2 parsing rejects duplicate names (including escaped spellings) and
// invalid Unicode recursively.
func StrictObject(raw []byte) bool {
	if len(raw) < 2 || raw[0] != '{' || raw[len(raw)-1] != '}' ||
		!utf8.Valid(raw) {
		return false
	}
	var value any
	return json.Unmarshal(raw, &value) == nil
}
