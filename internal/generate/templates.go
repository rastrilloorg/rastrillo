// Package generate's template emitter (this file) turns a validated
// rastrillo.Resource into the three screens a manifest owns —
// gen/templates/<name>/{list,show,form}.html — composed entirely from
// ui's partials (dict/list/icon come from ui.Funcs(), which the app
// registers alongside its own template tree; see ui/ui.go).
//
// Two kinds of string live in a generated template, and they are never
// mixed: strings the RESOURCE SHAPE decides (a screen's title, a field's
// label, a button's text) are known at generation time and baked in as
// literal `{{T $L "resource.<name>...."}}` / `{{T $L "ui...."}}` calls —
// EmitLocales (a later task) owns emitting the catalog keys these
// reference. Every body opens with {{$L := localizer $}} and every
// dict passes "L" $L (see migrated): the action puts the request's
// localizer in the view (actions.go's localizedField), so one parsed
// template serves every language. Strings a RECORD decides (a row's
// Main/Sub, a field's current Value, a validation Error) are runtime
// data the action (a later task) computes and hands to Execute; the
// template only names the data key, it never re-derives or formats the
// value — Money in particular is a formatted string by the time it
// reaches a template (or, in a form, a form.MoneyInput the action drew),
// never math the template does itself.
//
// list.html's data contract (pinned by the plan's golden, byte-exact):
// Empty bool; Query string; Carry [][2]string; Rows []struct{Href, Main,
// Sub string}; Pagination struct{Show bool; Items []struct{...}} (see
// ui/partials/pagination.html for Items' own shape); Filter
// filterView{SummaryKey, AriaField, Items []filterItem{Href, LabelKey,
// Current}} string, present only when the resource declares
// List.Filters (see actions.go). The original v1 amendment (list-bar
// renders Search only, no Filter key, because a manifest's `filter`
// entry validated but declared no enumerable dropdown values yet)
// stands for any resource that still declares no List.Filters — that
// shape's list.html is byte-identical to before List.Filters existed.
// A resource that DOES declare List.Filters gets a hand-composed
// rst-lbar strip instead of one "list-bar" dispatch — see listHTML's
// own doc for why. Both shapes gate their toolbar entirely at
// GENERATION time (not a runtime {{if}}, same discipline as form.html's
// Advanced-form gate below): a search=false, no-Filters resource's
// list.html contains no toolbar at all, so it never shows a search box
// the store's WHERE clause (which only honors `q` when List.Search is
// true) would silently ignore.
//
// show.html and form.html have no golden in the plan; this emitter
// pins their contracts (each file's own DO-NOT-EDIT comment restates
// it, since Task 8's action emitter is the contract's only reader
// besides this file):
//
//	show.html: Title string (the record's first text field's value —
//	list.html's Main, restated for the header); EditHref string (this
//	record's edit route); Fields map[string]string (every declared
//	field's current value, keyed by its declared name).
//
//	form.html: IsNew bool; Fields map[string]string (current values,
//	empty for New); Errors map[string]string, optional (a 400
//	re-render's per-field message, resolved through Tf so that a
//	catalog key localises) and ErrorArgs map[string]map[string]any (the
//	values a message interpolates); Money map[string]form.MoneyInput
//	(each money field's input), only for a resource with money;
//	BasicsAction/AdvancedAction string,
//	meaningful only when !IsNew (Edit's two POST targets — Advanced's
//	only exists in the template at all when the resource declares
//	Advanced fields).
package generate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/internal/tmplmigrate"
)

// contractL is Appendix C.6's gen.template.contract_l, the data-contract
// line every generated template with a contract carries for $L.
const contractL = "       L  *ui.Localizer, required: set by the action from the request; ui.Pages' middleware puts it there\n"

// contractMoney is Appendix C.6's gen.template.contract_money_seed, the
// line form.html carries for formView.Money in a resource with money.
const contractMoney = "       Money  map[string]form.MoneyInput, required for money fields: the text, the locale it is written in, the currency, and the seed (the text the server rendered, sent back so an unchanged edit is recognised), per field\n"

