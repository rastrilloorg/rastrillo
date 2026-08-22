package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/carlosframework/rastrillo/gormlite"
)

// Change is one generated statement.
type Change struct {
	SQL         string
	Destructive bool
}

// recorder is a GORM logger that captures every statement GORM
// executes. It is how this package generates DDL without writing any:
// AutoMigrate already knows how to produce correct SQLite DDL,
// including gormlite's twelve-step rebuild, so the generator runs it
// against a throwaway in-memory copy and keeps what it said.
type recorder struct {
	logger.Interface
	mu   sync.Mutex
	sqls []string
}

func (r *recorder) Trace(ctx context.Context, begin time.Time,
	fc func() (string, int64), err error) {
	s, _ := fc()
	r.mu.Lock()
	r.sqls = append(r.sqls, s)
	r.mu.Unlock()
}

func (r *recorder) take() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.sqls
	r.sqls = nil
	return out
}

// gormOn wraps an existing *sql.DB in GORM with a recording logger.
func gormOn(d *sql.DB) (*gorm.DB, *recorder, error) {
	rec := &recorder{Interface: logger.Discard}
	g, err := gorm.Open(gormlite.Dialector{Conn: d}, &gorm.Config{Logger: rec})
	return g, rec, err
}

// Generate computes the migration that would bring a database built
// from ms up to what models declares.
//
// It emits nothing by hand. The additive pass runs AutoMigrate against
// a replay of ms and keeps the SQL GORM executed. The destructive pass
// structurally compares that result against a clean AutoMigrate of
// models alone — AutoMigrate never drops, so anything left over is a
// column, index or table the models no longer declare — and emits the
// drops through the migrator, also captured.
func Generate(ctx context.Context, ms []Migration, models []any) ([]Change, error) {
	current, err := Replay(ctx, ms)
	if err != nil {
		return nil, err
	}
	defer current.Close()

	g, rec, err := gormOn(current)
	if err != nil {
		return nil, err
	}
	if err := g.AutoMigrate(models...); err != nil {
		return nil, fmt.Errorf("migrate: automigrate models: %w", err)
	}
	var out []Change
	for _, s := range rec.take() {
		if isDDL(s) {
			out = append(out, Change{SQL: ensureSemicolon(s)})
		}
	}

	// Desired: models alone, in a clean database.
	clean, err := Memory()
	if err != nil {
		return nil, err
	}
	defer clean.Close()
	cg, _, err := gormOn(clean)
	if err != nil {
		return nil, err
	}
	if err := cg.AutoMigrate(models...); err != nil {
		return nil, err
	}
	want, err := Read(ctx, clean)
	if err != nil {
		return nil, err
	}
	have, err := Read(ctx, current)
	if err != nil {
		return nil, err
	}

	drops, err := dropChanges(ctx, g, rec, have, want)
	if err != nil {
		return nil, err
	}
	return append(out, drops...), nil
}

// dropChanges emits, through the migrator so the SQL is gormlite's own,
// the removals AutoMigrate will never perform.
func dropChanges(ctx context.Context, g *gorm.DB, rec *recorder, have, want Snapshot) ([]Change, error) {
	var out []Change
	wantTables := index(want)
	haveTables := index(have)

	names := make([]string, 0, len(haveTables))
	for n := range haveTables {
		names = append(names, n)
	}
	sort.Strings(names)

	m := g.Migrator()
	for _, name := range names {
		if name == "rastrillo_migrations" {
			continue
		}
		wt, ok := wantTables[name]
		if !ok {
			if err := m.DropTable(name); err != nil {
				return nil, err
			}
			out = append(out, recorded(rec, true)...)
			continue
		}
		// DropColumn and DropIndex both route through GORM's schema
		// parser, and that parser resolves a table name by calling
		// TableName() on a *freshly zero-valued* instance of the value's
		// type (schema.ParseWithSpecialTableName uses reflect.New, not
		// the value passed in) — so a tableRef{name} carrying name in a
		// struct field never actually surfaces it that way; name comes
		// back "". Pre-setting the table on the session with .Table(name)
		// works around that: it fills in Statement.Table before
		// RunWithValue builds its per-call Statement, and GORM's
		// ParseWithSpecialTableName takes that pre-set table over calling
		// TableName() at all when it is non-empty. tableRef is still
		// passed as the value (rather than a plain string) only so
		// RunWithValue takes the schema-parsing branch instead of the
		// string branch, which never sets stmt.Schema and would leave
		// DropColumn's unconditional stmt.Schema.LookUpField call
		// dereferencing a nil Schema.
		mt := g.Table(name).Migrator()
		wc := cols(wt)
		for _, c := range haveTables[name].Columns {
			if _, ok := wc[c.Name]; ok {
				continue
			}
			if err := mt.DropColumn(tableRef{name}, c.Name); err != nil {
				return nil, err
			}
			out = append(out, recorded(rec, true)...)
		}
		wi := idxs(wt)
		for _, i := range haveTables[name].Indexes {
			if _, ok := wi[i.Name]; ok {
				continue
			}
			if err := mt.DropIndex(tableRef{name}, i.Name); err != nil {
				return nil, err
			}
			out = append(out, recorded(rec, true)...)
		}
	}
	return out, nil
}

// tableRef stands in for a model when there is no model left for a
// table — the models dropped it, so there is no struct to pass. Its
// name field is not what carries the table name to the migrator (see
// the comment above its use in dropChanges for why a Tabler's own
// TableName method cannot be trusted to do that here); it exists so
// RunWithValue takes GORM's schema-parsing branch instead of the bare
// string branch, which is what DropColumn needs.
type tableRef struct{ name string }

func (t tableRef) TableName() string { return t.name }

func recorded(rec *recorder, destructive bool) []Change {
	var out []Change
	for _, s := range rec.take() {
		if isDDL(s) {
			out = append(out, Change{SQL: ensureSemicolon(s), Destructive: destructive})
		}
	}
	return out
}

// isDDL filters the recorder's capture down to schema statements.
// AutoMigrate also issues introspection queries (PRAGMA, sqlite_master
// SELECTs) that must not end up in a migration file.
func isDDL(s string) bool {
	u := strings.ToUpper(strings.TrimSpace(s))
	for _, p := range []string{"CREATE TABLE", "CREATE UNIQUE INDEX", "CREATE INDEX",
		"ALTER TABLE", "DROP TABLE", "DROP INDEX", "INSERT INTO"} {
		if strings.HasPrefix(u, p) {
			return true
		}
	}
	return false
}

func ensureSemicolon(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, ";") {
		return s
	}
	return s + ";"
}

// SchemaSQL is the accumulated snapshot written to migrations/schema.sql:
// the DDL of a database with every migration applied, read back from
// sqlite_master so it is SQLite's own normalised text rather than the
// concatenation of the migration files.
func SchemaSQL(ctx context.Context, ms []Migration) (string, error) {
	d, err := Replay(ctx, ms)
	if err != nil {
		return "", err
	}
	defer d.Close()
	rows, err := d.QueryContext(ctx, `SELECT sql FROM sqlite_master
	  WHERE sql IS NOT NULL AND name NOT LIKE 'sqlite_%' ORDER BY type DESC, name`)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var b strings.Builder
	b.WriteString("-- Generated by rastrillo migration generate; DO NOT EDIT.\n")
	b.WriteString("-- The schema every migration in this directory adds up to.\n\n")
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return "", err
		}
		b.WriteString(strings.TrimSpace(s))
		b.WriteString(";\n\n")
	}
	return b.String(), rows.Err()
}
