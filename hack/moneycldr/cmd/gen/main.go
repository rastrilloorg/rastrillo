// Command gen writes the root module's locale data, from the fetched,
// hash-checked inputs: the core (locales/core_gen.go), one language pack
// per base language (locales/<lang>/pack_gen.go) and the all-modern
// convenience pack (locales/modern), plus testdata/storage.tsv and
// testdata/packs.tsv here. -root names the locales directory.
//
//	go run ./cmd/gen [-dir DIR] [-root DIR]
//
// It refuses to write anything the formatter could not honour: a
// currency-spacing rule other than the one the formatter implements, a
// numbering system without decimal digits, a locale whose parent is in
// another pack, or a parent-diff encoding that does not reconstruct a
// locale exactly.
package main

import (
	"encoding/xml"
	"flag"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"amadan.net/rastrillo/rastrillo/hack/moneycldr/cldrsrc"
	"amadan.net/rastrillo/rastrillo/locales"
	"amadan.net/rastrillo/rastrillo/money"
)

// reviewedDeviations are current codes whose frozen exponent is
// money.MinorUnits even though ISO 4217 says otherwise. Each one is a
// decision, not data: a code that disagrees and is not listed here is
// excluded, and the generator says so.
var reviewedDeviations = map[string]string{
	// money stores MGA in whole ariary (Stripe's convention). ISO says
	// 2, but the iraimbilanja is a fifth of an ariary, not a hundredth,
	// so no decimal scale is exact, and existing Tito data uses 0.
	"MGA": "money.MinorUnits is 0; ISO 4217 says 2; the subunit is 1/5, not decimal",
}

// root is CLDR's root locale, which cldr-json calls "und".
const root = "und"

func main() {
	dir := flag.String("dir", cldrsrc.DefaultDir(), "cache directory holding the fetched inputs")
	// rootDir, not root: the constant root names CLDR's root locale.
	rootDir := flag.String("root", "../../locales", "the root module's locales directory, where the generated data is written")
	flag.Parse()
	if _, err := cldrsrc.Verify(*dir, filepath.Join("sources", "inputs.sha256")); err != nil {
		fail(err)
	}

	currencies, storageTSV := storageSet(*dir)
	ids := modernLocales(*dir)
	parents := parentMap(*dir)
	shipped := map[string]bool{}
	for _, id := range ids {
		shipped[id] = true
	}
	parentOf := func(id string) string { return parentID(id, parents, shipped) }

	rootData := readLocale(*dir, root, currencies)
	resolved := map[string]locales.Locale{"root": rootData}
	for _, id := range ids {
		resolved[id] = readLocale(*dir, id, currencies)
	}
	packOf := func(id string) string { return strings.SplitN(id, "-", 2)[0] }

	var crossPack []string
	byPack := map[string]map[string]locales.Locale{}
	index := map[string]string{}
	fallbackParents := map[string]string{}
	for _, id := range ids {
		p := parentOf(id)
		// Two parents, kept apart: fallbackParents[id] is CLDR's parent,
		// which lookup walks when a pack is not linked (hi-Latn falls to
		// a linked en-IN); the data parent below is what the pack's diff
		// is relative to, and must stay inside the pack.
		fallbackParents[id] = p
		if p != "root" && packOf(p) != packOf(id) {
			// hi-Latn inherits from en-IN. A pack must not need another
			// pack to resolve, so such a locale is stored whole,
			// relative to root; the self-check below proves it still
			// resolves to the same data.
			crossPack = append(crossPack, id+"<"+p)
			p = "root"
		}
		if _, ok := resolved[p]; !ok {
			fail(fmt.Errorf("%s: parent %s is not shipped", id, p))
		}
		d := diff(resolved[id], p, resolved)
		if byPack[packOf(id)] == nil {
			byPack[packOf(id)] = map[string]locales.Locale{}
		}
		byPack[packOf(id)][id] = d
		index[id] = packOf(id)
	}

	// Self-check: rebuild every locale from its diffs, over the root
	// just read, and compare it with the locale read whole.
	byID := map[string]locales.Locale{}
	for _, pack := range byPack {
		for id, d := range pack {
			byID[id] = d
		}
	}
	for _, id := range ids {
		got := rebuild(id, byID, rootData)
		want := resolved[id]
		got.Parent, want.Parent = "", ""
		if !reflect.DeepEqual(got, want) {
			fail(fmt.Errorf("%s: the parent-diff encoding does not reconstruct the locale", id))
		}
	}

	aliases, likely := lookupTables(*dir, index)
	digitSet := map[string]bool{}
	for _, l := range resolved {
		digitSet[l.Digits] = true
	}
	core := emitCore(rootData, index, fallbackParents, currencies, aliases, likely, sortedKeys(digitSet))
	write(filepath.Join(*rootDir, "core_gen.go"), core)
	var packsTSV strings.Builder
	packsTSV.WriteString("# pack\tlocales\tsource_bytes\n")
	total := 0
	var all []string
	for _, pack := range sortedKeys(byPack) {
		src := emitPack(pack, byPack[pack])
		write(filepath.Join(*rootDir, pack, "pack_gen.go"), src)
		fmt.Fprintf(&packsTSV, "%s\t%d\t%d\n", pack, len(byPack[pack]), len(src))
		total += len(src)
		all = append(all, pack)
	}
	write(filepath.Join(*rootDir, "modern", "modern_gen.go"), emitModern(all))
	written := map[string]bool{"modern": true}
	for _, p := range all {
		written[p] = true
	}
	pruneStale(*rootDir, written)
	write("testdata/storage.tsv", storageTSV)
	write("testdata/packs.tsv", []byte(packsTSV.String()))
	fmt.Printf("locales=%d packs=%d core=%d bytes packs=%d bytes currencies=%d aliases=%d likely=%d cross-pack-parents=%v\n",
		len(ids), len(byPack), len(core), total, len(currencies), len(aliases), len(likely), crossPack)
}

