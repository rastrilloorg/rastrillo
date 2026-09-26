// Package clientip finds the address a request came from when it has
// passed through proxies, for the things that key on it: a per-IP rate
// limit, an abuse log line.
//
// The only safe direction to read X-Forwarded-For is from the RIGHT. A
// proxy APPENDS the peer it accepted the connection from (that is what
// net/http/httputil's SetXForwarded does, and the convention every
// other proxy follows), so the elements on the left are whatever the
// client chose to send and only the trailing ones were written by
// infrastructure you run. Taking the first element — the bug Tito Go
// shipped and then fixed, twice — lets anyone give every request a
// fresh fake leading address: each forged prefix looks like a new
// visitor, so every IP-keyed limiter counts to one and never fires, and
// every log line naming an address names the one the abuser picked.
//
// How many trailing elements are yours is a fact about the DEPLOYMENT,
// never inferred from the request: a header that could say how far to
// count back would be the same hole again. The caller passes it.
package clientip

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
)

// DefaultHops is the hop count for an app with exactly one proxy
// directly in front of it — on CARLOS, the platform edge, which hands
// the app a chain whose last element is the client that dialled it,
// even when the request crossed to a home edge in another region
// (carlosframework/platform internal/edge, setForwarded). It is what
// ParseHops returns for an empty setting.
//
// Too HIGH is a security hole: the trusted element moves left into
// what the client typed. Too LOW buckets everyone behind the proxy
// together. So it is a number somebody decides and writes down, and
// raises deliberately when they put a second proxy of their own in
// front.
const DefaultHops = 1

// From returns the address r originated from, trusting the last hops
// elements of X-Forwarded-For and nothing to their left.
//
// hops of zero (or less) trusts no header at all and answers the
// connection's peer: the right setting for an app nothing is in front
// of, where X-Forwarded-For is entirely the client's invention.
//
// Both fallbacks go to the peer, never to a client-supplied element.
// No header means nothing is in front (local development, tests), so
// the peer IS the client. A header with fewer elements than the trusted
// chain means the request did not come through that chain at all — a
// direct dial past the proxy, or a hop count set wrong. Bucketing every
// such request under the peer is the fail-closed answer: it can
// throttle too much, it cannot hand out unlimited keys.
//
// A peer that is not host:port (a unix socket's "@") is returned as it
// is, so every such request shares one bucket.
func From(r *http.Request, hops int) string {
	if hops > 0 {
		// Every field, not Header.Get's first: a proxy may append its
		// element as a field of its own rather than onto the existing
		// line, and then the first field is entirely the client's.
		// Joined in order, separate fields and one comma-separated
		// field are the same list (RFC 9110 5.3), which is how the
		// CARLOS edge reads them too.
		if xff := strings.Join(r.Header.Values("X-Forwarded-For"), ","); xff != "" {
			parts := strings.Split(xff, ",")
			if len(parts) >= hops {
				if ip := strings.TrimSpace(parts[len(parts)-hops]); ip != "" {
					return ip
				}
			}
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// ParseHops reads a configured hop count, typically from an
// environment variable. Empty means DefaultHops. Anything that is not
// a whole number of zero or more returns DefaultHops WITH an error, so
// the caller can log it and carry on: a limiter keyed off the wrong
// element is a security control that quietly does nothing, so a typo
// must never widen what is trusted, and refusing to boot over it would
// turn a typo into an outage.
//
// Zero is accepted: it narrows trust to the peer, which is safe to
// mistype into.
func ParseHops(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return DefaultHops, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return DefaultHops, fmt.Errorf("clientip: hop count %q is not a whole number of zero or more; using %d", raw, DefaultHops)
	}
	return n, nil
}
