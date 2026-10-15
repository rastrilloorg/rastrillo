package locales

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode"
)

// PacksFor derives the packs an app must link from the locale codes it
// offers. The two are different things and stay different: an offered
// code (zh-Hans, pt-PT) is a routing, catalog and template identity;
// a pack (zh, pt) is a base language's formatting data. The result is
// deduplicated and sorted.
func PacksFor(offered []string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, code := range offered {
		r := Resolve(code)
		if r.Malformed {
			return nil, fmt.Errorf("offered locale %q is not a well-formed language tag", code)
		}
		if r.Wanted == "" {
			return nil, fmt.Errorf("offered locale %q is not a locale CLDR's modern set formats", code)
		}
		if p := index[r.Wanted]; !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out, nil
}

// Config is the effective locale configuration CheckConfig accepted.
// Identities and formatting locales are separate fields and stay
// separate: an offered code (zh-TW) is what routing, catalogs and the
// template cache key on; its formatting locale (zh-Hant) is only what
// the money and number data is read from.
type Config struct {
	Default       string            // the declared effective default: an offered identity
	DefaultFormat string            // the linked locale that formats it
	Offered       []string          // as declared, canonical
	Formats       map[string]string // offered identity -> formatting locale
	Packs         []string          // derived, deduplicated
	LinkedPacks   []string          // what this binary actually links
}

// CanonicalIdentity checks a configuration identity strictly, before
// the forgiving request resolver ever sees it: no surrounding or inner
// whitespace, hyphens not underscores, RFC 5646 grammar, and canonical
// case by subtag role (language lower, script title, region upper;
// variants, extensions and private use lower).
// It returns the canonical form, so an error can name it. Request tags
// stay forgiving (Resolve); configuration does not, because an
// identity that only works after repair would be stored, routed and
// cached under the unrepaired spelling.
func CanonicalIdentity(code string) (string, error) {
	if code == "" {
		return "", errors.New("an empty locale code")
	}
	if strings.ContainsFunc(code, unicode.IsSpace) {
		return "", fmt.Errorf("%q contains white space; write %q", code, strings.Join(strings.Fields(code), ""))
	}
	repaired := strings.ReplaceAll(code, "_", "-")
	if _, _, _, ok := parseTag(repaired); !ok {
		return "", fmt.Errorf("%q is not a well-formed language tag", code)
	}
	parts := strings.Split(repaired, "-")
	// Case by grammatical role, the way the grammar is read, never by
	// length: in en-u-nu-latn, "nu" is an extension subtag, not a
	// region, and stays lower. Language and extlangs lower; then an
	// optional script (title); then an optional region (upper); then
	// variants, extensions and private use, all lower.
	i := 0
	parts[i] = strings.ToLower(parts[i])
	i++
	for n := 0; n < 3 && i < len(parts) && len(parts[i]) == 3 && isAlpha(parts[i]); n++ {
		parts[i] = strings.ToLower(parts[i])
		i++
	}
	if i < len(parts) && len(parts[i]) == 4 && isAlpha(parts[i]) {
		parts[i] = strings.ToUpper(parts[i][:1]) + strings.ToLower(parts[i][1:])
		i++
	}
	if i < len(parts) && (len(parts[i]) == 2 && isAlpha(parts[i]) || len(parts[i]) == 3 && isDigits(parts[i])) {
		parts[i] = strings.ToUpper(parts[i])
		i++
	}
	for ; i < len(parts); i++ {
		parts[i] = strings.ToLower(parts[i])
	}
	canon := strings.Join(parts, "-")
	if canon != code {
		return canon, fmt.Errorf("%q is not in canonical form; write %q", code, canon)
	}
	return canon, nil
}

// CheckConfig validates an app's locale configuration at boot, in the
// binary that will serve it, so it sees the packs actually linked.
//
// Every identity is checked strictly first (CanonicalIdentity), and
// duplicates are refused. Only then is each resolved, and it must be
// recognised and linked without any fallback: root, or a parent
// standing in for a missing pack, is never an accepted configuration.
// The effective default is defaultLocale, else offered[0], else "en";
// an app with no manifest, or with only DefaultLocale set, is checked
// the same way.
func CheckConfig(defaultLocale string, offered []string) (Config, error) {
	eff := defaultLocale
	if eff == "" && len(offered) > 0 {
		eff = offered[0]
	}
	if eff == "" {
		eff = "en"
	}
	cfg := Config{Offered: offered, Formats: map[string]string{}, LinkedPacks: LinkedPacks()}
	var problems []string
	seen := map[string]bool{}
	check := func(code string) {
		if _, err := CanonicalIdentity(code); err != nil {
			problems = append(problems, err.Error())
			return
		}
		r := Resolve(code)
		switch {
		case r.Wanted == "":
			problems = append(problems, fmt.Sprintf("%q is not a locale CLDR's modern set formats", code))
		case r.MissingPack != "" || r.ID != r.Wanted:
			problems = append(problems, fmt.Sprintf("%q needs the %s pack: import _ \"amadan.net/rastrillo/rastrillo/locales/%s\"",
				code, index[r.Wanted], index[r.Wanted]))
		default:
			cfg.Formats[code] = r.ID
		}
	}
	for _, code := range offered {
		if seen[code] {
			problems = append(problems, fmt.Sprintf("%q is offered twice", code))
			continue
		}
		seen[code] = true
		check(code)
	}
	if len(offered) == 0 {
		check(eff)
	} else if !seen[eff] {
		problems = append(problems, fmt.Sprintf("default locale %q is not one of the offered locales %v", eff, offered))
	}
	cfg.Default, cfg.DefaultFormat = eff, cfg.Formats[eff]
	if packs, err := PacksFor(append([]string{eff}, offered...)); err == nil {
		cfg.Packs = packs
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return cfg, errors.New("locale configuration: " + strings.Join(problems, "; "))
	}
	return cfg, nil
}
