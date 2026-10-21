# A `day` with its own personality: monochrome, cobalt, crisp, natural depth and a light

Status: design, approved by Paul on 2026-10-20. The engineering constraints at the end were added after a cross-provider review. Not implemented.

The reference implementation is the mockup CSS and JS kept beside this spec, in `2026-10-20-day-depth/`:
- `monochrome.css`: the neutrals;
- `cobalt.css` and `crisp.css`: the accent and the corners;
- `depth.css`: natural depth;
- `light.css` and `light.js`: the light.

They were layered over today's `day` and `tokens.css` as overrides with `!important`. The implementation folds them into the theme and the structural stylesheet properly, and none of the `!important` survives.

## Why

`day` looks like Tito. It uses the system font, a standard blue (`#2464e0`) on white, soft 8px cards and grey hairlines. `DESIGN.md` asked for exactly that: a default that is "correct and unobjectionable everywhere rather than memorable anywhere".

Paul wants the default to read as professional, in the family of Stripe, Linear and Wise, and to have a personality that is recognisably Rastrillo's.

The choices were made through mockups, in this order:
1. Personality lives in `day` itself, not in a new theme or only in rastrillo.org.
2. It keeps the system font.
3. Base: Monochrome, the Linear-like option of three.
4. Colour schemes alone were not enough. Paul asked for blues and corner shapes, and for depth.
5. Blue and corners: cobalt, with crisp corners.
6. Depth: natural rather than stylised or "floaty". Light comes from above, objects rest on the page, and controls press in. Assertive but subtle.
7. Something alive: a light the page can feel, so that buttons catch a sheen and shadows lean with the light.
8. The details, each agreed in a later round:
   - the sheen balanced between light and dark;
   - card light that breathes rather than tracks;
   - a lit switch knob;
   - a quiet hover lift for list rows, menu items and sidebar items, with no sheen;
   - working on a phone.

## What ships

### 1. The `day` palette: monochrome neutrals with cobalt

The neutrals are near-greyscale, with hairline borders.

Cobalt is the accent:
- `#1f56d6` for text and links in light mode, `#7aa2ff` in dark.
- A filled control (primary button, checked switch) uses its own fill colour: `#2257df` in light, `#2f61e8` in dark. Both carry white text.

Today's dark `day` puts dark text on a light accent. The mockup showed that this reads as washed out on a depth-lit button. A mid-tone fill with white text keeps the button solid in both schemes.

That calls for one new token pair:
- `--rst-accent-fill`: the colour of a filled control.
- `--rst-on-accent-fill`: its text.

`tokens.css` reads them through fallbacks: `var(--rst-accent-fill, var(--rst-accent))` and `var(--rst-on-accent-fill, var(--rst-on-accent))`. `plain`, `signal` and any app's own theme keep working unchanged.

Every colour pair in `day` gets its contrast row recomputed in both schemes, in the table at the top of `day.css`, and the contrast gate holds it. White on both fills clears 4.5:1, but that must be computed, not assumed.

### 2. Crisp corners

`--rst-radius: 6px` for cards and `--rst-radius-sm: 4px` for controls. The pill radius is unchanged.

### 3. Natural depth: a theme axis

Depth stays a theme decision, as radius and shadow already are. `day` turns it on; `plain` and `signal` stay flat, which proves the axis works.

`day` defines the light. Each depth token has a light value and a dark value:

| Token | Light | Dark | What it is |
|---|---|---|---|
| `--rst-depth-hl` | white at 0.8 | white at 0.06 | the highlight along a raised edge |
| `--rst-depth-contact` | `rgb(16 24 40)` at 0.10 | black at 0.5 | a short contact shadow where an object meets the page |
| `--rst-depth-ambient` | `rgb(16 24 40)` at 0.06 | black at 0.35 | soft ambient light around it |
| `--rst-depth-face` | `1` | `1` | how much a filled face shades from top to bottom; `0` makes it flat |

`tokens.css` composes those tokens into each component's shadows and faces, falling back to today's flat values when the tokens are absent:

