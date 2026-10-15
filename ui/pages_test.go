package ui

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/locales"
	_ "amadan.net/rastrillo/rastrillo/locales/de"
)

// pagesApp is the fixture app in testdata/pages: its templates were
// written in today's syntax and migrated with rastrillo
// migrate-templates, except legacy.html and nil.html (unmigrated on
// purpose), nilnew.html (migrated by hand, to compare with nil.html)
// and arity.html (wrong on purpose).
func pagesApp(t testing.TB) fstest.MapFS {
	t.Helper()
	app := fstest.MapFS{}
	err := fs.WalkDir(os.DirFS("testdata/pages"), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile("testdata/pages/" + path)
		app[path] = &fstest.MapFile{Data: b}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return app
}

func pagesConfig(t testing.TB, def string, compat bool, logs *syncBuffer) PagesConfig {
	t.Helper()
	cfg, err := locales.CheckConfig(def, []string{"en", "es"})
	if err != nil {
		t.Fatal(err)
	}
	app := pagesApp(t)
	pc := PagesConfig{
		App: app, Layout: "layout.html", Shared: []string{"who.html"},
		Pages: map[string]PageFile{"corpus": {Path: "corpus.html"}, "anon": {Path: "anon.html"},
			"legacy": {Path: "legacy.html"}, "nil": {Path: "nil.html"}, "nilnew": {Path: "nilnew.html"},
			"arity": {Path: "arity.html"}, "errors": {Path: "errors.html"}},
		Locale:   cfg,
		LocaleFS: app,
		Funcs: template.FuncMap{
			"asset": func(p string) string { return "/static/" + p },
			"icon":  func(n string) template.HTML { return template.HTML("<i>" + n + "</i>") },
		},
		Compat: compat,
	}
	if logs != nil {
		pc.Logger = slog.New(slog.NewTextHandler(logs, nil))
	}
	return pc
}

// syncBuffer is a log sink the 200-goroutine test can share.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

type pagePerson struct {
	Localized
	Name string
}

type pageAnon struct{ Name string }

type pageData struct {
	Localized
	Evil, URL, User, Cur string
	N                    int
	Price                int64
	Person               pagePerson
	Anon                 pageAnon
	Nil                  any
}

func pageIn(p *Pages, locale, user string) pageData { return pageFor(p.Localizer(locale), user) }

func pageFor(l *Localizer, user string) pageData {
	return pageData{Localized: Localized{L: l}, Evil: `<script>"x"</script>`, URL: "javascript:alert(1)", User: user,
		Cur: "EUR", N: 1234567, Price: 128450, Person: pagePerson{Localized{L: l}, "Ana"}, Anon: pageAnon{"Bo"}}
}

func runPage(p *Pages, name string, data any) (string, error) {
	var b strings.Builder
	err := p.Execute(&b, name, data)
	return b.String(), err
}

func strictPages(t testing.TB) *Pages {
	t.Helper()
	p, err := NewPages(pagesConfig(t, "en", false, nil))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// Migrated fixtures and real partials execute with production
// signatures; escaping holds in every context; numbers and money are
// localized; the localizer reaches a partial through field-daterange's
// withL (the map path) and through a struct that embeds Localized (the
// struct path).
func TestPagesCorpusExecutes(t *testing.T) {
	p := strictPages(t)
	for _, c := range []struct {
		lang  string
		wants []string
	}{
		{"es", []string{
			"<title>Tienda</title>",
			`<p id="num">1.234.567</p>`,
			"<p id=\"money\">1284,50\u00a0€</p>",
			`<p id="who">Hola Ana</p>`,
			`data-rst-date-set="` + rastrillo.BaseCatalogs()["es"]["rastrillo.ui.date_set"] + `"`,
		}},
		{"en", []string{
			"<title>Shop</title>",
			`<p id="num">1,234,567</p>`,
			`<p id="money">€1,284.50</p>`,
			`<p id="who">Hello Ana</p>`,
			`data-rst-date-set="` + rastrillo.BaseCatalogs()["en"]["rastrillo.ui.date_set"] + `"`,
		}},
	} {
		out, err := runPage(p, "corpus", pageIn(p, c.lang, "u"))
		if err != nil {
			t.Fatalf("%s: %v", c.lang, err)
		}
		escaping := []string{
			`<p id="html">&lt;script&gt;&#34;x&#34;&lt;/script&gt;</p>`, // HTML text
			`title="&lt;script&gt;&#34;x&#34;&lt;/script&gt;"`,          // attribute
			`href="#ZgotmplZ"`, // URL: javascript: refused
			"var v = \"\\u003cscript\\u003e\\\"x\\\"\\u003c/script\\u003e\";", // JS
			`<bdi>&lt;script&gt;&#34;x&#34;&lt;/script&gt;</bdi>`,             // Tbdi: user text escaped and isolated
		}
		for _, w := range append(c.wants, escaping...) {
			if !strings.Contains(out, w) {
				t.Errorf("%s: output lacks %s\n%s", c.lang, w, out)
			}
		}
		if strings.Contains(out, `<script>"x"`) {
			t.Errorf("%s: unescaped user text", c.lang)
		}
	}
}

// Negative fixtures: each must fail, in strict (production) mode.
func TestPagesStrictFailures(t *testing.T) {
	p := strictPages(t)
	if _, err := runPage(p, "anon", pageIn(p, "es", "u")); !errors.Is(err, ErrNoLocalizer) {
		t.Errorf("a partial reached with a struct lacking L: %v, want ErrNoLocalizer", err)
	}
	if _, err := runPage(p, "legacy", pageIn(p, "es", "u")); !errors.Is(err, ErrLegacyCall) {
		t.Errorf("an unmigrated call: %v, want ErrLegacyCall", err)
	}
	if _, err := runPage(p, "arity", pageIn(p, "es", "u")); err == nil || !strings.Contains(err.Error(), "want $L, an amount and a currency") {
		t.Errorf("money with a missing argument: %v", err)
	}
	if _, err := runPage(p, "corpus", pageData{}); !errors.Is(err, ErrNoLocalizer) {
		t.Errorf("page data without L: %v", err)
	}
	if _, err := runPage(p, "corpus", map[string]any{"L": 3}); !errors.Is(err, ErrNoLocalizer) {
		t.Errorf("an L that is not a localizer: %v", err)
	}
	if _, err := runPage(p, "corpus", map[string]any{"L": "es"}); !errors.Is(err, ErrNoLocalizer) {
		t.Errorf("an L that is a string: %v", err)
	}
	// FieldByName panics walking through a nil embedded pointer; the
	// data is merely missing its localizer, so it must be an error.
	type viaPointer struct{ *Localized }
	if _, err := runPage(p, "corpus", viaPointer{}); !errors.Is(err, ErrNoLocalizer) {
		t.Errorf("data embedding a nil *Localized: %v, want ErrNoLocalizer", err)
	}
	if _, err := runPage(p, "nope", pageIn(p, "es", "u")); err == nil || !strings.Contains(err.Error(), `ui: no page "nope"`) {
		t.Errorf("an unknown page: %v", err)
	}
}

// Compat is opt-in, logged, and uses the app's own default-locale
// translator: a Spanish-default app's legacy call renders its Spanish
// app key, here one only the Spanish catalog has, not the framework's
// English or the raw key.
func TestPagesCompatUsesTheAppsDefault(t *testing.T) {
	var logs syncBuffer
	p, err := NewPages(pagesConfig(t, "es", true, &logs))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), "COMPATIBILITY MODE") {
		t.Errorf("NewPages did not log compat mode:\n%s", logs.String())
	}
	out, err := runPage(p, "legacy", pageIn(p, "en", "u"))
	if err != nil || !strings.Contains(out, `<p id="legacy">Hola</p><p id="only">Solo en español</p>`) {
		t.Errorf("compat legacy call: %v\n%s", err, out)
	}
	out, err = runPage(p, "anon", pageIn(p, "en", "u"))
	if err != nil || !strings.Contains(out, `<p id="who">Hola Bo</p>`) {
		t.Errorf("compat missing localizer: %v\n%s", err, out)
	}
	joined := logs.String()
	if n := strings.Count(joined, "a legacy or localizer-less T call"); n != 1 {
		t.Errorf("T's compat call logged %d times, want once:\n%s", n, joined)
	}
	if !strings.Contains(joined, `default locale \"es\"`) {
		t.Errorf("the compat notice does not name the default:\n%s", joined)
	}
}

