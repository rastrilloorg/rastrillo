package oidc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Connection is one app's registration with one provider — ported from
// ips's Config, with AllowedDomains added (design D3, review finding 3).
type Connection struct {
	// Issuer is the provider's issuer identifier, scheme included, and is
	// the base the discovery document is fetched from. Provider.Issuer
	// builds it from what an app actually configured.
	Issuer string

	// ClientID is the application registration's identifier. It is the
	// value an ID token's `aud` must contain.
	ClientID string

	// ClientSecret is the registration's secret. Both providers supported
	// here are confidential clients — the code exchange is a
	// server-to-server call carrying this — so PKCE is defence in depth
	// rather than a substitute for it.
	ClientSecret string

	// RedirectURI is where the provider sends the browser back, and must
	// match the registration byte for byte. It must be on the same
	// origin as the *sessions.Sessions this connection's Handlers mints
	// into — an origin that could disagree with the one actually serving
	// requests is an origin that will one day disagree with it (review
	// finding 4).
	RedirectURI string

	// AllowedDomains is which of a shared-issuer provider's customers may
	// sign in through THIS connection, checked against the token's own
	// asserted organisation claim — never against the address. REQUIRED
	// when Provider.NeedsHostedDomain is true (Google today): New and the
	// connection resolver both refuse a connection with none set, because
	// ips's D88.11 found that leaving this to an app's own membership
	// policy is how a Google connection ends up trusting "whoever is on
	// our people list" against an issuer shared with every Gmail account
	// that has ever existed. Unused, and must be empty, for a provider
	// whose issuer already binds one tenant (Microsoft).
	AllowedDomains []string
}

// Metadata is the provider's discovery document, narrowed to the four
// fields this flow uses. Everything else the document carries is
// deliberately not read: a field nothing consults cannot be the reason a
// sign-in behaved unexpectedly.
type Metadata struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	JWKSURI               string `json:"jwks_uri"`
}

// The errors a caller is expected to tell apart. Everything else arrives
// as an *Error carrying the stage it failed at.
var (
	// ErrNotConfigured means the connection is missing something it
	// cannot be operated without.
	ErrNotConfigured = errors.New("oidc: the connection is not configured")

	// ErrMultiTenant means the discovery document refuses to name a
	// single issuer — see Discover.
	ErrMultiTenant = errors.New("oidc: that endpoint would admit accounts from any organisation")

	// ErrInsecure means a URL this flow would have used is not https.
	ErrInsecure = errors.New("oidc: every endpoint in this flow must be https")
)

// Error is a failure with the stage it happened at kept on it, so a
// caller can say which leg of the ceremony broke without matching on
// sentences.
//
// Body is what the provider actually answered, bounded. It is kept
// because a provider's own words about a misconfigured registration are
// worth more to whoever has to fix it than anything this package could
// write instead — and it must only ever be shown to somebody
// administering the connection, never to whoever is trying to sign in
// (see handlers.go's error-path table).
type Error struct {
	Stage   string
	Status  int
	Message string
	Body    string
}

func (e *Error) Error() string {
	msg := e.Message
	if msg == "" && e.Status != 0 {
		msg = fmt.Sprintf("HTTP %d", e.Status)
	}
	if e.Status != 0 {
		return fmt.Sprintf("%s: HTTP %d: %s", e.Stage, e.Status, msg)
	}
	return e.Stage + ": " + msg
}

// Engine performs the outbound calls this flow makes — discovery, the key
// fetch, and the code exchange — and caches what it has fetched across
// EVERY connection, keyed by issuer and jwks_uri (keys.go). Build exactly
// one per process and share it the way Handlers does: a fresh Engine per
// request would mean a fresh, cold cache every time.
//
// The zero value works for anything that does not need a test seam. HTTP
// and Now exist so a test can stand a fake provider up and hold the clock
// still.
type Engine struct {
	// HTTP is the client used for discovery, the JWKS fetch, and the code
	// exchange. Nil gets this package's own default: a 15-second timeout,
	// and redirects disabled (CheckRedirect refuses every hop). A
	// provider's own published endpoints have no reason to redirect, and
	// a client that follows one blindly turns a provider misconfiguration
	// — or a compromised endpoint — into this instance being told where
	// to send a client secret. A caller supplying its own HTTP client
	// (standing up a fake provider for a test, say) takes on deciding
	// that policy itself.
	HTTP *http.Client
	Now  func() time.Time

	cache cache
}

