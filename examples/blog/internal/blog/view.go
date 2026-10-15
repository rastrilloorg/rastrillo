package blog

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	blogassets "blog"
	genlocales "blog/gen/locales"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/locales"
	"amadan.net/rastrillo/rastrillo/ui"
)

//go:embed templates/layout.html templates/pages/*.html
var templateFS embed.FS

// PageSize is how many posts a list screen shows. Ten is small enough
// that eleven posts produce a real page strip in a test.
const PageSize = 10

// Assets fingerprints the app's embedded static files. One instance
// shared by the "asset" template func and main.go's /static/ mount,
// so the URL a layout renders is always one the handler serves
// immutable.
var Assets = rastrillo.NewAssets(blogassets.StaticFS)

// App is this app's one template set and its render seam: a single
// ui.Pages built once at NewApp, in every locale cfg offers (tickets'
// internal/tickets/render.go pattern, task 23). Unlike the hand-rolled
// map[string]*template.Template buildPages used to hold, there is one
// base (ui's partials and this layout) parsed once, not once per
// language or request, and each page is one clone of it made at
// NewApp — the clone is what makes the shared layout work: every
// screen file defines "content", the same name in every file, so
// parsing them into one shared tree would let the last one clobber the
// rest (see the package's own template tests,
// TestEachPageRendersItsOwnContent).
type App struct {
	pages *ui.Pages
}

// pageFiles resolves every page this app serves into one
// "<key>" -> ui.PageFile map for ui.NewPages: the app's own bare-named
// hand screens ("index", "post" — nothing to do with a manifest
// resource) plus every manifest-driven "<resource>/<page>" one —
// internal/generate/actions.go's page-name contract, restated in that
// package's own doc comment. A resource-keyed entry's *source file*
// may be hand-ejected (templates/<resource>/<page>.html — task 11's
// ejection) or still generated (gen/templates/<resource>/<page>.html);
// resourceSources resolves that per key, so Render itself never has to
// know or care which.
func pageFiles() (map[string]ui.PageFile, error) {
	names, err := fs.Glob(templateFS, "templates/pages/*.html")
	if err != nil {
		return nil, err // the pattern is a constant and the FS is embedded
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("blog: no page templates embedded")
	}
	out := make(map[string]ui.PageFile, len(names))
	for _, n := range names {
		out[strings.TrimSuffix(filepath.Base(n), ".html")] = ui.PageFile{FS: templateFS, Path: n}
	}
	for key, src := range resourceSources() {
		out[key] = ui.PageFile{FS: src.fsys, Path: src.name}
	}
	return out, nil
}

// templateSource is one resolved "<resource>/<page>" entry: the
// filesystem and path resourceSources found it at.
type templateSource struct {
	fsys fs.FS
	name string
}

// resourceSources walks gen/templates first, then templates/ (the
// ejection root — see genassets.go's AppTemplatesFS), so a key present
// in both overwrites with the ejected copy. In practice the two never
// collide once a file is properly ejected: EmitTemplates
// (internal/generate/templates.go) stops writing a generated file the
// moment a hand file claims its computed path, and this task deleted
// the stale generated copies it left behind before ejecting — but
// walking both, rather than hard-coding which of posts/list.html,
// posts/form.html or posts/show.html is which, means a later resource
// or a later ejection needs no change here.
func resourceSources() map[string]templateSource {
	out := map[string]templateSource{}
	addResourceDir(out, blogassets.GenTemplatesFS, "gen/templates")
	addResourceDir(out, blogassets.AppTemplatesFS, "templates")
	return out
}

// addResourceDir walks root's immediate subdirectories (one per
// manifest resource) inside embedded, adding a "<dir>/<file>" entry
// for every *.html file straight under each — overwriting whatever
// out already holds for that key, which is exactly how a later call
// with the ejection root lets an ejected file win over a generated one
// at the same key.
func addResourceDir(out map[string]templateSource, embedded embed.FS, root string) {
	sub, err := fs.Sub(embedded, root)
	if err != nil {
		panic(err) // root is a constant, embedded at compile time
	}
	resDirs, err := fs.ReadDir(sub, ".")
	if err != nil {
		panic(err)
	}
	for _, rd := range resDirs {
		if !rd.IsDir() {
			continue
		}
		files, err := fs.Glob(sub, rd.Name()+"/*.html")
		if err != nil {
			panic(err)
		}
		for _, f := range files {
			key := rd.Name() + "/" + strings.TrimSuffix(path.Base(f), ".html")
			out[key] = templateSource{fsys: sub, name: f}
		}
	}
}

