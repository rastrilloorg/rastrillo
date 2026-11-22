package oauth

import (
	"container/list"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"amadan.net/rastrillo/rastrillo/clientip"
)

const (
	registerLimit  = 10
	registerWindow = time.Minute
	unusedLife     = 30 * 24 * time.Hour
	pruneEvery     = time.Hour
	maxRegistered  = 64 << 10
	maxRedirects   = 10
	maxNameRunes   = 100
)

// clients finds the client behind a client_id and registers new ones.
type clients struct {
	store Store
	fetch *Fetcher
	limit *limiter
	now   func() time.Time
	// hops is how many proxies sit in front of the app, for clientip.
	hops int
	log  *slog.Logger

	mu     sync.Mutex
	pruned time.Time
}

func newClients(store Store, hops int) *clients {
	return &clients{
		store: store, fetch: NewFetcher(), now: time.Now, hops: hops,
		log:   slog.Default(),
		limit: &limiter{max: registerLimit, window: registerWindow},
	}
}

// resolve returns the client for id: a metadata document when id is an
// https URL with a path, a registered client otherwise.
func (registry *clients) resolve(
	ctx context.Context, id string,
) (Client, error) {
	if documentID(id) {
		return registry.fetch.Fetch(ctx, id)
	}
	return registry.store.Client(ctx, id)
}

func documentID(id string) bool {
	return strings.HasPrefix(id, "https://") && strings.Contains(id[8:], "/")
}

type registration struct {
	ClientName   string   `json:"client_name"`
	RedirectURIs []string `json:"redirect_uris"`
	GrantTypes   []string `json:"grant_types"`
	ResponseType []string `json:"response_types"`
	AuthMethod   string   `json:"token_endpoint_auth_method"`
}

// register answers POST /oauth/register (RFC 7591) for public clients.
func (registry *clients) register(
	response http.ResponseWriter, request *http.Request,
) {
	now := registry.now()
	address := clientip.From(request, registry.hops)
	if !registry.limit.allow(limitKey(address), now) {
		response.Header().Set("Retry-After",
			strconv.Itoa(int(registerWindow/time.Second)))
		oauthError(response, http.StatusTooManyRequests, "rate_limited",
			"Registration limit reached. Try again in one minute.")
		return
	}
	req, ok := readRegistration(response, request)
	if !ok {
		return
	}
	// The answer says what every client gets, whatever was asked.
	req.GrantTypes = []string{"authorization_code", "refresh_token"}
	req.ResponseType = []string{"code"}
	req.AuthMethod = "none"
	id, err := newID()
	if err == nil {
		err = registry.store.SaveClient(request.Context(), Client{
			ID: id, Name: req.ClientName, Redirects: req.RedirectURIs,
			RegisteredAt: now, UsedAt: now,
		})
	}
	if err != nil {
		oauthError(response, http.StatusInternalServerError, "server_error",
			"The server could not save the registration.")
		return
	}
	registry.prune(request.Context(), now)
	writeJSON(response, http.StatusCreated, struct {
		ClientID string `json:"client_id"`
		registration
	}{id, req})
}

// readRegistration reads and checks the metadata. Otherwise it has
// answered with the error.
func readRegistration(
	response http.ResponseWriter, request *http.Request,
) (registration, bool) {
	var req registration
	body := http.MaxBytesReader(response, request.Body, maxRegistered)
	dec := json.NewDecoder(body)
	err := dec.Decode(&req)
	if err == nil && dec.Decode(&struct{}{}) != io.EOF {
		err = errors.New("trailing data")
	}
	if err != nil {
		oauthError(response, http.StatusBadRequest, "invalid_client_metadata",
			"The request body must be a JSON object containing "+
				"client metadata.")
		return req, false
	}
	if code, msg := req.problem(); code != "" {
		oauthError(response, http.StatusBadRequest, code, msg)
		return req, false
	}
	return req, true
}

