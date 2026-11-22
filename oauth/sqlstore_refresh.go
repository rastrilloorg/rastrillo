package oauth

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

func (store *sqlStore) RotateRefresh(
	ctx context.Context, old []byte, next Refresh, now time.Time,
) (conn Connection, reused bool, err error) {
	err = store.write(ctx, func(tx *sql.Conn) error {
		var err error
		if conn, err = refreshOwner(ctx, tx, old, now); err != nil {
			return err
		}
		if next.Connection != conn.ID {
			return errors.New(
				"oauth: next token belongs to another connection")
		}
		if reused, err = retire(ctx, tx, old, now); err != nil || reused {
			return err
		}
		if err := replace(ctx, tx, next, now); err != nil {
			return err
		}
		conn.LastUsed = storedTime(now)
		return nil
	})
	if err != nil {
		return Connection{}, false, err
	}
	return conn, reused, nil
}

// refreshOwner returns the live connection a refresh token belongs to.
// A retired token is kept past its expiry while its connection lives,
// so a late replay is still reuse.
func refreshOwner(
	ctx context.Context, tx *sql.Conn, old []byte, now time.Time,
) (Connection, error) {
	var connection, expires string
	var usedAt sql.NullString
	err := tx.QueryRowContext(ctx, `
SELECT connection, expires_at, used_at FROM oauth_refresh WHERE hash = ?`,
		old).Scan(&connection, &expires, &usedAt)
	if err != nil {
		return Connection{}, notFound(err)
	}
	until, err := parseTime(expires)
	if err != nil {
		return Connection{}, err
	}
	if !usedAt.Valid && !until.After(now) {
		return Connection{}, ErrNotFound
	}
	conn, err := scanConnection(tx.QueryRowContext(ctx,
		`SELECT `+connectionColumns+` FROM oauth_connections
WHERE id = ?`, connection))
	if err != nil {
		return Connection{}, err
	}
	if conn.Ended != nil {
		return Connection{}, ErrNotFound
	}
	return conn, nil
}

// retire marks old used at now; reused reports it was used already.
func retire(
	ctx context.Context, tx *sql.Conn, old []byte, now time.Time,
) (reused bool, err error) {
	result, err := tx.ExecContext(ctx, `
UPDATE oauth_refresh SET used_at = ? WHERE hash = ? AND used_at IS NULL`,
		formatTime(now), old)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected == 0, nil
}

// replace sweeps refresh tokens nobody can use any more, stores next,
// and marks its connection used at now. Retired tokens of a live
// connection stay, to catch a replay.
func replace(
	ctx context.Context, tx *sql.Conn, next Refresh, now time.Time,
) error {
	if _, err := tx.ExecContext(ctx, `
DELETE FROM oauth_refresh WHERE expires_at <= ? AND (used_at IS NULL OR
  connection NOT IN (SELECT id FROM oauth_connections
                     WHERE ended_at IS NULL AND last_used >= ?))`,
		formatTime(now), formatTime(now.Add(-idleLife))); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO oauth_refresh (hash, connection, expires_at, used_at)
VALUES (?, ?, ?, NULL)`, next.Hash, next.Connection,
		formatTime(next.Expires)); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
UPDATE oauth_connections SET last_used = ? WHERE id = ?`,
		formatTime(now), next.Connection)
	return err
}

func (store *sqlStore) SaveRefresh(ctx context.Context, refresh Refresh) error {
	_, err := store.db.ExecContext(ctx, `
INSERT INTO oauth_refresh (hash, connection, expires_at, used_at)
VALUES (?, ?, ?,
	NULL)`, refresh.Hash, refresh.Connection, formatTime(refresh.Expires))
	return err
}

func (store *sqlStore) RefreshConnection(
	ctx context.Context, hash []byte, now time.Time,
) (Connection, bool, error) {
	var used sql.NullString
	var connection string
	err := store.db.QueryRowContext(ctx, `
SELECT connection, used_at FROM oauth_refresh
WHERE hash = ? AND (expires_at > ? OR used_at IS NOT NULL)`,
		hash, formatTime(now)).Scan(
		&connection, &used)
	if err != nil {
		return Connection{}, false, notFound(err)
	}
	found, err := store.Connection(ctx, connection)
	if err != nil {
		return Connection{}, false, err
	}
	if found.Ended != nil {
		return Connection{}, false, ErrNotFound
	}
	return found, used.Valid, nil
}
