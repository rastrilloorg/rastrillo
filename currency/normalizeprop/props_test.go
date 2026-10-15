package normalizeprop

import (
	"errors"
	"math"
	"math/big"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"unicode"

	"amadan.net/rastrillo/rastrillo/currency"
	"amadan.net/rastrillo/rastrillo/form"
	"amadan.net/rastrillo/rastrillo/locales"
	_ "amadan.net/rastrillo/rastrillo/locales/modern"
)

// corpus is a deterministic spread of what people type: digit runs of
// every length that matters, every separator, adjacent separators,
// signs, parentheses and spellings, own and shared.
func corpus() []string {
	runs := []string{"0", "1", "12", "123", "1234", "05", "50", "500", "5000"}
	seps := []string{".", ",", " ", "'", "\u00a0", "\u066b", "\u066c", ",.", ".,", ",,"}
	var out []string
	var build func(prefix string, depth int)
	build = func(prefix string, depth int) {
		out = append(out, prefix)
		if depth == 3 {
			return
		}
		ss, rs := seps, runs
		if depth == 2 { // the third group: the separators that can be ambiguous
			ss, rs = seps[:2], []string{"0", "50", "500", "5000"}
		}
		for _, s := range ss {
			for _, r := range rs {
				build(prefix+s+r, depth+1)
			}
		}
	}
	for _, r := range runs {
		build(r, 1)
	}
	n := len(out)
	for i, s := range out[:n:n] {
		if i%5 == 0 { // a deterministic sample keeps the run short
			out = append(out, "-"+s, "("+s+")", "--"+s, "-("+s+")", "(-"+s+")", "+"+s, s+"-",
				"$"+s, "US$"+s, "CA$"+s, "A$"+s, s+" €", "USD "+s, "CAD "+s, s+" kr", "¥"+s, ","+s, s+".")
		}
	}
	return out
}

var propertyLocales = []string{"en", "en-CA", "en-AU", "de", "fr", "hi", "ar", "ja", "de-CH", "fr-CH", "es", "pt-CV", "bn", "fa"}
var propertyCodes = []string{"JPY", "USD", "CAD", "AUD", "EUR", "KWD", "INR", "CVE", "CHF"}

// Consistency with strict reading, not proof of intent: where strict
// accepts, normalising returns the same value or refuses the input as
// ambiguous with strict's value among the readings. It never silently
// stores a different amount than today's parser would.
func TestNormalizeNeverOverridesStrict(t *testing.T) {
	inputs := corpus()
	var both, ambiguousOnly, normalisedOnly int
	for _, tag := range propertyLocales {
		for _, code := range propertyCodes {
			for _, in := range inputs {
				s, errS := currency.Parse(tag, in, code)
				n, errN := currency.ParseNormalized(tag, in, code)
				if errS != nil {
					if errN == nil {
						normalisedOnly++
					}
					continue
				}
				if errN == nil {
					if n != s {
						t.Fatalf("%s %s %q: strict %d, normalised %d", tag, code, in, s, n)
					}
					both++
					continue
				}
				var amb *currency.AmbiguousError
				if !errors.As(errN, &amb) || !holds(amb, s) {
					t.Fatalf("%s %s %q: strict %d, normalised %v (not an ambiguity holding it)", tag, code, in, s, errN)
				}
				ambiguousOnly++
			}
		}
	}
	t.Logf("both accept %d, only strict (ambiguous) %d, only normalised %d", both, ambiguousOnly, normalisedOnly)
	if both == 0 || ambiguousOnly == 0 || normalisedOnly == 0 {
		t.Fatal("the corpus no longer exercises every branch of the property")
	}
}

