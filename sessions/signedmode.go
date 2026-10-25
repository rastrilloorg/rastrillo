package sessions

import (
	"bytes"
	"context"
	"errors"
	"maps"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"

	"amadan.net/rastrillo/rastrillo/basepath"
)

// Signed turns a Sessions into signed mode: the session is a
// short-lived P-256 signed cookie and there are no rows. With Key set
// it is a hub and mints; with Keys only it is a tenant and verifies.
// A hub and its path-routed tenants share one origin, so they share
// this one cookie.
type Signed struct {
	// Name is the product's cookie name, "__Host-"+Name on https.
	Name string

	// Key signs. Only the hub holds it.
	Key *SigningKey

	// Keys verify, by kid: one, or two during a rotation. A hub that
	// leaves it empty verifies with Key's own public half.
	Keys map[string][]byte

	// Epoch is the subject's current epoch, read at sign-in and at
	// every refresh. ErrNoUser means the subject is gone. Required
	// with Key.
	Epoch func(ctx context.Context, subject string) (int64, error)

	// RefreshPath is the hub's refresh route. Default
	// DefaultRefreshPath.
	RefreshPath string
}

// DefaultRefreshPath is where an expired signed cookie is sent to be
// reissued.
const DefaultRefreshPath = "/_carlos/refresh"

var (
	// ErrSigned answers the row methods (Mint, List, Revoke,
	// RevokeOthers, RevokeAll) in signed mode.
	ErrSigned = errors.New("rastrillo/sessions: signed sessions keep no rows")

	// ErrVerifyOnly answers SignIn on a tenant, which holds no Key.
	ErrVerifyOnly = errors.New("rastrillo/sessions: this app verifies sessions and cannot mint them")

	// ErrNoUser is what Epoch returns for a subject that no longer
	// exists.
	ErrNoUser = errors.New("rastrillo/sessions: no such subject")
)

var signedName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

// signedConfig validates cfg.Signed and returns a deep copy of it, plus
// the key set verification uses. The copy is taken before any check,
// so what was checked is what is used: a caller editing its map during
// a rotation can neither race verification nor slip a key past New.
func signedConfig(cfg Config) (*Signed, map[string][]byte, error) {
	if cfg.DB != nil {
		return nil, nil, errors.New("rastrillo/sessions: signed sessions keep no rows; leave DB nil")
	}
	// Signed mode redirects to Origin plus a path, so a trailing slash
	// or a path here would send every refresh and sign-in somewhere the
	// hub does not answer.
	if u, err := url.Parse(cfg.Origin); err != nil || u.Host == "" || u.Scheme+"://"+u.Host != cfg.Origin {
		return nil, nil, errors.New("rastrillo/sessions: in signed mode Config.Origin is a scheme and host only, like https://app.example.com")
	}
	sg := *cfg.Signed
	sg.Keys = make(map[string][]byte, len(cfg.Signed.Keys))
	for kid, point := range cfg.Signed.Keys {
		sg.Keys[kid] = bytes.Clone(point)
	}
	if !signedName.MatchString(sg.Name) {
		return nil, nil, errors.New("rastrillo/sessions: Signed.Name is 1 to 32 of a-z, 0-9, _ and -, starting with a letter or digit")
	}
	for kid, point := range sg.Keys {
		// The check ParsePublicKeys makes, because a map built by hand
		// skips ParsePublicKeys.
		if err := validPublicKey(kid, point); err != nil {
			return nil, nil, err
		}
	}
	verify := maps.Clone(sg.Keys)
	if sg.Key != nil {
		if sg.Key.kp == nil {
			// A &SigningKey{} literal, not from NewSigningKey or
			// ParseSigningKey: refused here rather than at the first
			// sign-in.
			return nil, nil, errSecret
		}
		own := sg.Key.kp.SignPub()
		if point, ok := verify[sg.Key.kid]; ok && !bytes.Equal(point, own) {
			// Cookies this hub signs would name a kid its tenants
			// check against some other key, and every one would fail.
			return nil, nil, errors.New("rastrillo/sessions: Signed.Keys holds a different key under Signed.Key's key id")
		}
		verify[sg.Key.kid] = own
		if sg.Epoch == nil {
			return nil, nil, errors.New("rastrillo/sessions: Signed.Key needs Signed.Epoch")
		}
	}
	if len(verify) < 1 || len(verify) > 2 {
		return nil, nil, errors.New("rastrillo/sessions: Signed needs one or two key ids across Key and Keys")
	}
	if sg.RefreshPath == "" {
		sg.RefreshPath = DefaultRefreshPath
	}
	// toRefresh recognises the refresh route by comparing RefreshPath
	// with r.URL.Path, which Go has already decoded. A path that is not
	// already in that form never matches its own route: a query or
	// fragment, an escape such as /_carlos/%72efresh, or a segment that
	// cleaning removes (".", "..", "//", a trailing "/"). The route would
	// then be sent to itself, the loop counter in Refresh would never
	// run, and the browser would bounce until it gave up.
	if p := sg.RefreshPath; !strings.HasPrefix(p, "/") || strings.ContainsAny(p, "?#%") || path.Clean(p) != p {
		return nil, nil, errors.New("rastrillo/sessions: Signed.RefreshPath must be a clean absolute path like /_carlos/refresh: no ?, #, %, // or dot segments, and no trailing slash")
	}
	return &sg, verify, nil
}