func (e *Engine) httpClient() *http.Client {
	if e.HTTP != nil {
		return e.HTTP
	}
	return &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func (e *Engine) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

// Discover fetches and checks the provider's discovery document, caching
// it under the issuer it was fetched from (keys.go).
//
// Two refusals here are the whole of this flow's tenant binding.
//
// FIRST, the document's own `issuer` must equal the issuer it was fetched
// from. That is the standard's own mix-up defence: a document naming a
// different issuer is either a misconfiguration or an attacker persuading
// this instance to accept tokens signed by somebody else.
//
// SECOND — and this is the one that matters for Microsoft — a discovery
// document whose issuer contains a placeholder is refused by name.
// Entra's multi-tenant endpoints (`/common`, `/organizations`,
// `/consumers`) answer with `"issuer":
// "https://login.microsoftonline.com/{tenantid}/v2.0"`, because they will
// sign tokens for EVERY tenant in the world. A relying party that
// accepted that document would admit an account from any organisation
// that has ever existed. Refusing it is what makes "the firm's identity
// provider" mean one directory rather than Microsoft in general. The
// refusal is at CONFIGURATION time, on a screen, where somebody can act
// on it — not at sign-in, where the person reading it is not the person
// who chose the tenant.
func (e *Engine) Discover(ctx context.Context, issuer string) (Metadata, error) {
	issuer = strings.TrimRight(strings.TrimSpace(issuer), "/")
	if issuer == "" {
		return Metadata{}, ErrNotConfigured
	}
	if err := secure(issuer); err != nil {
		return Metadata{}, err
	}

	slot := e.cache.metaSlotFor(issuer)
	slot.mu.Lock()
	defer slot.mu.Unlock()
	if e.now().Sub(slot.at) < metaTTL && !slot.meta.isZero() {
		return slot.meta, nil
	}

	body, status, err := e.fetch(ctx, "discovery", issuer+"/.well-known/openid-configuration")
	if err != nil {
		return Metadata{}, err
	}

	var m Metadata
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		return Metadata{}, &Error{Stage: "discovery", Status: status,
			Message: "the discovery document could not be read as JSON", Body: body}
	}

	if strings.ContainsAny(m.Issuer, "{}") {
		return Metadata{}, fmt.Errorf("%w: it names its issuer as %q, which is a placeholder rather than one organisation",
			ErrMultiTenant, m.Issuer)
	}
	if strings.TrimRight(m.Issuer, "/") != issuer {
		return Metadata{}, &Error{Stage: "discovery", Status: status,
			Message: fmt.Sprintf("the document at %s says its issuer is %q, and the two must be the same", issuer, m.Issuer)}
	}
	for _, ep := range []struct{ name, value string }{
		{"authorization_endpoint", m.AuthorizationEndpoint},
		{"token_endpoint", m.TokenEndpoint},
		{"jwks_uri", m.JWKSURI},
	} {
		if ep.value == "" {
			return Metadata{}, &Error{Stage: "discovery", Status: status,
				Message: "the discovery document has no " + ep.name}
		}
		if err := secure(ep.value); err != nil {
			return Metadata{}, fmt.Errorf("%w (%s)", err, ep.name)
		}
	}
	m.Issuer = strings.TrimRight(m.Issuer, "/")
	// Cached only after every check above has passed, so a document this
	// package would refuse is never served from memory as one it
	// accepted.
	slot.meta, slot.at = m, e.now()
	return m, nil
}

func (m Metadata) isZero() bool { return m.Issuer == "" }

// AuthCodeURL is where the browser is sent to start the ceremony.
//
// The three unguessable values in Start are all bound to this one
// attempt and each closes a different door: `state` is compared on the
// way back and is what stops somebody else's completed ceremony being
// replayed into this browser; `nonce` is inside the signature and stops a
// token minted for another attempt being presented to this one; and the
// PKCE challenge stops an intercepted authorization code being exchanged
// by anybody who does not hold the verifier.
//
// `prompt` is the caller's to set (handlers.go chooses "select_account"
// ordinarily and "login" with "max_age=0" on a step-up — design D6):
// without SOME prompt, a person already signed in to a personal account
// in the same browser would be silently signed in here as that account,
// which then fails the admission gate with a refusal naming an address
// they did not choose to use.
func AuthCodeURL(m Metadata, conn Connection, s Start, prompt string) string {
	q := url.Values{
		"client_id":             {conn.ClientID},
		"response_type":         {"code"},
		"redirect_uri":          {conn.RedirectURI},
		"scope":                 {"openid email profile"},
		"state":                 {s.State},
		"nonce":                 {s.Nonce},
		"code_challenge":        {s.Challenge()},
		"code_challenge_method": {"S256"},
		// Query, not fragment (never reaches the server) and not
		// form_post. form_post is not rejected because of form-action —
		// a cross-site POST arriving at Return is a SameSite/CSRF
		// question, not a CSP one on OUR earlier response — it is simply
		// not needed: query keeps the whole ceremony a chain of GETs
		// with nothing new for csrf.Protect to carry an exception for
		// (design D7).
		"response_mode": {"query"},
	}
	if prompt != "" {
		q.Set("prompt", prompt)
	}
	sep := "?"
	if strings.Contains(m.AuthorizationEndpoint, "?") {
		sep = "&"
	}
	return m.AuthorizationEndpoint + sep + q.Encode()
}

