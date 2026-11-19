package auth

import (
	"html/template"
	"net/http"
	"net/url"
	"strings"

	"amadan.net/rastrillo/rastrillo/lastsignin"
)

// The one-way screen (Config.SigninOneWay) offers one way in per page:
// the way this browser used last, then "Other ways to sign in", a page
// of its own. Its rules live here and in Door, as Go, so they are
// tested; the partial only draws what they say. The design is the
// smart-door spec in Home, docs/superpowers/specs/2026-11-19-smart-door.md.

// StepWays is the chooser, "Other ways to sign in": ?ways=1 on the
// one-way screen.
const StepWays SigninStep = "ways"

// SigninWay is a way in the visitor can pick from the chooser, carried
// as ?way=. It picks a method, never an address: the address only ever
// comes from this browser's own cookies or the field.
type SigninWay string

const (
	WayEmail   SigninWay = "email"
	WayPasskey SigninWay = "passkey"
)

// A federated way is its provider's name, one lastsignin.Provider
// accepts: ?way=oficina.

// FederatedDoor is a sign-in another service runs, which the page
// supplies as it supplies Passkey: auth knows nothing of where the app
// mounted it.
type FederatedDoor struct {
	// Provider is the lastsignin provider this door's sign-in is
	// remembered under (lastsignin.ProviderOficina), and the ?way= that
	// picks it.
	Provider string
	// Href is where the button goes, by GET. The screen adds the page's
	// return_to to it.
	Href string
	// Label is the provider's name, "Oficina ID": the button reads
	// "Continue with" it, and the chooser's row reads it alone.
	Label string
	// Icon is optional: the provider's glyph as inline SVG drawn in
	// currentColor and marked aria-hidden, which lands as written.
	Icon template.HTML
}

// AppProblem is a problem the app found, which auth cannot name: an app
// that refuses a verified sign-in (Config.Refused) redirects back to the
// screen and sets this from its own query. Text is shown as the page's
// problem; Ask draws the email door with the field instead of the
// remembered one-tap, which would only offer the refused address again.
type AppProblem struct {
	Text string
	Ask  bool
}

// SigninWayRow is one row of the chooser: the way it opens, the page it
// opens, and the address the email row shows under its name.
type SigninWayRow struct {
	Way    SigninWay
	Href   string
	Detail string
	// Label and Icon are the federated door's, on its row only.
	Label string
	Icon  template.HTML
}

// ChooseWay reads the one-way screen's query: ?ways=1 for the chooser,
// ?way= for one way's own page, and ?reauth=1, which is kept apart from
// Problem because an error replaces the reauth problem and a step-up
// must still offer local ways only. An err the screen does not know is
// the generic problem here, so it too falls to the email field. It sets
// nothing unless OneWay is set, and never turns a problem, Sent or
// Continue into the chooser: problems are drawn on the email door, and
// those steps have no chooser. Home, which fills a SigninState without
// auth, calls it too.
func (st *SigninState) ChooseWay(query url.Values) {
	if !st.OneWay {
		return
	}
	st.Reauth = st.Problem == ProblemReauth || query.Get("reauth") == "1"
	asking := st.Step == StepAsk || st.Step == StepReturning
	unknown := st.Problem == ProblemNone || st.Problem == ProblemReauth
	if asking && unknown && query.Get("err") != "" {
		st.Step, st.Problem = StepAsk, ProblemGeneric
	}
	st.Way = ParseWay(query.Get("way"))
	quiet := st.Problem == ProblemNone || st.Problem == ProblemReauth
	if query.Get("ways") == "1" && quiet && asking {
		st.Step = StepWays
	}
}

// ParseWay returns the way a ?way= value names, or "" for anything
// else.
func ParseWay(value string) SigninWay {
	switch way := SigninWay(value); {
	case way == WayEmail, way == WayPasskey:
		return way
	case lastsignin.Provider(value):
		return way
	}
	return ""
}

// offers reports whether this page can draw way. Reauth offers local
// ways only: a live session at the provider would hand back an old
// sign-in, and a step-up needs a fresh one here.
func (st SigninState) offers(way SigninWay) bool {
	switch way {
	case WayEmail:
		return true
	case WayPasskey:
		return st.Passkey != nil
	}
	return st.Federated != nil && !st.reauth() &&
		string(way) == st.Federated.Provider
}

