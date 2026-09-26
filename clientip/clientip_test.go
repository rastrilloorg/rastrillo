package clientip

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// From must read X-Forwarded-For from the RIGHT: the trailing elements
// are the ones proxies you run appended, everything left of them is a
// string the client sent. Taking the first element (the bug this
// replaced in Tito Go) handed every IP-keyed limiter a fresh bucket per
// forged prefix.
func TestFromTrustsOnlyTheTrailingHops(t *testing.T) {
	cases := []struct {
		name   string
		hops   int
		xff    string
		remote string
		want   string
	}{
		{
			name:   "no header at all falls back to the peer",
			hops:   1,
			remote: "192.0.2.5:41234",
			want:   "192.0.2.5",
		},
		{
			name:   "one proxy, one element",
			hops:   1,
			xff:    "203.0.113.9",
			remote: "127.0.0.1:9999",
			want:   "203.0.113.9",
		},
		{
			name:   "a forged prefix is ignored for the appended peer",
			hops:   1,
			xff:    "1.1.1.1, 203.0.113.9",
			remote: "127.0.0.1:9999",
			want:   "203.0.113.9",
		},
		{
			name:   "a whole forged chain is ignored",
			hops:   1,
			xff:    "1.1.1.1, 2.2.2.2, 3.3.3.3, 203.0.113.9",
			remote: "127.0.0.1:9999",
			want:   "203.0.113.9",
		},
		{
			name:   "two trusted hops read one element further left",
			hops:   2,
			xff:    "1.1.1.1, 203.0.113.9, 10.0.0.1",
			remote: "127.0.0.1:9999",
			want:   "203.0.113.9",
		},
		{
			// Fewer elements than the trusted chain means the request never
			// came through it: fall back to the peer rather than to the one
			// element the client is holding.
			name:   "a header shorter than the trusted chain falls back to the peer",
			hops:   2,
			xff:    "1.1.1.1",
			remote: "192.0.2.7:1234",
			want:   "192.0.2.7",
		},
		{
			name:   "whitespace around the trusted element",
			hops:   1,
			xff:    "1.1.1.1,   203.0.113.9   ",
			remote: "127.0.0.1:9999",
			want:   "203.0.113.9",
		},
		{
			// An empty trusted element is not an address; the peer is.
			name:   "an empty trailing element falls back to the peer",
			hops:   1,
			xff:    "1.1.1.1, ",
			remote: "192.0.2.8:1234",
			want:   "192.0.2.8",
		},
		{
			// Zero hops is "nothing is in front of me": every element is the
			// client's invention, however plausible it looks.
			name:   "zero hops ignores the header entirely",
			hops:   0,
			xff:    "203.0.113.9",
			remote: "192.0.2.9:1234",
			want:   "192.0.2.9",
		},
		{
			// A unix-socket peer has no host:port to split — the raw value is
			// the honest answer, and every such request shares one bucket.
			name:   "a socket peer with no header",
			hops:   1,
			remote: "@",
			want:   "@",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = tc.remote
			if tc.xff != "" {
				r.Header.Set("X-Forwarded-For", tc.xff)
			}
			if got := From(r, tc.hops); got != tc.want {
				t.Errorf("From = %q, want %q (hops=%d, xff=%q, remote=%q)", got, tc.want, tc.hops, tc.xff, tc.remote)
			}
		})
	}
}

// The hop count is a deployment fact, so a value that can't be one must
// leave the default standing — and say so — rather than widening what
// is trusted.
func TestParseHopsRefusesNonsense(t *testing.T) {
	for _, raw := range []string{"-1", "two", "1.5", "0x2"} {
		got, err := ParseHops(raw)
		if err == nil {
			t.Errorf("ParseHops(%q) gave no error", raw)
		}
		if got != DefaultHops {
			t.Errorf("ParseHops(%q) = %d, want the default %d", raw, got, DefaultHops)
		}
	}
	for raw, want := range map[string]int{"": DefaultHops, "  ": DefaultHops, "0": 0, "1": 1, " 2 ": 2} {
		got, err := ParseHops(raw)
		if err != nil || got != want {
			t.Errorf("ParseHops(%q) = %d, %v; want %d, nil", raw, got, err, want)
		}
	}
}
