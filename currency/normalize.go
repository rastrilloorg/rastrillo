package currency

import (
	"errors"
	"math/big"
	"slices"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"amadan.net/rastrillo/rastrillo/locales"
	"amadan.net/rastrillo/rastrillo/money"
)

// ErrWrongCurrency is a spelling at either end of the input that names
// another currency and is not one of the field's own.
var ErrWrongCurrency = errors.New("money_wrong_currency")

// Reading is one interpretation of the input's digits and separators.
type Reading struct {
	// Decimal is the exact amount, normalised: no leading zeros, no
	// trailing fraction zeros ("1500", "1.5", "-0.25").
	Decimal string
	// Storable is false when a nonzero digit lies beyond the exponent:
	// storing it would round, and nothing is ever rounded.
	Storable bool
	Minor    int64 // when Storable
}

// AmbiguousError is input whose plausible readings disagree, such as
// de "1.500" for KWD (1500 or 1.5) or de "1.234" for EUR (1234, or
// 1.234, which is too precise). An over-precise reading still counts:
// that the store cannot hold it says nothing about what was meant.
type AmbiguousError struct {
	Readings []Reading // distinct, ascending, at least one storable
}

func (e *AmbiguousError) Error() string { return "money_ambiguous" }

// Choices writes the smallest and largest readings for the message.
// Each is written so that no reading of it, in any locale, is the
// other (spec §5.1): no group separators, the reader's decimal mark,
// the fraction without trailing zeros, one zero added when exactly
// three fraction digits would remain (three digits after a mark are
// what a thousands group looks like), and the ISO code.
func (e *AmbiguousError) Choices(locale, code string) (a, b string) {
	return writeChoice(locale, e.Readings[0], code), writeChoice(locale, e.Readings[len(e.Readings)-1], code)
}

func writeChoice(tag string, r Reading, code string) string {
	l := lookup(tag)
	dec, negative := strings.CutPrefix(r.Decimal, "-")
	whole, frac, _ := strings.Cut(dec, ".")
	if len(frac) == 3 {
		frac += "0"
	}
	// The reader's decimal mark, unless only that locale reads it as
	// one: pt-CV's "$" would be junk, or a currency, to anyone else.
	mark := l.forCurrency(code).decimal
	if mark != "." && mark != "," && mark != "\u066b" {
		mark = l.Decimal
	}
	text := whole
	if frac != "" {
		text += mark + frac
	}
	text = l.localDigits(text)
	if negative {
		text = "-" + text
	}
	return text + " " + code
}

// ParseNormalized is a money field's default reading (Field.Strict
// false); Parse is the strict one. It accepts input with exactly one
// plausible reading, refuses input whose readings disagree, and never
// rounds. Spec §5.1 is the contract; the steps below follow its
// numbering. Blank input is 0, nil, as in Parse: what blank means is
// the caller's decision.
func ParseNormalized(locale, s, code string) (int64, error) {
	code, err := Normalize(code)
	if err != nil {
		return 0, err
	}
	l := lookup(locale)
	mf := l.forCurrency(code)
	s = Clean(s) // step 1
	if s == "" {
		return 0, nil
	}
	rs, grouped, overflowed, err := interpret(l, s, code)
	if err != nil {
		return 0, err
	}
	storable := slices.ContainsFunc(rs, func(r Reading) bool { return r.Storable })
	switch {
	case len(rs) == 1 && storable:
		return rs[0].Minor, nil
	case len(rs) > 1 && grouped != nil && grouped.Storable:
		// Paul's decision 23: where the page's own locale reads every
		// separator as a group mark, in a valid position, that reading
		// wins (en "1,000" is 1000). The cost is accepted and stated:
		// German "1,500" meaning 1.5 KWD, pasted into an English page,
		// is 1500.
		return grouped.Minor, nil
	case len(rs) > 1 && storable:
		return 0, &AmbiguousError{Readings: rs}
	case len(rs) > 0:
		return 0, ErrPrecision // only over-precise readings
	case overflowed:
		// Every reading was too large to store. The decimal-mark advice
		// below would send the person changing marks that are right;
		// the strict parser calls this invalid too.
		return 0, ErrInvalid
	}
	switch mf.decimal {
	case ",":
		return 0, ErrDecimalComma
	case ".":
		return 0, ErrDecimalPoint
	}
	return 0, ErrInvalid
}

