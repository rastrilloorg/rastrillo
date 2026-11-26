//go:build browser

package auth

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/harness"
	"amadan.net/rastrillo/rastrillo/lastsignin"
)

// browserOficina is the Oficina ID door the one-way drive's app draws.
var browserOficina = &FederatedDoor{Provider: lastsignin.ProviderOficina,
	Href: "/signin/oficina", Label: "Oficina ID"}

// fedAddress is the address the fake Oficina ID sign-in resolves.
const fedAddress = "fed@example.com"

// fakeOficina stands in for an app's Oficina ID round trip: where the
// app mints its session (its MintHomeSession hook), it remembers the
// federated way in with the address of the account it resolved. The
// drive needs only that record, so it goes back to the sign-in page,
// keeping return_to, rather than minting a session.
func fakeOficina(auth *Auth) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		auth.RememberJar().Remember(response, lastsignin.Record{
			Method:   lastsignin.MethodFederated,
			Provider: lastsignin.ProviderOficina, Address: fedAddress})
		to := "/signin"
		if back := request.URL.Query().Get("return_to"); back != "" {
			to += "?return_to=" + url.QueryEscape(back)
		}
		http.Redirect(response, request, to, http.StatusSeeOther)
	}
}

// oneWayRig is the screen's app with the one-way option on.
func oneWayRig(t *testing.T) (*screenApp, *harness.Rig) {
	app, build := newScreenApp(t)
	app.oneWay = true
	return app, harness.New(t, build)
}

// text is the trimmed text of the first element selector matches, or
// "" when there is none.
func textOf(t *testing.T, rig *harness.Rig, selector string) string {
	t.Helper()
	var got string
	eval(t, rig, `(document.querySelector(`+jsString(selector)+
		`)?.textContent ?? "").replace(/\s+/g, " ").trim()`, &got)
	return got
}

func jsString(value string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value) +
		`"`
}

// The drive's selectors: the controls it presses, by what they post or
// where they go.
const (
	otherWays     = `a[href*="ways=1"]`
	backLink      = `[rst-signin-door] > a:last-of-type`
	beginSubmit   = `form[action="/signin"] button[type="submit"]`
	forgetButton  = `form[action="/signin/forget"] button`
	forgetToEmail = `form[action="/signin/forget"] ` +
		`input[name="way"][value="email"] ~ button`
)

