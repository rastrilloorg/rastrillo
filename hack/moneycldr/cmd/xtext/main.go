// Command xtext re-runs the spike's rejected alternative: it formats the
// oracle grid's rows for the 12 gallery locales with
// golang.org/x/text/currency and counts how many equal ICU.
//
//	go run ./cmd/xtext intl.tsv
//
// The int64 boundary rows are left out: x/text takes the amount as a
// float64, which cannot hold them, and counting them would blame x/text
// for the harness's choice of input rather than for its formatting.
package main

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/text/currency"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

var gallery = map[string]bool{"en": true, "ga": true, "zh-Hans": true, "es": true, "hi": true, "pt": true,
	"bn": true, "ru": true, "ja": true, "yue": true, "vi": true, "ar": true}

func main() {
	f, err := os.Open(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()
	printers := map[string]*message.Printer{}
	rows, equal, unparseable := 0, 0, map[string]bool{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		p := strings.Split(sc.Text(), "\t")
		if len(p) != 8 {
			continue // the completion line
		}
		tag, code, icu := p[1], p[2], p[7]
		minor, _ := strconv.ParseInt(p[4], 10, 64)
		if !gallery[tag] || minor == math.MaxInt64 || minor == math.MinInt64 {
			continue
		}
		u, err := currency.ParseISO(code)
		if err != nil {
			unparseable[code] = true
			continue
		}
		pr := printers[tag]
		if pr == nil {
			pr = message.NewPrinter(language.MustParse(tag))
			printers[tag] = pr
		}
		v, _ := strconv.ParseFloat(p[5], 64)
		rows++
		if pr.Sprint(currency.Symbol(u.Amount(v))) == icu {
			equal++
		}
	}
	var codes []string
	for c := range unparseable {
		codes = append(codes, c)
	}
	sort.Strings(codes)
	fmt.Printf("x/text (CLDR 32): %d of %d rows equal ICU; currency.ParseISO rejects %d storage codes: %s\n",
		equal, rows, len(codes), strings.Join(codes, " "))
}
