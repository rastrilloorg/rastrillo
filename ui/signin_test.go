package ui

import (
	"html"
	"html/template"
	"regexp"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/auth"
	"amadan.net/rastrillo/rastrillo/pow"
)

const keymailURL = "https://keymail.test/oauth/authorize?client_id=https%3A%2F%2Fapp.test&code_challenge=cccc&code_challenge_method=S256&redirect_uri=https%3A%2F%2Fapp.test%2Fauth%2Fcallback&scope=identify&state=ssss"

// The module URLs are absolute paths on purpose: the door's script
// import()s ModuleURL, which resolves against the script's own URL, so
// a relative one would name a file beside the script and not the page.
var signinDoor = &auth.PasskeyDoor{BeginPath: "/passkey/discover/begin", FinishPath: "/passkey/discover/finish", ModuleURL: "/static/webauthn.mjs", ScriptURL: "/static/passkey-signin.mjs"}

func signinData(st auth.SigninState) map[string]any {
	if st.BeginPath == "" {
		st.BeginPath = "/signin"
	}
	if st.ForgetPath == "" {
		st.ForgetPath = "/signin/forget"
	}
	return map[string]any{"State": st, "Brand": map[string]any{"Name": "Harbour"}}
}

func remembered(method, address string) *auth.Remembered {
	return &auth.Remembered{Method: method, Address: address}
}

// signinStates is every step × problem × remembered method the screen
// can be in, named, with the element focus must start on (§2's matrix):
// "field", "callout", "onetap" or "".
func signinStates() []struct {
	name  string
	st    auth.SigninState
	focus string
} {
	kay := remembered("keymail", "kay@example.org")
	return []struct {
		name  string
		st    auth.SigninState
		focus string
	}{
		{"ask", auth.SigninState{Step: auth.StepAsk}, "field"},
		{"ask with a passkey door", auth.SigninState{Step: auth.StepAsk, Passkey: signinDoor}, "field"},
		{"returning keymail", auth.SigninState{Step: auth.StepReturning, Remembered: kay, Address: "kay@example.org"}, "onetap"},
		{"returning magic link", auth.SigninState{Step: auth.StepReturning, Remembered: remembered("magiclink", "ada@example.com")}, "onetap"},
		{"returning passkey", auth.SigninState{Step: auth.StepReturning, Remembered: remembered("passkey", ""), Passkey: signinDoor}, ""},
		{"returning passkey, no door", auth.SigninState{Step: auth.StepReturning, Remembered: remembered("passkey", "")}, "field"},
		{"sent, bound", auth.SigninState{Step: auth.StepSent, SentTo: "ada@example.com"}, ""},
		{"sent, unbound", auth.SigninState{Step: auth.StepSent}, ""},
		{"sent instead of keymail", auth.SigninState{Step: auth.StepSent, SentTo: "kay@example.org", SentInstead: true}, ""},
		{"continue", auth.SigninState{Step: auth.StepContinue, ContinueURL: keymailURL}, ""},
		{"rate", auth.SigninState{Step: auth.StepAsk, Problem: auth.ProblemRate, Address: "ada@example.com"}, "callout"},
		{"address", auth.SigninState{Step: auth.StepAsk, Problem: auth.ProblemAddress, Address: "ada@example"}, "field"},
		{"expired", auth.SigninState{Step: auth.StepAsk, Problem: auth.ProblemExpired}, "callout"},
		{"keymail", auth.SigninState{Step: auth.StepAsk, Problem: auth.ProblemKeymail, Address: "kay@example.org"}, "callout"},
		{"generic", auth.SigninState{Step: auth.StepAsk, Problem: auth.ProblemGeneric}, "callout"},
		{"reauth, ask", auth.SigninState{Step: auth.StepAsk, Problem: auth.ProblemReauth}, "field"},
		{"reauth, returning keymail", auth.SigninState{Step: auth.StepReturning, Problem: auth.ProblemReauth, Remembered: kay}, "onetap"},
	}
}

var (
	h1Pattern        = regexp.MustCompile(`(?s)<h1 id="rst-signin-heading">(.*?)</h1>`)
	autofocusPattern = regexp.MustCompile(`<[a-z]+\b[^>]*\sautofocus[\s>][^>]*>?`)
	tagText          = regexp.MustCompile(`<[^>]*>`)
	forgetForm       = regexp.MustCompile(`<form rst-signin-form method="post" action="/signin/forget">`)
)

func text(s string) string {
	return strings.TrimSpace(html.UnescapeString(tagText.ReplaceAllString(s, "")))
}

