package number

import (
	"errors"
	"slices"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"amadan.net/rastrillo/rastrillo/internal/numeral"
)

// MaxDigits is the most digits a number read from text may have, whole
// and fraction together, leading and trailing zeros included, so a
// pasted page of digits is refused rather than compared and stored.
const MaxDigits = 30

// Errors a reading returns. Each is the suffix of the catalog key form
// reports (rastrillo.ui.number_invalid and so on).
var (
	// ErrInvalid is text with no reading: letters, an exponent, two
	// decimal marks, a misplaced group mark, or, read strictly, any
	// mark that is not the locale's own.
	ErrInvalid = errors.New("number_invalid")
	// ErrTooLong is a number with more than MaxDigits digits.
	ErrTooLong = errors.New("number_too_long")
)

// AmbiguousError is text whose plausible readings disagree, such as
// "1.500" in English (1.5, or 1500 grouped the German way). Nothing is
// guessed: the person is asked which one they mean.
type AmbiguousError struct {
	Readings []Decimal // distinct, ascending
}

func (ambiguity *AmbiguousError) Error() string { return "number_ambiguous" }

// Choices writes the smallest and largest readings for the message,
// each so that no reading of it, in any locale, is the other: the
// reader's digits and decimal mark, no group marks, and a fourth
// fraction digit where exactly three would remain ("1.5" and "1500";
// "1,2340" and "1234").
func (ambiguity *AmbiguousError) Choices(locale string) (
	smallest, largest string) {
	readings := ambiguity.Readings
	return writeChoice(locale, readings[0]),
		writeChoice(locale, readings[len(readings)-1])
}

func writeChoice(locale string, reading Decimal) string {
	text, negative := strings.CutPrefix(string(reading), "-")
	whole, fraction, _ := strings.Cut(text, ".")
	if len(fraction) == 3 {
		fraction += "0"
	}
	if fraction != "" {
		whole += "." + fraction
	}
	written, _ := InputValue(locale, Decimal(whole))
	if negative {
		written = "-" + written
	}
	return written
}

// ParseNormalized reads what a person typed as a number, the way money
// reads an amount (the multi-currency money spec, §5.1, with no
// currency and no exponent): any text with one plausible reading is
// accepted, in the locale's marks or the other common ones; where the
// page's locale takes every mark for a group mark, that reading wins
// ("1,500" is 1500 in English); text whose readings still disagree is
// an *AmbiguousError. Signs are a leading "+", "-" or U+2212, a
// trailing "-" or U+2212, or parentheses. Blank text is "" and no
// error: what blank means is the caller's decision.
func ParseNormalized(locale, text string) (Decimal, error) {
	text = numeral.Clean(text)
	if text == "" {
		return "", nil
	}
	data := lookup(locale)
	conventions := conventionsOf(data)
	negative, tokens, err := signedTokens(text, conventions[0])
	if err != nil {
		return "", err
	}
	read := func(local numeral.Convention, sizes [2]int) (Decimal, bool) {
		whole, fraction, ok := numeral.Read(tokens, local, sizes)
		return canonical(whole, fraction, negative), ok
	}
	var readings []Decimal
	for _, local := range conventions {
		for _, sizes := range numeral.Groupings(data.sizes[0],
			data.sizes[1]) {
			if found, ok := read(local, sizes); ok &&
				!slices.Contains(readings, found) {
				readings = append(readings, found)
			}
		}
	}
	grouped, groupedOK := read(conventions[0], data.sizes)
	groupedOK = groupedOK && numeral.AllGroups(tokens, conventions[0])
	return choose(readings, grouped, groupedOK)
}

// choose is ParseNormalized's verdict on its readings, in money's
// order (form's money field decides the same way).
func choose(
	readings []Decimal, grouped Decimal, groupedOK bool,
) (Decimal, error) {
	sort.Slice(readings, func(left, right int) bool {
		return numeral.Less(string(readings[left]),
			string(readings[right]))
	})
	fits := slices.ContainsFunc(readings, short)
	switch {
	case len(readings) == 1 && fits:
		return readings[0], nil
	case len(readings) > 1 && groupedOK && short(grouped):
		// Paul's decision 23, as for money: the page's own group mark
		// wins. German "1,500" meaning 1.5, pasted into an English
		// page, is 1500; that cost is accepted and stated.
		return grouped, nil
	case len(readings) > 1 && fits:
		return "", &AmbiguousError{Readings: readings}
	case len(readings) > 0:
		return "", ErrTooLong
	}
	return "", ErrInvalid
}

