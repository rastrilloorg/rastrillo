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
//     as a dollar string via form.FormatCents, everything else the raw
//     column value. Pagination has no gap-collapsing (v1
//     simplification: every page number renders; see the report for
//     the tradeoff).
//   - index.POST (create): parses every Form.Basics + Form.Advanced
//     field; a Money field's value is parsed as decimal dollars via
//     form.ParseCents, which rejects more than two decimal places, and any
//     field (Text, Textarea or Money) the manifest marks Required is
//     checked for blankness — Text/Textarea trimmed, Money via its
//     RAW submitted text (so "0"/"0.00" is a valid required value;
//     only an empty input trips the required check). All of that is
//     one form.Parse call now — form.Field's kinds carry the rules,
//     documented on the helper itself. Any failure
//     re-renders "<name>/form" (IsNew: true) at 400 with the field's
//     message in Errors and every field's raw submitted text preserved
//     in Fields — never reformatted, so a typo stays exactly as typed
//     for correction. A Required Money field that's both blank AND
//     would otherwise fail to parse (it never does — "" parses to zero,
//     see form.ParseCents) only ever reports the required message, never
//     both; a present-but-invalid Money value reports the parse error,
//     never the required one — see form.Parse's Money rules. Success
//     stamps both timestamps with the same UTC RFC3339 "now" and
//     redirects 303 to Show.
//   - [id]/index.GET (show): Get-or-404, then every declared field
//     (columns(r) order) into Fields, Money formatted.
//   - new.GET / [id]/edit.GET: render "<name>/form" with zero values
//     (New) or the record's current values (Edit, Get-or-404 first).
//   - [id]/edit-basics.POST / [id]/edit-advanced.POST: Get-or-404 (to
//     answer 404 before ever attempting an UPDATE against a missing
//     row, and — only when this field group has a Money field or a
//     Required field — to source the OTHER group's current values for
//     the 400 re-render), then parse and update only that field group.
//     A Money parse failure or a Required-blank failure re-renders
//     "<name>/form" (IsNew: false) at 400 with Fields seeded from the
//     fetched record and overridden with this group's just-submitted
//     raw text. A field group with no Money field and no Required
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

	"github.com/carlosframework/rastrillo"
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

// fieldExpr renders the Go expression that reads c's current value off
// a record variable named varName: a Money column reads through
// moneyFmt — either "form.FormatCents" for a display-only context
// (show.html's Fields/Title, index.GET's Rows) that wants the "$1.23"
// string a human reads, or "form.FormatCentsPlain" for a context that
// seeds a form field a browser might resubmit completely unchanged
// (edit.GET's Fields, and the OTHER field group's current values on a
// validation-failure re-render) — that seed must come back out exactly
// as form.ParseCents itself accepts back in ("1.23", never "$1.23"; see
// form.ParseCents' own doc for why a leading "$" 400s an untouched
// edit). Everything else (Text/Textarea) is the raw sqlc-generated
// field, unaffected by either formatter.
func fieldExpr(varName string, c column, moneyFmt string) string {
	if c.Kind == rastrillo.Money {
		return fmt.Sprintf("%s(%s.%s)", moneyFmt, varName, c.Name)
	}
	return varName + "." + c.Name
}

// fieldsMapLiteral renders a `map[string]string{...}` literal for
// every column in columns(r), each keyed by its declared name and
// valued by fieldExpr(varName, c, moneyFmt) — the shape show.html and
// the Edit half of form.html both read as Fields. Callers pass
// "form.FormatCents" (show's Fields, a display context) or
// "form.FormatCentsPlain" (edit's Fields, a re-submittable form seed)
// — see fieldExpr's doc for why the two contexts need different
// formatters.
func fieldsMapLiteral(r rastrillo.Resource, varName, moneyFmt string) string {
	var b strings.Builder
	b.WriteString("map[string]string{\n")
	for _, c := range columns(r) {
		fmt.Fprintf(&b, "%q: %s,\n", c.Name, fieldExpr(varName, c, moneyFmt))
	}
	b.WriteString("}")
	return b.String()
}

