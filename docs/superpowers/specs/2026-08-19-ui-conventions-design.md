# Design: opinionated-but-optional UI — icon sets, delivery modes, UX conventions

**Status:** approved 2026-08-19; reconciled against `main` 2026-08-22 and
split in two. See **Amendment** at the end — read it before the body, which
still describes the pre-reconciliation codebase in places.

## Goal

Rastrillo is deliberately more opinionated about UI than a general web
framework, to push apps toward good UX and accessibility by default. This
design makes that opinionatedness *choosable* without making it optional:
Rastrillo still has a recommended answer for every question, but the
operator can pick a different one, and the choice is recorded where both
humans and agents can see it.

Two axes are opened up:

1. **Icons** — which set (Lucide, Font Awesome) and how it is delivered
   (inline-vendored, CDN, JS runtime).
2. **UX conventions** — a named profile, seeded once at scaffold time,
   written into `AGENTS.md`, and enforced by real vendored components
   wherever a component exists.

## What exists today

- `icons.go` — four hard-coded Lucide SVGs in a `map[string]template.HTML`,
  exposed as `rastrillo.Icon(slug)`. Its doc comment states a framework
  *promise*: "no build step, no second origin a browser fetches from and
  the app can't vouch for or serve when offline."
- `ui/funcs.go` — `ui.Funcs()` hard-wires `"icon": rastrillo.Icon`. All
  eight shipped partials resolve through that one map.
- `ui/tokens.css` — written once into a new app's `static/` by
  `rastrillo new`, app-owned from then on. **This is the precedent every
  delivery decision below follows.**
- README lists "the component/UI vocabulary" and "the preloaded
  `CLAUDE.md`/skill scaffolding" as designed but unbuilt.
- The Claude Code skill lives in a separate repo, `carlosframework/skills`.

## Decisions

| Question | Decision |
|---|---|
| Scope | Both halves, framework first |
| How an app gets a non-default set | Scaffold-time, app-owned source |
| Delivery mode | Operator's choice: inline / CDN / JS |
| How hard to steer | Default + informed consent |
| Slug vocabulary | Lucide slugs as lingua franca |
| Convention selection | Named profiles, recorded in the app |
| Enforcement | Components where they exist, guidance where they don't |
| Build approach | Narrow, two phases |

### A promise becomes a default

Opening CDN and JS delivery means `icons.go`'s "vendored, never a CDN" stops
being a framework invariant. This is deliberate. The doc comment in
`icons.go` — and the matching file in `carlosframework/platform`'s
`internal/console/icons.go`, which it mirrors exactly — must be reworded to
describe inline-vendoring as the **default and recommendation**, with its
reasoning intact, rather than as a promise the framework enforces. Leaving
either file claiming a guarantee the framework no longer makes is a
documentation bug, not a cosmetic one.

### Steering posture: default + informed consent

Inline-vendored is the default and the recommendation. CDN and JS are
first-class and fully supported. When a non-default delivery mode is
chosen, the scaffold prints the specific cost once and writes a comment
into the generated code recording what was traded away. There are no
build-time or run-time warnings, and `generate --check` does not flag it —
a supported choice that nags is not supported.

The JS mode's cost is stated plainly because it collides with the
"progressively enhance, never overload" convention this same design
teaches: with JS delivery, icons do not render without JS.

## Architecture: the icon axis

### Flags

    rastrillo new <name> [--icons=lucide|font-awesome]
                         [--icon-delivery=inline|cdn|js]
                         [--ux=considered|standard]

Defaults: `lucide`, `inline`, `considered`. `--ux` defaults to the
opinionated profile rather than to `standard`, because Rastrillo's stated
posture is to have a recommended answer to every question — a framework
whose opinions are opt-in has no opinions. `--ux=standard` is the opt-out.

`--icons` and `--icon-delivery` are orthogonal; all six combinations are
valid and supported. `--ux` interacts with both: see "The profile is a seed"
below for how a flag that contradicts the chosen profile resolves.

### What the scaffold writes

One app-owned file, `internal/icons/icons.go`, following the `tokens.css`
precedent — delivered once, app-owned from then on. It contains:

- a slug→markup map, **always keyed by Lucide slugs** regardless of set;
- `func Icon(slug string) template.HTML`;
- `func Assets() template.HTML` — markup for the document `<head>`.

By delivery mode:

- **inline** — `Assets()` returns empty; the map holds vendored SVG.
- **cdn** / **js** — `Assets()` returns the `<link>` / `<script>` including
  an SRI hash; the map holds markup like
  `<i class="fa-solid fa-magnifying-glass" aria-hidden="true"></i>`. No SVG
  is vendored, because the remote asset is the glyph source.

