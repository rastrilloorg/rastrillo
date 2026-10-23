// Package apitoken is personal API tokens: a person mints one for an
// agent ("Claude") or for themselves, and the API tree verifies it on
// every request.
//
// A token reads tickets_k3Jx9QpL2mWa_<43 characters>: the app's prefix,
// a public ID, and a secret. The public ID is a lookup key and may be
// logged, so "token k3Jx9QpL2mWa deleted 4 orders" is traceable. The
// secret is shown once and stored only as its SHA-256.
package apitoken

import (
	"crypto/rand"
	"regexp"
	"strings"
)

const (
	idLen     = 12 // about 71 bits: enough that a guessed ID is not worth trying
	secretLen = 43 // about 256 bits
	base62    = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
)

// prefixRE is the prefix grammar. No underscore, because base62 has
// none either: that is what lets the three parts split on "_" with no
// ambiguity. ASCII only, so a secret scanner's regex for it is simple.
var prefixRE = regexp.MustCompile(`^[a-z][a-z0-9]{0,15}$`)

// DerivePrefix turns an app name into a prefix New accepts: lowercase,
// keep ASCII a-z and 0-9, prepend "app" when nothing is left or it
// starts with a digit, and cut to 16. "123-app" becomes "app123app".
func DerivePrefix(appName string) string {
	// Byte by byte, lowering only A-Z: strings.ToLower would turn the
	// Kelvin sign into a "k" and "İ" into an "i", so a non-ASCII name
	// would keep letters it never had in ASCII.
	var b strings.Builder
	for i := 0; i < len(appName); i++ {
		c := appName[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			b.WriteByte(c)
		}
	}
	p := b.String()
	if p == "" || (p[0] >= '0' && p[0] <= '9') {
		p = "app" + p
	}
	if len(p) > 16 {
		p = p[:16]
	}
	return p
}

// ValidAgentName trims name and reports whether it is 1 to 40 ASCII
// letters, digits, spaces, hyphens and dots. The name is shown as typed
// ("Claude for Paul") and inside the audit string, so it is kept to
// characters that cannot break either: no markup, no colon, no control
// characters.
func ValidAgentName(name string) (string, bool) {
	name = strings.TrimSpace(name)
	if len(name) < 1 || len(name) > 40 {
		return "", false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == ' ' || c == '-' || c == '.') {
			return "", false
		}
	}
	return name, true
}

// random returns n base62 characters from crypto/rand. A byte is used
// only when it is below 248, the largest multiple of 62 that fits, so
// every character is equally likely: b % 62 over all 256 values would
// favour the first eight.
func random(n int) (string, error) {
	out := make([]byte, 0, n)
	buf := make([]byte, n+n/4)
	for len(out) < n {
		if _, err := rand.Read(buf); err != nil {
			return "", err
		}
		for _, b := range buf {
			if b < 248 && len(out) < n {
				out = append(out, base62[b%62])
			}
		}
	}
	return string(out), nil
}

func isBase62(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z') {
			return false
		}
	}
	return true
}

// parse splits a token into its public ID and secret, or reports that
// it is not one of this app's tokens. It runs before any query, so a
// malformed or foreign token costs no database round trip.
func (s *Store) parse(token string) (id, secret string, ok bool) {
	rest, found := strings.CutPrefix(token, s.cfg.Prefix+"_")
	if !found {
		return "", "", false
	}
	id, secret, found = strings.Cut(rest, "_")
	if !found || !isBase62(id, idLen) || !isBase62(secret, secretLen) {
		return "", "", false
	}
	return id, secret, true
}

// Redact replaces the secret of every token with this app's prefix in
// text by "[redacted]", keeping the prefix and public ID so the log
// still says which token it was. The api tree passes everything it logs
// through it, because an error message can quote a request header.
//
// It matches any run of base62 after the ID, not exactly 43 characters:
// a token pasted with a character missing or extra is still nearly all
// of a secret.
func (s *Store) Redact(text string) string {
	return s.redact.ReplaceAllString(text, "${1}[redacted]")
}