// requestPath is the path and query the browser asked for. Behind
// http.StripPrefix or basepath.Handler, r.URL.Path has lost the
// tenant's /<slug> but the target as sent (basepath.Original, which is
// r.RequestURI) keeps it, and next and return_to must keep it to come
// back to this tenant. It carries the query, so it is never compared
// with RefreshPath.
func requestPath(r *http.Request) string {
	target := basepath.Original(r)
	if strings.HasPrefix(target, "/") {
		return target
	}
	// An absolute-form target, https://example.com/acme/x, which
	// net/http accepts and keeps whole. Its own escaped path and query,
	// as basepath.ProductURL takes them: r.URL has no slug, and next
	// would bring the reader back to the hub's root.
	if u, err := url.ParseRequestURI(target); err == nil && u.IsAbs() && u.Opaque == "" {
		p := u.EscapedPath()
		if p == "" {
			p = "/"
		}
		if u.RawQuery != "" {
			p += "?" + u.RawQuery
		}
		return p
	}
	return r.URL.RequestURI()
}

// signedCookie reads and verifies this request's cookie. The Session is
// set only for cookieValid. On a direct host it reads none: the hub's
// cookie is host-only on the product origin, so it should never arrive
// there, and if it did, a refresh from there would carry a next with no
// slug and the reader would come back to the hub's root (spec §7.3: the
// direct host carries no hub session).
func (s *Sessions) signedCookie(r *http.Request) (Session, cookieState) {
	if basepath.OnDirectHost(r) {
		return Session{}, cookieNone
	}
	c, err := r.Cookie(s.CookieName())
	if err != nil {
		return Session{}, cookieNone
	}
	claims, state := verifyCookie(s.keys, c.Value, s.now())
	if state != cookieValid {
		return Session{}, state
	}
	sess := Session{Subject: claims.Sub, Method: claims.Method, At: time.Unix(claims.At, 0).UTC()}
	if claims.AuthTime != 0 {
		sess.AuthTime = time.Unix(claims.AuthTime, 0).UTC()
	}
	return sess, cookieValid
}

// RefreshIfExpired sends a request whose signed cookie has expired to
// the hub's refresh route and reports whether it wrote that redirect.
// In row mode it does nothing and returns false.
func (s *Sessions) RefreshIfExpired(w http.ResponseWriter, r *http.Request) bool {
	if s.cfg.Signed == nil {
		return false
	}
	_, state := s.signedCookie(r)
	return s.toRefresh(w, r, state)
}

