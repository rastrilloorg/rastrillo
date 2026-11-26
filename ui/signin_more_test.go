package ui

import (
	"html"
	"regexp"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/auth"
	"amadan.net/rastrillo/rastrillo/signin"
)

// viewData is what an app's page hands signin-view.
func viewData(view signin.View) map[string]any {
	return map[string]any{"View": view,
		"Brand": map[string]any{"Name": "Harbour"}}
}

// escapedT is a default-catalog string as the page writes it.
func escapedT(key string) string {
	return html.EscapeString(defaultT(key))
}

var everyWay = signin.Passkey | signin.Authenticator | signin.Passphrase

func TestOfferRendersPasskeyFirstAndRecommended(t *testing.T) {
	out := render(t, "signin-view", viewData(signin.View{
		Offer: &signin.Offer{Ways: everyWay,
			LaterPath: signin.OfferLaterPath, ReturnTo: "/docs/7"},
	}))
	carry := "?return_to=%2Fdocs%2F7"
	at := func(href string) int {
		return strings.Index(out, `href="`+href+carry+`"`)
	}
	passkey, code := at(signin.AddPasskeyPath),
		at(signin.AddAuthenticatorPath)
	phrase := at(signin.AddPassphrasePath)
	if passkey < 0 || code < passkey || phrase < code {
		t.Fatalf("want passkey, authenticator, passphrase in order, "+
			"at %d, %d, %d:\n%s", passkey, code, phrase, out)
	}
	badge := strings.Index(out,
		escapedT("rastrillo.ui.signin_offer_recommended"))
	if badge < passkey || badge > code ||
		strings.Count(out, "rst-badge") != 1 {
		t.Fatalf("Recommended belongs on the passkey row alone:\n%s", out)
	}
	later := regexp.MustCompile(`(?s)<form[^>]*method="post"[^>]*action="` +
		signin.OfferLaterPath + `"[^>]*>.*?</form>`).FindString(out)
	if !strings.Contains(later, `name="return_to" value="/docs/7"`) ||
		!strings.Contains(later, `<button`) || !strings.Contains(later,
		escapedT("rastrillo.ui.signin_offer_later")) {
		t.Fatalf("Not now is a POST carrying return_to:\n%s", out)
	}
	if !strings.Contains(out, escapedT("rastrillo.ui.signin_offer_heading")) {
		t.Errorf("no heading:\n%s", out)
	}
}

func TestOfferWithoutAPasskeyHasNoRecommendation(t *testing.T) {
	out := render(t, "signin-view", viewData(signin.View{
		Offer: &signin.Offer{Ways: signin.Passphrase,
			LaterPath: signin.OfferLaterPath},
	}))
	if strings.Contains(out, "rst-badge") ||
		strings.Contains(out, signin.AddPasskeyPath) ||
		!strings.Contains(out, `href="`+signin.AddPassphrasePath+`"`) {
		t.Fatalf("a passphrase-only offer:\n%s", out)
	}
}

// addForm is the one form on an add page.
func addForm(t *testing.T, out, action string) string {
	t.Helper()
	form := regexp.MustCompile(`(?s)<form[^>]*method="post" action="` +
		action + `"[^>]*>.*?</form>`).FindString(out)
	if form == "" {
		t.Fatalf("no form posting to %s:\n%s", action, out)
	}
	return form
}

func TestAddAuthenticatorFieldIsANumericOneTimeCode(t *testing.T) {
	out := render(t, "signin-view", viewData(signin.View{
		Add: &signin.Add{Way: signin.Authenticator,
			Action: signin.AddAuthenticatorPath, ReturnTo: "/docs/7",
			URI: authenticatorURI, Key: "JBSWY3DPEHPK3PXP"},
	}))
	if !strings.Contains(out, `<svg `) ||
		!strings.Contains(out, "JBSWY3DPEHPK3PXP") {
		t.Fatalf("the QR and the key are not both shown:\n%s", out)
	}
	form := addForm(t, out, signin.AddAuthenticatorPath)
	code := inputNamed(form, "code")
	for _, attr := range []string{`inputmode="numeric"`,
		`autocomplete="one-time-code"`, `required`} {
		if !strings.Contains(code, attr) {
			t.Errorf("the code field lacks %s: %s", attr, code)
		}
	}
	if !strings.Contains(form,
		`<input type="hidden" name="return_to" value="/docs/7">`) {
		t.Errorf("the form drops return_to:\n%s", form)
	}
}

// authenticatorURI is the otpauth:// link an add page's QR code draws.
const authenticatorURI = "otpauth://totp/Harbour:ana@example.com" +
	"?secret=JBSWY3DPEHPK3PXP&issuer=Harbour"