// A nil value is not mistaken for a missing localizer. A legacy
// {{number .Nil}} in compat mode, and the migrated {{number $L .Nil}}
// in strict mode, both render what ui.Funcs rendered before the
// localizer existed (wantLegacyEN's first two fields).
func TestPagesNilValues(t *testing.T) {
	want := "NIL" + wantLegacyEN[:len("[][]")] + "NIL"
	extract := func(s string) string {
		i, j := strings.Index(s, "NIL["), strings.LastIndex(s, "]NIL")
		if i < 0 || j < 0 {
			return s
		}
		return s[i : j+4]
	}
	compat, err := NewPages(pagesConfig(t, "en", true, &syncBuffer{}))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		p    *Pages
		name string
	}{{compat, "nil"}, {strictPages(t), "nilnew"}} {
		out, err := runPage(c.p, c.name, pageIn(c.p, "es", "u"))
		if err != nil || extract(out) != want {
			t.Errorf("%s: %q, %v; want %q", c.name, out, err, want)
		}
	}
}

// Output isolation, asserted per execution, alongside -race. A clean
// -race run alone would not prove it: shared mutable binding mixes
// outputs without a data race.
func TestPagesTwoLanguagesIsolated(t *testing.T) {
	p := strictPages(t)
	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			lang, other := "en", "es"
			if i%2 == 1 {
				lang, other = other, lang
			}
			user := fmt.Sprintf("user-%d", i)
			out, err := runPage(p, "corpus", pageIn(p, lang, user))
			if err != nil {
				t.Error(err)
				return
			}
			title := map[string]string{"en": "<title>Shop</title>", "es": "<title>Tienda</title>"}
			if !strings.Contains(out, title[lang]) || strings.Contains(out, title[other]) ||
				!strings.Contains(out, `<p id="user">`+user+`</p>`) || strings.Count(out, "user-") != 1 {
				t.Errorf("%s/%s: output not isolated:\n%s", lang, user, out)
			}
		}(i)
	}
	wg.Wait()
}

