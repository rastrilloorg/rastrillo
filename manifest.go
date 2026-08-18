package rastrillo

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

// RenderFunc is how a generated action hands a page to the app's own
// template tree — the seam generated code needs because it cannot
// call an app-private helper (a hand-rolled blog.Render, say).
// Ctx.Render carries it; the app's ctx factory sets it, and a
// generated action nil-checks it before use. page is always one of
// "<resource>/list", "<resource>/show" or "<resource>/form" —
// internal/generate's action emitter documents and pins the exact
// contract (see actions.go).
type RenderFunc func(ctx *Ctx, w http.ResponseWriter, page string, status int, data any)

var (
	namePattern        = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	segmentPattern     = regexp.MustCompile(`^[a-z0-9_-]+$`)
	identPattern       = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*$`)
	filterValuePattern = regexp.MustCompile(`^[a-z0-9_-]+$`)
)

// Kind categorizes the input type for a column or form field.
type Kind string

const (
	Text     Kind = "text"
	Textarea Kind = "textarea"
	Money    Kind = "money"
)

// StoreKind categorizes how a resource's data is stored and synchronized.
type StoreKind string

const (
	Exclusive StoreKind = "exclusive"
	Mergeable StoreKind = "mergeable"
)

// Filter specifies a column and a set of values for filtering a list.
type Filter struct {
	Field  string   `json:"field" toml:"field"`
	Values []string `json:"values" toml:"values"`
}

// Resource is one manifest: the §9 sugar a route opts into. Its JSON
// encoding (the struct tags here and on the types it embeds) is the
// generator's stable artifact — gen/manifest.json — consumed by any
// renderer; evolution is additive only. It describes a CRUD interface
// for a data entity.
type Resource struct {
	Name  string    `json:"name" toml:"name"`
	Route string    `json:"route" toml:"route"`
	Store StoreKind `json:"store" toml:"store"`
	List  List      `json:"list" toml:"list"`
	Form  Form      `json:"form" toml:"form"`
}

// List describes the table view for a resource.
type List struct {
	Columns []Column `json:"columns" toml:"columns"`
	Search  bool     `json:"search" toml:"search"`
	Filter  []string `json:"filter" toml:"filter"` // superseded by Filters; still validated, generates the WHERE clause but no control.
	Filters []Filter `json:"filters" toml:"filters"`
}

// Column describes a column in a resource list.
type Column struct {
	Field string `json:"field" toml:"field"`
	Kind  Kind   `json:"kind" toml:"kind"` // zero value means Text
}

// Form describes the form views for creating and editing a resource.
type Form struct {
	Basics   []Field `json:"basics" toml:"basics"`
	Advanced []Field `json:"advanced" toml:"advanced"`
}

// Field describes an input field in a form.
type Field struct {
	Name     string `json:"name" toml:"name"`
	Kind     Kind   `json:"kind" toml:"kind"` // zero value means Text
	Required bool   `json:"required" toml:"required"`
}

// Validate checks the resource declaration for consistency and validity.
// It normalizes zero values for Kind and Store in place.
func (r *Resource) Validate() error {
	// Zero values normalize: empty Kind → Text, empty Store → Exclusive
	if r.Store == "" {
		r.Store = Exclusive
	}
	for i := range r.List.Columns {
		if r.List.Columns[i].Kind == "" {
			r.List.Columns[i].Kind = Text
		}
	}
	for i := range r.Form.Basics {
		if r.Form.Basics[i].Kind == "" {
			r.Form.Basics[i].Kind = Text
		}
	}
	for i := range r.Form.Advanced {
		if r.Form.Advanced[i].Kind == "" {
			r.Form.Advanced[i].Kind = Text
		}
	}

	if r.Name == "" {
		return fmt.Errorf("name: must not be empty")
	}
	if !namePattern.MatchString(r.Name) {
		return fmt.Errorf("name: must be snake_case")
	}

	if r.Route == "" {
		return fmt.Errorf("route: must not be empty")
	}
	if !strings.HasPrefix(r.Route, "/") {
		return fmt.Errorf("route: must start with /")
	}
	if strings.HasSuffix(r.Route, "/") && r.Route != "/" {
		return fmt.Errorf("route: must not have trailing slash")
	}
	// Route segments must be either {param} or [a-z0-9_-]+
	segments := strings.Split(strings.TrimPrefix(r.Route, "/"), "/")
	for _, seg := range segments {
		if seg == "" {
			continue
		}
		if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") {
			continue
		}
		if !segmentPattern.MatchString(seg) {
			return fmt.Errorf("route: invalid segment %q", seg)
		}
	}

	if r.Store == Mergeable {
		return fmt.Errorf("store: mergeable is not yet built")
	}
	if r.Store != Exclusive && r.Store != Mergeable {
		return fmt.Errorf("store: unknown value %q", r.Store)
	}

	for _, col := range r.List.Columns {
		if col.Kind != Text && col.Kind != Textarea && col.Kind != Money {
			return fmt.Errorf("kind: unknown value %q", col.Kind)
		}
	}
	for _, fld := range r.Form.Basics {
		if fld.Kind != Text && fld.Kind != Textarea && fld.Kind != Money {
			return fmt.Errorf("kind: unknown value %q", fld.Kind)
		}
	}
	for _, fld := range r.Form.Advanced {
		if fld.Kind != Text && fld.Kind != Textarea && fld.Kind != Money {
			return fmt.Errorf("kind: unknown value %q", fld.Kind)
		}
	}

	if len(r.List.Columns) == 0 && len(r.Form.Basics) == 0 && len(r.Form.Advanced) == 0 {
		return fmt.Errorf("declaration: must have at least one list column or form field")
	}

	columnFields := make(map[string]bool)
	for _, col := range r.List.Columns {
		columnFields[col.Field] = true
	}

	for _, f := range r.List.Filter {
		if !columnFields[f] {
			return fmt.Errorf("filter: %q is not a declared column", f)
		}
	}

	if len(r.List.Filters) > 1 {
		return fmt.Errorf("filters: at most one filter definition allowed")
	}
	for _, flt := range r.List.Filters {
		if !columnFields[flt.Field] {
			return fmt.Errorf("filter: %q is not a declared column", flt.Field)
		}
		if len(flt.Values) == 0 {
			return fmt.Errorf("filter values: must not be empty")
		}
		valueSet := make(map[string]bool)
		for _, v := range flt.Values {
			if !filterValuePattern.MatchString(v) {
				return fmt.Errorf("filter values: %q is invalid (must match ^[a-z0-9_-]+$)", v)
			}
			if valueSet[v] {
				return fmt.Errorf("filter values: %q is duplicated", v)
			}
			valueSet[v] = true
		}
	}

	// Field and column names must be valid identifiers
	for _, col := range r.List.Columns {
		if !identPattern.MatchString(col.Field) {
			return fmt.Errorf("name: %q is not a valid identifier", col.Field)
		}
	}
	for _, fld := range r.Form.Basics {
		if !identPattern.MatchString(fld.Name) {
			return fmt.Errorf("name: %q is not a valid identifier", fld.Name)
		}
	}
	for _, fld := range r.Form.Advanced {
		if !identPattern.MatchString(fld.Name) {
			return fmt.Errorf("name: %q is not a valid identifier", fld.Name)
		}
	}

	// Field and column names must not collide (case-insensitively) with
	// the fixed columns every generated store adds unconditionally (id,
	// created_at, updated_at — see internal/generate/store.go's
	// schemaSQL): a manifest field literally named Id or CreatedAt would
	// silently double-declare that column in the generated CREATE TABLE.
	for _, col := range r.List.Columns {
		if isReservedColumnName(col.Field) {
			return fmt.Errorf("name: %q is reserved (collides with a fixed column every store emits)", col.Field)
		}
	}
	for _, fld := range r.Form.Basics {
		if isReservedColumnName(fld.Name) {
			return fmt.Errorf("name: %q is reserved (collides with a fixed column every store emits)", fld.Name)
		}
	}
	for _, fld := range r.Form.Advanced {
		if isReservedColumnName(fld.Name) {
			return fmt.Errorf("name: %q is reserved (collides with a fixed column every store emits)", fld.Name)
		}
	}

	// Field and column names must also be the canonical spelling sqlc's
	// own generated Go field name would derive back to (see
	// isCanonicalIdent's doc for why identPattern alone isn't enough —
	// "title" and "IPAddress" both match identPattern but neither
	// round-trips). Checked after the reserved-name pass above so a name
	// that fails BOTH checks (e.g. "updatedat") still reports as
	// reserved, matching this function's existing precedence rather than
	// surfacing a second, unrelated complaint about it first.
	for _, col := range r.List.Columns {
		if !isCanonicalIdent(col.Field) {
			return fmt.Errorf("name: %q is not a canonical sqlc field spelling; use %q instead", col.Field, canonicalIdent(col.Field))
		}
	}
	for _, fld := range r.Form.Basics {
		if !isCanonicalIdent(fld.Name) {
			return fmt.Errorf("name: %q is not a canonical sqlc field spelling; use %q instead", fld.Name, canonicalIdent(fld.Name))
		}
	}
	for _, fld := range r.Form.Advanced {
		if !isCanonicalIdent(fld.Name) {
			return fmt.Errorf("name: %q is not a canonical sqlc field spelling; use %q instead", fld.Name, canonicalIdent(fld.Name))
		}
	}

	// Form fields (Basics + Advanced combined) must have no case-insensitive duplicates;
	// columns may repeat as form fields, but the form must not have duplicates.
	formFieldsLower := make(map[string]bool)
	for _, fld := range r.Form.Basics {
		lower := strings.ToLower(fld.Name)
		if formFieldsLower[lower] {
			return fmt.Errorf("name: duplicate %q (case-insensitive)", fld.Name)
		}
		formFieldsLower[lower] = true
	}
	for _, fld := range r.Form.Advanced {
		lower := strings.ToLower(fld.Name)
		if formFieldsLower[lower] {
			return fmt.Errorf("name: duplicate %q (case-insensitive)", fld.Name)
		}
		formFieldsLower[lower] = true
	}

	// Columns must have no case-insensitive duplicates among themselves
	columnFieldsLower := make(map[string]bool)
	for _, col := range r.List.Columns {
		lower := strings.ToLower(col.Field)
		if columnFieldsLower[lower] {
			return fmt.Errorf("name: duplicate %q (case-insensitive)", col.Field)
		}
		columnFieldsLower[lower] = true
	}

	return nil
}

// isReservedColumnName reports whether name collides case-insensitively
// with one of the fixed columns every generated store table carries
// unconditionally (id, created_at, updated_at).
func isReservedColumnName(name string) bool {
	switch strings.ToLower(name) {
	case "id", "createdat", "updatedat":
		return true
	default:
		return false
	}
}

// identSQLName and identPascalCase are a deliberate duplicate of
// internal/generate/store.go's sqlName and pascalCase, byte-for-byte
// the same algorithm. They cannot be shared by import: internal/generate
// imports this package (to build rastrillo.Resource-shaped output), so
// this package importing internal/generate back would cycle, and
// internal/generate is unexported to every consumer outside this
// module besides. Duplication is the least-coupling option available —
// the alternative (a third shared package under internal/ that both
// import) would add a whole new package for five lines, and neither
// side of the pair changes without deliberate, rare thought (sqlc's own
// column-naming convention isn't going anywhere). If store.go's
// algorithm ever changes, this copy must change with it in the same
// commit — TestValidateRejections' canonical-ident cases and
// store_test.go's sqlName/pascalCase cases are the tripwire that would
// fire if the two drift.
func identSQLName(s string) string {
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

func identPascalCase(s string) string {
	parts := strings.Split(s, "_")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, "")
}

// canonicalIdent is the spelling sqlc's own generated Go field name
// would derive name back to: the schema column is identSQLName(name),
// and sqlc pascal-cases that column name for the Go struct field, i.e.
// identPascalCase(identSQLName(name)). A declared field/column name
// that isn't already spelled this way (e.g. "title", whose canonical
// form is "Title"; or "IPAddress", whose consecutive-capitals collapse
// under identSQLName to "ipaddress" and pascal-case back to
// "Ipaddress", not "IPAddress") generates actions that reference a
// struct field sqlc never actually emits — a compile error in gen/,
// not a validation error at manifest-load time, unless Validate
// catches it first here.
func canonicalIdent(name string) string {
	return identPascalCase(identSQLName(name))
}

// isCanonicalIdent reports whether name is already its own canonical
// form — see canonicalIdent's doc.
func isCanonicalIdent(name string) bool {
	return canonicalIdent(name) == name
}
