package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
)

// Querier is the read surface Read needs, satisfied by *sql.DB and by
// a pinned *sql.Conn.
type Querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

type Column struct {
	Name    string
	Type    string
	NotNull bool
	Default string
	PK      int
}

type Index struct {
	Name    string
	Unique  bool
	Columns []string
}

type Table struct {
	Name    string
	Columns []Column
	Indexes []Index
}

// Snapshot is a database's structure, read in a form that compares
// cleanly: two databases built by differently-formatted DDL produce
// equal Snapshots.
type Snapshot struct{ Tables []Table }

func Read(ctx context.Context, q Querier) (Snapshot, error) {
	var snap Snapshot
	rows, err := q.QueryContext(ctx, `SELECT name FROM sqlite_master
	  WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		return snap, err
	}
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			return snap, err
		}
		names = append(names, n)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return snap, err
	}
	rows.Close()

	for _, name := range names {
		t := Table{Name: name}
		if t.Columns, err = readColumns(ctx, q, name); err != nil {
			return snap, err
		}
		if t.Indexes, err = readIndexes(ctx, q, name); err != nil {
			return snap, err
		}
		snap.Tables = append(snap.Tables, t)
	}
	return snap, nil
}

func readColumns(ctx context.Context, q Querier, table string) ([]Column, error) {
	rows, err := q.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%q)", table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Column
	for rows.Next() {
		var (
			cid     int
			c       Column
			notNull int
			dflt    sql.NullString
		)
		if err := rows.Scan(&cid, &c.Name, &c.Type, &notNull, &dflt, &c.PK); err != nil {
			return nil, err
		}
		c.NotNull = notNull != 0
		c.Default = dflt.String
		// SQLite reports declared types with inconsistent case
		// depending on how the DDL was written; normalise so
		// "integer" and "INTEGER" are one type.
		c.Type = strings.ToUpper(strings.TrimSpace(c.Type))
		out = append(out, c)
	}
	// Column order comes from PRAGMA table_info in declaration order,
	// which formatting cannot change; leave it as-is rather than
	// sorting, so callers that rely on positional order (e.g. which
	// column is the primary key by convention) see it.
	return out, rows.Err()
}

func readIndexes(ctx context.Context, q Querier, table string) ([]Index, error) {
	rows, err := q.QueryContext(ctx, fmt.Sprintf("PRAGMA index_list(%q)", table))
	if err != nil {
		return nil, err
	}
	type li struct {
		name   string
		unique bool
		origin string
	}
	var list []li
	for rows.Next() {
		var (
			seq     int
			name    string
			unique  int
			origin  string
			partial int
		)
		if err := rows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			rows.Close()
			return nil, err
		}
		// origin "pk" and "u" are indexes SQLite creates for PRIMARY
		// KEY and UNIQUE constraints; they are already captured by the
		// column data, and their auto-generated names differ between
		// two databases with identical structure.
		if origin != "c" {
			continue
		}
		list = append(list, li{name, unique != 0, origin})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	var out []Index
	for _, l := range list {
		cols, err := indexColumns(ctx, q, l.name)
		if err != nil {
			return nil, err
		}
		out = append(out, Index{Name: l.name, Unique: l.unique, Columns: cols})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func indexColumns(ctx context.Context, q Querier, index string) ([]string, error) {
	rows, err := q.QueryContext(ctx, fmt.Sprintf("PRAGMA index_info(%q)", index))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var (
			seqno, cid int
			name       sql.NullString
		)
		if err := rows.Scan(&seqno, &cid, &name); err != nil {
			return nil, err
		}
		out = append(out, name.String)
	}
	return out, rows.Err()
}

func (s Snapshot) Equal(other Snapshot) bool { return len(s.Diff(other)) == 0 }

// diffLine is one Diff entry plus the one fact the rendered text does
// not carry in a form anything should depend on: which direction the
// difference points.
//
// adopt needs that to choose which recovery it prints, and re-deriving
// it by matching on the wording below would put a second, silent
// parser of these strings in the tree — reword a line and adopt would
// quietly start recommending the recovery that strands a schema
// change.
type diffLine struct {
	text string
	// extra is true when this database has something the migration
	// set does not define. Those are the only differences `baseline`
	// can safely stamp over: the set would create nothing new, so
	// recording it as applied leaves nothing uncreated. Every other
	// direction — missing, or differing — means baseline would record
	// work that never ran.
	extra bool
}

// Diff reports, in human-readable lines, what other has that s lacks
// and vice versa. It is both the check failure message and the input
// to Generate's destructive pass.
func (s Snapshot) Diff(other Snapshot) []string {
	var out []string
	for _, l := range s.diffLines(other) {
		out = append(out, l.text)
	}
	return out
}

func (s Snapshot) diffLines(other Snapshot) []diffLine {
	var out []diffLine
	mine, theirs := index(s), index(other)
	for name, t := range theirs {
		m, ok := mine[name]
		if !ok {
			out = append(out, diffLine{text: "missing table " + name})
			continue
		}
		out = append(out, diffTable(m, t)...)
	}
	for name := range mine {
		if _, ok := theirs[name]; !ok {
			out = append(out, diffLine{text: "extra table " + name, extra: true})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].text < out[j].text })
	return out
}

func diffTable(mine, theirs Table) []diffLine {
	var out []diffLine
	mc, tc := cols(mine), cols(theirs)
	for name, c := range tc {
		m, ok := mc[name]
		if !ok {
			out = append(out, diffLine{text: fmt.Sprintf("%s: missing column %s", mine.Name, name)})
			continue
		}
		if m != c {
			out = append(out, diffLine{text: fmt.Sprintf("%s: column %s differs (%+v vs %+v)", mine.Name, name, m, c)})
		}
	}
	for name := range mc {
		if _, ok := tc[name]; !ok {
			out = append(out, diffLine{text: fmt.Sprintf("%s: extra column %s", mine.Name, name), extra: true})
		}
	}
	mi, ti := idxs(mine), idxs(theirs)
	for name, i := range ti {
		m, ok := mi[name]
		if !ok {
			out = append(out, diffLine{text: fmt.Sprintf("%s: missing index %s", mine.Name, name)})
			continue
		}
		if m.Unique != i.Unique || strings.Join(m.Columns, ",") != strings.Join(i.Columns, ",") {
			out = append(out, diffLine{text: fmt.Sprintf("%s: index %s differs", mine.Name, name)})
		}
	}
	for name := range mi {
		if _, ok := ti[name]; !ok {
			out = append(out, diffLine{text: fmt.Sprintf("%s: extra index %s", mine.Name, name), extra: true})
		}
	}
	return out
}

func index(s Snapshot) map[string]Table {
	m := map[string]Table{}
	for _, t := range s.Tables {
		m[t.Name] = t
	}
	return m
}

func cols(t Table) map[string]Column {
	m := map[string]Column{}
	for _, c := range t.Columns {
		m[c.Name] = c
	}
	return m
}

func idxs(t Table) map[string]Index {
	m := map[string]Index{}
	for _, i := range t.Indexes {
		m[i.Name] = i
	}
	return m
}
