// Package assertion signs short-lived identity handoffs between exact HTTPS
// origins. A verified identity establishes neither team membership nor access
// to an item. Your consumer must check both before serving private content.
package assertion

import (
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"amadan.net/rastrillo/rastrillo/crypto"
	json "github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
)

const signingContext = "rastrillo/assertion/v1"
const maxPayload = 4096

// Errors contain no submitted values, so callers can log a refusal safely.
var (
	ErrMalformed  = errors.New("assertion: malformed")
	ErrVersion    = errors.New("assertion: version")
	ErrUnknownKey = errors.New("assertion: unknown key")
	ErrSignature  = errors.New("assertion: signature")
	ErrIssuer     = errors.New("assertion: issuer")
	ErrAudience   = errors.New("assertion: audience")
	ErrLifetime   = errors.New("assertion: lifetime")
	ErrExpired    = errors.New("assertion: expired")
	ErrClaims     = errors.New("assertion: claims")
	ErrConfig     = errors.New("assertion: configuration")
	ErrRandom     = errors.New("assertion: randomness")
	ErrSigning    = errors.New("assertion: signing")
)

// Claims carries an opaque identity and the consumer's browser-bound nonce.
// AuthTime records the original authentication, not this handoff's issuance.
// Ext may carry cosmetic preferences only; it grants no authority.
type Claims struct {
	Version   int64          `json:"v"`
	Issuer    string         `json:"iss"`
	Audience  string         `json:"aud"`
	Subject   string         `json:"sub"`
	KeyID     string         `json:"kid"`
	ID        string         `json:"jti"`
	Nonce     string         `json:"nonce"`
	IssuedAt  int64          `json:"iat"`
	ExpiresAt int64          `json:"exp"`
	AuthTime  int64          `json:"auth_time"`
	Ext       jsontext.Value `json:"ext,omitzero"`
}

// RandomID supplies independent identifiers for assertion IDs and pending
// browser requests. Generate each separately; sharing a value is refused.
func RandomID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", ErrRandom
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

