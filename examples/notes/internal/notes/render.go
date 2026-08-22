package notes

import (
	"bytes"
	"embed"
	"html/template"
	"log/slog"
	"net/http"

	"github.com/carlosframework/rastrillo/flash"
	"github.com/carlosframework/rastrillo/form"
	"github.com/carlosframework/rastrillo/password"
	"github.com/carlosframework/rastrillo/sessions"
)

//go:embed templates
var templatesFS embed.FS

// pages is one *template.Template per page, each combining layout.html
// with that page's own file — kept separate so every page can define
// a template named "content" without colliding with the others (a
// single shared tree parsed from all files at once would let the last
// "content" definition silently win over the rest).
var pages = map[string]*template.Template{}

func init() {
	for _, name := range []string{"signin", "signup", "index", "show", "new", "edit"} {
		pages[name] = template.Must(template.New("layout").ParseFS(templatesFS,
			"templates/layout.html", "templates/"+name+".html"))
	}
}

// page is the data every template renders against: layout.html reads
// Flash/HasFlash/SignedIn for its chrome, and each page's own content
// block reads Content, cast back to its own view type.
type page struct {
	Flash    flash.Flash
	HasFlash bool
	SignedIn bool
	Content  any
}

// indexView, noteView and formView are the per-page Content types.
// formView serves both new (a blank Note, no Errors) and edit/the
// 422 re-render (a Note carrying the submitted values, Errors set).
type (
	indexView struct{ Notes []Note }
	noteView  struct{ Note Note }
	formView  struct {
		Note   Note
		Errors form.Errors
	}
)

// renderStatus takes the flash exactly once, resolves whether a
// session is current (for layout's nav), and executes name's layout
// into a buffer before anything touches the wire. The buffer matters
// twice: a template error becomes a clean 500 instead of garbage
// appended to a half-written page, and flash.Take's clearing
// Set-Cookie lands before the status line — headers added after
// WriteHeader are silently dropped, which is exactly how a 422
// re-render used to show the same flash twice. status 0 means let the
// first body write imply 200.
func renderStatus(w http.ResponseWriter, r *http.Request, status int, name string, content any) {
	fl, ok := flash.Take(w, r)
	_, signedIn := sessions.Current(r)
	data := page{Flash: fl, HasFlash: ok, SignedIn: signedIn, Content: content}
	var buf bytes.Buffer
	if err := pages[name].ExecuteTemplate(&buf, "layout", data); err != nil {
		slog.Default().Error("notes: render "+name, "err", err)
		http.Error(w, "something went wrong", http.StatusInternalServerError)
		return
	}
	if status != 0 {
		w.WriteHeader(status)
	}
	buf.WriteTo(w)
}

func renderContent(w http.ResponseWriter, r *http.Request, name string, content any) {
	renderStatus(w, r, 0, name, content)
}

// renderSignin and renderSignup are password.Config's RenderSignin/
// RenderSignup: password.go already writes the 422 status itself
// before calling either on a failed attempt, so these never touch the
// status line.
func renderSignin(w http.ResponseWriter, r *http.Request, d password.PageData) {
	renderContent(w, r, "signin", d)
}

func renderSignup(w http.ResponseWriter, r *http.Request, d password.PageData) {
	renderContent(w, r, "signup", d)
}
