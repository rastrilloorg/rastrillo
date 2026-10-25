package sessions

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"amadan.net/rastrillo/rastrillo/crypto"
	json "github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
)

// SignedLifetime is how long one signed cookie lives before it must be
// refreshed at the hub. It bounds how long a revoked session keeps
// working on a tenant that cannot ask the hub.
const SignedLifetime = 15 * time.Minute

// signedContext differs from assertion's "rastrillo/assertion/v1", so a
// handoff token signed by the same key can never pass as a session
// cookie, or a cookie as a handoff token.
const signedContext = "rastrillo/sessions/signed/v1"

// maxSignedCookie is the most a browser promises to store for one
// cookie, and it caps the work a hostile value can make us do.
const maxSignedCookie = 4096

// Fixed messages: none carries a submitted value, so a refusal is safe
// to log, and none can print a secret it failed to parse.
var (
	errKeyID     = errors.New("rastrillo/sessions: a key id is 1 to 32 of a-z, 0-9, _ and -")
	errSecret    = errors.New("rastrillo/sessions: malformed signing key")
	errPublic    = errors.New("rastrillo/sessions: malformed public key")
	errKeyCount  = errors.New("rastrillo/sessions: one or two public keys, comma-separated")
	errKeyDouble = errors.New("rastrillo/sessions: two public keys share a key id")
	errClaims    = errors.New("rastrillo/sessions: signed cookie claims out of range")
)

// SigningKey is the hub's private key for signed session cookies. Only
// the hub holds one; tenants hold the Public form and verify.
type SigningKey struct {
	kid string
	kp  *crypto.Keypair
}

// NewSigningKey generates a fresh P-256 key under kid. BoxPriv stays
// nil: a session key signs and never opens a box, so it cannot be
// misused as one.
func NewSigningKey(kid string) (*SigningKey, error) {
	if !validKeyID(kid) {
		return nil, errKeyID
	}
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	return &SigningKey{kid: kid, kp: &crypto.Keypair{SignPriv: priv}}, nil
}

// ParseSigningKey reads the form Secret writes:
// "<kid>.<base64url of the 32-byte P-256 scalar>".
func ParseSigningKey(s string) (*SigningKey, error) {
	kid, enc, ok := strings.Cut(s, ".")
	if !ok {
		return nil, errSecret
	}
	if !validKeyID(kid) {
		return nil, errKeyID
	}
	raw, ok := decode64(enc)
	if !ok || len(raw) != 32 {
		return nil, errSecret
	}
	// ParseRawPrivateKey refuses a scalar of zero or at least the curve
	// order. Either would sign with a key anyone can derive or none can
	// verify.
	priv, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), raw)
	if err != nil {
		return nil, errSecret
	}
	return &SigningKey{kid: kid, kp: &crypto.Keypair{SignPriv: priv}}, nil
}

// KeyID names the key in every cookie it signs, so a verifier holding
// two keys during a rotation knows which one to try.
func (k *SigningKey) KeyID() string { return k.kid }

// Secret is the form ParseSigningKey reads. It is a method and not
// String, so a %v in a log line never prints the private key.
func (k *SigningKey) Secret() string {
	raw, err := k.kp.SignPriv.Bytes()
	if err != nil {
		// Only a key off the four NIST curves fails here, and every
		// SigningKey is P-256. An empty string fails ParseSigningKey
		// loudly instead of storing a half-written secret.
		return ""
	}
	return k.kid + "." + base64.RawURLEncoding.EncodeToString(raw)
}

// Public is "<kid>.<base64url of the 65-byte uncompressed point>", the
// form ParsePublicKeys reads.
func (k *SigningKey) Public() string {
	return k.kid + "." + base64.RawURLEncoding.EncodeToString(k.kp.SignPub())
}

