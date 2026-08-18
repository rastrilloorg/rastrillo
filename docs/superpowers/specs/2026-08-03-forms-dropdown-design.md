# F2 + F3: form partials and the dropdown — design

**Date:** 2026-08-03
**Status:** approved
**Closes:** friction-log F2 (form partials + focus ring stops at the
library's edge) and F3 (no dropdown, so the admin list has no status
filter) — both recorded in `examples/blog/README.md`.

## Why

The blog example — the north-star sample app — hand-rolls every form
control and restates the library's focus ring, because the `field`
family was deferred out of the components slice (PR #4). Its admin list
ships without the obvious next control, a status filter, because the
library had no dropdown to attach. Roughly half of `blog.css` exists
only to cover this gap. This slice ships the missing partials and proves
them by adopting them in the blog.

## What ships

Four new partials in `ui/partials/`, one `{{define}}` per file, following
the as-built conventions exactly (PascalCase dict keys, data contract in
a template comment above the define, every optional key guarded, `rst-`
class prefix, no catalog strings — callers pass all visible text):

### `field-text`

Keys: `Name` (required — the input's `name`, `id`, and the prefix for
described-by ids), `Label` (required), `Value`, `Type` (default `text`),
`Required` (bool — renders the `required` attribute and a `*` marker),
`Hint`, `Error`, `Autocomplete`.

Renders:

```html
<div class="rst-field">
  <label for="{Name}">{Label}[ *]</label>
  <input class="rst-input" id="{Name}" name="{Name}" type="{Type}"
         value="{Value}" [required] [autocomplete=…]
         [aria-invalid="true"] [aria-describedby="{Name}-hint {Name}-error"]>
  [<small class="rst-field__hint" id="{Name}-hint">{Hint}</small>]
  [<small class="rst-field__error" id="{Name}-error">{Error}</small>]
</div>
```

`aria-describedby` lists only the ids actually rendered. `Error` also
sets `aria-invalid="true"`.

### `field-textarea`

Same wrapper, keys and a11y wiring as `field-text`, minus
`Type`/`Autocomplete`, plus `Rows` (optional; the attribute is omitted
when unset so the browser default applies). The control is
`<textarea class="rst-textarea">` with `{Value}` as its content.

### `form-foot`

Keys: `Submit` (required — the primary button's text), `CancelHref` +
`CancelLabel` (optional pair — a plain link styled `rst-btn`).

```html
<div class="rst-form__foot">
  <button class="rst-btn rst-btn--primary" type="submit">{Submit}</button>
  [<a class="rst-btn" href="{CancelHref}">{CancelLabel}</a>]
</div>
```

No danger variant: the blog's destructive actions live in its status
strip, not a form foot. Add it when a consumer exists.

### `dropdown`

The zero-JS disclosure: a native `<details>`, the only one HTML has.
Keys: `Label` (required — the summary's visible text, e.g. the current
filter value), `Items` (required — list of dicts: `Href`, `Label`,
`Current` bool), `Aria` (optional `aria-label` for the summary when its
visible text is not a full accessible name — "Status: All" vs "All").

```html
<details class="rst-dropdown">
  <summary class="rst-dropdown__summary" [aria-label="{Aria}"]>{Label} {icon chevron-down}</summary>
  <div class="rst-dropdown__menu">
    <a href="{Href}" [aria-current="true"]>{Label} [{icon check}]</a>
    …
  </div>
</details>
```

Every item is a plain link — one click applies a filter, no submit, no
JS. The current item carries `aria-current="true"` and the vendored
`check` icon (both `chevron-down` and `check` are already in icons.go).
The panel does not close on outside click without JS; that is the
accepted cost of zero-JS, stated in the partial's contract comment.

### `list-bar` gains `Filter`

Optional key, a dict passed straight through to `dropdown`, rendered
after the search form in the space the bar already reserves ("when the
dropdown slice lands" — that comment goes away). No `Filter`, no change:
the existing markup byte-for-byte. The ui_test assertion "list-bar
renders no dropdown" becomes "renders one only when Filter is passed".

## tokens.css

All values from tokens, same file conventions:

- `.rst-form` — vertical flex column, `gap: var(--rst-sp-2)`,
  `max-inline-size: 44rem` (the blog's measure, promoted). The app owns
  the `<form class="rst-form">` element, as it owns `.rst-page` and
  `.rst-list`.
- `.rst-field`, `.rst-field__hint`, `.rst-field__error` — label styling
  from `.blog-form label`, hint muted, error in
  `--rst-tone-negative-fg`.
- `.rst-input`, `.rst-textarea` — the blog's control rules moved in:
  surface background, `--rst-line-strong` border, `--rst-radius-sm`,
  `font: inherit`; textarea adds `line-height: 1.6; resize: vertical`.
- `.rst-form__foot` — flex row, `gap: var(--rst-sp-3)`, top margin.
- `.rst-dropdown` — `position: relative`; the menu
  `position: absolute`, surface background, line border, radius, shadow,
  `min-inline-size` enough for short labels; summary styled like
  `rst-btn` (marker hidden, chevron as the affordance). No shadow token
  exists yet, so the slice adds `--rst-shadow` (a light value and a
  dark-theme override, like every other themed token) rather than
  hardcoding one.
- **The F2 focus fix:** `.rst-page` joins the existing `:where()` scope
  list for `:focus-visible`. The ring then covers everything in the app
  column — library partials, the new controls, and hand-rolled ones —
  without a global rule (safe for embedding). The new containers need no
  separate entry.

## Blog adoption (the proof)

- `admin_new.html` / `admin_edit.html`: forms become
  `<form class="rst-form">` + `field-text` (Title, required) +
  `field-textarea` (Body, Rows 18) + `form-foot` (Save / Back to
  posts).
- `admin_list.html`: `list-bar` gets
  `Filter` = Status dropdown — All / Drafts / Published, hrefs carrying
  the current search query, current item marked. The handler and store
  gain a `status` parameter alongside the existing search; the
  pagination and search `Hidden` pairs carry `status` across.
- `blog.css`: the `.blog-form` block and the focus-ring restatement are
  deleted (`.blog-error`, `.blog-note`, `.blog-status`, article and
  footer rules stay — they are page styling, not gap-covering). The
  file's two rules (tokens only, no `.rst-` styling) keep holding.
- `examples/blog/README.md`: F2 and F3 gain *Fixed:* postscripts in the
  friction-log style already used by F9/F10.

`rastrillo new` needs no change: it writes `ui.TokensCSS()`, so new apps
pick the rules up automatically.

## Testing

- `ui/ui_test.go`, existing table style: each new partial rendered with
  minimal and maximal keys; assertions on the a11y wiring
  (`aria-describedby` lists only rendered ids, `aria-invalid` only with
  Error, `aria-current` only on Current, `Aria` only when passed);
  list-bar with and without `Filter`; no `<script>` anywhere (the
  existing zero-JS sweep covers new partials automatically).
- `examples/blog/internal/blogtest`: the filter round-trip — `?status=draft`
  lists only drafts, filter + search compose, the current item is
  marked, pagination carries `status`.
- The blog's existing css-conventions tests keep passing against the
  shrunk `blog.css`.

## Out of scope

`field-select`, the generic `field` wrapper, `toggle-block`,
`seg-tabs`, `menu`-as-row-actions, `confirm`, `modal-route`,
`bulk-select` (design doc §9's remaining vocabulary) — no consumer yet;
the manifest slice composes what exists when it lands. F1 (status slot
on `list-row-action`) is separate and untouched here.
