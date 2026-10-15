// genrender.go is render.go's generated-adapter half: the pieces
// GenRender needs specifically because manifest/bookmarks.toml's
// screens (gen/templates/bookmarks/*.html) render through here, inside
// the same chrome the hand screens use (templates/genlayout.html —
// layout.html's generated-screen twin), but a generated view model
// (listView, formView, ...) carries no Flash/HasFlash/SignedIn of its
// own — those come from the live request, not the manifest.
//
// The one trick worth reading: rastrillo.RenderFunc carries no
// *http.Request, but the generated router builds a fresh Ctx per
// request (gen.Router's ctxFactory), so App hands each request a
// Render CLOSURE over its own r (GenRender(r)) — which is how a
// generated screen still gets the live flash and the real signed-in
// state without the seam growing a request parameter.
package notes

import (
	"bytes"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"strings"

	notesassets "notes"
	genlocales "notes/gen/locales"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/flash"
	"amadan.net/rastrillo/rastrillo/locales"
	"amadan.net/rastrillo/rastrillo/sessions"
	"amadan.net/rastrillo/rastrillo/ui"
)

// genTemplatesSub is gen/templates, rooted so each page's path reads
// "<resource>/<page>.html" — internal/generate/actions.go's page-name
// contract. It is embedded at compile time (genassets.go), so this
// can only fail if that embed directive and this Sub ever disagree.
func genTemplatesSub() fs.FS {
	sub, err := fs.Sub(notesassets.GenTemplatesFS, "gen/templates")
	if err != nil {
		panic(err)
	}
	return sub
}

// genPageFiles is one ui.PageFile per generated screen under sub,
// keyed "<resource>/<page>" (internal/generate/actions.go's page-name
// contract) — tickets' internal/tickets/render.go pattern (Task 23),
// restated for notes' second resource.
func genPageFiles(sub fs.FS) (map[string]ui.PageFile, error) {
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
		return nil, fmt.Errorf("notes: no generated page templates embedded")
	}
	return out, nil
}

// newGenPages builds the generated-screen ui.Pages: genlayout.html as
// the layout, every bookmarks/* screen genPageFiles resolved as a page
// — see render.go's newPages doc comment for why this is a second
// ui.Pages rather than sharing the hand screens' one.
func newGenPages(cfg locales.Config) (*ui.Pages, error) {
	files, err := genPageFiles(genTemplatesSub())
	if err != nil {
		return nil, err
	}
	return ui.NewPages(ui.PagesConfig{
		App:     templatesFS,
		Layout:  "templates/genlayout.html",
		Pages:   files,
		Locale:  cfg,
		Catalog: genlocales.BaseCatalog,
	})
}

// notAViewMsg is Appendix C.2's op.render.not_a_view, verbatim: data
// that is not a generated view carries none of the request values
// (L, Locales, Return) the layout needs, so this fails loudly rather
// than let a type-assertion panic hide which page was wrong, or let
// strict rendering turn it into a less specific template error.
const notAViewMsg = "page %q: the data has no request values, because it is not a generated view; nothing was rendered"

// GenRender returns the rastrillo.RenderFunc for one request: the
// closure is what lets a generated screen read this request's flash
// and signed-in state through a seam that never sees r itself. App's
// ctx factory calls it per request.
func (a *app) GenRender(r *http.Request) rastrillo.RenderFunc {
	return func(ctx *rastrillo.Ctx, w http.ResponseWriter, name string, status int, data any) {
		v, ok := data.(interface{ Request() ui.Localized })
		if !ok {
			genFail(ctx, w, fmt.Sprintf(notAViewMsg, name), nil)
			return
		}
		fl, hasFlash := flash.Take(w, r)
		_, signedIn := sessions.Current(r)
		pd := page{Localized: v.Request(), Flash: fl, HasFlash: hasFlash, SignedIn: signedIn, Content: data}
		var buf bytes.Buffer
		if err := a.genPages.Execute(&buf, name, pd); err != nil {
			genFail(ctx, w, "rendering "+name, err)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(status)
		buf.WriteTo(w)
	}
}

// genFail logs through Ctx.Logger and answers a plain 500. err is a
// structured slog attribute, not folded into what: a nil err (the
// not-a-view failure, which has none) still logs cleanly as err=<nil>
// rather than losing the field shape every other caller here relies
// on to grep by.
func genFail(ctx *rastrillo.Ctx, w http.ResponseWriter, what string, err error) {
	if ctx != nil && ctx.Logger != nil {
		ctx.Logger.Error("notes: "+what, "err", err)
	}
	http.Error(w, "something went wrong", http.StatusInternalServerError)
}
