package oidc

import (
	"crypto/rsa"
	"sync"
	"time"
)

// cache holds every discovery document and signing-key set this Engine
// has fetched, keyed by ISSUER and by JWKS URI respectively — never by
// Kind or by any per-connection identifier.
//
// This is the fix for the review's finding 5: ips caches per Client value
// because ips has exactly one connection per provider per instance. This
// package is shared by every connection of an app, and a hub may hold many
// tenants under one Kind (many Entra tenants, say). A cache keyed by Kind
// would let one tenant's signing keys verify another tenant's tokens —
// concretely, Entra lets a tenant admin configure a custom signing key for
// an app using claims mapping, so a shared-by-kind cache is a real
// escalation path. Keying by the issuer/jwks_uri a document or key set was
// actually fetched FROM is what makes that impossible: two different
// issuers can never collide on one cache entry.
//
// Each slot has its own mutex, so a cold tenant's fetch does not block
// every other tenant's sign-in — only concurrent sign-ins against the
// SAME issuer or the same jwks_uri queue behind one fetch, which is the
// single-flight property the review asked for (no N simultaneous fetches
// for one cold cache entry).
//
// What this does NOT do: share a FAILED fetch. A hung or erroring
// provider is not cached, so N sign-ins against one cold, broken issuer
// each wait out their own fetch in turn rather than failing together —
// bounded by the HTTP client's own 15-second timeout per waiter, not
// unbounded, but still N times the latency a true single-flight-with-
// shared-result would give (code review). Accepted here: caching a
// failure risks caching a TRANSIENT one past the moment the provider
// recovers, and that trade was not worth taking for what is, in
// practice, a rare path — a provider outage during the brief window
// several sign-ins happen to overlap one cold cache entry.
type cache struct {
	mu    sync.Mutex
	metas map[string]*metaSlot
	keys  map[string]*keySlot
}

type metaSlot struct {
	mu   sync.Mutex
	meta Metadata
	at   time.Time
}

type keySlot struct {
	mu      sync.Mutex
	fetched time.Time
	keys    map[string]*rsa.PublicKey
}

// metaTTL is deliberately shorter than keysTTL: a stale key set repairs
// itself on the first token naming a key id it does not hold, and a stale
// endpoint list has no such signal.
const metaTTL = 10 * time.Minute

// keysTTL is how long a fetched signing-key set is trusted without asking
// again. An hour, and it is a ceiling rather than the mechanism: a key id
// missing from the cached set refetches immediately, and so does a
// signature that will not check out. The TTL only bounds how long a key
// WITHDRAWN by the provider goes on being accepted, which is the one case
// no miss can detect.
const keysTTL = time.Hour

func (c *cache) metaSlotFor(issuer string) *metaSlot {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.metas == nil {
		c.metas = map[string]*metaSlot{}
	}
	s, ok := c.metas[issuer]
	if !ok {
		s = &metaSlot{}
		c.metas[issuer] = s
	}
	return s
}

func (c *cache) keySlotFor(jwksURI string) *keySlot {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.keys == nil {
		c.keys = map[string]*keySlot{}
	}
	s, ok := c.keys[jwksURI]
	if !ok {
		s = &keySlot{}
		c.keys[jwksURI] = s
	}
	return s
}

// pick reads one key out of a fetched set. The caller holds the slot's
// lock.
//
// An empty kid matches the set only when the set holds exactly one key: a
// token that does not say which key signed it, against a provider
// publishing several, is one this package will not guess at.
func pickKey(keys map[string]*rsa.PublicKey, kid string) *rsa.PublicKey {
	if keys == nil {
		return nil
	}
	if kid == "" {
		if len(keys) != 1 {
			return nil
		}
		for _, k := range keys {
			return k
		}
	}
	return keys[kid]
}