// migrated is an emitter's text after the template codemod, so the
// generator and migrate-templates share one implementation of the
// localizer-first syntax and generated templates cannot drift from
// what the codemod writes. The emitters build the text, so a refusal
// here is a generator bug: it panics, like an unknown Kind, rather
// than write a template that fails in strict mode. A wrapped or
// data-less {{template}} counts too: generated data is a dict or a
// struct carrying L, never something withL would have to patch.
func migrated(name string, src []byte) []byte {
	out, st, err := tmplmigrate.Rewrite(name, string(src))
	if err == nil && (len(st.Review) > 0 || len(st.NoData) > 0) {
		err = fmt.Errorf("sites the codemod cannot finish: %v %v", st.Review, st.NoData)
	}
	if err != nil {
		panic(fmt.Sprintf("generate: %s: %v", name, err))
	}
	return []byte(out)
}

// EmitTemplates writes gen/templates/<name>/{list,show,form}.html for
// r. A hand-written templates/<name>/<file>.html already present under
// appRoot skips generating that one file — its computed gen/ path is
// reported in skipped (for --check), and nothing is written or touched
// there.
func EmitTemplates(appRoot, genDir string, r rastrillo.Resource) (written, skipped []string, err error) {
	genResDir := filepath.Join(genDir, "templates", r.Name)
	handResDir := filepath.Join(appRoot, "templates", r.Name)

	files := []struct {
		name    string
		content func(rastrillo.Resource) []byte
	}{
		{"list.html", listHTML},
		{"show.html", showHTML},
		{"form.html", formHTML},
		{"confirm.html", confirmHTML},
	}

	for _, f := range files {
		genPath := filepath.Join(genResDir, f.name)

		_, statErr := os.Stat(filepath.Join(handResDir, f.name))
		if statErr == nil {
			skipped = append(skipped, genPath)
			continue
		}
		if !os.IsNotExist(statErr) {
			return nil, nil, fmt.Errorf("%s: %s: %w", r.Name, f.name, statErr)
		}

		if err := writeFileIfChanged(genPath, f.content(r)); err != nil {
			return nil, nil, fmt.Errorf("%s: %s: %w", r.Name, f.name, err)
		}
		written = append(written, genPath)
	}

	return written, skipped, nil
}

// resourceKey builds a `resource.<name>.<suffix>` catalog key — the
// only key shape generated templates ever reference for resource-owned
// text (shared chrome uses the `ui.*` keys instead, spelled literally
// at each call site since they don't vary by resource).
func resourceKey(name, suffix string) string {
	return "resource." + name + "." + suffix
}

// fieldPartial names the ui partial that renders k's field: Textarea
// gets the multi-line control, Money gets field-money (the currency
// beside the label and the locale, currency and seed it posts back),
// Text gets field-text. Every Kind Validate accepts has an entry;
// an unrecognized Kind means Validate's accepted set and this emitter
// have drifted, so it panics rather than silently guessing — the same
// discipline store.go's kindSQLType uses for the same reason.
func fieldPartial(k rastrillo.Kind) string {
	switch k {
	case rastrillo.Text:
		return "field-text"
	case rastrillo.Money:
		return "field-money"
	case rastrillo.Textarea:
		return "field-textarea"
	default:
		panic(fmt.Sprintf("generate: unknown Kind %q", k))
	}
}

// fieldMono reports whether k's value is Mono in a detail-list: Money
// is a formatted figure, not prose, so it renders monospace like any
// other machine-ish value. Same unrecognized-Kind panic as fieldPartial.
func fieldMono(k rastrillo.Kind) bool {
	switch k {
	case rastrillo.Text, rastrillo.Textarea:
		return false
	case rastrillo.Money:
		return true
	default:
		panic(fmt.Sprintf("generate: unknown Kind %q", k))
	}
}