func write(path string, src []byte) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fail(err)
	}
	if err := os.WriteFile(path, src, 0o644); err != nil {
		fail(err)
	}
}

// generatedMark opens every file header writes; pruneStale removes only
// files that carry it.
const generatedMark = "// Code generated by cmd/gen; DO NOT EDIT.\n"

// pruneStale deletes the generated files of packs this run did not
// write, and their directories once empty. Without it a language that
// left CLDR's modern set would keep its old pack, still compiling into
// any app that imports it, and make money-cldr's git diff would pass.
// A file without generatedMark, or a directory holding anything else,
// is not the generator's to delete.
func pruneStale(localesDir string, written map[string]bool) {
	files, err := filepath.Glob(filepath.Join(localesDir, "*", "*_gen.go"))
	if err != nil {
		fail(err)
	}
	for _, f := range files {
		dir := filepath.Dir(f)
		if written[filepath.Base(dir)] {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			fail(err)
		}
		if !strings.HasPrefix(string(src), generatedMark) {
			continue
		}
		if err := os.Remove(f); err != nil {
			fail(err)
		}
		// Fails, harmlessly, when the directory holds other files.
		_ = os.Remove(dir)
	}
}

type isoEntry struct {
	exponent int    // -1 for "N.A."
	edition  string // timestamp of the last edition listing the code
	current  bool   // listed in the newest edition
	conflict string // non-empty when two editions disagree
}

// readISO takes each code's minor units from the newest pinned edition
// that lists it, and records any edition that disagrees.
func readISO(dir string) map[string]*isoEntry {
	type entry struct {
		Code1, Units1 string
		Code2, Units2 string
	}
	eds := append([]struct{ Timestamp, URL string }(nil), cldrsrc.ISOEditions...)
	sort.Slice(eds, func(i, j int) bool { return eds[i].Timestamp < eds[j].Timestamp })
	newest := eds[len(eds)-1].Timestamp
	out := map[string]*isoEntry{}
	for _, e := range eds {
		data, err := os.ReadFile(filepath.Join(dir, cldrsrc.ISOPath(e.Timestamp)))
		if err != nil {
			fail(err)
		}
		var doc struct {
			New []struct {
				Code  string `xml:"Ccy"`
				Units string `xml:"CcyMnrUnts"`
			} `xml:"CcyTbl>CcyNtry"`
			Old []struct {
				Code  string `xml:"ALPHABETIC_CODE"`
				Units string `xml:"MINOR_UNIT"`
			} `xml:"ISO_CURRENCY"`
		}
		if err := xml.Unmarshal(data, &doc); err != nil {
			fail(fmt.Errorf("%s: %w", e.Timestamp, err))
		}
		seen := map[string]string{}
		for _, n := range doc.New {
			seen[n.Code] = n.Units
		}
		for _, o := range doc.Old {
			seen[o.Code] = o.Units
		}
		if len(seen) < 150 {
			fail(fmt.Errorf("ISO edition %s lists only %d codes", e.Timestamp, len(seen)))
		}
		for code, units := range seen {
			code = strings.TrimSpace(code)
			if code == "" {
				continue
			}
			exp := -1
			if n, err := strconv.Atoi(strings.TrimSpace(units)); err == nil {
				exp = n
			}
			cur := out[code]
			if cur == nil {
				cur = &isoEntry{}
				out[code] = cur
			} else if cur.exponent != exp {
				cur.conflict = fmt.Sprintf("%d in %s, %d in %s", cur.exponent, cur.edition, exp, e.Timestamp)
			}
			cur.exponent, cur.edition, cur.current = exp, e.Timestamp, e.Timestamp == newest
		}
	}
	return out
}

