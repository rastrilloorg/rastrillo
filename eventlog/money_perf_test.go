//go:build perf

package eventlog_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/eventlog"
	"amadan.net/rastrillo/rastrillo/migrate"
)

// TestEventlogRenameCost times the rename on a large log. It is
// logged, not asserted: the number is evidence for the upgrade notes,
// and a timing bound would fail on a slow runner, not on a regression.
// Behind the perf tag, with the scaffold's perf lane, so the gate does
// not pay for a 200,000-row insert on every run: make perf runs it.
func TestEventlogRenameCost(t *testing.T) {
	ctx := context.Background()
	w := openMem(t)
	if _, err := migrate.Apply(ctx, migrate.Pool(w), eventlog.Schema); err != nil {
		t.Fatal(err)
	}
	tx, err := w.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 200000; i++ {
		if _, err := tx.Exec(`INSERT INTO eventlog (stream, writer, seq, lamport, ts, actor, kind, payload) VALUES (?, 'edge-a', 1, 1, '2026-01-01T00:00:00Z', 'human', 'created', '{"Price":1250}')`,
			fmt.Sprintf("bulk/%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if _, err := migrate.Apply(ctx, migrate.Pool(w), withMoney); err != nil {
		t.Fatal(err)
	}
	t.Logf("the money migration (ledger row, RENAME COLUMN and boundary snapshot) on 200,000 events took %v", time.Since(start))
}