// problem returns the RFC 7591 error code and a description for the
// first thing wrong with the metadata, or no code.
func (metadata registration) problem() (code, description string) {
	if !validName(metadata.ClientName) {
		return "invalid_client_metadata",
			"client_name is required and must be 100 characters or " +
				"fewer, with no control characters."
	}
	if len(metadata.RedirectURIs) == 0 ||
		len(metadata.RedirectURIs) > maxRedirects {
		return "invalid_redirect_uri",
			"redirect_uris must contain 1 to 10 entries."
	}
	for _, uri := range metadata.RedirectURIs {
		if _, ok := parseRedirect(uri); !ok {
			return "invalid_redirect_uri", "Each redirect_uris entry " +
				"must be an HTTPS URL or an HTTP URL whose host is " +
				"127.0.0.1, [::1], or localhost. URLs must not include " +
				"a fragment, whitespace, quotes, or angle brackets."
		}
	}
	if !subset(metadata.GrantTypes, "authorization_code", "refresh_token") {
		return "invalid_client_metadata", "grant_types may contain only " +
			"authorization_code and refresh_token."
	}
	if !subset(metadata.ResponseType, "code") {
		return "invalid_client_metadata",
			"response_types may contain only code."
	}
	if metadata.AuthMethod != "" && metadata.AuthMethod != "none" {
		return "invalid_client_metadata",
			`token_endpoint_auth_method must be "none".`
	}
	return "", ""
}

func subset(got []string, allowed ...string) bool {
	for _, item := range got {
		if !slices.Contains(allowed, item) {
			return false
		}
	}
	return true
}

// prune deletes clients unused for 30 days, at most once an hour.
func (registry *clients) prune(ctx context.Context, now time.Time) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if now.Sub(registry.pruned) < pruneEvery {
		return
	}
	err := registry.store.PruneClients(ctx, now.Add(-unusedLife))
	if err != nil {
		registry.log.Error("oauth: pruning unused clients", "err", err)
		return
	}
	registry.pruned = now
}

const base62 = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ" +
	"abcdefghijklmnopqrstuvwxyz"

// newID makes a random identifier: a client, connection or approval.
func newID() (string, error) {
	id := make([]byte, 22)
	for index := range id {
		digit, err := rand.Int(rand.Reader, big.NewInt(int64(len(base62))))
		if err != nil {
			return "", err
		}
		id[index] = base62[digit.Int64()]
	}
	return string(id), nil
}

// oauthError writes the JSON error of RFC 6749 §5.2, which
// registration and revocation share.
func oauthError(response http.ResponseWriter, status int, code, msg string) {
	writeJSON(response, status, map[string]string{
		"error": code, "error_description": msg,
	})
}

func writeJSON(response http.ResponseWriter, status int, body any) {
	response.Header().Set("Content-Type", "application/json")
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(body)
}

// validName is the one rule for a client's display name, whether it came
// from a registration or a document: it reaches the approval page.
func validName(name string) bool {
	return name != "" && utf8.RuneCountInString(name) <= maxNameRunes &&
		strings.IndexFunc(name, func(character rune) bool {
			return unicode.IsControl(character) ||
				unicode.Is(unicode.Cf, character)
		}) < 0
}

// limitKey buckets an IPv6 address by its /64, since one subscriber
// holds a whole /64 and could otherwise mint unlimited keys.
func limitKey(addr string) string {
	ip, err := netip.ParseAddr(addr)
	if err != nil || ip.Is4() || ip.Is4In6() {
		return addr
	}
	prefix, _ := ip.Prefix(64)
	return prefix.String()
}

type bucket struct {
	key  string
	hits []time.Time
}

// limiter allows max events per key in any window, in memory. It holds
// at most maxKeys keys; a new key past that evicts the oldest.
type limiter struct {
	max    int
	window time.Duration

	mu    sync.Mutex
	keys  map[string]*list.Element
	order *list.List
}

const maxKeys = 4096

func (limits *limiter) allow(key string, now time.Time) bool {
	limits.mu.Lock()
	defer limits.mu.Unlock()
	if limits.keys == nil {
		limits.keys, limits.order = map[string]*list.Element{}, list.New()
	}
	el, ok := limits.keys[key]
	if !ok {
		if limits.order.Len() >= maxKeys {
			oldest := limits.order.Front()
			delete(limits.keys, oldest.Value.(*bucket).key)
			limits.order.Remove(oldest)
		}
		el = limits.order.PushBack(&bucket{key: key})
		limits.keys[key] = el
	}
	counter := el.Value.(*bucket)
	for len(counter.hits) > 0 && now.Sub(counter.hits[0]) >= limits.window {
		counter.hits = counter.hits[1:]
	}
	if len(counter.hits) >= limits.max {
		return false
	}
	counter.hits = append(counter.hits, now)
	return true
}