// NewApp builds the one template set this app serves from, strict in
// every locale cfg offers (ui.Pages' own doc comment). cfg is
// locales.CheckConfig's result — main.go gets it from
// rastrillo.Handler's own validation; this call is only to read the
// value back.
func NewApp(cfg locales.Config) (*App, error) {
	pages, err := pageFiles()
	if err != nil {
		return nil, err
	}
	p, err := ui.NewPages(ui.PagesConfig{
		App:     templateFS,
		Layout:  "templates/layout.html",
		Pages:   pages,
		Locale:  cfg,
		Catalog: genlocales.BaseCatalog,
		Funcs:   template.FuncMap{"asset": Assets.Path},
	})
	if err != nil {
		return nil, err
	}
	return &App{pages: p}, nil
}

// Middleware puts the request's localizer on the context, for every
// page this app renders — ui.Pages' own Middleware.
func (a *App) Middleware(next http.Handler) http.Handler {
	return a.pages.Middleware(next)
}

// pageData is "layout"'s one data value. It embeds the ui.Localized
// copied off the view Render is handed, so the layout finds L (strict
// dispatch needs it non-nil), Locales and Return under their usual
// names (Task 14) without this app re-deriving any of them from the
// request itself. Head carries what <head> needs, and Data is passed
// through to "content" untouched. Every hand screen's own view model
// already declares its own Head field (HomeView.Head, PostView.Head,
// AdminListView.Head, ...); headFor (genrender.go) reflects it out
// rather than duplicating it, so a hand screen's content template
// still sees its own struct exactly, only reached through .Data
// instead of directly. A truly generated view model (posts/show's
// showView) carries no Head field at all — the action emitter doesn't
// know about the app's layout, by design — so headFor falls back to a
// page-name default for those.
type pageData struct {
	ui.Localized
	Head Head
	Data any
}

// notAViewMsg is Appendix C.2's op.render.not_a_view, verbatim: data
// that is not a generated view carries none of the request values
// (L, Locales, Return) the layout needs, so this fails loudly rather
// than let a type-assertion panic hide which page was wrong, or let
// strict rendering turn it into a less specific template error.
const notAViewMsg = "page %q: the data has no request values, because it is not a generated view; nothing was rendered"

// Render wraps data (a hand view model or a generated formView/
// showView/confirmView) in the layout's data and executes the one
// template set NewApp built, writing the status and the bytes only
// once the whole page has rendered — ui.Pages.Execute's own guarantee,
// so a template error is a clean 500 rather than half a page followed
// by whatever came after it failed.
//
// This is also the one seam every generated action's ctx.Render call
// resolves to (main.go wires it: &rastrillo.Ctx{Render: app.Render}).
// "posts/form" gets one extra step before Execute: formStripData
// (genrender.go) enriches the generated formView-shaped data with the
// Edit screen's Published/status-pill/publish-unpublish-delete-strip
// fields, none of which a generated new.GET/edit.GET action has any
// way to know about (posts.toml declares no such field) — see that
// function's own doc comment.
func (a *App) Render(ctx *rastrillo.Ctx, w http.ResponseWriter, page string, status int, data any) {
	v, ok := data.(interface{ Request() ui.Localized })
	if !ok {
		Fail(ctx, w, fmt.Sprintf(notAViewMsg, page), nil)
		return
	}
	loc := v.Request()
	if page == "posts/form" {
		data = formStripData(ctx, data, loc)
	}
	pd := pageData{Localized: loc, Head: headFor(page, data), Data: data}
	var buf bytes.Buffer
	if err := a.pages.Execute(&buf, page, pd); err != nil {
		Fail(ctx, w, "rendering "+page, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	w.Write(buf.Bytes())
}

// Fail logs through Ctx.Logger and answers a plain 500. Every store error
// that is not sql.ErrNoRows goes through it in one line. err is a
// structured slog attribute, not folded into what: a nil err (Render's
// own not-a-view failure, which has none) still logs cleanly as
// err=<nil> rather than losing the field shape every other caller here
// relies on to grep by.
func Fail(ctx *rastrillo.Ctx, w http.ResponseWriter, what string, err error) {
	if ctx != nil && ctx.Logger != nil {
		ctx.Logger.Error("blog: "+what, "err", err)
	}
	http.Error(w, "Something went wrong.", http.StatusInternalServerError)
}

// ParseID reads the {id} path value. A non-numeric id is a URL that was
// never ours, so the caller answers 404 rather than 400.
func ParseID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		return 0, false
	}
	return id, true
}

