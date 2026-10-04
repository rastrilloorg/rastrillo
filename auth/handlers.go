package auth

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/keymaildev/signin"

	"amadan.net/rastrillo/rastrillo/clientip"
	"amadan.net/rastrillo/rastrillo/lastsignin"
	"amadan.net/rastrillo/rastrillo/pow"
	"amadan.net/rastrillo/rastrillo/sessions"
)

// Begin is POST /signin: classify the submitted address and start
// whichever ceremony it gets. CSRF-checked here, not left to the app —
// Begin is an unauthenticated endpoint that sends mail and makes
// outbound DNS/HTTPS calls decided by the submitted address (a blind-
// SSRF surface by design, per signin's own README), so a cross-site
// form must never reach it.
//
// With Config.Proof set, the submission must also carry a solved
// challenge, checked before anything below reads a field or spends the
// rate budget; a refusal lands on ?err=check&rec=1.
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
//
// With Config.SigninScreen on, every answer also records this attempt
// for the screen (the attempt cookie), a sent link lands on
// ?sent=1&attempt=<id> so the Sent page can name the address, and an
// expect=keymail field — which only the remembered-Keymail one-tap
// sends — marks a link that went out where Keymail was promised. expect
// never chooses a path. The keymail answer is then no longer a 303 to
// the authorize URL but to SigninPath?continue=<id>, the URL kept in a
// sealed continuation cookie (continueKeymail says why).
func (a *Auth) Begin(w http.ResponseWriter, r *http.Request) {
	if !a.sameOrigin(r) {
		http.Error(w, "cross-origin form submission refused", http.StatusForbidden)
		return
	}
	recovered, ok := a.frontDoor(w, r)
	if !ok {
		return
	}
	force := r.FormValue("force") != ""
	address := r.FormValue("address")
	var method signin.Method
	if force {
		method = signin.MethodMagicLink
	}

	next, err := a.flow.Begin(r.Context(), address, clientip.From(r, a.hops), method)
	switch {
	case errors.Is(err, signin.ErrRateLimited):
		a.noteAttempt(w, attemptProblem, address, false)
		a.redirect(w, r, a.problemURL("rate", recovered, force))
		return
	case errors.Is(err, signin.ErrBadAddress):
		a.noteAttempt(w, attemptProblem, address, false)
		a.redirect(w, r, a.problemURL("address", recovered, force))
		return
	case err != nil:
		a.cfg.Logger.Error("rastrillo/auth: begin sign-in", "err", err)
		a.noteAttempt(w, attemptProblem, address, false)
		a.redirect(w, r, a.problemURL("1", recovered, force))
		return
	}

	if next.Kind == signin.NextKeymail {
		if a.cfg.SigninScreen {
			a.continueKeymail(w, r, address, next, recovered)
			return
		}
		a.setCookie(w, a.pendingCookie(), next.Pending, int(pendingTTL.Seconds()))
		// next.Redirect points at a host the submitted address chose
		// (via its DNS delegation) — untrusted output, sent as a
		// redirect the browser follows, never echoed into a page.
		a.redirect(w, r, next.Redirect)
		return
	}
	a.answerSent(w, r, address)
}

// frontDoor is the Config.Proof check every POST to the sign-in form
// meets first, Begin's and AnswerAsSent's alike. One function so the
// two cannot drift: an admission wrapper calls Begin for a member and
// AnswerAsSent for anyone else, so if AnswerAsSent skipped this, a post
// with no proof would be refused for a member and answered sent=1 for a
// stranger, telling anyone who is a member without a single solve. It
// also spends the token on both paths, or a stranger's one solved token
// could be replayed for ever where a member's works once. ok false
// means the refusal has been written. With Proof unset it admits
// everything and writes nothing.
func (a *Auth) frontDoor(w http.ResponseWriter, r *http.Request) (recovered, ok bool) {
	if a.cfg.Proof == nil {
		return false, true
	}
	// First, before any FormValue: FormValue swallows a parse error and
	// a later ParseForm does not repeat it, so reading a field first
	// would let a malformed body past the guard's bounds check. Check
	// rather than Admit: every outcome redirects, the next GET mints a
	// new challenge, and the spend must land before a probe or a mail
	// does.
	adm := a.cfg.Proof.Check(r, pow.Want{Scope: ProofScope})
	if !adm.OK {
		a.cfg.Logger.Debug("rastrillo/auth: sign-in refused at the front door", "reason", adm.Reason, "also", adm.Also)
		a.noteAttempt(w, attemptProblem, r.FormValue("address"), false)
		a.redirect(w, r, a.problemURL("check", true, r.PostFormValue("force") != ""))
		return false, false
	}
	return adm.Recovered(), true
}

