// Package eventlog is the rastrillo.Mergeable store shape (design doc
// §5) — the Eleven shape, extracted fresh since no app had extracted it:
// a command never UPDATEs; it appends an immutable event to a resource's
// stream, and a pure Derive fold recomputes the read model. Because the
// fold is pure and total over the history, replay works for free
// (truncate and refold), and multi-edge works for free — the visible
// state is the merge of many single-writer streams, which is exactly the
// platform's designed `mergeable` contract (many leased streams, one
// deterministic merge; carlosframework/platform design §"mergeable",
// still designed-not-built there, so this package is the app-side half
// and Ingest is the seam the platform's transport will call).
//
// One table holds every stream. Each row is one immutable event, keyed
// (stream, writer, seq): writer is the appending instance's identity,
// seq its own dense counter — a writer never rewrites its history. The
// merged read order is the total order (lamport, ts, writer, seq),
// deterministic across edges by construction: any two logs holding the
// same events in any ingest order read them back identically, which is
// what makes Derive converge. That default resolves the design doc's
// open question 2 the way the doc itself leans — a generic lamport
// order as the framework default, with Log.Order as the app-supplied
// comparator seam for when Eleven's real merge rule is finally
// confirmed against this shape.
//
// What is deliberately not here: transport (edge sync is the platform's
// designed territory), snapshots (replay is cheap until proven
// otherwise), and any UPDATE or DELETE verb — deletion is an appended
// tombstone event whose meaning belongs to the app's Derive.
package eventlog

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Migrations is the package's schema — additive and idempotent, meant
// to be appended to an app's Options.Migrations.
var Migrations = []string{
	`CREATE TABLE IF NOT EXISTS eventlog (
	  stream  TEXT    NOT NULL,
	  writer  TEXT    NOT NULL,
	  seq     INTEGER NOT NULL,
	  lamport INTEGER NOT NULL,
	  ts      TEXT    NOT NULL,
	  actor   TEXT    NOT NULL,
	  kind    TEXT    NOT NULL,
	  payload TEXT    NOT NULL,
	  PRIMARY KEY (stream, writer, seq)
	);`,
	`CREATE INDEX IF NOT EXISTS eventlog_merge_order
	  ON eventlog (stream, lamport, ts, writer, seq);`,
	`CREATE TABLE IF NOT EXISTS eventlog_writer (
	  id     INTEGER PRIMARY KEY CHECK (id = 1),
	  writer TEXT    NOT NULL
	);`,
}

// Event is one immutable entry in a stream.
type Event struct {
	Stream  string          `json:"stream"`
	Writer  string          `json:"writer"`
	Seq     int64           `json:"seq"`
	Lamport int64           `json:"lamport"`
	TS      time.Time       `json:"ts"`
	Actor   string          `json:"actor"` // rastrillo.Actor.String(): "human" or "agent:<name>"
	Kind    string          `json:"kind"`
	Payload json.RawMessage `json:"payload"`
}

// Log is one writer's view of the shared table.
type Log struct {
	db     *sql.DB
	writer string

	// Order, when set, replaces the default merge comparator for Events:
	// a three-way compare over two events. The default (nil) is the
	// (lamport, ts, writer, seq) total order. Whatever it returns must be
	// a total order over the events actually present, or merged reads
	// stop being deterministic — which is the entire point of the shape.
	Order func(a, b Event) int
}

// Open binds a Log to a database as one named writer. The writer string
// is this instance's stream identity — every edge that appends must use
// a distinct one, or two writers share a seq counter and the
// single-writer-per-stream premise collapses. It must never be empty.
func Open(db *sql.DB, writer string) (*Log, error) {
	if writer == "" {
		return nil, errors.New("rastrillo/eventlog: writer identity must not be empty")
	}
	return &Log{db: db, writer: writer}, nil
}

