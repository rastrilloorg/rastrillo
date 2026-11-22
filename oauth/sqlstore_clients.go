package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

func (store *sqlStore) SaveClient(ctx context.Context, client Client) error {
	redirects, err := json.Marshal(client.Redirects)
	if err != nil {
		return err
	}
	_, err = store.db.ExecContext(ctx, `
INSERT INTO oauth_clients
  (id, name, redirects, document, registered_at, used_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT (id) DO UPDATE SET
  name = excluded.name, redirects = excluded.redirects,
  document = excluded.document, used_at = excluded.used_at`,
		client.ID, client.Name, string(redirects), client.Document,
		formatTime(client.RegisteredAt), formatTime(client.UsedAt))
	return err
}

func (store *sqlStore) Client(ctx context.Context, id string) (Client, error) {
	var (
		client             Client
		redirects          string
		registered, usedAt string
	)
	err := store.db.QueryRowContext(ctx, `
SELECT id, name, redirects, document, registered_at, used_at
FROM oauth_clients WHERE id = ?`, id).Scan(
		&client.ID, &client.Name, &redirects, &client.Document, &registered,
		&usedAt)
	if err != nil {
		return Client{}, notFound(err)
	}
	if err := json.Unmarshal([]byte(redirects), &client.Redirects); err != nil {
		return Client{}, fmt.Errorf("oauth: stored redirects: %w", err)
	}
	if client.RegisteredAt, err = parseTime(registered); err != nil {
		return Client{}, err
	}
	if client.UsedAt, err = parseTime(usedAt); err != nil {
		return Client{}, err
	}
	return client, nil
}

func (store *sqlStore) PruneClients(
	ctx context.Context, unusedSince time.Time,
) error {
	_, err := store.db.ExecContext(ctx,
		`DELETE FROM oauth_clients WHERE used_at < ? AND id NOT IN (
  SELECT client_id FROM oauth_connections
  WHERE ended_at IS NULL AND last_used >= ?)`,
		formatTime(unusedSince), formatTime(unusedSince))
	return err
}
