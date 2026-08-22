package csrf

import (
	"net/http"
	"net/url"
)

// SameOrigin reports whether a state-changing request came from this
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
//  2. Origin header: must equal the origin parameter exactly.
//  3. Referer: its origin must equal the origin parameter.
//  4. None of the three present: refuse. Every current browser sends
//     Sec-Fetch-Site or Origin on form POSTs; a request with neither is
//     not a browser form submission, and this is a browser-form
//     endpoint.
func SameOrigin(r *http.Request, origin string) bool {
	switch r.Header.Get("Sec-Fetch-Site") {
	case "same-origin", "none":
		return true
	case "":
		// fall through to Origin/Referer
	default:
		return false
	}
	if o := r.Header.Get("Origin"); o != "" {
		return o == origin
	}
	if ref := r.Header.Get("Referer"); ref != "" {
		u, err := url.Parse(ref)
		if err != nil {
			return false
		}
		return u.Scheme+"://"+u.Host == origin
	}
	return false
}

// Protect refuses state-changing cross-origin requests: POST, PUT,
// PATCH and DELETE must carry browser evidence of same-origin
// submission (see SameOrigin); GET/HEAD/OPTIONS pass untouched. This
// is the family's CSRF defense — origin-checking, not tokens: every
// current browser sends Sec-Fetch-Site or Origin on form POSTs, there
// is no token to mint, store, or forget to check, and it covers
// generated and hand-written handlers alike when mounted app-wide.
func Protect(origin string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
				if !SameOrigin(r, origin) {
					http.Error(w, "cross-origin form submission refused", http.StatusForbidden)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