// Exchange turns the authorization code into a verified identity.
//
// It is one call and not two on purpose: there is no point at which a
// caller holds an unverified token, so there is no way for a caller to
// forget the verification step. The nonce is an argument rather than
// something read back out of the token for the same reason.
func (e *Engine) Exchange(ctx context.Context, m Metadata, conn Connection, code string, s Start) (Identity, error) {
	if conn.ClientID == "" || conn.ClientSecret == "" || conn.RedirectURI == "" {
		return Identity{}, ErrNotConfigured
	}
	if code == "" {
		return Identity{}, &Error{Stage: "exchange", Message: "the provider returned no authorization code"}
	}

	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {conn.RedirectURI},
		"client_id":     {conn.ClientID},
		"code_verifier": {s.Verifier},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return Identity{}, &Error{Stage: "exchange", Message: err.Error()}
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	// The secret goes in the Authorization header and nowhere else: one
	// place for a secret is one place it can be leaked from. Both
	// providers here accept client_secret_basic. The values are
	// form-encoded first, which the specification requires for this
	// header and which matters in practice — a client secret routinely
	// contains characters that would otherwise change what the header
	// says.
	req.SetBasicAuth(url.QueryEscape(conn.ClientID), url.QueryEscape(conn.ClientSecret))

	resp, err := e.httpClient().Do(req)
	if err != nil {
		return Identity{}, &Error{Stage: "exchange", Message: err.Error()}
	}
	defer resp.Body.Close()
	body := readBody(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return Identity{}, &Error{Stage: "exchange", Status: resp.StatusCode,
			Message: oauthMessage(body), Body: body}
	}

	var tok struct {
		IDToken string `json:"id_token"`
	}
	if err := json.Unmarshal([]byte(body), &tok); err != nil || tok.IDToken == "" {
		// A 200 carrying no ID token is not a sign-in. "The response was
		// not empty" is not a success test — a proxy's error page is a
		// non-empty 200.
		return Identity{}, &Error{Stage: "exchange", Status: resp.StatusCode,
			Message: "the token response held no id_token", Body: body}
	}

	return e.Verify(ctx, m, conn, tok.IDToken, s.Nonce)
}

// fetch is the one shape of GET this package makes.
func (e *Engine) fetch(ctx context.Context, stage, from string) (body string, status int, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, from, nil)
	if err != nil {
		return "", 0, &Error{Stage: stage, Message: err.Error()}
	}
	req.Header.Set("Accept", "application/json")
	resp, err := e.httpClient().Do(req)
	if err != nil {
		return "", 0, &Error{Stage: stage, Message: err.Error()}
	}
	defer resp.Body.Close()
	body = readBody(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", resp.StatusCode, &Error{Stage: stage, Status: resp.StatusCode,
			Message: oauthMessage(body), Body: body}
	}
	return body, resp.StatusCode, nil
}

// secure refuses anything that is not https.
//
// There is no development exception. A sign-in ceremony carrying a
// client secret and an identity assertion over plain HTTP is not a
// weaker version of this flow, it is a different one. Tests stand their
// fake provider up over TLS for exactly that reason.
func secure(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%w: %q could not be read as a URL", ErrInsecure, raw)
	}
	if u.Scheme != "https" || u.Host == "" {
		return fmt.Errorf("%w: %q", ErrInsecure, raw)
	}
	return nil
}

// oauthMessage prefers the provider's own words about a failure.
func oauthMessage(body string) string {
	var oe struct {
		Err  string `json:"error"`
		Desc string `json:"error_description"`
	}
	_ = json.Unmarshal([]byte(body), &oe)
	if d := strings.TrimSpace(oe.Desc); d != "" {
		return d
	}
	if e := strings.TrimSpace(oe.Err); e != "" {
		return e
	}
	return ""
}

// readBody reads a response whole, bounded.
//
// A megabyte: every document this package reads is small JSON, and the
// bound is what stops a far end answering with something enormous from
// becoming this process's memory problem.
func readBody(r io.Reader) string {
	b, _ := io.ReadAll(io.LimitReader(r, 1<<20))
	return string(b)
}
