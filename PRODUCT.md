# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

**Primary: human Go developers building web applications.** They write
GORM models, `net/http` handlers on a chi router and `html/template`
pages, and reach for rastrillo for the parts that are tedious to get
right twice — the database opener, sessions, identity, CSRF, owner
scoping, forms, background jobs, and a UI vocabulary they do not have to
invent.

**Close behind: LLM agents writing and editing rastrillo apps.** Not an
afterthought and not the deciding audience. Agent-legibility is a hard
constraint on the framework's artifacts — `SKILL.md` is byte-budgeted
and reviewed like code because it is what an agent loads *instead of*
reading the source, and the markup grammar and machine-checkable gates
exist so an agent cannot quietly get it wrong. Where the two audiences
conflict, the person wins.

**A third, distinct audience for the design system alone:** people who
want quality markup and a baseline visual aesthetic and are not using
the Go framework at all. Confirmed by the shipped Getting started page:
*"Rastrillo apps get all of this day one, but anyone can use the design
system"* and *"Take tokens.css and one theme and you have the whole
visual system: plain classes, ordinary HTML, no build step."*

## Product Purpose

Rastrillo is the CARLOS web framework — the shape of a CARLOS app, as
`carlosframework/platform` is the shape of the deployment substrate.
It supplies the parts of a web application that are tedious to get right
twice, and a design system that gives an app correct, accessible markup
and a working visual aesthetic on day one.

Success is an app that is scaffolded, correct and accessible without its
author having to make the same twenty decisions every other author
makes — and that keeps working with JavaScript switched off.

## Positioning

Adoptable by anyone: **public, MPL 2.0, `go get`-able**, at module path
`amadan.net/rastrillo/rastrillo`.

Three things a neighbouring framework could not truthfully copy without
doing the same work:

- **The design system stands alone.** `tokens.css` plus one theme is the
  whole visual system, in plain classes and ordinary HTML with no build
  step and no second origin. You do not have to adopt the Go framework
  to take it.
- **The scriptless path is the real one.** Menus, tabs, the preview
  widget, the modal, the demo application's view switching — all work
  with scripts disabled. `rastrillo.js`, `select.js` and `datetime.js`
  are enhancements, each inert until a control opts in and each
  deletable on its own.
- **The claims are gated rather than asserted.** WCAG 2.2 AA is held by
  an axe scan with zero exemptions and a contrast gate over documented
  token pairs in every theme and both schemes; twelve languages are held
  by a translation-parity gate and an English-leak gate. A claim that is
  not enforced by a test is not made.

## Operating Context

- `rastrillo new` scaffolds an app that builds and passes its own tests
  immediately, with flags for icon set, icon delivery and UX
  conventions.
- Delivered-once-then-yours: `tokens.css`, the theme, the scripts and the
  icon package are copied into the app at scaffold time and are
  app-owned from that moment. The known cost is drift — an app can run
  new markup against frozen old CSS — and `rastrillo doctor` compares an
  app's frozen files against the module's and offers to re-copy.
- One gate definition, run before pushing and by CI:
  `go vet ./... && gofmt -l . && go test ./...`. A scaffolded app's own
  gate is its `Makefile`'s `ci` target.
- Every change lands on its own branch through `amadan branch merge`,
  never a direct merge to main and never a squash. On the amadan hub the
  branch *is* the pull request, and merge is detected by ancestry — a
  squash rewrites the commits and the branch stays open. The GitHub
  remote is a mirror; `make mirror` and `make mirror-check` keep it one.
- The design system gallery is generated at build time by the public
  `cmd/dsgen` and is not committed; it is published at
  rastrillo.org/design-system.

## Capabilities and Constraints

- **Shipped subsystems:** database opener (GORM over modernc SQLite via
  `gormlite`), sessions, identity plugins (magic links and the keymail
  OAuth ceremony, passwords, passkeys, recovery codes), CSRF, owner
  scoping, form helpers, background jobs, migrations, manifests, mail,
  keyring, blobs, event log, vault client, scheduled ticks.
- **UI vocabulary:** `tokens.css`, three themes (`day`, `plain`,
  `signal`), four page-frame shells (`column`, `topbar`, `sidebar`,
  `console`), a partial library, and eleven-plus icon slugs vendored as
  inline SVG. Icon slugs are rastrillo's own names, not a vendor's, so
  `{{icon "search"}}` means the same thing whichever set an app chose.