- **Buttons.**
  - A 1px top highlight, a 1px contact shadow and a 3px ambient shadow.
  - Filled buttons (primary and danger) shade top to bottom by about 6%.
  - Each type has its own hover and press. Filled buttons darken a shade. Plain buttons take a light grey and a darker border. Ghost buttons gain a fill.
  - Press removes the outer shadow and sinks the face with an inset shadow. Nothing moves, so there is no layout shift.
- **Cards** (`rst-box`, `rst-card`, `rst-stats`) rest on the page: a hairline border and a 1–2px ambient shadow. Nothing floats.
- **The current sidebar item** is a selected thumb, like a segmented control: a surface fill, a top highlight, a hairline edge, a contact shadow and an accent tick.
- **Menus** genuinely float, so they keep real but tight elevation. The selected item is tinted.
- **Fields** sit slightly into the page with a 1px inset shadow. Focus is an accent border plus a 3px accent halo.
- **The switch.**
  - The knob is near-white in both schemes, with a top highlight and a contact shadow. In dark mode the knob had read as a hole in the track.
  - The off track in dark mode is a mid grey, so the knob still stands out.
  - The on track is the accent fill.
- **List rows, menu items and the other sidebar items** get a quiet hover lift, with no sheen:
  - a 5% accent tint in light and 9% in dark, and a 1px top highlight;
  - a short shadow that lets the row rise slightly above the next one, eased over about 0.25s;
  - on a phone, a pressed row tints a little more and sinks;
  - a hovered sidebar item is a half-raised preview of the current item's thumb.

### 4. The light: `ui/light.js`

A new vendored enhancement, separate from `rastrillo.js` because `rastrillo.js` is near its 17 KB cap. It works the way `busy.js` and `select.js` do: an app can delete it, and the page is the static depth design without it.

It sets custom properties that the depth styles read. Without the script, each one keeps the default noted below, which is the static design.

**On `:root`:**
- `--rst-light-x` and `--rst-light-y`: the light's direction, each from -1 to 1. The default is light from straight above.

**On each button and switch:**
- `--rst-mx` and `--rst-my`: where the pointer is, in the element's own coordinates.

**On each card:**
- `--rst-card-x` and `--rst-card-y`: where its light is.
- `--rst-card-lit`: how lit the card is, from 0 to 1.

What responds to the light:

- **The light follows the pointer** (mouse only), eased rather than snapped:
  - shadows lean away from it by up to 1.5px horizontally and 1.2px vertically;
  - the top highlight leans towards it by up to 1px.
- **Buttons catch a sheen where the pointer is.**
  - Filled buttons take white light, at 0.15 alpha in light mode and 0.07 in dark, because white reads about twice as bright on a dark surface.
  - Plain buttons in light mode take a faint accent tint instead, because light cannot brighten white. In dark mode they take a whisper of white.
- **The switch knob is a small dome.**
  - Its highlight sits on the side the light comes from and brightens on hover.
  - When the switch is on, the track catches a faint sheen near the knob.
- **Cards breathe rather than track.** No colour follows the pointer.
  - The card's border catches the light on the side nearest the pointer: a soft blue-grey rim in light mode and a brighter edge in dark. In dark mode the surface also lifts faintly, by a 3% white wash.
  - The light drifts after the pointer at about a walking pace, fades in over about a second when the pointer enters, and fades out when it leaves.
- **With no pointer** (touch or keyboard), the light comes from the hour: from the left in the morning, overhead at noon, from the right in the evening. Touch also lights things directly:
  - a pressed button shows its sheen under the finger while held;
  - a tapped card catches the light briefly.
- **Under `prefers-reduced-motion`** nothing moves. The light stays overhead, and there is no sheen or easing.

`light.js` has no dependencies. It uses one `requestAnimationFrame` loop, which runs only while something is easing and stops when everything has settled. It never forces layout during pointer moves, apart from one `getBoundingClientRect` on the element under the pointer.

**No sensors.**
- The Ambient Light Sensor API is unavailable: only Chrome has it, behind a flag.
- Phone tilt (`DeviceOrientationEvent`) needs a permission prompt on iOS and a secure page. A design-system flourish must not trigger a permission dialog.

Both decisions are recorded here so nobody adds them later without asking.

### 5. `DESIGN.md`