// zeroFieldsMapLiteral renders the New form's Fields: every declared
// field name mapped to "".
func zeroFieldsMapLiteral(r rastrillo.Resource) string {
	var b strings.Builder
	b.WriteString("map[string]string{\n")
	for _, c := range columns(r) {
		fmt.Fprintf(&b, "%q: \"\",\n", c.Name)
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
// stamped helper logged the same line view.Fail(ctx, w, "notes:
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
// alphabetically, same as gofmt.
func actionImportLines(r rastrillo.Resource, needsForm, needsSession bool) string {
	var b strings.Builder
	b.WriteString("\t\"github.com/carlosframework/rastrillo\"\n")
	if needsForm {
		b.WriteString("\t\"github.com/carlosframework/rastrillo/form\"\n")
	}
	if scoped(r) && needsSession {
		b.WriteString("\t\"github.com/carlosframework/rastrillo/sessions\"\n")
	}
	b.WriteString("\t\"github.com/carlosframework/rastrillo/view\"\n")
	return b.String()
}

// ── owner scoping (scope = "user") plumbing, shared by every action ──

// sessionStmts renders the statements a scoped Handle opens with:
// resolve the session, refuse a signed-out caller. 403 rather than a
// redirect — the mounting contract (SKILL.md, manifest/README) puts
// scoped routes behind sessions.Require / auth.RequireSession, which
// already redirect page requests; this refusal is the defense-in-depth
// backstop for an app that forgot, matching sessions' own answer for
// a non-page request. Empty for an unscoped resource.
func sessionStmts(r rastrillo.Resource) string {
	if !scoped(r) {
		return ""
	}
	return `sess, ok := sessions.Current(r)
	if !ok {
		http.Error(w, "signed out", http.StatusForbidden)
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
	Current  bool
	Disabled bool
	Gap      bool
}
`, filterField, filterTypes)
}

const showViewType = `
type showView struct {
	Title    string
	EditHref string
	Fields   map[string]string
}
`

const formViewType = `
type formView struct {
	IsNew          bool
	Fields         map[string]string
	Errors         map[string]string
	BasicsAction   string
	AdvancedAction string
}
`

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

	needsForm := main.Kind == rastrillo.Money || (hasSub && sub.Kind == rastrillo.Money)

	var b strings.Builder
	b.WriteString("import (\n")
	b.WriteString("\t\"fmt\"\n")
	b.WriteString("\t\"net/http\"\n")
	b.WriteString("\t\"net/url\"\n")
	b.WriteString("\t\"strconv\"\n")
	b.WriteString("\t\"strings\"\n\n")
	b.WriteString(actionImportLines(r, needsForm, true))
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
	fmt.Fprintf(&b, "if err != nil {\n\tview.Fail(ctx, w, %q, err)\n\treturn\n}\n", failWhat(r, "counting "+r.Name))

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
	fmt.Fprintf(&b, "if err != nil {\n\tview.Fail(ctx, w, %q, err)\n\treturn\n}\n", failWhat(r, "loading "+r.Name))

	fmt.Fprintf(&b, `
items := make([]listRow, 0, len(rows))
for _, n := range rows {
	items = append(items, listRow{
		Href: fmt.Sprintf(%q, n.ID),
		Main: %s,
		Sub:  %s,
	})
}
`, r.Route+"/%d", fieldExpr("n", main, "form.FormatCents"), subExpr(sub, hasSub))

	b.WriteString(`
totalPages := (int(total) + pageSize - 1) / pageSize
show := int(total) > pageSize
var pageItems []listPageItem
if show {
	prev := listPageItem{Label: "Previous", Disabled: true}
	if page > 1 {
		prev = listPageItem{Label: "Previous", Href: href(` + searchExprForHref(searchDeclared) + `, carry, page-1)}
	}
	pageItems = append(pageItems, prev)
	for n := 1; n <= totalPages; n++ {
		if n == page {
			pageItems = append(pageItems, listPageItem{Label: strconv.Itoa(n), Current: true})
		} else {
			pageItems = append(pageItems, listPageItem{Label: strconv.Itoa(n), Href: href(` + searchExprForHref(searchDeclared) + `, carry, n)})
		}
	}
	next := listPageItem{Label: "Next", Disabled: true}
	if page < totalPages {
		next = listPageItem{Label: "Next", Href: href(` + searchExprForHref(searchDeclared) + `, carry, page+1)}
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
	Empty:      total == 0,
	Query:      %s,
	Carry:      carry,
	%sRows:       items,
	Pagination: listPagination{Show: show, Items: pageItems},
})
}
`, r.Name+"/list", searchExpr, filterField)

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

// subExpr renders the Sub row expression: an empty literal when the
// resource declares no second List column.
func subExpr(sub column, hasSub bool) string {
	if !hasSub {
		return `""`
	}
	return fieldExpr("n", sub, "form.FormatCents")
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
		// no submitted value for it, so it gets the zero value. (A
		// well-formed manifest gives every List column a Form field
		// too; this is a fallback, not the expected shape.)
		zero := `""`
		if c.Kind == rastrillo.Money {
			zero = "0"
		}
		paramLines = append(paramLines, fmt.Sprintf("%s: %s,\n", c.Name, zero))
	}

	canFail := fieldsCanFail(order)

	var b strings.Builder
	b.WriteString("import (\n")
	b.WriteString("\t\"fmt\"\n")
	b.WriteString("\t\"net/http\"\n")
	b.WriteString("\t\"time\"\n\n")
	b.WriteString(actionImportLines(r, len(order) > 0, true))
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
		b.WriteString(parseCall(order))
	}

	if canFail {
		b.WriteString("if !p.OK() {\n")
		fmt.Fprintf(&b, "view.Render(ctx, w, %q, http.StatusBadRequest, formView{\n", r.Name+"/form")
		b.WriteString("IsNew: true,\nFields: p.Echo(),\nErrors: p.Errors(),\n})\nreturn\n}\n")
	}

	b.WriteString("\nnow := time.Now().UTC().Format(time.RFC3339)\n")
	fmt.Fprintf(&b, "store := %s.New(ctx.DB)\n", alias)
	fmt.Fprintf(&b, "id, err := store.Create%s(r.Context(), %s.Create%sParams{\n", singular, alias, singular)
	b.WriteString(ownerParamLine(r))
	for _, l := range paramLines {
		b.WriteString(l)
	}
	b.WriteString("Now: now,\n")
	b.WriteString("})\n")
	fmt.Fprintf(&b, "if err != nil {\n\tview.Fail(ctx, w, %q, err)\n\treturn\n}\n", failWhat(r, "creating "+r.Name))
	fmt.Fprintf(&b, "http.Redirect(w, r, fmt.Sprintf(%q, id), http.StatusSeeOther)\n}\n", r.Route+"/%d")

	b.WriteString(formViewType)
	return b.String()
}