func TestSigninHeadingAndTitleFollowTheState(t *testing.T) {
	name := "Harbour"
	for _, c := range signinStates() {
		out := render(t, "signin", signinData(c.st))
		m := h1Pattern.FindAllStringSubmatch(out, -1)
		if len(m) != 1 {
			t.Errorf("%s: %d <h1>s, want exactly one", c.name, len(m))
			continue
		}
		var heading, title string
		switch {
		case c.st.Step == auth.StepSent:
			heading, title = defaultT("rastrillo.ui.signin_sent_heading"), defaultTf("rastrillo.ui.signin_sent_title", "name", name)
		case c.st.Step == auth.StepContinue:
			heading, title = defaultT("rastrillo.ui.signin_continue_heading"), defaultTf("rastrillo.ui.signin_continue_title", "name", name)
		case c.st.Problem != auth.ProblemNone && c.st.Problem != auth.ProblemReauth:
			heading, title = defaultTf("rastrillo.ui.signin_heading", "name", name), defaultTf("rastrillo.ui.signin_problem_title", "name", name)
		default:
			heading, title = defaultTf("rastrillo.ui.signin_heading", "name", name), defaultTf("rastrillo.ui.signin_title", "name", name)
		}
		if got := text(m[0][1]); got != heading {
			t.Errorf("%s: <h1> %q, want %q", c.name, got, heading)
		}
		if got := text(render(t, "signin-title", signinData(c.st))); got != title {
			t.Errorf("%s: title %q, want %q", c.name, got, title)
		}
	}
}

func TestSigninFocusFollowsTheMatrix(t *testing.T) {
	for _, c := range signinStates() {
		out := render(t, "signin", signinData(c.st))
		tags := autofocusPattern.FindAllString(out, -1)
		if len(tags) > 1 {
			t.Errorf("%s: %d autofocus attributes; at most one element may ask for focus", c.name, len(tags))
			continue
		}
		var got string
		if len(tags) == 1 {
			switch tag := tags[0]; {
			case strings.Contains(tag, `id="rst-signin-email"`):
				got = "field"
			case strings.Contains(tag, `id="rst-signin-problem"`) && strings.Contains(tag, `tabindex="-1"`):
				got = "callout"
			case strings.HasPrefix(tag, "<button"):
				got = "onetap"
			default:
				got = "unexpected " + tag
			}
		}
		if got != c.focus {
			t.Errorf("%s: focus starts on %q, want %q", c.name, got, c.focus)
		}
	}
}

// A message present at load that focus lands on is announced by the
// focus; role="alert" would race it (§2). The only live region on the
// screen is the passkey message.
func TestSigninNeverUsesRoleAlert(t *testing.T) {
	for _, c := range signinStates() {
		if out := render(t, "signin", signinData(c.st)); strings.Contains(out, `role="alert"`) {
			t.Errorf("%s renders role=alert", c.name)
		}
	}
}

// "Use a different email" is the way out of every door that remembered
// or kept an address — the three one-taps and Sent — and nowhere else:
// on Ask the field is already the different email, and Continue has no
// controls but its link.
func TestUseADifferentEmailIsOnEveryRememberedDoor(t *testing.T) {
	for _, c := range signinStates() {
		out := render(t, "signin", signinData(c.st))
		want := 0
		switch c.st.Door() {
		case "keymail", "link", "passkey", "sent":
			want = 1
		}
		if got := len(forgetForm.FindAllString(out, -1)); got != want {
			t.Errorf("%s (door %s): %d forget forms, want %d", c.name, c.st.Door(), got, want)
		}
		if want == 1 && !strings.Contains(out, html.EscapeString(defaultT("rastrillo.ui.signin_different"))) {
			t.Errorf("%s: the forget form is not labelled Use a different email", c.name)
		}
	}
}

