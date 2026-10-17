// Package generate's migration emitter (this file) keeps a generated
// store's Migrations append-only once released.
//
// Apps keep generated statements in a migration ledger under IDs that
// count them (notes wraps bookmarksstore.Migrations as
// bookmarks_gen/0001_init, 0002_init, ...), and migrate.Apply refuses a
// statement whose checksum changed. Rewriting a released CREATE TABLE
// (to split a money column, say) would therefore stop every such app
// before its Boot could upgrade anything. So a released statement is
// kept byte for byte, and what the resource has gained since is
// appended. Money columns are never appended: Boot's sqlmoney.Upgrade
// owns them, replacing a released legacy column or adding the columns
// of a field added later, so no table ever holds both a legacy <f> and
// <f>_minor/<f>_currency.
package generate

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/migrate"
)

// migrationAppended is Appendix C.2's op.generate.migration_appended.
const migrationAppended = "%s: appended %s to gen/store/%s/migrations.go; released statements are never edited"

// History is each exclusive resource's released migration statements,
// by resource name, read from the app's real gen/ tree. It is an input
// to the pipeline, separate from where the pipeline writes, because
// generate --check writes into an empty temp dir: emitting there
// without the released statements would produce first-release SQL and
// report every upgraded tree as stale.
type History map[string][]string

// migrationsHandEdited is the refusal for a gen/store/<r>/migrations.go
// whose Migrations is neither the list of string literals generate
// wrote nor a mergeable store's. The wording is approved copy
// (op.generate.migrations_hand_edited); change it only through a copy review.
const migrationsHandEdited = "%s: Migrations is not a list of string literals, so the released statements cannot be read. Restore the generated file, or write each statement as a string literal, then run rastrillo generate again."

// ReadHistory reads gen/store/<r>/migrations.go for every exclusive
// resource in rs. A resource with no file yet, or with a mergeable
// store's file (it has just become exclusive, and released no table of
// its own), has no history and gets its final shape in its first
// statement. Any other file whose Migrations is not a list of string
// literals was edited by hand and is refused: read as no history it
// would be rewritten as a first release, and the app's ledger would
// then stop boot on the new statement without naming the edit.
func ReadHistory(genDir string, rs []rastrillo.Resource) (History, error) {
	h := History{}
	for _, r := range rs {
		if r.Store == rastrillo.Mergeable {
			continue
		}
		stmts, err := releasedMigrations(filepath.Join(genDir, "store", r.Name, "migrations.go"))
		switch {
		case errors.Is(err, fs.ErrNotExist), errors.Is(err, errMergeable):
			continue
		case errors.Is(err, errNotStatements):
			//lint:ignore ST1005 migrationsHandEdited is the operator's line, verbatim
			return nil, fmt.Errorf(migrationsHandEdited, "gen/store/"+r.Name+"/migrations.go")
		case err != nil:
			return nil, err
		}
		h[r.Name] = stmts
	}
	return h, nil
}

// errNotStatements is a Migrations that is neither a list of string
// literals nor the mergeable form: a hand edit. errMergeable is the
// mergeable form, Migrations = mergeableSchemaFunc().
var (
	errNotStatements = errors.New("not a list of string literals")
	errMergeable     = errors.New("a mergeable store's migrations")
)

// isMergeableMigrations recognises every value the generator has ever
// written for a mergeable store's Migrations: eventlogSchemaSQL() since
// the money release, and the re-export eventlog.Migrations that
// d36b5fcf wrote before it. A resource that was mergeable under that
// release and becomes exclusive now still has the older file, untouched,
// so refusing it as a hand edit would stop generate for a file nobody
// edited. testdata/released holds the older shape as it shipped.
func isMergeableMigrations(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.CallExpr:
		id, ok := x.Fun.(*ast.Ident)
		return ok && id.Name == mergeableSchemaFunc && len(x.Args) == 0
	case *ast.SelectorExpr:
		pkg, ok := x.X.(*ast.Ident)
		return ok && pkg.Name == "eventlog" && x.Sel.Name == "Migrations"
	}
	return false
}

