package sessions

import (
	"errors"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"amadan.net/rastrillo/rastrillo/csrf"
)

const (
	// refreshLoopLimit is how many fresh-cookie visits in a row the
	// refresh route answers before it calls the bouncing a loop. A real
	// loop bounces many times a second, and browsers give up after
	// about 20 redirects, so this fires first and leaves a log line.
	refreshLoopLimit = 10

	// refreshLoopWindow is how long, in seconds, the ten visits must
	// fall within. The window is fixed from its first visit: one that
	// slid with each visit would count a person who comes back every 9
	// seconds as a loop and log a clock fault that is not there.
	refreshLoopWindow = 10

	refreshLoopText = "This session could not be refreshed. Check that the hub and its apps agree on the time."
)

// Refresh is the hub's refresh route, mounted for every method at
// RefreshPath: Require, RequireFresh, Middleware and RefreshIfExpired
// send an expired cookie here, an unsafe method by 307. It reissues the
// cookie if the subject's epoch still matches, and sends the browser
// back to next. It never reads the body, so a 307 back hands the body
// to the browser unread and a form posted after the cookie expired is
// repeated with it. On a tenant, or in row mode, it is 404.
func (s *Sessions) Refresh(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Signed == nil || s.cfg.Signed.Key == nil {
		http.NotFound(w, r)
		return
	}
	safe := r.Method == http.MethodGet || r.Method == http.MethodHead
	// A cross-site form posting here could otherwise have the answer
	// clear the person's cookie: Lax keeps the cookie off that request,
	// but the browser still accepts the deletion.
	if !safe && !csrf.SameOrigin(r, s.cfg.Origin) {
		http.Error(w, "cross-origin form submission refused", http.StatusForbidden)
		return
	}
	next := s.refreshNext(r)
	c, err := r.Cookie(s.CookieName())
	if err != nil {
		// No Set-Cookie: there is nothing to clear, and a deletion here
		// is what a cross-site GET could otherwise trigger.
		http.Redirect(w, r, s.cfg.SigninPath+"?return_to="+url.QueryEscape(next), http.StatusSeeOther)
		return
	}
	now := s.now()
	claims, state := verifyCookie(s.keys, c.Value, now)
	switch state {
	case cookieValid:
		// Another tab or a parallel request refreshed first. Going back
		// without reissuing or reading the store is right for that, and
		// the counter catches the case where it is wrong.
		s.refreshBounce(w, r, next, safe)
		return
	case cookieExpired:
	default:
		// The hub is the authority on its own cookie, so clearing here
		// is safe where a tenant's clearing is not.
		s.refreshSignOut(w, r, next)
		return
	}
	end := claims.At + int64(s.cfg.TTL/time.Second)
	if now.Unix() >= end {
		s.refreshSignOut(w, r, next)
		return
	}
	epoch, err := s.cfg.Signed.Epoch(r.Context(), claims.Sub)
	if errors.Is(err, ErrNoUser) || err == nil && epoch != claims.Epoch {
		s.refreshSignOut(w, r, next)
		return
	}
	if err != nil {
		// The cookie is kept: a store outage must not sign everyone out.
		s.cfg.Logger.Error("rastrillo/sessions: refresh could not read the epoch", "err", err)
		http.Error(w, "Could not refresh your session. Try again in a minute.", http.StatusServiceUnavailable)
		return
	}
	// Signed under the current Key whatever kid it came in under, so a
	// rotation moves people onto the new key without signing them out.
	claims.IAT = now.Unix()
	claims.EXP = s.signedExp(claims)
	value, err := s.cfg.Signed.Key.sign(claims)
	if err != nil {
		s.cfg.Logger.Error("rastrillo/sessions: refresh could not sign the cookie", "err", err)
		http.Error(w, "Could not refresh your session. Try again.", http.StatusInternalServerError)
		return
	}
	// Max-Age is what is left of the session, not 15 minutes: the
	// browser keeps an expired cookie to bring back here, and drops it
	// once there is nothing left to refresh.
	s.setCookie(w, value, int(end-now.Unix()))
	if _, err := r.Cookie(s.refreshCounterName()); err == nil {
		s.setRefreshCounter(w, "", -1)
	}
	s.refreshBack(w, r, next, safe)
}