// interpret runs steps 2 to 4 on cleaned, non-blank input: every
// distinct reading, ascending, or an error found before reading. grouped
// is the reading in which the page's locale, with its own grouping,
// takes every separator for a group mark, if there is one. overflowed
// reports a reading dropped because it does not fit in int64.
func interpret(l resolved, s, code string) (rs []Reading, grouped *Reading, overflowed bool, err error) {
	convs := conventionsFor(l, code)
	body, negative, err := strip(s, spellingIndex(l), code, convs[0]) // step 2
	if err != nil {
		return nil, nil, false, err
	}
	if body == "" {
		// A sign or parentheses with no amount, such as "()": nothing to
		// read, so no decimal mark to advise on.
		return nil, nil, false, ErrInvalid
	}
	toks, err := tokenize(body, convs[0]) // step 3
	if err != nil {
		return nil, nil, false, err
	}
	exp := exponentOf(code)
	mf := l.forCurrency(code)
	var out []Reading
	if allGroups(toks, convs[0]) {
		if whole, frac, ok := reading(toks, convs[0], [2]int{mf.primary, mf.secondary}); ok {
			if r, ok := makeReading(whole, frac, negative, exp, code); ok {
				grouped = &r
			} else {
				overflowed = true
			}
		}
	}
	for _, conv := range convs { // step 4
		for _, gr := range groupings(mf) {
			whole, frac, ok := reading(toks, conv, gr)
			if !ok {
				continue
			}
			r, ok := makeReading(whole, frac, negative, exp, code)
			overflowed = overflowed || !ok
			if ok && !slices.ContainsFunc(out, func(o Reading) bool { return o.Decimal == r.Decimal }) {
				out = append(out, r)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return decimalLess(out[i].Decimal, out[j].Decimal) })
	return out, grouped, overflowed, nil
}

// allGroups reports whether the input has separators and the locale's
// own convention takes every one of them for a group mark.
func allGroups(toks []tok, local convention) bool {
	seps := 0
	for _, t := range toks {
		if t.sep != 0 {
			if !local.group[t.sep] {
				return false
			}
			seps++
		}
	}
	return seps > 0
}

// groupings are the locale's own and the two people type anywhere:
// thousands (3 and 3) and Indian (3, then 2). Equal values from
// different groupings are one reading.
func groupings(mf moneyFmt) [][2]int {
	out := [][2]int{{mf.primary, mf.secondary}}
	for _, g := range [][2]int{{3, 3}, {3, 2}} {
		if !slices.Contains(out, g) {
			out = append(out, g)
		}
	}
	return out
}

func makeReading(whole, frac string, negative bool, exp int, code string) (Reading, bool) {
	whole = strings.TrimLeft(whole, "0")
	if whole == "" {
		whole = "0"
	}
	frac = strings.TrimRight(frac, "0")
	dec := whole
	if frac != "" {
		dec += "." + frac
	}
	if negative && dec != "0" {
		dec = "-" + dec
	}
	r := Reading{Decimal: dec, Storable: len(frac) <= exp}
	if r.Storable {
		v, err := money.ParseDecimal(dec, code)
		if err != nil {
			return Reading{}, false // out of range: no reading at all
		}
		r.Minor = v
	}
	return r, true
}

func decimalLess(a, b string) bool {
	x, _ := new(big.Rat).SetString(a)
	y, _ := new(big.Rat).SetString(b)
	return x.Cmp(y) < 0
}

// spellings maps every currency spelling known in a locale (each code,
// and every symbol and narrow symbol in the locale and in root) to the
// currencies that use it, longest spelling first, so "US$" is matched
// before "$" could leave a stray "US".
type spellings struct {
	owners map[string][]string
	// byEdge lists the spellings by their first and by their last rune
	// (upper-cased, since codes match in any case), longest first, so a
	// match looks at a handful of candidates rather than every spelling.
	byFirst, byLast map[rune][]string
}

var spellingCache sync.Map

func spellingIndex(l resolved) *spellings {
	if s, ok := spellingCache.Load(l.id); ok {
		return s.(*spellings)
	}
	root := rootLocale()
	idx := &spellings{owners: map[string][]string{}, byFirst: map[rune][]string{}, byLast: map[rune][]string{}}
	add := func(v, code string) {
		if v = Clean(v); v != "" && !slices.Contains(idx.owners[v], code) {
			idx.owners[v] = append(idx.owners[v], code)
		}
	}
	for _, code := range locales.StorageCodes() {
		add(code, code)
		for _, m := range []map[string]string{l.Symbols, l.Narrow, root.Symbols, root.Narrow} {
			add(m[code], code)
		}
	}
	for v := range idx.owners {
		f, _ := utf8.DecodeRuneInString(v)
		l, _ := utf8.DecodeLastRuneInString(v)
		f, l = unicode.ToUpper(f), unicode.ToUpper(l)
		idx.byFirst[f] = append(idx.byFirst[f], v)
		idx.byLast[l] = append(idx.byLast[l], v)
	}
	for _, m := range []map[rune][]string{idx.byFirst, idx.byLast} {
		for _, list := range m {
			sort.Slice(list, func(i, j int) bool {
				a, b := list[i], list[j]
				return len(a) > len(b) || len(a) == len(b) && a < b
			})
		}
	}
	actual, _ := spellingCache.LoadOrStore(l.id, idx)
	return actual.(*spellings)
}

// rootLocale is resolved once: every field reads root's spellings.
var rootLocale = sync.OnceValue(func() locales.Locale {
	l, _ := locales.Data("root")
	return l
})

// match finds the longest known spelling at one end of s. A code
// matches in any case; a symbol only exactly. A spelling that ends in a
// letter on its inner side matches only where a non-letter follows, so
// "5 dollars" is not "5 doll" plus ARS. A spelling that is one of the
// locale's own separator marks is a separator there, not a currency
// (pt-CV's "$" decimal).
func (idx *spellings) match(s string, prefix bool, local convention) (string, bool) {
	edge, _ := utf8.DecodeLastRuneInString(s)
	candidates := idx.byLast
	if prefix {
		edge, _ = utf8.DecodeRuneInString(s)
		candidates = idx.byFirst
	}
	for _, t := range candidates[unicode.ToUpper(edge)] {
		if len(s) < len(t) {
			continue
		}
		if r, n := utf8.DecodeRuneInString(t); n == len(t) && (local.decimal[r] || local.group[r]) {
			continue
		}
		part, rest := s[len(s)-len(t):], s[:len(s)-len(t)]
		inner, _ := utf8.DecodeRuneInString(t)
		next, _ := utf8.DecodeLastRuneInString(rest)
		if prefix {
			part, rest = s[:len(t)], s[len(t):]
			inner, _ = utf8.DecodeLastRuneInString(t)
			next, _ = utf8.DecodeRuneInString(rest)
		}
		if part != t && !(len(t) == 3 && isAlpha(t) && strings.ToUpper(t) == t && strings.EqualFold(part, t)) {
			continue
		}
		if rest != "" && unicode.IsLetter(inner) && unicode.IsLetter(next) {
			continue
		}
		return t, true
	}
	return "", false
}

// strip removes, in any order, at most one sign or one pair of
// parentheses, and at most one spelling of the field's own currency at
// either end. The longest spelling at an end decides: one the field's
// own currency uses here or in root is stripped, even when other
// currencies share it ("$" in a CAD field); any other is
// ErrWrongCurrency ("US$" in a CAD field). A second sign, a sign with
// parentheses, or a second own spelling is ErrInvalid.
func strip(s string, idx *spellings, code string, local convention) (body string, negative bool, err error) {
	signed, spelled := false, false
	if strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")") {
		s, negative, signed = s[1:len(s)-1], true, true
	}
	for changed := true; changed; {
		changed = false
		s = strings.TrimSpace(s)
		for _, sign := range []string{"-", "\u2212", "+"} {
			lead := strings.HasPrefix(s, sign)
			if !lead && (sign == "+" || !strings.HasSuffix(s, sign)) {
				continue
			}
			if signed {
				return "", false, ErrInvalid
			}
			signed, negative, changed = true, sign != "+", true
			if lead {
				s = s[len(sign):]
			} else {
				s = s[:len(s)-len(sign)]
			}
			break
		}
		for _, prefix := range []bool{true, false} {
			t, ok := idx.match(s, prefix, local)
			if !ok {
				continue
			}
			if !slices.Contains(idx.owners[t], code) {
				return "", false, ErrWrongCurrency
			}
			if spelled {
				return "", false, ErrInvalid
			}
			if prefix {
				s = s[len(t):]
			} else {
				s = s[:len(s)-len(t)]
			}
			spelled, changed = true, true
			break
		}
	}
	return strings.TrimSpace(s), negative, nil
}

