# Rastrillo components (the gallery after Fiesta)

Status: design, 2026-11-18; approved by Paul the same day. Hashed asset names from earlier deploys are not preserved (they never were across syncs). Branch `components` (rastrillo). Follows
Fiesta part 2 (main 6b19df5, released in v0.34.0), and the original
Fiesta design's decision that "rastrillo.org's gallery shrinks to a
reference for the Go calls (which partial, which arguments, what it
renders), linking to Fiesta's design system for the visuals"
(`docs/superpowers/specs/2026-10-27-fiesta-design.md` on branch
`fiesta-design`).

## Why

Rastrillo's look now comes from Fiesta (fiesta.rastrillo.org), which
has its own design system at fiesta.rastrillo.org/design-system/. The
gallery at rastrillo.org/design-system/ still presents itself as "the
Rastrillo design system": it documents tokens, stylesheets and
hand-written markup shapes that Fiesta owns, and its title says so in
twelve languages. Those are the last references to Rastrillo owning
its look.

What people go there for (Paul, 2026-11-18): "just what the design
system produces". Seeing the result matters, so the gallery keeps its
live previews. What changes is what it is about: the Go side.

## Goals

- **One question answered:** "what do I write in my Go template, and
  what will I get?" Each component shows its Go call (partial and
  arguments, `{{icon}}`, a format function) beside the live result.
- **Named for what it is: "Rastrillo components"** (Paul's name),
  everywhere the gallery names itself: page titles and document
  titles, the top bar's brand, the Overview's heading and lead, the
  Getting started blurb, and their translations.
- **Fiesta owns the look, and the gallery says so.** What Fiesta
  documents leaves the gallery, each replaced by a link to the
  matching Fiesta page.
- **The address matches the name: `rastrillo.org/components/`**
  (Paul asked whether it should move; my call, which he left to me).
  A URL saying `design-system` would keep claiming Rastrillo has one,
  beside Fiesta's real `/design-system/`.
- **Nothing breaks for people with old links:** every
  `/design-system/...` address answers with a one-hop 301 to its
  `/components/...` equivalent (or, for a page that left, to its
  Fiesta page), through `_redirects`, which rastrillo.org's CARLOS
  static hosting honours (Fiesta's site uses it for its own moved
  addresses).

## What stays (it is Rastrillo's)

- **Component families** (the partials, idioms and forms; the pages
  `componentPages()` makes from `families()`): Go call and arguments,
  with Desktop, Mobile and HTML tabs. The HTML tab stays because it
  is how a reader checks what the call writes.
- **Icons:** the `{{icon}}` call and its slugs.
- **Dates, numbers and names** (my call): the page keeps its
  examples and its guidance (which formats people mix up, names,
  addresses), since that advice is about localising an app. Each
  section gains the Go call beside its example where Rastrillo has a
  helper for it (the `date` helper and the number and money functions
  from v0.37.0's localised formats), and prose that says Rastrillo
  cannot do something it now does is corrected. A section with no
  helper stays as guidance. Its samples keep their structure; where a
  helper now produces a value, the sample shows the helper's output
  for the page's locale.
- **Shells:** the frames `rastrillo new` scaffolds.
- **Screens:** starter templates built from partials.
- **Getting started's Rastrillo half:** how an app gets and keeps the
  files (`rastrillo new`, `doctor`, upgrading), without the per-file
  stylesheet and script documentation.
- **The twelve languages:** the partials' words come from Rastrillo's
  catalogs, so locale is a Go-side feature.
- **The theme switch:** an app picks its theme in Go configuration,
  so every page can still be seen in day, plain and signal, light and
  dark.

## What leaves (Fiesta's), each for a link

- **Tokens** (`tokens.html`): every custom property. Its page goes; a
  link to Fiesta's tokens page takes its place in the rail and the
  Overview.
- **Getting started's stylesheet and script details:** the per-file
  descriptions, weights and download rows. Replaced by one link to
  Fiesta's getting started page. The files themselves are still
  served under the gallery's mount (the previews and the website
  homepage need them; see Constraints).
- **UI primitives** (`primitives.html`): the hand-written shapes with
  no Go call (cards, data grids, menus, the shells' chrome). Linked to
  Fiesta's matching page. If a primitive turns out to have a Go call
  (a partial or func that draws it), it moves to the family that call
  belongs to instead of leaving.
- **"Design system" as the gallery's name**, wherever the gallery's
  own prose uses it to mean itself.

## Constraints

- No visible change to what any component renders. The previews,
  their markup and their scripts are untouched; only the gallery's
  chrome, page set and prose change. The one exception is Formats' helper-driven
  values (above).
- The gallery's default mount (`designsystem.DefaultMount`, `dsgen`'s
  documented `-mount`) becomes `/components`. dsgen also writes the
  redirect rules for the old mount (a `_redirects` fragment the
  website places at its root), covering every page, every theme and
  locale, the `/pt/` compatibility stubs, and the unhashed aliases
  (`tokens.css`, `theme-<name>.css`, `shell.css`) that the
  rastrillo-website homepage loads today. Until the website syncs, it
  keeps passing `-mount /design-system` and nothing changes there.
- Old page addresses (`tokens.html`, `primitives.html` under every
  theme and locale) do not 404: they redirect in one hop to the
  matching Fiesta page, using the gallery's existing `_redirects`
  mechanism (the one Fiesta part 4 used for its moved addresses, if
  rastrillo-website's hosting reads it; otherwise a small stub page
  with a link and a meta refresh). Which one is the builder's call
  after checking what rastrillo.org's CARLOS static hosting honours.
- Links to Fiesta go to the pinned Fiesta major's address
  (`https://fiesta.rastrillo.org/design-system/...`), matching theme
  and locale where Fiesta's page has them.
- The gallery's tests that cover what leaves go with it; tests that
  hold what stays (sweep, budgets, a11y, sliced pages, history) keep
  passing. Page weight budgets are not raised.
- Copy goes through Luna; translations follow the approved English.
  Prose has no em dashes as separators.
- References elsewhere follow the name: docs/site (`templates.md`'s
  "The design system" section, `nav.json`'s "Your app's design
  system"), `skill/design.md`, `skill/screens.md`, README, PRODUCT.md,
  DESIGN.md where they mean the gallery. Statements about Fiesta's
  design system stay as they are.

## Non-goals

- Changing any component, partial or the look.
- The rastrillo-website change: the next release's `Sync:` PR on the
  website repo switches `hack/gen-design-system.mjs` to `-mount
  /components`, includes dsgen's redirect fragment in the site's
  root `_redirects`, points the homepage's stylesheet links and any
  top-bar label at the new address and name, and is checked live
  from the flagship (old addresses 301 once, new ones 200). The
  CHANGELOG's upgrade notes say so; this branch makes it possible
  and tests the fragment, but does not edit the website.
- Fixing Fiesta's stale `ui/calendar.js` comment (fiesta #41) or the
  flaky bar test (rastrillo #233).

## Anti-goals

- A gallery that no longer shows what a call produces.
- A dead link: every page that leaves goes somewhere useful, and the
  rail never points to a page that is gone.
- A second description of the look drifting from Fiesta's.

## Left to the builder

- The exact Fiesta page for each link, read off Fiesta's gallery at
  the pinned version.
- Redirect file versus stub pages (Constraints).
- How the Overview routes to Fiesta: a row among its routes, or a
  short paragraph under the lead.
- Whether Getting started keeps its name or becomes "Using the
  components".
