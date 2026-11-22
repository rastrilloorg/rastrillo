// Package roster signs whole-roster snapshots that a root app sends to the
// instances living under it. A snapshot lists who belongs to one instance and
// whether it is active. It carries no credentials: the receiver must still
// check a session before serving private content.
package roster

import (
	"errors"
	"strings"
	"time"

	"amadan.net/rastrillo/rastrillo/crypto"
	"amadan.net/rastrillo/rastrillo/internal/wire"
	json "github.com/go-json-experiment/json"
)

// A snapshot has its own context so an assertion signed by the same key
// cannot be replayed as a roster, nor the reverse.
const signingContext = "rastrillo/roster/v1"

const (
	maxToken    = 1 << 20
	maxLifetime = 300
	// A snapshot is clock-checked on a different machine than the one that
	// signed it, so a little skew into the future is not an attack.
	allowedSkew = 30
)

// Errors contain no submitted values, so callers can log a refusal safely.
var (
	ErrMalformed  = errors.New("roster: malformed")
	ErrVersion    = errors.New("roster: version")
	ErrUnknownKey = errors.New("roster: unknown key")
	ErrSignature  = errors.New("roster: signature")
	ErrIssuer     = errors.New("roster: issuer")
	ErrAudience   = errors.New("roster: audience")
	ErrLifetime   = errors.New("roster: lifetime")
	ErrExpired    = errors.New("roster: expired")
	ErrClaims     = errors.New("roster: claims")
	ErrConfig     = errors.New("roster: configuration")
	ErrSigning    = errors.New("roster: signing")
)

// State says whether the instance may be used.
type State string

const (
	Active    State = "active"
	Suspended State = "suspended"
)

// Member is one person in the instance. Subject is the root's opaque
// identifier for them.
type Member struct {
	Subject string `json:"sub"`
	Email   string `json:"email"`
	Name    string `json:"name"`
	Role    string `json:"role"`
}

// Snapshot is the complete roster, not a change to it, so a receiver that
// missed one snapshot recovers from the next. Sequence rises with every
// snapshot the root sends for one instance. An empty Members list is valid.
//
// Origin is the account's custom origin, or empty for the instance's own
// host. It is left out of the token when empty, because an older
// receiver refuses a field it does not know.
type Snapshot struct {
	Version   int64    `json:"v"`
	Issuer    string   `json:"iss"`
	Audience  string   `json:"aud"`
	KeyID     string   `json:"kid"`
	Sequence  int64    `json:"seq"`
	IssuedAt  int64    `json:"iat"`
	ExpiresAt int64    `json:"exp"`
	State     State    `json:"state"`
	Members   []Member `json:"members"`
	Origin    string   `json:"origin,omitempty"`
}

// Sign refuses a snapshot that would not verify, without consulting a clock.
// The caller sets KeyID, Version, IssuedAt and ExpiresAt.
func Sign(key *crypto.Keypair, snapshot Snapshot) (string, error) {
	if snapshot.Members == nil {
		// A nil slice would be written as null, which Verify refuses.
		snapshot.Members = []Member{}
	}
	if err := validate(snapshot); err != nil {
		return "", err
	}
	if !wire.ValidSigningKey(key) {
		return "", ErrSigning
	}
	payload, err := json.Marshal(snapshot)
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
	token := wire.Join(payload, signature)
	if len(token) > maxToken {
		return "", ErrMalformed
	}
	return token, nil
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

// Verify returns a zero Snapshot on any failure. It does not compare
// Sequence with earlier snapshots; the receiver does that, because only it
// holds the last accepted value.
func (verifier *Verifier) Verify(
	token string, now time.Time,
) (Snapshot, error) {
	if verifier == nil || len(verifier.keys) == 0 {
		return Snapshot{}, ErrConfig
	}
	payload, signature, ok := wire.Split(token, maxToken)
	if !ok {
		return Snapshot{}, ErrMalformed
	}
	err := verifier.checkSignature(payload, signature)
	if err != nil {
		return Snapshot{}, err
	}
	snapshot, err := parse(payload, true)
	if err != nil {
		return Snapshot{}, err
	}
	err = verifier.checkSnapshot(snapshot, now)
	if err != nil {
		return Snapshot{}, err
	}
	return snapshot, nil
}

func (verifier *Verifier) checkSignature(payload, signature []byte) error {
	// Read only enough to pick the key. Field strictness waits until the
	// signature has passed, so a token signed for another purpose is
	// reported as a bad signature and not as odd fields.
	snapshot, err := parse(payload, false)
	if err != nil {
		return err
	}
	if snapshot.Version != 1 {
		return ErrVersion
	}
	key, ok := verifier.keys[snapshot.KeyID]
	if !ok {
		return ErrUnknownKey
	}
	if !crypto.Verify(key, signingContext, payload, signature) {
		return ErrSignature
	}
	return nil
}

func (verifier *Verifier) checkSnapshot(
	snapshot Snapshot, now time.Time,
) error {
	if snapshot.Issuer != verifier.issuer {
		return ErrIssuer
	}
	if snapshot.Audience != verifier.audience {
		return ErrAudience
	}
	if err := validate(snapshot); err != nil {
		return err
	}
	if snapshot.ExpiresAt <= now.Unix() {
		return ErrExpired
	}
	// IssuedAt is already positive, so this subtraction cannot wrap.
	if snapshot.IssuedAt-allowedSkew > now.Unix() {
		return ErrLifetime
	}
	return nil
}

func parse(payload []byte, strict bool) (Snapshot, error) {
	if len(payload) > maxToken || !wire.StrictObject(payload) {
		return Snapshot{}, ErrMalformed
	}
	var snapshot Snapshot
	err := json.Unmarshal(
		payload, &snapshot, json.RejectUnknownMembers(strict),
	)
	if err != nil {
		return Snapshot{}, ErrClaims
	}
	// A missing or null list is not an empty one.
	if strict && snapshot.Members == nil {
		return Snapshot{}, ErrClaims
	}
	return snapshot, nil
}

func validate(snapshot Snapshot) error {
	if snapshot.Version != 1 {
		return ErrVersion
	}
	if !wire.Origin(snapshot.Issuer) || !wire.Origin(snapshot.Audience) ||
		snapshot.Issuer == snapshot.Audience ||
		!wire.Identifier(snapshot.KeyID, 64) || snapshot.Sequence < 1 ||
		snapshot.Members == nil {
		return ErrClaims
	}
	if snapshot.State != Active && snapshot.State != Suspended {
		return ErrClaims
	}
	if !validOrigin(snapshot.Origin, snapshot.Issuer) {
		return ErrClaims
	}
	lifetime := snapshot.ExpiresAt - snapshot.IssuedAt
	if snapshot.IssuedAt <= 0 || lifetime <= 0 || lifetime > maxLifetime {
		return ErrLifetime
	}
	seen := make(map[string]bool, len(snapshot.Members))
	for _, member := range snapshot.Members {
		if !wire.Identifier(member.Subject, 128) ||
			!wire.Identifier(member.Role, 64) ||
			len(member.Email) > 320 || len(member.Name) > 256 ||
			seen[member.Subject] {
			return ErrClaims
		}
		seen[member.Subject] = true
	}
	return nil
}

// validOrigin is an empty origin, or https:// and a host with no port. A
// browser reaches a custom domain on 443 only, and an instance on the
// root's own origin would share its cookies.
func validOrigin(origin, issuer string) bool {
	if origin == "" {
		return true
	}
	return wire.Origin(origin) && origin != issuer &&
		!strings.Contains(strings.TrimPrefix(origin, "https://"), ":")
}
