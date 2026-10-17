package locales_test

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"amadan.net/rastrillo/rastrillo/locales"
	_ "amadan.net/rastrillo/rastrillo/locales/modern"
)

func TestResolveLocale(t *testing.T) {
	for tag, want := range map[string]string{
		"zh-TW": "zh-Hant", "zh-Hant": "zh-Hant", "ZH-hant-tw": "zh-Hant",
		"zh-HK": "zh-Hant-HK", "zh": "zh", "zh-CN": "zh-Hans",
		"en-IE": "en-IE", "es-MX": "es-MX", "pt_BR": "pt", "pt-PT": "pt-PT",
		"de-DE-u-nu-arab": "de", "de-CH-u-nu-arab": "de-CH", "de-1996": "de",
		"sr-ME": "sr-Latn-ME", "iw": "he",
		// Round two, finding 3: an alias's whole replacement applies.
		"swc": "sw-CD", "cnr": "sr-Latn-ME", "drw": "fa-AF", "prs": "fa-AF",
		"swc-TZ": "sw", // the tag's own region wins over the replacement's; TZ is sw's default
		// Malformed tags fall back to root, never to a guess.
		"de--CH": "root", "de-CH-!!!": "root", "-de": "root", "de-": "root",
		"de-CH-u": "root", "de-CH-x": "root", "x-private": "root",
		"toolonglanguage": "root", "d": "root", "": "root", "123": "root",
		// Grammatical but unknown.
		"xx": "root", "tlh": "root",
	} {
		if got := locales.Resolve(tag).ID; got != want {
			t.Errorf("Resolve(%q).ID = %q, want %q", tag, got, want)
		}
	}
	if r := locales.Resolve("de--CH"); !r.Malformed {
		t.Errorf("Resolve(%q).Malformed = false", "de--CH")
	}
}

// SetDefault and Install refuse an unlinked default by panicking:
// CheckConfig refuses that configuration first, so an unlinked id
// reaching them is a framework bug, and formatting every request in
// root instead would hide it.
func TestUnlinkedDefaultPanics(t *testing.T) {
	defer locales.SetDefaultForTest("")()
	for _, id := range []string{"xx", "EN", ""} {
		for name, call := range map[string]func(){
			"SetDefault": func() { locales.SetDefault(id) },
			"Install":    func() { locales.Install(locales.Config{DefaultFormat: id}) },
		} {
			func() {
				defer func() {
					if r := recover(); r == nil || !strings.Contains(fmt.Sprint(r), fmt.Sprintf("default locale %s is not linked", id)) {
						t.Errorf("%s(%q): panic %v, want one naming the unlinked default", name, id, r)
					}
				}()
				call()
			}()
			if got := locales.Resolve("xx").ID; got != "root" {
				t.Errorf("after %s(%q) refused, the default is %q, want root", name, id, got)
			}
		}
	}
}

// Install copies Formats: the configuration is the caller's value, and
// a caller that keeps editing it (or a test reusing one) must not
// change what a request formats in afterwards.
func TestInstallCopiesTheFormats(t *testing.T) {
	defer locales.SetDefaultForTest("")()
	cfg, err := locales.CheckConfig("en", []string{"en", "zh-TW"})
	if err != nil {
		t.Fatal(err)
	}
	locales.Install(cfg)
	cfg.Formats["zh-TW"] = "en"
	delete(cfg.Formats, "en")
	cfg.Formats["pt-PT"] = "en"
	for identity, want := range map[string]string{"zh-TW": "zh-Hant", "en": "en", "pt-PT": "pt-PT"} {
		if got := locales.FormatFor(identity); got != want {
			t.Errorf("FormatFor(%q) = %q after editing the installed Config, want %q", identity, got, want)
		}
	}
}

// Install and FormatFor run on different goroutines: a boot or a test
// installing while requests format. The package guards the default
// and the mapping with one lock; this test is the -race witness that
// it stays that way, and that a reader never sees a mapping from one
// configuration beside the default of another.
func TestInstallAndFormatForUnderRace(t *testing.T) {
	defer locales.SetDefaultForTest("")()
	a, err := locales.CheckConfig("en", []string{"en", "zh-TW"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := locales.CheckConfig("es", []string{"es", "zh-TW"})
	if err != nil {
		t.Fatal(err)
	}
	locales.Install(a)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				if j%2 == 0 {
					locales.Install(a)
				} else {
					locales.Install(b)
				}
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				if got := locales.FormatFor("zh-TW"); got != "zh-Hant" {
					t.Errorf("FormatFor(zh-TW) = %q during Install, want zh-Hant from either configuration", got)
				}
				// xx is in no pack, so it formats in the installed default.
				if got := locales.FormatFor("xx"); got != "en" && got != "es" {
					t.Errorf("FormatFor(xx) = %q during Install, want en or es", got)
				}
			}
		}()
	}
	wg.Wait()
}
