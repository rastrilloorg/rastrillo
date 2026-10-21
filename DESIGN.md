---
name: Rastrillo
description: A quiet, correct interface baseline built to be taken over.
colors:
  bg: "#fbfbfc"
  surface: "#ffffff"
  surface-2: "#f3f3f5"
  line: "#e6e6ea"
  line-strong: "#868691"
  text: "#18181b"
  text-muted: "#52525c"
  text-faint: "#6b6b75"
  accent: "#1f56d6"
  accent-strong: "#1844ac"
  accent-soft: "#ecf1ff"
  on-accent: "#ffffff"
  accent-fill: "#2257df"
  on-accent-fill: "#ffffff"
  danger-fill: "#c62f2f"
  control-border: "#d0d4dc"
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
    fontSize: "1.25rem"
    fontWeight: 600
    lineHeight: 1.25
    letterSpacing: "-0.012em"
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
    letterSpacing: "0"
    textTransform: "none"
  field:
    fontFamily: "system-ui, -apple-system, \"Segoe UI\", Roboto, \"Helvetica Neue\", Arial, sans-serif"
    fontSize: "1.0625rem"
    fontWeight: 550
  stat:
    fontFamily: "system-ui, -apple-system, \"Segoe UI\", Roboto, \"Helvetica Neue\", Arial, sans-serif"
    fontSize: "1.5rem"
    fontWeight: 650
    lineHeight: 1.1
  mono:
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace"
    fontSize: "0.78125rem"
rounded:
  bar: "2px"
  sm: "4px"
  md: "6px"
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
    backgroundColor: "{colors.accent-fill}"
    textColor: "{colors.on-accent-fill}"
    typography: "{typography.title}"
    rounded: "{rounded.sm}"
    padding: "0.45rem 0.75rem"
  button-primary-hover:
    backgroundColor: "#1d4dc8"
    textColor: "{colors.on-accent-fill}"
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

**Creative North Star: "A Default With a Point of View"**

This is the interface an application has before anyone has styled it,
and it should look like someone chose it. `day` is near-greyscale with
hairline borders, one cobalt for anything that acts, crisp corners, and
natural depth: light comes from above, cards rest on the page, controls
press in and slowly lift. It aims to read as professional, in the family
of the tools people already trust, and to be recognisably this system's.

Remixability is still the thesis. Every colour, radius, shadow,
highlight and strength resolves through a custom property, and the three
shipped themes prove the axes work rather than offering a menu of three
answers: `day` turns depth on, `plain` and `signal` turn it off and
render as they always did. An app that wants a fourth writes one `:root`
block. Files are delivered once and app-owned from that moment: edit
them, or delete what you do not use.

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

- **Interface Blue** (`#1f56d6` light, `#7aa2ff` dark): the accent, a
  cobalt. It marks what you can act on and where you are: focus rings,
  links, an active tab, the current menu item. It belongs to the chrome,
  not to the content.
- **Accent Fill** (`#2257df` light, `#2f61e8` dark): the primary
  button's fill, with a hover fill of its own (`#1d4dc8` light,
  `#2956d0` dark) and a white label (`on-accent-fill`). A filled control
  never darkens by filter: every state is a colour, gated.
- **Danger Fill** (`#c62f2f` light, `#c43232` dark): the danger button's
  fill, the same way, with a white label.
- **Interface Blue Deep** (`#1844ac` light, `#a3c0ff` dark): kept for a
  theme written before the fills; `tokens.css` reads it only as the
  primary hover's fallback.
- **Interface Blue Wash** (`#ecf1ff` light, `#16234a` dark): the tint
  behind a current menu item, and the fallback for the hover and press
  tints in a theme that declares none. Carries no text of its own beyond
  the accent and the muted greys, both gated against it.

### Neutral

- **Page** (`#fbfbfc` light, `#0c0d0f` dark): the page ground, one step
  below the surface, so a card reads as resting on it.
- **Surface** (`#ffffff` light, `#141518` dark): cards, panels, inputs
  and menus.
- **Surface Sunken** (`#f3f3f5` light, `#18191d` dark): the recessed
  step, for table header rows, the second tone in a stack and a hovered
  ghost button.
