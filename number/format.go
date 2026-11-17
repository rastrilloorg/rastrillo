// Package number writes and reads plain numbers in a reader's locale,
// from the same CLDR packs money uses (locales/<lang>), so a number
// drawn on a page is read with the marks it was drawn with. Values are
// exact decimals, never floats: nothing is rounded.
package number

import (
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"sync"

	"amadan.net/rastrillo/rastrillo/internal/numeral"
	"amadan.net/rastrillo/rastrillo/locales"
)

// Decimal is a number written exactly in ASCII: an optional "-", the
// digits of the whole part with no leading zeros, and optionally "."
// and the fraction's digits, as in "-1200.5" or "0.25". It is its own
// type so a string that only looks like a number (a version, "1.10")
// is never written as one by accident.
type Decimal string

// Options say how Format writes a number. The zero value groups it in
// the locale's way and writes the fraction it has.
type Options struct {
	// Plain writes no group marks: 1234567,5 rather than 1.234.567,5.
	Plain bool
	// Places is the fewest fraction digits to write, padding with
	// zeros. A longer fraction is written whole, never rounded.
	Places int
	// Percent writes the value with the locale's percent sign where
	// its pattern puts it. The value is what is shown: 50 is "50 %"
	// in German, not 5000 %.
	Percent bool
}

// resolved is one locale's merged pack data with what Format and
// the readers need decoded once.
type resolved struct {
	locales.Locale
	id      string
	digits  []rune
	minimum int    // CLDR minimumGroupingDigits
	sizes   [2]int // the decimal pattern's group sizes
}

// cache holds resolved locales by linked id: packs register only at
// init, and merging copies every symbol map.
var cache sync.Map

// lookup resolves a tag to the linked locale that formats it, as
// money's does, so a number and an amount on one page agree.
func lookup(tag string) resolved {
	resolution := locales.Resolve(tag)
	warnMissing(resolution)
	id := resolution.ID
	if found, ok := cache.Load(id); ok {
		return found.(resolved)
	}
	data, _ := locales.Data(id)
	made := resolved{Locale: data, id: id, digits: []rune(data.Digits)}
	made.minimum, _ = strconv.Atoi(data.MinGrouping)
	made.sizes[0], made.sizes[1] = numeral.Grouping(data.Number)
	stored, _ := cache.LoadOrStore(id, made)
	return stored.(resolved)
}

// missingPackMsg names the tag, the pack it needs, the locale used
// instead, and the import that fixes it.
const missingPackMsg = `number: %s needs the %s pack, which is not ` +
	`linked; numbers are written in %s instead. Import _ ` +
	`"amadan.net/rastrillo/rastrillo/locales/%s"`

var warned sync.Map // pack -> struct{}

// warnMissing logs once per pack per process, as money's does: a
// number written in another locale's marks is a silent change of
// format, and can be read back a thousand times wrong.
func warnMissing(resolution locales.Resolution) {
	pack := resolution.MissingPack
	if pack == "" {
		return
	}
	if _, seen := warned.LoadOrStore(pack, struct{}{}); seen {
		return
	}
	slog.Warn(fmt.Sprintf(missingPackMsg, resolution.Wanted, pack,
		resolution.ID, pack))
}

// Format writes value in locale's conventions: its digits, decimal
// mark, group mark and grouping, minus sign and, with Percent, its
// percent pattern. A value that is not a Decimal is an error.
func Format(locale string, value Decimal, options Options) (string, error) {
	negative, whole, fraction, err := split(value)
	if err != nil {
		return "", err
	}
	data := lookup(locale)
	if pad := options.Places - len(fraction); pad > 0 {
		fraction += strings.Repeat("0", pad)
	}
	sizes := data.sizes
	pattern := data.Number
	if options.Percent {
		pattern = data.Percent
		sizes[0], sizes[1] = numeral.Grouping(pattern)
	}
	if options.Plain {
		sizes = [2]int{}
	}
	body := numeral.Group(whole, sizes, data.minimum, data.Group)
	if fraction != "" {
		body += data.Decimal + fraction
	}
	body = numeral.LocalDigits(body, data.digits)
	return data.around(pattern, body, negative), nil
}

// FormatFloat writes a float as Format writes its shortest exact
// decimal (bits is 32 for a float32's), and NaN and the infinities in
// the locale's own symbols and pattern, as ICU does: Swedish writes
// minus infinity with U+2212, Finnish NaN as "epäluku".
func FormatFloat(locale string, value float64, bits int,
	options Options) (string, error) {
	if !math.IsNaN(value) && !math.IsInf(value, 0) {
		decimal := strconv.FormatFloat(value, 'f', -1, bits)
		return Format(locale, Decimal(decimal), options)
	}
	data := lookup(locale)
	pattern := data.Number
	if options.Percent {
		pattern = data.Percent
	}
	if math.IsNaN(value) {
		return data.around(pattern, data.NaN, false), nil
	}
	return data.around(pattern, data.Infinity, math.IsInf(value, -1)), nil
}

// around puts body in the pattern's prefix and suffix, with the
// locale's minus and percent sign. A pattern with no negative part
// writes a negative number with "-" before the positive one, as CLDR
// says.
func (data resolved) around(pattern, body string, negative bool) string {
	positive, minus, hasMinus := strings.Cut(pattern, ";")
	shape := positive
	if negative {
		shape = "-" + positive
		if hasMinus {
			shape = minus
		}
	}
	start := strings.IndexAny(shape, "#0,.")
	end := strings.LastIndexAny(shape, "#0,.") + 1
	signs := strings.NewReplacer("-", data.Minus, "%", data.PercentSign)
	return signs.Replace(shape[:start]) + body + signs.Replace(shape[end:])
}

// InputValue is the text an input starts from: the locale's digits and
// decimal mark, an ASCII minus and no group marks. A group mark is what
// can make the same digits mean a thousand times more in another
// reading, so a seed never carries one. Parse reads a seed back as the
// same value; ParseNormalized refuses one like German "1,234" (1.234,
// or 1234) as ambiguous, which is why form.Number keeps an unchanged
// seed unread.
func InputValue(locale string, value Decimal) (string, error) {
	negative, whole, fraction, err := split(value)
	if err != nil {
		return "", err
	}
	data := lookup(locale)
	text := whole
	if fraction != "" {
		text += data.Decimal + fraction
	}
	text = numeral.LocalDigits(text, data.digits)
	if negative {
		text = "-" + text
	}
	return text, nil
}

// split checks value and takes it apart. A negative zero is zero.
func split(value Decimal) (negative bool, whole, fraction string,
	err error) {
	text, negative := strings.CutPrefix(string(value), "-")
	whole, fraction, dotted := strings.Cut(text, ".")
	if !digitsOnly(whole) || dotted && !digitsOnly(fraction) ||
		len(whole) > 1 && whole[0] == '0' {
		return false, "", "", fmt.Errorf(
			"number: %q is not a decimal like -1200.5", string(value))
	}
	zero := strings.Trim(whole+fraction, "0") == ""
	return negative && !zero, whole, fraction, nil
}

func digitsOnly(text string) bool {
	if text == "" {
		return false
	}
	for index := 0; index < len(text); index++ {
		if text[index] < '0' || text[index] > '9' {
			return false
		}
	}
	return true
}