The north star changes from "The Remixable Default" ("correct and unobjectionable everywhere rather than memorable anywhere") to a default with a point of view. Remixability stays the thesis: every new value is a custom property, and `plain` and `signal` prove a theme can switch depth off.

The new sections cover:
- the depth axis and its tokens;
- the light, and what it does and does not do;
- the hover and press language per component.

The frontmatter's colours, radii and component values update to the new `day`.

## What stays the same

- `tokens.css` structure, sizes and spacing. The type scale too, apart from the title and label values the theme now chooses (C15). The system font.
- `plain` and `signal`, which stay flat. The new tokens fall back to today's values.
- Apps' own theme copies. `rastrillo new` writes `static/theme.css` once and the app owns it. An existing app changes only if it re-copies, which `rastrillo doctor` already offers.

## Copy (Paul reviews before anything ships)

- The `DESIGN.md` north star and its new sections.
- The CHANGELOG entry.
- The docs changes:
  - the theme reference;
  - a short "The light" section listing what `light.js` does and how to remove it or turn it down (delete the file, or set `--rst-depth-face: 0` and the depth tokens to `transparent`);
  - the scaffold's mention of the vendored files.
- Any gallery prose: the tokens page and the theme sample.
- Translations of gallery prose, done after approval.

## Tests

Each test fails before its change and passes after.

**Contrast**
- Every new colour pair in `day`, in both schemes, through the existing contrast gate. That includes white on the accent fill, and the knob against both track states.

**Browser drives** (Chromium and WebKit where the repo already runs WebKit, otherwise Chromium)
- Pointer moves change `--rst-light-x` and `--rst-light-y` and the button's sheen position, and the light settles.
- `prefers-reduced-motion` leaves every value at its default.
- No script means the static design: the computed shadows equal the defaults.
- Every button type has a distinct hover and press. A filled button's hover keeps its fill and never turns grey; that regression happened in the mockups.
- Press causes no layout shift: the button's bounding box is unchanged.
- A hovered row and a hovered sidebar item change their computed background.
- A card's lit value rises and falls when the pointer enters and leaves.

**Accessibility and the gallery**
- axe in both schemes over the gallery: focus rings are still visible, and the hover tints keep text contrast.
- The sweep and frame heights: depth adds no height. Shadows don't change layout; check anyway.

**Budgets**
- `light.js` gets its own byte cap, as `busy.js` has. `tokens.css` and `day.css` sizes stay within their budgets, or raise them with a reason.

## Rollout

- One branch. CHANGELOG under a release section, with this note: "day looks different; your app's copy doesn't change until you re-copy it."
- rastrillo.org's gallery shows the new `day` after the merge and deploy.
- The mockup files stay beside this spec as the visual reference, until the implementation's gallery replaces them.

## Engineering constraints (from the cross-provider review, 2026-10-20)

GPT-6 Astra reviewed this spec against the code before planning. Its 17 findings are settled below. These rulings bind the plan and the implementation.

Three of them change the look Paul approved slightly, and they are marked ⚠:
- the switch's off track is darker;
- input borders stay at control-border contrast;
- the button sheen may be capped lower to keep text readable.

The type tweaks shown in the Monochrome mockup are included (C15).

### Token contract

**C1. Every theme declares every token.** `TestThemesDeclareIdenticalTokenSets` stays as it is. `plain` and `signal` declare the new tokens with values that keep them flat: transparent highlights and shadows, `--rst-depth-face: 0`, and no sheen. `tokens.css` holds no colour literals, so every colour comes from a token.

**C2. The full set of new tokens.** Each one is in all three themes.

