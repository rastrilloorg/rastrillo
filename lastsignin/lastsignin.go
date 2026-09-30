// Package lastsignin remembers, in this browser only, which way in it
// last used — keymail, a magic link or a passkey — and the address for
// the two that have one, so the sign-in screen can offer a one-tap.
//
// It also owns the one seam every sign-in path calls when an attempt
// ends, EndAttempt, so auth and passkey clear the screen's attempt
// cookie the same way — and passkey never needs auth's InstanceKey or
// its keymail dependency to do it.
//
// What the remembered cookie is never evidence of: identity, admission,
// freshness or a second factor. Nothing reads it but the sign-in screen
// and this jar; a one-tap posts its address to auth.Begin exactly as if
// it had been typed, where it is classified, rate-limited and proved
// afresh.
package lastsignin

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"amadan.net/rastrillo/rastrillo/crypto"
	"amadan.net/rastrillo/rastrillo/internal/sealedcookie"
)

// Mode is what the jar is allowed to do, decided once from auth's
// configuration.
type Mode int

const (
	// Off: the app does not render the shipped screen. Nothing is
	// written, read or deleted — not one Set-Cookie header — which is
	// what "an app that leaves SigninScreen off sees no change" needs.
	Off Mode = iota
	// Forgetting: the screen is on and remembering is switched off (a
	// shared kiosk). Remember deletes what an earlier configuration
	// wrote, and Read reports any cookie as Invalid so the sign-in page
	// clears it too — turning remembering off forgets, it does not just
	// stop writing.
	Forgetting
	// On: the screen is on and remembers the way in.
	On
)

// The methods a record may name — the Identity methods auth mints, and
// passkey discovery's.
const (
	MethodKeymail   = "keymail"
	MethodMagicLink = "magiclink"
	MethodPasskey   = "passkey"
)

// lifetime is 400 days because that is the longest Max-Age browsers
// honour (RFC 6265bis clamps anything longer); every sign-in rewrites
// the cookie, so the 400 days run from the latest one.
const lifetime = 400 * 24 * time.Hour

// Config configures New.
type Config struct {
	// Origin decides Secure and the __Host- prefix, and is sealed into
	// the value so a key shared across instances cannot move a cookie
	// between them.
	Origin string
	// InstanceKey is auth's; the jar derives its own key from it.
	InstanceKey string
	// AttemptCookie is the attempt cookie's resolved name. auth passes
	// it in so EndAttempt can delete that cookie without knowing its
	// format. Required unless Mode is Off.
	AttemptCookie string
	Mode          Mode
	// Now is the clock values are sealed and judged by; time.Now if nil.
	Now func() time.Time
}

// Record is one remembered way in. Address is empty exactly when Method
// is MethodPasskey: discovery knows a subject, not an address.
type Record struct {
	Method  string
	Address string
}

// ReadResult says what Read found.
type ReadResult int

const (
	// Absent: no cookie, or the jar is Off.
	Absent ReadResult = iota
	// Valid: a record this jar sealed, for this origin, still in date.
	Valid
	// Invalid: a cookie is present and must not be believed — tampered,
	// expired, sealed elsewhere, or present while remembering is off.
	// The caller clears it.
	Invalid
)

// Jar is the remembered cookie and the attempt's end. Build one per
// Auth (auth.New does) and share it with passkey.
type Jar struct {
	cfg Config
	key []byte
}

// payload is the sealed value. Field names are short because every byte
// rides every request to this origin for 400 days.
type payload struct {
	O   string `json:"o"`
	M   string `json:"m"`
	A   string `json:"a,omitempty"`
	IAT int64  `json:"iat"`
	EXP int64  `json:"exp"`
}

