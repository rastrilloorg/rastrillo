package ui

import (
	"fmt"
	"html/template"

	"github.com/carlosframework/rastrillo"
)

// Option configures Funcs. No options is the framework's own behaviour,
// so Funcs() and Funcs(nothing) are the same thing.
type Option func(*config)

type config struct {
	icon   func(string) template.HTML
	assets func() template.HTML
	t      func(key string, args ...any) string
}

// WithIcons points the icon and iconAssets seams at an app's own icon
// package — the one rastrillo new scaffolds:
//
//	tmpl := template.Must(template.New("").
//	        Funcs(ui.Funcs(ui.WithIcons(icons.Icon, icons.Assets))).
//	        ParseFS(ui.Templates(), "*.html"))
//
// Both functions are taken because neither can be derived from the
// other: assets is the <head> markup cdn and js delivery need, and an app
// that supplied only icon would leave those two modes silently broken. A
// nil for either leaves the framework default in place.
func WithIcons(icon func(string) template.HTML, assets func() template.HTML) Option {
	return func(c *config) {
		if icon != nil {
			c.icon = icon
		}
		if assets != nil {
			c.assets = assets
		}
	}
}

// WithT replaces the T entry — the seam for an app that wants ui's
// partial defaults resolved in the request's locale instead of the
// framework's hardcoded English. See FuncsWith for the per-request
// Clone discipline this requires.
func WithT(t func(key string, args ...any) string) Option {
	return func(c *config) {
		if t != nil {
			c.t = t
		}
	}
}

// Funcs returns the helpers ui's partials call, for an app to register on
// its own template tree:
//
//	tmpl := template.Must(template.New("").Funcs(ui.Funcs()).
//	        ParseFS(ui.Templates(), "*.html"))
//
// dict and list are the map/slice builders that let a caller compose a
// partial's one data value inline, with no Go view-model type per
// combination. icon resolves an icon by its rastrillo slug, defaulting to
// the framework's own vendored set, so {{icon "search"}} resolves
// identically inside a vendored partial and inside an app's own
// templates. iconAssets is whatever markup the app's icon delivery needs
// in <head> — empty for the vendored-inline default, so a layout can call
// it unconditionally and switching delivery needs no template edit. T
// resolves the framework's default strings — English, the framework base
// catalog, unless rebound. Partials call it only for their own defaults
// (a caller-supplied Label/CancelLabel/etc. always wins over T); it never
// reaches into an app's own catalog on this path.
//
// An app is free to add its own entries on top; it must not drop these
// five, or the shipped partials stop parsing.
func Funcs(opts ...Option) template.FuncMap {
	c := config{
		icon:   rastrillo.Icon,
		assets: func() template.HTML { return "" },
		t:      defaultT,
	}
	for _, opt := range opts {
		opt(&c)
	}
	return template.FuncMap{
		"dict": dict, "list": list,
		"icon": c.icon, "iconAssets": c.assets, "T": c.t,
	}
}

// FuncsWith is Funcs(WithT(t)): the T entry replaced, everything else the
// framework default.
//
// TRAP, because it is silent: this rebinds icon and iconAssets back to
// the framework's own set. An app that scaffolded its own icons and uses
// the per-request rebind below must pass both seams every time —
// ui.Funcs(ui.WithT(...), ui.WithIcons(icons.Icon, icons.Assets)) —
// or its icons quietly revert to the built-in Lucide on every request
// while still rendering something plausible.
//
// html/template forbids Clone once a tree has executed even once
// (html/template: cannot Clone <name> after it has executed), so the
// tree registered with a rebind must stay pristine — parsed once at
// startup, never itself passed to Execute/ExecuteTemplate — and every
// request works off a Clone of it instead:
//
//	base := template.Must(template.New("").Funcs(ui.Funcs()).
//	        ParseFS(ui.Templates(), "*.html"))
//	// base is never executed directly from here on.
//
//	// Per request:
//	perReq, err := base.Clone()
//	if err != nil {
//	        return err
//	}
//	perReq.Funcs(ui.Funcs(ui.WithT(func(key string, _ ...any) string {
//	        return rastrillo.T(r, key)
//	})))
//	return perReq.ExecuteTemplate(w, "some-page", data)
func FuncsWith(t func(key string, args ...any) string) template.FuncMap {
	return Funcs(WithT(t))
}

func defaultT(key string, _ ...any) string {
	if v, ok := rastrillo.BaseCatalog()[key]; ok {
		return v
	}
	return key
}

// dict builds a map from alternating key/value arguments:
//
//	{{template "status-pill" dict "Tone" "positive" "Label" "Published"}}
//
// An odd argument count or a non-string key is an error, which
// html/template surfaces from Execute. Failing loudly beats silently
// dropping the trailing key — a partial rendering one field short is a
// bug that looks like a design decision.
func dict(pairs ...any) (map[string]any, error) {
	if len(pairs)%2 != 0 {
		return nil, fmt.Errorf("ui: dict wants an even number of arguments (key, value, …), got %d", len(pairs))
	}
	m := make(map[string]any, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		key, ok := pairs[i].(string)
		if !ok {
			return nil, fmt.Errorf("ui: dict key %d is %T, want string", i/2, pairs[i])
		}
		m[key] = pairs[i+1]
	}
	return m, nil
}

// list builds a slice from its arguments — pagination's Items, mostly:
//
//	{{template "pagination" dict "Items" (list
//	    (dict "Label" "1" "Current" true)
//	    (dict "Label" "2" "Href" "/posts?page=2"))}}
//
// It returns an empty, non-nil slice for no arguments so a template can
// range over the result unconditionally.
func list(items ...any) []any {
	out := make([]any, 0, len(items))
	return append(out, items...)
}
