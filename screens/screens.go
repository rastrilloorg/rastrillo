// Package screens is the manifest system's runtime: the code the
// generated actions call. A rastrillo.Resource declares a screen set —
// List → Show → Edit/New, plus the confirm-page delete flow — and this
// package renders and persists it, composing the ui partials, the
// blog-store SQL shape (Exclusive) or rastrillo/eventlog (Mergeable),
// and rastrillo/blobs for Blob fields.
//
// Nothing here decides a route: the generator emits one thin action per
// screen (skipped wherever a hand-written file exists at the same
// computed path — override-by-existence), and each action is one call
// into this package with its Resource value. Ejecting a screen is
// copying that generated action out and writing your own.
//
// Wiring: an app whose resources need more than ctx.DB — a blob store,
// an event log for Mergeable resources, template overrides — returns a
// Deps from its Ctx.Scope (the DepsProvider interface). Everything else
// works with a zero Deps.
package screens

import (
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"

	rastrillo "github.com/carlosframework/rastrillo"
	"github.com/carlosframework/rastrillo/blobs"
	"github.com/carlosframework/rastrillo/eventlog"
)

// pageSize is the list screen's page length — the blog example's 20.
const pageSize = 20

// Deps is what a resource screen may need beyond ctx.DB. An app
// provides it by making its Ctx.Scope implement DepsProvider; every
// field is optional until a manifest feature needs it (Blob fields need
// Blobs; a Mergeable resource needs Events), and the error when one is
// missing names exactly that.
type Deps struct {
	// Blobs stores Blob-kind field bytes. Wire blobs.S3FromEnv() on the
	// platform, blobs.Inline/Dir elsewhere.
	Blobs blobs.Store

	// Events is the app's event log, required by Mergeable resources.
	Events *eventlog.Log

	// Templates overrides the built-in screen templates: any template
	// name defined here (screen_layout, screen_list, screen_form,
	// screen_confirm, screen_show) wins over the default.
	Templates *template.Template

	// CSSPath is the stylesheet the standalone screen layout links —
	// default "/static/tokens.css", where rastrillo new puts it.
	CSSPath string
}

// DepsProvider is implemented by an app's Ctx.Scope to hand screens
// their dependencies.
type DepsProvider interface {
	ScreenDeps() Deps
}

func depsOf(ctx *rastrillo.Ctx) Deps {
	if p, ok := ctx.Scope.(DepsProvider); ok {
		return p.ScreenDeps()
	}
	return Deps{}
}

// engine is the storage half of a screen set, one per StoreKind.
type engine interface {
	list(q string, filters map[string]string, page int) (rows []map[string]any, total int, err error)
	get(id string) (map[string]any, error)
	create(vals map[string]any, actor string) (id string, err error)
	update(id string, vals map[string]any, actor string) error
	delete(id string, actor string) error
}

// errRowNotFound is any id that resolves to nothing — always a 404.
var errRowNotFound = errors.New("screens: no such row")

func engineFor(ctx *rastrillo.Ctx, deps Deps, res rastrillo.Resource) (engine, error) {
	if err := res.Validate(); err != nil {
		return nil, fmt.Errorf("screens: manifest %s: %w", res.Name, err)
	}
	switch res.Store {
	case rastrillo.Mergeable:
		if deps.Events == nil {
			return nil, fmt.Errorf("screens: resource %s is Mergeable but Deps.Events is nil — have Ctx.Scope implement screens.DepsProvider with an eventlog.Log", res.Name)
		}
		return &mergeableEngine{log: deps.Events, res: res}, nil
	default:
		if ctx.DB == nil {
			return nil, fmt.Errorf("screens: resource %s needs ctx.DB", res.Name)
		}
		return &exclusiveEngine{db: ctx.DB, res: res}, nil
	}
}

// fail logs and answers 500 without leaking the error to the page.
func fail(ctx *rastrillo.Ctx, w http.ResponseWriter, what string, err error) {
	if ctx.Logger != nil {
		ctx.Logger.Error("screens: "+what, "err", err)
	}
	http.Error(w, "something went wrong", http.StatusInternalServerError)
}

// sameOriginWrite refuses a state-changing request a current browser
// labels cross-site. Requests without the header (curl, tests, old
// clients) pass — this is the zero-config floor, not a substitute for
// the auth package's stricter check on authenticated apps.
func sameOriginWrite(w http.ResponseWriter, r *http.Request) bool {
	switch r.Header.Get("Sec-Fetch-Site") {
	case "", "same-origin", "none":
		return true
	}
	http.Error(w, "cross-origin form submission refused", http.StatusForbidden)
	return false
}

// resolveRoute substitutes the request's path values into a manifest
// route ("/{slug}/admin/ticket_types" under /acme/admin/ticket_types →
// "/acme/admin/ticket_types"), so every generated link stays inside the
// URL space the request came from.
func resolveRoute(route string, r *http.Request) string {
	segs := strings.Split(route, "/")
	for i, s := range segs {
		if strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}") {
			segs[i] = url.PathEscape(r.PathValue(s[1 : len(s)-1]))
		}
	}
	return strings.Join(segs, "/")
}

// label resolves a field's display label: the generated translation key
// first (§10 — "labels are translation keys by generation"), the
// title-cased field name when no catalog carries it.
func label(r *http.Request, res rastrillo.Resource, f string) string {
	key := "resource." + res.Name + ".field." + rastrillo.SnakeCase(f)
	if got := rastrillo.T(r, key); got != key {
		return got
	}
	return rastrillo.TitleCase(f)
}

// title resolves the resource's screen title the same way.
func title(r *http.Request, res rastrillo.Resource) string {
	key := "resource." + res.Name + ".title"
	if got := rastrillo.T(r, key); got != key {
		return got
	}
	return rastrillo.TitleCase(strings.ReplaceAll(res.Name, "_", " "))
}

// confirmSentence resolves the delete confirm-page sentence.
func confirmSentence(r *http.Request, res rastrillo.Resource) string {
	if res.Delete.Confirm != "" {
		return res.Delete.Confirm
	}
	key := "resource." + res.Name + ".delete.confirm"
	if got := rastrillo.T(r, key); got != key {
		return got
	}
	return "Delete this " + strings.ReplaceAll(singular(res.Name), "_", " ") + "? This cannot be undone."
}

// singular is a naive English singularization for default copy only —
// a catalog key overrides it the moment it reads wrongly.
func singular(name string) string {
	if strings.HasSuffix(name, "ies") {
		return strings.TrimSuffix(name, "ies") + "y"
	}
	return strings.TrimSuffix(name, "s")
}

// storedFields are the fields a save may write: every form field that
// is not Derived.
func storedFields(fields []rastrillo.Field) []rastrillo.Field {
	var out []rastrillo.Field
	for _, f := range fields {
		if !f.Derived {
			out = append(out, f)
		}
	}
	return out
}