// listHTML renders the List screen: page-header with the primary "new"
// action, an empty-state when there are no rows, otherwise a toolbar
// strip over a card of list-row-action rows and, when the action says
// so, pagination.
//
// The toolbar strip has two shapes, chosen at GENERATION time (same
// discipline as hasAdvanced's form.html gate) by whether r.List.Filters
// declares a filter at all (filtersDeclared, below — capped at one
// entry by Validate):
//
//   - No Filters declared: the plain v1 shape (unchanged since the
//     package doc's original v1 amendment) — {{template "list-bar" ...}}
//     dispatches the whole toolbar in one call, search only, no Filter
//     key, gated on r.List.Search exactly as before this task (a
//     Search:false, no-Filters resource still gets no toolbar at all).
//     A no-Filters resource's list.html is BYTE-IDENTICAL to what this
//     function emitted before Filters existed — see
//     TestEmitTemplatesGoldenFiles' goldenListHTML and
//     TestEmitTemplatesListHTMLGatesSearchOnFlag.
//
//   - Filters declared: list.html builds the "rst-lbar" strip itself —
//     {{template "list-bar-search" ...}} (only when Search is also on)
//     plus a hand-written <details rst-dropdown> block — rather
//     than dispatching the "list-bar"/"dropdown" partials as one call
//     with a "Filter" dict. Both are direct children of one
//     <div rst-lbar>, which tokens.css requires for the flex
//     layout that puts the search box and the dropdown side by side
//     ([rst-lbar] > [rst-search]'s 20rem cap reserves exactly this room).
//     The dropdown's own MARKUP is inlined rather than reached through
//     ui/partials/dropdown.html because that partial's contract takes
//     ONE dict already holding a fully-built Items list of RESOLVED
//     Label strings — and the per-item labels here are only KEYS
//     (filterItem.LabelKey; see actions.go's filterView/filterItem)
//     that must each go through (T ...) at RENDER time. There is no way
//     to build that one Items list, with a per-item (T ...) call baked
//     into each entry, from a single {{template "dropdown" ...}}
//     invocation: `list`'s argument list is fixed at template-parse
//     time, but the item count is a manifest-declared Values length —
//     unknown until generation. Emitting the equivalent HTML directly
//     inside a {{range .Filter.Items}} sidesteps that, calling
//     (T $L .LabelKey) per item exactly where a real range can reach it.
//     The inlined block mirrors dropdown.html's own template TEXT
//     byte-for-byte (same attributes, same {{if .Current}} aria-current
//     and check-icon guards, same {{- range}}/{{- end}} whitespace
//     trims) with exactly two substitutions — .Label becomes
//     (T $L .LabelKey), and .Aria/.Label on the summary become inline
//     (T .Filter.AriaField)/(T .Filter.SummaryKey) calls, composing
//     the "Filter by <field>: <current>" text the brief specifies —
//     so the rendered bytes match what dropdown.html itself would
//     render given the same resolved strings (see
//     TestListHTMLDropdownMatchesPartial, which renders both and
//     compares).
func listHTML(r rastrillo.Resource) []byte {
	newHref := r.Route + "/new"
	filtersDeclared := len(r.List.Filters) == 1

	var b strings.Builder
	fmt.Fprintf(&b, "{{/* Code generated by rastrillo generate; DO NOT EDIT.\n")
	fmt.Fprintf(&b, "     Eject: copy to templates/%s/list.html and edit — generation of\n", r.Name)
	b.WriteString("     this one file then stops.\n\n")
	b.WriteString("     Data contract (the action emitter reads this):\n")
	b.WriteString(contractL)
	b.WriteString("       Empty       bool, required: no row to list, so the empty state shows\n")
	b.WriteString("       Query       string, optional: the search text, when search is on\n")
	b.WriteString("       Carry       [][2]string, optional: the query pairs the search form carries\n")
	b.WriteString("       Filter      present only when the resource declares list filters:\n")
	b.WriteString("                   SummaryKey, AriaField, and Items with Href, LabelKey\n")
	b.WriteString("                   and Current per value\n")
	b.WriteString("       Rows        required: Href, Main and Sub per row\n")
	b.WriteString("       Pagination  required: Show, and Items in ui/partials/pagination.html's shape */}}\n")
	b.WriteString("{{define \"content\"}}\n")
	fmt.Fprintf(&b, "{{template \"page-header\" dict \"Title\" (T %q) \"ActionHref\" (path $ %q) \"ActionLabel\" (T \"ui.new\")}}\n",
		resourceKey(r.Name, "name"), newHref)
	b.WriteString("{{if .Empty}}\n")
	fmt.Fprintf(&b, "{{template \"empty-state\" dict \"Title\" (T %q) \"Body\" (T %q) \"ActionHref\" (path $ %q) \"ActionLabel\" (T \"ui.new\")}}\n",
		resourceKey(r.Name, "empty.title"), resourceKey(r.Name, "empty.body"), newHref)
	b.WriteString("{{else}}\n")
	b.WriteString("<div rst-list>\n")
	switch {
	case filtersDeclared:
		b.WriteString(listBarWithDropdownHTML(r))
	case r.List.Search:
		fmt.Fprintf(&b, "{{template \"list-bar\" dict \"SearchAction\" (path $ %q) \"Query\" .Query \"Placeholder\" (T \"ui.search\") \"Hidden\" .Carry}}\n", r.Route)
	}
	b.WriteString("{{range .Rows}}{{template \"list-row-action\" dict \"Href\" .Href \"Main\" .Main \"Sub\" .Sub}}\n")
	b.WriteString("{{end}}\n")
	b.WriteString("</div>\n")
	b.WriteString("{{if .Pagination.Show}}{{template \"pagination\" dict \"Items\" .Pagination.Items}}{{end}}\n")
	b.WriteString("{{end}}\n")
	b.WriteString("{{end}}\n")
	return migrated(r.Name+"/list.html", []byte(b.String()))
}