func TestTheOneTapNamesTheRememberedMethod(t *testing.T) {
	byName := map[string]auth.SigninState{}
	for _, c := range signinStates() {
		byName[c.name] = c.st
	}
	km := render(t, "signin", signinData(byName["returning keymail"]))
	for _, want := range []string{
		`<input type="hidden" name="address" value="kay@example.org">`,
		`<input type="hidden" name="expect" value="keymail">`,
		`aria-describedby="rst-signin-remembered"`,
		`id="rst-signin-remembered"`,
		`<bdi>kay@example.org</bdi>`,
		html.EscapeString(defaultT("rastrillo.ui.signin_to_keymail")),
	} {
		if !strings.Contains(km, want) {
			t.Errorf("keymail one-tap lacks %s:\n%s", want, km)
		}
	}

	ml := render(t, "signin", signinData(byName["returning magic link"]))
	button := regexp.MustCompile(`(?s)<button rst-btn="primary block" type="submit"[^>]*>(.*?)</button>`).FindStringSubmatch(ml)
	if button == nil || !strings.Contains(button[1], "<bdi>ada@example.com</bdi>") || strings.Contains(button[0], "aria-describedby") {
		t.Errorf("the magic-link one-tap must carry the address in its label and nothing else: %v", button)
	}
	if strings.Contains(ml, `name="expect"`) {
		t.Error("the magic-link one-tap posts expect; only the Keymail one does")
	}

	pk := render(t, "signin", signinData(byName["returning passkey"]))
	if !strings.Contains(pk, "data-rst-passkey ") || !strings.Contains(pk, html.EscapeString(defaultT("rastrillo.ui.signin_passkey_remembered"))) {
		t.Errorf("the passkey door is missing or mislabelled:\n%s", pk)
	}
	if !strings.Contains(pk, `id="rst-signin-email"`) {
		t.Error("the passkey door is on its own; the email form must be beside it (§1.6 step 6)")
	}
	// A remembered passkey is the primary way in, so its button comes
	// first; on Ask the email form is, and the passkey follows it.
	if b, f := strings.Index(pk, "data-rst-passkey "), strings.Index(pk, `id="rst-signin-email"`); b > f {
		t.Errorf("a remembered passkey's button comes after the email form:\n%s", pk)
	}
	ask := render(t, "signin", signinData(byName["ask with a passkey door"]))
	if b, f := strings.Index(ask, "data-rst-passkey "), strings.Index(ask, `id="rst-signin-email"`); b < 0 || b < f {
		t.Errorf("on Ask the passkey button must follow the email form:\n%s", ask)
	}
}

// Review Focus 2.
func TestAReturningPasskeyWithNoDoorAsksForAnAddress(t *testing.T) {
	st := auth.SigninState{Step: auth.StepReturning, Remembered: remembered("passkey", "")}
	out := render(t, "signin", signinData(st))
	if strings.Contains(out, "data-rst-passkey") || !strings.Contains(out, `id="rst-signin-email"`) {
		t.Fatalf("a remembered passkey with no door wired must be the email form:\n%s", out)
	}
	tags := autofocusPattern.FindAllString(out, -1)
	if len(tags) != 1 || !strings.Contains(tags[0], `id="rst-signin-email"`) {
		t.Errorf("focus must start on the email field, the only way in on this page: %v", tags)
	}
}

func TestTheKeymailProblemOffersTheEscapeHatch(t *testing.T) {
	for _, address := range []string{"kay@example.org", ""} {
		out := render(t, "signin", signinData(auth.SigninState{Step: auth.StepAsk, Problem: auth.ProblemKeymail, Address: address}))
		if !strings.Contains(out, `<input type="hidden" name="force" value="1">`) ||
			!strings.Contains(out, html.EscapeString(defaultT("rastrillo.ui.signin_send_link_instead"))) {
			t.Errorf("address %q: no escape hatch:\n%s", address, out)
		}
	}
	// The escape hatch appears on the Keymail problem only: anywhere else
	// force=1 would skip classification for an address nobody tried.
	for _, c := range signinStates() {
		if c.st.Problem == auth.ProblemKeymail {
			continue
		}
		if out := render(t, "signin", signinData(c.st)); strings.Contains(out, `name="force"`) {
			t.Errorf("%s posts force=1", c.name)
		}
	}
}

func TestContinueIsANavigationNotAForm(t *testing.T) {
	st := auth.SigninState{Step: auth.StepContinue, ContinueURL: keymailURL}
	out := render(t, "signin", signinData(st))
	if strings.Contains(out, "<form") {
		t.Error("Continue renders a form; it has no controls but its link")
	}
	meta := regexp.MustCompile(`<meta http-equiv="refresh" content="0;url=([^"]*)">`).FindStringSubmatch(out)
	link := regexp.MustCompile(`<a rst-btn="primary block" href="([^"]*)">`).FindStringSubmatch(out)
	if meta == nil || html.UnescapeString(meta[1]) != keymailURL {
		t.Errorf("meta refresh %v, want a zero-delay refresh to the continuation", meta)
	}
	if link == nil || html.UnescapeString(link[1]) != keymailURL {
		t.Errorf("fallback link %v, want the same URL", link)
	}

	data := signinData(st)
	data["Preview"] = true
	preview := render(t, "signin", data)
	if strings.Contains(preview, "http-equiv") || !strings.Contains(preview, `href="#"`) || strings.Contains(preview, "keymail.test") {
		t.Errorf("a Preview must not navigate anywhere:\n%s", preview)
	}
}

