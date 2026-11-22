package oauth

import (
	"context"
	"database/sql"
	"time"
)

func (store *sqlStore) SavePending(ctx context.Context, pending Pending) error {
	return store.write(ctx, func(tx *sql.Conn) error {
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM oauth_pending WHERE expires_at <= ?`,
			formatTime(pending.Expires.Add(-pendingLife))); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `
INSERT INTO oauth_pending (`+pendingColumns+`)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			pending.ID, pending.Subject, pending.ClientID, pending.ClientName,
			pending.Redirect, pending.Challenge, pending.Resource,
			sql.NullString{String: pending.State, Valid: pending.StateSent},
			pending.Return, pending.Requested.String(),
			formatTime(pending.Expires))
		return err
	})
}

const pendingColumns = `id, subject, client_id, client_name, redirect,
  challenge, resource, state, return_to, requested, expires_at`

func scanPending(row *sql.Row) (Pending, error) {
	var (
		pending          Pending
		state            sql.NullString
		requested, until string
	)
	err := row.Scan(&pending.ID, &pending.Subject, &pending.ClientID,
		&pending.ClientName, &pending.Redirect, &pending.Challenge,
		&pending.Resource, &state, &pending.Return, &requested, &until)
	if err != nil {
		return Pending{}, notFound(err)
	}
	pending.State, pending.StateSent = state.String, state.Valid
	if pending.Requested, err = parseAccess(requested); err != nil {
		return Pending{}, err
	}
	if pending.Expires, err = parseTime(until); err != nil {
		return Pending{}, err
	}
	return pending, nil
}

func readPending(
	ctx context.Context, reader querier, id, subject string, now time.Time,
) (Pending, error) {
	pending, err := scanPending(reader.QueryRowContext(ctx,
		`SELECT `+pendingColumns+` FROM oauth_pending WHERE id = ?`, id))
	if err != nil {
		return Pending{}, err
	}
	if pending.Subject != subject || !pending.Expires.After(now) {
		return Pending{}, ErrNotFound
	}
	return pending, nil
}

func (store *sqlStore) Pending(
	ctx context.Context, id, subject string, now time.Time,
) (Pending, error) {
	return readPending(ctx, store.db, id, subject, now)
}

func (store *sqlStore) TakePending(
	ctx context.Context, id, subject string, now time.Time,
) (Pending, error) {
	var pending Pending
	err := store.write(ctx, func(tx *sql.Conn) error {
		var err error
		if pending, err = readPending(ctx, tx, id, subject, now); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx,
			`DELETE FROM oauth_pending WHERE id = ?`, id)
		return err
	})
	if err != nil {
		return Pending{}, err
	}
	return pending, nil
}
