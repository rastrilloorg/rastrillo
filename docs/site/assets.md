# 🤖 Assets

`rastrillo.Assets` fingerprints an app's static files, so they can be
cached forever and still update on an ordinary reload.

```go
assets := rastrillo.NewAssets(app.StaticFS)
mux.Handle("GET /static/", assets.Handler())
```

`Path` maps a file to a URL carrying its content hash; `Handler` serves
that URL with an immutable `Cache-Control`. Because the hash changes
whenever the content does, the HTML always links a URL the browser has
never cached stale — no cache-busting query string, no version constant
to remember to bump.

The FS is served with `http.FileServerFS` semantics — URL path is `/`
plus the FS path — which matches how the scaffold embeds `static/`.
Mount at `GET /static/` with **no** `StripPrefix`.

## Delivered once, yours from then on

Two files arrive in your app's `static/` directory from `rastrillo new`
rather than from an import: `tokens.css`
([the design tokens](/docs/templates)) and `rastrillo.js`
([the progressive-enhancement shim](/docs/jobs)). The scaffolded icons
package works the same way.

The terms are the same for all three: delivered once, app-owned
afterwards. Edit them freely. Nothing in the framework will overwrite
them, and no upgrade will change them under you.

## The pin test

The scaffold ships a `vendored_test.go` asserting the delivered copies
are **byte-identical** to the library's.

That is not a prohibition on editing them — it is a tripwire. It tells
you that you have drifted, so drift is something you chose rather than
something you discover much later while debugging a component that no
longer matches its stylesheet.

When you do mean to diverge, update or delete that test in the same
commit as the edit. A pin test that everybody knows is failing protects
nothing.
