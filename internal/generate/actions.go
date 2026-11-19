// Package generate's action emitter (this file) turns a validated
// rastrillo.Resource into the (up to) nine action files a manifest
// owns: gen/actions/<route>/{index.GET,index.POST,new.GET}.go and
// gen/actions/<route>/[id]/{index.GET,edit.GET,edit-basics.POST,
// delete.GET,delete.POST}.go, plus [id]/edit-advanced.POST.go when the
// resource declares Form.Advanced. Each file is written straight into gen/actions/ —
// unlike a hand action under actions/, it never passes through
// Discover/Rewrite, so it carries no `//go:build` constraint and
// compiles as a normal package from the moment it's written (this is
// what "generated actions go straight into gen/actions/, compiled
// normally" means in the task brief). A hand-written actions/<same
// path> file present under appRoot skips that one file — its computed
// gen/ path is reported in skipped, mirroring EmitTemplates.
//
// # The Ctx.Render seam
//
// Every generated action hands its page to the app's template tree
// through ctx.Render (rastrillo.Ctx's optional RenderFunc field — see
// ctx.go). Generated code cannot call an app-private helper like the
// blog's own blog.Render, so the seam is the one exported hook: the
// app's ctx factory sets Ctx.Render (e.g. &rastrillo.Ctx{DB: db,
// Render: blog.Render}), and every generated action nil-checks it
// before use, answering a logged 500 rather than a nil-pointer panic
// when an app forgets to wire it.
//
// RenderFunc carries no request, so every view type embeds ui.Localized
// and every render sets it from ui.ForRequest(r) (localizedField): the
// app's render function could not find the request's localizer itself.
//
// # The page-name contract (pinned here; the blog adoption task must
// match it)
//
// A generated action calls ctx.Render(ctx, w, page, status, data) with
// page always one of exactly three names, independent of which of the
// seven files is calling:
//
//	"<resource.Name>/list"     — index.GET only
//	"<resource.Name>/show"     — [id]/index.GET only
//	"<resource.Name>/confirm"  — [id]/delete.GET only: the confirm page
//	                             every destructive action gets as its own
//	                             URL (§9; also what §8's agent consent
//	                             gate points a human at)
//	"<resource.Name>/form"  — new.GET and [id]/edit.GET always; also
//	                          every 400 re-render (index.POST,
//	                          edit-basics.POST, edit-advanced.POST) —
//	                          though a re-render only happens, and so
//	                          only those files call Render at all, when
//	                          their own field group actually has a
//	                          Money field or a Required field to fail on
//	                          (see "no validation branch" below). Every file
//	                          that does call it uses this same name:
//	                          form.html's IsNew flag is what tells the
//	                          New state from the Edit state, not a
//	                          separate template.
//
// This mirrors the templates' own directory shape (gen/templates/
// <name>/{list,show,form}.html minus the .html suffix) rather than
// blog's buildPages, which keys its page map by bare basename only
// ("admin_list", "admin_edit", ...) — a bare "list"/"show"/"form" key
// would collide across every resource a manifest declares. An app
// wiring Render for generated resources must therefore key its own
// page map by "<name>/<file>" (e.g. by walking gen/templates and using
// each file's path relative to that root, minus ".html"), not by
// basename alone.
//
// # Behavior pinned here (encoded in the goldens; see actions_test.go)
//
// Every action reads and writes through the resource's operations
// (gen/ops/<name>, opsgen) and never its store: the page door and the
// API door share one implementation, so a value one accepts the other
// accepts too (spec §2). An operation's refusal is answered by
// view.Refuse (op.NotFound 404, op.Forbidden 403, anything else 500);
// op.Invalid is the one refusal a door with a form shows on it.
//
//   - index.GET: parses q (only when List.Search is set — and only
//     really used by the store when there is at least one eligible
//     text/textarea List column; see searchColumns), one query param
//     per filterFields(r) entry (store.go's dedup of bare List.Filter
//     and List.Filters[].Field — named by its sqlName, e.g. ?title=),
//     and page (default 1), and calls List in page-number mode, which
//     counts for the numbered links. A field ALSO declared via
//     List.Filters (capped at one entry by Validate) gets its raw query
//     value run through a generated normalize<Field> first —
//     unknown/absent collapses to "" (all), never a 400 — and the
//     action builds a filterView (SummaryKey/AriaField/Items, every
//     label a T KEY, never a resolved string) the render call's
//     listView.Filter carries; list.html renders it as an inline
//     dropdown (see templates.go). A bare-only List.Filter field (no
//     List.Filters counterpart) still gets its query param
//     parsed/carried/bound as a raw passthrough — it has no enumerable
//     Values to build a control or a normalizer from. Builds Rows from
//     List.Columns[0] (Main) and, if declared, List.Columns[1] (Sub) —
//     Money formatted in the reader's locale by the money helper
//     (moneyactions.go), everything else the raw column value.
//     Pagination has no gap-collapsing (v1 simplification: every page
//     number renders; see the report).
//   - index.POST (create): parses every Form.Basics + Form.Advanced
//     field in one form.Parse call and builds the operation's
//     CreateInput from it, each field marked Set when the form sent it
//     (present). Text fields carry no Required at the door: the
//     operation refuses a blank, so both doors refuse the same one. A
//     Money field keeps Required, read in the resource's currency and
//     the locale the form echoes (spec §5): a blank money input is a
//     field the form did send, which an op.Opt cannot say. A money
//     parse failure never reaches the operation: the door merges
//     ValidateCreate's map with its own parse errors (the parse error
//     wins on a field, being the more specific) and re-renders
//     "<name>/form" (IsNew: true) at 400 with every field's raw text
//     preserved, so every problem shows at once and nothing is
//     written. Create's op.Invalid re-renders the same way. Success
//     redirects 303 to Show.
//   - [id]/index.GET (show): Get, then every declared field
//     (columns(r) order) into Fields, Money formatted in the reader's
//     locale.
//   - new.GET / [id]/edit.GET: render "<name>/form" with zero values
//     (New) or the record's current values (Edit, Get first); money
//     inputs come from form.SeedMoney in the render locale.
//   - [id]/edit-basics.POST / [id]/edit-advanced.POST: Get first —
//     only when this field group has a Money field or a Required field
//     — to source the OTHER group's current values for the 400
//     re-render and the record's currencies, then parse and send only
//     this field group as an UpdateInput. A money parse failure
//     re-renders as index.POST does, merged with ValidateUpdate's map;
//     Update's op.Invalid re-renders too. A group with a money field
//     runs again when Update refuses the save with op.Conflict (the
//     record's currency changed under it), whatever the store kind:
//     see updatePOST. A field group with no Money field and no
//     Required field has no validation branch and no Get at all — the
//     operation answers op.NotFound for a missing row and writes
//     nothing, so the 404 still wins.
//   - scope = "user" (rastrillo.UserScoped): the operations filter by
//     Ctx.Actor.Subject, which the generated router sets from the
//     session (sessions.ActorFrom), and op.Check refuses an empty
//     subject as op.Forbidden before any query — so scoped routes
//     still mount behind sessions.Require / auth.RequireSession, and a
//     signed-out caller answers 403 through view.Refuse. Someone
//     else's id is op.NotFound and answers the same 404 a missing row
//     does, the scope package's 404-not-403 discipline. An unscoped
//     resource's output differs from a scoped one's only in the ops
//     package it calls.
package generate

