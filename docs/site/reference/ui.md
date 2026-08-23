# 🤖 ui

`github.com/carlosframework/rastrillo/ui`

Rastrillo's server-shape component library: `html/template` partials, a
design-token stylesheet, and the template helpers they need — vendored
the same way icons are, so an app pulls in a working component with an
import and a `ParseFS` call rather than a hand-copy.

**It is a component library, not a screen generator.** Nothing here
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

Twenty-four partials span the list-screen, display, form and route
families. Each takes **exactly one** data value, built inline with
`dict`, and each partial's own file carries its data contract in a
comment above the `{{define}}`. `ui_test.go`'s
`TestAllPartialsAreDefined` is the authoritative list.

Three containers the partials assume but do not emit, because they
belong to your page markup: `<div class="rst-page">`,
`<div class="rst-list">` and `<form class="rst-form">`.

## Funcs

```go
func Funcs(opts ...Option) template.FuncMap
```

Registers `dict`, `list`, `icon` and `T`.

`dict` is how a partial's single data value is built at the call site:

```html
{{template "badge" dict "Label" "Draft" "Tone" "muted"}}
```

## Option, WithIcons, WithT

```go
type Option func(*config)
func WithIcons(icon func(string) template.HTML, assets func() template.HTML) Option
func WithT(t func(key string, args ...any) string) Option
```

`WithIcons` points both icon seams at the app's own scaffolded icons
package:

```go
tmpl := template.Must(template.New("").
	Funcs(ui.Funcs(ui.WithIcons(icons.Icon, icons.Assets))).
	ParseFS(ui.Templates(), "*.html"))
```

`{{iconAssets}}` goes in the layout's `<head>` and renders **empty** for
the vendored-inline default, so it is safe to call unconditionally and
the layout never changes when the delivery mode does.

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

A caller-supplied value always wins over a partial's default, and an app
catalog entry wins over the framework base catalog. See
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

All three are **delivered once and app-owned from then on**: edit them
freely, and nothing in the framework overwrites them. The scaffold's
`vendored_test.go` pins the delivered copies byte-identical to these, so
drift is something you choose rather than discover — update or delete
that test in the same commit as a deliberate edit. See
[Assets](/docs/assets).
