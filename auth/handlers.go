package auth

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/keymaildev/signin"

	"github.com/carlosframework/rastrillo/sessions"
)

// Begin is POST /signin: classify the submitted address and start
// whichever ceremony it gets. CSRF-checked here, not left to the app —
// Begin is an unauthenticated endpoint that sends mail and makes
// outbound DNS/HTTPS calls decided by the submitted address (a blind-
// SSRF surface by design, per signin's own README), so a cross-site
// form must never reach it.
//
// Form fields: address (required); force (any non-empty value) skips
// classification and goes straight to the magic link — the escape hatch
// the callback offers after a failed keymail approval.
//
// Outcomes land on Config.SigninPath: ?sent=1 (link emailed — also the
// answer when nothing was sent because the address or budget was bad;
// distinguishing would be an enumeration oracle on an unauthenticated
// endpoint), ?err=rate (over budget), ?err=address (unparseable), or a
// 303 to the keymail authorize URL with the pending cookie set.
func (a *Auth) Begin(w http.ResponseWriter, r *http.Request) {
	if !a.sameOrigin(r) {
		http.Error(w, "cross-origin form submission refused", http.StatusForbidden)
		return
	}
	address := r.FormValue("address")
	var force signin.Method
	if r.FormValue("force") != "" {
		force = signin.MethodMagicLink
	}

	next, err := a.flow.Begin(r.Context(), address, clientIP(r), force)
	switch {
	case errors.Is(err, signin.ErrRateLimited):
		a.redirect(w, r, a.cfg.SigninPath+"?err=rate")
		return
	case errors.Is(err, signin.ErrBadAddress):
		a.redirect(w, r, a.cfg.SigninPath+"?err=address")
		return
	case err != nil:
		a.cfg.Logger.Error("rastrillo/auth: begin sign-in", "err", err)
		a.redirect(w, r, a.cfg.SigninPath+"?err=1")
		return
	}

	if next.Kind == signin.NextKeymail {
		a.setCookie(w, a.pendingCookie(), next.Pending, int(pendingTTL.Seconds()))
		// next.Redirect points at a host the submitted address chose
		// (via its DNS delegation) — untrusted output, sent as a
		// redirect the browser follows, never echoed into a page.
		a.redirect(w, r, next.Redirect)
		return
	}
	a.redirect(w, r, a.cfg.SigninPath+"?sent=1")
}

// Callback is GET /auth/callback: the keymail OAuth return. The pending
// cookie is single-use — cleared the moment it is read, so a failed
// attempt cannot retry the same state for the rest of the cookie's
// lifetime (seapointish's rule). A keymail approval that could not be
// completed is never a dead end: it redirects to the signin page with
// ?force=1&err=keymail so the page can offer the plain-email path.
func (a *Auth) Callback(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(a.pendingCookie())
	if err != nil {
		a.redirect(w, r, a.cfg.SigninPath+"?err=expired")
		return
	}
	a.clearCookie(w, a.pendingCookie())

	id, err := a.flow.CompleteKeymail(r.Context(), c.Value,
		r.URL.Query().Get("code"), r.URL.Query().Get("state"))
	switch {
	case errors.Is(err, signin.ErrPendingInvalid):
		// Expired, tampered with, or a state mismatch — all "start again".
		a.redirect(w, r, a.cfg.SigninPath+"?err=expired")
		return
	case errors.Is(err, signin.ErrAddressMismatch):
		// Keymail's identity service is deliberately anonymous: the
		// gesture proves some passkey was verified in this browser, and
		// only the address comparison ties it to the address the flow
		// started for. A mismatch is a hard stop, not a retry.
		http.Error(w, "Keymail returned a different address from the one you entered.", http.StatusForbidden)
		return
	case err != nil:
		a.cfg.Logger.Error("rastrillo/auth: keymail callback", "err", err)
		a.redirect(w, r, a.cfg.SigninPath+"?force=1&err=keymail")
		return
	}
	a.admit(w, r, id)
}

// Verify is GET /auth/verify: the magic-link landing. One error for
// unknown, used and expired alike — signin refuses to be an oracle, and
// so does this handler.
func (a *Auth) Verify(w http.ResponseWriter, r *http.Request) {
	id, err := a.flow.CompleteMagicLink(r.Context(), r.URL.Query().Get("token"))
	if err != nil {
		a.redirect(w, r, a.cfg.SigninPath+"?err=expired")
		return
	}
	a.admit(w, r, id)
}