type currencyData struct {
	Supplemental struct {
		CurrencyData struct {
			Fractions map[string]map[string]string              `json:"fractions"`
			Region    map[string][]map[string]map[string]string `json:"region"`
		} `json:"currencyData"`
	} `json:"supplemental"`
}

// storageSet applies the one storage rule. A code is stored with
// exponent money.MinorUnits(code) when ISO 4217 gives it a numeric
// minor unit (in the newest pinned edition listing it) equal to that
// exponent, or when the disagreement is a reviewed deviation. CLDR's
// display digits play no part. A stored code is eligible as a new
// default when it is in the newest ISO edition and CLDR lists it as
// legal tender somewhere with no end date.
func storageSet(dir string) (map[string]locales.Currency, []byte) {
	iso := readISO(dir)
	var cd currencyData
	if err := cldrsrc.Read(dir, "cldr-core/supplemental/currencyData.json", &cd); err != nil {
		fail(err)
	}
	openTender := map[string]bool{}
	cldrKnown := map[string]bool{}
	for _, entries := range cd.Supplemental.CurrencyData.Region {
		for _, e := range entries {
			for code, attr := range e {
				cldrKnown[code] = true
				if attr["_tender"] == "false" {
					continue
				}
				if _, ended := attr["_to"]; !ended {
					openTender[code] = true
				}
			}
		}
	}
	codes := map[string]bool{}
	for c := range iso {
		codes[c] = true
	}
	for c := range cldrKnown {
		codes[c] = true
	}
	out := map[string]locales.Currency{}
	var tsv strings.Builder
	tsv.WriteString("# code\texponent\tiso_exponent\tiso_edition\tstatus\tnote\n")
	tsv.WriteString("# Generated by cmd/gen. exponent is money.MinorUnits; iso_edition is the newest pinned ISO 4217 edition listing the code.\n")
	for _, c := range sortedKeys(codes) {
		exp := money.MinorUnits(c)
		e := iso[c]
		isoExp, edition, note := "-", "-", ""
		status := ""
		switch {
		case e == nil:
			status, note = "excluded-unverified", "no pinned ISO edition lists it"
		case e.exponent < 0:
			isoExp, edition, status, note = "N.A.", e.edition, "excluded-no-minor-unit", "fund, metal or test code"
		default:
			isoExp, edition = strconv.Itoa(e.exponent), e.edition
			if e.conflict != "" {
				note = "editions disagree: " + e.conflict
			}
			switch {
			case e.exponent == exp && e.conflict == "":
				status = "stored"
			case reviewedDeviations[c] != "":
				status, note = "stored", "reviewed deviation: "+reviewedDeviations[c]
			default:
				status = "excluded-mismatch"
				if note == "" {
					note = fmt.Sprintf("money.MinorUnits is %d", exp)
				}
			}
			if status == "stored" && e.current && openTender[c] {
				status = "eligible"
			}
		}
		fmt.Fprintf(&tsv, "%s\t%d\t%s\t%s\t%s\t%s\n", c, exp, isoExp, edition, status, note)
		if status == "stored" || status == "eligible" {
			out[c] = locales.Currency{Exponent: exp, Eligible: status == "eligible"}
		}
	}
	return out, []byte(tsv.String())
}

