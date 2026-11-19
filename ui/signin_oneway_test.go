package ui

import (
	"html"
	"html/template"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/auth"
	"amadan.net/rastrillo/rastrillo/lastsignin"
)

// oficinaGlyph stands in for the glyph estilo passes: inline SVG in
// currentColor, hidden from assistive technology.
const oficinaGlyph = `<svg viewBox="0 0 24 24" aria-hidden="true" ` +
	`focusable="false"><rect x="4" y="4" width="16" height="16"/></svg>`

var signinOficina = &auth.FederatedDoor{
	Provider: lastsignin.ProviderOficina, Href: "/signin/oficina",
	Label: "Oficina ID", Icon: template.HTML(oficinaGlyph)}

// oneWayState is the state an app on the one-way screen fills, with
// mut applied.
func oneWayState(mut func(*auth.SigninState)) auth.SigninState {
	st := auth.SigninState{Step: auth.StepAsk, OneWay: true,
		SigninPath: "/signin", Federated: signinOficina}
	mut(&st)
	if st.Remembered != nil && st.Step == auth.StepAsk {
		st.Step = auth.StepReturning
	}
	return st
}

// oneWayStates is every door the one-way screen draws: an app's, with
// Oficina ID, and id.*'s, with passkeys and no federated door.
func oneWayStates() []struct {
	name string
	st   auth.SigninState
} {
	oficina := &auth.Remembered{Method: lastsignin.MethodFederated,
		Provider: lastsignin.ProviderOficina, Address: "ada@example.com"}
	link := remembered("magiclink", "ada@example.com")
	kay := remembered("keymail", "kay@example.org")
	pk := remembered("passkey", "")
	idStar := func(st *auth.SigninState) {
		st.Federated, st.Passkey = nil, signinDoor
	}
	return []struct {
		name string
		st   auth.SigninState
	}{
		{"app, nothing", oneWayState(func(st *auth.SigninState) {})},
		{"app, Oficina ID", oneWayState(func(st *auth.SigninState) {
			st.Remembered = oficina
		})},
		{"app, a link", oneWayState(func(st *auth.SigninState) {
			st.Remembered = link
		})},
		{"app, Keymail", oneWayState(func(st *auth.SigninState) {
			st.Remembered = kay
		})},
		{"app, email chosen", oneWayState(func(st *auth.SigninState) {
			st.Way = auth.WayEmail
		})},
		{"app, the chooser", oneWayState(func(st *auth.SigninState) {
			st.Remembered, st.Step = oficina, auth.StepWays
		})},
		{"app, the chooser, nothing", oneWayState(func(st *auth.SigninState) {
			st.Step = auth.StepWays
		})},
		{"app, sent", oneWayState(func(st *auth.SigninState) {
			st.Remembered, st.Step = link, auth.StepSent
			st.SentTo = "ada@example.com"
		})},
		{"app, refused", oneWayState(func(st *auth.SigninState) {
			st.Remembered = link
			st.AppProblem = &auth.AppProblem{
				Text: "ada@example.com can't use Docs.", Ask: true}
		})},
		{"app, rate", oneWayState(func(st *auth.SigninState) {
			st.Remembered, st.Problem = oficina, auth.ProblemRate
		})},
		{"app, address", oneWayState(func(st *auth.SigninState) {
			st.Problem, st.Address = auth.ProblemAddress, "ada@example"
		})},
		{"app, keymail failed", oneWayState(func(st *auth.SigninState) {
			st.Remembered, st.Problem = kay, auth.ProblemKeymail
			st.Force = true
		})},
		{"app, reauth", oneWayState(func(st *auth.SigninState) {
			st.Remembered, st.Problem = oficina, auth.ProblemReauth
		})},
		{"id, nothing", oneWayState(idStar)},
		{"id, a passkey", oneWayState(func(st *auth.SigninState) {
			idStar(st)
			st.Remembered = pk
		})},
		{"id, a link", oneWayState(func(st *auth.SigninState) {
			idStar(st)
			st.Remembered = link
		})},
		{"id, passkey chosen", oneWayState(func(st *auth.SigninState) {
			idStar(st)
			st.Remembered, st.Way = link, auth.WayPasskey
		})},
		{"id, the chooser", oneWayState(func(st *auth.SigninState) {
			idStar(st)
			st.Remembered, st.Step = pk, auth.StepWays
		})},
		{"email only, Keymail", oneWayState(func(st *auth.SigninState) {
			st.Federated, st.Remembered = nil, kay
		})},
	}
}

var anchors = regexp.MustCompile(`(?s)<a\b([^>]*)>(.*?)</a>`)

