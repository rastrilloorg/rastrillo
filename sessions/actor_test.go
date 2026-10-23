package sessions_test

import (
	"net/http/httptest"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/sessions"
)

// A page request's actor is the signed-in person with full access, so
// op.Check sees the same subject the scoped store filters by. A request
// with no session has no actor: the router leaves Ctx.Actor zero, which
// is how Check tells an anonymous visitor from a person.
func TestActorFrom(t *testing.T) {
	s := sessions.Session{Subject: "42", Method: "password", AuthTime: time.Now(), At: time.Now()}
	r := sessions.WithSession(httptest.NewRequest("GET", "/", nil), s)
	a, ok := sessions.ActorFrom(r)
	if !ok {
		t.Fatal("ActorFrom: ok = false for a request carrying a session")
	}
	if want := (rastrillo.Actor{Subject: "42", Access: rastrillo.Write}); a != want {
		t.Errorf("ActorFrom = %+v, want %+v", a, want)
	}

	if a, ok := sessions.ActorFrom(httptest.NewRequest("GET", "/", nil)); ok {
		t.Errorf("ActorFrom = %+v, true for a request with no session", a)
	}

	// A session with no subject names nobody. Calling it a person would
	// give an anonymous caller Write access and a non-zero actor that
	// stops the router looking any further.
	r = sessions.WithSession(httptest.NewRequest("GET", "/", nil), sessions.Session{})
	if a, ok := sessions.ActorFrom(r); ok {
		t.Errorf("ActorFrom = %+v, true for a session with no subject", a)
	}
}