| Token | What it controls |
|---|---|
| `--rst-accent-fill`, `-fill-hover`, `-fill-press`, `--rst-on-accent-fill` | filled primary controls: rest, hover and press, and their text |
| `--rst-danger-fill`, `-fill-hover`, `-fill-press`, `--rst-on-danger-fill` | danger buttons, independent of `--rst-tone-negative-*` (C3) |
| `--rst-control-face`, `-face-hover`, `-face-press`, `--rst-control-border` | plain buttons and menu summaries |
| `--rst-depth-hl`, `--rst-depth-contact`, `--rst-depth-ambient`, `--rst-depth-face` | the light from above (§3) |
| `--rst-knob`, `--rst-knob-shade`, `--rst-track-off` | the switch |
| `--rst-tint-hover`, `--rst-tint-press` | rows, menu items and sidebar items |
| `--rst-sheen-fill`, `--rst-sheen-plain`, `--rst-light-rim`, `--rst-light-wash` | the light's colours at full strength; `plain` and `signal` set them `transparent` |
| `--rst-title-size`, `--rst-title-weight`, `--rst-title-track`, `--rst-label-case`, `--rst-label-track`, `--rst-rail-bg` | the approved type and rail differences (C15) |

**Existing shadow tokens stay.** `--rst-shadow-pop`, `-knob` and `-lift` keep their current meanings and keep driving menus, modals, segmented tabs and popovers.

**Old app themes still work.** Wherever `tokens.css` reads a new token, it falls back to the old behaviour, for example `var(--rst-accent-fill, var(--rst-accent))`. An app that updates `tokens.css` but keeps an old `theme.css` renders as it does today.

**C3. Hover, press and danger are contracts, not filters.**
- `tokens.css` removes the primary hover's `--rst-accent-strong` and the danger hover filter. Every state reads its own token.
- White on `--rst-accent-fill` in every state is a contrast row. So is `--rst-on-danger-fill` on every danger state.
- Dark mode's danger uses a mid red with white text, as the mockup did, not the light `--rst-tone-negative-fg`.

### Accessibility

**C4. Contrast is checked on the composed face, not the token.** The contrast test computes the lightest point under a button's text: the fill at the top of its face gradient, plus the maximum sheen alpha. That point must clear 4.5:1.

⚠ If a value fails, the face gradient or the sheen cap comes down. The fill doesn't get lighter. The review computed 4.48:1 for a 15% white sheen on the light primary, so expect the cap to settle near 10–12% in light mode.

**C5. Functional boundaries keep 3:1.**
- Input and select borders stay at `--rst-line-strong`, which today passes at 3.63:1. ⚠ That is darker than the mockup's hairline.
- ⚠ The switch: the off track clears 3:1 against the surface, and the knob clears 3:1 against the off track. The off track darkens from the mockup's `#d5d9e0` to whatever passes.
- Plain buttons may keep the softer `--rst-control-border`. Their text label identifies them, and the boundary is decoration under WCAG 1.4.11. This is recorded in `day.css`'s contrast table, so nobody "fixes" it later.

**C6. Forced colours.** A `@media (forced-colors: active)` block in `tokens.css` covers what the browser strips:
- Buttons, cards and fields get real `ButtonBorder`/`CanvasText` borders.
- The switch knob is `ButtonText` on a `Canvas` track with a border.
- Selection and the current nav item use `Highlight` and `HighlightText`.
- The focus outline stays.
- `light.js` does nothing, because every effect is a background or a shadow, which forced colours already drop. It also skips its work entirely, to save the cost.
- Tested by emulating forced colours in the Chromium drive.

**C7. No new stacking contexts in rows.** A row's hover lift uses only inset shadows: a top highlight and a bottom shade inside the row. It never uses `position`, `z-index` or an outer shadow, because the `:where()` lifting rule in `tokens.css` exists to keep row menus' panels above the following rows.

The lift applies only to actionable rows, which are those that `tokens.css`'s existing `:has(> a.rst-nm, > a[rst-person])` filter already selects. A test opens a row menu and a `select.js` list across the following rows, with and without anchor positioning.

**C8. Reduced motion is total.**
- `light.js` does nothing under `prefers-reduced-motion: reduce`: no startup, no scheduling, no listeners writing styles.
- It listens for changes to that setting. When it turns on, it cancels the frame loop and clears its properties back to the defaults.
- The CSS transitions for sheen, rows and cards sit inside `@media (prefers-reduced-motion: no-preference)`.

**C14. The accessibility tests prove what they claim.**
- Interaction tests freeze animation through a test hook that sets the light and disables easing. They then assert:
  - focus outlines stay visible on every lifted or tinted component;
  - hover and press text contrast, computed on the composed face as in C4.
