# 🤖 tmpl

`amadan.net/rastrillo/rastrillo/tmpl`

Templates that run as compiled Go when compiled code exists, and as
`html/template` when it does not. A `tmpl.Template` has the part of
`*html/template.Template`'s API that Rastrillo apps use, so moving an
app over is usually an import change: replace `html/template` with
`amadan.net/rastrillo/rastrillo/tmpl`. It does not have `AddParseTree`,
the `Tree` field, or package helpers such as `HTMLEscapeString`. Keep
`html/template` for code that needs them.

An app on [`ui.Pages`](/docs/reference/ui#pages) changes nothing: `Pages`
already builds its templates with `tmpl`.

```go
t := tmpl.Must(tmpl.New("layout").Funcs(funcs).ParseFS(fsys, "*.html"))
err := t.ExecuteTemplate(w, "layout", data)
```

`FuncMap` and the content types are `html/template`'s own types, so your
functions and your `tmpl.HTML` values need no change.

## The API

```go
func New(name string) *Template
func Must(t *Template, err error) *Template
func ParseFiles(filenames ...string) (*Template, error)
func ParseGlob(pattern string) (*Template, error)
func ParseFS(fsys fs.FS, patterns ...string) (*Template, error)

func (t *Template) Name() string
func (t *Template) New(name string) *Template
func (t *Template) Funcs(funcMap FuncMap) *Template
func (t *Template) Option(opt ...string) *Template
func (t *Template) Delims(left, right string) *Template
func (t *Template) Parse(text string) (*Template, error)
func (t *Template) ParseFiles(filenames ...string) (*Template, error)
func (t *Template) ParseGlob(pattern string) (*Template, error)
func (t *Template) ParseFS(fsys fs.FS, patterns ...string) (*Template, error)
func (t *Template) Lookup(name string) *Template
func (t *Template) Templates() []*Template
func (t *Template) DefinedTemplates() string
func (t *Template) Defined(name string) bool
func (t *Template) Clone() (*Template, error)
func (t *Template) Execute(w io.Writer, data any) error
func (t *Template) ExecuteTemplate(w io.Writer, name string, data any) error
func (t *Template) IsCompiled(name string) bool

type (
	FuncMap  = template.FuncMap
	HTML     = template.HTML
	HTMLAttr = template.HTMLAttr
	URL      = template.URL
	JS       = template.JS
	JSStr    = template.JSStr
	CSS      = template.CSS
	Srcset   = template.Srcset
)

const (
	ShadowEnv = "RASTRILLO_TEMPLATE_SHADOW"
	DevEnv    = "RASTRILLO_DEV"
	TraceEnv  = "RASTRILLO_TEMPLATE_TRACE"
)
```

Each method does what the one on `*html/template.Template` does, and
`Template` is backed by a real one, so parsing, functions and errors are
`html/template`'s. `Parse` and `Clone` fail once a template has run, as
they do there.

`Defined` reports whether the set has a template of that name, the same
answer as `Lookup(name) != nil`. A template made with `New` and never
parsed is in the set, and running it fails, as on `html/template`.

## When code is compiled

`rastrillo generate` compiles the templates of your app's sets to Go. The
code is registered under each template's fingerprint, a hash of:

- the template,
- every template it calls, and
- the Go types of the functions it uses.

The first time a set runs, each template looks up its fingerprint. A
match renders through the compiled code. No match renders through
`html/template`. Edit a template and its fingerprint changes, so old
compiled code never renders new HTML.

The compiled code goes with the package that built the set. For a set
that `ui.Pages` builds, that is your app.

## When html/template runs

A template runs on `html/template` when:

- no compiled code was generated for it,
- it changed since `rastrillo generate` last ran,
- `rastrillo generate` could not compile something in it, or
- its set uses `Delims`.

Run `rastrillo generate` after you change a template. Nothing breaks if
you forget, because the page renders as it always did. It is only slower.

Change a set after it has run and its compiled code turns off for the
whole set. That covers `Funcs`, `Option`, `Delims`, and `New` with a name
the set already has. Build the set completely before the first `Execute`.

## The stale warning

A built binary that finds templates changed since `rastrillo generate`
logs one warning for the set, the first time it runs:

```text
tmpl: templates changed since the last rastrillo generate; run rastrillo generate
```

The entry has `owner`, the package the set belongs to, and `templates`,
the names that changed. Run `rastrillo generate` and rebuild.

A template `rastrillo generate` could not compile does not warn. Edit it,
and it warns like any other.

An app that has never generated code logs nothing. `rastrillo dev`
builds your app without its compiled code, so it never warns. An app
started with `RASTRILLO_DEV=1` (`DevEnv`), as `rastrillo dev` starts
it, logs the message at debug level.

## Shadow mode

Set `RASTRILLO_TEMPLATE_SHADOW=1` (`ShadowEnv`) to render each compiled
template both ways. The page is `html/template`'s output, and `Execute`
fails if the compiled bytes differ. The error names the template, the
entry and the first byte that differs, with the text around it.

Turn it on in tests and CI. It renders every page twice, so leave it off
in production.

Shadow mode compares each entry with `html/template` running it first
on a fresh copy of the set, which is what compiled code matches. On a
set shared by several entries, `html/template` can write different
bytes for an entry that runs after another: a partial run on its own,
then called inside `<script>`, is escaped twice.

Each template runs twice, so data that changes while it renders, such as
a channel it reads or a method that changes its receiver, comes out
different the second time. Shadow mode reports that as a difference
too. Turn shadow mode off for that test.

## Tracing

Set `RASTRILLO_TEMPLATE_TRACE=1` (`TraceEnv`) to log every execution
and whether it ran compiled code:

```text
INFO tmpl: executed template=layout compiled=true
```

Use it in a test that can only see your running app, such as a smoke
test of the served pages. In a Go test, use `IsCompiled`. Leave tracing
off in production: it logs every page.

## IsCompiled

```go
if !t.IsCompiled("layout") {
	log.Println("layout runs on html/template")
}
```

`IsCompiled` reports whether the template called `name` renders through
compiled code. Use it in a test to catch a page that quietly fell back
after a change. You can ask before the first `Execute` without stopping
yourself from adding templates.

## Capture

`rastrillo generate` compiles the sets your app really builds, so it
runs your app to find them. It builds the app with the
`rastrillo_capture` tag and starts it with `RASTRILLO_TEMPLATE_CAPTURE`
(`CaptureEnv`) set to a file. While that is set, every set your app
creates is recorded. `rastrillo.Serve` writes them to the file with
`WriteCapture` and returns without listening.

The app runs in a scratch directory with only `PATH`, `HOME`, `TMPDIR`
and `LANG` from your environment. A database it opens through
`rastrillo.OpenDB` or `db.Open` is a new file in that directory,
whatever path is configured, so capture never opens a real one.

You never call these yourself. They are exported so that generate can
read the file.

```go
const CaptureEnv = "RASTRILLO_TEMPLATE_CAPTURE"

func WriteCapture(path string) error

type Capture struct {
	Version          int
	GoVersion        string
	GeneratorVersion int
	Sets             []CapturedSet
}

type CapturedSet struct {
	Name                 string
	Owner                string
	Templates            map[string]string
	Fingerprints         map[string]string
	Funcs                map[string]FuncSig
	Option               string
	MetaContentURLEscape bool
	Uncompilable         map[string]string
}

type FuncSig struct {
	Expr     string
	Imports  []string
	Refs     []TypeRef
	Nameable bool
}

type TypeRef struct {
	PkgPath, Name string
}
```

A `CapturedSet` holds each template's source as parsed, the
fingerprint the set gives it, and the Go type of each function in its
`FuncMap` as a `FuncSig`. `Owner` is the package the compiled code goes
in. `Uncompilable` names the templates generate must leave on
`html/template`, with the reason.

If your app cannot start this way, for example because it needs a
secret that has no default, generate names the problem and your
templates stay on `html/template`.