// reauth is whether this page is a step-up: the Reauth ChooseWay read,
// or the reauth problem on a state filled without it.
func (st SigninState) reauth() bool {
	return st.Reauth || st.Problem == ProblemReauth
}

// rememberedWay is the way the page offers when the visitor picked
// none: the one this browser used last, if the page still has it;
// with nothing remembered, the federated door if there is one.
func (st SigninState) rememberedWay() SigninWay {
	fed := SigninWay("")
	if st.Federated != nil {
		fed = SigninWay(st.Federated.Provider)
	}
	rem := st.Remembered
	switch {
	case rem == nil && st.offers(fed):
		return fed
	case rem == nil:
		return WayEmail
	case rem.Method == lastsignin.MethodFederated &&
		st.offers(SigninWay(rem.Provider)):
		return SigninWay(rem.Provider)
	case rem.Method == lastsignin.MethodPasskey && st.offers(WayPasskey):
		return WayPasskey
	}
	return WayEmail
}

// currentWay is the way this page is about: the one picked, when the
// page can draw it, else the remembered one.
func (st SigninState) currentWay() SigninWay {
	if st.Way != "" && st.offers(st.Way) {
		return st.Way
	}
	return st.rememberedWay()
}

// oneWayDoor is Door for the one-way screen, below the steps and
// problems Door has already answered. Force is a problem too: it is a
// failed Keymail coming back for the send-a-link-instead field.
func (st SigninState) oneWayDoor() string {
	switch {
	case st.Force:
		return "ask"
	case st.Step == StepWays:
		return "ways"
	}
	switch st.currentWay() {
	case WayEmail:
		return st.emailDoor()
	case WayPasskey:
		return "passkey"
	}
	return "federated"
}

// emailDoor is the email way: the remembered address's one-tap, which
// Begin classifies afresh, or the field when no address is remembered.
// A remembered Keymail keeps its own one-tap; any other record with an
// address gets "Continue as", which promises neither Keymail nor a
// link.
func (st SigninState) emailDoor() string {
	rem := st.Remembered
	switch {
	case rem == nil || rem.Address == "":
		return "ask"
	case rem.Method == lastsignin.MethodKeymail:
		return "keymail"
	}
	return "link"
}

// shownWay is the way the drawn door belongs to, which the chooser
// leaves out and Back returns to.
func (st SigninState) shownWay() SigninWay {
	switch st.Door() {
	case "passkey":
		return WayPasskey
	case "federated", "ways":
		return st.currentWay()
	}
	return WayEmail
}

// Ways is the chooser's rows, every way but the one shown, in a fixed
// order: the federated door, email, a passkey. "Use a different email"
// is not a row here: it posts to Forget, and the partial draws it last
// whenever something is remembered. Nil off the one-way screen.
func (st SigninState) Ways() []SigninWayRow {
	if !st.OneWay || st.Unavailable {
		return nil
	}
	shown := st.shownWay()
	var rows []SigninWayRow
	if fed := st.Federated; fed != nil {
		way := SigninWay(fed.Provider)
		if way != shown && st.offers(way) {
			rows = append(rows, SigninWayRow{Way: way,
				Href:  st.signinHref("way", string(way)),
				Label: fed.Label, Icon: fed.Icon})
		}
	}
	if shown != WayEmail {
		rows = append(rows, SigninWayRow{Way: WayEmail,
			Href: st.signinHref("way", string(WayEmail)), Detail: st.address()})
	}
	if shown != WayPasskey && st.offers(WayPasskey) {
		rows = append(rows, SigninWayRow{Way: WayPasskey,
			Href: st.signinHref("way", string(WayPasskey))})
	}
	return rows
}

// address is the remembered address, "" when there is none.
func (st SigninState) address() string {
	if st.Remembered == nil {
		return ""
	}
	return st.Remembered.Address
}

// WaysHref is "Other ways to sign in": the chooser, told which way it
// was opened from so it leaves that one out and Back returns to it.
func (st SigninState) WaysHref() string {
	return st.signinHref("ways", "1", "way", string(st.shownWay()))
}

// BackHref is the chooser's Back: the way it was opened from.
func (st SigninState) BackHref() string {
	if st.Way == "" {
		return st.signinHref()
	}
	return st.signinHref("way", string(st.Way))
}