// The one-way screen, end to end: one way in per page, the chooser and
// its rows, every link keeping return_to, an emailed link remembered,
// Oficina ID remembered and never captioned with an address, and "Use a
// different email" opening the field.
func TestTheOneWayScreenInTheBrowser(t *testing.T) {
	app, rig := oneWayRig(t)
	catalog := rastrillo.BaseCatalog()
	start := rig.Origin + "/signin?return_to=%2Fabout"

	// Nothing remembered: Continue with Oficina ID is the one way in,
	// and Other ways lists Email and a passkey.
	run(t, rig, chromedp.Navigate(start),
		arrive(`a[href^="/signin/oficina"]`))
	rig.Screen("[rst-signin]", "the one-way door, nothing remembered")
	if got, want := textOf(t, rig, `a[href^="/signin/oficina"]`),
		strings.Replace(catalog["rastrillo.ui.signin_federated"], "{name}",
			"Oficina ID", 1); got != want {
		t.Fatalf("the one way in reads %q, want %q", got, want)
	}
	var count int
	eval(t, rig, `document.querySelectorAll('[rst-btn~="primary"]').length`,
		&count)
	if count != 1 {
		t.Fatalf("%d primaries on a one-way page", count)
	}
	run(t, rig, chromedp.Click(chromedp.CSS(otherWays)),
		arrive(`a[href*="way=email"]`))
	rig.Screen("[rst-signin]", "the chooser, nothing remembered")
	var hrefs []string
	eval(t, rig, `[...document.querySelectorAll("[rst-signin-door] a")]`+
		`.map(a => a.getAttribute("href"))`, &hrefs)
	if len(hrefs) != 3 {
		t.Fatalf("the chooser links to %v; want Email, a passkey, Back", hrefs)
	}
	for _, href := range hrefs {
		parsed, _ := url.Parse(href)
		if parsed.Query().Get("return_to") != "/about" {
			t.Fatalf("%s drops return_to", href)
		}
	}

	// Back returns to the door the chooser was opened from.
	run(t, rig, chromedp.Click(chromedp.CSS(backLink)),
		arrive(`a[href^="/signin/oficina"]`))

	// Email: the field, with return_to on its form, and a link sent.
	run(t, rig, chromedp.Click(chromedp.CSS(otherWays)),
		arrive(`a[href*="way=email"]`),
		chromedp.Click(chromedp.CSS(`a[href*="way=email"]`)),
		arrive("#rst-signin-email"))
	rig.Screen("[rst-signin]", "the email way")
	var returnTo string
	eval(t, rig, `document.querySelector('form[action="/signin"] `+
		`input[name="return_to"]').value`, &returnTo)
	if returnTo != "/about" {
		t.Fatalf("the email way's form posts return_to %q", returnTo)
	}
	// The link is sent from a page with no destination, so it lands at
	// home, where openLink looks for it.
	run(t, rig, chromedp.Navigate(rig.Origin+"/signin?way=email"),
		arrive("#rst-signin-email"),
		chromedp.SendKeys(chromedp.CSS("#rst-signin-email"), "ada@example.com"),
		chromedp.Click(chromedp.CSS(beginSubmit)))
	if got := awaitSent(t, rig); got != "ada@example.com" {
		t.Fatalf("Sent names %q", got)
	}
	run(t, rig, chromedp.WaitVisible(chromedp.CSS(forgetToEmail)))
	openLink(t, rig, app)
	signOut(t, rig)

	// A remembered link: Continue as Ada, and the chooser offers Oficina
	// ID, a passkey and Use a different email, which opens the field.
	run(t, rig, chromedp.Navigate(rig.Origin+"/signin"),
		arrive(`form[action="/signin"] button[autofocus]`))
	rig.Screen("[rst-signin]", "the one-way door, a remembered link")
	got := textOf(t, rig, `form[action="/signin"] button`)
	if !strings.Contains(got, "ada@example.com") {
		t.Fatalf("the remembered link's one-tap reads %q", got)
	}
	run(t, rig, chromedp.Click(chromedp.CSS(otherWays)),
		arrive(`a[href*="way=oficina"]`))
	rig.Screen("[rst-signin]", "the chooser, a remembered link")
	run(t, rig, chromedp.Click(chromedp.CSS(forgetButton)),
		arrive("#rst-signin-email"))
	var value string
	eval(t, rig, `document.getElementById("rst-signin-email").value`, &value)
	here, _ := url.Parse(read(t, rig, chromedp.Location()))
	if value != "" || here.Query().Get("way") != "email" {
		t.Fatalf("Use a different email landed on %s with %q in the field",
			here, value)
	}

	// Oficina ID, remembered with an address: its door again, and the
	// address nowhere on it.
	run(t, rig, chromedp.Navigate(rig.Origin+"/signin/oficina"),
		arrive(`a[href^="/signin/oficina"]`))
	rig.Screen("[rst-signin]", "the one-way door, Oficina ID remembered")
	door := textOf(t, rig, "[rst-signin-door]")
	if strings.Contains(door, fedAddress) {
		t.Fatalf("the Oficina ID door shows the address: %q", door)
	}
	run(t, rig, chromedp.Click(chromedp.CSS(otherWays)),
		arrive(`a[href*="way=email"]`))
	row := textOf(t, rig, `a[href*="way=email"]`)
	if !strings.Contains(row, fedAddress) {
		t.Fatalf("the chooser's Email row reads %q, want the address", row)
	}
}

// The passkey door on the one-way screen is the page's one way in. It
// is never shown late: the probe shows it, enabled, before the first
// paint; scripts off it stays hidden beside a noscript line; and in a
// browser that cannot use a passkey the page opens the email way.
func TestTheOneWayPasskeyDoorNeverPopsIn(t *testing.T) {
	_, rig := oneWayRig(t)
	door := `[data-rst-passkey]`
	way := rig.Origin + "/signin?way=passkey"

	run(t, rig, chromedp.Navigate(way), arrive(door))
	var state string
	eval(t, rig, `(b => b.disabled + " " + b.hidden)(document.`+
		`querySelector("[data-rst-passkey]"))`, &state)
	if state != "false false" {
		t.Fatalf("the door is disabled/hidden %q", state)
	}
	rig.Screen("[rst-signin]", "the passkey way, enabled")

	run(t, rig, scriptExecutionDisabled(true), chromedp.Navigate(way),
		chromedp.WaitVisible(chromedp.CSS("noscript, [rst-signin-door]")))
	eval(t, rig, `(b => b.hidden + " " + `+
		`(b.getBoundingClientRect().height > 0))(document.querySelector("`+
		`[data-rst-passkey]"))`, &state)
	if state != "true false" {
		t.Fatalf("scripts off, the door is hidden/drawn %q", state)
	}
	if line := textOf(t, rig, "noscript"); line == "" {
		t.Fatal("scripts off, no noscript line")
	}
	run(t, rig, scriptExecutionDisabled(false))

	run(t, rig, chromedp.Func(func(
		ctx context.Context, target *chromedp.Target,
	) error {
		_, err := cdp.Call(ctx, target, page.AddScriptToEvaluateOnNewDocument,
			page.AddScriptToEvaluateOnNewDocumentParams{
				Source: `delete window.PublicKeyCredential`})
		return err
	}))
	run(t, rig, chromedp.Navigate(way), arrive("#rst-signin-email"))
	here, _ := url.Parse(read(t, rig, chromedp.Location()))
	if here.Query().Get("way") != "email" {
		t.Fatalf("with no WebAuthn the passkey way stayed at %s", here)
	}
	rig.Screen("[rst-signin]", "the email way, after no passkey")
}
