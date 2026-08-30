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

Registers `dict`, `list`, `menuGroup`, `searchClear`, `icon`,
`iconAssets`, `T`, `Tf` and `dateWords`.

`dict` builds a partial's single data value at the call site:

```html
{{template "badge" dict "Label" "Draft" "Tone" "muted"}}
```

`searchClear` is where `list-bar-search`'s clear ✕ points: the app's own
`ClearHref` if it passed one, otherwise the same screen with `q` dropped
and every other `Hidden` pair kept. See
[Templates](/docs/templates#clearing-a-search).

## MenuGroupDefault

```go
const MenuGroupDefault = "rst-menus"
```

The `<details name>` exclusivity group every menu the library emits
joins unless its caller names another, so opening one menu closes
whichever was open — native, no script. `dropdown`, `locale-menu` and
`bulk-bar` take a `MenuGroup` key to override it; the `menuGroup`
template func resolves that key and falls back here.

A nested `rst-menu-group` must **not** use this value. `<details name>`
exclusivity is document-wide rather than sibling-scoped, so a submenu
sharing its parent's group closes that parent the moment it opens.

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

The three shipped themes, `day` first: `day`, `plain`, `signal`.
`ThemeCSS` returns one theme's bytes and reports `false` for a name that
is not shipped — `rastrillo new --theme` calls it before it writes
anything.

A theme is colour, type family and shape; the structure is `tokens.css`.
It is one `:root` block under `color-scheme: light dark`, with every
colour declared once as `light-dark(<light>, <dark>)` and two toggle
rules at the foot setting nothing but `color-scheme`, so an explicit
`[data-theme]` choice wins in both directions without restating a
colour. Each file carries its own measured WCAG 2.2 AA contrast table in
its header comment, for both schemes; `ui`'s `contrast_test.go` splits
the `light-dark()` calls back apart and recomputes every pair, so a
theme that drifts fails the build rather than shipping.

The chosen theme lands as `static/theme.css` and is app-owned from that
moment. Swapping in a hand-written one means replacing that file; the
whole surface a theme has to satisfy is the token set `day` declares —
which now includes the radii and the four depth tokens — and
`TestThemesDeclareIdenticalTokenSets` holds every theme to it.

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
it in chrome made of blocks with working defaults: `title`, `lang`,
`dir` and `head` in all three, plus `brand`, `nav`, `account` and
`locale` in the two chrome shells, and `foot` in `topbar`. No block
reads a field off the data, so a shell renders the same whether a
handler passes a struct, a `dict`-built map, or nil.

`head` is the one that is not chrome: it is an empty slot at the foot of
`<head>`, for a favicon, a meta tag, an extra stylesheet or a script
that must run before the body. Being last means an app's own CSS wins
the ties it should against `tokens.css` and the theme.

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
progressive-enhancement shim. It drives `data-poll` and
`data-poll-push` ([Background jobs](/docs/jobs)); it gives every submit
button a busy state and every form a double-submit guard by default,
with `data-busy="false"` as the opt-out and `data-busy-label` as the
label swap; and it closes an open `<details>` menu on an outside click
or Escape, which is the one part of the menu idiom the native element
cannot express. `SelectJS` backs the enhanced select: it mirrors a
`<select>` carrying `data-rst-select` as a
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
drift is something you choose rather than discover — name the file in
that test's `vendoredIsMine` in the same commit as a deliberate edit.
See [Assets](/docs/assets).

```go
func VendoredNames() []string
func VendoredAssets(theme string) (map[string][]byte, bool)
```

`VendoredAssets` is the whole vendored set for one theme, keyed by the
name each file takes in an app's `static/` directory: the four above
plus `theme.css`, which is `ThemeCSS(theme)`. It reports `false` for a
theme that is not shipped. `VendoredNames` is the same set as an ordered
list of names, for reporting on the files one at a time.

It exists so the list is written down once. `rastrillo new` writes these
bytes, the `vendored_test.go` it generates compares the app's copies
against them, and [`rastrillo doctor`](/docs/cli) reports the
difference — three readers of one function rather than three lists that
eventually disagree.
