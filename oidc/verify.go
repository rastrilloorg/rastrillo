package oidc

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"
)

// Verifying an ID token, and the exact list of what is checked.
//
// A JWT is a string somebody handed us. Everything inside it is a claim
// by whoever wrote it, and the only thing that makes any of it evidence
// is the signature — so the order below is not a style choice. The
// signature is checked FIRST, against a key fetched from the issuer's
// own published set, and nothing inside the token is read for any
// purpose before that succeeds. In particular the `kid` and `alg` in the
// header are used to SELECT a key and are never allowed to decide the
// algorithm: alg is compared against the one algorithm this package
// implements and anything else is refused by name. The header struct
// below decodes exactly `alg` and `kid` — nothing reads `jku`, `jwk` or
// `x5u`, so a token claiming to carry its own key is inert by omission.
//
// After the signature, five claims are checked and each closes
// something:
//
//	iss    must equal the discovery document's issuer. Together with
//	       Discover's refusal of a placeholder issuer, this is what
//	       binds a sign-in to ONE organisation's directory.
//	aud    must contain this connection's client id. A token minted for
//	       a different application, by the same issuer, is not a token
//	       for us.
//	nonce  must equal the one minted for this ceremony. A token replayed
//	       from another attempt fails here even if everything else about
//	       it is genuine.
//	exp    must not have passed, nbf must have arrived.
//	iat    must not be implausibly far in the future.
//
// `azp` is checked only where the specification requires it — when `aud`
// carries more than one value, in which case `azp` must be present and
// must be this client. A single-audience token needs none.
//
// What is deliberately NOT checked: `at_hash`, which binds an access
// token this package never asks for and never uses.

// skew is the clock tolerance on every time comparison.
//
// Two minutes. Somebody signing in on a laptop whose clock is a minute
// out must not be refused with a message about time, and two minutes
// does not meaningfully extend the life of a token whose whole purpose
// is spent within seconds of being minted.
const skew = 2 * time.Minute

// ErrToken is any refusal of an ID token: a bad signature, a claim that
// does not check out, a shape that will not parse.
//
// They are one error on purpose, for the same reason a sign-in screen
// gives one refusal: telling a caller which check missed tells whoever
// is probing which of their guesses was closer. The DETAIL is wrapped for
// an operator's log; a caller is expected to show the person one
// sentence (see handlers.go's error-path table).
var ErrToken = errors.New("oidc: that sign-in could not be verified")

