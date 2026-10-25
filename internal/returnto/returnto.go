// Package returnto holds the one check that decides whether a return
// path from a form or a cookie may be redirected to. It imports only
// the standard library so both sides that need it can share it: the
// sessions package's SafeReturn checks a sign-in's return_to with it,
// and the root package's language and theme switchers check their
// return field with it. sessions imports the root package for
// rastrillo.Actor, so the root package cannot import sessions without
// a cycle, and two copies of the check would let the switchers accept
// a path sign-in refuses.
package returnto

import "strings"

// Safe returns to when it is a same-site absolute path (starts with
// exactly one "/", no scheme, no backslash, no control characters) and
// fallback otherwise. Anything laxer is an open redirect. A backslash
// anywhere matters because http.Redirect's path clean turns
// "/a/../\evil.example/" into "/\evil.example/", which browsers read as
// "//evil.example/"; control characters matter because browsers strip
// tab, CR and LF from a URL before parsing it, so "/\t/evil.example"
// would pass a bare "//" check and still navigate off-site.
func Safe(to, fallback string) string {
	if to == "" || !strings.HasPrefix(to, "/") ||
		strings.HasPrefix(to, "//") || strings.ContainsAny(to, "\\") ||
		strings.ContainsFunc(to, func(c rune) bool { return c < 0x20 || c == 0x7f }) {
		return fallback
	}
	return to
}
