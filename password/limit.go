package password

import (
	"sync"
	"time"
)

// tooManyAttempts is the message a rate-limited sign-in or sign-up
// renders. It is deliberately distinct from wrongCredentials: the
// limit trips on attempt volume alone, before the email is ever looked
// up, so the message reveals nothing about whether the account exists.
const tooManyAttempts = "Too many attempts. Try again in a few minutes."

// The limiter's policy, fixed rather than configurable until a real
// deployment needs otherwise: maxFailures failed attempts against one
// email inside failureWindow block further attempts until the oldest
// failure ages out. 15 minutes matches the window auth's keymail flow
// gets from signin.NewMemoryLimiter.
//
// Handlers keeps two independent limiters of this type, and which one
// a failure lands in is a security decision, not bookkeeping:
//
//   - The shared budget is spent by wrong credentials at sign-in and
//     by the duplicate-email answer at sign-up, and it gates both
//     doors. Those two failures leak the same fact — whether an
//     address is registered — so letting an attacker switch endpoints
//     for a fresh allowance would defeat the limit.
//
//   - The refusal budget is spent only by a Config.Create policy
//     refusal, and gates sign-up alone. A refusal must cost something,
//     because Hash runs before Create and an unmetered refusal path is
//     a PBKDF2 amplifier. It must not cost the shared budget: a
//     refused address is by definition one the attacker does not have
//     an account for, so charging sign-in for it would let a stranger
//     who merely knows an invitee's address post ten refused signups
//     and hold that address at 429 on both doors, renewing it at about
//     one request every 90 seconds. Nothing an unregistered address's
//     visitor does should be able to lock its future owner out.
//
// Keying by email (not client IP) is the deliberate trade-off: it is
// what stops a credential-guessing run against one account, it works
// behind any proxy topology, and the cost — someone hammering a
// victim's email can lock the victim out of sign-in too — is bounded
// to the window, never a permanent lockout. IP-level throttling of
// signup spam and distributed guessing stays a deployment concern (the
// platform's reverse proxy), as SKILL.md says.
const (
	maxFailures   = 10
	failureWindow = 15 * time.Minute
)

// limitKeyCap bounds the limiter's memory: past this many tracked
// emails, every recording pass also drops keys whose failures have all
// aged out. A run of distinct emails can't grow the map without bound
// faster than the window drains it.
const limitKeyCap = 4096

// limiter tracks recent failures per key, in memory. Like
// signin's memory limiter, state dies with the process — an acceptable
// reset on a platform that restarts apps rarely, and per-instance by
// construction.
type limiter struct {
	mu    sync.Mutex
	fails map[string][]time.Time
}

func newLimiter() *limiter {
	return &limiter{fails: map[string][]time.Time{}}
}

// prune drops key's failures older than the window, removing the key
// entirely when none remain. Callers hold mu.
func (l *limiter) prune(key string, now time.Time) []time.Time {
	kept := l.fails[key][:0]
	for _, at := range l.fails[key] {
		if now.Sub(at) < failureWindow {
			kept = append(kept, at)
		}
	}
	if len(kept) == 0 {
		delete(l.fails, key)
		return nil
	}
	l.fails[key] = kept
	return kept
}

// blocked reports whether key has exhausted its failure budget.
func (l *limiter) blocked(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.prune(key, now)) >= maxFailures
}

// fail records one failed attempt against key.
func (l *limiter) fail(key string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.fails) > limitKeyCap {
		for k := range l.fails {
			l.prune(k, now)
		}
	}
	l.fails[key] = append(l.prune(key, now), now)
}

// clear forgets key — a successful sign-in proves the visitor is the
// account holder, so their earlier typos shouldn't linger toward a
// block.
func (l *limiter) clear(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.fails, key)
}