// modernLocales is CLDR's modern-coverage set plus every parent it needs.
func modernLocales(dir string) []string {
	var avail struct {
		AvailableLocales struct {
			Full []string `json:"full"`
		} `json:"availableLocales"`
	}
	if err := cldrsrc.Read(dir, "cldr-core/availableLocales.json", &avail); err != nil {
		fail(err)
	}
	all := map[string]bool{}
	for _, l := range avail.AvailableLocales.Full {
		all[l] = true
	}
	var cov struct {
		Levels map[string]string `json:"effectiveCoverageLevels"`
	}
	if err := cldrsrc.Read(dir, "cldr-core/coverageLevels.json", &cov); err != nil {
		fail(err)
	}
	parents := parentMap(dir)
	chosen := map[string]bool{}
	var add func(string)
	add = func(l string) {
		if l == root || chosen[l] || !all[l] {
			return
		}
		chosen[l] = true
		if p := parentID(l, parents, all); p != "root" {
			add(p)
		}
	}
	for l, level := range cov.Levels {
		if level == "modern" {
			add(l)
		}
	}
	return sortedKeys(chosen)
}

// parentID is CLDR inheritance: an explicit parentLocales entry, else
// truncation, skipping ids that do not exist (ca-ES-valencia inherits
// from ca, because there is no ca-ES), ending at "root".
func parentID(id string, parents map[string]string, exists map[string]bool) string {
	if p, ok := parents[id]; ok {
		if p == root || p == "root" {
			return "root"
		}
		return p
	}
	for p := id; ; {
		i := strings.LastIndex(p, "-")
		if i <= 0 {
			return "root"
		}
		p = p[:i]
		if exists[p] {
			return p
		}
	}
}

func parentMap(dir string) map[string]string {
	var pl struct {
		Supplemental struct {
			ParentLocales struct {
				ParentLocale map[string]string `json:"parentLocale"`
			} `json:"parentLocales"`
		} `json:"supplemental"`
	}
	if err := cldrsrc.Read(dir, "cldr-core/supplemental/parentLocales.json", &pl); err != nil {
		fail(err)
	}
	return pl.Supplemental.ParentLocales.ParentLocale
}

func readLocale(dir, id string, currencies map[string]locales.Currency) locales.Locale {
	var nums map[string]map[string]struct {
		Numbers map[string]any `json:"numbers"`
	}
	if err := cldrsrc.Read(dir, cldrsrc.LocaleFiles(id)[0], &nums); err != nil {
		fail(err)
	}
	n := nums["main"][id].Numbers
	ns, _ := n["defaultNumberingSystem"].(string)
	sym, _ := n["symbols-numberSystem-"+ns].(map[string]any)
	cf, _ := n["currencyFormats-numberSystem-"+ns].(map[string]any)
	if sym == nil || cf == nil {
		fail(fmt.Errorf("%s: no symbols or currency formats for numbering system %q", id, ns))
	}
	var nsys struct {
		Supplemental struct {
			NumberingSystems map[string]map[string]string `json:"numberingSystems"`
		} `json:"supplemental"`
	}
	if err := cldrsrc.Read(dir, "cldr-core/supplemental/numberingSystems.json", &nsys); err != nil {
		fail(err)
	}
	d := nsys.Supplemental.NumberingSystems[ns]
	if d["_type"] != "numeric" || len([]rune(d["_digits"])) != 10 {
		fail(fmt.Errorf("%s: numbering system %q has no decimal digits", id, ns))
	}
	str := func(m map[string]any, k string) string { s, _ := m[k].(string); return s }
	spacing, _ := cf["currencySpacing"].(map[string]any)
	before, _ := spacing["beforeCurrency"].(map[string]any)
	after, _ := spacing["afterCurrency"].(map[string]any)
	for _, side := range []map[string]any{before, after} {
		if str(side, "currencyMatch") != "[[:^S:]&[:^Z:]]" ||
			(str(side, "surroundingMatch") != "[:digit:]" && str(side, "surroundingMatch") != "[[:digit:]]") ||
			str(side, "insertBetween") != str(before, "insertBetween") {
			fail(fmt.Errorf("%s: currency spacing %v is not the rule the formatter implements", id, spacing))
		}
	}
	l := locales.Locale{
		CurrencyDecimal: str(sym, "decimal"),
		CurrencyGroup:   str(sym, "group"),
		Decimal:         str(sym, "decimal"),
		Minus:           str(sym, "minusSign"),
		Digits:          d["_digits"],
		MinGrouping:     str(n, "minimumGroupingDigits"),
		Standard:        str(cf, "standard"),
		Alpha:           str(cf, "standard-alphaNextToNumber"),
		Spacing:         str(before, "insertBetween"),
		Symbols:         map[string]string{},
		Narrow:          map[string]string{},
		Overrides:       map[string]locales.Override{},
	}
	if v := str(sym, "currencyDecimal"); v != "" {
		l.CurrencyDecimal = v
	}
	if v := str(sym, "currencyGroup"); v != "" {
		l.CurrencyGroup = v
	}
	if l.Alpha == "" {
		l.Alpha = l.Standard
	}
	if l.MinGrouping == "" || l.Standard == "" || l.Minus == "" || l.CurrencyGroup == "" {
		fail(fmt.Errorf("%s: missing a required number field", id))
	}
	var curs map[string]map[string]struct {
		Numbers struct {
			Currencies map[string]map[string]string `json:"currencies"`
		} `json:"numbers"`
	}
	if err := cldrsrc.Read(dir, cldrsrc.LocaleFiles(id)[1], &curs); err != nil {
		fail(err)
	}
	for code, c := range curs["main"][id].Numbers.Currencies {
		if _, ok := currencies[code]; !ok {
			continue
		}
		if c["symbol"] != "" && c["symbol"] != code {
			l.Symbols[code] = c["symbol"]
		}
		if n := c["symbol-alt-narrow"]; n != "" && n != code {
			l.Narrow[code] = n
		}
		if o := (locales.Override{Pattern: c["pattern"], Decimal: c["decimal"], Group: c["group"]}); o != (locales.Override{}) {
			l.Overrides[code] = o
		}
	}
	return l
}

