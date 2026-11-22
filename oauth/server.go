package oauth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	rastrillo "amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/api"
	"amadan.net/rastrillo/rastrillo/apitoken"
	"amadan.net/rastrillo/rastrillo/sessions"
)

const (
	doorJSON = "/api/v1"
	doorMCP  = "/api/v1/mcp"
)

// Config wires a Server to an app.
type Config struct {
	Store    Store
	Tokens   *apitoken.Store
	Origin   string // "https://patrix.cc"
	Base     string // "" or "/tenant"
	SiteName string
	Session  func(*http.Request) (sessions.Session, bool)
	// SignIn is where to send a person who must sign in, or sign in
	// again (reauth), to come back to returnTo: a local path with
	// its query, never an absolute URL.
	SignIn func(returnTo string, reauth bool) string
	// Fresh is how recent a sign-in must be to approve; 0 means ten
	// minutes.
	Fresh   time.Duration
	Allowed func(
		ctx context.Context, subject string,
	) (ok bool, refusal string, err error)
	Render func(response http.ResponseWriter, request *http.Request,
		page Page) error
	// Fetcher fetches client metadata documents; nil uses NewFetcher.
	Fetcher *Fetcher
	// Hops is how many proxies sit in front of the app, for clientip.
	Hops   int
	Logger *slog.Logger
	Now    func() time.Time
}

// Route is one route the server answers, for the app to register.
type Route struct {
	Method, Pattern string       // Pattern includes Base
	Handler         http.Handler // no-store already applied
	Reason          string       // a PageOnly reason for an app's route ledger
	Browser         bool         // needs the app's session and CSRF middleware
}

// Server is an app's OAuth authorization server.
type Server struct {
	cfg     Config
	issuer  string
	clients *clients
}

// New checks config and builds the server.
func New(config Config) (*Server, error) {
	switch {
	case config.Store == nil:
		return nil, errors.New("oauth: Config.Store is required")
	case config.Tokens == nil:
		return nil, errors.New("oauth: Config.Tokens is required")
	case config.Session == nil:
		return nil, errors.New("oauth: Config.Session is required")
	case config.SignIn == nil:
		return nil, errors.New("oauth: Config.SignIn is required")
	case config.Allowed == nil:
		return nil, errors.New("oauth: Config.Allowed is required")
	case config.Render == nil:
		return nil, errors.New("oauth: Config.Render is required")
	}
	if err := checkOrigin(config.Origin); err != nil {
		return nil, err
	}
	if err := checkBase(config.Base); err != nil {
		return nil, err
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.Fresh == 0 {
		config.Fresh = 10 * time.Minute
	}
	if config.Logger == nil {
		config.Logger = slog.Default()
	}
	cl := newClients(config.Store, config.Hops)
	cl.now, cl.log = config.Now, config.Logger
	if config.Fetcher != nil {
		cl.fetch = config.Fetcher
	}
	return &Server{cfg: config, issuer: config.Origin + config.Base,
		clients: cl}, nil
}

// checkOrigin wants scheme and host alone: https, or http on loopback.
func checkOrigin(origin string) error {
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" || parsed.Path != "" ||
		parsed.RawQuery != "" ||
		parsed.Fragment != "" || parsed.User != nil {
		return fmt.Errorf("oauth: Config.Origin %q must be a scheme and "+
			"host, like https://example.com", origin)
	}
	loopback := slices.Contains(
		[]string{"127.0.0.1", "::1", "localhost"}, parsed.Hostname())
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && loopback) {
		return fmt.Errorf("oauth: Config.Origin %q must be https, or "+
			"http on a loopback address", origin)
	}
	return nil
}

// checkBase wants "" or a plain path: anything else would panic in
// ServeMux at Mount, or name a different path in the issuer.
func checkBase(base string) error {
	if base == "" {
		return nil
	}
	segments := strings.Split(base, "/")
	ok := segments[0] == ""
	for _, segment := range segments[1:] {
		ok = ok && segment != "" && segment != "." && segment != ".." &&
			strings.Trim(segment, unreserved) == ""
	}
	if !ok {
		return fmt.Errorf("oauth: Config.Base %q must start with a "+
			"slash, must not end with one, and may contain only "+
			"letters, digits, and -._~ in its segments", base)
	}
	return nil
}

const unreserved = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz" +
	"0123456789-._~"

// Issuer is the authorization server's identifier: origin and base.
func (server *Server) Issuer() string { return server.issuer }