- **Hairline** (`#e6e6ea` light, `#26282e` dark): every divider, card
  edge and table rule.
- **Control Edge** (`#868691` light, `#71737c` dark): the border of a
  field and a select. Deliberately much darker
  than the hairline, and held at 3:1 against its backgrounds because it
  is a control boundary (WCAG 1.4.11), not decoration.
- **Control Border** (`#d0d4dc` light, `#343843` dark): a plain button's
  edge, softer than the field border on purpose. The label identifies
  the button, so the edge is decoration under 1.4.11 and carries no 3:1
  floor.
- **Ink** (`#18181b` light, `#ededf0` dark): body text.
- **Ink Muted** (`#52525c` light, `#a1a1aa` dark): secondary text,
  subheads, cell values that are not the row's name.
- **Ink Faint** (`#6b6b75` light, `#8c8c96` dark): hints, captions,
  counts. Still gated at 4.5:1; "faint" is a role, not a licence to
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
**Mono Font:** `ui-monospace, SFMono-Regular, Menlo, Consolas,
monospace`, on the `rst-mono` utility. It is a real second family and
not a weight of the first — reference codes, identifiers and machine
values are set in it so they read as things to be copied rather than
read.

**Character:** the type has no personality of its own on purpose. The
family is a theme axis, and the shipped default is whatever the reader's
OS considers correct — which is the right answer for an application
frame and the wrong answer for a brand, so an app that wants a voice
sets one property.

### Hierarchy

- **Display** (650, 2.1rem, 1.1): the lead reading in a stat band, and
  nothing else. The only genuinely large type in the system.
- **Headline** (600, 1.25rem, 1.25, −0.012em in `day`; 1.375rem and
  −0.015em in `plain` and `signal`): a screen's `<h1>` in the page
  header. Capped at 1.25rem under 34rem.
- **Field** (550, 1.0625rem = 17px, inherited 1.5): the form's headline
  input — a title, a name — and the only control that leaves the body
  step. Its label stays at Label size, so the emphasis is carried by
  the reader's own words rather than by the form's furniture.
- **Title** (600, 0.875rem, 1.4): section headings inside a card head,
  button labels, a row's name. A section `<h2>` separates by weight, not
  size: body-sized in `plain` and `signal`, a step below it (0.8125rem)
  in `day`.
- **Body** (400, 0.875rem = 14px, 1.6): everything else. Subheads cap at
  44rem.
- **Label** (650, 0.71875rem = 11.5px): the eyebrow over a value, such
  as stat labels, table column heads and the nav rail's section names.
  Sentence case with no tracking in `day`; uppercase at +0.06em in
  `plain` and `signal`.

### Steps outside the roles

Four literal sizes appear in `tokens.css` outside the roles above, and
they are recorded rather than tidied away — two have since become roles,
which is what recording them was for:

- **1.5rem** — a stat's number when it is not the lead. A role now
  (`stat`), because a dashboard uses it as one.
- **1.25rem**: the headline's cap under 34rem, and `day`'s headline at
  every width. A step of `headline` rather than a size of its own.
- **1.0625rem** — a role now (`field`, and `--rst-fs-lg`), because a
  form's headline input reads as one. The large person avatar's initials
  and the shell's brand land on the same value and stay literals: they
  are sized to fit a circle, so the match is a coincidence rather than a
  shared step, and tying them to the type scale would move them the next
  time the scale moves.
- **0.6875rem** — the ordinary person avatar's initials.

That last one is decoration sized to fit a circle, not type a reader
parses, which is why it is not a role.

### Named Rules

**The Small-Base Rule.** The base is 14px, not 16px, and every step is
declared in `rem` so the whole scale tracks a reader who has raised
their browser default. Do not convert any of it to `px`.

**The Weight-Not-Size Rule.** Hierarchy inside a screen is carried by
weight and colour before size. A card's `<h2>` is body-sized at 600;
reaching for a larger size to make a section feel important is how this
system loses its density.

