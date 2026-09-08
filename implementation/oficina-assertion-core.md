# Oficina team entry: assertion core

Status: proposed implementation contract, 2026-09-08, awaiting independent Fable review. User approved prioritising common app entry → Home → team choice → team subdomain. This is the first shared dependency, not a claim that the whole journey exists.

## Scope and reconciliation

Implement package assertion as pure signing and verification over the existing crypto package. No networking, cookies, handlers, sessions, SQL or roster in this package. Home and Docs are the first consumers. This avoids two different verifiers while the adapters are built.

The unimplemented 2026-08-31 design is amended for this consumer: compose existing P-256 crypto.Sign/Verify rather than introducing Home's earlier Ed25519 proposal; enforce the suite's 120-second maximum; carry signed authentication time separately from issuance; carry a signed consumer nonce to bind the browser request. These are proposed technical reconciliations of conflicting drafts, not newly attributed historical rulings. Existing key-fetch, HTTP transport and roster decisions remain adapter contracts. No existing signed tokens or users need a wire migration because no assertion package has shipped.

## API and wire

- Claims: v, iss, aud, sub, kid, jti, nonce, iat, exp, auth_time and optional ext. Version1. All required fields must occur exactly once. Unknown fields refused. Null required values refused. Duplicate object keys refused recursively, including extension objects. Invalid UTF-8 and unpaired JSON surrogate escapes refused rather than normalized.
- Token: unpadded base64url of exact UTF-8 JSON payload, dot, unpadded base64url of raw64-byte P-256 signature. Max encoded token8192bytes; decoded payload4096bytes. Both components must be canonical base64url; exactly one separator. No JWT algorithm field, no alternate signature algorithm.
- crypto domain separation is exactly rastrillo/assertion/v1, matching the framework draft. Verify signature over the original decoded payload bytes, never remarshal before verification.
- Sign(*crypto.Keypair, Claims) returns token or a fixed code error; validates structure and lifetime, not local current time. Caller supplies random jti and consumer-provided nonce. Both are canonical base64url encodings of16random bytes. This package also supplies a RandomID helper using crypto/rand so adapters do not invent identifiers.
- NewVerifier(issuer, audience string, keys map[string][]byte) validates and copies a bounded1..8kid→65-byte P-256 public-key map. It retains no private key. Caller may atomically replace the verifier for rotation. No fetch or fallback on unknownkid.
- Verify(token string, now time.Time) returns Claims only after all checks, otherwise zero Claims and a fixed error. It is safe for concurrent calls; no replay state or claim-dependent I/O. Signature first, then exact configured issuer/audience equality and semantic/time checks. Parsing/version/key lookup needed before signature have no side effects.
- Origins are canonical HTTPS origins: lowercase DNS hostname with optional valid numeric port, no credentials/path/query/fragment, no wildcard. No request Host/header normalization is used for matching. Local HTTP test origins are not a production verifier mode; use synthetic HTTPS origins in tests.
- sub and kid are opaque ASCII identifiers1..128 and1..64bytes respectively, restricted to letters,digits,hyphen,underscore. jti and nonce exactly22canonical base64url characters decoding to16bytes. Identity never carries an email address.
- Integer Unix seconds only: iat>0, auth_time>0, auth_time<=iat, exp>iat, exp-iat<=120, exp>now, iat<=now+30. No exp grace period. Future iat skew is bounded at30seconds. Integer overflow must not bypass any check. Zero/missing auth_time is not silently replaced by iat; Home adapter computes the same AuthTime-or-At rule as sessions.Fresh before signing.
- ext is optional, at most1024encoded bytes, JSON object only. Values are authenticated data but grant no authority. Apps validate a closed cosmetic field set and discard invalid preferences. No roles, memberships, grants or routing authority in ext.

## What verification does not promise

Pure Verify does not consume the jti or make a session. Documentation and names must not imply otherwise. Every HTTP adapter must atomically consume the browser-bound pending nonce and insert the jti into its local self-pruning replay store before session issuance, reject duplicates/concurrency, and preserve original authentication freshness. A claimed team is never authorization. The actual consumer origin determines audience; Home's authoritative roster and existing item grants determine access.

