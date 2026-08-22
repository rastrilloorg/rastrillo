package auth

import (
	"net/http"

	"github.com/carlosframework/rastrillo/csrf"
)

// sameOrigin is a pointer to the CSRF package's SameOrigin check,
// delegating the full implementation and its proven test coverage
// to the csrf package.
func (a *Auth) sameOrigin(r *http.Request) bool {
	return csrf.SameOrigin(r, a.cfg.Origin)
}
