// Package generate's store emitter (this file) turns a validated
// rastrillo.Resource into the SQL/Go inputs sqlc needs: one
// gen/store/sqlc.yaml covering every resource, and per resource a
// gen/store/<name>/{schema.sql,queries.sql,migrations.go}. It is pure
// text generation — EmitStore never shells out to sqlc; running sqlc
// against this output is a later generator step.
//
// schema.sql and a new resource's first migration both describe the
// same table from one internal columns model (columns, below) so the
// two cannot drift: schema.sql says CREATE TABLE for sqlc's benefit (it
// wants clean DDL, no IF NOT EXISTS), migrations.go says CREATE TABLE
// IF NOT EXISTS because the running app reapplies its migration list at
// boot. schema.sql is always the final shape; a released migrations.go
// only ever grows (migrationsgen.go), and Boot finishes the money
// columns.
package generate

import (
	"bytes"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"amadan.net/rastrillo/rastrillo"
)

// EmitStore writes per resource either the sqlc inputs (exclusive:
// gen/store/<name>/{schema.sql,queries.sql,migrations.go}) or the
// template-rendered eventlog store (mergeable:
// gen/store/<name>/{store.go,migrations.go} — see mergeablestore.go),
// plus gen/store/sqlc.yaml covering the exclusive resources — written
// only when at least one exists, since a module of only-mergeable
// resources needs no sqlc tool at all. Returns the emitted file paths
// (for idempotency and skip accounting). Pure text generation —
// running sqlc is a later step.
//
// h is the released migration statements (ReadHistory), which each
// exclusive store's migrations.go keeps as its prefix. appended is one
// op.generate.migration_appended line per statement added to them.
func EmitStore(genDir string, rs []rastrillo.Resource, h History) (paths, appended []string, err error) {
	sorted := append([]rastrillo.Resource(nil), rs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	var exclusive []rastrillo.Resource
	for _, r := range sorted {
		if r.Store != rastrillo.Mergeable {
			exclusive = append(exclusive, r)
		}
	}

	storeDir := filepath.Join(genDir, "store")

	if len(exclusive) > 0 {
		yamlPath := filepath.Join(storeDir, "sqlc.yaml")
		if err := writeFileIfChanged(yamlPath, sqlcYAML(exclusive)); err != nil {
			return nil, nil, fmt.Errorf("sqlc.yaml: %w", err)
		}
		paths = append(paths, yamlPath)
	}

	for _, r := range sorted {
		resDir := filepath.Join(storeDir, r.Name)

		if r.Store == rastrillo.Mergeable {
			storeSrc, err := format.Source(mergeableStoreGo(r))
			if err != nil {
				return nil, nil, fmt.Errorf("%s: store.go: %w", r.Name, err)
			}
			storePath := filepath.Join(resDir, "store.go")
			if err := writeFileIfChanged(storePath, storeSrc); err != nil {
				return nil, nil, fmt.Errorf("%s: store.go: %w", r.Name, err)
			}
			paths = append(paths, storePath)

			migSrc, err := format.Source(mergeableMigrationsGo(r))
			if err != nil {
				return nil, nil, fmt.Errorf("%s: migrations.go: %w", r.Name, err)
			}
			migPath := filepath.Join(resDir, "migrations.go")
			if err := writeFileIfChanged(migPath, migSrc); err != nil {
				return nil, nil, fmt.Errorf("%s: migrations.go: %w", r.Name, err)
			}
			paths = append(paths, migPath)
			continue
		}

		schemaPath := filepath.Join(resDir, "schema.sql")
		if err := writeFileIfChanged(schemaPath, schemaSQL(r)); err != nil {
			return nil, nil, fmt.Errorf("%s: schema.sql: %w", r.Name, err)
		}
		paths = append(paths, schemaPath)

		queriesPath := filepath.Join(resDir, "queries.sql")
		if err := writeFileIfChanged(queriesPath, queriesSQL(r)); err != nil {
			return nil, nil, fmt.Errorf("%s: queries.sql: %w", r.Name, err)
		}
		paths = append(paths, queriesPath)

		stmts, err := appendMigrations(h[r.Name], r)
		if err != nil {
			return nil, nil, err
		}
		appended = append(appended, appendedNotes(r, h[r.Name], stmts)...)
		migSrc, err := format.Source(migrationsGo(r, stmts))
		if err != nil {
			return nil, nil, fmt.Errorf("%s: migrations.go: %w", r.Name, err)
		}
		migPath := filepath.Join(resDir, "migrations.go")
		if err := writeFileIfChanged(migPath, migSrc); err != nil {
			return nil, nil, fmt.Errorf("%s: migrations.go: %w", r.Name, err)
		}
		paths = append(paths, migPath)
	}

	return paths, appended, nil
}

// writeFileIfChanged writes content to path only when the file is
// missing or its current content differs — later emitters reuse this
// to keep re-generation idempotent (no spurious mtime churn, no
// rewriting bytes that are already correct).
func writeFileIfChanged(path string, content []byte) error {
	if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, content) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o644)
}

