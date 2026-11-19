package auth

import (
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/lastsignin"
)

// oneWay is the state an app on the one-way screen fills: Oficina ID
// as its federated door, and a passkey door when passkey is true.
func oneWay(rem *Remembered, passkey bool) SigninState {
	st := SigninState{Step: StepAsk, OneWay: true, SigninPath: "/signin",
		Remembered: rem, Federated: oficinaDoor}
	if rem != nil {
		st.Step = StepReturning
	}
	if passkey {
		st.Passkey = &PasskeyDoor{BeginPath: "/b", FinishPath: "/f"}
	}
	return st
}

var oficinaDoor = &FederatedDoor{Provider: lastsignin.ProviderOficina,
	Href: "/signin/oficina", Label: "Oficina ID"}

var (
	byLink = &Remembered{Method: lastsignin.MethodMagicLink,
		Address: "ada@example.com"}
	byKeymail = &Remembered{Method: lastsignin.MethodKeymail,
		Address: "kay@example.org"}
	byOficina = &Remembered{Method: lastsignin.MethodFederated,
		Provider: lastsignin.ProviderOficina}
	byOficinaAs = &Remembered{Method: lastsignin.MethodFederated,
		Provider: lastsignin.ProviderOficina, Address: "ada@example.com"}
	byPasskey = &Remembered{Method: lastsignin.MethodPasskey}
)

// A config New would otherwise accept, so the refusal is the option's.
func TestTheOneWayOptionNeedsTheScreen(t *testing.T) {
	cfg := Config{DB: newTestAuthDB(t), Origin: "https://app.test",
		InstanceKey: "k", ProofOff: true, Mailer: &captureMailer{}}
	if _, err := New(cfg); err != nil {
		t.Fatalf("the control config is refused: %v", err)
	}
	cfg.SigninOneWay = true
	_, err := New(cfg)
	if err == nil || !strings.Contains(err.Error(), "SigninOneWay") {
		t.Fatalf("New with SigninOneWay and no screen: %v", err)
	}
}

func TestSigninStateCarriesTheOneWayOption(t *testing.T) {
	on, _ := newScreenAuth(t, func(cfg *Config) { cfg.SigninOneWay = true })
	st := stateAt(on, newBrowser(), "/signin?ways=1&way=email")
	if !st.OneWay || st.SigninPath != "/signin" || st.Step != StepWays ||
		st.Way != WayEmail {
		t.Fatalf("on: %+v, want the chooser opened from the email way", st)
	}
	off, _ := newScreenAuth(t, nil)
	st = stateAt(off, newBrowser(), "/signin?ways=1&way=email")
	if st.OneWay || st.SigninPath != "" || st.Step != StepAsk ||
		st.Way != "" {
		t.Fatalf("off: %+v, want the query's new keys ignored", st)
	}
}

// Problems win over the chooser, and Sent and Continue have none.
func TestTheChooserNeverHidesAProblem(t *testing.T) {
	auth, _ := newScreenAuth(t, func(cfg *Config) {
		cfg.SigninOneWay = true
	})
	for query, want := range map[string]SigninStep{
		"?ways=1":                StepWays,
		"?ways=1&reauth=1":       StepWays,
		"?ways=1&err=rate":       StepAsk,
		"?ways=1&err=keymail":    StepAsk,
		"?ways=1&sent=1":         StepSent,
		"?ways=1&continue=gone":  StepAsk,
		"?ways=yes":              StepAsk,
		"?ways=1&way=elsewhere":  StepWays,
		"?ways=1&way=passkey":    StepWays,
		"?ways=1&way=oficina":    StepWays,
		"?ways=1&force=1&way=no": StepWays,
	} {
		st := stateAt(auth, newBrowser(), "/signin"+query)
		if st.Step != want {
			t.Errorf("%s → %s, want %s", query, st.Step, want)
		}
	}
}

func TestParseWayKnowsOnlyTheClosedSet(t *testing.T) {
	for value, want := range map[string]SigninWay{
		"email": WayEmail, "passkey": WayPasskey, "oficina": "oficina",
		"": "", "google": "", "EMAIL": "", "email ": "",
	} {
		if got := ParseWay(value); got != want {
			t.Errorf("ParseWay(%q) = %q, want %q", value, got, want)
		}
	}
}

