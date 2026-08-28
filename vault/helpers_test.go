package vault_test

import (
	"errors"
	"net/http"
	"testing"
)

// failingHTTPClient fails the test on any dial — the
// strictly-additive assertion as a transport.
func failingHTTPClient(t *testing.T) *http.Client {
	t.Helper()
	return &http.Client{Transport: failTransport{t}}
}

type failTransport struct{ t *testing.T }

func (f failTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	f.t.Errorf("unexpected request: %s %s", r.Method, r.URL)
	return nil, errors.New("dial refused by test")
}
