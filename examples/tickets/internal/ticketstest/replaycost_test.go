//go:build perf

package ticketstest

import (
	"context"
	"fmt"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/currency/replay"
	"amadan.net/rastrillo/rastrillo/eventlog"

	announcementsstore "tickets/gen/store/announcements"
)

// TestAnnouncementsReplayCost times CheckReplay on a 200,000-event log,
// the way eventlog's TestEventlogRenameCost times the rename: logged,
// not asserted, because a timing bound fails on a slow runner and not
// on a regression. Boot replays every mergeable stream before the app
// serves (spec §7), so this is what a hibernating app with a large log
// pays inside the activator's budget on every start. Each event is a
// stream of its own, the worst case for the per-stream fold. Behind
// the perf tag so the gate does not pay for the insert: make perf at
// the repo root runs it.
func TestAnnouncementsReplayCost(t *testing.T) {
	_, db := newApp(t)
	ctx := context.Background()
	writer, err := eventlog.LocalWriter(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	log, err := eventlog.Open(db, writer)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	// Inserted directly rather than through Create: Create reads the
	// log to allocate each id, which would make the setup quadratic.
	// The column name comes from the open log, so the test follows the
	// payload rename rather than assuming it.
	stmt, err := tx.Prepare(`INSERT INTO eventlog (stream, writer, seq, lamport, ts, actor, kind, ` + log.PayloadColumn() + `)
		VALUES (?, ?, 1, ?, '2026-01-01T00:00:00Z', 'app', 'created', ?)`)
	if err != nil {
		t.Fatal(err)
	}
	payload := fmt.Sprintf(`{"V":%d,"title":"Doors open at noon","body":"Gates on the east side.","created_at":"2026-01-01T00:00:00Z"}`, replay.Version)
	for i := 1; i <= 200000; i++ {
		if _, err := stmt.Exec(fmt.Sprintf("%s%d", announcementsstore.StreamPrefix, i), writer, i, payload); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := announcementsstore.CheckReplay(ctx, db); err != nil {
		t.Fatal(err)
	}
	t.Logf("CheckReplay (every announcement stream replayed, 200,000 streams of one event) took %v", time.Since(start))
}