// PageParam reads ?page=, defaulting to 1 for anything missing or silly.
func PageParam(r *http.Request) int {
	n, err := strconv.Atoi(r.URL.Query().Get("page"))
	if err != nil || n < 1 {
		return 1
	}
	return n
}

// Offset is the LIMIT/OFFSET offset for a 1-based page number.
func Offset(page int) int { return (page - 1) * PageSize }

// ── View models ────────────────────────────────────────────────────────
//
// Every screen's data is a Go struct, never a dict-built map: a mistyped
// field on a struct fails loudly at Execute, while a missing map key
// renders empty and returns no error. dict stays in the templates, where
// it belongs — for the literal argument lists that call a partial.

// Head carries what the layout's <head> needs.
type Head struct{ Title string }

// Row is one list-row-action's data, formatted in Go.
type Row struct {
	Href        string
	Main        string
	Sub         string
	StatusTone  string
	StatusLabel string
	ActionHref  string
	ActionLabel string
	ActionAria  string
}

// PageItem is one control in the pagination strip. Exactly one of Href,
// Current, Disabled and Gap is meaningful per item. Rel is "prev" or
// "next" on the two steps, which a phone draws as chevrons.
type PageItem struct {
	Label    string
	Href     string
	Rel      string
	Current  bool
	Disabled bool
	Gap      bool
}

// Pagination is the strip plus the app's own decision to show it at all:
// the partial happily renders an empty <nav>, which would leave a
// landmark on every single-page list.
type Pagination struct {
	Show  bool
	Items []PageItem
}

// HomeView is GET /. It embeds ui.Localized, set from ui.ForRequest(r)
// by the hand action that builds it, so Render finds L/Locales/Return
// on it the same way it would on a generated view (Render's own doc
// comment, interface{ Request() ui.Localized }).
type HomeView struct {
	ui.Localized
	Head       Head
	Rows       []Row
	Pagination Pagination
}

// PostView is GET /posts/{id}; see HomeView's doc comment on the
// embedded ui.Localized.
type PostView struct {
	ui.Localized
	Head       Head
	Title      string
	Date       string
	Paragraphs []string
}

// AdminListView is GET /admin/posts; see HomeView's doc comment on the
// embedded ui.Localized.
type AdminListView struct {
	ui.Localized
	Head  Head
	Query string
	// Carry is list-bar's Hidden: name/value pairs a search must
	// preserve. The handler sets it to [][2]string{{"status", status}}
	// when a filter is applied, so a search from a filtered list keeps
	// it — a new search still deliberately returns to page 1.
	Carry       [][2]string
	Filter      Filter
	NoMatchNote string
	Rows        []Row
	Pagination  Pagination
	Empty       bool // no posts at all: the real blank state
	NoMatch     bool // a search or filter that matched nothing: a plain note, not a card
}

// FilterItem is one dropdown choice; Filter is list-bar's Filter value.
// The field names match the dropdown partial's key contract.
type FilterItem struct {
	Href    string
	Label   string
	Current bool
}

type Filter struct {
	Label string
	Aria  string
	Items []FilterItem
}

// statusLabels resolves a normalized status to its visible label.
var statusLabels = map[string]string{"": "All", "draft": "Drafts", "published": "Published"}

