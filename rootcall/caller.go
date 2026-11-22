package rootcall

import (
	"bytes"
	"context"
	"net/http"
	"time"

	"amadan.net/rastrillo/rastrillo/assertion"
	"amadan.net/rastrillo/rastrillo/crypto"
)

// lifetime is half the most a call may live, which leaves room for a slow
// cold start on the instance without the token outliving its use.
const lifetime = 60

// Caller is the root's side: it signs each request it builds.
type Caller struct {
	Origin string // the root's origin, the issuer
	Key    *crypto.Keypair
	KeyID  string
	// Now is the root's clock. Nil means time.Now.
	Now func() time.Time
}

// NewRequest builds a signed call to target, an https:// URL on the
// instance's platform host, which is also the call's audience. Send it
// once: a retry needs a new request, because the instance spends each
// call's ID. body may be nil.
func (caller Caller) NewRequest(
	ctx context.Context, method, target string, body []byte,
) (*http.Request, error) {
	request, err := http.NewRequestWithContext(
		ctx, method, target, bytes.NewReader(body))
	if err != nil {
		return nil, ErrClaims
	}
	if request.URL.Scheme != "https" || request.URL.User != nil {
		return nil, ErrClaims
	}
	id, err := assertion.RandomID()
	if err != nil {
		return nil, err
	}
	now := time.Now
	if caller.Now != nil {
		now = caller.Now
	}
	issuedAt := now().Unix()
	token, err := Sign(caller.Key, Claims{
		Version: 1, Issuer: caller.Origin,
		Audience: "https://" + request.URL.Host, KeyID: caller.KeyID,
		ID: id, IssuedAt: issuedAt, ExpiresAt: issuedAt + lifetime,
		Method: method, Target: request.URL.RequestURI(),
		BodyHash: BodyHash(body),
	})
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", Scheme+" "+token)
	return request, nil
}
