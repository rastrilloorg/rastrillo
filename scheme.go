package rastrillo

import "net/http"

// SchemeCookie stores the reader's colour-scheme choice, "light" or
// "dark". System — let the OS decide — is no cookie at all, not a third
// value: the themes already mean exactly that by no data-theme on
// <html>, and a cookie that said "system" would be a second way to
// spell the default. It is a display preference and nothing else reads
// it: not the session, not the event log, not a handler.
//
// Unlike LocaleCookie it is not HttpOnly, on purpose. rastrillo.js
// applies a choice in place and writes this same cookie so the next
// server render paints the same scheme first; a browser refuses a
// script's write over an HttpOnly cookie, and the page would snap back
// on reload.
const SchemeCookie = "rastrillo_scheme"

// SchemeSwitchPath is the framework route the scheme switcher POSTs to.
// Mounted by Serve always: a one-language app has colours too. "theme"
// rather than "scheme" because the attribute it drives is data-theme,
// which the themes' CSS has carried since before there was a switch.
const SchemeSwitchPath = "/_theme"

// schemes is what the switch accepts. "system" is accepted and stored
// as nothing.
var schemes = map[string]bool{"light": true, "dark": true, "system": true}

// SchemeFrom is the scheme the server renders r's first frame in:
// SchemeCookie's value when it is "light" or "dark", and "" otherwise,
// for no cookie, System, or anything nothing of ours wrote. The shells
// write it as data-theme on <html>; "" is no attribute, which is the
// OS deciding.
func SchemeFrom(r *http.Request) string {
	c, err := r.Cookie(SchemeCookie)
	if err != nil || (c.Value != "light" && c.Value != "dark") {
		return ""
	}
	return c.Value
}

// SchemeSwitchHandler answers POST /_theme: it stores "light" or "dark"
// in SchemeCookie, clears it for "system", and 303s back to the return
// path. It stands behind the locale switcher's own gate and guard
// (switchPost, returnPath), so the two routes refuse the same things.
//
// The return path is used as given, locale prefix included: it is the
// page's real address (ui.ForRequest, LocalePrefix), and a colour
// change is not a reason to move the reader to another one.
func SchemeSwitchHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		https, ok := switchPost(w, r)
		if !ok {
			return
		}
		scheme := r.PostFormValue("scheme")
		if !schemes[scheme] {
			http.Error(w, "unknown scheme", http.StatusBadRequest)
			return
		}
		ret, ok := returnPath(w, r)
		if !ok {
			return
		}
		c := &http.Cookie{
			Name: SchemeCookie, Value: scheme, Path: "/",
			MaxAge:   365 * 24 * 3600,
			SameSite: http.SameSiteLaxMode, Secure: https,
		}
		if scheme == "system" {
			c.Value, c.MaxAge = "", -1
		}
		http.SetCookie(w, c)
		http.Redirect(w, r, ret, http.StatusSeeOther)
	})
}
