package locales

import "sort"

// Locale is one locale's money-formatting data. Packs store each locale
// as what differs from its Parent, with "" meaning "inherit"; Data
// merges the chain. Every resolved field is non-empty: the generator
// folds CLDR's optional currencyDecimal, currencyGroup and
// alphaNextToNumber into their ordinary counterparts, so "" can never
// mean "absent" and be mistaken for "inherit".
//
// The fields are grouped by what reads them, so a later pack can carry
// date data beside money without a second registry: Decimal, Group,
// Minus, Digits, MinGrouping, Number, Percent and PercentSign are what
// {{number}} reads, and money shares the first four.
type Locale struct {
	Parent string // a locale id in the same pack, or "root"
	// CurrencyDecimal and CurrencyGroup are the marks money uses. They
	// differ from the ordinary marks in a few locales: fr-CH writes
	// plain numbers with "," and money with ".".
	CurrencyDecimal string
	CurrencyGroup   string
	// Decimal is the ordinary decimal mark, which the parser also reads
	// as a decimal mark when it is not the group mark.
	Decimal string
	// Group is the ordinary group mark, which plain numbers use. It is
	// not always CurrencyGroup: de-AT groups numbers with a no-break
	// space and money with ".".
	Group       string
	Minus       string
	Digits      string // the default numbering system's ten digits
	MinGrouping string // CLDR minimumGroupingDigits, "1" to "3"
	// Number and Percent are CLDR's standard decimal and percent
	// patterns ("#,##,##0.###" in hi), read for their group sizes and
	// for where the percent sign goes; PercentSign is the sign itself.
	Number      string
	Percent     string
	PercentSign string
	// Infinity and NaN are what a float that is no number is written
	// as: "∞", and "NaN" or Finnish "epäluku".
	Infinity string
	NaN      string
	Standard string // currency pattern
	Alpha    string // pattern when the symbol's touching rune is a letter
	Spacing  string // currency-spacing insert
	Symbols  map[string]string
	// Narrow are CLDR's narrow symbols ("$" for USD where the standard
	// symbol is "US$"). Formatting never uses them; the normalising
	// parser accepts them as spellings of the field's own currency.
	Narrow map[string]string
	// Overrides are CLDR's per-currency pattern and marks, which apply
	// only when formatting that currency: en-150 puts the euro sign
	// before the number and every other symbol after it, and pt-PT
	// writes escudos as 1284$50. A zero Override in a child cancels its
	// parent's.
	Overrides map[string]Override
}

// Override is one currency's exceptions in one locale; "" means none.
type Override struct {
	Pattern, Decimal, Group string
}

// Currency is one code the storage set accepts.
type Currency struct {
	// Exponent is the frozen number of minor-unit digits. It is
	// money.MinorUnits(code), verified against ISO 4217
	// (hack/moneycldr/testdata/storage.tsv says which edition); a test
	// holds the two together.
	Exponent int
	// Eligible says the code may be the default for new records: ISO
	// lists it today and CLDR lists it as legal tender somewhere with no
	// end date.
	Eligible bool
}

var (
	linked = map[string]Locale{} // locale id -> diff record, from linked packs
	packs  = map[string]bool{}
)

// Register adds a pack's locales. Packs call it from init; a pack
// registered twice, or a locale claimed by two packs, is a generator bug.
func Register(pack string, locs map[string]Locale) {
	if packs[pack] {
		panic("locales: pack " + pack + " registered twice")
	}
	packs[pack] = true
	for id, l := range locs {
		if _, dup := linked[id]; dup {
			panic("locales: locale " + id + " in two packs")
		}
		linked[id] = l
	}
}

// Linked reports whether id's data is in the binary.
func Linked(id string) bool {
	if id == "root" {
		return true
	}
	_, ok := linked[id]
	return ok
}

