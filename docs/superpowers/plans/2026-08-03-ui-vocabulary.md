# UI Vocabulary Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the rest of the §9 component vocabulary in `rastrillo/ui` — 12 new partials and 7 documented class idioms — plus the deferred localization threading (`T` in `ui.Funcs()`, framework base catalog wired into `Locales`).

**Architecture:** Extends the existing `ui` package in its own conventions: leaf components as `{{define}}` partials under `ui/partials/`, structural containers as documented CSS class idioms in `tokens.css`, everything `rst-`-prefixed on the `--rst-*` tokens, zero JavaScript, both themes, WCAG AA. Spec: `carlosframework/platform` `docs/superpowers/specs/2026-08-03-rastrillo-ui-vocabulary-design.md`. Source vocabulary: titogo's shipped design system (already surveyed; all markup in this plan is the re-skinned result — implementers need no access to titogo).

**Tech Stack:** Go stdlib (html/template, embed), no new dependencies.

## Global Constraints

- Zero JavaScript. Every interactive behavior is native HTML: `<details>` (+ `name` attribute for exclusivity), CSS `:has()`, form GET/POST round-trips.
- Every class any partial or documented sample emits must be styled in `ui/tokens.css`; tokens.css must carry no new `rst-` selector nothing emits.
- Every `transition`/`animation` added has a `@media (prefers-reduced-motion: reduce)` disable.
- Every interactive element (summary, checkbox, icon-only link) has an accessible name (visible text, `aria-label`, or `sr-only` span). Decorative graphics are `aria-hidden="true"`.
- Colors: only `--rst-*` custom properties — never raw hex in partial markup; new hex values may appear only in `tokens.css` token definitions. Text-on-background pairs ≥ 4.5:1, UI-component-on-adjacent ≥ 3:1, in BOTH light and dark (Task 5 adds the computed check).
- All strings a partial renders are caller-supplied except defaults, which go through the `T` template func (added in Task 5; Tasks 1–4 hardcode English defaults exactly where noted and Task 5 converts them).
- Sweeps before every commit: root `go build ./... && go vet ./... && go test ./... -count=1`, `gofmt -l` clean; `GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod`.
- File layout mirrors usage (a partial only its sibling calls is colocated); comments state constraints and reasons, never diff narration.
- The existing 8 partials and their tests keep passing untouched except where a task explicitly names them.

---

### Task 1: Icons + display partials (badge, meter, person, callout, detail-list)

