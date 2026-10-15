package ui

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"slices"
	"sync"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/locales"
)

// Appendix C.2 of the multi-currency money spec, verbatim: op.pages.*.
// op.pages.wrap and op.pages.base are the per-page and base parse wraps.
const (
	noLocalizerMsg       = "ui: no localizer"
	nilLocalizerMsg      = `%s called with a nil $L (the data reached this template without "L")`
	pageDataMsg          = "in the page data for %q"
	legacyCallMsg        = "ui: legacy locale call; run rastrillo migrate-templates"
	legacyFuncMsg        = "%s without $L"
	compatOnMsg          = "ui: COMPATIBILITY MODE: legacy locale calls and missing localizers render in the default locale %q instead of failing; finish rastrillo migrate-templates and turn it off"
	compatCallMsg        = "ui: COMPATIBILITY MODE: a legacy or localizer-less %s call rendered in the default locale"
	reservedFuncMsg      = "ui: app function %q would replace a framework function"
	offeredTwiceMsg      = "ui: %q offered twice"
	defaultNotOfferedMsg = "ui: default %q is not offered"
	noFormatMsg          = "ui: %q has no formatting locale; pass locales.CheckConfig's result"
	pageParseMsg         = "ui: %s: %w"
	baseParseMsg         = "ui: base: %w"
	noPageMsg            = "ui: no page %q"
)

// ErrNoLocalizer is a locale call, or page data, without a localizer.
var ErrNoLocalizer = errors.New(noLocalizerMsg)

// ErrLegacyCall is a locale call without $L outside compat mode.
var ErrLegacyCall = errors.New(legacyCallMsg)

// PagesConfig describes one app's pages and locales.
type PagesConfig struct {
	App      fs.FS               // the app's templates
	Layout   string              // the layout file in App, parsed into the base
	Shared   []string            // other shared files in App, parsed into the base
	Pages    map[string]PageFile // page name -> the page's own file
	Entry    string              // the template Execute runs; "layout" when empty
	Locale   locales.Config      // locales.CheckConfig's result
	Catalog  rastrillo.Catalog   // Options.BaseCatalog
	LocaleFS fs.FS               // Options.LocaleFS: locales/<code>.toml
	Funcs    template.FuncMap    // the app's request-independent functions (icon, iconAssets, asset)
	// Compat renders legacy-form calls and missing localizers with the
	// default identity's localizer instead of failing. For apps
	// mid-migration only; it is logged at NewPages and on each
	// function's first such call, because a page rendering in the
	// default language is otherwise indistinguishable from a correct
	// one in a test that only checks the default.
	Compat bool
	Logger *slog.Logger // nil means slog.Default()
}

// PageFile is one page's own template file. FS nil means App: the
// examples read pages from more than one tree (gen/templates and an
// ejection root), so each page names its own.
type PageFile struct {
	FS   fs.FS
	Path string
}

// Pages is one app's page templates and its localizers: template sets
// that are not per language and not per request.
//
// NewPages parses the base (ui's partials, the app's layout and shared
// files) once, then makes each page one clone of that base with the
// page's own definitions added ("content", "nav" and so on). Pages
// repeat those names, so they cannot share one namespace. The base is
// dropped once the clones exist, and nothing is cloned per language or
// per request.
//
// Locale-dependent output is resolved per execution from the localizer
// the data carries: {{T $L "key"}}, {{money $L .Price .PriceCurrency}}.
// html/template needs a function's name when it parses and evaluates its
// arguments when it executes, so the implementation behind a name could
// be swapped later with Funcs. It never is: rebinding a function map
// that other requests are executing puts one request's language or user
// into another's output, and Go synchronises those reads and writes, so
// -race has no data race to report. The map is built once in NewPages
// and the locale arrives as an argument.
//
// Strict by default: a localizer-form call with a nil $L, and any
// legacy-form call, is an execution error, so a page fails rather than
// render in the wrong language. PagesConfig.Compat relaxes that for
// apps mid-migration.
type Pages struct {
	pages      map[string]*template.Template
	localizers map[string]*Localizer // fixed at NewPages
	def        *Localizer
	locales    *rastrillo.Locales
	entry      string
	compat     bool
	logger     *slog.Logger
	warned     sync.Map
}

