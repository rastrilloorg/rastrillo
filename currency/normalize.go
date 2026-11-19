package currency

import (
	"errors"
	"slices"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"amadan.net/rastrillo/rastrillo/internal/numeral"
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
	body, negative, err := numeral.Strip(s,
		ownSpelling(spellingIndex(l), code, convs[0])) // step 2
	if err != nil {
		return nil, nil, false, moneyError(err)
	}
	if body == "" {
		// A sign or parentheses with no amount, such as "()": nothing to
		// read, so no decimal mark to advise on.
		return nil, nil, false, ErrInvalid
	}
	toks, err := numeral.Tokenize(body, convs[0]) // step 3
	if err != nil {
		return nil, nil, false, ErrInvalid
	}
	mf := l.forCurrency(code)
	sizes := [2]int{mf.primary, mf.secondary}
	read := func(whole, frac string) (Reading, bool) {
		return makeReading(whole, frac, negative, exponentOf(code), code)
	}
	grouped, overflowed = groupedReading(toks, convs[0], sizes, read)
	var out []Reading
	for _, conv := range convs { // step 4
		for _, gr := range numeral.Groupings(sizes[0], sizes[1]) {
			whole, frac, ok := numeral.Read(toks, conv, gr)
			if !ok {
				continue
			}
			r, ok := read(whole, frac)
			overflowed = overflowed || !ok
			if ok && !slices.ContainsFunc(out, func(o Reading) bool { return o.Decimal == r.Decimal }) {
				out = append(out, r)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return numeral.Less(out[i].Decimal, out[j].Decimal)
	})
	return out, grouped, overflowed, nil
}

// groupedReading is the reading in which the page's own locale, with
// its own grouping, takes every mark for a group mark, if there is
// one; overflowed says that reading was too large to store.
func groupedReading(
	tokens []numeral.Token, local numeral.Convention, sizes [2]int,
	read func(whole, fraction string) (Reading, bool),
) (grouped *Reading, overflowed bool) {
	if !numeral.AllGroups(tokens, local) {
		return nil, false
	}
	whole, fraction, ok := numeral.Read(tokens, local, sizes)
	if !ok {
		return nil, false
	}
	reading, ok := read(whole, fraction)
	if !ok {
		return nil, true
	}
	return &reading, false
}

// moneyError is a numeral error as money reports it.
func moneyError(err error) error {
	if errors.Is(err, numeral.ErrInvalid) {
		return ErrInvalid
	}
	return err
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
func (idx *spellings) match(
	s string, prefix bool, local numeral.Convention,
) (string, bool) {
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
		r, n := utf8.DecodeRuneInString(t)
		if n == len(t) && (local.Decimal[r] || local.Group[r]) {
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

// ownSpelling finds a currency spelling at one end of the text. The
// longest spelling at an end decides: one the field's own currency
// uses here or in root is stripped, even when other currencies share
// it ("$" in a CAD field); any other is ErrWrongCurrency ("US$" in a
// CAD field).
func ownSpelling(
	idx *spellings, code string, local numeral.Convention,
) numeral.Spelling {
	return func(text string, prefix bool) (int, error) {
		spelled, ok := idx.match(text, prefix, local)
		if !ok {
			return 0, nil
		}
		if !slices.Contains(idx.owners[spelled], code) {
			return 0, ErrWrongCurrency
		}
		return len(spelled), nil
	}
}

var conventionCache sync.Map

// conventionsFor caches conventions by locale and currency: a
// currency's override can change the marks (pt-CV's CVE).
func conventionsFor(l resolved, code string) []numeral.Convention {
	key := l.id + "\x00" + code
	if c, ok := conventionCache.Load(key); ok {
		return c.([]numeral.Convention)
	}
	c, _ := conventionCache.LoadOrStore(key, conventions(l, l.forCurrency(code)))
	return c.([]numeral.Convention)
}

// conventions are numeral's three readings for a currency in a
// locale: its own marks, point-decimal and comma-decimal.
func conventions(l resolved, mf moneyFmt) []numeral.Convention {
	return numeral.Conventions(mf.group, mf.decimal, l.Decimal)
}
