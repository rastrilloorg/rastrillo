package screens

import (
	"embed"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"

	rastrillo "github.com/carlosframework/rastrillo"
	"github.com/carlosframework/rastrillo/ui"
)

//go:embed templates/*.html
var templatesFS embed.FS

var (
	baseTmplOnce sync.Once
	baseTmpl     *template.Template
	baseTmplErr  error
)

// tmpl assembles the template set: ui partials + the screen templates,
// with any Deps.Templates definitions layered last so they win — the
// same override-by-existence idea, applied to templates at runtime.
func tmpl(deps Deps) (*template.Template, error) {
	baseTmplOnce.Do(func() {
		t := template.New("").Funcs(ui.Funcs())
		if t, baseTmplErr = t.ParseFS(ui.Templates(), "*.html"); baseTmplErr != nil {
			return
		}
		baseTmpl, baseTmplErr = t.ParseFS(templatesFS, "templates/*.html")
	})
	if baseTmplErr != nil {
		return nil, baseTmplErr
	}
	if deps.Templates == nil {
		return baseTmpl, nil
	}
	merged, err := baseTmpl.Clone()
	if err != nil {
		return nil, err
	}
	for _, o := range deps.Templates.Templates() {
		if o.Tree == nil {
			continue
		}
		if _, err := merged.AddParseTree(o.Name(), o.Tree); err != nil {
			return nil, err
		}
	}
	return merged, nil
}

