package rastrillo

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"amadan.net/rastrillo/rastrillo/currency"
	"amadan.net/rastrillo/rastrillo/internal/manifesterr"
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

// ScopeKind categorizes who a resource's rows belong to.
type ScopeKind string

const (
	// Unscoped rows belong to the app: one shared table, no owner
	// column — the right shape for admin-style resources.
	Unscoped ScopeKind = ""

	// UserScoped rows belong to whoever created them: the generated
	// store adds an `owner` column holding the session Subject
	// (sessions.Session.Subject — a TEXT value, so keymail's email
	// subjects and password's numeric-string subjects both fit), every
	// generated query filters by it, and a row that isn't yours
	// answers 404 — the scope package's discipline, declared instead
	// of hand-written. Generated actions read the subject via
	// sessions.Current, so scoped routes must mount behind
	// sessions.Require / sessions.Middleware / auth.RequireSession.
	UserScoped ScopeKind = "user"
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
	Scope ScopeKind `json:"scope,omitempty" toml:"scope"` // omitempty: an unscoped resource's manifest.json stays byte-identical to pre-Scope artifacts
	List  List      `json:"list" toml:"list"`
	Form  Form      `json:"form" toml:"form"`

	// Currency is the default for new records. It decides nothing about
	// rows already stored, which carry their own code; omitempty keeps a
	// resource without money byte-identical in gen/manifest.json.
	Currency string `json:"currency,omitempty" toml:"currency"`

	// Legacy is separate from Currency (decision 10) because rows stored
	// before money had a currency were written in whatever the app always
	// meant, which may be neither today's default nor a code still issued.
	Legacy *Legacy `json:"legacy,omitempty" toml:"legacy"`
}

// Legacy is a manifest's [legacy] table: what the integers stored before
// money had a currency mean, so the boot upgrade can rescale them once.
type Legacy struct {
	Currency string `json:"currency" toml:"currency"`
	// Scale is a pointer because 0 is a real answer (the old integers
	// were whole units) and omission must not be read as it: a table
	// naming only USD would otherwise turn every stored 1250 cents into
	// 125000 cents.
	Scale *int `json:"scale" toml:"scale"`
}

// msgCurrencyWithoutMoney and msgLegacyWithoutMoney are the messages
// for money settings on a resource with nothing to apply them to. One
// constant each so the copy review's approved text lands in one place;
// internal/manifest.Load prefixes the file.
const (
	msgCurrencyWithoutMoney = `currency is set, but no column or field is money; give one kind = "money", or remove currency`
	msgLegacyWithoutMoney   = `[legacy] is set, but no column or field is money; give one kind = "money", or remove [legacy]`
)

// maxLegacyScale bounds [legacy] scale: currency.Rescale multiplies or
// divides by ten once per digit of difference, and 10^18 is the largest
// power of ten an int64 holds, so a larger scale could only ever be
// inexact or overflow.
const maxLegacyScale = 18