// listBarWithDropdownHTML renders the rst-lbar strip for a resource
// with a declared List.Filters entry: list-bar-search (only when
// r.List.Search is also on) plus the inline dropdown block, both
// direct children of one <div rst-lbar> — see listHTML's doc
// for why this doesn't dispatch "list-bar"/"dropdown" as one call.
func listBarWithDropdownHTML(r rastrillo.Resource) string {
	var b strings.Builder
	b.WriteString("<div rst-lbar>\n")
	if r.List.Search {
		fmt.Fprintf(&b, "{{template \"list-bar-search\" dict \"Action\" (path $ %q) \"Query\" .Query \"Placeholder\" (T \"ui.search\") \"Hidden\" .Carry}}\n", r.Route)
	}
	b.WriteString("{{/* dropdown markup inlined, not dispatched via the \"dropdown\" partial:\n")
	b.WriteString("     per-item (T $L .LabelKey) resolution inside this range cannot be\n")
	b.WriteString("     expressed as a single dict/list argument to one {{template}} call\n")
	b.WriteString("     (see listHTML's doc comment). Keep this block structurally\n")
	b.WriteString("     identical to ui/partials/dropdown.html: same attributes, same\n")
	b.WriteString("     aria-current/check-icon guards, same whitespace trims, so the\n")
	b.WriteString("     existing CSS and a11y contract hold unchanged. name=\"rst-menus\"\n")
	b.WriteString("     is the library's shared dropdown exclusivity group, written\n")
	b.WriteString("     literally here because the partial's MenuGroup override has no\n")
	b.WriteString("     counterpart in a generated screen's filterView. */}}\n")
	b.WriteString("<details rst-dropdown name=\"rst-menus\">\n")
	b.WriteString("  <summary rst-btn aria-label=\"Filter by {{T .Filter.AriaField}}: {{T .Filter.SummaryKey}}\">{{T .Filter.SummaryKey}} {{icon \"chevron-down\"}}</summary>\n")
	b.WriteString("  <div rst-dropdown-menu>\n")
	b.WriteString("    {{- range .Filter.Items}}\n")
	b.WriteString("    <a href=\"{{.Href}}\"{{if .Current}} aria-current=\"true\"{{end}}>{{T .LabelKey}}{{if .Current}} {{icon \"check\"}}{{end}}</a>\n")
	b.WriteString("    {{- end}}\n")
	b.WriteString("  </div>\n")
	b.WriteString("</details>\n")
	b.WriteString("</div>\n")
	return b.String()
}