// column is the emitter's single shared model of a resource's data
// columns; schema.sql, queries.sql and migrations.go are all rendered
// from it so they cannot describe different tables.
type column struct {
	Name string // declared name, e.g. "MaxPerOrder"
	SQL  string // sqlName(Name), e.g. "max_per_order"
	Kind rastrillo.Kind
}

// columns returns r's data columns (excluding id/created_at/updated_at,
// which every table has unconditionally) as the union of its List
// columns and Form fields, in first-declaration order: List columns
// first, then Form basics, then Form advanced, skipping any name
// (compared via sqlName, so case-insensitively) already present.
func columns(r rastrillo.Resource) []column {
	var cols []column
	seen := map[string]bool{}
	add := func(name string, kind rastrillo.Kind) {
		sql := sqlName(name)
		if seen[sql] {
			return
		}
		seen[sql] = true
		cols = append(cols, column{Name: name, SQL: sql, Kind: kind})
	}
	for _, c := range r.List.Columns {
		add(c.Field, c.Kind)
	}
	for _, f := range r.Form.Basics {
		add(f.Name, f.Kind)
	}
	for _, f := range r.Form.Advanced {
		add(f.Name, f.Kind)
	}
	return cols
}

// kindSQLType is the single Kind→SQL map the boundary rule (money
// stores as an integer, never a float) lives behind: text/textarea
// collapse to the same TEXT column, money's amount column is INTEGER
// (columnDefs adds its currency beside it). Every Kind
// Validate accepts has an entry here; an unrecognized Kind means
// Validate's own set of accepted kinds and this map have drifted, which
// Validate guarantees cannot happen for a resource that reached this
// emitter — so sqlColumnType panics rather than emitting bad SQL.
var kindSQLType = map[rastrillo.Kind]string{
	rastrillo.Text:     "TEXT NOT NULL DEFAULT ''",
	rastrillo.Textarea: "TEXT NOT NULL DEFAULT ''",
	rastrillo.Money:    "INTEGER",
}

func sqlColumnType(k rastrillo.Kind) string {
	t, ok := kindSQLType[k]
	if !ok {
		panic(fmt.Sprintf("generate: unknown Kind %q", k))
	}
	return t
}

// sqlName converts a declared identifier (PascalCase or camelCase, per
// Validate's identPattern) to a snake_case SQL column name: an
// underscore is inserted before each interior uppercase run (a run
// starting at index 0 gets none, since the name is already lowercase
// there once the whole thing is lowercased), then the whole string is
// lowercased. "MaxPerOrder" -> "max_per_order".
func sqlName(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		isUpper := c >= 'A' && c <= 'Z'
		prevUpper := i > 0 && s[i-1] >= 'A' && s[i-1] <= 'Z'
		if isUpper && i > 0 && !prevUpper {
			b.WriteByte('_')
		}
		if isUpper {
			c += 'a' - 'A'
		}
		b.WriteByte(c)
	}
	return b.String()
}

// pascalCase title-cases a snake_case name for use in a Go/sqlc
// identifier: "ticket_types" -> "TicketTypes".
func pascalCase(s string) string {
	parts := strings.Split(s, "_")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, "")
}

// singularPascal is pascalCase after stripping one trailing "s" from s,
// if present — the plan's singularization rule for per-record query
// names: "notes" -> "Note".
func singularPascal(s string) string {
	s = strings.TrimSuffix(s, "s")
	return pascalCase(s)
}

