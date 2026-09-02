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
- `SKILL.md` ≤ 19,000 bytes. Raised from 18,000 on 2026-09-02 after a
  clean merge of two under-budget branches summed to 18,084 and turned
  `main` red; the reason is recorded in `skillmd_test.go`.
- Never hardcode the theme, locale, shell or icon lists.

---

### Task 0: copy review of what has already landed — DONE (2026-09-01)

**Files:** `internal/designsystem/samples.go`, `page.go`, `prose.go`.

Thirteen English strings shipped in §6-v2.12 with machine-drafted
translations and **no copy review**: the six state names and six notes
in `statStates()`, the `stat` partial blurb, the `stat-band` idiom
blurb, and `"since Monday"` in the demo dashboard.

Done, over two rerolls. It was fifteen strings rather than thirteen —
miscounted across three insertion passes. Paul's verdict on the first
draft was "I have NO idea what this means"; the notes had been written
about the design decision instead of the action. The rewrite is in
AGENTS.md now as a standing rule.

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
3. **`<figure>`/`<figcaption>`** for the gallery's preview frames —
   **still open, still needs a decision.** A visible caption changes the
   page's design; a visually-hidden one duplicates the `title` attribute
   already on the frame, which is what a screen reader announces on
   entering it. Neither is obviously right, so neither was built.

**DONE (2026-09-02),** and the measurement moved the target. A probe of
the real patterns found `person` and `job-status` are NOT affected —
blocks and intervening Latin words insulate them — while the list row's
name cell reorders badly: the time draws left of the name. The fix went
there, plus the rule in templates.md for app authors. `<time>` landed on
`detail-list` as an optional `DateTime`. `<figure>` was not built; see
below.

### Task 2: `<meter>` and `<progress>` — DONE (2026-09-02)

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

**RULED 2026-09-02 by Paul: ship native, flag the risk.** Built. The
`-moz-` rules are unverified and the commit and the stylesheet both say
so. `meter` keeps `aria-hidden` — the fraction beside it is the
accessible carrier, and a nameless meter would announce it twice —
so what the element buys is machine-readability, not accessibility.
`job-status` takes an optional `Percent` and draws a `<progress>` only
when it has one.

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

### Task 5: sign-in screens

**Spec:** §6-v2.10.

**RULED 2026-09-02 by Paul: partials now, pages later.** The screens are
built as `ui` partials so the gallery can show them and an app can
compose its own; `auth` stays free of HTML. A renderable-page layer over
them is a separate decision, taken once the shapes have settled rather
than now.

**Still open:** is a polished username/password card a position we want
to take? The framework documents passwords, and a beautiful password
card in the gallery encourages them.

Everything else in §6-v2.10 is settled: the one-primary-door
composition, find-or-create rather than a sign-in/sign-up split,
provider buttons hand-built to each spec with no vendor JavaScript, the
server-rendered/enhancement split, and the honesty constraint — **ship
the buttons, never imply `auth` implements Google, Apple, Microsoft or
GitHub sign-in.**

### Task 6: the account lifecycle — everything behind the door

**RULED 2026-09-02 by Paul: yes, as its own task.** §6-v2.10 covers
getting in and stops there. Grep it for "enroll", "profile", "settings",
"change", "2FA" or "recovery" and it says nothing, which leaves out most
of the account surface and all of the screens an app needs *after* the
first session.

The screens, in the order an app needs them:

- **Security** — enrolled passkeys with add and remove, recovery codes
  with regenerate. `passkey` ships enrollment and `recovery.go`
  already; the design system documents the door and nothing behind it.
- **Profile** — name, email, the ordinary account record.
- **Email and password change** — both are re-authentication flows, not
  plain form saves, and the screens should show that.
- **Two-factor setup** — the enrolment step, its recovery codes, and
  the "you will be signed out elsewhere" consequence.
- **Active sessions** — what is signed in, and signing one out.

Same home as Task 5 (partials), and the same honesty constraint applies
with more force: **ship screens for what `auth`, `passkey` and
`password` actually implement.** This is a documentation gap rather than
a build gap — the mechanisms exist — so a screen here that implies a
flow the framework does not have is a straightforward lie about the
product.

Copy-heavy. Review before translation, as everywhere else.

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

Tasks 0, 1 and 2 have landed. What is left, in order:

**Task 3 (Common data formats)** is unblocked and is the largest copy
surface — draft, review, then translate. **Tasks 4, 5 and 6** all wait
on the screens-tier decision below; 5 and 6 are otherwise unblocked now
that the partials-not-pages ruling is in. The one remaining question
inside Task 5 is whether to ship a password card at all.

`<figure>` from Task 1 is still unbuilt and still needs a decision.

## Self-review

**Coverage:** §6-v2.4 elements → T1, T2; §6-v2.4 data formats page →
T3; §6-v2.4 dashboard stats → landed as §6-v2.12; §6-v2.10 → T5.
**Not in scope:** the palette generator (§6-v2.2), the markup
migration's stage 3, microdata (ruled out until a real machine consumer
appears).
**Risk:** T3 and T4 are the copy-heavy ones, and copy reviewed late is
copy paid for twice. T2 is the one that can ship a real cross-engine
defect, which is why it is a question and not a task.
