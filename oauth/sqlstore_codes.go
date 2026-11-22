package oauth

import (
	"context"
	"database/sql"
	"time"
)

func (store *sqlStore) Code(
	ctx context.Context, hash []byte, now time.Time,
) (Code, error) {
	return readCode(ctx, store.db, hash, now)
}

// readCode reads a code of a live connection, taken or not, unless it
// has expired at now.
func readCode(
	ctx context.Context, reader querier, hash []byte, now time.Time,
) (Code, error) {
	code := Code{Hash: hash}
	var expires string
	err := reader.QueryRowContext(ctx, `
SELECT connection, client_id, redirect, challenge, resource, expires_at
FROM oauth_codes WHERE hash = ? AND connection IN (
  SELECT id FROM oauth_connections WHERE ended_at IS NULL)`,
		hash).Scan(&code.Connection,
		&code.ClientID, &code.Redirect, &code.Challenge, &code.Resource,
		&expires)
	if err != nil {
		return Code{}, notFound(err)
	}
	if code.Expires, err = parseTime(expires); err != nil {
		return Code{}, err
	}
	if !code.Expires.After(now) {
		return Code{}, ErrNotFound
	}
	return code, nil
}

func (store *sqlStore) TakeCode(
	ctx context.Context, hash []byte, now time.Time,
) (code Code, used bool, err error) {
	err = store.write(ctx, func(tx *sql.Conn) error {
		res, err := tx.ExecContext(ctx, `
UPDATE oauth_codes SET used_at = ?
WHERE hash = ? AND used_at IS NULL AND expires_at > ?
  AND connection IN (SELECT id FROM oauth_connections
                     WHERE ended_at IS NULL)`,
			formatTime(now), hash, formatTime(now))
		if err != nil {
			return err
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return err
		}
		used = affected == 0
		code, err = readCode(ctx, tx, hash, now)
		return err
	})
	if err != nil {
		return Code{}, false, err
	}
	return code, used, nil
}