// Append records one event on stream and returns it as stored. payload
// is marshalled to JSON (a json.RawMessage passes through verbatim).
// The lamport stamp is max(seen on this stream)+1, so an event appended
// after ingesting another edge's history orders after it.
func (l *Log) Append(ctx context.Context, stream, kind, actor string, payload any) (Event, error) {
	if stream == "" || kind == "" {
		return Event{}, errors.New("rastrillo/eventlog: stream and kind must not be empty")
	}
	raw, err := marshalPayload(payload)
	if err != nil {
		return Event{}, fmt.Errorf("rastrillo/eventlog: marshal payload: %w", err)
	}

	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return Event{}, err
	}
	defer tx.Rollback()

	var maxLamport, maxSeq int64
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(lamport), 0) FROM eventlog WHERE stream = ?`, stream).
		Scan(&maxLamport); err != nil {
		return Event{}, err
	}
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(seq), 0) FROM eventlog WHERE stream = ? AND writer = ?`,
		stream, l.writer).Scan(&maxSeq); err != nil {
		return Event{}, err
	}

	ev := Event{
		Stream: stream, Writer: l.writer,
		Seq: maxSeq + 1, Lamport: maxLamport + 1,
		TS: time.Now().UTC(), Actor: actor, Kind: kind, Payload: raw,
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO eventlog (stream, writer, seq, lamport, ts, actor, kind, payload)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		ev.Stream, ev.Writer, ev.Seq, ev.Lamport, ev.TS.Format(time.RFC3339Nano),
		ev.Actor, ev.Kind, string(ev.Payload)); err != nil {
		return Event{}, err
	}
	if err := tx.Commit(); err != nil {
		return Event{}, err
	}
	return ev, nil
}

// Events returns a stream's full merged history in the deterministic
// merge order (or Log.Order when set).
func (l *Log) Events(ctx context.Context, stream string) ([]Event, error) {
	out, err := l.queryEvents(ctx,
		`SELECT stream, writer, seq, lamport, ts, actor, kind, payload
		 FROM eventlog WHERE stream = ?
		 ORDER BY lamport, ts, writer, seq`, stream)
	if err != nil {
		return nil, err
	}
	if l.Order != nil {
		slices.SortStableFunc(out, l.Order)
	}
	return out, nil
}

// EventsByPrefix returns every event whose stream name has the given
// prefix — the read a mergeable store's list screens need, since only
// this package owns the table. Events come back in the same total
// order Events uses ((lamport, ts, writer, seq) across the whole
// result); a set Log.Order is applied per stream group, matching
// Events' own per-stream semantics — one stream's events reorder among
// themselves, never across streams. The prefix matches literally:
// LIKE's metacharacters (%, _) are escaped, so "a_b/" never matches
// "axb/…", and "bookmarks/" never matches "bookmarksarchive/…".
func (l *Log) EventsByPrefix(ctx context.Context, prefix string) ([]Event, error) {
	escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(prefix)
	out, err := l.queryEvents(ctx,
		`SELECT stream, writer, seq, lamport, ts, actor, kind, payload
		 FROM eventlog WHERE stream LIKE ? ESCAPE '\'
		 ORDER BY lamport, ts, writer, seq`, escaped+"%")
	if err != nil {
		return nil, err
	}
	if l.Order != nil {
		// Stable-sort each stream's events in place: collect every
		// stream's positions in the merged result, order that stream's
		// events by the comparator, and write them back into the same
		// positions — cross-stream interleaving is untouched, so the
		// comparator is never asked to order two different streams.
		positions := map[string][]int{}
		for i, ev := range out {
			positions[ev.Stream] = append(positions[ev.Stream], i)
		}
		for _, idxs := range positions {
			group := make([]Event, len(idxs))
			for i, idx := range idxs {
				group[i] = out[idx]
			}
			slices.SortStableFunc(group, l.Order)
			for i, idx := range idxs {
				out[idx] = group[i]
			}
		}
	}
	return out, nil
}

