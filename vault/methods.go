package vault

import (
	"context"
	"net/http"
)

// Method is one way into the vault: a passkey, an identity anchor, a
// synced item — Kass's generalisation past "credential", and the unit
// the wrapped seed is keyed by.
type Method struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	CreatedAt string `json:"created_at"`
}

// BlobInfo is one row of the vault's listing: a name and the opaque
// version to fetch or Put against — never content.
type BlobInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ceremonyKey carries the X-Ceremony-Proof header through do, like
// ifMatchKey: reductions of access demand a fresh ceremony, and the
// proof is the home's evidence of one.
type ceremonyKey struct{}

// Methods lists the ways into this vault.
func (c *Client) Methods(ctx context.Context) ([]Method, error) {
	var out struct {
		Methods []Method `json:"methods"`
	}
	if err := c.do(ctx, http.MethodGet, "/v1/methods", nil, &out); err != nil {
		return nil, err
	}
	return out.Methods, nil
}

// Wrapped fetches the seed wrapped for one method — what sign-in
// unwraps with the method's own secret (keyring.Ring.UnwrapSeed).
func (c *Client) Wrapped(ctx context.Context, methodID string) ([]byte, error) {
	var out struct {
		Wrapped []byte `json:"wrapped"`
	}
	if err := c.do(ctx, http.MethodGet, "/v1/methods/"+methodID+"/wrapped", nil, &out); err != nil {
		return nil, err
	}
	return out.Wrapped, nil
}

// Enrol stores the seed wrapped for a new (or re-wrapped) method.
// Adding a way in changes who can open everything, so the home
// demands a fresh ceremony proof.
func (c *Client) Enrol(ctx context.Context, methodID string, wrapped []byte, ceremonyProof string) error {
	ctx = context.WithValue(ctx, ceremonyKey{}, ceremonyProof)
	body := struct {
		Wrapped []byte `json:"wrapped"`
	}{wrapped}
	return c.do(ctx, http.MethodPut, "/v1/methods/"+methodID+"/wrapped", body, nil)
}

// RemoveMethod removes a way in — ceremony-gated, and the home's
// last-method guard refuses to strand the vault (surfaced as an
// ordinary error; the guard itself lives server-side, atomic with the
// delete).
func (c *Client) RemoveMethod(ctx context.Context, methodID, ceremonyProof string) error {
	ctx = context.WithValue(ctx, ceremonyKey{}, ceremonyProof)
	return c.do(ctx, http.MethodDelete, "/v1/methods/"+methodID, nil, nil)
}

// Blobs lists the vault's blob names and versions.
func (c *Client) Blobs(ctx context.Context) ([]BlobInfo, error) {
	var out struct {
		Blobs []BlobInfo `json:"blobs"`
	}
	if err := c.do(ctx, http.MethodGet, "/v1/blobs", nil, &out); err != nil {
		return nil, err
	}
	return out.Blobs, nil
}
