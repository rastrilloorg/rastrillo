package pow

import (
	"sync"
	"time"
)

// attempts counts admissions per token, in process. It bounds handler
// work, not replay: a solved token that keeps failing the handler's own
// validation would otherwise be free work for its whole lifetime.
// Replay is the durable ledger's job, so losing this on restart is fine.
type attempts struct {
	mu        sync.Mutex
	limit     int
	max       int
	m         map[string]attempt
	nextPrune time.Time
}

type attempt struct {
	n       int
	expires time.Time
}

func newAttempts(limit, max int) *attempts {
	return &attempts{limit: limit, max: max, m: make(map[string]attempt)}
}

// take counts one admission. Entries live until their token expires,
// never until a commit: the commit's insert succeeding says nothing
// about the caller's transaction, and releasing on it let admit, spend,
// business refusal, rollback repeat forever on one solve.
//
// At capacity a NEW token is refused and nothing live is evicted.
// Evicting the oldest would let an attacker cycling one more token than
// capacity reset every allowance without a new solve.
func (t *attempts) take(nonce string, expires, now time.Time) Reason {
	t.mu.Lock()
	defer t.mu.Unlock()
	// At most once a second: at capacity every new token would otherwise
	// pay a scan of the whole map.
	if !now.Before(t.nextPrune) {
		for k, e := range t.m {
			if now.After(e.expires) {
				delete(t.m, k)
			}
		}
		t.nextPrune = now.Add(time.Second)
	}
	e, ok := t.m[nonce]
	if !ok {
		if len(t.m) >= t.max {
			return ReasonBusy
		}
		e = attempt{expires: expires}
	}
	if e.n >= t.limit {
		return ReasonAttempts
	}
	e.n++
	t.m[nonce] = e
	return ""
}
