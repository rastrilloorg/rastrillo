// Package opsgen emits gen/ops/<resource>: one operation per action
// (List, Get, Create, Update, Delete), called by the page door and the
// API door alike so a value one door accepts the other accepts too.
// Each package is a types.go holding the result and input types, the
// pure validators and the descriptors, plus one file per operation,
// which a hand-written file of the same name under ops/<resource>/
// replaces (Task 17); types.go is the contract and always stays.
//
// The emitter reads the resource through shape, the model the store
// and action emitters read, so the row the operation maps is the row
// the store returns, and the Count and List calls take the binds the
// store declares. The golden files under testdata/ops are the contract
// for what is emitted.
package opsgen

import (
	"fmt"
	"go/format"
	"path/filepath"
	"strings"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/form"
	"amadan.net/rastrillo/rastrillo/internal/generate/shape"
	"amadan.net/rastrillo/rastrillo/internal/opsnames"
	"amadan.net/rastrillo/rastrillo/internal/wirename"
)

// commentWidth is the text width the goldens pin, after the "// ".
const commentWidth = 69

// field is one form field as the operations see it: the input type it
// decodes into, its wire name and the message its required check gives.
type field struct {
	rastrillo.Field
	Wire     string // wirename.Name(Name): the JSON tag and the schema property
	Human    string // form.Humanize(Name): the page's own words for it
	Money    bool
	Optional bool // Nullable money: null clears it
	Textarea bool
}

// resource is everything the files share, computed once.
type resource struct {
	rastrillo.Resource
	Pkg        string // ops_<name>
	Singular   string // TicketType
	Plural     string // TicketTypes
	Alias      string // ticket_typesstore
	Path       string // <module>/gen/store/ticket_types
	Scoped     bool
	Mergeable  bool
	Cols       []shape.Column
	Fields     []field
	Filters    []shape.FilterVar
	SearchBind bool
	HumanOne   string // "ticket type", for sentences
	HumanMany  string // "ticket types"
	Article    string // "a" or "an" before HumanOne
}

func describe(r rastrillo.Resource, module string) resource {
	alias, path := shape.StoreImport(r, module)
	one := strings.ToLower(form.Humanize(strings.TrimSuffix(r.Name, "s")))
	res := resource{
		Resource:   r,
		Pkg:        "ops_" + r.Name,
		Singular:   shape.SingularPascal(r.Name),
		Plural:     shape.PascalCase(r.Name),
		Alias:      alias,
		Path:       path,
		Scoped:     shape.Scoped(r),
		Mergeable:  r.Store == rastrillo.Mergeable,
		Cols:       shape.Columns(r),
		Filters:    shape.FilterVars(r),
		SearchBind: shape.SearchBind(r),
		HumanOne:   one,
		HumanMany:  strings.ToLower(form.Humanize(r.Name)),
		Article:    article(one),
	}
	for _, f := range shape.FormFields(r) {
		res.Fields = append(res.Fields, field{
			Field:    f,
			Wire:     wirename.Name(f.Name),
			Human:    form.Humanize(f.Name),
			Money:    f.Kind == rastrillo.Money,
			Optional: f.Kind == rastrillo.Money && !f.Required,
			Textarea: f.Kind == rastrillo.Textarea,
		})
	}
	return res
}

// article is "an" before a vowel sound, so a sentence about an
// announcement reads as one.
func article(word string) string {
	if word != "" && strings.ContainsRune("aeiou", rune(word[0])) {
		return "an"
	}
	return "a"
}

// inputMoney reports whether any form field is money, which decides
// the input helpers and imports a file carries (checkMoney, settle,
// currency, the money input schema): an unused helper fails the
// examples' staticcheck and an unused import fails to compile.
func (r resource) inputMoney() bool {
	for _, f := range r.Fields {
		if f.Money {
			return true
		}
	}
	return false
}

// resultMoney reports whether any column is money, which decides the
// result helpers (moneyOf, the money result schema). A list column the
// form does not carry is shown but never set, so the two differ.
func (r resource) resultMoney() bool {
	for _, c := range r.Cols {
		if c.Kind == rastrillo.Money {
			return true
		}
	}
	return false
}

// optionalMoney reports whether any money field is optional.
func (r resource) optionalMoney() bool {
	for _, f := range r.Fields {
		if f.Optional {
			return true
		}
	}
	return false
}

// trimsText reports whether any form field is trimmed on the way in:
// text is, a textarea is stored as sent, money is numbers.
func (r resource) trimsText() bool {
	for _, f := range r.Fields {
		if !f.Money && !f.Textarea {
			return true
		}
	}
	return false
}