- **Twelve shipped locales:** en, ga, zh-Hans, es, hi, pt, bn, ru, ja,
  yue, vi, ar. Arabic makes right-to-left a shipped requirement, not a
  hypothetical.
- **Markup grammar, mid-migration.** The vocabulary is moving from
  classes to attributes — `<div rst-box>`, `<a rst-btn="primary">` —
  with seven cross-cutting utilities keeping `class`. Stages 1 and 2
  have landed; every `tokens.css` rule currently carries both spellings
  and a gate enforces the pairing. Stage 3 drops the class selectors
  before 1.0.
- **Released:** v0.24.0. `main` regularly runs ahead of the latest tag,
  and the spec's own §0 warns that the gap between RELEASED and ON MAIN
  is the one that bites.
- **Undecided, recorded rather than invented:** whether sign-in screens
  live in `ui` as partials or in `auth` as renderable pages; whether the
  framework should ship a polished username/password screen at all;
  whether the gallery gains a "screens" tier for compositions such as
  dashboards and sign-in.

## Brand Commitments

- The name is **rastrillo**, lower case in prose and in the module path.
- **Copy tone, recorded in AGENTS.md:** short sentences, plain words,
  and tell the reader what to do. Reasoning belongs in the code comment
  and the spec; the instruction belongs on the page. Brevity is the
  goal and flippancy is its failure mode — cut words, not seriousness.
- **Commit and comment voice:** imperative subject; the body explains
  *why* — the failure prevented, the alternative rejected — never what
  the diff already shows.
- Every user-facing string passes a copy review before it ships. In
  `internal/designsystem` the English *is* the translation key, so a
  word changed afterwards costs eleven redrafted translations.

## Evidence on Hand

- **The design system gallery** — every partial, idiom, token and icon,
  in three themes and twelve languages, generated by `cmd/dsgen`.
- **The demo application** on the gallery's Overview: a dashboard, a
  list and one record open, built only from the shipped vocabulary,
  three views at three addresses, working with scripts disabled.
- **A worked example app** at `examples/notes/` (a separate Go module
  with a `replace` back to the checkout).
- **The documentation site** at `docs/site/`, and the spec library at
  `docs/superpowers/specs/`.
- **Absent, and not to be fabricated:** there are no testimonials, named
  customers, adoption numbers, benchmarks, case studies or press. There
  is no pricing — the licence is MPL 2.0. Do not invent deployment or
  performance claims; measure them from the shipped artifact or leave
  them unsaid.

## Product Principles

1. **The scriptless path is the real one.** JavaScript is enhancement,
   never the mechanism. If a feature needs a script to work at all, that
   is a finding, not a design.
2. **Delivered once, yours from then on.** What the framework hands an
   app becomes the app's to edit or delete. The framework's job is to
   hand over something correct, not to retain control of it.
3. **A claim not held by a gate is not made.** Accessibility,
   localisation, contrast and cross-spelling parity are enforced by
   tests over the shipped artifact. Prose that asserts more than the
   gates hold is a defect.
4. **Humans first, agents close behind.** Every artifact is written to
   be read by a person and loaded by an agent. Neither may be served by
   making the other worse.
5. **Measured, not guessed.** Numbers in this codebase — preview
   heights, asset weights, scale factors, contrast ratios — are taken
   from a real engine and gated, so they cannot rot quietly.

## Accessibility & Inclusion

**WCAG 2.2 AA, enforced rather than intended.** The axe scan covers
every page kind of the gallery in both colour schemes with zero
violations and zero rules exempted. A contrast gate holds documented
token pairs at 4.5:1 for text and 3:1 for control boundaries, across
three themes and two schemes; adding a colour that carries text means
adding its pair to that gate.

Specific commitments the codebase already keeps:

- **State is never colour alone.** A status pill renders its label, a
  meter prints its fraction, and a stat's change carries its own `+`
  or `−` sign.
- **Right-to-left is shipped, not hypothetical.** Arabic is one of the
  twelve locales; logical properties are used so borders and dividers
  land on the correct side.
- **Reflow to 320px** is committed to in writing.
- **`prefers-reduced-motion`** is honoured.