import (
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"strings"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/internal/generate/shape"
)

// actionSpec is one of the (up to) seven files EmitActions considers
// for a resource.
type actionSpec struct {
	dir    string // slash-separated, relative to actions/, e.g. "admin/notes" or "admin/notes/[id]"
	name   string // filename stem: "index", "new", "edit", "edit-basics", "edit-advanced"
	method string
	build  func(rastrillo.Resource, string) string // (r, module) -> unformatted Go source
	op     string                                  // the operation the file calls: List, Get, Create, Update, Delete; "" for new.GET
}

// Door is one generated page action and the operation it calls, as the
// parity lint (internal/generate/lint) reads them. It comes off the
// same actionSpecs table the builders do, so a door the generator
// starts calling a different operation is a door the lint checks for
// that operation, with no second table to forget.
type Door struct {
	Source string // relative to actions/, e.g. "admin/notes/index.POST.go"
	Op     string // "" for a door that calls no operation
}

// Doors lists r's generated page actions and the operation each calls.
func Doors(r rastrillo.Resource) []Door {
	var out []Door
	for _, s := range actionSpecs(r) {
		out = append(out, Door{Source: s.dir + "/" + s.name + "." + s.method + ".go", Op: s.op})
	}
	return out
}

// EmitActions writes gen/actions/<route path>/... for r: index.GET,
// index.POST, new.GET, [id]/index.GET, [id]/edit.GET,
// [id]/edit-basics.POST, [id]/edit-advanced.POST (last one only when
// r.Form.Advanced is non-empty). A hand-written actions/<same path>
// file already present under appRoot skips generating that one file —
// its computed gen/ path is reported in skipped, and nothing is
// written or touched there. Returns the written/skipped gen/ paths.
func EmitActions(appRoot, genDir string, r rastrillo.Resource) (written, skipped []string, err error) {
	module, err := actionsModulePath(appRoot)
	if err != nil {
		return nil, nil, err
	}

	for _, s := range actionSpecs(r) {
		base := s.name + "." + s.method + ".go"
		relSource := s.dir + "/" + base
		genPath := filepath.Join(genDir, "actions", filepath.FromSlash(genDirFor(s.dir, s.name, s.method)), base)
		handPath := filepath.Join(appRoot, "actions", filepath.FromSlash(relSource))

		if _, statErr := os.Stat(handPath); statErr == nil {
			skipped = append(skipped, genPath)
			continue
		} else if !os.IsNotExist(statErr) {
			return nil, nil, fmt.Errorf("%s: %w", relSource, statErr)
		}

		pkg := packageNameFor(relSource)
		src := "// Code generated by rastrillo generate. DO NOT EDIT.\n\npackage " + pkg + "\n\n" + s.build(r, module)
		formatted, ferr := format.Source([]byte(src))
		if ferr != nil {
			return nil, nil, fmt.Errorf("%s: %w", relSource, ferr)
		}
		if err := writeFileIfChanged(genPath, formatted); err != nil {
			return nil, nil, fmt.Errorf("%s: %w", relSource, err)
		}
		written = append(written, genPath)
	}

	return written, skipped, nil
}

// actionSpecs enumerates the (up to) seven action files a resource
// gets — dir/name/method plus the builder that renders each one.
// EmitActions builds every file from exactly this list; manifestgen.go's
// ManifestActions reads dir/name/method off the SAME list to compute
// each file's Route and gen/ path for the router and the
// route-collision check, so the two enumerations cannot drift apart.
func actionSpecs(r rastrillo.Resource) []actionSpec {
	resDir := strings.TrimPrefix(r.Route, "/")
	idDir := resDir + "/[id]"

	specs := []actionSpec{
		{resDir, "index", "GET", actionIndexGET, "List"},
		{resDir, "index", "POST", actionIndexPOST, "Create"},
		{resDir, "new", "GET", actionNewGET, ""},
		{idDir, "index", "GET", actionShowGET, "Get"},
		{idDir, "edit", "GET", actionEditGET, "Get"},
		{idDir, "edit-basics", "POST", actionEditBasicsPOST, "Update"},
		{idDir, "delete", "GET", actionDeleteGET, "Get"},
		{idDir, "delete", "POST", actionDeletePOST, "Delete"},
	}
	if len(r.Form.Advanced) > 0 {
		specs = append(specs, actionSpec{idDir, "edit-advanced", "POST", actionEditAdvancedPOST, "Update"})
	}
	return specs
}

func actionsModulePath(appRoot string) (string, error) { return shape.ModulePath(appRoot) }

func storeImport(r rastrillo.Resource, module string) (alias, path string) {
	return shape.StoreImport(r, module)
}

// mainSubColumns returns the columns index.GET's Main/Sub and show's
// Title derive from: List.Columns[0] and, if declared, List.Columns[1].
// Falls back to columns(r)'s first entry when the resource declares no
// List columns at all (Validate only requires at least one List column
// OR Form field, not specifically the former) — a real but unlikely
// manifest shape, handled rather than left to panic.
func mainSubColumns(r rastrillo.Resource) (main column, sub column, hasSub bool) {
	if len(r.List.Columns) == 0 {
		if cols := columns(r); len(cols) > 0 {
			return cols[0], column{}, false
		}
		return column{}, column{}, false
	}
	c0 := r.List.Columns[0]
	main = column{Name: c0.Field, SQL: sqlName(c0.Field), Kind: c0.Kind}
	if len(r.List.Columns) > 1 {
		c1 := r.List.Columns[1]
		sub = column{Name: c1.Field, SQL: sqlName(c1.Field), Kind: c1.Kind}
		hasSub = true
	}
	return main, sub, hasSub
}

// fieldsMapLiteral renders a map[string]string literal of r's
// columns off record v, keyed by declared name: show.html's Fields
// and the Edit half of form.html's. In show, a money column reads the
// <field>Text local displayStmts formatted; a form shows money through
// formView.Money instead, so withMoney false leaves it out.
func fieldsMapLiteral(r rastrillo.Resource, v string, withMoney bool) string {
	var b strings.Builder
	b.WriteString("map[string]string{\n")
	for _, c := range columns(r) {
		switch {
		case c.Kind != rastrillo.Money:
			fmt.Fprintf(&b, "%q: %s.%s,\n", c.Name, v, c.Name)
		case withMoney:
			fmt.Fprintf(&b, "%q: %sText,\n", c.Name, lowerFirst(c.Name))
		}
	}
	b.WriteString("}")
	return b.String()
}

