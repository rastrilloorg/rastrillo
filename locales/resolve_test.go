package locales_test

import (
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