// The checks every state of the classic screen passes, held for every
// one-way state too: one heading, focus where Focus says, no
// role="alert", named controls, unique ids, and every hook styled.
func TestOneWayStatesKeepTheScreensRules(t *testing.T) {
	css := string(TokensCSS())
	ids := regexp.MustCompile(`\sid="([^"]*)"`)
	for _, state := range oneWayStates() {
		out := render(t, "signin", signinData(state.st))
		if got := len(h1Pattern.FindAllString(out, -1)); got != 1 {
			t.Errorf("%s: %d headings", state.name, got)
		}
		focus := autofocusPattern.FindAllString(out, -1)
		if want := state.st.Focus(); len(focus) != min(1, len(want)) {
			t.Errorf("%s: %d autofocus, Focus says %q", state.name,
				len(focus), want)
		}
		if strings.Contains(out, `role="alert"`) {
			t.Errorf("%s renders role=alert", state.name)
		}
		for _, link := range anchors.FindAllStringSubmatch(out, -1) {
			if text(link[2]) == "" {
				t.Errorf("%s: a link with no text: %s", state.name, link[0])
			}
		}
		seen := map[string]bool{}
		for _, id := range ids.FindAllStringSubmatch(out, -1) {
			if seen[id[1]] {
				t.Errorf("%s: id %q twice", state.name, id[1])
			}
			seen[id[1]] = true
		}
		for name := range rstVocabulary(out) {
			if name != "rst-tone~=info" && !qualifiedOnly[name] &&
				!tokensStyle(css, name) {
				t.Errorf("%s: tokens.css has no selector for %q",
					state.name, name)
			}
		}
		if strings.Contains(out, "rastrillo.ui.") {
			t.Errorf("%s: an unresolved key:\n%s", state.name, out)
		}
	}
}

// One way in per screen: the door's one primary, then a quiet way on.
func TestOneWayDrawsOnePrimary(t *testing.T) {
	primary := regexp.MustCompile(`rst-btn="primary block"`)
	for _, state := range oneWayStates() {
		out := render(t, "signin", signinData(state.st))
		want := 1
		switch state.st.Door() {
		case "ways", "sent":
			want = 0
		}
		if got := len(primary.FindAllString(out, -1)); got != want {
			t.Errorf("%s (door %s): %d primaries, want %d", state.name,
				state.st.Door(), got, want)
		}
		if strings.Contains(out, "rst-signin-or") {
			t.Errorf("%s: an \"or\" between two ways on one screen",
				state.name)
		}
	}
}

// Other ways to sign in is on every door with another way; an email-only
// app shows Use a different email in its place.
func TestOtherWaysOrADifferentEmail(t *testing.T) {
	other := html.EscapeString(defaultT("rastrillo.ui.signin_ways"))
	different := html.EscapeString(defaultT("rastrillo.ui.signin_different"))
	for _, state := range oneWayStates() {
		out := render(t, "signin", signinData(state.st))
		door := state.st.Door()
		hasOther := strings.Contains(out, ">"+other+"</span>")
		wantOther := door != "ways" && door != "sent" &&
			len(state.st.Ways()) > 0
		if hasOther != wantOther {
			t.Errorf("%s: Other ways shown %v, want %v", state.name,
				hasOther, wantOther)
		}
		forgets := len(forgetForm.FindAllString(out, -1))
		wantForget := 0
		switch {
		case door == "sent", door == "ways" && state.st.Remembered != nil:
			wantForget = 1
		case !wantOther && (door == "keymail" || door == "link"):
			wantForget = 1
		}
		if forgets != wantForget {
			t.Errorf("%s: %d forget forms, want %d", state.name, forgets,
				wantForget)
		}
		if wantForget == 1 && (!strings.Contains(out, different) ||
			!strings.Contains(out, `name="way" value="email"`)) {
			t.Errorf("%s: the forget form does not open the email way:\n%s",
				state.name, out)
		}
	}
}

// Every link off the screen keeps where the visitor was going.
func TestOneWayLinksCarryReturnTo(t *testing.T) {
	for _, state := range oneWayStates() {
		st := state.st
		st.ReturnTo = "/docs/7?tab=a"
		out := render(t, "signin", signinData(st))
		for _, link := range anchors.FindAllStringSubmatch(out, -1) {
			href := regexp.MustCompile(`href="([^"]*)"`).
				FindStringSubmatch(link[1])
			if href == nil {
				continue
			}
			parsed, err := url.Parse(html.UnescapeString(href[1]))
			if err != nil ||
				parsed.Query().Get("return_to") != "/docs/7?tab=a" {
				t.Errorf("%s: %s drops return_to", state.name, href[1])
			}
		}
	}
}