- The solid 2px focus outline stays. The accent halo is extra and never replaces it.

### `light.js`

**C9. Performance has a budget, and the budget is measured.**
- Coalescing: pointer moves are folded into the next animation frame. All reads (`getBoundingClientRect` for the button and the card) happen before any writes. Root properties are written only when they change by more than 0.01.
- One loop: a single `requestAnimationFrame` loop drives everything, and it runs only while something is easing.
- Budget: in Chromium, a drive on a 500-row list page moves the pointer for 2 seconds. Style recalculation plus paint must stay under 4 ms a frame on the CI runner; the plan fixes the exact figure after a first measurement.
- Fallback: if the measured cost can't meet the budget, the pointer-following root light is dropped in favour of the hour-only light. Per-element sheen stays.

**C10. Lifecycle.**
- Card state lives in a `WeakMap`. Entries for cards that have left the page are dropped.
- Easing runs on elapsed time, not a fixed per-frame step, so the speed is the same at 60 Hz and 120 Hz.
- The loop pauses on `visibilitychange` and resumes when the page is visible again.
- The hour is read again every 5 minutes.
- A key press or a touch switches out of pointer mode, and the light goes back to following the hour.

**C11. No `!important`, with each variant's states composed explicitly.** `tokens.css` edits its own component rules, not overrides on top. For each button variant, the plan writes a table of rest, hover, press, focus, disabled and busy, so specificity never decides the outcome. A pressed face replaces the hovered face, which the mockup got wrong.

**C12. Coverage, listed.** These components use the new language:
- **Buttons:** every `rst-btn` variant and size, including button-styled links.
- **Dropdowns:** `rst-dropdown` summaries and their menu `a` and `button` items, including `rst-menu-group` summaries.
- **Lists and menus:**
  - row-menu panel items;
  - `select.js` and combo options, on hover and when keyboard-active (`aria-selected` or the active descendant);
  - date-picker rows;
  - `rst-row` and actionable `rst-lrow`;
  - media-grid tiles, which already use the row-overlay pattern.
- **Navigation:**
  - shell nav items in the sidebar, topbar and console;
  - `rst-seg-tabs`;
  - `rst-pagination` links.
- **Fields:** every `rst-input`.
- **Switches:** `rst-switch`, including the `rst-tblock` variant.

Every rule covers the class form and the attribute form. Header exclusion uses `~="head"`, never `="head"`.

**C13. Right-to-left layouts and dragging.**
- The light's direction is physical, because the pointer is physical, so it doesn't mirror.
- The switch track's sheen follows the knob through `--rst-switch-x` and `--rst-switch-dir` rather than a fixed 70%.
- `light.js` never captures the pointer. While `rastrillo.js` owns a switch drag, `light.js` skips sheen updates on that switch.
- Tests cover a right-to-left drag, cancel and capture, and the `rst-tblock` variant.

### Scope and delivery

**C15. The Monochrome type differences are in.** These were in the mockups Paul approved:
- a smaller, tighter page title;
- a smaller box heading;
- stat labels in sentence case;
- the rail on the page background;
- a neutral header rule.

They become the C2 type and rail tokens. `plain` and `signal` declare today's values, so only `day` changes. The "What stays the same" section is amended to match: the type scale's sizes and spacing are unchanged, apart from these theme-chosen title and label values.

**C16. Delivery is part of the work.**
- `light.js` joins `vendoredNames` in `ui/vendored.go`, with its accessor and embed.
- It also goes into:
  - the shells' script tags (deferred);
  - the scaffold copy and `rastrillo new`'s pins;
  - `rastrillo doctor`'s frozen-file comparison;
  - the gallery's asset and preview paths, through `cmd/dsgen`.
- Deleting `light.js` from an app is documented: remove the file and its script tag. The doctor reports it as removed, not as drift.
- Tests:
  - a new scaffold;
  - new `tokens.css` with an old `theme.css`;
  - old `tokens.css` with a new `theme.css`;
  - an app with `light.js` deleted.

**C17. Print.** `@media print` turns off every shadow, gradient, sheen and tint. Cards and controls get real `--rst-line-strong` borders. Filled buttons print as outlined text. A Chromium print-emulation drive with backgrounds disabled checks for borders on cards, fields and buttons.