// The spec's tables, "What each door shows": the one option, and what
// the chooser lists, for each way this browser last used.
func TestOneWayDoorsFollowTheSpec(t *testing.T) {
	for _, row := range []struct {
		name string
		st   SigninState
		door string
		ways []SigninWay
	}{
		{"app, nothing", oneWay(nil, false), "federated",
			[]SigninWay{WayEmail}},
		{"app, Oficina ID", oneWay(byOficina, false), "federated",
			[]SigninWay{WayEmail}},
		{"app, a link", oneWay(byLink, false), "link",
			[]SigninWay{"oficina"}},
		{"app, Keymail", oneWay(byKeymail, false), "keymail",
			[]SigninWay{"oficina"}},
		{"app, Oficina ID, email chosen",
			withWay(oneWay(byOficinaAs, false), WayEmail), "link",
			[]SigninWay{"oficina"}},
		{"app, nothing, email chosen",
			withWay(oneWay(nil, false), WayEmail), "ask",
			[]SigninWay{"oficina"}},
		{"app, no Oficina ID, a link", noFederated(oneWay(byLink, false)),
			"link", nil},
		{"app, no Oficina ID, nothing", noFederated(oneWay(nil, false)),
			"ask", nil},
		{"id, nothing", noFederated(oneWay(nil, true)), "ask",
			[]SigninWay{WayPasskey}},
		{"id, passkey", noFederated(oneWay(byPasskey, true)), "passkey",
			[]SigninWay{WayEmail}},
		{"id, a link", noFederated(oneWay(byLink, true)), "link",
			[]SigninWay{WayPasskey}},
		{"a remembered passkey, no door", oneWay(byPasskey, false),
			"ask", []SigninWay{"oficina"}},
		{"a way the page cannot draw",
			withWay(noFederated(oneWay(byLink, false)), "oficina"), "link",
			nil},
	} {
		if got := row.st.Door(); got != row.door {
			t.Errorf("%s: door %q, want %q", row.name, got, row.door)
		}
		var ways []SigninWay
		for _, way := range row.st.Ways() {
			ways = append(ways, way.Way)
		}
		if !reflect.DeepEqual(ways, row.ways) {
			t.Errorf("%s: other ways %v, want %v", row.name, ways, row.ways)
		}
	}
}

func withWay(st SigninState, way SigninWay) SigninState {
	st.Way = way
	return st
}

func noFederated(st SigninState) SigninState {
	st.Federated = nil
	return st
}

// Every problem, force, and an app's refusal draw the email door with
// the field; Sent and Continue are drawn as they always were.
func TestProblemsFallToTheEmailField(t *testing.T) {
	refused := oneWay(byLink, false)
	refused.AppProblem = &AppProblem{Text: "Not here.", Ask: true}
	noted := oneWay(byLink, false)
	noted.AppProblem = &AppProblem{Text: "Note."}
	for name, row := range map[string]struct {
		st   SigninState
		door string
	}{
		"rate":    {problem(oneWay(byOficina, false), ProblemRate), "ask"},
		"keymail": {problem(oneWay(byKeymail, false), ProblemKeymail), "ask"},
		"force":   {forced(oneWay(byOficina, false)), "ask"},
		"refused": {refused, "ask"},
		"noted":   {noted, "link"},
		"sent":    {stepped(oneWay(byOficina, false), StepSent), "sent"},
		"continue": {stepped(oneWay(byOficina, false), StepContinue),
			"continue"},
	} {
		if got := row.st.Door(); got != row.door {
			t.Errorf("%s: door %q, want %q", name, got, row.door)
		}
	}
	if refused.Focus() != "callout" || noted.Focus() != "callout" {
		t.Errorf("an app's problem does not take focus: %q, %q",
			refused.Focus(), noted.Focus())
	}
}

func problem(st SigninState, pr SigninProblem) SigninState {
	st.Problem = pr
	return st
}