func render(ctx *rastrillo.Ctx, deps Deps, w http.ResponseWriter, name string, status int, data any) {
	t, err := tmpl(deps)
	if err != nil {
		fail(ctx, w, "parsing templates", err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := t.ExecuteTemplate(w, name, data); err != nil && ctx.Logger != nil {
		ctx.Logger.Error("screens: render "+name, "err", err)
	}
}

func cssPath(deps Deps) string {
	if deps.CSSPath != "" {
		return deps.CSSPath
	}
	return "/static/tokens.css"
}

// --- view models ---

type pageItem struct {
	Label    string
	Href     string
	Current  bool
	Disabled bool
	Gap      bool
}

type listView struct {
	CSS      string
	Title    string
	NewURL   string
	NewLabel string
	Search   bool
	BaseURL  string
	Query    string
	Filters  []filterView
	Headers  []string
	Rows     []rowView
	Empty    bool
	EmptyMsg string
	Pages    []pageItem
}

type filterView struct {
	Label   string
	Name    string // the query-param name (snake of the field)
	Options []optionView
}

type optionView struct {
	Value, Label string
	Selected     bool
}

type rowView struct {
	ShowURL string
	Cells   []template.HTML
}

type fieldView struct {
	Label    string
	Name     string // control name (snake)
	Control  string // "text" | "textarea" | "checkbox" | "select" | "file" | "readonly"
	Value    string
	Checked  bool
	Required bool
	Options  []optionView
	Hint     string
}

type formView struct {
	CSS      string
	Title    string
	BackURL  string
	Error    string
	Sections []sectionView
}

type sectionView struct {
	Title  string // empty for a single-form screen
	Action string
	Fields []fieldView
	Submit string
}

type confirmView struct {
	CSS       string
	Title     string
	Sentence  string
	Action    string
	CancelURL string
}

type showView struct {
	CSS       string
	Title     string
	EditURL   string
	DeleteURL string
	BackURL   string
	Pairs     []pairView
}

type pairView struct {
	Label, Value string
}

// cellHTML renders one list cell: a Render func's trusted HTML, or the
// kind's default display, escaped.
func cellHTML(res rastrillo.Resource, col rastrillo.Column, row map[string]any, r *http.Request) template.HTML {
	if col.Render != nil {
		return col.Render(row)
	}
	f, ok := res.FieldByName(col.Field)
	if !ok {
		f = rastrillo.Field{Name: col.Field, Kind: col.Kind}
	}
	return template.HTML(template.HTMLEscapeString(display(f, row[col.Field])))
}

// buildListView assembles the list screen.
func buildListView(r *http.Request, res rastrillo.Resource, deps Deps, rows []map[string]any, total, page int, q string, filters map[string]string) listView {
	base := resolveRoute(res.Route, r)
	v := listView{
		CSS:      cssPath(deps),
		Title:    title(r, res),
		NewURL:   base + "/new",
		NewLabel: "New " + singularTitle(res),
		Search:   res.List.Search,
		BaseURL:  base,
		Query:    q,
		Empty:    total == 0,
		EmptyMsg: "No " + strings.ReplaceAll(res.Name, "_", " ") + " yet.",
	}
	for _, name := range res.List.Filter {
		f, _ := res.FieldByName(name)
		fv := filterView{Label: label(r, res, name), Name: rastrillo.SnakeCase(name)}
		fv.Options = append(fv.Options, optionView{Value: "", Label: "All", Selected: filters[name] == ""})
		if f.Kind == rastrillo.Bool {
			for _, o := range []optionView{{Value: "1", Label: "Yes"}, {Value: "0", Label: "No"}} {
				o.Selected = filters[name] == o.Value
				fv.Options = append(fv.Options, o)
			}
		} else {
			for _, o := range f.Options {
				fv.Options = append(fv.Options, optionView{Value: o, Label: o, Selected: filters[name] == o})
			}
		}
		v.Filters = append(v.Filters, fv)
	}
	for _, c := range res.List.Columns {
		v.Headers = append(v.Headers, label(r, res, c.Field))
	}
	for _, row := range rows {
		rv := rowView{ShowURL: base + "/" + url.PathEscape(fmt.Sprint(row["ID"]))}
		for _, c := range res.List.Columns {
			rv.Cells = append(rv.Cells, cellHTML(res, c, row, r))
		}
		v.Rows = append(v.Rows, rv)
	}
	v.Pages = pageItems(base, r.URL.Query(), page, (total+pageSize-1)/pageSize)
	return v
}

// pageItems builds the pagination strip, carrying the current query.
func pageItems(base string, query url.Values, page, pages int) []pageItem {
	if pages <= 1 {
		return nil
	}
	href := func(p int) string {
		q := url.Values{}
		for k, vs := range query {
			for _, v := range vs {
				q.Add(k, v)
			}
		}
		q.Set("page", strconv.Itoa(p))
		return base + "?" + q.Encode()
	}
	var items []pageItem
	if page > 1 {
		items = append(items, pageItem{Label: "Previous", Href: href(page - 1)})
	} else {
		items = append(items, pageItem{Label: "Previous", Disabled: true})
	}
	for p := 1; p <= pages; p++ {
		if p == page {
			items = append(items, pageItem{Label: strconv.Itoa(p), Current: true})
		} else {
			items = append(items, pageItem{Label: strconv.Itoa(p), Href: href(p)})
		}
	}
	if page < pages {
		items = append(items, pageItem{Label: "Next", Href: href(page + 1)})
	} else {
		items = append(items, pageItem{Label: "Next", Disabled: true})
	}
	return items
}

func singularTitle(res rastrillo.Resource) string {
	return rastrillo.TitleCase(singular(rastrillo.SnakeCase(res.Name)))
}

// buildFields turns a field list plus a row (nil for a blank form) into
// controls.
func buildFields(r *http.Request, res rastrillo.Resource, fields []rastrillo.Field, row map[string]any) []fieldView {
	var out []fieldView
	for _, f := range fields {
		fv := fieldView{
			Label:    label(r, res, f.Name),
			Name:     rastrillo.SnakeCase(f.Name),
			Required: f.Required,
		}
		var v any
		if row != nil {
			v = row[f.Name]
		}
		switch {
		case f.Derived:
			fv.Control = "readonly"
			fv.Value = display(f, v)
		case f.Kind == rastrillo.LongText:
			fv.Control = "textarea"
			fv.Value = formValue(f, v)
		case f.Kind == rastrillo.Bool:
			fv.Control = "checkbox"
			fv.Checked, _ = v.(bool)
		case f.Kind == rastrillo.Select:
			fv.Control = "select"
			cur := formValue(f, v)
			if !f.Required {
				fv.Options = append(fv.Options, optionView{Value: "", Label: "—", Selected: cur == ""})
			}
			for _, o := range f.Options {
				fv.Options = append(fv.Options, optionView{Value: o, Label: o, Selected: cur == o})
			}
		case f.Kind == rastrillo.Blob:
			fv.Control = "file"
			fv.Hint = blobHint(f, v)
		default:
			fv.Control = "text"
			fv.Value = formValue(f, v)
			if f.Kind == rastrillo.Time {
				fv.Hint = "RFC3339, e.g. 2026-08-17T09:00:00Z"
			}
			if f.Kind == rastrillo.Money {
				fv.Hint = "an amount like 12.34"
			}
		}
		out = append(out, fv)
	}
	return out
}

func blobHint(f rastrillo.Field, v any) string {
	if s := display(f, v); s != "" {
		return "current: " + s + " — choose a file to replace it"
	}
	return ""
}
