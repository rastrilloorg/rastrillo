package rastrillo

import "sort"

// retagged maps a locale code the framework used to ship under to the
// code it ships under now. The framework's Portuguese was always
// European Portuguese, but tagged pt it was formatted as CLDR's pt,
// which is Brazil: € 1.284,50 where Portugal writes 1284,50 €. It ships
// as pt-PT now (spec decision 8, §9).
//
// Retagging alone would have broken everything that still says pt: old
// /pt/ links, rastrillo_locale=pt cookies, an app's locales/pt.toml, and
// an app whose manifest offers pt. This table is what each of those is
// checked against, so that there is one place a future retag is added.
var retagged = map[string]string{"pt": "pt-PT"}

// RetagOf reports the code the framework now ships code's catalog under,
// and whether code is one it retagged. It is exported for
// internal/generate, whose catalog checks have to agree with NewLocales
// about which codes the framework covers.
func RetagOf(code string) (string, bool) {
	to, ok := retagged[code]
	return to, ok
}

// Retagged returns a copy of the table, old code to new, for the code
// outside this package that has to act on every retag rather than ask
// about one code: dsgen's redirect stubs for old gallery addresses, the
// catalog checks, and the datetime harness's coverage of old codes. A
// "pt" spelled at any of those would miss the next retag silently.
func Retagged() map[string]string {
	out := make(map[string]string, len(retagged))
	for from, to := range retagged {
		out[from] = to
	}
	return out
}

// retaggedFrom is RetagOf's inverse: the old codes whose catalogs now
// ship as code. Sorted, because NewLocales layers their files in this
// order and map order would make which file wins vary between boots.
func retaggedFrom(code string) []string {
	var out []string
	for from, to := range retagged {
		if to == code {
			out = append(out, from)
		}
	}
	sort.Strings(out)
	return out
}
