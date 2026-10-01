package auth

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"
	"time"

	"amadan.net/rastrillo/rastrillo/internal/sealedcookie"
)

// continuationTTL is the pending cookie's own life: a continuation
// describes one pending round trip, and outliving it would only offer
// an authorize URL for an attempt that is already dead.
const continuationTTL = pendingTTL

// continuation is the pending cookie's sidecar: the authorize URL the
// sign-in page navigates to, and what binds a callback to it. Separate
// from the attempt cookie because the two live differently — a magic
// link submitted after a keymail one replaces the attempt, while the
// keymail round trip is still out and its callback must still be
// recognised (round 2, finding 19).
type continuation struct {
	O   string `json:"o"`
	ID  string `json:"id"`
	IAT int64  `json:"iat"`
	EXP int64  `json:"exp"`
	U   string `json:"u"`
	// ST is the digest of the URL's state, so Callback can tell a
	// callback for this attempt from one for any other.
	ST string `json:"st"`
	// PH is the digest of the pending cookie written beside it, so a
	// continuation dies with its pending cookie and never outlives the
	// attempt it describes.
	PH string `json:"ph"`
}

func (a *Auth) continueCookie() string { return a.cookieName("rastrillo_continue") }

// digest is base64url SHA-256: enough to compare, and nothing a reader
// of the cookie could replay.
func digest(s string) string {
	sum := sha256.Sum256([]byte(s))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func (a *Auth) sealContinuation(authorizeURL, pending string) (id, value string, err error) {
	u, err := url.Parse(authorizeURL)
	if err != nil {
		return "", "", err
	}
	if id, err = newID(); err != nil {
		return "", "", err
	}
	now := a.now().Unix()
	value, err = sealedcookie.Seal(a.continueKey, continuation{
		O: a.cfg.Origin, ID: id, IAT: now, EXP: now + int64(continuationTTL/time.Second),
		U: authorizeURL, ST: digest(u.Query().Get("state")), PH: digest(pending),
	})
	return id, value, err
}

func (a *Auth) openContinuation(r *http.Request) (continuation, cookieState) {
	c, err := r.Cookie(a.continueCookie())
	if err != nil {
		return continuation{}, cookieAbsent
	}
	var p continuation
	if sealedcookie.Open(a.continueKey, c.Value, &p) != nil ||
		!sealedcookie.Fresh(p.O, a.cfg.Origin, p.IAT, p.EXP, a.now(), continuationTTL) {
		return continuation{}, cookieInvalid
	}
	return p, cookieValid
}

// continuationFor is the authorize URL this browser may be sent to for
// ?continue=id. The query carries only an id that means nothing without
// this browser's own cookie, so ?continue=https://evil can never become
// a link. It resolves only while the continuation is in date, names
// this id, belongs to the pending cookie the browser holds now — so it
// dies with that cookie, and going back after the callback gives
// Expired — and its URL still passes the predicate.
func (a *Auth) continuationFor(r *http.Request, id string) (string, bool) {
	p, st := a.openContinuation(r)
	if st != cookieValid || id == "" || p.ID != id {
		return "", false
	}
	pending, err := r.Cookie(a.pendingCookie())
	if err != nil || digest(pending.Value) != p.PH {
		return "", false
	}
	if !a.validAuthorizeURL(p.U) {
		return "", false
	}
	return p.U, true
}

// authorizeParams is every parameter keymail's authorize URL may carry
// (K/keymail.go:44-57). Anything else is not a URL auth built.
var authorizeParams = map[string]bool{
	"client_id": true, "redirect_uri": true, "scope": true, "state": true,
	"code_challenge": true, "code_challenge_method": true, "prompt": true,
}

// validAuthorizeURL is fichas' predicate (F/auth_navigation.go:79-99)
// made stricter, every expected value taken from the configuration that
// built the URL. It runs before the URL is sealed and again before it
// becomes a link, because this is the one place auth turns a URL into
// something the browser navigates to: the one place an open redirect
// could be born.
func (a *Auth) validAuthorizeURL(raw string) bool {
	// Checked on the raw string: url.Parse drops an empty fragment, and
	// any '#' at all is not something the library writes.
	if strings.ContainsRune(raw, '#') {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Opaque != "" ||
		u.Path != "/oauth/authorize" || u.RawPath != "" {
		return false
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return false
	}
	for k, v := range q {
		if !authorizeParams[k] || len(v) != 1 || v[0] == "" {
			return false
		}
	}
	if p, ok := q["prompt"]; ok && p[0] != "login" {
		return false
	}
	// The six required parameters need no presence check of their own:
	// each is pinned below to a non-empty value or to the PKCE shape, and
	// an absent one reads as "", which matches neither.
	origin := strings.TrimSuffix(a.cfg.Origin, "/")
	if q.Get("client_id") != origin || q.Get("redirect_uri") != origin+callbackPath ||
		q.Get("scope") != "identify" || q.Get("code_challenge_method") != "S256" ||
		!pkceValue(q.Get("state")) || !pkceValue(q.Get("code_challenge")) {
		return false
	}
	return a.servers == nil || a.servers[serverKey(u.Host)]
}

// pkceValue is 43 base64url characters: what signin.NewVerifier and
// ChallengeS256 produce (K/keymail.go:133-145).
func pkceValue(s string) bool {
	if len(s) != 43 {
		return false
	}
	for _, c := range s {
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}