// ── new.GET ──────────────────────────────────────────────────────────

func actionNewGET(r rastrillo.Resource, module string) string {
	var b strings.Builder
	b.WriteString("import (\n")
	b.WriteString("\t\"net/http\"\n\n")
	b.WriteString(actionImportLines(r, false, false))
	b.WriteString(")\n")

	fmt.Fprintf(&b, "\n// Handle is GET %s/new.\n", r.Route)
	b.WriteString("func Handle(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request) {\n")
	fmt.Fprintf(&b, "view.Render(ctx, w, %q, http.StatusOK, formView{\n", r.Name+"/form")
	b.WriteString("IsNew: true,\nFields: " + zeroFieldsMapLiteral(r) + ",\n")
	b.WriteString("})\n}\n")

	b.WriteString(formViewType)
	return b.String()
}

// ── [id]/index.GET (Show) ───────────────────────────────────────────

func actionShowGET(r rastrillo.Resource, module string) string {
	alias, path := storeImport(r, module)
	singular := singularPascal(r.Name)
	main, _, _ := mainSubColumns(r)
	needsForm := hasMoneyColumn(columns(r))

	var b strings.Builder
	b.WriteString("import (\n")
	b.WriteString("\t\"database/sql\"\n")
	b.WriteString("\t\"errors\"\n")
	b.WriteString("\t\"fmt\"\n")
	b.WriteString("\t\"net/http\"\n\n")
	b.WriteString(actionImportLines(r, needsForm, true))
	fmt.Fprintf(&b, "\t%s %q\n", alias, path)
	b.WriteString(")\n")

	fmt.Fprintf(&b, "\n// Handle is GET %s/{id}.\n", r.Route)
	b.WriteString(`func Handle(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request) {
	id, ok := view.ParseID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
`)
	b.WriteString(sessionStmts(r))
	fmt.Fprintf(&b, "store := %s.New(ctx.DB)\n", alias)
	fmt.Fprintf(&b, "n, err := store.Get%s(r.Context(), %s)\n", singular, recordArg(r, alias, "Get", singular))
	b.WriteString(`if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
`)
	fmt.Fprintf(&b, "if err != nil {\n\tview.Fail(ctx, w, %q, err)\n\treturn\n}\n", failWhat(r, "loading "+r.Name))

	fmt.Fprintf(&b, `
view.Render(ctx, w, %q, http.StatusOK, showView{
	Title:    %s,
	EditHref: fmt.Sprintf(%q, id),
	Fields:   %s,
})
}
`, r.Name+"/show", fieldExpr("n", main, "form.FormatCents"), r.Route+"/%d/edit", fieldsMapLiteral(r, "n", "form.FormatCents"))

	b.WriteString(showViewType)
	return b.String()
}