// Challenge is what door's 401 tells a client: where its protected
// resource metadata is, and the scopes to ask for.
func (server *Server) Challenge(door string) api.Challenge {
	return api.Challenge{
		ResourceMetadata: server.issuer +
			"/.well-known/oauth-protected-resource" + door,
		Scope: strings.Join(scopes, " "),
	}
}

// Routes lists everything the server answers. Only the authorize routes
// are for a browser; the rest are called across origins by clients.
func (server *Server) Routes() []Route {
	base := server.cfg.Base
	const wk = "/.well-known/"
	const reason = "OAuth protocol endpoint for clients, not for agents"
	const human = "the approval page is for a person in a browser"
	routes := []Route{
		{"GET", base + wk + "oauth-protected-resource",
			server.resourceDocument(doorMCP), reason, false},
		{"GET", base + wk + "oauth-protected-resource" + doorMCP,
			server.resourceDocument(doorMCP), reason, false},
		{"GET", base + wk + "oauth-protected-resource" + doorJSON,
			server.resourceDocument(doorJSON), reason, false},
		{"GET", base + wk + "oauth-authorization-server",
			server.serverDocument(), reason, false},
		{"GET", base + wk + "openid-configuration",
			server.serverDocument(), reason, false},
		{"GET", base + "/oauth/authorize",
			noFrame(http.HandlerFunc(server.authorize)), human, true},
		{"POST", base + "/oauth/authorize",
			noFrame(http.HandlerFunc(server.answer)), human, true},
		{"POST", base + "/oauth/token",
			http.HandlerFunc(server.token), reason, false},
		{"POST", base + "/oauth/register",
			http.HandlerFunc(server.clients.register), reason, false},
		{"POST", base + "/oauth/revoke",
			http.HandlerFunc(server.revoke), reason, false},
	}
	for index := range routes {
		routes[index].Handler = noStore(routes[index].Handler)
	}
	return routes
}

func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(
		response http.ResponseWriter, request *http.Request,
	) {
		response.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(response, request)
	})
}

// noFrame keeps the approval page out of other sites' frames, where a
// hidden Approve could be clicked: a framed form posts as same-origin,
// so the CSRF check can't catch it. The CSP is Added, not Set, so an
// app's own policy stays in force alongside it.
func noFrame(next http.Handler) http.Handler {
	return http.HandlerFunc(func(
		response http.ResponseWriter, request *http.Request,
	) {
		response.Header().Set("X-Frame-Options", "DENY")
		response.Header().Add("Content-Security-Policy",
			"frame-ancestors 'none'")
		next.ServeHTTP(response, request)
	})
}

// Mount registers every route on mux, for apps with no router of their
// own. The browser routes get no session or CSRF middleware here.
func (server *Server) Mount(mux *http.ServeMux) {
	for _, route := range server.Routes() {
		mux.Handle(route.Method+" "+route.Pattern, route.Handler)
	}
}

// Grant says what a connection's token may do, for
// apitoken.Config.Grant: its scopes follow the token's access, which a
// refresh may have narrowed below the connection's. A connection that
// has ended, that is for a resource this app does not serve, or whose
// person is no longer allowed, is refused as unauthorized; a store that
// fails is unavailable.
func (server *Server) Grant(
	ctx context.Context, connection string, access rastrillo.Access,
) (*rastrillo.Grant, error) {
	found, err := server.cfg.Store.Connection(ctx, connection)
	if errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("%w: no connection %s",
			api.ErrUnauthorized, connection)
	}
	if err != nil {
		return nil, fmt.Errorf("%w: oauth: connection %s: %w",
			api.ErrUnavailable, connection, err)
	}
	if found.Ended != nil {
		return nil, fmt.Errorf("%w: connection %s has ended",
			api.ErrUnauthorized, connection)
	}
	if found.Resource != server.issuer+doorMCP &&
		found.Resource != server.issuer+doorJSON {
		return nil, fmt.Errorf("%w: connection %s is for %s",
			api.ErrUnauthorized, connection, found.Resource)
	}
	ok, _, err := server.cfg.Allowed(ctx, found.Subject)
	if err != nil {
		return nil, fmt.Errorf("%w: oauth: allowed %s: %w",
			api.ErrUnavailable, found.Subject, err)
	}
	if !ok {
		return nil, fmt.Errorf("%w: %s is no longer allowed",
			api.ErrUnauthorized, found.Subject)
	}
	granted := []string{"read"}
	if access == rastrillo.Write && found.Access == rastrillo.Write {
		granted = scopes
	}
	return &rastrillo.Grant{
		ID: found.ID, Issuer: server.issuer, IdentityIssuer: server.issuer,
		Audience: found.Resource, Scopes: slices.Clone(granted),
	}, nil
}