// problemURL is the sign-in page for a problem. rec keeps recovery
// sticky: a visitor whose password manager fills the honeypot was
// given a trapless form, and an ordinary form after their next typo
// would trap them again. force keeps the send-a-link-instead choice a
// failed keymail exchange offered: without it, a refusal would drop it
// and Begin would classify the address back to the failing provider.
// Both are attacker-controllable and select nothing a script can use:
// a recovery form costs the same proof.
func (a *Auth) problemURL(problem string, rec, force bool) string {
	q := url.Values{"err": {problem}}
	if rec {
		q.Set("rec", "1")
	}
	if force {
		q.Set("force", "1")
	}
	return a.cfg.SigninPath + "?" + q.Encode()
}

// continueKeymail is the keymail answer with SigninScreen on. A 303
// straight to the provider is refused by the default CSP, whose
// form-action 'self' covers a form's whole redirect chain; so this
// stays on the origin — the pending cookie as today, a continuation
// cookie holding the authorize URL, and a redirect to the sign-in page,
// whose Continue state navigates on by itself. recovered keeps rec=1
// on its failures as on Begin's: a visitor holding a recovery form
// would otherwise be handed the honeypot again after a failure that
// was not theirs. force is always false here, since a forced post never
// reaches keymail.
func (a *Auth) continueKeymail(w http.ResponseWriter, r *http.Request, address string, next signin.Next, recovered bool) {
	if !a.validAuthorizeURL(next.Redirect) {
		// A correct library never builds one. If one appears it is not
		// turned into a link, and nothing is left half-set.
		a.cfg.Logger.Error("rastrillo/auth: the keymail authorize URL failed the predicate; not continuing")
		a.redirect(w, r, a.problemURL("1", recovered, false))
		return
	}
	id, value, err := a.sealContinuation(next.Redirect, next.Pending)
	if err != nil {
		a.cfg.Logger.Error("rastrillo/auth: seal continuation", "err", err)
		a.redirect(w, r, a.problemURL("1", recovered, false))
		return
	}
	a.setCookie(w, a.pendingCookie(), next.Pending, int(pendingTTL.Seconds()))
	a.setCookie(w, a.continueCookie(), value, int(continuationTTL.Seconds()))
	a.noteAttempt(w, attemptKeymail, address, false)
	a.redirect(w, r, a.cfg.SigninPath+"?continue="+id)
}

// AnswerAsSent is Begin's magic-link answer without the magic link, for
// an admission wrapper that refuses an address before Begin can
// classify it. A wrapper that answered a refusal with a plain ?sent=1
// would give a refused address no attempt cookie and no attempt= while
// an admitted one got both: a membership oracle on the first try. This
// answers exactly as Begin does for a sent link, down to the cookie,
// and sends nothing. With SigninScreen off that is today's plain
// ?sent=1 and no cookie.
//
// With Config.Proof set it checks and spends the challenge first, as
// Begin does, and refuses a post that fails with Begin's own ?err=check
// answer; only an admitted post is answered as sent.
//
// It does not hide what classification and the per-address rate limit
// reveal; docs/site/magic-links.md says what does.
func (a *Auth) AnswerAsSent(w http.ResponseWriter, r *http.Request) {
	if !a.sameOrigin(r) {
		http.Error(w, "cross-origin form submission refused", http.StatusForbidden)
		return
	}
	if _, ok := a.frontDoor(w, r); !ok {
		return
	}
	a.answerSent(w, r, r.FormValue("address"))
}