// Signout is POST /signout: revoke the session row (real revocation —
// the cookie alone dying would leave the token valid) and clear the
// cookie.
func (a *Auth) Signout(w http.ResponseWriter, r *http.Request) {
	if !a.sameOrigin(r) {
		http.Error(w, "cross-origin form submission refused", http.StatusForbidden)
		return
	}
	a.sessions.SignOut(w, r)
	a.redirect(w, r, a.cfg.SigninPath)
}

// admit runs the admission gate and mints the session.
func (a *Auth) admit(w http.ResponseWriter, r *http.Request, id Identity) {
	if a.cfg.Authorize != nil && !a.cfg.Authorize(id.Address) {
		http.Error(w, "This address is verified but not admitted here.", http.StatusForbidden)
		return
	}
	if err := a.sessions.SignIn(w, r, sessions.Session{
		Subject:  id.Address,
		Method:   string(id.Method),
		AuthTime: id.AuthTime,
	}); err != nil {
		a.cfg.Logger.Error("rastrillo/auth: store session", "err", err)
		http.Error(w, "sign-in failed", http.StatusInternalServerError)
		return
	}
	a.redirect(w, r, a.cfg.SignedInPath)
}

// identityFrom converts a resolved sessions.Session to this plugin's
// Identity view of it.
func identityFrom(s sessions.Session) Identity {
	return Identity{
		Address:  s.Subject,
		Method:   signin.Method(s.Method),
		AuthTime: s.AuthTime,
		At:       s.At,
	}
}

// SessionFrom resolves the request's session cookie to its identity —
// the direct lookup, for handlers outside RequireSession that want to
// know who (a public page with a signed-in header, say).
func (a *Auth) SessionFrom(r *http.Request) (Identity, bool) {
	s, ok := a.sessions.From(r)
	if !ok {
		return Identity{}, false
	}
	return identityFrom(s), true
}

type identityCtxKey struct{}

// RequireSession guards a handler: no valid session sends a page
// request to the sign-in page and answers anything else 403; a valid
// one rides the request context for From — and for sessions.Current,
// which this middleware stashes too (sessions.WithSession), so code
// that reads identity through the sessions package alone — generated
// scoped actions in particular — agrees with From about who is signed
// in. (Before this stash, sessions.Current behind RequireSession
// answered nothing at all — the uid-0 trap's uglier sibling.)
func (a *Auth) RequireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s, ok := a.sessions.From(r)
		if !ok {
			a.refuseSession(w, r, "")
			return
		}
		r = sessions.WithSession(r, s)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), identityCtxKey{}, identityFrom(s))))
	})
}

// RequireFreshSession is RequireSession plus step-up: the credential
// must have been verified within maxAge (keymail's auth_time when the
// deployment reports one, else the session's own minting time — see
// sessions.Fresh). A stale-but-valid page request redirects to the
// sign-in page with reauth=1 so it can say "confirm it's you";
// re-verifying rotates the session fresh. Against a keymail deployment
// that advertises reauth, signin sends prompt=login, so the ceremony
// really re-authenticates rather than silently reusing the inbox
// session.
func (a *Auth) RequireFreshSession(maxAge time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			s, ok := a.sessions.From(r)
			if !ok {
				a.refuseSession(w, r, "")
				return
			}
			if !sessions.Fresh(s, maxAge, time.Now()) {
				a.refuseSession(w, r, "?reauth=1")
				return
			}
			r = sessions.WithSession(r, s)
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), identityCtxKey{}, identityFrom(s))))
		})
	}
}

// refuseSession sends a page request to the sign-in page (query names
// why, when step-up rather than plain sign-in is wanted) and answers
// anything else 403.
func (a *Auth) refuseSession(w http.ResponseWriter, r *http.Request, query string) {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		a.redirect(w, r, a.cfg.SigninPath+query)
		return
	}
	if query != "" {
		http.Error(w, "re-authentication required", http.StatusForbidden)
		return
	}
	http.Error(w, "signed out", http.StatusForbidden)
}

// From returns the identity RequireSession resolved for this request.
func From(r *http.Request) (Identity, bool) {
	id, ok := r.Context().Value(identityCtxKey{}).(Identity)
	return id, ok
}

func (a *Auth) redirect(w http.ResponseWriter, r *http.Request, to string) {
	http.Redirect(w, r, to, http.StatusSeeOther)
}

// clientIP is the per-IP rate-limit key: the connection's remote host.
// Behind the platform edge every connection shares the edge's address,
// collapsing the per-IP budget into a global one — a conservative
// failure (limits bite sooner, never later).
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
