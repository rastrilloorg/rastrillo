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

// Set writes a flash cookie to the response.
func Set(w http.ResponseWriter, kind, message string) {
	data := kind + sep + message
	encoded := base64.RawURLEncoding.EncodeToString([]byte(data))

	cookie := &http.Cookie{
		Name:     cookieName,
		Value:    encoded,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   60,
	}

	http.SetCookie(w, cookie)
}

// clear writes a clearing cookie (MaxAge=-1) to the response.
func clear(w http.ResponseWriter) {
	cookie := &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	}
	http.SetCookie(w, cookie)
}

// Take reads a flash cookie from the request, clears it, and returns it.
// If no flash cookie is present, it returns (Flash{}, false) and writes
// no Set-Cookie header. If the cookie is present but malformed, it still
// clears the cookie and returns (Flash{}, false).
func Take(w http.ResponseWriter, r *http.Request) (Flash, bool) {
	cookie, err := r.Cookie(cookieName)
	if err == http.ErrNoCookie {
		// No cookie, nothing to do
		return Flash{}, false
	}

	// Decode the cookie value
	data, err := base64.RawURLEncoding.DecodeString(cookie.Value)
	if err != nil {
		// Malformed cookie, clear it and return false
		clear(w)
		return Flash{}, false
	}

	// Split on separator (only on first occurrence to allow separator in message)
	parts := strings.SplitN(string(data), sep, 2)
	if len(parts) != 2 {
		// Malformed data, clear it and return false
		clear(w)
		return Flash{}, false
	}

	// Clear the cookie
	clear(w)

	// Return the flash
	return Flash{
		Kind:    parts[0],
		Message: parts[1],
	}, true
}
