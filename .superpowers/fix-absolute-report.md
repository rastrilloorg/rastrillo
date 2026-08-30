# design-system: absolute paths

Branch `design-system-absolute`, commit `e7779e4` on top of `b4ee9cc`.

## The bug

`rastrillo.org/design-system` rendered unstyled at the slash-less URL. The
CARLOS static edge serves a directory index at its slash-less address as a
200 with no redirect, so `/design-system` and `/design-system/` return the
same bytes at two different bases. Every href the renderer emitted was
relative (`tokens.css`, `shells/topbar.html`, `../../theme-ink.css`), so on
the slash-less visit — the one a person types — the stylesheets resolved
against `/` and the navigation pointed one directory too high.

## The fix

One constant in `internal/designsystem/designsystem.go`:

    const mountPath = "/design-system"

Every URL the renderer owns is derived from it. Two helpers carry the
derivation so no call site concatenates a path by hand:

- `indexHref(theme, locale)` → `/design-system/<theme>/<locale>/index.html`
- `shellHref(theme, locale, shell)` → `…/shells/<shell>.html`

Changed surfaces: the two stylesheet `<link>`s and three `<script src>`s
(`{{.Root}}x` → `{{.Mount}}/x`), the shell demo `<iframe src>` and its
"Open the … shell" button, the theme switcher, the language switcher, the
shell idioms' `DemoHref`, the shells' `asset` function, and the shells'
`Index` back-link.

The `Root`/`Self` depth prefixes are deleted. `renderIndex(theme, locale)`,
`renderShell(theme, locale, shell)`, `themeLinks(theme, locale)`,
`localeLinks(theme, locale)`, `shellViews(theme, locale)` and
`buildIdioms(tmpl, theme, locale)` all lost their prefix parameters.

Two behavioural consequences, both taken deliberately:

1. **The switchers' current entry now points at that page's canonical
   address**, not at the bare filename it used to self-link. It could not
   keep self-linking: `href="index.html"` is exactly the relative form that
   breaks. From the tree root that address is `ink/en/index.html`, a
   different file holding the same bytes.
2. **The root `index.html` is byte-identical to `ink/en/index.html`.** With
   no prefixes left there is nothing to rewrite, so `Render` copies the
   nested page rather than calling the renderer a second time with
   arguments that could only produce the same output. Verified on disk with
   `diff`.

The trade is that the tree only works at this one mount path. That is the
path the site serves it from, and the constant is the whole of the binding.

## Gates

`TestEveryPageIsAWholeLocalisedDocument` — inverted, not dropped. It used
to forbid a `/`-rooted stylesheet or script. It now requires, per page:

- every `<link href>`, `<script src>`, `<iframe src>` to start
  `/design-system/` **and** to name a key in `Render()`'s own file map;
- every `<a href>` that starts `/design-system/` to resolve the same way.

An `<a href>` that does *not* start with the mount prefix is passed over:
that is the scope rule that exempts the sample content (`/orders/AB3PX`,
`/posts?page=2`, `#main`) without marking anything up. The escaped shell
source in the `<pre>` blocks reaches the page as `&lt;link …` and is
invisible to the regexes.

The resolution half is new coverage the relative scheme never had — it is
the cross-page link check PR 5 deferred, arriving early and read off the
renderer's file map rather than a crawl.

Both halves were mutation-tested rather than assumed:

- pointing `indexHref` at a non-existent `nope.html` → 1,635 "names nothing
  in the tree" failures;
- reverting one stylesheet link to `href="tokens.css"` → "is not an
  absolute path under /design-system/" on every page.

`TestRootIndexIsInkEnglishAtTheTreeRoot` — was "blank every href/src and
diff the rest", which was the weaker assertion the two depths forced. Now
asserts byte-identity outright, plus a tree-wide sweep that no rendered
page carries a `="../` anywhere.

`TestTreeShapeIsComplete`'s per-theme stylesheet assertion updated from
`href="../../theme-<theme>.css"` to the absolute form.

`TestDesignSystemIsCurrent` — unchanged, and satisfied: the commit carries
the tree from `go generate ./...` (152 files, 4,330,685 bytes).

## Docs

- `docs/site/templates.md`, design-system section: a new paragraph saying
  the tree's links are absolute under `/design-system/`, that it is
  therefore served from that path and no other, and why relative looked
  more portable and was wrong.
- `docs/superpowers/specs/2026-08-28-design-system-design.md` §5.2: the
  as-built sentence claiming the root index is independently generated from
  a different pair of path prefixes is struck through and replaced, dated
  2026-08-29, along with the any-mount-path relativity it implied. The
  gates paragraph's "no absolute asset paths" clause is rewritten to
  describe the inverted gate and the sample-href exemption.

## Verification

    GOFLAGS=-mod=mod go test . ./ui/ ./internal/... ./cmd/... -count=1

All 10 packages pass, run again after the commit with a clean tree.
`gofmt -l` clean, `go vet` clean.

Spot-reads: `docs/design-system/ink/en/index.html` and
`docs/design-system/warm/ar/shells/sidebar.html` carry only
`/design-system/…` chrome links plus `#` fragments and sample routes.
A grep of the whole tree for `href="../`, `src="../`, `href="tokens.css"`,
`href="index.html"`, `href="shells/…"`, `href="theme-…"` and bare `.js`
srcs returns nothing.

## Concerns

1. **The website's sync step is untouched and unverified from here.**
   `rastrillo-website`'s `hack/sync-docs.mjs` copies this tree to
   `/design-system/`. If it is ever mounted anywhere else — a preview
   deploy under a path prefix, say — every page in the tree breaks
   silently, where before it merely broke at slash-less URLs. `mountPath`
   is the single place to change, but nothing outside this repo asserts
   the two agree. A gate on the website side that checks one page's
   stylesheet href against its own serve path would close it.
2. **The language switcher on a shell demo still returns to the index
   page**, not to the same shell in the new locale. That is pre-existing
   behaviour, preserved deliberately; the old `selfHref` parameter that
   encoded it is gone, and the reasoning now lives in `localeLinks`' doc
   comment.
3. **The renderer-owned/content split is enforced by the mount prefix, not
   by a marker.** A future sample whose fixture href happened to start
   `/design-system/` would be held to resolve. That seems like the right
   failure — such a sample would be lying about what the site serves — but
   it is a rule worth knowing about before writing one.