// EmailHref is the email way's own page, where the passkey door sends
// a browser that cannot use a passkey.
func (st SigninState) EmailHref() string {
	return st.signinHref("way", string(WayEmail))
}

// FederatedHref is the federated door's Href with this page's return_to
// on it, set rather than appended so the page's wins.
func (st SigninState) FederatedHref() string {
	if st.Federated == nil {
		return ""
	}
	return withReturnTo(st.Federated.Href, st.ReturnTo)
}

// signinHref is the sign-in page with pairs as its query, carrying
// return_to and a reauth on every link, because the query is the only
// place either arrives and a chooser that dropped them would land a
// step-up as an ordinary sign-in somewhere else.
func (st SigninState) signinHref(pairs ...string) string {
	query := url.Values{}
	for at := 0; at+1 < len(pairs); at += 2 {
		query.Set(pairs[at], pairs[at+1])
	}
	if st.ReturnTo != "" {
		query.Set("return_to", st.ReturnTo)
	}
	if st.reauth() {
		query.Set("reauth", "1")
	}
	if len(query) == 0 {
		return st.SigninPath
	}
	return st.SigninPath + "?" + query.Encode()
}

// withReturnTo sets return_to on href's query, or returns href as it is
// when there is none to set or it does not parse.
func withReturnTo(href, returnTo string) string {
	parsed, err := url.Parse(href)
	if returnTo == "" || err != nil {
		return href
	}
	query := parsed.Query()
	query.Set("return_to", returnTo)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

// ConditionalUI is whether the email field offers saved passkeys in its
// autofill: on the one-way screen's field, with a passkey door, and
// never where LegacyRPID is set, because a conditional request cannot
// fall back to a second relying party the way the button's can.
func (st SigninState) ConditionalUI() bool {
	return st.OneWay && st.Passkey != nil &&
		st.Passkey.LegacyRPID == "" && st.Door() == "ask"
}

// remembered reads this browser's remembered way in, marking a cookie
// that cannot be believed for PrepareSigninResponse to clear.
func (auth *Auth) remembered(
	request *http.Request, st *SigninState,
) *Remembered {
	rec, res := auth.jar.Read(request)
	switch res {
	case lastsignin.Valid:
		return &Remembered{Method: rec.Method, Address: rec.Address,
			Provider: rec.Provider}
	case lastsignin.Invalid:
		st.clear = append(st.clear, auth.jar.CookieName())
	}
	return nil
}

// oneWayPath is SigninPath for the one-way screen's links, and "" off
// it, so a state with the option off is the state it always was.
func (auth *Auth) oneWayPath() string {
	if !auth.cfg.SigninOneWay {
		return ""
	}
	return auth.cfg.SigninPath
}

// afterForget is where Forget sends the browser: the sign-in page with
// the posted return_to, since a different address is still going to the
// same page. On the one-way screen it opens the email way too, because
// "Use a different email" promises the field, and with nothing
// remembered the page would otherwise offer the federated door.
func (auth *Auth) afterForget(request *http.Request) string {
	query := url.Values{}
	if dest := postedReturnTo(request); dest != "" {
		query.Set("return_to", dest)
	}
	if auth.cfg.SigninOneWay {
		if ParseWay(request.PostFormValue("way")) == WayEmail {
			query.Set("way", string(WayEmail))
		}
		if request.PostFormValue("reauth") == "1" {
			query.Set("reauth", "1")
		}
	}
	if len(query) == 0 {
		return auth.cfg.SigninPath
	}
	return auth.cfg.SigninPath + "?" + query.Encode()
}

// keepReauth carries a posted step-up onto the sign-in page a one-way
// form's post is sent back to, so a problem (a rate limit, a bad
// address) does not turn a step-up into an ordinary sign-in that offers
// the federated door. Every other redirect is returned as it is.
func (auth *Auth) keepReauth(request *http.Request, to string) string {
	prefix := auth.cfg.SigninPath + "?"
	if !auth.cfg.SigninOneWay || request.Method != http.MethodPost ||
		request.PostFormValue("reauth") != "1" ||
		!strings.HasPrefix(to, prefix) {
		return to
	}
	query, err := url.ParseQuery(strings.TrimPrefix(to, prefix))
	if err != nil || query.Has("reauth") {
		return to
	}
	query.Set("reauth", "1")
	return prefix + query.Encode()
}
