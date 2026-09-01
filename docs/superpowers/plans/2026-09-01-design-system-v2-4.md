# Design system: semantic elements, dashboards and sign-in

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development.
> Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** finish §6-v2.4 (semantic elements, common data formats,
dashboard stats) and §6-v2.10 (sign-in screens), on top of the preview
width fix (§6-v2.11) and the stat band (§6-v2.12), which have landed.

**Spec:** `docs/superpowers/specs/2026-08-28-design-system-design.md`,
§6-v2.4, §6-v2.10, §6-v2.11 and §6-v2.12.

## What has landed

- **§6-v2.11, the preview width classes.** Components lay out at a
  virtual 900px and render unscaled; whole-page examples keep 1200px.
  This was Paul's "teeny tiny" report.
- **§6-v2.12, the stat band.** `stat` partial + `rst-stats` idiom, the
  lead cell, the delta rules, two new contrast pairs, the demo
  dashboard rebuilt on it.

## Global constraints

- **Every user-facing string is a `prose.go` key in twelve locales**,
  and **the English IS the key** — so an edit after the fact costs
  eleven redrafted translations. Copy review comes BEFORE the
  translations are drafted, not after. This is the binding constraint on
  every task below and the reason Task 0 exists.
- Gates: `go vet ./...`, `gofmt -l .` empty, `GOFLAGS=-mod=mod go test
  ./...`, and `go test -tags browser -p 1 ./internal/designsystem/
  ./ui/ ./harness/`. **Plain `go test ./...` fails in `cmd/rastrillo`,
  `internal/generate` and `internal/manifest` with "missing go.sum
  entry" — that is the sandbox, not a defect. Use `-mod=mod`.**
- Every tokens.css rule is written in **both spellings**, paired;
  `internal/markup` owns the grammar and two gates enforce the pairing.
- New colour pairs go in `ui/contrast_test.go`. 4.5:1 for text, 3:1 for
  control borders, three themes × two schemes.
- WCAG 2.2 AA: the axe scan covers every page kind. Zero violations,
  zero exemptions.
- `SKILL.md` ≤ 18,000 bytes (16,471 now).
- Never hardcode the theme, locale, shell or icon lists.

---

### Task 0: copy review of what has already landed — DO THIS FIRST

**Files:** `internal/designsystem/samples.go`, `page.go`, `prose.go`.

Thirteen English strings shipped in §6-v2.12 with machine-drafted
translations and **no copy review**: the six state names and six notes
in `statStates()`, the `stat` partial blurb, the `stat-band` idiom
blurb, and `"since Monday"` in the demo dashboard.

- [ ] Run the copy-review gate over those thirteen. Apply Paul's text
      verbatim, then redraft the eleven translations of anything he
      changed. Commit `designsystem: the stat band's copy, as reviewed`.

---

### Task 1: the semantic elements that need no new copy

**Files:** `ui/partials/*.html`, `ui/tokens.css`, `ui/ui_test.go`.

§6-v2.4's element table, restricted to what changes markup rather than
prose. Independent of every other task and the cheapest real value here.

1. **`<bdi>` around user-supplied names in running text.** The `person`
   partial's Name and Email. Twelve locales including Arabic: a
   right-to-left name inside a left-to-right line reorders it without
   this. **Currently wrong**, invisibly, on a shipped locale.
2. **`<time datetime>`** in `detail-list` (an optional `DateTime` per
   item) and `job-status`. An API addition, not a change: an item
   without it renders exactly as today.
3. **`<figure>`/`<figcaption>`** for the gallery's preview frames.
   **Decide first:** a visible caption changes the page's design; a
   visually-hidden one duplicates the `title` attribute that is already
   there. Ask before building.

- [ ] TDD per element. `<bdi>` needs a browser drive with an Arabic
      name in an English sentence — a Go assertion on the markup proves
      the element is present, not that it fixed the reordering.
      Commit `ui: bdi, time, and the elements the framework was missing`.

### Task 2: `<meter>` and `<progress>` — RAISE BEFORE BUILDING

**Files:** `ui/partials/meter.html`, `job-status.html`, `ui/tokens.css`.

§6-v2.4 names these two pointedly: "the `meter` partial is named after
an element it does not use." The a11y win is real — a native `<meter>`
has `role="meter"` and `aria-valuenow`, and ARIA-instead-of-native is
the anti-pattern the first rule of ARIA names.

**The reason this is its own task and not part of Task 1:** styling a
native `<meter>` needs `::-webkit-meter-bar`,
`::-webkit-meter-optimum-value` and `::-moz-meter-bar` — a well-trodden
path, but **`-moz-` rules cannot be verified here.** The drive is
Chromium. Shipping untested Firefox pseudo-element rules into a
framework this careful about cross-engine behaviour is a decision, not
an implementation detail.