// Whatever normalising accepts, the stored value's display and seed are
// never read as a different single amount: they read back to it, or
// are refused as ambiguous with it among the readings. Where the seed
// is refused, the field's unchanged-seed check is what keeps it, so
// the second half posts each seed back, untouched, through form.Parse
// on an edit form and requires the same amount saved: a seed the
// parser cannot read must still save unchanged.
func TestNormalizeRedisplay(t *testing.T) {
	inputs := corpus()
	for _, tag := range propertyLocales {
		locale := currency.Resolve(tag).ID
		for _, code := range propertyCodes {
			for _, in := range inputs {
				v, err := currency.ParseNormalized(tag, in, code)
				if err != nil {
					continue
				}
				shown, _ := currency.Format(tag, v, code)
				seed, _ := currency.InputValue(tag, v, code)
				for _, text := range []string{shown, seed} {
					if !readsBackOrRefuses(tag, text, code, v) {
						t.Fatalf("%s %s %q = %d: %q does not read back", tag, code, in, v, text)
					}
				}
				rendered, err := form.SeedMoney(locale, &v, code, false)
				if err != nil {
					t.Fatal(err)
				}
				f := form.Field{Name: "P", Kind: form.Money, Currency: code, LocaleEcho: true, Negative: true, Stored: &v}
				if got, ok := form.Parse(unchanged(rendered), f).Minor("P"); !ok || got != v {
					t.Fatalf("%s %s %q = %d: unchanged seed %+v saved %d, %v", tag, code, in, v, rendered, got, ok)
				}
			}
		}
	}
}

// unchanged is the POST of a form whose money input nobody touched.
// The form values are set directly rather than encoded and parsed back:
// the property posts one for every input the corpus has accepted, in
// every locale and currency, inside this package's time budget.
func unchanged(in form.MoneyInput) *http.Request {
	v := url.Values{"P": {in.Value}, "P.locale": {in.Locale}, "P.currency": {in.Currency}, "P.seed": {in.Seed}}
	return &http.Request{Method: http.MethodPost, Form: v, PostForm: v}
}

// The same over every display and seed in every shipped locale and
// storage code. Since decision 23, a refusal is only ever the decimal
// direction: the one separator is the locale's decimal mark, never its
// group mark. The counts are reported for the spec.
func TestNormalizeReadsEveryDisplay(t *testing.T) {
	if testing.Short() {
		t.Skip("about 300,000 formats and parses")
	}
	amounts := []int64{math.MinInt64, -1, 0, 1, 1500, 123456789, math.MaxInt64}
	var displays, seeds, refusedDisplays, refusedSeeds int
	for _, id := range append(locales.LinkedIDs(), "root") {
		for _, code := range locales.StorageCodes() {
			for _, m := range amounts {
				shown, _ := currency.Format(id, m, code)
				seed, _ := currency.InputValue(id, m, code)
				for i, text := range []string{shown, seed} {
					if i == 0 {
						displays++
					} else {
						seeds++
					}
					if !readsBackOrRefuses(id, text, code, m) {
						t.Fatalf("%s %s %d: %q does not read back", id, code, m, text)
					}
					if v, err := currency.ParseNormalized(id, text, code); err == nil && v == m {
						continue
					}
					if !decimalDirection(text, code, id, m) {
						t.Fatalf("%s %s %d: %q refused, but not in the decimal direction", id, code, m, text)
					}
					if i == 0 {
						refusedDisplays++
					} else {
						refusedSeeds++
					}
				}
			}
		}
	}
	t.Logf("refused as ambiguous: %d of %d displays, %d of %d seeds", refusedDisplays, displays, refusedSeeds, seeds)
}

// decimalDirection reports whether a refused display or seed wrote m
// with its separator as the decimal mark: m is then the reading with
// the smaller magnitude, since a group reading of the same digits is a
// thousand times larger. The prototype asked the parser's internals for
// the page's grouped reading; the readings an AmbiguousError carries
// say the same thing without exporting them.
func decimalDirection(text, code, id string, m int64) bool {
	_, err := currency.ParseNormalized(id, text, code)
	var amb *currency.AmbiguousError
	if !errors.As(err, &amb) {
		return false
	}
	mine := absDecimal(amb.Readings, m)
	for _, r := range amb.Readings {
		x, _ := new(big.Rat).SetString(r.Decimal)
		if mine == nil || x.Abs(x).Cmp(mine) < 0 {
			return false
		}
	}
	return true
}

func absDecimal(rs []currency.Reading, m int64) *big.Rat {
	for _, r := range rs {
		if r.Storable && r.Minor == m {
			x, _ := new(big.Rat).SetString(r.Decimal)
			return x.Abs(x)
		}
	}
	return nil
}

func readsBackOrRefuses(tag, text, code string, v int64) bool {
	got, err := currency.ParseNormalized(tag, text, code)
	var amb *currency.AmbiguousError
	return err == nil && got == v || errors.As(err, &amb) && holds(amb, v)
}

