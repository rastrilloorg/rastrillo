package oauth

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	fetchTimeout = 5 * time.Second
	maxDocument  = 64 << 10
	maxCacheAge  = 24 * time.Hour
	maxCached    = 1024
)

var errUnsafeAddress = errors.New("oauth: not a public address")

// specialUse lists the ranges the netip predicates miss: addresses that
// are valid unicast but never name a public server.
var specialUse = parsePrefixes(
	"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24",
	"198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4",
	"::/96", "64:ff9b::/96", "64:ff9b:1::/48", "2002::/16", "2001:db8::/32",
	"fc00::/7", "fe80::/10", "fec0::/10")

func parsePrefixes(cidrs ...string) []netip.Prefix {
	var out []netip.Prefix
	for _, cidr := range cidrs {
		out = append(out, netip.MustParsePrefix(cidr))
	}
	return out
}

// publicAddr reports whether address is an address on the open internet. An
// IPv4-mapped IPv6 address is judged as the IPv4 address it carries.
func publicAddr(address netip.Addr) bool {
	address = address.Unmap()
	if !address.IsGlobalUnicast() || address.IsPrivate() ||
		address.IsLoopback() || address.IsLinkLocalUnicast() {
		return false
	}
	for _, prefix := range specialUse {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

type cachedClient struct {
	client  Client
	expires time.Time
}

// Fetcher reads Client ID Metadata Documents. The address check runs
// when the connection is made, on the address actually dialed, so a name
// that resolves differently the second time cannot get past it.
type Fetcher struct {
	client    *http.Client
	transport *http.Transport
	// The three hooks below are set by tests only.
	lookup  func(context.Context, string) ([]netip.Addr, error)
	allowed func(netip.Addr) bool
	now     func() time.Time

	mu    sync.Mutex
	cache map[string]cachedClient
}

func NewFetcher() *Fetcher {
	fetcher := &Fetcher{
		lookup: func(
			ctx context.Context, host string,
		) ([]netip.Addr, error) {
			return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		},
		allowed: publicAddr,
		now:     time.Now,
		cache:   map[string]cachedClient{},
	}
	fetcher.transport = &http.Transport{
		Proxy:                 nil,
		DialContext:           fetcher.dial,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout:   fetchTimeout,
		ResponseHeaderTimeout: fetchTimeout,
		// Each fetch dials afresh, so each is checked afresh.
		DisableKeepAlives: true,
	}
	fetcher.client = &http.Client{
		Transport: fetcher.transport,
		Timeout:   fetchTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return fetcher
}

// dial connects to the first of the host's addresses that will take
// the connection and that the address check admits.
func (fetcher *Fetcher) dial(
	ctx context.Context, network, address string,
) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	addrs, err := fetcher.lookup(ctx, host)
	if err != nil {
		return nil, err
	}
	dialer := &net.Dialer{Timeout: fetchTimeout, Control: fetcher.control}
	err = fmt.Errorf("oauth: %s resolves to nothing", host)
	for _, resolved := range addrs {
		conn, derr := dialer.DialContext(ctx, network,
			net.JoinHostPort(resolved.String(), port))
		if derr == nil {
			return conn, nil
		}
		err = derr
	}
	return nil, err
}

// control runs the address check on the address actually dialed.
func (fetcher *Fetcher) control(
	_, address string, _ syscall.RawConn,
) error {
	ap, err := netip.ParseAddrPort(address)
	if err != nil {
		return err
	}
	if !fetcher.allowed(ap.Addr()) {
		return errUnsafeAddress
	}
	return nil
}

// Fetch returns the client described by the document at rawURL.
func (fetcher *Fetcher) Fetch(
	ctx context.Context, rawURL string,
) (Client, error) {
	if client, ok := fetcher.cached(rawURL); ok {
		return client, nil
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" ||
		parsed.Path == "" || parsed.User != nil ||
		strings.Contains(rawURL, "#") {
		return Client{}, errors.New("oauth: not a client document URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return Client{}, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := fetcher.client.Do(req)
	if err != nil {
		return Client{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Client{}, fmt.Errorf("oauth: client document: %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxDocument+1))
	if err != nil {
		return Client{}, err
	}
	if len(body) > maxDocument {
		return Client{}, errors.New("oauth: client document too large")
	}
	found, err := parseDocument(rawURL, body)
	if err != nil {
		return Client{}, err
	}
	fetcher.store(rawURL, found, maxAge(resp.Header.Get("Cache-Control")))
	return found, nil
}

// parseDocument reads the client a document at id describes. Only
// public clients are served, so a document that asks for client
// authentication, or carries a secret, is refused rather than taken
// for a public client (Client ID Metadata Document §6.2).
func parseDocument(id string, body []byte) (Client, error) {
	var doc struct {
		ClientID     string   `json:"client_id"`
		ClientName   string   `json:"client_name"`
		RedirectURIs []string `json:"redirect_uris"`
		AuthMethod   *string  `json:"token_endpoint_auth_method"`
	}
	var fields map[string]json.RawMessage
	err := json.Unmarshal(body, &doc)
	if err == nil {
		err = json.Unmarshal(body, &fields)
	}
	if err != nil {
		return Client{}, fmt.Errorf("oauth: client document: %w", err)
	}
	_, secret := fields["client_secret"]
	_, expiry := fields["client_secret_expires_at"]
	if secret || expiry || doc.AuthMethod != nil && *doc.AuthMethod != "none" {
		return Client{}, errors.New(
			"oauth: client document is not for a public client")
	}
	if doc.ClientID != id || !validName(doc.ClientName) ||
		len(doc.RedirectURIs) == 0 {
		return Client{}, errors.New("oauth: client document is incomplete")
	}
	return Client{
		ID: id, Name: doc.ClientName, Redirects: doc.RedirectURIs,
		Document: true,
	}, nil
}

func (fetcher *Fetcher) cached(key string) (Client, bool) {
	fetcher.mu.Lock()
	defer fetcher.mu.Unlock()
	entry, ok := fetcher.cache[key]
	if !ok || !fetcher.now().Before(entry.expires) {
		delete(fetcher.cache, key)
		return Client{}, false
	}
	return entry.client, true
}

func (fetcher *Fetcher) store(key string, client Client, age time.Duration) {
	if age <= 0 {
		return
	}
	fetcher.mu.Lock()
	defer fetcher.mu.Unlock()
	now := fetcher.now()
	for cachedKey, entry := range fetcher.cache {
		if !now.Before(entry.expires) {
			delete(fetcher.cache, cachedKey)
		}
	}
	if len(fetcher.cache) < maxCached {
		expires := now.Add(min(age, maxCacheAge))
		fetcher.cache[key] = cachedClient{client, expires}
	}
}

// maxAge reads max-age from a Cache-Control header. Without one, or with
// no-store or no-cache, nothing is kept.
func maxAge(header string) time.Duration {
	var age time.Duration
	for _, directive := range strings.Split(header, ",") {
		directive = strings.ToLower(strings.TrimSpace(directive))
		switch {
		case directive == "no-store" || directive == "no-cache":
			return 0
		case strings.HasPrefix(directive, "max-age="):
			value := directive[len("max-age="):]
			seconds, err := strconv.ParseInt(value, 10, 32)
			if err == nil && seconds > 0 {
				age = time.Duration(seconds) * time.Second
			}
		}
	}
	return age
}