### Settled, so nobody looks again

- There is no spinner collision: `busy.js` inserts a real `<span rst-spin>`, and the sheen uses backgrounds.
- The pseudo-element to preserve is the small button's `::before` touch-target extension.

## Amendments from planning (2026-10-20)

Paul's rulings during planning, and the places where C1 to C17 met the code and one of them had to give. Each binds the plan (`docs/superpowers/plans/2026-10-20-day-depth.md`) and the implementation. Where an amendment changes a constraint above, the amendment wins.

**A1. Tap highlight (adds to C12).** On iOS a tapped row link flashes the browser's own tap highlight, a different grey from the row's press tint, and only on the link. Every component with its own press state sets `-webkit-tap-highlight-color: transparent`: buttons, rows (`rst-row`, `rst-lrow`, media tiles) and their links, nav items, dropdown summaries and menu items, row-menu items, seg-tabs, pagination, combo options, date-picker rows, switches and `rst-tblock` heads. A row takes its press tint while its overlay link is `:active`, through `:has(> a.rst-nm:active, > a[rst-person]:active)` beside the existing actionable-row filter (`rst-row` through `:has(> [rst-row-main] > a:active)`). A coarse-pointer drive checks the computed tap-highlight colour and the row's background while the link is active.

**A2. The press is stronger than the hover, instant, and slow to leave.** Amends §3's "Nothing moves" to: no layout moves; a pressed button may translate by 1px. Amends §4's touch bullet: a pressed button shows the press, not the sheen. On `:active`, every button variant:
- drops 1px with `translate: 0 1px` (a transform, so nothing around it moves; none under reduced motion);
- loses its top highlight and outer shadow for a deeper inset shadow;
- switches its sheen off;
- darkens through two overlay layers driven by a registered property, `@property --rst-press { syntax: "<number>"; inherits: false; initial-value: 0; }`: a whole-face wash and a 70px radial spot at the press point (`--rst-px`, `--rst-py`, which `light.js` sets on `pointerdown` for mouse and touch; the centre without it). The spot is stronger than the wash, so it fades last.
- `:active` sets `--rst-press: 1` with `transition: none`. Release fades it: `--rst-press 0.7s cubic-bezier(0.2, 0.6, 0.3, 1)`, `translate 0.3s`, `box-shadow 0.45s`. Under reduced motion there is no transition and no translate.
- The strengths are number tokens set per scheme: `--rst-press-wash` and `--rst-press-spot` for filled buttons (light 0.08 and 0.16, dark 0.18 and 0.30), `--rst-press-wash-plain` and `--rst-press-spot-plain` for plain buttons (light 0.04 and 0.07, dark 0.18 and 0.30). Ghost buttons press to `--rst-control-face-press`.
- A drive presses, checks `--rst-press` is 1, releases, and checks the value at about 250ms is between 0.1 and 0.6; under reduced motion it is 0 right after release.
- Press faces are contrast rows, and C4's composed-face check covers the darkest press point as well as the lightest sheen point.

**A3. iOS `:active` needs a touch listener, and `rastrillo.js` owns it.** iOS applies `:active` on tap only when a `touchstart` listener exists. `rastrillo.js` registers one passive, empty `touchstart` listener on the document, so an app that deletes `light.js` keeps its pressed states.

**A4. Per-scheme numbers, and what Safari refused.** `light-dark()` takes colours only, and `color-mix()` with `calc(var())` in a percentage blanked every button in Safari (the whole `background-image` went invalid). The rules: no `calc()` with `var()` inside a `color-mix()` percentage; no `light-dark()` around a number; a per-scheme number is a number token declared in the theme's `:root` (the light value) and restated for dark in two scheme blocks at the end of the theme file, `:root[data-theme="dark"] { … }` and `@media (prefers-color-scheme: dark) { :root:not([data-theme="light"]) { … } }`. Those blocks may hold numbers only; the theme-format gate (`ui/ui_test.go`) is amended to allow exactly that and nothing else. The alpha composition is `rgb(0 0 0 / calc(var(--rst-press) * var(--rst-press-spot)))`. The sheen is switched off while pressed by selector (`:hover:not(:active)`, inside `@media (hover: hover)`), so no colour carries a `(1 - press)` factor. A Chromium drive checks a primary button's computed `background-image` at rest includes its face gradient, which catches whole-declaration invalidation; WebKit is not in this repo's harness, so the Safari check itself is done by hand and recorded in the branch description.

