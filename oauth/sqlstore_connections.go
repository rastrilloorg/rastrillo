package oauth

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const connectionColumns = `id, subject, client_id, client_name,
  return_host, resource, access, created_at, last_used, ended_at`

func scanConnection(row interface{ Scan(...any) error }) (Connection, error) {
	var (
		connection                Connection
		access, created, lastUsed string
		ended                     sql.NullString
	)
	err := row.Scan(&connection.ID, &connection.Subject,
		&connection.ClientID, &connection.ClientName, &connection.ReturnHost,
		&connection.Resource, &access, &created, &lastUsed, &ended)
	if err != nil {
		return Connection{}, notFound(err)
	}
	if connection.Access, err = parseAccess(access); err != nil {
		return Connection{}, err
	}
	if connection.Created, err = parseTime(created); err != nil {
		return Connection{}, err
	}
	if connection.LastUsed, err = parseTime(lastUsed); err != nil {
		return Connection{}, err
	}
	if ended.Valid {
		at, err := parseTime(ended.String)
		if err != nil {
			return Connection{}, err
		}
		connection.Ended = &at
	}
	return connection, nil
}

func (store *sqlStore) CreateConnection(
	ctx context.Context, connection Connection, code Code,
) error {
	if code.Connection != connection.ID {
		return errors.New("oauth: code belongs to another connection")
	}
	return store.write(ctx, func(conn *sql.Conn) error {
		if _, err := conn.ExecContext(ctx, `
INSERT INTO oauth_connections (`+connectionColumns+`)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, NULL)`,
			connection.ID, connection.Subject, connection.ClientID,
			connection.ClientName, connection.ReturnHost,
			connection.Resource, connection.Access.String(),
			formatTime(connection.Created),
			formatTime(connection.LastUsed)); err != nil {
			return err
		}
		// Spent codes stay as tombstones only until they expire; this
		// is the one place that sweeps them.
		if _, err := conn.ExecContext(ctx,
			`DELETE FROM oauth_codes WHERE expires_at <= ?`,
			formatTime(connection.Created)); err != nil {
			return err
		}
		_, err := conn.ExecContext(ctx, `
INSERT INTO oauth_codes (hash, connection, client_id, redirect, challenge,
  resource, expires_at, used_at)
VALUES (?, ?, ?, ?, ?, ?, ?, NULL)`,
			code.Hash, code.Connection, code.ClientID, code.Redirect,
			code.Challenge, code.Resource, formatTime(code.Expires))
		return err
	})
}

func (store *sqlStore) Connection(
	ctx context.Context, id string,
) (Connection, error) {
	return scanConnection(store.db.QueryRowContext(ctx,
		`SELECT `+connectionColumns+` FROM oauth_connections WHERE id = ?`,
		id))
}

func (store *sqlStore) ConnectionsOf(
	ctx context.Context, subject string, now time.Time,
) ([]Connection, error) {
	rows, err := store.db.QueryContext(ctx, `
SELECT `+connectionColumns+` FROM oauth_connections
WHERE subject = ? AND ended_at IS NULL AND last_used >= ?
ORDER BY created_at DESC, id`, subject, formatTime(now.Add(-idleLife)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Connection
	for rows.Next() {
		connection, err := scanConnection(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, connection)
	}
	return out, rows.Err()
}

func (store *sqlStore) EndConnection(
	ctx context.Context, id string, at time.Time,
) error {
	return store.write(ctx, func(tx *sql.Conn) error {
		res, err := tx.ExecContext(ctx, `
UPDATE oauth_connections SET ended_at = ?
WHERE id = ? AND ended_at IS NULL`, formatTime(at), id)
		if err != nil {
			return err
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if affected > 0 {
			// The retired tokens kept to catch a replay have
			// nothing left to protect.
			_, err := tx.ExecContext(ctx,
				`DELETE FROM oauth_refresh WHERE connection = ?`, id)
			return err
		}
		// Ending twice is not an error; ending a stranger is.
		var one int
		return notFound(tx.QueryRowContext(ctx,
			`SELECT 1 FROM oauth_connections WHERE id = ?`, id).Scan(&one))
	})
}
