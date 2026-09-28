package auth

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// callbackPath is where keymail sends the browser back. auth sets it on
// every signin.Keymail explicitly rather than inheriting the library's
// default, because the authorize-URL predicate checks redirect_uri
// against this same constant: one value built, the same value checked,
// so the two cannot drift apart on a library upgrade.
const callbackPath = "/auth/callback"

// The library's own client timeouts (K/classify.go:68,
// K/keymail.go:107), kept when auth replaces the clients to guard
// them: a guard that dropped them would let a slow keymail server hold
// a sign-in request open indefinitely.
const (
	classifyTimeout = 5 * time.Second
	exchangeTimeout = 15 * time.Second
)

// keymailServers parses Config.KeymailServers into a lowercased set; nil
// means any delegated server. An entry that could never equal a URL's
// host — a scheme, a path, userinfo, a port that is not a port, an
// unclosed IPv6 bracket, a control character — is refused here, because
// accepted it would match nothing and every keymail user would quietly
// be sent a link instead.
func keymailServers(list []string) (map[string]bool, error) {
	if len(list) == 0 {
		return nil, nil
	}
	set := make(map[string]bool, len(list))
	for _, s := range list {
		h := strings.ToLower(strings.TrimSpace(s))
		if !validAuthority(h) {
			return nil, fmt.Errorf("rastrillo/auth: KeymailServers entry %q is not a host or host:port", s)
		}
		set[h] = true
	}
	return set, nil
}

// validAuthority is a host with an optional port, exactly as a URL's
// Host carries it: parsing it as one and demanding it come back
// unchanged is what makes "a string that can equal some URL's host"
// the rule, rather than a list of characters someone thought of.
func validAuthority(h string) bool {
	if h == "" || strings.ContainsFunc(h, func(r rune) bool { return r <= ' ' || r == 0x7f }) {
		return false
	}
	u, err := url.Parse("https://" + h)
	if err != nil || u.Host != h || u.User != nil || u.Path != "" || u.RawQuery != "" ||
		u.Fragment != "" || u.Hostname() == "" || strings.HasSuffix(h, ":") {
		return false
	}
	if p := u.Port(); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			return false
		}
	}
	return true
}

// hostGuard is the transport under both keymail clients when
// KeymailServers is set. It refuses any request that is not https to a
// listed host — on every hop, because http.Client calls its transport
// again for each redirect it follows, so a listed server cannot bounce
// a probe, or a token exchange carrying the code and verifier, to a
// host nobody listed or down to plain http.
type hostGuard struct {
	allow map[string]bool
	// base carries what the guard lets through; http.DefaultTransport
	// when nil. A field so tests can put an in-process keymail server
	// under the guard.
	base http.RoundTripper
}

func (g *hostGuard) RoundTrip(r *http.Request) (*http.Response, error) {
	host := strings.ToLower(r.URL.Host)
	if r.URL.Scheme != "https" || !g.allow[host] {
		// A RoundTripper owns the body even when it refuses.
		if r.Body != nil {
			r.Body.Close()
		}
		// Host only: the probe's query carries the address being signed in.
		return nil, fmt.Errorf("rastrillo/auth: %s://%s is not a listed keymail server", r.URL.Scheme, host)
	}
	base := g.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(r)
}
