package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"strings"
	"time"

	"amadan.net/rastrillo/rastrillo/crypto"
	"amadan.net/rastrillo/rastrillo/objrec"
)

// recordLinks implements signin.LinkStore as objrec records, for a
// central app that keeps no database. A link is
// links/<day>/<hash>.json; redeeming it creates
// links-used/<day>/<hash>. Both keys carry the UTC day the link was
// sent so sweep can drop whole days by key alone, without opening
// each record to find its expiry.
type recordLinks struct {
	store  objrec.Store
	pepper []byte
	// seal encrypts the address at rest, so anyone who can list or read
	// the bucket learns who asked to sign in only by also holding the
	// instance key.
	seal []byte
	// now is called on every use and never copied: auth passes a
	// closure over a.now, and a test that moves a.now after New must
	// move the link store with it, or links would be judged on a
	// different clock from the cookie.
	now func() time.Time
	log *slog.Logger
}

type linkRecord struct {
	EH string `json:"eh"`
	// Addr is the SealSym of the address; encoding/json writes []byte
	// as base64.
	Addr    []byte `json:"addr"`
	Purpose string `json:"purpose"`
	Exp     int64  `json:"exp"`
}

// emailHash is <eh>: lowercase hex HMAC-SHA256(pepper, lowercase
// address). Lowercasing first means Ana@Example.com and
// ana@example.com are one person, not two records.
func emailHash(pepper []byte, address string) string {
	m := hmac.New(sha256.New, pepper)
	m.Write([]byte(strings.ToLower(address)))
	return hex.EncodeToString(m.Sum(nil))
}

func linkDay(t time.Time) string { return t.UTC().Format(time.DateOnly) }

func (l *recordLinks) PutLink(ctx context.Context, hash, address, purpose string, expires time.Time) error {
	sealed, err := crypto.SealSym(l.seal, []byte(address))
	if err != nil {
		return err
	}
	_, err = objrec.CreateJSON(ctx, l.store, "links/"+linkDay(l.now())+"/"+hash+".json", linkRecord{
		EH: emailHash(l.pepper, address), Addr: sealed, Purpose: purpose, Exp: expires.Unix(),
	})
	return err
}

// TakeLink redeems a link. Unknown hash, wrong purpose, already used
// and expired all answer the same not-ok, as the SQL store does:
// telling them apart would be an oracle.
func (l *recordLinks) TakeLink(ctx context.Context, hash, purpose string) (string, bool, error) {
	now := l.now()
	// LinkTTL is 15 minutes, so a live link was sent today or, just
	// after midnight UTC, yesterday. Reading only today would lose
	// every link sent in the last minutes of a day.
	//
	// The day is subtracted after converting to UTC. On a local clock,
	// one day back across a DST change is 23 or 25 hours, which can
	// skip the UTC day a live link is filed under.
	utc := now.UTC()
	var (
		rec  linkRecord
		day  string
		read error
	)
	for _, d := range []string{linkDay(utc), linkDay(utc.AddDate(0, 0, -1))} {
		rec, _, read = objrec.GetJSON[linkRecord](ctx, l.store, "links/"+d+"/"+hash+".json")
		if read == nil {
			day = d
			break
		}
		if !errors.Is(read, objrec.ErrNotFound) {
			return "", false, read
		}
	}
	// An unknown token writes nothing. If it wrote a used marker, anyone
	// could fill the bucket by posting random tokens.
	if day == "" {
		return "", false, nil
	}
	// A wrong purpose leaves the link for its own purpose, as the SQL
	// store's WHERE purpose = ? does.
	if rec.Purpose != purpose {
		return "", false, nil
	}
	// This create is the single use. Every racing redeemer may have read
	// the link above; only one of them creates the marker.
	if _, err := objrec.CreateJSON(ctx, l.store, "links-used/"+day+"/"+hash, struct{}{}); err != nil {
		if errors.Is(err, objrec.ErrExists) {
			return "", false, nil
		}
		return "", false, err
	}
	// The marker already makes the link unusable, so a failed delete
	// only leaves a record for sweep. Failing the sign-in for it would
	// turn a storage hiccup into a spent link and no session.
	if err := l.store.Delete(ctx, "links/"+day+"/"+hash+".json"); err != nil {
		l.log.Warn("auth: delete redeemed link", "err", err)
	}
	// Checked after the marker, so a presented token is spent even when
	// expired, as in the SQL store.
	if now.After(time.Unix(rec.Exp, 0)) {
		return "", false, nil
	}
	addr, err := crypto.OpenSym(l.seal, rec.Addr)
	if err != nil {
		return "", false, err
	}
	return string(addr), true, nil
}

// sweep deletes every link and used marker from before yesterday.
// Correctness never depends on it (TakeLink checks expiry and reads
// only two days); it keeps unclicked links and spent markers from
// piling up for the life of the bucket. Yesterday stays because a link
// sent just before midnight is still live, and TakeLink still reads
// it: deleting its marker would let it be redeemed twice.
func (l *recordLinks) sweep(ctx context.Context, now time.Time) error {
	// UTC before AddDate, as in TakeLink: a local day back across a DST
	// change would move the cutoff by a whole day.
	cutoff := linkDay(now.UTC().AddDate(0, 0, -1))
	for _, prefix := range []string{"links/", "links-used/"} {
		cursor := ""
		for {
			keys, next, err := l.store.List(ctx, prefix, cursor, 0)
			if err != nil {
				return err
			}
			for _, k := range keys {
				seg, _, _ := strings.Cut(strings.TrimPrefix(k, prefix), "/")
				// A key whose day does not parse is not one this store
				// wrote; leave it rather than guess.
				if _, err := time.Parse(time.DateOnly, seg); err != nil || seg >= cutoff {
					continue
				}
				if err := l.store.Delete(ctx, k); err != nil {
					return err
				}
			}
			if next == "" {
				break
			}
			cursor = next
		}
	}
	return nil
}
