// Package shape is the one model of a manifest resource the emitters
// share: its data columns, the names the stores give them, which
// clauses its list binds, and the import path of its store. The store,
// action and operation emitters each render Go from it, so a column one
// of them knows about cannot go missing from another. It lives apart
// from internal/generate so an emitter can be a package of its own
// without copying these rules, which is how two copies would start to
// disagree.
package shape

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/internal/opsnames"
)

// Column is one of a resource's data columns; schema.sql, queries.sql,
// migrations.go and the operations are all rendered from it so they
// cannot describe different tables.
type Column struct {
	Name string // declared name, e.g. "MaxPerOrder"
	SQL  string // SQLName(Name), e.g. "max_per_order"
	Kind rastrillo.Kind
}

// Columns returns r's data columns (excluding id/created_at/updated_at,
// which every table has unconditionally) as the union of its List
// columns and Form fields, in first-declaration order: List columns
// first, then Form basics, then Form advanced, skipping any name
// (compared via SQLName, so case-insensitively) already present.
func Columns(r rastrillo.Resource) []Column {
	var cols []Column
	seen := map[string]bool{}
	add := func(name string, kind rastrillo.Kind) {
		sql := SQLName(name)
		if seen[sql] {
			return
		}
		seen[sql] = true
		cols = append(cols, Column{Name: name, SQL: sql, Kind: kind})
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

// SQLName converts a declared identifier (PascalCase or camelCase, per
// Validate's identPattern) to a snake_case SQL column name: an
// underscore is inserted before each interior uppercase run (a run
// starting at index 0 gets none, since the name is already lowercase
// there once the whole thing is lowercased), then the whole string is
// lowercased. "MaxPerOrder" -> "max_per_order".
func SQLName(s string) string {
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

// PascalCase and SingularPascal are opsnames' rules, which the root
// package's Validate also reads; see that package for why it is a leaf.
func PascalCase(s string) string { return opsnames.Pascal(s) }

func SingularPascal(s string) string { return opsnames.Singular(s) }

// LowerFirst is a declared field name as a local's prefix: "Price" ->
// "price". Locals add a suffix (Text, Input, Code, Minor, Currency), so
// a field named Store or Page cannot shadow a local of the same name.
func LowerFirst(s string) string {
	return strings.ToLower(s[:1]) + s[1:]
}

// Scoped reports whether r declares per-user scoping, the one flag
// every emitter branches on. Validate already rejected any Scope other
// than "" and UserScoped.
func Scoped(r rastrillo.Resource) bool {
	return r.Scope == rastrillo.UserScoped
}

// SearchColumns returns r's List columns eligible for the search LIKE
// clause: text/textarea kinds only (a money column has nothing to
// substring-match).
func SearchColumns(r rastrillo.Resource) []Column {
	var cols []Column
	for _, c := range r.List.Columns {
		if c.Kind == rastrillo.Text || c.Kind == rastrillo.Textarea {
			cols = append(cols, Column{Name: c.Field, SQL: SQLName(c.Field), Kind: c.Kind})
		}
	}
	return cols
}

// SearchBind reports whether r's list queries take a search parameter:
// the manifest turned search on and there is a column to search. The
// stores emit the clause and the parameter together on this condition,
// so a caller that passes a search where there is none fails to compile
// rather than quietly matching everything.
func SearchBind(r rastrillo.Resource) bool {
	return r.List.Search && len(SearchColumns(r)) > 0
}

// FilterFields returns r's filter field names as an ordered union of
// bare Filter and Filters[].Field, in first-mention order with no
// duplicates.
func FilterFields(r rastrillo.Resource) []string {
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

// FilterVar is one declared filter as the generated code names it.
type FilterVar struct {
	Field string // declared name, e.g. "Title"
	Query string // SQLName(Field), the URL query key, e.g. "title"
	Var   string // "filterTitle"
	Param string // the store Params field name, "Filter"+Field
}

// FilterVars walks FilterFields(r), the deduped union of bare
// List.Filter and List.Filters[].Field, rather than r.List.Filter
// alone, so a field declared ONLY via List.Filters (with enumerable
// Values, no bare List.Filter entry) still gets its query param
// parsed, carried and bound to the store's filter_<col> arg; the store's
// WHERE clause already honours it either way, so the callers have to
// keep up. A field present in both collections appears here once.
func FilterVars(r rastrillo.Resource) []FilterVar {
	var out []FilterVar
	for _, f := range FilterFields(r) {
		out = append(out, FilterVar{
			Field: f,
			Query: SQLName(f),
			Var:   "filter" + f,
			Param: "Filter" + f,
		})
	}
	return out
}

// DeclaredFilter returns r's single List.Filters entry, if declared:
// Validate caps List.Filters at exactly one, so "declared" here always
// means exactly one or none. Field always matches one of FilterVars(r)'s
// own Field values, since FilterFields unions List.Filter and
// List.Filters[].Field. Values is its enumerable dropdown choices,
// already validated non-empty and deduped by Validate.
func DeclaredFilter(r rastrillo.Resource) (field string, values []string, ok bool) {
	if len(r.List.Filters) != 1 {
		return "", nil, false
	}
	return r.List.Filters[0].Field, r.List.Filters[0].Values, true
}

// CountBinds counts a resource's Count bind parameters. sqlc passes one
// bind bare and two or more in a Params struct, and the mergeable store
// copies that convention, so every caller of Count<Plural> switches on
// this number and the emitted signature and its call sites cannot
// drift.
func CountBinds(r rastrillo.Resource) int {
	binds := len(FilterVars(r))
	if SearchBind(r) {
		binds++
	}
	if Scoped(r) {
		binds++
	}
	return binds
}

// MoneyFields is fields' money fields.
func MoneyFields(fields []rastrillo.Field) []rastrillo.Field {
	var out []rastrillo.Field
	for _, f := range fields {
		if f.Kind == rastrillo.Money {
			out = append(out, f)
		}
	}
	return out
}

// FormFields is r's form fields, Basics then Advanced: the fields a
// create form posts and an edit form shows across its two groups.
func FormFields(r rastrillo.Resource) []rastrillo.Field {
	return append(append([]rastrillo.Field(nil), r.Form.Basics...), r.Form.Advanced...)
}

// StoreImport returns the store package's import alias and path for r:
// alias equals the package name sqlc.yaml told sqlc to generate
// (<name>store), written explicitly rather than relying on the compiler
// inferring it from the package clause, matching Router()'s own
// explicit-alias style.
func StoreImport(r rastrillo.Resource, module string) (alias, path string) {
	return r.Name + "store", module + "/gen/store/" + r.Name
}

// ModulePath reads the module directive from appRoot's go.mod.
// Hand-rolled rather than shared across packages, matching this
// codebase's own precedent (internal/manifest/goeval.go's
// readModulePath, cmd/rastrillo/modpath.go's modulePath): the module
// line is one fixed shape, and each package that needs it keeps its
// own unexported copy.
func ModulePath(appRoot string) (string, error) {
	f, err := os.Open(filepath.Join(appRoot, "go.mod"))
	if err != nil {
		return "", fmt.Errorf("read go.mod: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module ")), nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("go.mod: no module directive found")
}

// WrapComment word-wraps text into "// " comment lines no longer than
// width characters after the marker. The width is the caller's because
// each emitter's goldens pin their own.
func WrapComment(text string, width int) string {
	var lines []string
	line := ""
	for _, word := range strings.Fields(text) {
		switch {
		case line == "":
			line = word
		case len([]rune(line))+1+len([]rune(word)) <= width:
			line += " " + word
		default:
			lines = append(lines, line)
			line = word
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	var b strings.Builder
	for _, l := range lines {
		b.WriteString("// " + l + "\n")
	}
	return b.String()
}

// WriteFile writes content to path only when the file is missing or
// its current content differs, so re-generation is idempotent: no
// spurious mtime churn, no rewriting bytes that are already correct.
func WriteFile(path string, content []byte) error {
	if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, content) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o644)
}