// refreshNext is the same-site next, or "/". A next that is the refresh
// route itself, once decoded and cleaned as the browser and
// http.Redirect will, would bring the browser straight back here.
func (s *Sessions) refreshNext(r *http.Request) string {
	next := SafeReturnTo(r.URL.Query().Get("next"), "/")
	u, err := url.Parse(next)
	if err != nil || path.Clean(u.Path) == s.cfg.Signed.RefreshPath {
		return "/"
	}
	return next
}

// refreshBack goes back to next: 303 for GET and HEAD, 307 otherwise so
// the browser repeats the method and its body.
func (s *Sessions) refreshBack(w http.ResponseWriter, r *http.Request, next string, safe bool) {
	code := http.StatusTemporaryRedirect
	if safe {
		code = http.StatusSeeOther
	}
	http.Redirect(w, r, next, code)
}

func (s *Sessions) refreshSignOut(w http.ResponseWriter, r *http.Request, next string) {
	s.clearCookie(w)
	http.Redirect(w, r, s.cfg.SigninPath+"?return_to="+url.QueryEscape(next), http.StatusSeeOther)
}

// refreshBounce goes back with a fresh cookie, counting the visit. A
// tenant whose clock is ahead keeps sending a cookie the hub calls
// fresh, and without the count that loops until the browser gives up,
// leaving no trace of why. The count lives in a cookie because a count
// per redirect chain would need a marker in next, which is the app's
// URL. Ten tabs refreshing in the same 10 seconds also trip it; a
// reload clears it.
func (s *Sessions) refreshBounce(w http.ResponseWriter, r *http.Request, next string, safe bool) {
	now := s.now().Unix()
	n, start := 0, now
	if c, err := r.Cookie(s.refreshCounterName()); err == nil {
		if v, at, ok := parseRefreshCounter(c.Value); ok && at <= now && now-at < refreshLoopWindow {
			n, start = v, at
		}
	}
	n++
	if n >= refreshLoopLimit {
		s.setRefreshCounter(w, "", -1)
		// The path only: the query is the app's and may carry anything.
		p, _, _ := strings.Cut(next, "?")
		s.cfg.Logger.Error("rastrillo/sessions: refresh loop: a fresh cookie came back to refresh 10 times in 10 seconds; check that the hub and its apps agree on the time", "next", p)
		http.Error(w, refreshLoopText, http.StatusInternalServerError)
		return
	}
	// Max-Age is what is left of the window, so the browser drops the
	// count when the window closes; the start in the value is what
	// decides, because a browser may keep a cookie a little longer.
	s.setRefreshCounter(w, strconv.Itoa(n)+"."+strconv.FormatInt(start, 10), int(start+refreshLoopWindow-now))
	s.refreshBack(w, r, next, safe)
}

// parseRefreshCounter reads "<count>.<unix seconds the window began>".
// Anything else reads as no counter, so a garbled cookie starts a fresh
// window rather than a 500.
func parseRefreshCounter(v string) (count int, start int64, ok bool) {
	c, st, found := strings.Cut(v, ".")
	if !found {
		return 0, 0, false
	}
	count, err := strconv.Atoi(c)
	if err != nil || count < 1 {
		return 0, 0, false
	}
	start, err = strconv.ParseInt(st, 10, 64)
	if err != nil || start < 1 {
		return 0, 0, false
	}
	return count, start, true
}

func (s *Sessions) refreshCounterName() string { return s.CookieName() + "_rf" }

func (s *Sessions) setRefreshCounter(w http.ResponseWriter, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name: s.refreshCounterName(), Value: value, Path: "/",
		MaxAge: maxAge, HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Secure: s.secure(),
	})
}
