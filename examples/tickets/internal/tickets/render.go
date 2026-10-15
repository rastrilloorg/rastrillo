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
	"io/fs"
	"net/http"
	"path"
	"strings"

	ticketsassets "tickets"
	genlocales "tickets/gen/locales"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/locales"
	"amadan.net/rastrillo/rastrillo/ui"
)

//go:embed templates/layout.html
var layoutFS embed.FS

// App is this app's one template set and its render seam: a single
// ui.Pages built once at NewApp, in every locale cfg offers. Unlike
// the hand-rolled map[string]*template.Template this replaces, there
// is one base (ui's partials and this layout) parsed once, not once
// per language or request — NewPages' own doc comment.
type App struct {
	pages *ui.Pages
}

// genTemplatesSub is gen/templates, rooted so each page's path reads
// "<resource>/<page>.html" — internal/generate/actions.go's page-name
// contract. It is embedded at compile time (genassets.go), so this
// can only fail if that embed directive and this Sub ever disagree.
func genTemplatesSub() fs.FS {
	sub, err := fs.Sub(ticketsassets.GenTemplatesFS, "gen/templates")
	if err != nil {
		panic(err)
	}
	return sub
}

// pageFiles is one ui.PageFile per generated screen under sub, keyed
// "<resource>/<page>" so ui.NewPages clones the base once per screen:
// every screen file defines "content" under the same name, and only a
// clone per screen keeps the last one parsed from clobbering the rest
// (ui.NewPages' own doc comment covers the mechanism; this app used to
// do the walk and the cloning by hand before Task 23).
func pageFiles(sub fs.FS) (map[string]ui.PageFile, error) {
	resDirs, err := fs.ReadDir(sub, ".")
	if err != nil {
		return nil, err
	}
	out := map[string]ui.PageFile{}
	for _, rd := range resDirs {
		if !rd.IsDir() {
			continue
		}
		files, err := fs.Glob(sub, rd.Name()+"/*.html")
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			key := rd.Name() + "/" + strings.TrimSuffix(path.Base(f), ".html")
			out[key] = ui.PageFile{FS: sub, Path: f}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("tickets: no generated page templates embedded")
	}
	return out, nil
}

// NewApp builds the one template set this app serves from: the
// generated screens cloned onto this app's layout, strict in every
// locale cfg offers (ui.Pages' own doc comment). cfg is
// locales.CheckConfig's result — main.go gets it from
// rastrillo.Handler's own validation, this call is only to read the
// value back.
//
// This app ships no locales/<code>.toml of its own (genlocales.
// BaseCatalog is English only), so an offered locale with no app
// translation falls back to English for app keys while the
// framework's own chrome (the switcher, money) still renders in that
// locale — the layered catalogs' contract (ui.Pages' doc comment),
// not a gap this app has to close.
func NewApp(cfg locales.Config) (*App, error) {
	pages, err := pageFiles(genTemplatesSub())
	if err != nil {
		return nil, err
	}
	p, err := ui.NewPages(ui.PagesConfig{
		App:     layoutFS,
		Layout:  "templates/layout.html",
		Pages:   pages,
		Locale:  cfg,
		Catalog: genlocales.BaseCatalog,
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

// Head carries what the layout's <head> needs.
type Head struct{ Title string }

// pageData is "layout"'s one data value. It embeds the ui.Localized
// copied off the view Render is handed, so the layout finds L (strict
// dispatch needs it non-nil), Locales and Return under their usual
// names (Task 14) without this app re-deriving any of them from the
// request itself. Head carries what <head> needs, and Data is passed
// through to "content" untouched.
type pageData struct {
	ui.Localized
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

// notAViewMsg is Appendix C.2's op.render.not_a_view, verbatim: data
// that is not a generated view carries none of the request values
// (L, Locales, Return) the layout needs, so this fails loudly rather
// than let a type-assertion panic hide which page was wrong, or let
// strict rendering turn it into a less specific template error.
const notAViewMsg = "page %q: the data has no request values, because it is not a generated view; nothing was rendered"

// Render wraps data (a generated view: formView, listView, showView
// or confirmView) in the layout's data and executes the one template
// set NewApp built, writing the status and the bytes only once the
// whole page has rendered — ui.Pages.Execute's own guarantee, so a
// template error is a clean 500 rather than half a page followed by
// whatever came after it failed.
//
// This is the one seam every generated action's ctx.Render call
// resolves to (main.go wires it: &rastrillo.Ctx{Render: app.Render}) —
// the only render adapter this app has, since there is no hand screen
// and no ejected template needing anything more (contrast the blog's
// Render, which also special-cases "posts/form" to enrich the
// generated data with hand-owned fields; this app never does).
func (a *App) Render(ctx *rastrillo.Ctx, w http.ResponseWriter, page string, status int, data any) {
	v, ok := data.(interface{ Request() ui.Localized })
	if !ok {
		a.fail(ctx, w, fmt.Sprintf(notAViewMsg, page))
		return
	}
	pd := pageData{Localized: v.Request(), Head: genHead(page), Data: data}
	var buf bytes.Buffer
	if err := a.pages.Execute(&buf, page, pd); err != nil {
		a.fail(ctx, w, fmt.Sprintf("rendering %s: %v", page, err))
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	w.Write(buf.Bytes())
}

// fail logs through Ctx.Logger and answers a plain 500 — the same
// shape the generated actions' own unexported fail helper uses
// (internal/generate/actions.go's helperFuncs), kept here only for
// this package's one Render seam.
func (a *App) fail(ctx *rastrillo.Ctx, w http.ResponseWriter, what string) {
	if ctx != nil && ctx.Logger != nil {
		ctx.Logger.Error("tickets: " + what)
	}
	http.Error(w, "Something went wrong.", http.StatusInternalServerError)
}