type tok struct {
	digits string // ASCII digits; "" for a separator
	sep    rune
}

// tokenize splits the body into digit runs and single separators and
// refuses anything else as junk. The locale's own marks are separators
// too, so pt-CV's "$" decimal (1284$50) is not junk.
func tokenize(s string, local convention) ([]tok, error) {
	var out []tok
	var run strings.Builder
	flush := func() {
		if run.Len() > 0 {
			out = append(out, tok{digits: run.String()})
			run.Reset()
		}
	}
	for _, r := range s {
		switch {
		case digitValue[r] > 0:
			run.WriteByte(byte('0' + digitValue[r] - 1))
		case strings.ContainsRune(".,' \u2019\u066b\u066c", r) || local.decimal[r] || local.group[r]:
			flush()
			out = append(out, tok{sep: r})
		default:
			return nil, ErrInvalid
		}
	}
	flush()
	return out, nil
}

var conventionCache sync.Map

// conventionsFor caches conventions by locale and currency: a
// currency's override can change the marks (pt-CV's CVE).
func conventionsFor(l resolved, code string) []convention {
	key := l.id + "\x00" + code
	if c, ok := conventionCache.Load(key); ok {
		return c.([]convention)
	}
	c, _ := conventionCache.LoadOrStore(key, conventions(l, l.forCurrency(code)))
	return c.([]convention)
}

