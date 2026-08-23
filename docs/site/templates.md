# 🤖 Templates and the UI vocabulary

Rastrillo apps render with `html/template`. There is no template
language of its own, and `ui` is a component library rather than a
screen generator — nothing in it generates a screen, decides a route, or
owns rendering.

## One template per page

```go
tmpl := template.Must(template.New("").Funcs(ui.Funcs()).
	ParseFS(ui.Templates(), "*.html"))
tmpl = template.Must(tmpl.ParseFS(appTemplateFS, "templates/*.html"))
```

Parse **layout plus one page** per template, rather than one tree
containing everything. Two pages can then both define `"content"`, which
they otherwise could not — the second `{{define "content"}}` would win
for both.

`render.go` is also where `flash.Take(w, r)` is called, once per page,
so the layout can render a notice. See [Forms](/docs/forms).

## Template functions

`ui.Funcs()` registers `dict`, `list`, `icon` and `T`.

`dict` is how you build a partial's data inline, because each partial
takes exactly one value:

```html
{{template "badge" dict "Label" "Draft" "Tone" "muted"}}
```

An app with its own scaffolded icon set points both icon seams at it:

```go
tmpl := template.Must(template.New("").
	Funcs(ui.Funcs(ui.WithIcons(icons.Icon, icons.Assets))).
	ParseFS(ui.Templates(), "*.html"))
```

`{{iconAssets}}` goes in the layout's `<head>`. It renders empty for the
vendored-inline default, so it is safe to call unconditionally and you
never have to edit the layout when the delivery mode changes — see
[Icons](/docs/icons).

`ui.WithT` and `ui.FuncsWith` rebind `T` to a request-scoped lookup, so
a partial's built-in strings resolve in the request's locale. See
[Localization](/docs/localization).

## The partials

Twenty-four, spanning the list-screen, display, form and route families:

```text
badge          bulk-bar       callout        choice-field
confirm-form   detail-list    dropdown       empty-state
field          field-check    field-select   field-text
field-textarea form-foot      job-status     list-bar
list-bar-search list-row-action meter        page-header
pagination     person         seg-tabs       status-pill
```

Each partial's own file carries its data contract in a comment above the
`{{define}}`, and `ui_test.go`'s `TestAllPartialsAreDefined` is the
authoritative list.

### Three containers the partials assume but do not emit

They belong to your page markup, not to the library:

```html
<div class="rst-page">   <!-- the centred content column every screen sits in -->
<div class="rst-list">   <!-- the card wrapping a list-bar and a run of rows -->
<form class="rst-form">  <!-- the column a run of fields and a form-foot sit in -->
```

There is also a class idiom vocabulary — list grid, dropdown, filter
tokens, help tooltip, selection checkbox — that is CSS rather than a Go
partial. The `ui` package's doc comment has the full list.

## Styling

`ui.TokensCSS()` is the design-token stylesheet, which `rastrillo new`
writes once into your app's `static/` directory. From that moment it is
**app-owned**: edit it freely, and nothing in the framework will
overwrite it.

The scaffold ships a `vendored_test.go` pinning the delivered copy
byte-identical to the library's, so you find out you have drifted when
you meant to, rather than at an upgrade. Delete or update that test when
you intend to diverge. See [Assets](/docs/assets).

## The view helpers

For generated actions working against a `*rastrillo.Ctx`:

```go
func Render(ctx *rastrillo.Ctx, w http.ResponseWriter, page string, status int, data any)
func Fail(ctx *rastrillo.Ctx, w http.ResponseWriter, what string, err error)
func ParseID(r *http.Request) (int64, bool)
```

`Fail` logs the real error and answers a safe 500 — the detail reaches
your logs, never the response body. `ParseID` reads the `{id}` path
value; a malformed one answers `false`, which your handler should turn
into a 404 rather than a 400. See [Scoping](/docs/scoping).