// Verify checks a raw ID token and returns what it said.
//
// It is exported separately from Exchange so a test can drive it with a
// token it minted itself, and for no other reason: nothing in this
// package's own HTTP wiring calls it directly, because a caller holding
// an unverified token is a caller who can forget to verify it.
func (e *Engine) Verify(ctx context.Context, m Metadata, conn Connection, raw, nonce string) (Identity, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return Identity{}, fmt.Errorf("%w: it is not a three-part JWT", ErrToken)
	}

	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return Identity{}, fmt.Errorf("%w: its header is not base64url", ErrToken)
	}
	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return Identity{}, fmt.Errorf("%w: its header is not JSON", ErrToken)
	}
	// The one algorithm, and everything else refused BY NAME. This is
	// the whole defence against algorithm confusion: a token whose
	// header says "none" asks for no verification at all, and one
	// saying "HS256" asks a verifier to check an HMAC using a key it
	// treats as public — both are accepted only by a verifier that takes
	// the algorithm from the token. Both providers this package supports
	// sign with RS256 and publish nothing else.
	if header.Alg != "RS256" {
		return Identity{}, fmt.Errorf("%w: it is signed with %q and only RS256 is accepted", ErrToken, header.Alg)
	}

	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return Identity{}, fmt.Errorf("%w: its signature is not base64url", ErrToken)
	}
	signed := sha256.Sum256([]byte(parts[0] + "." + parts[1]))

	slot := e.cache.keySlotFor(m.JWKSURI)
	key, err := e.key(ctx, slot, m.JWKSURI, header.Kid)
	if err != nil {
		return Identity{}, err
	}
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, signed[:], sig); err != nil {
		// A signature that fails against the key the token NAMED is
		// worth one retry with a freshly fetched set, and exactly one:
		// providers rotate signing keys, and a cached set can name a
		// key id whose key has since been replaced. A second miss is a
		// refusal.
		fresh, ferr := e.refetchKeys(ctx, slot, m.JWKSURI, header.Kid)
		if ferr != nil || rsa.VerifyPKCS1v15(fresh, crypto.SHA256, signed[:], sig) != nil {
			return Identity{}, fmt.Errorf("%w: the signature does not check out", ErrToken)
		}
	}

	// Only now is anything inside the token read.
	claimsJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Identity{}, fmt.Errorf("%w: its claims are not base64url", ErrToken)
	}
	var cl claims
	if err := json.Unmarshal(claimsJSON, &cl); err != nil {
		return Identity{}, fmt.Errorf("%w: its claims are not JSON", ErrToken)
	}

	// Exact match, not trimmed: m.Issuer is already canonical (Discover
	// trims it once, at the only place it is ever set), and the
	// specification's own comparison is exact. Trimming the TOKEN's
	// side as well was a leniency beyond that, found in code review —
	// harmless in practice against this package's own closed provider
	// list, but a token is untrusted input and this check should say
	// exactly what it means.
	if cl.Issuer != m.Issuer {
		return Identity{}, fmt.Errorf("%w: it was issued by %q and this connection expects %q",
			ErrToken, cl.Issuer, m.Issuer)
	}
	if !cl.Audience.has(conn.ClientID) {
		return Identity{}, fmt.Errorf("%w: it was issued for a different application", ErrToken)
	}
	// azp is checked whenever it is PRESENT, not only when aud carries
	// more than one value: OpenID Connect Core says a client SHOULD
	// verify azp against its own client_id whenever the claim appears,
	// and a single-audience token carrying an azp naming somebody else
	// is a malformed or tampered token either way (found in code
	// review — the prior version only checked azp in the multi-audience
	// case the specification makes it REQUIRED in, and silently ignored
	// it otherwise).
	if cl.AuthorizedParty != "" && cl.AuthorizedParty != conn.ClientID {
		return Identity{}, fmt.Errorf("%w: it names an authorized party other than this application", ErrToken)
	}
	if len(cl.Audience) > 1 && cl.AuthorizedParty == "" {
		return Identity{}, fmt.Errorf("%w: it names several audiences and does not name this "+
			"application as the authorized party", ErrToken)
	}
	// The nonce is compared even when the provider omitted it, so a
	// provider that quietly dropped the claim is a refusal rather than a
	// token that passes without the check it was meant to carry.
	if nonce == "" || cl.Nonce != nonce {
		return Identity{}, fmt.Errorf("%w: it does not carry this sign-in's nonce", ErrToken)
	}

	now := e.now()
	if cl.Expires == 0 || now.After(time.Unix(cl.Expires, 0).Add(skew)) {
		return Identity{}, fmt.Errorf("%w: it has expired", ErrToken)
	}
	if cl.NotBefore != 0 && now.Add(skew).Before(time.Unix(cl.NotBefore, 0)) {
		return Identity{}, fmt.Errorf("%w: it is not valid yet", ErrToken)
	}
	// iat is REQUIRED by the specification, unlike nbf — treated that
	// way here now rather than as optional like nbf is (found in code
	// review: the prior version let a token with no iat at all skip
	// this check entirely, the same gap exp does not have).
	if cl.IssuedAt == 0 {
		return Identity{}, fmt.Errorf("%w: it names no issued-at time", ErrToken)
	}
	if now.Add(skew).Before(time.Unix(cl.IssuedAt, 0)) {
		return Identity{}, fmt.Errorf("%w: it claims to have been issued in the future", ErrToken)
	}
	if cl.Subject == "" {
		return Identity{}, fmt.Errorf("%w: it names no subject", ErrToken)
	}

	id := Identity{
		Subject:         cl.Subject,
		Issuer:          cl.Issuer, // == m.Issuer, exactly, by the check above
		Address:         address(cl),
		AddressVerified: cl.EmailVerified.tristate(),
		Name:            strings.TrimSpace(cl.Name),
		Tenant:          cl.Tenant,
		HostedDomain:    strings.ToLower(strings.TrimSpace(cl.HostedDomain)),
		Methods:         cl.Methods,
	}
	if cl.AuthTime != 0 {
		id.AuthTime = time.Unix(cl.AuthTime, 0).UTC()
	}
	return id, nil
}

// ErrNotAdmitted is Admitted's refusal — see its doc comment.
var ErrNotAdmitted = errors.New("oidc: that address is not admitted here")

