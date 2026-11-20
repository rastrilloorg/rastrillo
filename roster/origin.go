package roster

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
)

// announce calls OnOrigin when the snapshot just applied changed the
// origin. The answer is flushed first, so the root has it before the app
// starts to stop and does not retry a snapshot that was applied.
//
// It runs on the request's goroutine. The app fixed its origin at boot,
// so OnOrigin should start a graceful shutdown and let the platform
// start it on the new one, without waiting for the server to stop: that
// waits for this request.
func (receiver *Receiver) announce(
	response http.ResponseWriter, previous, next string,
) {
	if previous == next || receiver.onOrigin == nil {
		return
	}
	http.NewResponseController(response).Flush()
	receiver.onOrigin(previous, next)
}

// Origin is the custom origin in the last applied snapshot, or "" when
// the instance serves on its own host.
func (receiver *Receiver) Origin(ctx context.Context) (string, error) {
	return StoredOrigin(ctx, receiver.db)
}

// StoredOrigin is Receiver.Origin for boot, before there is a receiver:
// the app reads it after migrations to choose the origin it serves on.
func StoredOrigin(ctx context.Context, db *sql.DB) (string, error) {
	var origin string
	err := db.QueryRowContext(ctx,
		`SELECT origin FROM roster_state WHERE id = 1`).Scan(&origin)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return origin, err
}