For Font Awesome, the scaffold also writes the CC BY 4.0 attribution the
licence requires. Vendoring or embedding FA without attribution is a licence
violation, so this is not optional and not a follow-up.

`tokens.css` gains `.icon` sizing rules that reserve space, so a JS-delivery
app does not shift layout as icons resolve.

### Accessibility invariant

Every icon carries `aria-hidden="true"` in all six combinations. The
existing rule holds unchanged across delivery modes: icons sit beside their
own visible text label, which is the accessible name; a control that uses an
icon as its *only* label must carry an explicit `aria-label`.

### Framework changes

- `rastrillo.Icon` is unchanged — still the built-in inline Lucide set, so
  existing apps and `ui.Funcs()` callers keep working untouched.
- `ui.Funcs()` becomes variadic: `ui.Funcs(opts ...Option)`. Called bare it
  behaves exactly as today. `ui.WithIcons(icons.Icon, icons.Assets)`
  overrides the resolver and registers **both** template seams, `icon` and
  `iconAssets`. It takes both functions because one cannot be derived from
  the other: `Assets` is what CDN and JS delivery need in `<head>`, and a
  single-argument `WithIcons` would leave that seam unregistered and those
  two delivery modes silently broken.
- New `internal/iconsets/` holds, per set: the Lucide-slug alias table, SVG
  path data for inline mode, CDN/JS URLs with SRI hashes, and licence and
  attribution text. It is internal, so it never compiles into an app that
  did not ask for it.

### Slug vocabulary

Lucide's slugs are the interface. Every other set ships an alias table
mapping Lucide slugs to its own glyph names (`search` →
`fa-magnifying-glass`). The eight shipped partials therefore never change,
and switching sets stays a flag rather than a code migration. The cost —
one vendor's naming enshrined as the API — is accepted; it preserves
`icons.go`'s existing stated value that the mapping back to lucide.dev
stays obvious, and avoids inventing a vocabulary nobody knows.

### `generate --check` slug gate

`generate --check` gains a check: a template calling `{{icon "x"}}` for a
slug absent from the app's icon map is an error.

At run time the existing behaviour is correct and unchanged — an unknown
slug renders nothing rather than panicking a page mid-response. `--check`
is where it should be loud, matching the i18n catalog gate's established
posture: silent fallback while iterating, loud failure before ship.

## Architecture: UX convention profiles

### AGENTS.md is the source of truth

`rastrillo new` writes the `## UX conventions` section into the app's
`AGENTS.md` — the cross-agent convention, so the conventions apply to
whatever agent someone uses, not only Claude Code.

`rastrillo new` also writes a `CLAUDE.md` whose entire body is an
`@AGENTS.md` import line and a sentence explaining why. **No convention text
is duplicated into it.** Two copies drift, and the drift is silent.

This is the first thing in rastrillo that writes either file. Both stay
minimal: the conventions section and the import line, nothing more. The
broader "preloaded CLAUDE.md" idea from the framework design is untouched
and still unbuilt.

### The profile is a seed, not a live binding

`--ux=<profile>` selects a starting set of conventions. The scaffold
immediately resolves it and writes the **resolved list, line by line**, into
`AGENTS.md`. The profile name never binds again; nothing at build or run
time re-reads what a profile currently means.

Three consequences, all intended:

- Flags that contradict the profile (`--ux=considered --icons=font-awesome`)
  resolve at scaffold time with the **flag winning**, and the file records
  the truth. The file never lies about what the app does.
- Editing a line is exactly as valid as picking a profile. This is what
  "override any line" must mean to be real.
- Upgrading rastrillo cannot silently change a shipped app's UX.

Accepted cost: an app scaffolded a year ago will not pick up conventions
added since. Conventions arriving unannounced in someone's app is the
failure mode, not the feature.

### Format

    ## UX conventions (seeded from profile: considered)

    - [x] Selects — searchable, progressively enhanced — `ui/searchable-select`
    - [x] Icons — lucide, inline-vendored — `internal/icons`
    - [ ] Dates — relative, with absolute in `title`
    - [ ] Destructive actions — inline confirm, never a modal

    `[x]` is enforced by a vendored component; `[ ]` the agent applies by hand.
    Edit any line. This file is the source of truth, not the profile name.

The `[x]`/`[ ]` distinction is load-bearing and machine-readable: it tells an
agent which conventions it must apply by hand, and it keeps the honest gap
between "Rastrillo enforces this" and "Rastrillo recommends this" visible
rather than blurred.

### Profiles shipped in phase one