// convention says which characters mark decimals and groups.
type convention struct{ decimal, group map[rune]bool }

// conventions are the readings of §5.1's decision table: the locale's
// own (exactly the strict parser's), point-decimal and comma-decimal. In
// all of them a space, an apostrophe and the Arabic thousands mark
// group, and the Arabic decimal mark is a decimal.
func conventions(l resolved, mf moneyFmt) []convention {
	base := func(dec ...rune) convention {
		c := convention{decimal: map[rune]bool{'\u066b': true}, group: map[rune]bool{' ': true, '\'': true, '\u2019': true, '\u066c': true}}
		for _, d := range dec {
			c.decimal[d] = true
		}
		return c
	}
	g, _ := utf8.DecodeRuneInString(foldSpace(mf.group))
	d, _ := utf8.DecodeRuneInString(mf.decimal)
	od, _ := utf8.DecodeRuneInString(l.Decimal)
	local := base(d, od)
	if g != '.' {
		local.decimal['.'] = true
	}
	delete(local.decimal, g)
	local.group[g] = true
	point := base('.')
	point.group[','] = true
	comma := base(',')
	comma.group['.'] = true
	return []convention{local, point, comma}
}

// reading reads the tokens under one convention and grouping, or
// reports that it cannot. A group mark must follow digits and be
// followed by digits, so "12,.50" and "1,,2" have no reading; a decimal
// mark may come first (".50") or after digits, once, after every group
// mark. All group marks are one character, at the grouping's positions.
func reading(toks []tok, c convention, gr [2]int) (whole, frac string, ok bool) {
	var groups []string
	var groupMark rune
	const start, digits, group, decimal = 0, 1, 2, 3
	prev, sawDecimal := start, false
	for _, t := range toks {
		switch {
		case t.sep == 0:
			if sawDecimal {
				frac = t.digits
			} else {
				groups = append(groups, t.digits)
			}
			prev = digits
		case c.decimal[t.sep]:
			if sawDecimal || prev == group {
				return "", "", false
			}
			sawDecimal, prev = true, decimal
		case c.group[t.sep]:
			if prev != digits || sawDecimal || groupMark != 0 && groupMark != t.sep {
				return "", "", false
			}
			groupMark, prev = t.sep, group
		default:
			return "", "", false
		}
	}
	if prev == group || prev == start {
		return "", "", false
	}
	if len(groups) > 1 && !validGroups(groups, gr[0], gr[1]) {
		return "", "", false
	}
	whole = strings.Join(groups, "")
	if whole == "" && frac == "" {
		return "", "", false
	}
	return whole, frac, true
}