// ContinueURL is data, and html/template's attribute escaping is what
// keeps it inside its attribute. A URL carrying a quote — which the
// authorize-URL predicate refuses, but the partial must not rely on
// that — stays one attribute value in both places it lands.
func TestContinueURLCannotLeaveItsAttribute(t *testing.T) {
	hostile := `https://keymail.test/a" onload="alert(1)`
	out := render(t, "signin", signinData(auth.SigninState{Step: auth.StepContinue, ContinueURL: hostile}))
	// Each tag must carry exactly the attributes the partial wrote; a
	// quote that escaped would have added one.
	meta := regexp.MustCompile(`<meta http-equiv="refresh" content="([^"]*)">`).FindStringSubmatch(out)
	if meta == nil || html.UnescapeString(meta[1]) != "0;url="+hostile {
		t.Errorf("the meta refresh is not one attribute holding the URL as given: %v\n%s", meta, out)
	}
	if !regexp.MustCompile(`<a rst-btn="primary block" href="[^"]*">`).MatchString(out) {
		t.Errorf("the fallback link gained an attribute:\n%s", out)
	}
}

func TestSentNamesOnlyABoundAddress(t *testing.T) {
	bound := render(t, "signin", signinData(auth.SigninState{Step: auth.StepSent, SentTo: "ada@example.com"}))
	if !strings.Contains(bound, "<bdi>ada@example.com</bdi>") {
		t.Errorf("bound Sent does not name the address:\n%s", bound)
	}
	unbound := render(t, "signin", signinData(auth.SigninState{Step: auth.StepSent}))
	if strings.Contains(unbound, "@") || !strings.Contains(unbound, html.EscapeString(defaultT("rastrillo.ui.signin_sent_inbox"))) {
		t.Errorf("unbound Sent must say your inbox and no address:\n%s", unbound)
	}
	instead := render(t, "signin", signinData(auth.SigninState{Step: auth.StepSent, SentTo: "kay@example.org", SentInstead: true}))
	i, j := strings.Index(instead, html.EscapeString(defaultT("rastrillo.ui.signin_sent_instead"))), strings.Index(instead, "<bdi>kay@example.org</bdi>")
	if i < 0 || j < 0 || i > j {
		t.Errorf("the one-time line must come first, before where the link went:\n%s", instead)
	}
}

// Review Focus 1. What a visitor typed reaches the page in four places:
// the field's prefill, the Sent line, and the two email one-taps. In
// each it must arrive escaped, whole, exactly as typed — a {name} in it
// printed rather than substituted into — and, where it sits in running
// text, inside a <bdi> so a right-to-left address cannot reorder the
// sentence around it.
func TestSigninEscapesWhatAVisitorTyped(t *testing.T) {
	hostile := `"><script>alert(1)</script>{address}{name}مرحبا@example.com`
	hostile += strings.Repeat("a", 300-len(hostile))
	if len(hostile) != 300 {
		t.Fatalf("the hostile address is %d bytes, want 300", len(hostile))
	}
	isolated := "<bdi>" + htmlEscapeText(hostile) + "</bdi>"
	valueAttr := regexp.MustCompile(`<input\b[^>]*\sname="address"[^>]*\svalue="([^"]*)"`)
	for _, c := range []struct {
		name string
		st   auth.SigninState
		// bdi is how many times the address appears as running text; 0
		// where it is only a field's value.
		bdi int
	}{
		{"a prefill", auth.SigninState{Step: auth.StepAsk, Problem: auth.ProblemAddress, Address: hostile}, 0},
		{"a Sent address", auth.SigninState{Step: auth.StepSent, SentTo: hostile}, 1},
		{"a Keymail one-tap", auth.SigninState{Step: auth.StepReturning, Remembered: remembered("keymail", hostile)}, 1},
		{"a link one-tap", auth.SigninState{Step: auth.StepReturning, Remembered: remembered("magiclink", hostile)}, 1},
	} {
		out := render(t, "signin", signinData(c.st))
		if strings.Contains(out, "<script") {
			t.Errorf("%s: markup a visitor typed reached the page:\n%s", c.name, out)
		}
		if got := strings.Count(out, isolated); got != c.bdi {
			t.Errorf("%s: the address appears %d times escaped, whole and isolated in <bdi>, want %d:\n%s", c.name, got, c.bdi, out)
		}
		if c.bdi > 0 && strings.Count(out, "<bdi>") != c.bdi {
			t.Errorf("%s: %d <bdi>s; the {address} a visitor typed was substituted into:\n%s", c.name, strings.Count(out, "<bdi>"), out)
		}
		if c.st.Step != auth.StepSent {
			m := valueAttr.FindStringSubmatch(out)
			if m == nil || html.UnescapeString(m[1]) != hostile {
				t.Errorf("%s: the address field's value does not round-trip to what was typed: %v", c.name, m)
			}
		}
	}
	rtl := render(t, "signin", signinData(auth.SigninState{Step: auth.StepSent, SentTo: "مرحبا@example.com"}))
	if !strings.Contains(rtl, "<bdi>مرحبا@example.com</bdi>") {
		t.Errorf("an RTL address is not isolated:\n%s", rtl)
	}
}

