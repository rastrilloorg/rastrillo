# 🤖 Templates and the UI vocabulary

You render with `html/template`. There is no template language of its
own, and `ui` is a component library — nothing in it generates a screen,
decides a route, or owns rendering.

## One template per page

```go
tmpl := template.Must(template.New("").Funcs(ui.Funcs()).
	ParseFS(ui.Templates(), "*.html"))
tmpl = template.Must(tmpl.ParseFS(appTemplateFS, "templates/*.html"))
```

Parse layout plus one page per template, rather than one tree containing
everything. Two pages can then both define `"content"`; in one tree the
second `{{define "content"}}` would win for both.

`render.go` is also where `flash.Take(w, r)` gets called, once per page,
so the layout can render a notice. See [Forms](/docs/forms).

## Template functions

`ui.Funcs()` registers `dict`, `list`, `icon` and `T`.

Each partial takes exactly one data value, and `dict` is how you build
it at the call site:

```html
{{template "badge" dict "Label" "Draft" "Tone" "muted"}}
```

If you scaffolded your own icon set, point both icon seams at it:

```go
tmpl := template.Must(template.New("").
	Funcs(ui.Funcs(ui.WithIcons(icons.Icon, icons.Assets))).
	ParseFS(ui.Templates(), "*.html"))
```

`{{iconAssets}}` goes in the layout's `<head>`. It renders empty for the
vendored-inline default, so you can call it unconditionally and never
edit the layout when the delivery mode changes — see
[Icons](/docs/icons).

`ui.WithT` and `ui.FuncsWith` rebind `T` to a request-scoped lookup, so
a partial's built-in strings resolve in the request's locale. See
[Localization](/docs/localization).

## The partials

They span the list-screen, display, form and route families:

```text
badge          bulk-bar       callout        choice-field
confirm-form   detail-list    dropdown       empty-state
field          field-check    field-select   field-text
field-textarea form-foot      job-status     list-bar
list-bar-search list-row-action locale-menu   meter
page-header    pagination     person         seg-tabs
status-pill
```

`locale-menu` is the language switcher; see
[Localization](/docs/localization).

Each partial's file carries its data contract in a comment above the
`{{define}}`, and `ui_test.go`'s `TestAllPartialsAreDefined` is the
authoritative list.

### Three containers the partials assume

They belong to your page markup, so the library does not emit them:

```html
<div class="rst-page">   <!-- the centred content column every screen sits in -->
<div class="rst-list">   <!-- the card wrapping a list-bar and a run of rows -->
<form class="rst-form">  <!-- the column a run of fields and a form-foot sit in -->
```

There is also a class idiom vocabulary — section box, list grid,
dropdown, filter tokens, help tooltip, selection checkbox — that is CSS
rather than a Go partial. The `ui` package's doc comment has the full
list.

### Which card is which

Two of those containers look like cards and are not the card you want
for ordinary content. `rst-list` and `rst-card` have **no padding by
design**: they hold a run of rows, and each row pads itself. Put a form,
a paragraph, a strip of links or anything else that is not a row straight
into one and it renders flush against the border — the text touching the
edge is the tell.

The padded card for arbitrary content is `rst-box`, with its heading as
a sibling `rst-box-head` before it:

```html
<div class="rst-box-head"><h2>Sign in</h2></div>
<section class="rst-box">
  <form class="rst-form" method="post" action="/signin">…</form>
</section>
```

`rst-form` is a hook the form partials assume, not a container: it draws
nothing on its own, so it needs a `rst-box` (or the bare page) around it.

### Screens stack vertically

A screen is a column: page-header, then section-header + card, then the
next section-header + card, in reading order. Do not compose a heading, a
paragraph and a button side by side in a flex row — a three-word heading
wrapped onto three lines beside a full-width paragraph and a tall narrow
button is what that produces at any real width. A notice that needs a
call to action is either a `callout` whose body ends in a link, or a
`rst-box-head` (the `<h2>` plus one compact `rst-btn`) over a `rst-box`
holding the explanation. Horizontal arrangement is reserved for the
idioms that ship it: `rst-box-head`, `rst-field-row`, `rst-lbar`,
`rst-lrow` cells, `rst-seg-tabs`.

## Styling

`ui.TokensCSS()` is the design-token stylesheet, and `rastrillo new`
writes it once into your `static/` directory. From that moment it is
yours: edit it freely, and nothing in the framework will overwrite it.

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

`Fail` logs the real error and answers a safe 500, so the detail reaches
your logs and never the response body. `ParseID` reads the `{id}` path
value; a malformed one answers `false`, which your handler should turn
into a 404 rather than a 400. See [Scoping](/docs/scoping).
