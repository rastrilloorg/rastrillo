// Command check runs the formatter and parser against an independent
// oracle: ICU, through Node's Intl.NumberFormat.
//
//	go run ./cmd/check -emit grid.tsv
//	node oracle/intl.js < grid.tsv > intl.tsv
//	go run ./cmd/check -compare intl.tsv [-mismatches out.tsv]
//
// The grid is every shipped locale ("shipped") plus tags that are not
// CLDR locale ids ("ordinary": regional tags, aliases, odd case), times
// every currency in the storage set, times amounts that include both
// int64 boundaries. Amounts go to Intl as exact decimal strings with the
// fraction digits forced to the storage exponent, because the design
// never rounds.
//
// The comparison is strict in four ways, each answering a way a
// harness can claim more than it checked:
//   - the oracle file must contain exactly the expected rows, in order,
//     and end with the oracle's completion line, so an empty or
//     truncated run cannot pass;
//   - a row whose string differs from ICU's must be explained by an
//     executable check on that row (exemptions.go), never by its
//     locale's name alone;
//   - when our parser cannot read ICU's string, the row's explanation
//     must name the conventions ICU used, and the parser must read the
//     string under those; any other parse failure fails the run;
//   - Parse(Format(x)) and Parse(InputValue(x)) must return x on every
//     row, ICU or no ICU.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"

	"amadan.net/rastrillo/rastrillo/currency"
	"amadan.net/rastrillo/rastrillo/locales"
	_ "amadan.net/rastrillo/rastrillo/locales/modern"
	"amadan.net/rastrillo/rastrillo/money"
)

// ordinaryTags are tags an app's locale list or a browser can hold that
// are not CLDR locale ids, so they exercise resolution, not lookup.
var ordinaryTags = []string{
	"en-IE", "es-MX", "zh-TW", "zh-HK", "zh-MO", "zh-Hant", "zh-Hans-SG",
	"ZH-hant-tw", "pt_BR", "sr-ME", "uz-AF", "pa-PK", "iw", "in", "en-US",
	"de-DE", "fr-FR", "ar-SA", "ar-MA", "es-US", "swc", "cnr", "drw", "prs",
}

// gallery are the 12 languages a new app offers by default, and
// galleryLocales the exact locale ids the gallery renders (pt-PT after
// the retag, pt kept for existing URLs).
var gallery = map[string]bool{"en": true, "ga": true, "zh": true, "es": true, "hi": true, "pt": true,
	"bn": true, "ru": true, "ja": true, "yue": true, "vi": true, "ar": true}

var galleryLocales = map[string]bool{"en": true, "ga": true, "zh-Hans": true, "es": true, "hi": true,
	"pt": true, "pt-PT": true, "bn": true, "ru": true, "ja": true, "yue": true, "vi": true, "ar": true}

// icuUnsupported pins the tags ICU is expected to have no data for. An
// empty ICU result on any other row fails the run, and so does a pinned
// tag that ICU now formats, so the oracle can never be bypassed by
// answering nothing.
var icuUnsupported = map[string]bool{"ht": true}

var amounts = []int64{0, 1, -1, 1234, 12345, -128450, 1234567850, math.MaxInt64, math.MinInt64}

type gridRow struct {
	category, tag, code string
	exponent            int
	minor               int64
}

func (g gridRow) String() string {
	return fmt.Sprintf("%s\t%s\t%s\t%d\t%d\t%s", g.category, g.tag, g.code, g.exponent, g.minor, money.Decimal(g.minor, g.code))
}

// grid is the one definition of the expected rows, used to write the
// grid and to check that the oracle answered exactly it.
func grid() []gridRow {
	var tags [][2]string
	for _, id := range locales.LinkedIDs() {
		tags = append(tags, [2]string{"shipped", id})
	}
	for _, t := range ordinaryTags {
		if _, shipped := locales.PackOf(t); !shipped {
			tags = append(tags, [2]string{"ordinary", t})
		}
	}
	var out []gridRow
	for _, t := range tags {
		for _, c := range locales.StorageCodes() {
			cur, _ := locales.Storage(c)
			for _, m := range amounts {
				out = append(out, gridRow{t[0], t[1], c, cur.Exponent, m})
			}
		}
	}
	return out
}

