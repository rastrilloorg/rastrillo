package rastrillo

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"amadan.net/rastrillo/rastrillo/internal/catalog"
)

// Catalog is one locale's flat key → string table (design doc §10).
type Catalog map[string]string

// Locales is an app's declared locale set, its own catalogs, and the
// framework's base English catalog underneath them.
//
// Lookup is layered, in this order: the requested locale's app catalog,
// the default locale's app catalog, the framework's catalog for the
// requested locale, when it ships one, the base catalog, then the key
// itself. The middle layer is design doc §10's "missing keys fall back
// to the declared default locale during development"; the base layer is
// what lets a single-locale app get correctly-worded built-in
// components without writing a catalog at all. Returning the key —
// never "" — keeps a missing string visible on the page instead of
// silently blanking a sentence.
type Locales struct {
	codes []string
	def   string
	app   map[string]Catalog
	fw    map[string]Catalog
	base  Catalog
}

// NewLocales validates the declared set and reads locales/<code>.toml
// out of fsys for each declared code. fsys may be nil (framework base
// catalog only). A declared locale with no catalog file is not an error:
// a single-locale app declares "en" and ships no locales/ directory.
func NewLocales(codes []string, def string, base Catalog, fsys fs.FS) (*Locales, error) {
	if def == "" {
		def = "en"
	}
	if len(codes) == 0 {
		codes = []string{def}
	}
	seen := map[string]bool{}
	for _, c := range codes {
		if c == "" {
			return nil, fmt.Errorf("rastrillo: empty locale code in %v", codes)
		}
		if seen[c] {
			return nil, fmt.Errorf("rastrillo: duplicate locale code %q", c)
		}
		seen[c] = true
	}
	if !seen[def] {
		return nil, fmt.Errorf("rastrillo: default locale %q is not in the declared set %v", def, codes)
	}

	l := &Locales{
		codes: append([]string(nil), codes...),
		def:   def,
		app:   map[string]Catalog{},
		base:  base,
	}
	if l.base == nil {
		l.base = Catalog{}
	}
	l.fw = map[string]Catalog{}
	for _, c := range codes {
		if bc, ok := baseCatalogs[c]; ok {
			l.fw[c] = bc
		}
	}
	if fsys == nil {
		return l, nil
	}
	for _, c := range codes {
		name := path.Join("locales", c+".toml")
		data, err := fs.ReadFile(fsys, name)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("rastrillo: read %s: %w", name, err)
		}
		m, err := catalog.Decode(data)
		if err != nil {
			return nil, fmt.Errorf("rastrillo: %s: %w", name, err)
		}
		l.app[c] = Catalog(m)
	}
	return l, nil
}

// Codes returns the declared locale codes in declaration order.
func (l *Locales) Codes() []string { return append([]string(nil), l.codes...) }

// Default returns the declared default locale.
func (l *Locales) Default() string { return l.def }

// Has reports whether code is one of the declared locales.
func (l *Locales) Has(code string) bool {
	for _, c := range l.codes {
		if c == code {
			return true
		}
	}
	return false
}

// T looks key up for locale, layered per this type's doc comment.
func (l *Locales) T(locale, key string) string {
	if v, ok := l.app[locale][key]; ok {
		return v
	}
	if v, ok := l.app[l.def][key]; ok {
		return v
	}
	if v, ok := l.fw[locale][key]; ok {
		return v
	}
	if v, ok := l.base[key]; ok {
		return v
	}
	return key
}

// FrameworkHas reports whether the framework ships a base catalog for
// a declared code — matched exactly, so "zh" never finds "zh-Hans"
// (spec §3.3).
func (l *Locales) FrameworkHas(code string) bool { _, ok := l.fw[code]; return ok }

// Dir is the HTML dir attribute for a locale: "rtl" for the
// right-to-left scripts a rastrillo app can declare, "ltr" otherwise.
// Decided on the primary subtag, so ar-EG mirrors as ar does.
func Dir(locale string) string {
	switch primarySubtag(strings.ToLower(locale)) {
	case "ar", "fa", "he", "ur":
		return "rtl"
	}
	return "ltr"
}

// Tf is T plus {name} placeholder interpolation — design doc §10's
// `{{Tf "key" .Args}}`. args are slog-style alternating name/value
// pairs (the convention this repo already uses for key/value lists), or
// a single map[string]any / map[string]string, which is exactly what
// the doc's own .Args example passes.
func (l *Locales) Tf(locale, key string, args ...any) string {
	return interpolate(l.T(locale, key), args)
}

// interpolate substitutes {name} placeholders. A placeholder with no
// matching argument is left verbatim: a translator's typo then shows up
// in the page rather than silently deleting part of a sentence.
func interpolate(s string, args []any) string {
	if !strings.Contains(s, "{") {
		return s
	}
	m := argMap(args)
	var b strings.Builder
	for {
		i := strings.Index(s, "{")
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		j := strings.Index(s[i:], "}")
		if j < 0 {
			b.WriteString(s)
			return b.String()
		}
		j += i
		if v, ok := m[s[i+1:j]]; ok {
			b.WriteString(s[:i])
			b.WriteString(v)
		} else {
			b.WriteString(s[:j+1])
		}
		s = s[j+1:]
	}
}

func argMap(args []any) map[string]string {
	if len(args) == 1 {
		switch v := args[0].(type) {
		case map[string]string:
			return v
		case map[string]any:
			m := make(map[string]string, len(v))
			for k, val := range v {
				m[k] = fmt.Sprint(val)
			}
			return m
		}
	}
	m := make(map[string]string, len(args)/2)
	for i := 0; i+1 < len(args); i += 2 {
		m[fmt.Sprint(args[i])] = fmt.Sprint(args[i+1])
	}
	return m
}
