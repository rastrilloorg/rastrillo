// Command numcheck writes the plain-number grid the ICU oracle checks
// number.Format against: every shipped locale, a handful of values,
// grouped, plain, with two places and as a percent, beside what Format
// writes. oracle/numbers.js formats each row with Intl.NumberFormat and
// fails on any row whose string differs and is not one of its listed
// data disagreements (fr-CH's group mark, as for money).
//
//	go run ./cmd/numcheck > numbers.tsv
//	node oracle/numbers.js < numbers.tsv
package main

import (
	"fmt"
	"math"
	"os"
	"strings"

	"amadan.net/rastrillo/rastrillo/locales"
	_ "amadan.net/rastrillo/rastrillo/locales/modern"
	"amadan.net/rastrillo/rastrillo/number"
)

// values cover minimum grouping (4 and 5 digits), lakh grouping, a
// negative with a fraction, a fraction alone, and zero.
var values = []number.Decimal{"0", "7", "1234", "12345", "-1234.5",
	"1234567.891", "0.25", "100000000"}

var styles = map[string]number.Options{
	"grouped": {},
	"plain":   {Plain: true},
	"places2": {Places: 2},
	"percent": {Percent: true},
}

// nonFinite writes NaN and the infinities, grouped and as a percent,
// which FormatFloat writes in each locale's own symbols.
func nonFinite(out *strings.Builder) int {
	rows := 0
	for _, id := range locales.LinkedIDs() {
		for _, value := range []float64{math.NaN(), math.Inf(1),
			math.Inf(-1)} {
			for _, style := range []string{"grouped", "percent"} {
				written, _ := number.FormatFloat(id, value, 64, styles[style])
				fmt.Fprintf(out, "%s\t%v\t%s\t%s\n", id, value, style,
					written)
				rows++
			}
		}
	}
	return rows
}

func main() {
	var out strings.Builder
	rows := 0
	for _, id := range locales.LinkedIDs() {
		for _, value := range values {
			for _, style := range []string{"grouped", "plain", "places2",
				"percent"} {
				written, err := number.Format(id, value, styles[style])
				if err != nil {
					fmt.Fprintln(os.Stderr, "numcheck:", err)
					os.Exit(1)
				}
				fmt.Fprintf(&out, "%s\t%s\t%s\t%s\n", id, value, style,
					written)
				rows++
			}
		}
	}
	rows += nonFinite(&out)
	fmt.Fprintf(&out, "#rows\t%d\n", rows)
	os.Stdout.WriteString(out.String())
}