// Callback is GET /auth/callback: the keymail OAuth return. The pending
// cookie is single-use — cleared the moment it is read, so a failed
// attempt cannot retry the same state for the rest of the cookie's
// lifetime (seapointish's rule). A keymail approval that could not be
// completed is never a dead end: it redirects to the signin page with
// ?force=1&err=keymail so the page can offer the plain-email path.
// With SigninScreen on, a callback whose state is not the attempt the
// browser's continuation describes answers Expired without clearing
// anything (see the comment inside).
func (a *Auth) Callback(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(a.pendingCookie())
	if err != nil {
		a.redirect(w, r, a.cfg.SigninPath+"?err=expired")
		return
	}
	if a.cfg.SigninScreen {
		// Today the pending cookie is cleared the moment it is read and
		// the library then finds a state mismatch — so a late callback
		// from tab A, or a callback URL a third party makes the browser
		// open, throws away tab B's newer attempt. When the continuation
		// describes the pending cookie just read and this callback's
		// state is not that attempt's, the callback belongs to some other
		// attempt: answer Expired and leave the pair alone. The pending
		// blob's own authenticated expiry still bounds it; nothing is
		// refreshed. Any other case — no continuation, or one left from
		// an earlier attempt or a screen-off Begin — is today's path.
		if p, st := a.openContinuation(r); st == cookieValid && p.PH == digest(c.Value) &&
			p.ST != digest(r.URL.Query().Get("state")) {
			a.redirect(w, r, a.cfg.SigninPath+"?err=expired")
			return
		}
		a.clearCookie(w, a.continueCookie())
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

// admit runs the admission gate, offers the session to the
// SecondFactor hook (which may trade it for a pending half-session
// and take over the response), and otherwise mints it.
func (a *Auth) admit(w http.ResponseWriter, r *http.Request, id Identity) {
	// A first factor has verified, so this browser's attempt is over
	// whether or not the address is admitted. Left behind, it would keep
	// prefilling the form with an address already proved (round 3,
	// finding 25).
	a.jar.EndAttempt(w)
	if a.cfg.Authorize != nil && !a.cfg.Authorize(id.Address) {
		http.Error(w, "This address is verified but not admitted here.", http.StatusForbidden)
		return
	}
	subject := id.Address
	if a.cfg.SubjectFor != nil {
		s, err := a.cfg.SubjectFor(id.Address)
		if err != nil {
			// No fallback to the address: an app sets this hook
			// precisely because writing one is the failure it cannot
			// have.
			a.cfg.Logger.Error("rastrillo/auth: subject for address", "err", err)
			http.Error(w, "sign-in failed", http.StatusInternalServerError)
			return
		}
		subject = s
	}
	// The address, never the subject: SubjectFor may make subjects
	// opaque, and the screen shows this address back. Before the
	// SecondFactor hook because nothing after it knows the address —
	// Gate.Complete holds only the subject — so a sign-in held for a
	// second factor is remembered too. The record says which door this
	// browser used, never that the person got in.
	a.jar.Remember(w, lastsignin.Record{Method: string(id.Method), Address: id.Address})
	sess := sessions.Session{
		Subject:  subject,
		Method:   string(id.Method),
		AuthTime: id.AuthTime,
	}
	if a.cfg.SecondFactor != nil {
		done, err := a.cfg.SecondFactor(w, r, sess)
		if err != nil {
			a.cfg.Logger.Error("rastrillo/auth: second factor", "err", err)
			http.Error(w, "sign-in failed", http.StatusInternalServerError)
			return
		}
		if done {
			return
		}
	}
	if err := a.sessions.SignIn(w, r, sess); err != nil {
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