// Sign validates the claims without consulting a clock. The receiver applies
// expiry checks against its own clock. The caller must not mutate key or Ext
// while this call is running.
func Sign(key *crypto.Keypair, claims Claims) (string, error) {
	if err := validate(claims); err != nil {
		return "", err
	}
	if !validSigningKey(key) {
		return "", ErrSigning
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", ErrClaims
	}
	// Parsing our own bytes keeps both directions on the same wire rules,
	// including extension size after serialization.
	if _, err := parse(payload); err != nil {
		return "", err
	}
	sig, err := crypto.Sign(key, signingContext, payload)
	if err != nil {
		return "", ErrSigning
	}
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func validSigningKey(key *crypto.Keypair) bool {
	if key == nil || key.SignPriv == nil {
		return false
	}
	k := key.SignPriv
	curve := elliptic.P256()
	if k.Curve != curve || k.D == nil || k.X == nil || k.Y == nil || k.D.Sign() <= 0 || k.D.Cmp(curve.Params().N) >= 0 || !curve.IsOnCurve(k.X, k.Y) {
		return false
	}
	x, y := curve.ScalarBaseMult(k.D.Bytes())
	return x.Cmp(k.X) == 0 && y.Cmp(k.Y) == 0
}

// Verifier holds a copied trust set for one audience. It is safe for concurrent
// use. To rotate keys, construct a replacement and swap it at the caller.
type Verifier struct {
	issuer, audience string
	keys             map[string][]byte
}

func NewVerifier(issuer, audience string, keys map[string][]byte) (*Verifier, error) {
	if !origin(issuer) || !origin(audience) || issuer == audience || len(keys) < 1 || len(keys) > 8 {
		return nil, ErrConfig
	}
	v := &Verifier{issuer: issuer, audience: audience, keys: make(map[string][]byte, len(keys))}
	for kid, pub := range keys {
		if !identifier(kid, 64) || len(pub) != 65 {
			return nil, ErrConfig
		}
		if x, _ := elliptic.Unmarshal(elliptic.P256(), pub); x == nil {
			return nil, ErrConfig
		}
		v.keys[kid] = append([]byte(nil), pub...)
	}
	return v, nil
}

// Verify returns zero Claims on any failure. It returns the signed nonce but
// does not compare or consume it. Before issuing a session, compare it with the
// pending browser value using subtle.ConstantTimeCompare, then atomically
// consume that request and record the assertion ID in a persistent replay store.
// Repeated calls here can succeed: this pure verifier supplies no replay store.
func (v *Verifier) Verify(token string, now time.Time) (Claims, error) {
	if v == nil || len(v.keys) == 0 {
		return Claims{}, ErrConfig
	}
	if len(token) > 8192 || strings.Count(token, ".") != 1 {
		return Claims{}, ErrMalformed
	}
	p, s, _ := strings.Cut(token, ".")
	if len(p) > base64.RawURLEncoding.EncodedLen(maxPayload) || len(s) != 86 {
		return Claims{}, ErrMalformed
	}
	payload, ok := decode(p)
	if !ok {
		return Claims{}, ErrMalformed
	}
	sig, ok := decode(s)
	if !ok || len(sig) != 64 {
		return Claims{}, ErrMalformed
	}
	c, err := parse(payload)
	if err != nil {
		return Claims{}, err
	}
	if c.Version != 1 {
		return Claims{}, ErrVersion
	}
	key, ok := v.keys[c.KeyID]
	if !ok {
		return Claims{}, ErrUnknownKey
	}
	if !crypto.Verify(key, signingContext, payload, sig) {
		return Claims{}, ErrSignature
	}
	if c.Issuer != v.issuer {
		return Claims{}, ErrIssuer
	}
	if c.Audience != v.audience {
		return Claims{}, ErrAudience
	}
	if err := validate(c); err != nil {
		return Claims{}, err
	}
	if c.ExpiresAt <= now.Unix() {
		return Claims{}, ErrExpired
	}
	// iat is already positive, so this subtraction cannot wrap int64.
	if c.IssuedAt-30 > now.Unix() {
		return Claims{}, ErrLifetime
	}
	return c, nil
}

func parse(payload []byte) (Claims, error) {
	if len(payload) > maxPayload || !object(payload) {
		return Claims{}, ErrMalformed
	}
	var fields map[string]jsontext.Value
	if err := json.Unmarshal(payload, &fields); err != nil {
		return Claims{}, ErrMalformed
	}
	var c Claims
	required := []string{"v", "iss", "aud", "sub", "kid", "jti", "nonce", "iat", "exp", "auth_time"}
	for _, key := range required {
		value, ok := fields[key]
		if !ok || string(value) == "null" {
			return Claims{}, ErrClaims
		}
	}
	for key, value := range fields {
		var number *int64
		var str *string
		switch key {
		case "v":
			number = &c.Version
		case "iat":
			number = &c.IssuedAt
		case "exp":
			number = &c.ExpiresAt
		case "auth_time":
			number = &c.AuthTime
		case "iss":
			str = &c.Issuer
		case "aud":
			str = &c.Audience
		case "sub":
			str = &c.Subject
		case "kid":
			str = &c.KeyID
		case "jti":
			str = &c.ID
		case "nonce":
			str = &c.Nonce
		case "ext":
			if len(value) > 1024 || !object(value) {
				return Claims{}, ErrClaims
			}
			c.Ext = append(jsontext.Value(nil), value...)
		default:
			return Claims{}, ErrClaims
		}
		if number != nil {
			if len(value) == 0 || value[0] == '+' {
				return Claims{}, ErrClaims
			}
			n, err := strconv.ParseInt(string(value), 10, 64)
			if err != nil {
				return Claims{}, ErrClaims
			}
			*number = n
		}
		if str != nil {
			if len(value) == 0 || value[0] != '"' || json.Unmarshal(value, str) != nil {
				return Claims{}, ErrClaims
			}
		}
	}
	return c, nil
}

// Strict v2 parsing rejects duplicate names (including escaped spellings) and
// invalid Unicode recursively. Keep this separate from raw-value extraction so
// changing that extraction cannot accidentally skip validation of extensions.
func object(b []byte) bool {
	if len(b) < 2 || b[0] != '{' || b[len(b)-1] != '}' || !utf8.Valid(b) {
		return false
	}
	var value any
	return json.Unmarshal(b, &value) == nil
}

func validate(c Claims) error {
	if c.Version != 1 {
		return ErrVersion
	}
	if !origin(c.Issuer) || !origin(c.Audience) || c.Issuer == c.Audience || !identifier(c.Subject, 128) || !identifier(c.KeyID, 64) || !randomID(c.ID) || !randomID(c.Nonce) || c.ID == c.Nonce {
		return ErrClaims
	}
	if c.IssuedAt <= 0 || c.AuthTime <= 0 || c.AuthTime > c.IssuedAt || c.ExpiresAt <= c.IssuedAt || c.ExpiresAt-c.IssuedAt > 120 {
		return ErrLifetime
	}
	if c.Ext != nil && (len(c.Ext) > 1024 || !object(c.Ext)) {
		return ErrClaims
	}
	return nil
}

func identifier(s string, max int) bool {
	if len(s) == 0 || len(s) > max {
		return false
	}
	for i := 0; i < len(s); i++ {
		b := s[i]
		if !alphaNum(b) && b != '-' && b != '_' {
			return false
		}
	}
	return true
}
func alphaNum(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
}
func decode(s string) ([]byte, bool) {
	if !identifier(s, 8192) {
		return nil, false
	}
	b, err := base64.RawURLEncoding.Strict().DecodeString(s)
	return b, err == nil && base64.RawURLEncoding.EncodeToString(b) == s
}
func randomID(s string) bool {
	if len(s) != 22 {
		return false
	}
	b, ok := decode(s)
	return ok && len(b) == 16
}

func origin(s string) bool {
	if !strings.HasPrefix(s, "https://") {
		return false
	}
	host := strings.TrimPrefix(s, "https://")
	if h, p, found := strings.Cut(host, ":"); found {
		if p == "" || p[0] == '0' || p == "443" {
			return false
		}
		for i := 0; i < len(p); i++ {
			if p[i] < '0' || p[i] > '9' {
				return false
			}
		}
		port, err := strconv.ParseUint(p, 10, 16)
		if err != nil || port == 0 {
			return false
		}
		host = h
	}
	if len(host) == 0 || len(host) > 253 || net.ParseIP(host) != nil {
		return false
	}
	// Numeric final labels can be interpreted as noncanonical IPv4 by browser
	// URL parsers (for example 127.1); require a DNS suffix with a letter.
	labels := strings.Split(host, ".")
	last := labels[len(labels)-1]
	hasLetter := false
	for _, b := range []byte(last) {
		if b >= 'a' && b <= 'z' {
			hasLetter = true
		}
	}
	if strings.HasPrefix(last, "0x") {
		if _, err := strconv.ParseUint(last[2:], 16, 64); err == nil {
			return false
		}
	}
	if !hasLetter {
		return false
	}
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, b := range []byte(label) {
			if !(b >= 'a' && b <= 'z' || b >= '0' && b <= '9' || b == '-') {
				return false
			}
		}
	}
	return true
}