The one exception is a form's headline field (the Field role above),
and it is an exception because of what is being sized. Everywhere else
the rule governs *chrome* — a heading the system wrote, labelling
content underneath it — and chrome has no business growing. A title
input is the opposite case: the large thing is what the reader typed,
which is the only content on a form that is not furniture. The
exception is deliberately narrow, one control on one form; a second
large field on the same screen is the density loss this rule is about.

**The Grouped Number Rule.** Every number a person reads as a quantity is
grouped for their locale — `54,173` in English, `54.173` in German,
`٥٤٬١٧٣` in Arabic. Never a bare run of digits. Grouping is not "add a
comma": the separator, the group size and the digits themselves all
change with the locale, and a number's readability is the whole reason
the rule exists — `54173` is counted, `54,173` is read.

The exception is exact and small: **an identifier is not a quantity.**
Reference codes, order numbers, years, versions and port numbers are
labels that happen to be made of digits, and grouping one changes what it
appears to be. Order 4471 is not order 4,471.

The rule is met. `{{number $L .Count}}` groups a quantity and `{{money $L .Amount .Currency}}` writes money, both on the server, in the reader's language, from CLDR data. In the browser, `Intl` is at most an enhancement.

Money has two rules of its own. The currency always comes from the data, never from the reader: an amount is stored as a whole number of its currency's smallest unit, with its ISO 4217 code beside it, so a price in euros is in euros on every page. And every digit of that smallest unit is shown: 1.5 dinars is `KWD 1.500`, never `KWD 1.5`.

Each language's formatting data is a pack an app links only when it offers that language, so a language costs nothing until then. Templates are built once, and the localizer in each page's data chooses the language, so one request's language never reaches another's page.

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

Depth is a theme axis, and `day` turns it on. The light comes from
above: a 1px highlight along a raised edge (`--rst-depth-hl`), a short
contact shadow where an object meets the page (`--rst-depth-contact`),
soft ambient light around it (`--rst-depth-ambient`), a face shade for
filled controls (`--rst-depth-face`, 0.04 in `day`), and a 1px drop for
a pressed button (`--rst-press-drop`). Buttons are a touch proud of the
page and press in; cards rest on it; menus float, with tight elevation;
fields sit slightly into it. `plain` and `signal` set every one flat,
and a theme written before the axis is read through fallbacks that are
today's values.

### Shadow Vocabulary (the `day` theme)

- **Pop** (`0 2px 4px light-dark(rgba(16,24,40,0.06), rgba(0,0,0,0.35)),
  0 8px 16px -4px light-dark(rgba(16,24,40,0.1), rgba(0,0,0,0.5))`): an
  open menu, a dropdown panel, a modal: surfaces that float over the
  page. Real elevation, kept tight.
- **Knob** (`0 1px 2px light-dark(rgba(16,24,40,0.18), rgba(0,0,0,0.4))`):
  the moving part of a control, such as a switch's knob.
- **Lift** (`0 1px 2px light-dark(rgba(16,24,40,0.08), rgba(0,0,0,0.3))`):
  the barely-there separation on a raised strip.
- **Overlay** (`light-dark(rgba(0,0,0,0.45), rgba(0,0,0,0.6))`): the
  modal scrim. Deeper in dark on purpose: black reads lighter against a
  dark backdrop and needs more alpha to still dim.

## The light

`light.js` moves the light. The shadows' light comes from the hour: the
left in the morning, overhead at noon, the right in the evening, so
shadows lean away from it by up to 1.5px and the top highlight leans
toward it. With a mouse, a button catches a sheen where the pointer is
(white on a filled button, a faint accent tint on a plain one in light
mode), and a card's rim brightens near the pointer over about a second
and fades when it leaves. On a phone a pressed button shows the press,
not the sheen, and a tapped card catches the light briefly. The whole
page's light can follow the pointer instead: set `FOLLOW` to `true` at
the top of the file. It is off because every write of it restyles the
page.

It does not run under reduced motion, does nothing under forced colours,
reads no sensor, and never captures the pointer. For the still design,
delete the file and its script tag, and name it in `vendoredIsMine` in
the app's pin test; `rastrillo doctor` then lists it as yours, a
recorded edit, and `--fix` leaves it alone.

## Shapes