// scoped reports whether r declares per-user scoping — the one flag
// this emitter and the action emitter both branch on. Validate already
// rejected any Scope other than "" and UserScoped.
func scoped(r rastrillo.Resource) bool {
	return r.Scope == rastrillo.UserScoped
}

// schemaSQL renders the sqlc-facing DDL: a plain CREATE TABLE with an
// id primary key, the owner column when the resource is user-scoped,
// r's data columns, and the two timestamp columns every resource gets.
// The owner index rides along (sqlc parses CREATE INDEX fine) so
// schema.sql and migrations.go keep describing the same table from the
// same model.
func schemaSQL(r rastrillo.Resource) []byte {
	var b strings.Builder
	b.WriteString("-- Code generated by rastrillo generate; DO NOT EDIT.\n")
	fmt.Fprintf(&b, "CREATE TABLE %s (\n", r.Name)
	b.WriteString(strings.Join(tableBody(r), "\n"))
	b.WriteString("\n);\n")
	if scoped(r) {
		fmt.Fprintf(&b, "CREATE INDEX %s_owner ON %s (owner);\n", r.Name, r.Name)
	}
	return []byte(b.String())
}

// tableBody is the column lines of r's table, shared by schema.sql and
// a new resource's first migration so the two cannot describe
// different tables.
func tableBody(r rastrillo.Resource) []string {
	lines := []string{"  id INTEGER PRIMARY KEY,"}
	if scoped(r) {
		lines = append(lines, "  owner TEXT NOT NULL,")
	}
	for _, c := range columns(r) {
		for _, d := range columnDefs(c) {
			lines = append(lines, "  "+d+",")
		}
	}
	return append(lines, "  created_at TEXT NOT NULL,", "  updated_at TEXT NOT NULL")
}

// columnDefs is c's column definitions. A money field is two columns,
// <f>_minor and <f>_currency, both nullable with no default (spec §1):
// an empty field has neither, and a default of 0 would give every
// existing row an amount with no currency, which the pairing rule
// refuses at boot.
func columnDefs(c column) []string {
	if c.Kind == rastrillo.Money {
		return []string{c.SQL + "_minor INTEGER", c.SQL + "_currency TEXT"}
	}
	return []string{c.SQL + " " + sqlColumnType(c.Kind)}
}

// columnNames is c's column names, in columnDefs' order.
func columnNames(c column) []string {
	if c.Kind == rastrillo.Money {
		return []string{c.SQL + "_minor", c.SQL + "_currency"}
	}
	return []string{c.SQL}
}

// migrationsGo renders stmts (appendMigrations: the released statements
// and anything appended) as the Go source file exposing Migrations.
// A statement is a raw string literal, as every released file has it,
// so a released file regenerates byte for byte.
func migrationsGo(r rastrillo.Resource, stmts []string) []byte {
	quoted := make([]string, len(stmts))
	for i, s := range stmts {
		if strings.Contains(s, "`") {
			quoted[i] = strconv.Quote(s)
		} else {
			quoted[i] = "`" + s + "`"
		}
	}
	var b strings.Builder
	b.WriteString("// Code generated by rastrillo generate; DO NOT EDIT.\n")
	fmt.Fprintf(&b, "package %sstore\n\n", r.Name)
	b.WriteString("// Migrations is the schema for Options.Migrations. generate appends to\n")
	b.WriteString("// it when the resource gains a field or a scope, and never edits a\n")
	b.WriteString("// released statement. Money columns are gen.Boot's. Any other change is\n")
	b.WriteString("// the app's own migration.\n")
	fmt.Fprintf(&b, "var Migrations = []string{%s}\n", strings.Join(quoted, ", "))
	return []byte(b.String())
}

// searchColumns returns r's List columns eligible for the search LIKE
// clause: text/textarea kinds only (a money column has nothing to
// substring-match).
func searchColumns(r rastrillo.Resource) []column {
	var cols []column
	for _, c := range r.List.Columns {
		if c.Kind == rastrillo.Text || c.Kind == rastrillo.Textarea {
			cols = append(cols, column{Name: c.Field, SQL: sqlName(c.Field), Kind: c.Kind})
		}
	}
	return cols
}

