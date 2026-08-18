package eventlog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	rastrillo "github.com/carlosframework/rastrillo"
)

func openLog(t *testing.T, writer string) *Log {
	t.Helper()
	db, err := rastrillo.OpenDB(filepath.Join(t.TempDir(), writer+".db"), Migrations)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	l, err := Open(db, writer)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return l
}

func TestOpenRequiresWriter(t *testing.T) {
	if _, err := Open(&sql.DB{}, ""); err == nil {
		t.Fatal("Open with an empty writer succeeded")
	}
}

func TestAppendAssignsSeqAndLamport(t *testing.T) {
	l := openLog(t, "edge-a")
	ctx := context.Background()

	e1, err := l.Append(ctx, "s", "created", "human", map[string]int{"n": 1})
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	e2, _ := l.Append(ctx, "s", "bumped", "human", nil)
	if e1.Seq != 1 || e2.Seq != 2 {
		t.Fatalf("seq = %d, %d; want 1, 2", e1.Seq, e2.Seq)
	}
	if e1.Lamport != 1 || e2.Lamport != 2 {
		t.Fatalf("lamport = %d, %d; want 1, 2", e1.Lamport, e2.Lamport)
	}
	// Streams are independent counters.
	e3, _ := l.Append(ctx, "other", "created", "human", nil)
	if e3.Seq != 1 || e3.Lamport != 1 {
		t.Fatalf("new stream seq/lamport = %d/%d, want 1/1", e3.Seq, e3.Lamport)
	}
}

func TestAppendRejectsBadInput(t *testing.T) {
	l := openLog(t, "edge-a")
	if _, err := l.Append(context.Background(), "", "kind", "human", nil); err == nil {
		t.Fatal("empty stream accepted")
	}
	if _, err := l.Append(context.Background(), "s", "", "human", nil); err == nil {
		t.Fatal("empty kind accepted")
	}
	if _, err := l.Append(context.Background(), "s", "k", "human", json.RawMessage("{not json")); err == nil {
		t.Fatal("invalid RawMessage accepted; it would poison every future read")
	}
}

func TestDeriveFolds(t *testing.T) {
	l := openLog(t, "edge-a")
	ctx := context.Background()
	l.Append(ctx, "acct", "credit", "human", map[string]int{"amount": 100})
	l.Append(ctx, "acct", "debit", "human", map[string]int{"amount": 30})
	l.Append(ctx, "acct", "credit", "agent:biller", map[string]int{"amount": 7})

	events, err := l.Events(ctx, "acct")
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	balance := Derive(events, func(b int, ev Event) int {
		var p struct{ Amount int }
		json.Unmarshal(ev.Payload, &p)
		if ev.Kind == "debit" {
			return b - p.Amount
		}
		return b + p.Amount
	})
	if balance != 77 {
		t.Fatalf("balance = %d, want 77", balance)
	}
	// Replay = truncate and refold.
	if got := Derive(events[:2], func(b int, ev Event) int {
		var p struct{ Amount int }
		json.Unmarshal(ev.Payload, &p)
		if ev.Kind == "debit" {
			return b - p.Amount
		}
		return b + p.Amount
	}); got != 70 {
		t.Fatalf("replayed balance = %d, want 70", got)
	}
}

type vectors struct {
	Stream        string   `json:"stream"`
	Events        []Event  `json:"events"`
	ExpectedOrder []string `json:"expected_order"`
}

func loadVectors(t *testing.T) vectors {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "merge-vectors.json"))
	if err != nil {
		t.Fatalf("read vectors: %v", err)
	}
	var v vectors
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("unmarshal vectors: %v", err)
	}
	return v
}

func keyOf(ev Event) string { return fmt.Sprintf("%s:%d", ev.Writer, ev.Seq) }

// TestMergeVectors pins the deterministic merge order: every ingest
// permutation of the vector events reads back in exactly
// expected_order. The vectors are the spec — if this fails, the merge
// diverged; fix the implementation, not the file.
func TestMergeVectors(t *testing.T) {
	v := loadVectors(t)
	permutations := [][]int{
		{0, 1, 2, 3, 4},
		{4, 3, 2, 1, 0},
		{2, 0, 4, 1, 3},
		{1, 4, 0, 3, 2},
	}
	for i, perm := range permutations {
		l := openLog(t, fmt.Sprintf("reader-%d", i))
		var batch []Event
		for _, idx := range perm {
			batch = append(batch, v.Events[idx])
		}
		if err := l.Ingest(context.Background(), batch); err != nil {
			t.Fatalf("perm %v: Ingest: %v", perm, err)
		}
		events, err := l.Events(context.Background(), v.Stream)
		if err != nil {
			t.Fatalf("Events: %v", err)
		}
		var got []string
		for _, ev := range events {
			got = append(got, keyOf(ev))
		}
		if fmt.Sprint(got) != fmt.Sprint(v.ExpectedOrder) {
			t.Fatalf("perm %v merged as %v, want %v", perm, got, v.ExpectedOrder)
		}
	}
}

