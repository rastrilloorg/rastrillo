package basepath

import "testing"

func TestProductHostIsMatchedTheWayBrowsersSendIt(t *testing.T) {
	for _, c := range []struct {
		received, origin string
		want             bool
	}{
		{"example.com", "https://example.com", true},
		{"Example.COM", "https://example.com", true},
		{"example.com.", "https://example.com", true},
		{"example.com:443", "https://example.com", true},
		{"example.com.:443", "https://example.com", true},
		{"example.com:8443", "https://example.com", false},
		{"www.example.com", "https://example.com", false},
		{"carlos", "https://example.com", false},
		{"localhost:8080", "http://localhost:8080", true},
		{"127.0.0.1:8080", "http://localhost:8080", false},
		{"localhost", "http://localhost:8080", false},
		{"example.com", "https://Example.com:443", true},
	} {
		if got := sameHost(c.received, c.origin); got != c.want {
			t.Errorf("sameHost(%q, %q) = %v, want %v", c.received, c.origin, got, c.want)
		}
	}
}
