# Fix: the modal idiom opened over the gallery

Branch `modal-demo-page`, commit `2842cb9`, on top of `9d92149`.

## The bug

`ui.Styleguide()["modal"]` was rendered as live markup inside every
index page's class-idiom section. `.rst-modal-overlay` is
`position: fixed; inset: 0; z-index: 10` (ui/tokens.css:760) and
`body:has(.rst-backdrop) { overflow: hidden }` (:759), so the sample did
not sit in the gallery's flow at all: rastrillo.org/design-system loaded
with a full-viewport modal over the page, the content behind it
unscrollable, and the panel's Close link pointing at the sample's own
`/settings` — a 404 on a static tree. All 37 index pages (36 plus the
tree root) carried it.

Same shape as the shell samples' bug (styling escapes the gallery
flow); those were fixed in c623745 by showing the sample as escaped
source beside a link to the full-page demo.

## The fix

The modal's own doctrine supplies the cure — "a modal is its own URL,
closing is a plain link back" — so it gets an URL.

**internal/designsystem/page.go**

- `shellIdioms map[string]string` becomes `sourceIdioms
  map[string]sourceIdiom`: three entries (shell-topbar, shell-sidebar,
  modal), each carrying its own `Why` (the reason it is not rendered
  inline), `Label` (the link's own words) and `Href` (a
  `func(theme, locale) string`). The old shape hard-coded a
  shell-shaped sentence in the template and could not say anything
  different about the modal.
- `idiomView`'s `Demo string` becomes `Why` and `DemoLabel`; the
  template's note line is now
  `{{.Why}} <a href="{{.DemoHref}}">{{.DemoLabel}}</a>.`
- `modalHref(theme, locale)` — `<mount>/<theme>/<locale>/modal.html`.
- `renderModal` + `modalTemplate`: a hand-written document (not a
  shell — the idiom is body-level and no shell has a block outside its
  own `<main>`), structured exactly like the styleguide sample so a
  reader copying the source sees the same thing live: inert backdrop
  holding a modest Settings screen (the `page-header` partial plus a
  `rst-box`), then overlay and panel. It loads tokens.css and the
  page's theme stylesheet; no scripts, because there is nothing on it
  to enhance — which is itself the demonstration.

Three deliberate deviations from the sample, all documented at
`modalTemplate`: the backdrop holds a real screen rather than a bare
`<h1>`; the panel's nav tabs self-link `modal.html` (Profile
`aria-current="page"`) so switching tabs keeps the modal open instead
of landing on `/settings/billing`; and Close returns to that page's own
`index.html`, since the backdrop's screen has no URL of its own here.
The panel says so in prose rather than leaving a reader to wonder.

**internal/designsystem/designsystem.go** — `Render` writes
`<theme>/<locale>/modal.html` beside each index page. Doc comments
updated: 144 → 180 documents, the tree listing gained a row,
`partialTree` now runs 72 times per generate rather than 36.

**Gates (internal/designsystem/designsystem_test.go)**

- `TestNoIndexPageOpensAModalOverTheGallery` (new): no page whose name
  ends `index.html` may contain `class="rst-modal-overlay"` or
  `class="rst-backdrop"`; every one must contain
  `class=&#34;rst-modal-overlay&#34;` and `href="<its modal demo>"`.
  The plain string match is the right instrument precisely because
  escaped source cannot trip it — `html/template` writes the sample's
  quotes as `&#34;` inside the `<code>` element, so the unescaped form
  occurs only where a browser would actually lay the overlay out. The
  positive half matters more in a year: a gate that only forbade the
  live markup would pass just as happily on a page that had dropped the
  modal idiom altogether. Helper `themeLocaleOfPath` reads theme/locale
  out of a page path (root index is ink/en by definition, as in
  `localeOfPath`).
- `TestTreeShapeIsComplete` expects `<theme>/<locale>/modal.html`; the
  exact-count assertion carries 152 → 188 with no further edit.
- The absolute-link gate (`TestEveryPageIsAWholeLocalisedDocument`,
  PR #108) covers the new pages automatically — it collects every
  `.html` name in the render map — and they pass: every href on a demo
  page is absolute under `/design-system/` and names a file the tree
  renders. Confirmed by running the gate, not by reading alone.
- Size gate unaffected: 4,427,216 bytes (4.22 MiB) against the 20 MB
  ceiling.

**Docs**

- `docs/site/templates.md` § The design system: the tree now has a
  modal demo per theme × locale, and a new paragraph explains the three
  escaped-source samples and why the modal is the sharp case.
- `docs/superpowers/specs/2026-08-28-design-system-design.md` §5: a
  bullet for the modal route page, the escaped-source as-built sentence
  widened from two idioms to three with the modal's reason spelled out,
  the tree diagram gained `modal.html`, the file count 152 → 188 and
  the byte count refreshed, and the new gate recorded in §5.2.

The tree was regenerated with `go generate ./...`; the commit carries
it (36 added files, 42 modified — the 37 index pages plus the 5 source
and doc files).

## Verification

- `GOFLAGS=-mod=mod go test . ./ui/ ./internal/... ./cmd/... -count=1`
  — all green.
- `go vet ./internal/designsystem/...` — clean.
- The new gate was proved to bite: temporarily removing `modal` from
  `sourceIdioms` and running it alone failed on all 37 index pages with
  all four assertions, then the file was restored.
- Read `docs/design-system/ink/en/index.html`: no live overlay; the
  modal idiom shows the escaped sample in `<pre class="ds-src">` and
  links `/design-system/ink/en/modal.html`.
- Read `docs/design-system/ink/en/modal.html`: overlay and panel
  present, Close `href="/design-system/ink/en/index.html"`, tabs
  self-link, backdrop `inert`.
- Read `docs/design-system/warm/ar/modal.html`: `lang="ar" dir="rtl"`,
  `theme-warm.css`, links pointing at `warm/ar`.

## Concerns

- The demo's Close link goes to the gallery index, not to a Settings
  page, because there is no such page in this tree. The panel's prose
  says so, but a reader skimming the markup sees a Close that returns
  somewhere other than the screen in the backdrop. The alternative —
  a second page per theme × locale to close onto — would be 36 more
  files for a href.
- The demo carries no `rst-skip` link. A skip link into content that is
  `inert` is a dead control, and the only reachable content is the
  panel. Every other page in the tree has one, so this is a deliberate
  odd-one-out.
- Nothing in the tree renders the modal panel inside a landmark: the
  only `<main>` on the demo page is inside the inert backdrop, which is
  what the styleguide sample's structure gives you. That is worth a
  look when the modal idiom is next revisited — but changing it here
  would make the demo stop matching the source it demonstrates, which
  is the whole point of the page.
- Docs prose in `templates.md` was written without the `copy-review`
  gate: this ran as a non-interactive fix task with no reviewer to
  serve the copy to. Worth a pass before release if that gate is meant
  to be unconditional.
