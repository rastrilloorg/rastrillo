# Compiled templates: html/template pages rendered by generated Go — design

**Date:** 2026-10-29
**Status:** Approved (Paul, in conversation: "OK, let's see it.")
**Branch:** `compiled-templates`
**Written by:** Claude Opus 5.5 (claude-opus-5-5). Builder check by
GPT-6 Astra (high reasoning); its three findings are settled in 3, 5
and 7.

## Why

In the TodoMVC bake-off (`~/amadan.net/carlos/todomvc-bakeoff`,
`docs/writeup.md`), Go + Rastrillo served 336 page loads a second on
Carloku's default instance. Nim and Zig served about 1,400 to 1,500, and
Rust reached 1,398. A profile of the Go app at 64 connections put
`html/template` at about 65% of CPU, mostly reflection calls to its
escapers, with garbage collection at 16% and SQLite at 7%.

When the plain Go app's page was rewritten in templ, which compiles
templates to Go, it served 3.8x as many page loads on one core (4,216
against 1,122). A second measurement on Rastrillo's tickets list
(2026-10-28) found rendering about 6x faster and allocations down from
2,164 to 781 per request. The whole request was 1.7 to 2x faster there,
because the database was a larger share.

Paul wants that speed without changing how apps are written. App
authors (mostly agents) keep writing `html/template`. Rastrillo turns
those templates into Go behind the scenes.

## What success looks like

1. **The bake-off number.** The bake-off's `apps/go-rastrillo`, moved
   onto compiled templates, is rerun on Carloku with the same load ramp.
   It serves **more than 1,000 page loads a second** on the 50-item page
   and still passes all 67 shared tests. That puts it alongside Nim,
   Zig and Rust. Paul gives the go-ahead for the Carloku deploy and run
   when the build is ready; the builder does not deploy on their own.
2. **A local proxy, before any deploy.** A benchmark in this repo
   renders a copy of the bake-off page (template and data). The
   compiled renderer uses at least **3x less CPU per page** than
   `html/template` on one core, with GOMAXPROCS=1 and GOMAXPROCS=8 both
   recorded. 336 × 3 ≈ 1,000. Allocations per page are recorded next to
   templ's 781 on the tickets list as a reference point.
   *2026-10-30:* `BenchmarkTodoMVC`, 50 items, median of 6, shadow off.
   GOMAXPROCS=1: `html/template` 633,602 ns/op and 5,097 allocs/op,
   compiled 12,399 ns/op and 6 allocs/op, 51x less. GOMAXPROCS=8
   (wall time per page across 8 cores): 149,797 ns/op and 5,097
   allocs/op against 2,274 ns/op and 6 allocs/op, 66x.
3. **Nothing visible breaks.** Every page renders the same document it
   renders today: the same elements, attributes, text and escaping.
4. **View-source looks hand-written** (phase 2, below). Paul: "If it
   were tidier I would be happier ... e.g. if the nesting and formatting
   were perfect."

## Goals

- Pages render through generated Go code, not through `html/template`'s
  interpreter, wherever the compiler supports what the page uses.
- App authors change nothing in their templates and nothing in their
  workflow. `rastrillo generate` produces the compiled code for every
  app, whether or not it has a manifest.
- The compiled path is never less correct than `html/template`. When
  there is any doubt (a stale build, a construct the compiler does not
  handle, data of a type it did not expect), that template runs on
  `html/template`.
- Phase 2: the HTML is formatted with consistent nesting and
  indentation.

## Non-goals

- **Write throughput.** Go + Rastrillo did 309 toggles a second against
  plain Go's 810. Templates don't explain that gap. Record it as a
  follow-up and leave it alone here.
- **SQLite throughput.** On the tickets app the single SQLite
  connection serialises requests on 8 cores, and re-parsing SQL text is
  about 17% of a request. Both matter later, but not in this project.
- **A new template language.** Not templ, and no new syntax in
  `html/template` files.
- **Reformatting HTML that only exists at runtime.** A `template.HTML`
  value (rendered Markdown, an SVG icon) goes out exactly as it
  arrives. Reformatting it would mean parsing it per request.

## Anti-goals (a failure even if it is faster)

- Ugly view-source. Phase 1 output is no worse than today's. Phase 2
  makes it better.
- Any change to how a page renders in a browser, or any weaker
  escaping.
- Stale HTML: a page showing an older template than the one on disk
  because compiled code was not regenerated.
- A page that fails to render because of the compiler when
  `html/template` would have rendered it.
- App authors having to annotate templates, declare types, or learn
  anything new to get the speed.

## How it fits together

### 1. One template set type that both paths sit behind