func forced(st SigninState) SigninState {
	st.Force = true
	return st
}

func stepped(st SigninState, step SigninStep) SigninState {
	st.Step = step
	return st
}

// A step-up offers local ways only: a live session at the provider
// would hand back an old sign-in.
func TestReauthOffersLocalWaysOnly(t *testing.T) {
	for name, st := range map[string]SigninState{
		"remembered Oficina ID": problem(oneWay(byOficinaAs, true),
			ProblemReauth),
		"nothing remembered": problem(oneWay(nil, true), ProblemReauth),
		"Oficina ID picked": withWay(problem(oneWay(nil, true),
			ProblemReauth), "oficina"),
	} {
		if door := st.Door(); door == "federated" {
			t.Errorf("%s: a step-up offers the federated door", name)
		}
		if len(st.Ways()) == 0 {
			t.Errorf("%s: the chooser offers no local way", name)
		}
		for _, way := range st.Ways() {
			if way.Way == "oficina" {
				t.Errorf("%s: the chooser offers Oficina ID", name)
			}
		}
	}
}

func TestOneWayFocus(t *testing.T) {
	for name, row := range map[string]struct {
		st    SigninState
		focus string
	}{
		"federated":   {oneWay(nil, false), "onetap"},
		"link":        {oneWay(byLink, false), "onetap"},
		"field":       {withWay(oneWay(nil, false), WayEmail), "field"},
		"passkey":     {noFederated(oneWay(byPasskey, true)), ""},
		"the chooser": {stepped(oneWay(byLink, false), StepWays), ""},
	} {
		if got := row.st.Focus(); got != row.focus {
			t.Errorf("%s: focus %q, want %q", name, got, row.focus)
		}
	}
}

// Every link off the screen carries return_to and a step-up, the only
// place either arrives, and picks a way, never an address.
func TestTheChoosersLinksCarryTheDestination(t *testing.T) {
	st := problem(oneWay(byLink, true), ProblemReauth)
	st.ReturnTo = "/docs/7?tab=a"
	keep := url.Values{"return_to": {"/docs/7?tab=a"}, "reauth": {"1"}}
	check := func(what, href string, want url.Values) {
		t.Helper()
		parsed, err := url.Parse(href)
		if err != nil || parsed.Path != "/signin" {
			t.Fatalf("%s: %q", what, href)
		}
		for key, values := range keep {
			want[key] = values
		}
		if got := parsed.Query(); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: query %v, want %v", what, got, want)
		}
	}
	check("Other ways", st.WaysHref(),
		url.Values{"ways": {"1"}, "way": {"email"}})
	check("the email way", st.EmailHref(), url.Values{"way": {"email"}})
	for _, row := range st.Ways() {
		check("row "+string(row.Way), row.Href,
			url.Values{"way": {string(row.Way)}})
	}
	chooser := withWay(stepped(st, StepWays), WayPasskey)
	check("Back", chooser.BackHref(), url.Values{"way": {"passkey"}})
	fed := oneWay(nil, false)
	fed.ReturnTo = "/docs/7"
	want := "/signin/oficina?return_to=%2Fdocs%2F7"
	if got := fed.FederatedHref(); got != want {
		t.Errorf("FederatedHref = %q, want %q", got, want)
	}
}

// The chooser's email row shows the remembered address under its name.
func TestTheEmailRowNamesTheRememberedAddress(t *testing.T) {
	rows := oneWay(byOficinaAs, false).Ways()
	if len(rows) != 1 || rows[0].Detail != "ada@example.com" {
		t.Fatalf("rows %+v, want the email row naming Ada", rows)
	}
	rows = noFederated(oneWay(byLink, true)).Ways()
	if len(rows) != 1 || rows[0].Detail != "" {
		t.Fatalf("rows %+v, want the passkey row with no address", rows)
	}
	rows = oneWay(byLink, false).Ways()
	if len(rows) != 1 || rows[0].Label != "Oficina ID" {
		t.Fatalf("rows %+v, want the federated row with its label", rows)
	}
}

