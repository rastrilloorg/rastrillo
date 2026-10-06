# Gallery usability: a Code tab worth copying, and a site you can find things in

Status: direction approved in conversation 2026-09-27. Revised the same
day after adversarial review rounds 1–5 (38 findings; see the
review log at the end). Revised again 2026-10-04 after mobile-ergonomics
landed on main (0506401): the phone navigates by the shell's index and
back control instead of a drawer, Mobile previews render at their real
size, previews pan correctly right to left, and every measurement and
line reference is re-taken on the merged tree. This document is for
review before an implementation plan is written.

Part B of the design-system iteration. Part A (a busy button's spinner
replaces its label; the `--rst-muted` fix) landed separately as
`busy-spinner-replaces-label`. Parts C (themeable structure and three
genuinely different themes), D (asking about look and feel, the design
layer, shapes and a random roll) and E (components extracted from Tito
Go) get their own specs.

## Why

A hands-on review of the generated gallery (`cmd/dsgen`, every theme,
both schemes, desktop 1440px and phone 390px, keyboard only) found that
the site fails the two readers it exists for:

- **A developer** who wants the right component and the code to use it.
- **An agent** that SKILL.md sends to rastrillo.org/design-system.

The worst of it is the Code tab. It shows the partial's *rendered HTML*
and never the `{{template …}}` call that produces it — so the gallery
teaches exactly the hand-rolling SKILL.md §8 forbids ("a labelled
control is never hand-rolled"). The HTML also loses the partial's
logic: `meter` is called with `Percent 140` and clamps it, so its Code
tab shows `value="100"`, and anyone copying it has copied the wrong
number. Past that:

- Source is one line wherever the template was one line: 32 of 36
  blocks on Display, lines to 1,392 characters, and on a phone
  `choice-field` is a single 3,817px line with ~40 characters visible.
- Every Form and Date-and-time block opens with the demo's own wrapper,
  `<section rst-box><form rst-form method="post" action="#">`, glued on
  with no newline (`page.go` `wrap`). `action="#"` is a placeholder a
  reader has to know to delete.
- No highlighting, no copy button.
- The filter matches names only: "button", "checkbox", "toggle",
  "dialog", "table", "card", "avatar" all say No matches. Buttons have
  no entry at all; `sm` and `block` appear nowhere.
- Every page's `<h1>` is "rastrillo design system" with the same
  subtitle; the page's real title is an `<h2>` further down.
- A 13-link section-tab row repeats the rail (and omits Demos). On a
  phone it wraps to five rows and the first component starts ~900px
  down; by keyboard it is ~20 Tab presses past the skip link.
- One status pill costs a ~190px card with its own tab bar and a 900px
  virtual frame; "Default tone (neutral)" and "Neutral" render
  identically. Display is ~10,900px tall on a phone. Form has 32
  separate tab bars, so reading its code is 32 clicks.
- Switching theme or language drops the reader's place; the header
  scrolls away, so switching from deep in a page means scrolling back
  up first.
- The intro prose reads like a changelog ("The word on this page
  changed; the code's did not.") and the Shells callout promises source
  that page does not have.
- Weight, measured on a full build of the merged tree (`go run
  ./cmd/dsgen`, 2026-10-04: 914 files, 31,520,462 bytes): Form's
  heaviest variant (`signal/hi`) is 131,297 bytes, 225 over the 128 KiB
  (131,072-byte) cap and held there by a `pageBudgetDebt` entry
  (`designsystem_test.go:97-106`); Date and time (`signal/hi`) is at
  128,631, 2,441 under. On those two pages 47,347 and 49,850 bytes are
  the escaped second copy of every sample inside `srcdoc`. The gallery
  page itself loads `select.js`, `calendar.js` and `datetime.js`
  (150,823 bytes) on every page though none of its own controls use
  them.

What works and must keep working: dark scheme reaches every frame and
persists; Arabic lays out correctly; focus rings are visible, including
inside frames; the skip link works; nothing scrolls sideways at 390px;
each tab bar is one keyboard stop; everything works with scripts off.

## Section 1 — the Code tab

### 1.1 The call comes first

For every sample built from a partial and its `Data` (or `Build`), the
Code tab leads with the call, generated from the same value the preview
rendered:

```
{{template "meter" dict "Percent" 140 "Text" "700/500"}}
```

**The generator.** `callFor(partial string, s sample, locale string)
(call, error)` in `internal/designsystem`, where `call` is
`{Source string; Dot map[string]any}`. `Source` is what the Code tab
shows; `Dot` is what `.` must be when `Source` executes (empty unless
the sample binds a placeholder, below). An error fails the build and
names the partial, the state and the value's Go type.

**Values it writes**, by the reflected kind of each value:

| Value | Written as |
|---|---|
| top-level `string`, `int`, `bool` | the argument itself: `{{template "notice" "Post saved."}}` (notice and form-error take a plain string) |
| `string` | `strconv.Quote` — a Go interpreted literal, which is also template literal syntax |
| any signed or unsigned integer kind | decimal |
| `float64` | `strconv.FormatFloat(v, 'f', -1, 64)` (no sample uses one today; allowed so the first one does not need a spec) |
| `bool` | `true` / `false` |
| map with `string` keys (any value type) | `dict "K" v …` — the `dict` in `ui.Funcs` (`ui/funcs.go`), which takes alternating string keys and values |
| slice or array of anything (`[]any`, `[][2]string`, `[]rastrillo.LocaleItem` when bound, …) | `list v …` — `ui.Funcs`'s `list`. `[][2]string` becomes a list of two-item lists; the partials read Hidden through `index` and `optPairs` (`ui/funcs.go:385`), both of which walk any slice or array, so it renders the same |
| a key the sample binds | the placeholder, e.g. `.Locales` |
| anything else — a struct, a pointer, `nil`, a func, a named string type such as `template.HTML` | **build error**: "bind it to a placeholder or give it as a map" |

Structs are an error rather than being flattened into a `dict` on
purpose: a struct caller has different missing-field semantics
(`ui/ui.go`, "a struct caller must carry every field the partial
names"), so a `dict` rendering of a struct would show a call the reader
did not write. A struct value means "the app passes its own value",
which is what a placeholder says.

**Placeholders.** `sample` gains `Bind map[string]string`: data key →
dot field. locale-menu binds `"Items": "Locales"`, so its call reads

```
{{template "locale-menu" dict "Items" .Locales "Return" "/"}}
```

and `Dot` is `{"Locales": items}`, where `items` is the `Items` value
of the map `Build` returns (`localeMenuData` returns
`{"Items": []rastrillo.LocaleItem, "Return": "/"}`, `samples.go:902`;
the binding replaces one key of it, not the whole value). The sample's
Note is rewritten to say where the value comes from
(`rastrillo.LocaleItems(r)`, which the partial's own `Keys:` block
already names) as well as what it says now; that is one key replaced
in the 2.8 inventory. locale-menu is the only binding today.

**Key order.** Keys are written in the order the partial's own `Keys:`
comment block lists them (every partial but job-status and list-bar has
one); keys the block does not list, and keys of nested maps, follow in
alphabetical order. Both rules are deterministic, which is what
`TestRenderIsDeterministic` needs.

**Layout.** A call that fits in 80 columns is one line. A longer one
breaks after `dict` with one key/value pair per line, four-space
indent, and each nested `(dict …)` inside a `(list …)` on its own line
one level deeper. Go templates allow newlines inside an action, and the
contract test below parses exactly what is shown, so the layout cannot
produce a call that does not parse.

**Defaults.** A sample that relies on a default (an unset Label that
resolves through `T`) simply has no such key, so its call has none —
the call is the same in every locale while the rendering differs. That
is why the contract runs in all twelve locales.

**Raw samples** (markup no partial emits: the optgroup'd select, the
busy form-foot) have no call. Their Code tab shows the executed markup,
formatted per 1.2, as today.

**The contract.** For every partial sample and every locale, parse
`Source` into a clone of that locale's partial tree, execute it against
`Dot`, and require the output to be byte-identical to `renderSample`'s
output for the same sample — the partial's own bytes, before `wrap`,
before `deaden`, before the preview document is built. That is the
comparison that can hold, because the preview document is not the
partial's output: it is wrapped (`page.go:1542`), its links rewritten
and its forms retargeted (`page.go:1482`), and a sink frame appended
(`page.go:1444`). Those transforms are tested separately (Tests,
below). A call that does not parse, parses to different markup, or
cannot be generated fails the build or the test; where a sample cannot
produce a truthful call, the sample changes, not the test.

### 1.2 The rendered HTML, formatted, under a disclosure

Below the call, `<details class="ds-html"><summary>Rendered
HTML</summary>` holds the markup `renderSample` produced (no wrapper,
not deadened — the routes a reader copies are the real ones, as
today), laid out at build time by a small formatter:

- A line break and two-space indent are inserted only between two tags
  where at least one is block-level; whitespace-only text between such
  tags is replaced by that break. Nothing else changes: inline
  elements, text, `<svg>` and everything inside `<pre>` and
  `<textarea>` stay byte-for-byte, and attribute values are never
  touched. That is a safe rewrite because whitespace between two
  block-level boxes does not render.
- **No column limit is promised.** An inline run such as meter's whole
  output (`ui/partials/meter.html:27`) or a choice card
  (`ui/partials/choice-field.html:10`) stays one line however long it
  is, because breaking inside it would change rendered whitespace. The
  code block soft-wraps instead: `.ds-src code { white-space: pre-wrap;
  overflow-wrap: anywhere; }`. Copying takes `textContent`, which has no
  soft breaks in it, so the copied text is exactly the formatted
  source.
- Only the Code tab's copy is formatted. The preview document keeps the
  unformatted bytes, so `TestPreviewFrameHeightsFitTheirContent` does
  not move.

The demo wrapper is no longer part of any source block. A wrapped
sample shows one muted line above its code, built from the sample's
`wrapper`: "Put this inside `<form rst-form>` in an `rst-box`." (one
prose key, "Put this inside {wrapper}.", with the markup substituted as
code; final wording through copy review). The rule survives and
`action="#"` never reaches a clipboard.

### 1.3 Highlighting, at build time

A Go tokenizer marks five kinds of token: tag, attribute name,
attribute value, template action (`{{`, `}}`, `template`, `dict`,
`list`, placeholders) and string literal. No client-side highlighter,
no dependency, no script.

- **Markup: five two-letter custom elements, `<ds-t>`, `<ds-a>`,
  `<ds-v>`, `<ds-x>`, `<ds-s>`.** Chosen for bytes, and the budget
  table below is the reason: a token costs 13 bytes this way against 29
  for `<span class="ds-tk-t">…</span>`, and at 29 Form's heaviest
  variant projects to ~139 KB — over the cap even with the previews
  moved out. An undefined custom element is an inline element with no
  role, so it changes nothing for assistive technology or for
  `textContent`.
- The highlighter returns `template.HTML` and escapes only `&`, `<` and
  `>` in text. Quotes stay literal, which is valid in text content and
  saves four bytes on every `&#34;` html/template writes today (2,672
  bytes on Date and time alone).
- Colours are declared in `gallery.css` from theme tokens
  (`--rst-accent`, the `--rst-tone-*-fg` foregrounds,
  `--rst-text-muted`), never literals, so they follow theme and scheme.
  The block's background is `--rst-surface`.
- Contrast is gated twice: in the rendered cascade by axe (Tests, "Code
  selected"), and pair-wise by a Go test that resolves each highlight
  element's token to its light and dark value in every theme (the
  Tokens page's `themePalette`/`lightHalf`, plus a dark counterpart)
  and requires 4.5:1 against `--rst-surface` via `ui.ContrastRatio`.

### 1.4 A copy button

- **Which blocks.** Every call block, every Rendered HTML block, every
  idiom/screen/format source block and the Getting started snippets.
  Not a block marked as an illustration: `sample` gains `Illustration
  bool`, set today only on form-foot's "Working" state (what
  rastrillo.js writes, not markup to copy). Its block renders with
  `data-ds-nocopy`, gets no button, and keeps its existing State label,
  which already says what it is.
- **No script, no button.** `gallery.js` inserts the buttons, and only
  when `navigator.clipboard && navigator.clipboard.writeText` exists
  (it does not on a plain-HTTP origin such as a tailnet address). With
  no clipboard API the `<pre>` stays selectable and no dead control is
  drawn.
- **Names.** The visible label is "Copy". The script appends a visually
  hidden suffix built from text already on the page, so the server
  writes no per-block attribute (repeating a translated name on every
  block would cost several KB on Form): the section's heading (the
  partial or idiom name) and, where the block has one, its state label
  ("field-text, Required"); a block inside a Rendered HTML disclosure
  prefixes that disclosure's summary ("Rendered HTML, field-text,
  Required"). The joiner is a draft for copy review (C17). Names are therefore unique on the page wherever the
  state labels are (the Copy leg asserts it on Form and Display) and
  satisfy label-in-name.
- **Success** is announced only after `writeText` resolves: the page's
  one polite live region (`<p class="rst-sr-only" role="status"
  data-ds-copy-status>`, rendered empty) receives "Copied", and the
  button's visible label reads "Copied" for two seconds.
- **Failure** (the promise rejects — permission, focus, policy): the
  live region receives "Copy failed. Select the code and copy it
  yourself.", and the script selects the block's text with a `Range`
  so Ctrl/Cmd+C works at once. The storage wrappers' rule applies: no
  exception escapes.
- The three strings reach the script the way the filter's "No matches"
  does — rendered by the page, in its language, as `data-copy`,
  `data-copied` and `data-failed` on the live region. `gallery.js`
  still says nothing of its own.

### 1.5 Paying for it: previews as files, and three scripts dropped

**Decision: move every `srcdoc` preview to a static file.** Measured,
not assumed. The heaviest variant of each page, now and projected
after everything in this spec (model and per-line numbers in Budgets,
below). "Now" and "shed" were re-measured on the merged tree
(2026-10-04); "Projected" is now − shed + the additions modelled on the
2026-09-27 tree, Form's scaled by its frames, 32 then and 35 now
(field-url's three samples landed in between):

| Page (heaviest variant) | Now | `srcdoc` shed | Projected | Headroom |
|---|---|---|---|---|
| Date and time (`signal/hi`) | 128,631 | 49,850 | ~96,400 | ~34,700 |
| Form (`signal/hi`) | 131,297 | 47,347 | ~122,400 | ~8,700 |
| Display (`signal/hi`) | 109,240 | 35,536 | ~91,900 | ~39,200 |
| List screen (`signal/bn`) | 90,690 | 27,286 | ~81,500 | ~49,600 |
| UI primitives (`signal/hi`) | 69,406 | 17,687 | ~67,500 | ~63,600 |

**The alternative, measured and rejected: keep `srcdoc`, trim
elsewhere.** On Date and time (`signal/hi`) everything this spec can
trim without touching `srcdoc` — the tab strip (1,090), the intro
(1,534), the subtitle (241), the wrapper in source (1,498), literal
quotes in code (2,672) — frees 7,035 bytes. The calls, their
highlighting, the disclosures, the wrapper lines, the view control and
the search terms cost 9,839. (Both measured on the 2026-09-27 tree.)
On today's 128,631 that lands at ~131,400: over the cap *with the
Rendered HTML neither highlighted nor formatted*. Highlighting it
(+12,259) and formatting it (+1,536) puts the page 14 KB over. Keeping
`srcdoc` means giving up 1.2/1.3 on the pages that need them most, and
Form, already in debt, has nowhere to go at all.

**Which previews move:** every preview built by `newPreview` —
partial samples (`page.go:1000`), markup idioms (`page.go:1652`),
screens (`screens.go:257`, `:269`) and formats (`formats.go:193`). The shell
demos and the Overview's demo application already load pages of the
tree through `Src` and are unchanged. After this spec no frame in the
tree carries `srcdoc`, and a test says so.

**Where they go:** `<theme>/<locale>/<page>/<group>.html`, where
`<page>` is the page's file stem (`form`, `primitives`, `screens`, …)
and `<group>` is the preview's radio-group name, already unique on its
page and asserted so (`partial-field-text-3`, `idiom-box-0`,
`screen-signin-link-0`). The shells page already keeps its framed
documents in `shells/`; this is the same shape for the pages that have
none yet. `newPreview` takes the page kind and returns the preview
view plus the file it implies; `renderGallery` adds the files to its
output map and **fails the build** if a path is produced twice or
collides with any other file in the tree.

**URLs** are absolute and mount-prefixed like every other in-tree URL
(`mount + "/" + theme + "/" + locale + "/form/partial-field-text-3.html"`):
the tree's live bug was relative paths at the slash-less URL, and
`TestRootIndexIsTheDefaultThemeInEnglishAtTheTreeRoot` forbids climbing.
The frame is `<iframe … src="…" loading="lazy">`.

**The document** is exactly today's `srcdoc` string (`page.go:1411`),
written unescaped, plus `<meta name="robots" content="noindex">`: 4,800
fragment documents are not pages a search engine should list.

**What survives unchanged.** `gallery.js` paints `data-theme` into
`contentDocument` (`gallery.js:87`) and on `load` (`:113`); a
same-origin `src` frame is as readable as a `srcdoc` one, which the
shell demos already prove. `srcdocScripts` becomes the preview
document's script list with no other change. `cmd/dsgen` already
creates nested directories and sorts paths (`cmd/dsgen/main.go:119-123`);
`TestWriteIsTheWholeRender` derives from `Render` and needs nothing.

**Cost, measured on the merged tree (2026-10-04) and projected:**

- Files: 5,220 previews today (145 per theme × locale, × 36). After
  compact rows (2.7: −12 per theme × locale; status-pill, badge and
  meter frame five states each on Display) and the Buttons idiom (+1)
  that is 134 × 36 = **4,824 new files; the tree goes from 914 to
  ~5,738**.
- Bytes: the escaped `srcdoc` is 7,995,363 bytes; the same documents
  unescaped are 5,846,391 (~1.1 KB each). HTML pages fall from
  31.08 MB to ~27.9 MB, the source growth (highlighted, formatted
  source and calls on every component page) scaled from the
  2026-09-27 model by previews, 145/133. **The tree goes from 31.52 MB
  to ~34.4 MB (+9%).**
- Requests: one same-origin request per preview, ~1 KB each, only as a
  lazy frame nears the viewport; stylesheets are the page's own and
  cached. Form has 35 frames, so reading the whole page costs up to 35
  small requests it does not make today. The browser leg below counts
  them on Form rather than assuming.
- Build: the site copies the tree through Eleventy; ~4,800 more small
  files is the cost to watch there (Operator decision 1).

**Scripts.** The gallery page stops loading `select.js`, `calendar.js`
and `datetime.js`; they load inside the preview documents that need
them, as `srcdocScripts` already arranges. `rastrillo.js` stays: the
chrome's language menu uses its light dismiss. `shell.js` and
`shell.css` join it, for the phone's index and back control (2.3).

### Out of this round

A per-partial parameter table (partial parameters are not declared
anywhere machine-readable yet; that is its own piece of work).

## Section 2 — wayfinding and layout

### 2.1 Real titles

Each page's `<h1>` is its topic ("Form"). The page-header block keeps
the `<h1>` and loses the subtitle ("An overview of everything… Theme:
day. Language: English."); theme and language are visible in their
own controls. Each body's own title heading (`<div class="ds-head">
<h2>Form</h2>`) is deleted — its `id`, where it has one
(`id="tokens"`, `id="overview"`, …), moves to the `<h1>` — and every
other heading in the page bodies moves up one level, so the outline
stays unbroken: a partial is an `<h2>`, the Tokens groups are `<h2>`,
Screens' "Signing in" is an `<h2>` and each screen an `<h3>`. Styles
that select by heading level move with it: `.ds-partial > :is(h3, h4)`
(`gallery.css:46`) becomes a class on the heading (`.ds-partial__name`)
so the next outline change cannot silently unstyle it.
"rastrillo design system" becomes the brand at the start of the top
bar, linking to Overview. `<title>` already carries the page name
first and stays. On a phone the Overview is the index (2.3), its main
and that main's `<h1>` are hidden, and the page's one visible `<h1>` is
the index title, "rastrillo design system", the shell's
`[rst-shell-title]`, which is `display: none` everywhere else
(`ui/tokens.css:2163`, `:2198`).

### 2.2 One navigation on each kind of screen

The section-tab row (`ds-switch`, `page.go:2398`) is removed, with its
prose key "Sections". What it added was a visible route between pages
on a phone, where the drawer hid the rail. That route is now the
shell's own.

- **At 800px and wider** nothing changes but the strip: the rail and
  Previous/Next are the navigation, and
  `TestEverySectionOfTheRailRoutesToItsOwnPage`
  (`designsystem_test.go:2243`) still requires each page to be linked
  exactly once from the rail's tree.
- **Below 800px** the gallery navigates the way the sidebar shell has
  since mobile-ergonomics landed (`ui/layouts/sidebar.html:30-54`,
  `ui/tokens.css:2149-2226`, `docs/site/templates.md:1045-1086`): the
  Overview is the index, a page of rows with one per section, and every
  other page is a content page whose back control returns to its own
  row. No drawer, no Menu button. Previous/Next stay at the foot of each
  content page for reading straight through. The phone index leg
  (Tests) replaces what the strip gave a phone.

### 2.3 The frame: the shell's two views, and a pinned bar on desktop

**DOM.** The gallery's frame becomes the shipped sidebar layout's markup
(`ui/layouts/sidebar.html:47-83`), attribute for attribute, so every
shell rule in `tokens.css` and `shell.css` and every selector in
`shell.js` applies to it unmodified and nothing of the shell is
re-implemented in `gallery.css`. Today it is a hand copy of the old
layout, drawer included (`page.go:2362-2378`); mobile-ergonomics left it
for this spec (`docs/superpowers/specs/2026-09-30-mobile-ergonomics-design.md`,
§4.9).

```
<div rst-shell-sidebar="page">                     "index" on the Overview
  <a rst-skip href="#main">…</a>
  <div rst-shell-back><a href="…/<theme>/<locale>/index.html#nav-<kind>"
       rel="up" aria-label="Back to Sections">Sections</a></div>   not on the Overview
  <aside class="ds-rail" rst-shell-rail>
    <h1 rst-shell-title>rastrillo design system</h1>              Overview only
    <p class="ds-index-lead">…</p>                                Overview only
    <search class="ds-search">…the filter…</search>
    <p class="ds-nav__empty" data-ds-filter-empty role="status" hidden>…</p>
    <nav class="ds-index" rst-shell-nav aria-label="Sections">…rows…</nav>   Overview only
    <nav class="ds-nav" rst-shell-nav id="ds-nav" aria-label="Sections and demos">…tree…</nav>
    <div rst-shell-rail-foot id="ds-prefs">theme links · scheme buttons · language menu</div>   Overview only
  </aside>
  <header class="ds-top">
    <a class="ds-top__brand" href="…/index.html">rastrillo design system</a>
    <div class="ds-top__controls">theme links · scheme buttons · language menu</div>
  </header>
  <main rst-shell-main id="main"><div rst-page>…</div></main>
</div>
```

The head links `shell.css` and `shell.js` as the layout does
(`ui/layouts/sidebar.html:12`, `:18`, `blocking="render"`), and the tree
already carries both files (`designsystem.go:144-145`). That brings the
slide between the index and a page, Back through history when the entry
behind is provably the index, and focus returned to the row the reader
left (`ui/shell.js:14-24`). The gallery adds no script for any of it.

**Prerender: none.** In an app the phone's next page is prerendered
because `rastrillo.Serve` sends a `Speculation-Rules` header naming its
ruleset (`serve.go:469`, `speculation.go:15`), and the ruleset's
selector would match the gallery's new markup (`speculation.go:39`).
The gallery is never served by `Serve`: `cmd/dsgen` writes it and
rastrillo.org ships it as static files, and the site's repository has
no headers file and no mention of speculation rules. So no gallery page
is prerendered, and the gallery does not add an inline
`<script type="speculationrules">` to make up for it, for three
reasons. The tree has no inline script anywhere (checked on the merged
build, pages and `srcdoc` documents alike), and the framework
delivers its rules by header precisely so that a CSP need not allow one
(`speculation.go:35-38`). A gallery page is heavy, up to ~122 KB after
this spec, and on Android `moderate` fires about 500ms after scrolling
stops near the last tap (`speculation.go:26-30`), so reading down the
index would fetch pages nobody opens, on a phone's data. And nothing
depends on it: the slide, Back through history and the focus return
are `shell.js` and `shell.css`, which work on a page that was not
prerendered (`docs/site/templates.md:1066-1068`). A test holds that no
ruleset sneaks in.

**The two views.** The Overview's root says `index`; every other page's
says `page`. A content page's way up is
`<mount>/<theme>/<locale>/index.html#nav-<kind>`: absolute and
mount-prefixed like every in-tree URL, the fragment naming that page's
row, the shape `docs/site/templates.md:1059-1064` tells apps to use. The
back control's words are the framework's, `rastrillo.ui.shell_up_label`
("Sections") and `rastrillo.ui.shell_up` ("Back to {name}",
`locales/en.toml:34-35`), translated in all twelve locales already.

Where the gallery departs from the layout, and why:

- **The index pieces are written on the Overview only.** On a content
  page they would never show at any width: the title is `display: none`
  outside the phone index (`ui/tokens.css:2163`), an empty foot is
  `:empty` (`:1957`), and the rows and lead are hidden the same way by
  the rules below. Writing them on the other twelve pages × 36 would be
  bytes nobody sees. For the same reason the Overview has no back control: its way up
  would be itself.
- **The rows are a nav of their own, not the tree.** The tree's sections
  are `<details>` disclosures (`page.go:2373`), and both halves of the
  shell's index need plain links: the rows are styled as
  `[rst-shell-nav] > a` (`ui/tokens.css:2209`), and `shell.js` returns
  focus to the first `[rst-shell-nav] a[href]` whose path is the page
  just left (`ui/shell.js:37`, `:75-84`). In the tree that link is a
  section's Overview item inside a closed disclosure, which cannot take
  focus, so the return would silently do nothing. The rows nav comes
  before the tree in document order so its row is the one `shell.js`
  finds. Rows are read off the same `galleryNav` output as the tree
  (`page.go:463`), so the two cannot disagree: one per page kind but the
  Overview, labelled with the section's title and carrying
  `id="nav-<kind>"`, then a `<p rst-shell-group>` "Demos" and the tree's
  Demos links, still opening in a new tab. `anchorID` never writes a
  `nav-` id (`page.go:610`; its kinds are `partial`, `idiom`, `screen`,
  …), and kinds are unique (`TestNoTwoPageKindsShareAName`,
  `designsystem_test.go:119`), so the ids cannot collide. The nav's
  label is the back control's word, "Sections", so "Back to Sections"
  lands on a list of that name.
- **The phone index keeps the filter.** A query swaps the rows for the
  tree, filtered exactly as on desktop, so typing "checkbox" on the
  index and tapping the result lands on `form.html#partial-field-check`.
  The swap is CSS on `#ds-filter:not(:placeholder-shown)`, so `gallery.js`
  needs nothing new. With scripts off the filter is not shown
  (`gallery.css:387-388`) and the rows are the list.
- **Paul's paragraph leads the phone index.** The index hides main
  (`ui/tokens.css:2187`), and with it the Overview's body: the paragraph
  that is the whole of what the system claims (`page.go:2424-2425`) and
  the framed demo application. The paragraph is written a second time
  under the index title, shown only in the index view; the demo
  application is a Demos row (decision 9).
- **No brand in the rail.** The brand is in the bar (2.1). On the phone
  index the title stands in for it, as it does for the shell's brand
  (`ui/tokens.css:2187`).
- **The controls are written twice on the Overview.** Decision 7 puts
  them under the section list on a phone and keeps them in the pinned
  bar on desktop, and one element cannot be in both places: the rail is a
  grid item, so nothing inside it can be placed in the bar's cell, and a
  bar after the rail would sit below the index's full-screen rail
  (`min-block-size: 100dvh`, `ui/tokens.css:2188`), off the first
  screen. The foot is the layout's slot for exactly these controls
  (`ui/layouts/sidebar.html:63-77`), and the shell already lets the foot
  follow the rows by a fixed gap rather than float to the screen's
  bottom (`ui/tokens.css:2221-2225`). Only one copy is ever rendered:
  the foot below 800px on the index, the bar everywhere else. Neither
  carries an id but the foot's own, and `gallery.js` already drives
  every `[data-ds-scheme]` button on the page (`gallery.js:119-139`), so
  both stay in step.
- **The foot's language menu positions itself as the framework's does,
  in either direction.** The gallery adds no rule to it. With anchor
  positioning the panel is `position: fixed` against its summary and
  flips upward when there is no room below (`position-try-fallbacks:
  flip-block`, `ui/tokens.css:1305-1323`), which at the end of a long
  index is the normal case. Without it the panel is absolutely
  positioned under the summary (`top: 100%`, `ui/tokens.css:1195`); the
  fixed gap reserves nothing below the foot, so the panel extends the
  document and the reader scrolls the page to reach its lower links.
  Either way the panel's height is capped and it scrolls inside itself
  (`ui/tokens.css:1301`, raised for touch at `:2432`). So the promise
  is reachability, not a direction and not containment in the viewport:
  every language can be reached by keyboard and by scrolling, with
  anchor positioning or without, with scripts or without, in both
  directions and at 320px (tested).

**At 800px and wider** (the shell's breakpoint, `ui/tokens.css:1958`)
the header is a direct child of `[rst-shell-sidebar]`, after the rail
and before `<main>`. Every control in it exists once on a content
page; on the Overview the foot's copy is `display: none` at this width.
The header is outside `<main>`, so it is the page's one banner landmark and the
skip link still skips it and the rail. The back control and the index
title are `display: none` here (`ui/tokens.css:2163`) and take no grid
cell; the skip link is absolutely positioned (`:1825`). On the Overview
the rows, lead and foot are hidden too: at this width the Overview is a
page with the rail beside it, as the shell's index is
(`ui/tokens.css:2158-2162`). Tab order is skip link, filter, rail,
brand, controls, main: the controls come after the rail exactly as they
do today.

*Why the placement is by grid line, not by grid area.* A grid item's
containing block is its grid area, and a sticky box cannot leave its
containing block. A header in a one-row area has no room to stick. The
rail sticks today because its area is the whole height of the grid
(`ui/tokens.css:1962`). So the header gets the same: its area spans
every row, and a fixed first row reserves its height. `gallery.css`,
for this page only:

```
body > [rst-shell-sidebar] { grid-template-rows: var(--ds-bar-h) 1fr; }
body > [rst-shell-sidebar] > [rst-shell-rail] { grid-column: 1; grid-row: 1 / -1; }
.ds-top { grid-column: 2; grid-row: 1 / -1; align-self: start;
          position: sticky; inset-block-start: 0; z-index: 10;
          box-sizing: border-box; block-size: var(--ds-bar-h);
          background: var(--rst-bg); }
body > [rst-shell-sidebar] > [rst-shell-main] { grid-column: 2; grid-row: 2; }
```

The header's area runs the full height of the page, so it stays pinned
while main scrolls under it; row one is exactly `--ds-bar-h`, so main
starts below it. Brand at the inline start, controls at the end, one
row. The lines are numbered, not sided, so in Arabic the rail is on the
right and the bar over main on the left with no rule of the gallery's
(the shell's columns, `ui/tokens.css:1959`; tested, 2.10).

**Below 800px** there is no top bar and no drawer:

- *A content page* is the skip link, the shell's back strip (sticky,
  opaque `--rst-surface`, `z-index: 9`, `ui/tokens.css:2170`), then
  main. The rail is `display: none` (`ui/tokens.css:1930`) and so is
  `.ds-top`: decision 7, a section page shows only the way back and the
  content. Tab order is skip link, Back, main.
- *The index* is the rail as the whole page (`ui/tokens.css:2187-2188`):
  title, lead, filter, rows, foot. Main, the skip link and the back
  control are hidden. Tab order is filter, rows, then the foot's theme
  links, scheme buttons and language menu: screen order.

`gallery.css` adds, for these two views, only the gallery's own pieces:

```
/* Pieces only the phone index shows, written on the Overview alone. At
   800px and up the Overview is a page with the rail beside it, as the
   shell's index is, and these would be a second list under the tree
   and a second set of controls beside the bar's. */
.ds-index-lead, .ds-rail > .ds-index, .ds-rail > [rst-shell-rail-foot] { display: none; }
@media (max-width: 799.98px) {
  /* On a phone a section page is the way back and the content, and
     the controls are in the index's foot, under the list. */
  .ds-top { display: none; }
  [rst-shell-sidebar~="index"] .ds-index-lead { display: block; }
  [rst-shell-sidebar~="index"] > .ds-rail > :is(.ds-index, [rst-shell-rail-foot]) { display: flex; }
  /* A query swaps the rows for the tree it filters. Two rules, so an
     engine without :has() drops both and shows rows and tree one after
     the other: long, but with no route missing. */
  [rst-shell-sidebar~="index"]:not(:has(#ds-filter:not(:placeholder-shown))) > .ds-rail > .ds-nav { display: none; }
  [rst-shell-sidebar~="index"]:has(#ds-filter:not(:placeholder-shown)) > .ds-rail > .ds-index { display: none; }
}
```

**The gallery's own controls under a thumb.** The theme links and the
language summary are framework controls, which the touch block already
sizes to `--rst-tap` (`ui/tokens.css:2423`, `:2469`). The scheme buttons
(`gallery.css:40`, under 30px tall) and the page-wide view buttons (2.6)
are the gallery's, so `gallery.css` gives them
`min-block-size: var(--rst-tap)` under the same query,
`(pointer: coarse), (max-width: 40rem)` (`ui/tokens.css:2408`). The
foot of the index is where a phone reader meets them.

**Painting.** Desktop: the bar is opaque `--rst-bg` at `z-index: 10`,
above page content (`auto`, including the positioned preview boxes and
transformed frames) and below the skip link (60, `ui/tokens.css:1826`).
Phone: the back strip is the shell's, opaque at `z-index: 9`
(`ui/tokens.css:2170`); nothing in the gallery's content sets a
z-index, so it paints over every preview.

**Not obscuring focus.** At 800px and wider only, `html {
scroll-padding-block-start: calc(var(--ds-bar-h) + var(--rst-sp-2));
}`, so fragment navigation and focus scrolling stop below the pinned
bar (WCAG technique C43).

- `--ds-bar-h` is the top bar's border-box height, set per breakpoint
  band to the measured maximum over all 36 theme × locale pages; the
  bar is one row at 1024px and up and may take two between 800 and
  1023px, and its band's value says so.
- Because the bar's own box is fixed at that height, the gate measures
  what is inside it, not the box: every theme × locale at each band's
  edges, the union of the brand's and every control's bounding boxes
  must lie inside the bar, with no horizontal overflow.
- Below 800px what is pinned is the framework's back strip
  (`ui/tokens.css:2170`), and keeping fragments and focus clear of it
  is the framework's job: `tokens.css` is gaining that `scroll-padding`
  on its own branch (operator, 2026-10-04). This spec neither sets it
  nor depends on it. The rule above is inside the 800px query for that
  reason: a gallery rule on `html` at every width would override the
  framework's on a phone.

**The reading line** (desktop, where the switchers are). One threshold
serves both directions: the *reading line* is the document's computed
`scroll-padding-block-start` plus 1px, the line a fragment lands just
below. Fragment navigation puts a section's top on it, and the switcher
lookup below reads the section at it, so landing on a section and then
switching theme or language keeps that section.

**Keeping your place.** On desktop the theme and language links are in
the pinned bar and keep the section the reader is in. On the phone
index they keep the reader at the controls. Phone content pages carry
no switchers (decision 7).

*Desktop.* The current position is computed when a switcher link in
`.ds-top` is clicked, not tracked while scrolling, and the URL is never
rewritten behind the reader's back. On `click`, `gallery.js` picks an
anchor id in this order and sets the link's `href` to the same address
with `#<that id>` before the browser follows it:

1. **A fragment the reader is still on.** The record is a pair: an
   anchor id and that target's viewport top. If `location.hash` names
   the record's id and the target's top is within 2px of the recorded
   one, that id wins. A record is written only when this document
   itself puts a fragment's target in place, one
   `requestAnimationFrame` later, at two moments, and it is dropped on
   history traversal:
   - *On load, in a fixed order.* `gallery.js` applies a stored
     page-wide view (2.6) at `DOMContentLoaded`, so before `load`. At
     `load`, if this was a fresh navigation
     (`performance.getEntriesByType("navigation")[0].type` is
     `"navigate"`) and the reader has not touched the page since it
     began loading (no `wheel`, `touchstart`, `pointerdown` or
     `keydown`, listened for from the moment the blocking script runs in
     `<head>`), `gallery.js` puts the target in place itself with
     `scrollIntoView({block: "start"})` and then records. The browser's
     own fragment scroll ran during parsing, against a layout the stored
     view then changed (Code replaces every preview above the target
     with a code block of another height), and whether scroll anchoring
     carried the target along is the engine's call. `scrollIntoView`
     honours `scroll-padding` and is clamped at the page's end exactly
     as fragment navigation is, so it puts the target where following
     the fragment now would. After a reload or a history traversal the
     browser restores the reader's own scroll position, which is not a
     fragment the reader is on; so nothing is positioned or recorded and
     rule 2 decides, as it does after any input before `load`.
   - *On following a fragment link in this document*: a `click` on a
     link whose URL is this page's with a fragment naming an anchor,
     new or the one already in the address, that this document will
     follow. The guard is `shell.js`'s for its back control
     (`ui/shell.js:132`): primary button, no Ctrl, Meta, Shift or Alt,
     and no `target` or `download` attribute. A Ctrl/Cmd-click opens
     another tab and scrolls nothing here, so it must not renew
     anything. Cancellation is read late: the record is written in the
     frame after the click only if `event.defaultPrevented` is still
     false then, since a listener that runs after `gallery.js`'s can
     cancel the navigation, and the event keeps the flag after
     dispatch. Following the fragment already in the address is the
     renewal case: it scrolls without firing `hashchange`, so after a
     layout change (resize, a preview tab chosen above the target) it
     is the only signal there is. The scroll is the link's activation
     behaviour, which runs after the click's listeners, so the frame
     after the click sees it in place.
   - *Not on `hashchange`.* A `hashchange` also fires after the reader
     goes Back or Forward between fragment entries, and then the
     browser restores the scroll position the reader left that entry
     at, which is wherever they had scrolled to, not the fragment's
     target. Recording there made the restored fragment win over the
     section being read. `popstate`, which a same-document traversal
     fires, drops the record, and so does a Navigation API `navigate`
     event whose `navigationType` is `"traverse"` where the API exists.
     A fragment typed into the address bar is not recorded either; the
     id check makes an older record for another id irrelevant, and
     rule 2 decides.

   Measuring the target rather than `scrollY` means a layout change
   above it that scroll anchoring compensates for does not cancel the
   preference, while any scroll that moves the target does. This is the
   case geometry cannot answer: a target near the end of the page,
   where scrolling is clamped and the target stays below the reading
   line, and an anchor sharing its row with others.
2. **Otherwise, the anchor at the reading line.** Among the
   `[data-ds-anchor]` elements whose top is at or above the reading
   line, take those with the greatest top, and of them the first in
   document order. On most pages that is simply the last section
   passed. On Icons, where every glyph in the grid is an anchor
   (`page.go:2500`) and a row of them shares one top, it is the first
   glyph of the row the reader is on, not the last.
3. **Above every anchor**, the link is left alone.

Enter fires `click`, so the keyboard gets the same. Anchor ids are the
same in every theme and locale (they are built from English names,
`page.go:610`; checked across `day/en` and `signal/ja` on every page),
and a test holds that.

*The switcher's own address is never left changed.* The page renders
each switcher link fragment-free, and `gallery.js` keeps that canonical
address per link, read from the rendered `href` when the script starts,
so the page carries no extra attribute. On `click` it schedules the
restore *first* and then sets the computed `href`, so nothing that
fails between the two can leave it set. The restore is
`setTimeout(…, 0)` and not a microtask: the microtask checkpoint after
a listener returns comes before the link's activation behaviour reads
`href`, so a microtask would put the old address back before the
browser followed the new one. `pageshow` restores again, for a page
that comes back from the back/forward cache with a timer that never
ran. So a Ctrl/Cmd- or Shift-click opens the computed place in a new
tab or window and leaves this page's link canonical, and a later plain
click from the top of the page lands at the top of the other page. A
middle-click fires `auxclick`, not `click`, and a context menu's "Open
in new tab" reads the attribute as rendered, so both open the top of
the page, as does every switcher with scripts off.

*The phone index.* Its foot's theme and language links are rendered
with a fixed fragment, `index.html#ds-prefs`, the foot's own id. The
reader is at the controls when they tap one, and lands with the
controls on screen in the new theme or language; the scheme buttons
navigate nowhere. It is static, so it works with scripts off, and
nothing is rewritten, so finding 38 cannot arise. The desktop handler
is delegated from `.ds-top` and never touches these links. `shell.js`
reads `#ds-prefs` on arrival and leaves it alone, since it names no
nav link (`ui/shell.js:79-82`).

*Phone content pages* carry no switchers. A reader deep in Form who
wants another language goes Back to the index, switches there and taps
Form again, landing at its top: the place in the page is not carried
(Risks).

**A page restored from the back/forward cache keeps up with the
scheme.** `gallery.js` applies the stored scheme while the head parses,
again to the buttons at `DOMContentLoaded`, and to each frame on its
`load` (`gallery.js:95-117`); a click updates only the document it is in
and that document's frames (`:130-137`). A page restored from the
back/forward cache is reactivated, not re-run, so none of that happens
again. Before this spec that was rare, since every page carried the
toggle and a reader could correct it; on a phone the toggle is now on
the index only (decision 7), and `shell.js` deliberately sends Back
through history so the index and its pages come back from the cache
(`ui/shell.js:121-142`). So: open Form in Light, go Back to the index,
choose Dark, go Forward, and Form returns Light, previews and all, with
no control on it to fix that. `gallery.js` therefore listens for
`pageshow` and, when `event.persisted` is true, rereads the stored
scheme and applies it to the root, sets the scheme buttons' pressed
state (on the index, where they are) and repaints every frame whose
document has loaded, the same three steps as a click. Restored scroll
position, widget choices and the page-wide view are left as the reader
left them on that page: those are the page's own state, not a
preference set somewhere else.

The theme and language need nothing of the kind. They are not stored;
each is a different file (`<theme>/<locale>/form.html`), so a restored
Form is Form in the theme and language its own address names, which is
correct: a reader who switched theme on the index and then went Forward
has gone forward to the page they were on, in the theme it was in.
Choosing a section on the new index opens it in the new theme.

### 2.4 Search finds what people call things

The rail filter matches on each entry's name and a short synonym list
kept in Go next to the sample data, keyed by the entry's real anchor id
— the kind-prefixed id `anchorID` builds (`page.go:610`):
`partial-field-check`: "checkbox toggle switch"; `idiom-modal`: "dialog
popup"; `partial-choice-field`: "radio"; `idiom-list-grid`: "table";
`idiom-box`: "card panel"; `partial-person`: "avatar user";
`idiom-button`: "button submit cta". The terms ride on the rail links
as `data-ds-terms`.

- **English, untranslated.** The terms are web-platform vocabulary a
  developer types — the same footing as the partial names, which are
  code and not translated (prose.go's rule). They are not prose keys,
  so they cost no translations and the leak gate does not see them. A
  Spanish reader typing "casilla" finds nothing; that is the accepted
  limit.
- **Not the Blurb.** Matching the one-line description was in the
  first draft. Measured, it would put 11,023 bytes of translated text
  (Hindi) into the rail on every one of the 540 pages; the synonyms
  cost ~350.
- The plain page links in the rail (Overview, …) filter by name like
  every other entry, so "No matches" and a visible Overview link never
  show together.
- A test fails on a synonym entry whose anchor id no longer exists.

### 2.5 A Buttons entry

A `button` sample is added to `ui.Styleguide`, so it appears on the UI
primitives page like every other idiom — that page's promise is that
everything on it is "the exact sample ui.Styleguide returns", and
`TestEveryStyleguideSampleAppearsAcrossThePages` covers it with no
edit. One frame holds the set that shows distinct behaviour, not the
Cartesian product: the four variants at default size (`rst-btn`,
`primary`, `ghost`, `danger`), `sm` and `lg` on `primary`, a link
styled as a button (`<a rst-btn href>`), `block`, and `disabled`. Its
blurb points to form-foot's idle/working pair for the busy state
rather than drawing it again, so there is no second busy illustration
and no copy-eligibility question here.

### 2.6 One view choice for the whole page

On every page with at least one Code tab (the five component pages,
UI primitives, Formats, Screens), a group of four buttons at the top
of the content: **Auto**, Desktop, Mobile, Code, `role="group"`,
labelled "Show every example as".

- **Buttons, not radios,** so pressing the one already pressed still
  fires `click` and re-applies it — the case a radio cannot express.
- **Initial state.** With nothing stored, Auto is pressed and no
  widget radio is checked: each widget keeps today's width-chosen
  default (`gallery.css:325`).
- **Applying.** Desktop, Mobile or Code sets that radio in every
  widget that has it. A widget with no such tab (a framed page has no
  Code) is left on Auto. Auto clears every widget's radios. Setting
  `.checked` fires no `change`, which is fine: the panels follow from
  `:has(:checked)`.
- **What the group announces** is read off the radios, never off what
  the width happens to be showing. Auto is pressed when no widget has
  a checked radio. Desktop, Mobile or Code is pressed when every widget
  that has that tab has it checked and every widget without it is
  unchecked. Anything else is mixed, and no button is pressed: a reader
  who picks Mobile in one widget after pressing Code sees nothing
  pressed, and pressing Code again brings that widget back. An
  unchecked widget showing Desktop because its stage is wide is Auto,
  not Desktop, so no resize handling is needed. Recomputed on every
  widget radio `change`.
- **Persistence.** The last page-wide choice is stored under
  `rst-ds-view` (`desktop`, `mobile`, `code`; Auto removes the key),
  with the same try/catch wrappers as the scheme (`gallery.js:46`), and
  applied to the next page at `DOMContentLoaded`: as soon as the
  widgets exist, before `load`, which is the order the fragment record
  depends on (2.3, rule 1). A widget's own choice is not stored.
- **On a phone** the group sits at the top of the content like
  everywhere else; its buttons take the touch size (2.3).
- **No script.** The group is `display: none` until `data-rst-js` is
  set, like the scheme toggle. The per-sample radios are the
  scriptless behaviour, unchanged.

### 2.7 Compact rows for small inline components

status-pill, badge and meter (the list is `partialDoc.Row bool` in
`samples.go`, not a heuristic) render all their states in one widget:

- **Frame.** One preview document holding a `<ul>` of the states, each
  `<li>` a translated state label and the sample, laid out by a few
  lines in the document's own `<style>` (flex, wrapping, so the row
  becomes rows on a phone). No `ds-` classes inside it — the
  stylesheet gate would require `gallery.css` there. One frame title
  per partial, from `previewTitle(locale, name, "")`, still unique.
  Heights re-measured.
- **Code.** For each state in order: its label (`<p class="ds-state">`)
  and its call, each call with its own Copy. One Rendered HTML
  disclosure below lists each state's HTML under the same labels.
- **Notes** stay, listed under the widget as "label: note" in state
  order.
- **Duplicates.** Only states that render byte-identically are
  duplicates. status-pill's "Default tone (neutral)" is removed; its
  neutral state drops `Tone` from its data and gains the Note "Tone
  defaults to neutral, so this call leaves it out." (copy review).
  badge's "No tone (quiet default)" renders differently from neutral
  (`samples.go:640`) and stays. A test fails if two states of one
  partial render the same bytes, so a duplicate cannot come back.
- The frame sits flush with its tab bar rather than centred in the
  card.

### 2.8 Plain prose

- On the five component pages the intro block, the "Links here are
  inactive" callout and the two notes above the first sample become
  two sentences: "Each example is live, but its links go nowhere." and
  "Code shows the template call to copy." UI primitives, Formats and
  Screens get the first only. Final wording through copy review.
- No callout on Shells, which has no Code tab.
- Getting started's "Using it without the framework" says attributes,
  not "plain classes", and gets a copyable `<link>` snippet for
  `tokens.css` and a theme.
- **Inventory** (English is the key; each added key is copy-reviewed,
  then eleven translations drafted into `prose.go`, labelled as
  machine-drafted in the branch description):

| Removed (11 keys) | Added (14 keys) |
|---|---|
| "An overview of everything the design system provides. Theme: {theme}. Language: {language}." | "Each example is live, but its links go nowhere." |
| "Sections" | "Code shows the template call to copy." |
| "Pre-built, consistent UI elements, rendered server-side." | "Rendered HTML" |
| "The framework's own vocabulary calls these partials: …" | "Put this inside {wrapper}." |
| "Links here are inactive" | "Copy" |
| "Links inactive, sample source provided." | "Copied" |
| "Each sample below in its own frame." | "Copy failed. Select the code and copy it yourself." |
| "Sample content in English. Sample shells translated." | "Show every example as" |
| "Default tone (neutral)" | "Auto" |
| "The names above are links. Take tokens.css and one theme …" | "Tone defaults to neutral, so this call leaves it out." |
| | the `button` idiom blurb |
| | the rewritten "Using it without the framework" lead |
| | the lead for its `<link>` snippet |
| locale-menu's Note ("This one posts to /_locale, …") | locale-menu's Note, rewritten to add where `.Locales` comes from (1.1) |

  154 translations to draft, 121 to delete. `TestEveryProseKeyIsTranslated`
  fails on a stale key as well as a missing one
  (`designsystem_test.go:1300`), so the removals are part of the work,
  not cleanup.
- **Leak-gate sentinels.** Two of the three (`designsystem_test.go:1413`)
  are sentences this spec removes. They become "Each example is live,
  but its links go nowhere." and the `button` blurb; "Screens stack
  vertically" stays.
- **Fixture exemptions.** `proseFixtureCollisions`
  (`designsystem_test.go:1368`) is matched with
  `strings.HasSuffix(name, on)` (`:1493`). The fixture strings move
  out of `list-screen.html` and `primitives.html` into their preview
  files (`list-screen/partial-page-header-0.html`,
  `primitives/idiom-modal-0.html`), so the table is keyed by page kind
  and matched with a helper that maps both a page and the files under
  its directory to that kind. The sweep count at `:1470` gains the
  translated preview files.

### 2.9 On a phone, Mobile is the page at its real size

Today Mobile lays out a 390px page and scales it into the stage by
`--ds-k` (`gallery.css:328`, `:333`, `:346`, `:340`). On a 390px phone
the stage is 324px (the page's 1rem padding, `ui/tokens.css:186-191`,
and the sample card's 1rem padding and 1px border, `gallery.css:47`,
each side), or 309px under the classic scrollbar the browser legs run
with. `--ds-k` is then 0.83 (0.79), a 16px field shows at 13.3px
(12.6px), and the preview is no longer the thing it previews. Decision
5 makes Mobile the page at its real size.

- **The stage is the screen's width.** Below 800px the widget bleeds to
  the viewport's edges: `.ds-view` takes negative inline margins equal
  to what lies between it and the edge, and gives its tabs and code
  panel the same back as margins so they stay in the text column. Only
  the stage reaches the edges. The box drops its inline borders and
  corner radius there: the box is content-box sized under
  `max-inline-size: 100%` (`gallery.css:337`), so its 2px of border
  would push a full-width box past the viewport and scroll the page
  sideways, and at the screen's edge a border is only a line against
  the bezel.

  ```
  @media (max-width: 799.98px) {
    /* The bleed is what sits between the widget and the screen's edge:
       the page's padding, and inside a sample card its padding and
       border as well. Written per context rather than measured, so a
       change to either shows up as a sideways scroll in the reflow and
       phone legs instead of a silently narrower stage. */
    .ds-view { --ds-bleed: var(--rst-sp-4); margin-inline: calc(-1 * var(--ds-bleed)); }
    .ds-sample .ds-view { --ds-bleed: calc(2 * var(--rst-sp-4) + 1px); }
    .ds-view__tabs, .ds-view__code { margin-inline: var(--ds-bleed); }
    /* Content-box sizing under max-inline-size: 100%: a border here
       would make a full-width box 2px wider than the screen. */
    .ds-view__box { border-inline-width: 0; border-radius: 0; }
  }
  ```

  `.ds-view` is the container `--ds-k` is measured against
  (`gallery.css:264-272`), so the bleed widens the measured stage too,
  and the drive's check that container and stage are the same width
  still holds.
- **Mobile's width is the stage's, up to 390px.** `.ds-view` gains
  `--ds-wm: 390px`, narrowed to `min(390px, 100cqw)` inside the
  `@supports` block that already guards `100cqw` (`gallery.css:339`),
  and the three Mobile declarations read `--ds-w: var(--ds-wm)`
  instead of `390px` (`gallery.css:328`, `:333`, `:346`). `--ds-k` is
  `clamp(--ds-kmin, stage / --ds-w, 1)` (`gallery.css:340`), and the
  stage over a width no larger than the stage is at least 1, so Mobile
  is never scaled: on a 390px phone it is the 390px page, edge to edge;
  on a 360px phone the 360px page; on any stage of 390px or more, the
  centred 390px page it has always been. Checked in headless Chromium
  with these declarations: a 375px container gives a 375px frame,
  `--ds-k` 1, transform `matrix(1, 0, 0, 1, 0, 0)`. An engine without
  container units keeps `390px` and today's behaviour.
- **Why no media query decides "phone".** A stage narrower than 390px
  is the only case in which Mobile was ever scaled, and it is exactly
  the case in which laying the page out at the stage's width is what
  that screen would show. `min()` keeps a 760px window's Mobile the
  390px phone rather than a 760px tablet. A fine pointer in a narrow
  window gets the same frame, and the frame's own width puts its
  document inside the touch query's `max-width: 40rem` half
  (`ui/tokens.css:150`), so its type is the phone's either way.
- **Desktop on a phone** is unchanged in kind: `--ds-k` clamps at 0.72
  and the box pans (`gallery.css:240-247`), now across the whole width
  of the screen and from the inline start in either direction (2.10).
- **Auto on a phone opens on Mobile, as today.** The opening view is
  chosen by the stage width (`gallery.css:325-334`), and a phone's bled
  stage is under both thresholds (648px and 864px). The bleed does move
  the line between 640 and 800px: the stage there is the window less a
  scrollbar rather than less 66px more, so a component opens on Desktop
  in a window about 66px narrower than before. That is the threshold
  doing what it says, legible at 0.72 (`gallery.css:185-194`), measured
  on the stage as ever.
- **Heights.** A Mobile box is `--ds-hm` tall rather than
  `--ds-hm × 0.83`, so a phone page grows by about a fifth of its Mobile
  frames; compact rows (2.7) take 12 frames off Display, the longest.
- **Mobile heights are sized for the narrowest frame, not for 390px.**
  Below 390px a frame now lays out at the stage's width, and text that
  fits one line at 390px can wrap at 305px, so a height measured at
  390px is no longer a ceiling. `--ds-hm` is today 1.25× the desktop
  height, or an entry in `previewMobileHeights` where a layout changes
  axis (`page.go:1190-1223`), and it is held only at a 390px frame:
  the height drive selects Mobile in a 1500px window
  (`browser_test.go:1024-1038`). The narrowest frame the gallery
  commits to is the one a 320px viewport gives (320px is the reflow
  width; 305px under the browser legs' classic scrollbar, which is
  narrower and so errs taller), and that is the width every Mobile
  height must fit. Where the factor falls short there, the example gets
  a measured `previewMobileHeights` entry, and that map's comment,
  which today admits only a change of axis, says so: an entry is also
  owed by an example that wraps taller at the narrowest phone frame.
  The allowance is none: at that width a Mobile frame's document must
  fit its box, so nothing in a component preview is reached by
  scrolling inside it. The one exception is the 48px for frames
  holding a sidebar-shell page, whose rail is 100dvh tall and so can
  never be fitted (`browser_test.go:1078-1087`); today's gate grants it
  to every frame, and the narrow leg grants it to those frames only. Taller
  boxes at 390px are the price: the slack is logged by the same drive,
  as it is today.
- **Code** is not part of the bleed. It stays in the text column, where
  its soft wrap (1.2) already fits it.

### 2.10 Right to left: the frame is anchored at inline start

`.ds-view__frame` is placed with `left: 0; top: 0` and scaled from
`top left` (`gallery.css:338`). In a right-to-left page the box's
scroll origin is its right edge and it can scroll only leftward, so a
frame hanging off the right edge is overflow nobody can reach. Measured
in headless Chromium on 2026-10-04 with the gallery's own declarations
in a `dir="rtl"` page: a 900px frame scaled 0.72 in a 309px box, which
is Desktop on a phone, leaves the box with `scrollWidth` equal to
`clientWidth` (309px), so no scrolling at all, and the right-hand
339px of the 648px rendering, the start of an Arabic page, cropped. The
same box with the frame at `inset-inline-start: 0`, scaled from
`top right`, scrolls 339px and opens on the page's start.

```
.ds-view__frame { inset-block-start: 0; inset-inline-start: 0; transform-origin: top left; }
/* transform-origin has no logical keywords, so the side is a selector,
   as tokens.css does for its drawn chevrons. Anchored at the physical
   left, a right-to-left box could not scroll to the frame's start. */
[dir="rtl"] .ds-view__frame { transform-origin: top right; }
```

- The `[dir="rtl"]` ancestor selector is tokens.css's own form for its
  mirrored drawings (`ui/tokens.css:2178`). Nothing else in the widget
  is physical.
- It shows only where the frame is wider than its box, which is Desktop
  panning on a narrow screen. At desktop widths the box is the scaled
  frame's width, centred by `margin-inline: auto`, and the anchor is
  invisible.
- The rail is on the right in Arabic with no rule of the gallery's: the
  shell's grid puts it in column 1 (`ui/tokens.css:1959`), and the 2.3
  placement names lines, not sides.

### Out of this round

Revisiting the virtual frame widths; translated search synonyms; the
preview tabs' own touch size (they are under 30px tall, which passes
WCAG 2.5.8's 24px but not the framework's 44px); deleting tokens.css's
legacy drawer rules, which ask to go "once no layout and no gallery page
writes rst-shell-chrome" (`ui/tokens.css:1911-1918`): after this spec no
gallery page does, but an app's old hand-copied layout may, so that is
a framework decision.

## Copy for review

The phone navigation adds no user-facing string. Its chrome
speaks in the framework's strings, reviewed and translated with
mobile-ergonomics: the back control's visible "Sections" and its name
"Back to {name}" (`locales/en.toml:34-35`), and the rows nav's label is
that same "Sections". The index title ("rastrillo design system"), its
lead (the Overview's paragraph), the row labels (section titles and
the Demos entries) and the foot's controls are existing gallery keys.

The drafts below are this spec's (2.8, Added). In
`internal/designsystem` the English is also the translation key, so
each is reviewed before its eleven translations are drafted. No draft has
an em dash, and none may gain one in review. C15 and C16 are existing
strings that use one; they join the list so that this spec removes
them (operator, 2026-10-04).

| Id | Draft | Where |
|---|---|---|
| C1 | "Each example is live, but its links go nowhere." | framed pages' intro (2.8) |
| C2 | "Code shows the template call to copy." | component pages' intro (2.8) |
| C3 | "Rendered HTML" | disclosure summary (1.2) |
| C4 | "Put this inside {wrapper}." | wrapper line (1.2) |
| C5 | "Copy" | copy button (1.4) |
| C6 | "Copied" | copy button and live region (1.4) |
| C7 | "Copy failed. Select the code and copy it yourself." | live region (1.4) |
| C8 | "Show every example as" | page-wide group label (2.6) |
| C9 | "Auto" | page-wide group (2.6) |
| C10 | "Tone defaults to neutral, so this call leaves it out." | status-pill note (2.7) |
| C11 | the `button` idiom blurb, not yet drafted | 2.5 |
| C12 | the "Using it without the framework" lead, not yet drafted | 2.8 |
| C13 | the lead for its `<link>` snippet, not yet drafted | 2.8 |
| C14 | locale-menu's Note, rewritten to say where `.Locales` comes from, not yet drafted | 1.1 |
| C15 | the document title's separator, today " — " ("Form — rastrillo design system — day", `page.go:2351`; the modal route, shell demos and demo application titles the same way, `:2683`, `:1759`, `:1967`). Drafts: "Form: rastrillo design system (day)" or "Form · rastrillo design system · day"; the format is copy review's call | every gallery `<title>` |
| C16 | the preview frame title's separator, today " — " ("{name} sample standalone preview — Required", `page.go:1516`), read aloud as the frame's name. Draft: "{name} sample standalone preview, Required" | every preview frame |
| C17 | the copy button's name joiner (1.4). Draft: ", " ("Copy field-text, Required") | copy buttons |

## Budgets

**Pages.** The model behind the 1.5 table, per heaviest variant: shed
`srcdoc`, the tab strip, the old intro (less ~300 bytes for the new
sentences), the subtitle, the wrapper in source, and four bytes per
literal quote in code; add 72 bytes of `src` per frame, 13 bytes per
highlighted token (counted from the 2026-09-27 source blocks: 943 on
Date and time, 1,193 on Form), 12 bytes per block-level tag for
formatting, the calls (measured by generating them: 3,155 escaped bytes
on Date and time, 8,095 on Form), ~110 bytes per disclosure, ~180 per
wrapper line (Hindi), ~500 for the view control and copy strings, ~500
for the brand and search terms; Display also sheds 12 widgets to
compact rows, and UI primitives gains ~6 KB for Buttons. Copy-button
names are built by the script from text already on the page (1.4), so
they add no bytes per block. The pages without frames each get ~870
bytes lighter (tab strip and subtitle out, brand and terms in).

The 2026-10-04 chrome changes little: on a content page the back
control, the two head links and the root's `="page"` (about 330 bytes
in Hindi) replace the drawer's `<details>` (287 bytes, measured on Form
`signal/hi`): about +40 bytes. The Overview gains the index's title, lead, rows and the
foot's copy of the controls, about 5.5 KB on its heaviest variant
(`signal/bn`, 30,284 bytes today; the bar's controls alone are 2,051
bytes there), so it lands near 36 KB.

Every page stays under 131,072 bytes and `pageBudgetDebt` empties:
Form's entry (`designsystem_test.go:97-106`) must go with the move,
since the gate fails on an entry no page needs (`:88-90`). The
tightest page is Form at ~8.7 KB of headroom. It was ~19 KB on the
2026-09-27 tree; field-url's three samples and the framework's new
asset links landed since.

`shell.js` (7,650 bytes) and `shell.css` (4,294) now load on every
gallery page, once per visit from cache: what the sidebar shell costs
any app.

**gallery.js.** 9,553 bytes today against a 10,240-byte cap
(`designsystem_test.go:1749`). The first estimate of the additions,
including the comments that say why, was ~5,000 bytes (a 15 KiB cap).
The plan's review weighed the code actually supplied and found it
undercounted badly: applied in order, the first draft came to 24,651
bytes. The code was then slimmed. One pair of storage wrappers is now
shared by the scheme and the view, no helper exists twice, a `Promise`
carries the copy's error handling, and comments that repeated each
other were cut. Nothing in `shell.js` or `rastrillo.js` is reusable,
because both are closed IIFEs. Measured on 2026-10-04 by applying each
feature's code to today's file:

| Feature | Added | Running total |
|---|---|---|
| Today: the scheme, its frames, the rail filter | | 9,553 |
| Copy: feature detection, insertion, names built from the page, write, announce, select on failure | 2,955 | 12,508 |
| Search: folding `data-ds-terms` in, filtering the plain page links | 467 | 12,975 |
| Page-wide view, with storage shared with the scheme | 2,580 | 15,555 |
| Keeping your place, and the scheme on a back/forward cache restore (2.3) | 5,201 | 20,756 |

The cap becomes 22,832 bytes: the measured 20,756 plus 10%. That is
over the 15 KiB first proposed, and the reason is stated rather than
the comments cut to fit. The why-comments that remain are what make
the keeping-your-place rules (2.3) reviewable at all. The script is
the docs site's own and is never shipped to an app. It loads once,
blocking in `<head>`, and is cached from then on, so its bytes are
paid on the first gallery page a reader opens, not on every page. The
test's own history (`designsystem_test.go:1744`, "It fit, with 32 bytes
to spare") says a ceiling that tight buys cut comments rather than less
code. The test's comment keeps the running totals the way it itemises
the frame painting now. The phone's index, back control and slide are
the framework's `shell.js` and `shell.css` plus the CSS in 2.3, and
the existing frame painting needs nothing for `src` frames.

## Tests

### New

- **Every generated call renders the partial's own output** — for every
  partial sample in all twelve locales, parse `Source`, execute against
  `Dot`, compare byte-for-byte with `renderSample`'s output (1.1). Also
  asserts `callFor` errors on a struct, a pointer, `nil` and a
  `template.HTML` value.
- **The preview document is the sample, wrapped and deadened** — the
  transforms held on their own, separately from the call, under two
  contracts:
  - *A single-sample preview*: the file's body is exactly
    `deaden(mount, wrap(w, renderSample(state)))`, followed by the sink
    iff it contains a form.
  - *A grouped preview* (2.7): the body is exactly one `<ul>` with one
    `<li>` per state of the partial, in `samples.go` order; each `<li>`
    holds the state's label as `proseIn(locale, state)` and then
    exactly `deaden(mount, wrap(w, renderSample(state)))`, each state
    appearing once and nowhere else in the document; a sink follows
    iff any state contains a form (none of the three today). Every
    grouped partial's states appear in its one preview and in no
    single-sample preview.
- **Formatting moves only whitespace between block-level tags** —
  removing whitespace-only text between tags where one side is
  block-level gives the same string for the formatted and the raw
  markup; `<pre>`/`<textarea>` content and attribute values are
  untouched.
- **The Code tab never carries the demo wrapper** — no source block of
  a partial sample begins with the markup `wrap` injects, and no source
  block anywhere contains `action="#"`. Idioms whose own markup is a
  box or a stat band (`ui/styleguide.go`) are not wrappers and are not
  checked.
- **The text on screen is the text copied** — for every source block,
  the highlighted HTML with its tags stripped and entities decoded
  equals exactly the intended source: the call's `Source`,
  `format(renderSample(…))`, or the formatted idiom/screen/format
  markup. Entities in the source (`&euro;`, `&minus;` in the stat band)
  survive as written. The Copy leg asserts the clipboard receives that
  same string.
- **No code block scrolls sideways at 390px** — browser leg, Code
  chosen page-wide and every disclosure open, on Form and Date and
  time: every visible `.ds-src` has `scrollWidth <= clientWidth`. This
  replaces the first draft's 100-column rule.
- **Highlight colours pass 4.5:1** — the Go pair check (1.3) in every
  theme × scheme.
- **Axe with Code selected** — `TestA11yScansTheGallery` gains a leg on
  Form and UI primitives in every theme × scheme (six each): press
  Code in the page-wide group, open every `details.ds-html`, run axe.
  Each leg first asserts which highlight elements are on the page, so
  its coverage cannot shrink silently: all five on Form (calls carry
  actions and strings, the HTML tags, attributes and values); tag,
  attribute and value on UI primitives, whose markup has no template
  actions. Today's scan never sees a code panel (`gallery.css:347`
  hides them, and `TestA11yScansTheGallery`, `a11y_test.go:413-445`,
  presses no tab).
- **Copy** — browser leg with a stubbed clipboard: a resolved write
  announces "Copied" and changes the label; a rejected write announces
  the failure and leaves the block's text selected; with
  `navigator.clipboard` removed, no button is inserted; every Copy
  button's accessible name is unique on Form and on Display. A Go
  check: a `data-ds-nocopy` block has no call and is only on a sample
  marked `Illustration`.
- **Page-wide view** — browser leg for every rule in 2.6: Auto initially
  with nothing checked; Code applies to every widget; a local Mobile
  un-presses the group; Code again re-applies; the next page loads in
  Code; Auto clears radios and the stored key; scripts off, the group
  has no box.
- **Every page's `<h1>` is its own title**, no two pages share one, and
  no page's heading outline skips a level. The `<h1>` read is main's;
  the Overview alone also carries the index's `[rst-shell-title]`
  (2.1), which only the phone index shows, and the axe legs at both
  widths see exactly one.
- **No page loads a script it does not use** — gallery pages link no
  `select.js`/`calendar.js`/`datetime.js`.
- **No frame carries `srcdoc`**, every frame `src` names a file in the
  tree, and every file under a page's preview directory is named by
  exactly one frame (no orphans).
- **Search terms** — every synonym entry names an existing anchor; a
  browser leg types "checkbox", "dialog" and "button" and finds the
  right entries, and types a non-match and sees "No matches" with no
  visible page link.
- **Position survives a switch** (desktop, 1280px, where the switchers
  are) — browser leg: scroll so a partial's section sits on the reading
  line, click a theme link, and land with that partial's id as the
  fragment and its section's top on the reading line (±2px); the same
  through a language-menu link. Then, with no scrolling in between:
  follow a rail fragment, switch theme, switch language — the same
  section every time. The two cases rule 1 exists for: on Form, follow
  the rail fragment of the last partial (the page clamps before it
  reaches the reading line) and switch — that partial's id, not the
  one above it; on Icons, follow the fragment of the first glyph in a
  grid row and switch — that glyph, not the row's last. And that rule 1
  lets go: after either, scroll by a screen and switch, and the id is
  the one geometry picks. Where this leg asserts a landing position it
  is "top on the reading line (±2px), or the page scrolled to its end
  with the target's top below the line", because the browser clamps at
  the end.
  - *The record's start* (finding 37): store Code as the page-wide view,
    load Form at the last partial's fragment and switch at once — that
    partial (the target was put in place after Code applied); the same
    load, then a reload after scrolling a screen up — the id geometry
    picks, because a restored scroll is not a fragment the reader is on.
  - *The record's renewal* (finding 37): follow the last partial's rail
    fragment, press Mobile on a widget above it, follow the same
    fragment again (no `hashchange`) and switch — that partial.
  - *History is not a destination* (round 6): follow the last partial's
    rail fragment, scroll a screen up, follow another partial's
    fragment, press Back (the address names the last partial again and
    the browser restores the scrolled-up position) and switch — the id
    geometry picks, not the last partial. Then Forward and switch — the
    id geometry picks again.
  - *Only a followed link renews* (round 6): follow the last partial's
    rail fragment, scroll a screen up, Ctrl-click that same rail link
    (a new tab opens, this page does not move) and switch — the id
    geometry picks. The same with a plain click whose default a test
    listener, added after `gallery.js`'s, prevents.
  - *Canonical switcher links* (finding 38): Ctrl-click a theme link
    from deep in the page — the new tab opens at the computed fragment
    and this page's `href` is fragment-free again; scroll above every
    anchor and plain-click it — the other theme's page loads with no
    fragment, at its top; back, and middle-click it — the new tab opens
    at the top; return through the back/forward cache — still
    fragment-free.

  A Go check that every page's anchor ids are identical across themes
  and locales.
- **Focus is never under the pinned bar** — on Form in `day/en` and
  `day/ar`, at 1440 and 800px: forty Tabs from the first control in
  `<main>` and forty Shift+Tabs back, the skip link, and every rail
  fragment of the page. For each focused element or target, its top is
  at or below the bar's bottom edge. (Below 800px the pinned box is the
  framework's back strip, and its clearance is the framework's test,
  2.3.) Inside a preview frame the element's rectangle is
  converted through the frame's transform — the frame is
  `scale(var(--ds-k))` (`gallery.css:338`), so its inner offset is
  multiplied by the frame box's rendered width over its layout width
  before adding the box's top.