// NewPages builds one localizer per offered identity, parses the base
// once, and makes one clone per page.
func NewPages(cfg PagesConfig) (*Pages, error) {
	p := &Pages{pages: map[string]*template.Template{}, localizers: map[string]*Localizer{},
		entry: cfg.Entry, compat: cfg.Compat, logger: cfg.Logger}
	if p.entry == "" {
		p.entry = "layout"
	}
	if p.logger == nil {
		p.logger = slog.Default()
	}
	ids := cfg.Locale.Offered
	if len(ids) == 0 {
		ids = []string{cfg.Locale.Default}
	}
	for i, id := range ids {
		if slices.Contains(ids[:i], id) {
			return nil, fmt.Errorf(offeredTwiceMsg, id)
		}
	}
	if !slices.Contains(ids, cfg.Locale.Default) {
		return nil, fmt.Errorf(defaultNotOfferedMsg, cfg.Locale.Default)
	}
	for _, id := range ids {
		if cfg.Locale.Formats[id] == "" {
			return nil, fmt.Errorf(noFormatMsg, id)
		}
	}
	// The framework's English under the app's base catalog, the merge
	// buildHandler does: without it a framework key in an offered
	// locale the framework does not translate (de) renders as the raw
	// key here while Serve's own translator renders it in English.
	base := rastrillo.BaseCatalog()
	for k, v := range cfg.Catalog {
		base[k] = v
	}
	cats, err := rastrillo.NewLocales(ids, cfg.Locale.Default, base, cfg.LocaleFS)
	if err != nil {
		return nil, err
	}
	p.locales = cats
	for _, id := range ids {
		p.localizers[id] = NewLocalizer(id, cfg.Locale.Formats[id], func(k string, a ...any) string { return cats.Tf(id, k, a...) })
	}
	p.def = p.localizers[cfg.Locale.Default]

	funcs := p.funcs()
	for name := range cfg.Funcs {
		if slices.Contains(LocaleFuncs, name) || name == "localizer" || name == "withL" {
			return nil, fmt.Errorf(reservedFuncMsg, name)
		}
		funcs[name] = cfg.Funcs[name]
	}
	if p.compat {
		p.logger.Warn(fmt.Sprintf(compatOnMsg, cfg.Locale.Default))
	}

	// ui's partials parse first, so an app's {{define}} of a partial
	// wins, as it does on an app's own tree.
	tmpl, err := template.New(p.entry).Funcs(funcs).ParseFS(Templates(), "*.html")
	var shared []string
	if cfg.Layout != "" {
		shared = append(shared, cfg.Layout)
	}
	if shared = append(shared, cfg.Shared...); err == nil && len(shared) > 0 {
		tmpl, err = tmpl.ParseFS(cfg.App, shared...)
	}
	if err != nil {
		return nil, fmt.Errorf(baseParseMsg, err)
	}
	for name, file := range cfg.Pages {
		fsys := file.FS
		if fsys == nil {
			fsys = cfg.App
		}
		c, err := tmpl.Clone() // at boot, once per page; never per language or request
		if err == nil {
			c, err = c.ParseFS(fsys, file.Path)
		}
		if err != nil {
			return nil, fmt.Errorf(pageParseMsg, name, err)
		}
		p.pages[name] = c
	}
	return p, nil
}

// Localizer is the localizer for an offered identity, and the default
// identity's for anything else, so a cookie or Accept-Language naming a
// locale the app does not offer renders in the default rather than
// failing. It never allocates: it runs on every request.
func (p *Pages) Localizer(identity string) *Localizer {
	if l, ok := p.localizers[identity]; ok {
		return l
	}
	return p.def
}