// NormalizeStatus maps a raw query value onto the three states the
// screen has. Anything unrecognized is "all", not an error: a stale
// bookmark should show posts, not a 400.
func NormalizeStatus(raw string) string {
	if raw == "draft" || raw == "published" {
		return raw
	}
	return ""
}

// BuildStatusFilter builds the admin list's status dropdown. Hrefs
// carry the current search and reset paging — changing a filter starts
// at page 1 by construction. status must be a NormalizeStatus result
// ("", "draft" or "published"): it is interpolated into the href
// unescaped, which every current caller satisfies.
func BuildStatusFilter(q, status string) Filter {
	href := func(s string) string {
		var params []string
		if q != "" {
			params = append(params, "q="+url.QueryEscape(q))
		}
		if s != "" {
			params = append(params, "status="+s)
		}
		if len(params) == 0 {
			return "/admin/posts"
		}
		return "/admin/posts?" + strings.Join(params, "&")
	}
	f := Filter{
		Label: statusLabels[status],
		Aria:  "Filter by status: " + statusLabels[status],
	}
	for _, s := range []string{"", "draft", "published"} {
		f.Items = append(f.Items, FilterItem{Href: href(s), Label: statusLabels[s], Current: s == status})
	}
	return f
}

// NoMatchNote words the "nothing matched" note for the applied search
// and filter. Formatting stays in Go, where a test reaches it.
func NoMatchNote(q, status string) string {
	subject := map[string]string{"": "posts", "draft": "drafts", "published": "published posts"}[status]
	if q != "" {
		return fmt.Sprintf("No %s match “%s”.", subject, q)
	}
	return fmt.Sprintf("No %s yet.", subject)
}

// AdminFormView is GET /admin/posts/new and GET /admin/posts/{id}/edit,
// and the 400 re-render of either POST.
type AdminFormView struct {
	Head        Head
	Title       string // page-header title (the post's own, when editing)
	Sub         string // page-header subhead
	Action      string // the form's POST target
	FormTitle   string // the title field's current value
	Body        string // the textarea's current value
	Error       string // a validation message, rendered role="alert"
	ID          int64
	Published   bool
	StatusTone  string // status-pill Tone: neutral or positive
	StatusLabel string // status-pill Label: Draft or Published
}

// EditForm builds the edit screen's view model from a post. Resolving a
// raw published flag to a Tone/Label pair is product logic and stays
// here, in the app: status-pill takes the resolved pair by design.
func EditForm(p Post) AdminFormView {
	tone, label := "neutral", "Draft"
	if p.Published {
		tone, label = "positive", "Published"
	}
	return AdminFormView{
		Head:        Head{Title: p.Title},
		Title:       p.Title,
		Sub:         "Editing",
		Action:      fmt.Sprintf("/admin/posts/%d", p.ID),
		FormTitle:   p.Title,
		Body:        p.Body,
		ID:          p.ID,
		Published:   p.Published,
		StatusTone:  tone,
		StatusLabel: label,
	}
}

// ── Formatting, all of it in Go ────────────────────────────────────────
//
// The app registers no template funcs of its own: ui.Funcs() is the whole
// FuncMap. Every date, every meta line, every page item is computed here,
// where a test reaches it directly.

// FormatDate renders a timestamp the way the screens read it.
func FormatDate(t time.Time) string { return t.Format("2 January 2006") }

// PublishedLine is a published post's meta line.
func PublishedLine(t time.Time) string { return "Published " + FormatDate(t) }

// DraftLine is a draft's meta line: when it was last touched. Status
// lives in the row's pill, not here.
func DraftLine(t time.Time) string { return "Edited " + FormatDate(t) }

// PublicRows builds the public index's rows. No action pill: the row
// already goes to the only place it could go, and a second link to the
// same URL is noise with a tab stop.
func PublicRows(posts []Post) []Row {
	rows := make([]Row, 0, len(posts))
	for _, p := range posts {
		rows = append(rows, Row{
			Href: fmt.Sprintf("/posts/%d", p.ID),
			Main: p.Title,
			Sub:  FormatDate(p.CreatedAt),
		})
	}
	return rows
}