// A screen reader says what the QR code is in the page's language, not
// in the English totp draws it with.
func TestAddAuthenticatorQRIsLabelledInThePageLanguage(t *testing.T) {
	locales, err := rastrillo.NewLocales([]string{"ga"}, "ga",
		rastrillo.BaseCatalog(), nil)
	if err != nil {
		t.Fatal(err)
	}
	irish := NewLocalizer("ga", "ga", func(key string, args ...any) string {
		return locales.Tf("ga", key, args...)
	})
	data := viewData(signin.View{Add: &signin.Add{
		Way: signin.Authenticator, Action: signin.AddAuthenticatorPath,
		URI: authenticatorURI, Key: "JBSWY3DPEHPK3PXP"}})
	data["L"] = irish
	out := render(t, "signin-view", data)
	label := locales.Tf("ga", "rastrillo.ui.signin_add_authenticator_qr")
	said := regexp.MustCompile(`<svg [^>]*role="img" aria-label="([^"]*)"`).
		FindStringSubmatch(out)
	if label == defaultT("rastrillo.ui.signin_add_authenticator_qr") ||
		len(said) < 2 || html.UnescapeString(said[1]) != label {
		t.Fatalf("the QR is not labelled in Irish (%q):\n%s", label, out)
	}
}

func TestAddAuthenticatorWrongCodeIsOnTheField(t *testing.T) {
	out := render(t, "signin-view", viewData(signin.View{
		Add: &signin.Add{Way: signin.Authenticator,
			Action: signin.AddAuthenticatorPath, Key: "JBSWY3DPEHPK3PXP",
			URI:     authenticatorURI,
			Problem: "rastrillo.ui.signin_add_authenticator_wrong"},
	}))
	code := inputNamed(out, "code")
	if !strings.Contains(code, `aria-invalid="true"`) || !strings.Contains(
		out, escapedT("rastrillo.ui.signin_add_authenticator_wrong")) {
		t.Fatalf("the wrong code is not on the field:\n%s", out)
	}
}

func TestAddPassphraseFieldIsANewPassword(t *testing.T) {
	out := render(t, "signin-view", viewData(signin.View{
		Add: &signin.Add{Way: signin.Passphrase,
			Action: signin.AddPassphrasePath, Address: "ada@example.com"},
	}))
	form := addForm(t, out, signin.AddPassphrasePath)
	secret := inputNamed(form, "passphrase")
	for _, attr := range []string{`type="password"`,
		`autocomplete="new-password"`, `minlength="15"`, `required`} {
		if !strings.Contains(secret, attr) {
			t.Errorf("the passphrase field lacks %s: %s", attr, secret)
		}
	}
	// The browser counts UTF-16 units and the server counts runes: 129
	// emoji are a valid passphrase a maxlength of 256 would refuse.
	if strings.Contains(secret, "maxlength") {
		t.Errorf("the passphrase field has a maxlength: %s", secret)
	}
	username := inputNamed(form, "username")
	for _, attr := range []string{`autocomplete="username"`,
		`value="ada@example.com"`, ` hidden`} {
		if !strings.Contains(username, attr) {
			t.Errorf("the username field lacks %s: %s", attr, username)
		}
	}
	if strings.Index(form, username) > strings.Index(form, secret) {
		t.Error("the username field must come before the passphrase")
	}
}

func TestAddPasskeyButtonCarriesTheRegisterPair(t *testing.T) {
	view := signin.View{
		Add: &signin.Add{Way: signin.Passkey, Address: "ada@example.com",
			Passkey: &auth.PasskeyDoor{
				BeginPath:  signin.RegisterBeginPath,
				FinishPath: signin.RegisterFinishPath + "?return_to=%2Fx",
				ModuleURL:  signin.WebauthnModulePath,
				ScriptURL:  signin.RegisterModulePath,
				ProbeURL:   signin.PasskeyProbePath}},
	}
	out := render(t, "signin-view", viewData(view))
	button := regexp.MustCompile(`<button[^>]*data-rst-passkey-register` +
		`[^>]*>`).FindString(out)
	for _, attr := range []string{
		`data-rst-passkey-begin="` + signin.RegisterBeginPath + `"`,
		`data-rst-passkey-finish="` + signin.RegisterFinishPath +
			`?return_to=%2Fx"`,
		`data-rst-passkey-module="` + signin.WebauthnModulePath + `"`,
		`data-rst-passkey-user="ada@example.com"`,
		`data-rst-passkey-rp-name="Harbour"`, `disabled`,
	} {
		if !strings.Contains(button, attr) {
			t.Errorf("the button lacks %s: %s", attr, button)
		}
	}
	script := `<script type="module" src="` + signin.RegisterModulePath
	if !strings.Contains(out, script) {
		t.Errorf("the register module is not loaded:\n%s", out)
	}
	// In the head, so the button is enabled in its first frame.
	head := render(t, "signin-view-head", viewData(view))
	if head != `<script src="`+signin.PasskeyProbePath+`"></script>`+"\n" {
		t.Errorf("signin-view-head is %q, want the probe", head)
	}
	view.Add.Done = true
	if head := render(t, "signin-view-head", viewData(view)); head != "" {
		t.Errorf("signin-view-head is %q once the passkey is added", head)
	}
}

