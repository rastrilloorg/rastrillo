package signin

import (
	"context"
	"net/http"
	"strings"

	"amadan.net/rastrillo/rastrillo/action"
	"amadan.net/rastrillo/rastrillo/sessions"
)

// The offer of a faster way in next time, its "Not now", the page
// that adds each way, and the passkey enrolment pair and module the
// passkey's add page runs, and the passphrase's show/hide module.
const (
	OfferPath            = "/signin/faster"
	OfferLaterPath       = "/signin/faster/later"
	AddPasskeyPath       = "/signin/add/passkey"
	AddAuthenticatorPath = "/signin/add/authenticator"
	AddPassphrasePath    = "/signin/add/passphrase"
	OptionsPath          = "/account/signin"
	OptionsRemovePath    = "/account/signin/remove"
	RegisterBeginPath    = "/passkey/register/begin"
	RegisterFinishPath   = "/passkey/register/finish"
	RegisterModulePath   = "/signin/js/register.mjs"
	RevealModulePath     = "/signin/js/reveal.mjs"
)

// Step is a sign-up detail the app collects after the email is proved.
// The app mounts and draws Path; Needed says whether this person still
// owes it, so a closed tab resumes where it stopped.
type Step struct {
	Path   action.Path
	Needed func(ctx context.Context, subject string) (bool, error)
}

// gateExempt are the path prefixes the gate never redirects from, with
// auth's sign-in page wherever the app put it: the sign-in screen and
// its ways, the offer and enrolment, and the routes that sign people in
// or out, so the gate cannot trap anyone.
var gateExempt = []string{
	"/signin", "/auth/", "/passkey/", "/signout", "/account/signin",
}

// Gate wraps the app's handler and sends a signed-in person on a page
// navigation to the first step they still owe, then to the offer.
func (signins *Signins) Gate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(
		response http.ResponseWriter, request *http.Request,
	) {
		if !signins.gated(request) {
			next.ServeHTTP(response, request)
			return
		}
		session, ok := signins.config.Sessions.From(request)
		if !ok {
			next.ServeHTTP(response, request)
			return
		}
		to := sessions.SafeReturnTo(request.URL.RequestURI(), "/")
		where, err := signins.nextStop(request.Context(), session, to)
		switch {
		case err != nil:
			signins.neededFailed(response, err)
		case where == "":
			next.ServeHTTP(response, request)
		default:
			http.Redirect(response, request, where, http.StatusSeeOther)
		}
	})
}

// Next is what a step's page calls once it has saved: on to the next
// step still needed, else the offer, else where the person was going.
func (signins *Signins) Next(
	response http.ResponseWriter, request *http.Request,
) {
	to := sessions.SafeReturnTo(request.FormValue("return_to"), "/")
	session, ok := signins.config.Sessions.From(request)
	if !ok {
		http.Redirect(response, request, to, http.StatusSeeOther)
		return
	}
	where, err := signins.nextStop(request.Context(), session, to)
	switch {
	case err != nil:
		signins.neededFailed(response, err)
	case where == "":
		http.Redirect(response, request, to, http.StatusSeeOther)
	default:
		http.Redirect(response, request, where, http.StatusSeeOther)
	}
}

// gated is whether request is a page navigation the gate may redirect.
func (signins *Signins) gated(request *http.Request) bool {
	if request.Method != http.MethodGet {
		return false
	}
	if mode := request.Header.Get("Sec-Fetch-Mode"); mode != "" {
		if mode != "navigate" {
			return false
		}
	} else if !strings.Contains(request.Header.Get("Accept"), "text/html") {
		return false
	}
	path := request.URL.Path
	if under(path, signins.config.Auth.SigninPath()) {
		return false
	}
	for _, prefix := range gateExempt {
		if under(path, prefix) {
			return false
		}
	}
	for _, step := range signins.config.Steps {
		if path == string(step.Path) {
			return false
		}
	}
	return true
}

// under is whether path is prefix or below it, a whole segment at a
// time, so /signin exempts /signin/code but not /signing-up.
func under(path, prefix string) bool {
	prefix = strings.TrimSuffix(prefix, "/")
	return path == prefix || strings.HasPrefix(path, prefix+"/")
}

// nextStop is where session's subject has to go before reaching to: the
// first needed step, the offer if it is due, or "" when nowhere. Needed
// is asked afresh each time, so a step that saved is skipped.
func (signins *Signins) nextStop(
	ctx context.Context, session sessions.Session, to string,
) (string, error) {
	onward := carry(to)
	for _, step := range signins.config.Steps {
		needed, err := step.Needed(ctx, session.Subject)
		if err != nil {
			return "", err
		}
		if needed {
			return string(step.Path) + onward, nil
		}
	}
	due, err := signins.offerDue(ctx, session)
	if err != nil || !due {
		return "", err
	}
	return OfferPath + onward, nil
}

// neededFailed answers a failed Needed with a plain 500: letting the
// person through would skip a step the app requires.
func (signins *Signins) neededFailed(
	response http.ResponseWriter, err error,
) {
	signins.config.Logger.Error("signin: a sign-up step could not say "+
		"whether it is needed", "err", err)
	http.Error(response, "Something went wrong. Please try again.",
		http.StatusInternalServerError)
}