// Admitted applies the connection-level admission rules every provider
// this package supports needs, BEFORE an app's own Config.Admit ever
// runs — design D3, written in response to review finding 3 ("the rule
// for deriving the address is not in this document"). An app's Admit
// hook sees only identities that have already passed this.
//
// Three refusals, in order:
//
//   - No address at all. A provider is not obliged to release one.
//   - AddressVerified == Denied. Entra never asserts the claim at all
//     (Unstated), and Unstated is let through — refusing every Entra
//     sign-in over a claim it never sends would be wrong in the opposite
//     direction.
//   - For Provider.NeedsHostedDomain (Google today): Identity.HostedDomain
//     absent, or not one of Connection.AllowedDomains. This is ips's
//     D88.11 turned into an unconditional check here rather than a
//     convention an app's own membership hook might forget to apply:
//     without it, the only thing standing between a stranger and a
//     connection whose issuer is shared by every Gmail account that has
//     ever existed is "is this address on our people list" — checked
//     against an address field the provider does not even vouch for in
//     the same way it vouches for hd.
func Admitted(p Provider, conn Connection, id Identity) error {
	if id.Address == "" {
		return fmt.Errorf("%w: the provider released no address", ErrNotAdmitted)
	}
	if id.AddressVerified == Denied {
		return fmt.Errorf("%w: the provider says that address is not verified", ErrNotAdmitted)
	}
	if p.NeedsHostedDomain {
		if id.HostedDomain == "" {
			return fmt.Errorf("%w: that account carries no organisation claim this connection can check", ErrNotAdmitted)
		}
		ok := false
		for _, d := range conn.AllowedDomains {
			if strings.EqualFold(strings.TrimSpace(d), id.HostedDomain) {
				ok = true
				break
			}
		}
		if !ok {
			return fmt.Errorf("%w: that account's organisation is not one this connection accepts", ErrNotAdmitted)
		}
	}
	return nil
}

// claims is the ID token, narrowed to what is read.
type claims struct {
	Issuer          string   `json:"iss"`
	Subject         string   `json:"sub"`
	Audience        audience `json:"aud"`
	AuthorizedParty string   `json:"azp"`
	Nonce           string   `json:"nonce"`
	Expires         int64    `json:"exp"`
	IssuedAt        int64    `json:"iat"`
	NotBefore       int64    `json:"nbf"`
	AuthTime        int64    `json:"auth_time"`
	Email           string   `json:"email"`
	EmailVerified   flexBool `json:"email_verified"`
	Preferred       string   `json:"preferred_username"`
	UPN             string   `json:"upn"`
	Name            string   `json:"name"`
	Tenant          string   `json:"tid"`
	HostedDomain    string   `json:"hd"`
	Methods         []string `json:"amr"`
}

// address is which claim this flow takes an address from, and the order
// is a decision rather than a convenience.
//
// `email` first, because it is the claim that means an address. Where a
// provider releases none, Entra's `preferred_username` and `upn` are
// accepted ONLY when they parse as an address — a user principal name
// usually is one, and where it is not it is an identifier that would
// match nobody on an app's roll anyway. Nothing here invents an address:
// an empty answer is a refusal (Admitted's job), never "no match
// required".
func address(cl claims) string {
	for _, candidate := range []string{cl.Email, cl.Preferred, cl.UPN} {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		// EXACTLY ONE "@". A first draft that took the first one and let
		// a caller read the domain from the last would let
		// "attacker@evil.example@firm.co.uk" satisfy both, so a domain
		// allowlist matching on the last segment could be passed by an
		// address whose local part is somebody else's whole address.
		at := strings.Index(candidate, "@")
		if at <= 0 || at != strings.LastIndex(candidate, "@") || at == len(candidate)-1 ||
			strings.ContainsAny(candidate, " \t") {
			continue
		}
		return candidate
	}
	return ""
}

// audience decodes `aud`, which the specification allows to be either one
// string or an array of them. A decoder that handled only one shape would
// refuse half the providers in the world, and one that silently ignored
// the field would be worse.
type audience []string