func TestAddDoneContinuesToReturnTo(t *testing.T) {
	out := render(t, "signin-view", viewData(signin.View{
		Add: &signin.Add{Way: signin.Passphrase, Done: true,
			ReturnTo: "/docs/7"},
	}))
	if !strings.Contains(out, escapedT("rastrillo.ui.signin_added")) ||
		!strings.Contains(out, `href="/docs/7"`) ||
		strings.Contains(out, "<form") {
		t.Fatalf("done is a message and a Continue link:\n%s", out)
	}
}

// signin-view with no offer, add or options draws the sign-in screen
// exactly as signin does.
func TestSigninViewDrawsTheSigninScreen(t *testing.T) {
	state := auth.SigninState{Step: auth.StepAsk, BeginPath: "/signin",
		ForgetPath: "/signin/forget", Address: "grace@example.com"}
	got := render(t, "signin-view", viewData(signin.View{Signin: &state}))
	want := render(t, "signin", signinData(state))
	if got != want {
		t.Fatalf("signin-view drew\n%s\nsignin draws\n%s", got, want)
	}
}

func TestSigninViewTitleNamesEachPage(t *testing.T) {
	for key, view := range map[string]signin.View{
		"rastrillo.ui.signin_offer_heading": {Offer: &signin.Offer{}},
		"rastrillo.ui.signin_add_passphrase_heading": {
			Add: &signin.Add{Way: signin.Passphrase}},
	} {
		got := render(t, "signin-view-title", viewData(view))
		if !strings.Contains(got, escapedT(key)) {
			t.Errorf("title %q lacks %s", got, key)
		}
	}
}

// The show/hide switch is a checkbox that sends nothing, hidden until
// the reveal module runs, which is the only thing that can work it.
func TestAddPassphraseHasAShowSwitch(t *testing.T) {
	add := &signin.Add{Way: signin.Passphrase,
		Action:    signin.AddPassphrasePath,
		RevealURL: signin.RevealModulePath}
	out := render(t, "signin-view", viewData(signin.View{Add: add}))
	row := regexp.MustCompile(`(?s)<div data-rst-reveal-row hidden>` +
		`.*?</div>`).FindString(out)
	box := regexp.MustCompile(`<input type="checkbox"[^>]*>`).
		FindString(row)
	if !strings.Contains(box, `data-rst-reveal`) ||
		!strings.Contains(box, `aria-controls="rst-signin-passphrase"`) ||
		strings.Contains(box, "name=") {
		t.Fatalf("no hidden show switch for the field:\n%s", out)
	}
	if !strings.Contains(row,
		escapedT("rastrillo.ui.signin_add_passphrase_show")) {
		t.Errorf("the switch has no label:\n%s", row)
	}
	if !strings.Contains(out, `<script type="module" src="`+
		signin.RevealModulePath+`"></script>`) {
		t.Errorf("the reveal module is not loaded:\n%s", out)
	}
	preview := viewData(signin.View{Add: add})
	preview["Preview"] = true
	shown := render(t, "signin-view", preview)
	if !strings.Contains(shown, "<div data-rst-reveal-row>") ||
		strings.Contains(shown, "<script") {
		t.Errorf("a preview shows the switch and loads nothing:\n%s",
			shown)
	}
}

func TestOptionsDrawsEmailPlainAndWaysByWhatTheyHave(t *testing.T) {
	out := render(t, "signin-view", viewData(signin.View{
		Options: &signin.Options{Address: "ada@example.com",
			RemovePath: signin.OptionsRemovePath, Removed: true,
			Ways: []signin.OptionRow{
				{Way: signin.Passkey, Have: true, Label: "Ada's laptop",
					ID: "abc"},
				{Way: signin.Authenticator,
					AddPath: signin.AddAuthenticatorPath},
				{Way: signin.Passphrase, Have: true},
			}},
	}))
	for _, want := range []string{
		escapedT("rastrillo.ui.signin_options_heading"),
		escapedT("rastrillo.ui.signin_options_removed"),
		"Ada&#39;s laptop", "ada@example.com",
		`<input type="hidden" name="way" value="passkey">`,
		`<input type="hidden" name="id" value="abc">`,
		`<input type="hidden" name="way" value="passphrase">`,
		`href="` + signin.AddAuthenticatorPath + `"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	forms := strings.Count(out, `action="`+signin.OptionsRemovePath+`"`)
	if forms != 2 {
		t.Errorf("%d remove forms, want 2, none for email:\n%s", forms,
			out)
	}
	if strings.Contains(out, `value="email"`) {
		t.Errorf("email must not be removable:\n%s", out)
	}
}
