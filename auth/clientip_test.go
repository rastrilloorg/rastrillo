package auth

import (
	"fmt"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// beginFrom POSTs a forced magic-link sign-in as if it arrived with the
// given X-Forwarded-For, from a proxy at 10.9.9.9.
func beginFrom(t *testing.T, a *Auth, address, xff string) string {
	t.Helper()
	form := url.Values{"address": {address}, "force": {"1"}}
	r := httptest.NewRequest("POST", "http://app.test/signin", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.RemoteAddr = "10.9.9.9:4321"
	r.Header.Set("X-Forwarded-For", xff)
	w := httptest.NewRecorder()
	a.Begin(w, r)
	return w.Header().Get("Location")
}

// The per-IP sign-in budget (20 per window in signin) must count the
// client the proxy saw, not the prefix the client wrote. With one
// trusted hop, a fresh forged prefix per request is the same visitor:
// the budget runs out. Before clientip, every request here was keyed
// on the proxy's address — correct but shared by everyone.
func TestSigninBudgetIgnoresAForgedPrefix(t *testing.T) {
	a, _ := newTestAuth(t, func(c *Config) { one := 1; c.TrustedProxyHops = &one })
	var last string
	for i := 0; i < 21; i++ {
		// A distinct address each time, so the per-address budget (5)
		// is never what refuses.
		last = beginFrom(t, a, fmt.Sprintf("p%d@example.com", i), fmt.Sprintf("198.51.100.%d, 203.0.113.9", i))
	}
	if last != "/signin?err=rate" {
		t.Fatalf("21st sign-in from one client behind forged prefixes → %q, want the rate refusal", last)
	}
}

// And the other half: with one trusted hop, two real visitors behind
// the same proxy get a budget each rather than sharing the proxy's.
func TestSigninBudgetIsPerClientBehindAProxy(t *testing.T) {
	a, _ := newTestAuth(t, func(c *Config) { one := 1; c.TrustedProxyHops = &one })
	for i := 0; i < 21; i++ {
		if got := beginFrom(t, a, fmt.Sprintf("p%d@example.com", i), fmt.Sprintf("203.0.113.%d", i)); got != "/signin?sent=1" {
			t.Fatalf("sign-in %d from its own client → %q, want sent", i, got)
		}
	}
}

// Off CARLOS with nothing set: no proxy is trusted, the header is
// ignored and everyone behind a proxy shares its budget.
func TestSigninBudgetWithNoTrustedHopsIsThePeers(t *testing.T) {
	t.Setenv("CARLOS_CONTROL_SOCKET", "")
	a, _ := newTestAuth(t, nil)
	var last string
	for i := 0; i < 21; i++ {
		last = beginFrom(t, a, fmt.Sprintf("p%d@example.com", i), fmt.Sprintf("203.0.113.%d", i))
	}
	if last != "/signin?err=rate" {
		t.Fatalf("21st sign-in through one proxy with no trusted hops → %q, want the rate refusal", last)
	}
}

// On CARLOS with nothing set, the edge is the one trusted hop: the last
// X-Forwarded-For element is the visitor, so a forged prefix per request
// is still one visitor and runs out of budget...
func TestSigninBudgetOnCarlosTrustsTheEdge(t *testing.T) {
	t.Setenv("CARLOS_CONTROL_SOCKET", "/run/carlos/app.sock.control")
	a, _ := newTestAuth(t, nil)
	var last string
	for i := 0; i < 21; i++ {
		last = beginFrom(t, a, fmt.Sprintf("p%d@example.com", i), fmt.Sprintf("198.51.100.%d, 203.0.113.9", i))
	}
	if last != "/signin?err=rate" {
		t.Fatalf("21st sign-in from one visitor on CARLOS -> %q, want the rate refusal", last)
	}
}

// ...and two visitors behind the edge get a budget each.
func TestSigninBudgetOnCarlosIsPerVisitor(t *testing.T) {
	t.Setenv("CARLOS_CONTROL_SOCKET", "/run/carlos/app.sock.control")
	a, _ := newTestAuth(t, nil)
	for i := 0; i < 21; i++ {
		if got := beginFrom(t, a, fmt.Sprintf("p%d@example.com", i), fmt.Sprintf("203.0.113.%d", i)); got != "/signin?sent=1" {
			t.Fatalf("sign-in %d from its own visitor on CARLOS -> %q, want sent", i, got)
		}
	}
}

// A value the app sets wins over the platform default, 0 included.
func TestExplicitZeroHopsWinsOnCarlos(t *testing.T) {
	t.Setenv("CARLOS_CONTROL_SOCKET", "/run/carlos/app.sock.control")
	zero := 0
	a, _ := newTestAuth(t, func(c *Config) { c.TrustedProxyHops = &zero })
	var last string
	for i := 0; i < 21; i++ {
		last = beginFrom(t, a, fmt.Sprintf("p%d@example.com", i), fmt.Sprintf("203.0.113.%d", i))
	}
	if last != "/signin?err=rate" {
		t.Fatalf("explicit 0 on CARLOS -> %q, want every visitor on the peer's one budget", last)
	}
}
