# 🤖 ui

`amadan.net/rastrillo/rastrillo/ui`

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
func Wash(hue, chroma, separation float64, ink, background string) (Swatch, error)
func Allocate(keys []string, avoid []float64) ([]Intent, bool)
func Offered() []Intent
func CheckIntents(intents []Intent, backgrounds []string) error
func CheckWashes(intents []Intent, separation float64, canvases []Canvas) error
func WorstSeparation(intents []Intent, backgrounds []string) (deltaE float64, background, a, b string)
func ContrastRatio(a, b string) (float64, error)
func DeltaEOK(a, b string) (float64, error)
```

Three entry points for apps that have to colour things the framework has
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

`Wash` is `Pair`'s sibling, and the difference is who owns the ink. Use
`Pair` when a rule picks the colour and you write both halves — a
presence cursor, an author dot, a conditional format. Use `Wash` when a
person picked the colour and kept their own text: someone selects a cell
and clicks yellow. They asked for a background, not for their font colour
to change, and if you hand them an on-fill you have to persist it — which
on export is a font colour written into the file that nobody set. Import
a workbook, highlight one cell, export, and it comes back with font
colours throughout.

So `Wash` takes the ink the author already has and returns a fill of that
hue their ink still reads on. Its two floors: the ink clears 4.5:1
against the fill, and the fill is at least `MinSeparation` from the
background — perceptibly different, or the user clicks yellow and nothing
appears to happen. That second one is a perceptual distance and not a
contrast ratio on purpose: a pale yellow wash is about 1.2:1 against
white, a number that would condemn every wash anyone has ever shipped.

`separation` is how heavy you want the wash, and the contract is *as
close to it as the ink and background allow, never below the floor*. A
fill exists to be found — someone scans a thousand rows for the cell they
flagged — so it is a flag and not a texture, and one sitting at the
threshold of perceptibility inverts its own job.

**Pass 0.12 if you have no particular opinion.** That is the middle of the
weight Excel's own conditional-formatting presets carry. Two bands are
worth naming: **0.10–0.14** is the rule-driven register — a conditional
format, a status tint, anything a rule applied rather than a person chose
— and **0.21 and above** is the hand-picked register, where somebody
deliberately reached for a colour and wants it seen. Below 0.05 you are
asking for a tint a scanning eye will miss.

### What can actually be delivered

Near-black ink on a white canvas is not an edge case, it is the default
one: it is what a cell looks like before anybody touches it, what
imported files overwhelmingly carry, and what a user picking a fill has
almost always left alone. So that is the case a weight picker has to be
built against.

> Under any near-black ink on white, **every offered hue reaches 0.39**.
> Under pure black specifically, **0.43**.

Both named bands sit comfortably inside that, so a picker built on them
will not offer weights it cannot honour. Above roughly 0.39 the answer
starts to depend on the hue; past about 0.47 nothing is reachable at all,
because black text still has to be readable on the result and that caps
how dark the fill can go.

### The scale underneath

Provenance for those numbers — real colours measured against a white
canvas on the same scale `Swatch.Separation` reports, so you can choose
by pointing at one you know. The rule marks where these stop being
weights you can ask for and become colours you can only paint:

| weight | colour | |
| --- | --- | --- |
| 0.030 | `MinSeparation` — **our** floor, where a fill stops being visible | |
| 0.036 | Google Sheets' palest fill, which is a grey, `#F3F3F3` | |
| 0.064 | Google Sheets' palest *coloured* fill, `#FFF2CC` | |
| 0.11 | Excel's "light green" preset, `#C6EFCE` | |
| 0.12 | Excel's "light yellow" preset, `#FFEB9C` | |
| 0.14 | Excel's "light red" preset, `#FFC7CE` | |
| 0.21 | flat yellow `#FFFF00`, the most-clicked fill there is | |
| 0.38 | a solid green `#00B050` | |
| — | *ceiling under near-black ink on white* | |
| 0.45 | flat red `#FF0000` | reachable at some hues, not others |
| 0.63 | flat blue `#0000FF` | not reachable under black ink at all |

