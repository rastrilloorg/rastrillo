package currency

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"amadan.net/rastrillo/rastrillo/locales"
)

// missingPackMsg is Appendix C.2's op.currency.missing_pack, verbatim:
// the tag asked for, the missing pack, the locale used instead, and the
// pack again for the import line.
const missingPackMsg = `currency: %s needs the %s pack, which is not linked; amounts are formatted in %s instead. Import _ "amadan.net/rastrillo/rastrillo/locales/%s"`

var warned sync.Map // pack -> struct{}

// warnMissing logs one warning per missing pack per process. A stored
// preference or explicit app code can still ask for a language whose
// pack the binary lacks; the currency never changes, only the
// conventions fall back, and the operator needs to hear about it once,
// not on every request.
func warnMissing(r locales.Resolution) {
	if r.MissingPack == "" {
		return
	}
	if _, seen := warned.LoadOrStore(r.MissingPack, struct{}{}); seen {
		return
	}
	slog.Warn(fmt.Sprintf(missingPackMsg, r.Wanted, r.MissingPack, r.ID, r.MissingPack))
}

type resolved struct {
	locales.Locale
	id       string
	digits   []rune
	minGroup int
}

func lookup(tag string) resolved {
	res := locales.Resolve(tag)
	warnMissing(res)
	if r, ok := resolvedCache.Load(res.ID); ok {
		return r.(resolved)
	}
	l, _ := locales.Data(res.ID)
	r := resolved{Locale: l, id: res.ID, digits: []rune(l.Digits)}
	r.minGroup, _ = strconv.Atoi(l.MinGrouping)
	actual, _ := resolvedCache.LoadOrStore(res.ID, r)
	return actual.(resolved)
}

// resolvedCache holds merged locales by resolved id. Merging copies
// every symbol map, which made each parse cost a few hundred map
// writes; the id already reflects the app default and the linked packs,
// and packs register only at init.
var resolvedCache sync.Map

// moneyFmt is the locale's data for one currency, with CLDR's
// per-currency overrides applied.
type moneyFmt struct {
	standard, alpha    string
	decimal, group     string
	primary, secondary int
}

func (l resolved) forCurrency(code string) moneyFmt {
	m := moneyFmt{standard: l.Standard, alpha: l.Alpha, decimal: l.CurrencyDecimal, group: l.CurrencyGroup}
	if o, ok := l.Overrides[code]; ok {
		if o.Pattern != "" {
			m.standard, m.alpha = o.Pattern, o.Pattern
		}
		if o.Decimal != "" {
			m.decimal = o.Decimal
		}
		if o.Group != "" {
			m.group = o.Group
		}
	}
	m.primary, m.secondary = grouping(m.standard)
	return m
}

// pattern chooses between the standard and alphaNextToNumber patterns
// by the symbol's rune that touches the number in the standard one.
// Decision 6 follows CLDR, not ICU: when that rune is a letter, the
// alpha pattern is used even where it moves the symbol to the other
// side of the number, which ICU 78 never does. No modern locale's
// pattern moves it today; make money-cldr fails if one starts to.
func (m moneyFmt) pattern(sym string) string {
	pos, _, _ := strings.Cut(m.standard, ";")
	touch, _ := utf8.DecodeRuneInString(sym)
	if strings.Index(pos, "¤") < strings.IndexAny(pos, "#0") {
		touch, _ = utf8.DecodeLastRuneInString(sym)
	}
	if unicode.IsLetter(touch) {
		return m.alpha
	}
	return m.standard
}

// grouping reads the primary and secondary group sizes from a pattern's
// integer part: "#,##,##0.00" is 3 then 2 (lakh).
func grouping(pattern string) (int, int) {
	pos, _, _ := strings.Cut(pattern, ";")
	start := strings.IndexAny(pos, "#0,.")
	end := strings.LastIndexAny(pos, "#0,.") + 1
	intPart, _, _ := strings.Cut(pos[start:end], ".")
	groups := strings.Split(intPart, ",")
	if len(groups) < 2 {
		return 0, 0
	}
	primary := len(groups[len(groups)-1])
	secondary := primary
	if len(groups) > 2 {
		secondary = len(groups[len(groups)-2])
	}
	return primary, secondary
}

func symbol(l resolved, code string) string {
	if s, ok := l.Symbols[code]; ok {
		return s
	}
	return code
}

// magnitude is money.Decimal's trick: converting to uint64 before
// negating gives the right magnitude for MinInt64, where -minor would
// overflow back to itself.
func magnitude(minor int64) uint64 {
	m := uint64(minor)
	if minor < 0 {
		m = -m
	}
	return m
}

func splitDigits(minor int64, exp int) (string, string) {
	s := strconv.FormatUint(magnitude(minor), 10)
	if len(s) <= exp {
		s = strings.Repeat("0", exp+1-len(s)) + s
	}
	return s[:len(s)-exp], s[len(s)-exp:]
}

func (l resolved) localDigits(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return l.digits[r-'0']
		}
		return r
	}, s)
}

func isSZ(r rune) bool { return unicode.IsSymbol(r) || unicode.In(r, unicode.Z) }

// Format writes minor units of code in locale's conventions, from CLDR
// data. An unsupported code is an error, never an invented scale.
func Format(locale string, minor int64, code string) (string, error) {
	code, err := Normalize(code)
	if err != nil {
		return "", err
	}
	exp := exponentOf(code)
	l := lookup(locale)
	mf := l.forCurrency(code)
	sym := symbol(l, code)
	first, _ := utf8.DecodeRuneInString(sym)
	last, _ := utf8.DecodeLastRuneInString(sym)

	pos, neg, hasNeg := strings.Cut(mf.pattern(sym), ";")
	sub := pos
	if minor < 0 {
		if hasNeg {
			sub = neg
		} else {
			sub = "-" + pos
		}
	}
	start := strings.IndexAny(sub, "#0,.")
	end := strings.LastIndexAny(sub, "#0,.") + 1
	prefix, suffix := sub[:start], sub[end:]

	whole, frac := splitDigits(minor, exp)
	body := group(whole, mf.primary, mf.secondary, l.minGroup, mf.group)
	if exp > 0 {
		body += mf.decimal + frac
	}
	body = l.localDigits(body)
	if strings.HasSuffix(prefix, "¤") && !isSZ(last) {
		prefix += l.Spacing
	}
	if strings.HasPrefix(suffix, "¤") && !isSZ(first) {
		suffix = l.Spacing + suffix
	}
	repl := strings.NewReplacer("¤", sym, "-", l.Minus)
	return repl.Replace(prefix) + body + repl.Replace(suffix), nil
}

func group(whole string, primary, secondary, minGroup int, sep string) string {
	if primary == 0 || len(whole) < primary+minGroup {
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
	return strings.Join(parts, sep)
}

// InputValue is the seed for an input: the locale's digits and currency
// decimal mark, an ASCII minus, no grouping and no symbol. Grouping is
// left out because a seed is read back by the parser, and a group mark
// is the one thing that can make the same digits mean a thousand times
// more in another reading.
func InputValue(locale string, minor int64, code string) (string, error) {
	c, err := Normalize(code)
	if err != nil {
		return "", err
	}
	exp := exponentOf(c)
	l := lookup(locale)
	whole, frac := splitDigits(minor, exp)
	s := whole
	if exp > 0 {
		s += l.forCurrency(c).decimal + frac
	}
	s = l.localDigits(s)
	if minor < 0 {
		s = "-" + s
	}
	return s, nil
}
