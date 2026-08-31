// Package generate's locales emitter (this file) turns a resource set
// into the catalog keys templates.go's own (T "...") calls reference —
// templates.go is the source of truth for the key SET, not this file's
// own idea of what a resource needs; see EmitLocales' doc comment for
// the derivation. Two files come out of one map so they cannot drift:
// gen/locales/<default>.toml (human/translator-facing) and
// gen/locales/locales.go (a generated Go var, parse-free at runtime —
// what the app actually wires as Options.BaseCatalog).
package generate

import (
	"fmt"
	"go/format"
	"path/filepath"
	"sort"
	"strings"

	"amadan.net/rastrillo/rastrillo"
)

// uiKeys are the shared-chrome catalog keys templates.go spells
// literally at each call site (they don't vary by resource — see
// templates.go's package doc): "ui.new", "ui.save", "ui.search",
// "ui.cancel", "ui.edit". Grepped exhaustively against every `(T "...")`
// call in templates.go; if a future template adds another `ui.*` call,
// this map — not a golden — is what must grow to match it.
var uiKeys = map[string]string{
	"ui.new":    "New",
	"ui.save":   "Save",
	"ui.search": "Search",
	"ui.cancel": "Cancel",
	"ui.edit":   "Edit",
	"ui.delete": "Delete",
}

// EmitLocales writes gen/locales/<defaultLocale>.toml (for humans/
// translators) and gen/locales/locales.go (the generated `var
// BaseCatalog = rastrillo.Catalog{...}` an app wires as
// Options.BaseCatalog — see serve.go) from one key→value map, so the
// two cannot drift from one another.
//
// The key set is derived directly from templates.go's own `(T "...")`
// calls (the templates are the source of truth, not this doc comment):
// for every resource r, `resource.<r.Name>.name`, `.empty.title`,
// `.empty.body`, and `.field.<sqlName(field)>` for every field in
// columns(r) (store.go's own union of List columns + Form fields, the
// same set show.html/form.html label) — plus the five shared `ui.*`
// keys above, emitted once regardless of how many resources there are.
//
// A resource with a declared List.Filters entry (declaredFilter) adds
// `resource.<r.Name>.filter.<sqlName(field)>.<value>` per declared
// value — the LabelKey/SummaryKey actions.go's filterHelperFuncs emits
// (filter<Field>LabelKey) can resolve a non-empty filter value to — plus
// the shared `ui.all`, which the same function returns for the "" (all)
// value. `.field.<sqlName(field)>` for the filtered field itself is
// already covered by the columns(r) loop above (it's also a List column
// or Form field, per Validate — see actions.go's filterViewStmts
// AriaField); this block never emits it a second time.
//
// Values are a title-cased fallback derived from the declared
// identifier (titleCase: "MaxPerOrder" -> "Max per order", "notes" ->
// "Notes") — a reasonable default a translator or app author edits,
// not a pinned contract.
//
// defaultLocale is always "en" in this slice's caller (GenerateManifests)
// — Options.DefaultLocale is a runtime value manifests can't see at
// generation time, so the fragment is always authored in English; an
// app with a different default locale copies these keys into its own
// catalog for that locale. BaseCatalog still layers underneath
// regardless of which locale set the app declares (Locales' own layering
// doc comment holds unconditionally).
func EmitLocales(genDir, defaultLocale string, rs []rastrillo.Resource) error {
	m := localeMap(rs)

	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	tomlPath := filepath.Join(genDir, "locales", defaultLocale+".toml")
	if err := writeFileIfChanged(tomlPath, catalogTOML(keys, m)); err != nil {
		return fmt.Errorf("locales/%s.toml: %w", defaultLocale, err)
	}

	goSrc, err := catalogGo(keys, m)
	if err != nil {
		return fmt.Errorf("locales.go: %w", err)
	}
	goPath := filepath.Join(genDir, "locales", "locales.go")
	if err := writeFileIfChanged(goPath, goSrc); err != nil {
		return fmt.Errorf("locales.go: %w", err)
	}
	return nil
}

