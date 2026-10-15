// Package sqlmoney upgrades generated SQL money columns once at boot
// and refuses to start on rows that break "a currency if and only if an
// amount".
package sqlmoney

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"amadan.net/rastrillo/rastrillo/currency"
)

// Version is the money data version this binary writes into
// rastrillo_money. A later binary that changes the stored shape bumps
// it, so this one refuses that data instead of misreading it.
const Version = 2

// Appendix C.2's op.legacy.* messages, verbatim.
const (
	msgUndeclared   = "%s has %d rows from before money had a currency. Add [legacy] currency and scale to manifest/%s.toml and run rastrillo generate."
	msgFailed       = "%s.%s: %d rows cannot be converted, and nothing was changed:"
	msgRow          = "row %d: %s"
	msgMore         = "and %d more"
	msgPairs        = "%s.%s: %d rows have an amount without a currency or a currency without an amount; fix them before starting"
	msgNewer        = "this database's money data is version %d, and this binary understands version 2. Run the newer version, or restore the backup taken before it ran."
	msgAddedColumns = "%s.%s: added %s_minor and %s_currency; existing rows have no amount"
	msgDropBlocked  = "%s.%s cannot be replaced by %s_minor because %v; remove that index, view or trigger and start again"
	msgBadName      = "%q is not a table or column name the money upgrade can use: use only a-z, 0-9 and _"
)

// maxListed caps the rows a refusal names: a table with thousands of
// inexact amounts would otherwise print a refusal nobody can read.
const maxListed = 20

// Table is one generated table's money fields. Fields are SQL base
// names: "price" for the columns price_minor and price_currency. Legacy
// is the resource's [legacy] table, nil when the manifest declares none.
type Table struct {
	Name   string
	Fields []string
	Legacy *currency.Legacy
}

// Upgrade is the boot step for every generated SQL money column. It runs
// after the app's migrations and is idempotent: what it does is decided
// by which columns exist, so a second boot finds nothing to do.
//
// A legacy column <f> (an integer with no currency) is replaced by
// <f>_minor and <f>_currency, each amount rescaled exactly from the
// declared [legacy] scale into the declared currency. Dropping <f> is
// the old-binary tripwire: every query an old binary was generated with
// names <f>, so it fails instead of storing 1200 yen for twelve dollars.
// A money field that is new to an existing table gets its two columns,
// nullable, so every existing row has no amount. Then every field is
// checked for rows with an amount and no currency or the reverse.
//
// Any failure rolls everything back, so a refused boot leaves the
// database exactly as it was.
func Upgrade(ctx context.Context, db *sql.DB, tables []Table) error {
	if err := checkNames(tables); err != nil {
		return err
	}
	// One pinned connection: BEGIN, the work and COMMIT must share it,
	// and database/sql would otherwise hand each statement any idle one.
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	// IMMEDIATE takes the write lock before the first read, so of two
	// instances booting together the second waits (busy_timeout), then
	// reads what the first committed: the legacy column is gone, and it
	// changes nothing. Under a plain BEGIN both would read the legacy
	// column first. Under WAL, the second's first write would then fail
	// with SQLITE_BUSY, because a transaction that read an older
	// snapshot cannot become a writer, and that instance's boot would
	// fail. Nothing would be rescaled twice, but a routine double start
	// would stop one copy of the app. TestUpgradeConcurrentBootsRescaleOnce
	// fails that way under a plain BEGIN.
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			// WithoutCancel: a cancelled boot must still release the
			// write lock, or the next instance waits out busy_timeout.
			conn.ExecContext(context.WithoutCancel(ctx), "ROLLBACK")
		}
	}()

	notes, err := upgrade(ctx, conn, tables)
	if err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return err
	}
	committed = true
	// Logged only after COMMIT, so the log never reports columns that a
	// later failure rolled back.
	for _, n := range notes {
		slog.Info(n)
	}
	return nil
}

// CheckPairs is the one rule, checked alone: no row of any money field
// may have exactly one of its amount and its currency. Upgrade runs it
// too, inside its transaction.
func CheckPairs(ctx context.Context, db *sql.DB, tables []Table) error {
	if err := checkNames(tables); err != nil {
		return err
	}
	return checkPairs(ctx, db, tables)
}