// HasMoney reports whether any list column or form field is money, and
// so whether the resource needs a default currency.
func (r *Resource) HasMoney() bool {
	for _, c := range r.List.Columns {
		if c.Kind == Money {
			return true
		}
	}
	for _, f := range r.Form.Basics {
		if f.Kind == Money {
			return true
		}
	}
	for _, f := range r.Form.Advanced {
		if f.Kind == Money {
			return true
		}
	}
	return false
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
// It normalizes zero values for Kind and Store, and the default and
// [legacy] currency codes, in place.
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

	if r.Store != Exclusive && r.Store != Mergeable {
		return fmt.Errorf("store: unknown value %q", r.Store)
	}

	if r.Scope != Unscoped && r.Scope != UserScoped {
		return fmt.Errorf("scope: unknown value %q", r.Scope)
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
	moneyColumns := make(map[string]bool)
	for _, col := range r.List.Columns {
		columnFields[col.Field] = true
		moneyColumns[col.Field] = col.Kind == Money
	}

	// A filter is one exact value from the URL, compared with the
	// column. An amount has no such value now: its minor units mean
	// nothing without its currency, and the text it shows depends on the
	// reader's language. Generated, the filter named the column the money
	// upgrade replaces, and every list query failed.
	for _, f := range r.List.Filter {
		if !columnFields[f] {
			return fmt.Errorf("filter: %q is not a declared column", f)
		}
		if moneyColumns[f] {
			return fmt.Errorf(msgMoneyFilter, f)
		}
	}

	if len(r.List.Filters) > 1 {
		return fmt.Errorf("filters: at most one filter definition allowed")
	}
	for _, flt := range r.List.Filters {
		if !columnFields[flt.Field] {
			return fmt.Errorf("filter: %q is not a declared column", flt.Field)
		}
		if moneyColumns[flt.Field] {
			return fmt.Errorf(msgMoneyFilter, flt.Field)
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

	if err := r.checkGeneratedNames(); err != nil {
		return err
	}
	return r.validateMoney()
}

// Drafts for the copy review (copy-review/final): a filter on money,
// and a declared field whose name the generator already makes for
// something else.
const (
	msgMoneyFilter       = "filter: %q is a money field, and a list cannot filter on money; remove the filter"
	msgMoneyNameTaken    = "name: %q is also a name the money field %q generates; rename one of them"
	msgEventVersionTaken = "name: %q is the name generated stores give the event version; rename the field"
	msgNowTaken          = "name: %q is the name generated stores give the time of a save; rename the field"
)

// checkGeneratedNames refuses a field whose name the generator already
// makes for something else, which the duplicate checks above cannot see
// because they compare only declared names. Two namespaces matter:
//   - columns, and the Go fields sqlc derives from them: a money field
//     Price becomes price_minor and price_currency (PriceMinor and
//     PriceCurrency), a text field Title becomes title;
//   - a mergeable store's event keys: a money field's amount under its
//     own key, price, and its currency under price_currency; a text
//     field under its own key.
//
// A field declared as PriceCurrency, text or money, beside a money field
// Price is emitted twice in one of them, and the generated store does
// not compile. Both are checked for every store kind, so changing the
// kind later never invalidates a manifest. Declared names have no
// underscore, so a collision is always one field's own name against
// another's suffixed one. EventVersion is a mergeable store's
// event-version member, and Now is the save's timestamp every Create
// and Update Params struct ends in; both are refused like Owner, for
// every store kind, since a field of either name sat beside the
// generated member and the store did not compile.
func (r *Resource) checkGeneratedNames() error {
	type field struct {
		name string
		kind Kind
	}
	// First declaration wins, as internal/generate's columns does: a
	// list column and a form field of the same name are one field.
	var fields []field
	seen := map[string]bool{}
	add := func(name string, kind Kind) {
		if sql := identSQLName(name); !seen[sql] {
			seen[sql] = true
			fields = append(fields, field{name, kind})
		}
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
	// own: a generated name → the field whose own name it is.
	// suffixed: a generated name → the money field that adds a suffix.
	own := map[string]string{}
	suffixed := map[string]string{}
	for _, f := range fields {
		sql := identSQLName(f.name)
		if f.kind != Money {
			if sql == "event_version" {
				return fmt.Errorf(msgEventVersionTaken, f.name)
			}
			if sql == "now" {
				return fmt.Errorf(msgNowTaken, f.name)
			}
			own["column "+sql], own["key "+sql] = f.name, f.name
			continue
		}
		own["key "+sql] = f.name
		suffixed["column "+sql+"_minor"] = f.name
		suffixed["column "+sql+"_currency"] = f.name
		suffixed["key "+sql+"_currency"] = f.name
	}
	// Report in declaration order, so the same manifest always names
	// the same pair.
	for _, f := range fields {
		for _, ns := range []string{"column ", "key "} {
			name := ns + identSQLName(f.name)
			if money, ok := suffixed[name]; ok && own[name] == f.name {
				return fmt.Errorf(msgMoneyNameTaken, f.name, money)
			}
		}
	}
	return nil
}

// validateMoney checks the default currency and [legacy], rewriting both
// codes to canonical form so every emitter reads the same spelling. Its
// messages are Appendix C.2's without the leading "manifest/%s.toml",
// because internal/manifest.Load prefixes each Validate error with the
// file it came from, which for a Go manifest is not a .toml at all.
// The ineligible-default warning is not here: it must not stop
// generating, so cmd/rastrillo prints it after loading.
func (r *Resource) validateMoney() error {
	// Either setting without a money field is refused rather than
	// ignored: silently accepted, a currency on a resource whose Price
	// was never given kind = "money" generates a text column and nobody
	// is told, and a [legacy] table outliving its field promises an
	// upgrade that never runs.
	hasMoney := r.HasMoney()
	if r.Currency != "" {
		if !hasMoney {
			return errors.New(msgCurrencyWithoutMoney)
		}
		code, err := currency.Normalize(r.Currency)
		if err != nil {
			return fmt.Errorf("currency %q: %v", r.Currency, err)
		}
		r.Currency = code
	} else if hasMoney {
		return manifesterr.ErrNoCurrency
	}

	if r.Legacy == nil {
		return nil
	}
	if !hasMoney {
		return errors.New(msgLegacyWithoutMoney)
	}
	if r.Legacy.Currency == "" {
		return errors.New("[legacy] needs currency, the currency the old integers were in")
	}
	// Any stored code is accepted, eligible or not: the old rows were
	// written in whatever the app meant then, withdrawn currencies too.
	code, err := currency.Normalize(r.Legacy.Currency)
	if err != nil {
		return fmt.Errorf("[legacy] currency %q: %v", r.Legacy.Currency, err)
	}
	r.Legacy.Currency = code
	if r.Legacy.Scale == nil {
		return errors.New("[legacy] needs scale, the number of decimal places the old integers carry (2 for cents, 0 for whole units)")
	}
	if s := *r.Legacy.Scale; s < 0 || s > maxLegacyScale {
		return fmt.Errorf("[legacy] scale %d must be from 0 to 18", s)
	}
	return nil
}

// isReservedColumnName reports whether name collides case-insensitively
// with one of the fixed columns every generated store table carries
// unconditionally (id, created_at, updated_at) — or with owner, the
// column a scope = "user" store emits. Owner is reserved even for an
// unscoped resource, deliberately: flipping scope on later must never
// invalidate a manifest that was valid before.
func isReservedColumnName(name string) bool {
	switch strings.ToLower(name) {
	case "id", "createdat", "updatedat", "owner":
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
