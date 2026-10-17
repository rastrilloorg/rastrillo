package eventlog

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/db"
	"amadan.net/rastrillo/rastrillo/migrate"
)

func openLog(t *testing.T, writer string) *Log {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), writer+".db"), nil)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	if _, err := migrate.Apply(context.Background(), d, Schema); err != nil {
		t.Fatalf("migrate.Apply: %v", err)
	}
	l, err := Open(d.Writer(), writer)
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

// TestEventsByPrefix pins the prefix read the mergeable store's list
// screens need: every event whose stream has the prefix, in the same
// total order Events uses — and prefix isolation ("bookmarks/1" never
// matches "bookmarksarchive/…", and a LIKE metacharacter in the prefix
// matches only itself).
func TestEventsByPrefix(t *testing.T) {
	ctx := context.Background()
	l := openLog(t, "edge-a")
	l.Append(ctx, "bookmarks/1", "created", "human", map[string]int{"n": 1})
	l.Append(ctx, "bookmarks/2", "created", "human", map[string]int{"n": 2})
	l.Append(ctx, "bookmarks/1", "updated", "human", map[string]int{"n": 3})
	l.Append(ctx, "bookmarksarchive/1", "created", "human", map[string]int{"n": 9})

	events, err := l.EventsByPrefix(ctx, "bookmarks/")
	if err != nil {
		t.Fatalf("EventsByPrefix: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("got %d events, want 3 (bookmarksarchive/* must not match)", len(events))
	}
	for _, ev := range events {
		if ev.Stream != "bookmarks/1" && ev.Stream != "bookmarks/2" {
			t.Errorf("stream %q matched prefix %q", ev.Stream, "bookmarks/")
		}
	}
	// Same total order as Events: (lamport, ts, writer, seq) across the
	// whole result. Each stream's lamports here are 1,1,2 in append
	// order, so lamport must be non-decreasing in the merged read.
	for i := 1; i < len(events); i++ {
		if events[i].Lamport < events[i-1].Lamport {
			t.Fatalf("events out of merge order at %d: lamport %d after %d",
				i, events[i].Lamport, events[i-1].Lamport)
		}
	}

	// A LIKE metacharacter in the prefix matches only itself.
	l.Append(ctx, "a_b/1", "created", "human", nil)
	l.Append(ctx, "axb/1", "created", "human", nil)
	got, err := l.EventsByPrefix(ctx, "a_b/")
	if err != nil {
		t.Fatalf("EventsByPrefix: %v", err)
	}
	if len(got) != 1 || got[0].Stream != "a_b/1" {
		t.Fatalf("prefix %q matched %v, want exactly a_b/1 (LIKE _ must be literal)", "a_b/", got)
	}
}

// TestEventsByPrefixCustomOrder: Log.Order replaces the default
// comparator per stream group, matching Events' own semantics — events
// of one stream reorder among themselves, never across streams.
func TestEventsByPrefixCustomOrder(t *testing.T) {
	ctx := context.Background()
	l := openLog(t, "edge-a")
	l.Append(ctx, "notes/1", "k", "human", map[string]string{"v": "first"})
	l.Append(ctx, "notes/1", "k", "human", map[string]string{"v": "second"})
	l.Append(ctx, "notes/2", "k", "human", nil)

	l.Order = func(a, b Event) int { return int(b.Seq - a.Seq) } // newest first
	events, err := l.EventsByPrefix(ctx, "notes/")
	if err != nil {
		t.Fatalf("EventsByPrefix: %v", err)
	}
	var ones []Event
	for _, ev := range events {
		if ev.Stream == "notes/1" {
			ones = append(ones, ev)
		}
	}
	if len(ones) != 2 || ones[0].Seq != 2 || ones[1].Seq != 1 {
		t.Fatalf("custom order not applied within the stream group: %v", ones)
	}
}

// TestLocalWriter: one durable identity per database — minted once,
// then read back stable forever; distinct databases mint distinct
// identities.
func TestLocalWriter(t *testing.T) {
	ctx := context.Background()
	// db.Open + migrate.Apply, not rastrillo.OpenDB + Migrations:
	// eventlog ships a *migrate.Set now, and the raw []string this
	// test was written against no longer exists.
	sq1 := openWriterDB(t, "a")

	w1, err := LocalWriter(ctx, sq1)
	if err != nil {
		t.Fatalf("LocalWriter: %v", err)
	}
	if w1 == "" {
		t.Fatal("LocalWriter minted an empty identity")
	}
	w2, err := LocalWriter(ctx, sq1)
	if err != nil {
		t.Fatalf("LocalWriter (2nd): %v", err)
	}
	if w2 != w1 {
		t.Fatalf("LocalWriter unstable: %q then %q", w1, w2)
	}
	// Usable as a writer identity directly.
	if _, err := Open(sq1, w1); err != nil {
		t.Fatalf("Open(LocalWriter): %v", err)
	}

	sq2 := openWriterDB(t, "b")
	w3, err := LocalWriter(ctx, sq2)
	if err != nil {
		t.Fatalf("LocalWriter (db2): %v", err)
	}
	if w3 == w1 {
		t.Fatalf("two databases share writer identity %q", w1)
	}
}

// openWriterDB is openLog's first half: a migrated database handed
// back as the writer *sql.DB, for the tests that want the handle
// rather than a *Log.
func openWriterDB(t *testing.T, name string) *sql.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), name+".db"), nil)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	if _, err := migrate.Apply(context.Background(), d, Schema); err != nil {
		t.Fatalf("migrate.Apply: %v", err)
	}
	return d.Writer()
}

// TestMergeVectorsFileUntouched pins the vectors file byte-for-byte:
// "Fix implementations, not vectors" — no change to this package may
// rewrite the spec it is tested against.
func TestMergeVectorsFileUntouched(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "merge-vectors.json"))
	if err != nil {
		t.Fatalf("read vectors: %v", err)
	}
	const want = "907bd5494d3a370e7cc40197439dbaa254525ffd973c68eadb83fa20a3ec4108"
	if got := fmt.Sprintf("%x", sha256.Sum256(raw)); got != want {
		t.Fatalf("merge-vectors.json changed (sha256 %s, want %s); fix implementations, not vectors", got, want)
	}
}

// AppendChecked's check sees the stream's whole history, and a refusal
// appends nothing and comes back unchanged, so a caller can match it.
func TestAppendCheckedRefusalAppendsNothing(t *testing.T) {
	l := openLog(t, "edge-a")
	ctx := context.Background()
	l.Append(ctx, "s", "created", "human", map[string]int{"n": 1})
	l.Append(ctx, "s", "bumped", "human", nil)
	l.Append(ctx, "other", "created", "human", nil)

	refused := errors.New("refused")
	var seen []string
	_, err := l.AppendChecked(ctx, "s", "bumped", "human", nil, func(history []Event) error {
		for _, ev := range history {
			seen = append(seen, ev.Kind)
		}
		return refused
	})
	if err != refused {
		t.Fatalf("AppendChecked = %v, want the check's own error", err)
	}
	if fmt.Sprint(seen) != "[created bumped]" {
		t.Errorf("check saw %v, want the stream's two events", seen)
	}
	if evs, _ := l.Events(ctx, "s"); len(evs) != 2 {
		t.Fatalf("a refused append wrote: %d events, want 2", len(evs))
	}

	ev, err := l.AppendChecked(ctx, "s", "bumped", "human", nil, func([]Event) error { return nil })
	if err != nil || ev.Seq != 3 || ev.Lamport != 3 {
		t.Fatalf("accepted append = %+v, %v; want seq 3, lamport 3", ev, err)
	}
}

// The check runs under the write lock: while it runs, a writer on
// another connection cannot append, so the history it judged is the
// history the event lands on.
func TestAppendCheckedHoldsTheWriteLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	d, err := db.Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	ctx := context.Background()
	if _, err := migrate.Apply(ctx, d, Schema); err != nil {
		t.Fatal(err)
	}
	l, err := Open(d.Writer(), "edge-a")
	if err != nil {
		t.Fatal(err)
	}
	// busy_timeout 0: a writer that finds the lock taken fails at once
	// instead of waiting for the check to finish.
	other, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(0)&_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { other.Close() })
	racer, err := Open(other, "edge-b")
	if err != nil {
		t.Fatal(err)
	}
	var raced error
	if _, err := l.AppendChecked(ctx, "s", "created", "human", nil, func([]Event) error {
		_, raced = racer.Append(ctx, "s", "created", "human", nil)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if raced == nil {
		t.Fatal("another connection appended while the check ran")
	}
	if evs, _ := l.Events(ctx, "s"); len(evs) != 1 || evs[0].Writer != "edge-a" {
		t.Fatalf("events %+v, want only edge-a's", evs)
	}
}

// AppendUnder's name sees only the streams under the prefix, literally
// matched; the event lands on the stream it names; a refusal from it,
// and a name outside the prefix, append nothing.
func TestAppendUnderNamesTheStream(t *testing.T) {
	l := openLog(t, "edge-a")
	ctx := context.Background()
	l.Append(ctx, "a_b/1", "created", "human", nil)
	l.Append(ctx, "a_b/1", "bumped", "human", nil)
	l.Append(ctx, "a_b/2", "created", "human", nil)
	l.Append(ctx, "axb/9", "created", "human", nil)

	var seen []string
	ev, err := l.AppendUnder(ctx, "a_b/", "created", "human", map[string]int{"n": 3}, func(streams []string) (string, error) {
		seen = streams
		return "a_b/3", nil
	})
	if err != nil || ev.Stream != "a_b/3" || ev.Seq != 1 {
		t.Fatalf("AppendUnder = %+v, %v; want seq 1 on a_b/3", ev, err)
	}
	if fmt.Sprint(seen) != "[a_b/1 a_b/2]" {
		t.Errorf("name saw %v, want each stream under a_b/ once and axb/9 not at all", seen)
	}

	refused := errors.New("refused")
	if _, err := l.AppendUnder(ctx, "a_b/", "created", "human", nil, func([]string) (string, error) { return "", refused }); err != refused {
		t.Fatalf("a refused name: %v, want the refusal as it is", err)
	}
	if _, err := l.AppendUnder(ctx, "a_b/", "created", "human", nil, func([]string) (string, error) { return "axb/4", nil }); err == nil {
		t.Fatal("a stream outside the prefix was appended")
	}
	if evs, _ := l.EventsByPrefix(ctx, "a"); len(evs) != 5 {
		t.Fatalf("%d events after the two refusals, want 5", len(evs))
	}
}

// name runs under the write lock, so another connection cannot add a
// stream under the prefix while it chooses: the streams it saw are the
// streams the event lands beside.
func TestAppendUnderHoldsTheWriteLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	d, err := db.Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	ctx := context.Background()
	if _, err := migrate.Apply(ctx, d, Schema); err != nil {
		t.Fatal(err)
	}
	l, err := Open(d.Writer(), "edge-a")
	if err != nil {
		t.Fatal(err)
	}
	other, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(0)&_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { other.Close() })
	racer, err := Open(other, "edge-b")
	if err != nil {
		t.Fatal(err)
	}
	var raced error
	if _, err := l.AppendUnder(ctx, "s/", "created", "human", nil, func([]string) (string, error) {
		_, raced = racer.Append(ctx, "s/1", "created", "human", nil)
		return "s/1", nil
	}); err != nil {
		t.Fatal(err)
	}
	if raced == nil {
		t.Fatal("another connection appended while name ran")
	}
	if evs, _ := l.Events(ctx, "s/1"); len(evs) != 1 || evs[0].Writer != "edge-a" {
		t.Fatalf("events %+v, want only edge-a's", evs)
	}
}

// Astra's P2: streamsUnder runs under the write lock, so it must reach
// its prefix through the primary key's binary order rather than scan
// the whole index. A LIKE prefix cannot use a binary-collated index,
// and every Create then read the entire log while every other writer
// waited: about 70 ms per million unrelated events.
func TestStreamsUnderSearchesTheIndex(t *testing.T) {
	l := openLog(t, "edge-a")
	ctx := context.Background()
	query, args := streamsUnderQuery("prices/")
	rows, err := l.db.QueryContext(ctx, "EXPLAIN QUERY PLAN "+query, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var id, parent, notUsed int
		var detail string
		if err := rows.Scan(&id, &parent, &notUsed, &detail); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	joined := fmt.Sprint(plan)
	if !strings.Contains(joined, "SEARCH eventlog USING COVERING INDEX") || strings.Contains(joined, "SCAN") {
		t.Fatalf("plan %v; want a SEARCH on an index from the prefix, not a SCAN of the log", plan)
	}
}

// Astra's round two: the upper bound of a prefix's range carries. A
// prefix ending in 0xFF after other bytes, "a\xff", still has a bound,
// "b"; raising only the last byte left it open, and every later stream
// ("b/1") came through. Only a prefix that is all 0xFF has no bound.
func TestPrefixBoundCarries(t *testing.T) {
	for _, c := range []struct {
		prefix, want string
		ok           bool
	}{
		{"prices/", "prices0", true},
		{"a\xff", "b", true},
		{"a\xff\xff", "b", true},
		{"\xff\xff", "", false},
		{"", "", false},
		{"café/", "café0", true},
		{"caf\xc3\xa9", "caf\xc3\xaa", true},
	} {
		got, ok := prefixBound(c.prefix)
		if got != c.want || ok != c.ok {
			t.Errorf("prefixBound(%q) = %q, %v; want %q, %v", c.prefix, got, ok, c.want, c.ok)
		}
	}
	// The bound is what keeps a later stream out of the range.
	l := openLog(t, "edge-a")
	ctx := context.Background()
	l.Append(ctx, "a\xff/1", "created", "human", nil)
	l.Append(ctx, "b/1", "created", "human", nil)
	var seen []string
	if _, err := l.AppendUnder(ctx, "a\xff", "created", "human", nil, func(streams []string) (string, error) {
		seen = streams
		return "a\xff/2", nil
	}); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%q", seen) != `["a\xff/1"]` {
		t.Errorf("name saw %q, want only the stream under the prefix", seen)
	}
}
