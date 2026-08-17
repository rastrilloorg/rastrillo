package rastrillo

import (
	"fmt"
	"html/template"
	"regexp"
	"strings"
)

// This file is the manifest vocabulary (design doc §3): the typed-Go
// canonical form of a Resource. TOML manifests are an optional
// serialization of the pure-data subset of these same structs — the
// generator lowers them into identical values, one pipeline, two
// spellings. Write TOML for a plain screen; drop to a .go manifest in
// the app's manifest/ package the moment you need a function value
// (a Column.Render — TOML cannot express a Go closure).
//
// A Resource generates the canonical screens (List → Show → Edit/New,
// plus the confirm-page delete flow) as real action files under gen/,
// each skipped when a hand-written file exists at the same computed
// path in actions/ — override-by-existence, the one mechanism (§2).
// The runtime half lives in the screens package; nothing here executes.

// StoreKind selects a Resource's storage shape (§5).
type StoreKind int

const (
	// Exclusive is the plain single-writer shape: ordinary rows in a
	// generated table, UPDATEs in place — titogo's and kass's shape,
	// and the default.
	Exclusive StoreKind = iota

	// Mergeable is the event-sourced shape: commands append immutable
	// events (rastrillo/eventlog), a pure fold derives the rows, and
	// merging other edges' streams is the platform's designed
	// `mergeable` contract. Deleting appends a tombstone, never a
	// DELETE.
	Mergeable
)

// Kind is a field's semantic type — it decides the SQLite column, the
// form control, and the list rendering in one place. The design doc
// names Text, Money, Meter and Blob; LongText, Bool, Time and Select
// are the richer kinds the blog and the surveyed apps actually needed.
type Kind int

const (
	// Text is a one-line string.
	Text Kind = iota
	// LongText is a multi-line string (a textarea).
	LongText
	// Bool is a checkbox.
	Bool
	// Time is an RFC3339 timestamp, stored as TEXT in UTC — formatted
	// in Go, never in a template (the family convention).
	Time
	// Money is an integer number of cents, never a float — CARLOS's
	// non-negotiable: "a float never touches a value a person will be
	// held to." Forms accept "12.34"; storage and arithmetic are int64.
	Money
	// Meter is a read-only gauge for lists — usually paired with a
	// Column.Render. Never a form field.
	Meter
	// Blob is bytes stored content-addressed in a blobs.Store; the row
	// holds only the Ref (hash, size, content type) as JSON (§5).
	Blob
	// Select is one value from a declared Options list.
	Select
)

// kindNames maps the TOML spellings to kinds, and back for errors.
var kindNames = map[string]Kind{
	"text": Text, "longtext": LongText, "bool": Bool, "time": Time,
	"money": Money, "meter": Meter, "blob": Blob, "select": Select,
}

// KindByName resolves a TOML kind spelling ("text", "money", ...).
func KindByName(name string) (Kind, bool) {
	k, ok := kindNames[name]
	return k, ok
}

func (k Kind) String() string {
	for name, kk := range kindNames {
		if kk == k {
			return name
		}
	}
	return fmt.Sprintf("Kind(%d)", int(k))
}

// GoName returns the rastrillo identifier for generated code
// ("rastrillo.LongText").
func (k Kind) GoName() string {
	switch k {
	case Text:
		return "Text"
	case LongText:
		return "LongText"
	case Bool:
		return "Bool"
	case Time:
		return "Time"
	case Money:
		return "Money"
	case Meter:
		return "Meter"
	case Blob:
		return "Blob"
	case Select:
		return "Select"
	}
	return k.String()
}

// SQLType is the SQLite column a kind stores as.
func (k Kind) SQLType() string {
	switch k {
	case Bool, Money, Meter:
		return "INTEGER NOT NULL DEFAULT 0"
	default:
		return "TEXT NOT NULL DEFAULT ''"
	}
}

// Column is one list-screen column.
type Column struct {
	Field string
	Kind  Kind

	// Render replaces the default cell rendering — the reason to drop
	// from TOML to a .go manifest. It receives the row (field names →
	// values, plus "ID") and returns trusted HTML: escape anything
	// user-authored yourself.
	Render func(row map[string]any) template.HTML
}

// List declares a Resource's list screen.
type List struct {
	Columns []Column
	// Search enables a LIKE search over the resource's Text and
	// LongText fields, as a GET round trip (zero-JS, §9).
	Search bool
	// Filter names Bool or Select form fields that get an exact-match
	// filter control.
	Filter []string
}

// Field is one form field.
type Field struct {
	Name     string
	Kind     Kind
	Required bool
	// Derived fields render read-only and are never part of any save's
	// column list — the display half of "a computed field" (§3).
	Derived bool
	// Options is Select's closed value list.
	Options []string
}

// Form declares the edit/new screens. Basics and Advanced generate two
// independent POST actions, each scoped to only its own fields — a
// basics save can never clobber an advanced setting, by construction
// (§3, titogo's named safety property).
type Form struct {
	Basics   []Field
	Advanced []Field
}