- **What is pinned fits its reservation** — separately, every theme ×
  locale (36 pages of Form), scripts on and off. At 1440, 1024, 1023 and
  800px: the union of the bar's children's bounding boxes lies inside
  the bar, nothing overflows sideways, and after scrolling to the
  bottom of the page the bar's top is still 0; with the language menu
  open, its panel lies inside the viewport in LTR and RTL, with anchor
  positioning available and with it disabled, and all twelve languages
  are reachable by keyboard.
- **Nothing paints over the bar or the back strip** — scrolled through
  Form with Desktop chosen page-wide (positioned boxes, transformed
  frames): at 1440px `elementFromPoint` at the bar's centre and edges,
  and at 390px at the back strip's, is always inside it.
- **The phone index and the way back** (replaces the drawer leg) —
  browser leg at 390px with a coarse pointer (`harness.WithCoarsePointer`,
  with `requirePointer` as its control, `ui/touch_browser_test.go:60-80`),
  in `day/en` and `day/ar`:
  - The Overview is the index: the rail has a box and `<main>` has none;
    the one visible `<h1>` is "rastrillo design system"; the lead is
    shown; the rows are one per page kind but the Overview, in
    `pageKinds()` order, then Demos, and each row is at least 44px tall.
    The bar has no box.
  - Tab runs filter, rows, then the foot's theme links, scheme buttons
    and language summary, in screen order; every scheme button is at
    least 44px tall. (The language menu has its own leg, below.)
  - Tapping the Form row loads `form.html` as a content page: the back
    control is the first thing after the skip link and has a box, the
    rail and the bar have none, and no theme, scheme or language
    control has a box. The back control's `href` is that theme ×
    locale's `index.html#nav-form`. Activating it returns to the index
    with `#nav-form` focused (scripts on). Scripts off: the row is the
    `:target` and the next Tab lands on the row after it (the
    starting-point assertion mobile-ergonomics uses, its §4.4).
  - Typing "checkbox" into the index's filter hides the rows and shows
    the matching tree entry; tapping it loads Form with
    `#partial-field-check` as the fragment and that partial in the
    viewport; clearing the filter brings the rows back.
  - A theme link in the index's foot loads the other theme's index at
    `#ds-prefs` with the foot on screen.
  - Axe runs on both views in every theme × scheme, with the
    animations and view transition settled first, as
    `TestA11yScansTheShellsCollapsed` does (`a11y_test.go:479-523`).
