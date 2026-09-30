// Package sealedcookie is the one envelope the sign-in screen's cookies
// share: "v1." + base64url(AES-256-GCM(JSON)).
//
// auth's attempt and continuation cookies and lastsignin's remembered
// cookie each carry something the browser must neither read nor edit —
// an address, an authorize URL — and each must expire on a clock the
// browser cannot move. Three hand-written envelopes would agree until
// the day one of them was edited; this is the one they share.
//
// crypto.SealSym checks nothing about time, so expiry lives in the
// payload and Fresh is the one place those times are judged. A cookie's
// Max-Age is a courtesy to the browser and is never trusted: a copied
// value presented after its Max-Age must still pass Fresh.
package sealedcookie

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"amadan.net/rastrillo/rastrillo/crypto"
)

// version prefixes every value. A value without it — a future v2, or a
// cookie older code wrote under the same name — is refused before any
// decryption, so a format change can never be misread as this one.
const version = "v1."

// ErrInvalid is every way a value can fail to open. One error on
// purpose: the caller's only decision is "treat it as absent and clear
// it", and a caller able to tell tampering from expiry would be tempted
// to say which to the browser.
var ErrInvalid = errors.New("sealedcookie: invalid value")

// FutureSkew is how far ahead of this server's clock an issue time may
// sit and still be believed. A minute absorbs drift between instances
// behind one origin; more, and a value sealed by a fast clock would
// outlive its cap by the difference.
const FutureSkew = time.Minute

// Seal encrypts payload's JSON under key (32 bytes, a crypto.Derive
// output, one context per cookie so no cookie can open another's).
func Seal(key []byte, payload any) (string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sealed, err := crypto.SealSym(key, raw)
	if err != nil {
		return "", err
	}
	return version + base64.RawURLEncoding.EncodeToString(sealed), nil
}

// Open decrypts value into payload. Decoding is strict — an unknown
// field or trailing data is refused — so a payload some other code or
// version wrote can never be half-read as this one.
func Open(key []byte, value string, payload any) error {
	body, ok := strings.CutPrefix(value, version)
	if !ok {
		return ErrInvalid
	}
	sealed, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return ErrInvalid
	}
	raw, err := crypto.OpenSym(key, sealed)
	if err != nil {
		return ErrInvalid
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(payload); err != nil {
		return ErrInvalid
	}
	if _, err := dec.Token(); err != io.EOF {
		return ErrInvalid
	}
	return nil
}

// Fresh reports whether a payload's origin and times can be trusted at
// now: sealed for this origin (so an InstanceKey shared across
// instances cannot move a cookie between them), not expired, not issued
// further in the future than FutureSkew, and living no longer than max —
// so a value sealed with a longer life, by a bug or an older version,
// is refused rather than honoured.
func Fresh(origin, want string, iat, exp int64, now time.Time, max time.Duration) bool {
	n := now.Unix()
	return origin == want &&
		iat > 0 && exp > iat &&
		n < exp &&
		iat <= n+int64(FutureSkew/time.Second) &&
		exp-iat <= int64(max/time.Second)
}