func TestConditionalUIOnlyOnTheFieldWithAPasskeyDoor(t *testing.T) {
	legacy := noFederated(oneWay(nil, true))
	legacy.Passkey.LegacyRPID = "old.example"
	off := noFederated(oneWay(nil, true))
	off.OneWay = false
	for name, row := range map[string]struct {
		st   SigninState
		want bool
	}{
		"id, nothing remembered": {noFederated(oneWay(nil, true)), true},
		"no passkey door":        {noFederated(oneWay(nil, false)), false},
		"a legacy relying party": {legacy, false},
		"the one-tap":            {noFederated(oneWay(byLink, true)), false},
		"the option off":         {off, false},
	} {
		if got := row.st.ConditionalUI(); got != row.want {
			t.Errorf("%s: ConditionalUI %v, want %v", name, got, row.want)
		}
	}
}

// "Use a different email" forgets and opens the field: on the one-way
// screen Forget lands on the email way, keeping return_to and a step-up.
// With the option off it lands where it always did.
func TestForgetOpensTheEmailWay(t *testing.T) {
	form := url.Values{"return_to": {"/docs/7"}, "way": {"email"},
		"reauth": {"1"}}
	on, _ := newScreenAuth(t, func(cfg *Config) { cfg.SigninOneWay = true })
	res := newBrowser().do(on.Forget, http.MethodPost, "/signin/forget",
		form)
	want := "/signin?reauth=1&return_to=%2Fdocs%2F7&way=email"
	if got := res.Header().Get("Location"); got != want {
		t.Errorf("one-way Forget went to %q, want %q", got, want)
	}
	off, _ := newScreenAuth(t, nil)
	res = newBrowser().do(off.Forget, http.MethodPost, "/signin/forget",
		form)
	want = "/signin?return_to=%2Fdocs%2F7"
	if got := res.Header().Get("Location"); got != want {
		t.Errorf("Forget with the option off went to %q, want %q", got, want)
	}
}

// A step-up survives an error: the error replaces the reauth problem,
// but the page still offers local ways only, and every link and the
// Begin form's post keep reauth=1.
func TestReauthSurvivesAnError(t *testing.T) {
	auth, _ := newScreenAuth(t, func(cfg *Config) { cfg.SigninOneWay = true })
	for _, query := range []string{
		"?reauth=1&err=rate", "?reauth=1&err=address",
		"?reauth=1&err=unknown", "?reauth=1&err=rate&ways=1",
	} {
		browser := newBrowser()
		rememberAs(auth, browser, lastsignin.MethodMagicLink,
			"ada@example.com")
		st := stateAt(auth, browser, "/signin"+query)
		st.Federated, st.Passkey = oficinaDoor, &PasskeyDoor{}
		if !st.Reauth || st.Door() != "ask" {
			t.Errorf("%s: Reauth %v, door %q; want a step-up on the field",
				query, st.Reauth, st.Door())
		}
		for _, row := range st.Ways() {
			if row.Way == "oficina" {
				t.Errorf("%s: the chooser offers Oficina ID", query)
			}
			if !strings.Contains(row.Href, "reauth=1") {
				t.Errorf("%s: %s drops reauth", query, row.Href)
			}
		}
		if !strings.Contains(st.WaysHref(), "reauth=1") {
			t.Errorf("%s: Other ways drops reauth", query)
		}
	}
}

// Begin's problem redirect keeps a posted step-up on the one-way
// screen, and only there.
func TestAProblemRedirectKeepsTheStepUp(t *testing.T) {
	form := url.Values{"address": {"not-an-address"}, "reauth": {"1"}}
	on, _ := newScreenAuth(t, func(cfg *Config) {
		cfg.SigninOneWay, cfg.ProofOff, cfg.Proof = true, true, nil
	})
	res := newBrowser().do(on.Begin, http.MethodPost, "/signin", form)
	if got := res.Header().Get("Location"); !strings.Contains(got,
		"reauth=1") || !strings.Contains(got, "err=address") {
		t.Errorf("one-way Begin's problem went to %q, want reauth kept", got)
	}
	off, _ := newScreenAuth(t, func(cfg *Config) {
		cfg.ProofOff, cfg.Proof = true, nil
	})
	res = newBrowser().do(off.Begin, http.MethodPost, "/signin", form)
	if got := res.Header().Get("Location"); strings.Contains(got, "reauth") {
		t.Errorf("with the option off Begin's problem went to %q", got)
	}
}