// filterFields returns r's filter field names as an ordered union of bare
// Filter and Filters[].Field, in first-mention order with no duplicates.
func filterFields(r rastrillo.Resource) []string {
	var fields []string
	seen := map[string]bool{}
	add := func(name string) {
		if seen[name] {
			return
		}
		seen[name] = true
		fields = append(fields, name)
	}
	for _, f := range r.List.Filter {
		add(f)
	}
	for _, flt := range r.List.Filters {
		add(flt.Field)
	}
	return fields
}

// whereClauses builds the optional-filter WHERE conditions shared by
// ListNotes and CountNotes: one clause for the search box (if List.Search
// is set and there is at least one eligible column) and one
// equality clause per filter field (from filterFields, the union of
// bare Filter and Filters[].Field). Each condition is written so
// an empty sqlc.arg() disables it — the caller decides whether to run
// the query with or without a value, sqlc gives it exactly one query to
// call either way.
func whereClauses(r rastrillo.Resource) []string {
	var clauses []string

	// The owner clause leads and is always-on — unlike every clause
	// below, there is no empty-disables-it escape: a scoped list that
	// forgot its owner would be everyone's list.
	if scoped(r) {
		clauses = append(clauses, "owner = sqlc.arg(owner)")
	}

	if r.List.Search {
		cols := searchColumns(r)
		switch len(cols) {
		case 0:
			// nothing text-like to search over
		case 1:
			clauses = append(clauses, fmt.Sprintf(
				"(sqlc.arg(search) = '' OR %s LIKE '%%' || sqlc.arg(search) || '%%')", cols[0].SQL))
		default:
			var parts []string
			for _, c := range cols {
				parts = append(parts, fmt.Sprintf("%s LIKE '%%' || sqlc.arg(search) || '%%'", c.SQL))
			}
			clauses = append(clauses, fmt.Sprintf(
				"(sqlc.arg(search) = '' OR (%s))", strings.Join(parts, " OR ")))
		}
	}

	for _, f := range filterFields(r) {
		sql := sqlName(f)
		clauses = append(clauses, fmt.Sprintf(
			"(sqlc.arg(filter_%s) = '' OR %s = sqlc.arg(filter_%s))", sql, sql, sql))
	}

	return clauses
}

// buildStatement joins firstLine, an optional WHERE block built from
// clauses (the first clause on a "WHERE " line, the rest each on their
// own "  AND " line), and any tailLines, then terminates the whole
// statement with a semicolon attached to its last line.
func buildStatement(firstLine string, clauses []string, tailLines []string) string {
	lines := []string{firstLine}
	for i, c := range clauses {
		if i == 0 {
			lines = append(lines, "WHERE "+c)
		} else {
			lines = append(lines, "  AND "+c)
		}
	}
	lines = append(lines, tailLines...)
	lines[len(lines)-1] += ";"
	return strings.Join(lines, "\n") + "\n"
}

// updateStatement renders an UPDATE ... SET statement for one field
// group (Form.Basics or Form.Advanced): one "col = sqlc.arg(col)" per
// field plus the always-present updated_at bump, keyed on id — AND
// owner for a scoped resource, so an UPDATE against someone else's row
// is a no-op the same way a Get against it is a no-row.
func updateStatement(r rastrillo.Resource, fields []rastrillo.Field) string {
	var sets []string
	for _, f := range fields {
		for _, col := range columnNames(column{SQL: sqlName(f.Name), Kind: f.Kind}) {
			sets = append(sets, fmt.Sprintf("%s = sqlc.arg(%s)", col, col))
		}
	}
	sets = append(sets, "updated_at = sqlc.arg(now)")
	return fmt.Sprintf("UPDATE %s SET %s WHERE %s;\n", r.Name, strings.Join(sets, ", "), recordKey(r))
}

// recordKey is the WHERE condition that identifies ONE record a caller
// may touch: id alone for an unscoped resource, id AND owner for a
// scoped one — the single spelling Get, both Updates and Delete share,
// so no per-record query can ever forget the owner filter the others
// enforce.
func recordKey(r rastrillo.Resource) string {
	if scoped(r) {
		return "id = sqlc.arg(id) AND owner = sqlc.arg(owner)"
	}
	return "id = sqlc.arg(id)"
}