func TestTheChooserListsTheOtherWays(t *testing.T) {
	byName := oneWayByName()
	out := render(t, "signin", signinData(byName["app, the chooser"]))
	for _, want := range []string{
		`<h1 id="rst-signin-heading">` +
			html.EscapeString(defaultT("rastrillo.ui.signin_ways")),
		`href="/signin?way=email"`,
		`<bdi>ada@example.com</bdi>`,
		html.EscapeString(defaultT("rastrillo.ui.signin_back")),
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the chooser lacks %s:\n%s", want, out)
		}
	}
	if strings.Contains(out, `href="/signin?way=oficina"`) {
		// The chooser was opened from the remembered Oficina ID door,
		// so it does not offer that door again.
		t.Errorf("the chooser offers the way it was opened from:\n%s", out)
	}
	title := text(render(t, "signin-title",
		signinData(byName["app, the chooser"])))
	if want := defaultTf("rastrillo.ui.signin_ways_title", "name",
		"Harbour"); title != want {
		t.Errorf("the chooser's title is %q, want %q", title, want)
	}
}

func oneWayByName() map[string]auth.SigninState {
	byName := map[string]auth.SigninState{}
	for _, state := range oneWayStates() {
		byName[state.name] = state.st
	}
	return byName
}

func TestTheFederatedDoorIsALinkWithItsGlyph(t *testing.T) {
	st := oneWayByName()["app, Oficina ID"]
	st.ReturnTo = "/docs/7"
	out := render(t, "signin", signinData(st))
	want := `<a rst-btn="primary block" ` +
		`href="/signin/oficina?return_to=%2Fdocs%2F7" autofocus>` +
		oficinaGlyph + `<span>` + html.EscapeString(defaultTf(
		"rastrillo.ui.signin_federated", "name", "Oficina ID")) +
		`</span></a>`
	if !strings.Contains(out, want) {
		t.Errorf("the federated door is not %s:\n%s", want, out)
	}
	// Never captioned with an address, though one is remembered.
	if strings.Contains(out, "ada@example.com") {
		t.Errorf("the federated door shows an address:\n%s", out)
	}
}

// No pop-in: the passkey door is drawn visible and disabled, with the
// email way to fall back to, and a line for a browser with no script.
func TestThePasskeyDoorIsVisibleBeforeItsScript(t *testing.T) {
	out := render(t, "signin", signinData(oneWayByName()["id, a passkey"]))
	button := regexp.MustCompile(`<button[^>]*data-rst-passkey[^>]*>`).
		FindString(out)
	for _, want := range []string{
		" disabled ", `data-rst-passkey-fallback="/signin?way=email"`,
		`data-rst-passkey-unavailable="`,
	} {
		if !strings.Contains(button, want) {
			t.Errorf("the passkey button lacks %s: %s", want, button)
		}
	}
	if strings.Contains(button, " hidden") {
		t.Errorf("the one-way passkey door is hidden: %s", button)
	}
	if !strings.Contains(out, "<noscript><p>"+html.EscapeString(
		defaultT("rastrillo.ui.signin_passkey_noscript"))) {
		t.Errorf("no <noscript> line:\n%s", out)
	}
	data := signinData(oneWayByName()["id, a passkey"])
	data["Preview"] = true
	preview := render(t, "signin", data)
	if strings.Contains(preview, "disabled") ||
		strings.Contains(preview, "<script") {
		t.Errorf("a Preview shows a disabled door or loads a script:\n%s",
			preview)
	}
}

func TestTheFieldOffersPasskeysInItsAutofill(t *testing.T) {
	out := render(t, "signin", signinData(oneWayByName()["id, nothing"]))
	for _, want := range []string{
		`autocomplete="username webauthn"`,
		`data-rst-passkey-conditional`,
		`<script type="module" src="/static/passkey-signin.mjs"></script>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the id.* field lacks %s:\n%s", want, out)
		}
	}
	app := render(t, "signin",
		signinData(oneWayByName()["app, email chosen"]))
	if strings.Contains(app, "webauthn") || strings.Contains(app, "<script") {
		t.Errorf("a field with no passkey door offers passkeys:\n%s", app)
	}
}

// An app's own problem is the page's problem, escaped, in both modes.
func TestAnAppProblemIsShownAndEscaped(t *testing.T) {
	problem := &auth.AppProblem{Text: `<b>ada</b> can't use Docs.`,
		Ask: true}
	classic := auth.SigninState{Step: auth.StepReturning,
		Remembered: remembered("magiclink", "ada@example.com"),
		AppProblem: problem}
	for name, st := range map[string]auth.SigninState{
		"one-way": oneWayState(func(st *auth.SigninState) {
			st.Remembered = classic.Remembered
			st.AppProblem = problem
		}),
		"classic": classic,
	} {
		out := render(t, "signin", signinData(st))
		if !strings.Contains(out, `id="rst-signin-app-problem"`) ||
			!strings.Contains(out, "&lt;b&gt;ada&lt;/b&gt;") ||
			!strings.Contains(out, `id="rst-signin-email"`) {
			t.Errorf("%s: the problem or the field is missing:\n%s",
				name, out)
		}
		if title := text(render(t, "signin-title", signinData(st))); title !=
			defaultTf("rastrillo.ui.signin_problem_title", "name",
				"Harbour") {
			t.Errorf("%s: title %q", name, title)
		}
	}
}