// diff stores what id's resolved data changes relative to its parent.
func diff(l locales.Locale, parent string, resolved map[string]locales.Locale) locales.Locale {
	p := resolved[parent]
	keep := func(c, pv string) string {
		if c == pv {
			return ""
		}
		return c
	}
	out := locales.Locale{
		Parent:          parent,
		CurrencyDecimal: keep(l.CurrencyDecimal, p.CurrencyDecimal),
		CurrencyGroup:   keep(l.CurrencyGroup, p.CurrencyGroup),
		Decimal:         keep(l.Decimal, p.Decimal),
		Minus:           keep(l.Minus, p.Minus),
		Digits:          keep(l.Digits, p.Digits),
		MinGrouping:     keep(l.MinGrouping, p.MinGrouping),
		Standard:        keep(l.Standard, p.Standard),
		Alpha:           keep(l.Alpha, p.Alpha),
		Spacing:         keep(l.Spacing, p.Spacing),
	}
	sym := func(m map[string]string, code string) string {
		if s, ok := m[code]; ok {
			return s
		}
		return code
	}
	for c := range union(l.Symbols, p.Symbols) {
		if sym(l.Symbols, c) != sym(p.Symbols, c) {
			if out.Symbols == nil {
				out.Symbols = map[string]string{}
			}
			out.Symbols[c] = sym(l.Symbols, c)
		}
	}
	for c := range union(l.Narrow, p.Narrow) {
		if sym(l.Narrow, c) != sym(p.Narrow, c) {
			if out.Narrow == nil {
				out.Narrow = map[string]string{}
			}
			out.Narrow[c] = sym(l.Narrow, c)
		}
	}
	for c := range union(l.Overrides, p.Overrides) {
		if l.Overrides[c] != p.Overrides[c] {
			if out.Overrides == nil {
				out.Overrides = map[string]locales.Override{}
			}
			out.Overrides[c] = l.Overrides[c]
		}
	}
	return out
}

func union[V any](a, b map[string]V) map[string]bool {
	keys := map[string]bool{}
	for k := range a {
		keys[k] = true
	}
	for k := range b {
		keys[k] = true
	}
	return keys
}

