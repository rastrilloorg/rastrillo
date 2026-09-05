package pow_test

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/db"
	"amadan.net/rastrillo/rastrillo/migrate"
	"amadan.net/rastrillo/rastrillo/pow"
)

func newTestStore(t *testing.T) pow.NonceStore {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "pow.db"), nil)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	if _, err := migrate.Apply(context.Background(), d, pow.Schema); err != nil {
		t.Fatalf("migrate.Apply: %v", err)
	}
	return pow.SQLNonces(d.Writer())
}

func TestSQLNoncesSpendsOnce(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	expires := time.Now().Add(time.Hour)

	fresh, err := s.Spend(ctx, "abc", expires)
	if err != nil {
		t.Fatalf("Spend: %v", err)
	}
	if !fresh {
		t.Fatal("the first spend reported the nonce already used")
	}
	fresh, err = s.Spend(ctx, "abc", expires)
	if err != nil {
		t.Fatalf("Spend: %v", err)
	}
	if fresh {
		t.Fatal("a nonce was spent twice")
	}
}

func TestSQLNoncesSpendIsAtomicUnderConcurrency(t *testing.T) {
	// The race single use exists to close: a SELECT then an INSERT lets
	// two concurrent replays both see an empty table and both proceed.
	// Exactly one caller must win.
	s := newTestStore(t)
	const racers = 8

	var wg sync.WaitGroup
	results := make(chan bool, racers)
	start := make(chan struct{})
	for range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			fresh, err := s.Spend(context.Background(), "contested", time.Now().Add(time.Hour))
			if err != nil {
				t.Errorf("Spend: %v", err)
				return
			}
			results <- fresh
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	won := 0
	for fresh := range results {
		if fresh {
			won++
		}
	}
	if won != 1 {
		t.Fatalf("%d of %d concurrent spends were told the nonce was fresh, want exactly 1", won, racers)
	}
}

func TestSQLNoncesSweepsWhatHasExpired(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now()

	if _, err := s.Spend(ctx, "old", now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Spend(ctx, "live", now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.Sweep(now); err != nil {
		t.Fatalf("Sweep: %v", err)
	}

	// The sweep is housekeeping, never correctness: an expired
	// challenge is refused on age long before its nonce is consulted.
	// So the observable effect is only that the row is gone.
	if fresh, _ := s.Spend(ctx, "old", now.Add(time.Hour)); !fresh {
		t.Error("an expired nonce survived the sweep")
	}
	if fresh, _ := s.Spend(ctx, "live", now.Add(time.Hour)); fresh {
		t.Error("a live nonce was swept")
	}
}

func TestGuardSweepReachesTheStore(t *testing.T) {
	s := newTestStore(t)
	g, err := pow.New(pow.Config{InstanceKey: "k", Nonces: s})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Spend(context.Background(), "old", time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := g.Sweep(time.Now()); err != nil {
		t.Fatalf("Guard.Sweep: %v", err)
	}
	if fresh, _ := s.Spend(context.Background(), "old", time.Now().Add(time.Hour)); !fresh {
		t.Error("Guard.Sweep did not reach the store")
	}
}
