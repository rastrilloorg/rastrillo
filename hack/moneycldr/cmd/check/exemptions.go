package main

import (
	"regexp"
	"strings"
)

// exemption is one known disagreement between ICU's data and
// cldr-json's, written as the narrowest transformation that turns ICU's
// string into ours. It explains a row only when the transformation,
// applied to that row's ICU string, yields exactly our string, and only
// for its locale and codes; any other difference in the same locale
// stays unexplained.
type exemption struct {
	locale  string
	codes   []string // nil means every code
	except  []string
	reason  string
	replace func(string) string
}

func (e exemption) applies(id, code string) bool {
	if id != e.locale {
		return false
	}
	for _, c := range e.except {
		if c == code {
			return false
		}
	}
	if e.codes == nil {
		return true
	}
	for _, c := range e.codes {
		if c == code {
			return true
		}
	}
	return false
}

func (e exemption) transform(s string) string { return e.replace(s) }

var dollarDecimal = regexp.MustCompile(`(\d)\$(\d)`)

var exemptions = []exemption{
	{
		locale: "fr-CH",
		reason: "ICU groups fr-CH with U+0027 (plain numbers too: 1'234,5); cldr-json 48 groups with U+202F",
		replace: func(s string) string {
			return strings.ReplaceAll(s, "'", "\u202f")
		},
	},
	{
		locale: "pt-CV",
		except: []string{"CVE"},
		reason: "ICU applies the default currency's (CVE) \"$\" decimal mark to every currency in pt-CV; cldr-json gives it to CVE only",
		replace: func(s string) string {
			return dollarDecimal.ReplaceAllString(s, "$1,$2")
		},
	},
	{
		locale: "sr-Cyrl-ME",
		codes:  []string{"BAM"},
		reason: "ICU writes BAM as Latin KM in sr-Cyrl-ME (Cyrillic КМ in sr-Cyrl-BA); cldr-json sr-Cyrl-ME has КМ",
		replace: func(s string) string {
			return strings.ReplaceAll(s, "KM", "КМ")
		},
	},
}