- **Mobile heights fit the narrowest phone frame** (2.9; rounds 6 and
  7) — a test of its own, `TestPreviewFrameHeightsFitAtThePhonesNarrowest`,
  beside `TestPreviewFrameHeightsFitTheirContent` (`browser_test.go:939`)
  and sharing its measuring script, with its own deadline (CI bound,
  below). At a 320px viewport, Mobile selected on every widget through
  its own radio, over every page the wide drive covers, Display's
  grouped status-pill, badge and meter previews (2.7) included.
  - *Coverage, by viewport.* The wide drive's coverage assertion stays
    as it is (`:1013-1018`: a page kind with frames and no row fails).
    The narrow test asserts the same table minus a named exemption list
    in which every entry carries its reason, and today it has one:
    the Overview, because below 800px it is the phone index and its main,
    which holds the framed demo application (`page.go:2438`), is
    `display: none` (`ui/tokens.css:2187`). No preview is displayed
    there, so there is no stage to measure, and the wide drive's wait for
    a visible frame (`:1026`) would time out. The demo's own pages are
    real documents, and at phone width they are checked directly: the
    320px reflow gate already loads the demo application and its
    requests page (`a11y_test.go:909-910`). An exemption naming a page
    that does display a preview below 800px fails the test, so the list
    cannot grow quietly.
  - *Locales.* In CI, a fixed subset of the root theme's locales: `en`,
    `ar`, and the one or two whose labels wrap worst. Those are found
    once during implementation by running the full twelve-locale sweep
    and comparing, per page, how far each locale's content height
    exceeds `en`'s; the subset is written in the test with that
    measurement beside it as its reason. The full twelve run through
    `make browser-sweep` (CI bound, below).
  - *Assertions.* The control first: every frame's layout width is the
    stage's and under 390px, so the test is measuring the narrow frame
    and not the 390px one. Then, for every frame, its document's
    content height must not exceed its box's height (a frame holding a
    sidebar-shell page keeps the existing 48px). The number to write is
    the largest across the locales run, which the failure names. A
    height set too large shows up in the slack log, not as a failure.
