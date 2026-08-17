package auth

import (
	"net/http"
	"time"

	"github.com/keymaildev/signin"
)

// newClassifier builds the address classifier with one correction:
// signin v0.1.0 probes GET /api/lookup?addr=..., but keymail's real
// route is GET /api/federation/lookup (keymail main.go registers only
// the latter; the CARLOS console client uses it too). Because
// classification fails open — any probe failure reads as "not keymail"
// — the stock classifier silently sends every address down the email
// path against a real keymail deployment. The classifier exposes no
// path knob, so the fix rides its HTTP client as a rewriting
// RoundTripper. Delete this (and the transport) when signin fixes the
// path upstream.
func newClassifier() *signin.Classifier {
	return &signin.Classifier{
		HTTP: &http.Client{
			Timeout:   5 * time.Second,
			Transport: lookupPathFix{next: http.DefaultTransport},
		},
	}
}

// lookupPathFix rewrites the classifier's /api/lookup probe to keymail's
// real /api/federation/lookup. Everything else (the /.well-known/keymail
// fetch) passes through untouched.
type lookupPathFix struct{ next http.RoundTripper }

func (t lookupPathFix) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Path == "/api/lookup" {
		r2 := req.Clone(req.Context())
		r2.URL.Path = "/api/federation/lookup"
		req = r2
	}
	return t.next.RoundTrip(req)
}