// An err the screen does not know is still a problem on the one-way
// screen: the email field, never the chooser or a remembered door.
// With the option off it is no problem at all, as it always was.
func TestAnUnknownErrorFallsToTheField(t *testing.T) {
	on, _ := newScreenAuth(t, func(cfg *Config) { cfg.SigninOneWay = true })
	for _, query := range []string{"?err=unknown", "?err=unknown&ways=1",
		"?err=x&way=oficina"} {
		browser := newBrowser()
		rememberAs(on, browser, lastsignin.MethodMagicLink,
			"ada@example.com")
		st := stateAt(on, browser, "/signin"+query)
		st.Federated = oficinaDoor
		if st.Problem != ProblemGeneric || st.Door() != "ask" {
			t.Errorf("%s: problem %q, door %q; want generic on the field",
				query, st.Problem, st.Door())
		}
	}
	off, _ := newScreenAuth(t, nil)
	if st := stateAt(off, newBrowser(), "/signin?err=unknown"); st.Problem !=
		ProblemNone {
		t.Errorf("with the option off ?err=unknown is %q", st.Problem)
	}
}

// A step-up through Keymail stays a step-up when the round trip fails:
// the callback's failure redirects restore reauth from the continuation
// bound to this round trip, so the page that follows offers local ways
// only. A link sent from the same browser in between replaces the
// attempt cookie, not the continuation, and changes nothing.
func TestAFailedKeymailStepUpStaysAStepUp(t *testing.T) {
	oneWayOn := func(cfg *Config) { cfg.SigninOneWay = true }
	for name, fail := range map[string]func(*Auth, *keymailFake){
		"err=keymail": func(_ *Auth, fake *keymailFake) {
			fake.servers["keymail.test"] = false
		},
		"err=expired": func(auth *Auth, _ *keymailFake) {
			auth.flow.Now = func() time.Time {
				return time.Now().Add(time.Hour)
			}
		},
	} {
		auth, fake := newKeymailScreen(t, oneWayOn)
		browser := newBrowser()
		begun := browser.do(auth.Begin, http.MethodPost, "/signin",
			url.Values{"address": {"kay@example.org"}, "reauth": {"1"}})
		continued := auth.SigninState(browser.request(http.MethodGet,
			begun.Header().Get("Location"), nil))
		state := stateOf(t, continued.ContinueURL)
		browser.do(auth.Begin, http.MethodPost, "/signin",
			url.Values{"address": {"ada@example.com"}})
		fail(auth, fake)
		back := callback(auth, browser, state).Header().Get("Location")
		st := stateAt(auth, browser, back)
		st.Federated = oficinaDoor
		if !strings.Contains(back, name) || !strings.Contains(back,
			"reauth=1") || !st.Reauth {
			t.Errorf("%s: the callback went to %q, which drops reauth",
				name, back)
		}
		for _, row := range st.Ways() {
			if row.Way == "oficina" {
				t.Errorf("%s: after %q the chooser offers Oficina ID",
					name, back)
			}
		}
	}
}

// Without the one-way option a failed callback answers as it always did.
func TestAFailedKeymailCallbackIsUnchangedWithTheOptionOff(t *testing.T) {
	auth, fake := newKeymailScreen(t, nil)
	browser := newBrowser()
	begun := browser.do(auth.Begin, http.MethodPost, "/signin",
		url.Values{"address": {"kay@example.org"}, "reauth": {"1"}})
	continued := auth.SigninState(browser.request(http.MethodGet,
		begun.Header().Get("Location"), nil))
	fake.servers["keymail.test"] = false
	back := callback(auth, browser, stateOf(t, continued.ContinueURL))
	got := back.Header().Get("Location")
	if got != "/signin?force=1&err=keymail" {
		t.Errorf("with the option off the callback went to %q", got)
	}
}