// ── [id]/edit.GET ────────────────────────────────────────────────────

func actionEditGET(r rastrillo.Resource, module string) string {
	alias, path := storeImport(r, module)
	singular := singularPascal(r.Name)
	hasAdvanced := len(r.Form.Advanced) > 0
	needsForm := hasMoneyColumn(columns(r))

	var b strings.Builder
	b.WriteString("import (\n")
	b.WriteString("\t\"database/sql\"\n")
	b.WriteString("\t\"errors\"\n")
	b.WriteString("\t\"fmt\"\n")
	b.WriteString("\t\"net/http\"\n\n")
	b.WriteString(actionImportLines(r, needsForm, true))
	fmt.Fprintf(&b, "\t%s %q\n", alias, path)
	b.WriteString(")\n")

	fmt.Fprintf(&b, "\n// Handle is GET %s/{id}/edit.\n", r.Route)
	b.WriteString(`func Handle(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request) {
	id, ok := view.ParseID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
`)
	b.WriteString(sessionStmts(r))
	fmt.Fprintf(&b, "store := %s.New(ctx.DB)\n", alias)
	fmt.Fprintf(&b, "n, err := store.Get%s(r.Context(), %s)\n", singular, recordArg(r, alias, "Get", singular))
	b.WriteString(`if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
`)
	fmt.Fprintf(&b, "if err != nil {\n\tview.Fail(ctx, w, %q, err)\n\treturn\n}\n", failWhat(r, "loading "+r.Name))

	fmt.Fprintf(&b, `
view.Render(ctx, w, %q, http.StatusOK, formView{
	IsNew:  false,
	Fields: %s,
	BasicsAction: fmt.Sprintf(%q, id),
`, r.Name+"/form", fieldsMapLiteral(r, "n", "form.FormatCentsPlain"), r.Route+"/%d/edit-basics")
	if hasAdvanced {
		fmt.Fprintf(&b, "AdvancedAction: fmt.Sprintf(%q, id),\n", r.Route+"/%d/edit-advanced")
	}
	b.WriteString("})\n}\n")

	b.WriteString(formViewType)
	return b.String()
}

// ── [id]/edit-basics.POST and [id]/edit-advanced.POST ──────────────