func main() {
	emit := flag.String("emit", "", "write the grid to this file")
	compare := flag.String("compare", "", "compare against this oracle output")
	mismatches := flag.String("mismatches", "", "write every differing row and its explanation here")
	flag.Parse()
	switch {
	case *emit != "":
		writeGrid(*emit)
	case *compare != "":
		os.Exit(compareGrid(*compare, *mismatches))
	default:
		flag.Usage()
		os.Exit(2)
	}
}

func writeGrid(path string) {
	f, err := os.Create(path)
	if err != nil {
		fail(err)
	}
	w := bufio.NewWriter(f)
	for _, g := range grid() {
		fmt.Fprintln(w, g.String())
	}
	if err := w.Flush(); err != nil {
		fail(err)
	}
	f.Close()
}

type tally struct {
	rows, noICU, equal, differ, unexplained                  int
	parsedICU, parseExplained, parseWaived, parseUnexplained int
	roundTripFail                                            int
	byReason                                                 map[string]int
	reasonLocales                                            map[string]map[string]bool
}

func compareGrid(path, mismatchPath string) int {
	f, err := os.Open(path)
	if err != nil {
		fail(err)
	}
	defer f.Close()
	var out *bufio.Writer
	if mismatchPath != "" {
		mf, err := os.Create(mismatchPath)
		if err != nil {
			fail(err)
		}
		defer mf.Close()
		out = bufio.NewWriter(mf)
		defer out.Flush()
	}
	expected := grid()
	tallies := map[string]*tally{}
	for _, c := range []string{"shipped", "ordinary", "gallery", "gallery-exact"} {
		tallies[c] = &tally{byReason: map[string]int{}, reasonLocales: map[string]map[string]bool{}}
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	n := 0
	missingICU := 0
	complete := false
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "#complete\t") {
			if got, _ := strconv.Atoi(strings.TrimPrefix(line, "#complete\t")); got != len(expected) || n != len(expected) {
				fail(fmt.Errorf("oracle says it completed %s rows; expected %d, read %d", strings.TrimPrefix(line, "#complete\t"), len(expected), n))
			}
			complete = true
			continue
		}
		if complete {
			fail(fmt.Errorf("row after the completion line: %q", line))
		}
		if n >= len(expected) {
			fail(fmt.Errorf("oracle has more rows than the grid's %d", len(expected)))
		}
		want := expected[n].String()
		p := strings.Split(line, "\t")
		if len(p) != 8 || strings.Join(p[:6], "\t") != want {
			fail(fmt.Errorf("oracle row %d is %q, want the grid's %q plus two columns", n+1, line, want))
		}
		g := expected[n]
		n++
		icuLocale, icu := p[6], p[7]
		ts := []*tally{tallies[g.category]}
		if pack, _ := locales.PackOf(g.tag); g.category == "shipped" && gallery[pack] {
			ts = append(ts, tallies["gallery"])
		}
		if g.category == "shipped" && galleryLocales[g.tag] {
			ts = append(ts, tallies["gallery-exact"])
		}
		ours, err := currency.Format(g.tag, g.minor, g.code)
		if err != nil {
			fail(err)
		}
		seed, _ := currency.InputValue(g.tag, g.minor, g.code)
		v1, e1 := currency.Parse(g.tag, ours, g.code)
		v2, e2 := currency.Parse(g.tag, seed, g.code)
		roundTrip := e1 == nil && e2 == nil && v1 == g.minor && v2 == g.minor
		for _, t := range ts {
			t.rows++
			if !roundTrip {
				t.roundTripFail++
			}
		}
		if !roundTrip && out != nil {
			fmt.Fprintf(out, "round-trip\t%s\t%s\t%d\t%q\t%q\n", g.tag, g.code, g.minor, ours, seed)
		}
		if strings.HasPrefix(icuLocale, "!") {
			fail(fmt.Errorf("oracle row %d: ICU raised %s", n, strings.TrimPrefix(icuLocale, "!")))
		}
		if (icuLocale == "") != icuUnsupported[g.tag] {
			missingICU++
			if missingICU <= 5 {
				fmt.Fprintf(os.Stderr, "check: row %d (%s %s): ICU result %q, but %s is %spinned as unsupported\n",
					n, g.tag, g.code, icuLocale, g.tag, map[bool]string{true: "", false: "not "}[icuUnsupported[g.tag]])
			}
		}
		if icuLocale == "" {
			for _, t := range ts {
				t.noICU++
			}
			continue
		}
		if ours == icu {
			for _, t := range ts {
				t.equal++
			}
			continue
		}
		reason, parseLocale, transform := explain(g, icuLocale, icu)
		v, perr := currency.Parse(g.tag, icu, g.code)
		parsed := perr == nil && v == g.minor
		parseOK := parsed
		waived := !parsed && reason == notShipped // ICU used conventions no shipped locale has
		if !parsed && reason != "" && !waived {
			s := icu
			if transform != nil {
				s = transform(icu)
			}
			pv, pe := currency.Parse(parseLocale, s, g.code)
			parseOK = pe == nil && pv == g.minor
		}
		for _, t := range ts {
			t.differ++
			if reason == "" {
				t.unexplained++
			} else {
				t.byReason[reason]++
				if t.reasonLocales[reason] == nil {
					t.reasonLocales[reason] = map[string]bool{}
				}
				t.reasonLocales[reason][g.tag] = true
			}
			switch {
			case parsed:
				t.parsedICU++
			case waived:
				t.parseWaived++
			case parseOK:
				t.parseExplained++
			default:
				t.parseUnexplained++
			}
		}
		if out != nil {
			label := reason
			if label == "" {
				label = "UNEXPLAINED"
			}
			fmt.Fprintf(out, "format\t%s\t%s\t%s\t%d\t%q\t%q\t%s\tparsed=%v\tparse-explained=%v\t%s\n",
				g.category, g.tag, g.code, g.minor, ours, icu, icuLocale, parsed, parseOK, label)
		}
	}
	if err := sc.Err(); err != nil {
		fail(err)
	}
	if !complete {
		fail(fmt.Errorf("oracle output has no completion line after %d of %d rows: incomplete run", n, len(expected)))
	}
	failed := missingICU > 0
	if ids := sideFlips(); len(ids) > 0 {
		fmt.Printf(sideFlipMsg+"\n", ids)
		failed = true
	}
	fmt.Printf("ICU results missing outside the pinned unsupported tags %v, or present for one: %d\n", sortedKeys(boolCounts(icuUnsupported)), missingICU)
	for _, c := range []string{"shipped", "ordinary", "gallery", "gallery-exact"} {
		t := tallies[c]
		fmt.Printf("%s: %d rows (%d for pinned unsupported tags)\n", c, t.rows, t.noICU)
		fmt.Printf("  format: %d equal to ICU, %d differ, %d unexplained\n", t.equal, t.differ, t.unexplained)
		fmt.Printf("  parse of ICU's differing strings: %d read directly, %d read under the explained conventions, %d not checkable (no shipped locale), %d unexplained\n",
			t.parsedICU, t.parseExplained, t.parseWaived, t.parseUnexplained)
		fmt.Printf("  round trip Parse(Format) and Parse(InputValue): %d failures\n", t.roundTripFail)
		for _, r := range sortedKeys(t.byReason) {
			fmt.Printf("    %d rows, %d locales: %s\n", t.byReason[r], len(t.reasonLocales[r]), r)
		}
		if t.unexplained > 0 || t.parseUnexplained > 0 || t.roundTripFail > 0 {
			failed = true
		}
	}
	if failed {
		fmt.Println("FAIL")
		return 1
	}
	return 0
}

