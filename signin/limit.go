package signin

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"strings"
	"time"
)

// The budgets for the two secret ways in, from the design's
// constraints. A six-digit code is the only proof on the authenticator
// way, so an account gets few guesses; the client budget stops one
// client spreading its guesses over many accounts.
const (
	accountMisses = 5
	clientMisses  = 30
	missWindow    = 15 * time.Minute
)

// missLayout is fixed width, so the text comparisons in SQL order the
// times as times.
const missLayout = "2006-01-02T15:04:05.000000000Z"

// bucket is one budget a miss counts against.
type bucket struct {
	key   string
	limit int
}

// accountBucket is the budget the guesses at one account share: its
// subject's, so every address that signs in as it draws on the same
// five, else, for an address that is nobody's, the address's however it
// was typed. Any subject counts, enrolled or not: were only holders of
// the secret pooled, how a pause fell across two addresses would say
// who had one. Hashed so the table holds neither, and an unknown
// address has a budget like any other, or its answer would differ.
func accountBucket(subject, address string) bucket {
	kind, name := "subject:", subject
	if subject == "" {
		kind, name = "account:", strings.ToLower(strings.TrimSpace(address))
	}
	sum := sha256.Sum256([]byte(name))
	return bucket{key: kind + hex.EncodeToString(sum[:]),
		limit: accountMisses}
}

func clientBucket(ip string) bucket {
	return bucket{key: "ip:" + ip, limit: clientMisses}
}

// limiter counts misses in SQL, so a restart or a second process
// shares them.
type limiter struct {
	database *sql.DB
}

// reservation names the rows one attempt wrote, kept as its misses if
// the secret is wrong and deleted if it is right. A random token, not a
// rowid: SQLite hands a swept row's rowid to the next insert, and a
// late release would then delete a newer attempt's misses.
type reservation string

func missStamp(at time.Time) string { return at.UTC().Format(missLayout) }

// reserve counts the attempt against every bucket before its secret is
// checked, in one immediate transaction, so attempts racing in parallel
// each see the others and only the budget's worth get a guess. paused
// true means a bucket was spent and nothing was written.
func (limit limiter) reserve(
	ctx context.Context, now time.Time, buckets []bucket,
) (held reservation, paused bool, err error) {
	conn, err := limit.database.Conn(ctx)
	if err != nil {
		return "", false, err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return "", false, err
	}
	committed := false
	defer func() {
		if !committed {
			rollBack(ctx, conn)
		}
	}()
	paused, err = spent(ctx, conn, now, buckets)
	if err != nil || paused {
		return "", paused, err
	}
	held, err = hold(ctx, conn, now, buckets)
	if err != nil {
		return "", false, err
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return "", false, err
	}
	committed = true
	return held, false, nil
}

// rollBack ends a transaction reserve did not commit. A connection that
// cannot roll back is evicted, because back in the pool it would hold
// the write lock for whoever came next.
func rollBack(ctx context.Context, conn *sql.Conn) {
	_, err := conn.ExecContext(context.WithoutCancel(ctx), "ROLLBACK")
	if err != nil {
		conn.Raw(func(any) error { return driver.ErrBadConn })
	}
}

func spent(
	ctx context.Context, conn *sql.Conn, now time.Time, buckets []bucket,
) (bool, error) {
	since := missStamp(now.Add(-missWindow))
	for _, each := range buckets {
		var misses int
		err := conn.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM signin_failures
			 WHERE bucket = ? AND at > ?`, each.key, since).Scan(&misses)
		if err != nil {
			return false, err
		}
		if misses >= each.limit {
			return true, nil
		}
	}
	return false, nil
}

func hold(
	ctx context.Context, conn *sql.Conn, now time.Time, buckets []bucket,
) (reservation, error) {
	held := reservation(rand.Text())
	for _, each := range buckets {
		_, err := conn.ExecContext(ctx,
			`INSERT INTO signin_failures (bucket, at, attempt)
			 VALUES (?, ?, ?)`, each.key, missStamp(now), string(held))
		if err != nil {
			return "", err
		}
	}
	return held, nil
}

// release gives a successful attempt its budget back: only misses are
// counted, so a person who signs in is not paused by their own
// sign-ins.
func (limit limiter) release(ctx context.Context, held reservation) error {
	_, err := limit.database.ExecContext(ctx,
		`DELETE FROM signin_failures WHERE attempt = ?`, string(held))
	return err
}

// sweep is called from the app's sweep loop: rows from before the
// window no longer count, so keeping them only grows the table.
func (limit limiter) sweep(ctx context.Context, now time.Time) error {
	_, err := limit.database.ExecContext(ctx,
		`DELETE FROM signin_failures WHERE at < ?`,
		missStamp(now.Add(-missWindow)))
	return err
}