// updatePOST builds edit-basics.POST or edit-advanced.POST: they are
// the same shape (Get-or-404, parse this group's fields, update,
// redirect — or, only when this group declares a Money field or a
// Required field, a validation branch), parameterized on which field
// group and which sqlc Update query own it.
func updatePOST(r rastrillo.Resource, module string, group []rastrillo.Field, groupName string) string {
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

	needsForm := len(group) > 0 || (groupHasValidation && hasMoneyColumn(columns(r)))

	var b strings.Builder
	b.WriteString("import (\n")
	b.WriteString("\t\"database/sql\"\n")
	b.WriteString("\t\"errors\"\n")
	b.WriteString("\t\"fmt\"\n")
	b.WriteString("\t\"net/http\"\n")
	b.WriteString("\t\"time\"\n\n")
	b.WriteString(actionImportLines(r, needsForm, true))
	fmt.Fprintf(&b, "\t%s %q\n", alias, path)
	b.WriteString(")\n")

	fmt.Fprintf(&b, "\n// Handle is POST %s/{id}/edit-%s.\n", r.Route, strings.ToLower(groupName))
	b.WriteString(`func Handle(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request.", http.StatusBadRequest)
		return
	}
	id, ok := view.ParseID(r)
	if !ok {
		http.NotFound(w, r)
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
		http.NotFound(w, r)
		return
	}
`)
	fmt.Fprintf(&b, "if err != nil {\n\tview.Fail(ctx, w, %q, err)\n\treturn\n}\n\n", failWhat(r, "loading "+r.Name))

	if len(group) > 0 {
		b.WriteString(parseCall(group))
	}

	if groupHasValidation {
		b.WriteString("\nif !p.OK() {\n")
		b.WriteString("fields := " + fieldsMapLiteral(r, "n", "form.FormatCentsPlain") + "\n")
		b.WriteString("for k, v := range p.Echo() {\nfields[k] = v\n}\n")
		fmt.Fprintf(&b, "view.Render(ctx, w, %q, http.StatusBadRequest, formView{\n", r.Name+"/form")
		b.WriteString("IsNew: false,\nFields: fields,\nErrors: p.Errors(),\n")
		fmt.Fprintf(&b, "BasicsAction: fmt.Sprintf(%q, id),\n", r.Route+"/%d/edit-basics")
		if hasAdvanced {
			fmt.Fprintf(&b, "AdvancedAction: fmt.Sprintf(%q, id),\n", r.Route+"/%d/edit-advanced")
		}
		b.WriteString("})\nreturn\n}\n")
	}

	b.WriteString("\nnow := time.Now().UTC().Format(time.RFC3339)\n")
	fmt.Fprintf(&b, "if err := store.Update%s%s(r.Context(), %s.Update%s%sParams{\n",
		singular, groupName, alias, singular, groupName)
	for _, l := range paramLines {
		b.WriteString(l)
	}
	b.WriteString(ownerParamLine(r) + "Now: now,\nID: id,\n")
	b.WriteString("}); err != nil {\n")
	fmt.Fprintf(&b, "view.Fail(ctx, w, %q, err)\nreturn\n}\n", failWhat(r, "updating "+r.Name))
	fmt.Fprintf(&b, "http.Redirect(w, r, fmt.Sprintf(%q, id), http.StatusSeeOther)\n}\n", r.Route+"/%d")

	if groupHasValidation {
		b.WriteString(formViewType)
	}
	return b.String()
}

func actionEditBasicsPOST(r rastrillo.Resource, module string) string {
	return updatePOST(r, module, r.Form.Basics, "Basics")
}

func actionEditAdvancedPOST(r rastrillo.Resource, module string) string {
	return updatePOST(r, module, r.Form.Advanced, "Advanced")
}

// ── field parsing shared by Create and both Update actions ─────────