Rastrillo gets a template set type with the shape of `html/template`'s
`*Template`: `New`, `Funcs`, `Parse`, `ParseFS`, `ParseFiles`,
`Lookup`, `Clone`, `Templates`, `Name`, `Execute` and `ExecuteTemplate`.
It is backed by a real `*html/template.Template`. Parsing, function
maps and errors behave exactly as they do today. The package name and
location are the builder's call (for example
`amadan.net/rastrillo/rastrillo/tmpl`).

- `ui.Pages` builds its base and per-page clones from it.
- `live.ExecuteRegions` / `live.WriteRegions` and `live.Render` accept
  it. Their existing `*html/template.Template` signatures stay for apps
  that pass one, and those apps simply don't get compiled code.
- An app that parses its own templates switches with an import change.
  The bake-off's `internal/todo/render.go` changes `"html/template"` to
  the new package and nothing else, if the names line up. Any scaffold
  that still writes its own `html/template` setup is moved over.
- The execution path must not take `html/template`'s namespace mutex
  per request (`ExecuteTemplate` locks it today,
  `html/template/template.go:146`).

### 2. Matching compiled code to templates: fingerprints

The generated code registers each compiled template under a
**fingerprint**: a hash of its parse tree as escaped by the fork in 3, the escaped trees of
every template it calls (resolved in its own set, so an app overriding
a ui partial changes the fingerprint of everything that calls it), and
the names and Go types of the functions it calls.

When a set is first executed, the fork escapes it and each template
looks up its fingerprint. A hit runs the compiled
function, and a miss runs `html/template`. This decides what runs from
the templates actually loaded, so:

- **Stale code can't serve old HTML.** An edited template has a new
  fingerprint and falls back. In `rastrillo dev`, which already rebuilds
  on template saves, the refreshed page always shows the file on disk.
- **The way a set was built doesn't matter.** Overrides, clones, ejected
  pages and per-page sets all work.
- A built binary whose templates fell back for staleness logs one
  warning per set at first use, naming the templates and saying "run
  `rastrillo generate`". In `rastrillo dev` this is quiet (debug level),
  because a save is expected to outrun generate. A fallback for an
  unsupported construct or an unexpected data type is logged at debug
  only, because it is a known, reported state (see 5).

### 3. Escaping: a fork of the stdlib's escaper, held to the stdlib

The compiler can't simply read `html/template`'s escaped trees. When a
template is called from a context other than plain HTML, as with ui's
layouts calling `{{block "title"}}` inside `<title>`
(`ui/layouts/topbar.html:2`), escaping creates a derived copy of the
callee. That copy is added only to the private `text/template`
namespace (`html/template/escape.go:930`), and `Templates` and `Lookup`
can't reach it (`template.go:46`, `template.go:355`). Falling back for
those templates would leave every layout, and so every page,
uncompiled.

**Decision (my call):** Rastrillo carries a fork of `html/template`'s
escaping pass and escaper functions (`escape.go`, `context.go`,
`transition.go`, and the escapers in `js.go`, `css.go`, `url.go`,
`html.go`, `attr.go`, `content.go`: about 3,700 lines in Go 1.26),
copied under Go's BSD licence. It runs on the parse trees the set
already holds, and the compiler reads its output, derived templates
included. Reaching into `html/template`'s unexported fields with
`unsafe` was rejected. That would put escaping, the part that must
never be wrong, on the internals of whatever Go version the app is
built with.

The fork must be held to the standard library:

- The escaped trees it produces must make text/template output exactly
  what `html/template` outputs. The equivalence corpus (Testing)
  checks this through the compiled path.
- The fork records the Go version it was copied from. A test fails
  when the toolchain's minor version differs, naming the files to
  re-sync. A Go upgrade then becomes a deliberate re-sync instead of a
  silent drift.
- The ported escapers get typed fast paths: a `string`, an `int` or a
  `template.HTML` is escaped without going through `fmt` or `reflect`.
  A fuzz test compares each one against the standard library's
  escaper, reached through a one-action template in the matching
  context, on the same input.

**Equivalence is with a fresh set.** Compiled code writes
what `html/template` writes for the same entry executed first on a fresh
set. `html/template` escapes a set's trees in place the first time each
entry runs, so on a set shared by several entries its output can depend
on what ran before: a partial executed on its own is escaped for HTML,
and a page that then calls it inside `<script>` escapes the value
twice. Compiled code does not repeat that history. Shadow mode compares
it with a clone of the set taken before anything executed, on which
only that entry runs.

The `html/template` fallback path is unchanged and keeps using the
standard library itself.

