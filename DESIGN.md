---
name: Rastrillo
description: A quiet, correct interface baseline built to be taken over.
colors:
  bg: "#ffffff"
  surface: "#ffffff"
  surface-2: "#f6f7f8"
  line: "#e5e7eb"
  line-strong: "#7f8792"
  text: "#1a1d21"
  text-muted: "#4b5563"
  text-faint: "#656c78"
  accent: "#2464e0"
  accent-strong: "#1a4fc0"
  accent-soft: "#eef3fe"
  on-accent: "#ffffff"
  tone-neutral-fg: "#374151"
  tone-neutral-bg: "#eef0f2"
  tone-positive-fg: "#166534"
  tone-positive-bg: "#dcfce7"
  tone-warning-fg: "#854d0e"
  tone-warning-bg: "#fef3c7"
  tone-negative-fg: "#b91c1c"
  tone-negative-bg: "#fee2e2"
typography:
  display:
    fontFamily: "system-ui, -apple-system, \"Segoe UI\", Roboto, \"Helvetica Neue\", Arial, sans-serif"
    fontSize: "2.1rem"
    fontWeight: 650
    lineHeight: 1.1
  headline:
    fontFamily: "system-ui, -apple-system, \"Segoe UI\", Roboto, \"Helvetica Neue\", Arial, sans-serif"
    fontSize: "1.375rem"
    fontWeight: 600
    lineHeight: 1.25
    letterSpacing: "-0.015em"
  title:
    fontFamily: "system-ui, -apple-system, \"Segoe UI\", Roboto, \"Helvetica Neue\", Arial, sans-serif"
    fontSize: "0.875rem"
    fontWeight: 600
    lineHeight: 1.4
  body:
    fontFamily: "system-ui, -apple-system, \"Segoe UI\", Roboto, \"Helvetica Neue\", Arial, sans-serif"
    fontSize: "0.875rem"
    fontWeight: 400
    lineHeight: 1.6
  label:
    fontFamily: "system-ui, -apple-system, \"Segoe UI\", Roboto, \"Helvetica Neue\", Arial, sans-serif"
    fontSize: "0.71875rem"
    fontWeight: 650
    letterSpacing: "0.06em"
rounded:
  bar: "2px"
  sm: "6px"
  md: "8px"
  pill: "999px"
spacing:
  "1": "0.25rem"
  "2": "0.5rem"
  "3": "0.75rem"
  "4": "1rem"
  "5": "1.5rem"
  "6": "2.5rem"
components:
  button:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.text}"
    typography: "{typography.title}"
    rounded: "{rounded.sm}"
    padding: "0.45rem 0.75rem"
  button-primary:
    backgroundColor: "{colors.accent}"
    textColor: "{colors.on-accent}"
    typography: "{typography.title}"
    rounded: "{rounded.sm}"
    padding: "0.45rem 0.75rem"
  button-primary-hover:
    backgroundColor: "{colors.accent-strong}"
    textColor: "{colors.on-accent}"
  input:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.text}"
    rounded: "{rounded.sm}"
    padding: "0.45rem 0.6rem"
  card:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.text}"
    rounded: "{rounded.md}"
    padding: "1.1rem 1.25rem"
  status-pill:
    backgroundColor: "{colors.tone-neutral-bg}"
    textColor: "{colors.tone-neutral-fg}"
    rounded: "{rounded.pill}"
    padding: "0.15rem 0.5rem"
---

# Design System: Rastrillo