// lookupTables keeps the CLDR language aliases whose replacement's
// language ships, with the whole replacement (language, script and
// region: swc becomes sw-CD, not sw), and the likely script of every
// shipped language and of each language-region pair where it differs.
func lookupTables(dir string, index map[string]string) (map[string]string, map[string]string) {
	langs := map[string]bool{}
	for _, pack := range index {
		langs[pack] = true
	}
	var al struct {
		Supplemental struct {
			Metadata struct {
				Alias struct {
					LanguageAlias map[string]map[string]string `json:"languageAlias"`
				} `json:"alias"`
			} `json:"metadata"`
		} `json:"supplemental"`
	}
	if err := cldrsrc.Read(dir, "cldr-core/supplemental/aliases.json", &al); err != nil {
		fail(err)
	}
	aliases := map[string]string{}
	for from, a := range al.Supplemental.Metadata.Alias.LanguageAlias {
		to := strings.ReplaceAll(a["_replacement"], "_", "-")
		// Only aliases keyed by a bare language subtag: the resolver
		// matches on the language alone.
		if len(from) < 2 || len(from) > 3 || strings.ContainsAny(from, "_-") || to == "" {
			continue
		}
		if langs[strings.SplitN(to, "-", 2)[0]] {
			aliases[from] = to
		}
	}
	var ls struct {
		Supplemental struct {
			LikelySubtags map[string]string `json:"likelySubtags"`
		} `json:"supplemental"`
	}
	if err := cldrsrc.Read(dir, "cldr-core/supplemental/likelySubtags.json", &ls); err != nil {
		fail(err)
	}
	script := func(max string) string {
		for _, p := range strings.Split(max, "-")[1:] {
			if len(p) == 4 {
				return p
			}
		}
		return ""
	}
	likely := map[string]string{}
	for l := range langs {
		if m, ok := ls.Supplemental.LikelySubtags[l]; ok {
			likely[l] = script(m)
		}
	}
	for k, m := range ls.Supplemental.LikelySubtags {
		parts := strings.Split(k, "-")
		if len(parts) == 2 && langs[parts[0]] && len(parts[1]) != 4 && script(m) != likely[parts[0]] {
			likely[k] = script(m)
		}
	}
	return aliases, likely
}

func header(b *strings.Builder, pkg, doc string) {
	b.WriteString(generatedMark + "\n")
	fmt.Fprintf(b, "// Source: cldr-json %s and ISO 4217 editions, listed with their\n", cldrsrc.Version)
	b.WriteString("// SHA-256 in hack/moneycldr/sources/inputs.sha256. Data from the Unicode CLDR,\n")
	b.WriteString("// © Unicode, Inc., under the Unicode License v3 (locales/LICENSE-UNICODE).\n\n")
	b.WriteString(doc)
	fmt.Fprintf(b, "package %s\n\n", pkg)
}

func localeLiteral(b *strings.Builder, d locales.Locale) {
	b.WriteString("{")
	field := func(name, v string) {
		if v != "" {
			fmt.Fprintf(b, "%s: %s, ", name, strconv.Quote(v))
		}
	}
	field("Parent", d.Parent)
	field("CurrencyDecimal", d.CurrencyDecimal)
	field("CurrencyGroup", d.CurrencyGroup)
	field("Decimal", d.Decimal)
	field("Minus", d.Minus)
	field("Digits", d.Digits)
	field("MinGrouping", d.MinGrouping)
	field("Standard", d.Standard)
	field("Alpha", d.Alpha)
	field("Spacing", d.Spacing)
	if len(d.Symbols) > 0 {
		b.WriteString("Symbols: map[string]string{")
		for _, k := range sortedKeys(d.Symbols) {
			fmt.Fprintf(b, "%q: %s, ", k, strconv.Quote(d.Symbols[k]))
		}
		b.WriteString("}, ")
	}
	if len(d.Narrow) > 0 {
		b.WriteString("Narrow: map[string]string{")
		for _, k := range sortedKeys(d.Narrow) {
			fmt.Fprintf(b, "%q: %s, ", k, strconv.Quote(d.Narrow[k]))
		}
		b.WriteString("}, ")
	}
	if len(d.Overrides) > 0 {
		b.WriteString("Overrides: map[string]Override{")
		for _, k := range sortedKeys(d.Overrides) {
			o := d.Overrides[k]
			fmt.Fprintf(b, "%q: {", k)
			for _, f := range [][2]string{{"Pattern", o.Pattern}, {"Decimal", o.Decimal}, {"Group", o.Group}} {
				if f[1] != "" {
					fmt.Fprintf(b, "%s: %s, ", f[0], strconv.Quote(f[1]))
				}
			}
			b.WriteString("}, ")
		}
		b.WriteString("}")
	}
	b.WriteString("}")
}