// htmlEscapeText is how Tbdi escapes a value: template.HTMLEscapeString's
// five replacements, spelled out so the test does not call the code it
// is checking.
func htmlEscapeText(s string) string {
	return strings.NewReplacer(`&`, "&amp;", `'`, "&#39;", `<`, "&lt;", `>`, "&gt;", `"`, "&#34;").Replace(s)
}

func TestThePasskeyButtonWaitsForItsScript(t *testing.T) {
	st := auth.SigninState{Step: auth.StepAsk, Passkey: &auth.PasskeyDoor{BeginPath: "/b", FinishPath: "/f", ModuleURL: "/m.mjs", ScriptURL: "/door.mjs", LegacyRPID: "old.example"}}
	out := render(t, "signin", signinData(st))
	for _, want := range []string{
		" hidden", `data-rst-passkey-begin="/b"`, `data-rst-passkey-finish="/f"`, `data-rst-passkey-module="/m.mjs"`,
		`<script type="module" src="/door.mjs"></script>`,
		`data-rst-passkey-legacy-rpid="old.example"`, `aria-describedby="rst-signin-passkey-msg"`,
		`id="rst-signin-passkey-msg"`, `aria-live="polite"`,
		`data-rst-passkey-cancelled="` + html.EscapeString(defaultT("rastrillo.ui.signin_passkey_cancelled")) + `"`,
		`data-rst-passkey-failed="` + html.EscapeString(defaultT("rastrillo.ui.signin_passkey_failed")) + `"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the passkey door lacks %s:\n%s", want, out)
		}
	}
	data := signinData(st)
	data["Preview"] = true
	if preview := render(t, "signin", data); strings.Contains(preview, " hidden") || strings.Contains(preview, "data-rst-passkey") || strings.Contains(preview, "<script") {
		t.Errorf("a Preview shows the door as the enhanced page would, loading nothing and with nothing for a script to find:\n%s", preview)
	}
}

// The door's script is the one request the screen adds, and only a page
// that renders the door makes it: an email-only screen, a Sent or
// Continue page, a one-tap for keymail or a link, and every Preview
// load nothing. ui's shells never name it either.
func TestOnlyAPasskeyDoorLoadsItsScript(t *testing.T) {
	for _, c := range signinStates() {
		out := render(t, "signin", signinData(c.st))
		n := strings.Count(out, "<script")
		door := c.st.Passkey != nil && (c.st.Door() == "ask" || c.st.Door() == "passkey")
		switch {
		case door && (n != 1 || !strings.Contains(out, `<script type="module" src="/static/passkey-signin.mjs"></script>`)):
			t.Errorf("%s renders a passkey door and %d scripts; want exactly its module", c.name, n)
		case !door && n != 0:
			t.Errorf("%s renders no passkey door but loads %d scripts", c.name, n)
		}
		data := signinData(c.st)
		data["Preview"] = true
		if strings.Contains(render(t, "signin", data), "<script") {
			t.Errorf("%s: a Preview loads a script", c.name)
		}
	}
	for _, name := range LayoutNames() {
		src, _ := Layout(name)
		if strings.Contains(string(src), "passkey") {
			t.Errorf("layouts/%s.html names the passkey door; only the partial may load it", name)
		}
	}
}

func TestSigninControlsAreNamedAndIDsUnique(t *testing.T) {
	inputs := regexp.MustCompile(`<input\b[^>]*>`)
	buttons := regexp.MustCompile(`(?s)<button\b[^>]*>(.*?)</button>`)
	for _, c := range signinStates() {
		out := render(t, "signin", signinData(c.st))
		for _, in := range inputs.FindAllString(out, -1) {
			if strings.Contains(in, `type="hidden"`) {
				continue
			}
			id := regexp.MustCompile(`id="([^"]*)"`).FindStringSubmatch(in)
			if id == nil || !strings.Contains(out, `for="`+id[1]+`"`) {
				t.Errorf("%s: an input with no label: %s", c.name, in)
			}
		}
		for _, b := range buttons.FindAllStringSubmatch(out, -1) {
			if text(b[1]) == "" {
				t.Errorf("%s: a button with no text: %s", c.name, b[0])
			}
		}
		seen := map[string]bool{}
		for _, m := range idAttr.FindAllStringSubmatch(out, -1) {
			if seen[m[1]] {
				t.Errorf("%s: id %q twice", c.name, m[1])
			}
			seen[m[1]] = true
		}
	}
}