// showHTML renders the Show screen: page-header (the record's own
// title, an edit pill) over a detail-list of every declared field, in
// columns(r)'s order — the same union+order store.go's schema uses, so
// the screen shows exactly the columns the table has.
func showHTML(r rastrillo.Resource) []byte {
	cols := columns(r)

	names := make([]string, len(cols))
	var items strings.Builder
	for i, c := range cols {
		names[i] = c.Name
		if i > 0 {
			items.WriteString("\n")
		}
		extra := ""
		if fieldMono(c.Kind) {
			extra = " \"Mono\" true"
		}
		fmt.Fprintf(&items, "  (dict \"Label\" (T %q) \"Value\" .Fields.%s%s)",
			resourceKey(r.Name, "field."+c.SQL), c.Name, extra)
	}

	var b strings.Builder
	b.WriteString("{{/* Code generated by rastrillo generate; DO NOT EDIT.\n")
	fmt.Fprintf(&b, "     Eject: copy to templates/%s/show.html and edit — generation of\n", r.Name)
	b.WriteString("     this one file then stops.\n\n")
	b.WriteString("     Data contract (the action emitter reads this):\n")
	b.WriteString(contractL)
	b.WriteString("       Title     string, required — the record shown here (its first\n")
	b.WriteString("                 text field's value; list.html's Main, restated for the\n")
	b.WriteString("                 header)\n")
	b.WriteString("       EditHref  string, required — this record's edit route\n")
	fmt.Fprintf(&b, "       Fields    map[string]string, required: every declared field's\n")
	fmt.Fprintf(&b, "                 value keyed by its declared name, in this resource's\n")
	fmt.Fprintf(&b, "                 declaration order: %s. Money is already\n", strings.Join(names, ", "))
	b.WriteString("                 written by the action, in the reader's language and\n")
	b.WriteString("                 the record's currency; templates never do money math. */}}\n")
	b.WriteString("{{define \"content\"}}\n")
	b.WriteString("{{template \"page-header\" dict \"Title\" .Title \"ActionHref\" .EditHref \"ActionLabel\" (T \"ui.edit\")}}\n")
	b.WriteString("{{template \"detail-list\" dict \"Items\" (list\n")
	b.WriteString(items.String())
	b.WriteString("\n)}}\n")
	b.WriteString("{{end}}\n")
	return migrated(r.Name+"/show.html", []byte(b.String()))
}

