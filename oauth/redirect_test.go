package oauth_test

import (
	"testing"

	"amadan.net/rastrillo/rastrillo/oauth"
)

func TestRedirectAllowed(t *testing.T) {
	const app = "https://app.example/cb"
	tests := []struct {
		name       string
		registered string
		requested  string
		want       bool
	}{
		{"exact https", app, app, true},
		{"other path", app, "https://app.example/other", false},
		{"other query", app + "?a=1", app + "?a=2", false},
		{"same query", app + "?a=1", app + "?a=1", true},
		{"no query against query", app, app + "?a=1", false},
		{"escaped slash", "https://app.example/a/b",
			"https://app.example/a%2Fb", false},
		{"same escaped path", "https://app.example/a%2Fb",
			"https://app.example/a%2Fb", true},
		{"fragment", app, app + "#x", false},
		{"other host", app, "https://evil.example/cb", false},
		{"other https port", app, "https://app.example:8443/cb", false},
		{"loopback other port", "http://127.0.0.1:1111/cb",
			"http://127.0.0.1:2222/cb", true},
		{"loopback no port", "http://127.0.0.1/cb",
			"http://127.0.0.1:2222/cb", true},
		{"loopback ipv6 other port", "http://[::1]:1/cb",
			"http://[::1]:2/cb", true},
		{"localhost other port", "http://localhost:1/cb",
			"http://localhost:2/cb", true},
		{"loopback other path", "http://127.0.0.1:1/cb",
			"http://127.0.0.1:1/x", false},
		{"loopback other query", "http://127.0.0.1:1/cb",
			"http://127.0.0.1:1/cb?a=1", false},
		{"localhost for 127.0.0.1", "http://localhost:1/cb",
			"http://127.0.0.1:1/cb", false},
		{"127.0.0.1 for localhost", "http://127.0.0.1:1/cb",
			"http://localhost:1/cb", false},
		{"http to a public host", "http://app.example/cb",
			"http://app.example/cb", false},
		{"https to loopback", "https://127.0.0.1/cb",
			"https://127.0.0.1/cb", true},
		{"custom scheme", "app://cb", "app://cb", false},
		{"userinfo", app, "https://u@app.example/cb", false},
		{"not a url", app, "::", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := oauth.RedirectAllowed(
				[]string{"https://other.example/x", tt.registered},
				tt.requested)
			if got != tt.want {
				t.Fatalf("registered %q, requested %q: got %v",
					tt.registered, tt.requested, got)
			}
		})
	}
	if oauth.RedirectAllowed(nil, app) {
		t.Fatal("nothing registered allowed a redirect")
	}
}

// The approval page shows and links to a redirect, so one holding a
// quote, angle bracket or white space is refused even when it matches.
func TestRedirectRefusesMarkupAndSpace(t *testing.T) {
	for _, redirect := range []string{
		`https://app.example/c"b`, "https://app.example/c<b",
		"https://app.example/c>b", "https://app.example/c b",
		"https://app.example/cb?a=\tb", "https://app.example/c\u00a0b",
	} {
		if oauth.RedirectAllowed([]string{redirect}, redirect) {
			t.Errorf("allowed %q", redirect)
		}
	}
}
