package pow_test

import (
	"context"
	"database/sql"
	"errors"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"amadan.net/rastrillo/rastrillo/db"
	"amadan.net/rastrillo/rastrillo/migrate"
	"amadan.net/rastrillo/rastrillo/pow"
)

func openTestDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "pow.db"), nil)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func newTestStore(t *testing.T) (pow.NonceStore, *db.DB) {
	t.Helper()
	d := openTestDB(t)
	if _, err := migrate.Apply(context.Background(), d, pow.Schema); err != nil {
		t.Fatalf("migrate.Apply: %v", err)
	}
	return pow.SQLNonces(d.Writer()), d
}

func TestSQLNoncesSpendsOnce(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	expires := time.Now().Add(time.Hour)
	if fresh, err := s.Spend(ctx, nil, "abc", expires); err != nil || !fresh {
		t.Fatalf("first spend = %v, %v; want fresh", fresh, err)
	}
	if fresh, err := s.Spend(ctx, nil, "abc", expires); err != nil || fresh {
		t.Fatalf("second spend = %v, %v; want not fresh", fresh, err)
	}
	if spent, err := s.Spent(ctx, "abc"); err != nil || !spent {
		t.Fatalf("Spent = %v, %v; want true", spent, err)
	}
}

func TestSQLNoncesSpendIsAtomicUnderConcurrency(t *testing.T) {
	// SELECT-then-INSERT would let two replays both see an empty table.
	s, _ := newTestStore(t)
	ctx := context.Background()
	expires := time.Now().Add(time.Hour)
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fresh, err := s.Spend(ctx, nil, "race", expires)
			if err != nil {
				t.Errorf("Spend: %v", err)
				return
			}
			if fresh {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("%d concurrent spends won, want exactly 1", wins)
	}
}

func TestSQLNoncesRefusesAnExpiredSpendOnTheDatabaseClock(t *testing.T) {
	// The expiry is checked by the INSERT itself against SQLite's clock.
	// A handler that checked in Go and then stalled past expiry and a
	// sweep would otherwise insert into a slot the sweep had emptied.
	s, _ := newTestStore(t)
	ctx := context.Background()
	if fresh, err := s.Spend(ctx, nil, "late", time.Now().Add(-time.Second)); err != nil || fresh {
		t.Fatalf("spend past expiry = %v, %v; want refused", fresh, err)
	}
}