- **The scheme follows a page back from the cache** (round 7) —
  browser leg at 390px, `day/en`, scripts on: open the index, tap the
  Form row, go Back to the index, press Dark, go Forward. Control first:
  the Forward really was a back/forward cache restore (the `pageshow`
  event the test records has `persisted` true, and
  `performance.getEntriesByType("navigation")` on Form is unchanged
  from the first visit); a leg that silently reloaded Form would pass
  on startup code alone. Then the root has `data-theme="dark"` and
  every loaded preview frame's root does too. The reverse journey, Dark
  to System, removes the attribute everywhere. If the engine declines
  to cache the page, the leg fails rather than passing on a reload,
  and names why from `notRestoredReasons` where the engine reports it;
  the tree handler must not send `Cache-Control: no-store`, which
  would block the cache.
- **Every language is reachable from the phone index** (round 6) — on
  the Overview at 390 and 320px, in `day/en` and `day/ar`, with anchor
  positioning available and with it disabled (the `@supports` block at
  `ui/tokens.css:1305-1323` forced off), scripts on and off; the index
  scrolled to its end first, the case that leaves no room below the
  foot. Open the language menu and, for each of the twelve links:
  Tab to it (scripts on) and require it to be `document.activeElement`
  with its rectangle inside the viewport; and, in every combination,
  scroll it into view the way a finger would (the panel's own scroll,
  then the document's) and require `elementFromPoint` at its centre to
  be the link. Which way the panel opens is not asserted: with anchor
  positioning it may flip upward, and without it, it opens downward and
  the document scrolls to reach it (2.3).
- **Mobile is the page at its real size** (2.9) — browser leg at 390px
  with a coarse pointer, on Form and Shells (both width classes), in
  `day/en` and `day/ar`, Mobile as Auto opens it (the Mobile tab lit,
  asserted first): every Mobile frame's transform is the identity
  and its layout width equals its rendered width; a text field inside
  it computes `font-size: 16px` and renders 16px (computed size × the
  frame's rendered width over its layout width); the box's inline
  edges are the document's client edges, so the frame bleeds; the
  document does not scroll sideways. The same at 320px, where the frame
  lays out at the stage's width, and at 390px with a fine pointer,
  where the frame's own width puts it in the touch query. Control: at
  1280px the Mobile frame is 390px wide and centred, as today
  (`browser_test.go:875-880`).
- **Right to left** (2.10) — browser leg at 390px in `day/ar` and
  `day/en`, on Form and Shells, with Desktop chosen through each
  widget's own Desktop radio (Shells has no Code tab, so no page-wide
  group, 2.6; Form takes the same route so one helper serves both, the
  `clickAll` the phone drive already uses, `browser_test.go:1876`).
  First, the control: every widget has its Desktop radio checked and
  every frame's layout width is its class's `--ds-wd` (900px on Form,
  1200px on Shells); a widget left on Auto or Mobile would never
  overflow, and the assertions after this would pass on nothing. Then:
  every box scrolls (`scrollWidth > clientWidth`); at the initial scroll position
  the frame's inline-start edge is the box's (its right edge in Arabic,
  its left in English, ±1px); scrolled to the far end, the frame's
  inline-end edge is the box's. Run against today's `gallery.css` this
  leg fails in Arabic (2.10's measurement), which is its control. At
  1280px in `day/ar` the rail lies to the right of `<main>` and the bar
  above `<main>`.
- **The gallery is the shipped shell** — Go, every page in every theme
  × locale (replaces the markup half of
  `TestTheSidebarIsTheShellTheGalleryDocuments`, below): the root is
  `<div rst-shell-sidebar="index">` on the Overview and `="page"` on
  every other page; no page anywhere in the tree writes
  `rst-shell-chrome`; every content page has exactly one
  `<div rst-shell-back>` immediately after the skip link, its link with
  `rel="up"` and the framework's label and name; only the Overview has
  the index title, the lead, the `.ds-index` nav and a non-empty
  `[rst-shell-rail-foot]`; every page links `shell.css` and loads
  `shell.js` with `defer blocking="render"`.
- **Every way up names its row** — Go: for every content page in every
  theme × locale, the back control's `href` is that theme × locale's
  `index.html` plus a fragment naming an `<a>` inside the `.ds-index`
  nav whose `href` is the content page itself. The rows are exactly the
  sections the tree lists, in order, with the tree's Demos links after
  them.
- **The two copies of the controls agree** — Go, on every Overview: the
  foot and the bar carry the same theme links, scheme buttons and
  locale links in the same order, the foot's links each ending in
  `#ds-prefs` and the bar's fragment-free.
- **No prerender** — Go: no file in the tree contains `speculationrules`
  or names `_speculation-rules`. The gallery is static files and asks
  for nothing it would have to be served with (2.3; Risks).
- **No two states of a partial render alike** (2.7).
- **Preview requests on Form** — the frame-loading helper below counts
  requests for preview files and logs them; a request for a missing
  file fails the leg.

### CI bound

The browser target runs its packages one at a time with a 20-minute
limit per package (`Makefile:207-220`); the design-system package
already takes 7 to 8 minutes on a quiet machine by that target's own
note, and the height drive alone has a 420-second deadline
(`browser_test.go:941`), about two minutes of it fixed sleeps (8s and
4s per page, `:1031`, `:1037`). This spec adds browser work to that
package, and the narrow height test is the heaviest of it, so the
bound is set here rather than discovered in CI:

- **Routine CI runs the locale subset** (above): `en`, `ar` and the
  worst-wrapping one or two, about a third of the twelve.
- **The full sweep is its own target**, `make browser-sweep`, not part
  of `make ci` and not in `.amadan/ci.d/`. It runs the narrow height
  test over all twelve locales (the test reads an opt-in environment
  variable the target sets, and runs the subset without it). It is run,
  and its result recorded on the branch, before any release that
  changes locale strings or preview content, and the comment above the
  target in the `Makefile` says so.
- **No fixed sleeps in the height drives.** Both height tests wait on
  the migrated `eagerly` helper's settled-frame condition (Migrated:
  expected URL, complete, populated, stable across 150ms), and after
  switching every widget to Mobile they wait until every box has its
  Mobile width and every frame's content height has held for 150ms,
  in place of the 8s and 4s sleeps. File-backed previews make this
  possible: each frame's document URL can be checked, which a `srcdoc`
  frame's `about:blank` start could not be (`flipevidence_test.go:253`).
- **Deadlines from measurement.** The narrow test's own context
  deadline, and the wide test's 420 seconds if its work changes, are
  set to twice their measured duration on a quiet runner, and the
  measurement is written in the comment beside each. The package's
  measured total must leave at least a third of its 20-minute limit
  free; if it does not, the narrow test moves to its own package
  rather than the limit rising.
- **Acceptance: the whole of `make ci` stays under 20 minutes on a
  quiet runner.** Measured, not estimated, and recorded in the
  implementation plan's last task with the per-package times.

### Migrated — the gates that read `srcdoc` or the old layout

A shared resolver, `frameDocs(files, page) []frameDoc`, returns every
`.ds-view__frame` on a page with the document it shows: read from the
tree by `src` (mount stripped), failing if the file is missing. Every
gate below switches to it without dropping its coverage assertion
("no preview documents found" stays a failure). Line numbers are the
merged tree's (2026-10-04).

| Gate | Where | Change |
|---|---|---|
| `srcdocs` / `srcdocAttr` helpers | `designsystem_test.go:539`–`:542` | replaced by `frameDocs`; `srcdocAttr` kept only for the no-`srcdoc` assertion |
| `TestEveryPageIsAWholeLocalisedDocument` | `:585`, `:594` | preview files are whole documents; checked via `frameDocs`, counted, and not double-counted by the all-files loop |
| `TestTreeShapeIsComplete` | `:760`, `:799` | `want` gains every frame `src`; the exact count stays exact |
| `TestEnhancedControlsAreOnTheComponentPages` | `:848` | reads `frameDocs` |
| `TestEveryExampleIsFramedDesktopMobileAndCode` | `:919`, `:973` | counts follow the markup already; the Code check goes by source kind: a partial sample with data has a call block and a Rendered HTML disclosure (grouped widgets one call per state and one disclosure); a raw sample, idiom, screen or format has one markup block and no call; an illustration has one block marked `data-ds-nocopy` |
| `TestSampleLinksAndFormsAreDeadInThePreviews` | `:1097`, `:1155` | reads `frameDocs`; the "Code keeps the real route" check reads the text of the source blocks (tags stripped, entities decoded), since highlighting splits `href=&#34;…` |
| `TestNoGalleryPageOpensAModalOverTheGallery` | `:731` | the `&lt;div rst-modal-overlay&gt;` check reads source-block text, for the same reason |
| `proseSentinels`, `proseFixtureCollisions`, sweep count | `:1368`, `:1413`, `:1470`, `:1493` | as 2.8 |
| `TestTheChromeCarriesTheThreeSwitchers` | `:1516`–`:1548` | the header is `.ds-top`, a child of `[rst-shell-sidebar]` between the rail and `<main>`; `<main>` opens with `<div rst-page>`. The link count reads `.ds-top__controls`, since the brand (2.1) is an in-tree link too. On the Overview the foot's copy is checked by "The two copies of the controls agree" |
| `TestEveryGalleryPageLinksTheStylesheet` | `:1632` | preview files carry no `ds-` class, so they need no `gallery.css`; the row documents are written to keep it that way |
| `TestTheSidebarIsTheShellTheGalleryDocuments` | `:2018`–`:2034` | its markup list wants `<div rst-shell-sidebar>` and `<details rst-shell-chrome>`, which this spec removes; that half moves to "The gallery is the shipped shell". The filter and rail-section checks after it stay |
| `TestNoPageCarriesTheSameIdTwice`, `uniqueIDs` | `:2771`, `:2805` | preview files are checked with `mustHaveOne=false` (a status pill has no id); pages keep `true`; ids are read with every source block removed first (the `escapedSource` cut, `:1394`), so whether highlighting happens to split `id="…"` cannot decide what counts as an element id |
| `header_rule_test.go` | `:48`, `:61`, `:146` | reads `frameDocs`; the Tokens group heading it finds is an `<h2>` now |
| `header_rule_browser_test.go` | `:71`–`:80` | uses the shared frame-loading helper below instead of its own wait: today an initial `about:blank` can pass its `readyState`/`body` check (`:71`), it returns before forcing eager loading (`:74`), and a timeout counts as done (`:80`). A frame that never loads fails the leg |
| `flipevidence_test.go` | `:253` | treats `about:blank` as not-yet-loaded for any frame with `src` or `srcdoc` |
| `TestA11yScansThePreviewDocuments` | `a11y_test.go:791`–`:803` | the settle check also requires `contentDocument.URL` to end with the frame's `src` |
| `TestA11yWalksTheKeyboard` | `a11y_test.go:1119` | seeks to the first focusable inside `[rst-page]` instead of `.ds-switch` |
| `TestA11yScansTheGallery` | `a11y_test.go:413` | gains the Code-selected leg (New) and the phone views through "The phone index and the way back" |
| `TestThePreviewWidgetIsUsableOnAPhone` | `browser_test.go:1777`, `:1799` | its 390 and 320px legs move from the Overview to Shells: on a phone the Overview is the index, its main has no box, and the wait for `.ds-view__box` (`:1833`) would time out. Mobile's expected width becomes `min(390px, stage)` in `opensOn` (`:1839`, `:1860`) and in `agree`'s Mobile case (`:1653`), and `boxes` reads the width off the frame's resolved inline size: Mobile's `--ds-w` is now the unregistered token stream `min(390px, 100cqw)`, which `getPropertyValue("--ds-w")` (`:1428`) returns as text. Desktop's pan check expects the box to span the viewport. The 1280px calibration on the Overview (`:1797`) is unchanged |
| `TestThePreviewDefaultIsMonotoneInStageWidth` | `browser_test.go:2068` | the page width class is swept on Shells, not the Overview, for the same reason; below 800px the stage is now the window less a scrollbar (the bleed, 2.9), so the comments' stage arithmetic changes while the assertion, monotone in stage width, does not |
| `eagerly` | `browser_test.go:1479`–`:1486` | the six-second sleep becomes a wait: every frame eager in one mutation, then each frame's document must have its expected URL, be complete and populated, and be stable across 150ms, within 30s; frames that never settle are named in the failure; the tree handler fails the leg on any request for a missing file. Used by `TestPreviewFrameHeightsFitTheirContent` (`:939`) |

### Retired

- `TestTheSectionTabsNameEveryPage` (`designsystem_test.go:2126`) —
  the strip is gone. Its job moves to the rail-uniqueness gate
  (`:2243`, unchanged) on desktop and to "The phone index and the way
  back" and "Every way up names its row" on a phone.
- The drawer-era legs this spec listed as new until 2026-10-04 (the
  phone Menu, the pinned Menu row's painting, the drawer foot's fit and
  dropup, the skip link withdrawn while the drawer is open) are dropped
  before they were written: the drawer they test does not exist.

### Unchanged

128 KiB per page with an empty debt table, axe WCAG 2.2 AA over pages
and frames in every theme and scheme, 320px reflow (now also over the
phone index, which the Overview is at that width), the keyboard walk,
frame-height measurements (re-measured where 2.7 changes a frame),
determinism, unique frame titles, every prose key translated.

## Risks

- **Call generation for bound and struct data** is the one place the
  renderer has to know how an app would really write the call. The
  contract test keeps it honest, and an unsupported value is a build
  error rather than a guess.
- **File count and deploy.** ~4,800 more files per build. The gallery
  is generated at the site's build, not committed, so the cost is build
  time and upload size; the implementation branch records the measured
  file count, tree bytes and Form's request count.
- **The byte model is a projection, and Form is the tight one.** It is
  built from the tree's own counts, but the highlighter, formatter and
  Buttons are estimated, and the additions were modelled on the
  2026-09-27 tree and scaled. Form projects to ~8.7 KB of headroom. The
  page gate is the check; if Form's real number lands within 5 KB of
  the cap, the first thing to cut is highlighting attribute values (a
  third of the tokens, ~5 KB on Form), not the cap.
- **A phone reader who switches language loses their place in a page.**
  Decision 7 keeps section pages to the way back and the content, so
  the switch happens on the index and the reader taps back into the
  section at its top. Accepted with the decision; desktop keeps the
  place.
- **The Overview's body is not on the phone.** The index hides main.
  The paragraph is repeated as the index's lead and the demo
  application is a Demos row (decision 9), but the Overview's
  route list with its one-line blurbs is not shown on a phone; the rows
  carry the titles only.
- **Focus is not returned after a filtered search.** Back from a page
  reached through the index's filter restores the index with the query
  still in the box, so the tree is shown and the rows are hidden;
  `shell.js` looks for the row, finds it hidden, and focuses nothing
  (`ui/shell.js:75-84`). The reader is back on the list they searched,
  at its scroll position when the page comes from the back/forward
  cache. Accepted rather than taught to `gallery.js`.
- **Every phone navigation slides**, switching theme on the index
  included: `shell.css` opts every page that links it into cross-page
  transitions below 800px, and `shell.js` types any link that is not
  Back as forward (`ui/shell.css:20-33`, `ui/shell.js:97-105`). A theme
  switch slides the same index in from the end. Cosmetic; under reduced
  motion nothing moves.
- **Two copies of the controls on the Overview** can drift when the
  next control is added to one. "The two copies of the controls agree"
  fails on that.
- **A phone fragment can land under Back until the framework's fix
  lands.** The gallery deliberately does not paper over it (decision
  10), so on a tree built before that `tokens.css` change, a target
  reached from the index's filter may sit under the back strip.
- **The routine gate sees four locales, not twelve, at 320px.** A
  label that wraps worse only in a locale outside the subset fails
  `make browser-sweep`, not `make ci`, which is why the sweep is owed
  before any release that changes locale strings or preview content
  (CI bound).
- **Copy cost.** 14 new keys × 11 translations, and 11 keys retired.
  The phone chrome adds none; C15 to C17 change separators, not
  keys.

## Operator decisions

Decisions 1 to 4 were accepted as recommended on 2026-09-27, 5 was
raised on 2026-10-03, 6 to 8 were made on 2026-10-04 after
mobile-ergonomics landed, and 9 to 11 rule that revision's open
questions. 2 is superseded by 6 and 7.

1. **~4,800 extra files on rastrillo.org** (914 → ~5,738; +9% bytes,
   re-measured 2026-10-04; it was ~4,400 on the 2026-09-27 tree). The
   alternative that avoids them gives up highlighting and formatting
   the Rendered HTML (1.5). Accepted.
2. **Superseded by 6 and 7.** It was: the phone's pinned row is the
   Menu drawer's summary alone, with the brand and the three controls at
   the drawer's foot. The framework has since retired the sidebar's
   drawer for an index page and a back control, and the gallery follows
   it, so there is no Menu row, no drawer and no drawer foot.
3. **Search synonyms are English only** (2.4).
4. **`button` joins `ui.Styleguide`** (2.5) — a new key in a framework
   API, rather than a gallery-only section that would break the
   primitives page's "exact sample" promise.
5. **On a phone, the Mobile rendering is the page at its real size**
   (raised 2026-10-03 while checking the mobile-ergonomics branch on a
   phone): the stage's real width, `--ds-k` = 1, full bleed; only
   Desktop shrinks or pans. Specified in 2.9.
6. **The gallery navigates on a phone the way the sidebar shell does.**
   The Overview is the index page (`view` = index) listing every
   section; every other page is a content page with the shell's back
   control ("Back to Sections", its way up the index row's fragment) and
   the shell's slide. No drawer, no Menu button for navigation (2.2,
   2.3).
7. **On a phone the theme, scheme and language controls live on the
   index**, under the section list; section pages show only the back
   control and their content. At 800px and up the pinned top bar with
   the brand and the controls stays as 2.3 designs it.
8. **Right to left, the preview frame is anchored at inline start**,
   and the rail is on the right through the shell's grid. Specified and
   tested in Arabic (2.10).
9. **The Overview's paragraph leads the phone index** (2.3), written a
   second time under the index title because the index hides main.
   About 900 bytes per Overview and some eight lines above the first
   row on a 390px phone, for the one statement of what the system is.
10. **Keeping fragments clear of the phone's back strip is the
    framework's**, fixed in `tokens.css` on its own branch. This spec
    neither sets nor depends on it; its own `scroll-padding` is for the
    desktop bar only (2.3).
11. **The em-dash separators in the gallery's titles go through copy
    review** with this spec's strings (C15, C16), drafted without one;
    the format is copy review's call.

## Open questions

None open. The questions raised on 2026-10-04 were ruled the same day
(decisions 9 to 11); Form's narrow projected headroom stays under
Risks.

## Review log

Round 1 (codex, Astra), 16 findings, verdict "not ready". Each was
checked against the code before being accepted.

1. *Blocker — byte-identical to the frame body is the wrong contract.*
   Accepted. The call is compared with `renderSample`'s output before
   wrap/deaden; the transforms have their own test (1.1, Tests).
2. *Serializer and placeholder contract incomplete.* Accepted. Value
   table covering scalars, maps, `[]any`, `[][2]string`, bound structs;
   `Bind` for placeholders with a `Dot` to execute against; build error
   for anything else (1.1).
3. *Blocker — static files break several gates.* Accepted. Shared
   `frameDocs` resolver; every affected gate listed with file:line
   (Tests, Migrated).
4. *File emission and cost need a contract.* Accepted. Which previews
   move, paths, collision rule, mount-prefixed URLs, measured file
   count and bytes, request cost; the keep-`srcdoc` alternative
   measured and rejected (1.5).
5. *Frame measurements need reliable loading.* Accepted. The sleep
   becomes a URL-checked wait with named failures and 404s failing the
   leg (Tests, `eagerly`).
6. *Formatting contradicts the line gate.* Accepted. No column promise;
   soft wrap; the gate becomes "no sideways scroll at 390px" (1.2).
7. *Axe misses the code UI.* Accepted. Code-selected, disclosures-open
   leg in every theme × scheme, plus a pair check (1.3, Tests).
8. *Page-wide view semantics undefined.* Accepted. Buttons with Auto,
   mixed state, re-apply, persistence, framed pages, no-script (2.6).
9. *Phone header conflicts with the shell DOM.* Accepted. Header moved
   out of main to sit between the rail and main; the `+` relationship
   is untouched; CSS for both widths; no duplicates (2.3, as corrected
   in round 2).
10. *Focus obscuring not covered.* Accepted. `scroll-padding` per band
    and a geometry leg for Tab, Shift+Tab, skip link and fragments
    (2.3, Tests).
11. *`location.hash` does not preserve position.* Accepted. The section
    under the bar is computed on click; the URL is not rewritten while
    scrolling (2.3).
12. *Translation and leak-gate changes larger than stated.* Accepted.
    Inventory, sentinels, fixture exemptions by page kind; English
    synonyms (2.8, 2.4).
13. *Budget feasibility unproven.* Accepted, and measured: it was
    unproven in a way that mattered — with `<span class>` highlighting
    Form would exceed the cap even after the move, which is why the
    highlight markup is custom elements; blurb search was cut for the
    same reason. Budget table and itemised `gallery.js` cap (Budgets).
14. *Clipboard failure unspecified.* Accepted. Feature detection,
    announce after resolve, select on failure, translated strings,
    distinct names (1.4).
15. *Grouped and illustrative samples need rules.* Accepted.
    `Illustration` flag; grouped structure; duplicates only when
    byte-identical, with a gate; Buttons cut to nine (1.4, 2.5, 2.7).
16. *Section-tab gate must be retired.* Accepted. Retired; phone Menu
    leg added; keyboard walk re-anchored (Tests).

Round 2 (codex, Astra), re-verdict: 7 of the 16 resolved, 9 partially;
11 new findings (17–27), one Blocker; verdict "not ready". A third
round was run because of the Blocker (below).

- *17, Blocker — the sticky header could not stick:* a grid item's
  containing block is its grid area, and the draft put the header in a
  one-row area. Confirmed against the CSS Grid and Position specs; the
  same bug had been spotted independently while round 2 ran. The header
  is now placed by grid line across every row with the first row
  reserved at `--ds-bar-h`, and the phone pins the shell's own Menu row,
  which lives in the page-high shell (2.3).
- *18 — opening the phone Menu destroyed the reader's position:* the
  drawer is now a fixed overlay with main `visibility: hidden`, so
  nothing moves, focus cannot go under it, and closing it restores the
  place (2.3).
- *19 — the header-rule sweep's own wait accepts `about:blank`:*
  confirmed at `header_rule_browser_test.go:71`–`:80`; it uses the
  shared loading helper and fails on timeout (Migrated).
- *20 — assertions contradicted legitimate sources:* token coverage and
  Code-panel shape are now asserted per source kind, and the wrapper
  check only looks at what `wrap` injects (Tests).
- *21 — the tested call is not necessarily the copied text:* new test
  that each block's text content is exactly the intended source and
  that the clipboard receives it; id gates read with source blocks cut
  out (Tests).
- *22 — `.ds-partial > :is(h3, h4)` would unstyle the shifted
  headings:* becomes a class (2.1).
- *23 — per-block copy names were not budgeted:* names are now built by
  the script from text on the page, costing no bytes per block; the
  `gallery.js` cap is 15 KiB with the itemisation (1.4, Budgets).
  (Revised after the plan's review weighed the real code: 22,832
  bytes, measured plus 10%; see Budgets.)
- *24 — the locale-menu Note change was not inventoried, and `Build`
  returns a map, not the slice:* both corrected (1.1, 2.8).
- *25 — focus geometry ignored the frame's scale, and two locales
  cannot bound a bar height across twelve:* coordinates go through the
  frame transform; a separate gate measures every theme × locale at
  each band edge (Tests).
- *26 — Auto and "every widget shows Desktop" could both be pressed:*
  pressed state is read off the radios only (2.6).
- *27 — synonym keys were not anchor ids:* keyed by the prefixed ids
  (2.4).

Round 3 (codex, Astra), run because round 2 had a Blocker: 24 of the
27 earlier findings resolved, 11, 18 and 25 partially (their remaining
gaps are 31, 33 and 30); six new findings, all Important, **no
Blocker**; the desktop layout confirmed sound. Addressed without a
fourth round:

- *28 — the language menu in the phone foot would open off-screen:*
  a phone-foot dropup with the anchor-positioning hand-back, and a test
  for all twelve languages in LTR/RTL with and without anchor
  positioning (2.3, Tests).
- *29 — a same-page rail link left its target behind the open drawer:*
  `gallery.js` closes the drawer first; without script the target is
  in place when Menu closes (2.3, Tests).
- *30 — a fixed-height bar cannot measure its own overflow:* border-box
  reservations, and the gate measures the children's bounds (2.3,
  Tests).