- [ ] Ask Paul: native elements with a Firefox rule we cannot test, or
      keep the spans? If native: build, and say plainly in the commit
      that the `-moz-` branch is unverified.

### Task 3: the Common data formats page

**Files:** `internal/designsystem/page.go` (a new `pageKind`),
`prose.go`, `docs/site/templates.md`.

§6-v2.4's page: dates, durations, numbers, currency, percentages, file
sizes, identifiers, people and addresses — each with the element that
carries it and how it renders across the twelve catalogs. This is where
`<address>`, `<abbr>`, `<data>` and `<output>` get a home; they have no
partial to live in.

**`<address>` is the one most likely to be got wrong** and it is worth
stating on the page itself: it marks contact information for its nearest
`<article>` or `<body>` ancestor — the author of that content. Not
postal addresses generally, not a list of people. **The `person` partial
must not become one.**

This page is heavy on new English. Copy review before translation.

- [ ] Copy review, then build, then translate. Commit
      `designsystem: Common data formats, and where <address> is wrong`.

### Task 4: more dashboard examples

**Files:** `internal/designsystem/page.go`, `samples.go`, `prose.go`.

Paul asked for "more dashboard examples with semantic elements" and
"different shapes of dashboard cards". The stat band is the strip; this
is what sits under it.

- A dashboard with **no data yet** — the state everyone forgets, and the
  one a new app shows first.
- Card shapes: a stat band over a chart-shaped box, a two-column split,
  a list card beside a meter.
- The demo application gaining a second dashboard view, or a second demo.

**Decide first:** these are *screens*, not partials or idioms, and the
gallery has no tier for one except the single demo app. See "The screens
tier" below — settle it here, because Task 5 wants the same answer.

### Task 5: sign-in screens — BLOCKED ON TWO ANSWERS

**Spec:** §6-v2.10, which is already written in detail and ends with two
questions addressed to Paul.

- [ ] **Do the screens live in `ui` as partials, or in `auth` as
      renderable pages?** Partials keep `auth` free of HTML and let an
      app compose its own; renderable pages are turnkey and match how
      `auth` already owns its routes.
- [ ] **Is a polished username/password card a position we want to
      take?** The framework documents passwords; a beautiful password
      card in the gallery encourages them.

Everything else in §6-v2.10 is settled: the one-primary-door
composition, find-or-create rather than a sign-in/sign-up split,
provider buttons hand-built to each spec with no vendor JavaScript, the
server-rendered/enhancement split, and the honesty constraint — **ship
the buttons, never imply `auth` implements Google, Apple, Microsoft or
GitHub sign-in.**

Add: **the security settings screen** (registered passkeys, recovery
codes). Passkey sign-in documented with nowhere to manage the keys is
half a story.

---

## The screens tier — one decision that unblocks Tasks 4 and 5

The gallery has partials (fixed data shapes), idioms (a shape wrapping a
caller's body), shells (page frames) and **one** demo application. A
sign-in screen and a dashboard are none of those: they are compositions
of the vocabulary, which is exactly what a reader wants to copy and
exactly what the gallery cannot currently show.

Adding a family to `families()` costs one page, its rail entry, its tab,
its prev/next step and its Overview route with no renderer edit — that
machinery already reads off the table. What it costs in judgement is
whether a composition belongs in `ui` at all, or whether the gallery
should show screens it does not ship as components.

**Recommendation:** a `screens` page whose examples are compositions
rendered from the shipped vocabulary and NOT exported as partials, with
the page saying so. It answers "what does a sign-in screen look like"
without the framework claiming to own one.

## Suggested order

Task 0 first and alone — it is cheap and it stops the copy debt
compounding. Tasks 1 and 2 are independent of everything and can land
next. Task 3 is the largest copy surface. Tasks 4 and 5 both wait on the
screens-tier decision, and 5 additionally on §6-v2.10's two questions.

## Self-review

**Coverage:** §6-v2.4 elements → T1, T2; §6-v2.4 data formats page →
T3; §6-v2.4 dashboard stats → landed as §6-v2.12; §6-v2.10 → T5.
**Not in scope:** the palette generator (§6-v2.2), the markup
migration's stage 3, microdata (ruled out until a real machine consumer
appears).
**Risk:** T3 and T4 are the copy-heavy ones, and copy reviewed late is
copy paid for twice. T2 is the one that can ship a real cross-engine
defect, which is why it is a question and not a task.