Exactly two:

- **`considered`** — the full opinionated stack. Named for what it does.
  An earlier draft called it `impeccable`, after impeccable.style; that
  was dropped rather than borrow someone else's name for a profile they
  had no say in. External references are linked as further reading, not
  presented as something rastrillo endorses or implements.
- **`standard`** — rastrillo's plain defaults, no added conventions.

A third profile is a phase-two question, not a guess to make now.

### What an agent does with the file

- `[x]` — use the named component; do not hand-roll an equivalent.
- `[ ]` — apply the guidance by hand.
- Absent — rastrillo has no opinion; ask, or use judgement.
- Deleted by the user — **not** a convention to reinstate.

## Architecture: `ui/searchable-select`

The phase-one exemplar, chosen because it is where "progressively enhance,
never overload" has actual teeth, and because it forces the decision that
`ui/` has never shipped JavaScript before.

### Shape

The native `<select>` stays in the DOM and remains the source of truth. The
JS does not replace it: it visually hides it, renders an ARIA 1.2 combobox
alongside, and mirrors every change back into the select.

Form submission, `required` validation, form reset and browser autofill all
keep working, because the element being submitted is the same one it always
was. There is no duplicated state to diverge, and if the JS throws mid-page
the user is left with a working native select rather than a broken widget.

### Enhancement threshold

Templates always call `searchable-select`. The JS enhances only past a
threshold — default ~10 options, overridable per instance. Below it, the
native control renders as-is.

This reconciles the stated convention ("always a searchable select rather
than native") with good practice (search that filters four items is
furniture, not help). The judgement lives in the component, so no caller has
to make it.

### Phase-one cut: bounded option sets only

Every option renders into the select. The unbounded case — thousands of
options, server-side filtering — requires a search-endpoint route contract,
which is real design work rather than a parameter. Deferred to phase two and
stated as a limitation, not left implied.

### Files

- `ui/partials/searchable-select.html` — one `{{define}}`, one data value
  built with `dict`, data contract in a template comment above the define,
  matching the existing eight partials exactly.
- Component CSS in `tokens.css` (app-owned, written at scaffold).
- A plain ES module, no bundler, written into the app's `static/` at
  scaffold and app-owned from then on — same deal as `tokens.css`. No build
  step survives this design intact.

### Accessibility

ARIA 1.2 combobox pattern: `role="combobox"` with `aria-expanded` and
`aria-controls`, a listbox popup, `aria-activedescendant` for the active
option, full keyboard support (up/down/enter/escape/home/end), and a live
region announcing the filtered result count.

## Phasing

### Phase one

1. `internal/iconsets/` — Lucide and Font Awesome: alias tables, SVG data,
   CDN/JS URLs with SRI, licence and attribution text.
2. `rastrillo new` flags `--icons`, `--icon-delivery`, `--ux`; the scaffold
   writes `internal/icons/icons.go`, the FA attribution, `AGENTS.md`,
   `CLAUDE.md`, and the informed-consent note for non-default delivery.
3. Variadic `ui.Funcs(opts ...Option)` and `ui.WithIcons`, registering the
   `icon` and `iconAssets` seams.
4. Reword `icons.go`'s promise to a default; the same wording change lands
   in `carlosframework/platform`'s `internal/console/icons.go`.
5. `generate --check` slug gate.
6. `ui/searchable-select` — partial, CSS, ES module.
7. `examples/blog` uses a searchable select and a non-default icon
   combination, so both are exercised end to end.
8. README and docs.
9. **A small PR in `carlosframework/skills`** teaching the skill to read
   `AGENTS.md`'s conventions section. Without it the profile file is inert,
   so this is part of phase one rather than a follow-up.

### Phase two — separate brainstorm

The rest of the convention catalogue; the server-backed unbounded
searchable select; further profiles.

## Testing

- Go render tests in `ui/` for the `searchable-select` markup contract, in
  the style of the existing `ui/ui_test.go`.
- Icon resolution tests across all six set × delivery combinations,
  including the `aria-hidden` invariant.
- `generate --check` slug-gate tests, matching the existing i18n gate tests.
- `examples/blog`'s `blogtest` suite covers the rendered searchable select
  end to end.

**Honest gap:** the enhanced JS path has no automated coverage in phase one.
There is no browser harness in this repo and adding one is its own project.
The no-JS path — the one that must never break — is fully covered.

## Open items

1. ~~**Profile naming.**~~ Resolved 2026-08-20: the profile is
   `considered`. No permission is needed, because impeccable.style is now
   linked as one of several further-reading references rather than as a
   source the framework endorses or implements.
