package sessions

import (
	"net/http"

	rastrillo "amadan.net/rastrillo/rastrillo"
)

// ActorFrom is a page request's actor: the signed-in person, with Write
// access, since a browser session can do whatever its person can. The
// generated router uses it when the app's ctx factory left Ctx.Actor
// zero, so op.Check sees the same subject the scoped store filters by
// (Current's Subject) instead of refusing a signed-in owner for having
// no subject.
//
// ok is false with no session, and for a session with no Subject: that
// names nobody, and calling it a person would hand an anonymous caller
// Write access.
func ActorFrom(r *http.Request) (rastrillo.Actor, bool) {
	sess, ok := Current(r)
	if !ok || sess.Subject == "" {
		return rastrillo.Actor{}, false
	}
	return rastrillo.Actor{Subject: sess.Subject, Access: rastrillo.Write}, true
}
