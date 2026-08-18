// Package tickets is the whole hand-written half of this example: the
// Ctx.Render seam every generated action calls into (internal/
// generate/actions.go's package doc), and the one layout every screen
// shares. Nothing else lives here — no store code, no view models, no
// formatting helpers — because there is nothing else to write: this
// app declares its one resource in manifest/ticket_types.toml and
// ships whatever `rastrillo generate` produced for it, unedited and
// unejected (see the module README).
package tickets

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"path"
	"strings"

	ticketsassets "tickets"
	"tickets/gen/locales"

	"github.com/carlosframework/rastrillo"
	"github.com/carlosframework/rastrillo/ui"
)

//go:embed templates/layout.html
var layoutFS embed.FS

// pages is one template tree per generated screen, built once at
// process start and keyed "<resource>/<page>" — internal/generate/
// actions.go's page-name contract, restated in that package's own doc
// comment. Unlike the blog's buildPages (internal/blog/view.go),
// there is no bare-keyed entry for a hand screen and no second walk
// over an ejection root: gen/templates is the only template source
// this app has.
var pages = buildPages()

// buildPages parses ui's partials and the layout into a base tree,
// then clones that base once per generated screen — the clone is what
// lets every screen file define "content" under the same name without
// the last one clobbering the rest (see the blog's buildPages doc for
// the fuller explanation; the mechanism is identical here). T resolves
// against gen/locales.BaseCatalog (genT, below) for every screen.
func buildPages() map[string]*template.Template {
	base := template.New("").Funcs(ui.Funcs()).Funcs(template.FuncMap{"T": genT})
	base = template.Must(base.ParseFS(ui.Templates(), "*.html"))
	base = template.Must(base.ParseFS(layoutFS, "templates/layout.html"))

	sub, err := fs.Sub(ticketsassets.GenTemplatesFS, "gen/templates")
	if err != nil {
		panic(err) // gen/templates is embedded at compile time (genassets.go)
	}
	resDirs, err := fs.ReadDir(sub, ".")
	if err != nil {
		panic(err)
	}

	out := make(map[string]*template.Template)
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
			t := template.Must(base.Clone())
			t = template.Must(t.ParseFS(sub, f))
			out[key] = t
		}
	}
	if len(out) == 0 {
		panic("tickets: no generated page templates embedded")
	}
	return out
}

// genT resolves a manifest catalog key against gen/locales.BaseCatalog
// directly — a plain map lookup, not rastrillo.T(r, key)/Locales.
// Middleware. This app is monolingual today, so wiring Options.Locales
// would install locale-prefix routing (/en/...) nothing here asked
// for; genT reads the same map main.go wires as Options.BaseCatalog,
// so the two can't drift apart (see the blog's genT, genrender.go, the
// same reasoning). A missing key renders as the key itself, matching
// (*rastrillo.Locales).T's own fallback.
func genT(key string) string {
	if v, ok := locales.BaseCatalog[key]; ok {
		return v
	}
	return key
}

// Head carries what the layout's <head> needs.
type Head struct{ Title string }

// pageData is "layout"'s one data value: Head carries what <head>
// needs, Data is passed through to "content" untouched.
type pageData struct {
	Head Head
	Data any
}

// genHead computes the Head value every screen gets. Every value a
// generated action hands Render (formView, showView, listView —
// internal/generate/actions.go) carries no Head field at all — the
// action emitter doesn't know about an app's layout, by design — and
// this app adds no hand wrapper on top of any of them (contrast the
// blog's headFor/headField, which reflects a Head off a hand view
// model first and only falls back to a page-name default for the one
// screen, posts/show, that still had none). Every screen here is that
// same case, so genHead is the only rule this app needs.
func genHead(page string) Head {
	switch {
	case strings.HasSuffix(page, "/list"):
		return Head{Title: "Ticket types"}
	case strings.HasSuffix(page, "/show"):
		return Head{Title: "Ticket type"}
	default:
		return Head{Title: "Ticket type"}
	}
}

// Render executes one page's "layout" into a buffer and only then
// writes the status and the bytes, so a template error is a clean 500
// rather than half a page followed by a stack trace.
//
// This is the one seam every generated action's ctx.Render call
// resolves to (main.go wires it: &rastrillo.Ctx{Render: tickets.
// Render}) — the only render adapter this app has, since there is no
// hand screen and no ejected template needing anything more (contrast
// the blog's Render, which also special-cases "posts/form" to enrich
// the generated data with hand-owned fields; this app never does).
func Render(ctx *rastrillo.Ctx, w http.ResponseWriter, page string, status int, data any) {
	t, ok := pages[page]
	if !ok {
		Fail(ctx, w, "rendering "+page, fmt.Errorf("no such page template"))
		return
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "layout", pageData{Head: genHead(page), Data: data}); err != nil {
		Fail(ctx, w, "rendering "+page, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	w.Write(buf.Bytes())
}

// Fail logs through Ctx.Logger and answers a plain 500. Every store
// error a generated action doesn't already handle itself lands here
// only through this package's own Render — the generated actions call
// their own unexported fail helper directly (internal/generate/
// actions.go's helperFuncs), never this one.
func Fail(ctx *rastrillo.Ctx, w http.ResponseWriter, what string, err error) {
	if ctx != nil && ctx.Logger != nil {
		ctx.Logger.Error("tickets: "+what, "err", err)
	}
	http.Error(w, "Something went wrong.", http.StatusInternalServerError)
}