// formField renders one field-text/field-textarea call for f, wired to
// its Fields/Errors data keys and its resource-key label. A Required
// field adds "Required" true to the dict — field-text.html/
// field-textarea.html read that key for both the required attribute
// and the visible "*" marker (see their own docs); a non-Required field
// omits the key entirely rather than emitting "Required" false, which
// is what keeps this call byte-identical to before Required existed
// for every field that doesn't declare it.
//
// The error value goes through T on its way into the partial. form's
// date/time/datetime parsers report a catalog key ("rastrillo.ui.
// date_invalid") rather than a sentence, so the app's locale decides
// the wording; the older parsers still report plain English, and T
// hands back an unknown key verbatim, so those pass through unchanged.
// The partials render .Error as given — wrapping here rather than in
// field-text.html/field-textarea.html leaves a hand-written caller
// that already passes a finished sentence exactly as it was.
//
// index .Errors, not .Errors.<Name>: a field with no error (and the
// nil Errors map every non-400 render passes) makes the field lookup
// an *invalid* reflect.Value, which text/template will hand to a
// func(...any) like dict as a nil but refuses to hand to T, whose
// parameter is a string ("invalid value; expected string"). index
// yields the map element type's zero value instead, so the no-error
// case reaches T as the empty string and the partial's {{if .Error}}
// stays false.
//
// The error goes through Tf with the field's ErrorArgs, so a money
// message with values ("Enter the amount in {currency}.") renders
// them; a field with no arguments gets a nil map, which interpolates
// nothing, so its message renders as T would.
//
// A money field's input comes from .Money, not .Fields: the action
// draws it (seeded from the record, or the person's own text echoed
// with the locale it was read in), and the partial posts the locale,
// currency and seed back beside it.
func formField(r rastrillo.Resource, f rastrillo.Field) string {
	required := ""
	if f.Required {
		required = " \"Required\" true"
	}
	value := ".Fields." + f.Name
	if f.Kind == rastrillo.Money {
		m := fmt.Sprintf("(index .Money %q)", f.Name)
		value = m + ".Value \"Locale\" " + m + ".Locale \"Currency\" " + m + ".Currency \"Seed\" " + m + ".Seed"
	}
	return fmt.Sprintf("{{template %q dict \"Name\" %q \"Label\" (T %q) \"Value\" %s \"Error\" (Tf (index .Errors %q) (index .ErrorArgs %q))%s}}\n",
		fieldPartial(f.Kind), f.Name, resourceKey(r.Name, "field."+sqlName(f.Name)), value, f.Name, f.Name, required)
}

// formFoot renders one form-foot call. Cancel always targets the list
// (r.Route) — the plan's contract for both New and Edit forms.
func formFoot(r rastrillo.Resource) string {
	return fmt.Sprintf("{{template \"form-foot\" dict \"Submit\" (T \"ui.save\") \"CancelHref\" (path $ %q) \"CancelLabel\" (T \"ui.cancel\")}}\n", r.Route)
}

// formHTML renders the New/Edit screen. New is one form posting every
// field (Basics then Advanced) to the create route (r.Route, same path
// index.POST claims). Edit is always a Basics form posting to the
// data-supplied .BasicsAction, and — only when r.Form.Advanced is
// non-empty, decided here at generation time since that fact never
// changes at runtime — a second form posting Advanced fields to
// .AdvancedAction.
func formHTML(r rastrillo.Resource) []byte {
	hasAdvanced := len(r.Form.Advanced) > 0

	var b strings.Builder
	b.WriteString("{{/* Code generated by rastrillo generate; DO NOT EDIT.\n")
	fmt.Fprintf(&b, "     Eject: copy to templates/%s/form.html and edit — generation of\n", r.Name)
	b.WriteString("     this one file then stops.\n\n")
	b.WriteString("     Data contract (the action emitter reads this):\n")
	b.WriteString(contractL)
	b.WriteString("       IsNew           bool, required — New renders one form posting\n")
	b.WriteString("                       every field to the create route; Edit renders\n")
	b.WriteString("                       Basics (and, when this resource declares\n")
	b.WriteString("                       Advanced fields, a second Advanced form)\n")
	b.WriteString("       Fields          map[string]string, required — current values\n")
	b.WriteString("                       keyed by declared field name; empty strings\n")
	b.WriteString("                       for New\n")
	if r.HasMoney() {
		b.WriteString(contractMoney)
	}
	b.WriteString("       Errors          map[string]string, optional: a validation\n")
	b.WriteString("                       message or catalog key per field name, set\n")
	b.WriteString("                       only on a 400 re-render; each is resolved\n")
	b.WriteString("                       through Tf with that field's ErrorArgs, so a\n")
	b.WriteString("                       key localises and a plain sentence renders as\n")
	b.WriteString("                       written\n")
	b.WriteString("       ErrorArgs       map[string]map[string]any, optional: per field,\n")
	b.WriteString("                       the values its message takes, such as\n")
	b.WriteString("                       {currency}, {a} and {b}\n")
	if hasAdvanced {
		b.WriteString("       BasicsAction    string, required when !IsNew — the\n")
		b.WriteString("                       edit-basics POST target\n")
		b.WriteString("       AdvancedAction  string, required when !IsNew — the\n")
		b.WriteString("                       edit-advanced POST target */}}\n")
	} else {
		b.WriteString("       BasicsAction    string, required when !IsNew — the\n")
		b.WriteString("                       edit-basics POST target (this resource has\n")
		b.WriteString("                       no Advanced fields, so there is no second\n")
		b.WriteString("                       form) */}}\n")
	}
	b.WriteString("{{define \"content\"}}\n")
	b.WriteString("{{if .IsNew}}\n")
	fmt.Fprintf(&b, "<form rst-form method=\"post\" action=\"{{path $ %q}}\">\n", r.Route)
	for _, f := range r.Form.Basics {
		b.WriteString(formField(r, f))
	}
	for _, f := range r.Form.Advanced {
		b.WriteString(formField(r, f))
	}
	b.WriteString(formFoot(r))
	b.WriteString("</form>\n")
	b.WriteString("{{else}}\n")
	b.WriteString("<form rst-form method=\"post\" action=\"{{.BasicsAction}}\">\n")
	for _, f := range r.Form.Basics {
		b.WriteString(formField(r, f))
	}
	b.WriteString(formFoot(r))
	b.WriteString("</form>\n")
	if hasAdvanced {
		b.WriteString("<form rst-form method=\"post\" action=\"{{.AdvancedAction}}\">\n")
		for _, f := range r.Form.Advanced {
			b.WriteString(formField(r, f))
		}
		b.WriteString(formFoot(r))
		b.WriteString("</form>\n")
	}
	b.WriteString("{{end}}\n")
	b.WriteString("{{end}}\n")
	return migrated(r.Name+"/form.html", []byte(b.String()))
}

