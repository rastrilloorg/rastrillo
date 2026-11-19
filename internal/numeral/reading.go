package numeral

import "strings"

// What the last token was, as Read walks them.
const (
	atStart = iota
	afterDigits
	afterGroup
	afterDecimal
)

// Read reads the tokens under one convention and grouping, or reports
// that it cannot. A group mark must follow digits and be followed by
// digits, so "12,.50" and "1,,2" have no reading; a decimal mark may
// come first (".50") or after digits, once, after every group mark.
// All group marks are one character, at the grouping's positions.
func Read(
	tokens []Token, local Convention, sizes [2]int,
) (whole, fraction string, ok bool) {
	var groups []string
	var groupMark rune
	last, sawDecimal := atStart, false
	for _, token := range tokens {
		switch {
		case token.Mark == 0:
			if sawDecimal {
				fraction = token.Digits
			} else {
				groups = append(groups, token.Digits)
			}
			last = afterDigits
		case local.Decimal[token.Mark]:
			if sawDecimal || last == afterGroup {
				return "", "", false
			}
			sawDecimal, last = true, afterDecimal
		case local.Group[token.Mark]:
			if last != afterDigits || sawDecimal ||
				groupMark != 0 && groupMark != token.Mark {
				return "", "", false
			}
			groupMark, last = token.Mark, afterGroup
		default:
			return "", "", false
		}
	}
	return joined(groups, fraction, last, sizes)
}

// joined finishes Read: the groups must end on digits and sit at the
// grouping's positions, and something must have been read.
func joined(
	groups []string, fraction string, last int, sizes [2]int,
) (string, string, bool) {
	if last == afterGroup || last == atStart {
		return "", "", false
	}
	if len(groups) > 1 && !ValidGroups(groups, sizes[0], sizes[1]) {
		return "", "", false
	}
	whole := strings.Join(groups, "")
	if whole == "" && fraction == "" {
		return "", "", false
	}
	return whole, fraction, true
}

// ValidGroups reports whether digit groups sit at a grouping's
// positions: the last primary long, those before it secondary long,
// and the first one to that size.
func ValidGroups(groups []string, primary, secondary int) bool {
	if primary == 0 {
		return false
	}
	count := len(groups)
	if len(groups[count-1]) != primary {
		return false
	}
	for index := 1; index < count-1; index++ {
		if len(groups[index]) != secondary {
			return false
		}
	}
	size := secondary
	if count == 2 {
		size = primary
	}
	return len(groups[0]) >= 1 && len(groups[0]) <= size
}