**A5. C1 relaxed for ink and light.** `tokens.css` may paint pure black or pure white at an alpha (`rgb(0 0 0 / …)`, `rgb(255 255 255 / …)`) where the strength is a number token or a fixed fraction: the press layers and the face shade. Every hue still comes from a token, and the colour-literal gate exempts those two spellings only.

**A6. C2 amended.** The table changes as follows.
- Dropped: `--rst-accent-fill-press`, `--rst-danger-fill-press`. The press is an overlay (A2), not a swapped face.
- Added: `--rst-press-wash`, `--rst-press-spot`, `--rst-press-wash-plain`, `--rst-press-spot-plain` (numbers, per scheme, A2); `--rst-heading-size` (C15's smaller box heading, which the table had no token for); `--rst-nav-current-bg` and `--rst-nav-current-fg` (the current nav item's fill and text, so `plain` and `signal` keep today's accent-soft item while `day`'s is a surface thumb).
- `--rst-depth-face` is the fraction a filled face shades by, 0.04 in `day` (both schemes) and 0 in `plain` and `signal`, not 1 or 0.
- `--rst-control-face-press` is the ghost button's press face.
- Flat themes declare `--rst-light-rim` as their `--rst-line` colour, not `transparent`: the rim gradient falls back to the line, and a transparent rim would cut a hole in the border.

**A7. C4's figures.** Measured on the composed face: a face shade of 0.04 white at the top, a sheen cap of 0.08 in light and 0.04 in dark, and the dark danger fill darkened from the mockup's `#cf3a3a` to `#c43232` (white on `#cf3a3a` plus the shade and the sheen was 4.33:1; the fill got darker, never lighter). Light primary composes to 4.78:1, light danger 4.61:1, dark primary 4.58:1, dark danger 4.87:1. The spec's expectation of a 10 to 12% light sheen did not survive the light danger fill, which is the binding pair.

**A8. C8's form.** Transitions are declared where the component is and disabled with an exact-selector `transition: none` inside `@media (prefers-reduced-motion: reduce)`, the shape `TestReducedMotionDisablesEveryTransition` already enforces, rather than wrapped in a `no-preference` block. The effect is the one C8 asks for: nothing moves and nothing eases under reduced motion.

**A9. The card's light fades by radius.** The rim and the wash cannot scale a colour's alpha by `--rst-card-lit` without the construct A4 bans, so the lit radius scales instead: `radial-gradient(calc(var(--rst-card-lit) * 260px) circle at …)`. At 0 the card shows its hairline; as the value eases up the lit rim grows out from under the pointer, which reads as a fade.

**A10. Switches do not read `--rst-mx`/`--rst-my`.** The knob's highlight follows the light direction and the track's sheen follows the knob (C13), so `light.js` writes the pointer position on buttons only.

**A11. The current nav item has no accent tick.** §3 names one; the approved mockup never drew one. The thumb is the mockup's: a surface fill, a top highlight, a hairline ring and a contact shadow.

**A12. Dropdown summaries that become buttons.** The mockup gave `[rst-dropdown] > summary` a plain-button face. In the shells that selector also matches the topbar and console account menus and the sidebar profile, and a dropdown nested in a menu is one of that menu's rows. The face applies to a dropdown's summary except `[rst-shell-account]`, `[rst-shell-profile]` and any dropdown inside `[rst-dropdown-menu]`.

**A13. What changes in `plain` and `signal`.** They stay flat (transparent highlights and shadows, face 0, no sheen, no rim), and three things still change, because the structural rules changed: a plain button's hover no longer turns its border and label accent (it reads `--rst-control-face-hover`, which they declare as their surface, and `--rst-line-strong`); buttons, rows and nav items take the press language (translate, wash, press tint); and a list-bar dropdown's summary wears a button face. Nothing else moves. Paul can set the press numbers to 0 in those two themes if he wants them stiller.