func TestSigninClassesAreStyled(t *testing.T) {
	css := string(TokensCSS())
	for _, c := range signinStates() {
		for name := range rstVocabulary(render(t, "signin", signinData(c.st))) {
			// The Reauth callout's tone. info is the callout's own default
			// look, painted by [rst-callout] itself, so no rule names it
			// and none should: the callout partial writes it on every
			// callout that sets no tone.
			if name == "rst-tone~=info" {
				continue
			}
			if !qualifiedOnly[name] && !tokensStyle(css, name) {
				t.Errorf("%s: tokens.css has no selector for %q", c.name, name)
			}
		}
	}
}

func TestSigninTakesABrandStruct(t *testing.T) {
	type brand struct{ Name string }
	out := render(t, "signin", map[string]any{"State": auth.SigninState{Step: auth.StepAsk, BeginPath: "/signin"}, "Brand": brand{Name: "Harbour"}})
	if !strings.Contains(out, "Harbour") {
		t.Fatalf("a Brand struct with only Name must render:\n%s", out)
	}
}

// Pitch and Mark are optional, and a caller's Mark is its own markup —
// an <img> or inline SVG — so it lands as written, in the mark slot.
func TestSigninRendersTheBrandsOptionalParts(t *testing.T) {
	mark := `<svg viewBox="0 0 24 24" aria-hidden="true" focusable="false"><circle cx="12" cy="12" r="9"/></svg>`
	data := signinData(auth.SigninState{Step: auth.StepAsk})
	data["Brand"] = map[string]any{"Name": "Harbour", "Pitch": "Moorings & berths", "Mark": template.HTML(mark)}
	out := render(t, "signin", data)
	for _, want := range []string{
		`<div rst-signin-mark>` + mark + `</div>`,
		`<p rst-signin-name>Harbour</p>`,
		`<p rst-signin-pitch>Moorings &amp; berths</p>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the brand column lacks %s:\n%s", want, out)
		}
	}
	bare := render(t, "signin", signinData(auth.SigninState{Step: auth.StepAsk}))
	if strings.Contains(bare, "rst-signin-mark") || strings.Contains(bare, "rst-signin-pitch") {
		t.Errorf("a Brand with no Mark or Pitch leaves empty slots:\n%s", bare)
	}
}

// [rst-btn] is an inline-flex box with a gap, so a label whose text and
// <bdi> sit directly in the button are two flex items, and the gap
// between them reads as a double space before the address. The label
// must be one element, and its text must carry exactly one space there.
func TestTheLinkOneTapLabelIsOneRunOfText(t *testing.T) {
	out := render(t, "signin", signinData(auth.SigninState{Step: auth.StepReturning, Remembered: remembered("magiclink", "grace@example.com")}))
	button := regexp.MustCompile(`(?s)<button rst-btn="primary block" type="submit"[^>]*>(.*?)</button>`).FindStringSubmatch(out)
	if button == nil {
		t.Fatalf("no one-tap button:\n%s", out)
	}
	if !regexp.MustCompile(`^<span>[^<]*<bdi>[^<]*</bdi>[^<]*</span>$`).MatchString(button[1]) {
		t.Errorf("the one-tap's label is not a single <span> run, so the button's flex gap splits it: %q", button[1])
	}
	label := text(button[1])
	if strings.Contains(label, "  ") || label != strings.TrimSpace(defaultTf("rastrillo.ui.signin_continue_as", "address", "grace@example.com")) {
		t.Errorf("the one-tap reads %q, want %q with single spaces", label, defaultTf("rastrillo.ui.signin_continue_as", "address", "grace@example.com"))
	}
}

// The passkey message is empty until the door's script writes to it,
// and an empty paragraph directly in the door would still take one of
// the door's gaps. The button and its message are one child of the door.
func TestThePasskeyButtonAndItsMessageAreOneDoorChild(t *testing.T) {
	for _, preview := range []bool{false, true} {
		for _, st := range []auth.SigninState{
			{Step: auth.StepAsk, Passkey: signinDoor},
			{Step: auth.StepReturning, Remembered: remembered("passkey", ""), Passkey: signinDoor},
		} {
			data := signinData(st)
			if preview {
				data["Preview"] = true
			}
			out := render(t, "signin", data)
			if !regexp.MustCompile(`<div rst-signin-passkey><button [^>]*>[^<]*</button>\s*<p rst-signin-passkey-msg id="rst-signin-passkey-msg" aria-live="polite"></p>\s*</div>`).MatchString(out) {
				t.Errorf("door %s, preview %v: the passkey button and message are not wrapped together:\n%s", st.Door(), preview, out)
			}
		}
	}
}

// An app that mounts auth somewhere else must get a screen that posts
// there: every form action is BeginPath or ForgetPath, and the forget
// form is the only one posting to ForgetPath.
func TestEveryFormPostsWhereTheAppMountedAuth(t *testing.T) {
	action := regexp.MustCompile(`<form\b[^>]*\saction="([^"]*)"`)
	for _, c := range signinStates() {
		st := c.st
		st.BeginPath, st.ForgetPath = "/account/begin", "/account/forget"
		out := render(t, "signin", signinData(st))
		forms := action.FindAllStringSubmatch(out, -1)
		if st.Door() != "continue" && len(forms) == 0 {
			t.Errorf("%s: no form at all", c.name)
		}
		forget := 0
		for _, f := range forms {
			switch f[1] {
			case st.BeginPath:
			case st.ForgetPath:
				forget++
			default:
				t.Errorf("%s: a form posts to %q, neither BeginPath nor ForgetPath", c.name, f[1])
			}
		}
		if forget != strings.Count(out, html.EscapeString(defaultT("rastrillo.ui.signin_different"))) {
			t.Errorf("%s: %d forms post to ForgetPath but the page has a different number of Use a different email buttons", c.name, forget)
		}
	}
}