Soft rectangles, and the exact softness is a theme axis: `day` uses 6px
on cards and 4px on controls, `plain` 4px and 3px, `signal` 4px and 2px.
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
per theme: it is `color-mix(in oklab, var(--rst-accent) N%, var(--rst-line))`,
mixed at 45% in `signal`, and left as the plain hairline in `plain` and
in `day`, which keeps its cobalt for things that act. A theme that
changes its accent gets a matching rule with no second value to keep in
step. It is decorative and carries no contrast floor.

## Components

### Hover and press

Hover is quiet and press is clear. A plain button's hover is a light
grey face and a darker border; a filled button's is a deeper fill; a
ghost's is a surface tint. Rows, menu items and sidebar items lift: a 5%
accent tint (9% in dark), a highlight along the top edge, no sheen. The
current sidebar item is a selected thumb, and hovering another is a
half-raised preview of it. These tints and lifts apply only on a
device that can hover, so a tap leaves no tint behind; the phone index
rows keep their tap tint on purpose.

A press is instant and slow to leave. The button drops 1px, loses its
highlight and outer shadow for an inset one, switches its sheen off and
darkens, most under the pointer; on release the darkness lifts over
about 0.7s, the spot under the finger last. A row pressed, or its link
tapped, takes a stronger tint and sinks. Under reduced motion nothing
moves and nothing fades.

### Buttons

- **Shape:** crisp (`{rounded.sm}`, 4px in `day`).
- **Default:** control face, control-border edge (softer than the field
  border on purpose), Ink label at 600 and 12.5px, `0.45rem 0.75rem`
  padding. Hover: a light grey face and a darker border.
- **Primary:** the accent fill with a 4% top-to-bottom shade and a white
  label; hover deepens the fill.
- **Danger:** the danger fill, the same way.
- **Ghost:** no fill, muted label; hover a surface tint, with the label
  darkening to Ink rather than picking up the accent. The accent means
  "this goes somewhere new", and a ghost Cancel does not.
- **Press:** every variant sinks and darkens; see Hover and press.
- **Focus:** 2px accent outline, 2px offset.

### Cards / Containers

- **Corner Style:** `{rounded.md}` (6px in `day`).
- **Background:** Surface, on a Page ground one step below it, with the
  hairline painted as the outer stop of the rim gradient. To recolour a
  card, set `--rst-surface` on it, which also recolours the surfaces
  inside it; a `background` of your own paints over the hairline and a
  `background-color` sits under the face.
- **Shadow Strategy:** at rest, a hairline and two ambient layers lit
  from above; the rim catches the light near the pointer. Menus float
  with real but tight elevation.
- **Internal Padding:** `1.1rem 1.25rem`. A card head (`rst-box-head`)
  sits *outside* the card, holding an `<h2>` and one compact button.
- **Note:** list cards and data grids hold rows only and are unpadded by
  design; prose, forms and links go in a padded box.

### Inputs / Fields

- **Style:** Surface fill, 1px Control Edge (`--rst-line-strong`)
  border, a 1px inset shadow so the field sits into the page,
  `{rounded.sm}`, `0.45rem 0.6rem` padding, full width by default.
- **Focus:** the accent border, a 3px accent halo, and the 2px outline
  offset 2px.
- **Error:** the message is text, and the field is described by it via
  `aria-describedby`; the required marker uses the negative tone.

### Navigation

The rail sits on the page background. The current item is a surface
thumb with a top highlight, a hairline ring and a contact shadow, marked
by `aria-current` in the markup; a hovered item is the thumb's
half-raised preview. Below 800px the rail collapses into a
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
- **Do** group the digits of any number a person reads as a quantity, for
  their locale — and never group an identifier, a year or a version.
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
- **Don't** float a resting surface. A card rests on the page with the
  theme's ambient light; a pop shadow means something has left the page
  plane.
- **Don't** make a section feel important by making it bigger. Weight and
  colour carry hierarchy before size; a card's `<h2>` is body-sized.
- **Don't** shrink control padding without re-measuring against WCAG
  2.2's 24px target-size minimum.
- **Don't** compose a heading, a paragraph and a button side by side in a
  flex row. Screens stack vertically; horizontal arrangement is reserved
  for the idioms that ship it.