func TestPagesLocalizerAllocatesNothing(t *testing.T) {
	p := strictPages(t)
	if got := testing.AllocsPerRun(1000, func() { p.Localizer("xx-junk") }); got != 0 {
		t.Errorf("Localizer(junk) allocates %v", got)
	}
}

func TestPagesNewRefusals(t *testing.T) {
	for name, c := range map[string]struct {
		mutate func(*PagesConfig)
		want   string
	}{
		"app T": {func(c *PagesConfig) { c.Funcs = template.FuncMap{"T": func(string) string { return "" }} },
			`ui: app function "T" would replace a framework function`},
		"app localizer": {func(c *PagesConfig) { c.Funcs = template.FuncMap{"localizer": func() string { return "" }} },
			`ui: app function "localizer" would replace a framework function`},
		"app withL": {func(c *PagesConfig) { c.Funcs = template.FuncMap{"withL": func() string { return "" }} },
			`ui: app function "withL" would replace a framework function`},
		"duplicate": {func(c *PagesConfig) { c.Locale.Offered = []string{"en", "es", "en"} },
			`ui: "en" offered twice`},
		"default not offered": {func(c *PagesConfig) { c.Locale.Default = "de" },
			`ui: default "de" is not offered`},
		"syntax error in a page": {func(c *PagesConfig) {
			c.App.(fstest.MapFS)["corpus.html"] = &fstest.MapFile{Data: []byte(`{{define "content"}}{{if}}{{end}}`)}
		}, "ui: corpus: "},
		"syntax error in the base": {func(c *PagesConfig) {
			c.App.(fstest.MapFS)["who.html"] = &fstest.MapFile{Data: []byte(`{{define "who"}}{{if}}{{end}}`)}
		}, "ui: base: "},
		"no format": {func(c *PagesConfig) { delete(c.Locale.Formats, "es") },
			`ui: "es" has no formatting locale; pass locales.CheckConfig's result`},
		"undecodable catalog": {func(c *PagesConfig) {
			c.LocaleFS = fstest.MapFS{"locales/es.toml": {Data: []byte("= = =")}}
		}, "locales/es.toml"},
		"unreadable catalog": {func(c *PagesConfig) {
			c.LocaleFS = fstest.MapFS{"locales/es.toml/x": {Data: []byte("")}}
		}, "locales/es.toml"},
	} {
		cfg := pagesConfig(t, "en", false, nil)
		// The refusals mutate the config's maps; give each its own.
		cfg.Locale.Formats = map[string]string{"en": "en", "es": "es"}
		c.mutate(&cfg)
		if _, err := NewPages(cfg); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: NewPages gave %v, want %q", name, err, c.want)
		}
	}
}