// releasedMigrations is the string literals of a generated
// migrations.go's Migrations, in order. It parses the file rather than
// matching text, so the statements come back exactly as the compiler
// would see them.
func releasedMigrations(path string) ([]string, error) {
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	for _, d := range f.Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok || g.Tok != token.VAR {
			continue
		}
		for _, spec := range g.Specs {
			v := spec.(*ast.ValueSpec)
			if len(v.Names) != 1 || v.Names[0].Name != "Migrations" || len(v.Values) != 1 {
				continue
			}
			if isMergeableMigrations(v.Values[0]) {
				return nil, fmt.Errorf("%s: %w", path, errMergeable)
			}
			lit, ok := v.Values[0].(*ast.CompositeLit)
			if !ok {
				return nil, fmt.Errorf("%s: %w", path, errNotStatements)
			}
			var out []string
			for _, e := range lit.Elts {
				b, ok := e.(*ast.BasicLit)
				if !ok || b.Kind != token.STRING {
					return nil, fmt.Errorf("%s: %w", path, errNotStatements)
				}
				s, err := strconv.Unquote(b.Value)
				if err != nil {
					return nil, fmt.Errorf("%s: %w", path, err)
				}
				out = append(out, s)
			}
			return out, nil
		}
	}
	return nil, fmt.Errorf("%s: %w", path, errNotStatements)
}

// appendMigrations is released plus the statements that bring a replay
// of it to r's non-money columns: one ADD COLUMN per missing column,
// and the owner column and index of a resource scoped since. Nothing
// released is removed or rewritten, so a removed field's column stays,
// as migrations are never reversed. With nothing released, it is r's
// first statement in its final shape.
//
// Each appended statement is run against the replay before it is kept,
// so a statement the app's own boot would fail on fails generate.
func appendMigrations(released []string, r rastrillo.Resource) ([]string, error) {
	if len(released) == 0 {
		return firstRelease(r), nil
	}
	ms := make([]migrate.Migration, len(released))
	for i, s := range released {
		ms[i] = migrate.Migration{ID: fmt.Sprintf("%s_gen/%04d", r.Name, i+1), SQL: s}
	}
	ctx := context.Background()
	db, err := migrate.Replay(ctx, ms)
	if err != nil {
		return nil, fmt.Errorf("gen/store/%s/migrations.go: %w", r.Name, err)
	}
	defer db.Close()

	have := map[string]bool{}
	rows, err := db.QueryContext(ctx, "SELECT name FROM pragma_table_info(?)", r.Name)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return nil, err
		}
		have[name] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var add []string
	if scoped(r) && !have["owner"] {
		add = append(add, fmt.Sprintf("ALTER TABLE %s ADD COLUMN owner TEXT NOT NULL DEFAULT ''", r.Name))
	}
	for _, c := range columns(r) {
		if c.Kind != rastrillo.Money && !have[c.SQL] {
			add = append(add, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", r.Name, c.SQL, sqlColumnType(c.Kind)))
		}
	}
	if scoped(r) {
		var n int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?", r.Name+"_owner").Scan(&n); err != nil {
			return nil, err
		}
		if n == 0 {
			add = append(add, fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s_owner ON %s (owner)", r.Name, r.Name))
		}
	}
	for _, s := range add {
		if _, err := db.ExecContext(ctx, s); err != nil {
			return nil, fmt.Errorf("gen/store/%s/migrations.go: %w", r.Name, err)
		}
	}
	return append(append([]string(nil), released...), add...), nil
}

// firstRelease is a new resource's statements: the table in its final
// shape, money as two nullable columns, and the owner index.
func firstRelease(r rastrillo.Resource) []string {
	lines := []string{fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (", r.Name)}
	lines = append(lines, tableBody(r)...)
	lines = append(lines, ");")
	stmts := []string{strings.Join(lines, "\n")}
	if scoped(r) {
		stmts = append(stmts, fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s_owner ON %s (owner);", r.Name, r.Name))
	}
	return stmts
}

// appendedNotes is one op.generate.migration_appended line per
// statement in stmts beyond the released ones.
func appendedNotes(r rastrillo.Resource, released, stmts []string) []string {
	var out []string
	for _, s := range stmts[len(released):] {
		out = append(out, fmt.Sprintf(migrationAppended, r.Name, s, r.Name))
	}
	return out
}
