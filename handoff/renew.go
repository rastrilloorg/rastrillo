package handoff

import (
	"net/http"
	"net/url"
	"time"

	"amadan.net/rastrillo/rastrillo/basepath"
	"amadan.net/rastrillo/rastrillo/sessions"
)

// Renew sends a handoff session older than MaxAge back through the root,
// so a member the root has since removed or suspended loses access within
// that window. Wrap the signed-in routes with it, inside
// sessions.Middleware. Requests to EnterPath always pass, or a stale
// person could never reach the route that renews them. Other sessions and
// path-routed requests pass too: public visitors' sessions are not
// handoff sessions, and path tenants use the hub's session.
func (handoff *Handoff) Renew(next http.Handler) http.Handler {
	return http.HandlerFunc(func(
		response http.ResponseWriter, request *http.Request,
	) {
		session, ok := sessions.Current(request)
		if !ok || request.URL.Path == EnterPath ||
			session.Method != Method ||
			basepath.PathRouted(request) ||
			handoff.now().Sub(session.At) <= handoff.maxAge {
			next.ServeHTTP(response, request)
			return
		}
		if request.Method == http.MethodGet ||
			request.Method == http.MethodHead {
			http.Redirect(response, request, EnterPath+"?next="+
				url.QueryEscape(request.URL.RequestURI()),
				http.StatusSeeOther)
			return
		}
		// A redirect would drop the submitted body, and a new page of
		// copy would need review, so the refusal carries no body.
		response.Header().Set("Cache-Control", "no-store")
		response.WriteHeader(http.StatusUnauthorized)
	})
}

// Sweep deletes spent assertion IDs that Verify would refuse anyway, so
// the replay table stays small. Correctness never depends on it.
func (handoff *Handoff) Sweep(now time.Time) error {
	_, err := handoff.db.Exec(
		`DELETE FROM handoff_used WHERE expires_at <= ?`, now.Unix())
	return err
}