POST callback transport is narrowly outside ordinary same-origin CSRF middleware because it arrives from the credential origin. It must require the configured issuer Origin and pass the signed nonce/browser pending check; the signed token must not be placed in a URL, log, referrer or browser history. Full adapter design and browser review remain required before it is deployed. The existing module alone cannot satisfy them.

## Verification

- Independent signed fixture: fixed public key/payload/signature generated using a second implementation, with provenance and fixed synthetic identities. Verify existing crypto vectors remain green; do not implement ECDSA again.
- Positive roundtrip plus exact output payload framing. Sign output verifies in the independent implementation. Unicode cosmetic strings allowed; malformed JSON Unicode rejected.
- Negative vectors: wrong key/context/signature, unsigned field mutation, wrong exact audience/issuer, unknown version/kid/fields, duplicates, trailing JSON, malformed/oversized encoding, null/missing/type-mismatched claims, invalid points, nonce/jti aliases, TTL/future/expiry/auth_time edges and integer overflow.
- No nonzero Claims on any failure. Key-map mutation after construction cannot change verifier trust. Concurrent verification race test.
- Observe meaningful mutations fail: remove audience check and expiry check independently, then restore.
- Full repository make ci, actual hub CI, and Fable whole-code review before merge. No app deployment from this package completion alone.

## Review amendment: exact parsing and construction

Fable 5.1 reviewed fd60416 and requested the following executable details. This revision uses the already pinned github.com/go-json-experiment/json parser rather than encoding/json's permissive decoder. No dependency is added.

- Parse payload into map[string]jsontext.Value with json.Unmarshal using its strict defaults (duplicate names and invalid Unicode refused). A separate recursive validation into any ensures raw values cannot hide duplicate keys or malformed Unicode. Require utf8.Valid and the first/last byte to be braces, so outside whitespace and trailing data are refused. Match decoded member names exactly against the closed claim set. Require each mandatory key and reject a raw null. Explicit tests cover nested duplicates, escaped duplicate names, case variants, malformed UTF-8 and lone surrogate escapes. Valid paired surrogates remain accepted Unicode: the v2 parser validates pairing rather than replacing lone surrogates.
- Parse every numeric raw value with strconv.ParseInt(string(raw), 10, 64), after rejecting a leading plus. Fractions, exponents and overflow fail. now.Unix() defines whole-second time. Check iat > 0, exp > iat before exp-iat; check future issuance with iat-30 > now.Unix(), never now+30.
- Check every base64url component's alphabet before RawURLEncoding.Strict decoding, then require re-encoding equality. No CR/LF exception. Signature component is exactly 86 characters and 64 decoded bytes.
- Claims.Ext is jsontext.Value with omitempty. Absence is nil; present values must be objects, not null. Sign validates extension JSON using the same recursive parser, then json.Marshal with default v2 options (no HTML escaping, no appended newline). Verify measures the raw ext value within the received payload; Sign measures both caller input and resulting embedded value. The 1024-byte limit includes the object's braces. Sign validates its serialized payload using the same claim parser before signing.
- Export sentinel errors ErrMalformed, ErrVersion, ErrUnknownKey, ErrSignature, ErrIssuer, ErrAudience, ErrLifetime, ErrExpired, ErrClaims, ErrConfig, ErrRandom and ErrSigning. Never include input, token, claim values or underlying error text. Invalid signer/key returns ErrSigning rather than panicking.
- NewVerifier checks each kid grammar and validates every key with elliptic.Unmarshal(P256). Copy both map and every byte slice. Refuse equal issuer and audience. Sign also refuses equal issuer/audience and equal jti/nonce.
- Canonical origins have ASCII lowercase DNS A-labels, no trailing dot, no IP literals, no slash. Labels have 1..63 bytes, alphanumeric ends and only alphanumeric/hyphen interiors; total hostname at most 253 bytes. Optional explicit port is 1..65535, without leading zeros; explicit 443 is refused. Compare received issuer/audience byte-for-byte without normalization.
- Verify returns the signed nonce; it does not check it. Its doc comment requires adapter comparison to pending browser state with subtle.ConstantTimeCompare. One verifier serves one audience; a process serving several origins holds separate verifiers.
- Add assertion to the existing Makefile race target; the existing ci.d step invokes that target and its description must be updated too. The unconditional independent fixture verifies a fixed external P-256 signature; reverse-direction interoperability uses Node when available and never compares randomized signatures.