func TestSQLNoncesSpendRidesTheCallersTransaction(t *testing.T) {
	s, d := newTestStore(t)
	ctx := context.Background()
	tx, err := d.Writer().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if fresh, err := s.Spend(ctx, tx, "tx", time.Now().Add(time.Hour)); err != nil || !fresh {
		t.Fatalf("spend in tx = %v, %v", fresh, err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if spent, _ := s.Spent(ctx, "tx"); spent {
		t.Fatal("a rolled-back spend stayed spent: the caller's retry would be refused")
	}
}

func TestSQLNoncesSweepIsBoundedAndKeepsTheMargin(t *testing.T) {
	s, d := newTestStore(t)
	ctx := context.Background()
	w := d.Writer()
	old := time.Now().Add(-time.Hour).UnixMilli()
	recent := time.Now().Add(-5 * time.Minute).UnixMilli() // inside the 10-minute margin
	for i := 0; i < 600; i++ {
		if _, err := w.ExecContext(ctx, `INSERT INTO pow_spent_nonces (nonce, expires_ms) VALUES (?, ?)`,
			"old"+strconv.Itoa(i), old); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := w.ExecContext(ctx, `INSERT INTO pow_spent_nonces (nonce, expires_ms) VALUES ('recent', ?)`, recent); err != nil {
		t.Fatal(err)
	}
	if err := s.Sweep(time.Now()); err != nil {
		t.Fatal(err)
	}
	var left int
	w.QueryRowContext(ctx, `SELECT count(*) FROM pow_spent_nonces WHERE nonce LIKE 'old%'`).Scan(&left)
	if left != 100 {
		t.Fatalf("one sweep left %d old rows, want 100 (a batch of 500)", left)
	}
	if spent, _ := s.Spent(ctx, "recent"); !spent {
		t.Fatal("a row inside the 10-minute margin was swept")
	}
}

func TestSQLNoncesReadyNamesAMissingTable(t *testing.T) {
	d := openTestDB(t)
	err := pow.SQLNonces(d.Writer()).Ready(context.Background())
	if !errors.Is(err, pow.ErrNoSchema) {
		t.Fatalf("Ready on an empty database = %v, want ErrNoSchema", err)
	}
}

func newSQLGuard(t *testing.T, d *db.DB) *pow.Guard {
	t.Helper()
	g, err := pow.New(pow.Config{
		InstanceKey: "k", Nonces: pow.SQLNonces(d.Writer()), Difficulty: pow.NoProof,
		MinAge: time.Millisecond,
	})
	if err != nil {
		t.Fatalf("pow.New: %v", err)
	}
	return g
}

func formPost(f pow.Form) *http.Request {
	page := string(f.Fields())
	v := url.Values{}
	for _, m := range regexp.MustCompile(`name="([^"]+)" value="([^"]*)"`).FindAllStringSubmatch(page, -1) {
		v.Set(m[1], html.UnescapeString(m[2]))
	}
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(v.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return r
}

func TestNewRefusesAMissingTable(t *testing.T) {
	d := openTestDB(t)
	_, err := pow.New(pow.Config{InstanceKey: "k", Nonces: pow.SQLNonces(d.Writer()), Difficulty: pow.NoProof})
	if !errors.Is(err, pow.ErrNoSchema) {
		t.Fatalf("New over an unmigrated database = %v, want ErrNoSchema", err)
	}
}

func TestCommitRollsBackWithTheCallersTransaction(t *testing.T) {
	_, d := newTestStore(t)
	g := newSQLGuard(t, d)
	f := g.Form(time.Now().Add(-time.Second), "s")
	ctx := context.Background()
	a := g.Admit(formPost(f), pow.Want{Scope: "s"})
	tx, _ := d.Writer().BeginTx(ctx, nil)
	if err := a.Commit(ctx, tx); err != nil {
		t.Fatal(err)
	}
	tx.Rollback()
	b := g.Admit(formPost(f), pow.Want{Scope: "s"})
	if !b.OK {
		t.Fatalf("after a rollback the token was refused: %s", b.Reason)
	}
}

func TestAttemptsSurviveCommitAndRollback(t *testing.T) {
	// Admit, spend inside the caller's transaction, business refusal,
	// rollback: if a successful insert released the attempt entry, this
	// loop would run forever on one solve.
	_, d := newTestStore(t)
	g, err := pow.New(pow.Config{InstanceKey: "k", Nonces: pow.SQLNonces(d.Writer()), Difficulty: pow.NoProof,
		MinAge: time.Millisecond, Attempts: 2})
	if err != nil {
		t.Fatal(err)
	}
	f := g.Form(time.Now().Add(-time.Second), "s")
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		a := g.Admit(formPost(f), pow.Want{Scope: "s"})
		if !a.OK {
			t.Fatalf("cycle %d refused: %s", i+1, a.Reason)
		}
		tx, err := d.Writer().BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := a.Commit(ctx, tx); err != nil {
			t.Fatalf("cycle %d Commit: %v", i+1, err)
		}
		tx.Rollback()
	}
	if a := g.Admit(formPost(f), pow.Want{Scope: "s"}); a.Reason != pow.ReasonAttempts {
		t.Fatalf("third admission after two commit-and-rollback cycles = %s, want attempts", a.Reason)
	}
}

// stalledExec holds ExecContext until release is closed: a request
// that passed every check, then stalled before its INSERT ran.
type stalledExec struct {
	ex      pow.Execer
	entered chan struct{} // closed when Commit has handed over its statement
	release chan struct{}
}

func (s stalledExec) ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error) {
	close(s.entered)
	<-s.release
	return s.ex.ExecContext(ctx, q, args...)
}

func TestACommitStalledPastExpiryAndASweepIsRefused(t *testing.T) {
	// A and B are admitted before expiry. A commits. B stalls; the token
	// expires and its row is swept. B's INSERT then runs. Checked in Go
	// before the executor, B would pass and write a second time; checked
	// in the INSERT on the database clock, it cannot.
	_, d := newTestStore(t)
	g, err := pow.New(pow.Config{InstanceKey: "k", Nonces: pow.SQLNonces(d.Writer()), Difficulty: pow.NoProof,
		MinAge: time.Millisecond, MaxAge: 300 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	f := g.Form(time.Now(), "s")
	time.Sleep(5 * time.Millisecond)
	ctx := context.Background()
	a := g.Admit(formPost(f), pow.Want{Scope: "s"})
	b := g.Admit(formPost(f), pow.Want{Scope: "s"})
	if !a.OK || !b.OK {
		t.Fatalf("admissions: %s %s", a.Reason, b.Reason)
	}
	if err := a.Commit(ctx, nil); err != nil {
		t.Fatal(err)
	}
	st := stalledExec{ex: d.Writer(), entered: make(chan struct{}), release: make(chan struct{})}
	done := make(chan error, 1)
	go func() { done <- b.Commit(ctx, st) }()
	// Commit must have passed any check of its own and reached the
	// executor BEFORE expiry, or a Go-side expiry check would refuse B
	// for the wrong reason and this test would pass against it.
	select {
	case <-st.entered:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Commit did not reach the executor within 200ms")
	}
	if !time.Now().Before(f.Expires) {
		t.Fatal("the executor was reached after expiry; the test proves nothing")
	}
	time.Sleep(time.Until(f.Expires) + 100*time.Millisecond)
	if _, err := d.Writer().ExecContext(ctx, `DELETE FROM pow_spent_nonces`); err != nil {
		t.Fatal(err) // the sweep, without waiting out its margin
	}
	close(st.release)
	if err := <-done; !errors.Is(err, pow.ErrSpent) {
		t.Fatalf("stalled commit after expiry and sweep = %v, want ErrSpent", err)
	}
}

func TestConcurrentCommitsHaveOneWinner(t *testing.T) {
	_, d := newTestStore(t)
	g := newSQLGuard(t, d)
	f := g.Form(time.Now().Add(-time.Second), "s")
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a := g.Admit(formPost(f), pow.Want{Scope: "s"})
			if !a.OK {
				return
			}
			if a.Commit(context.Background(), nil) == nil {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("%d commits won, want 1", wins)
	}
}

func TestCommitThroughAGormTransaction(t *testing.T) {
	// GORM's *gorm.DB is not an Execer; its transaction's ConnPool is.
	// pow does not import GORM, so this is the documented way in.
	_, d := newTestStore(t)
	g := newSQLGuard(t, d)
	f := g.Form(time.Now().Add(-time.Second), "s")
	a := g.Admit(formPost(f), pow.Want{Scope: "s"})
	err := d.G.Transaction(func(tx *gorm.DB) error {
		return a.Commit(context.Background(), tx.Statement.ConnPool)
	})
	if err != nil {
		t.Fatalf("Commit through tx.Statement.ConnPool: %v", err)
	}
	if b := g.Admit(formPost(f), pow.Want{Scope: "s"}); b.Reason != pow.ReasonSpent {
		t.Fatalf("after a GORM commit the token = %s, want spent", b.Reason)
	}
}

func TestErrSpentInsideATransactionReturnsPromptly(t *testing.T) {
	// A lookup through another handle while this transaction holds the
	// only writer would wait on itself forever.
	_, d := newTestStore(t)
	g := newSQLGuard(t, d)
	f := g.Form(time.Now().Add(-time.Second), "s")
	ctx := context.Background()
	a := g.Admit(formPost(f), pow.Want{Scope: "s"})
	if err := a.Commit(ctx, nil); err != nil {
		t.Fatal(err)
	}
	tx, err := d.Writer().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	done := make(chan error, 1)
	go func() { done <- a.Commit(ctx, tx) }()
	select {
	case err := <-done:
		if !errors.Is(err, pow.ErrSpent) {
			t.Fatalf("Commit = %v, want ErrSpent", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Commit inside a transaction hung: it is waiting on its own connection")
	}
}