2. **Font Awesome attribution text.** The exact CC BY 4.0 attribution the
   scaffold writes needs a read-through against the licence before it ships.

## Out of scope

- Icon sets beyond Lucide and Font Awesome.
- Server-backed / unbounded searchable select.
- A generic preset or config-file mechanism. Rejected deliberately: it would
  be designed against two data points. Revisit once there are five real
  axes, not two.
- Any wider "preloaded CLAUDE.md" scaffolding beyond the import line.


---

## Amendment, 2026-08-22 — reconciliation with `main`

This design was written against `b254ab6`. `main` moved 52 commits (to
v0.12.0) before it landed: the known-libraries middle layer, the manifest
system, background jobs, and a fragment shim. Reconciling changed enough
to be worth recording, because a reader of the body above would otherwise
be misled.

### Split in two

The design is now two pieces of work.

- **This PR — icons and conventions.** Survives essentially as designed.
- **Separate work — the searchable select.** Superseded in *shape*, not
  in idea. See below.

### What the body gets wrong now

1. **"Lucide slugs as lingua franca" is no longer accurate, and the
   design is stronger for it.** The framework's set grew from 4 slugs to
   11, and five of them are *not* Lucide's canonical names — `kebab` is
   Lucide's `ellipsis-vertical`, and v1 renamed `check-circle`,
   `alert-triangle`, `x-circle` and `help-circle`. The vocabulary is
   rastrillo's own. Even the Lucide set now needs a translation table,
   which is the clearest possible evidence that the alias layer earns its
   keep rather than being speculative.

   `rastrillo.IconSlugs` is exported, and `internal/iconsets` asserts
   every scaffoldable set covers all of it. Without that guard, an icon
   added to `icons.go` would silently vanish for anyone who passed
   `--icons`.

2. **`ui.Funcs` had grown a second extension mechanism.** `main` added
   `FuncsWith(t)` for the i18n `T` seam while this design added
   `Funcs(opts ...Option)`. Two patterns for extending one function.
   Resolved by making the option form canonical — `WithIcons`, `WithT` —
   with `FuncsWith(t)` kept as `Funcs(WithT(t))`, unchanged for callers.

   That `FuncsWith` silently resets the icon seam back to the framework
   default is a real trap for an app using both. It is documented on the
   function and pinned by a test rather than left to be discovered.

3. **The scaffold layout moved.** `internal/<pkg>/static/`,
   `internal/<pkg>/templates/`, plus `manifest/`, `Makefile`, `.amadan/`.
   The icons package is `internal/<pkg>/icons`, and the slug gate
   discovers it under `internal/*/icons` rather than assuming one path.

4. **`CLAUDE.md` already existed, with real content.** The original
   design made it a one-line pointer, which would have deleted `main`'s
   preload. Resolved per the decision recorded in the PR: that content
   moves wholesale to `AGENTS.md` — the cross-agent file — and
   `CLAUDE.md` becomes the `@AGENTS.md` import. Nothing is duplicated.

5. **The wiring is not optional.** `--icon-delivery=cdn|js` needs
   `{{iconAssets}}` in `<head>` or the flag is inert, so the scaffold now
   registers both seams in the generated `render.go` and calls
   `{{iconAssets}}` in the layout. Verified end to end: a scaffolded
   font-awesome/cdn app serves the SRI-pinned stylesheet from its own
   test harness.

### searchable-select: superseded in shape

`main` now has `field-select.html` — a native select inside a full field
envelope (label, hint, help, error, `aria-describedby` wiring, `Short`)
with `T` i18n — and `ui/rastrillo.js`, a fragment shim whose stated
doctrine is *"inert by default: only elements that opt in with a data
attribute get behavior, and everything it enhances also works with
scripts disabled."* That is this design's progressive-enhancement
contract, already implemented as a house style.

Shipping the standalone `searchable-select` partial as designed would put
two selects with different markup conventions into one library, and a
second script file with the same doctrine as the first. The right shape
is now:

- **an option on `field-select`**, not a parallel partial — which gets
  error, help and hint handling for free; and
- **an entry in `rastrillo.js`'s vocabulary** (`data-rst-select` beside
  `data-poll` and `data-busy`), not a second script.

The *idea* is unchanged and still correct: the native `<select>` stays
the control, enhancement is opt-in, and the no-JS path is the real path.
Only the packaging changes. Deferred to its own work rather than force-
fitted here.

### Still open

The Font Awesome CC BY 4.0 attribution wording wants a read against the
licence before release. The profile-naming item is closed: it ships as
`--ux=considered`, with external references linked as further reading
rather than as endorsed sources.
