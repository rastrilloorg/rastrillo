package signin

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"amadan.net/rastrillo/rastrillo/auth"
	"amadan.net/rastrillo/rastrillo/basepath"
	"amadan.net/rastrillo/rastrillo/clientip"
	"amadan.net/rastrillo/rastrillo/csrf"
	"amadan.net/rastrillo/rastrillo/lastsignin"
	"amadan.net/rastrillo/rastrillo/pow"
	"amadan.net/rastrillo/rastrillo/sessions"
)

// secretWay is what tells the two secret ways in apart: the door the
// screen goes back to, the problem a miss shows, the form field the
// secret arrives in, the method a session records, and the check.
type secretWay struct {
	way     auth.SigninWay
	problem auth.SigninProblem
	field   string
	// identity carries the method only; the address and times are this
	// request's.
	identity auth.Identity
	check    secretCheck
}

// SignInWithCode is POST CodePath: an address and the code its
// authenticator app shows.
func (signins *Signins) SignInWithCode(
	response http.ResponseWriter, request *http.Request,
) {
	if signins.codes == nil {
		http.NotFound(response, request)
		return
	}
	signins.signInWithSecret(response, request, secretWay{
		way: auth.WayAuthenticator, problem: auth.ProblemCode,
		field:    "code",
		identity: auth.Identity{Method: lastsignin.MethodTOTP},
		check:    signins.checkCode,
	})
}

// SignInWithPassphrase is POST PassphrasePath: an address and its
// passphrase.
func (signins *Signins) SignInWithPassphrase(
	response http.ResponseWriter, request *http.Request,
) {
	if signins.config.Ways&Passphrase == 0 {
		http.NotFound(response, request)
		return
	}
	signins.signInWithSecret(response, request, secretWay{
		way: auth.WayPassphrase, problem: auth.ProblemPassphrase,
		field:    "passphrase",
		identity: auth.Identity{Method: lastsignin.MethodPassphrase},
		check:    signins.checkPassphrase,
	})
}

// signInWithSecret finds the subject, then reserves the attempt against
// its budgets before the secret is checked, so a paused account costs a
// guesser a lookup and teaches nothing, and answers an unknown address
// exactly as a wrong secret: the same check runs, the same miss is kept
// and the same page comes back.
func (signins *Signins) signInWithSecret(
	response http.ResponseWriter, request *http.Request, way secretWay,
) {
	if !signins.admitsTheRequest(response, request, way) {
		return
	}
	address := cleanAddress(request.PostFormValue("address"))
	ctx, now := request.Context(), signins.config.Now()
	subject, err := signins.subject(ctx, address)
	if signins.failed(response, err) {
		return
	}
	held, paused, err := signins.limit.reserve(ctx, now,
		signins.budgets(request, subject, address))
	if signins.failed(response, err) {
		return
	}
	if paused {
		signins.sendBack(response, request, way, auth.ProblemPaused, false)
		return
	}
	matched, err := signins.matches(request, subject, way)
	if signins.failed(response, err) {
		return
	}
	if !matched {
		signins.sendBack(response, request, way, way.problem, false)
		return
	}
	if err := signins.limit.release(ctx, held); err != nil {
		signins.config.Logger.Error("rastrillo/signin: release", "err", err)
	}
	identity := way.identity
	identity.Address, identity.AuthTime, identity.At = address, now, now
	signins.config.Auth.Admit(response, request, identity, subject)
}

// budgets are the two an attempt counts against: the account's and the
// client's.
func (signins *Signins) budgets(
	request *http.Request, subject, address string,
) []bucket {
	return []bucket{accountBucket(subject, address),
		clientBucket(clientip.From(request, signins.hops))}
}

// matches is whether the posted secret is subject's. An unknown address
// still runs the check, against no subject.
func (signins *Signins) matches(
	request *http.Request, subject string, way secretWay,
) (bool, error) {
	secret := request.PostFormValue(way.field)
	matched, err := way.check(request.Context(), subject, secret)
	return matched && subject != "", err
}

// admitsTheRequest is auth.Begin's front door for the secret ways: a
// tenant under a base path signs nobody in, a cross-site post is
// refused, and the proof is checked before any field is read, as auth
// does, so a malformed body cannot slip past the guard's bounds.
func (signins *Signins) admitsTheRequest(
	response http.ResponseWriter, request *http.Request, way secretWay,
) bool {
	if basepath.PathRouted(request) {
		http.NotFound(response, request)
		return false
	}
	if !csrf.SameOrigin(request, signins.config.Origin) {
		http.Error(response, "cross-origin form submission refused",
			http.StatusForbidden)
		return false
	}
	if signins.config.Proof == nil {
		return true
	}
	admission := signins.config.Proof.Check(request,
		pow.Want{Scope: auth.ProofScope})
	if !admission.OK {
		signins.config.Logger.Debug("rastrillo/signin: refused at the door",
			"reason", admission.Reason, "also", admission.Also)
		signins.sendBack(response, request, way, auth.ProblemCheck, true)
	}
	return admission.OK
}

// subject is who address signs in as, "" for nobody. Lookup when the
// app gave one, because auth's own answer would create the subject.
func (signins *Signins) subject(
	ctx context.Context, address string,
) (string, error) {
	if address == "" {
		return "", nil
	}
	if signins.config.Lookup == nil {
		return signins.config.Auth.Subject(ctx, address)
	}
	subject, ok, err := signins.config.Lookup(ctx, address)
	if err != nil || !ok {
		return "", err
	}
	return subject, nil
}

// sendBack returns the visitor to the secret door they came from with
// the problem, keeping where they were going and a step-up, because
// the forms do not post the way and the query is the only place the
// others arrive. The address they typed goes in auth's sealed attempt,
// never the query. recovery asks the page for a trapless challenge.
func (signins *Signins) sendBack(
	response http.ResponseWriter, request *http.Request, way secretWay,
	problem auth.SigninProblem, recovery bool,
) {
	signins.config.Auth.NoteProblem(response,
		request.PostFormValue("address"))
	query := url.Values{"way": {string(way.way)}, "err": {string(problem)}}
	if recovery {
		query.Set("rec", "1")
	}
	to := sessions.SafeReturnTo(request.PostFormValue("return_to"), "")
	if to != "" {
		query.Set("return_to", to)
	}
	if request.PostFormValue("reauth") == "1" {
		query.Set("reauth", "1")
	}
	http.Redirect(response, request,
		signins.config.Auth.SigninPath()+"?"+query.Encode(),
		http.StatusSeeOther)
}

// failed answers an error nobody but the operator can fix.
func (signins *Signins) failed(response http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	signins.config.Logger.Error("rastrillo/signin: sign-in", "err", err)
	http.Error(response, "sign-in failed", http.StatusInternalServerError)
	return true
}

// cleanAddress trims the address and lowercases its domain, as auth's
// link sign-in does, so a code or passphrase finds the subject a link
// made.
func cleanAddress(raw string) string {
	address := strings.TrimSpace(raw)
	at := strings.LastIndex(address, "@")
	if at < 0 {
		return address
	}
	return address[:at+1] + strings.ToLower(address[at+1:])
}