// zeroFieldsMapLiteral renders the New form's Fields: every declared
// non-money field name mapped to "" (money is in formView.Money).
func zeroFieldsMapLiteral(r rastrillo.Resource) string {
	var b strings.Builder
	b.WriteString("map[string]string{\n")
	for _, c := range columns(r) {
		if c.Kind != rastrillo.Money {
			fmt.Fprintf(&b, "%q: \"\",\n", c.Name)
		}
	}
	b.WriteString("}")
	return b.String()
}

// filterVar and the filter helpers live in shape, shared with the
// operation emitter.
type filterVar = shape.FilterVar

func filterVars(r rastrillo.Resource) []filterVar { return shape.FilterVars(r) }

func declaredFilter(r rastrillo.Resource) (field string, values []string, ok bool) {
	return shape.DeclaredFilter(r)
}

// ── form/view call-site plumbing, shared across all seven files for a
// given resource ────────────────────────────────────────────────────

// failWhat renders the "what" argument a view.Fail or view.Refuse call
// site passes: the shared helpers (form/view — Task 6) can't close over
// the resource name the way the old stamped-per-file fail helper did,
// so the "<name>: " prefix that used to live in the helper's own log
// line is folded into this string instead. Net effect on the emitted
// log line is unchanged.
func failWhat(r rastrillo.Resource, what string) string {
	return r.Name + ": " + what
}

// refuseStmts is the error branch after an operation call: the
// operation's refusal mapped onto the page by view.Refuse.
func refuseStmts(r rastrillo.Resource, what string) string {
	return fmt.Sprintf("if err != nil {\n\tview.Refuse(ctx, w, r, %q, err)\n\treturn\n}\n", failWhat(r, what))
}

// opsImport is the import an action takes its operations from:
// package ops_<name> at <module>/gen/ops/<name> (opsgen's Emit).
func opsImport(r rastrillo.Resource, module string) (alias, path string) {
	return "ops_" + r.Name, module + "/gen/ops/" + r.Name
}

// actionImportLines renders the module import lines every generated
// action needs (rastrillo, always; view, always — every one of the
// eight/nine files calls at least one of view.Refuse/view.Render/
// view.ParseID unconditionally) plus form when needsForm — an action
// with no Money field anywhere it reads/writes must not import form
// unused (go vet would reject it) — plus op when needsOp: an action
// that builds an operation input (present, amount, clearable), matches
// a refusal (op.Invalid, op.Conflict) or reads a result's money
// (*op.Money) uses it, and one that does none of those must not import
// it unused. Line order within the block doesn't matter: go/format
// sorts import specs within a contiguous group alphabetically, same as
// gofmt. hasView adds ui, for the ui.Localized every view type embeds
// and the ui.ForRequest(r) every view.Render call sets
// (localizedField); an action that declares no view must not import it
// unused.
func actionImportLines(r rastrillo.Resource, needsForm, needsOp, hasView bool) string {
	var b strings.Builder
	b.WriteString("\t\"amadan.net/rastrillo/rastrillo\"\n")
	if needsForm {
		b.WriteString("\t\"amadan.net/rastrillo/rastrillo/form\"\n")
	}
	if needsOp {
		b.WriteString("\t\"amadan.net/rastrillo/rastrillo/op\"\n")
	}
	if hasView {
		b.WriteString("\t\"amadan.net/rastrillo/rastrillo/ui\"\n")
	}
	b.WriteString("\t\"amadan.net/rastrillo/rastrillo/view\"\n")
	return b.String()
}

// localizedField opens every view literal a generated action renders.
// RenderFunc carries no request, so the app's render function cannot
// find the request's localizer for a generated view; without this the
// page would render in the default language whatever the reader asked
// for. ui.ForRequest also carries the switcher's items and the return
// path the shells read.
const localizedField = "Localized: ui.ForRequest(r),\n"

// hasMoneyColumn reports whether any of cols is a Money column.
func hasMoneyColumn(cols []column) bool {
	for _, c := range cols {
		if c.Kind == rastrillo.Money {
			return true
		}
	}
	return false
}

// listViewTypes renders index.GET's data-shape types. hasFilter is
// false for every resource without a declared List.Filters entry — the
// exact string this function returned before Filter existed at all, so
// that byte-identical regression golden (fixtureResource/
// filterOnlyFixtureResource, neither of which declares List.Filters)
// never sees a Filter field it has no data for. hasFilter true (a
// declared List.Filters — see declaredFilter) adds listView.Filter and
// the filterView/filterItem pair the dropdown markup in list.html reads
// (see templates.go's listHTML): LabelKey fields, never resolved
// strings — the template resolves each one itself via (T .LabelKey) at
// render time, since only the template has a bound T (see the package
// doc's "labels resolve at RENDER time" note on actionIndexGET).
func listViewTypes(hasFilter bool) string {
	filterField := ""
	filterTypes := ""
	if hasFilter {
		filterField = "\tFilter     filterView\n"
		filterTypes = `
type filterView struct {
	SummaryKey string
	AriaField  string
	Items      []filterItem
}

type filterItem struct {
	Href     string
	LabelKey string
	Current  bool
}
`
	}
	return fmt.Sprintf(`
type listView struct {
	ui.Localized
	Empty      bool
	Query      string
	Carry      [][2]string
%sRows       []listRow
	Pagination listPagination
}
%s
type listRow struct {
	Href string
	Main string
	Sub  string
}

type listPagination struct {
	Show  bool
	Items []listPageItem
}

type listPageItem struct {
	Label    string
	Href     string
	Rel      string
	Current  bool
	Disabled bool
	Gap      bool
}
`, filterField, filterTypes)
}

const showViewType = `
type showView struct {
	ui.Localized
	Title    string
	EditHref string
	Fields   map[string]string
}
`

// formViewType is form.html's data. ErrorArgs carries the values a
// money error interpolates ({currency}, {a}, {b}); the template passes
// every field's entry to Tf, and a missing one is a nil map that
// interpolates nothing. Money, for a resource with money fields, is
// each one's input across both groups: text, locale, currency and seed.
func formViewType(r rastrillo.Resource) string {
	money := ""
	if r.HasMoney() {
		money = "\tMoney          map[string]form.MoneyInput\n"
	}
	return `
type formView struct {
	ui.Localized
	IsNew          bool
	Fields         map[string]string
	Errors         map[string]string
	ErrorArgs      map[string]map[string]any
` + money + `	BasicsAction   string
	AdvancedAction string
}
`
}

// currencyImport is the import line an action that formats amounts
// (displayHelper) adds to actionImportLines'.
const currencyImport = "\t\"amadan.net/rastrillo/rastrillo/currency\"\n"

