package auth

import (
	"net/http"
	"net/url"
)

// sameOrigin reports whether a state-changing request came from this
// app's own pages. Baked into the package rather than left to each app
// because hand-rolled same-origin checks are where consumers actually
// shipped bugs (vitogo's refused every write, twice, before being
// fixed).
//
// The check, in order of evidence quality:
//
//  1. Sec-Fetch-Site, when the browser sent it: "same-origin" passes,
//     "none" passes (a user-typed address bar navigation), anything
//     else fails.
//  2. Origin header: must equal Config.Origin exactly.
//  3. Referer: its origin must equal Config.Origin.
//  4. None of the three present: refuse. Every current browser sends
//     Sec-Fetch-Site or Origin on form POSTs; a request with neither is
//     not a browser form submission, and this is a browser-form
//     endpoint.
func (a *Auth) sameOrigin(r *http.Request) bool {
	switch r.Header.Get("Sec-Fetch-Site") {
	case "same-origin", "none":
		return true
	case "":
		// fall through to Origin/Referer
	default:
		return false
	}
	if o := r.Header.Get("Origin"); o != "" {
		return o == a.cfg.Origin
	}
	if ref := r.Header.Get("Referer"); ref != "" {
		u, err := url.Parse(ref)
		if err != nil {
			return false
		}
		return u.Scheme+"://"+u.Host == a.cfg.Origin
	}
	return false
}
