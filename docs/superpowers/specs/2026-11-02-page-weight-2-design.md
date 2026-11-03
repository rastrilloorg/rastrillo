# A page's stylesheet holds only what it can show (#158, iteration 2)

Status: design, 2026-11-02. Branch `page-weight-2`. Follows iteration 1
(`docs/superpowers/specs/2026-10-31-page-weight-design.md`, on main as
9a72d55d). Discussion 211 on rastrillo/rastrillo holds Paul's answers and
the first measurement.

## Why

After iteration 1, a Rastrillo page still sends the whole of the app's
`tokens.css` (about 20 kB gzipped) and five always-on scripts, whatever
the page shows. The agent benchmark's "spec" arm, built with no framework,
sends 5–7 kB pages: about 2.5 kB of CSS and under 1 kB of JS, with the
same security headers, axe 0 and Lighthouse 100.

Paul's answers (discussion 211):

1. **Keep the look.** Depth and light are part of Rastrillo's identity.
   Accept only visual changes a person would not notice.
2. Most of the saving comes from **loading per component**: the
   stylesheet cut to the components a page uses, and scripts only where
   a page uses them.
3. A page may go up to **about 15 kB** of framework CSS and JS rather
   than flatten the look. The aim is to get close to the spec arm while
   looking finished, not to match it.
4. **Slice the app's own `tokens.css` automatically as each page is
   served.** No new files in the app, an app's edits still apply, and
   agents see nothing new.

## What we measured

On the benchmark's nine Rastrillo apps (`~/.cache/agent-bench/accept-1`,
groomer, lending and noticeboard, three runs each), using a throwaway
slicer over each app's own minified `tokens.css` and main's partials.
Sizes are gzipped.

| | `tokens.css` | + `theme.css` | + scripts | total |
|---|---|---|---|---|
| today, whole file | 19.9 kB | 1.7 kB | 6.0 kB | 27.6 kB |
| one slice for the whole app | 9.7–11.5 kB | 1.7 kB | 6.0 kB | 17.4–19.3 kB |
| a slice per page, home page | 4.9–7.6 kB | 1.7 kB | 6.0 kB | 12.6–15.3 kB |
| a slice per page, largest page | up to 8.9 kB | 1.7 kB | 6.0 kB + | about 16.6 kB |

So a slice per page. A whole-app slice misses the 15 kB ceiling on every
app; Paul agreed beforehand that it would be per page if so.

10 kB is not reachable with the look kept. On groomer's home page the
topbar shell is 28% of the slice, buttons (mostly the depth styles) 21%
and the language and scheme menus 16%: the layout's chrome and the look
Paul asked to keep.

Two findings shape the design:

- **Count only the hooks a script creates.** `rastrillo.js` names many
  hooks it looks for (`rst-switch`, `rst-shell-console`, `rst-select`)
  and creates few. Counting every name in a script nearly doubled the
  slice (5.2 kB to 9.0 kB on one page).
- **The layout's menus are forms.** The language and scheme menus are
  one-field POST forms, so "load `busy.js` with forms" would load it on
  every page.

## Goal

A page's framework CSS and JS, as served (minified, gzipped), stays
under 15 kB for the benchmark's home pages and for every page of a fresh
scaffold, with no visible change to any page.

## Non-goals

- Changing how anything looks or behaves. Every browser test passes
  unchanged against the served files.
- Changing the source files. `tokens.css` stays one commented file in
  the repo and in each app's `static/`; agents and people read it.
- A build step, a new file in the app, or a new concept an agent has to
  learn.
- Slicing `theme.css` (1.7 kB). Left whole for now (my call).
- Fiesta. Sites that load Fiesta from fiesta.rastrillo.org still get the
  whole stylesheet; that is Fiesta's question, later.

## Changes

### 1. The hooks a page can show

