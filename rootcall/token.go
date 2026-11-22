// Package rootcall signs one HTTP request from a root app to one of its
// instances, and checks it on the instance. A call proves the root sent
// this method, target and body to this instance, recently, and once. It
// carries no person: the handler decides what the root may ask.
package rootcall

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"time"

	"amadan.net/rastrillo/rastrillo/crypto"
	"amadan.net/rastrillo/rastrillo/internal/wire"
	json "github.com/go-json-experiment/json"
)

// A call has its own context so that a handoff assertion or a roster
// snapshot signed by the same key cannot be replayed as a call, nor the
// reverse. Spec: docs/superpowers/specs/2026-11-22-rootcall-design.md.
const signingContext = "rastrillo/rootcall/v1"

const (
	maxToken    = 8192
	maxLifetime = 120
	maxTarget   = 2048
	// A call is clock-checked on a different machine than the one that
	// signed it, so a little skew into the future is not an attack.
	allowedSkew = 30
)

// Errors contain no submitted values, so callers can log a refusal safely.
var (
	ErrMalformed  = errors.New("rootcall: malformed")
	ErrVersion    = errors.New("rootcall: version")
	ErrUnknownKey = errors.New("rootcall: unknown key")
	ErrSignature  = errors.New("rootcall: signature")
	ErrIssuer     = errors.New("rootcall: issuer")
	ErrAudience   = errors.New("rootcall: audience")
	ErrLifetime   = errors.New("rootcall: lifetime")
	ErrExpired    = errors.New("rootcall: expired")
	ErrClaims     = errors.New("rootcall: claims")
	ErrConfig     = errors.New("rootcall: configuration")
	ErrSigning    = errors.New("rootcall: signing")
)

// Claims is what the root signs for one call. Target is the request
// target as sent on the request line, path and query. BodyHash is
// BodyHash of the exact body bytes, of no bytes for a call without one.
type Claims struct {
	Version   int64  `json:"v"`
	Issuer    string `json:"iss"`
	Audience  string `json:"aud"`
	KeyID     string `json:"kid"`
	ID        string `json:"jti"`
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
	Method    string `json:"method"`
	Target    string `json:"target"`
	BodyHash  string `json:"body"`
}

// BodyHash is the unpadded base64url SHA-256 of body.
func BodyHash(body []byte) string {
	sum := sha256.Sum256(body)
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// Sign refuses claims that would not verify, without consulting a clock.
// Most roots want Caller.NewRequest, which fills the claims in.
func Sign(key *crypto.Keypair, claims Claims) (string, error) {
	if err := validate(claims); err != nil {
		return "", err
	}
	if !wire.ValidSigningKey(key) {
		return "", ErrSigning
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", ErrClaims
	}
	// Parsing our own bytes keeps both directions on the same wire rules.
	if _, err := parse(payload, true); err != nil {
		return "", err
	}
	signature, err := crypto.Sign(key, signingContext, payload)
	if err != nil {
		return "", ErrSigning
	}
	return wire.Join(payload, signature), nil
}

// Verifier holds a copied trust set for one issuer and audience. It is safe
// for concurrent use. To rotate keys, construct a replacement and swap it at
// the caller.
type Verifier struct {
	issuer, audience string
	keys             map[string][]byte
}

func NewVerifier(
	issuer, audience string, keys map[string][]byte,
) (*Verifier, error) {
	trusted, ok := wire.TrustSet(keys)
	if !ok || !wire.Origin(issuer) || !wire.Origin(audience) ||
		issuer == audience {
		return nil, ErrConfig
	}
	return &Verifier{issuer: issuer, audience: audience, keys: trusted}, nil
}

// Verify returns zero Claims on any failure. It neither compares the
// binding with a request nor spends the ID: Guard does both, and a call
// checked only here can be replayed.
func (verifier *Verifier) Verify(token string, now time.Time) (Claims, error) {
	if verifier == nil || len(verifier.keys) == 0 {
		return Claims{}, ErrConfig
	}
	payload, signature, ok := wire.Split(token, maxToken)
	if !ok {
		return Claims{}, ErrMalformed
	}
	if err := verifier.checkSignature(payload, signature); err != nil {
		return Claims{}, err
	}
	claims, err := parse(payload, true)
	if err != nil {
		return Claims{}, err
	}
	if err := verifier.checkClaims(claims, now); err != nil {
		return Claims{}, err
	}
	return claims, nil
}

func (verifier *Verifier) checkSignature(payload, signature []byte) error {
	// Read only enough to pick the key. Field strictness waits until the
	// signature has passed, so a token signed for another purpose is
	// reported as a bad signature and not as odd fields.
	claims, err := parse(payload, false)
	if err != nil {
		return err
	}
	if claims.Version != 1 {
		return ErrVersion
	}
	key, ok := verifier.keys[claims.KeyID]
	if !ok {
		return ErrUnknownKey
	}
	if !crypto.Verify(key, signingContext, payload, signature) {
		return ErrSignature
	}
	return nil
}

func (verifier *Verifier) checkClaims(claims Claims, now time.Time) error {
	if claims.Issuer != verifier.issuer {
		return ErrIssuer
	}
	if claims.Audience != verifier.audience {
		return ErrAudience
	}
	if err := validate(claims); err != nil {
		return err
	}
	if claims.ExpiresAt <= now.Unix() {
		return ErrExpired
	}
	// IssuedAt is already positive, so this subtraction cannot wrap.
	if claims.IssuedAt-allowedSkew > now.Unix() {
		return ErrLifetime
	}
	return nil
}

func parse(payload []byte, strict bool) (Claims, error) {
	if len(payload) > maxToken || !wire.StrictObject(payload) {
		return Claims{}, ErrMalformed
	}
	var claims Claims
	err := json.Unmarshal(payload, &claims, json.RejectUnknownMembers(strict))
	if err != nil {
		return Claims{}, ErrClaims
	}
	return claims, nil
}

func validate(claims Claims) error {
	if claims.Version != 1 {
		return ErrVersion
	}
	if !wire.Origin(claims.Issuer) || !wire.Origin(claims.Audience) ||
		claims.Issuer == claims.Audience ||
		!wire.Identifier(claims.KeyID, 64) || !randomID(claims.ID) ||
		!validMethod(claims.Method) || !validTarget(claims.Target) ||
		!validHash(claims.BodyHash) {
		return ErrClaims
	}
	lifetime := claims.ExpiresAt - claims.IssuedAt
	if claims.IssuedAt <= 0 || lifetime <= 0 || lifetime > maxLifetime {
		return ErrLifetime
	}
	return nil
}

// randomID is assertion.RandomID's shape: 16 bytes, 22 characters.
func randomID(text string) bool {
	if len(text) != 22 {
		return false
	}
	raw, ok := wire.Decode(text, 22)
	return ok && len(raw) == 16
}

func validMethod(method string) bool {
	if len(method) == 0 || len(method) > 16 {
		return false
	}
	for index := 0; index < len(method); index++ {
		if method[index] < 'A' || method[index] > 'Z' {
			return false
		}
	}
	return true
}

// validTarget is an origin-form request target: what follows the method
// on the request line. Anything but printable ASCII there is refused or
// rewritten by a server or proxy, so it could never match.
func validTarget(target string) bool {
	if len(target) == 0 || len(target) > maxTarget || target[0] != '/' {
		return false
	}
	for index := 0; index < len(target); index++ {
		char := target[index]
		if char <= ' ' || char > '~' || char == '#' {
			return false
		}
	}
	return true
}

func validHash(hash string) bool {
	raw, ok := wire.Decode(hash, 43)
	return ok && len(raw) == sha256.Size
}
