package oauth

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"time"

	rastrillo "amadan.net/rastrillo/rastrillo"
)

// NewSQLStore returns a Store over db, which must have Schema applied.
//
// Writes that read before they write run in a BEGIN IMMEDIATE
// transaction on one pinned connection, as migrate.Apply does: the
// writer lock is taken up front, and a busy writer waits out the
// database's busy_timeout instead of failing.
func NewSQLStore(db *sql.DB) Store { return &sqlStore{db: db} }

type sqlStore struct{ db *sql.DB }

const (
	// pendingLife is how long an approval waits, so SavePending can
	// tell which stored ones had expired when it was made.
	pendingLife = 10 * time.Minute
	// idleLife is how long a connection may go unused before it
	// counts as ended.
	idleLife = 30 * 24 * time.Hour
)

// querier is what *sql.DB and *sql.Conn share.
type querier interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func formatTime(at time.Time) string { return at.UTC().Format(time.RFC3339) }

// storedTime is at as a read of what formatTime stored would give it.
func storedTime(at time.Time) time.Time {
	return at.UTC().Truncate(time.Second)
}

func parseTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("oauth: stored time: %w", err)
	}
	return parsed, nil
}

func parseAccess(value string) (rastrillo.Access, error) {
	switch value {
	case "read":
		return rastrillo.Read, nil
	case "write":
		return rastrillo.Write, nil
	}
	return 0, fmt.Errorf("oauth: stored access %q is not read or write", value)
}

// write runs fn inside BEGIN IMMEDIATE and commits if fn succeeds. Any
// error, or a panic, rolls everything back.
func (store *sqlStore) write(
	ctx context.Context, fn func(tx *sql.Conn) error,
) error {
	conn, err := store.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return err
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		// The caller's context may be the reason for the failure.
		_, err := conn.ExecContext(context.WithoutCancel(ctx), "ROLLBACK")
		if err != nil {
			// Never hand the pool a connection that may still hold
			// the write lock; database/sql would not drop it itself.
			conn.Raw(func(any) error { return driver.ErrBadConn })
		}
	}()
	if err := fn(conn); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return err
	}
	committed = true
	return nil
}

func notFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