// explain returns why ICU's string differs from ours on this row, the
// locale whose conventions ICU's string follows, and the transformation
// (if any) that maps ICU's string onto ours. Every branch is checked
// against this row's own strings; "" means nothing explains it.
func explain(g gridRow, icuLocale, icu string) (string, string, func(string) string) {
	r := currency.Resolve(g.tag)
	ourID := r.ID
	// No shipped locale carries the tag's script (uz-AF wants uz-Arab,
	// which CLDR does not rate modern), so it formats with root by
	// design, and ICU, which has the data, cannot agree.
	if r.Wanted == "" && ourID == "root" {
		if _, shipped := locales.PackOf(icuLocale); !shipped {
			if s, err := currency.Format("root", g.minor, g.code); err == nil && s != icu {
				return notShipped, "", nil
			}
		}
	}
	// ICU formatted a different locale than the one we resolved.
	if icuID := currency.Resolve(icuLocale).ID; icuID != ourID {
		if s, err := currency.Format(icuLocale, g.minor, g.code); err == nil && s == icu {
			return "ICU resolved the tag to another locale and formatted that", icuLocale, nil
		}
	}
	// ICU reports our locale but used another shipped locale's data.
	for _, cand := range candidates(ourID) {
		if s, err := currency.Format(cand, g.minor, g.code); err == nil && s == icu {
			return "ICU reports the locale but formats with a parent's or the English hub's data", cand, nil
		}
	}
	for _, e := range exemptions {
		if !e.applies(ourID, g.code) {
			continue
		}
		ours, _ := currency.Format(g.tag, g.minor, g.code)
		if e.transform(icu) == ours {
			return e.reason, g.tag, e.transform
		}
	}
	return "", "", nil
}

