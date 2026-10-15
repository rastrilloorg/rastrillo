package locales_test

import (
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/locales"
	_ "amadan.net/rastrillo/rastrillo/locales/modern"
)

// Round three, finding 2: offered locale codes and packs are different
// things; the packs are derived, deduplicated.
func TestPacksFor(t *testing.T) {
	got, err := locales.PacksFor([]string{"en", "zh-Hans", "zh-Hant", "pt-PT", "pt", "yue"})
	if err != nil || strings.Join(got, " ") != "en pt yue zh" {
		t.Errorf("PacksFor = %v, %v; want [en pt yue zh]", got, err)
	}
	if _, err := locales.PacksFor([]string{"de--CH"}); err == nil {
		t.Error("PacksFor accepted a malformed tag")
	}
}

// Round three, finding 3, and round four, findings 5 and 6: the
// runtime check, in the binary that serves, never accepts a
// configuration that would fall back, never repairs an identity, and
// keeps the declared default apart from its formatting locale. The two
// missing-pack cases live in localestest/gallery: with every pack
// linked here, they cannot fail.
func TestCheckConfig(t *testing.T) {
	for _, c := range []struct {
		def           string
		offered       []string
		wantErr       string // substring; "" means accepted
		wantDef       string
		wantDefFormat string
	}{
		{"", nil, "", "en", "en"},                                        // nothing set: en, and en is linked
		{"pt-PT", nil, "", "pt-PT", "pt-PT"},                             // DefaultLocale only
		{"en", []string{"en", "zh-Hans", "pt-PT", "ar"}, "", "en", "en"}, // offered codes, not packs
		{"", []string{"es", "en"}, "", "es", "es"},                       // default is offered[0]
		{"zh-TW", []string{"zh-TW", "en"}, "", "zh-TW", "zh-Hant"},       // identity kept, format separate
		{"en-US", nil, "", "en-US", "en"},
		{"en", []string{"en", "xx"}, "not a locale", "", ""},
		{"en", []string{"en", "de--CH"}, "not a well-formed", "", ""},
		{"es", []string{"en"}, "is not one of the offered", "", ""},
		// Strict identities: reported with the canonical form, never repaired.
		{" en ", nil, `write "en"`, "", ""},
		{"en", []string{"en", "en_US"}, `write "en-US"`, "", ""},
		{"EN", nil, `write "en"`, "", ""},
		{"en", []string{"en", "zh-hant"}, `write "zh-Hant"`, "", ""},
		{"en", []string{"en", "pt-pt"}, `write "pt-PT"`, "", ""},
		{"en", []string{"en", "es", "en"}, "offered twice", "", ""},
		{"en", []string{"en", "e n"}, "white space", "", ""},
	} {
		cfg, err := locales.CheckConfig(c.def, c.offered)
		switch {
		case c.wantErr == "" && err != nil:
			t.Errorf("CheckConfig(%q, %v) = %v", c.def, c.offered, err)
		case c.wantErr != "" && (err == nil || !strings.Contains(err.Error(), c.wantErr)):
			t.Errorf("CheckConfig(%q, %v) error = %v, want %q", c.def, c.offered, err, c.wantErr)
		case c.wantErr == "" && (cfg.Default != c.wantDef || cfg.DefaultFormat != c.wantDefFormat):
			t.Errorf("CheckConfig(%q, %v) = Default %q DefaultFormat %q, want %q %q", c.def, c.offered, cfg.Default, cfg.DefaultFormat, c.wantDef, c.wantDefFormat)
		}
	}
}

// Confirmation round, gap 2: case follows each subtag's grammatical
// role, never its length.
func TestCanonicalIdentityByRole(t *testing.T) {
	for in, want := range map[string]string{
		"en-u-nu-latn":            "", // "nu" is an extension subtag: stays lower
		"de-CH-1996":              "", // a variant after a region
		"zh-Hant-TW":              "",
		"en-x-private":            "",
		"sr-Latn-RS-u-ca-gregory": "",
		"EN-u-NU-latn":            "en-u-nu-latn",
		"de-ch-1996":              "de-CH-1996",
		"zh-hant-tw":              "zh-Hant-TW",
		"en-X-Private":            "en-x-private",
		"es-419":                  "",
		"de-DE-1901":              "",
	} {
		got, err := locales.CanonicalIdentity(in)
		if want == "" {
			if err != nil || got != in {
				t.Errorf("CanonicalIdentity(%q) = %q, %v; want it accepted as canonical", in, got, err)
			}
			continue
		}
		if err == nil || got != want || !strings.Contains(err.Error(), `write "`+want+`"`) {
			t.Errorf("CanonicalIdentity(%q) = %q, %v; want an error naming %q", in, got, err, want)
		}
	}
	// A private-use-only tag has no language: it is not an identity.
	if _, err := locales.CanonicalIdentity("x-private"); err == nil || !strings.Contains(err.Error(), "well-formed") {
		t.Errorf("CanonicalIdentity(x-private) = %v, want not well-formed", err)
	}
}