// Delete declares the delete flow: always its own confirm-page URL
// (GET <route>/{id}/delete) before the POST — §9's "destructive actions
// as their own confirm-page URL", which is also what §8's agent
// consent gate renders.
type Delete struct {
	// Confirm is the confirm page's sentence. Empty generates
	// "Delete this <singular name>? This cannot be undone." — via the
	// translation key resource.<name>.delete.confirm, so a catalog can
	// reword it per locale.
	Confirm string
}

// Resource is one manifest: a named, routed, stored, listed, edited
// thing. The canonical typed-Go form (§3).
type Resource struct {
	Name  string // snake_case; the table (or stream) name and the translation-key root
	Route string // e.g. "/admin/ticket_types"; {params} allowed
	Store StoreKind
	List  List
	Form  Form
	Delete Delete
}

// Fields returns Basics then Advanced.
func (r Resource) Fields() []Field {
	return append(append([]Field{}, r.Form.Basics...), r.Form.Advanced...)
}

// FieldByName finds a form field.
func (r Resource) FieldByName(name string) (Field, bool) {
	for _, f := range r.Fields() {
		if f.Name == name {
			return f, true
		}
	}
	return Field{}, false
}

var (
	resourceNameRe = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	fieldNameRe    = regexp.MustCompile(`^[A-Z][A-Za-z0-9]*$`)
)

// Validate checks the manifest's static shape — the same validation for
// both spellings, run by the generator and again by screens at first
// use, so a bad manifest fails loudly at generate time and can never
// limp into serving.
func (r Resource) Validate() error {
	if !resourceNameRe.MatchString(r.Name) {
		return fmt.Errorf("resource name %q must be snake_case ([a-z][a-z0-9_]*)", r.Name)
	}
	if !strings.HasPrefix(r.Route, "/") || (len(r.Route) > 1 && strings.HasSuffix(r.Route, "/")) {
		return fmt.Errorf("resource %s: route %q must start with / and not end with one", r.Name, r.Route)
	}
	if len(r.Form.Basics) == 0 {
		return fmt.Errorf("resource %s: Form.Basics must declare at least one field", r.Name)
	}

	seen := map[string]bool{}
	for _, f := range r.Fields() {
		if !fieldNameRe.MatchString(f.Name) {
			return fmt.Errorf("resource %s: field name %q must be an exported-style identifier", r.Name, f.Name)
		}
		if seen[f.Name] {
			return fmt.Errorf("resource %s: field %q declared twice", r.Name, f.Name)
		}
		seen[f.Name] = true
		if f.Kind == Meter {
			return fmt.Errorf("resource %s: field %q: Meter is a list-only kind, never a form field", r.Name, f.Name)
		}
		if f.Kind == Select && len(f.Options) == 0 {
			return fmt.Errorf("resource %s: Select field %q needs Options", r.Name, f.Name)
		}
		if f.Kind != Select && len(f.Options) > 0 {
			return fmt.Errorf("resource %s: field %q has Options but is not Select", r.Name, f.Name)
		}
	}

	for _, c := range r.List.Columns {
		if c.Field == "CreatedAt" || c.Field == "UpdatedAt" {
			continue // row metadata, always present
		}
		if _, ok := r.FieldByName(c.Field); !ok && c.Render == nil {
			return fmt.Errorf("resource %s: list column %q is not a form field, not CreatedAt/UpdatedAt, and has no Render — nothing could fill it", r.Name, c.Field)
		}
	}
	for _, name := range r.List.Filter {
		f, ok := r.FieldByName(name)
		if !ok {
			return fmt.Errorf("resource %s: filter %q is not a form field", r.Name, name)
		}
		if f.Kind != Bool && f.Kind != Select {
			return fmt.Errorf("resource %s: filter %q must be a Bool or Select field (an exact-match control needs a closed value set)", r.Name, name)
		}
	}
	return nil
}

// Migration returns the additive CREATE TABLE for an Exclusive
// resource. Mergeable resources have no table of their own — their
// storage is eventlog.Migrations.
func (r Resource) Migration() string {
	if r.Store != Exclusive {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "CREATE TABLE IF NOT EXISTS %s (\n  id INTEGER PRIMARY KEY", r.Name)
	for _, f := range r.Fields() {
		fmt.Fprintf(&b, ",\n  %s %s", SnakeCase(f.Name), f.Kind.SQLType())
	}
	b.WriteString(",\n  created_at TEXT NOT NULL,\n  updated_at TEXT NOT NULL\n);")
	return b.String()
}

// SnakeCase converts a field name ("MaxPerOrder") to its column name
// ("max_per_order").
func SnakeCase(name string) string {
	var b strings.Builder
	for i, r := range name {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				b.WriteByte('_')
			}
			b.WriteRune(r - 'A' + 'a')
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// TitleCase is the label fallback when no translation catalog carries a
// field's key: "MaxPerOrder" → "Max per order" (§10's title-cased
// fallback).
func TitleCase(name string) string {
	var words []string
	var cur strings.Builder
	for i, r := range name {
		if r >= 'A' && r <= 'Z' && i > 0 {
			words = append(words, cur.String())
			cur.Reset()
		}
		cur.WriteRune(r)
	}
	words = append(words, cur.String())
	for i, w := range words {
		if i == 0 {
			continue
		}
		words[i] = strings.ToLower(w)
	}
	return strings.Join(words, " ")
}