// confirmHTML renders <name>/confirm.html — the delete flow's confirm
// page, composed exactly the way ui's confirm-form partial documents
// itself: back-nav → page-header with the question as Title →
// explanation paragraph → the destructive POST with Cancel first in
// the DOM. A GET renders this; only the form's POST deletes.
func confirmHTML(r rastrillo.Resource) []byte {
	var b strings.Builder
	b.WriteString("{{/* Code generated by rastrillo generate; DO NOT EDIT.\n")
	fmt.Fprintf(&b, "     Eject: copy to templates/%s/confirm.html and edit — generation of\n", r.Name)
	b.WriteString("     this one file then stops.\n\n")
	b.WriteString("     Data contract (the action emitter reads this):\n")
	b.WriteString(contractL)
	b.WriteString("       Title       string, required — the record being deleted (its\n")
	b.WriteString("                   first text field's value; show.html's Title,\n")
	b.WriteString("                   restated so the person confirms the right one)\n")
	b.WriteString("       DeleteHref  string, required — the sibling POST that deletes\n")
	b.WriteString("       CancelHref  string, required — back to the record's show page */}}\n")
	b.WriteString("{{define \"content\"}}\n")
	fmt.Fprintf(&b, "{{template \"back-nav\" dict \"Href\" .CancelHref \"Label\" (T %q)}}\n",
		resourceKey(r.Name, "name"))
	fmt.Fprintf(&b, "{{template \"page-header\" dict \"Title\" (T %q)}}\n",
		resourceKey(r.Name, "delete.title"))
	fmt.Fprintf(&b, "<p>{{.Title}} — {{T %q}}</p>\n", resourceKey(r.Name, "delete.confirm"))
	fmt.Fprintf(&b, "{{template \"confirm-form\" dict \"Action\" .DeleteHref \"Label\" (T \"ui.delete\") \"Danger\" true \"CancelHref\" .CancelHref}}\n")
	b.WriteString("{{end}}\n")
	return migrated(r.Name+"/confirm.html", []byte(b.String()))
}