// Pages repeat definition names ("content"); each page clone keeps its
// own.
func TestPagesKeepTheirOwnDefinitions(t *testing.T) {
	p := strictPages(t)
	a, err := runPage(p, "corpus", pageIn(p, "en", "u"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(a, `id="num"`) || strings.Contains(a, `id="legacy"`) {
		t.Errorf("corpus rendered another page's content:\n%s", a)
	}
	if _, err := runPage(p, "legacy", pageIn(p, "en", "u")); !errors.Is(err, ErrLegacyCall) {
		t.Errorf("legacy rendered corpus's content instead of its own: %v", err)
	}
}

// The framework's English sits under every Pages catalog, as it does
// under Serve's own: a locale the framework does not translate still
// renders its strings in English, never as raw keys.
func TestPagesFrameworkEnglishUnderneath(t *testing.T) {
	cfg, err := locales.CheckConfig("en", []string{"en", "de"})
	if err != nil {
		t.Fatal(err)
	}
	p, err := NewPages(PagesConfig{App: fstest.MapFS{
		"layout.html": {Data: []byte(`{{define "layout"}}{{$L := localizer $}}{{T $L "rastrillo.ui.cancel"}}{{end}}`)},
		"home.html":   {Data: []byte(``)},
	}, Layout: "layout.html", Pages: map[string]PageFile{"home": {Path: "home.html"}}, Locale: cfg})
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	if err := p.Execute(&b, "home", map[string]any{"L": p.Localizer("de")}); err != nil || b.String() != rastrillo.BaseCatalog()["rastrillo.ui.cancel"] {
		t.Errorf("de: %q, %v; want the framework's English", b.String(), err)
	}
}

// Review Focus 4: html/template writes as it executes. A strict failure
// halfway down a page must not leave half a page on the wire under a
// 200; the caller gets the error and writes its own 500.
func TestPagesExecuteWritesNothingOnError(t *testing.T) {
	p := strictPages(t)
	var w strings.Builder
	err := p.Execute(&w, "anon", pageIn(p, "en", "u")) // "who" reached with a struct lacking L
	if !errors.Is(err, ErrNoLocalizer) || w.Len() != 0 {
		t.Errorf("err %v, wrote %d bytes; want ErrNoLocalizer and nothing written", err, w.Len())
	}
}

// Review Focus 2: a cookie or Accept-Language names an identity the app
// does not offer, linked or not. The page renders as the default
// identity, whose localizer carries the default's catalog and format.
func TestPagesLocalizerForUnofferedIdentity(t *testing.T) {
	p := strictPages(t) // offers en and es, default en
	for _, id := range []string{"de", "de-CH", "es-MX", "xx", "", "EN"} {
		if l := p.Localizer(id); l.Identity() != "en" || l.Format() != "en" {
			t.Errorf("Localizer(%q) = %s/%s, want the default en/en", id, l.Identity(), l.Format())
		}
	}
	h := p.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if l := LocalizerFrom(r); l == nil || l.Identity() != "en" {
			t.Errorf("Middleware put %v on a request with no locale", l)
		}
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
}