// queryEvents runs one SELECT over the eventlog table and scans the
// rows into Events — the shared read path under Events and
// EventsByPrefix.
func (l *Log) queryEvents(ctx context.Context, query string, args ...any) ([]Event, error) {
	rows, err := l.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Event
	for rows.Next() {
		var ev Event
		var ts, payload string
		if err := rows.Scan(&ev.Stream, &ev.Writer, &ev.Seq, &ev.Lamport,
			&ts, &ev.Actor, &ev.Kind, &payload); err != nil {
			return nil, err
		}
		if ev.TS, err = time.Parse(time.RFC3339Nano, ts); err != nil {
			return nil, fmt.Errorf("rastrillo/eventlog: bad timestamp %q: %w", ts, err)
		}
		ev.Payload = json.RawMessage(payload)
		out = append(out, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// LocalWriter returns this database's durable writer identity, minting
// and persisting one (16 random bytes, base64url) on first use — the
// per-instance identity every mergeable store binds its appends to. A
// hardcoded writer would poison history the first time two edges
// merge; this one is stable for the database's whole life. Safe under
// the single-writer SQLite pool both db paths enforce; a concurrent
// mint from another connection loses the INSERT and reads back the
// winner's identity.
func LocalWriter(ctx context.Context, db *sql.DB) (string, error) {
	var writer string
	err := db.QueryRowContext(ctx, `SELECT writer FROM eventlog_writer WHERE id = 1`).Scan(&writer)
	if err == nil {
		return writer, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("rastrillo/eventlog: read writer identity: %w", err)
	}

	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("rastrillo/eventlog: mint writer identity: %w", err)
	}
	minted := base64.RawURLEncoding.EncodeToString(raw[:])
	if _, err := db.ExecContext(ctx,
		`INSERT OR IGNORE INTO eventlog_writer (id, writer) VALUES (1, ?)`, minted); err != nil {
		return "", fmt.Errorf("rastrillo/eventlog: persist writer identity: %w", err)
	}
	// Read back rather than returning minted: if another connection won
	// the INSERT, its identity is the durable one.
	if err := db.QueryRowContext(ctx, `SELECT writer FROM eventlog_writer WHERE id = 1`).Scan(&writer); err != nil {
		return "", fmt.Errorf("rastrillo/eventlog: reread writer identity: %w", err)
	}
	return writer, nil
}

// ErrDiverged means Ingest met an event claiming a (stream, writer, seq)
// the table already holds with different content — a protocol violation
// (a writer rewrote its history, or two edges share a writer identity),
// never something to paper over.
var ErrDiverged = errors.New("rastrillo/eventlog: ingested event diverges from the stored event at the same position")

// Ingest imports events from another writer's stream — the platform
// transport's seam, and directly usable for backup replay. Idempotent:
// an event already present (same stream, writer, seq, same content) is
// skipped; same position with different content is ErrDiverged.
func (l *Log) Ingest(ctx context.Context, events []Event) error {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, ev := range events {
		if ev.Stream == "" || ev.Writer == "" || ev.Seq <= 0 {
			return fmt.Errorf("rastrillo/eventlog: malformed event %+v", ev)
		}
		res, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO eventlog (stream, writer, seq, lamport, ts, actor, kind, payload)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			ev.Stream, ev.Writer, ev.Seq, ev.Lamport, ev.TS.UTC().Format(time.RFC3339Nano),
			ev.Actor, ev.Kind, string(ev.Payload))
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			var lamport int64
			var ts, kind, payload string
			if err := tx.QueryRowContext(ctx,
				`SELECT lamport, ts, kind, payload FROM eventlog
				 WHERE stream = ? AND writer = ? AND seq = ?`,
				ev.Stream, ev.Writer, ev.Seq).Scan(&lamport, &ts, &kind, &payload); err != nil {
				return err
			}
			if lamport != ev.Lamport || kind != ev.Kind ||
				ts != ev.TS.UTC().Format(time.RFC3339Nano) || payload != string(ev.Payload) {
				return fmt.Errorf("%w: stream %q writer %q seq %d", ErrDiverged, ev.Stream, ev.Writer, ev.Seq)
			}
		}
	}
	return tx.Commit()
}

// Derive is the pure fold that turns a history into a read model —
// "literally kass's next(spec, events) → session pattern, generalized"
// (§5). Keep reduce pure and total: same events, same order, same
// state, on every edge, forever. Replay to a point in time is a slice
// of the input.
func Derive[S any](events []Event, reduce func(S, Event) S) S {
	var s S
	for _, ev := range events {
		s = reduce(s, ev)
	}
	return s
}

// marshalPayload turns payload into stored JSON. nil stores as "null";
// a json.RawMessage must already be valid JSON — it is checked, not
// trusted, since a bad byte here would poison every future read of the
// stream.
func marshalPayload(payload any) (json.RawMessage, error) {
	if raw, ok := payload.(json.RawMessage); ok {
		if !json.Valid(raw) {
			return nil, errors.New("payload json.RawMessage is not valid JSON")
		}
		return raw, nil
	}
	return json.Marshal(payload)
}