// parseCall emits the one form.Parse call a POST action opens with —
// the same documented helper a hand-written app uses (form.Field's
// kinds carry the parse/trim/echo rules that used to be stamped here
// as per-field statements).
func parseCall(fields []rastrillo.Field) string {
	var b strings.Builder
	b.WriteString("p := form.Parse(r,\n")
	for _, f := range fields {
		parts := []string{fmt.Sprintf("Name: %q", f.Name)}
		switch f.Kind {
		case rastrillo.Textarea:
			parts = append(parts, "Kind: form.Textarea")
		case rastrillo.Money:
			parts = append(parts, "Kind: form.Money")
		}
		if f.Required {
			parts = append(parts, "Required: true")
		}
		b.WriteString("form.Field{" + strings.Join(parts, ", ") + "},\n")
	}
	b.WriteString(")\n")
	return b.String()
}

// storeParamLine is one store-Params field fed from the Parse result.
func storeParamLine(f rastrillo.Field) string {
	if f.Kind == rastrillo.Money {
		return fmt.Sprintf("%s: p.Cents(%q),\n", f.Name, f.Name)
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
	needsForm := main.Kind == rastrillo.Money

	var b strings.Builder
	b.WriteString("import (\n")
	b.WriteString("\t\"database/sql\"\n")
	b.WriteString("\t\"errors\"\n")
	b.WriteString("\t\"fmt\"\n")
	b.WriteString("\t\"net/http\"\n\n")
	b.WriteString(actionImportLines(r, needsForm, true))
	fmt.Fprintf(&b, "\t%s %q\n", alias, path)
	b.WriteString(")\n")

	fmt.Fprintf(&b, "\n// Handle is GET %s/{id}/delete: the confirm page. A GET never\n", r.Route)
	b.WriteString("// mutates; the delete itself is the sibling POST.\n")
	b.WriteString(`func Handle(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request) {
	id, ok := view.ParseID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
`)
	b.WriteString(sessionStmts(r))
	fmt.Fprintf(&b, "store := %s.New(ctx.DB)\n", alias)
	fmt.Fprintf(&b, "n, err := store.Get%s(r.Context(), %s)\n", singular, recordArg(r, alias, "Get", singular))
	b.WriteString(`if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
`)
	fmt.Fprintf(&b, "if err != nil {\n\tview.Fail(ctx, w, %q, err)\n\treturn\n}\n", failWhat(r, "loading "+r.Name))

	fmt.Fprintf(&b, `
view.Render(ctx, w, %q, http.StatusOK, confirmView{
	Title:      %s,
	DeleteHref: fmt.Sprintf(%q, id),
	CancelHref: fmt.Sprintf(%q, id),
})
}
`, r.Name+"/confirm", fieldExpr("n", main, "form.FormatCents"), r.Route+"/%d/delete", r.Route+"/%d")

	b.WriteString(confirmViewType)
	return b.String()
}

const confirmViewType = `
type confirmView struct {
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
	b.WriteString(actionImportLines(r, false, true))
	fmt.Fprintf(&b, "\t%s %q\n", alias, path)
	b.WriteString(")\n")

	fmt.Fprintf(&b, "\n// Handle is POST %s/{id}/delete.\n", r.Route)
	b.WriteString(`func Handle(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request) {
	id, ok := view.ParseID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
`)
	b.WriteString(sessionStmts(r))
	fmt.Fprintf(&b, "store := %s.New(ctx.DB)\n", alias)
	fmt.Fprintf(&b, "if _, err := store.Get%s(r.Context(), %s); errors.Is(err, sql.ErrNoRows) {\n", singular, recordArg(r, alias, "Get", singular))
	b.WriteString("\thttp.NotFound(w, r)\n\treturn\n")
	fmt.Fprintf(&b, "} else if err != nil {\n\tview.Fail(ctx, w, %q, err)\n\treturn\n}\n", failWhat(r, "loading "+r.Name))
	fmt.Fprintf(&b, "if err := store.Delete%s(r.Context(), %s); err != nil {\n\tview.Fail(ctx, w, %q, err)\n\treturn\n}\n", singular, recordArg(r, alias, "Delete", singular), failWhat(r, "deleting "+r.Name))
	fmt.Fprintf(&b, "http.Redirect(w, r, %q, http.StatusSeeOther)\n}\n", r.Route)

	return b.String()
}
