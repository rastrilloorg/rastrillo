package auth

import (
	"net/http"
	"net/url"
	"sync"

	"amadan.net/rastrillo/rastrillo/lastsignin"
)

// SigninStep is which screen the sign-in page shows. A string, not an
// int, because ui's signin partial compares it by name.
type SigninStep string

const (
	StepAsk       SigninStep = "ask"
	StepReturning SigninStep = "returning"
	StepSent      SigninStep = "sent"
	StepContinue  SigninStep = "continue"
)

// SigninProblem is what went wrong on the way to the page, if anything.
// Each value is also the suffix of its catalog key
// (rastrillo.ui.signin_problem_<value>), so a problem added here without
// its string fails the partial's unresolved-key check.
type SigninProblem string

const (
	ProblemNone    SigninProblem = ""
	ProblemRate    SigninProblem = "rate"
	ProblemAddress SigninProblem = "address"
	ProblemExpired SigninProblem = "expired"
	ProblemKeymail SigninProblem = "keymail"
	ProblemGeneric SigninProblem = "generic"
	// ProblemReauth is not an error: RequireFreshSession wants a fresh
	// sign-in, and the page says so above whichever door it would
	// otherwise show.
	ProblemReauth SigninProblem = "reauth"
)

// SigninState is everything ui's signin partial needs, as plain data.
// Build it with SigninState, set Passkey if the app mounted passkey
// discovery, call PrepareSigninResponse, then render.
type SigninState struct {
	Step    SigninStep
	Problem SigninProblem
	// Address prefills the email field: this browser's latest attempt's
	// address, else the remembered one. Never from the query.
	Address string
	// SentTo is the address this attempt's link went to, on Sent only,
	// and only when the page's attempt= matches this browser's attempt
	// cookie; otherwise the page says "your inbox" and guesses nothing.
	SentTo string
	// SentInstead: a link went out where the remembered-Keymail one-tap
	// promised Keymail. Bound like SentTo, and false whenever it is "".
	SentInstead bool
	// Remembered is this browser's remembered way in; nil when nothing
	// valid is remembered or remembering is off.
	Remembered *Remembered
	// ContinueURL is the keymail authorize URL, on Continue only. It
	// comes from the continuation cookie, never the query, and has
	// passed the authorize-URL predicate on the way out. It is data, not
	// markup: a renderer must emit it through html/template's attribute
	// escaping (the partial puts it in a meta refresh and an href), never
	// as template.URL or template.HTML, or a URL that somehow carried a
	// quote would become an attribute of the renderer's choosing.
	ContinueURL string
	BeginPath   string
	ForgetPath  string
	// Passkey is set by the app, which knows whether and where it
	// mounted passkey discovery; auth cannot. Nil means no passkey door.
	Passkey *PasskeyDoor

	// clear names cookies SigninState found present and unbelievable:
	// how the read-only half tells PrepareSigninResponse what to delete.
	clear []string
}

// Remembered is the way in this browser used last. Address is "" for a
// passkey.
type Remembered struct {
	Method  string
	Address string
}

// PasskeyDoor is where the app mounted passkey discovery (BeginPath,
// FinishPath), where it serves webauthn.JS() (ModuleURL, the WebAuthn
// helper) and passkey.JS() (ScriptURL, the door's own script, which the
// partial loads only when it renders the door), and
// passkey.Config.LegacyRPID, if any.
type PasskeyDoor struct {
	BeginPath, FinishPath string
	// ModuleURL must be an absolute path ("/static/webauthn.mjs"), not a
	// relative one. The door's script import()s it, and import() resolves
	// "./x.mjs" against the importing module's own URL, not the page's,
	// and refuses "static/x.mjs" as a bare specifier: either way the
	// import fails and the button stays hidden on every page, with no
	// error anyone sees.
	ModuleURL  string
	ScriptURL  string
	LegacyRPID string
}

// advisoryOnce makes the screen-off advisory fire once per process, as
// the spec and the docs say — package-level rather than a field on Auth,
// because a process that built two Auths would otherwise warn twice. A
// pointer so a test can replace it with a fresh one and restore it.
var advisoryOnce = new(sync.Once)

// advisoryText is worded as a condition rather than a diagnosis on
// purpose: auth cannot see whether the app widened form-action or wraps
// Begin itself as fichas does, so it must never say the app is wrong.
const advisoryText = "rastrillo/auth: SigninScreen is off: under the default CSP, with no continuation of your own, " +
	"a keymail address cannot leave the sign-in form, and nothing is remembered"

