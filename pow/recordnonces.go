package pow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"

	"amadan.net/rastrillo/rastrillo/objrec"
)

// RecordNonces stores spent nonces as objrec records, for a front door
// with no database. A spend is the create of
// pow-nonces/<hash>/<expires>, and a create that finds the key taken
// means the nonce was spent.
//
// It ignores the caller's transaction, as MemoryNonces does: a stateless
// centre has no transaction to ride, so a spend stays spent when the
// caller's work rolls back.
func RecordNonces(store objrec.Store) NonceStore {
	return &recordNonces{store: store, now: time.Now}
}

type recordNonces struct {
	store objrec.Store
	now   func() time.Time
}

const recordNoncePrefix = "pow-nonces/"

// nonceDir is the prefix of every record for one nonce. The nonce is
// hashed because it is mixed-case base64url and objrec keys are
// lowercase only.
func nonceDir(nonce string) string {
	h := sha256.Sum256([]byte(nonce))
	return recordNoncePrefix + hex.EncodeToString(h[:]) + "/"
}

// Spend checks the expiry, creates the marker, then checks the expiry
// again. NonceStore wants the check and the spend to be one step, and a
// remote create cannot share a statement with the clock. Without the
// second check, a request that stalls past expiry while another spends
// the nonce and a sweep deletes the marker would create it afresh and be
// admitted. A marker left by a refused late create is swept like any
// other.
func (r *recordNonces) Spend(ctx context.Context, _ Execer, nonce string, expires time.Time) (bool, error) {
	if r.now().After(expires) {
		return false, nil
	}
	key := nonceDir(nonce) + strconv.FormatInt(expires.UnixMilli(), 10)
	if _, err := r.store.Create(ctx, key, []byte("{}")); err != nil {
		if errors.Is(err, objrec.ErrExists) {
			return false, nil
		}
		return false, err
	}
	return !r.now().After(expires), nil
}

// Spent lists the nonce's directory rather than naming a key, so it
// needs no expiry.
func (r *recordNonces) Spent(ctx context.Context, nonce string) (bool, error) {
	keys, _, err := r.store.List(ctx, nonceDir(nonce), "", 1)
	return len(keys) > 0, err
}

func (r *recordNonces) Ready(context.Context) error { return nil }

// Sweep deletes markers expired longer ago than sweepMargin, judging by
// the expiry in the key so it never reads an object. It follows the
// listing to the end: a centre that sweeps once a day would otherwise
// leave every marker past the first batch to pile up.
func (r *recordNonces) Sweep(now time.Time) error {
	ctx := context.Background()
	cutoff := now.Add(-sweepMargin).UnixMilli()
	cursor := ""
	for {
		keys, next, err := r.store.List(ctx, recordNoncePrefix, cursor, 0)
		if err != nil {
			return err
		}
		for _, k := range keys {
			ms, err := strconv.ParseInt(k[strings.LastIndexByte(k, '/')+1:], 10, 64)
			// A key whose last segment is not an expiry is not one this
			// store wrote; leave it rather than guess.
			if err != nil || ms >= cutoff {
				continue
			}
			if err := r.store.Delete(ctx, k); err != nil {
				return err
			}
		}
		if next == "" {
			return nil
		}
		cursor = next
	}
}