func holds(amb *currency.AmbiguousError, v int64) bool {
	for _, r := range amb.Readings {
		if r.Storable && r.Minor == v {
			return true
		}
	}
	return false
}

// readerLocales are the readers a choice must be unambiguous to: every
// convention the decision table knows, through locales that use them.
var readerLocales = []string{"en", "de", "fr", "de-CH", "hi", "ar", "fa", "ja", "pt-CV", "bn"}

// Each choice in money_ambiguous, read by any reader, has exactly one
// reading, its own: the two can never be taken for each other.
func TestAmbiguousChoicesAreUnambiguous(t *testing.T) {
	inputs := corpus()
	checked := 0
	for _, tag := range propertyLocales {
		for _, code := range propertyCodes {
			for _, in := range inputs {
				_, err := currency.ParseNormalized(tag, in, code)
				var amb *currency.AmbiguousError
				if !errors.As(err, &amb) {
					continue
				}
				a, b := amb.Choices(tag, code)
				lo, hi := amb.Readings[0], amb.Readings[len(amb.Readings)-1]
				for _, reader := range readerLocales {
					for _, c := range []struct {
						text string
						want currency.Reading
					}{{a, lo}, {b, hi}} {
						if !readsOnlyAs(reader, c.text, code, c.want) {
							v, err := currency.ParseNormalized(reader, c.text, code)
							t.Fatalf("%s %s %q: choice %q read by %s gives %d, %v; want only %s", tag, code, in, c.text, reader, v, err, c.want.Decimal)
						}
					}
				}
				checked++
			}
		}
	}
	if checked == 0 {
		t.Fatal("the corpus produced no ambiguity to check")
	}
}

// readsOnlyAs is the prototype's "exactly one reading, want" asked
// through the exported reading: a storable want must come back as its
// own amount, and an over-precise one as ErrPrecision, which the parser
// returns only when no reading is storable. A choice has at most one
// separator and never three digits after it, so no grouping can read
// it, and the decimal readings agree; that is why a single value is
// enough here.
func readsOnlyAs(reader, text, code string, want currency.Reading) bool {
	v, err := currency.ParseNormalized(reader, text, code)
	if want.Storable {
		return err == nil && v == want.Minor
	}
	return errors.Is(err, currency.ErrPrecision)
}

// §5.1 states the ambiguity's exact shape: the only separator is one
// "." or ",", with one to three digits before it and exactly three
// after, the two readings differ, and the page's locale does not group
// with that separator (decision 23). Checked in every locale and
// currency against every digits-and-separators input in the corpus.
func TestAmbiguityHasExactlyTheStatedShape(t *testing.T) {
	plain := regexp.MustCompile("^[0-9 .,'\u00a0\u066b\u066c]+$")
	shape := regexp.MustCompile("^[0-9]{1,3}[.,][0-9]{3}$")
	inputs := corpus()
	for _, tag := range propertyLocales {
		for _, code := range propertyCodes {
			group := moneyGroup(tag, code)
			for _, in := range inputs {
				if !plain.MatchString(in) {
					continue
				}
				_, err := currency.ParseNormalized(tag, in, code)
				var amb *currency.AmbiguousError
				want := shape.MatchString(in) && strings.Trim(in, "0.,") != "" && !strings.Contains(in, group)
				if got := errors.As(err, &amb); got != want {
					t.Fatalf("%s %s %q: ambiguous %v, want %v (%v)", tag, code, in, got, want, err)
				}
			}
		}
	}
}

// moneyGroup is the group mark the page's locale writes money in, with
// the currency's own override applied and an all-space mark folded to
// U+0020 as the parser's Clean folds the input: the currency package's
// forCurrency and foldSpace, copied because only this test needs the
// mark on its own.
func moneyGroup(tag, code string) string {
	l, _ := locales.Data(currency.Resolve(tag).ID)
	g := l.CurrencyGroup
	if o, ok := l.Overrides[code]; ok && o.Group != "" {
		g = o.Group
	}
	if strings.TrimFunc(g, func(r rune) bool { return unicode.IsSpace(r) || unicode.In(r, unicode.Zs) }) == "" {
		return " "
	}
	return g
}