// New validates cfg and derives the jar's key.
func New(cfg Config) (*Jar, error) {
	if !strings.HasPrefix(cfg.Origin, "https://") && !strings.HasPrefix(cfg.Origin, "http://") {
		return nil, errors.New("rastrillo/lastsignin: Config.Origin must be an absolute origin like https://app.example.com")
	}
	if cfg.InstanceKey == "" {
		return nil, errors.New("rastrillo/lastsignin: Config.InstanceKey must not be empty")
	}
	if cfg.Mode != Off && cfg.AttemptCookie == "" {
		return nil, errors.New("rastrillo/lastsignin: Config.AttemptCookie is required unless Mode is Off")
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Jar{cfg: cfg, key: crypto.Derive([]byte(cfg.InstanceKey), "rastrillo/lastsignin/v1")}, nil
}

// Mode is the mode the jar was built with.
func (j *Jar) Mode() Mode { return j.cfg.Mode }

func (j *Jar) secure() bool { return strings.HasPrefix(j.cfg.Origin, "https://") }

// CookieName is the remembered cookie's name for this origin: __Host-
// on https, where the prefix pins it to this host and path; unprefixed
// on a plain-http dev origin, because __Host- requires Secure.
func (j *Jar) CookieName() string {
	if j.secure() {
		return "__Host-rastrillo_last_signin"
	}
	return "rastrillo_last_signin"
}

func (j *Jar) set(w http.ResponseWriter, name, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: value, Path: "/",
		MaxAge: maxAge, HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Secure: j.secure(),
	})
}

// EndAttempt deletes the sign-in screen's attempt cookie. Every path
// that verifies a first factor calls it: an attempt left behind keeps
// prefilling the form with an address the visitor has already proved —
// or, after a passkey sign-in, somebody else's.
func (j *Jar) EndAttempt(w http.ResponseWriter) {
	if j.cfg.Mode == Off {
		return
	}
	j.set(w, j.cfg.AttemptCookie, "", -1)
}

// Remember records the way in this browser just used. A record Read
// would refuse is not written at all, so a bad caller can never leave a
// cookie behind that only ever reads as Invalid.
func (j *Jar) Remember(w http.ResponseWriter, rec Record) {
	switch j.cfg.Mode {
	case Off:
		return
	case Forgetting:
		j.set(w, j.CookieName(), "", -1)
		return
	}
	if !validRecord(rec) {
		return
	}
	now := j.cfg.Now().Unix()
	v, err := sealedcookie.Seal(j.key, payload{
		O: j.cfg.Origin, M: rec.Method, A: rec.Address,
		IAT: now, EXP: now + int64(lifetime/time.Second),
	})
	if err != nil {
		// Only crypto/rand can fail here. Remembering is a convenience;
		// failing a verified sign-in over it would be the wrong trade.
		return
	}
	j.set(w, j.CookieName(), v, int(lifetime/time.Second))
}

// Read returns the remembered record. It is never Valid unless the jar
// is On.
func (j *Jar) Read(r *http.Request) (Record, ReadResult) {
	if j.cfg.Mode == Off {
		return Record{}, Absent
	}
	c, err := r.Cookie(j.CookieName())
	if err != nil {
		return Record{}, Absent
	}
	if j.cfg.Mode == Forgetting {
		return Record{}, Invalid
	}
	var p payload
	if sealedcookie.Open(j.key, c.Value, &p) != nil ||
		!sealedcookie.Fresh(p.O, j.cfg.Origin, p.IAT, p.EXP, j.cfg.Now(), lifetime) {
		return Record{}, Invalid
	}
	rec := Record{Method: p.M, Address: p.A}
	if !validRecord(rec) {
		return Record{}, Invalid
	}
	return rec, Valid
}

// Clear deletes the remembered cookie: "Use a different email".
func (j *Jar) Clear(w http.ResponseWriter) {
	if j.cfg.Mode == Off {
		return
	}
	j.set(w, j.CookieName(), "", -1)
}

func validRecord(rec Record) bool {
	switch rec.Method {
	case MethodPasskey:
		return rec.Address == ""
	case MethodKeymail, MethodMagicLink:
		return validAddress(rec.Address)
	}
	return false
}

// validAddress is the shape a remembered address must have: at most 254
// bytes (the longest address SMTP carries), exactly one @, and no
// control bytes — the address is shown back on the screen and posted
// back to Begin, and a CR or LF in either place is an injection waiting
// for a careless consumer.
func validAddress(a string) bool {
	return a != "" && len(a) <= 254 && strings.Count(a, "@") == 1 &&
		!strings.ContainsFunc(a, func(r rune) bool { return r < 0x20 || r == 0x7f })
}
