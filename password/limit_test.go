package password

import (
	"fmt"
	"testing"
	"time"
)

// The limiter's clock is passed in by its callers, so these tests
// drive it with explicit instants instead of sleeping.

func TestLimiterBlocksAfterMaxFailures(t *testing.T) {
	l := newLimiter()
	now := time.Now()
	for i := 0; i < maxFailures; i++ {
		if l.blocked("a@example.com", now) {
			t.Fatalf("blocked after %d failures, want unblocked below %d", i, maxFailures)
		}
		l.fail("a@example.com", now)
	}
	if !l.blocked("a@example.com", now) {
		t.Errorf("not blocked after %d failures", maxFailures)
	}
	if l.blocked("b@example.com", now) {
		t.Errorf("unrelated key blocked — budgets must be per email")
	}
}

func TestLimiterWindowAgesOut(t *testing.T) {
	l := newLimiter()
	now := time.Now()
	for i := 0; i < maxFailures; i++ {
		l.fail("a@example.com", now)
	}
	if !l.blocked("a@example.com", now) {
		t.Fatalf("not blocked at the window's start")
	}
	later := now.Add(failureWindow)
	if l.blocked("a@example.com", later) {
		t.Errorf("still blocked after the window elapsed")
	}
}

func TestLimiterClearResetsBudget(t *testing.T) {
	l := newLimiter()
	now := time.Now()
	for i := 0; i < maxFailures-1; i++ {
		l.fail("a@example.com", now)
	}
	l.clear("a@example.com")
	l.fail("a@example.com", now)
	if l.blocked("a@example.com", now) {
		t.Errorf("blocked after clear plus one failure — clear must reset the budget")
	}
}

func TestLimiterPrunesStaleKeysPastCap(t *testing.T) {
	l := newLimiter()
	now := time.Now()
	for i := 0; i <= limitKeyCap; i++ {
		l.fail(fmt.Sprintf("u%d@example.com", i), now)
	}
	// Every key above is stale by now+window; the next recording pass
	// must sweep them rather than let the map grow forever.
	l.fail("fresh@example.com", now.Add(failureWindow))
	l.mu.Lock()
	n := len(l.fails)
	l.mu.Unlock()
	if n != 1 {
		t.Errorf("tracked keys after sweep = %d, want 1 (only the fresh key)", n)
	}
}
