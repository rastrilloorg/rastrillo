// Package bootgen emits gen/boot.go: the Boot every app with manifest
// resources must call before gen.Router will build. Money upgrades
// have to run before an app serves, and nothing generated runs unless
// the app calls it, so Router takes the Booted only Boot returns.
package bootgen

import (
	"bytes"
	"errors"
	"fmt"
	"go/format"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"amadan.net/rastrillo/rastrillo"
)

// Appendix C.6 gen.boot.booted and gen.boot.doc, the comments an app
// author reads above Booted and Boot.
const (
	bootedDoc = "Booted proves Boot ran for this database. Router takes it so an app cannot serve without it."
	bootDoc   = "Boot upgrades this app's money data once and checks it, then returns the value Router needs. Call it after the schema is applied and before serving; a non-nil error must stop the start."
)

// MoneySchemaAdded is Appendix C.2's op.generate.money_schema_added,
// naming a resource whose events carry money.
const MoneySchemaAdded = "%s keeps money in events, so the next deploy renames the event log's payload column. Take a backup, stop every running copy, then start the new version. Never run old and new versions side by side. To go back, restore the backup."

// upgradeCall is how Emit tells a boot.go that renames the event log
// from one that does not.
const upgradeCall = "eventlog.UpgradeForMoney("

// step is one thing Boot does before it vouches for db, in order. Each
// is a statement list that returns Booted{} and the error on failure;
// its imports join the file's, each a path or "alias path".
type step struct {
	imports []string
	code    string
}

// Table is one exclusive store's money for the SQL upgrade: its table,
// its money fields by column name, and its [legacy] if declared. The
// caller derives Fields from the same column list the store emitter
// writes, so the columns Upgrade adds are the ones the queries name.
type Table struct {
	Name   string
	Fields []string
	Legacy *rastrillo.Legacy
}

// EventStore is a mergeable store: its resource name, the package Boot
// calls CheckReplay in, and whether its events carry money, which
// alone decides that the event log is renamed.
type EventStore struct {
	Name, Alias, Path string
	Money             bool
}

// steps lists what Boot runs, in order. The event log is renamed
// first: replay reads payload_v2 and the boundary the rename
// snapshots, and every Log opened before it would keep the old column.
// Every mergeable store's streams are replayed last, money or not, so
// an event from a newer writer stops the start before the app serves a
// request that would fail on it.
func steps(sql []Table, events []EventStore) []step {
	var ss []step
	if hasMoney(events) {
		ss = append(ss, step{
			imports: []string{"amadan.net/rastrillo/rastrillo/eventlog"},
			code:    "if err := " + upgradeCall + "ctx, db); err != nil {\nreturn Booted{}, err\n}",
		})
	}
	if len(sql) > 0 {
		ss = append(ss, sqlMoney(sql))
	}
	for _, e := range events {
		ss = append(ss, step{
			imports: []string{e.Alias + " " + e.Path},
			code:    "if err := " + e.Alias + ".CheckReplay(ctx, db); err != nil {\nreturn Booted{}, err\n}",
		})
	}
	return ss
}

// sqlMoney is the SQL money upgrade over every exclusive store with
// money, whether or not its table has anything to upgrade yet:
// Upgrade decides that from the columns it finds, and a store left out
// would let an old binary keep writing its legacy column, or leave a
// field added later with no columns to read.
func sqlMoney(sql []Table) step {
	s := step{imports: []string{"amadan.net/rastrillo/rastrillo/currency/sqlmoney"}}
	var b strings.Builder
	b.WriteString("if err := sqlmoney.Upgrade(ctx, db, []sqlmoney.Table{\n")
	for _, t := range sql {
		quoted := make([]string, len(t.Fields))
		for i, f := range t.Fields {
			quoted[i] = fmt.Sprintf("%q", f)
		}
		fmt.Fprintf(&b, "{Name: %q, Fields: []string{%s}", t.Name, strings.Join(quoted, ", "))
		if t.Legacy != nil {
			fmt.Fprintf(&b, ", Legacy: &currency.Legacy{Currency: %q, Scale: %d}", t.Legacy.Currency, *t.Legacy.Scale)
			s.imports = append(s.imports, "amadan.net/rastrillo/rastrillo/currency")
		}
		b.WriteString("},\n")
	}
	b.WriteString("}); err != nil {\nreturn Booted{}, err\n}")
	s.code = b.String()
	return s
}

