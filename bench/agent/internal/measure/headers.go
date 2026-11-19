package measure

import (
	"net/http"
	"strings"
)

var securityHeaders = []string{
	"content-security-policy", "strict-transport-security",
	"x-content-type-options", "referrer-policy", "x-frame-options",
	"permissions-policy",
}

// SecurityHeaders lists the protective headers a response carries,
// lower-cased, with frame-ancestors pulled out of the CSP: it does the
// job of x-frame-options, so an app using either is protected.
func SecurityHeaders(header http.Header) map[string]string {
	found := map[string]string{}
	for _, name := range securityHeaders {
		if value := header.Get(name); value != "" {
			found[name] = value
		}
	}
	for _, directive := range strings.Split(found["content-security-policy"], ";") {
		directive = strings.TrimSpace(directive)
		if strings.HasPrefix(directive, "frame-ancestors") {
			found["frame-ancestors"] = directive
		}
	}
	return found
}
