package vault

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// ErrNotFound is Get's answer for a blob that has never been written —
// first-run, not an outage; callers typically Put(name, ..., Create).
var ErrNotFound = errors.New("rastrillo/vault: blob not found")

// pad-v1 is the fixed plaintext envelope sealed into every blob:
// u32BE(len(plaintext)) || plaintext || zeros to the pad target. Put
// uses a target of zero (no padding beyond the prefix); PutPadded
// rounds up to its target so ciphertext length leaks nothing. One
// format for both means Get never needs a marker to know how to open.

// wrap applies pad-v1: length prefix plus zero padding to target (a
// floor, not a cap — an oversize plaintext is prefixed, never
// truncated).
func wrap(plaintext []byte, target int) []byte {
	n := 4 + len(plaintext)
	if n < target {
		n = target
	}
	out := make([]byte, n)
	binary.BigEndian.PutUint32(out, uint32(len(plaintext)))
	copy(out[4:], plaintext)
	return out
}

// unwrap reverses pad-v1, refusing a corrupt prefix.
func unwrap(padded []byte) ([]byte, error) {
	if len(padded) < 4 {
		return nil, errors.New("rastrillo/vault: sealed blob too short for pad-v1")
	}
	n := binary.BigEndian.Uint32(padded)
	if int64(n) > int64(len(padded)-4) {
		return nil, errors.New("rastrillo/vault: pad-v1 length prefix exceeds blob")
	}
	return padded[4 : 4+n], nil
}

// blobWire is the v1 blob body in both directions: sealed bytes
// (base64 in JSON, Go's []byte default) plus the opaque version.
type blobWire struct {
	Sealed  []byte `json:"sealed,omitempty"`
	Version string `json:"version,omitempty"`
}

// do runs one authenticated request and decodes the JSON answer into
// out (which may be nil). 404 maps to ErrNotFound and 409 to ErrStale
// carrying the server's current version; anything else non-2xx is an
// opaque error — the home being down is normal operation, and the
// caller's copy is "couldn't reach your home".
func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.cfg.Home+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if v, ok := ctx.Value(ifMatchKey{}).(string); ok {
		req.Header.Set("If-Match", v)
	}
	if v, ok := ctx.Value(ceremonyKey{}).(string); ok {
		req.Header.Set("X-Ceremony-Proof", v)
	}
	res, err := c.cfg.Client.Do(req)
	if err != nil {
		return fmt.Errorf("rastrillo/vault: %s %s: %w", method, path, err)
	}
	defer res.Body.Close()
	switch {
	case res.StatusCode == http.StatusNotFound:
		return ErrNotFound
	case res.StatusCode == http.StatusConflict:
		var w blobWire
		if err := json.NewDecoder(res.Body).Decode(&w); err != nil {
			return fmt.Errorf("rastrillo/vault: conflict body: %w", err)
		}
		return ErrStale{Current: w.Version}
	case res.StatusCode < 200 || res.StatusCode > 299:
		return fmt.Errorf("rastrillo/vault: %s %s answered %s", method, path, res.Status)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(out)
}

// ifMatchKey smuggles the If-Match header through do without widening
// its signature for the one method that needs it.
type ifMatchKey struct{}