**A14. Cards include the list card.** `rst-list` is a card by DESIGN.md's own words and sits beside `rst-box` on the same pages, so it takes the rest shadow and the light with `rst-box`, `rst-card` and `rst-stats`.

**A13, re-ruled (Paul, 2026-10-20, after Astra's review of the plan).** `plain` and `signal` render exactly as today. Their new tokens are today's values, their press strengths are 0, their press drop is 0px, and a pixel state matrix in the plan proves it on every component in every state. The three changes A13 listed are withdrawn.

**A15. C2's old-theme promise is pixel-exact, and costs eighteen tokens.** "Renders as it does today" with an old theme means every state of every component, so every token is read through a fallback that is today's value, and where today's value had no token to fall back to, one is added: `--rst-accent-fill-edge` and `-edge-hover`, `--rst-danger-fill-edge` and `-edge-hover` (the filled borders, which were the accent and accent-strong), `--rst-on-danger-fill-hover` and `--rst-danger-hover-filter` (the old filter darkened the whole rasterised button, label and edges included, and no colour reproduces its corners pixel for pixel: the filter stays as a token, `none` in `day` and `brightness(0.9)` elsewhere, with `day` reading its own hover fill under it; measured: the filter gives `rgb(166, 25, 25)` for `#b91c1c`, a linear-space mix `rgb(176, 26, 26)`, and an sRGB mix the same solid colour with different corners), `--rst-control-border-hover` and `--rst-control-text-hover` (a plain button's hover border and label, which were the accent), `--rst-ghost-edge`, `--rst-ghost-face-hover` and `--rst-ghost-edge-hover` (a ghost kept the base border, hovered to no fill and `--rst-line-strong`), `--rst-summary-face`, `--rst-summary-face-hover`, `--rst-summary-edge` and `--rst-summary-edge-hover` (a dropdown summary's face, which was none; the edge is a ring in the shadow, never a border, so a transparent edge is not 1px of layout), `--rst-press-drop` (0px), `--rst-focus-edge` and `--rst-focus-halo` (a field's focus border and halo, which were `--rst-line-strong` and none), `--rst-row-tint-hover` and `--rst-row-tint-press` (a list-row-action row hovers to `--rst-surface-2`, not the accent tint), and `--rst-tint-tap` (a touch press with no hover paints nothing today, in any theme, so every press rule is two rules: `:hover:active` to the press tint inside `@media (hover: hover)`, `:active` to the tap tint outside it). Segmented tabs and pagination chips get no press rule: a pressed tab has no fill today. The press rules set no border and no label colour, so a pressed button keeps its hover's or its rest's. The plan's matrix holds the old `day`, `plain` and `signal` under the new `tokens.css` to the old pair, pixel for pixel, in both schemes, with a negative control that proves it looks.

**A16. C4's darkest point includes the face's black endpoint.** The face shade darkens the bottom by `--rst-depth-face` as it lightens the top, so the darkest point is the hover fill under black at 0.04, then the wash, then the spot: light primary 10.23:1, dark primary 12.32:1, light danger 9.68:1, dark danger 12.71:1.

**A17. The media blocks flatten the axis itself.** Forced colours and print set the depth, sheen, press, tint, drop and halo tokens flat on `html:root`, which outranks a theme's `:root`, so every rule in every state paints flat with no specificity contest; the rules they add (system-colour borders, the knob in `ButtonText`, the highlight pair, outlined text on a filled button) use the shell-qualified selectors the states use, so they win by order.

**A18. The remaining rulings.** No accent tick on the current nav item (A11 stands). The darker dark danger fill (`#c43232`) and the 0.08 light sheen cap stand; both are contrast-driven. No WebKit in the Go harness and no Playwright in the repo: the Chromium drive holds a filled button's resolved `background-image` to the full four-layer stack, the fill and the label, with a negative control that severs the face, so a dropped declaration is a shorter list; the controller does the Safari check out of band. The plan lands nothing: it ends green, pushed, described and reviewed, and the merge is Paul's word. Copy is drafted in place and ledgered as it lands; the one gated review and the translations come after everything else.
