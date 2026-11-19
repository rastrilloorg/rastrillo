package numeral

import "strings"

// Spelling finds a spelling, such as a currency's, at the start
// (prefix) or the end of text. It returns the spelling's length in
// bytes when the text's own one is there, 0 when none is, and an error
// for one that is refused (another currency's).
type Spelling func(text string, prefix bool) (int, error)

// Strip removes, in any order, at most one sign or one pair of
// parentheses around the whole, and at most one spelling at either
// end. A sign is a leading "+", "-" or U+2212, or a trailing "-" or
// U+2212. A second sign, a sign with parentheses, or a second spelling
// is ErrInvalid. spelling may be nil: plain numbers have none.
func Strip(
	text string, spelling Spelling,
) (body string, negative bool, err error) {
	signed, spelled := false, false
	if strings.HasPrefix(text, "(") && strings.HasSuffix(text, ")") {
		text, negative, signed = text[1:len(text)-1], true, true
	}
	for changed := true; changed; {
		changed = false
		text = strings.TrimSpace(text)
		var minus bool
		text, minus, changed = cutSign(text)
		if changed {
			if signed {
				return "", false, ErrInvalid
			}
			signed, negative = true, minus
		}
		if spelling == nil {
			continue
		}
		cut, err := cutSpelling(text, spelling)
		if err != nil || cut != text && spelled {
			return "", false, firstOf(err, ErrInvalid)
		}
		if cut != text {
			text, spelled, changed = cut, true, true
		}
	}
	return strings.TrimSpace(text), negative, nil
}

// cutSign removes one sign, saying whether it was a minus.
func cutSign(text string) (rest string, minus, cut bool) {
	for _, sign := range []string{"-", "\u2212", "+"} {
		if strings.HasPrefix(text, sign) {
			return text[len(sign):], sign != "+", true
		}
		if sign != "+" && strings.HasSuffix(text, sign) {
			return text[:len(text)-len(sign)], true, true
		}
	}
	return text, false, false
}

// cutSpelling removes the spelling at the start, else the end.
func cutSpelling(text string, spelling Spelling) (string, error) {
	for _, prefix := range []bool{true, false} {
		width, err := spelling(text, prefix)
		switch {
		case err != nil:
			return text, err
		case width > 0 && prefix:
			return text[width:], nil
		case width > 0:
			return text[:len(text)-width], nil
		}
	}
	return text, nil
}

func firstOf(err, otherwise error) error {
	if err != nil {
		return err
	}
	return otherwise
}
