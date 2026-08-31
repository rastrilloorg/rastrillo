// Package vault is rastrillo's client half of the Pegamento vault
// facet: one person's named sealed blobs and per-method wrapped seed
// on a home service the app's operator may not run. The server is
// Pegamento's (amadan.net/carlos/pegamento); this package speaks its
// v1 wire, seals and opens locally so plaintext never crosses the
// package boundary, and enforces the closed blob namespace before any
// request leaves the process.
//
// Two rulings bind everything here, both inherited from the doctrine.
// Strictly additive: an app that configures no home constructs no
// Client and makes no request, ever — there is no default home, no
// probe, no "check for a backup". Closed namespace: every blob name
// this app may touch is declared at construction, and an undeclared
// name refuses locally with ErrUndeclared, before any dial.
//
// Design: docs/superpowers/specs/2026-08-27-vault-client-design.md.
package vault

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"amadan.net/rastrillo/rastrillo/crypto"
	"amadan.net/rastrillo/rastrillo/keyring"
)

// Create is the version sentinel for a blob that must not exist yet:
// Put(name, plaintext, Create) succeeds only if the name has never
// been written. Every other version string is opaque — compared for
// equality by the server, never parsed, incremented, or ordered by
// this client.
const Create = "0"

// ErrUndeclared is returned when a blob name outside Config.Blobs is
// used — the closed namespace enforced locally, before any request.
var ErrUndeclared = errors.New("rastrillo/vault: undeclared blob name")

// ErrStale is Put's compare-and-swap refusal: the blob changed since
// the version the caller read. Current is the version to re-read,
// merge onto, and retry with.
type ErrStale struct{ Current string }

func (e ErrStale) Error() string {
	return "rastrillo/vault: blob changed (current version " + e.Current + "); re-read, merge, retry"
}

// Config configures New. Everything is required except Client: a
// vault Client is one person's bound home session, not a service
// handle — construct it when you hold a token and an unwrapped seed,
// never at boot.
type Config struct {
	// Home is the home service's origin, scheme included —
	// "https://home.example.net". There is no default: an app with no
	// home configured never constructs this package.
	Home string

	// Ring is the app's keyring namespace; BlobKey(seed, name) under
	// it seals each named blob.
	Ring keyring.Ring

	// Blobs is the closed namespace: every name this app may touch,
	// each matching [a-z0-9-]{1,64}. An undeclared name refuses
	// locally with ErrUndeclared.
	Blobs []string

	// Token is the person's home link token, sent as a bearer.
	Token string

	// Seed is the person's unwrapped 32-byte seed. It never leaves
	// the process; only ciphertext crosses the wire.
	Seed []byte

	// Client is the HTTP client; nil gets a 10-second timeout default.
	Client *http.Client
}

// Client speaks the Pegamento vault v1 wire for one person. Build one
// per unwrapped home session with New.
type Client struct {
	cfg      Config
	declared map[string]bool
}

// New validates cfg and returns a ready *Client. It makes no request:
// construction is inert, per the strictly-additive rule.
func New(cfg Config) (*Client, error) {
	if !strings.HasPrefix(cfg.Home, "https://") && !strings.HasPrefix(cfg.Home, "http://") {
		return nil, errors.New("rastrillo/vault: Config.Home must be an absolute origin like https://home.example.net")
	}
	if cfg.Ring.Namespace == "" {
		return nil, errors.New("rastrillo/vault: Config.Ring needs a namespace")
	}
	if len(cfg.Blobs) == 0 {
		return nil, errors.New("rastrillo/vault: Config.Blobs is the closed namespace and must declare at least one name")
	}
	declared := make(map[string]bool, len(cfg.Blobs))
	for _, name := range cfg.Blobs {
		if !validName(name) {
			return nil, fmt.Errorf("rastrillo/vault: blob name %q outside [a-z0-9-]{1,64}", name)
		}
		declared[name] = true
	}
	if cfg.Token == "" {
		return nil, errors.New("rastrillo/vault: Config.Token is required — a Client is a bound home session")
	}
	if len(cfg.Seed) != 32 {
		return nil, errors.New("rastrillo/vault: Config.Seed must be the person's 32-byte seed")
	}
	if cfg.Client == nil {
		cfg.Client = &http.Client{Timeout: 10 * time.Second}
	}
	cfg.Home = strings.TrimSuffix(cfg.Home, "/")
	return &Client{cfg: cfg, declared: declared}, nil
}

// validName is the closed namespace's character rule: [a-z0-9-]{1,64}.
// The name lands inside a derivation context string (keyring.BlobKey)
// and a URL path, and this one rule keeps it inert in both.
func validName(name string) bool {
	if len(name) == 0 || len(name) > 64 {
		return false
	}
	for _, c := range name {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
			return false
		}
	}
	return true
}

// declaredOr returns ErrUndeclared for a name outside the closed
// namespace — the local half of the two-ended check.
func (c *Client) declaredOr(name string) error {
	if !c.declared[name] {
		return fmt.Errorf("%w: %q", ErrUndeclared, name)
	}
	return nil
}

// Get fetches and opens one named blob, returning its plaintext and
// the opaque version to Put against.
func (c *Client) Get(ctx context.Context, name string) (plaintext []byte, version string, err error) {
	if err := c.declaredOr(name); err != nil {
		return nil, "", err
	}
	var w blobWire
	if err := c.do(ctx, http.MethodGet, "/v1/blobs/"+name, nil, &w); err != nil {
		return nil, "", err
	}
	padded, err := crypto.OpenSym(c.cfg.Ring.BlobKey(c.cfg.Seed, name), w.Sealed)
	if err != nil {
		return nil, "", fmt.Errorf("rastrillo/vault: open %q: %w", name, err)
	}
	plaintext, err = unwrap(padded)
	if err != nil {
		return nil, "", err
	}
	return plaintext, w.Version, nil
}

// Put seals plaintext and writes it as the named blob, requiring the
// version last read (or Create for a blob that must not exist yet).
// An ErrStale return carries the current version for the caller's
// re-read, merge, retry loop.
func (c *Client) Put(ctx context.Context, name string, plaintext []byte, version string) (newVersion string, err error) {
	return c.put(ctx, name, plaintext, 0, version)
}

// PutPadded is Put with a pad target: the sealed length is uniform
// for any plaintext up to target-4 bytes, so a blob whose size leaks
// (a server list, say) leaks nothing. The target is a floor — an
// oversize plaintext still round-trips, just unpadded.
func (c *Client) PutPadded(ctx context.Context, name string, plaintext []byte, target int, version string) (newVersion string, err error) {
	return c.put(ctx, name, plaintext, target, version)
}

func (c *Client) put(ctx context.Context, name string, plaintext []byte, target int, version string) (string, error) {
	if err := c.declaredOr(name); err != nil {
		return "", err
	}
	sealed, err := crypto.SealSym(c.cfg.Ring.BlobKey(c.cfg.Seed, name), wrap(plaintext, target))
	if err != nil {
		return "", err
	}
	var w blobWire
	ctx = context.WithValue(ctx, ifMatchKey{}, version)
	if err := c.do(ctx, http.MethodPut, "/v1/blobs/"+name, blobWire{Sealed: sealed}, &w); err != nil {
		return "", err
	}
	return w.Version, nil
}
