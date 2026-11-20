package handoff

import (
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"

	"amadan.net/rastrillo/rastrillo/assertion"
	"amadan.net/rastrillo/rastrillo/basepath"
	"amadan.net/rastrillo/rastrillo/sessions"
)

const (
	nonceCookie = "__Host-carlos-enter"
	nonceMaxAge = 300
	// Browsers drop a cookie over about 4 KiB, and Consume would then
	// refuse a good assertion. Half that leaves room for the attributes.
	maxNonceCookie = 2048
)

// Enter starts a handoff: it binds a fresh nonce to this browser and sends
// it to the root, which asserts who it is and posts the answer back.
func (handoff *Handoff) Enter(
	response http.ResponseWriter, request *http.Request,
) {
	// Path tenants use the hub's session, so a handoff here would sign
	// one browser in to every tenant on the origin.
	if basepath.PathRouted(request) {
		http.NotFound(response, request)
		return
	}
	if request.Method != http.MethodGet &&
		request.Method != http.MethodHead {
		response.Header().Set("Allow", "GET, HEAD")
		http.Error(response, http.StatusText(http.StatusMethodNotAllowed),
			http.StatusMethodNotAllowed)
		return
	}
	// The root posts back to Origin, so a nonce cookie set on any other
	// host would never come back with the assertion.
	if !basepath.SameHost(request.Host, handoff.origin) {
		target := handoff.origin + EnterPath
		if request.URL.RawQuery != "" {
			target += "?" + request.URL.RawQuery
		}
		http.Redirect(response, request, target, http.StatusSeeOther)
		return
	}
	nonce, err := assertion.RandomID()
	if err != nil {
		handoff.logger.Error("handoff: nonce", "err", err)
		http.Error(response, http.StatusText(http.StatusInternalServerError),
			http.StatusInternalServerError)
		return
	}
	value, next := boundNonceCookie(nonce,
		sessions.SafeReturnTo(request.URL.Query().Get("next"), "/"))
	setNonceCookie(response, value)
	query := url.Values{
		"aud":   {handoff.origin},
		"nonce": {nonce},
		"next":  {next},
	}
	http.Redirect(response, request,
		handoff.root+"/assert?"+query.Encode(), http.StatusSeeOther)
}

func setNonceCookie(response http.ResponseWriter, value string) {
	http.SetCookie(response, &http.Cookie{
		Name:  nonceCookie,
		Value: value,
		Path:  "/",
		// None, because the root's POST back is a cross-site request.
		SameSite: http.SameSiteNoneMode,
		MaxAge:   nonceMaxAge,
		Secure:   true,
		HttpOnly: true,
	})
}

// boundNonceCookie returns the cookie value for nonce and next, and the
// next it holds: / when next would make the value too long to keep.
func boundNonceCookie(nonce, next string) (value, kept string) {
	value = encodeNonceCookie(nonce, next)
	if len(value) <= maxNonceCookie {
		return value, next
	}
	return encodeNonceCookie(nonce, "/"), "/"
}

// encodeNonceCookie joins the nonce and the page to land on, so the POST
// back finds both without a server-side pending table.
func encodeNonceCookie(nonce, next string) string {
	return nonce + "." + base64.RawURLEncoding.EncodeToString([]byte(next))
}

// decodeNonceCookie reverses encodeNonceCookie. It reports false for a
// value that is not exactly a nonce, a dot and a base64url next.
func decodeNonceCookie(value string) (nonce, next string, ok bool) {
	nonce, encoded, found := strings.Cut(value, ".")
	if !found || nonce == "" {
		return "", "", false
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil {
		return "", "", false
	}
	// Task 5 reads next only through here, so an unsafe value in a
	// forged cookie cannot become an open redirect.
	return nonce, sessions.SafeReturnTo(string(raw), "/"), true
}