### 4. Types: inferred, guarded, never declared

The big win comes from reading struct fields directly instead of
through reflection. Authors don't declare types, so `rastrillo
generate` infers them:

- It type-checks the app's packages (`go/packages`) and, for each
  template, collects the field and method paths the template uses on
  `.` (for example `.Filter`, `.Items` ranged with `.ID` and `.Title`).
- Every named type in the app's packages whose fields and methods cover
  those paths, with result types that fit how the template uses them,
  is a **candidate**. The compiler emits a typed fast path for each
  candidate (value and pointer), up to a small cap. The builder sets
  it; four is a reasonable start.
- At runtime the compiled function switches on the data's dynamic type.
  A candidate runs the typed path. Anything else runs a dynamic path
  that still calls functions and escapers directly but reads fields by
  reflection, or falls back to `html/template` if the builder finds the
  dynamic path isn't worth keeping.
- `map[string]any` data (the bake-off's error page, and every `dict`)
  is read with map lookups, not reflection.

ui's partials are mostly called with `dict "Key" value ...` and read
through reflection helpers (`optKey`, `opt`, `optString` in
`ui/funcs.go`). Phase 1 compiles them on the dynamic path. Phase 3
specialises a partial per call site: the keys are constants and the
caller's value types are known, so the call can pass a generated struct
instead of building a map. ui exports whatever support the generated
code needs for its own functions (`rowMenuItems`, `paginationItems`),
in a form that doesn't widen its public API for app authors (an
`internal`-style support package is fine).

App functions in a `FuncMap` (`asset`, `plural`, `icon`) are called
directly when generate can see their Go types: at capture (below)
their `reflect.Type` is known, and the generated code type-asserts the
map entry once at registration, not per call. A signature the compiler
can't express falls back to a reflection call for that function only.

### 5. What `rastrillo generate` does

1. **Capture.** generate needs the exact sets the app builds, including
   the effects of `Funcs`, overrides and clones. Apps build them at
   different points in startup. The scaffold and the bake-off build
   them at package init (`var pages, pagesErr = newPages()`,
   `var templates = template.Must(...)`). Tickets and blog build them
   inside `Options.Router` after migrations
   (`examples/tickets/cmd/tickets/main.go:72`), and notes builds them in
   `App` after its own migration (`examples/notes/internal/notes/app.go:111`).

   **Decision (my call):** generate runs the app's real startup in
   capture mode. It builds the app's `cmd/<name>` with a build tag that
   leaves out existing generated template files, so a stale one that
   no longer compiles can't block regeneration. It runs the binary in
   a scratch working directory (so a relative `DBPath` gets a
   throwaway database) with a capture environment variable set. The set
   type records every set as it is built, along with the package that
   built it. `rastrillo.Run` and `rastrillo.Serve`, in capture mode,
   finish building the handler, write the captured sets (escaped trees
   from the fork in 3, function names and `reflect.Type`s) to a file
   generate names, and exit without listening. No app changes.

   An app whose startup can't run this way (it needs a secret with no
   default, say) gets a one-line message from generate naming the
   failure. It stays uncompiled and still works. Rastrillo's scaffold
   and examples must start in capture mode, and the bake-off app too.
   A static reimplementation of every app's set-building code was
   rejected: it would drift from what apps actually do.
2. **Compile** each template the compiler supports. Unsupported
   constructs leave that template, and anything that calls it, on
   `html/template`. generate prints one line per such template, naming
   it and the construct.
3. **Write** one generated file per package that builds sets, in that
   package (for example `internal/todo/templates_gen.go`). Being in the
   same package gives the typed paths access to unexported view types,
   and needs no new import in `main.go`. The file registers its compiled
   functions by fingerprint from `init`.

An app's existing generated-code check (Rastrillo's own `make
generate-check`, and whatever the scaffold gives apps) covers the new
file, so CI fails when compiled templates are stale.

### 6. What the compiler supports

The compiler must support everything used by ui's partials and layouts
(`ui/partials`, `ui/layouts`), the scaffold's pages, the examples
(blog, notes, tickets), and the bake-off app. In practice that means
text, actions and pipelines, variables, `if`/`else if`/`with`/`range`
(including `else`, `break` and `continue`, and ranging over slices,
maps, ints and channels), `template` and `block`, the builtins
(`and`/`or` short-circuiting, `not`, `eq`/`ne`/`lt`/`le`/`gt`/`ge` with
`eq`'s multi-argument form, `len`, `index`, `slice`, `print*`, `call`,
`html`/`js`/`urlquery`), method calls on data, and functions returning
`(value, error)`.

Error behaviour matches `html/template`: a nil pointer dereferenced, a
missing method, a function's error, and `missingkey` handling produce
an error or output where it does, so callers' existing error paths
(ui.Pages' clean 500) keep working. Partial output before an error is
not a contract; the callers already buffer.

### 7. Phase 2: formatting

Once the compiled path matches `html/template` byte for byte (phase 1),
formatting is added:

- The aim is that each element the template already puts on its own
  line is indented by its true nesting depth, with tabs, across
  partials. A partial called at depth 4 is indented from depth 4. The
  static text is formatted at generate time, and only the starting
  depth is passed at runtime (one integer per call).
- **The safety rule (my call).** An app's CSS can make any element
  inline (TodoMVC's filter `<li>`s) or `display: contents` (its forms),
  so the formatter can't know which whitespace between tags is
  ignorable. It relies instead on one fact that holds under normal CSS
  whitespace handling: a run of whitespace renders the same as any
  other non-empty run. So the formatter:
  - only reshapes whitespace runs that already contain a newline,
    keeping the newline and changing the indentation after it;
  - never removes a whitespace run and never adds one where tags touch
    or text meets a tag. Markup the author wrote on one line stays on
    one line;
  - may add line breaks freely only inside `<head>` and between
    `<html>`, `<head>` and `<body>`;
  - leaves `<pre>`, `<textarea>`, `<script>`, `<style>` and everything
    inside them verbatim.

  The one case this doesn't cover is CSS `white-space: pre`,
  `pre-wrap` or `break-spaces` on some other element whose template
  content contains newlines. There the new indentation would show.
  That is a documented limitation. Rastrillo's own CSS uses it only in
  the design-system gallery (`internal/designsystem/gallery.css:119`,
  on `<code>` holding a value), which the formatter doesn't affect.
  Paul ranks rendering above tidiness, so where the rule and "perfect"
  nesting conflict, the rule wins.
- Runtime `template.HTML` values are written as they arrive (see
  non-goals).
- The builder's call: whether the formatter is a transform on the
  parse trees, which would let the `html/template` fallback produce the
  same formatting for static text, or something the compiled path
  alone does. Paul doesn't mind if development output differs.

When formatting lands, the equivalence tests change from byte equality
to comparing parsed documents. Both outputs are parsed with an HTML
parser, and the comparison ignores only whitespace the browser ignores.
`golang.org/x/net/html` is the obvious parser, but it isn't in
Rastrillo's `go.mod` yet, so the builder follows the repo's rules for
adding a dependency or uses what the repo already has. A browser
screenshot comparison of the gallery pages and the bake-off page
before and after backs this up. It catches mistakes in the rule's
implementation; it doesn't prove the rule.

### 8. Phases

1. **Compiled path, byte-identical.** The set type, capture,
   fingerprints, the escaping fork, the compiler on the typed and dynamic
   paths, generate integration, `ui.Pages` and `live` on the new type,
   and the bake-off page benchmark (success 2). The bake-off app moved
   over in its own repo. Rastrillo released. Then the Carloku rerun
   (success 1), when Paul says go.
2. **Formatting.**
3. **`dict` partials specialised per call site**, for scaffolded apps'
   pages, measured on the tickets list against the 2026-10-28 numbers.

If phase 1 misses 3x locally, the builder stops and reports where the
time goes (pprof) before starting phase 2.

## Testing

- **Equivalence corpus.** Every template set in this repo (ui's
  partials and layouts, the scaffold's pages, the examples, the
  `ui/testdata/pages` fixtures) plus a copy of the bake-off page is
  rendered both ways, with the data the existing tests already use and
  with generated variations (nil pointers, empty slices, HTML special
  characters in every string, `template.HTML`/`URL`/`JS`/`CSS`/
  `HTMLAttr` values in every context). Phase 1: byte equality. Phase 2:
  document equality as above.
- **Escaper fuzz tests** against the standard library, and the
  Go-version check on the fork (see 3).
- **Fallback tests.** A stale fingerprint, an unsupported construct, an
  unexpected data type, and an app override of a ui partial each render
  correctly through `html/template`, with the log line described in 2.
- **Concurrency.** The compiled path under `-race` with many goroutines
  on one set.
- **Benchmarks** for success 2, kept in the repo so a regression is
  visible.
- New CI checks follow the sharded-gate convention (a SUITE role and a
  bumped `@N` in the suite filename).

## Left to the builder

The package name and API details of the set type; the fingerprint
format; the capture mechanism within the constraints in 5; the
candidate cap; whether the dynamic path is kept or replaced by fallback;
the generated code's layout; the formatter's architecture within 7; and
the order of work within each phase.