The first row is ours and is attributed to nobody: it is where this engine
stops calling something a wash. The two below it are what the palest thing
another product actually ships measures — a different and more useful
claim, and the reason a request near the floor gets you something fainter
than any coloured fill in a spreadsheet you have used.

The last two rows are why the line is drawn rather than left implied.
Flat red and flat blue are perfectly good colours and you may well store
them; they are not weights to ask a wash for under black text, because no
fill that heavy can carry black text at 4.5:1.

When the ink cannot carry the weight you asked for, you get the closest
it can and `SeparationMet` comes back false — not an error, because the
constrained answer is correct and is the one to paint. Flat blue is the
worked case: black ink on `#0000FF` is 2.44:1, so asking for blue at its
own 0.63 under black ink returns a much lighter blue with the flag down.
If your picker offers weights past the ceiling, though, that flag becomes
wallpaper — which is the argument for keeping the offered weights inside
the reachable band rather than spanning the published one.

`SeparationMet` is false in one case only: the wash came back **lighter**
than you asked for. A wash that comes back heavier is met — you asked for
at least that much weight and got at least that much — because a warning
that fires when nothing is wrong stops being read. It is also true for a
swatch from `Pair`, which requests no weight, so `if !sw.SeparationMet`
is safe to write against any swatch from either function.
`SeparationRequested` sits beside `Separation` if you want the numbers,
but read the boolean rather than comparing them — the achievable weights
are not evenly spaced, so the comparison needs a tolerance.

`Wash` returns an error when no fill of that hue can carry that ink at
all, and that is the feature. Excel ships a conditional-formatting preset
that fails WCAG AA: "Yellow Fill with Dark Yellow Text" pairs `#9C6500`
on `#FFEB9C` at 4.12:1. The product's own default does it.

What is claimed is precise: **a wash this function produces cannot be
unreadable.** Not "no cell in your app can be unreadable" — if you retain
an imported file's fill and font colour verbatim, which is what makes
import and export lossless, then a document using that preset displays at
4.12:1 and it should. Faithfully showing a document somebody else
authored is a different act from generating a colour, and only the second
is `Wash`'s to guarantee.

`Wash` also never invents a font colour: `On` is the ink you passed,
normalised to lower-case `#rrggbb` so a `Swatch` spells all three of its
colours one way. Compare against `sw.On` rather than against the string
you stored.

It does need to know that ink, though, and that is a real limit: `Wash`
is the wrong function if you do not control the text colour. Passing a
guessed ink buys a guarantee about a colour that is not on the screen,
and applying the guess restyles an author's text — the same harm the
function exists to prevent. A highlight behind text the app did not
choose needs a different guarantee ("any ink that was legible on the
background stays legible on the wash"), and that is a separate function
being designed rather than this one with the argument left out.

`background` is a literal colour here for the same reason it is on
`Pair`, and export is the clearest case. A stored intent resolves per
reader, so a light reader and a dark reader see two washes of one
highlight. XLSX carries one hex per cell, so on export that resolution
collapses and a background must be chosen — and the theme someone
happened to be using when they picked a colour must not leak into the
file. A fill imported from a workbook exports as its retained original
hex untouched; a fill picked in-app exports resolved against a canonical
light background, because Excel's canvas is white and that is where the
file will be opened. The export surface is not the reader's surface.

`CheckWashes` is the wash half of the proof, and it inspects the resolved
colours rather than only asking whether an error came back. A `Canvas` is
a background together with every ink that can appear on it, because the
two are not independent — a light canvas carries near-black theme ink —
and because an author can *pin* their font colour and keep it when a
reader switches theme. "Pinned black on dark paper" is a real canvas and
the hardest one: black ink needs a light fill whatever surrounds the
cell, so the wash stops being pale. It still resolves.

Across the twelve offered hues and the shipped surfaces there are no
gaps: 26 canvases carrying 76 background/ink pairs between them — the two
document canvases have no theme ink of their own and carry two each, the
24 theme canvases carry three — which is 912 combinations.

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
which clears the floor by 49%.

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
