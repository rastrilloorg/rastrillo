package roster

import (
	"context"
	"time"
)

// confirmLease reports errLeaseLost once another process has taken the
// lease. A lapsed lease that no one took is still ours: nothing else ran.
func (receiver *Receiver) confirmLease(
	parent context.Context, lease int64,
) error {
	ctx, cancel := bookkeeping(parent)
	defer cancel()
	var until int64
	err := receiver.db.QueryRowContext(ctx,
		`SELECT lease_until FROM roster_state WHERE id = 1`).Scan(&until)
	if err != nil {
		return err
	}
	if until != lease {
		return errLeaseLost
	}
	return nil
}

// bookkeeping is the context for lease and state writes after the Store's.
// It is not the apply's, which a slow Store may have used up, and without
// these writes a lost lease would go unnoticed.
func bookkeeping(parent context.Context) (context.Context, func()) {
	return context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
}

// takeLease serialises applies across processes sharing the database. It
// returns the lease's expiry, which also names it: a process that takes a
// lapsed lease writes a later one.
func (receiver *Receiver) takeLease(ctx context.Context) (int64, error) {
	_, err := receiver.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO roster_state
		   (id, sequence, state, lease_until) VALUES (1, 0, 'active', 0)`)
	if err != nil {
		return 0, err
	}
	now := receiver.now()
	lease := now.Add(leaseLife).Unix()
	result, err := receiver.db.ExecContext(ctx,
		`UPDATE roster_state SET lease_until = ?
		 WHERE id = 1 AND lease_until < ?`,
		lease, now.Unix())
	if err != nil {
		return 0, err
	}
	taken, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if taken == 0 {
		return 0, errLeaseHeld
	}
	return lease, nil
}

// releaseLease clears only our own value, because a lapsed lease may
// already belong to another process.
func (receiver *Receiver) releaseLease(parent context.Context, lease int64) {
	ctx, cancel := bookkeeping(parent)
	defer cancel()
	_, err := receiver.db.ExecContext(ctx,
		`UPDATE roster_state SET lease_until = 0
		 WHERE id = 1 AND lease_until = ?`, lease)
	if err != nil {
		receiver.logger.Error("roster lease not released", "error", err)
	}
}