// sideFlipMsg is Appendix C.2's op.check.side_flip, verbatim.
const sideFlipMsg = "a modern locale now moves the symbol for letter-final symbols (%v); decision 6 applies (follow CLDR); review the oracle's side-flip explanation"

// sideFlips lists the linked locales whose alphaNextToNumber pattern
// puts the symbol on the other side of the number. The oracle cannot
// vouch for them: ICU never applies such a pattern, and currency.Format
// does (decision 6), so their rows would differ for a reason no check
// here explains. It is a check of the data rather than a second
// formatting path, because the shipped package has only the one. Today
// it lists none: every locale that moves the symbol is outside the
// modern set.
func sideFlips() []string {
	var out []string
	for _, id := range locales.LinkedIDs() {
		if l, ok := locales.Data(id); ok && movesSymbol(l) {
			out = append(out, id)
		}
	}
	return out
}

// movesSymbol reports whether Alpha puts the symbol on the other side of
// the number from Standard. Only the positive subpattern is read, as
// currency.Format reads it to choose between them.
func movesSymbol(l locales.Locale) bool {
	if l.Alpha == l.Standard {
		return false
	}
	before := func(pattern string) bool {
		pos, _, _ := strings.Cut(pattern, ";")
		return strings.Index(pos, "¤") < strings.IndexAny(pos, "#0")
	}
	return before(l.Alpha) != before(l.Standard)
}

const notShipped = "no shipped locale has the tag's script, so it formats with root by design"

// candidates are the locales ICU plausibly substitutes: id's CLDR
// parents, then the English hubs, then root.
func candidates(id string) []string {
	var out []string
	for p := locales.Parent(id); p != "root"; p = locales.Parent(p) {
		out = append(out, p)
	}
	return append(out, "en-001", "en", "root")
}

func boolCounts(m map[string]bool) map[string]int {
	out := map[string]int{}
	for k := range m {
		out[k] = 1
	}
	return out
}

func sortedKeys(m map[string]int) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "check:", err)
	os.Exit(1)
}