// Emit writes genDir/boot.go when rs is non-empty and returns its path,
// changed or not, so pruning never mistakes it for stale. Without
// manifest resources the router takes no Booted and nothing is written.
// sql is the exclusive stores with money, in the order Boot upgrades
// them; events every mergeable store, in the order Boot replays them.
//
// notices is one MoneySchemaAdded line per event store when this write
// adds the event log's rename to a boot.go that lacked it: the next
// deploy is the stop-start one, and nothing else tells the app author.
// A boot.go that already renames says nothing, because that deploy has
// been announced. If money is removed and added back, the notice comes
// again though the column was renamed the first time: it then asks for
// a backup that was not needed, which is the safe way to be wrong.
func Emit(genDir string, rs []rastrillo.Resource, sql []Table, events []EventStore) (paths, notices []string, err error) {
	if len(rs) == 0 {
		return nil, nil, nil
	}
	src, err := render(steps(sql, events))
	if err != nil {
		return nil, nil, err
	}
	path := filepath.Join(genDir, "boot.go")
	// Only a missing file is "no boot.go": an unreadable one would
	// otherwise print the stop-start notice for an app already renamed.
	existing, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, nil, err
	}
	if bytes.Equal(existing, src) {
		return []string{path}, nil, nil
	}
	if !bytes.Contains(existing, []byte(upgradeCall)) {
		for _, e := range events {
			if e.Money {
				notices = append(notices, fmt.Sprintf(MoneySchemaAdded, e.Name))
			}
		}
	}
	if err := os.MkdirAll(genDir, 0o755); err != nil {
		return nil, nil, err
	}
	return []string{path}, notices, os.WriteFile(path, src, 0o644)
}

func hasMoney(events []EventStore) bool {
	for _, e := range events {
		if e.Money {
			return true
		}
	}
	return false
}

func render(ss []step) ([]byte, error) {
	imports := map[string]bool{"context": true, "database/sql": true}
	var body strings.Builder
	for _, s := range ss {
		for _, imp := range s.imports {
			imports[imp] = true
		}
		body.WriteString(s.code)
		body.WriteString("\n")
	}
	specs := make([]string, 0, len(imports))
	for imp := range imports {
		specs = append(specs, imp)
	}
	sort.Strings(specs)

	var b strings.Builder
	b.WriteString("// Code generated by rastrillo generate; DO NOT EDIT.\n\npackage gen\n\nimport (\n")
	// The standard library first, then a blank line and the rest, as
	// goimports groups them: gofmt sorts within a group but never
	// splits one. An aliased import is one of the app's own stores,
	// whose module path ("tickets") may have no dot, so it is put with
	// the rest by its alias, as the generated actions place it.
	var std, other []string
	for _, imp := range specs {
		alias, path, aliased := strings.Cut(imp, " ")
		switch {
		case aliased:
			other = append(other, alias+" \""+path+"\"")
		case strings.Contains(strings.Split(imp, "/")[0], "."):
			other = append(other, "\""+imp+"\"")
		default:
			std = append(std, "\""+imp+"\"")
		}
	}
	for i, group := range [][]string{std, other} {
		if i > 0 && len(group) > 0 {
			b.WriteString("\n")
		}
		for _, imp := range group {
			b.WriteString("\t" + imp + "\n")
		}
	}
	b.WriteString(")\n\n")
	b.WriteString(comment(bootedDoc))
	b.WriteString("type Booted struct{ db *sql.DB }\n\n")
	b.WriteString(comment(bootDoc))
	b.WriteString("func Boot(ctx context.Context, db *sql.DB) (Booted, error) {\n")
	b.WriteString(body.String())
	b.WriteString("\treturn Booted{db: db}, nil\n}\n")
	return format.Source([]byte(b.String()))
}

// comment wraps text as a // block at 72 columns.
func comment(text string) string {
	var b strings.Builder
	line := "//"
	for _, w := range strings.Fields(text) {
		if len(line)+1+len(w) > 72 && line != "//" {
			b.WriteString(line + "\n")
			line = "//"
		}
		line += " " + w
	}
	b.WriteString(line + "\n")
	return b.String()
}
