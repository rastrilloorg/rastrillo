package auth

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// keymailFake is a keymail federation server in-process: an
// http.RoundTripper answering the endpoints auth's two clients call —
// the well-known document and the federation lookup for
// classification, the token endpoint for the exchange — for the hosts
// it is told are servers, and 404 for any other host. It records every
// request that reaches it, which is how the allowlist tests prove a
// host was never contacted: a request the guard refused never arrives.
// Nothing here opens a socket or resolves a name.
type keymailFake struct {
	mu       sync.Mutex
	servers  map[string]bool   // hosts that answer as keymail servers
	claimed  map[string]bool   // lowercased addresses whose lookup is a 200
	delegate map[string]string // domain -> the host its _keymail TXT names
	address  string            // what the token endpoint says was verified
	redirect map[string]string // path -> Location: a server bouncing the request
	// status is the redirect's code: 302 by default; 307 and 308 keep
	// the method and body, so a bounced token exchange would carry the
	// code and verifier to wherever Location points.
	status int
	seen   []string // "host path" of every request that arrived
}

func newKeymailFake() *keymailFake {
	return &keymailFake{
		servers: map[string]bool{}, claimed: map[string]bool{},
		delegate: map[string]string{}, redirect: map[string]string{},
	}
}

// kayFake is the common fixture: kay@example.org delegates to
// keymail.test, which answers for her and verifies her.
func kayFake() *keymailFake {
	f := newKeymailFake()
	f.servers["keymail.test"] = true
	f.delegate["example.org"] = "keymail.test"
	f.claimed["kay@example.org"] = true
	f.address = "kay@example.org"
	return f
}

func (f *keymailFake) RoundTrip(r *http.Request) (*http.Response, error) {
	f.mu.Lock()
	f.seen = append(f.seen, r.URL.Host+" "+r.URL.Path)
	server, to := f.servers[r.URL.Host], f.redirect[r.URL.Path]
	claimed, address, status := f.claimed[r.URL.Query().Get("addr")], f.address, f.status
	f.mu.Unlock()
	if r.Body != nil {
		r.Body.Close()
	}
	rec := httptest.NewRecorder()
	switch {
	case !server:
		rec.WriteHeader(http.StatusNotFound)
	case to != "":
		if status == 0 {
			status = http.StatusFound
		}
		rec.Header().Set("Location", to)
		rec.WriteHeader(status)
	case r.URL.Path == "/.well-known/keymail":
		rec.WriteString(`{"version":"1"}`)
	case r.URL.Path == "/api/federation/lookup" && claimed:
		rec.WriteString(`{}`)
	case r.URL.Path == "/api/oauth/token":
		fmt.Fprintf(rec, `{"access_token":"t","token_type":"bearer","address":%q}`, address)
	default:
		rec.WriteHeader(http.StatusNotFound)
	}
	res := rec.Result()
	res.Request = r
	return res, nil
}

func (f *keymailFake) lookupTXT(_ context.Context, name string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if host, ok := f.delegate[strings.TrimPrefix(name, "_keymail.")]; ok {
		return []string{"v=1 host=" + host}, nil
	}
	return nil, &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
}

// hits counts the requests that reached host — at path, or anywhere
// when path is "".
func (f *keymailFake) hits(host, path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, s := range f.seen {
		h, p, _ := strings.Cut(s, " ")
		if h == host && (path == "" || p == path) {
			n++
		}
	}
	return n
}

// wireKeymail points a's keymail clients at f. Without KeymailServers
// the fake replaces the default clients outright; with them it sits
// UNDER the guard, which is the arrangement the allowlist tests need.
func wireKeymail(a *Auth, f *keymailFake) {
	a.flow.Classifier.LookupTXT = f.lookupTXT
	if a.guard != nil {
		a.guard.base = f
		return
	}
	a.flow.Classifier.HTTP = &http.Client{Transport: f}
	a.exchangeHTTP = &http.Client{Transport: f}
}

// stateOf is the state parameter of an authorize URL.
func stateOf(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parsing %q: %v", raw, err)
	}
	return u.Query().Get("state")
}
