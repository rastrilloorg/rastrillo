// Package basepathtest checks that an app's pages work under a base
// path.
package basepathtest

import (
	"regexp"
	"strings"
)

var attrRe = regexp.MustCompile(`(?i)\b(href|src|action|formaction|poster|data-[a-z0-9-]+)\s*=\s*("([^"]*)"|'([^']*)')`)

// Leaks returns every URL attribute in body whose value is a
// root-absolute path outside base, such as href="/static/t.css" when
// base is /acme. Each is returned as it appears in the source, name and
// quotes included, in source order. With base "", it returns nil.
//
// Run it over every page an app renders under a base path in its own
// tests: a leaked path works at the root and 404s under /<slug>, so
// nothing else catches it until someone clicks.
func Leaks(body []byte, base string) []string {
	if base == "" {
		return nil
	}
	var leaks []string
	for _, m := range attrRe.FindAllSubmatchIndex(body, -1) {
		v := value(body, m)
		if !strings.HasPrefix(v, "/") || strings.HasPrefix(v, "//") {
			continue
		}
		if v == base || strings.HasPrefix(v, base+"/") || strings.HasPrefix(v, base+"?") || strings.HasPrefix(v, base+"#") {
			continue
		}
		leaks = append(leaks, string(body[m[2]:m[1]]))
	}
	return leaks
}

// value is the quoted value of a match: group 3 for double quotes,
// group 4 for single.
func value(body []byte, m []int) string {
	if m[6] >= 0 {
		return string(body[m[6]:m[7]])
	}
	return string(body[m[8]:m[9]])
}