func emitCore(rootData locales.Locale, index, parents map[string]string, cur map[string]locales.Currency, aliases, likely map[string]string, digitSets []string) []byte {
	var b strings.Builder
	header(&b, "locales", "")
	fmt.Fprintf(&b, "// CLDRVersion is the cldr-json tag the table was generated from.\nconst CLDRVersion = %q\n\n", cldrsrc.Version)
	b.WriteString("// root is CLDR's root locale (und): the stated fallback when no linked pack covers a locale.\nvar root = Locale")
	localeLiteral(&b, rootData)
	b.WriteString("\n\n// index maps every locale id any pack carries to its pack, linked or not,\n// so a missing pack can be named.\nvar index = map[string]string{\n")
	for _, k := range sortedKeys(index) {
		fmt.Fprintf(&b, "%q: %q,\n", k, index[k])
	}
	b.WriteString("}\n\n// parents maps every locale id in index to its CLDR fallback parent (\"root\" for none).\n// It can cross packs (hi-Latn -> en-IN); a pack's data parent (Locale.Parent) never does.\nvar parents = map[string]string{\n")
	for _, k := range sortedKeys(parents) {
		fmt.Fprintf(&b, "%q: %q,\n", k, parents[k])
	}
	b.WriteString("}\n\n// allDigits are the digit sets of every numbering system a shipped locale\n// uses, so the parser reads them whichever packs are linked.\nvar allDigits = []string{\n")
	for _, d := range digitSets {
		fmt.Fprintf(&b, "%s,\n", strconv.Quote(d))
	}
	b.WriteString("}\n\n// currencies is the storage set with frozen exponents.\nvar currencies = map[string]Currency{\n")
	for _, c := range sortedKeys(cur) {
		fmt.Fprintf(&b, "%q: {%d, %v},\n", c, cur[c].Exponent, cur[c].Eligible)
	}
	b.WriteString("}\n\n// aliases maps a deprecated language subtag to its whole replacement tag.\nvar aliases = map[string]string{\n")
	for _, k := range sortedKeys(aliases) {
		fmt.Fprintf(&b, "%q: %q,\n", k, aliases[k])
	}
	b.WriteString("}\n\n// likelyScript maps a language, or language-region where it differs, to its likely script.\nvar likelyScript = map[string]string{\n")
	for _, k := range sortedKeys(likely) {
		fmt.Fprintf(&b, "%q: %q,\n", k, likely[k])
	}
	b.WriteString("}\n")
	return gofmt(b.String())
}

func emitPack(pack string, locs map[string]locales.Locale) []byte {
	var b strings.Builder
	header(&b, pack, fmt.Sprintf("// Package %s links CLDR money-formatting data for every modern %q\n// locale. Import it for its side effect.\n", pack, pack))
	b.WriteString("import \"amadan.net/rastrillo/rastrillo/locales\"\n\n")
	fmt.Fprintf(&b, "func init() { locales.Register(%q, data) }\n\n", pack)
	b.WriteString("var data = map[string]locales.Locale{\n")
	for _, id := range sortedKeys(locs) {
		fmt.Fprintf(&b, "%q: ", id)
		var lb strings.Builder
		localeLiteral(&lb, locs[id])
		b.WriteString(strings.ReplaceAll(lb.String(), "map[string]Override{", "map[string]locales.Override{"))
		b.WriteString(",\n")
	}
	b.WriteString("}\n")
	return gofmt(b.String())
}

func emitModern(packs []string) []byte {
	var b strings.Builder
	header(&b, "modern", "// Package modern links every language pack: all CLDR modern-coverage\n// locales. Import it for its side effect.\n")
	b.WriteString("import (\n")
	for _, p := range packs {
		fmt.Fprintf(&b, "\t_ \"amadan.net/rastrillo/rastrillo/locales/%s\"\n", p)
	}
	b.WriteString(")\n")
	return gofmt(b.String())
}

// rebuild folds id's diff chain onto rootData, the root just read from
// CLDR. It never reads locales.Data: that is the previous generation,
// compiled into this program, so a changed root would be checked
// against stale data, and the check would depend on the output it is
// meant to vouch for.
func rebuild(id string, byID map[string]locales.Locale, rootData locales.Locale) locales.Locale {
	if id == "root" {
		return locales.Merge(rootData, locales.Locale{})
	}
	d, ok := byID[id]
	if !ok {
		fail(fmt.Errorf("%s: a parent-diff chain reaches a locale no pack carries", id))
	}
	return locales.Merge(d, rebuild(d.Parent, byID, rootData))
}

func gofmt(s string) []byte {
	src, err := format.Source([]byte(s))
	if err != nil {
		fail(err)
	}
	return src
}

func sortedKeys[V any](m map[string]V) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "gen:", err)
	os.Exit(1)
}
