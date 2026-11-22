package signin

import (
	"errors"
	"net/http"
	"net/url"
	"time"
	"unicode/utf8"

	"amadan.net/rastrillo/rastrillo/passkey"
	"amadan.net/rastrillo/rastrillo/sessions"
	"amadan.net/rastrillo/rastrillo/totp"
)

// strongFor is how long a sign-in gesture is fresh enough to add a way.
const strongFor = 10 * time.Minute

// The problems an add page draws on its field, as locale keys.
const (
	problemWrongCode = "rastrillo.ui.signin_add_authenticator_wrong"
	problemShort     = "rastrillo.ui.signin_add_passphrase_short"
	problemLong      = "rastrillo.ui.signin_add_passphrase_long"
)

// The passphrase rules: NIST 800-63B's single-factor minimum, a
// ceiling that keeps hashing cheap, and nothing about composition.
const (
	passphraseMin = 15
	passphraseMax = 256
)

// strong is whether session proved who it is recently and well enough
// to change how the account is entered. Only the gesture counts, never
// when the session row was made, which a refresh moves: auth stamps
// every way's gesture but Keymail's, whose server may report none. A
// passkey used without user verification proved only possession, and a
// gesture in the future is a clock nobody can trust.
func (signins *Signins) strong(session sessions.Session) bool {
	if session.Method == passkey.MethodUnverified ||
		session.AuthTime.IsZero() {
		return false
	}
	age := signins.config.Now().Sub(session.AuthTime)
	return age >= 0 && age <= strongFor
}

// toSignin sends the browser to the sign-in page, which brings it back
// here.
func (signins *Signins) toSignin(
	response http.ResponseWriter, request *http.Request,
	reauth, force bool,
) {
	http.Redirect(response, request,
		signins.signinURL(here(request), reauth, force),
		http.StatusSeeOther)
}

// signinURL is the sign-in page returning to page. reauth asks for a
// fresh sign-in; force asks for an emailed link, because Keymail
// without an auth_time could not prove a fresh one.
func (signins *Signins) signinURL(page string, reauth, force bool) string {
	query := ""
	if reauth {
		query = "reauth=1&"
	}
	query += "return_to=" + url.QueryEscape(page)
	if force {
		query += "&force=1"
	}
	return signins.config.Auth.SigninPath() + "?" + query
}

// mustReauth is whether a signed-in session needs the emailed link
// rather than Keymail to step up: Keymail with no auth_time.
func mustReauth(session sessions.Session) bool {
	return session.Method == "keymail" && session.AuthTime.IsZero()
}

// here is the page a request came from, as a GET can reach it again:
// its path and the return_to it carried, by query or by form.
func here(request *http.Request) string {
	return request.URL.Path + carry(returnTo(request))
}

// adding is the session of a person who may add way now, or false once
// it has answered: 404 for a way that is off, else the sign-in page.
func (signins *Signins) adding(
	response http.ResponseWriter, request *http.Request, way Ways,
) (sessions.Session, bool) {
	if signins.config.Ways&way == 0 {
		http.NotFound(response, request)
		return sessions.Session{}, false
	}
	session, ok := signins.signedIn(response, request)
	if !ok {
		return session, false
	}
	if !signins.strong(session) {
		signins.toSignin(response, request, true, mustReauth(session))
		return session, false
	}
	return session, true
}

// render draws add with status. Done draws its Continue link, so it
// goes somewhere even with no return_to.
func (signins *Signins) render(
	response http.ResponseWriter, request *http.Request, status int,
	add Add,
) {
	if add.Done {
		add.ReturnTo = orHome(add.ReturnTo)
	}
	signins.config.Render(response, request, View{Status: status, Add: &add})
}

// address is what to call subject's account to a password manager or
// on a passkey. Without Lookup subjects are addresses; with it, the
// address this browser just signed in with is the best signin knows.
func (signins *Signins) address(
	request *http.Request, subject string,
) string {
	if signins.config.Lookup == nil {
		return subject
	}
	if remembered := signins.config.Auth.SigninState(request).
		Remembered; remembered != nil {
		return remembered.Address
	}
	return ""
}

