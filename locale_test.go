package rastrillo

import (
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
)

func testFS() fstest.MapFS {
	return fstest.MapFS{
		"locales/en.toml": {Data: []byte("app.title = \"Orders\"\napp.hi = \"Hello {name}\"\n")},
		"locales/fr.toml": {Data: []byte("app.title = \"Commandes\"\n")},
	}
}

func TestNewLocalesReadsCatalogs(t *testing.T) {
	l, err := NewLocales([]string{"en", "fr"}, "en", Catalog{"ui.save": "Save"}, testFS())
	if err != nil {
		t.Fatal(err)
	}
	if got := l.Codes(); !reflect.DeepEqual(got, []string{"en", "fr"}) {
		t.Errorf("Codes = %v, want [en fr]", got)
	}
	if l.Default() != "en" {
		t.Errorf("Default = %q, want en", l.Default())
	}
	if !l.Has("fr") || l.Has("de") {
		t.Errorf("Has: fr should be declared, de should not")
	}
}

func TestLookupLayers(t *testing.T) {
	l, err := NewLocales([]string{"en", "fr"}, "en", Catalog{"ui.save": "Save"}, testFS())
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name        string
		locale, key string
		want        string
	}{
		{"app catalog for the requested locale", "fr", "app.title", "Commandes"},
		{"falls back to the default locale's catalog", "fr", "app.hi", "Hello {name}"},
		{"falls back to the framework base catalog", "fr", "ui.save", "Save"},
		{"unknown key returns the key verbatim", "fr", "nope.nothing", "nope.nothing"},
		{"undeclared locale still resolves via the default", "de", "app.title", "Orders"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := l.T(tt.locale, tt.key); got != tt.want {
				t.Errorf("T(%q, %q) = %q, want %q", tt.locale, tt.key, got, tt.want)
			}
		})
	}
}

func TestTfInterpolation(t *testing.T) {
	l, err := NewLocales([]string{"en"}, "en", Catalog{
		"greet":   "Hello {name}, you have {count} orders",
		"noargs":  "Plain",
		"missing": "Hi {who}",
		"twice":   "{a} and {a}",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		key  string
		args []any
		want string
	}{
		{"slog-style pairs", "greet", []any{"name", "Ada", "count", 3}, "Hello Ada, you have 3 orders"},
		{"single map[string]any", "greet", []any{map[string]any{"name": "Ada", "count": 3}}, "Hello Ada, you have 3 orders"},
		{"single map[string]string", "greet", []any{map[string]string{"name": "Ada", "count": "3"}}, "Hello Ada, you have 3 orders"},
		{"no placeholders", "noargs", nil, "Plain"},
		{"unmatched placeholder is left verbatim", "missing", []any{"name", "Ada"}, "Hi {who}"},
		{"repeated placeholder", "twice", []any{"a", "x"}, "x and x"},
		{"odd argument count ignores the tail", "greet", []any{"name", "Ada", "count"}, "Hello Ada, you have {count} orders"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := l.Tf("en", tt.key, tt.args...); got != tt.want {
				t.Errorf("Tf(%q, %v) = %q, want %q", tt.key, tt.args, got, tt.want)
			}
		})
	}
}

func TestNewLocalesDefaults(t *testing.T) {
	l, err := NewLocales(nil, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := l.Codes(); !reflect.DeepEqual(got, []string{"en"}) {
		t.Errorf("Codes = %v, want [en] for an app that declares nothing", got)
	}
	if l.Default() != "en" {
		t.Errorf("Default = %q, want en", l.Default())
	}
}

func TestNewLocalesRejectsBadSets(t *testing.T) {
	tests := []struct {
		name    string
		codes   []string
		def     string
		wantSub string
	}{
		{"default not declared", []string{"fr", "de"}, "en", `default locale "en" is not in the declared set`},
		{"duplicate code", []string{"en", "en"}, "en", `duplicate locale code "en"`},
		{"empty code", []string{"en", ""}, "en", "empty locale code"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewLocales(tt.codes, tt.def, nil, nil)
			if err == nil {
				t.Fatal("want an error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantSub) {
				t.Errorf("error = %q, want it to contain %q", err, tt.wantSub)
			}
		})
	}
}

func TestNewLocalesReportsABadCatalog(t *testing.T) {
	bad := fstest.MapFS{"locales/en.toml": {Data: []byte("[list]\n")}}
	_, err := NewLocales([]string{"en"}, "en", nil, bad)
	if err == nil {
		t.Fatal("want an error for an undecodable catalog, got nil")
	}
	if !strings.Contains(err.Error(), "locales/en.toml") {
		t.Errorf("error = %q, want it to name the offending file", err)
	}
}

func TestNewLocalesToleratesAMissingCatalog(t *testing.T) {
	// A single-locale app declares "en" and ships no locales/ at all:
	// the framework base catalog alone must carry it.
	l, err := NewLocales([]string{"en", "fr"}, "en", Catalog{"ui.save": "Save"}, fstest.MapFS{})
	if err != nil {
		t.Fatal(err)
	}
	if got := l.T("fr", "ui.save"); got != "Save" {
		t.Errorf("T = %q, want Save", got)
	}
}

func TestFrameworkCatalogLevel(t *testing.T) {
	// ga is shipped by the framework, fr is not; neither has an app
	// catalog for rastrillo.ui.cancel. base is what serve.go passes —
	// framework en plus the app's overlay.
	l, err := NewLocales([]string{"en", "ga", "fr"}, "en", BaseCatalog(), testFS())
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, locale, key, want string
	}{
		{"shipped locale resolves the framework catalog", "ga", "rastrillo.ui.cancel", "Cealaigh"},
		{"unshipped locale falls to framework en", "fr", "rastrillo.ui.cancel", "Cancel"},
		{"app catalog still beats the framework", "fr", "app.title", "Commandes"},
		{"app default beats the framework catalog", "ga", "app.title", "Orders"},
		{"declared code must match exactly", "zh", "rastrillo.ui.cancel", "Cancel"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := l.T(tt.locale, tt.key); got != tt.want {
				t.Errorf("T(%q,%q) = %q, want %q", tt.locale, tt.key, got, tt.want)
			}
		})
	}
	if !l.FrameworkHas("ga") || l.FrameworkHas("fr") {
		t.Error("FrameworkHas: ga yes, fr no")
	}
}

func TestAppCatalogOverridesAFrameworkKey(t *testing.T) {
	fsys := testFS()
	fsys["locales/ga.toml"] = &fstest.MapFile{Data: []byte("rastrillo.ui.cancel = \"Stad\"\n")}
	l, err := NewLocales([]string{"en", "ga"}, "en", BaseCatalog(), fsys)
	if err != nil {
		t.Fatal(err)
	}
	if got := l.T("ga", "rastrillo.ui.cancel"); got != "Stad" {
		t.Errorf("app ga catalog should win: %q", got)
	}
}

func TestDir(t *testing.T) {
	for in, want := range map[string]string{"ar": "rtl", "ar-EG": "rtl", "he": "rtl", "fa": "rtl", "ur": "rtl", "en": "ltr", "ga": "ltr", "": "ltr", "zh-Hans": "ltr"} {
		if got := Dir(in); got != want {
			t.Errorf("Dir(%q) = %q, want %q", in, got, want)
		}
	}
}