`ui.Pages` already walks each page's templates at boot to find the
component scripts it needs (`pageScripts`, through `tmpl.Inspect`, which
follows every `{{template}}` call). The same walk collects every `rst-*`
hook the page can emit: attribute names and class names in the markup,
and `rst-*` names in string arguments (a partial's `dict` arguments),
in every branch, taken or not.

To that set it adds the hooks each script the page loads **creates**:
a per-script list, kept in one place beside the scripts (for example in
`internal/vendored`), and held to the scripts by a test that reads each
script for the hooks it sets (`setAttribute`, `toggleAttribute`,
`classList.add`/`toggle`, the element-building helpers, markup in
strings). Today's lists, from the measurement:

- `rastrillo.js`: `rst-dropdown`, `rst-menu-group`, `rst-row-menu`,
  `rst-shell-menu`, `rst-drag`, `rst-settle`
- `anchor.js`: `rst-flip-menus`
- `busy.js`, `live.js`: `rst-spin`
- `select.js`: `rst-combo` and its parts, `rst-enhanced`, `rst-input`,
  `rst-select-group`, `rst-sr-only`
- `datetime.js`: `rst-dtp` and its parts, `rst-input`, `rst-sr-only`
- `calendar.js`: `rst-cal` and its parts
- `shell.js`: `rst-shell-sidebar`
- `light.js`: none (it sets custom properties only)

The builder confirms each list by reading the scripts; the test is what
keeps them true.

### 2. Slice the stylesheet by those hooks

A rule is kept when at least one of its comma-separated selectors needs
only hooks the page can show. Rules with no `rst-*` hook (`:root`,
`html`, `body`), `@property`, `@keyframes` and `@font-face` are always
kept; `@media`, `@supports`, `@layer`, `@container` and `@scope` keep
their kept contents. A selector's hooks are its `rst-*` attribute and
class names; attribute values do not count, so `[rst-btn=primary]` needs
`rst-btn`. A hook inside `:not()` is never needed (the rule applies
where it is absent), and an `:is()`, `:where()` or `:has()` list needs
only one of its branches.

The slicer reads the minified file, so it never sees comments. It must
be a real CSS reader (strings, escapes, nested parentheses in `:is()`
and `:has()`), not text matching; the measurement's Python slicer is a
starting point, not the design.

**Fail whole, never empty.** If the app's `tokens.css` cannot be read
(an edit that does not parse, or a construct the slicer does not
understand), every page gets the whole file, as today.

### 3. Serve each distinct slice once

Pages with the same hook set share one slice. Each slice is minified
and gzipped like iteration 1's files, fingerprinted by its bytes, and
served immutable from `Assets`. The layout links it in place of
`static/tokens.css`.

**`ui.Pages` gets the app's `Assets`** through a new `PagesConfig`
field (my call). It needs them to read `static/tokens.css`, to register
the slices, and to know which vendored scripts the app still has: an
app may delete `light.js` or `anchor.js`, and a page must not link a
file that is gone. Without the field, pages keep the whole file.

Left to the builder: how `Assets` holds derived entries and how the
layout asks for the page's slice. The shape iteration 1 used for scripts
is the obvious one: `ui.Pages` binds a per-page function at boot (as it
binds `neededScripts`), and the layout calls it. It must work under
compiled templates (`rastrillo generate`), whose generated code checks
each template function's type. A render outside `ui.Pages` gets the
whole file, as `neededScripts` gives every script.

New apps get both from the scaffold. Existing apps make two one-line
edits to get slices: the `PagesConfig` field in `render.go` and the
stylesheet line in the app-owned layout. The changelog gives both. An
app that makes neither keeps working and keeps the whole file.

### 3a. A hook the templates do not show

The slice comes from the templates, so a hook that is not in them is
invisible: HTML built in an app's Go code (`template.HTML` from a
handler or a helper function), or markup an app's own script builds.
On the whole file it renders fine; sliced, it would render unstyled
with no error. The framework's own functions that emit hooks (such as
`stageArt`) are listed with the scripts' hooks in change 1.

Paul's answer: **catch it**. The app's perf test (`perftest.Screens`,
which every scaffold runs in `make perf`) renders each page and fails
when a hook in the rendered HTML has no rules in that page's slice,
naming the page, the hook and the fix: ask for it in the template, the
way `{{needs "select.js"}}` asks for a script (for example
`{{needs "rst-card"}}`). An agent sees nothing new until it happens.
`needs` and SKILL.md's one line about it grow to cover hooks.

### 4. Live and polling pages get the whole app's slice

A fragment swapped into a live or polling page is rendered from the
app's own templates, so the union of every page's hooks covers it.
Those pages link the whole-app slice instead of the whole file. For
scripts they keep iteration 1's rule (every component script).

### 5. Scripts only where they are used

| script | loads on a page that |
|---|---|
| `rastrillo.js` | always (fragments, menu dismiss, the switch) |
| `anchor.js` | has a menu (`rst-dropdown` or `rst-menus`); every shipped layout does |
| `busy.js` | has a form other than the layout's language and scheme menus |
| `live.js` | is live by iteration 1's rule: a region other than the notice, or `data-poll` (my call: the live meta comes from page data the walk cannot read) |
| `light.js` | has a hook light.js lights: `rst-btn`, `rst-box`, `rst-card`, `rst-stats`, `rst-list` |
| `select.js`, `datetime.js`, `calendar.js` | as iteration 1 |

How the builder tells the menus' forms from the page's own is its call
(the walk knows which template a node is in). A page with a hand-written
form that the walk cannot see, such as one built from data, can still
ask with `{{needs "busy.js"}}`; `needs` grows to accept these scripts.

### 6. The budget

`perftest.FirstLoad` moves from 40 kB to **15 kB** of framework CSS and
JS for a page, plus each component script it loads (select, date,
calendar), measured as served.

A page that still links the whole `tokens.css` (an app that has not
made the two edits, or the fail-whole fallback) keeps iteration 1's
40 kB, so upgrading turns no app's CI red (my call). Its report says
the page is unsliced and names the two edits, or that the app's
`tokens.css` could not be read. A sliced page that is genuinely over
15 kB can be recorded in `.rastrillo/budgets.txt` with a reason, like
any other budget. The sidebar, console and stage shells,
which iteration 1 measured separately, get what they measure after this
change plus 10% headroom, but no more than 15 kB plus their component
scripts; if one cannot fit, the builder comes back with the number
rather than raising it.

## How we know

- Each change lands with its own tests, red before and green after.
- The gallery's browser drive, run against sliced stylesheets as
  iteration 1 ran it against minified ones, shows no visible change:
  every component state, three themes, both schemes.
- A test that renders every shipped layout's pages and checks that each
  hook in the rendered HTML has its rules in the page's slice (the
  inverse check: nothing shown is unstyled).
- Then the benchmark: when the branch is built, the benchmark session
  runs it against the spec arm on all three briefs before it lands. We
  do not run the benchmark ourselves; it spends money.

## Left to the builder

- the slicer's code and where it lives (`internal/vendored` is the
  obvious place, beside the minifier)
- how `Assets` stores and serves slices, and the layout's function name
- whether slices are built at boot or on first request
- how the menus' forms are told apart for `busy.js`
