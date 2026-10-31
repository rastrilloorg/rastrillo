// Package tmpl is html/template with compiled templates behind it. A
// Template has the part of html/template's API that Rastrillo apps use,
// and is backed by a real *html/template.Template, so parsing, functions
// and errors behave as they do there. Switching an app over is usually
// an import change. There is no AddParseTree, no Tree field, and none of
// the package helpers such as HTMLEscapeString.
//
// rastrillo generate compiles each template of the app's sets to Go and
// registers the code under the template's fingerprint: a hash of its
// parse tree, the trees of every template it calls, and the Go types of
// the functions it uses. The first time a set executes, each template
// looks up its fingerprint. A hit renders through the compiled code; a
// miss runs html/template. An edited template has a new fingerprint, so
// stale code never serves old HTML, however the set was built.
//
// Set RASTRILLO_TEMPLATE_SHADOW=1 to run both and fail when their bytes
// differ.
package tmpl

import (
	"bytes"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"sync"

	"amadan.net/rastrillo/rastrillo/tmpl/internal/tmplhook"
	"amadan.net/rastrillo/rastrillo/tmpl/tmplrt"
)

// The types html/template uses for functions and trusted content, so
// that an app's FuncMap and content values carry over unchanged.
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
	// ShadowEnv set to 1 renders each compiled template through
	// html/template as well, and makes Execute fail if the bytes differ.
	ShadowEnv = "RASTRILLO_TEMPLATE_SHADOW"
	// DevEnv is set to 1 by rastrillo dev. Stale compiled code is then
	// logged at debug, because a save is expected to outrun generate.
	DevEnv = "RASTRILLO_DEV"
	// TraceEnv set to 1 logs every execution and whether it ran
	// compiled code. It is for tests that see only a running app.
	TraceEnv = "RASTRILLO_TEMPLATE_TRACE"
)

// traceMessage is TraceEnv's log line, with the attributes template
// and compiled.
const traceMessage = "tmpl: executed"

// tracing is TraceEnv, read once: Execute must not take the
// environment's lock on every request.
var tracing = os.Getenv(TraceEnv) == "1"

// trace logs that name ran, if tracing is on.
func trace(name string, compiled bool) {
	if tracing {
		slog.Info(traceMessage, "template", name, "compiled", compiled)
	}
}

// Template is a set of templates, like html/template's.
type Template struct {
	std *template.Template
	set *set
}

// New allocates a new set with one template of the given name. The set
// belongs to the calling package, or to the first caller outside ui,
// live and view if one of those calls New on an app's behalf.
func New(name string) *Template {
	std := template.New(name)
	set := newSet(std, callerPackage())
	captured.add(set)
	return set.wrap(std)
}

// Must panics if err is non-nil, as html/template's Must does.
func Must(template *Template, err error) *Template {
	if err != nil {
		panic(err)
	}
	return template
}

// ParseFiles is html/template's ParseFiles.
func ParseFiles(filenames ...string) (*Template, error) {
	return fromStd(template.ParseFiles(filenames...))
}

// ParseGlob is html/template's ParseGlob.
func ParseGlob(pattern string) (*Template, error) {
	return fromStd(template.ParseGlob(pattern))
}

// ParseFS is html/template's ParseFS.
func ParseFS(fsys fs.FS, patterns ...string) (*Template, error) {
	return fromStd(template.ParseFS(fsys, patterns...))
}

func fromStd(std *template.Template, err error) (*Template, error) {
	if err != nil {
		return nil, err
	}
	set := newSet(std, callerPackage())
	captured.add(set)
	return set.wrap(std), nil
}

// Name returns the name of the template.
func (template *Template) Name() string { return template.std.Name() }

// New allocates a new template in template's set, as html/template's New does.
func (template *Template) New(name string) *Template {
	replaces := template.std.Lookup(name) != nil
	set := template.set // template itself may be the template replaced
	std := template.std.New(name)
	set.adopt(std)
	// Compiled code bound before first use must not outlive the template
	// it renders.
	set.changed(nil, replaces)
	return set.wrap(std)
}

// Funcs adds funcMap to the set's functions, as html/template's Funcs
// does.
func (template *Template) Funcs(funcMap FuncMap) *Template {
	template.std.Funcs(funcMap)
	template.set.changed(func(set *set) { maps.Copy(set.funcs, funcMap) }, true)
	return template
}

// Option sets options for the set, as html/template's Option does.
func (template *Template) Option(opt ...string) *Template {
	template.std.Option(opt...)
	template.set.changed(func(set *set) {
		for _, option := range opt {
			set.missingKey = missingKeyOption(option, set.missingKey)
		}
	}, true)
	return template
}

// Delims sets the action delimiters, as html/template's Delims does. A
// set that uses it always runs on html/template, from then on, even if
// it has already executed.
func (template *Template) Delims(left, right string) *Template {
	template.std.Delims(left, right)
	template.set.changed(func(set *set) { set.delims = true }, true)
	return template
}

// errParseAfterExecute is html/template's own error, for a set whose
// compiled code ran in place of its first Execute.
var errParseAfterExecute = fmt.Errorf(
	"html/template: cannot Parse after Execute",
)

// Parse parses text into template, as html/template's Parse does.
func (template *Template) Parse(text string) (*Template, error) {
	return template.parse(func() error {
		_, err := template.std.Parse(text)
		return err
	})
}