// ShowAddPassphrase is GET AddPassphrasePath.
func (signins *Signins) ShowAddPassphrase(
	response http.ResponseWriter, request *http.Request,
) {
	session, ok := signins.adding(response, request, Passphrase)
	if !ok {
		return
	}
	signins.render(response, request, http.StatusOK,
		signins.passphraseAdd(request, session))
}

func (signins *Signins) passphraseAdd(
	request *http.Request, session sessions.Session,
) Add {
	return Add{Way: Passphrase, Action: AddPassphrasePath,
		ReturnTo: returnTo(request), RevealURL: RevealModulePath,
		Address: signins.address(request, session.Subject)}
}

// AddPassphrase is POST AddPassphrasePath: the rules, then the hash.
// Characters are runes, so a phrase in any script is as long as it
// looks.
func (signins *Signins) AddPassphrase(
	response http.ResponseWriter, request *http.Request,
) {
	session, ok := signins.adding(response, request, Passphrase)
	if !ok {
		return
	}
	add := signins.passphraseAdd(request, session)
	submitted := request.PostFormValue("passphrase")
	switch length := utf8.RuneCountInString(submitted); {
	case length < passphraseMin:
		add.Problem = problemShort
	case length > passphraseMax:
		add.Problem = problemLong
	}
	if add.Problem != "" {
		signins.render(response, request, http.StatusUnprocessableEntity,
			add)
		return
	}
	err := signins.keepPassphrase(request.Context(), session.Subject,
		submitted)
	if err != nil {
		signins.broke(response, "set a passphrase", err)
		return
	}
	add.Done = true
	signins.render(response, request, http.StatusOK, add)
}

// ShowAddAuthenticator is GET AddAuthenticatorPath: the QR and key of
// the secret waiting to be confirmed, begun only when none is stored,
// so two tabs show one secret and a code from either confirms it.
func (signins *Signins) ShowAddAuthenticator(
	response http.ResponseWriter, request *http.Request,
) {
	session, ok := signins.adding(response, request, Authenticator)
	if !ok {
		return
	}
	signins.authenticatorPage(response, request, session, "")
}

// AddAuthenticator is POST AddAuthenticatorPath: one code from the app
// turns the secret on.
func (signins *Signins) AddAuthenticator(
	response http.ResponseWriter, request *http.Request,
) {
	session, ok := signins.adding(response, request, Authenticator)
	if !ok {
		return
	}
	confirmed, err := signins.codes.Confirm(session.Subject,
		request.PostFormValue("code"))
	switch {
	case err != nil:
		signins.broke(response, "confirm an authenticator", err)
	case !confirmed:
		signins.authenticatorPage(response, request, session,
			problemWrongCode)
	case signins.answered(response, request, session.Subject, func() error {
		return signins.codes.Disable(session.Subject)
	}):
		signins.render(response, request, http.StatusOK, Add{
			Way: Authenticator, ReturnTo: returnTo(request), Done: true})
	}
}

// authenticatorPage draws the pending secret with problem, or Done if
// the authenticator is on already.
func (signins *Signins) authenticatorPage(
	response http.ResponseWriter, request *http.Request,
	session sessions.Session, problem string,
) {
	add := Add{Way: Authenticator, Action: AddAuthenticatorPath,
		ReturnTo: returnTo(request), Problem: problem,
		Address: signins.address(request, session.Subject)}
	enrolment, err := signins.pending(session.Subject, add.Address)
	switch {
	case errors.Is(err, totp.ErrEnrolled):
		add.Done, add.Problem = true, ""
	case err != nil:
		signins.broke(response, "begin an authenticator", err)
		return
	}
	add.URI, add.Key = enrolment.URI, enrolment.Key
	status := http.StatusOK
	if add.Problem != "" {
		status = http.StatusUnprocessableEntity
	}
	signins.render(response, request, status, add)
}

// pending is the secret waiting for subject's first code, begun now if
// there is none, in one write that keeps another tab's secret.
func (signins *Signins) pending(
	subject, account string,
) (totp.Enrolment, error) {
	if account == "" {
		account = subject
	}
	return signins.codes.PendingOrBegin(subject, account)
}
