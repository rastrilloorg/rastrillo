package oauth

import (
	"context"
	"time"
)

// Store keeps what the oauth flow remembers. The SQL store is the one
// implementation; the interface is the seam a later grants design
// replaces, so every answer it gives is stated here.
//
// ErrNotFound is the one refusal for a thing that is absent, expired,
// ended, or not the caller's.
type Store interface {
	// SaveClient inserts client or updates its name, redirects and UsedAt;
	// RegisteredAt is kept from the first save.
	SaveClient(ctx context.Context, client Client) error
	Client(ctx context.Context, id string) (Client, error)
	// PruneClients deletes clients last used before unusedSince,
	// except those with a live connection used since then.
	PruneClients(ctx context.Context, unusedSince time.Time) error
	// SavePending stores pending and sweeps approvals that had expired
	// by the time it was made. It keeps the client's name as the page
	// showed it, for the connection, and whether a state was sent at
	// all.
	SavePending(ctx context.Context, pending Pending) error
	// Pending reads an approval without taking it; the same refusals as
	// TakePending.
	Pending(
		ctx context.Context, id, subject string, now time.Time,
	) (Pending, error)
	// TakePending deletes and returns an approval; ErrNotFound if absent,
	// expired at now, or another subject's.
	TakePending(
		ctx context.Context, id, subject string, now time.Time,
	) (Pending, error)
	// CreateConnection stores connection and its first code in one
	// transaction. code.Connection must be connection.ID.
	CreateConnection(ctx context.Context, connection Connection,
		code Code) error
	// Connection returns the connection whether or not it has ended.
	Connection(ctx context.Context, id string) (Connection, error)
	// ConnectionsOf lists subject's live connections, newest Created
	// first: not ended, and used within 30 days of now.
	ConnectionsOf(
		ctx context.Context, subject string, now time.Time,
	) ([]Connection, error)
	// EndConnection ends the connection at at and deletes its refresh
	// tokens. Ending twice is not an error; an unknown id is
	// ErrNotFound.
	EndConnection(ctx context.Context, id string, at time.Time) error
	// Code returns the code without taking it, whether or not it was
	// taken before; the same refusals as TakeCode.
	Code(ctx context.Context, hash []byte, now time.Time) (Code, error)
	// TakeCode marks the code taken at now and returns it, keeping it as
	// a tombstone until it expires; used reports a code taken before.
	// ErrNotFound if absent, expired at now, or its connection ended.
	TakeCode(
		ctx context.Context, hash []byte, now time.Time,
	) (code Code, used bool, err error)
	// RotateRefresh, in one transaction, retires old, stores next
	// and sets the connection's LastUsed to now; reused reports old
	// was retired already, and then changes nothing. Exactly one of
	// two racing callers gets reused == false, and a failure anywhere
	// rolls back all three. ErrNotFound if old is absent, unretired and
	// expired, or its connection ended; next.Connection must be old's
	// connection. A retired token is kept past its expiry while its
	// connection lives, so a late replay is still reuse.
	RotateRefresh(
		ctx context.Context, old []byte, next Refresh, now time.Time,
	) (connection Connection, reused bool, err error)
	SaveRefresh(ctx context.Context, refresh Refresh) error
	// RefreshConnection returns the connection a refresh token belongs
	// to without touching it; retired reports a token already rotated.
	// ErrNotFound if the hash is unknown, unretired and expired at now,
	// or its connection ended.
	RefreshConnection(
		ctx context.Context, hash []byte, now time.Time,
	) (connection Connection, retired bool, err error)
}