// basepathImport is the import an action that hands its view a link
// takes. Every link and form action a view renders goes through
// basepath.Path, or a resource served under /acme links to the hub's
// page of the same name. Headers stay app paths: the redirects after a
// save and the JSON API's create Location (apigen's create) are
// prefixed once by the base-path middleware, and wrapping them as well
// would send the reader to /acme/acme/….
const basepathImport = "\t\"amadan.net/rastrillo/rastrillo/basepath\"\n"

// ── index.GET (List) ────────────────────────────────────────────────

func actionIndexGET(r rastrillo.Resource, module string) string {
	ops, path := opsImport(r, module)
	fvars := filterVars(r)
	searchDeclared := r.List.Search
	searchParam := r.List.Search && len(searchColumns(r)) > 0
	main, sub, hasSub := mainSubColumns(r)

	// declaredField/declaredValues/filtersDeclared drive the ONE thing
	// this task adds beyond filterVars' plain query-param plumbing: a
	// rendered dropdown control. A bare List.Filter entry (no Filters)
	// still gets its query param parsed and bound above — it just has no
	// enumerable Values to build a control or a normalize function from
	// (see the package doc's "still validated, generates the WHERE
	// clause but no control" note), so it stays a raw passthrough exactly
	// as before this task. declaredVar resolves the matching filterVar by
	// Field — always found, since filterFields (store.go) always
	// includes a declared Filters[].Field in its union.
	declaredField, declaredValues, filtersDeclared := declaredFilter(r)
	var declaredVar filterVar
	if filtersDeclared {
		for _, fv := range fvars {
			if fv.Field == declaredField {
				declaredVar = fv
				break
			}
		}
	}

	showsMoney := main.Kind == rastrillo.Money || (hasSub && sub.Kind == rastrillo.Money)

	var b strings.Builder
	b.WriteString("import (\n")
	b.WriteString("\t\"fmt\"\n")
	b.WriteString("\t\"net/http\"\n")
	b.WriteString("\t\"net/url\"\n")
	b.WriteString("\t\"strconv\"\n")
	b.WriteString("\t\"strings\"\n\n")
	b.WriteString(actionImportLines(r, false, showsMoney, true))
	b.WriteString(basepathImport)
	if showsMoney {
		b.WriteString(currencyImport)
	}
	fmt.Fprintf(&b, "\t%s %q\n", ops, path)
	b.WriteString(")\n")

	fmt.Fprintf(&b, "\n// Handle is GET %s.\n", r.Route)
	b.WriteString("func Handle(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request) {\n")

	searchExpr := `""`
	if searchDeclared {
		b.WriteString("search := strings.TrimSpace(r.URL.Query().Get(\"q\"))\n")
		searchExpr = "search"
	}
	for _, fv := range fvars {
		if filtersDeclared && fv.Field == declaredField {
			fmt.Fprintf(&b, "%s := normalize%s(r.URL.Query().Get(%q))\n", fv.Var, fv.Field, fv.Query)
		} else {
			fmt.Fprintf(&b, "%s := r.URL.Query().Get(%q)\n", fv.Var, fv.Query)
		}
	}

	b.WriteString(`
page := 1
if v := r.URL.Query().Get("page"); v != "" {
	if n, err := strconv.Atoi(v); err == nil && n > 0 {
		page = n
	}
}
const pageSize = 10
`)

	b.WriteString("\nvar carry [][2]string\n")
	for _, fv := range fvars {
		fmt.Fprintf(&b, "if %s != \"\" {\n\tcarry = append(carry, [2]string{%q, %s})\n}\n", fv.Var, fv.Query, fv.Var)
	}

	// The operation counts and lists in page-number mode. Search is
	// bound only when the store honours it (searchParam): ListInput has
	// the member only then (opsgen's SearchBind).
	fmt.Fprintf(&b, "\nres, err := %s.List(r.Context(), ctx, %s.ListInput{\n", ops, ops)
	if searchParam {
		b.WriteString("Search: " + searchExpr + ",\n")
	}
	for _, fv := range fvars {
		fmt.Fprintf(&b, "%s: %s,\n", fv.Param, fv.Var)
	}
	b.WriteString("PageNumber: page,\nLimit: pageSize,\n})\n")
	b.WriteString(refuseStmts(r, "loading "+r.Name))
	b.WriteString("total, rows := res.Total, res.Items\n")

	b.WriteString("\nitems := make([]listRow, 0, len(rows))\nfor _, n := range rows {\n")
	mainExpr := displayStmts(&b, "n", main, "n.ID")
	subExpr := `""`
	if hasSub {
		subExpr = displayStmts(&b, "n", sub, "n.ID")
	}
	fmt.Fprintf(&b, `items = append(items, listRow{
		Href: basepath.Path(r, fmt.Sprintf(%q, n.ID)),
		Main: %s,
		Sub:  %s,
	})
}
`, r.Route+"/%d", mainExpr, subExpr)

	b.WriteString(`
totalPages := (int(total) + pageSize - 1) / pageSize
show := int(total) > pageSize
var pageItems []listPageItem
if show {
	prev := listPageItem{Label: "Previous", Rel: "prev", Disabled: true}
	if page > 1 {
		prev = listPageItem{Label: "Previous", Rel: "prev", Href: basepath.Path(r, href(` + searchExprForHref(searchDeclared) + `, carry, page-1))}
	}
	pageItems = append(pageItems, prev)
	for n := 1; n <= totalPages; n++ {
		if n == page {
			pageItems = append(pageItems, listPageItem{Label: strconv.Itoa(n), Current: true})
		} else {
			pageItems = append(pageItems, listPageItem{Label: strconv.Itoa(n), Href: basepath.Path(r, href(` + searchExprForHref(searchDeclared) + `, carry, n))})
		}
	}
	next := listPageItem{Label: "Next", Rel: "next", Disabled: true}
	if page < totalPages {
		next = listPageItem{Label: "Next", Rel: "next", Href: basepath.Path(r, href(` + searchExprForHref(searchDeclared) + `, carry, page+1))}
	}
	pageItems = append(pageItems, next)
}
`)

	filterField := ""
	if filtersDeclared {
		b.WriteString(filterViewStmts(r, declaredVar, declaredValues, searchExpr))
		filterField = "Filter:      filter,\n"
	}

	fmt.Fprintf(&b, `
view.Render(ctx, w, %q, http.StatusOK, listView{
	%sEmpty:      total == 0,
	Query:      %s,
	Carry:      carry,
	%sRows:       items,
	Pagination: listPagination{Show: show, Items: pageItems},
})
}
`, r.Name+"/list", localizedField, searchExpr, filterField)

	fmt.Fprintf(&b, `
// href builds one list/pagination link, preserving the current search
// and filter values and setting page.
func href(search string, carry [][2]string, page int) string {
	var params []string
	if search != "" {
		params = append(params, "q="+url.QueryEscape(search))
	}
	for _, kv := range carry {
		params = append(params, kv[0]+"="+url.QueryEscape(kv[1]))
	}
	params = append(params, "page="+strconv.Itoa(page))
	return %q + "?" + strings.Join(params, "&")
}
`, r.Route)

	if filtersDeclared {
		b.WriteString(filterHelperFuncs(r, declaredVar, declaredValues))
	}

	if showsMoney {
		b.WriteString(displayHelper(r))
	}
	b.WriteString(listViewTypes(filtersDeclared))
	return b.String()
}

