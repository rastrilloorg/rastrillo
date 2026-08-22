package password

import (
	"sync"
	"time"
)

// tooManyAttempts is the message a rate-limited sign-in renders. It is
// deliberately distinct from wrongCredentials: the limit trips on
// attempt volume alone, before the email is ever looked up, so the
// message reveals nothing about whether the account exists.
const tooManyAttempts = "Too many sign-in attempts. Try again in a few minutes."

// The sign-in limiter's policy, fixed rather than configurable until a
// real deployment needs otherwise: maxFailures failed attempts against
// one email inside failureWindow block further attempts until the
// oldest failure ages out. 15 minutes matches the window auth's keymail
// flow gets from signin.NewMemoryLimiter.
//
// Keying by email (not client IP) is the deliberate trade-off: it is
// what stops a credential-guessing run against one account, it works
// behind any proxy topology, and the cost — someone hammering a
// victim's email can lock the victim out too — is bounded to the
// window, never a permanent lockout. IP-level throttling of signup
// spam and distributed guessing stays a deployment concern (the
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

// limiter tracks recent sign-in failures per key, in memory. Like
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