// toRefresh acts only on an expired cookie. An invalid one is never
// sent: a tenant whose key set lags a rotation would bounce every
// cookie the hub calls fresh, a loop. It compares r.URL.Path, because
// requestPath carries the query and never equals RefreshPath, which
// would send the refresh route to itself.
func (s *Sessions) toRefresh(w http.ResponseWriter, r *http.Request, state cookieState) bool {
	if state != cookieExpired || r.URL.Path == s.cfg.Signed.RefreshPath {
		return false
	}
	// 307 for an unsafe method, so a form posted after the cookie
	// expired is repeated with its body instead of lost.
	code := http.StatusTemporaryRedirect
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		code = http.StatusSeeOther
	}
	// Absolute, so a base-path rewrite of relative redirects cannot
	// turn it into /<slug>/_carlos/refresh.
	http.Redirect(w, r, s.cfg.Origin+s.cfg.Signed.RefreshPath+"?next="+url.QueryEscape(requestPath(r)), code)
	return true
}

// signedAdmit resolves for Middleware, Require and RequireFresh:
// Middleware's stash first, then the cookie. answered means it wrote
// the refresh redirect and the caller must write nothing more. It never
// clears a cookie: on one origin that cookie is the whole product's
// session.
func (s *Sessions) signedAdmit(w http.ResponseWriter, r *http.Request) (sess Session, ok, answered bool) {
	if sess, ok := Current(r); ok {
		return sess, true, false
	}
	sess, state := s.signedCookie(r)
	if state == cookieValid {
		return sess, true, false
	}
	return Session{}, false, s.toRefresh(w, r, state)
}

func (s *Sessions) signedMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, ok, answered := s.signedAdmit(w, r)
		if answered {
			return
		}
		if ok {
			r = WithSession(r, sess)
		}
		next.ServeHTTP(w, r)
	})
}

// signedGate is Require when fresh is nil, and RequireFresh otherwise.
func (s *Sessions) signedGate(next http.Handler, fresh func(Session) bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, ok, answered := s.signedAdmit(w, r)
		if answered {
			return
		}
		if !ok {
			s.refuse(w, r, false)
			return
		}
		if fresh != nil && !fresh(sess) {
			s.refuse(w, r, true)
			return
		}
		next.ServeHTTP(w, WithSession(r, sess))
	})
}

// signedSignIn mints a fresh session: a new sid, and the epoch the
// subject holds now, so a later "sign out everywhere" ends it at its
// next refresh.
func (s *Sessions) signedSignIn(w http.ResponseWriter, r *http.Request, sess Session) error {
	if s.cfg.Signed.Key == nil {
		return ErrVerifyOnly
	}
	epoch, err := s.cfg.Signed.Epoch(r.Context(), sess.Subject)
	if err != nil {
		return err
	}
	sid, err := newSID()
	if err != nil {
		return err
	}
	now := s.now().Unix()
	c := signedClaims{Sub: sess.Subject, SID: sid, Epoch: epoch, IAT: now, At: now, Method: sess.Method}
	c.EXP = s.signedExp(c)
	if !sess.AuthTime.IsZero() {
		c.AuthTime = sess.AuthTime.Unix()
	}
	value, err := s.cfg.Signed.Key.sign(c)
	if err != nil {
		return err
	}
	s.setCookie(w, value, int(s.cfg.TTL.Seconds()))
	return nil
}

// signedExp is min(iat + SignedLifetime, at + TTL). Without the second
// bound, a TTL under 15 minutes, or a refresh near the end of the
// session, would grant a cookie past the session's absolute end.
func (s *Sessions) signedExp(c signedClaims) int64 {
	return min(c.IAT+int64(SignedLifetime/time.Second), c.At+int64(s.cfg.TTL/time.Second))
}