**Files:**
- Modify: `icons.go` (root package — add seven Lucide constants to the existing map/switch structure; match the existing code shape exactly)
- Create: `ui/partials/badge.html`, `ui/partials/meter.html`, `ui/partials/person.html`, `ui/partials/callout.html`, `ui/partials/detail-list.html`
- Modify: `ui/tokens.css` (append the component blocks below in the file's existing section style)
- Modify: `ui/ui_test.go` (fixtures + assertions)

**Interfaces:**
- Consumes: `rastrillo.Icon(name)` via the `icon` template func; existing `--rst-*` tokens.
- Produces: partials named `badge`, `meter`, `person`, `callout`, `detail-list`; icon names `kebab`, `x`, `info`, `check-circle`, `alert-triangle`, `x-circle`, `help-circle`. Tasks 2–4 rely on the icon names; Task 5 rewrites nothing here except noted defaults.

- [ ] **Step 1: Icons.** Open `icons.go`, note how the existing icons (`check`, `chevron-down`, `plus`, `search`) are declared and registered, and add these seven in the identical style (24×24 Lucide, `fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"`):

```
kebab         <circle cx="12" cy="12" r="1"/><circle cx="12" cy="5" r="1"/><circle cx="12" cy="19" r="1"/>
x             <path d="M18 6 6 18"/><path d="m6 6 12 12"/>
info          <circle cx="12" cy="12" r="10"/><path d="M12 16v-4"/><path d="M12 8h.01"/>
check-circle  <circle cx="12" cy="12" r="10"/><path d="m9 12 2 2 4-4"/>
alert-triangle <path d="m21.73 18-8-14a2 2 0 0 0-3.48 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.73-3Z"/><path d="M12 9v4"/><path d="M12 17h.01"/>
x-circle      <circle cx="12" cy="12" r="10"/><path d="m15 9-6 6"/><path d="m9 9 6 6"/>
help-circle   <circle cx="12" cy="12" r="10"/><path d="M9.09 9a3 3 0 0 1 5.83 1c0 2-3 3-3 3"/><path d="M12 17h.01"/>
```

Add a test in the file that tests icons (find it via `grep -rn "Icon(" --include="*_test.go" .`) asserting each new name resolves to non-empty SVG containing `<svg` and `aria-hidden` handling identical to existing icons, and that an unknown name still behaves as before.

- [ ] **Step 2: Partials.** Create each file with a doc-block comment in the established style (see `ui/partials/pagination.html` for the voice: what it is, the keys, the rules it encodes).

`ui/partials/badge.html`:
```
{{/* badge — an uppercase bordered chip for small static markers (LIVE,
     Draft, Sold out): distinct from status-pill, which is record status.
     Keys:
       Label  string, required — the visible text
       Tone   string, optional — positive | warning | negative | neutral;
              omitted means the quiet default (border + muted text). */}}
{{define "badge"}}<span class="rst-badge{{with .Tone}} rst-badge--{{.}}{{end}}">{{.Label}}</span>{{end}}
```

`ui/partials/meter.html`:
```
{{/* meter — a capacity bar with its number: 412/500 next to a 4px fill.
     The fraction is ALWAYS text — a bar never carries the number alone.
     Keys:
       Percent  int, required — fill width, clamped here to 0..100
       Text     string, required — the fraction, already formatted */}}
{{define "meter"}}{{$p := .Percent}}{{if lt $p 0}}{{$p = 0}}{{end}}{{if gt $p 100}}{{$p = 100}}{{end}}<span class="rst-meter"><span class="rst-meter__bar"><i style="--rst-meter-fill: {{$p}}%"></i></span><span class="rst-meter__num">{{.Text}}</span></span>{{end}}
```

`ui/partials/person.html`:
```
{{/* person — initials avatar + name + email, the identity cell for
     record types that are people; Large is the Show-header size.
     The avatar is decoration (aria-hidden) — the text carries identity.
     Keys:
       Href     string, required — where the person's record lives
       Name     string, required
       Email    string, optional
       Initial  string, optional — defaults to nothing rendered in the
                avatar when empty (the dashed empty-avatar state)
       Large    bool, optional */}}
{{define "person"}}<a class="rst-person{{if .Large}} rst-person--lg{{end}}" href="{{.Href}}"><span class="rst-person__av{{if not .Initial}} rst-person__av--empty{{end}}" aria-hidden="true">{{.Initial}}</span><span class="rst-person__meta"><span class="rst-person__name">{{.Name}}</span>{{with .Email}}<span class="rst-person__email">{{.}}</span>{{end}}</span></a>{{end}}
```

`ui/partials/callout.html`:
```
{{/* callout — the one alert vocabulary: tinted bordered strip, tone
     icon, optional bold title, one body paragraph. The icon aligns to
     the first text line, not the block center. Alert=true adds
     role="alert" — reserve it for live problems, not ambient notes.
     Richer bodies (lists, links) hand-write the same classes instead.
     Keys:
       Body   string, required
       Title  string, optional
       Tone   string, optional — info (default) | positive | warning | negative
       Alert  bool, optional */}}
{{define "callout"}}<div class="rst-callout" data-tone="{{or .Tone "info"}}"{{if .Alert}} role="alert"{{end}}><span class="rst-callout__ic" aria-hidden="true">{{if eq (or .Tone "info") "positive"}}{{icon "check-circle"}}{{else if eq (or .Tone "info") "warning"}}{{icon "alert-triangle"}}{{else if eq (or .Tone "info") "negative"}}{{icon "x-circle"}}{{else}}{{icon "info"}}{{end}}</span><div class="rst-callout__body">{{with .Title}}<strong>{{.}}</strong>{{end}}<p>{{.Body}}</p></div></div>{{end}}
```

`ui/partials/detail-list.html`:
```
{{/* detail-list — label/value rows in a hairline card: the Show
     screen's settings block. Never restate a number a stat tile
     already shows — this list holds only what doesn't earn a tile.
     Values needing markup hand-write a <dl class="rst-detail-list">.
     Keys:
       Items  list, required — each {Label, Value} strings */}}
{{define "detail-list"}}<dl class="rst-detail-list">{{range .Items}}<dt>{{.Label}}</dt><dd>{{.Value}}</dd>{{end}}</dl>{{end}}
```

- [ ] **Step 3: tokens.css.** Append, in the file's existing comment/section style:

```css
/* badge — uppercase bordered chip; tones reuse the pill tone pairs. */
.rst-badge {
  border: 1px solid var(--rst-line);
  border-radius: var(--rst-radius-sm);
  color: var(--rst-text-muted);
  display: inline-block;
  font-size: 0.6875rem;
  font-variant-numeric: tabular-nums;
  font-weight: 700;
  letter-spacing: 0.04em;
  padding: 0.05rem 0.4rem;
  text-transform: uppercase;
  white-space: nowrap;
}
.rst-badge--positive { background: var(--rst-tone-positive-bg); border-color: var(--rst-tone-positive-fg); color: var(--rst-tone-positive-fg); }
.rst-badge--warning { background: var(--rst-tone-warning-bg); border-color: var(--rst-tone-warning-fg); color: var(--rst-tone-warning-fg); }
.rst-badge--negative { background: var(--rst-tone-negative-bg); border-color: var(--rst-tone-negative-fg); color: var(--rst-tone-negative-fg); }
.rst-badge--neutral { background: var(--rst-tone-neutral-bg); border-color: var(--rst-tone-neutral-fg); color: var(--rst-tone-neutral-fg); }

/* meter — 4px capacity bar; the number rides beside it as text. */
.rst-meter { align-items: center; color: var(--rst-text-muted); display: inline-flex; font-size: var(--rst-fs-xs); gap: var(--rst-sp-2); }
.rst-meter__bar { background: var(--rst-accent-soft); border-radius: 2px; flex: 1; height: 4px; min-width: 34px; overflow: hidden; }
.rst-meter__bar i { background: var(--rst-accent); border-radius: 2px; display: block; height: 100%; width: var(--rst-meter-fill, 0%); }
.rst-meter__num { font-variant-numeric: tabular-nums; white-space: nowrap; }

/* person — avatar + name + email; avatar is decoration, text is identity. */
.rst-person { align-items: center; color: inherit; display: inline-flex; gap: 0.7rem; min-width: 0; text-decoration: none; }
.rst-person__av { align-items: center; background: var(--rst-accent); border-radius: 50%; color: var(--rst-on-accent); display: flex; flex: none; font-size: 0.6875rem; font-weight: 600; height: 28px; justify-content: center; width: 28px; }
.rst-person__av--empty { background: transparent; border: 1px dashed var(--rst-line-strong); color: transparent; }
.rst-person__meta { min-width: 0; }
.rst-person__name { display: block; font-weight: 550; line-height: 1.25; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.rst-person__email { color: var(--rst-text-muted); display: block; font-size: var(--rst-fs-xs); line-height: 1.25; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.rst-person:hover .rst-person__name { color: var(--rst-accent); }
.rst-person--lg .rst-person__av { font-size: 1.0625rem; height: 46px; width: 46px; }

/* callout — the one alert vocabulary; icon sized to the first text line. */
.rst-callout { align-items: flex-start; background: var(--rst-surface-2); border: 1px solid var(--rst-line-strong); border-radius: var(--rst-radius); display: flex; gap: 0.6rem; margin: var(--rst-sp-4) 0; padding: 0.85rem 1rem; }
.rst-callout__ic { align-items: center; color: var(--rst-accent); display: inline-flex; flex: none; height: 1.5rem; justify-content: center; width: 1.25rem; }
.rst-callout__ic svg, .rst-callout__ic .icon { display: block; height: 1.25rem; width: 1.25rem; }
.rst-callout__body { line-height: 1.5; min-width: 0; }
.rst-callout__body > strong { display: block; font-weight: 600; }
.rst-callout__body > p { color: var(--rst-text-muted); margin: 0.15rem 0 0; }
.rst-callout__body > p:only-child { color: inherit; margin: 0; }
.rst-callout__body > ul { color: var(--rst-text-muted); margin: 0.15rem 0 0; padding-left: 1.1rem; }
.rst-callout[data-tone="positive"] { background: var(--rst-tone-positive-bg); border-color: var(--rst-tone-positive-fg); }
.rst-callout[data-tone="positive"] > .rst-callout__ic { color: var(--rst-tone-positive-fg); }
.rst-callout[data-tone="warning"] { background: var(--rst-tone-warning-bg); border-color: var(--rst-tone-warning-fg); }
.rst-callout[data-tone="warning"] > .rst-callout__ic { color: var(--rst-tone-warning-fg); }
.rst-callout[data-tone="negative"] { background: var(--rst-tone-negative-bg); border-color: var(--rst-tone-negative-fg); }
.rst-callout[data-tone="negative"] > .rst-callout__ic { color: var(--rst-tone-negative-fg); }

/* detail-list — hairline label/value rows; bare inside a box, framed alone. */
.rst-detail-list { display: grid; grid-template-columns: max-content 1fr; margin: var(--rst-sp-4) 0; }
.rst-detail-list dt { border-top: 1px solid var(--rst-line); color: var(--rst-text-muted); padding: 0.6rem 0; }
.rst-detail-list dd { border-top: 1px solid var(--rst-line); font-weight: 500; margin: 0; padding: 0.6rem 0 0.6rem 1.5rem; text-align: right; }
.rst-detail-list dt:first-of-type, .rst-detail-list dd:first-of-type { border-top: 0; }
```

- [ ] **Step 4: Tests.** In `ui/ui_test.go`: add each partial to `allPartials()` with a realistic fixture (badge: `{"Label": "Draft"}`; meter: `{"Percent": 82, "Text": "412/500"}`; person: `{"Href": "/people/1", "Name": "Grace Hopper", "Email": "grace@example.com", "Initial": "G"}`; callout: `{"Tone": "warning", "Title": "Connect payments to start selling", "Body": "Your event is live but can't take payment yet."}`; detail-list: `{"Items": [{"Label": "Audience", "Value": "Members"}, {"Label": "Main page", "Value": "No"}]}` — use `list`/`dict`-compatible `[]any`/`map[string]any` values like the existing fixtures). Update `TestAllEightPartialsAreDefined` to the new full name list (rename it `TestAllPartialsAreDefined`). Add per-partial behavior tests:

```go
func TestMeterClampsAndAlwaysShowsTheNumber(t *testing.T) {
	over := render(t, "meter", map[string]any{"Percent": 140, "Text": "7/5"})
	if !strings.Contains(over, "--rst-meter-fill: 100%") {
		t.Errorf("percent not clamped high: %s", over)
	}
	under := render(t, "meter", map[string]any{"Percent": -3, "Text": "0/5"})
	if !strings.Contains(under, "--rst-meter-fill: 0%") {
		t.Errorf("percent not clamped low: %s", under)
	}
	if !strings.Contains(over, `<span class="rst-meter__num">7/5</span>`) {
		t.Errorf("the fraction text is the accessible value and must render: %s", over)
	}
}

func TestCalloutTones(t *testing.T) {
	for tone, iconFrag := range map[string]string{
		"info": "M12 16v-4", "positive": "m9 12 2 2 4-4",
		"warning": "M12 9v4", "negative": "m15 9-6 6",
	} {
		got := render(t, "callout", map[string]any{"Tone": tone, "Body": "b"})
		if !strings.Contains(got, `data-tone="`+tone+`"`) || !strings.Contains(got, iconFrag) {
			t.Errorf("tone %s: wrong attribute or icon: %s", tone, got)
		}
	}
	plain := render(t, "callout", map[string]any{"Body": "b"})
	if !strings.Contains(plain, `data-tone="info"`) {
		t.Errorf("default tone is info: %s", plain)
	}
	if strings.Contains(plain, `role="alert"`) {
		t.Errorf("role=alert must be opt-in: %s", plain)
	}
	alert := render(t, "callout", map[string]any{"Body": "b", "Alert": true})
	if !strings.Contains(alert, `role="alert"`) {
		t.Errorf("Alert did not add role=alert: %s", alert)
	}
}

func TestPersonAvatarIsDecorationOnly(t *testing.T) {
	got := render(t, "person", fixtureFor(t, "person"))
	if !strings.Contains(got, `aria-hidden="true"`) {
		t.Errorf("avatar must be aria-hidden: %s", got)
	}
	empty := render(t, "person", map[string]any{"Href": "/x", "Name": "N"})
	if !strings.Contains(empty, "rst-person__av--empty") {
		t.Errorf("missing Initial renders the empty-avatar state: %s", empty)
	}
}
```

Also extend the existing class↔css check pattern (the F10 regression test's approach): for every new class these partials emit (`rst-badge`, `rst-badge--warning`, `rst-meter`, `rst-meter__bar`, `rst-meter__num`, `rst-person`, `rst-person__av`, `rst-callout`, `rst-callout__ic`, `rst-callout__body`, `rst-detail-list`), assert `TokensCSS()` contains the selector.

- [ ] **Step 5: Sweep and commit**

Run the root sweep + gofmt. Expected clean. Commit:
```bash
git add icons.go ui/
git commit -m "ui: display partials — badge, meter, person, callout, detail-list (+7 icons)"
```

---

### Task 2: The list grid and dropdown idioms

**Files:**
- Modify: `ui/tokens.css` (append)
- Modify: `ui/ui.go` (extend the package doc comment with the idiom vocabulary — markup samples live here and in the test)
- Modify: `ui/ui_test.go` (styleguide samples + assertions)

**Interfaces:**
- Consumes: icon names `kebab`, `chevron-down` from Task 1 / existing set.
- Produces: documented classes `rst-box`, `rst-box-head`, `rst-box-foot`, `rst-card`, `rst-lrow`, `rst-lrow--head`, `rst-m-hide`, `rst-nm`, `rst-cell-mut`, `rst-no-match`, `rst-count-line`, `rst-row-menu`, `rst-row-menu__panel`, `rst-danger`, `rst-dropdown`, `rst-dropdown__menu`, `rst-menu-group`, `rst-caret`, `rst-ftok`. Task 4's bulk-bar reuses `rst-dropdown` panel styling.

- [ ] **Step 1: tokens.css.** Append:

```css
/* box — the section card. Its heading (rst-box-head) is a SIBLING
   before the box, never inside it: a screen is a stack of
   section-header + card. A box-head action is a compact real button,
   never a pill. */
.rst-box { background: var(--rst-surface); border: 1px solid var(--rst-line); border-radius: var(--rst-radius); margin: var(--rst-sp-4) 0; padding: 1.1rem 1.25rem; }
.rst-box > :first-child { margin-top: 0; }
.rst-box > :last-child { margin-bottom: 0; }
.rst-box-head { align-items: baseline; display: flex; gap: var(--rst-sp-4); justify-content: space-between; margin: 1.5rem 0 0.5rem; }
.rst-box-head:first-child { margin-top: 0; }
.rst-box-head + .rst-box { margin-top: 0; }
.rst-box-head h2 { font-size: var(--rst-fs-base); margin: 0; }
.rst-box-foot { border-top: 1px solid var(--rst-line); color: var(--rst-text-muted); margin-top: var(--rst-sp-4); padding-top: 0.75rem; }
.rst-box > .rst-detail-list { margin: 0; }

/* The list grid — the real data-table vocabulary. The card sets the
   columns once (--rst-cols, inline, trailing 32px for the kebab);
   rows only choose cells. Hover fill only on rows that contain the
   identity link — a display-only row must never look clickable. */
.rst-card { background: var(--rst-surface); border: 1px solid var(--rst-line); border-radius: var(--rst-radius); }
.rst-lrow { align-items: center; border-bottom: 1px solid var(--rst-line); display: grid; gap: 0.85rem; grid-template-columns: var(--rst-cols, 1fr); padding: 0.68rem 1rem; position: relative; }
.rst-lrow:last-child { border-bottom: 0; }
.rst-lrow--head { color: var(--rst-text-faint); font-size: 0.71875rem; font-weight: 550; letter-spacing: 0.05em; padding: 0.55rem 1rem; text-transform: uppercase; }
.rst-lrow:not(.rst-lrow--head):has(> .rst-nm, > .rst-person):hover { background: var(--rst-accent-soft); }
.rst-nm { border-radius: var(--rst-radius-sm); color: inherit; font-weight: 550; min-width: 0; text-align: left; text-decoration: none; }
a.rst-nm:hover { color: var(--rst-accent); }
.rst-nm small { color: var(--rst-text-muted); display: block; font-size: var(--rst-fs-xs); font-weight: 400; }
.rst-cell-mut { color: var(--rst-text-muted); font-size: var(--rst-fs-xs); min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.rst-no-match { color: var(--rst-text-muted); font-size: var(--rst-fs-sm); padding: 1.6rem 1rem; text-align: center; }
.rst-no-match a { color: var(--rst-accent); }
.rst-count-line { color: var(--rst-text-faint); font-size: var(--rst-fs-xs); margin: 0.7rem 0 0; }
@media (max-width: 800px) {
  .rst-m-hide { display: none; }
  .rst-lrow, .rst-lrow--head { grid-template-columns: minmax(0, 1fr) auto 32px; }
}

/* row-menu — the per-row kebab: native details/summary, no JS. The
   destructive item sits last, class rst-danger, label ending "…". */
.rst-row-menu { justify-self: end; position: relative; }
.rst-row-menu > summary { align-items: center; border-radius: var(--rst-radius-sm); color: var(--rst-text-faint); cursor: pointer; display: flex; height: 26px; justify-content: center; list-style: none; width: 26px; }
.rst-row-menu > summary::-webkit-details-marker { display: none; }
.rst-row-menu > summary:hover, .rst-row-menu[open] > summary { background: var(--rst-accent-soft); color: var(--rst-text); }
.rst-row-menu__panel { background: var(--rst-surface); border: 1px solid var(--rst-line); border-radius: 9px; box-shadow: var(--rst-shadow-pop); min-width: 176px; padding: 0.25rem; position: absolute; right: 0; top: calc(100% + 4px); z-index: 40; }
.rst-row-menu__panel a, .rst-row-menu__panel button { background: none; border: 0; border-radius: var(--rst-radius-sm); color: var(--rst-text); cursor: pointer; display: block; font: inherit; font-size: var(--rst-fs-sm); font-weight: 450; margin: 0; padding: 0.4rem 0.65rem; text-align: left; text-decoration: none; width: 100%; }
.rst-row-menu__panel a:hover, .rst-row-menu__panel button:hover { background: var(--rst-accent-soft); }
.rst-row-menu__panel hr { border: 0; border-top: 1px solid var(--rst-line); margin: 0.25rem 0; }
.rst-danger { color: var(--rst-tone-negative-fg); }
.rst-row-menu__panel .rst-danger:hover { background: var(--rst-tone-negative-bg); }

/* dropdown — the details/summary menu vocabulary (header overflow,
   list-bar Filter/Sort). Exclusivity between siblings is the native
   details name attribute — zero JS. */
.rst-dropdown { position: relative; }
.rst-dropdown > summary { align-items: center; color: var(--rst-text-muted); cursor: pointer; display: flex; gap: 0.3rem; list-style: none; padding: 0.4rem 0.6rem; white-space: nowrap; }
.rst-dropdown > summary::-webkit-details-marker { display: none; }
.rst-dropdown > summary:hover, .rst-dropdown[open] > summary { color: var(--rst-text); }
.rst-dropdown__menu { background: var(--rst-surface); border: 1px solid var(--rst-line); border-radius: 9px; box-shadow: var(--rst-shadow-pop); margin-top: 4px; min-width: 176px; padding: 0.25rem; position: absolute; right: 0; top: 100%; z-index: 30; }
.rst-dropdown__menu a { border-radius: var(--rst-radius-sm); color: inherit; display: block; font-size: var(--rst-fs-sm); padding: 0.4rem 0.65rem; text-decoration: none; }
.rst-dropdown__menu a:hover, .rst-dropdown__menu a[aria-current] { background: var(--rst-accent-soft); }
.rst-dropdown__menu .rst-menu-group > summary { color: var(--rst-text-muted); cursor: pointer; font-size: var(--rst-fs-sm); list-style: none; padding: 0.4rem 0.65rem; }
.rst-dropdown__menu .rst-menu-group > summary::-webkit-details-marker { display: none; }
.rst-dropdown__menu .rst-menu-group > div { padding-left: 0.6rem; }
.rst-caret { align-items: center; color: var(--rst-text-faint); display: inline-flex; font-size: 0.8em; transition: transform 0.15s; }
.rst-caret svg, .rst-caret .icon { display: block; height: 1em; width: 1em; }
details[open] > summary > .rst-caret { transform: rotate(180deg); }

/* ftok — an applied filter as a removable chip; the × is a plain link
   to the unfiltered URL, so removing a filter is just navigation. */
.rst-ftok { align-items: center; background: var(--rst-surface); border: 1px solid var(--rst-line-strong); border-radius: 7px; display: inline-flex; font-size: var(--rst-fs-xs); gap: 0.45rem; padding: 0.1rem 0.3rem 0.1rem 0.6rem; }
.rst-ftok .rst-ftok__k { color: var(--rst-text-muted); }
.rst-ftok a { align-items: center; border-radius: 4px; color: var(--rst-text-faint); display: inline-flex; height: 17px; justify-content: center; text-decoration: none; width: 17px; }
.rst-ftok a:hover { background: var(--rst-tone-negative-bg); color: var(--rst-tone-negative-fg); }

@media (prefers-reduced-motion: reduce) {
  .rst-caret { transition: none; }
}
```

Also add to the `:root` token block (both themes if the file splits them): `--rst-shadow-pop` — light: `0 8px 24px rgba(0, 0, 0, 0.12)`; dark: `0 8px 24px rgba(0, 0, 0, 0.5)`. Follow the file's existing light/dark override structure exactly.

- [ ] **Step 2: Package doc.** In `ui/ui.go`'s package comment, add a "Class idioms" section listing each idiom with a minimal sample (the grid card with `--rst-cols`, a head row, a data row with `rst-nm` + `rst-m-hide` cell + kebab `rst-row-menu`; the `rst-dropdown` with `name` grouping and a nested `rst-menu-group`; `rst-ftok`). Samples must match what Step 3's test renders — write them once in the test as Go raw strings and reference the test from the doc ("the canonical samples live in ui_test.go's styleguideSamples, rendered by the smoke test").

- [ ] **Step 3: Tests.** In `ui/ui_test.go`, add:

```go
// styleguideSamples are the canonical markup samples for the class
// idioms — structural components with arbitrary bodies that a Go
// template partial cannot wrap. The smoke test renders them so every
// documented class is exercised, and the class↔css test keeps them
// honest against tokens.css (the F10 lesson, generalized).
var styleguideSamples = map[string]string{
	"box": `<div class="rst-box-head"><h2>Payout</h2><a class="rst-btn" href="/payout/edit">Edit</a></div>
<section class="rst-box"><p>Everything on a screen sits inside boxes.</p><div class="rst-box-foot">Last updated 2 hours ago</div></section>`,
	"list-grid": `<div class="rst-card" style="--rst-cols: 2fr 110px 32px">
  <div class="rst-lrow rst-lrow--head"><span>Order</span><span class="rst-m-hide">Status</span><span></span></div>
  <div class="rst-lrow">
    <a class="rst-nm" href="/orders/AB3PX">Grace Hopper<small>AB3PX · grace@example.com</small></a>
    <span class="rst-m-hide rst-cell-mut">Paid</span>
    <details class="rst-row-menu"><summary aria-label="Actions for order AB3PX">` + iconSVG("kebab") + `</summary>
      <div class="rst-row-menu__panel"><a href="/orders/AB3PX">View</a><hr><button type="submit" class="rst-danger">Refund order…</button></div>
    </details>
  </div>
  <p class="rst-no-match">No orders match. <a href="/orders">Clear filters</a></p>
</div>
<p class="rst-count-line">Displaying <strong>1–20</strong> of <strong>412</strong></p>`,
	"dropdown": `<details class="rst-dropdown" name="list-controls">
  <summary>Filter<span class="rst-caret" aria-hidden="true">` + iconSVG("chevron-down") + `</span><span class="rst-sr-only">Filter orders: Paid</span></summary>
  <div class="rst-dropdown__menu">
    <a aria-current="true" href="/orders?status=paid">Paid</a>
    <details class="rst-menu-group" open><summary>Price</summary><div><a href="/orders?price=free">Free</a></div></details>
  </div>
</details>
<span class="rst-ftok"><span class="rst-ftok__k">Paid</span><a href="/orders" aria-label="Remove filter Paid">✕</a></span>`,
}
```

(`iconSVG` is a tiny test helper calling `rastrillo.Icon` and returning its string — check how the partials' own icon output renders first and match it; if `rastrillo.Icon` returns `template.HTML`, wrap with `string()`. If the existing test file renders icons another way, follow that instead. `rst-sr-only`: check whether the existing tokens.css already ships an sr-only utility — the List slice's `list-search-submit` used one; reuse its exact class name in the sample instead if it differs.)

Tests to add: `TestStyleguideSamplesRender` — for each sample, execute it through a template parsed WITH the ui funcs (samples are static HTML so parsing is enough; assert non-empty and balanced via the existing smoke-test balance check helper if one exists, else a simple open/close tag count for div/details). `TestIdiomClassesAreStyled` — every `rst-` class extracted from the samples (regexp `rst-[a-z-]+(?:__[a-z-]+)?(?:--[a-z-]+)?`) appears in `TokensCSS()`; and the reverse direction for the new selectors added this task (list them literally). `TestDropdownExclusivityIsNative` — the dropdown sample uses a `name=` attribute on `<details>` (assert the substring `` `<details class="rst-dropdown" name=` ``) and contains no `<script`.

- [ ] **Step 4: Sweep and commit**

```bash
git add ui/
git commit -m "ui: the list grid, row-menu kebab, dropdown, and filter-token idioms"
```

---

### Task 3: Form family

**Files:**
- Create: `ui/partials/field.html` (defines `field`, and colocated `field-help` if extraction helps — implementer's call, keys below are the contract), `ui/partials/field-select.html`, `ui/partials/field-textarea.html`, `ui/partials/field-check.html`, `ui/partials/choice-field.html`, `ui/partials/seg-tabs.html`
- Modify: `ui/tokens.css` (append field/form blocks)
- Modify: `ui/ui_test.go`

**Interfaces:**
- Consumes: existing tokens; `rst-switch` classes defined here are reused by Task 4's `rst-tblock`.
- Produces: partials `field`, `field-select`, `field-textarea`, `field-check`, `choice-field`, `seg-tabs`; classes `rst-field`, `rst-field__label`, `rst-field__hint`, `rst-field__help`, `rst-field__error`, `rst-input`, `rst-input--short`, `rst-switch`, `rst-switch__track`, `rst-choice`, `rst-choice__cards`, `rst-choice__title`, `rst-choice__desc`, `rst-seg-tabs`, `rst-form-flow`, `rst-field-row`, `rst-form-foot`, `rst-form-actions`, `rst-btn--ghost` (only if the existing button classes lack a ghost variant — check `rst-btn` in tokens.css first and reuse whatever exists).

- [ ] **Step 1: Partials.**

`ui/partials/field.html`:
```
{{/* field — label + input + optional hint/help/error, the one text-like
     control. Help is wired aria-describedby; Error adds aria-invalid
     and its own role=alert line. Autofocus is for a dedicated create
     page's first field only — never a settings panel.
     Keys:
       ID, Name, Label  strings, required
       Type      string, optional — default "text"
       Value, Placeholder, Autocomplete, Maxlength, Min, Max, Pattern
                 strings, optional — emitted only when set
       Required  bool, optional
       Hint      string, optional — quiet parenthetical after the label
       Help      string, optional — the line under the control
       Error     string, optional — validation message
       Short     bool, optional — compact width
       Autofocus bool, optional */}}
{{define "field"}}<div class="rst-field"><label class="rst-field__label" for="{{.ID}}">{{.Label}}{{with .Hint}} <span class="rst-field__hint">({{.}})</span>{{end}}</label>
<input class="rst-input{{if .Short}} rst-input--short{{end}}" type="{{or .Type "text"}}" id="{{.ID}}" name="{{.Name}}"{{with .Value}} value="{{.}}"{{end}}{{with .Placeholder}} placeholder="{{.}}"{{end}}{{if .Required}} required{{end}}{{if .Autofocus}} autofocus{{end}}{{with .Autocomplete}} autocomplete="{{.}}"{{end}}{{with .Maxlength}} maxlength="{{.}}"{{end}}{{with .Min}} min="{{.}}"{{end}}{{with .Max}} max="{{.}}"{{end}}{{with .Pattern}} pattern="{{.}}"{{end}}{{if .Error}} aria-invalid="true" aria-describedby="{{.ID}}-error"{{else if .Help}} aria-describedby="{{.ID}}-help"{{end}}>
{{if .Error}}<p class="rst-field__error" id="{{.ID}}-error" role="alert">{{.Error}}</p>
{{else if .Help}}<p class="rst-field__help" id="{{.ID}}-help">{{.Help}}</p>
{{end}}</div>{{end}}
```

`ui/partials/field-select.html` — same envelope (label/hint/help/error identical, same describedby logic), body:
```
<select class="rst-input{{if .Short}} rst-input--short{{end}}" id="{{.ID}}" name="{{.Name}}"{{if .Required}} required{{end}}{{if .Error}} aria-invalid="true" aria-describedby="{{.ID}}-error"{{else if .Help}} aria-describedby="{{.ID}}-help"{{end}}>{{range .Options}}<option value="{{.Value}}"{{if .Selected}} selected{{end}}>{{.Label}}</option>{{end}}</select>
```
with keys doc: `Options` list of `{Value, Label, Selected}`.

`ui/partials/field-textarea.html` — same envelope, body `<textarea class="rst-input" id="{{.ID}}" name="{{.Name}}" rows="{{or .Rows "3"}}" ...same conditionals...>{{or .Value ""}}</textarea>`. Doc notes: plain scrolling textarea, no autosize — zero-JS baseline.

`ui/partials/field-check.html`:
```
{{/* field-check — the one toggle mechanism: a real checkbox skinned as
     a switch. The input is visually hidden but keyboard/AT-intact; the
     focus ring is reproduced on the visible track via :has(). Plain
     checkboxes are not part of the vocabulary — see choice-field for
     pick-any lists that deserve explanation.
     Keys:
       Name, Label  strings, required
       Value        string, optional
       Checked, Disabled  bools, optional */}}
{{define "field-check"}}<label class="rst-switch"><input type="checkbox" name="{{.Name}}"{{with .Value}} value="{{.}}"{{end}}{{if .Checked}} checked{{end}}{{if .Disabled}} disabled{{end}}><span class="rst-switch__track" aria-hidden="true"></span> {{.Label}}</label>{{end}}
```

`ui/partials/choice-field.html`:
```
{{/* choice-field — option cards, the preferred radio/checkbox group:
     whole-card hit target, native input visible (it signals pick-one
     vs pick-any), selection ring via :has(). Long sentence labels are
     what made forms hard to parse — Title stays short, Desc explains.
     Keys:
       Legend, Name  strings, required
       Type          string, optional — "radio" (default) | "checkbox"
       LegendHidden  bool, optional
       Options       list, required — each {Value, Title, Desc, Checked} */}}
{{define "choice-field"}}<fieldset class="rst-choice"><legend{{if .LegendHidden}} class="rst-sr-only"{{end}}>{{.Legend}}</legend><div class="rst-choice__cards">{{$name := .Name}}{{$type := or .Type "radio"}}{{range .Options}}<label><input type="{{$type}}" name="{{$name}}" value="{{.Value}}"{{if .Checked}} checked{{end}}><span><span class="rst-choice__title">{{.Title}}</span>{{with .Desc}}<span class="rst-choice__desc">{{.}}</span>{{end}}</span></label>{{end}}</div></fieldset>{{end}}
```
(Use the sr-only utility class the package already ships — check its exact name first and use that.)

`ui/partials/seg-tabs.html`:
```
{{/* seg-tabs — a segmented control of server-rendered links: edit-form
     Basics/Advanced (each tab its own form, so one tab's save can never
     clobber the other's fields) and list scope-switching. Zero JS —
     the current tab is just aria-current on a link.
     Keys:
       Label  string, required — the nav's accessible name
       Items  list, required — each {Label, Href, Current} */}}
{{define "seg-tabs"}}<nav class="rst-seg-tabs" aria-label="{{.Label}}">{{range .Items}}<a href="{{.Href}}"{{if .Current}} aria-current="page"{{end}}>{{.Label}}</a>{{end}}</nav>{{end}}
```

- [ ] **Step 2: tokens.css.** Append (reuse the existing button classes for anything button-shaped — read the current file first and match):

```css
/* fields — bare on the page, one column; a card in a form is reserved
   for collections, toggle blocks, and choice cards. */
.rst-field { margin: var(--rst-sp-4) 0; }
.rst-field__label { color: var(--rst-text-muted); display: block; font-size: var(--rst-fs-sm); margin: 0 0 0.2rem; }
.rst-field__hint { color: var(--rst-text-faint); font-weight: 400; }
.rst-input { background: var(--rst-surface); border: 1px solid var(--rst-line-strong); border-radius: var(--rst-radius-sm); color: inherit; font: inherit; padding: 0.45rem 0.6rem; width: 100%; }
.rst-input:focus-visible { outline: 2px solid var(--rst-accent); outline-offset: 2px; }
.rst-input--short { width: 6.5rem; }
textarea.rst-input { min-height: 5rem; resize: vertical; }
.rst-field__help { color: var(--rst-text-muted); font-size: var(--rst-fs-xs); margin: 0.45rem 0 0; }
.rst-field__error { color: var(--rst-tone-negative-fg); font-size: var(--rst-fs-xs); margin: 0.45rem 0 0; }

/* switch — the one toggle mechanism: real checkbox, visible track. */
.rst-switch { align-items: center; cursor: pointer; display: inline-flex; gap: 0.6rem; position: relative; }
.rst-switch input { height: 1px; opacity: 0; position: absolute; width: 1px; }
.rst-switch__track { background: var(--rst-line-strong); border-radius: 10px; flex: none; height: 19px; position: relative; transition: background 0.15s; width: 34px; }
.rst-switch__track::after { background: var(--rst-surface); border-radius: 50%; box-shadow: 0 1px 2px rgba(0, 0, 0, 0.3); content: ""; height: 15px; left: 2px; position: absolute; top: 2px; transition: transform 0.22s cubic-bezier(0.34, 1.56, 0.64, 1); width: 15px; }
.rst-switch:has(input:checked) .rst-switch__track { background: var(--rst-accent); }
.rst-switch:has(input:checked) .rst-switch__track::after { transform: translateX(15px); }
.rst-switch:has(input:focus-visible) .rst-switch__track { outline: 2px solid var(--rst-accent); outline-offset: 2px; }
.rst-switch:has(input:disabled) { cursor: default; opacity: 0.55; }

/* choice cards — whole-card target, native input visible. */
.rst-choice { border: 0; margin: var(--rst-sp-4) 0; padding: 0; }
.rst-choice > legend { font-size: var(--rst-fs-sm); font-weight: 600; margin: 0 0 0.45rem; padding: 0; }
.rst-choice__cards { display: grid; gap: 0.5rem; }
.rst-choice__cards label { align-items: flex-start; border: 1px solid var(--rst-line); border-radius: var(--rst-radius); cursor: pointer; display: flex; gap: 0.7rem; margin: 0; padding: 0.75rem 0.95rem; }
.rst-choice__cards label:hover { border-color: var(--rst-accent); }
.rst-choice__cards input { accent-color: var(--rst-accent); flex: none; margin-top: 0.18rem; }
.rst-choice__title { display: block; font-size: var(--rst-fs-sm); font-weight: 550; line-height: 1.35; }
.rst-choice__desc { color: var(--rst-text-muted); display: block; font-size: var(--rst-fs-xs); line-height: 1.4; }
.rst-choice__cards label:has(input:checked) { background: var(--rst-accent-soft); border-color: var(--rst-accent); }
.rst-choice__cards label:has(input:focus-visible) { outline: 2px solid var(--rst-accent); outline-offset: 2px; }

/* seg-tabs — filled pill inside a soft track; current is aria-current. */
.rst-seg-tabs { background: var(--rst-accent-soft); border-radius: 8px; display: flex; gap: 2px; padding: 2px; width: max-content; }
.rst-seg-tabs a { border-radius: var(--rst-radius-sm); color: var(--rst-text-muted); font-size: var(--rst-fs-sm); font-weight: 550; padding: 5px 16px; text-decoration: none; }
.rst-seg-tabs a:hover { color: var(--rst-text); }
.rst-seg-tabs a[aria-current] { background: var(--rst-surface); box-shadow: 0 1px 2px rgba(0, 0, 0, 0.08); color: var(--rst-text); font-weight: 600; }

/* form layout — the interview rhythm and the save bar. */
.rst-form-flow > .rst-field + .rst-field { margin-top: var(--rst-sp-5); }
.rst-field-row { align-items: end; display: flex; flex-wrap: wrap; gap: 0.75rem; margin: 0.75rem 0; }
.rst-field-row .rst-field { margin: 0; }
.rst-field-row > .rst-grow { flex: 1; min-width: 0; }
.rst-form-foot { align-items: center; background: var(--rst-bg); border-top: 1px solid var(--rst-line); bottom: 0; display: flex; gap: 0.5rem; justify-content: flex-end; margin-top: 1.75rem; padding: 0.8rem 0; position: sticky; z-index: 5; }
.rst-form-foot .rst-form-foot__note { color: var(--rst-text-muted); font-size: var(--rst-fs-xs); margin-right: auto; }
.rst-form-actions { display: flex; gap: 0.5rem; justify-content: flex-end; }
.rst-form-actions > a { order: -1; }

@media (prefers-reduced-motion: reduce) {
  .rst-switch__track, .rst-switch__track::after { transition: none; }
}
```

- [ ] **Step 3: Tests.** Fixtures for all six partials in `allPartials()`. Behavior tests (same file):

```go
func TestFieldWiresHelpAndError(t *testing.T) {
	help := render(t, "field", map[string]any{"ID": "f1", "Name": "n", "Label": "L", "Help": "h"})
	if !strings.Contains(help, `aria-describedby="f1-help"`) || !strings.Contains(help, `id="f1-help"`) {
		t.Errorf("Help not wired via aria-describedby: %s", help)
	}
	errd := render(t, "field", map[string]any{"ID": "f1", "Name": "n", "Label": "L", "Help": "h", "Error": "bad"})
	if !strings.Contains(errd, `aria-invalid="true"`) || !strings.Contains(errd, `role="alert"`) {
		t.Errorf("Error not wired: %s", errd)
	}
	if strings.Contains(errd, "f1-help") {
		t.Errorf("Error replaces Help — both rendered: %s", errd)
	}
}

func TestFieldCheckIsARealCheckbox(t *testing.T) {
	got := render(t, "field-check", map[string]any{"Name": "on", "Label": "Enable", "Checked": true})
	for _, want := range []string{`type="checkbox"`, "checked", `aria-hidden="true"`, "rst-switch__track"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q: %s", want, got)
		}
	}
}

func TestSegTabsMarksCurrent(t *testing.T) {
	got := render(t, "seg-tabs", map[string]any{"Label": "Sections", "Items": []any{
		map[string]any{"Label": "Basics", "Href": "?tab=basics", "Current": true},
		map[string]any{"Label": "Advanced", "Href": "?tab=advanced"},
	}})
	if !strings.Contains(got, `aria-current="page">Basics`) {
		t.Errorf("current tab unmarked: %s", got)
	}
	if strings.Count(got, "aria-current") != 1 {
		t.Errorf("exactly one current tab: %s", got)
	}
}
```

Extend the accessible-name test (`TestEveryControlHasAnAccessibleName`) with: `field`'s input has a `<label for=`; `choice-field`'s legend renders; `field-check`'s label text renders outside the aria-hidden track. Extend the class↔css assertions with this task's classes.

- [ ] **Step 4: Sweep and commit**

```bash
git add ui/
git commit -m "ui: form family — field partials, switch, choice cards, seg-tabs, form layout"
```

---

### Task 4: Routes family — confirm, toggle-block, modal shells, bulk-bar, help

**Files:**
- Create: `ui/partials/confirm-form.html` (defines `confirm-form`, plus colocated `back-nav`, `notice`, `form-error` — one file: they are the confirm page's companions), `ui/partials/bulk-bar.html`
- Modify: `ui/tokens.css` (append), `ui/ui.go` (idiom docs), `ui/ui_test.go`

**Interfaces:**
- Consumes: `rst-switch` classes from Task 3 (tblock reuses them), `rst-dropdown` panel classes from Task 2 (bulk-bar's actions menu), icons `x`, `help-circle`, `chevron-down`.
- Produces: partials `confirm-form`, `back-nav`, `notice`, `form-error`, `bulk-bar`; classes `rst-tblock`, `rst-tblock__head`, `rst-tblock__title`, `rst-tblock__desc`, `rst-tblock__body`, `rst-backdrop`, `rst-modal-overlay`, `rst-modal-panel`, `rst-modal-close`, `rst-help`, `rst-tip`, `rst-bulkbar`, `rst-bulkbar__close`, `rst-bulkbar__count`, `rst-bulkbar__escalate`, `rst-selbox`.

- [ ] **Step 1: Partials.**

`ui/partials/confirm-form.html`:
```
{{/* confirm-form — the destructive-action POST, plus its page
     companions. A destructive action is its own URL: GET renders
     back-nav → page-header with the question as Title → explanation
     paragraphs → this form. A GET never mutates; Cancel is a plain
     link; the submit POSTs to Action. Cancel renders before the
     button visually regardless of source order.
     Keys:
       Action, Label  strings, required
       Danger    bool, optional — the darkened danger fill
       Hidden    map, optional — hidden inputs, key-ordered
       CancelHref, CancelLabel  strings, optional — label default "Cancel" */}}
{{define "confirm-form"}}<form class="rst-form-actions" method="post" action="{{.Action}}">{{range $k, $v := .Hidden}}<input type="hidden" name="{{$k}}" value="{{$v}}">{{end}}<button type="submit" class="rst-btn{{if .Danger}} rst-btn--danger{{else}} rst-btn--primary{{end}}">{{.Label}}</button>{{if .CancelHref}}<a class="rst-btn rst-btn--ghost" href="{{.CancelHref}}">{{or .CancelLabel "Cancel"}}</a>{{end}}</form>{{end}}

{{/* back-nav — the ← return link above a confirm or detail page. */}}
{{define "back-nav"}}<p class="rst-back-nav"><a href="{{.Href}}">← {{.Label}}</a></p>{{end}}

{{/* notice — a one-line calm confirmation; renders nothing when empty. */}}
{{define "notice"}}{{if .}}<p class="rst-notice" role="status">{{.}}</p>{{end}}{{end}}

{{/* form-error — a one-line validation failure; renders nothing when empty. */}}
{{define "form-error"}}{{if .}}<p class="rst-form-error" role="alert">{{.}}</p>{{end}}{{end}}
```
Check the existing `rst-btn` variants in tokens.css: reuse `rst-btn--primary` if it exists; add `rst-btn--danger` and `rst-btn--ghost` only if absent (danger: solid fill `--rst-tone-negative-fg` background with `--rst-on-accent`-equivalent text — if the existing tone tokens can't clear 4.5:1 as a solid fill, define `--rst-danger` and `--rst-on-danger` tokens in both themes and note them for Task 5's contrast test).

`ui/partials/bulk-bar.html`:
```
{{/* bulk-bar — select-mode's header strip, replacing the list-bar:
     count, escalate/clear link, and the actions menu whose items are
     real submit buttons on the surrounding form. Entering and leaving
     select mode are plain links (?select=1 / DoneHref) — the server
     renders both modes; nothing here needs JS.
     Keys:
       DoneHref  string, required — leaves select mode
       DoneLabel string, required — its accessible name ("Done selecting")
       Count     string, required — the selection line ("3 selected",
                 "All 412 selected", "Select items…")
       EscalateHref, EscalateLabel  strings, optional — "Select all 412
                 matching" or "Clear selection"
       MenuLabel string, required — the actions summary text ("Actions")
       Actions   list, required — each {Value, Label, Danger}: submit
                 buttons named "action" */}}
{{define "bulk-bar"}}<div class="rst-bulkbar"><a class="rst-bulkbar__close" href="{{.DoneHref}}" aria-label="{{.DoneLabel}}">{{icon "x"}}</a><span class="rst-bulkbar__count">{{.Count}}</span>{{if .EscalateHref}}<a class="rst-bulkbar__escalate" href="{{.EscalateHref}}">{{.EscalateLabel}}</a>{{end}}<details class="rst-dropdown"><summary>{{.MenuLabel}}<span class="rst-caret" aria-hidden="true">{{icon "chevron-down"}}</span></summary><div class="rst-dropdown__menu">{{range .Actions}}<button type="submit" name="action" value="{{.Value}}"{{if .Danger}} class="rst-danger"{{end}}>{{.Label}}</button>{{end}}</div></details></div>{{end}}
```
(The dropdown menu's `button` styling: Task 2 styled panel buttons under `rst-row-menu__panel`; add the same `button` rules under `.rst-dropdown__menu` if Task 2's block doesn't already cover buttons — check first.)

- [ ] **Step 2: tokens.css.** Append:

```css
/* toggle-block — a bordered card whose head is a switch; the body
   reveals via :has(), zero JS. The switch is authoritative: the server
   treats off as off, whatever the revealed (still-POSTed) fields say. */
.rst-tblock { background: var(--rst-surface); border: 1px solid var(--rst-line); border-radius: var(--rst-radius); margin: 0.7rem 0; }
.rst-tblock > .rst-tblock__head { align-items: flex-start; cursor: pointer; display: flex; gap: 0.85rem; margin: 0; padding: 0.9rem 1.05rem; }
.rst-tblock > .rst-tblock__head:hover { background: var(--rst-accent-soft); }
.rst-tblock .rst-tblock__title { display: block; font-size: var(--rst-fs-sm); font-weight: 550; }
.rst-tblock .rst-tblock__desc { color: var(--rst-text-muted); display: block; font-size: var(--rst-fs-xs); }
.rst-tblock .rst-tblock__head input { height: 1px; opacity: 0; position: absolute; width: 1px; }
.rst-tblock:has(.rst-tblock__head input:checked) .rst-switch__track { background: var(--rst-accent); }
.rst-tblock:has(.rst-tblock__head input:checked) .rst-switch__track::after { transform: translateX(15px); }
.rst-tblock:has(.rst-tblock__head input:focus-visible) .rst-switch__track { outline: 2px solid var(--rst-accent); outline-offset: 2px; }
.rst-tblock > .rst-tblock__body { display: none; }
.rst-tblock:has(.rst-tblock__head input:checked) > .rst-tblock__body { background: var(--rst-surface-2); border-radius: 0 0 var(--rst-radius) var(--rst-radius); border-top: 1px solid var(--rst-line); display: block; padding: 0.9rem 1.05rem 1.05rem; }

/* modal route — its own URL: the response renders the page you'll
   return to inside an inert backdrop, then the overlay. Closing is a
   plain link to that page. */
.rst-backdrop { display: contents; }
body:has(.rst-backdrop) { overflow: hidden; }
.rst-modal-overlay { align-items: center; background: rgba(0, 0, 0, 0.45); display: flex; inset: 0; justify-content: center; padding: 2rem 1rem; position: fixed; z-index: 10; }
.rst-modal-panel { background: var(--rst-bg); border: 1px solid var(--rst-line); border-radius: 14px; box-shadow: var(--rst-shadow-pop); display: flex; max-height: min(85vh, 640px); max-width: 860px; overflow: hidden; width: 100%; }
.rst-modal-panel > nav { border-right: 1px solid var(--rst-line); display: flex; flex: none; flex-direction: column; gap: 0.15rem; padding: 1.1rem 0.8rem; width: 180px; }
.rst-modal-panel > nav a { border-radius: 8px; color: inherit; padding: 0.4rem 0.65rem; text-decoration: none; }
.rst-modal-panel > nav a:hover { background: var(--rst-accent-soft); }
.rst-modal-panel > nav a[aria-current] { background: var(--rst-accent-soft); color: var(--rst-accent); font-weight: 600; }
.rst-modal-panel > section { flex: 1; min-width: 0; overflow-y: auto; padding: 1.5rem 2rem 2rem; position: relative; }
.rst-modal-close { color: var(--rst-text-muted); font-size: 1.5rem; line-height: 1; position: absolute; right: 1.1rem; text-decoration: none; top: 1rem; }
.rst-modal-close:hover { color: inherit; }
@media (max-width: 800px) {
  .rst-modal-overlay { padding: 0.75rem; }
  .rst-modal-panel { flex-direction: column; max-height: 92vh; }
  .rst-modal-panel > nav { border-bottom: 1px solid var(--rst-line); border-right: 0; flex-direction: row; width: auto; }
}

/* help — a bordered ? icon-link to a section's help article: plain <a>,
   new tab, CSS tooltip. The tooltip is NOT the accessible name — the
   link must carry a full aria-label of its own. */
.rst-help { align-items: center; border: 1px solid var(--rst-line-strong); border-radius: 7px; color: var(--rst-text-muted); display: inline-flex; height: 28px; justify-content: center; position: relative; width: 28px; }
.rst-help:hover { border-color: var(--rst-text-faint); color: var(--rst-text); }
.rst-help svg, .rst-help .icon { height: 15px; width: 15px; }
.rst-tip::after { background: var(--rst-text); border-radius: var(--rst-radius-sm); color: var(--rst-bg); content: attr(data-tip); font-size: 0.71875rem; font-weight: 500; opacity: 0; padding: 3px 8px; pointer-events: none; position: absolute; right: 0; top: calc(100% + 6px); transition: opacity 0.12s; white-space: nowrap; z-index: 35; }
.rst-tip:hover::after, .rst-tip:focus-visible::after { opacity: 1; }

/* bulk-bar + selbox — select mode as server-rendered state. */
.rst-bulkbar { align-items: center; background: var(--rst-accent-soft); border-bottom: 1px solid var(--rst-line); display: flex; gap: 0.7rem; min-height: 46px; padding: 0 0.85rem 0 0.6rem; }
.rst-bulkbar__close { align-items: center; border-radius: var(--rst-radius-sm); color: var(--rst-text-muted); display: inline-flex; height: 26px; justify-content: center; text-decoration: none; width: 26px; }
.rst-bulkbar__close:hover { background: var(--rst-accent-soft); color: var(--rst-text); }
.rst-bulkbar__close svg, .rst-bulkbar__close .icon { height: 15px; width: 15px; }
.rst-bulkbar__count { font-size: var(--rst-fs-sm); font-weight: 600; }
.rst-bulkbar__escalate { color: var(--rst-accent); font-size: var(--rst-fs-xs); font-weight: 550; text-decoration: none; }
.rst-bulkbar__escalate:hover { text-decoration: underline; }
.rst-bulkbar .rst-dropdown { margin-left: auto; }
.rst-bulkbar .rst-dropdown > summary { background: var(--rst-surface); border: 1px solid var(--rst-line-strong); border-radius: 7px; font-size: var(--rst-fs-xs); font-weight: 550; padding: 0.22rem 0.6rem; }
.rst-selbox { align-items: center; display: flex; margin: 0; }
.rst-selbox input { accent-color: var(--rst-accent); cursor: pointer; height: 16px; margin: 0; width: 16px; }
.rst-selbox input:focus-visible { outline: 2px solid var(--rst-accent); outline-offset: 2px; }

.rst-back-nav { margin: 0 0 var(--rst-sp-3); }
.rst-back-nav a { color: var(--rst-text-muted); text-decoration: none; }
.rst-back-nav a:hover { color: var(--rst-accent); }
.rst-notice { background: var(--rst-tone-positive-bg); border-radius: var(--rst-radius-sm); color: var(--rst-tone-positive-fg); font-size: var(--rst-fs-sm); padding: 0.5rem 0.75rem; }
.rst-form-error { color: var(--rst-tone-negative-fg); font-size: var(--rst-fs-sm); }

@media (prefers-reduced-motion: reduce) {
  .rst-tip::after { transition: none; }
}
```

- [ ] **Step 3: Idiom docs + styleguide samples.** Add to `styleguideSamples`: a `tblock` sample (head label with hidden checkbox + `rst-switch__track` + title/desc, plus a body containing a `field` render note — keep the sample static HTML: hand-write a plain input inside), a `modal` sample (`rst-backdrop` with `inert` + overlay + panel with nav rail, section, `rst-modal-close` with `aria-label="Close settings"`), a `help` sample (`<a class="rst-help rst-tip" href="/help/orders" target="_blank" rel="noopener" aria-label="Help: orders" data-tip="About orders">` + help-circle icon), and a `selbox` sample (`<label class="rst-selbox"><input type="checkbox" aria-label="Select order AB3PX"></label>`). Extend `ui/ui.go`'s idiom docs with tblock (including the authoritative-switch rule), modal-route (the mechanism: own URL, inert backdrop, closing is a link), help, selbox.

- [ ] **Step 4: Tests.** Fixtures for `confirm-form` (`{"Action": "/orders/1/refund", "Label": "Refund €10.00", "Danger": true, "Hidden": {"csrf": "tok"}, "CancelHref": "/orders/1"}`), `back-nav`, `bulk-bar`; `notice`/`form-error` get direct render tests (string data, empty renders nothing — assert `render(t, "notice", "") == ""` modulo whitespace). Behavior tests:

```go
func TestConfirmFormShape(t *testing.T) {
	got := render(t, "confirm-form", fixtureFor(t, "confirm-form"))
	for _, want := range []string{`method="post"`, `action="/orders/1/refund"`,
		`<input type="hidden" name="csrf" value="tok">`, "rst-btn--danger",
		`href="/orders/1"`, ">Cancel</a>"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q: %s", want, got)
		}
	}
	// Cancel is an <a>, never a submit — a GET never mutates.
	if strings.Contains(got, `type="submit">Cancel`) {
		t.Errorf("cancel must be a link: %s", got)
	}
}

func TestBulkBarActionsAreRealSubmits(t *testing.T) {
	got := render(t, "bulk-bar", fixtureFor(t, "bulk-bar"))
	if !strings.Contains(got, `<button type="submit" name="action" value="refund" class="rst-danger">`) {
		t.Errorf("actions must be named submit buttons on the surrounding form: %s", got)
	}
	if !strings.Contains(got, `aria-label=`) {
		t.Errorf("the close control needs an accessible name: %s", got)
	}
}
```

Extend class↔css for this task's classes; extend the no-script check to all new samples (`<script` never appears).

- [ ] **Step 5: Sweep and commit**

```bash
git add ui/
git commit -m "ui: routes family — confirm-form, toggle-block, modal shells, bulk-bar, help"
```

---

### Task 5: Localization threading + contrast/motion gates + docs

**Files:**
- Create: `basecatalog.go` (root package), `basecatalog_test.go`
- Modify: `serve.go` (`buildHandler`: `NewLocales(..., nil, ...)` → `NewLocales(..., BaseCatalog(), ...)`), `locale.go` only if reading it shows the base layer needs plumbing it doesn't have (it shouldn't — `NewLocales` already takes a base `Catalog`)
- Modify: `ui/funcs.go` (`T` default + `FuncsWith`), `ui/partials/pagination.html`, the partial containing `list-search-submit`, `ui/partials/confirm-form.html` (defaults through `T`)
- Modify: `ui/ui_test.go`, `README.md`
- Create: `ui/contrast_test.go`

**Interfaces:**
- Consumes: `NewLocales(codes, def, base Catalog, fsys fs.FS)` — the base layer that has been nil since the i18n slice; `Catalog` type from `locale.go`.
- Produces: `rastrillo.BaseCatalog() Catalog`; `ui.FuncsWith(t func(string, ...any) string) template.FuncMap`; `ui.Funcs()` now returns four entries (dict, list, icon, T).

- [ ] **Step 1: Base catalog.** Create `basecatalog.go`:

```go
package rastrillo

// baseCatalog is the framework's own strings — the third fallback
// layer the §10 design reserved ("locale → default → framework base →
// key"). Keys are namespaced rastrillo.ui.* so an app catalog can
// override any of them per locale without colliding with app keys.
var baseCatalog = Catalog{
	"rastrillo.ui.pagination":    "Pagination",
	"rastrillo.ui.search_submit": "Search",
	"rastrillo.ui.cancel":        "Cancel",
}

// BaseCatalog returns a copy of the framework's base strings, so a
// caller can inspect them without being able to mutate the layer every
// app shares.
func BaseCatalog() Catalog {
	out := make(Catalog, len(baseCatalog))
	for k, v := range baseCatalog {
		out[k] = v
	}
	return out
}
```

First read `locale.go` to confirm `Catalog`'s exact type (map[string]string named type) and `NewLocales`'s base-layer semantics; adjust only if the real signatures differ, and say so in your report. The key list must exactly match the defaults the partials use after Step 3 — audit every partial for hardcoded default English (pagination's "Pagination", the search submit's label, confirm-form's "Cancel") and add one key per string found, no more.

- [ ] **Step 2: Wire the layer.** In `serve.go`'s `buildHandler`, change `NewLocales(opts.Locales, def, nil, opts.LocaleFS)` to pass `BaseCatalog()`. Test in `basecatalog_test.go`:

```go
func TestBaseCatalogResolvesThroughLocales(t *testing.T) {
	loc, err := NewLocales([]string{"en"}, "en", BaseCatalog(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := loc.T("en", "rastrillo.ui.cancel"); got != "Cancel" {
		t.Errorf("base layer not resolving: %q", got)
	}
	if got := loc.T("en", "no.such.key"); got != "no.such.key" {
		t.Errorf("missing keys still fall back to the key: %q", got)
	}
}
```
(Check `Locales.T`'s real signature in `locale.go` first — if it takes a locale code vs a request, match it; if the method shape differs, write the equivalent assertion through the real API and note it.) Also add a buildHandler-level test in the file that already tests buildHandler: an app with `Locales: []string{"en"}` and a `LocaleFS` whose `locales/en.toml` overrides `rastrillo.ui.cancel = "Never mind"` — assert the override wins over the base layer through the public request path (`T(r, "rastrillo.ui.cancel")` inside a test handler).

- [ ] **Step 3: ui funcs.** In `ui/funcs.go`:

```go
// Funcs returns the helpers ui's partials call... (extend the existing
// doc comment: T resolves the framework's default strings — English
// unless the app rebinds it via FuncsWith; partials call it only for
// their defaults, and every caller-supplied value still wins.)
func Funcs() template.FuncMap {
	return FuncsWith(defaultT)
}

// FuncsWith is Funcs with the T entry replaced: the seam for an app
// that wants ui defaults in the request's locale. Clone the parsed
// tree and re-bind:
//
//	tmpl.Funcs(ui.FuncsWith(func(key string, _ ...any) string {
//		return rastrillo.T(r, key)
//	}))
func FuncsWith(t func(key string, args ...any) string) template.FuncMap {
	return template.FuncMap{"dict": dict, "list": list, "icon": rastrillo.Icon, "T": t}
}

// defaultT resolves the framework base catalog and falls back to the
// key — the same last-resort rule the locale chain ends with (§10).
func defaultT(key string, _ ...any) string {
	if v, ok := rastrillo.BaseCatalog()[key]; ok {
		return v
	}
	return key
}
```

Then convert the default strings: `pagination.html`'s `{{if .Label}}{{.Label}}{{else}}Pagination{{end}}` → `{{if .Label}}{{.Label}}{{else}}{{T "rastrillo.ui.pagination"}}{{end}}`; the search submit's fixed label likewise (find its exact current form first); `confirm-form.html`'s `{{or .CancelLabel "Cancel"}}` → `{{if .CancelLabel}}{{.CancelLabel}}{{else}}{{T "rastrillo.ui.cancel"}}{{end}}`. Tests:

```go
func TestUIDefaultsResolveAndRebind(t *testing.T) {
	got := render(t, "pagination", map[string]any{})
	if !strings.Contains(got, `aria-label="Pagination"`) {
		t.Errorf("default label lost: %s", got)
	}
	// FuncsWith rebinds every default.
	tmpl := template.Must(template.New("").Funcs(FuncsWith(func(key string, _ ...any) string {
		return "X-" + key
	})).ParseFS(Templates(), "*.html"))
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "pagination", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `aria-label="X-rastrillo.ui.pagination"`) {
		t.Errorf("FuncsWith did not rebind T: %s", buf.String())
	}
}
```

- [ ] **Step 4: Contrast + motion gates.** Create `ui/contrast_test.go`: parse `TokensCSS()` for the `--rst-*` token definitions in both themes (the file's structure: a base `:root` block and a dark override — read it and parse accordingly, resolving `light-dark()` if used), compute WCAG relative luminance and contrast ratios in pure Go (no dependencies — the standard sRGB formula), and assert for both themes: every `--rst-tone-*-fg` on its `--rst-tone-*-bg` ≥ 4.5:1; `--rst-text`, `--rst-text-muted` on `--rst-bg` and `--rst-surface` ≥ 4.5:1; `--rst-text-faint` on `--rst-bg` ≥ 3:1 (it styles non-essential chrome — if it fails 3:1, flag BLOCKED rather than weakening the test); `--rst-on-accent` on `--rst-accent` ≥ 4.5:1; any token added by Tasks 1–4 (e.g. a `--rst-danger` pair if Task 4 added one) included. Tokens defined with `color-mix()` may be skipped with a named skip list and a comment — computing color-mix is out of scope; keep the list short and explicit. Motion gate in `ui_test.go`: every `transition:`/`animation:` line in `TokensCSS()` has its property disabled somewhere under a `prefers-reduced-motion: reduce` block — implement as: collect selectors carrying transitions, assert each appears (selector substring) inside a reduce block, with an explicit allowlist for any pre-existing violations found (list them in the test with a comment; do not fix pre-existing CSS silently).

- [ ] **Step 5: README + sweep + commit.** Root `README.md`: the `rastrillo/ui` Built bullet grows to name the vocabulary ("List screens plus the display, form, and route families — badges, meters, person cells, callouts, fields, choice cards, toggle blocks, seg-tabs, confirm forms, bulk select, modal shells — with framework strings resolved through the §10 locale chain"). Full sweeps: root + both examples. Commit:

```bash
git add basecatalog.go basecatalog_test.go serve.go ui/ README.md
git commit -m "ui+locale: T threading — framework base catalog, FuncsWith, partial defaults"
```
