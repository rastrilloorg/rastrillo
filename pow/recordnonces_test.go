package pow

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/objrec"
)

// recordNonce is mixed-case base64url, as a real nonce is, so every test
// shows that hashing it makes it a valid objrec key.
const recordNonce = "AbC-dEf_GhIjKlMnOpQrSt"

var recordEpoch = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

// newRecordNonces returns a store on a temp dir with a clock the test moves.
func newRecordNonces(t *testing.T, store objrec.Store) (*recordNonces, *time.Time) {
	t.Helper()
	if store == nil {
		store = objrec.Dir(t.TempDir())
	}
	now := recordEpoch
	n := RecordNonces(store).(*recordNonces)
	n.now = func() time.Time { return now }
	return n, &now
}

func listAll(t *testing.T, s objrec.Store) []string {
	t.Helper()
	keys, _, err := s.List(context.Background(), "", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	return keys
}

func TestRecordNoncesSpendOnce(t *testing.T) {
	n, now := newRecordNonces(t, nil)
	exp := now.Add(time.Minute)
	for i, want := range []bool{true, false} {
		got, err := n.Spend(context.Background(), nil, recordNonce, exp)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("spend %d: got %v, want %v", i+1, got, want)
		}
	}
}

func TestRecordNoncesRaceHasOneWinner(t *testing.T) {
	n, now := newRecordNonces(t, nil)
	exp := now.Add(time.Minute)
	var wins atomic.Int32
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := n.Spend(context.Background(), nil, recordNonce, exp)
			if err != nil {
				t.Error(err)
			}
			if ok {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("%d goroutines spent the nonce, want exactly 1", wins.Load())
	}
}

func TestRecordNoncesRefuseAnExpiredSpend(t *testing.T) {
	store := objrec.Dir(t.TempDir())
	n, now := newRecordNonces(t, store)
	exp := now.Add(-time.Second)
	ok, err := n.Spend(context.Background(), nil, recordNonce, exp)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("an expired nonce was spent")
	}
	if keys := listAll(t, store); len(keys) != 0 {
		t.Fatalf("an expired spend wrote %v, want nothing", keys)
	}
}

func TestRecordNoncesSpentSeesASpend(t *testing.T) {
	n, now := newRecordNonces(t, nil)
	ctx := context.Background()
	if spent, err := n.Spent(ctx, recordNonce); err != nil || spent {
		t.Fatalf("before the spend: spent=%v err=%v, want false", spent, err)
	}
	if ok, err := n.Spend(ctx, nil, recordNonce, now.Add(time.Minute)); err != nil || !ok {
		t.Fatalf("spend: ok=%v err=%v", ok, err)
	}
	if spent, err := n.Spent(ctx, recordNonce); err != nil || !spent {
		t.Fatalf("after the spend: spent=%v err=%v, want true", spent, err)
	}
	if spent, err := n.Spent(ctx, "another-nonce"); err != nil || spent {
		t.Fatalf("a different nonce: spent=%v err=%v, want false", spent, err)
	}
}

// slowCreate moves the clock past the expiry while Create is in flight,
// as a request stalled on a slow network would see it.
type slowCreate struct {
	objrec.Store
	advance func()
}

func (s slowCreate) Create(ctx context.Context, key string, body []byte) (objrec.Version, error) {
	v, err := s.Store.Create(ctx, key, body)
	s.advance()
	return v, err
}

func TestRecordNoncesRefuseASpendThatStalledPastExpiry(t *testing.T) {
	var now *time.Time
	store := slowCreate{
		Store:   objrec.Dir(t.TempDir()),
		advance: func() { *now = now.Add(2 * time.Minute) },
	}
	n, clock := newRecordNonces(t, store)
	now = clock
	ok, err := n.Spend(context.Background(), nil, recordNonce, clock.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("a spend that finished after its expiry was admitted")
	}
}

func TestRecordNoncesSweepKeepsTheMargin(t *testing.T) {
	store := objrec.Dir(t.TempDir())
	n, now := newRecordNonces(t, store)
	ctx := context.Background()
	old, recent := "OldNonce-AAAA", "RecentNonce-BBBB"
	// Spend while the nonces are still live, then move the clock on.
	for nonce, exp := range map[string]time.Time{
		old:    now.Add(time.Minute),
		recent: now.Add(3 * time.Minute),
	} {
		if ok, err := n.Spend(ctx, nil, nonce, exp); err != nil || !ok {
			t.Fatalf("spend %s: ok=%v err=%v", nonce, ok, err)
		}
	}
	for _, nonce := range []string{old, recent} {
		if spent, err := n.Spent(ctx, nonce); err != nil || !spent {
			t.Fatalf("%s is not recorded before the sweep", nonce)
		}
	}
	// old expired 11 minutes ago, recent 9 minutes ago.
	*now = recordEpoch.Add(time.Minute + 11*time.Minute)
	if err := n.Sweep(*now); err != nil {
		t.Fatal(err)
	}
	if spent, err := n.Spent(ctx, old); err != nil || spent {
		t.Errorf("a nonce expired 11 minutes ago: spent=%v err=%v, want swept", spent, err)
	}
	if spent, err := n.Spent(ctx, recent); err != nil || !spent {
		t.Errorf("a nonce expired 9 minutes ago: spent=%v err=%v, want kept inside the margin", spent, err)
	}
}

// smallPages caps every listing, so a sweep has to follow the cursor.
type smallPages struct{ objrec.Store }

func (s smallPages) List(ctx context.Context, prefix, cursor string, limit int) ([]string, string, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	return s.Store.List(ctx, prefix, cursor, limit)
}

// A sweep that stopped after a fixed number of deletions would leave a
// day's markers behind whenever the centre sweeps rarely.
func TestRecordNoncesSweepClearsEveryExpiredRecordAcrossPages(t *testing.T) {
	store := objrec.Dir(t.TempDir())
	n, now := newRecordNonces(t, smallPages{store})
	ctx := context.Background()
	const total = sweepBatch + 101
	expired := recordEpoch.Add(-time.Hour).UnixMilli()
	for i := range total {
		key := fmt.Sprintf("pow-nonces/%064x/%d", i, expired)
		if _, err := store.Create(ctx, key, []byte("{}")); err != nil {
			t.Fatal(err)
		}
	}
	live := fmt.Sprintf("pow-nonces/%064x/%d", total, now.Add(time.Minute).UnixMilli())
	if _, err := store.Create(ctx, live, []byte("{}")); err != nil {
		t.Fatal(err)
	}
	if err := n.Sweep(*now); err != nil {
		t.Fatal(err)
	}
	if keys := listAll(t, store); len(keys) != 1 || keys[0] != live {
		t.Fatalf("after the sweep %d keys remain, want only the live one", len(keys))
	}
}

func TestGuardStartsOnRecordNonces(t *testing.T) {
	_, err := New(Config{
		InstanceKey: "k",
		Nonces:      RecordNonces(objrec.Dir(t.TempDir())),
		ScriptURL:   "/a.js",
		WorkerURL:   "/w.js",
	})
	if err != nil {
		t.Fatalf("New on RecordNonces: %v", err)
	}
}