// orDivider is the "or" between the email form and the passkey door,
// in the words of the catalog: visible text, read in order, and nothing
// that tells a screen reader to skip it or read it as a rule.
var orDivider = regexp.MustCompile(`<p rst-signin-or>([^<]*)</p>`)

// "or" only means something with two ways in beside it. A screen with
// no passkey door — email only, a one-tap, Sent, Continue, a remembered
// passkey the app has not wired — has nothing to set the form against,
// and a divider there points at a button that is not on the page.
func TestTheOrDividerStandsOnlyBetweenTheFormAndAPasskeyDoor(t *testing.T) {
	for _, c := range signinStates() {
		for _, preview := range []bool{false, true} {
			data := signinData(c.st)
			if preview {
				data["Preview"] = true
			}
			out := render(t, "signin", data)
			door := c.st.Passkey != nil && (c.st.Door() == "ask" || c.st.Door() == "passkey")
			m := orDivider.FindAllStringSubmatch(out, -1)
			if !door {
				if len(m) != 0 || strings.Contains(out, "rst-signin-or") {
					t.Errorf("%s, preview %v: no passkey door, but the screen says or:\n%s", c.name, preview, out)
				}
				continue
			}
			if len(m) != 1 {
				t.Errorf("%s, preview %v: %d or dividers, want one:\n%s", c.name, preview, len(m), out)
				continue
			}
			if got, want := m[0][1], html.EscapeString(defaultT("rastrillo.ui.signin_or")); got != want {
				t.Errorf("%s, preview %v: the divider reads %q, want %q", c.name, preview, got, want)
			}
			or, form, pk := strings.Index(out, "<p rst-signin-or>"), strings.Index(out, `id="rst-signin-email"`), strings.Index(out, "<div rst-signin-passkey>")
			if (or < form) == (or < pk) {
				t.Errorf("%s, preview %v: the divider is not between the email form and the passkey door:\n%s", c.name, preview, out)
			}
		}
	}
}

