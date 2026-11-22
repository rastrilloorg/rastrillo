package oauth

import (
	"context"
	"crypto/x509"
	"database/sql"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"time"
)

// Write exposes the SQL store's transaction helper to the tests.
func Write(store Store, fn func(*sql.Conn) error) error {
	return store.(*sqlStore).write(context.Background(), fn)
}

var (
	RedirectAllowed  = redirectAllowed
	PublicAddr       = publicAddr
	ErrUnsafeAddress = errUnsafeAddress
)

// Trust points the fetcher at a test document server: its roots, a
// resolver, and an address check that may admit loopback. A nil
// argument keeps the production behavior.
func (fetcher *Fetcher) Trust(
	roots *x509.CertPool,
	lookup func(context.Context, string) ([]netip.Addr, error),
	allowed func(netip.Addr) bool,
) {
	if roots != nil {
		fetcher.transport.TLSClientConfig.RootCAs = roots
	}
	if lookup != nil {
		fetcher.lookup = lookup
	}
	if allowed != nil {
		fetcher.allowed = allowed
	}
}

func (fetcher *Fetcher) SetClock(now func() time.Time) { fetcher.now = now }

type Clients = clients

func NewClients(
	store Store, fetcher *Fetcher, hops int, now func() time.Time,
) *Clients {
	registry := newClients(store, hops)
	registry.fetch, registry.now = fetcher, now
	return registry
}

func (registry *clients) Resolve(
	ctx context.Context,
	id string,
) (Client, error) {
	return registry.resolve(ctx, id)
}

func (registry *clients) Register(
	response http.ResponseWriter,
	request *http.Request,
) {
	registry.register(response, request)
}

func (fetcher *Fetcher) Proxy() func(*http.Request) (*url.URL, error) {
	return fetcher.transport.Proxy
}

func (clients *Clients) SetLogger(logger *slog.Logger) { clients.log = logger }

func (clients *Clients) Allow(addr string, now time.Time) bool {
	return clients.limit.allow(limitKey(addr), now)
}

func (clients *Clients) LimiterSize() int { return len(clients.limit.keys) }