// SigninState reads the query and this browser's four cookies (attempt,
// continuation, pending, remembered) and returns what the page shows.
// It writes nothing and consults nothing else — no database, no
// classifier, no Authorize, no mailer — which is why the screen adds no
// membership oracle: every state is a function of what this browser
// already holds.
//
// With SigninScreen off it reads only the query, marks nothing, and
// logs the advisory once per process.
func (a *Auth) SigninState(r *http.Request) SigninState {
	st := SigninState{BeginPath: a.cfg.BeginPath, ForgetPath: a.cfg.ForgetPath}
	q := r.URL.Query()
	if !a.cfg.SigninScreen {
		advisoryOnce.Do(func() { a.cfg.Logger.Warn(advisoryText) })
		st.Step, st.Problem = outcome(q, false)
		return st
	}

	rec, res := a.jar.Read(r)
	switch res {
	case lastsignin.Valid:
		st.Remembered = &Remembered{Method: rec.Method, Address: rec.Address}
	case lastsignin.Invalid:
		st.clear = append(st.clear, a.jar.CookieName())
	}
	at, atState := a.openAttempt(r)
	if atState == cookieInvalid {
		st.clear = append(st.clear, a.attemptCookie())
	}
	if _, cs := a.openContinuation(r); cs == cookieInvalid {
		st.clear = append(st.clear, a.continueCookie())
	}

	if q.Has("continue") {
		if u, ok := a.continuationFor(r, q.Get("continue")); ok {
			st.Step, st.ContinueURL = StepContinue, u
			return st
		}
	}
	st.Step, st.Problem = outcome(q, st.Remembered != nil)

	// A prefill is the visitor's own input and stays editable, so the
	// latest attempt wins even if another tab made it.
	switch {
	case atState == cookieValid:
		st.Address = at.A
	case st.Remembered != nil:
		st.Address = st.Remembered.Address
	}
	if st.Step == StepSent && atState == cookieValid && at.K == attemptLink &&
		at.ID != "" && at.ID == q.Get("attempt") {
		st.SentTo, st.SentInstead = at.A, at.A != "" && at.X
	}
	return st
}

// outcome maps the query to a step and a problem, in §1.2's order: the
// first row that applies wins. A ?continue= that did not resolve is
// Expired; an unknown err is no problem at all.
func outcome(q url.Values, remembered bool) (SigninStep, SigninProblem) {
	if q.Has("continue") {
		return StepAsk, ProblemExpired
	}
	if q.Get("sent") == "1" {
		return StepSent, ProblemNone
	}
	switch q.Get("err") {
	case "rate":
		return StepAsk, ProblemRate
	case "address":
		return StepAsk, ProblemAddress
	case "expired":
		return StepAsk, ProblemExpired
	case "keymail":
		return StepAsk, ProblemKeymail
	case "1":
		return StepAsk, ProblemGeneric
	}
	step := StepAsk
	if remembered {
		step = StepReturning
	}
	if q.Get("reauth") == "1" {
		return step, ProblemReauth
	}
	return step, ProblemNone
}

// PrepareSigninResponse is the only writer: the app calls it before
// rendering any state.
//
// no-store on every state, because the Returning and Sent pages carry an
// address and a shared cache or the back-forward history keeps a page,
// not a cookie. no-referrer on Continue only: that page navigates to a
// URL carrying state and the PKCE challenge. Not on the others, on
// purpose — under no-referrer a form POST sends Origin: null, and a
// browser without Sec-Fetch-Site would then fail csrf.SameOrigin, so
// every state with a form keeps the app's policy.
func (a *Auth) PrepareSigninResponse(w http.ResponseWriter, st SigninState) {
	h := w.Header()
	h.Set("Cache-Control", "no-store")
	if st.Step == StepContinue {
		h.Set("Referrer-Policy", "no-referrer")
	}
	for _, name := range st.clear {
		a.clearCookie(w, name)
	}
}

// Forget is POST at Config.ForgetPath — "Use a different email": it
// forgets the remembered way in and ends the attempt. POST only, because
// a state change on GET is one a link prefetcher can make; same-origin
// only, as Begin is.
func (a *Auth) Forget(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !a.sameOrigin(r) {
		http.Error(w, "cross-origin form submission refused", http.StatusForbidden)
		return
	}
	a.jar.Clear(w)
	a.jar.EndAttempt(w)
	a.redirect(w, r, a.cfg.SigninPath)
}

// Door is which way in the page offers, derived here rather than in the
// template so the rules are Go and tested: "sent" and "continue" for
// those steps; "ask" for any error problem, so the visitor can correct
// what they typed; for Returning, the remembered method's one-tap —
// "keymail", "link", or "passkey" when the app wired a passkey door —
// and "ask" otherwise.
func (s SigninState) Door() string {
	switch s.Step {
	case StepSent:
		return "sent"
	case StepContinue:
		return "continue"
	}
	if s.Problem != ProblemNone && s.Problem != ProblemReauth {
		return "ask"
	}
	if s.Step == StepReturning && s.Remembered != nil {
		switch s.Remembered.Method {
		case lastsignin.MethodKeymail:
			return "keymail"
		case lastsignin.MethodMagicLink:
			return "link"
		case lastsignin.MethodPasskey:
			// A remembered passkey with no door wired is not a dead end:
			// it is the email form.
			if s.Passkey != nil {
				return "passkey"
			}
		}
	}
	return "ask"
}

// Focus is where focus starts on an ordinary load (§2's matrix): the
// email field for an address problem, whose error it carries; the
// callout for any other error; the one-tap for a keymail or link
// Returning door; the email field for Ask; nothing for the passkey door,
// Sent and Continue, whose new title is the announcement.
func (s SigninState) Focus() string {
	switch {
	case s.Problem == ProblemAddress:
		return "field"
	case s.Problem != ProblemNone && s.Problem != ProblemReauth:
		return "callout"
	}
	switch s.Door() {
	case "keymail", "link":
		return "onetap"
	case "ask":
		return "field"
	}
	return ""
}
