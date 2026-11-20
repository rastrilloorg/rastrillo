package instance

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"path"
	"strings"

	"amadan.net/rastrillo/rastrillo/basepath"
	"amadan.net/rastrillo/rastrillo/internal/wire"
	"amadan.net/rastrillo/rastrillo/roster"
)

var errRedirectOrigin = errors.New("rastrillo/instance: RedirectToOrigin " +
	"needs https:// and a lower-case host with no path")

// ServingOrigin is the origin the instance serves on: the custom origin
// of the last roster snapshot it applied, else fallback. Pass as
// fallback the public origin the root sends members to, such as
// https://<slug>.<parent> under a catch-all, which is https:// and
// carlos.Host() only when there is no parent. Handoff, sessions, CSRF
// and emailed links all use the origin this returns, so call it after
// migrations and before building handlers.
//
// The roster verifier's audience is the platform host, https:// and
// carlos.Host(), whatever this returns, so a custom domain that stops
// working cannot cut the root off.
func ServingOrigin(
	ctx context.Context, db *sql.DB, fallback string,
) (string, error) {
	origin, err := roster.StoredOrigin(ctx, db)
	if err != nil {
		return "", fmt.Errorf("rastrillo/instance: stored origin: %w", err)
	}
	if origin == "" {
		return fallback, nil
	}
	return origin, nil
}

// platformPaths are answered on every host: the root delivers rosters to
// the platform host under /_carlos/, and the platform checks the version
// and health on the hosts it knows.
var platformPaths = []string{"/api/version", "/healthz"}

// RedirectToOrigin sends a request on any other host to the same path
// and query on origin, except the platform's own paths: /_carlos/*,
// /api/version and /healthz. An empty origin redirects nothing.
//
// origin must be https:// and a lower-case host with no path, as the
// roster carries it, or RedirectToOrigin returns an error. In local
// development pass a real https:// origin, or "" to leave every host
// alone.
//
// The redirect is a 307 with Cache-Control: no-store, never a permanent
// one. A browser keeps a 301 or 308 and would go on sending visitors to
// a custom domain after the owner has gone back to the default address,
// perhaps because the domain stopped working. 307 also keeps the method
// and body of a POST.
func RedirectToOrigin(
	origin string, next http.Handler,
) (http.Handler, error) {
	if origin == "" {
		return next, nil
	}
	if !wire.Origin(origin) {
		return nil, errRedirectOrigin
	}
	return http.HandlerFunc(
		func(response http.ResponseWriter, request *http.Request) {
			if basepath.SameHost(request.Host, origin) ||
				platformPath(request.URL.Path) {
				next.ServeHTTP(response, request)
				return
			}
			target := origin + request.URL.EscapedPath()
			if request.URL.RawQuery != "" {
				target += "?" + request.URL.RawQuery
			}
			response.Header().Set("Cache-Control", "no-store")
			http.Redirect(response, request, target,
				http.StatusTemporaryRedirect)
		}), nil
}

// platformPath matches only a path spelt plainly, so /_carlos/../admin
// cannot reach the app on a host it no longer serves.
func platformPath(requestPath string) bool {
	if path.Clean(requestPath) != requestPath {
		return false
	}
	for _, exempt := range platformPaths {
		if requestPath == exempt {
			return true
		}
	}
	return strings.HasPrefix(requestPath, "/_carlos/")
}
