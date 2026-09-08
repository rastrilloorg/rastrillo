# 🤖 assertion

Use an assertion to pass an authenticated identity from one exact HTTPS origin to another. The verifier checks the signature, issuer, audience, key, claims, lifetime, and expiry. It does not consume the browser’s random value (`nonce`) or assertion ID (`jti`).

```go
keys := map[string][]byte{
	"login-2026": pinnedPublicKey,
}

verifier, err := assertion.NewVerifier(
	"https://login.example.com",
	"https://app.example.com",
	keys,
)
if err != nil {
	return err
}

claims, err := verifier.Verify(suppliedToken, time.Now())
if err != nil {
	return err // Fixed error text is safe to log.
}

// Compare claims.Nonce with the pending browser nonce using
// crypto/subtle.ConstantTimeCompare. Then atomically consume the pending
// request and persistently record claims.ID before creating a session.
```

Before serving private content, preserve `AuthTime` and check the applicable team membership and item grants. Repeated verification can succeed until expiry: session creation and replay prevention belong to your consumer. No HTTP adapter is included.

Rotate keys by constructing and swapping in a replacement verifier.

Tokens must not appear in logs or URLs. The total token is limited to 8192 bytes, and its JSON payload is limited to 4096 bytes. The optional `Ext` object is limited to 1024 bytes and carries cosmetic data only. It grants no authority.

## API

```go
var (
	ErrMalformed  = errors.New("assertion: malformed")
	ErrVersion    = errors.New("assertion: version")
	ErrUnknownKey = errors.New("assertion: unknown key")
	ErrSignature  = errors.New("assertion: signature")
	ErrIssuer     = errors.New("assertion: issuer")
	ErrAudience   = errors.New("assertion: audience")
	ErrLifetime   = errors.New("assertion: lifetime")
	ErrExpired    = errors.New("assertion: expired")
	ErrClaims     = errors.New("assertion: claims")
	ErrConfig     = errors.New("assertion: configuration")
	ErrRandom     = errors.New("assertion: randomness")
	ErrSigning    = errors.New("assertion: signing")
)
```

All error text is fixed and contains no submitted values, so callers can log it safely.

```go
type Claims struct {
	Version   int64          `json:"v"`          // Must be 1.
	Issuer    string         `json:"iss"`        // Exact HTTPS origin.
	Audience  string         `json:"aud"`        // Exact HTTPS origin.
	Subject   string         `json:"sub"`        // Opaque identity, up to 128 bytes.
	KeyID     string         `json:"kid"`        // Signing-key identifier, up to 64 bytes.
	ID        string         `json:"jti"`        // Assertion ID; use for replay prevention.
	Nonce     string         `json:"nonce"`      // Browser-bound random value.
	IssuedAt  int64          `json:"iat"`        // Unix timestamp.
	ExpiresAt int64          `json:"exp"`        // Unix timestamp.
	AuthTime  int64          `json:"auth_time"`  // Original authentication time.
	Ext       jsontext.Value `json:"ext,omitzero"` // Cosmetic object, up to 1024 bytes.
}
```

```go
func RandomID() (string, error)
```

`RandomID` returns 16 random bytes encoded as 22 unpadded base64url characters. Generate separate values for `Claims.ID` and `Claims.Nonce`; sharing one value is rejected.

```go
func Sign(key *crypto.Keypair, claims Claims) (string, error)
```

`Sign` validates and signs claims without reading a clock. ExpiresAt must be later than IssuedAt, with an interval of at most 120 seconds. Serialization may compact or re-escape `Ext` while preserving its JSON value. The caller must not mutate `key` or `Ext` while signing.

```go
type Verifier struct { /* unexported fields */ }

func NewVerifier(
	issuer string,
	audience string,
	keys map[string][]byte,
) (*Verifier, error)

func (v *Verifier) Verify(
	token string,
	now time.Time,
) (Claims, error)
```

`NewVerifier` requires different canonical HTTPS origins, one to eight uncompressed 65-byte P-256 public keys, and valid key identifiers. It copies the keys and is safe for concurrent use. Origins use lowercase ASCII DNS names, without paths, credentials, IP literals or explicit port 443.

`Verify` returns zero `Claims` on failure. It rejects expired tokens with no expiry grace period. It accepts an issue time no more than 30 seconds in the future relative to `now`.
