# 🤖 ui

`github.com/carlosframework/rastrillo/ui`

Rastrillo's server-shape component library: `html/template` partials, a
design-token stylesheet, and the template helpers they need. They are
vendored the same way icons are, so you pull in a working component with
an import and a `ParseFS` call instead of a hand-copy.

It is a component library, not a screen generator. Nothing here
generates a screen, decides a route, or owns rendering.

[Templates and the UI vocabulary](/docs/templates) is the guide.

## Templates

```go
func Templates() fs.FS
```

The partial set, for `ParseFS`:

```go
tmpl := template.Must(template.New("").Funcs(ui.Funcs()).
	ParseFS(ui.Templates(), "*.html"))
tmpl = template.Must(tmpl.ParseFS(appTemplateFS, "templates/*.html"))
```

The partials span the list-screen, display, form and route families.
Each takes exactly one data value, built inline with `dict`, and each
partial's file carries its data contract in a comment above the
`{{define}}`. `ui_test.go`'s `TestAllPartialsAreDefined` is the
authoritative list.

The partials assume three containers they do not emit, because those
belong to your page markup: `<div class="rst-page">`,
`<div class="rst-list">` and `<form class="rst-form">`.

## Funcs

```go
func Funcs(opts ...Option) template.FuncMap
```

Registers `dict`, `list`, `icon` and `T`.

`dict` builds a partial's single data value at the call site:

```html
{{template "badge" dict "Label" "Draft" "Tone" "muted"}}
```

## Option, WithIcons, WithT

```go
type Option func(*config)
func WithIcons(icon func(string) template.HTML, assets func() template.HTML) Option
func WithT(t func(key string, args ...any) string) Option
```

`WithIcons` points both icon seams at your own scaffolded icons
package:

```go
tmpl := template.Must(template.New("").
	Funcs(ui.Funcs(ui.WithIcons(icons.Icon, icons.Assets))).
	ParseFS(ui.Templates(), "*.html"))
```

`{{iconAssets}}` goes in the layout's `<head>` and renders empty for the
vendored-inline default, so you can call it unconditionally and never
touch the layout when the delivery mode changes.

`WithT` rebinds the `T` function.

## FuncsWith

```go
func FuncsWith(t func(key string, args ...any) string) template.FuncMap
```

`Funcs` with `T` bound to a request-scoped lookup, so a partial's own
hardcoded-English defaults — `pagination`'s "Pagination",
`confirm-form`'s "Cancel" — resolve in the request's locale:

```go
tmpl.Funcs(ui.FuncsWith(func(key string, args ...any) string {
	return rastrillo.Tf(r, key, args...)
}))
```

A value you supply beats a partial's default, and your catalog entry
beats the framework base catalog. See
[Localization](/docs/localization).

## The vendored assets

```go
func TokensCSS() []byte
func ShimJS() []byte
func SelectJS() []byte
```

`TokensCSS` is the design-token stylesheet `rastrillo new` writes once
into the app's `static/`. `ShimJS` is `rastrillo.js` — the
progressive-enhancement shim that drives `data-poll`, `data-poll-push`
and `data-busy` ([Background jobs](/docs/jobs)). `SelectJS` backs the
enhanced select.

All three are delivered once and yours from then on. Edit them freely;
nothing in the framework overwrites them. The scaffold's
`vendored_test.go` pins the delivered copies byte-identical to these, so
drift is something you choose rather than discover — update or delete
that test in the same commit as a deliberate edit. See
[Assets](/docs/assets).
