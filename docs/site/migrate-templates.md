# 🤖 Upgrading templates

This release changes how templates call `T` and the other locale
functions. Each call takes the page's localizer, `$L`, first, and each
partial gets it as `"L" $L`. Every app needs the change, whether it
stores money or not. `rastrillo migrate-templates` makes most of it for
you, and lists the rest.

## See what it would do

Run it on your app with no flags. It changes nothing:

```sh
rastrillo migrate-templates .
```

This is its report on an app written before this release:

```text
internal/notes/templates/edit.html                               calls   0  dicts  4  wrapped  0  preludes  1
internal/notes/templates/genlayout.html                          calls   0  dicts  0  wrapped  1  preludes  1
internal/notes/templates/new.html                                calls   0  dicts  4  wrapped  0  preludes  1
internal/notes/templates/signin.html                             calls   0  dicts  4  wrapped  1  preludes  1
internal/notes/templates/signup.html                             calls   0  dicts  4  wrapped  1  preludes  1
internal/notes/templates/status.html                             calls   0  dicts  0  wrapped  1  preludes  1
dry run: 6 templates; 0 locale calls, 16 dict calls, 4 wrapped calls, 6 preludes
  review (if this data is a struct, it needs an L field): internal/notes/templates/genlayout.html: template "content" .Content
  review (if this data is a struct, it needs an L field): internal/notes/templates/signin.html: template "form-error" .Error
  review (if this data is a struct, it needs an L field): internal/notes/templates/signup.html: template "form-error" .Error
  review (if this data is a struct, it needs an L field): internal/notes/templates/status.html: template "job-status" .Content
  go: internal/notes/genrender.go:39: function map entry "T"; remove it: the localizer's catalog now serves T; give that catalog to ui.Pages as the locale's T
  gen holds generated templates in the old syntax; run rastrillo generate
  pending: 6 templates need the automatic rewrite; run with -write
```

Each file line counts the changes it would make to that file. The
indented lines are the list you work through by hand, below.
`rastrillo doctor` runs the same report under `Templates:`.

## What it changes

With `-write` it rewrites your templates in place:

1. Every call of `T`, `Tf`, `Tbdi`, `number`, `displayValue`,
   `dateWords`, `money` or `actor` gets `$L` as its first argument.
2. Every `{{template "name" dict ...}}` gets `"L" $L`.
3. Any other data passed to a partial is wrapped as
   `(withL (DATA) $L)`, and listed for review.
4. Every template body that now uses `$L` starts with
   `{{$L := localizer $}}`.

Here is part of a sign-in page, before:

```html
{{define "content"}}
{{template "page-header" dict "Title" "Sign in"}}
{{template "form-error" .Error}}
```

and after:

```html
{{define "content"}}{{$L := localizer $}}
{{template "page-header" dict "L" $L "Title" "Sign in"}}
{{template "form-error" (withL (.Error) $L)}}
```

It only inserts, at places the template parser reports, and it parses
each result again before writing it. A rewrite that does not check out
is not written. Running it twice changes nothing the second time.

## Work through the list

**`review`**: data passed to a partial got wrapped in `withL`. That adds
`L` to a map. A struct cannot gain a field, so if the data is a struct,
give it an `L` field: embed `ui.Localized`. Otherwise the partial fails
when it renders.

**`no data`**: a `{{template "name"}}` passes no data at all, so the
partial gets no localizer. Pass it `.`, or a `dict` with `"L" $L`.

**`refused`**: the file already uses `$L` for something else, so it was
left alone. The line names each place. Rename your variable and run the
command again.

**`go`**: your Go code registers its own `T`, or another locale
function, in a template function map, or calls `ui.WithT` or
`ui.FuncsWith`. The new templates call `T $L "key"`, which a
one-argument `T` cannot take. Remove it, and render through
[`ui.Pages`](/docs/reference/ui#pages), which gives each language's
catalog to its localizer. A `go (review)` line is a key the check could
not read as a constant: make it one, or check by hand that it can never
be a locale function's name.

**`gen holds generated templates in the old syntax`**: run
`rastrillo generate`. Generated templates are written in the new syntax
already.

## Finish

```sh
rastrillo migrate-templates -write .
```

Then work through the list, run `rastrillo generate`, and run
`rastrillo migrate-templates .` again. It exits 1 while anything is
left to do, a pending rewrite or a list item, so it works as a CI check.
When it exits 0, so does the `Templates:` part of `rastrillo doctor`,
which exits 6 until then.

If you cannot finish in one go, set `Compat: true` in your
`ui.PagesConfig`. A call without `$L` then renders in your default
language instead of failing, and is logged in capitals. Turn it off
when the command reports nothing.
