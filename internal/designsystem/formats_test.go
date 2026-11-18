package designsystem

import (
	"slices"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/currency"
	"amadan.net/rastrillo/rastrillo/dates"
	"amadan.net/rastrillo/rastrillo/number"
)

// formatHelper is a section whose value a ui helper writes: the
// helper's name, and what it writes in one formatting locale.
type formatHelper struct {
	name   string
	values func(t *testing.T, format string) []string
}

// formatHelpers are the sections a ui helper writes a value for. Every
// other section is guidance: nothing in ui writes a duration, a file
// size, a code, a name or an address.
var formatHelpers = map[string]formatHelper{
	"dates": {"date", func(t *testing.T, format string) []string {
		published := time.Date(2026, time.August, 2, 0, 0, 0, 0, time.UTC)
		return []string{dates.For(format).Format("dateYear", published)}
	}},
	"numbers": {"number", func(t *testing.T, format string) []string {
		return []string{
			numberIn(t, format, "54173"), numberIn(t, format, "1284902"),
		}
	}},
	"money": {"money", func(t *testing.T, format string) []string {
		return []string{
			moneyIn(t, format, 128450, "EUR"),
			moneyIn(t, format, 1285, "JPY"),
			moneyIn(t, format, 1284500, "KWD"),
		}
	}},
	"output": {"money", func(t *testing.T, format string) []string {
		return []string{moneyIn(t, format, 14400, "USD")}
	}},
}

func numberIn(t *testing.T, format string, value number.Decimal) string {
	t.Helper()
	written, err := number.Format(format, value, number.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return written
}

func moneyIn(t *testing.T, format string, minor int64, code string) string {
	t.Helper()
	written, err := currency.Format(format, minor, code)
	if err != nil {
		t.Fatal(err)
	}
	return written
}

// A section whose value a ui helper writes shows the call in its
// Template tab, and its sample is what that call writes on the page's
// locale; a section with no helper has no Template tab.
func TestEachFormatShowsItsHelper(t *testing.T) {
	for _, locale := range rastrillo.BaseLocales() {
		views, err := buildFormats("", RootTheme(), locale, previewSet{})
		if err != nil {
			t.Fatal(err)
		}
		docs := formatDocs()
		for index, doc := range docs {
			view := views[index]
			call := sourceTexts(`<pre class="ds-src"><code>` +
				string(view.Preview.Call) + `</code></pre>`)
			helper, ok := formatHelpers[doc.Key]
			if !ok {
				if view.Preview.Call != "" {
					t.Errorf("%s/%s: a Template tab with no helper in it",
						locale, doc.Key)
				}
				continue
			}
			if len(call) == 0 ||
				!strings.Contains(call[0], "{{"+helper.name+" $L ") {
				t.Errorf("%s/%s: the Template tab lacks {{%s $L …}}: %q",
					locale, doc.Key, helper.name, call)
			}
			sample := formatMarkup(doc, locale)
			for _, value := range helper.values(t, formatLocale(locale)) {
				if !strings.Contains(sample, value) {
					t.Errorf("%s/%s: the sample lacks %s's %q:\n%s",
						locale, doc.Key, helper.name, value, sample)
				}
			}
		}
	}
}

// Rastrillo writes dates now, so no section says it will not.
func TestNoFormatClaimsRastrilloCannot(t *testing.T) {
	gone := "Put a date in a <time> tag, with a version of the date a " +
		"computer can read. Write the date itself however your readers " +
		"expect to see it. Rastrillo will not format it for you."
	if _, ok := prose[gone]; ok {
		t.Errorf("prose still carries %q", gone)
	}
	for _, doc := range formatDocs() {
		if strings.Contains(doc.Body, "will not format") {
			t.Errorf("%s: %q", doc.Key, doc.Body)
		}
	}
}

// The Money sample writes three currencies the way each page's
// language does; the currency comes from the data, so the euro stays a
// euro on the Japanese page.
func TestMoneySampleIsFormattedPerLocale(t *testing.T) {
	money := formatDocNamed(t, "money")
	for locale, wants := range map[string][]string{
		"en":    {"€1,284.50", "¥1,285", "KWD 1,284.500"}, // English writes the yen with U+00A5
		"ja":    {"￥1,285"},                               // Japanese with the fullwidth U+FFE5 (Appendix A.1)
		"es":    {"1284,50 €"},
		"pt-PT": {"1284,50\u00a0€"}, // Appendix A.1: European Portuguese, not CLDR pt's Brazilian € 1.284,50
		// currency.Format("ar", 128450, "EUR") has no trailing RLM
		// after the symbol; Appendix A.1 only tables ar SAR, not ar
		// EUR, so this is the formatter's actual shape, not the
		// table's.
		"ar": {"‏1,284.50 €"},
	} {
		// The money helper formats an unknown locale in the app's
		// default, so a locale the gallery has no page for would pass
		// here and prove nothing about what a reader sees.
		if !slices.Contains(rastrillo.BaseLocales(), locale) {
			t.Errorf("%s: the gallery has no page in this locale", locale)
		}
		got := formatMarkup(money, locale)
		for _, w := range wants {
			if !strings.Contains(got, w) {
				t.Errorf("%s: %q lacks %q", locale, got, w)
			}
		}
	}
}

func formatDocNamed(t *testing.T, key string) formatDoc {
	t.Helper()
	for _, doc := range formatDocs() {
		if doc.Key == key {
			return doc
		}
	}
	t.Fatalf("no %s section", key)
	return formatDoc{}
}