// ParsePublicKeys reads one or two comma-separated Public values. Two
// is the rotation window: the new key and the one it replaces.
func ParsePublicKeys(s string) (map[string][]byte, error) {
	parts := strings.Split(s, ",")
	if len(parts) > 2 {
		return nil, errKeyCount
	}
	keys := make(map[string][]byte, len(parts))
	for _, p := range parts {
		kid, enc, ok := strings.Cut(p, ".")
		if !ok {
			return nil, errPublic
		}
		point, ok := decode64(enc)
		if !ok {
			return nil, errPublic
		}
		if err := validPublicKey(kid, point); err != nil {
			return nil, err
		}
		if _, dup := keys[kid]; dup {
			// Two keys under one kid would leave a cookie's kid naming
			// a key the verifier may not be holding.
			return nil, errKeyDouble
		}
		keys[kid] = point
	}
	return keys, nil
}

// validPublicKey is the one definition of a key a verifier may hold;
// ParsePublicKeys and a hand-built key map both pass through it, so the
// two cannot drift on what they accept.
func validPublicKey(kid string, point []byte) error {
	if !validKeyID(kid) {
		return errKeyID
	}
	if len(point) != 65 {
		return errPublic
	}
	if _, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), point); err != nil {
		return errPublic
	}
	return nil
}

// signedClaims is the cookie payload. Every member is required on the
// wire, so a missing epoch cannot decode to a zero some cookie really
// carries.
type signedClaims struct {
	V        int64  `json:"v"`
	KID      string `json:"kid"`
	Sub      string `json:"sub"`
	SID      string `json:"sid"`
	Epoch    int64  `json:"epoch"`
	IAT      int64  `json:"iat"`
	EXP      int64  `json:"exp"`
	At       int64  `json:"at"`
	Method   string `json:"m"`
	AuthTime int64  `json:"auth_time"`
}

