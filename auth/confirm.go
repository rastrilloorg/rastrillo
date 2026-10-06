package auth

import (
	"context"
	"html/template"
	"net/http"
	"strings"
)

// ConfirmPageData is what a confirm page needs to draw: the token to
// carry forward, where to post it, and the host to name in the copy.
// The token is untrusted input straight off the query string — a
// renderer must escape it (html/template does; fmt.Fprintf into a
// string does not).
type ConfirmPageData struct {
	// Token is the value from the emailed link, to be carried in a
	// hidden field named "token".
	Token string

	// Action is where the form posts — the request's own path, so an
	// app that mounted Verify somewhere other than /auth/verify still
	// gets a working form.
	Action string

	// Host is Config.Origin's host, for copy like "Sign in to
	// app.example.com".
	Host string
}

// confirmCtxKey carries the confirm page's data on the request handed
// to Config.RenderConfirm, so SigninState can answer StepConfirm there
// and nowhere else. From the context rather than the query: a sign-in
// page reached with ?token= in its URL must not grow a one-click
// sign-in button for a token somebody else chose.
type confirmCtxKey struct{}

// confirm draws the interstitial and consumes nothing. This is the
// whole defence against link-scanning gateways: Microsoft Defender's
// Safe Links, Proofpoint URL Defense, Barracuda and friends fetch every
// URL in an inbound message before the human ever sees it, and a GET
// that redeems a single-use token hands them the sign-in. They issue
// GETs, not form posts, so moving redemption to POST puts the link
// beyond their reach without any user-agent sniffing to keep current.
//
// Because the GET touches no storage, scanner traffic also costs the
// database nothing — and there is no oracle in it, since the page looks
// identical for a live token, a spent one and a fabricated one. The
// accepted cost: a link that has already been used shows the confirm
// page and only then reports ?err=expired.
func (a *Auth) confirm(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		a.redirect(w, r, a.cfg.SigninPath+"?err=expired")
		return
	}
	d := ConfirmPageData{Token: token, Action: r.URL.Path, Host: a.host()}

	// no-store keeps the token out of shared caches and the back
	// button; noindex keeps a crawler that reaches the page from filing
	// it. The framing pair is login-CSRF defence: without it an attacker
	// could iframe this page bearing a token minted for their OWN
	// address and trick a viewer into clicking "Sign in", landing them
	// in the attacker's account.
	//
	// Referrer-Policy is strict-origin because the URL is a credential.
	// The app's baseline, strict-origin-when-cross-origin, sends the
	// full URL as the Referer of every same-origin request, so an app
	// layout drawn by RenderConfirm would copy a still-redeemable token
	// into the requests for its CSS, scripts and images, and into
	// whatever logs them. strict-origin sends the origin alone. Not
	// no-referrer: under it the button's POST carries Origin: null,
	// which csrf.SameOrigin refuses in a browser that sends no
	// Sec-Fetch-Site (PrepareSigninResponse keeps it off every form
	// state for the same reason). strict-origin leaves Origin intact.
	//
	// frame-ancestors is Added, not Set. serve.go's securityHeaders has
	// already Set the app's real Content-Security-Policy, and replacing
	// it here would trade a whole policy for one directive; a second CSP
	// header is enforced alongside the first, so the page ends up with
	// both.
	h := w.Header()
	h.Set("Cache-Control", "no-store")
	h.Set("Referrer-Policy", "strict-origin")
	h.Set("X-Robots-Tag", "noindex, nofollow")
	h.Set("X-Frame-Options", "DENY")
	h.Add("Content-Security-Policy", "frame-ancestors 'none'")

	if a.cfg.RenderConfirm != nil {
		a.cfg.RenderConfirm(w, r.WithContext(context.WithValue(r.Context(), confirmCtxKey{}, d)), d)
		return
	}
	h.Set("Content-Type", "text/html; charset=utf-8")
	if err := confirmTmpl.Execute(w, d); err != nil {
		a.cfg.Logger.Error("rastrillo/auth: render confirm page", "err", err)
	}
}

// confirmFrom is the confirm page's data when r is the request
// RenderConfirm was handed.
func confirmFrom(r *http.Request) (ConfirmPageData, bool) {
	d, ok := r.Context().Value(confirmCtxKey{}).(ConfirmPageData)
	return d, ok
}

// host is Config.Origin without its scheme — the name the confirm page
// puts in front of the person, so they can see which app they are
// signing in to before they commit.
func (a *Auth) host() string {
	h := strings.TrimPrefix(a.cfg.Origin, "https://")
	return strings.TrimPrefix(h, "http://")
}

// confirmTmpl is the fallback page: self-contained, no app assets, no
// script, English only. An app with the shipped sign-in screen sets
// Config.RenderConfirm to its sign-in page, which then draws this step
// in its own layout and language; one without sets it to anything it
// likes, the same shape jobs and password use for their pages. Unlike
// those, this one has a default, because making it required would
// break every app that upgrades without noticing.
var confirmTmpl = template.Must(template.New("confirm").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex, nofollow">
<title>Sign in to {{.Host}}</title>
<style>
:root { color-scheme: light dark; }
body {
  margin: 0; min-height: 100vh; display: grid; place-items: center;
  font: 16px/1.5 system-ui, -apple-system, "Segoe UI", sans-serif;
  background: Canvas; color: CanvasText;
}
main { max-width: 26rem; padding: 2rem 1.5rem; text-align: center; }
h1 { font-size: 1.25rem; margin: 0 0 1.5rem; }
button {
  font: inherit; font-weight: 600; cursor: pointer;
  min-height: 44px; padding: .7rem 1.6rem; border: 0; border-radius: .5rem;
  background: AccentColor; color: AccentColorText;
}
</style>
</head>
<body>
<main>
<h1>Sign in to {{.Host}}</h1>
<form method="post" action="{{.Action}}">
<input type="hidden" name="token" value="{{.Token}}">
<button type="submit" autofocus>Sign in</button>
</form>
</main>
</body>
</html>
`))