// localeMap builds the single key->value source both emitted files
// render from: the shared ui.* keys, plus every resource's own
// resource.<name>.{name,empty.title,empty.body,field.<sql>} keys —
// exactly the set templates.go's (T "...") calls reference for that
// resource (resourceKey and columns are templates.go's/store.go's own
// helpers, reused here so the key shape cannot drift from theirs) —
// plus, for a resource with a declared List.Filters entry
// (declaredFilter, actions.go's own helper), one
// resource.<name>.filter.<sql>.<value> key per declared value and the
// shared ui.all, added only then so a resource set with no declared
// filter still emits exactly the five ui.* keys (TestEmitLocalesUIKeyValues).
func localeMap(rs []rastrillo.Resource) map[string]string {
	m := make(map[string]string, len(uiKeys))
	for k, v := range uiKeys {
		m[k] = v
	}
	for _, r := range rs {
		title := titleCase(r.Name)
		m[resourceKey(r.Name, "name")] = title
		m[resourceKey(r.Name, "empty.title")] = fmt.Sprintf("No %s yet", strings.ToLower(title))
		m[resourceKey(r.Name, "empty.body")] = "Get started by creating your first one."
		for _, c := range columns(r) {
			m[resourceKey(r.Name, "field."+c.SQL)] = titleCase(c.Name)
		}
		// The delete flow's confirm page (templates.go's confirmHTML):
		// the page title and the one-sentence question under it.
		singularLower := strings.ToLower(titleCase(singularPascal(r.Name)))
		m[resourceKey(r.Name, "delete.title")] = "Delete " + singularLower
		m[resourceKey(r.Name, "delete.confirm")] = fmt.Sprintf("Delete this %s? This cannot be undone.", singularLower)
		if field, values, ok := declaredFilter(r); ok {
			m["ui.all"] = "All"
			prefix := resourceKey(r.Name, "filter."+sqlName(field))
			for _, v := range values {
				m[prefix+"."+v] = titleCase(v)
			}
		}
	}
	return m
}

// titleCase renders a declared identifier — r.Name's snake_case, or a
// Field/Column's camelCase/PascalCase (Validate's identPattern allows
// no other shape) — as a human-readable label: split into words at
// each underscore and each interior uppercase letter, then join with
// spaces, capitalizing only the very first letter of the whole phrase
// and lowercasing every other letter. "MaxPerOrder" -> "Max per order";
// "notes" -> "Notes"; "ticket_types" -> "Ticket types".
func titleCase(s string) string {
	var words []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			words = append(words, cur.String())
			cur.Reset()
		}
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '_' {
			flush()
			continue
		}
		isUpper := c >= 'A' && c <= 'Z'
		prevUpper := i > 0 && s[i-1] >= 'A' && s[i-1] <= 'Z'
		if isUpper && i > 0 && !prevUpper {
			flush()
		}
		cur.WriteByte(c)
	}
	flush()

	for i, w := range words {
		words[i] = strings.ToLower(w)
	}
	phrase := strings.Join(words, " ")
	if phrase == "" {
		return phrase
	}
	return strings.ToUpper(phrase[:1]) + phrase[1:]
}

// catalogTOML renders the human/translator-facing catalog: one
// `key = "value"` line per key, in sorted order — internal/catalog's
// Decode grammar (flat key -> string, one per line).
func catalogTOML(keys []string, m map[string]string) []byte {
	var b strings.Builder
	b.WriteString("# Code generated by rastrillo generate; DO NOT EDIT.\n")
	b.WriteString("# This is the manifest system's own fragment (English) — the field\n")
	b.WriteString("# labels and shared ui.* chrome strings a manifest resource's\n")
	b.WriteString("# generated templates reference. gen/locales/locales.go's var\n")
	b.WriteString("# BaseCatalog is what the app actually wires (Options.BaseCatalog);\n")
	b.WriteString("# this file exists for humans/translators to read and copy from —\n")
	b.WriteString("# both are emitted from one source map, so they cannot drift.\n")
	for _, k := range keys {
		fmt.Fprintf(&b, "%s = %q\n", k, m[k])
	}
	return []byte(b.String())
}

// catalogGo renders gen/locales/locales.go: a `var BaseCatalog =
// rastrillo.Catalog{...}` literal from the exact same key->value map
// catalogTOML rendered, so the two files cannot drift. Wired as
// Options.BaseCatalog (serve.go), it sits underneath every app catalog
// (rastrillo.Locales' own layering doc comment).
func catalogGo(keys []string, m map[string]string) ([]byte, error) {
	var b strings.Builder
	b.WriteString("// Code generated by rastrillo generate; DO NOT EDIT.\n\n")
	b.WriteString("package locales\n\n")
	b.WriteString("import \"amadan.net/rastrillo/rastrillo\"\n\n")
	b.WriteString("// BaseCatalog is the manifest system's own base English catalog —\n")
	b.WriteString("// wire it as Options.BaseCatalog (serve.go) so it layers underneath\n")
	b.WriteString("// every app catalog. Emitted from the exact same key->value map as\n")
	b.WriteString("// this package's sibling locales/en.toml, so the two cannot drift;\n")
	b.WriteString("// this Go var is what the app actually loads at runtime — the TOML\n")
	b.WriteString("// file is for humans/translators.\n")
	b.WriteString("var BaseCatalog = rastrillo.Catalog{\n")
	for _, k := range keys {
		fmt.Fprintf(&b, "\t%q: %q,\n", k, m[k])
	}
	b.WriteString("}\n")
	return format.Source([]byte(b.String()))
}
