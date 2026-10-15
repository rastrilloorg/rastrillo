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
//   - index.GET: parses q (only when List.Search is set — and only
//     really used by the store when there is at least one eligible
//     text/textarea List column; see searchColumns), one query param
//     per filterFields(r) entry (store.go's dedup of bare List.Filter
//     and List.Filters[].Field — named by its sqlName, e.g. ?title=),
//     and page (default 1). A field ALSO declared via List.Filters
//     (capped at one entry by Validate) gets its raw query value run
//     through a generated normalize<Field> first — unknown/absent
//     collapses to "" (all), never a 400 — and the action builds a
//     filterView (SummaryKey/AriaField/Items, every label a T KEY, never
//     a resolved string) the render call's listView.Filter carries;
//     list.html renders it as an inline dropdown (see templates.go).
//     A bare-only List.Filter field (no List.Filters counterpart) still
//     gets its query param parsed/carried/bound as a raw passthrough,
//     exactly as before this — it has no enumerable Values to build a
//     control or a normalizer from. Builds Rows from List.Columns[0]
//     (Main) and, if declared, List.Columns[1] (Sub) — Money formatted
//     in the reader's locale by the money helper (moneyactions.go),
//     everything else the raw column value. Pagination has no gap-collapsing (v1
//     simplification: every page number renders; see the report for
//     the tradeoff).
//   - index.POST (create): parses every Form.Basics + Form.Advanced
//     field in one form.Parse call — form.Field's kinds carry the rules,
//     documented on the helper itself. A Money field is read in the
//     resource's currency, in the locale the form echoes (spec §5), and
//     any field the manifest marks Required is checked for blankness
//     ("0" is a valid required amount). Any failure re-renders
//     "<name>/form" (IsNew: true) at 400 with the field's message in
//     Errors (its values in ErrorArgs) and every field's raw submitted
//     text preserved — in Fields, and for money in Money with the
//     locale it was read in — never reformatted, so a typo stays
//     exactly as typed for correction. Success stamps both timestamps
//     with the same UTC RFC3339 "now" and redirects 303 to Show.
//   - [id]/index.GET (show): Get-or-404, then every declared field
//     (columns(r) order) into Fields, Money formatted in the reader's
//     locale.
//   - new.GET / [id]/edit.GET: render "<name>/form" with zero values
//     (New) or the record's current values (Edit, Get-or-404 first);
//     money inputs come from form.SeedMoney in the render locale.
//   - [id]/edit-basics.POST / [id]/edit-advanced.POST: Get-or-404 (to
//     answer 404 before ever attempting an UPDATE against a missing
//     row, and — only when this field group has a Money field or a
//     Required field — to source the OTHER group's current values for
//     the 400 re-render), then parse and update only that field group.
//     A Money parse failure or a Required-blank failure re-renders
//     "<name>/form" (IsNew: false) at 400 with Fields seeded from the
//     fetched record and overridden with this group's just-submitted
//     raw text; this group's money inputs echo what was typed, the
//     other group's are seeded from the record. A field group with no Money field and no Required
//     field at all has no validation branch to speak of — generation
//     time already knows no check here can fail, so none is emitted
//     (the same "bake what the manifest already decided" discipline
//     templates.go uses for the Advanced-form and Search gates).
//   - scope = "user" (rastrillo.UserScoped): every action except
//     new.GET (which touches no data) opens by resolving the session
//     (sessions.Current — so scoped routes mount behind
//     sessions.Require / sessions.Middleware / auth.RequireSession)
//     and answers 403 signed out; every store call binds
//     Owner: sess.Subject. List/Count carry the always-on owner
//     clause, Create stamps the owner, and every per-record query
//     keys on id AND owner (store.go's recordKey) — so someone else's
//     id is sql.ErrNoRows and answers the same 404 a missing row
//     does, the scope package's 404-not-403 discipline. An unscoped
//     resource's output is byte-identical to before Scope existed
//     (the fixture goldens pin this).
package generate

import (
	"bufio"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"strings"

	"amadan.net/rastrillo/rastrillo"
)

// actionSpec is one of the (up to) seven files EmitActions considers
// for a resource.
type actionSpec struct {
	dir    string // slash-separated, relative to actions/, e.g. "admin/notes" or "admin/notes/[id]"
	name   string // filename stem: "index", "new", "edit", "edit-basics", "edit-advanced"
	method string
	build  func(rastrillo.Resource, string) string // (r, module) -> unformatted Go source
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
		{resDir, "index", "GET", actionIndexGET},
		{resDir, "index", "POST", actionIndexPOST},
		{resDir, "new", "GET", actionNewGET},
		{idDir, "index", "GET", actionShowGET},
		{idDir, "edit", "GET", actionEditGET},
		{idDir, "edit-basics", "POST", actionEditBasicsPOST},
		{idDir, "delete", "GET", actionDeleteGET},
		{idDir, "delete", "POST", actionDeletePOST},
	}
	if len(r.Form.Advanced) > 0 {
		specs = append(specs, actionSpec{idDir, "edit-advanced", "POST", actionEditAdvancedPOST})
	}
	return specs
}

