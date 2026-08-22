// genrender.go is the Ctx.Render seam for this app's generated half:
// manifest/bookmarks.toml's screens (gen/templates/bookmarks/*.html)
// render through here, inside the same chrome the hand screens use
// (templates/genlayout.html — layout.html's generated-screen twin).
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
	"html/template"
	"io/fs"
	"net/http"
	"path"
	"strings"

	notesassets "notes"
	"notes/gen/locales"

	"github.com/carlosframework/rastrillo"
	"github.com/carlosframework/rastrillo/flash"
	"github.com/carlosframework/rastrillo/sessions"
	"github.com/carlosframework/rastrillo/ui"
)

// genPages is one template tree per generated screen, keyed
// "<resource>/<page>" (internal/generate/actions.go's page-name
// contract) — built once at process start, tickets'
// internal/tickets/render.go pattern with this app's own chrome.
var genPages = buildGenPages()

func buildGenPages() map[string]*template.Template {
	base := template.New("").Funcs(ui.Funcs()).Funcs(template.FuncMap{"T": genT})
	base = template.Must(base.ParseFS(ui.Templates(), "*.html"))
	base = template.Must(base.ParseFS(templatesFS, "templates/genlayout.html"))

	sub, err := fs.Sub(notesassets.GenTemplatesFS, "gen/templates")
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
		panic("notes: no generated page templates embedded")
	}
	return out
}

// genT resolves a manifest catalog key against gen/locales.BaseCatalog
// directly — a plain map lookup; this app is monolingual, and a
// missing key renders as itself so a typo shows on the page instead of
// blanking a sentence (the same reasoning as tickets' genT).
func genT(key string) string {
	if v, ok := locales.BaseCatalog[key]; ok {
		return v
	}
	return key
}

// GenRender returns the rastrillo.RenderFunc for one request: the
// closure is what lets a generated screen read this request's flash
// and signed-in state through a seam that never sees r itself. App's
// ctx factory calls it per request.
func GenRender(r *http.Request) rastrillo.RenderFunc {
	return func(ctx *rastrillo.Ctx, w http.ResponseWriter, name string, status int, data any) {
		t, ok := genPages[name]
		if !ok {
			genFail(ctx, w, "rendering "+name, fmt.Errorf("no such page template"))
			return
		}
		fl, hasFlash := flash.Take(w, r)
		_, signedIn := sessions.Current(r)
		var buf bytes.Buffer
		if err := t.ExecuteTemplate(&buf, "layout", page{Flash: fl, HasFlash: hasFlash, SignedIn: signedIn, Content: data}); err != nil {
			genFail(ctx, w, "rendering "+name, err)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(status)
		buf.WriteTo(w)
	}
}

func genFail(ctx *rastrillo.Ctx, w http.ResponseWriter, what string, err error) {
	if ctx != nil && ctx.Logger != nil {
		ctx.Logger.Error("notes: "+what, "err", err)
	}
	http.Error(w, "something went wrong", http.StatusInternalServerError)
}
