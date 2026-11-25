package migrate

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// Problem is one thing wrong with how a Set declares its tables.
type Problem struct {
	Table string
	// Message is one sentence that names what to fix.
	Message string
}

// CheckDeclarations applies s to an empty in-memory database and
// compares the tables that result with the declarations on s, so a
// table nobody declared, or a declaration naming nothing, is found
// without a real database. Go migrations run too, because the tables
// they create are tables the app will have.
func CheckDeclarations(ctx context.Context, set *Set) ([]Problem, error) {
	mem, err := Memory()
	if err != nil {
		return nil, err
	}
	defer mem.Close()
	if _, err := Apply(ctx, Pool(mem), set); err != nil {
		return nil, err
	}
	return checkDatabase(ctx, mem, set.Declarations())
}

// checkDatabase reads the structure of q and checks ds against it.
func checkDatabase(
	ctx context.Context, db Querier, ds []Data,
) ([]Problem, error) {
	snap, err := Read(ctx, db)
	if err != nil {
		return nil, err
	}
	virtual, withoutRowid, err := readTableKinds(ctx, db)
	if err != nil {
		return nil, err
	}
	return checkAgainst(snap, virtual, withoutRowid, ds), nil
}

// readTableKinds finds the virtual tables, whose shadow tables need no
// declaration, and the WITHOUT ROWID tables, the only ones on which
// "rowid" is not a column to key on. Neither shows in a Snapshot.
func readTableKinds(ctx context.Context, db Querier) (
	virtual, withoutRowid map[string]bool, err error,
) {
	rows, err := db.QueryContext(ctx,
		`SELECT name, COALESCE(sql, '') FROM sqlite_master
		  WHERE type = 'table'`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	virtual, withoutRowid = map[string]bool{}, map[string]bool{}
	for rows.Next() {
		var name, ddl string
		if err := rows.Scan(&name, &ddl); err != nil {
			return nil, nil, err
		}
		upper := strings.ToUpper(strings.TrimSpace(ddl))
		if strings.HasPrefix(upper, "CREATE VIRTUAL TABLE") {
			virtual[name] = true
		}
		if strings.Contains(upper, "WITHOUT ROWID") {
			withoutRowid[name] = true
		}
	}
	return virtual, withoutRowid, rows.Err()
}

var fts5Shadows = []string{"_data", "_idx", "_content", "_docsize", "_config"}

// isFrameworkTable is true for tables no app declares.
func isFrameworkTable(name string) bool {
	return name == "rastrillo_migrations" ||
		strings.HasPrefix(name, "sqlite_") ||
		strings.HasPrefix(name, "_litestream_")
}

// isShadow is true for the tables FTS5 keeps behind a declared
// virtual table.
func isShadow(name string, virtual, declared map[string]bool) bool {
	for _, suffix := range fts5Shadows {
		base, found := strings.CutSuffix(name, suffix)
		if found && virtual[base] && declared[base] {
			return true
		}
	}
	return false
}

// checkAgainst compares snap with ds. It is shared by
// CheckDeclarations and Apply, so both read the same rules.
func checkAgainst(
	snap Snapshot, virtual, withoutRowid map[string]bool, ds []Data,
) []Problem {
	tables := map[string]Table{}
	for _, table := range snap.Tables {
		tables[table.Name] = table
	}
	byName := map[string][]Data{}
	declared := map[string]bool{}
	for _, decl := range ds {
		byName[decl.Name] = append(byName[decl.Name], decl)
		declared[decl.Name] = true
	}

	var found []Problem
	report := func(table, message string) {
		found = append(found, Problem{table, message})
	}

	found = append(found, undeclared(snap, virtual, declared)...)
	for name, decls := range byName {
		if len(decls) > 1 {
			report(name, "declared by "+namespaces(decls))
		}
		table, exists := tables[name]
		if !exists {
			report(name, "declared but no migration creates it")
			continue
		}
		for _, decl := range decls {
			for _, message := range checkOne(
				decl, table, withoutRowid, declared) {
				report(name, message)
			}
		}
	}
	return sortedUnique(found)
}

// undeclared reports the tables nobody declared.
func undeclared(snap Snapshot, virtual, declared map[string]bool) []Problem {
	var found []Problem
	for _, table := range snap.Tables {
		if isFrameworkTable(table.Name) || declared[table.Name] ||
			isShadow(table.Name, virtual, declared) {
			continue
		}
		found = append(found, Problem{table.Name, "no declaration; " +
			"add one with Declare next to the migration that creates it"})
	}
	return found
}

// namespaces lists who declared a table, in the order they did.
func namespaces(decls []Data) string {
	names := make([]string, len(decls))
	for at, decl := range decls {
		names[at] = decl.Namespace
		if names[at] == "" {
			names[at] = "(no namespace)"
		}
	}
	if len(names) == 1 {
		return names[0]
	}
	last := len(names) - 1
	return strings.Join(names[:last], ", ") + " and " + names[last]
}

// checkOne returns the messages for one declaration of a table that
// exists.
func checkOne(
	decl Data, table Table, withoutRowid, declared map[string]bool,
) []string {
	has := map[string]bool{}
	for _, column := range table.Columns {
		has[column.Name] = true
	}
	var out []string
	missing := func(format, column string) {
		if !has[column] {
			out = append(out, fmt.Sprintf(format, column))
		}
	}
	for _, column := range decl.Key {
		if column == "rowid" && !withoutRowid[table.Name] {
			continue
		}
		missing("key column %s does not exist", column)
	}
	if decl.Owner.Column != "" {
		missing("column %s does not exist", decl.Owner.Column)
	}
	if parent := decl.Owner.Parent; parent != "" && !declared[parent] {
		out = append(out, fmt.Sprintf(
			"owner parent %s is not declared", parent))
	}
	for _, field := range decl.Fields {
		missing("column %s does not exist", field.Name)
	}
	for _, column := range decl.Recorded {
		missing("column %s does not exist", column)
	}
	out = append(out, checkComplete(decl)...)
	out = append(out, checkRecorded(decl)...)
	if decl.Kind == Personal {
		out = append(out, checkPersonal(decl)...)
	}
	if decl.Erase == EraseKeep && decl.KeepReason == "" {
		out = append(out, "Keep needs a reason")
	}
	return out
}

// checkComplete refuses a declaration that capture or erasure could
// not act on: rows need a key, a table a kind, and an owner a column
// and a way to read it.
func checkComplete(decl Data) []string {
	var out []string
	if len(decl.Key) == 0 {
		out = append(out, "declaration needs a Key")
	}
	if decl.Kind < Personal || decl.Kind > System {
		out = append(out, "declaration needs a Kind: Personal, Shared "+
			"or System")
	}
	owner := decl.Owner
	switch {
	case owner == Owner{}:
	case owner.Column == "" && owner.Parent != "":
		out = append(out, fmt.Sprintf(
			"owner parent %s needs a foreign key column", owner.Parent))
	case owner.Column == "":
		out = append(out, "owner has no column")
	case owner.Parent == "" && !identifies(owner.Holds):
		out = append(out, fmt.Sprintf("owner column %s must hold a "+
			"subject, an address or a user ID", owner.Column))
	}
	return out
}

// identifies is true for the kinds of value privacy.Identity can match
// to a person; free text names anyone, so it owns nothing.
func identifies(holds Holds) bool {
	return holds == HoldsSubject || holds == HoldsAddress ||
		holds == HoldsUserID
}

// checkRecorded refuses a recorded column that is the owner or another
// personal column, since the audit log would then keep what erasure
// removes.
func checkRecorded(decl Data) []string {
	personal := map[string]bool{}
	if decl.Owner.Column != "" {
		personal[decl.Owner.Column] = true
	}
	for _, field := range decl.Fields {
		personal[field.Name] = true
	}
	var out []string
	for _, column := range decl.Recorded {
		if personal[column] {
			out = append(out, fmt.Sprintf(
				"recorded column %s holds personal data", column))
		}
	}
	return out
}

// checkPersonal holds a personal table to having a way to erase it
// and, for anything that deletes or clears values, a way to find
// whose they are.
func checkPersonal(decl Data) []string {
	var out []string
	if decl.Erase == 0 {
		out = append(out, "personal table needs an Erase")
	}
	if decl.Owner != (Owner{}) {
		return out
	}
	needsOwner := decl.Erase == EraseRow
	for _, field := range decl.Fields {
		if field.Erase == EraseNull || field.Erase == EraseTombstone {
			needsOwner = true
		}
	}
	if needsOwner {
		out = append(out, "erasing rows needs an Owner")
	}
	return out
}

// sortedUnique orders problems by table then message, so output is
// stable, and drops repeats.
func sortedUnique(found []Problem) []Problem {
	sort.Slice(found, func(first, second int) bool {
		one, two := found[first], found[second]
		if one.Table != two.Table {
			return one.Table < two.Table
		}
		return one.Message < two.Message
	})
	var out []Problem
	for _, problem := range found {
		if len(out) == 0 || out[len(out)-1] != problem {
			out = append(out, problem)
		}
	}
	return out
}

// declarationProblems is what Apply reports on the real database. A
// check that cannot read becomes a problem, because a boot must not
// fail over its own diagnostics.
func declarationProblems(
	ctx context.Context, db Querier, set *Set,
) []Problem {
	problems, err := checkDatabase(ctx, db, set.Declarations())
	if err != nil {
		return []Problem{{Message: "cannot check declarations: " +
			err.Error()}}
	}
	return problems
}