// With the passkey button hidden — no script yet, scripts off, or a
// browser with no WebAuthn — the page offers one way in, and an "or"
// beside it would dangle. tokens.css hides the divider with the pair,
// through the wrapper's own rule, which reaches the divider only as the
// wrapper's immediate neighbour: anything written between them would
// leave the divider showing beside a hidden button.
func TestTheOrDividerIsHiddenWithThePasskeyButton(t *testing.T) {
	adjacent := regexp.MustCompile(`<p rst-signin-or>[^<]*</p>\s*<div rst-signin-passkey>|<div rst-signin-passkey>(?s:.*?)</div>\s*<p rst-signin-or>`)
	for _, st := range []auth.SigninState{
		{Step: auth.StepAsk, Passkey: signinDoor},
		{Step: auth.StepReturning, Remembered: remembered("passkey", ""), Passkey: signinDoor},
	} {
		if out := render(t, "signin", signinData(st)); !adjacent.MatchString(out) {
			t.Errorf("door %s: the divider is not the passkey wrapper's immediate neighbour:\n%s", st.Door(), out)
		}
	}
	css := string(TokensCSS())
	for _, sel := range []string{
		`[rst-signin-passkey]:has(> [rst-btn][hidden]) + [rst-signin-or]`,
		`[rst-signin-or]:has(+ [rst-signin-passkey] > [rst-btn][hidden])`,
	} {
		if !cssRuleSays(css, sel, "display: none") {
			t.Errorf("tokens.css has no %s { display: none } rule; the divider shows beside a hidden passkey button", sel)
		}
	}
}

// cssRuleSays reports whether some rule in css lists selector among its
// selectors and declares decl.
func cssRuleSays(css, selector, decl string) bool {
	for _, rule := range regexp.MustCompile(`(?s)([^{}]*)\{([^{}]*)\}`).FindAllStringSubmatch(stripCSSComments(css), -1) {
		if !strings.Contains(rule[2], decl) {
			continue
		}
		for _, sel := range splitSelectorList(rule[1]) {
			if collapseSpace(sel) == selector {
				return true
			}
		}
	}
	return false
}

func proofGuard(t *testing.T, difficulty int) *pow.Guard {
	t.Helper()
	g, err := pow.New(pow.Config{InstanceKey: "ui", Nonces: pow.MemoryNonces(), Difficulty: difficulty,
		ScriptURL: "/pow/pow.js", WorkerURL: "/pow/pow-worker.js"})
	if err != nil {
		t.Fatal(err)
	}
	return g
}

var (
	beginForm  = regexp.MustCompile(`(?s)<form rst-signin-form method="post" action="/signin"[^>]*>.*?</form>`)
	forgetFull = regexp.MustCompile(`(?s)<form rst-signin-form method="post" action="/signin/forget">.*?</form>`)
)

func TestSigninRendersTheChallengeOnTheOneBeginForm(t *testing.T) {
	g := proofGuard(t, 12)
	for _, c := range signinStates() {
		st := c.st
		f := g.Form(time.Now(), auth.ProofScope)
		st.Proof = &f
		out := render(t, "signin", signinData(st))
		forms := beginForm.FindAllString(out, -1)
		switch st.Door() {
		case "sent", "continue":
			if len(forms) != 0 || strings.Contains(out, "pow_seal") {
				t.Errorf("%s: a %s page carries a challenge", c.name, st.Door())
			}
			continue
		}
		if len(forms) != 1 {
			t.Fatalf("%s: %d Begin forms, want exactly 1", c.name, len(forms))
		}
		form := forms[0]
		for _, want := range []string{`data-pow-form`, `name="pow_seal"`, `data-pow-submit`, `disabled`, `data-pow-status`, `<noscript>`} {
			if !strings.Contains(form, want) {
				t.Errorf("%s: Begin form lacks %s", c.name, want)
			}
		}
		if strings.Count(out, `<script type="module" src="/pow/pow.js">`) != 1 {
			t.Errorf("%s: pow.js is not loaded exactly once", c.name)
		}
		if forget := forgetFull.FindString(out); strings.Contains(forget, "pow_") {
			t.Errorf("%s: the Forget form carries a challenge", c.name)
		}
	}
}

func TestSigninNoProofRendersAnEnabledSubmit(t *testing.T) {
	g := proofGuard(t, pow.NoProof)
	f := g.Form(time.Now(), auth.ProofScope)
	out := render(t, "signin", signinData(auth.SigninState{Step: auth.StepAsk, Proof: &f}))
	form := beginForm.FindString(out)
	if strings.Contains(form, "disabled") || strings.Contains(out, "pow.js") {
		t.Fatal("a NoProof sign-in form renders a disabled submit or loads a module it does not need")
	}
	if !strings.Contains(form, `name="pow_seal"`) {
		t.Fatal("a NoProof sign-in form lost its token")
	}
}

func TestSigninForceComesFromState(t *testing.T) {
	g := proofGuard(t, 12)
	f := g.Recovery(time.Now(), auth.ProofScope)
	out := render(t, "signin", signinData(auth.SigninState{Step: auth.StepAsk, Problem: auth.ProblemCheck, Force: true, Proof: &f}))
	if !strings.Contains(beginForm.FindString(out), `name="force" value="1"`) {
		t.Fatal("a check refusal after a keymail failure dropped the send-a-link-instead choice")
	}
}
