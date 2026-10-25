package csrf

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
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

// Option configures Protect.
type Option func(*options)

type options struct {
	exempt map[string]bool
}

// Exempt names paths that Protect lets through without browser
// evidence. It is for a path a machine reaches, such as a payment
// provider's webhook or a carlos schedule tick: those POSTs come from a
// server, which sends no Sec-Fetch-Site, Origin or Referer, so Protect
// would refuse every one.
//
// An exempt path is not checked by this middleware at all.
// Its handler must check its own credential (a webhook
// signature, carlos.Tick) before it does anything, and refuse the
// request when the check fails. A handler that skips the check is open
// to any site on the internet.
//
// A path matches only when it equals r.URL.Path exactly: "/hook" does
// not exempt "/hook/", "/HOOK" or "/hook/x". Exempting by prefix would
// quietly cover routes added later under it. Exempt panics on a path
// that is empty or does not start with "/", because such a path can
// never match and the mistake should stop the app at boot, not leave
// the webhook answering 403 in production.
func Exempt(paths ...string) Option {
	for _, p := range paths {
		if !strings.HasPrefix(p, "/") {
			panic(fmt.Sprintf("csrf.Exempt: path %q must start with \"/\"", p))
		}
	}
	return func(o *options) {
		for _, p := range paths {
			o.exempt[p] = true
		}
	}
}

// Protect refuses state-changing cross-origin requests: POST, PUT,
// PATCH and DELETE must carry browser evidence of same-origin
// submission (see SameOrigin); GET/HEAD/OPTIONS pass untouched. This
// is the family's CSRF defense — origin-checking, not tokens: every
// current browser sends Sec-Fetch-Site or Origin on form POSTs, there
// is no token to mint, store, or forget to check, and it covers
// generated and hand-written handlers alike when mounted app-wide.
//
// Options name paths to leave unchecked; see Exempt.
func Protect(origin string, opts ...Option) func(http.Handler) http.Handler {
	// Built once here, not per request, so a bad option fails at boot
	// and the request path does one map lookup.
	o := options{exempt: map[string]bool{}}
	for _, opt := range opts {
		opt(&o)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
				if !o.exempt[r.URL.Path] && !SameOrigin(r, origin) {
					http.Error(w, "cross-origin form submission refused", http.StatusForbidden)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