// LocalizerFor is the request's localizer wherever the request is:
// LocalizerFrom(r) when the middleware ran; else the locale the locale
// middleware put on the context (rastrillo.LocaleFrom); else the
// request resolved directly, by the same rules (URL prefix, cookie,
// Accept-Language, default) through the catalogs' Negotiate. Error
// pages use it: Serve's panic recovery wraps the locale middleware and
// calls Options.ErrorPage with the original request, and handlers call
// ErrorPage directly, so neither can rely on the middleware's context.
func (p *Pages) LocalizerFor(r *http.Request) *Localizer {
	if l := LocalizerFrom(r); l != nil {
		return l
	}
	if code := rastrillo.LocaleFrom(r); code != "" {
		return p.Localizer(code)
	}
	return p.Localizer(p.locales.Negotiate(r))
}

// Middleware puts p.LocalizerFor(r) on the request, for LocalizerFrom
// and ForRequest.
func (p *Pages) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, WithLocalizer(r, p.LocalizerFor(r)))
	})
}

// pageBuffers holds Execute's buffers between requests.
var pageBuffers = sync.Pool{New: func() any { return new(bytes.Buffer) }}

// maxPooledBuffer keeps one unusually large page from pinning its
// buffer in the pool for the life of the process.
const maxPooledBuffer = 1 << 20

// Execute renders page with data, which must carry a non-nil *Localizer
// as L. It renders into a buffer and writes nothing unless the whole
// page succeeded: html/template writes as it executes, so a strict
// failure halfway down would otherwise leave half a page on the wire
// under a 200, with no way for the caller to send its 500.
func (p *Pages) Execute(w io.Writer, page string, data any) error {
	if localizer(data) == nil {
		return fmt.Errorf("%w "+pageDataMsg, ErrNoLocalizer, page)
	}
	t, ok := p.pages[page]
	if !ok {
		return fmt.Errorf(noPageMsg, page)
	}
	b := pageBuffers.Get().(*bytes.Buffer)
	defer func() {
		if b.Cap() <= maxPooledBuffer {
			b.Reset()
			pageBuffers.Put(b)
		}
	}()
	b.Reset()
	if err := t.ExecuteTemplate(b, p.entry, data); err != nil {
		return err
	}
	_, err := b.WriteTo(w)
	return err
}

// resolve applies the strict or compat policy to one locale call and
// returns the arguments to hand ui.Funcs' implementation, which then
// always sees a non-nil localizer first.
func (p *Pages) resolve(name string, args []any) ([]any, error) {
	l, localizerForm, rest := pick(args)
	switch {
	case l != nil:
		return args, nil
	case name == "money" && !localizerForm:
		return nil, errMoneyFirstArg // money has no legacy form, compat or not
	case !p.compat && localizerForm:
		return nil, fmt.Errorf("%w: "+nilLocalizerMsg, ErrNoLocalizer, name)
	case !p.compat:
		return nil, fmt.Errorf("%w: "+legacyFuncMsg, ErrLegacyCall, name)
	}
	if _, seen := p.warned.LoadOrStore(name, true); !seen {
		p.logger.Warn(fmt.Sprintf(compatCallMsg, name))
	}
	return append([]any{p.def}, rest...), nil
}

// funcs is the function map, built once: ui.Funcs with each locale
// function behind the strict or compat dispatcher. The implementations
// underneath are ui.Funcs' own, so a call through Pages formats and
// checks its arguments exactly as one through ui.Funcs with the same
// localizer.
func (p *Pages) funcs() template.FuncMap {
	m := Funcs()
	for _, name := range LocaleFuncs {
		impl := m[name].(func(...any) (any, error))
		m[name] = func(args ...any) (any, error) {
			args, err := p.resolve(name, args)
			if err != nil {
				return nil, err
			}
			return impl(args...)
		}
	}
	return m
}