// querier is what both a pinned *sql.Conn and a *sql.DB offer, so the
// pairing check runs the same inside Upgrade's transaction and alone.
type querier interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func upgrade(ctx context.Context, q querier, tables []Table) (notes []string, err error) {
	if _, err := q.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS rastrillo_money (version INTEGER NOT NULL)`); err != nil {
		return nil, err
	}
	var version sql.NullInt64
	if err := q.QueryRowContext(ctx, `SELECT MAX(version) FROM rastrillo_money`).Scan(&version); err != nil {
		return nil, err
	}
	if version.Valid && version.Int64 > Version {
		//lint:ignore ST1005 msgNewer is Appendix C.2's wording to the operator, verbatim
		return nil, fmt.Errorf(msgNewer, version.Int64)
	}

	for _, t := range tables {
		cols, err := columns(ctx, q, t.Name)
		if err != nil {
			return nil, err
		}
		for _, f := range t.Fields {
			note, err := upgradeField(ctx, q, t, f, cols)
			if err != nil {
				return nil, err
			}
			if note != "" {
				notes = append(notes, note)
			}
		}
	}

	if err := checkPairs(ctx, q, tables); err != nil {
		return nil, err
	}
	if !version.Valid {
		if _, err := q.ExecContext(ctx, `INSERT INTO rastrillo_money (version) VALUES (?)`, Version); err != nil {
			return nil, err
		}
	}
	return notes, nil
}

// upgradeField brings one field to its final two columns. cols is the
// table's column set and is kept current, so a later field of the same
// table sees what an earlier one changed. It returns a note for the
// operator whenever it added the two columns to a table that had no
// <f>, whether or not the table has rows: the note says existing rows
// have no amount, which an empty table makes trivially true.
func upgradeField(ctx context.Context, q querier, t Table, f string, cols map[string]bool) (string, error) {
	minor, cur := f+"_minor", f+"_currency"
	if !cols[f] {
		switch {
		case cols[minor] && cols[cur]:
			return "", nil
		case cols[minor] || cols[cur]:
			// Half a pair is a schema someone changed by hand. Adding the
			// other half would make every row with a value break the rule
			// with nothing to say which half is wrong, so boot stops.
			half := minor
			if cols[cur] {
				half = cur
			}
			n, err := count(ctx, q, fmt.Sprintf(`SELECT COUNT(*) FROM %q WHERE %q IS NOT NULL`, t.Name, half))
			if err != nil {
				return "", err
			}
			return "", fmt.Errorf(msgPairs, t.Name, f, n)
		}
		// A money field added to a table that already existed: the
		// regenerated CREATE TABLE IF NOT EXISTS adds no column to it, so
		// without this the app would need a hand-written migration. Both
		// columns are nullable with no default, because there is no
		// default currency in the schema and no amount to give old rows.
		if err := addColumns(ctx, q, t.Name, f, cols); err != nil {
			return "", err
		}
		return fmt.Sprintf(msgAddedColumns, t.Name, f, f, f), nil
	}

	// <f> exists, so the column is legacy. A legacy row is one whose
	// amount was never written with a currency; a row that already has
	// either new column set was, and is never touched. Such a row also
	// holds <f> only if someone wrote both by hand (an old binary never
	// wrote the new columns, and this one never writes <f>). It keeps
	// its new value, and its <f> is dropped with the column below,
	// unread and unreported.
	legacyRows := fmt.Sprintf(`%q IS NOT NULL`, f)
	if cols[minor] {
		legacyRows += fmt.Sprintf(` AND %q IS NULL`, minor)
	}
	if cols[cur] {
		legacyRows += fmt.Sprintf(` AND %q IS NULL`, cur)
	}
	if t.Legacy == nil {
		n, err := count(ctx, q, fmt.Sprintf(`SELECT COUNT(*) FROM %q WHERE %s`, t.Name, legacyRows))
		if err != nil {
			return "", err
		}
		// Guessing a currency or scale for real amounts would store a
		// wrong price nobody chose. With no rows there is nothing to
		// convert: a fresh database whose released CREATE TABLE still
		// declares the legacy column takes this path.
		if n > 0 {
			//lint:ignore ST1005 msgUndeclared is Appendix C.2's wording to the operator, verbatim
			return "", fmt.Errorf(msgUndeclared, t.Name, n, t.Name)
		}
	}

	if err := addColumns(ctx, q, t.Name, f, cols); err != nil {
		return "", err
	}
	if t.Legacy != nil {
		if err := convert(ctx, q, t, f, legacyRows); err != nil {
			return "", err
		}
	}
	if _, err := q.ExecContext(ctx, fmt.Sprintf(`ALTER TABLE %q DROP COLUMN %q`, t.Name, f)); err != nil {
		// An index, view or trigger naming <f> blocks the drop. Leaving <f>
		// in place would leave old binaries able to write, so boot stops.
		return "", fmt.Errorf(msgDropBlocked, t.Name, f, f, err)
	}
	delete(cols, f)
	return "", nil
}

// convert rescales every legacy row of f into the declared currency, or
// fails naming the rows it cannot convert exactly, before writing any.
func convert(ctx context.Context, q querier, t Table, f, legacyRows string) error {
	code, err := currency.Normalize(t.Legacy.Currency)
	if err != nil {
		return err
	}
	// Read every row before the first write: updating a table while a
	// cursor over it is open on the same connection can revisit or skip
	// rows.
	type update struct{ rowid, minor int64 }
	var (
		updates  []update
		failures []string
		failed   int
	)
	rs, err := q.QueryContext(ctx, fmt.Sprintf(`SELECT rowid, %q FROM %q WHERE %s ORDER BY rowid`, f, t.Name, legacyRows))
	if err != nil {
		return err
	}
	for rs.Next() {
		var (
			rowid int64
			raw   any
		)
		if err := rs.Scan(&rowid, &raw); err != nil {
			rs.Close()
			return err
		}
		// SQLite types columns by affinity, not enforcement: a legacy
		// INTEGER column can hold 12.5 or '12'. Those are listed like an
		// inexact amount, never truncated or read as zero.
		v, _, err := currency.AmountFromAny(raw)
		if err == nil {
			v, err = currency.Rescale(v, t.Legacy.Scale, code)
		}
		if err != nil {
			failed++
			if failed <= maxListed {
				failures = append(failures, fmt.Sprintf(msgRow, rowid, err))
			}
			continue
		}
		updates = append(updates, update{rowid, v})
	}
	if err := rs.Close(); err != nil {
		return err
	}
	if err := rs.Err(); err != nil {
		return err
	}
	if failed > 0 {
		lines := append([]string{fmt.Sprintf(msgFailed, t.Name, f, failed)}, failures...)
		if failed > maxListed {
			lines = append(lines, fmt.Sprintf(msgMore, failed-maxListed))
		}
		return errors.New(strings.Join(lines, "\n"))
	}
	set := fmt.Sprintf(`UPDATE %q SET %q = ?, %q = ? WHERE rowid = ?`, t.Name, f+"_minor", f+"_currency")
	for _, u := range updates {
		if _, err := q.ExecContext(ctx, set, u.minor, code, u.rowid); err != nil {
			return err
		}
	}
	return nil
}

// addColumns adds whichever of f's two columns the table lacks, both
// nullable with no default: §1's empty field is NULL in both, and a
// schema default currency would give rows a currency nobody chose.
func addColumns(ctx context.Context, q querier, table, f string, cols map[string]bool) error {
	for _, c := range []struct{ name, typ string }{{f + "_minor", "INTEGER"}, {f + "_currency", "TEXT"}} {
		if cols[c.name] {
			continue
		}
		if _, err := q.ExecContext(ctx, fmt.Sprintf(`ALTER TABLE %q ADD COLUMN %q %s`, table, c.name, c.typ)); err != nil {
			return err
		}
		cols[c.name] = true
	}
	return nil
}

func checkPairs(ctx context.Context, q querier, tables []Table) error {
	for _, t := range tables {
		for _, f := range t.Fields {
			n, err := count(ctx, q, fmt.Sprintf(`SELECT COUNT(*) FROM %q WHERE (%q IS NULL) <> (%q IS NULL)`, t.Name, f+"_minor", f+"_currency"))
			if err != nil {
				return err
			}
			if n > 0 {
				return fmt.Errorf(msgPairs, t.Name, f, n)
			}
		}
	}
	return nil
}

func columns(ctx context.Context, q querier, table string) (map[string]bool, error) {
	rs, err := q.QueryContext(ctx, `SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return nil, err
	}
	defer rs.Close()
	cols := map[string]bool{}
	for rs.Next() {
		var name string
		if err := rs.Scan(&name); err != nil {
			return nil, err
		}
		cols[name] = true
	}
	return cols, rs.Err()
}

func count(ctx context.Context, q querier, query string) (int64, error) {
	var n int64
	err := q.QueryRowContext(ctx, query).Scan(&n)
	return n, err
}

// checkNames refuses any table or field name outside [a-z0-9_]+.
// Identifiers cannot be bound as parameters, so these are interpolated
// into SQL. The generator only passes names the manifest's identifier
// rules accepted, but this package is public: a hand-written caller's
// name must not become SQL, and the check costs nothing at boot. It is
// also what makes %q a correct SQL identifier quote here: Go would
// escape a quote or backslash differently from SQL, and neither passes.
func checkNames(tables []Table) error {
	for _, t := range tables {
		if !validName(t.Name) {
			return fmt.Errorf(msgBadName, t.Name)
		}
		for _, f := range t.Fields {
			if !validName(f) {
				return fmt.Errorf(msgBadName, f)
			}
		}
	}
	return nil
}

func validName(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !('a' <= c && c <= 'z' || '0' <= c && c <= '9' || c == '_') {
			return false
		}
	}
	return true
}