// Parse is the strict reading: the locale's own decimal mark, and its
// own group mark only at its own grouping's positions, as a Strict
// money field reads. One convention means one reading, so it is never
// ambiguous. A point is a decimal mark wherever it is not the group
// mark, U+0027 and U+2019 are one mark, and the only sign is a leading
// or trailing minus. Blank text is "" and no
// error.
func Parse(locale, text string) (Decimal, error) {
	text = numeral.Clean(text)
	if text == "" {
		return "", nil
	}
	if strings.HasPrefix(text, "+") || strings.HasPrefix(text, "(") {
		// Strict money reads a minus only; so does a strict number.
		return "", ErrInvalid
	}
	data := lookup(locale)
	local := strictConvention(data)
	negative, tokens, err := signedTokens(text, local)
	if err != nil {
		return "", err
	}
	if local.Group['\''] {
		// One mark typed two ways: de-CH's U+0027 and a phone's U+2019
		// may meet in one number, which Read would take for two marks.
		for index := range tokens {
			if tokens[index].Mark == '\u2019' {
				tokens[index].Mark = '\''
			}
		}
	}
	whole, fraction, ok := numeral.Read(tokens, local, data.sizes)
	if !ok {
		return "", ErrInvalid
	}
	found := canonical(whole, fraction, negative)
	if !short(found) {
		return "", ErrTooLong
	}
	return found, nil
}

// signedTokens strips the sign from cleaned text and splits the rest.
func signedTokens(
	text string, local numeral.Convention,
) (bool, []numeral.Token, error) {
	body, negative, err := numeral.Strip(text, nil)
	if err != nil || body == "" {
		return false, nil, ErrInvalid
	}
	split, err := numeral.Tokenize(body, local)
	if err != nil {
		return false, nil, ErrInvalid
	}
	// Counted as typed, before zeros are trimmed: a hundred thousand
	// leading zeros are not a short number, and must not be read as one.
	digits := 0
	for _, token := range split {
		digits += len(token.Digits)
	}
	if digits > MaxDigits {
		return false, nil, ErrTooLong
	}
	return negative, split, nil
}

// canonical is a reading as a Decimal: no leading zeros, no trailing
// fraction zeros, and no sign on zero.
func canonical(whole, fraction string, negative bool) Decimal {
	whole = strings.TrimLeft(whole, "0")
	if whole == "" {
		whole = "0"
	}
	text := whole
	if fraction = strings.TrimRight(fraction, "0"); fraction != "" {
		text += "." + fraction
	}
	if negative && text != "0" {
		text = "-" + text
	}
	return Decimal(text)
}

// short reports a reading written in at most MaxDigits digits: the
// whole part's, unless it is a lone zero, and every fraction digit.
func short(reading Decimal) bool {
	text := strings.TrimPrefix(string(reading), "-")
	whole, fraction, _ := strings.Cut(text, ".")
	if whole == "0" {
		whole = ""
	}
	return len(whole)+len(fraction) <= MaxDigits
}

var conventionCache sync.Map

// conventionsOf are numeral's three readings with the locale's plain
// marks.
func conventionsOf(data resolved) []numeral.Convention {
	if found, ok := conventionCache.Load(data.id); ok {
		return found.([]numeral.Convention)
	}
	made := numeral.Conventions(data.Group, data.Decimal, data.Decimal)
	stored, _ := conventionCache.LoadOrStore(data.id, made)
	return stored.([]numeral.Convention)
}

// strictConvention is the locale's own marks and no others.
func strictConvention(data resolved) numeral.Convention {
	group, _ := utf8.DecodeRuneInString(numeral.FoldSpace(data.Group))
	decimal, _ := utf8.DecodeRuneInString(data.Decimal)
	local := numeral.Convention{Decimal: map[rune]bool{decimal: true},
		Group: map[rune]bool{group: true}}
	if group != '.' {
		local.Decimal['.'] = true
	}
	if group == '\'' || group == '\u2019' {
		local.Group['\''], local.Group['\u2019'] = true, true
	}
	delete(local.Decimal, group)
	return local
}