// LinkedPacks lists the packs in the binary, sorted.
func LinkedPacks() []string {
	var out []string
	for p := range packs {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// LinkedIDs lists the linked locale ids, sorted, root excluded.
func LinkedIDs() []string {
	var out []string
	for id := range linked {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// Data merges id's parent chain. ok is false for an id whose data,
// or whose parent's data, is not linked.
func Data(id string) (Locale, bool) {
	if id == "root" {
		// A copy: root's maps seed every locale's through Merge, so
		// handing them out would let any importer rewrite a symbol for
		// every locale that inherits it.
		return Merge(root, Locale{}), true
	}
	l, ok := linked[id]
	if !ok {
		return Locale{}, false
	}
	p, ok := Data(l.Parent)
	if !ok {
		return Locale{}, false
	}
	return Merge(l, p), true
}

// Merge fills child's empty fields from parent and overlays its symbols
// and overrides.
func Merge(child, parent Locale) Locale {
	out := mergeMarks(child, parent)
	out.Symbols = map[string]string{}
	out.Narrow = map[string]string{}
	out.Overrides = map[string]Override{}
	for k, v := range parent.Narrow {
		out.Narrow[k] = v
	}
	for k, v := range child.Narrow {
		if v == k {
			delete(out.Narrow, k)
		} else {
			out.Narrow[k] = v
		}
	}
	for k, v := range parent.Overrides {
		out.Overrides[k] = v
	}
	for k, v := range child.Overrides {
		if v == (Override{}) {
			delete(out.Overrides, k)
		} else {
			out.Overrides[k] = v
		}
	}
	for k, v := range parent.Symbols {
		out.Symbols[k] = v
	}
	// A child entry equal to its own code cancels a parent's symbol (the
	// child writes the ISO code); dropping it keeps "no entry means the
	// code" the only representation of that.
	for k, v := range child.Symbols {
		if v == k {
			delete(out.Symbols, k)
		} else {
			out.Symbols[k] = v
		}
	}
	return out
}

// mergeMarks is Merge's string fields: the child's, or else the
// parent's.
func mergeMarks(child, parent Locale) Locale {
	pick := func(own, inherited string) string {
		if own != "" {
			return own
		}
		return inherited
	}
	return Locale{
		Parent:          child.Parent,
		CurrencyDecimal: pick(child.CurrencyDecimal, parent.CurrencyDecimal),
		CurrencyGroup:   pick(child.CurrencyGroup, parent.CurrencyGroup),
		Decimal:         pick(child.Decimal, parent.Decimal),
		Group:           pick(child.Group, parent.Group),
		Minus:           pick(child.Minus, parent.Minus),
		Digits:          pick(child.Digits, parent.Digits),
		MinGrouping:     pick(child.MinGrouping, parent.MinGrouping),
		Number:          pick(child.Number, parent.Number),
		Percent:         pick(child.Percent, parent.Percent),
		PercentSign:     pick(child.PercentSign, parent.PercentSign),
		Infinity:        pick(child.Infinity, parent.Infinity),
		NaN:             pick(child.NaN, parent.NaN),
		Standard:        pick(child.Standard, parent.Standard),
		Alpha:           pick(child.Alpha, parent.Alpha),
		Spacing:         pick(child.Spacing, parent.Spacing),
	}
}

// Storage reports the storage set's entry for code, which must already
// be upper-case: callers normalise once, through currency.Normalize, so
// a lower-case lookup here is a bug to surface, not to forgive.
func Storage(code string) (Currency, bool) {
	c, ok := currencies[code]
	return c, ok
}

// StorageCodes lists the storage set, sorted.
func StorageCodes() []string {
	out := make([]string, 0, len(currencies))
	for c := range currencies {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

// DigitSets returns every shipped numbering system's digits, so a
// parser reads them whichever packs are linked. A copy: the slice is
// package state.
func DigitSets() []string { return append([]string(nil), allDigits...) }

// PackOf names the pack that carries a shipped locale id, linked or
// not, so a missing pack can be named in an error.
func PackOf(id string) (string, bool) {
	p, ok := index[id]
	return p, ok
}

// Parent is CLDR's fallback parent for a shipped id, "root" at the top.
// Unlike Locale.Parent (the data parent, always inside the same pack),
// this crosses packs: hi-Latn falls back to en-IN.
func Parent(id string) string {
	if p, ok := parents[id]; ok {
		return p
	}
	return "root"
}