- *31 — the lookup threshold disagreed with where fragments land:* one
  reading line, `scroll-padding-block-start` + 1px, for both (2.3,
  Tests).
- *32 — the pinned phone row had no background or stacking order:*
  both specified; tested with `elementFromPoint` while scrolling (2.3,
  Tests).
- *33 — drawer scrolling could chain into the hidden page:* root
  `overflow: hidden` while open and `overscroll-behavior: contain` on
  the rail; tested by scrolling past the rail's end (2.3, Tests).

Round 4 (codex, Astra): all six round-3 findings resolved; three new,
all Important, no Blocker. Addressed:

- *34 — the position lookup lost explicit destinations near the page's
  end and on the Icons grid:* a fragment the reader has not scrolled
  away from wins; otherwise the first anchor on the lowest row at the
  reading line; tests for the clamped last partial, an Icons row, and
  the preference expiring on scroll (2.3, Tests).
- *35 — the skip link pointed into a hidden main while the drawer was
  open:* withdrawn with CSS while Menu is open, scripts on or off, and
  tested on both sides of closing Menu (2.3, Tests).
- *36 — grouped previews contradicted the per-sample body contract:*
  two contracts, single and grouped, each state checked once, in order,
  with its label (Tests).

Round 5 (codex, Astra): 35 and 36 resolved, 34 partially; two new,
both Important, no Blocker, so no further round. Addressed:

- *37 — the fragment record had no reliable start or renewal:* recorded
  one frame after the browser's fragment scroll, on load only after the
  stored page-wide view is applied, and renewed when the same fragment
  is followed again; it now records the target's position rather than
  `scrollY`, so a compensated layout change above it does not cancel
  it. Tests for both paths with the clamped last partial (2.3, Tests).
- *38 — mutating a switcher's `href` left stale fragments behind:* the
  canonical address is kept and restored after every activation and on
  `pageshow`; tested with Ctrl-click, middle-click and back/forward
  (2.3, Tests).

Revision, 2026-10-04, after mobile-ergonomics landed on main
(0506401) and main was merged into this branch. Not a review round: the
operator's decisions 6 to 8 and the framework it now sits on.

- *Navigation on a phone.* 2.2 and 2.3 rebuilt on the shipped sidebar
  layout's markup and rules: the Overview is the index, every other
  page a content page with the shell's back control, `shell.js` and
  `shell.css` linked. Deleted with the drawer: the Menu row, the fixed
  rail, the `html` overflow lock, `visibility: hidden` main, the
  withdrawn skip link, the drawer foot and its dropup, the same-page
  rail-link close and its ~250 bytes of `gallery.js`, and the four
  tests that existed only for them. Desktop's pinned bar is unchanged.