// requiredText reports whether any text field is required, which is
// the one check ValidateCreate makes with strings.
func (r resource) requiredText() bool {
	for _, f := range r.Fields {
		if !f.Money && f.Required {
			return true
		}
	}
	return false
}

// inForm is the form field for column c, if the column is one.
func (r resource) inForm(c shape.Column) (field, bool) {
	for _, f := range r.Fields {
		if f.Name == c.Name {
			return f, true
		}
	}
	return field{}, false
}

// wire is a field's JSON name, the rule behind api.FieldName. It is not
// sqlName: the two agree on every name Validate accepts, and the tag
// must follow the wire's definition, not the column's.
func wire(name string) string { return wirename.Name(name) }

// comment wraps text at the width the goldens pin.
func comment(text string) string { return shape.WrapComment(text, commentWidth) }

// ownerArg is the Owner: field a scoped store's params take, or nothing.
func (r resource) ownerArg() string {
	if r.Scoped {
		return "Owner: rc.Actor.Subject,\n"
	}
	return ""
}

// actorArg is the actor every mergeable write names, or nothing.
func (r resource) actorArg() string {
	if r.Mergeable {
		return "rc.Actor.String(), "
	}
	return ""
}

// recordArg is what a per-record store call takes: the bare id, or the
// id-and-owner params a scoped store keys on.
func (r resource) recordArg(query string) string {
	if r.Scoped {
		return fmt.Sprintf("%s.%s%sParams{ID: id, Owner: rc.Actor.Subject}", r.Alias, query, r.Singular)
	}
	return "id"
}

// Emit writes gen/ops/<name>/{types,list,get,create,update,delete}.go
// for r, package ops_<name>, and returns the paths it wrote. A
// hand-written ops/<name>/<op>.go under appRoot replaces that one
// generated file: its copy, package clause rewritten and build
// constraint stripped, goes to the same gen/ path, which is reported in
// skipped rather than written. The copy may carry the generated header
// (an author starts from the generated file), so the caller keeps
// skipped paths from pruning, and --check diffs them like any other,
// which is how a hand file edited since the last generate is caught.
// types.go is never replaced: it is the contract both doors and the
// parity check depend on.
func Emit(appRoot, genDir string, r rastrillo.Resource) (written, skipped []string, err error) {
	module, err := shape.ModulePath(appRoot)
	if err != nil {
		return nil, nil, err
	}
	if taken := opsnames.Taken(r.Name); taken != "" {
		// Validate refuses this first; a resource that reached the
		// emitter another way must not become a package that does not
		// compile.
		return nil, nil, fmt.Errorf("ops/%s: its type %s is a name every operations package declares", r.Name, taken)
	}
	res := describe(r, module)
	builders := map[string]func(resource) string{
		"types": typesGo, "list": listGo, "get": getGo, "create": createGo, "update": updateGo, "delete": deleteGo,
	}
	for _, name := range append([]string{"types"}, opFiles...) {
		path := filepath.Join(genDir, "ops", r.Name, name+".go")
		if name != "types" {
			hand, err := replace(appRoot, Hand{Resource: r.Name, Op: name}, path, res.Pkg)
			if err != nil {
				return nil, nil, err
			}
			if hand {
				skipped = append(skipped, path)
				continue
			}
		}
		src := "// Code generated by rastrillo generate. DO NOT EDIT.\n\n" + builders[name](res)
		formatted, ferr := format.Source([]byte(src))
		if ferr != nil {
			return nil, nil, fmt.Errorf("ops/%s/%s.go: %w\n%s", r.Name, name, ferr, src)
		}
		if err := shape.WriteFile(path, formatted); err != nil {
			return nil, nil, fmt.Errorf("ops/%s/%s.go: %w", r.Name, name, err)
		}
		written = append(written, path)
	}
	return written, skipped, nil
}

// imports renders an import block: the standard library, then this
// module's packages and the store, each only when the file uses it.
func imports(std []string, mod []string, store *resource) string {
	var b strings.Builder
	b.WriteString("import (\n")
	for _, s := range std {
		fmt.Fprintf(&b, "%q\n", s)
	}
	b.WriteString("\n")
	b.WriteString("\"amadan.net/rastrillo/rastrillo\"\n")
	for _, m := range mod {
		fmt.Fprintf(&b, "%q\n", "amadan.net/rastrillo/rastrillo/"+m)
	}
	if store != nil {
		fmt.Fprintf(&b, "%s %q\n", store.Alias, store.Path)
	}
	b.WriteString(")\n\n")
	return b.String()
}

// nowExpr is the timestamp every write stores.
const nowExpr = "time.Now().UTC().Format(time.RFC3339)"
