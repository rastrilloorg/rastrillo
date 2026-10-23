// Package wirename holds the one rule that turns a manifest field name
// into its JSON wire name. It imports only the standard library so both
// sides that need the rule can share it: api names refused fields with
// it, and the root package's Resource.Validate checks filter names
// against the list's own query parameters with it. api imports the root
// package, so neither can own the rule without an import cycle, and two
// copies would let Validate check a name the API never reads.
package wirename

import "strings"

// Name turns a manifest field name into its wire name: "Title" is
// "title", "MaxPerOrder" is "max_per_order", "IPAddress" is "ip_address".
//
// It is the generator's sqlName with one more rule: an uppercase run
// splits before its last letter when a lowercase letter follows, so an
// acronym stays one word. sqlName has no such rule because it never
// needed one: a column called ipaddress is merely ugly, but a client
// reading "ipaddress" on the wire would not know where the words are.
// TestNameAgreesWithSQLName pins that the two agree on every name
// sqlName's own tests cover.
func Name(goName string) string {
	s := goName
	var b strings.Builder
	b.Grow(len(s) + 4)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if isUpper(c) {
			prevUpper := i > 0 && isUpper(s[i-1])
			nextLower := i+1 < len(s) && isLower(s[i+1])
			if i > 0 && (!prevUpper || nextLower) {
				b.WriteByte('_')
			}
			c += 'a' - 'A'
		}
		b.WriteByte(c)
	}
	return b.String()
}

func isUpper(c byte) bool { return c >= 'A' && c <= 'Z' }
func isLower(c byte) bool { return c >= 'a' && c <= 'z' }