// TestCrossIngestConvergence is the multi-edge property end to end: two
// writers append independently, exchange histories in opposite orders,
// and every edge derives the identical state.
func TestCrossIngestConvergence(t *testing.T) {
	ctx := context.Background()
	a := openLog(t, "edge-a")
	b := openLog(t, "edge-b")

	a.Append(ctx, "s", "add", "human", map[string]int{"n": 1})
	a.Append(ctx, "s", "add", "human", map[string]int{"n": 2})
	b.Append(ctx, "s", "add", "human", map[string]int{"n": 10})

	aEvents, _ := a.Events(ctx, "s")
	bEvents, _ := b.Events(ctx, "s")
	if err := a.Ingest(ctx, bEvents); err != nil {
		t.Fatalf("a.Ingest: %v", err)
	}
	if err := b.Ingest(ctx, aEvents); err != nil {
		t.Fatalf("b.Ingest: %v", err)
	}
	// Idempotent: a second exchange changes nothing.
	if err := a.Ingest(ctx, bEvents); err != nil {
		t.Fatalf("repeat a.Ingest: %v", err)
	}

	sum := func(events []Event) int {
		return Derive(events, func(s int, ev Event) int {
			var p struct{ N int }
			json.Unmarshal(ev.Payload, &p)
			return s + p.N
		})
	}
	mergedA, _ := a.Events(ctx, "s")
	mergedB, _ := b.Events(ctx, "s")
	if len(mergedA) != 3 || len(mergedB) != 3 {
		t.Fatalf("merged lengths = %d, %d; want 3, 3", len(mergedA), len(mergedB))
	}
	if sum(mergedA) != 13 || sum(mergedB) != 13 {
		t.Fatalf("derived = %d, %d; want 13 on both edges", sum(mergedA), sum(mergedB))
	}
	for i := range mergedA {
		if keyOf(mergedA[i]) != keyOf(mergedB[i]) {
			t.Fatalf("edge orders diverge at %d: %s vs %s", i, keyOf(mergedA[i]), keyOf(mergedB[i]))
		}
	}

	// An append after ingest orders after everything it has seen.
	ev, _ := a.Append(ctx, "s", "add", "human", map[string]int{"n": 0})
	if ev.Lamport <= 2 {
		t.Fatalf("post-ingest lamport = %d, must exceed every seen lamport", ev.Lamport)
	}
}

func TestIngestDivergenceIsAnError(t *testing.T) {
	ctx := context.Background()
	l := openLog(t, "reader")
	ts := time.Date(2026, 8, 17, 10, 0, 0, 0, time.UTC)
	ev := Event{Stream: "s", Writer: "edge-x", Seq: 1, Lamport: 1, TS: ts,
		Actor: "human", Kind: "created", Payload: json.RawMessage(`{"a":1}`)}
	if err := l.Ingest(ctx, []Event{ev}); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	forged := ev
	forged.Payload = json.RawMessage(`{"a":2}`)
	err := l.Ingest(ctx, []Event{forged})
	if !errors.Is(err, ErrDiverged) {
		t.Fatalf("divergent ingest: %v, want ErrDiverged", err)
	}
	if err := l.Ingest(ctx, []Event{{Stream: "s", Writer: "", Seq: 1}}); err == nil {
		t.Fatal("malformed event accepted")
	}
}

func TestCustomOrder(t *testing.T) {
	ctx := context.Background()
	l := openLog(t, "edge-a")
	l.Append(ctx, "s", "k", "human", map[string]string{"v": "first"})
	l.Append(ctx, "s", "k", "human", map[string]string{"v": "second"})

	l.Order = func(a, b Event) int { return int(b.Seq - a.Seq) } // newest first
	events, err := l.Events(ctx, "s")
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if events[0].Seq != 2 {
		t.Fatalf("custom order ignored: first event seq = %d", events[0].Seq)
	}
}
