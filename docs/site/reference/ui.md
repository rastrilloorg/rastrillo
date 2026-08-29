# 🤖 ui

`github.com/carlosframework/rastrillo/ui`

Rastrillo's server-shape component library: `html/template` partials, a
design-token stylesheet, and the template helpers they need. They are
vendored the same way icons are, so you pull in a working component with
an import and a `ParseFS` call instead of a hand-copy.

It is a component library, not a screen generator. Nothing here
generates a screen, decides a route, or owns rendering.

[Templates and the UI vocabulary](/docs/templates) is the guide.

## Templates

```go
func Templates() fs.FS
```

The partial set, for `ParseFS`:

```go
tmpl := template.Must(template.New("").Funcs(ui.Funcs()).
	ParseFS(ui.Templates(), "*.html"))
tmpl = template.Must(tmpl.ParseFS(appTemplateFS, "templates/*.html"))
```

The partials span the list-screen, display, form and route families.
Each takes exactly one data value, built inline with `dict`, and each
partial's file carries its data contract in a comment above the
`{{define}}`. `ui_test.go`'s `TestAllPartialsAreDefined` is the
authoritative list.

The partials assume three containers they do not emit, because those
belong to your page markup: `<div class="rst-page">`,
`<div class="rst-list">` and `<form class="rst-form">`.

## Funcs

```go
func Funcs(opts ...Option) template.FuncMap
```

Registers `dict`, `list`, `icon` and `T`.

`dict` builds a partial's single data value at the call site:

```html
{{template "badge" dict "Label" "Draft" "Tone" "muted"}}
```

## Option, WithIcons, WithT

```go
type Option func(*config)
func WithIcons(icon func(string) template.HTML, assets func() template.HTML) Option
func WithT(t func(key string, args ...any) string) Option
```

`WithIcons` points both icon seams at your own scaffolded icons
package:

```go
tmpl := template.Must(template.New("").
	Funcs(ui.Funcs(ui.WithIcons(icons.Icon, icons.Assets))).
	ParseFS(ui.Templates(), "*.html"))
```

`{{iconAssets}}` goes in the layout's `<head>` and renders empty for the
vendored-inline default, so you can call it unconditionally and never
touch the layout when the delivery mode changes.

`WithT` rebinds the `T` function.

## FuncsWith

```go
func FuncsWith(t func(key string, args ...any) string) template.FuncMap
```

`Funcs` with `T` bound to a request-scoped lookup, so a partial's own
hardcoded-English defaults — `pagination`'s "Pagination",
`confirm-form`'s "Cancel" — resolve in the request's locale:

```go
tmpl.Funcs(ui.FuncsWith(func(key string, args ...any) string {
	return rastrillo.Tf(r, key, args...)
}))
```

A value you supply beats a partial's default, and your catalog entry
beats the framework base catalog. See
[Localization](/docs/localization).

## Themes

```go
func ThemeNames() []string
func ThemeCSS(name string) ([]byte, bool)
```

The three shipped themes, `ink` first: `ink`, `teal`, `warm`.
`ThemeCSS` returns one theme's bytes and reports `false` for a name that
is not shipped — `rastrillo new --theme` calls it before it writes
anything.

A theme is colour and type family only; the structure is `tokens.css`.
It declares its tokens three times — light, dark under
`prefers-color-scheme`, and again under `[data-theme]` so an explicit
toggle wins in both directions — and carries its own measured WCAG 2.2
AA contrast table in its header comment. `ui`'s `contrast_test.go`
recomputes every pair, so a theme that drifts fails the build rather
than shipping.

The chosen theme lands as `static/theme.css` and is app-owned from that
moment. Swapping in a hand-written one means replacing that file; the
whole surface a theme has to satisfy is the token set `ink` declares,
which `TestThemesDeclareIdenticalTokenSets` holds every theme to.

## Shells

```go
func LayoutNames() []string
func Layout(name string) ([]byte, bool)
```

The three shipped page frames, `column` first: `column` is the plain
centred page, `topbar` adds a header bar with nav and an account menu,
`sidebar` a left rail that collapses to a `<details>` chrome bar below
800px. `Layout` returns one shell's complete `layout.html` text and
reports `false` for a name that is not shipped.

A shell executes `{{template "content" .}}` for the page body and wraps
it in chrome made of blocks with working defaults: `title`, `lang` and
`dir` in all three, plus `brand`, `nav`, `account` and `locale` in the
two chrome shells, and `foot` in `topbar`. No block reads a field off
the data, so a shell renders the same whether a handler passes a struct,
a `dict`-built map, or nil.

`rastrillo new --shell` writes the chosen one as
`templates/layout.html`. It is an ordinary template from then on — no
pin, no vendoring test — so overriding a block, or rewriting the file
outright, is expected on day one. [Templates](/docs/templates) has the
block contract with a worked override.

## Styleguide

```go
func Styleguide() map[string]string
```

The canonical markup for the class idioms — structural components with
an arbitrary caller body, such as the section box, the list-grid card,
the modal route and the page shells, that a `html/template` partial
can't wrap because it doesn't know that body's shape in advance.
`tokens.css` ships the class vocabulary; `Styleguide` is the exercised
markup that goes with it, keyed by idiom name (`box`, `list-grid`,
`dropdown`, `form-layout`, `tblock`, `modal`, `help`, `selbox`,
`shell-topbar`, `shell-sidebar`). The design-system page renders every
sample it returns, and `ui_test.go`'s `TestIdiomClassesAreStyled` holds
them honest against `tokens.css` in both directions: a sample can't use
a class the stylesheet doesn't style, and an idiom class can't ship
undemonstrated. The returned map is a copy, safe to mutate.

## The vendored assets

```go
func TokensCSS() []byte
func ShimJS() []byte
func SelectJS() []byte
func DatetimeJS() []byte
```

`TokensCSS` is the design-token stylesheet `rastrillo new` writes once
into the app's `static/`. `ShimJS` is `rastrillo.js` — the
progressive-enhancement shim that drives `data-poll`, `data-poll-push`
and `data-busy` ([Background jobs](/docs/jobs)). `SelectJS` backs the
enhanced select: it mirrors a `<select>` carrying `data-rst-select` as a
filterable ARIA combobox, renders any `<optgroup>`s as labelled
`role="group"`s rather than flattening them, and never touches one
marked `data-rst-select="false"`. `DatetimeJS` backs the date fields: it
turns an input carrying `data-rst-date` or `data-rst-time` into a
combobox that reads "tomorrow", "next fri 9am" or "in 2 weeks" and
writes the result back to the native input, which stays in the form as
the value carrier. It holds no month names, no weekday names and no
English vocabulary — the calendar names come from `Intl` in the page's
language, and the words it matches on arrive on `data-rst-date-words`
from the request's catalog.
Its on-screen labels have English fallbacks, the same way `select.js`
does, for a field that reaches it without the attributes.

All four are delivered once and yours from then on. Edit them freely;
nothing in the framework overwrites them. The scaffold's
`vendored_test.go` pins the delivered copies byte-identical to these, so
drift is something you choose rather than discover — update or delete
that test in the same commit as a deliberate edit. See
[Assets](/docs/assets).
