package auth

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/keymaildev/signin"

	"amadan.net/rastrillo/rastrillo/lastsignin"
	"amadan.net/rastrillo/rastrillo/sessions"
)

// ErrNeedsLookup is what Subject answers when finding a subject would
// mean creating one, which a lookup must never do.
var ErrNeedsLookup = errors.New("rastrillo/auth: this configuration " +
	"resolves subjects by creating them; give signin a Lookup")

// Admit runs the admission gate, offers the session to the
// SecondFactor hook (which may trade it for a pending half-session
// and take over the response), and otherwise mints it and lands on
// return_to — Begin's own cookie if this request carries none itself
// — falling back to SignedInPath. Another way in calls it after
// verifying its credential, so refusal, the remembered way, return_to
// and the session are the same as for a magic link. A non-empty
// subject is the one the credential belongs to: if the address now
// resolves to another, the answer is a refusal.
func (auth *Auth) Admit(
	response http.ResponseWriter, request *http.Request,
	identity Identity, subject string,
) {
	// A first factor has verified, so this browser's attempt is over
	// whether or not the address is admitted. Left behind, it would keep
	// prefilling the form with an address already proved (round 3,
	// finding 25).
	auth.jar.EndAttempt(response)
	auth.spendReturnTo(response, request)
	if auth.cfg.Authorize != nil && !auth.cfg.Authorize(identity.Address) {
		auth.refuseAdmission(response, request, identity)
		return
	}
	resolved, err := auth.subjectFor(request.Context(), identity.Address)
	if errors.Is(err, sessions.ErrNoUser) {
		auth.refuseAdmission(response, request, identity)
		return
	}
	if err != nil {
		auth.logError("rastrillo/auth: subject for address", err)
		http.Error(response, "sign-in failed", http.StatusInternalServerError)
		return
	}
	// The address is left out of the log: it is the one thing a blind
	// deployment never records.
	if subject != "" && subject != resolved {
		auth.cfg.Logger.Error("rastrillo/auth: the credential's subject " +
			"and the address's subject differ")
		auth.refuseAdmission(response, request, identity)
		return
	}
	auth.mint(response, request, identity, resolved)
}

// spendReturnTo moves the kept destination onto this request.
func (auth *Auth) spendReturnTo(
	response http.ResponseWriter, request *http.Request,
) {
	// The kept destination goes onto this request's own form, where
	// sessions.SafeReturn and secondfactor.Hold already look, so the
	// SecondFactor hook and the redirect at the end agree on it without
	// either knowing the cookie exists. Spent here, admitted or not: the
	// attempt it belonged to is over.
	if _, err := request.Cookie(auth.returnCookie()); err == nil {
		to := auth.keptReturnTo(request)
		auth.clearCookie(response, auth.returnCookie())
		_ = request.ParseForm()
		if to != "" && request.Form.Get("return_to") == "" {
			request.Form.Set("return_to", to)
		}
	}
}

// mint remembers the way in, offers the session to the SecondFactor
// hook, and otherwise signs it in and redirects.
func (auth *Auth) mint(
	response http.ResponseWriter, request *http.Request,
	identity Identity, resolved string,
) {
	// The address, never the subject: SubjectFor may make subjects
	// opaque, and the screen shows this address back. Before the
	// SecondFactor hook because nothing after it knows the address —
	// Gate.Complete holds only the subject — so a sign-in held for a
	// second factor is remembered too. The record says which door this
	// browser used, never that the person got in.
	auth.jar.Remember(response, lastsignin.Record{
		Method: string(identity.Method), Address: identity.Address,
	})
	session := sessions.Session{
		Subject:  resolved,
		Method:   string(identity.Method),
		AuthTime: auth.gestureTime(identity),
	}
	if auth.cfg.SecondFactor != nil {
		done, err := auth.cfg.SecondFactor(response, request, session)
		if err != nil {
			auth.cfg.Logger.Error("rastrillo/auth: second factor", "err", err)
			http.Error(response, "sign-in failed",
				http.StatusInternalServerError)
			return
		}
		if done {
			return
		}
	}
	if err := auth.sessions.SignIn(response, request, session); err != nil {
		auth.logError("rastrillo/auth: store session", err)
		http.Error(response, "sign-in failed", http.StatusInternalServerError)
		return
	}
	auth.redirect(response, request,
		sessions.SafeReturn(request, auth.cfg.SignedInPath))
}

// gestureTime is when identity's credential was proved. Every way but
// Keymail proves it in this request, so an unset time is now; Keymail's
// is its server's auth_time, and none stays none, because the person
// may have proved nothing to that server for hours. Without the stamp a
// freshness check would fall back on when the session row was made,
// which a refresh moves without any proof.
func (auth *Auth) gestureTime(identity Identity) time.Time {
	if identity.AuthTime.IsZero() &&
		identity.Method != signin.MethodKeymail {
		return auth.now()
	}
	return identity.AuthTime
}

// Subject is the subject an address signs in as, found without creating
// anything. Only a plain database configuration can answer: Records,
// BlindLinks and SubjectFor all decide a subject by making or mapping
// one, so they answer ErrNeedsLookup and the caller brings its own.
func (auth *Auth) Subject(
	_ context.Context, address string,
) (string, error) {
	if auth.users != nil || auth.cfg.BlindLinks != nil ||
		auth.cfg.SubjectFor != nil {
		return "", ErrNeedsLookup
	}
	return address, nil
}

// SigninPath is the app's sign-in page (Config.SigninPath, "/signin"
// by default), where another way in sends a visitor back with its
// problem in the query.
func (auth *Auth) SigninPath() string { return auth.cfg.SigninPath }

// Authorized is Config.Authorize's answer for address, true with no
// hook, so a way in auth does not run (a passkey) admits whom auth's
// own ways would.
func (auth *Auth) Authorized(address string) bool {
	return auth.cfg.Authorize == nil || auth.cfg.Authorize(address)
}

// Assembles reports the first setting a signin built on this Auth
// cannot honour, so the mistake surfaces at start-up.
func (auth *Auth) Assembles() error {
	switch {
	case auth.users != nil:
		return errors.New(
			"rastrillo/auth: signin cannot assemble an Auth with Records set")
	case auth.cfg.BlindLinks != nil:
		return errors.New("rastrillo/auth: signin cannot assemble an Auth " +
			"with BlindLinks set")
	case auth.cfg.SecondFactor != nil:
		return errors.New("rastrillo/auth: signin cannot assemble an Auth " +
			"with SecondFactor set")
	}
	return nil
}
