package eventlog

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sync"

	"amadan.net/rastrillo/rastrillo/migrate"
)

//go:embed money/migrations/*.sql
var moneyFS embed.FS

// MoneySchema renames payload to payload_v2 and snapshots the upgrade
// boundary. An app includes it, after Schema, only when its events
// carry money: that is declared, never guessed from the data, so an
// app without money keeps payload and upgrades with no stop-start.
// The migration is post-adoption, so a database from before the
// ledger is adopted against Schema first and then renamed.
var MoneySchema = func() *migrate.Set {
	// A separate directory, not migrations/ beside Schema's: FromFS
	// reads every file in migrations/, and Schema must not carry the
	// rename into apps without money.
	sub, err := fs.Sub(moneyFS, "money")
	if err != nil {
		panic(err)
	}
	return migrate.MustFromFS(sub, "eventlog_money")
}()

// Operator messages, Appendix C.2 of the multi-currency money spec.
// The wording is approved copy: change it there first.
var (
	errBoth       = errors.New("eventlog: the table has both payload and payload_v2; restore the backup taken before the upgrade")
	errNeither    = errors.New("eventlog: the table has neither payload nor payload_v2; apply eventlog.Schema first")
	errNoBoundary = errors.New("eventlog: this database has no eventlog_upgrade table, so the money upgrade has not run on it")
)

// upgradeRefused keeps the approved %v wording; %w prints the same
// text and lets a caller still match the cause with errors.Is.
const upgradeRefused = "eventlog: could not rename payload for money: %w. If this app keeps its tables in Options.Migrations, apply eventlog.Schema with migrate.Apply instead; see the eventlog reference"

// columns caches each handle's payload column, *sql.DB to string.
//
// Once per handle: the column changes only in UpgradeForMoney, which
// drops this handle's entry, or in a migration applied through another
// handle before this process opened the log; a failed detection is not
// cached, so Open works once the schema exists. A Log value opened
// before the upgrade keeps the column it saw: generated apps open their
// logs only after gen.Boot, and hand-written apps must do the same.
//
// It holds one small entry per handle an app ever opened. Apps open a
// handful, so it is never evicted.
var columns sync.Map

type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// detectColumn refuses both columns as firmly as neither: a table with
// both is a hand-made half-rollback, and reading either column would
// silently drop the events written to the other.
func detectColumn(ctx context.Context, q querier) (string, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT name FROM pragma_table_info('eventlog') WHERE name IN ('payload', 'payload_v2')`)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return "", err
		}
		names = append(names, n)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	switch len(names) {
	case 0:
		return "", errNeither
	case 1:
		return names[0], nil
	default:
		return "", errBoth
	}
}

func payloadColumn(db *sql.DB) (string, error) {
	if col, ok := columns.Load(db); ok {
		return col.(string), nil
	}
	col, err := detectColumn(context.Background(), db)
	if err != nil {
		return "", err
	}
	columns.Store(db, col)
	return col, nil
}

// PayloadColumn reports the column this Log reads and writes:
// "payload", or "payload_v2" after the money upgrade.
func (l *Log) PayloadColumn() string { return l.col }

// StreamWriter keys the upgrade boundary: the log has no global
// position, so (stream, writer) is the finest unit with an ordered seq.
type StreamWriter struct {
	Stream, Writer string
}

// UpgradeBoundary returns the snapshot MoneySchema took: for each
// stream and writer, the highest seq written before the upgrade.
// Replay uses it to refuse an unversioned event above it, which only a
// currency-unaware binary could have appended. It fails on a database
// the money upgrade has not run on.
func UpgradeBoundary(ctx context.Context, db *sql.DB) (map[StreamWriter]int64, error) {
	var exists bool
	if err := db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = 'eventlog_upgrade')`).
		Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, errNoBoundary
	}
	rows, err := db.QueryContext(ctx, `SELECT stream, writer, seq FROM eventlog_upgrade`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[StreamWriter]int64{}
	for rows.Next() {
		var k StreamWriter
		var seq int64
		if err := rows.Scan(&k.Stream, &k.Writer, &seq); err != nil {
			return nil, err
		}
		out[k] = seq
	}
	return out, rows.Err()
}

// UpgradeForMoney makes the event log money-ready however the app
// applies its schema, and does nothing once it is. db must be the
// app's writer, capped at one connection.
//
// An app with a migration ledger gets MoneySchema through
// migrate.Apply, so the ledger records the rename. An app without one
// (tables from Options.Migrations) gets the same SQL in one
// transaction and no ledger: a ledger created here would make the
// app's own later migrate.Apply adopt a database it did not describe.
func UpgradeForMoney(ctx context.Context, db *sql.DB) error {
	if err := upgradeForMoney(ctx, db); err != nil {
		return fmt.Errorf(upgradeRefused, err)
	}
	// Even when another handle did the rename: this handle may have
	// cached "payload" before it, and every Log it opened since would
	// write to a column that no longer exists.
	columns.Delete(db)
	return nil
}

func upgradeForMoney(ctx context.Context, db *sql.DB) error {
	col, err := detectColumn(ctx, db)
	if err != nil {
		return err
	}
	if col == "payload_v2" {
		return nil
	}
	ledgered, err := hasLedgerRows(ctx, db)
	if err != nil {
		return err
	}
	if ledgered {
		// A non-empty ledger means no adoption. eventlog/0001_init is
		// CREATE IF NOT EXISTS, so it is a no-op, recorded, when the
		// app made the table some other way.
		_, err := migrate.Apply(ctx, migrate.Pool(db), migrate.Merge(Schema, MoneySchema))
		return err
	}
	return renameWithoutLedger(ctx, db)
}

// hasLedgerRows asks for rows, not the table: an empty ledger is what
// a failed first Apply leaves, and that app still has no history the
// ledger describes.
func hasLedgerRows(ctx context.Context, db *sql.DB) (bool, error) {
	var exists bool
	if err := db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = 'rastrillo_migrations')`).
		Scan(&exists); err != nil || !exists {
		return false, err
	}
	var rows bool
	err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM rastrillo_migrations)`).Scan(&rows)
	return rows, err
}

// renameWithoutLedger runs MoneySchema's SQL on one pinned connection
// inside BEGIN IMMEDIATE, re-checking the column under the write lock:
// two instances starting at once both saw payload, and the one that
// waits must find payload_v2 and stop, not rename a column that is
// gone.
func renameWithoutLedger(ctx context.Context, db *sql.DB) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return err
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		// WithoutCancel, and evict on failure: a ROLLBACK refused on a
		// cancelled ctx would return the app's only writer connection
		// to the pool with the write lock still held.
		if _, err := conn.ExecContext(context.WithoutCancel(ctx), "ROLLBACK"); err != nil {
			conn.Raw(func(any) error { return driver.ErrBadConn })
		}
	}()
	col, err := detectColumn(ctx, conn)
	if err != nil {
		return err
	}
	if col == "payload" {
		for _, m := range MoneySchema.All() {
			if _, err := conn.ExecContext(ctx, m.SQL); err != nil {
				return err
			}
		}
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return err
	}
	committed = true
	return nil
}
