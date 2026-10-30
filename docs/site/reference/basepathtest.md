# 🤖 basepathtest

`amadan.net/rastrillo/rastrillo/basepath/basepathtest`

Finds links in a page that would break under a [`basepath`](/docs/reference/basepath).

## Leaks

```go
func Leaks(body []byte, base string) []string
```

`Leaks` returns every `href`, `src`, `action`, `formaction`, `poster` and `data-*` value in `body` that starts with one `/` and is not under `base`. Each comes back as it appears in the page, such as `href="/static/t.css"`, in page order. With `base` empty, it returns nil.

Run it over every page your app renders under a base path, in your own tests. A leaked path works at the root and 404s under `/<slug>`.
