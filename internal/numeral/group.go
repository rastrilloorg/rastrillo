package numeral

import "strings"

// Grouping reads the primary and secondary group sizes from a CLDR
// pattern's integer part: "#,##,##0.00" is 3 then 2 (lakh). 0, 0 for
// a pattern that does not group.
func Grouping(pattern string) (primary, secondary int) {
	positive, _, _ := strings.Cut(pattern, ";")
	start := strings.IndexAny(positive, "#0,.")
	end := strings.LastIndexAny(positive, "#0,.") + 1
	integer, _, _ := strings.Cut(positive[start:end], ".")
	groups := strings.Split(integer, ",")
	if len(groups) < 2 {
		return 0, 0
	}
	primary = len(groups[len(groups)-1])
	secondary = primary
	if len(groups) > 2 {
		secondary = len(groups[len(groups)-2])
	}
	return primary, secondary
}

// Group writes whole, ASCII digits, with mark between its groups. It
// leaves whole alone when the pattern does not group or whole is
// shorter than the first group plus CLDR's minimum grouping digits:
// es writes 1234 but 12.345.
func Group(whole string, sizes [2]int, minimum int, mark string) string {
	primary, secondary := sizes[0], sizes[1]
	if primary == 0 || len(whole) < primary+minimum {
		return whole
	}
	parts := []string{whole[len(whole)-primary:]}
	whole = whole[:len(whole)-primary]
	for len(whole) > secondary {
		parts = append([]string{whole[len(whole)-secondary:]}, parts...)
		whole = whole[:len(whole)-secondary]
	}
	if whole != "" {
		parts = append([]string{whole}, parts...)
	}
	return strings.Join(parts, mark)
}
