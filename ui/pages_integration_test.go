package ui

import (
	"strings"
	"testing"
	"testing/fstest"

	"amadan.net/rastrillo/rastrillo/locales"
	_ "amadan.net/rastrillo/rastrillo/locales/en"
	_ "amadan.net/rastrillo/rastrillo/locales/zh"
)

// CheckConfig's declared default is an offered identity, and its
// formatting locale is a separate field. The identity keys the catalog
// and the localizer; the formatting locale only formats. zh-TW is the
// case where the two differ, so a Pages that keyed formatting by the
// identity would print NT$ in Taiwan and $ in English.
func TestPagesCheckConfigFeedsCatalogs(t *testing.T) {
	cfg, err := locales.CheckConfig("zh-TW", []string{"zh-TW", "en"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Default != "zh-TW" || cfg.DefaultFormat != "zh-Hant" || cfg.Formats["en"] != "en" {
		t.Fatalf("config = %+v", cfg)
	}
	p, err := NewPages(PagesConfig{
		App: fstest.MapFS{
			"layout.html": {Data: []byte(`{{define "layout"}}{{$L := localizer $}}{{T $L "price"}}: {{money $L .Price "TWD"}}{{end}}`)},
			"home.html":   {Data: []byte(``)},
		},
		Layout: "layout.html", Pages: map[string]PageFile{"home": {Path: "home.html"}},
		Locale: cfg,
		LocaleFS: fstest.MapFS{
			"locales/zh-TW.toml": {Data: []byte(`price = "價格"`)},
			"locales/en.toml":    {Data: []byte(`price = "Price"`)},
		},
	})
	if err != nil {
		t.Fatalf("NewPages with CheckConfig's identities: %v", err)
	}
	for identity, want := range map[string]string{"zh-TW": "價格: $1,284.50", "en": "Price: NT$1,284.50"} {
		var b strings.Builder
		data := map[string]any{"L": p.Localizer(identity), "Price": int64(128450)}
		if err := p.Execute(&b, "home", data); err != nil || b.String() != want {
			t.Errorf("%s: %q, %v; want %q", identity, b.String(), err, want)
		}
	}
}
