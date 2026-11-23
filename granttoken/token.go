// Package granttoken signs and verifies grant tokens: a root-signed bearer
// token that lets an agent act for one person on one site, until it
// expires. A verified token says only what the root granted when it
// signed. The site must still check the grant against its latest roster.
package granttoken

import (
	"errors"
	"regexp"
	"strings"
	"time"

	"amadan.net/rastrillo/rastrillo/apitoken"
	"amadan.net/rastrillo/rastrillo/crypto"
	"amadan.net/rastrillo/rastrillo/internal/wire"
	json "github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
)

// Prefix marks a grant token, so a log scrubber or a token router can
// tell one apart without verifying it.
const Prefix = "rastrillo_grant_"

// Access values on the wire.
const Read, Write = "read", "write"

// A grant has its own context so a roster or an assertion signed by the
// same root key cannot be replayed as a grant, nor the reverse.
const signingContext = "rastrillo/grant/v1"

const (
	maxToken = 4096
	// 366 days, so "at most a year" still holds across a leap year.
	maxLifetime = 31_622_400
	// A token is clock-checked on a different machine than the one that
	// signed it, so a little skew into the future is not an attack.
	allowedSkew = 30
)

// Errors contain no submitted values, so callers can log a refusal safely.
var (
	ErrMalformed  = errors.New("granttoken: malformed")
	ErrVersion    = errors.New("granttoken: version")
	ErrUnknownKey = errors.New("granttoken: unknown key")
	ErrSignature  = errors.New("granttoken: signature")
	ErrIssuer     = errors.New("granttoken: issuer")
	ErrAudience   = errors.New("granttoken: audience")
	ErrLifetime   = errors.New("granttoken: lifetime")
	ErrExpired    = errors.New("granttoken: expired")
	ErrClaims     = errors.New("granttoken: claims")
	ErrConfig     = errors.New("granttoken: configuration")
	ErrSigning    = errors.New("granttoken: signing")
)

// Claims name the person (Subject), the root's grant record (Grant) and
// the agent acting under it. Issuer is the root's origin and Audience
// the one site the token is for; the two are equal on the root's own
// door.
type Claims struct {
	Version   int64  `json:"v"`
	Issuer    string `json:"iss"`
	Audience  string `json:"aud"`
	KeyID     string `json:"kid"`
	Subject   string `json:"sub"`
	Grant     string `json:"gid"`
	Agent     string `json:"agent"`
	Access    string `json:"access"`
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
}

// Every member is required, and none may be null.
var required = []string{
	"v", "iss", "aud", "kid", "sub", "gid", "agent", "access", "iat", "exp",
}

var tokenPattern = regexp.MustCompile(
	regexp.QuoteMeta(Prefix) + `[A-Za-z0-9._-]+`,
)

// Redact hides every grant token in text, keeping the prefix so a reader
// can still see that one was there.
func Redact(text string) string {
	return tokenPattern.ReplaceAllLiteralString(text, Prefix+"[redacted]")
}

// Sign refuses claims that would not verify, without consulting a clock,
// an issuer or an audience: those are the verifier's to judge.
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
	if _, err := parse(payload); err != nil {
		return "", err
	}
	signature, err := crypto.Sign(key, signingContext, payload)
	if err != nil {
		return "", ErrSigning
	}
	token := wire.Join(payload, signature)
	if len(token) > maxToken {
		return "", ErrMalformed
	}
	return Prefix + token, nil
}

// Verifier holds a copied trust set for one issuer and audience. It is
// safe for concurrent use. To rotate keys, construct a replacement and
// swap it at the caller.
type Verifier struct {
	issuer, audience string
	keys             map[string][]byte
}