// sign mints a cookie value. It runs checkClaims first, so the hub
// cannot mint a cookie its own tenants would refuse, and an address
// handed in as the subject is refused here rather than leaking into
// every request. It has no clock: whether a cookie is live is
// verifyCookie's call.
func (k *SigningKey) sign(c signedClaims) (string, error) {
	c.V = 1
	c.KID = k.kid
	if err := checkClaims(c); err != nil {
		return "", err
	}
	payload, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	sig, err := crypto.Sign(k.kp, signedContext, payload)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// checkClaims holds the shape and range rules that sign and verifyCookie
// share. The clock rules stay in verifyCookie, because sign has no
// clock to judge them by.
func checkClaims(c signedClaims) error {
	ok := c.V == 1 &&
		validKeyID(c.KID) &&
		// An opaque id, never an address: the cookie is readable by
		// anyone holding the browser, and every tenant logs requests.
		identifier(c.Sub, 128) &&
		randomID(c.SID) &&
		(c.Method == "" || validKeyID(c.Method)) &&
		c.Epoch >= 0 &&
		c.IAT > 0 &&
		c.At > 0 && c.At <= c.IAT &&
		// exp > iat first, so exp - iat cannot wrap: both are positive.
		c.EXP > c.IAT && c.EXP-c.IAT <= int64(SignedLifetime/time.Second) &&
		c.AuthTime >= 0 && c.AuthTime <= c.IAT
	if !ok {
		return errClaims
	}
	return nil
}

type cookieState int

const (
	// cookieNone is for the cookie reader: no cookie was sent.
	cookieNone cookieState = iota
	cookieValid
	cookieExpired
	cookieInvalid
)

// verifyCookie returns the claims only for cookieValid and
// cookieExpired: an expired cookie still carries the session the hub
// refreshes. Every other outcome returns zero claims. It never returns
// cookieNone: a cookie sent with an empty value is malformed, not
// absent, and only the cookie reader knows whether one was sent.
func verifyCookie(keys map[string][]byte, value string, now time.Time) (signedClaims, cookieState) {
	if len(value) > maxSignedCookie || strings.Count(value, ".") != 1 {
		return signedClaims{}, cookieInvalid
	}
	p, s, _ := strings.Cut(value, ".")
	if len(s) != 86 {
		return signedClaims{}, cookieInvalid
	}
	sig, ok := decode64(s)
	if !ok || len(sig) != 64 {
		return signedClaims{}, cookieInvalid
	}
	payload, ok := decode64(p)
	if !ok {
		return signedClaims{}, cookieInvalid
	}
	c, ok := parseClaims(payload)
	if !ok || c.V != 1 {
		return signedClaims{}, cookieInvalid
	}
	key, ok := keys[c.KID]
	if !ok || !crypto.Verify(key, signedContext, payload, sig) {
		return signedClaims{}, cookieInvalid
	}
	if checkClaims(c) != nil {
		return signedClaims{}, cookieInvalid
	}
	// iat is positive, so this cannot wrap. Thirty seconds forgives a
	// hub clock slightly ahead of this one.
	if c.IAT-30 > now.Unix() {
		return signedClaims{}, cookieInvalid
	}
	if c.EXP <= now.Unix() {
		return c, cookieExpired
	}
	return c, cookieValid
}

// parseClaims reads the payload as strictly as assertion reads its
// own: exactly the ten members, none null, no duplicates, numbers as
// plain integers. A lax reader would let one signed payload mean two
// things to two verifiers.
func parseClaims(payload []byte) (signedClaims, bool) {
	if !jsonObject(payload) {
		return signedClaims{}, false
	}
	var fields map[string]jsontext.Value
	if err := json.Unmarshal(payload, &fields); err != nil {
		return signedClaims{}, false
	}
	var c signedClaims
	numbers := map[string]*int64{"v": &c.V, "epoch": &c.Epoch, "iat": &c.IAT, "exp": &c.EXP, "at": &c.At, "auth_time": &c.AuthTime}
	strs := map[string]*string{"kid": &c.KID, "sub": &c.Sub, "sid": &c.SID, "m": &c.Method}
	if len(fields) != len(numbers)+len(strs) {
		return signedClaims{}, false
	}
	for name, value := range fields {
		if len(value) == 0 || string(value) == "null" {
			return signedClaims{}, false
		}
		if n, ok := numbers[name]; ok {
			// ParseInt takes a leading '+', which JSON does not.
			if value[0] == '+' {
				return signedClaims{}, false
			}
			v, err := strconv.ParseInt(string(value), 10, 64)
			if err != nil {
				return signedClaims{}, false
			}
			*n = v
			continue
		}
		if s, ok := strs[name]; ok {
			if value[0] != '"' || json.Unmarshal(value, s) != nil {
				return signedClaims{}, false
			}
			continue
		}
		// An unknown member: with the count above, this is also what
		// catches a required member that is missing.
		return signedClaims{}, false
	}
	return c, true
}

// jsonObject refuses duplicate names (escaped spellings included) and
// invalid UTF-8 before any member is read, as assertion's object does.
func jsonObject(b []byte) bool {
	if len(b) < 2 || b[0] != '{' || b[len(b)-1] != '}' || !utf8.Valid(b) {
		return false
	}
	var v any
	return json.Unmarshal(b, &v) == nil
}

// newSID is a session id, fixed for the life of a signed session across
// every refresh.
func newSID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

func validKeyID(s string) bool {
	if len(s) == 0 || len(s) > 32 {
		return false
	}
	for i := 0; i < len(s); i++ {
		b := s[i]
		if !(b >= 'a' && b <= 'z' || b >= '0' && b <= '9' || b == '_' || b == '-') {
			return false
		}
	}
	return true
}

func identifier(s string, max int) bool {
	if len(s) == 0 || len(s) > max {
		return false
	}
	for i := 0; i < len(s); i++ {
		b := s[i]
		if !(b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '_' || b == '-') {
			return false
		}
	}
	return true
}

func randomID(s string) bool {
	if len(s) != 22 {
		return false
	}
	b, ok := decode64(s)
	return ok && len(b) == 16
}

// decode64 is strict base64url that must re-encode to itself. Without
// the round trip, two spellings of one payload (differing in the
// unused low bits of the last character) would both verify.
func decode64(s string) ([]byte, bool) {
	if !identifier(s, maxSignedCookie) {
		return nil, false
	}
	b, err := base64.RawURLEncoding.Strict().DecodeString(s)
	return b, err == nil && base64.RawURLEncoding.EncodeToString(b) == s
}