<!--
  Scope note, because this system has three themes and two colour schemes.
  The frontmatter carries the `day` theme's LIGHT values, because
  ui.ThemeNames()[0] is `day` and it is what `rastrillo new` copies. The
  normative source in the repository is one custom property per colour
  holding a light-dark() pair, so a single-value frontmatter slot cannot
  hold it; .impeccable/design.json carries both halves per token under
  extensions.colorMeta. Do not treat a hex here as the only value.
  Source of truth: ui/tokens.css (structure) and ui/themes/*.css (colour,
  type family, shape).
-->

## Overview

**Creative North Star: "The Remixable Default"**

This is the interface an application has before anyone has styled it —
done properly, and built to be taken over. It aims to be correct and
unobjectionable everywhere rather than memorable anywhere, so an app
that never touches it still looks deliberate, and an app that wants its
own character can get there by setting variables rather than by fighting
the stylesheet.

Remixability is the thesis, not a feature. Every colour, radius, shadow
and type family resolves through a custom property, and the three
shipped themes are proof that the axes work rather than a menu of three
answers. `day` is soft (8px/6px corners, wide low-alpha shadows),
`plain` is nearly square with a near-black accent (4px/3px, one hairline
layer) and `signal` is milled (4px/2px, short dense shadows, an electric
blue). An app that wants a fourth writes one `:root` block. Files are
delivered once and app-owned from that moment: edit them, or delete what
you do not use.

The system is deliberately under-designed at the component level so an
app's own character can land on top without a fight. It is explicitly
not the SaaS gradient template — no purple-to-blue washes, glassmorphism,
oversized rounded cards or decorative blobs — and explicitly not a
vendor-branded admin kit that stamps someone else's identity on an app
and resists removal. Both hand you an identity you did not choose, which
is the one thing this system exists not to do.

**Key Characteristics:**

- Small, dense type: a 14px base with 12.5px and 11.5px steps below it.
- Near-flat surfaces separated by 1px hairlines; depth is a theme axis.
- One accent, carrying action and position; status carries its own
  four-tone palette.
- Every colour is a `light-dark()` pair, so light and dark are one
  declaration rather than two stylesheets.
- Works with JavaScript off. Scripts are enhancement and each is
  deletable on its own.

## Colors

A restrained near-neutral palette with a single working blue, held to
WCAG 2.2 AA by a contrast gate over documented pairs in every theme and
both schemes.

### Primary

- **Interface Blue** (`#2464e0` light, `#7ba7ff` dark): the accent. It
  marks what you can act on and where you are — focus rings, the primary
  button, links, the current navigation item, an active tab. It belongs
  to the chrome, not to the content.
- **Interface Blue Deep** (`#1a4fc0` light, `#a3c0ff` dark): the primary
  button's hover and active fill, and nothing else.
- **Interface Blue Wash** (`#eef3fe` light, `#17233a` dark): the tint
  behind a current or hovered item. Carries no text of its own beyond
  the accent and the muted greys, both gated against it.

### Neutral

- **Page** (`#ffffff` light, `#111418` dark): the page ground.
- **Surface** (`#ffffff` light, `#1a1f26` dark): cards, panels, inputs
  and menus. In light `day` it is the same white as the page, so cards
  are separated by their border rather than by tone.
- **Surface Sunken** (`#f6f7f8` light, `#15191f` dark): the recessed
  step — table header rows, hovered rows, the second tone in a stack.
- **Hairline** (`#e5e7eb` light, `#2a3038` dark): every divider, card
  edge and table rule.
- **Control Edge** (`#7f8792` light, `#79828e` dark): the border of a
  thing you can operate — buttons, inputs, selects. Deliberately much
  darker than the hairline, and held at 3:1 against its backgrounds
  because it is a control boundary (WCAG 1.4.11), not decoration.
- **Ink** (`#1a1d21` light, `#e8eaed` dark): body text.
- **Ink Muted** (`#4b5563` light, `#a8b0ba` dark): secondary text,
  subheads, cell values that are not the row's name.
- **Ink Faint** (`#656c78` light, `#98a1ac` dark): hints, captions,
  counts. Still gated at 4.5:1 — "faint" is a role, not a licence to
  drop below the floor.

### Tertiary

The four status tones, each a foreground/background pair used together:
**Neutral** (`#374151` on `#eef0f2`), **Positive** (`#166534` on
`#dcfce7`), **Warning** (`#854d0e` on `#fef3c7`), **Negative**
(`#b91c1c` on `#fee2e2`). They exist so status never borrows the accent.

### Named Rules

**The Gated Pair Rule.** A colour that carries text owes 4.5:1 against
the surface it actually sits on, and a control boundary owes 3:1 — and
owes it in every theme and both schemes, in the contrast gate, before it
ships. Adding a colour means adding its pair to that gate. A tone chosen
against a tinted ground says nothing about the same tone on a plain card.

**The Two-Signal Rule.** State is never colour alone. A status pill
renders its label, a meter prints its fraction as text beside the bar,
and a stat's change carries its own `+` or `−` sign. Colour is the
second signal, never the first.

**The Light-Dark Rule.** Every colour is declared once as
`light-dark(<light>, <dark>)` under `color-scheme: light dark`. There is
no dark stylesheet and no second palette to keep in step; an explicit
theme choice re-resolves the same declarations by setting
`color-scheme`.

## Typography

**Display / Body Font:** the platform UI stack — `system-ui`,
`-apple-system`, `"Segoe UI"`, `Roboto`, `"Helvetica Neue"`, `Arial`,
`sans-serif`. `signal` leads the same stack with `"Helvetica Neue"`.
**Label/Mono Font:** no separate family; monospaced values use the
`rst-mono` utility.

**Character:** the type has no personality of its own on purpose. The
family is a theme axis, and the shipped default is whatever the reader's
OS considers correct — which is the right answer for an application
frame and the wrong answer for a brand, so an app that wants a voice
sets one property.

### Hierarchy

- **Display** (650, 2.1rem, 1.1): the lead reading in a stat band, and
  nothing else. The only genuinely large type in the system.
- **Headline** (600, 1.375rem, 1.25, −0.015em): a screen's `<h1>` in the
  page header. Drops to 1.25rem under 34rem.
- **Title** (600, 0.875rem, 1.4): section headings inside a card head,
  button labels, a row's name. Note that a section `<h2>` is the same
  SIZE as body text and separates by weight alone.
- **Body** (400, 0.875rem = 14px, 1.6): everything else. Subheads cap at
  44rem.
- **Label** (650, 0.71875rem = 11.5px, +0.06em, uppercase): the eyebrow
  over a value — stat labels, table column heads, the nav rail's section
  names.

### Named Rules

**The Small-Base Rule.** The base is 14px, not 16px, and every step is
declared in `rem` so the whole scale tracks a reader who has raised
their browser default. Do not convert any of it to `px`.

**The Weight-Not-Size Rule.** Hierarchy inside a screen is carried by
weight and colour before size. A card's `<h2>` is body-sized at 600;
reaching for a larger size to make a section feel important is how this
system loses its density.

## Layout

A single centred content column, capped at **64rem** with `1.5rem 1rem
2.5rem` of padding, holding every screen. The column is markup an app
emits (`rst-page`); no partial brings its own.

**Spacing** is a six-step rem scale — 0.25 / 0.5 / 0.75 / 1 / 1.5 /
2.5rem — and screens stack vertically in reading order: page header,
then section head plus card, then the next. Horizontal arrangement is
reserved for the idioms that ship it (card heads, field rows, toolbars,
list-row cells, segmented tabs, stat bands).

**Breakpoints.** 800px is the system's real line: `rst-m-hide` columns
fold away, the sidebar rail collapses into a disclosure, and the topbar
takes its narrow layout. 34rem (544px) stacks the page header's title
and action. **320px is a written commitment** — the layout reflows to it
without a horizontal scrollbar.

**Density** is high by intent: 14px base, 0.45rem control padding, rows
that fit a working screenful. The floor on that density is WCAG 2.2's
Target Size (2.5.8) — pagination chips measure about 30px, row-action
pills about 27px and buttons about 34px on their smaller axis, all clear
of the 24px minimum. Do not shrink them without re-measuring.

The scrollbar gutter is reserved whether or not there is a scrollbar in
it (`--rst-scrollbar-gutter`), so a short page and a long one are the
same width and moving between them does not slide the layout sideways.

## Elevation & Depth

**Depth is the theme's job, and that is the design decision.** The
system paints with `var(--rst-shadow-*)` everywhere and does not decide
what those are: `day` is soft and wide, `plain` is a single hairline
layer, `signal` is short and dense. An app changing its shadow
vocabulary is changing how it feels, which is exactly what a theme
should be able to do.

What is constant is that surfaces are near-flat and separated by 1px
hairlines, with a sunken tonal step for recessed rows. Shadow is
reserved for things that leave the page plane.

### Shadow Vocabulary (the `day` theme)

- **Pop** (`0 8px 24px light-dark(rgba(0,0,0,0.12), rgba(0,0,0,0.5))`):
  an open menu, a dropdown panel, a modal — surfaces that float over the
  page.
- **Knob** (`0 1px 2px rgba(0,0,0,0.3)`): the moving part of a control,
  such as a switch's knob.
- **Lift** (`0 1px 2px rgba(0,0,0,0.08)`): the barely-there separation
  on a raised strip.
- **Overlay** (`light-dark(rgba(0,0,0,0.45), rgba(0,0,0,0.6))`): the
  modal scrim. Deeper in dark on purpose — black reads lighter against a
  dark backdrop and needs more alpha to still dim.

## Shapes

Soft rectangles, and the exact softness is a theme axis: `day` uses 8px
on cards and 6px on controls, `plain` 4px and 3px, `signal` 4px and 2px.
A pill radius (999px) is reserved for status pills and count chips —
things that are read as tokens rather than operated.

A 2px radius is the thin-bar step, and it is not on the theme axis: the
capacity meter and a running job's progress bar both use it, at a height
of 4px where a themed 8px corner would swallow the bar whole.

Borders do most of the structural work: 1px hairlines between and around
surfaces, and a distinctly darker 1px edge on anything operable. Focus
is a 2px accent outline offset 2px, never a border swap, so focus never
changes an element's size.

**Known gap, recorded rather than smoothed over.** `tokens.css` also
hard-codes 9px, 7px, 4px, 10px and 14px radii in a handful of places —
the combobox and date-picker lists, and a few small chrome pieces — that
do not resolve through `var(--rst-radius*)`. A theme that changes its
corner language does not change those. It is a real limit on the
remixability this system claims, it predates this record, and it is not
something to fix by editing one component in passing.

**The Derived Rule Rule.** The page header's underline is not authored
per theme — it is `color-mix(in oklab, var(--rst-accent) N%, var(--rst-line))`,
mixed at 18% in `day`, 45% in `signal`, and left as the plain hairline
in `plain`. A theme that changes its accent gets a matching rule with no
second value to keep in step. It is decorative and carries no contrast
floor.

## Components

### Buttons

- **Shape:** softly rounded (`{rounded.sm}` — 6px in `day`).
- **Default:** surface fill, a Control Edge border, Ink label at 600 and
  12.5px, `0.45rem 0.75rem` padding. Hover moves border and label to the
  accent rather than filling.
- **Primary:** accent fill, accent border, `on-accent` label. Hover
  deepens to Interface Blue Deep.
- **Ghost:** no fill, muted label, darkens toward ordinary Ink on hover
  rather than picking up the accent — the accent means "this goes
  somewhere new", and a ghost Cancel does not.
- **Danger:** the negative tone as a solid fill; its label pair is in the
  contrast gate.
- **Focus:** 2px accent outline, 2px offset.

### Cards / Containers

- **Corner Style:** `{rounded.md}` (8px in `day`).
- **Background:** Surface, on a Page ground that is the same white in
  light `day` — so the 1px hairline border is what separates them.
- **Shadow Strategy:** none at rest. See Elevation & Depth.
- **Internal Padding:** `1.1rem 1.25rem`. A card head (`rst-box-head`)
  sits *outside* the card, holding an `<h2>` and one compact button.
- **Note:** list cards and data grids hold rows only and are unpadded by
  design; prose, forms and links go in a padded box.

### Inputs / Fields

- **Style:** Surface fill, 1px Control Edge border, `{rounded.sm}`,
  `0.45rem 0.6rem` padding, full width by default.
- **Focus:** 2px accent outline offset 2px.
- **Error:** the message is text, and the field is described by it via
  `aria-describedby`; the required marker uses the negative tone.

### Navigation

The rail's current item is marked by three signals together —
`accent-soft` background, accent colour and weight 600 — and by
`aria-current` in the markup. Below 800px the rail collapses into a
`<details>` disclosure with a menu icon. Menus are
`<details name="rst-menus">`, so opening one closes the rest with no
script; `rastrillo.js` adds outside-click and Escape as enhancement.

### Status Pill

A pill-radius tinted chip carrying a foreground/background tone pair and
**always its label**. The leading dot is decoration on top of the word,
never a substitute for it.

### Stat Band (signature)

The instrument strip a dashboard opens with: a row of stat cells in one
card, divided by inline-start hairlines, wrapping rather than squeezing
so any number of cells works. One cell may be marked `lead`, which is
the same component at 2.1rem instead of 1.5rem — there is no separate
headline component. Numbers are `tabular-nums` so a polled figure does
not jitter its neighbours. A change carries its own sign in the text and
a tone the caller supplies; the tone is never derived from the sign,
because a falling number is good about as often as it is bad.

## Do's and Don'ts

### Do:

- **Do** change the system by setting custom properties. Every colour,
  radius, shadow and type family resolves through one; a fourth theme is
  a single `:root` block.
- **Do** declare colours as `light-dark(<light>, <dark>)` pairs under
  `color-scheme: light dark`, so light and dark stay one declaration.
- **Do** add any new colour that carries text to the contrast gate, at
  4.5:1 against the surface it actually sits on, in all three themes and
  both schemes.
- **Do** give state a second signal besides colour — a label, a sign, a
  number as text.
- **Do** keep type in `rem` and the base at 14px, so the scale tracks a
  reader's own browser setting.
- **Do** use logical properties (`border-inline-start`,
  `margin-block-end`) so borders and dividers land correctly in Arabic.
- **Do** let the scriptless rendering be the real one; add script only as
  enhancement that can be deleted.

### Don't:

- **Don't** reach for the SaaS gradient template — purple-to-blue
  washes, glassmorphism, oversized rounded cards, decorative blobs.
- **Don't** stamp a vendor kit's identity onto an app. This system is
  under-designed on purpose so the app's own character can land on top.
- **Don't** use the accent as decoration or mood. It marks what you can
  act on and where you are; status has its own four tones.
- **Don't** put a shadow on a resting surface. Shadow means something has
  left the page plane.
- **Don't** make a section feel important by making it bigger. Weight and
  colour carry hierarchy before size; a card's `<h2>` is body-sized.
- **Don't** shrink control padding without re-measuring against WCAG
  2.2's 24px target-size minimum.
- **Don't** compose a heading, a paragraph and a button side by side in a
  flex row. Screens stack vertically; horizontal arrangement is reserved
  for the idioms that ship it.
