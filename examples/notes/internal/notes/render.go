package notes

import (
	"bytes"
	"embed"
	"log/slog"
	"net/http"

	genlocales "notes/gen/locales"

	"amadan.net/rastrillo/rastrillo/apitoken"
	"amadan.net/rastrillo/rastrillo/flash"
	"amadan.net/rastrillo/rastrillo/form"
	"amadan.net/rastrillo/rastrillo/jobs"
	"amadan.net/rastrillo/rastrillo/locales"
	"amadan.net/rastrillo/rastrillo/password"
	"amadan.net/rastrillo/rastrillo/sessions"
	"amadan.net/rastrillo/rastrillo/ui"
)

//go:embed templates
var templatesFS embed.FS

// handPageFiles is one ui.PageFile per hand screen this app serves,
// each combining layout.html with that page's own file — kept
// separate so every page can define a template named "content"
// without colliding with the others: ui.NewPages clones the base once
// per key, which is what keeps a single shared tree from letting the
// last "content" definition silently win over the rest.
func handPageFiles() map[string]ui.PageFile {
	out := map[string]ui.PageFile{}
	for _, name := range []string{"signin", "signup", "index", "show", "new", "edit", "status", "tokens"} {
		out[name] = ui.PageFile{FS: templatesFS, Path: "templates/" + name + ".html"}
	}
	return out
}

// fragmentPageFiles backs the one-page ui.Pages renderJobFragment
// executes directly: "job-status" is already defined by ui.Templates()
// (every ui.Pages base parses the partials first), so the file named
// here never itself has to supply it — this map exists only because
// ui.NewPages' Pages needs at least one file per clone, and
// templates/status.html is an unused but already-embedded one to
// point it at, rather than a file invented just to be legal.
func fragmentPageFiles() map[string]ui.PageFile {
	return map[string]ui.PageFile{"job-status": {FS: templatesFS, Path: "templates/status.html"}}
}

// newPages and newFragPages build this app's hand-screen ui.Pages and
// its job-status fragment one. Both share cfg, genlocales.BaseCatalog
// and templatesFS; newGenPages (genrender.go) is their generated-screen
// twin, kept as a separate ui.Pages because it needs its own layout
// (genlayout.html) — the two cannot share a base, since each defines
// its own "layout" and a shared base would let the second clobber the
// first.
//
// fragPages needs its own instance for a different reason: it has no
// layout at all and its Entry is "job-status", not "layout" — one
// ui.Pages has one Entry for every page it holds (PagesConfig's own
// doc comment), so a fragment needing a different one gets a second
// instance rather than widening that API.
func newPages(cfg locales.Config) (*ui.Pages, error) {
	return ui.NewPages(ui.PagesConfig{
		App:     templatesFS,
		Layout:  "templates/layout.html",
		Pages:   handPageFiles(),
		Locale:  cfg,
		Catalog: genlocales.BaseCatalog,
	})
}

func newFragPages(cfg locales.Config) (*ui.Pages, error) {
	return ui.NewPages(ui.PagesConfig{
		App:     templatesFS,
		Pages:   fragmentPageFiles(),
		Entry:   "job-status",
		Locale:  cfg,
		Catalog: genlocales.BaseCatalog,
	})
}

// page is the data every hand template renders against: layout.html
// reads Flash/HasFlash/SignedIn for its chrome and L/Locales/Return
// (the embedded ui.Localized) for the switcher, and each page's own
// content block reads Content, cast back to its own view type.
// renderStatus sets Localized from ui.ForRequest(r) itself — unlike
// GenRender, there is no generated view to read it off.
type page struct {
	ui.Localized
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
func (a *app) renderStatus(w http.ResponseWriter, r *http.Request, status int, name string, content any) {
	fl, ok := flash.Take(w, r)
	_, signedIn := sessions.Current(r)
	data := page{Localized: ui.ForRequest(r), Flash: fl, HasFlash: ok, SignedIn: signedIn, Content: content}
	var buf bytes.Buffer
	if err := a.pages.Execute(&buf, name, data); err != nil {
		slog.Default().Error("notes: render "+name, "err", err)
		http.Error(w, "something went wrong", http.StatusInternalServerError)
		return
	}
	if status != 0 {
		w.WriteHeader(status)
	}
	buf.WriteTo(w)
}

func (a *app) renderContent(w http.ResponseWriter, r *http.Request, name string, content any) {
	a.renderStatus(w, r, 0, name, content)
}

// renderSignin and renderSignup are password.Config's RenderSignin/
// RenderSignup: password.go writes the status itself before calling
// either — 422 for a failed attempt, 403 for a Create refusal, 429
// when the rate limiter trips — so these never touch the status line.
func (a *app) renderSignin(w http.ResponseWriter, r *http.Request, d password.PageData) {
	a.renderContent(w, r, "signin", d)
}

func (a *app) renderSignup(w http.ResponseWriter, r *http.Request, d password.PageData) {
	a.renderContent(w, r, "signup", d)
}

// renderTokens is apitoken.HandlersConfig.Render. The handlers have
// already set Cache-Control and any non-200 status, so this goes
// through the ordinary page path like every other hand screen; the
// template hands d to ui's api-tokens partial.
func (a *app) renderTokens(w http.ResponseWriter, r *http.Request, d apitoken.PageData) {
	a.renderContent(w, r, "tokens", d)
}

// renderJobPage and renderJobFragment are jobs.Config's two render
// seams (app.go wires them into jobs.NewHandlers). renderJobPage goes
// through the ordinary page path — same layout, same flash/signed-in
// chrome as every other page. renderJobFragment does not: it executes
// "job-status" alone, through fragPages (this file's own doc on
// newFragPages), because the fragment is swapped into a page that
// already has a layout, and a full page (or even a flash) inside it
// would be a layout nested inside a layout the next time the shim
// polls.
func (a *app) renderJobPage(w http.ResponseWriter, r *http.Request, d jobs.PageData) {
	a.renderContent(w, r, "status", statusView(d))
}

func (a *app) renderJobFragment(w http.ResponseWriter, r *http.Request, d jobs.PageData) {
	data := statusView(d)
	// job-status itself calls no locale function (ui/partials/
	// job-status.html), but ui.Pages.Execute still requires a
	// non-nil L: the guarantee is "this data carries a localizer",
	// not "this template happens to use one".
	data["L"] = ui.ForRequest(r).L
	if err := a.fragPages.Execute(w, "job-status", data); err != nil {
		slog.Default().Error("notes: render job fragment", "err", err)
	}
}

// statusView flattens jobs.PageData into the dict-shaped keys the
// job-status partial documents (ui/partials/job-status.html), plus
// Running for status.html's own noscript meta-refresh check — the
// partial itself has no notion of "should this page keep refreshing",
// only of what to show for the current status.
func statusView(d jobs.PageData) map[string]any {
	return map[string]any{
		"Name": d.Job.Name, "Status": string(d.Job.Status),
		"Progress": d.Job.Progress, "Err": d.Job.Err,
		"PollURL": d.FragmentPath, "PollSeconds": d.PollSeconds,
		"PushURL": d.EventsPath,
		"Running": d.Job.Status == jobs.Running,
	}
}
