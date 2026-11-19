// Package flash provides one-shot notice messages via HTTP cookies.
//
// A flash is display state, not a record — losing one to a cleared cookie
// costs a notice, not data. Cookie-based storage is simpler than
// DB-backed, since the client carries the message through the page load
// and we clear it once read (one-shot semantics).
package flash

import (
	"encoding/base64"
	"net/http"
	"strings"

	"amadan.net/rastrillo/rastrillo/basepath"
)

const (
	cookieName = "rastrillo_flash"
	sep        = "\x00"
)

// Flash is a one-shot notice message.
type Flash struct {
	Kind    string
	Message string
}

// cookieFor is the cookie's name and path for r's app: rastrillo_flash at
// Path=/ at the root, rastrillo_flash_<slug> at Path=/<slug> under a base
// path.
//
// Both the name and the path carry the slug. The path alone keeps a
// tenant's notice from the hub and from its siblings, but the browser
// still sends the hub's root cookie to every tenant path: a tenant that
// read the same name would show the hub's notice on every page, and could
// not clear it, because a clearing cookie at /<slug> is a different
// cookie from one at /. A name of its own makes each app read and clear
// only its own.
func cookieFor(r *http.Request) (name, path string) {
	base := basepath.Base(r)
	if base == "" {
		return cookieName, "/"
	}
	return cookieName + "_" + strings.TrimPrefix(base, "/"), base
}

// Set writes a flash cookie to the response. It takes the request to know
// which app the flash belongs to: a notice set under /acme is shown by
// /acme and by no other app on the same origin.
func Set(w http.ResponseWriter, r *http.Request, kind, message string) {
	data := kind + sep + message
	encoded := base64.RawURLEncoding.EncodeToString([]byte(data))

	name, path := cookieFor(r)
	cookie := &http.Cookie{
		Name:     name,
		Value:    encoded,
		Path:     path,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   60,
	}

	http.SetCookie(w, cookie)
}

// clear writes a clearing cookie (MaxAge=-1) to the response, for the
// cookie of r's own app.
func clear(w http.ResponseWriter, r *http.Request) {
	name, path := cookieFor(r)
	cookie := &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     path,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	}
	http.SetCookie(w, cookie)
}

// Take reads r's own app's flash cookie, clears it, and returns it. It
// never reads or clears another app's: under /acme it ignores the hub's
// root cookie.
// If no flash cookie is present, it returns (Flash{}, false) and writes
// no Set-Cookie header. If the cookie is present but malformed, it still
// clears the cookie and returns (Flash{}, false).
func Take(w http.ResponseWriter, r *http.Request) (Flash, bool) {
	name, _ := cookieFor(r)
	cookie, err := r.Cookie(name)
	if err == http.ErrNoCookie {
		// No cookie, nothing to do
		return Flash{}, false
	}

	// Decode the cookie value
	data, err := base64.RawURLEncoding.DecodeString(cookie.Value)
	if err != nil {
		// Malformed cookie, clear it and return false
		clear(w, r)
		return Flash{}, false
	}

	// Split on separator (only on first occurrence to allow separator in message)
	parts := strings.SplitN(string(data), sep, 2)
	if len(parts) != 2 {
		// Malformed data, clear it and return false
		clear(w, r)
		return Flash{}, false
	}

	// Clear the cookie
	clear(w, r)

	// Return the flash
	return Flash{
		Kind:    parts[0],
		Message: parts[1],
	}, true
}