// actionsModulePath reads the module directive from appRoot's go.mod.
// Hand-rolled rather than shared across packages, matching this
// codebase's own precedent (internal/manifest/goeval.go's
// readModulePath, cmd/rastrillo/modpath.go's modulePath): the module
// line is one fixed shape, and each package that needs it keeps its
// own unexported copy.
func actionsModulePath(appRoot string) (string, error) {
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

// storeImport returns the store package's import alias and path for r:
// alias equals the package name sqlc.yaml told sqlc to generate
// (<name>store — see store.go's sqlcYAML), written explicitly rather
// than relying on the compiler inferring it from the package clause,
// matching Router()'s own explicit-alias style in generate.go.
func storeImport(r rastrillo.Resource, module string) (alias, path string) {
	return r.Name + "store", module + "/gen/store/" + r.Name
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

// filterVar is one filterFields(r) entry's generated shape: a
// query-string key (its sqlName) and a local variable name ("filter" +
// the declared name, used as-is — not reconstructed through
// sqlName/pascalCase). Param — "Filter"+Field — has to spell exactly
// what sqlc names the CountXParams/ListXParams struct field for this
// filter, and sqlc derives that name as pascalCase(sqlName(f)), NOT f
// itself. Those two only agree for every identPattern-valid f because
// rastrillo.Resource.Validate additionally rejects any field/column
// name where pascalCase(sqlName(name)) != name (see manifest.go) —
// without that guarantee a name like "title" or "IPAddress" would
// still pass identPattern but generate a Param field the real sqlc
// struct doesn't have. This comment used to claim sqlName+pascalCase
// "round-trips any identPattern-valid identifier", which is false on
// its own (see manifest.go's isCanonicalIdent doc) and only became true
// once Validate started enforcing it.
type filterVar struct {
	Field string // declared name, e.g. "Title"
	Query string // sqlName(Field), the URL query key, e.g. "title"
	Var   string // "filterTitle"
	Param string // the store Params field name, "Filter"+Field
}

// filterVars walks filterFields(r) — the deduped union of bare
// List.Filter and List.Filters[].Field (store.go) — rather than
// r.List.Filter alone, so a field declared ONLY via List.Filters (with
// enumerable Values, no bare List.Filter entry) still gets its query
// param parsed, carried and bound to the store's filter_<col> arg; the
// same WHERE clause (whereClauses in store.go) already honors it either
// way, so the action-side param handling has to keep up. A field
// present in both collections (the filteredFixtureResource shape:
// declared as both a bare Filter and a Filters[] with Values) appears
// here exactly once.
func filterVars(r rastrillo.Resource) []filterVar {
	var out []filterVar
	for _, f := range filterFields(r) {
		out = append(out, filterVar{
			Field: f,
			Query: sqlName(f),
			Var:   "filter" + f,
			Param: "Filter" + f,
		})
	}
	return out
}

// declaredFilter returns r's single List.Filters entry, if declared:
// Validate caps List.Filters at exactly one (len(r.List.Filters) > 1 is
// rejected — manifest.go), so "declared" here always means exactly one
// or none. Field always matches one of filterVars(r)'s own Field
// values, since filterFields unions List.Filter and List.Filters[].Field
// (store.go) — a Filters entry can never be absent from that union.
// Values is its enumerable dropdown choices, already validated non-empty
// and deduped by Validate.
func declaredFilter(r rastrillo.Resource) (field string, values []string, ok bool) {
	if len(r.List.Filters) != 1 {
		return "", nil, false
	}
	return r.List.Filters[0].Field, r.List.Filters[0].Values, true
}

// ── form/view call-site plumbing, shared across all seven files for a
// given resource ────────────────────────────────────────────────────

// failWhat renders the "what" argument a view.Fail call site passes:
// the shared view.Fail (form/view — Task 6) can't close over the
// resource name the way the old stamped-per-file fail helper did, so
// the "<name>: " prefix that used to live in the helper's own log line
// is folded into this string instead. Net effect on the emitted log
// line is unchanged: fail(ctx, w, "creating notes", err) under the old
// stamped helper logged the same line view.Fail(ctx, w, r, "notes:
// creating notes", err) logs now.
func failWhat(r rastrillo.Resource, what string) string {
	return r.Name + ": " + what
}

// actionImportLines renders the module import lines every generated
// action needs (rastrillo, always; view, always — every one of the
// eight/nine files calls at least one of view.Fail/view.Render/
// view.ParseID unconditionally) plus form when needsForm — an action
// with no Money field anywhere it reads/writes must not import form
// unused (go vet would reject it) — plus sessions when the resource is
// scoped AND this action resolves the owner (needsSession: every
// scoped action except new.GET, which touches no data and so emits no
// sessionStmts to use the import). Line order within the block doesn't
// matter: go/format sorts import specs within a contiguous group
// alphabetically, same as gofmt. hasView adds ui, for the ui.Localized
// every view type embeds and the ui.ForRequest(r) every view.Render call
// sets (localizedField); an action that declares no view must not
// import it unused.
func actionImportLines(r rastrillo.Resource, needsForm, needsSession, hasView bool) string {
	var b strings.Builder
	b.WriteString("\t\"amadan.net/rastrillo/rastrillo\"\n")
	if needsForm {
		b.WriteString("\t\"amadan.net/rastrillo/rastrillo/form\"\n")
	}
	if scoped(r) && needsSession {
		b.WriteString("\t\"amadan.net/rastrillo/rastrillo/sessions\"\n")
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

// ── owner scoping (scope = "user") plumbing, shared by every action ──

// sessionStmts renders the statements a scoped Handle opens with:
// resolve the session, refuse a signed-out caller. 403 rather than a
// redirect — the mounting contract (SKILL.md, manifest/README) puts
// scoped routes behind sessions.Require / auth.RequireSession, which
// already redirect page requests; this refusal is the defense-in-depth
// backstop for an app that forgot, matching sessions' own answer for
// a non-page request. It goes through view.Forbidden, so the app's
// error page (or a JSON client's JSON) answers it the same way
// view.NotFound answers a missing record. Empty for an unscoped
// resource.
func sessionStmts(r rastrillo.Resource) string {
	if !scoped(r) {
		return ""
	}
	return `sess, ok := sessions.Current(r)
	if !ok {
		view.Forbidden(ctx, w, r)
		return
	}
`
}

// ownerParamLine is the Owner bind a scoped store call carries; empty
// for an unscoped resource, so call sites splice it unconditionally.
func ownerParamLine(r rastrillo.Resource) string {
	if !scoped(r) {
		return ""
	}
	return "Owner: sess.Subject,\n"
}

// recordArg renders the argument a per-record query (Get<Singular>,
// Delete<Singular>) takes: the bare id for an unscoped resource — one
// bind parameter, passed bare under sqlc's convention (see the
// countBinds comment in actionIndexGET) — or the two-field Params
// struct for a scoped one, whose id-AND-owner WHERE (store.go's
// recordKey) is what turns someone else's row into sql.ErrNoRows and
// so into the same 404 a missing row answers.
func recordArg(r rastrillo.Resource, alias, query, singular string) string {
	if scoped(r) {
		return fmt.Sprintf("%s.%s%sParams{ID: id, Owner: sess.Subject}", alias, query, singular)
	}
	return "id"
}

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

// ── index.GET (List) ────────────────────────────────────────────────

func actionIndexGET(r rastrillo.Resource, module string) string {
	alias, path := storeImport(r, module)
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
	b.WriteString(actionImportLines(r, false, true, true))
	if showsMoney {
		b.WriteString(currencyImport)
	}
	fmt.Fprintf(&b, "\t%s %q\n", alias, path)
	b.WriteString(")\n")

	fmt.Fprintf(&b, "\n// Handle is GET %s.\n", r.Route)
	b.WriteString("func Handle(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request) {\n")
	b.WriteString(sessionStmts(r))

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
offset := (page - 1) * pageSize
`)

	b.WriteString("\nvar carry [][2]string\n")
	for _, fv := range fvars {
		fmt.Fprintf(&b, "if %s != \"\" {\n\tcarry = append(carry, [2]string{%q, %s})\n}\n", fv.Var, fv.Query, fv.Var)
	}

	fmt.Fprintf(&b, "\nstore := %s.New(ctx.DB)\n", alias)
	plural := pascalCase(r.Name)

	// CountX has no tail clause the way ListX's LIMIT/OFFSET always
	// gives it one (ListX always binds at least page_limit/page_offset,
	// so it always gets a real Params struct — sqlc's sqlite codegen
	// only elides the struct at zero OR exactly one bind parameter,
	// and List never has fewer than two). CountX has no such floor: its
	// bind-parameter count is exactly len(fvars) plus one more if
	// searchParam, and sqlc's own convention (verified against a real
	// `sqlc generate` run — see the blog's committed
	// gen/store/posts/queries.sql.go, a search-only resource, whose
	// CountPosts takes a bare `search interface{}`, no CountPostsParams)
	// is:
	//   - zero bind params  -> no argument at all, not an empty struct
	//   - exactly one       -> that one value passed bare, not wrapped
	//     in a one-field struct
	//   - two or more       -> the Params struct, as below
	// Emitting the Params struct whenever searchParam || len(fvars) > 0
	// (as an earlier draft did) is wrong for the one-param case and
	// fails to compile against real sqlc output — a resource with only
	// Search, or only a single Filter, has no way to also declare a
	// second Filter that would push it into the struct case.
	countBinds := len(fvars)
	if searchParam {
		countBinds++
	}
	if scoped(r) {
		countBinds++ // the always-on owner clause (store.go's whereClauses)
	}
	switch {
	case countBinds == 0:
		b.WriteString("total, err := store.Count" + plural + "(r.Context())\n")
	case countBinds == 1:
		var arg string
		switch {
		case scoped(r):
			arg = "sess.Subject"
		case searchParam:
			arg = searchExpr
		default:
			arg = fvars[0].Var
		}
		b.WriteString("total, err := store.Count" + plural + "(r.Context(), " + arg + ")\n")
	default:
		b.WriteString("total, err := store.Count" + plural + "(r.Context(), " + alias + ".Count" + plural + "Params{\n")
		b.WriteString(ownerParamLine(r))
		if searchParam {
			b.WriteString("Search: " + searchExpr + ",\n")
		}
		for _, fv := range fvars {
			fmt.Fprintf(&b, "%s: %s,\n", fv.Param, fv.Var)
		}
		b.WriteString("})\n")
	}
	fmt.Fprintf(&b, "if err != nil {\n\tview.Fail(ctx, w, r, %q, err)\n\treturn\n}\n", failWhat(r, "counting "+r.Name))

	b.WriteString("rows, err := store.List" + plural + "(r.Context(), " + alias + ".List" + plural + "Params{\n")
	b.WriteString(ownerParamLine(r))
	if searchParam {
		b.WriteString("Search: " + searchExpr + ",\n")
	}
	for _, fv := range fvars {
		fmt.Fprintf(&b, "%s: %s,\n", fv.Param, fv.Var)
	}
	b.WriteString("PageOffset: int64(offset),\nPageLimit: pageSize,\n")
	b.WriteString("})\n")
	fmt.Fprintf(&b, "if err != nil {\n\tview.Fail(ctx, w, r, %q, err)\n\treturn\n}\n", failWhat(r, "loading "+r.Name))

	b.WriteString("\nitems := make([]listRow, 0, len(rows))\nfor _, n := range rows {\n")
	mainExpr := displayStmts(&b, "n", main, "n.ID")
	subExpr := `""`
	if hasSub {
		subExpr = displayStmts(&b, "n", sub, "n.ID")
	}
	fmt.Fprintf(&b, `items = append(items, listRow{
		Href: fmt.Sprintf(%q, n.ID),
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
		prev = listPageItem{Label: "Previous", Rel: "prev", Href: href(` + searchExprForHref(searchDeclared) + `, carry, page-1)}
	}
	pageItems = append(pageItems, prev)
	for n := 1; n <= totalPages; n++ {
		if n == page {
			pageItems = append(pageItems, listPageItem{Label: strconv.Itoa(n), Current: true})
		} else {
			pageItems = append(pageItems, listPageItem{Label: strconv.Itoa(n), Href: href(` + searchExprForHref(searchDeclared) + `, carry, n)})
		}
	}
	next := listPageItem{Label: "Next", Rel: "next", Disabled: true}
	if page < totalPages {
		next = listPageItem{Label: "Next", Rel: "next", Href: href(` + searchExprForHref(searchDeclared) + `, carry, page+1)}
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
	fmt.Fprintf(&b, "{Href: filter%sHref(%s, \"\"), LabelKey: filter%sLabelKey(\"\"), Current: %s == \"\"},\n",
		fv.Field, searchExpr, fv.Field, fv.Var)
	for _, v := range values {
		fmt.Fprintf(&b, "{Href: filter%sHref(%s, %q), LabelKey: filter%sLabelKey(%q), Current: %s == %q},\n",
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
	alias, path := storeImport(r, module)
	singular := singularPascal(r.Name)

	var order []rastrillo.Field
	order = append(order, r.Form.Basics...)
	order = append(order, r.Form.Advanced...)
	byName := map[string]rastrillo.Field{}
	for _, f := range order {
		byName[f.Name] = f
	}

	var paramLines []string
	for _, c := range columns(r) {
		if f, ok := byName[c.Name]; ok {
			paramLines = append(paramLines, storeParamLine(f))
			continue
		}
		// A List-only column with no matching Form field: Create has
		// no submitted value for it, so it gets the zero value — for
		// money, nil in both columns, an empty field. (A well-formed
		// manifest gives every List column a Form field too; this is a
		// fallback, not the expected shape.)
		if c.Kind != rastrillo.Money {
			paramLines = append(paramLines, fmt.Sprintf("%s: \"\",\n", c.Name))
		}
	}

	canFail := fieldsCanFail(order)

	var b strings.Builder
	b.WriteString("import (\n")
	b.WriteString("\t\"fmt\"\n")
	b.WriteString("\t\"net/http\"\n")
	b.WriteString("\t\"time\"\n\n")
	b.WriteString(actionImportLines(r, len(order) > 0, true, true))
	fmt.Fprintf(&b, "\t%s %q\n", alias, path)
	b.WriteString(")\n")

	fmt.Fprintf(&b, "\n// Handle is POST %s.\n", r.Route)
	b.WriteString(`func Handle(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request.", http.StatusBadRequest)
		return
	}

`)
	b.WriteString(sessionStmts(r))
	if len(order) > 0 {
		b.WriteString(parseCall(r, order, false))
	}

	if canFail {
		b.WriteString("if !p.OK() {\n")
		b.WriteString(echoStmts(order, false))
		fmt.Fprintf(&b, "view.Render(ctx, w, %q, http.StatusBadRequest, formView{\n", r.Name+"/form")
		b.WriteString(localizedField + "IsNew: true,\nFields: p.Echo(),\nErrors: p.Errors(),\nErrorArgs: p.ErrorArgs(),\n")
		if r.HasMoney() {
			b.WriteString("Money: " + moneyMapLiteral(order) + ",\n")
		}
		b.WriteString("})\nreturn\n}\n")
	}

	b.WriteString(writeStmts(order, func(rastrillo.Field) string { return fmt.Sprintf("%q", r.Currency) }))
	b.WriteString("\nnow := time.Now().UTC().Format(time.RFC3339)\n")
	fmt.Fprintf(&b, "store := %s.New(ctx.DB)\n", alias)
	fmt.Fprintf(&b, "id, err := store.Create%s(r.Context(), %s.Create%sParams{\n", singular, alias, singular)
	b.WriteString(ownerParamLine(r))
	for _, l := range paramLines {
		b.WriteString(l)
	}
	b.WriteString("Now: now,\n")
	b.WriteString("})\n")
	fmt.Fprintf(&b, "if err != nil {\n\tview.Fail(ctx, w, r, %q, err)\n\treturn\n}\n", failWhat(r, "creating "+r.Name))
	fmt.Fprintf(&b, "http.Redirect(w, r, fmt.Sprintf(%q, id), http.StatusSeeOther)\n}\n", r.Route+"/%d")

	if len(moneyFields(order)) > 0 {
		b.WriteString(amountHelper)
	}
	b.WriteString(formViewType(r))
	return b.String()
}

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

// ── [id]/index.GET (Show) ───────────────────────────────────────────

func actionShowGET(r rastrillo.Resource, module string) string {
	alias, path := storeImport(r, module)
	singular := singularPascal(r.Name)
	main, _, _ := mainSubColumns(r)
	showsMoney := hasMoneyColumn(columns(r))

	var b strings.Builder
	b.WriteString("import (\n")
	b.WriteString("\t\"database/sql\"\n")
	b.WriteString("\t\"errors\"\n")
	b.WriteString("\t\"fmt\"\n")
	b.WriteString("\t\"net/http\"\n\n")
	b.WriteString(actionImportLines(r, false, true, true))
	if showsMoney {
		b.WriteString(currencyImport)
	}
	fmt.Fprintf(&b, "\t%s %q\n", alias, path)
	b.WriteString(")\n")

	fmt.Fprintf(&b, "\n// Handle is GET %s/{id}.\n", r.Route)
	b.WriteString(`func Handle(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request) {
	id, ok := view.ParseID(r)
	if !ok {
		view.NotFound(ctx, w, r)
		return
	}
`)
	b.WriteString(sessionStmts(r))
	fmt.Fprintf(&b, "store := %s.New(ctx.DB)\n", alias)
	fmt.Fprintf(&b, "n, err := store.Get%s(r.Context(), %s)\n", singular, recordArg(r, alias, "Get", singular))
	b.WriteString(`if errors.Is(err, sql.ErrNoRows) {
		view.NotFound(ctx, w, r)
		return
	}
`)
	fmt.Fprintf(&b, "if err != nil {\n\tview.Fail(ctx, w, r, %q, err)\n\treturn\n}\n", failWhat(r, "loading "+r.Name))

	title := "n." + main.Name
	for _, c := range columns(r) {
		if e := displayStmts(&b, "n", c, "id"); c.Name == main.Name {
			title = e
		}
	}
	fmt.Fprintf(&b, `
view.Render(ctx, w, %q, http.StatusOK, showView{
	%sTitle:    %s,
	EditHref: fmt.Sprintf(%q, id),
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
	alias, path := storeImport(r, module)
	singular := singularPascal(r.Name)
	hasAdvanced := len(r.Form.Advanced) > 0
	money := moneyFields(formFields(r))

	var b strings.Builder
	b.WriteString("import (\n")
	b.WriteString("\t\"database/sql\"\n")
	b.WriteString("\t\"errors\"\n")
	b.WriteString("\t\"fmt\"\n")
	b.WriteString("\t\"net/http\"\n\n")
	b.WriteString(actionImportLines(r, r.HasMoney(), true, true))
	fmt.Fprintf(&b, "\t%s %q\n", alias, path)
	b.WriteString(")\n")

	fmt.Fprintf(&b, "\n// Handle is GET %s/{id}/edit.\n", r.Route)
	b.WriteString(`func Handle(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request) {
	id, ok := view.ParseID(r)
	if !ok {
		view.NotFound(ctx, w, r)
		return
	}
`)
	b.WriteString(sessionStmts(r))
	fmt.Fprintf(&b, "store := %s.New(ctx.DB)\n", alias)
	fmt.Fprintf(&b, "n, err := store.Get%s(r.Context(), %s)\n", singular, recordArg(r, alias, "Get", singular))
	b.WriteString(`if errors.Is(err, sql.ErrNoRows) {
		view.NotFound(ctx, w, r)
		return
	}
`)
	fmt.Fprintf(&b, "if err != nil {\n\tview.Fail(ctx, w, r, %q, err)\n\treturn\n}\n", failWhat(r, "loading "+r.Name))

	b.WriteString(seedStmts(money, "id",
		func(f rastrillo.Field) string { return "n." + f.Name + "Minor" },
		func(f rastrillo.Field) string { return "currencyOf(n." + f.Name + "Currency)" }))
	fmt.Fprintf(&b, `
view.Render(ctx, w, %q, http.StatusOK, formView{
	%sIsNew:  false,
	Fields: %s,
`, r.Name+"/form", localizedField, fieldsMapLiteral(r, "n", false))
	if r.HasMoney() {
		b.WriteString("Money: " + moneyMapLiteral(money) + ",\n")
	}
	fmt.Fprintf(&b, "BasicsAction: fmt.Sprintf(%q, id),\n", r.Route+"/%d/edit-basics")
	if hasAdvanced {
		fmt.Fprintf(&b, "AdvancedAction: fmt.Sprintf(%q, id),\n", r.Route+"/%d/edit-advanced")
	}
	b.WriteString("})\n}\n")

	if len(money) > 0 {
		b.WriteString(seedHelper(r) + codeHelper(r))
	}
	b.WriteString(formViewType(r))
	return b.String()
}

// ── [id]/edit-basics.POST and [id]/edit-advanced.POST ──────────────

// updatePOST builds edit-basics.POST or edit-advanced.POST: they are
// the same shape (Get-or-404, parse this group's fields, update,
// redirect — or, only when this group declares a Money field or a
// Required field, a validation branch), parameterized on which field
// group and which sqlc Update query own it.
//
// other is the other group's fields: a failed POST seeds its money
// inputs from the record, in the render locale, since this POST did
// not submit them.
func updatePOST(r rastrillo.Resource, module string, group, other []rastrillo.Field, groupName string) string {
	alias, path := storeImport(r, module)
	singular := singularPascal(r.Name)
	hasAdvanced := len(r.Form.Advanced) > 0

	// groupHasValidation is true when ANY field in this group can fail
	// validation — a Money parse (any Money field) or a Required-blank
	// check (any Required Text/Textarea/Money field). A group with
	// neither has no way to fail, so no validation branch and no `n`
	// (the fetched record) is needed at all — Get-or-404 still runs
	// (404 must win over a validation failure), its result just
	// discarded.
	groupHasValidation := fieldsCanFail(group)

	var paramLines []string
	for _, f := range group {
		paramLines = append(paramLines, storeParamLine(f))
	}

	needsForm := len(group) > 0 || (groupHasValidation && r.HasMoney())
	money := moneyFields(group)
	// The record's other money inputs are drawn only on a failed POST.
	otherMoney := []rastrillo.Field(nil)
	if groupHasValidation {
		otherMoney = moneyFields(other)
	}

	var b strings.Builder
	b.WriteString("import (\n")
	b.WriteString("\t\"database/sql\"\n")
	b.WriteString("\t\"errors\"\n")
	b.WriteString("\t\"fmt\"\n")
	b.WriteString("\t\"net/http\"\n")
	b.WriteString("\t\"time\"\n\n")
	// A mergeable store refuses an amount whose currency is not the one
	// its field now holds. This action writes only the currency it read,
	// so that refusal means the record changed after the read. The
	// action then runs again: once more to save, which re-reads the
	// record so the form's currency echo, no longer matching, shows the
	// person money_currency_changed with their text kept, as a currency
	// that changed before the POST does, not a 500. A second refusal in
	// the same request (the currency moved back, then away again) runs
	// it a last time only to show the form: re-read, never save, and
	// show the conflict on the group's money fields with the text the
	// person typed, whatever the re-parse makes of a record that moved.
	retries := r.Store == rastrillo.Mergeable && len(money) > 0
	b.WriteString(actionImportLines(r, needsForm, true, groupHasValidation))
	if retries {
		b.WriteString("\t\"amadan.net/rastrillo/rastrillo/currency/replay\"\n")
	}
	fmt.Fprintf(&b, "\t%s %q\n", alias, path)
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
	id, ok := view.ParseID(r)
	if !ok {
		view.NotFound(ctx, w, r)
		return
	}
`)
	b.WriteString(sessionStmts(r))
	fmt.Fprintf(&b, "store := %s.New(ctx.DB)\n", alias)

	if groupHasValidation {
		fmt.Fprintf(&b, "n, err := store.Get%s(r.Context(), %s)\n", singular, recordArg(r, alias, "Get", singular))
	} else {
		fmt.Fprintf(&b, "_, err := store.Get%s(r.Context(), %s)\n", singular, recordArg(r, alias, "Get", singular))
	}
	b.WriteString(`if errors.Is(err, sql.ErrNoRows) {
		view.NotFound(ctx, w, r)
		return
	}
`)
	fmt.Fprintf(&b, "if err != nil {\n\tview.Fail(ctx, w, r, %q, err)\n\treturn\n}\n\n", failWhat(r, "loading "+r.Name))

	b.WriteString(codeStmts(group))
	if len(group) > 0 {
		b.WriteString(parseCall(r, group, true))
	}

	if groupHasValidation {
		if retries {
			b.WriteString("\nif !p.OK() || try == 2 {\n")
		} else {
			b.WriteString("\nif !p.OK() {\n")
		}
		b.WriteString("fields := " + fieldsMapLiteral(r, "n", false) + "\n")
		b.WriteString("for k, v := range p.Echo() {\nfields[k] = v\n}\n")
		b.WriteString(echoStmts(group, true))
		b.WriteString(seedStmts(otherMoney, "id",
			func(f rastrillo.Field) string { return "n." + f.Name + "Minor" },
			func(f rastrillo.Field) string { return "currencyOf(n." + f.Name + "Currency)" }))
		errs := "p.Errors()"
		if retries {
			// The last run's own parse cannot be trusted to say what
			// happened: the record moved twice, so it may refuse the seed
			// (an amount cleared in between leaves nothing to match) and
			// reseed the input blank, or refuse nothing at all. What the
			// person must see is the conflict, with the text they typed,
			// which the first run already read in its locale, beside the
			// record's current currency. Nothing below saves.
			var keys, inputs []string
			for _, f := range money {
				l := lowerFirst(f.Name)
				keys = append(keys, fmt.Sprintf("%q: %q", f.Name, moneyCurrencyChangedKey))
				inputs = append(inputs, fmt.Sprintf("%sInput = form.MoneyInput{Value: p.Echo()[%q], Locale: r.PostFormValue(%q), Currency: %sCode}\n", l, f.Name, f.Name+".locale", l))
			}
			b.WriteString("errs := p.Errors()\nif try == 2 {\nerrs = map[string]string{" + strings.Join(keys, ", ") + "}\n" + strings.Join(inputs, "") + "}\n")
			errs = "errs"
		}
		fmt.Fprintf(&b, "view.Render(ctx, w, %q, http.StatusBadRequest, formView{\n", r.Name+"/form")
		b.WriteString(localizedField + "IsNew: false,\nFields: fields,\nErrors: " + errs + ",\nErrorArgs: p.ErrorArgs(),\n")
		if r.HasMoney() {
			b.WriteString("Money: " + moneyMapLiteral(formFields(r)) + ",\n")
		}
		fmt.Fprintf(&b, "BasicsAction: fmt.Sprintf(%q, id),\n", r.Route+"/%d/edit-basics")
		if hasAdvanced {
			fmt.Fprintf(&b, "AdvancedAction: fmt.Sprintf(%q, id),\n", r.Route+"/%d/edit-advanced")
		}
		b.WriteString("})\nreturn\n}\n")
	}

	b.WriteString(writeStmts(group, func(f rastrillo.Field) string { return lowerFirst(f.Name) + "Code" }))
	b.WriteString("\nnow := time.Now().UTC().Format(time.RFC3339)\n")
	fmt.Fprintf(&b, "if err := store.Update%s%s(r.Context(), %s.Update%s%sParams{\n",
		singular, groupName, alias, singular, groupName)
	for _, l := range paramLines {
		b.WriteString(l)
	}
	b.WriteString(ownerParamLine(r) + "Now: now,\nID: id,\n")
	b.WriteString("}); err != nil {\n")
	if retries {
		b.WriteString("if try < 2 && errors.Is(err, replay.ErrCurrencyChange) {\nhandle(ctx, w, r, try+1)\nreturn\n}\n")
	}
	fmt.Fprintf(&b, "view.Fail(ctx, w, r, %q, err)\nreturn\n}\n", failWhat(r, "updating "+r.Name))
	fmt.Fprintf(&b, "http.Redirect(w, r, fmt.Sprintf(%q, id), http.StatusSeeOther)\n}\n", r.Route+"/%d")

	if len(money) > 0 {
		b.WriteString(amountHelper)
	}
	if len(money)+len(otherMoney) > 0 {
		if groupHasValidation {
			b.WriteString(seedHelper(r))
		}
		b.WriteString(codeHelper(r))
	}
	if groupHasValidation {
		b.WriteString(formViewType(r))
	}
	return b.String()
}

// handleDoc is the doc comment on a mergeable money update's handle,
// a draft for the copy review (copy-review/final).
const handleDoc = "handle is Handle's body. When the store refuses the save because the record's currency changed, it runs again: once more to save, and then only to show the form with that message."

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
// as per-field statements). A money field's is moneyField's: edit says
// whether it reads the record's currency and amount or the resource
// default.
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
		if f.Required {
			parts = append(parts, "Required: true")
		}
		b.WriteString("form.Field{" + strings.Join(parts, ", ") + "},\n")
	}
	b.WriteString(")\n")
	return b.String()
}

// storeParamLine is one store-Params field fed from the Parse result;
// a money field's two are writeStmts' locals.
func storeParamLine(f rastrillo.Field) string {
	if f.Kind == rastrillo.Money {
		l := lowerFirst(f.Name)
		return fmt.Sprintf("%sMinor: %sMinor,\n%sCurrency: %sCurrency,\n", f.Name, l, f.Name, l)
	}
	return fmt.Sprintf("%s: p.String(%q),\n", f.Name, f.Name)
}

// fieldsCanFail reports whether any field can fail validation — a
// Money parse, or a Required-blank check — i.e. whether the action
// needs the !p.OK() re-render branch at all.
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
// the only thing that deletes. It is also the page §8's agent consent
// gate points a human at before a write tool may run.
func actionDeleteGET(r rastrillo.Resource, module string) string {
	alias, path := storeImport(r, module)
	singular := singularPascal(r.Name)
	main, _, _ := mainSubColumns(r)
	showsMoney := main.Kind == rastrillo.Money

	var b strings.Builder
	b.WriteString("import (\n")
	b.WriteString("\t\"database/sql\"\n")
	b.WriteString("\t\"errors\"\n")
	b.WriteString("\t\"fmt\"\n")
	b.WriteString("\t\"net/http\"\n\n")
	b.WriteString(actionImportLines(r, false, true, true))
	if showsMoney {
		b.WriteString(currencyImport)
	}
	fmt.Fprintf(&b, "\t%s %q\n", alias, path)
	b.WriteString(")\n")

	fmt.Fprintf(&b, "\n// Handle is GET %s/{id}/delete: the confirm page. A GET never\n", r.Route)
	b.WriteString("// mutates; the delete itself is the sibling POST.\n")
	b.WriteString(`func Handle(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request) {
	id, ok := view.ParseID(r)
	if !ok {
		view.NotFound(ctx, w, r)
		return
	}
`)
	b.WriteString(sessionStmts(r))
	fmt.Fprintf(&b, "store := %s.New(ctx.DB)\n", alias)
	fmt.Fprintf(&b, "n, err := store.Get%s(r.Context(), %s)\n", singular, recordArg(r, alias, "Get", singular))
	b.WriteString(`if errors.Is(err, sql.ErrNoRows) {
		view.NotFound(ctx, w, r)
		return
	}
`)
	fmt.Fprintf(&b, "if err != nil {\n\tview.Fail(ctx, w, r, %q, err)\n\treturn\n}\n", failWhat(r, "loading "+r.Name))

	title := displayStmts(&b, "n", main, "id")
	fmt.Fprintf(&b, `
view.Render(ctx, w, %q, http.StatusOK, confirmView{
	%sTitle:      %s,
	DeleteHref: fmt.Sprintf(%q, id),
	CancelHref: fmt.Sprintf(%q, id),
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

// actionDeletePOST is the delete itself: Get-or-404 first (a missing
// row answers 404, never a silent no-op success), then the store's
// DELETE, then 303 back to the list.
func actionDeletePOST(r rastrillo.Resource, module string) string {
	alias, path := storeImport(r, module)
	singular := singularPascal(r.Name)

	var b strings.Builder
	b.WriteString("import (\n")
	b.WriteString("\t\"database/sql\"\n")
	b.WriteString("\t\"errors\"\n")
	b.WriteString("\t\"net/http\"\n\n")
	b.WriteString(actionImportLines(r, false, true, false))
	fmt.Fprintf(&b, "\t%s %q\n", alias, path)
	b.WriteString(")\n")

	fmt.Fprintf(&b, "\n// Handle is POST %s/{id}/delete.\n", r.Route)
	b.WriteString(`func Handle(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request) {
	id, ok := view.ParseID(r)
	if !ok {
		view.NotFound(ctx, w, r)
		return
	}
`)
	b.WriteString(sessionStmts(r))
	fmt.Fprintf(&b, "store := %s.New(ctx.DB)\n", alias)
	fmt.Fprintf(&b, "if _, err := store.Get%s(r.Context(), %s); errors.Is(err, sql.ErrNoRows) {\n", singular, recordArg(r, alias, "Get", singular))
	b.WriteString("\tview.NotFound(ctx, w, r)\n\treturn\n")
	fmt.Fprintf(&b, "} else if err != nil {\n\tview.Fail(ctx, w, r, %q, err)\n\treturn\n}\n", failWhat(r, "loading "+r.Name))
	fmt.Fprintf(&b, "if err := store.Delete%s(r.Context(), %s); err != nil {\n\tview.Fail(ctx, w, r, %q, err)\n\treturn\n}\n", singular, recordArg(r, alias, "Delete", singular), failWhat(r, "deleting "+r.Name))
	fmt.Fprintf(&b, "http.Redirect(w, r, %q, http.StatusSeeOther)\n}\n", r.Route)

	return b.String()
}
