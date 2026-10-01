package auth

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"strings"
	"time"

	"amadan.net/rastrillo/rastrillo/internal/sealedcookie"
)

// LinkTTL is how long an emailed sign-in link works. New sets it on the
// flow explicitly rather than inheriting keymaildev/signin's default, so
// the number the email and the apps' pages state is one this package
// owns: a signin upgrade that moved its default cannot make them false.
const LinkTTL = 15 * time.Minute

// attemptTTL is the magic link's own lifetime: the Sent page may name
// where a link went for exactly as long as that link can work, and no
// longer.
const attemptTTL = LinkTTL

// The kinds of attempt: a link went out, Begin answered with a problem,
// or a keymail round trip started.
const (
	attemptLink    = "link"
	attemptProblem = "problem"
	attemptKeymail = "keymail"
)

// attempt is the latest thing this browser submitted to Begin, kept for
// the screen: the prefill after a problem, and the address on the Sent
// page. It decides nothing about signing in.
type attempt struct {
	O   string `json:"o"`
	ID  string `json:"id"`
	IAT int64  `json:"iat"`
	EXP int64  `json:"exp"`
	K   string `json:"k"`
	A   string `json:"a,omitempty"`
	// X: this link went out where the remembered-Keymail one-tap
	// promised Keymail. It changes one line of this browser's own Sent
	// page and nothing else.
	X bool `json:"x,omitempty"`
}

type cookieState int

const (
	cookieAbsent cookieState = iota
	cookieValid
	cookieInvalid
)

func (a *Auth) attemptCookie() string { return a.cookieName("rastrillo_attempt") }

// newID is 16 random bytes, base64url: an attempt or continuation id.
func newID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// attemptAddress is what of a submitted address the cookie may keep:
// trimmed, and only if it is at most 254 bytes with no control bytes.
// Anything longer is dropped rather than truncated — a truncated
// address shown back on the Sent page would name somebody else.
func attemptAddress(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 254 || strings.ContainsFunc(s, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return ""
	}
	return s
}

// noteAttempt records what this browser just submitted and returns the
// attempt's id, or "" when nothing was written: the screen is off, or
// crypto/rand failed — in which case the answer falls back to today's
// unbound one rather than failing a sign-in over a screen nicety.
func (a *Auth) noteAttempt(w http.ResponseWriter, kind, address string, expectedKeymail bool) string {
	if !a.cfg.SigninScreen {
		return ""
	}
	id, err := newID()
	if err != nil {
		a.cfg.Logger.Error("rastrillo/auth: attempt id", "err", err)
		return ""
	}
	now := a.now().Unix()
	v, err := sealedcookie.Seal(a.attemptKey, attempt{
		O: a.cfg.Origin, ID: id, IAT: now, EXP: now + int64(attemptTTL/time.Second),
		K: kind, A: attemptAddress(address), X: kind == attemptLink && expectedKeymail,
	})
	if err != nil {
		a.cfg.Logger.Error("rastrillo/auth: seal attempt", "err", err)
		return ""
	}
	a.setCookie(w, a.attemptCookie(), v, int(attemptTTL/time.Second))
	return id
}

// openAttempt reads this browser's attempt. Invalid means present and
// not to be believed; SigninState marks it for deletion.
func (a *Auth) openAttempt(r *http.Request) (attempt, cookieState) {
	c, err := r.Cookie(a.attemptCookie())
	if err != nil {
		return attempt{}, cookieAbsent
	}
	var p attempt
	if sealedcookie.Open(a.attemptKey, c.Value, &p) != nil ||
		!sealedcookie.Fresh(p.O, a.cfg.Origin, p.IAT, p.EXP, a.now(), attemptTTL) {
		return attempt{}, cookieInvalid
	}
	return p, cookieValid
}

// answerSent is the magic-link answer, shared by Begin and AnswerAsSent
// so the two cannot drift: an address an admission wrapper refused and
// one that got a link must look the same down to the cookie, or the
// difference is a membership oracle (round 2, finding 17).
func (a *Auth) answerSent(w http.ResponseWriter, r *http.Request, address string) {
	id := a.noteAttempt(w, attemptLink, address, r.FormValue("expect") == "keymail")
	if id == "" {
		a.redirect(w, r, a.cfg.SigninPath+"?sent=1")
		return
	}
	a.redirect(w, r, a.cfg.SigninPath+"?sent=1&attempt="+id)
}
