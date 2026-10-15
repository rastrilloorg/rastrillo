package currency

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"amadan.net/rastrillo/rastrillo/locales"
	"amadan.net/rastrillo/rastrillo/money"
)

// bidi are the marks a pasted, displayed amount carries.
func bidi(r rune) bool {
	return r == '\u200e' || r == '\u200f' || r == '\u061c' ||
		('‪' <= r && r <= '‮') || ('⁦' <= r && r <= '⁩')
}

// Clean removes bidi marks and folds every space separator to U+0020,
// then trims. Required checks in the new path run on this, so a value
// of only spaces or U+200F is blank.
func Clean(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case bidi(r):
		case unicode.IsSpace(r) || unicode.In(r, unicode.Zs):
			b.WriteByte(' ')
		default:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

func foldSpace(s string) string {
	if strings.TrimFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.In(r, unicode.Zs) }) == "" {
		return " "
	}
	return s
}

// Parse reads what a person typed into a minor-unit count. It accepts a
// sign; whether a negative is allowed is the field's policy, not the
// parser's, so a stored refund survives an unchanged edit.
func Parse(locale, s, code string) (int64, error) {
	code, err := Normalize(code)
	if err != nil {
		return 0, err
	}
	exp := exponentOf(code)
	l := lookup(locale)
	s = Clean(s)
	if s == "" {
		return 0, nil
	}
	negative := false
	// A sign may lead, follow a leading marker, or trail: fy writes
	// "AED 1.284,50-", so a parser that only read leading signs would
	// reject a value its own locale displays.
	sign := func() {
		if negative {
			return
		}
		if strings.HasPrefix(s, "-") || strings.HasPrefix(s, "\u2212") {
			negative = true
			_, n := utf8.DecodeRuneInString(s)
			s = strings.TrimSpace(s[n:])
		} else if strings.HasSuffix(s, "-") || strings.HasSuffix(s, "\u2212") {
			negative = true
			_, n := utf8.DecodeLastRuneInString(s)
			s = strings.TrimSpace(s[:len(s)-n])
		}
	}
	sign()
	// One marker for this currency, at either end: its symbol here
	// (cleaned the same way as the input, since some carry no-break
	// spaces) or its ISO code in any case.
	for _, m := range []string{Clean(symbol(l, code)), code} {
		if m == "" {
			continue
		}
		if hasPrefixMarker(s, m, code) {
			s = strings.TrimSpace(s[len(m):])
			sign()
			break
		}
		if hasSuffixMarker(s, m, code) {
			s = strings.TrimSpace(s[:len(s)-len(m)])
			sign()
			break
		}
	}

	mf := l.forCurrency(code)
	g := foldSpace(mf.group)
	decimals := map[string]bool{mf.decimal: true, l.Decimal: true}
	if g != "." {
		decimals["."] = true
	}
	delete(decimals, g)

	var b strings.Builder
	for _, r := range s {
		switch {
		case digitValue[r] > 0:
			b.WriteByte(byte('0' + digitValue[r] - 1))
		case r == ' ' || string(r) == g || decimals[string(r)] || r == '.' || r == ',' || r == '\'' || r == '\u2019':
			sep := string(r)
			if r == '\'' && g == "\u2019" {
				sep = g // de-CH groups with U+2019; a keyboard types U+0027
			}
			b.WriteString("\x00" + sep + "\x00")
		case unicode.IsLetter(r) || unicode.Is(unicode.Sc, r):
			return 0, ErrCurrencySign
		default:
			return 0, ErrInvalid
		}
	}
	tokens := strings.Split(b.String(), "\x00")
	sepErr := ErrInvalid
	switch mf.decimal {
	case ",":
		sepErr = ErrDecimalComma
	case ".":
		sepErr = ErrDecimalPoint
	}

	// tokens alternate digits, separator, digits, ...
	var groups []string
	whole, frac, sawDecimal := "", "", false
	for i := 0; i < len(tokens); i += 2 {
		digits := tokens[i]
		if i == 0 {
			groups = append(groups, digits)
			continue
		}
		sep := tokens[i-1]
		switch {
		case sawDecimal:
			return 0, sepErr
		case decimals[sep] && i == len(tokens)-1:
			sawDecimal, frac = true, digits
		case sep == g:
			groups = append(groups, digits)
		case sep == "." || sep == ",":
			return 0, sepErr
		default:
			return 0, ErrInvalid
		}
	}
	if len(groups) > 1 && !validGroups(groups, mf.primary, mf.secondary) {
		return 0, sepErr
	}
	whole = strings.Join(groups, "")
	if whole == "" && frac == "" {
		return 0, ErrInvalid
	}
	if len(frac) > exp {
		return 0, ErrPrecision
	}
	if whole == "" {
		whole = "0"
	}
	text := whole + "." + frac
	if negative {
		text = "-" + text
	}
	v, err := money.ParseDecimal(text, code)
	if err != nil {
		return 0, ErrInvalid
	}
	return v, nil
}

func hasPrefixMarker(s, m, code string) bool {
	if m == code {
		return len(s) >= len(m) && strings.EqualFold(s[:len(m)], m)
	}
	return strings.HasPrefix(s, m)
}

func hasSuffixMarker(s, m, code string) bool {
	if m == code {
		return len(s) >= len(m) && strings.EqualFold(s[len(s)-len(m):], m)
	}
	return strings.HasSuffix(s, m)
}

func validGroups(groups []string, primary, secondary int) bool {
	if primary == 0 {
		return false
	}
	n := len(groups)
	if len(groups[n-1]) != primary {
		return false
	}
	for i := 1; i < n-1; i++ {
		if len(groups[i]) != secondary {
			return false
		}
	}
	size := secondary
	if n == 2 {
		size = primary
	}
	return len(groups[0]) >= 1 && len(groups[0]) <= size
}

// digitValue maps every digit of every shipped numbering system, plus
// ASCII, to its value plus one (zero means "not a digit"). Restricting
// input to these is deliberate: Unicode has other decimal-digit runs
// (mathematical bold, for one) that nobody types into a price.
var digitValue = func() map[rune]int {
	m := map[rune]int{}
	for i := 0; i < 10; i++ {
		m[rune('0'+i)] = i + 1
	}
	// Every numbering system any shipped locale uses, linked or not:
	// the set of digits a person can type does not depend on which
	// packs an app links.
	for _, d := range locales.DigitSets() {
		for i, r := range []rune(d) {
			m[r] = i + 1
		}
	}
	return m
}()