// ParseFiles parses the named files into template's set, as html/template's
// ParseFiles does.
func (template *Template) ParseFiles(filenames ...string) (*Template, error) {
	return template.parse(func() error {
		_, err := template.std.ParseFiles(filenames...)
		return err
	})
}

// ParseGlob parses the files matching pattern into template's set, as
// html/template's ParseGlob does.
func (template *Template) ParseGlob(pattern string) (*Template, error) {
	return template.parse(func() error {
		_, err := template.std.ParseGlob(pattern)
		return err
	})
}

// ParseFS parses the files in fsys matching patterns into template's set, as
// html/template's ParseFS does.
func (template *Template) ParseFS(
	fsys fs.FS,
	patterns ...string,
) (*Template, error) {
	return template.parse(func() error {
		_, err := template.std.ParseFS(fsys, patterns...)
		return err
	})
}

func (template *Template) parse(parse func() error) (*Template, error) {
	if template.set.compiledRan.Load() {
		return nil, errParseAfterExecute
	}
	err := parse()
	// ParseFiles replaces a template named like a file with New.
	template.set.adopt(template.std)
	// Even a failed parse of several files may have added some.
	template.set.changed(nil, false)
	if err != nil {
		return nil, err
	}
	return template, nil
}

// Lookup returns the template with the given name in template's set, or nil.
func (template *Template) Lookup(name string) *Template {
	return template.set.wrap(template.std.Lookup(name))
}

// Templates returns the templates in template's set, template included.
func (template *Template) Templates() []*Template {
	std := template.std.Templates()
	out := make([]*Template, len(std))
	for index, standard := range std {
		out[index] = template.set.wrap(standard)
	}
	return out
}

// DefinedTemplates lists the defined templates, as html/template's
// DefinedTemplates does.
func (template *Template) DefinedTemplates() string {
	return template.std.DefinedTemplates()
}

// Defined reports whether template's set has a template of that name, which is
// whether Lookup finds one. A template made with New and never parsed is
// in the set, and running it fails as it does on html/template.
// Defined exists so code that takes either kind of template, such as
// live, can ask without Lookup's differing return types.
func (template *Template) Defined(name string) bool {
	return template.std.Lookup(name) != nil
}

// Clone returns a copy of the set, as html/template's Clone does. The
// copy belongs to the same package as the original.
func (template *Template) Clone() (*Template, error) {
	if template.set.compiledRan.Load() {
		return nil, fmt.Errorf(
			"html/template: cannot Clone %q after it has executed",
			template.Name(),
		)
	}
	std, err := template.std.Clone()
	if err != nil {
		return nil, err
	}
	return template.set.clone(std).wrap(std), nil
}

// IsCompiled reports whether the template called name runs compiled
// code. Asking prepares the set, as its first Execute would.
func (template *Template) IsCompiled(name string) bool {
	return template.set.peek().compiled[name] != nil
}

// Execute applies template to data and writes the output to writer, as
// html/template's Execute does.
func (template *Template) Execute(writer io.Writer, data any) error {
	name := template.std.Name()
	exec, shadow := template.set.compiledExec(name)
	tmplhook.Executed(name, exec != nil)
	trace(name, exec != nil)
	switch {
	case exec == nil:
		return template.std.Execute(writer, data)
	case shadow != nil:
		return template.shadow(writer, name, exec, data, shadow)
	}
	return render(writer, exec, data)
}

// ExecuteTemplate applies the template called name in template's set to data
// and writes the output to writer, as html/template's ExecuteTemplate does.
func (template *Template) ExecuteTemplate(
	writer io.Writer,
	name string,
	data any,
) error {
	exec, shadow := template.set.compiledExec(name)
	tmplhook.Executed(name, exec != nil)
	trace(name, exec != nil)
	switch {
	case exec == nil:
		return template.std.ExecuteTemplate(writer, name, data)
	case shadow != nil:
		return template.shadow(writer, name, exec, data, shadow)
	}
	return render(writer, exec, data)
}

// render runs compiled code straight into writer when it is a Buffer, which is
// what Exec writes to, and through a pooled one otherwise.
func render(writer io.Writer, exec tmplrt.Exec, data any) error {
	if buffer, ok := writer.(*bytes.Buffer); ok {
		return exec(buffer, data)
	}
	buffer := buffers.Get().(*bytes.Buffer)
	defer putBuffer(buffer)
	if err := exec(buffer, data); err != nil {
		return err
	}
	_, err := writer.Write(buffer.Bytes())
	return err
}

var buffers = sync.Pool{New: func() any { return new(bytes.Buffer) }}

// putBuffer keeps a buffer for reuse unless one page made it unusually
// large, which would pin that memory for every later page.
func putBuffer(buffer *bytes.Buffer) {
	if buffer.Cap() > 1<<20 {
		return
	}
	buffer.Reset()
	buffers.Put(buffer)
}

// owner is the import path of the package the set belongs to: the one
// rastrillo generate writes its compiled code into.
func (template *Template) owner() string { return template.set.owner }

// fingerprints maps each template in the set to its fingerprint, as
// taken when the set was prepared.
func (template *Template) fingerprints() map[string]string {
	return maps.Clone(template.set.peek().fingerprints)
}
