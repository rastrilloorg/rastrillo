package auth

import (
	"context"
	"errors"
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

// keymailServers parses Config.KeymailServers into a set of
// serverKeys; nil means any delegated server, and a non-nil empty slice
// means none. The empty case must come back as a non-nil empty map, not
// nil: New and continuation.go both test the set against nil, so
// collapsing it would read as "any server" and leave an app with no
// federation partners no way to turn probing off. An entry that
// could never equal a URL's host — a scheme, a path, userinfo, a port
// that is not a port, an unclosed IPv6 bracket, a control character —
// is refused here, because accepted it would match nothing and every
// keymail user would quietly be sent a link instead.
func keymailServers(list []string) (map[string]bool, error) {
	if list == nil {
		return nil, nil
	}
	set := make(map[string]bool, len(list))
	for _, s := range list {
		h := strings.ToLower(strings.TrimSpace(s))
		if !validAuthority(h) {
			return nil, fmt.Errorf("rastrillo/auth: KeymailServers entry %q is not a host or host:port", s)
		}
		set[serverKey(h)] = true
	}
	return set, nil
}

// errNeverDelegates is a plain, non-"not found" error: classify.go's
// delegate() treats "not found" (NXDOMAIN) as an answer — no
// delegation, the domain itself is the candidate — and proceeds to
// probe it. Any other resolver error "names nothing", and delegate()
// gives up without a candidate. neverDelegates wants the second
// reading: with KeymailServers: []string{} there is no candidate worth
// naming, because every candidate is refused.
var errNeverDelegates = errors.New("rastrillo/auth: KeymailServers is empty — no server is ever delegated to")

// neverDelegates is the LookupTXT New installs when KeymailServers is
// a non-nil empty slice. It never touches the network: every domain's
// _keymail delegation "fails" the same way, which skips the
// well-known probe that would otherwise follow a real DNS answer,
// before the guard would refuse it anyway.
func neverDelegates(context.Context, string) ([]string, error) {
	return nil, errNeverDelegates
}

// serverKey is the one spelling of a keymail server that the list and
// every host checked against it are reduced to: lowercased, one trailing
// dot dropped, and an explicit :443 dropped. Each of those names the
// same https server, and compared as written an operator's
// "keymail.dev:443" never equals the "keymail.dev" DNS delegates to —
// so keymail is silently off, every keymail user gets a link, and
// nothing is logged. Any other port stays: it is a different server.
func serverKey(authority string) string {
	h, port := strings.ToLower(authority), ""
	// The last colon starts a port only outside an IPv6 literal's
	// brackets.
	if i := strings.LastIndexByte(h, ':'); i >= 0 && !strings.Contains(h[i:], "]") {
		h, port = h[:i], h[i:]
	}
	if port == ":443" {
		port = ""
	}
	return strings.TrimSuffix(h, ".") + port
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
	// url.Parse folds an unbracketed IPv6 literal's colons into
	// Hostname()+Port() the same way it would a bracketed one, but
	// serverKey does not: it takes only the last colon as a port. An
	// unbracketed "::1" and a request to ::1 (which Go dials as
	// [::1]:443) would then key alike, and an entry like "::1:8443"
	// would key alike with a request to ::1:8443 (dialed as
	// [::1:8443]:443) — two different authorities sharing one
	// allowlist entry. Demanding brackets around any colon closes that;
	// a bracketed host is left to serverKey exactly as parsed.
	host := h
	if !strings.HasPrefix(host, "[") {
		if p := u.Port(); p != "" {
			host = strings.TrimSuffix(host, ":"+p)
		}
		if strings.Contains(host, ":") {
			return false
		}
	}
	// A host of only dots parses clean but names nothing: serverKey
	// trims one trailing dot, so "." and ".." key as "" and ".",
	// neither of which any real request's host can equal.
	if strings.Trim(host, ".") == "" {
		return false
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
	host := serverKey(r.URL.Host)
	if r.URL.Scheme != "https" || !g.allow[host] {
		// A RoundTripper owns the body even when it refuses.
		if r.Body != nil {
			r.Body.Close()
		}
		// This message names only the host, but that is not what keeps
		// the address out of logs: http.Client wraps it in a *url.Error
		// carrying the full URL, and the federation probe's query is the
		// address being signed in. What keeps it out is the classifier,
		// which reads any failure as "not keymail" and discards the
		// error unlogged (K/classify.go:285-293). Callback does log a
		// refused exchange, but that URL is the token endpoint or the
		// hop a listed server redirected to: the code and verifier
		// travel in the body, never the URL. Anything that starts
		// logging a probe's error must strip the URL first.
		return nil, fmt.Errorf("rastrillo/auth: %s://%s is not a listed keymail server", r.URL.Scheme, host)
	}
	base := g.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(r)
}