// filterViewStmts renders the Handle-body statements that build the
// declared filter's filterView value (assigned to the local "filter"
// listView reads into its Filter field): a "" All item first, then one
// item per declared value, in declaration order — matching the "All
// first" ordering the blog's hand pattern (BuildStatusFilter) set. Every
// Href goes through fv.Field's own filter<Field>Href, which — per the
// brief's contract — carries the current search and this ONE filter
// value, never page (changing a filter starts over at page 1). It also
// never carries any OTHER filterVar's current query value — unlike
// pagination's href, which loops the full carry slice. That only
// matters for a bare List.Filter field (a raw WHERE-clause passthrough
// with no [[list.filters]] entry and so no generated dropdown of its
// own): its active query param is silently dropped by a click on this
// dropdown. Narrow in practice — no shipped example combines a bare
// List.Filter field with a declared List.Filters field on the same
// resource — but real should that combination ever appear.
// Every label is a T KEY (filter<Field>LabelKey), never a resolved
// string — only list.html's template execution has a bound T (see the
// package doc's "labels resolve at RENDER time" note).
func filterViewStmts(r rastrillo.Resource, fv filterVar, values []string, searchExpr string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\nfilter := filterView{\n")
	fmt.Fprintf(&b, "SummaryKey: filter%sLabelKey(%s),\n", fv.Field, fv.Var)
	fmt.Fprintf(&b, "AriaField:  %q,\n", resourceKey(r.Name, "field."+fv.Query))
	b.WriteString("Items: []filterItem{\n")
	fmt.Fprintf(&b, "{Href: basepath.Path(r, filter%sHref(%s, \"\")), LabelKey: filter%sLabelKey(\"\"), Current: %s == \"\"},\n",
		fv.Field, searchExpr, fv.Field, fv.Var)
	for _, v := range values {
		fmt.Fprintf(&b, "{Href: basepath.Path(r, filter%sHref(%s, %q)), LabelKey: filter%sLabelKey(%q), Current: %s == %q},\n",
			fv.Field, searchExpr, v, fv.Field, v, fv.Var, v)
	}
	b.WriteString("},\n}\n")
	return b.String()
}

// filterHelperFuncs renders the three package-level functions the
// declared filter's dropdown needs — normalize<Field>, filter<Field>
// LabelKey and filter<Field>Href — parameterized on the resource so
// AriaField and every LabelKey/Href reference this resource's own name,
// route and sqlName(fv.Field), matching resourceKey's "resource.<name>.
// <suffix>" shape (templates.go) and sqlName's column-name convention
// (store.go) exactly, so the T keys this emits are the SAME keys
// EmitLocales (a later task) must cover.
func filterHelperFuncs(r rastrillo.Resource, fv filterVar, values []string) string {
	var b strings.Builder
	quoted := make([]string, len(values))
	for i, v := range values {
		quoted[i] = fmt.Sprintf("%q", v)
	}
	fmt.Fprintf(&b, `
// normalize%s maps a raw query value onto %s's declared filter values:
// anything else — an empty string, a stale bookmark, a hand-edited URL —
// normalizes to "" (all), never a 400.
func normalize%s(raw string) string {
	switch raw {
	case %s:
		return raw
	}
	return ""
}

// filter%sLabelKey resolves a normalized %s value ("" or a declared
// value) to its catalog key; T renders it at request time, never here.
func filter%sLabelKey(v string) string {
	if v == "" {
		return "ui.all"
	}
	return %q + v
}

// filter%sHref builds one %s dropdown item's link: the current search
// plus this filter value (never page — changing a filter starts at page
// 1 by construction), value == "" for the "All" item.
func filter%sHref(search, value string) string {
	var params []string
	if search != "" {
		params = append(params, "q="+url.QueryEscape(search))
	}
	if value != "" {
		params = append(params, %q+url.QueryEscape(value))
	}
	if len(params) == 0 {
		return %q
	}
	return %q + "?" + strings.Join(params, "&")
}
`,
		fv.Field, fv.Field,
		fv.Field, strings.Join(quoted, ", "),
		fv.Field, fv.Field,
		fv.Field, resourceKey(r.Name, "filter."+fv.Query)+".",
		fv.Field, fv.Field,
		fv.Field,
		fv.Query+"=",
		r.Route,
		r.Route,
	)
	return b.String()
}

// searchExprForHref is the argument href() is called with at each of
// its three call sites: the live "search" variable when List.Search
// declares a search box at all (independent of whether the store ends
// up honoring it — see searchParam), or a literal "" when it doesn't,
// so no unused variable is ever emitted.
func searchExprForHref(searchDeclared bool) string {
	if searchDeclared {
		return "search"
	}
	return `""`
}

// ── index.POST (Create) ─────────────────────────────────────────────

func actionIndexPOST(r rastrillo.Resource, module string) string {
	ops, path := opsImport(r, module)
	order := formFields(r)
	canFail := fieldsCanFail(order)
	money := moneyFields(order)

	var b strings.Builder
	b.WriteString("import (\n")
	if canFail {
		b.WriteString("\t\"errors\"\n")
	}
	b.WriteString("\t\"fmt\"\n")
	b.WriteString("\t\"net/http\"\n\n")
	b.WriteString(actionImportLines(r, len(order) > 0, len(order) > 0, true))
	fmt.Fprintf(&b, "\t%s %q\n", ops, path)
	b.WriteString(")\n")

	fmt.Fprintf(&b, "\n// Handle is POST %s.\n", r.Route)
	b.WriteString(`func Handle(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request.", http.StatusBadRequest)
		return
	}

`)
	if len(order) > 0 {
		b.WriteString(parseCall(r, order, false))
	}
	fmt.Fprintf(&b, "in := %s.CreateInput{\n%s}\n", ops, inputLines(order, func(rastrillo.Field) string { return fmt.Sprintf("%q", r.Currency) }))

	if canFail {
		b.WriteString("reshow := func(errs map[string]string) {\n")
		b.WriteString(echoStmts(order, false))
		fmt.Fprintf(&b, "view.Render(ctx, w, %q, http.StatusBadRequest, formView{\n", r.Name+"/form")
		b.WriteString(localizedField + "IsNew: true,\nFields: p.Echo(),\nErrors: errs,\nErrorArgs: p.ErrorArgs(),\n")
		if r.HasMoney() {
			b.WriteString("Money: " + moneyMapLiteral(order) + ",\n")
		}
		b.WriteString("})\n}\n")
	}
	if len(money) > 0 {
		fmt.Fprintf(&b, "if !p.OK() {\nerrs := %s.ValidateCreate(in)\n%sreshow(errs)\nreturn\n}\n", ops, mergeParseErrors)
	}

	fmt.Fprintf(&b, "\nn, err := %s.Create(r.Context(), ctx, in)\n", ops)
	if canFail {
		b.WriteString("var invalid op.Invalid\nif errors.As(err, &invalid) {\nreshow(invalid.Fields)\nreturn\n}\n")
	}
	b.WriteString(refuseStmts(r, "creating "+r.Name))
	fmt.Fprintf(&b, "http.Redirect(w, r, fmt.Sprintf(%q, n.ID), http.StatusSeeOther)\n}\n", r.Route+"/%d")

	b.WriteString(inputHelpers(order))
	b.WriteString(formViewType(r))
	return b.String()
}

// mergeParseErrors folds the door's own parse errors over the
// operation's map. The parse error wins on a field: it names what was
// typed (not an amount, a stale currency), where the operation can
// only say the field is missing.
const mergeParseErrors = "for k, v := range p.Errors() {\nerrs[k] = v\n}\n"

// ── new.GET ──────────────────────────────────────────────────────────

func actionNewGET(r rastrillo.Resource, module string) string {
	money := moneyFields(formFields(r))
	var b strings.Builder
	b.WriteString("import (\n")
	if len(money) > 0 {
		b.WriteString("\t\"fmt\"\n")
	}
	b.WriteString("\t\"net/http\"\n\n")
	b.WriteString(actionImportLines(r, r.HasMoney(), false, true))
	b.WriteString(")\n")

	fmt.Fprintf(&b, "\n// Handle is GET %s/new.\n", r.Route)
	b.WriteString("func Handle(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request) {\n")
	// A new record has no row id; 0 names none in a failure, which only
	// a currency the manifest check let through could cause.
	b.WriteString(seedStmts(money, "0",
		func(rastrillo.Field) string { return "nil" },
		func(rastrillo.Field) string { return fmt.Sprintf("%q", r.Currency) }))
	fmt.Fprintf(&b, "view.Render(ctx, w, %q, http.StatusOK, formView{\n", r.Name+"/form")
	b.WriteString(localizedField + "IsNew: true,\nFields: " + zeroFieldsMapLiteral(r) + ",\n")
	if r.HasMoney() {
		b.WriteString("Money: " + moneyMapLiteral(money) + ",\n")
	}
	b.WriteString("})\n}\n")

	if len(money) > 0 {
		b.WriteString(seedHelper(r))
	}
	b.WriteString(formViewType(r))
	return b.String()
}

// ── the Get every per-record page opens with ────────────────────────

// getStmts renders the operation call a per-record page opens with:
// the id from the path, then Get, whose op.NotFound (a missing row, or
// someone else's) view.Refuse answers as 404.
func getStmts(r rastrillo.Resource, ops string) string {
	return fmt.Sprintf(`id, ok := view.ParseID(r)
	if !ok {
		view.NotFound(ctx, w, r)
		return
	}
	n, err := %s.Get(r.Context(), ctx, id)
%s`, ops, refuseStmts(r, "loading "+r.Name))
}

// ── [id]/index.GET (Show) ───────────────────────────────────────────

func actionShowGET(r rastrillo.Resource, module string) string {
	ops, path := opsImport(r, module)
	main, _, _ := mainSubColumns(r)
	showsMoney := hasMoneyColumn(columns(r))

	var b strings.Builder
	b.WriteString("import (\n")
	b.WriteString("\t\"fmt\"\n")
	b.WriteString("\t\"net/http\"\n\n")
	b.WriteString(actionImportLines(r, false, showsMoney, true))
	b.WriteString(basepathImport)
	if showsMoney {
		b.WriteString(currencyImport)
	}
	fmt.Fprintf(&b, "\t%s %q\n", ops, path)
	b.WriteString(")\n")

	fmt.Fprintf(&b, "\n// Handle is GET %s/{id}.\n", r.Route)
	b.WriteString("func Handle(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request) {\n")
	b.WriteString(getStmts(r, ops))

	title := "n." + main.Name
	for _, c := range columns(r) {
		if e := displayStmts(&b, "n", c, "id"); c.Name == main.Name {
			title = e
		}
	}
	fmt.Fprintf(&b, `
view.Render(ctx, w, %q, http.StatusOK, showView{
	%sTitle:    %s,
	EditHref: basepath.Path(r, fmt.Sprintf(%q, id)),
	Fields:   %s,
})
}
`, r.Name+"/show", localizedField, title, r.Route+"/%d/edit", fieldsMapLiteral(r, "n", true))

	if showsMoney {
		b.WriteString(displayHelper(r))
	}
	b.WriteString(showViewType)
	return b.String()
}

// ── [id]/edit.GET ────────────────────────────────────────────────────

func actionEditGET(r rastrillo.Resource, module string) string {
	ops, path := opsImport(r, module)
	hasAdvanced := len(r.Form.Advanced) > 0
	money := moneyFields(formFields(r))

	var b strings.Builder
	b.WriteString("import (\n")
	b.WriteString("\t\"fmt\"\n")
	b.WriteString("\t\"net/http\"\n\n")
	b.WriteString(actionImportLines(r, r.HasMoney(), len(money) > 0, true))
	b.WriteString(basepathImport)
	fmt.Fprintf(&b, "\t%s %q\n", ops, path)
	b.WriteString(")\n")

	fmt.Fprintf(&b, "\n// Handle is GET %s/{id}/edit.\n", r.Route)
	b.WriteString("func Handle(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request) {\n")
	b.WriteString(getStmts(r, ops))

	b.WriteString(seedStmts(money, "id", recordMinor, recordCode))
	fmt.Fprintf(&b, `
view.Render(ctx, w, %q, http.StatusOK, formView{
	%sIsNew:  false,
	Fields: %s,
`, r.Name+"/form", localizedField, fieldsMapLiteral(r, "n", false))
	if r.HasMoney() {
		b.WriteString("Money: " + moneyMapLiteral(money) + ",\n")
	}
	fmt.Fprintf(&b, "BasicsAction: basepath.Path(r, fmt.Sprintf(%q, id)),\n", r.Route+"/%d/edit-basics")
	if hasAdvanced {
		fmt.Fprintf(&b, "AdvancedAction: basepath.Path(r, fmt.Sprintf(%q, id)),\n", r.Route+"/%d/edit-advanced")
	}
	b.WriteString("})\n}\n")

	if len(money) > 0 {
		b.WriteString(seedHelper(r) + codeHelper(r) + minorHelper)
	}
	b.WriteString(formViewType(r))
	return b.String()
}

// recordMinor and recordCode are a seed's arguments off the fetched
// record n: the stored amount, or nil, and the field's currency, or
// the resource default while it is empty.
func recordMinor(f rastrillo.Field) string { return "minorOf(n." + f.Name + ")" }
func recordCode(f rastrillo.Field) string  { return "currencyOf(n." + f.Name + ")" }

// ── [id]/edit-basics.POST and [id]/edit-advanced.POST ──────────────

// updatePOST builds edit-basics.POST or edit-advanced.POST: they are
// the same shape (parse this group's fields into an UpdateInput, call
// Update, redirect — or, only when this group declares a Money field or
// a Required field, Get first and a re-render on a refusal),
// parameterized on which field group owns it.
//
// other is the other group's fields: a failed POST seeds its money
// inputs from the record, in the render locale, since this POST did
// not submit them.
func updatePOST(r rastrillo.Resource, module string, group, other []rastrillo.Field, groupName string) string {
	ops, path := opsImport(r, module)
	hasAdvanced := len(r.Form.Advanced) > 0

	// groupHasValidation is true when ANY field in this group can be
	// refused — a Money parse (any Money field) or the operation's
	// Required check (any Required Text/Textarea/Money field). A group
	// with neither has no way to fail, so no re-render and no Get: the
	// operation answers op.NotFound for a missing row itself.
	groupHasValidation := fieldsCanFail(group)

	needsForm := len(group) > 0 || (groupHasValidation && r.HasMoney())
	money := moneyFields(group)
	// The record's other money inputs are drawn only on a failed POST.
	otherMoney := []rastrillo.Field(nil)
	if groupHasValidation {
		otherMoney = moneyFields(other)
	}

	var b strings.Builder
	b.WriteString("import (\n")
	if groupHasValidation {
		b.WriteString("\t\"errors\"\n")
	}
	b.WriteString("\t\"fmt\"\n")
	b.WriteString("\t\"net/http\"\n\n")
	// The operation refuses an amount whose currency is not the one the
	// field now holds, for either store kind (opsgen's settle). This
	// action sends only the currency it read, so that refusal means the
	// record changed after the read. The action then runs again: once
	// more to save, which re-reads the record so the form's currency
	// echo, no longer matching, shows the person money_currency_changed
	// with their text kept, as a currency that changed before the POST
	// does, not a 500. A second refusal in the same request (the
	// currency moved back, then away again) runs it a last time only to
	// show the form: re-read, never save, and show the conflict on the
	// group's money fields with the text the person typed, whatever the
	// re-parse makes of a record that moved.
	retries := len(money) > 0
	b.WriteString(actionImportLines(r, needsForm, len(group) > 0 || groupHasValidation, groupHasValidation))
	if groupHasValidation {
		b.WriteString(basepathImport)
	}
	fmt.Fprintf(&b, "\t%s %q\n", ops, path)
	b.WriteString(")\n")

	fmt.Fprintf(&b, "\n// Handle is POST %s/{id}/edit-%s.\n", r.Route, strings.ToLower(groupName))
	if retries {
		b.WriteString("func Handle(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request) {\n\thandle(ctx, w, r, 0)\n}\n\n" +
			wrapComment(handleDoc) +
			"func handle(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request, try int) {\n")
	} else {
		b.WriteString("func Handle(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request) {\n")
	}
	b.WriteString(`	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request.", http.StatusBadRequest)
		return
	}
`)
	if groupHasValidation {
		b.WriteString(getStmts(r, ops) + "\n")
	} else {
		b.WriteString("id, ok := view.ParseID(r)\nif !ok {\nview.NotFound(ctx, w, r)\nreturn\n}\n\n")
	}

	b.WriteString(codeStmts(group))
	if len(group) > 0 {
		b.WriteString(parseCall(r, group, true))
	}
	fmt.Fprintf(&b, "in := %s.UpdateInput{\n%s}\n", ops, inputLines(group, func(f rastrillo.Field) string { return lowerFirst(f.Name) + "Code" }))

	if groupHasValidation {
		b.WriteString("reshow := func(errs map[string]string) {\n")
		b.WriteString("fields := " + fieldsMapLiteral(r, "n", false) + "\n")
		b.WriteString("for k, v := range p.Echo() {\nfields[k] = v\n}\n")
		b.WriteString(echoStmts(group, true))
		b.WriteString(seedStmts(otherMoney, "id", recordMinor, recordCode))
		if retries {
			// The last run's own parse cannot be trusted to say what
			// happened: the record moved twice, so it may refuse the seed
			// (an amount cleared in between leaves nothing to match) and
			// reseed the input blank, or refuse nothing at all. What the
			// person must see is the conflict, with the text they typed,
			// which the first run already read in its locale, beside the
			// record's current currency. Nothing in that run saves.
			b.WriteString("if try == 2 {\n")
			for _, f := range money {
				l := lowerFirst(f.Name)
				fmt.Fprintf(&b, "%sInput = form.MoneyInput{Value: p.Echo()[%q], Locale: r.PostFormValue(%q), Currency: %sCode}\n", l, f.Name, f.Name+".locale", l)
			}
			b.WriteString("}\n")
		}
		fmt.Fprintf(&b, "view.Render(ctx, w, %q, http.StatusBadRequest, formView{\n", r.Name+"/form")
		b.WriteString(localizedField + "IsNew: false,\nFields: fields,\nErrors: errs,\nErrorArgs: p.ErrorArgs(),\n")
		if r.HasMoney() {
			b.WriteString("Money: " + moneyMapLiteral(formFields(r)) + ",\n")
		}
		fmt.Fprintf(&b, "BasicsAction: basepath.Path(r, fmt.Sprintf(%q, id)),\n", r.Route+"/%d/edit-basics")
		if hasAdvanced {
			fmt.Fprintf(&b, "AdvancedAction: basepath.Path(r, fmt.Sprintf(%q, id)),\n", r.Route+"/%d/edit-advanced")
		}
		b.WriteString("})\n}\n")
	}
	if retries {
		var keys []string
		for _, f := range money {
			keys = append(keys, fmt.Sprintf("%q: %q", f.Name, moneyCurrencyChangedKey))
		}
		fmt.Fprintf(&b, "if !p.OK() || try == 2 {\nerrs := %s.ValidateUpdate(in)\n%sif try == 2 {\nerrs = map[string]string{%s}\n}\nreshow(errs)\nreturn\n}\n", ops, mergeParseErrors, strings.Join(keys, ", "))
	}

	// err is new in a group with no Get above, and reassigned after one.
	assign := ":="
	if groupHasValidation {
		assign = "="
	}
	fmt.Fprintf(&b, "\n_, err %s %s.Update(r.Context(), ctx, id, in)\n", assign, ops)
	switch {
	case retries:
		b.WriteString("var invalid op.Invalid\nswitch {\ncase errors.As(err, &invalid):\nreshow(invalid.Fields)\nreturn\ncase errors.As(err, new(op.Conflict)):\nhandle(ctx, w, r, try+1)\nreturn\ncase err != nil:\n")
		fmt.Fprintf(&b, "view.Refuse(ctx, w, r, %q, err)\nreturn\n}\n", failWhat(r, "updating "+r.Name))
	case groupHasValidation:
		b.WriteString("var invalid op.Invalid\nif errors.As(err, &invalid) {\nreshow(invalid.Fields)\nreturn\n}\n")
		b.WriteString(refuseStmts(r, "updating "+r.Name))
	default:
		b.WriteString(refuseStmts(r, "updating "+r.Name))
	}
	fmt.Fprintf(&b, "http.Redirect(w, r, fmt.Sprintf(%q, id), http.StatusSeeOther)\n}\n", r.Route+"/%d")

	b.WriteString(inputHelpers(group))
	if len(money)+len(otherMoney) > 0 {
		b.WriteString(seedHelper(r) + codeHelper(r) + minorHelper)
	}
	if groupHasValidation {
		b.WriteString(formViewType(r))
	}
	return b.String()
}

// handleDoc is the doc comment on a money update's handle, a draft for
// the copy review (docs/superpowers/plans/2026-10-05-json-api-copy.md).
const handleDoc = "handle is Handle's body. When the operation refuses the save because the record's currency changed, it runs again: once more to save, and then only to show the form with that message."

// moneyCurrencyChangedKey is form's rastrillo.ui.money_currency_changed,
// which the last run sets itself on the group's money fields.
const moneyCurrencyChangedKey = "rastrillo.ui.money_currency_changed"

func actionEditBasicsPOST(r rastrillo.Resource, module string) string {
	return updatePOST(r, module, r.Form.Basics, r.Form.Advanced, "Basics")
}

func actionEditAdvancedPOST(r rastrillo.Resource, module string) string {
	return updatePOST(r, module, r.Form.Advanced, r.Form.Basics, "Advanced")
}

// ── field parsing shared by Create and both Update actions ─────────

// parseCall emits the one form.Parse call a POST action opens with —
// the same documented helper a hand-written app uses (form.Field's
// kinds carry the parse/trim/echo rules that used to be stamped here
// as per-field statements). A text field carries no Required: that
// check is the operation's, so both doors refuse the same blank. A
// money field's is moneyField's: edit says whether it reads the
// record's currency and amount or the resource default.
func parseCall(r rastrillo.Resource, fields []rastrillo.Field, edit bool) string {
	var b strings.Builder
	b.WriteString("p := form.Parse(r,\n")
	for _, f := range fields {
		if f.Kind == rastrillo.Money {
			b.WriteString(moneyField(f, edit, r) + ",\n")
			continue
		}
		parts := []string{fmt.Sprintf("Name: %q", f.Name)}
		if f.Kind == rastrillo.Textarea {
			parts = append(parts, "Kind: form.Textarea")
		}
		b.WriteString("form.Field{" + strings.Join(parts, ", ") + "},\n")
	}
	b.WriteString(")\n")
	return b.String()
}

// fieldsCanFail reports whether any field can be refused — a Money
// parse, or the operation's Required check — i.e. whether the action
// needs a re-render at all.
func fieldsCanFail(fields []rastrillo.Field) bool {
	for _, f := range fields {
		if f.Required || f.Kind == rastrillo.Money {
			return true
		}
	}
	return false
}

// ── [id]/delete.GET (Confirm) ───────────────────────────────────────

// actionDeleteGET renders the delete confirm page — §9's rule that
// every destructive action is its own URL first: a GET that never
// mutates, showing the record, the question, and a form whose POST is
// the only thing that deletes.
func actionDeleteGET(r rastrillo.Resource, module string) string {
	ops, path := opsImport(r, module)
	main, _, _ := mainSubColumns(r)
	showsMoney := main.Kind == rastrillo.Money

	var b strings.Builder
	b.WriteString("import (\n")
	b.WriteString("\t\"fmt\"\n")
	b.WriteString("\t\"net/http\"\n\n")
	b.WriteString(actionImportLines(r, false, showsMoney, true))
	b.WriteString(basepathImport)
	if showsMoney {
		b.WriteString(currencyImport)
	}
	fmt.Fprintf(&b, "\t%s %q\n", ops, path)
	b.WriteString(")\n")

	fmt.Fprintf(&b, "\n// Handle is GET %s/{id}/delete: the confirm page. A GET never\n", r.Route)
	b.WriteString("// mutates; the delete itself is the sibling POST.\n")
	b.WriteString("func Handle(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request) {\n")
	b.WriteString(getStmts(r, ops))

	title := displayStmts(&b, "n", main, "id")
	fmt.Fprintf(&b, `
view.Render(ctx, w, %q, http.StatusOK, confirmView{
	%sTitle:      %s,
	DeleteHref: basepath.Path(r, fmt.Sprintf(%q, id)),
	CancelHref: basepath.Path(r, fmt.Sprintf(%q, id)),
})
}
`, r.Name+"/confirm", localizedField, title, r.Route+"/%d/delete", r.Route+"/%d")

	if showsMoney {
		b.WriteString(displayHelper(r))
	}
	b.WriteString(confirmViewType)
	return b.String()
}

const confirmViewType = `
type confirmView struct {
	ui.Localized
	Title      string
	DeleteHref string
	CancelHref string
}
`

// ── [id]/delete.POST ────────────────────────────────────────────────

// actionDeletePOST is the delete itself: the operation's Delete, whose
// op.NotFound for a missing row answers 404 (never a silent no-op
// success) and writes nothing, then 303 back to the list.
func actionDeletePOST(r rastrillo.Resource, module string) string {
	ops, path := opsImport(r, module)

	var b strings.Builder
	b.WriteString("import (\n")
	b.WriteString("\t\"net/http\"\n\n")
	b.WriteString(actionImportLines(r, false, false, false))
	fmt.Fprintf(&b, "\t%s %q\n", ops, path)
	b.WriteString(")\n")

	fmt.Fprintf(&b, "\n// Handle is POST %s/{id}/delete.\n", r.Route)
	b.WriteString(`func Handle(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request) {
	id, ok := view.ParseID(r)
	if !ok {
		view.NotFound(ctx, w, r)
		return
	}
`)
	fmt.Fprintf(&b, "err := %s.Delete(r.Context(), ctx, id)\n", ops)
	b.WriteString(refuseStmts(r, "deleting "+r.Name))
	fmt.Fprintf(&b, "http.Redirect(w, r, %q, http.StatusSeeOther)\n}\n", r.Route)

	return b.String()
}
