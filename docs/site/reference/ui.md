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
belong to your page markup: `<div rst-page>`,
`<div rst-list>` and `<form rst-form>`.

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

## Colour

```go
func Pair(hue, chroma float64, background string) (Swatch, error)
func Allocate(keys []string, avoid []float64) ([]Intent, bool)
func Offered() []Intent
func CheckIntents(intents []Intent, backgrounds []string) error
func WorstSeparation(intents []Intent, backgrounds []string) (deltaE float64, background, a, b string)
func ContrastRatio(a, b string) (float64, error)
func DeltaEOK(a, b string) (float64, error)
```

Two entry points for apps that have to colour things the framework has
never seen — a cell fill, a text highlight, a presence cursor, an author
dot.

`Pair` resolves one colour intent against one background into an
`Intent` made concrete: a `Swatch` carrying `Fill`, the colour you
paint, and `On`, the colour you draw on it. The fill clears
`ContrastFloorBoundary` (3:1) against the background and the on-fill
clears `ContrastFloorText` (4.5:1) against the fill, by construction —
lightness is chosen to make that true, so the same intent comes back
dark on paper white and light on dark paper.

`background` is a literal `#rgb` or `#rrggbb` colour and never a theme
or a scheme, because the colour a fill sits on is often not the theme's
surface: a document canvas can be paper white in a dark theme, and a
conditional format paints under a user fill. Pass a scheme instead and
your own contrast test asserts the pair against the same wrong
assumption it was built from, and passes.

`Allocate` gives each of a set of opaque keys a hue from `Offered()`,
chosen so no two share one for as long as the set has room. It returns
the intents aligned with `keys` plus a flag: false once the keys, plus
anything in `avoid`, outrun the twelve offered hues. Separation is the
guarantee; cross-document stability is best-effort, because two keys in
one document can hash to the same hue and one of them has to move. The
allocation is a pure function of the key *set* — sorted, then open
addressing from an FNV-1a probe — so two clients rendering one document
agree about who is which colour.

A key is a stable string you own the meaning of. The framework has no
idea what an identity is.

`CheckIntents` is the build-time proof, exported so you can run it
against backgrounds the framework does not know: it resolves every
intent against every background and reports each one that fails a floor,
comes back a grey, or lands within `MinSeparation` of another one. `ui`'s
own gate runs it over `Offered()` against paper white and every surface
every shipped theme declares, in both schemes.

That last check is the one worth knowing about. Hues thirty degrees apart
are far apart as angles and can still resolve to nearly the same colour
once a dark background has squeezed the lightness out of them — the
shipped set's closest pair is two teals ΔE_OK 0.045 apart on a dark page,
which is a 50% margin over the floor.

`WorstSeparation` returns that measurement, so it is also what answers
"could we have more than twelve". Measured against the shipped surfaces,
evenly spaced hues clear the floor up to **seventeen** and fail at
eighteen. **Sixteen is what we would recommend**, though, and the gap
between those two numbers is the reason: seventeen clears by 1.9% and
sixteen by 7.3%, and 1.9% is inside the range a new theme surface or a
tweak to the palette could move. Seventeen is the measurement; sixteen is
the one with room in it. Run `WorstSeparation` against your own canvas
before relying on either — a dark paper we have never seen is exactly the
kind of background that squeezes the teals together.

`ContrastRatio` is the WCAG arithmetic all of it is built on, and
`DeltaEOK` is plain euclidean distance in OKLab. Both are exported so an
app gating its own colours — or mapping an imported fill onto the nearest
offered intent — measures with the same ones the framework does.

Colour still never carries meaning alone. Clearing a floor makes a label
legible; the name or initials beside it are what say whose it is.

## Shells

```go
func LayoutNames() []string
func Layout(name string) ([]byte, bool)
```

The four shipped page frames, `column` first: `column` is the plain
centred page, `topbar` adds a header bar with nav and an account menu,
`sidebar` a left rail that collapses to a `<details>` chrome bar below
800px, and `console` is both at once — a brand-and-account bar across
the top with the navigation rail beneath it down the side, which is the
shape most admin consoles are. `Layout` returns one shell's complete
`layout.html` text and reports `false` for a name that is not shipped.

A shell executes `{{template "content" .}}` for the page body and wraps
it in chrome made of blocks with working defaults: `title`, `lang`,
`dir` and `head` in all four, plus `brand`, `nav`, `account` and
`locale` in the three chrome shells, and `foot` in `topbar` and
`console`. No block reads a field off the data, so a shell renders the
same whether a handler passes a struct, a `dict`-built map, or nil.

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

The canonical markup for the markup idioms — structural components with
an arbitrary caller body, such as the section box, the list-grid card,
the modal route and the page shells, that a `html/template` partial
can't wrap because it doesn't know that body's shape in advance.
`tokens.css` ships the vocabulary; `Styleguide` is the exercised markup
that goes with it, keyed by idiom name (`box`, `list-grid`, `dropdown`,
`form-layout`, `tblock`, `modal`, `help`, `selbox`, `shell-topbar`,
`shell-sidebar`). The design-system page renders every sample it
returns, and `ui_test.go`'s `TestIdiomClassesAreStyled` holds them
honest against `tokens.css` in both directions: a sample can't write an
attribute the stylesheet doesn't style, and an idiom can't ship
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