// AdminRows builds the admin list's rows. A draft gets no action pill:
// it has no public page to send anyone to, and list-row-action renders
// nothing when ActionHref is empty.
func AdminRows(posts []Post) []Row {
	rows := make([]Row, 0, len(posts))
	for _, p := range posts {
		row := Row{
			Href:        fmt.Sprintf("/admin/posts/%d/edit", p.ID),
			Main:        p.Title,
			Sub:         DraftLine(p.UpdatedAt),
			StatusTone:  "neutral",
			StatusLabel: "Draft",
		}
		if p.Published {
			row.Sub = PublishedLine(p.CreatedAt)
			row.StatusTone, row.StatusLabel = "positive", "Published"
			row.ActionHref = fmt.Sprintf("/posts/%d", p.ID)
			row.ActionLabel = "View"
			row.ActionAria = "View " + p.Title
		}
		rows = append(rows, row)
	}
	return rows
}

// Paragraphs splits a plain-text body on blank lines. Bodies are
// author-supplied plain text and stay that way: each paragraph is
// rendered as an ordinary {{.}} value, escaped by html/template, never
// template.HTML.
func Paragraphs(body string) []string {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	var out []string
	for _, chunk := range strings.Split(body, "\n\n") {
		if s := strings.TrimSpace(chunk); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// BuildPagination builds the page strip for a list of total items at a
// 1-based page number. Show is false at or below one page, and the page
// template guards the partial with it. status must be a NormalizeStatus
// result ("", "draft" or "published"): it is interpolated into each
// href unescaped, which every current caller satisfies.
func BuildPagination(base, q, status string, page, total int) Pagination {
	p := Pagination{Show: total > PageSize}
	if !p.Show {
		return p
	}
	pages := (total + PageSize - 1) / PageSize
	if page < 1 {
		page = 1
	}
	if page > pages {
		page = pages
	}

	// Built by hand rather than with url.Values.Encode, which sorts its
	// keys and would emit page before q.
	href := func(n int) string {
		var params []string
		if q != "" {
			params = append(params, "q="+url.QueryEscape(q))
		}
		if status != "" {
			params = append(params, "status="+status)
		}
		params = append(params, "page="+strconv.Itoa(n))
		return base + "?" + strings.Join(params, "&")
	}

	items := []PageItem{{Label: "Previous", Rel: "prev", Disabled: true}}
	if page > 1 {
		items[0] = PageItem{Label: "Previous", Rel: "prev", Href: href(page - 1)}
	}
	for _, n := range pageNumbers(page, pages) {
		switch {
		case n == 0:
			items = append(items, PageItem{Gap: true})
		case n == page:
			items = append(items, PageItem{Label: strconv.Itoa(n), Current: true})
		default:
			items = append(items, PageItem{Label: strconv.Itoa(n), Href: href(n)})
		}
	}
	next := PageItem{Label: "Next", Rel: "next", Disabled: true}
	if page < pages {
		next = PageItem{Label: "Next", Rel: "next", Href: href(page + 1)}
	}
	p.Items = append(items, next)
	return p
}

// pageNumbers returns the page numbers to render, with 0 standing for a
// gap. Up to seven pages every number renders; beyond that it is
// first · gap · current±1 · gap · last. A gap needs 71 posts to appear,
// so this app builds it correctly and never renders it — the library's
// own fixtures cover that item kind.
func pageNumbers(page, pages int) []int {
	if pages <= 7 {
		out := make([]int, 0, pages)
		for n := 1; n <= pages; n++ {
			out = append(out, n)
		}
		return out
	}
	want := map[int]bool{1: true, pages: true, page: true}
	if page-1 >= 1 {
		want[page-1] = true
	}
	if page+1 <= pages {
		want[page+1] = true
	}
	var out []int
	prev := 0
	for n := 1; n <= pages; n++ {
		if !want[n] {
			continue
		}
		if prev != 0 && n != prev+1 {
			out = append(out, 0)
		}
		out = append(out, n)
		prev = n
	}
	return out
}