// NewVerifier allows issuer to equal audience: that is the root's own
// door.
func NewVerifier(
	issuer, audience string, keys map[string][]byte,
) (*Verifier, error) {
	trusted, ok := wire.TrustSet(keys)
	if !ok || !wire.Origin(issuer) || !wire.Origin(audience) {
		return nil, ErrConfig
	}
	return &Verifier{issuer: issuer, audience: audience, keys: trusted}, nil
}

// Verify returns zero Claims on any failure. It does not ask whether the
// grant is still active; only the site's roster knows that.
func (verifier *Verifier) Verify(token string, now time.Time) (Claims, error) {
	if verifier == nil || len(verifier.keys) == 0 {
		return Claims{}, ErrConfig
	}
	body, found := strings.CutPrefix(token, Prefix)
	if !found {
		return Claims{}, ErrMalformed
	}
	payload, signature, ok := wire.Split(body, maxToken)
	if !ok {
		return Claims{}, ErrMalformed
	}
	if err := verifier.checkSignature(payload, signature); err != nil {
		return Claims{}, err
	}
	claims, err := parse(payload)
	if err != nil {
		return Claims{}, err
	}
	if err := validate(claims); err != nil {
		return Claims{}, err
	}
	if err := verifier.checkTrust(claims, now); err != nil {
		return Claims{}, err
	}
	return claims, nil
}

func (verifier *Verifier) checkSignature(payload, signature []byte) error {
	// Read only enough to pick the key. Strictness waits until the
	// signature has passed, so a token signed for another purpose is
	// reported as a bad signature and not as odd members.
	if !wire.StrictObject(payload) {
		return ErrMalformed
	}
	var header struct {
		Version *int64  `json:"v"`
		KeyID   *string `json:"kid"`
	}
	if json.Unmarshal(payload, &header) != nil ||
		header.Version == nil || header.KeyID == nil {
		return ErrClaims
	}
	if *header.Version != 1 {
		return ErrVersion
	}
	key, ok := verifier.keys[*header.KeyID]
	if !ok {
		return ErrUnknownKey
	}
	if !crypto.Verify(key, signingContext, payload, signature) {
		return ErrSignature
	}
	return nil
}

func (verifier *Verifier) checkTrust(claims Claims, now time.Time) error {
	if claims.Issuer != verifier.issuer {
		return ErrIssuer
	}
	if claims.Audience != verifier.audience {
		return ErrAudience
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

// parse refuses a missing or null member before unmarshalling, because
// unmarshalling would turn either into a zero value.
func parse(payload []byte) (Claims, error) {
	if len(payload) > maxToken || !wire.StrictObject(payload) {
		return Claims{}, ErrMalformed
	}
	var members map[string]jsontext.Value
	if json.Unmarshal(payload, &members) != nil {
		return Claims{}, ErrMalformed
	}
	for _, name := range required {
		value, ok := members[name]
		if !ok || string(value) == "null" {
			return Claims{}, ErrClaims
		}
	}
	var claims Claims
	err := json.Unmarshal(payload, &claims, json.RejectUnknownMembers(true))
	if err != nil {
		return Claims{}, ErrClaims
	}
	return claims, nil
}

func validate(claims Claims) error {
	if claims.Version != 1 {
		return ErrVersion
	}
	agent, ok := apitoken.ValidAgentName(claims.Agent)
	if !wire.Origin(claims.Issuer) || !wire.Origin(claims.Audience) ||
		!wire.Identifier(claims.KeyID, 64) ||
		!wire.Identifier(claims.Subject, 128) ||
		!wire.Identifier(claims.Grant, 64) ||
		!ok || agent != claims.Agent ||
		(claims.Access != Read && claims.Access != Write) {
		return ErrClaims
	}
	// Both are positive before subtracting, so it cannot wrap.
	if claims.IssuedAt <= 0 || claims.ExpiresAt <= 0 {
		return ErrLifetime
	}
	lifetime := claims.ExpiresAt - claims.IssuedAt
	if lifetime <= 0 || lifetime > maxLifetime {
		return ErrLifetime
	}
	return nil
}
