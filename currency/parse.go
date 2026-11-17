package currency

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"amadan.net/rastrillo/rastrillo/internal/numeral"
	"amadan.net/rastrillo/rastrillo/money"
)

// Clean removes bidi marks and folds every space separator to U+0020,
// then trims. Required checks in the new path run on this, so a value
// of only spaces or U+200F is blank.
func Clean(s string) string { return numeral.Clean(s) }

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
	g := numeral.FoldSpace(mf.group)
	decimals := map[string]bool{mf.decimal: true, l.Decimal: true}
	if g != "." {
		decimals["."] = true
	}
	delete(decimals, g)

	var b strings.Builder
	for _, r := range s {
		switch {
		case isDigit(r):
			b.WriteByte(asciiDigit(r))
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

func isDigit(char rune) bool {
	_, ok := numeral.Digit(char)
	return ok
}

// validGroups is numeral's, for the strict reading above.
func validGroups(groups []string, primary, secondary int) bool {
	return numeral.ValidGroups(groups, primary, secondary)
}

// asciiDigit is a digit of any shipped numbering system in ASCII.
func asciiDigit(char rune) byte {
	value, _ := numeral.Digit(char)
	return byte('0' + value)
}
