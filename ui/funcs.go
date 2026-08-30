package ui

import (
	"encoding/json"
	"fmt"
	"html/template"
	"reflect"
	"strings"

	"github.com/carlosframework/rastrillo"
)

// Option configures Funcs. No options is the framework's own behaviour,
// so Funcs() and Funcs(nothing) are the same thing.
type Option func(*config)

type config struct {
	icon   func(string) template.HTML
	assets func() template.HTML
	t      func(key string, args ...any) string
	tf     func(key string, args ...any) string
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
//
// It replaces Tf as well, and with the same function: a translator
// signature already takes args (rastrillo.Tf's own shape), so an app
// that rebinds lookup would otherwise get its own wording for every
// plain default and the framework's English for the one sentence that
// interpolates a value. One option, both entries, no way to bind half.
func WithT(t func(key string, args ...any) string) Option {
	return func(c *config) {
		if t != nil {
			c.t = t
			c.tf = t
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
// reaches into an app's own catalog on this path. Tf is T with {name}
// placeholders filled from alternating name/value arguments — the
// wording that has a value inside the sentence rather than beside it
// ({{Tf "rastrillo.ui.error_ref" "ref" .Ref}}), which is a translation
// unit in a way string concatenation never is.
//
// dateWords is the date fields' vocabulary, JSON in one attribute — see
// its own doc comment below. menuGroup resolves the optional MenuGroup
// key every menu partial takes, defaulting to MenuGroupDefault; it reads
// the key reflectively so it stays optional whether the caller passed a
// dict or a Go struct.
//
// An app is free to add its own entries on top; it must not drop these
// eight, or the shipped partials stop parsing.
func Funcs(opts ...Option) template.FuncMap {
	c := config{
		icon:   rastrillo.Icon,
		assets: func() template.HTML { return "" },
		t:      defaultT,
		tf:     defaultTf,
	}
	for _, opt := range opts {
		opt(&c)
	}
	return template.FuncMap{
		"dict": dict, "list": list, "menuGroup": menuGroup,
		"icon": c.icon, "iconAssets": c.assets, "T": c.t, "Tf": c.tf,
		"dateWords": dateWords(c.t),
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

// defaultTf is defaultT plus {name} substitution: alternating
// name/value arguments, each {name} replaced wherever it appears, and a
// placeholder with no matching argument left verbatim so a translator's
// typo shows in the page instead of silently eating a sentence. The
// substitution happens before html/template escapes the result, so a
// value carrying markup is escaped like any other interpolated string.
//
// It covers the partials' own use and is deliberately not the same code
// as rastrillo.Locales.Tf. Two differences, neither reachable from a
// shipped partial: that one also accepts a single map argument, which
// this one ignores; and it walks the string once, while this one is a
// sequence of ReplaceAll calls, so a value that itself contains
// {another-name} would be substituted into by a later pass.
func defaultTf(key string, args ...any) string {
	s := defaultT(key)
	for i := 0; i+1 < len(args); i += 2 {
		name, ok := args[i].(string)
		if !ok {
			continue
		}
		s = strings.ReplaceAll(s, "{"+name+"}", fmt.Sprint(args[i+1]))
	}
	return s
}

// dateWordNames are the seventeen words datetime.js parses with, short
// name first because the short name is what the browser reads: the
// catalog key is rastrillo.ui.date_ + the name. They are the vocabulary
// half of the date_* keys — the ones whose values are |-separated lists
// of accepted spellings — and deliberately not the display half
// (date_set, date_hint, the quick picks), which the partials emit as
// their own attributes because each is a sentence in its own right.
var dateWordNames = []string{
	"today", "tomorrow", "yesterday", "next", "last", "in", "ago", "at",
	"day", "week", "month", "hour", "minute", "noon", "midnight", "am", "pm",
}

// dateWords returns the {{dateWords}} helper bound to one translator: it
// resolves all seventeen vocabulary keys through t and encodes them as a
// single JSON object, so a date field carries its whole parser
// vocabulary in one data-rst-date-words attribute instead of seventeen.
//
// It is bound rather than free-standing for the same reason T is: an app
// that rebinds T per request (see FuncsWith) gets the request's language
// in the words attribute too, and a field enhanced in Japanese parses
// Japanese. A free function reading the base catalog would have pinned
// every page's parser to English while every visible string localised —
// the most confusing possible half-failure.
//
// The result is a plain string, not template.HTMLAttr or template.JS:
// html/template escapes it as an ordinary attribute value, turning the
// JSON's quotes into &#34;, and getAttribute reverses that before
// JSON.parse ever sees it. Marking it "safe" would only disable the
// escaping that makes the attribute well-formed.
//
// json.Marshal sorts map keys, so the same catalog renders the same
// bytes every time.
func dateWords(t func(key string, args ...any) string) func() string {
	return func() string {
		words := make(map[string]string, len(dateWordNames))
		for _, name := range dateWordNames {
			words[name] = t("rastrillo.ui.date_" + name)
		}
		// A map[string]string of catalog values cannot fail to encode;
		// the empty object keeps a broken build rendering a page rather
		// than a template error, and the field still works unenhanced.
		encoded, err := json.Marshal(words)
		if err != nil {
			return "{}"
		}
		return string(encoded)
	}
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

// MenuGroupDefault is the <details name> exclusivity group every menu
// the library emits joins unless its caller names another: the dropdown,
// locale-menu and bulk-bar partials, the row menus in the list grid, and
// the topbar shell's account menu. Sharing one group is what makes
// opening a row's kebab close the header menu — native, no script.
//
// A nested rst-menu-group must NOT use this value. <details name>
// exclusivity is document-wide rather than sibling-scoped, so a submenu
// sharing its parent's group closes that parent the instant it opens.
const MenuGroupDefault = "rst-menus"

// menuGroup resolves a partial's optional MenuGroup key to the group
// name its <details> should carry, defaulting to MenuGroupDefault.
//
// It exists rather than a plain {{if .MenuGroup}} in the template
// because ui's partials accept two data shapes — a dict-built
// map[string]any, and a Go struct, which the package doc offers exactly
// so a caller gets missing-field detection. A template action reading
// .MenuGroup off a struct that has no such field is an Execute error, so
// adding the key inline would have turned every existing struct caller's
// list screen into a 500 (examples/blog's blog.Filter is precisely such
// a caller, and did). Reading it reflectively keeps an optional key
// optional for both shapes, which is what "optional" has always meant
// for every other key in this library.
//
// Anything that is not a non-empty string — key absent, field absent,
// nil, empty, a number — is the default. A menu in no group at all is
// not a state worth being able to reach by accident: it would sit open
// beside the one the user just opened, which is the bug the group exists
// to prevent. A caller who genuinely wants that writes the <details>
// themselves; the class idioms are hand-written markup anyway.
func menuGroup(data any) string {
	v := reflect.ValueOf(data)
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return MenuGroupDefault
		}
		v = v.Elem()
	}
	var got reflect.Value
	switch v.Kind() {
	case reflect.Map:
		if v.Type().Key().Kind() == reflect.String {
			got = v.MapIndex(reflect.ValueOf("MenuGroup").Convert(v.Type().Key()))
		}
	case reflect.Struct:
		got = v.FieldByName("MenuGroup")
	}
	for got.IsValid() && (got.Kind() == reflect.Interface || got.Kind() == reflect.Pointer) {
		if got.IsNil() {
			return MenuGroupDefault
		}
		got = got.Elem()
	}
	if !got.IsValid() || got.Kind() != reflect.String || got.String() == "" {
		return MenuGroupDefault
	}
	return got.String()
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