func (a *audience) UnmarshalJSON(b []byte) error {
	var one string
	if err := json.Unmarshal(b, &one); err == nil {
		*a = audience{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(b, &many); err != nil {
		return err
	}
	*a = many
	return nil
}

func (a audience) has(want string) bool {
	for _, v := range a {
		if v == want {
			return true
		}
	}
	return false
}

// flexBool decodes a claim that is a boolean in the specification and a
// string in some deployments, and — crucially — remembers whether it was
// there at all. Absent is not false: see Identity.AddressVerified.
type flexBool struct {
	set   bool
	value bool
}

func (f *flexBool) UnmarshalJSON(b []byte) error {
	var v bool
	if err := json.Unmarshal(b, &v); err == nil {
		f.set, f.value = true, v
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		f.set, f.value = true, s == "true"
		return nil
	}
	// A claim of a shape nothing here understands is left unstated
	// rather than guessed. Refusing the whole token over a field that
	// decides nothing on its own would be the guard crying wolf.
	return nil
}

func (f flexBool) tristate() Tristate {
	switch {
	case !f.set:
		return Unstated
	case f.value:
		return Asserted
	default:
		return Denied
	}
}

// key returns the signing key with this id from the slot, fetching if
// cold, stale, or missing it. The slot's own mutex is this fetch's
// single-flight guard (keys.go).
func (e *Engine) key(ctx context.Context, slot *keySlot, uri, kid string) (*rsa.PublicKey, error) {
	slot.mu.Lock()
	defer slot.mu.Unlock()
	if e.now().Sub(slot.fetched) < keysTTL {
		if k := pickKey(slot.keys, kid); k != nil {
			return k, nil
		}
	}
	return e.fetchKeysLocked(ctx, slot, uri, kid)
}

// refetchKeys always goes to the provider, regardless of TTL. It is
// separate from key so the one-retry-on-a-bad-signature path can say
// what it means; the slot's lock is what makes two concurrent retries
// against the same jwks_uri queue rather than both fetching.
func (e *Engine) refetchKeys(ctx context.Context, slot *keySlot, uri, kid string) (*rsa.PublicKey, error) {
	slot.mu.Lock()
	defer slot.mu.Unlock()
	return e.fetchKeysLocked(ctx, slot, uri, kid)
}

// fetchKeysLocked does the actual HTTP fetch. The caller holds slot.mu.
func (e *Engine) fetchKeysLocked(ctx context.Context, slot *keySlot, uri, kid string) (*rsa.PublicKey, error) {
	body, status, err := e.fetch(ctx, "signing keys", uri)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Keys []struct {
			Kty string `json:"kty"`
			Kid string `json:"kid"`
			Alg string `json:"alg"`
			Use string `json:"use"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		return nil, &Error{Stage: "signing keys", Status: status,
			Message: "the key set could not be read as JSON", Body: body}
	}

	parsed := map[string]*rsa.PublicKey{}
	for _, k := range doc.Keys {
		// Only RSA keys published for signatures. A key marked for
		// encryption is not one this flow may verify with, and a key
		// naming an algorithm this package does not implement is
		// skipped rather than mis-used.
		if k.Kty != "RSA" || (k.Use != "" && k.Use != "sig") || (k.Alg != "" && k.Alg != "RS256") {
			continue
		}
		pub, err := rsaKey(k.N, k.E)
		if err != nil {
			continue
		}
		parsed[k.Kid] = pub
	}
	if len(parsed) == 0 {
		return nil, &Error{Stage: "signing keys", Status: status,
			Message: "the provider published no RSA signing keys this can verify with"}
	}

	// e.now(), not time.Now(): the TTL check in key() compares this
	// against e.now() too, and a test holding the clock still with a
	// fake Now would otherwise compare a fake timestamp against a real
	// wall-clock one — found in code review, the kind of bug a test
	// using today's real time as its fake clock cannot expose.
	slot.keys, slot.fetched = parsed, e.now()
	got := pickKey(slot.keys, kid)
	if got == nil {
		return nil, fmt.Errorf("%w: it names a signing key the provider does not publish", ErrToken)
	}
	return got, nil
}

// rsaKey builds a public key from a JWK's modulus and exponent, both
// base64url big-endian.
func rsaKey(nRaw, eRaw string) (*rsa.PublicKey, error) {
	n, err := base64.RawURLEncoding.DecodeString(nRaw)
	if err != nil || len(n) == 0 {
		return nil, errors.New("oidc: unreadable key modulus")
	}
	modulus := new(big.Int).SetBytes(n)
	// BitLen, not len(n): checking the DECODED BYTE LENGTH is checking
	// the encoding, not the value — a modulus padded with leading zero
	// bytes passes a byte-length check at any size while SetBytes
	// (which ignores leading zeros, per its own doc) still produces the
	// small, easily-factorable integer those zeros were padding around
	// (found in code review). 2048 bits is the floor for every provider
	// in this list and the floor Go's own crypto/rsa applies to
	// signing; nothing today can supply a smaller one legitimately — so
	// this is depth rather than a hole being closed, but it is depth
	// against the actual VALUE this builds, not against how it happened
	// to be spelled. 8192 bits bounds the top: readBody's 1MB response
	// cap already bounds it loosely, but a key this package will spend
	// real CPU verifying against should have an explicit, deliberate
	// ceiling rather than "whatever fits in a megabyte".
	if bits := modulus.BitLen(); bits < 2048 || bits > 8192 {
		return nil, errors.New("oidc: the key modulus is not a plausible signing key size")
	}
	e, err := base64.RawURLEncoding.DecodeString(eRaw)
	if err != nil || len(e) == 0 || len(e) > 8 {
		return nil, errors.New("oidc: unreadable key exponent")
	}
	// Left-pad to eight bytes and read as a big-endian unsigned integer:
	// the encoding is minimal-length, so 65537 arrives as three bytes
	// rather than as a fixed-width field.
	var padded [8]byte
	copy(padded[8-len(e):], e)
	exp := binary.BigEndian.Uint64(padded[:])
	if exp < 3 || exp > 1<<31 {
		return nil, errors.New("oidc: implausible key exponent")
	}
	return &rsa.PublicKey{N: modulus, E: int(exp)}, nil
}