- *Findings 37 and 38*, which 88a764d0 answered in substance, are
  finished. 37: the stored view is applied at `DOMContentLoaded`; at
  `load` `gallery.js` puts the fragment's target in place itself before
  recording, only on a fresh navigation the reader has not touched, so
  a parse-time fragment scroll the stored view invalidated is never the
  record and a restored scroll is never mistaken for one; a reload
  test joins the two existing ones. 38: the restore is scheduled before
  the `href` is changed, the timer's reason is stated (a microtask
  would run before the activation reads `href`), the handler is scoped
  to the bar, and the test plain-clicks the restored link from the top.
  On the phone index the switchers carry a fixed fragment, so 38 cannot
  arise there.
- *Decision 5 specified* (2.9) and *decision 8* (2.10), each with a
  browser leg; the RTL crop was measured before it was specified.
- *Prerender*: none, and why (2.3).
- *Re-measured* on the merged tree: Form is now over the cap on a debt
  entry, the 1.5 table and the cost lines are re-taken, Form's
  projected headroom falls from ~19 KB to ~8.7 KB, and every line
  reference in the spec is checked against the merged code.

Round 6 (Astra, on 5f6fa6a7): 38 resolved, 37 partially; four
findings, three Important and one Minor, no Blocker. All four
addressed, with the operator's rulings on the revision's questions:

- *37, still partial — history and non-navigating clicks renewed the
  record.* A `hashchange` also follows Back and Forward between
  fragment entries, when the browser restores a scrolled position, not
  the target's; and a Ctrl-click or a cancelled click on the current
  fragment scrolled nothing yet renewed it. The record is now an id
  and a top, written only when this document puts a target in place:
  on an untouched fresh load, or after an unmodified, primary click
  whose `defaultPrevented` is still false a frame later. `hashchange`
  writes nothing, and `popstate` or a Navigation API traverse drops the
  record. Tests for Back and Forward after scrolling away, and for a
  Ctrl-click and a cancelled click (2.3, Tests).
- *No content-fit gate at 320px.* Mobile heights are now sized for the
  narrowest frame a 320px viewport gives, with no inner-scroll
  allowance for components, measured in every locale over every
  preview-bearing page including Display's grouped previews (2.9,
  Tests).
- *The phone language menu contradicted the framework's positioning.*
  The flip upward is allowed, the scriptless and no-anchor fallback is
  the document scrolling to a downward panel, and the test asserts only
  that every language is reachable, across anchor positioning on and
  off, scripts on and off, LTR and RTL, 390 and 320px (2.3, Tests).
- *The RTL leg asked Shells for a page-wide control it does not have.*
  Desktop is chosen through each widget's own radio, and every frame's
  Desktop layout width is asserted before the scroll endpoints (Tests).
- *Operator, 2026-10-04:* the Overview paragraph stays as the phone
  index's lead (decision 9); the back strip's missing `scroll-padding`
  is the framework's to fix, so the gallery's phone workaround and the
  tests that relied on it are removed (decision 10); the em-dash title
  separators become copy items C15 and C16, and the copy button's
  joiner, which this spec had drafted with one, is C17 (decision 11).

Round 7 (Astra, on ca09626f): 37, 38 and round 6's menu and RTL
findings resolved, round 6's content-fit finding partially; three new
findings, all Important, no Blocker. Addressed with the operator's
rulings:

- *The narrow height leg measured a preview the phone index hides.*
  Coverage is now defined per viewport: the Overview is exempt from the
  320px test by name and with its reason, the exemption list fails on
  an entry that does display a preview, and the demo's own pages stay
  covered at phone width by the reflow gate (Tests).
- *No CI runtime bound.* Routine CI runs the 320px test on `en`, `ar`
  and the worst-wrapping one or two locales, chosen by one measured
  full sweep and recorded with that reason; the full twelve are
  `make browser-sweep`, outside `make ci`, owed before a release that
  changes locale strings or preview content. The height drives lose
  their fixed sleeps, deadlines are set from measurement, and the whole
  `make ci` must measure under 20 minutes on a quiet runner, recorded
  in the plan's last task (Tests, CI bound).
- *A page restored from the back/forward cache kept an old scheme*, and
  on a phone it has no toggle to correct it. A persisted `pageshow`
  now resyncs the root, the buttons and loaded frames, tested through
  index, Form, Back, Dark, Forward with the cache restore asserted.
  Theme and language need nothing: they are addresses, not stored
  preferences, so a restored page is rightly in its own (2.3, Tests).
