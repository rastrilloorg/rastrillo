// Package cspallow adds sources to a Content-Security-Policy for
// rastrillo.Options.CSPAllow, refusing what would change the policy in
// ways the app did not ask for.
package cspallow

import (
	"encoding/base64"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// cspFallbacks names each directive Options.CSPAllow may add to, with
// the directives a browser uses in its place when the policy lacks it,
// nearest first (CSP Level 3's directive fallback list). form-action,
// base-uri and frame-ancestors have none: missing, they allow anything.
var cspFallbacks = map[string][]string{
	"default-src":     nil,
	"child-src":       {"default-src"},
	"connect-src":     {"default-src"},
	"font-src":        {"default-src"},
	"frame-src":       {"child-src", "default-src"},
	"img-src":         {"default-src"},
	"manifest-src":    {"default-src"},
	"media-src":       {"default-src"},
	"object-src":      {"default-src"},
	"script-src":      {"default-src"},
	"script-src-elem": {"script-src", "default-src"},
	"script-src-attr": {"script-src", "default-src"},
	"style-src":       {"default-src"},
	"style-src-elem":  {"style-src", "default-src"},
	"style-src-attr":  {"style-src", "default-src"},
	"worker-src":      {"child-src", "script-src", "default-src"},
	"base-uri":        nil,
	"form-action":     nil,
	"frame-ancestors": nil,
}

var cspKeywords = []string{
	"'self'", "'none'", "'unsafe-inline'", "'unsafe-eval'",
	"'unsafe-hashes'", "'strict-dynamic'", "'report-sample'",
	"'wasm-unsafe-eval'", "'inline-speculation-rules'",
}

var cspHashSizes = map[string]int{"sha256": 32, "sha384": 48, "sha512": 64}

// cspHostSource's path is CSP Level 3's path-part: RFC 3986 path
// characters and well-formed %HH escapes. A browser drops a source with
// anything else, such as %GG or a backslash, and the host stays blocked.
var (
	cspSchemeSource = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*:$`)
	cspHostSource   = regexp.MustCompile(`^([a-zA-Z][a-zA-Z0-9+.-]*://)?` +
		`(\*|(\*\.)?[a-zA-Z0-9-]+(\.[a-zA-Z0-9-]+)*)(:([0-9]+|\*))?` +
		`(/([a-zA-Z0-9._~!$&'()*+=:@/-]|%[0-9a-fA-F]{2})*)?$`)
)

// Add returns policy with allow's sources added. New directives go
// last in name order, so the header is the same on every start.
func Add(policy string, allow map[string][]string) (string, error) {
	if len(allow) == 0 {
		return policy, nil
	}
	if strings.Contains(policy, ",") {
		return "", errors.New("Options.CSP holds a comma, which starts " +
			"a second policy, so Options.CSPAllow cannot tell which " +
			"one to add to: give Options.CSP a single policy")
	}
	var directives [][]string
	for _, directive := range strings.Split(policy, ";") {
		if fields := strings.Fields(directive); len(fields) > 0 {
			directives = append(directives, fields)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(allow)) {
		err := checkAllowed(name, allow[name])
		if err != nil {
			return "", err
		}
		if len(allow[name]) > 0 {
			directives, err = addToDirective(directives, name, allow[name])
			if err != nil {
				return "", err
			}
		}
	}
	joined := make([]string, len(directives))
	for index, fields := range directives {
		joined[index] = strings.Join(fields, " ")
	}
	return strings.Join(joined, "; "), nil
}

// addToDirective starts a missing directive from the one the browser
// was using in its place, so adding a source never refuses one it
// already allowed.
func addToDirective(
	directives [][]string, name string, sources []string,
) ([][]string, error) {
	at := findDirective(directives, name)
	if at < 0 {
		seed := firstPresent(directives, cspFallbacks[name])
		if seed < 0 {
			return nil, fmt.Errorf("Options.CSPAllow adds to %s, which "+
				"the policy leaves open to every source: take it out "+
				"of CSPAllow, or write the directive in Options.CSP",
				name)
		}
		directives = pinInheritors(directives, name, seed)
		started := append([]string{name}, directives[seed][1:]...)
		directives = append(directives, started)
		at = len(directives) - 1
	}
	directives[at] = withSources(directives[at], sources)
	return directives, nil
}

// pinInheritors writes out each missing directive that falls back
// through name to sources other than seed's. A new name would sit
// ahead of them in their fallback list and change what they allow:
// worker-src inheriting script-src's blob: would lose it to a new
// child-src started from default-src.
func pinInheritors(
	directives [][]string, name string, seed int,
) [][]string {
	for _, inheritor := range slices.Sorted(maps.Keys(cspFallbacks)) {
		chain := cspFallbacks[inheritor]
		through := slices.Index(chain, name)
		if through < 0 || findDirective(directives, inheritor) >= 0 {
			continue
		}
		current := firstPresent(directives, chain)
		ahead := firstPresent(directives, chain[:through])
		if current >= 0 && ahead < 0 && current != seed {
			pinned := append([]string{inheritor},
				directives[current][1:]...)
			directives = append(directives, pinned)
		}
	}
	return directives
}

// withSources keeps 'none' only alone: beside any other source,
// browsers ignore it and warn in the console.
func withSources(directive, sources []string) []string {
	for _, source := range sources {
		if !slices.Contains(directive[1:], source) {
			directive = append(directive, source)
		}
	}
	if slices.IndexFunc(directive[1:], isNotNone) >= 0 {
		directive = slices.DeleteFunc(directive, isNone)
	}
	return directive
}

func isNone(source string) bool {
	return strings.EqualFold(source, "'none'")
}

func isNotNone(source string) bool {
	return !isNone(source)
}

// findDirective finds the first directive called name, the one a
// browser enforces when a policy repeats it.
func findDirective(directives [][]string, name string) int {
	return slices.IndexFunc(directives, func(fields []string) bool {
		return strings.EqualFold(fields[0], name)
	})
}

func firstPresent(directives [][]string, names []string) int {
	for _, name := range names {
		if at := findDirective(directives, name); at >= 0 {
			return at
		}
	}
	return -1
}

func checkAllowed(name string, sources []string) error {
	if _, known := cspFallbacks[name]; !known {
		return fmt.Errorf("Options.CSPAllow names %q, which is not "+
			"a directive that takes sources, such as img-src", name)
	}
	for _, source := range sources {
		if err := checkSource(source); err != nil {
			return fmt.Errorf("Options.CSPAllow[%q] holds %q: %v",
				name, source, err)
		}
	}
	return nil
}

// checkSource refuses a source a browser would drop, so a typo stops
// the deploy instead of blocking the host in production, and anything
// that would end the directive early and smuggle in one of its own.
func checkSource(source string) error {
	lower := strings.ToLower(source)
	switch {
	case source == "" || strings.ContainsFunc(source, endsSource):
		return errors.New("give each source as its own string, " +
			"with no spaces, commas or semicolons")
	case strings.HasPrefix(lower, "'nonce-"):
		return errors.New("this header is built once at startup, so " +
			"a nonce would be the same on every response: allow the " +
			"script by its hash instead")
	case slices.Contains(cspKeywords, lower), isCSPHash(source),
		cspSchemeSource.MatchString(source),
		cspHostSource.MatchString(source):
		return nil
	}
	return errors.New("write a host such as https://logos.example.com, " +
		"a scheme such as blob:, a quoted keyword such as 'self', " +
		"or a hash such as 'sha256-...'")
}

func endsSource(char rune) bool {
	return char == ';' || char == ',' ||
		unicode.IsSpace(char) || unicode.IsControl(char)
}

// isCSPHash accepts base64 and base64url, padded or not, as CSP Level
// 3's base64-value does, but only at the algorithm's digest length.
func isCSPHash(source string) bool {
	quoted, found := strings.CutPrefix(source, "'")
	quoted, closed := strings.CutSuffix(quoted, "'")
	algorithm, digest, dashed := strings.Cut(quoted, "-")
	size, known := cspHashSizes[strings.ToLower(algorithm)]
	if !found || !closed || !dashed || !known {
		return false
	}
	for _, encoding := range []*base64.Encoding{
		base64.StdEncoding, base64.URLEncoding,
		base64.RawStdEncoding, base64.RawURLEncoding,
	} {
		decoded, err := encoding.DecodeString(digest)
		if err == nil && len(decoded) == size {
			return true
		}
	}
	return false
}