// queriesSQL renders the sqlc named-query file for r: List/Count (with
// the optional search+filter WHERE), Get, Create, and one Update per
// form field group (Advanced only when non-empty). Query names
// singularize r.Name for per-record queries (Task 5 brief:
// "notes" -> "Note") and title-case it as-is for per-collection ones.
func queriesSQL(r rastrillo.Resource) []byte {
	plural := pascalCase(r.Name)
	singular := singularPascal(r.Name)
	wc := whereClauses(r)

	var blocks []string

	blocks = append(blocks, fmt.Sprintf("-- name: List%s :many\n%s", plural,
		buildStatement(fmt.Sprintf("SELECT * FROM %s", r.Name), wc,
			[]string{
				"ORDER BY created_at DESC, id DESC",
				"LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset)",
			})))

	blocks = append(blocks, fmt.Sprintf("-- name: Count%s :one\n%s", plural,
		buildStatement(fmt.Sprintf("SELECT COUNT(*) FROM %s", r.Name), wc, nil)))

	blocks = append(blocks, fmt.Sprintf("-- name: Get%s :one\nSELECT * FROM %s WHERE %s;\n",
		singular, r.Name, recordKey(r)))

	cols := columns(r)
	names := make([]string, 0, len(cols)+3)
	vals := make([]string, 0, len(cols)+3)
	if scoped(r) {
		names = append(names, "owner")
		vals = append(vals, "sqlc.arg(owner)")
	}
	for _, c := range cols {
		for _, col := range columnNames(c) {
			names = append(names, col)
			vals = append(vals, fmt.Sprintf("sqlc.arg(%s)", col))
		}
	}
	names = append(names, "created_at", "updated_at")
	// Both timestamp columns take the same value at creation time, and
	// sqlc accepts reusing one sqlc.arg(now) twice in the same query
	// (verified against sqlc's sqlite engine in Task 6) — no need for
	// the separate now/now2 args an earlier draft used.
	vals = append(vals, "sqlc.arg(now)", "sqlc.arg(now)")
	blocks = append(blocks, fmt.Sprintf("-- name: Create%s :one\n%s", singular,
		buildStatement(
			fmt.Sprintf("INSERT INTO %s (%s)", r.Name, strings.Join(names, ", ")),
			nil,
			[]string{fmt.Sprintf("VALUES (%s)", strings.Join(vals, ", ")), "RETURNING id"})))

	blocks = append(blocks, fmt.Sprintf("-- name: Update%sBasics :exec\n%s",
		singular, updateStatement(r, r.Form.Basics)))

	if len(r.Form.Advanced) > 0 {
		blocks = append(blocks, fmt.Sprintf("-- name: Update%sAdvanced :exec\n%s",
			singular, updateStatement(r, r.Form.Advanced)))
	}

	blocks = append(blocks, fmt.Sprintf("-- name: Delete%s :exec\nDELETE FROM %s WHERE %s;\n",
		singular, r.Name, recordKey(r)))

	var b strings.Builder
	b.WriteString("-- Code generated by rastrillo generate; DO NOT EDIT.\n\n")
	b.WriteString(strings.Join(blocks, "\n"))
	return []byte(b.String())
}

// sqlcYAML renders gen/store/sqlc.yaml: one sql: entry per resource,
// each pointing at that resource's own schema.sql/queries.sql and
// generating into its own <name>store Go package.
func sqlcYAML(rs []rastrillo.Resource) []byte {
	var b strings.Builder
	b.WriteString("# Code generated by rastrillo generate; DO NOT EDIT.\n")
	b.WriteString("version: \"2\"\n")
	b.WriteString("sql:\n")
	for _, r := range rs {
		b.WriteString("  - engine: \"sqlite\"\n")
		fmt.Fprintf(&b, "    schema: \"%s/schema.sql\"\n", r.Name)
		fmt.Fprintf(&b, "    queries: \"%s/queries.sql\"\n", r.Name)
		b.WriteString("    gen:\n")
		b.WriteString("      go:\n")
		fmt.Fprintf(&b, "        package: \"%sstore\"\n", r.Name)
		fmt.Fprintf(&b, "        out: \"%s\"\n", r.Name)
		// A nullable money column reads as *int64 and *string, so an
		// empty field is nil rather than a zero amount in no currency.
		b.WriteString("        emit_pointers_for_null_types: true\n")
	}
	return []byte(b.String())
}
