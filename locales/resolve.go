package locales

import (
	"strings"
	"sync"
)

// Resolution says which locale's data formats a tag, and why.
type Resolution struct {
	// ID is the linked locale whose data is used: a CLDR locale id, the
	// app's default locale, or "root" when no default is set.
	ID string
	// Wanted is the shipped locale the tag names, linked or not; "" when
	// no pack carries the language at all, or the tag is malformed.
	Wanted string
	// MissingPack names the pack that would carry Wanted when it is not
	// linked. It is logged once per pack and reported by the
	// RASTRILLO_CHECK_CONFIG probe, so an unlinked language is never a
	// silent change of format.
	MissingPack string
	// Malformed says the tag failed BCP 47 grammar.
	Malformed bool
	// UsedDefault says nothing linked in Wanted's CLDR parent chain
	// covered the tag, so the app's default locale formatted it.
	UsedDefault bool
}

// mu guards appDefault and installed. Serve writes them at boot while
// tests and tools may read them from other goroutines; a bare package
// variable would be a data race that -race reports.
var mu sync.RWMutex

// appDefault is the app's effective default formatting locale, set at
// boot after CheckConfig has proved it linked. When a tag falls through
// every linked parent, it formats in the app's default locale, the same
// way the page's words fall back through the catalogs, so an amount is
// never written in conventions the rest of the page does not use.
// Library use without an app (tests, tools) falls back to root.
var appDefault = ""

var installed map[string]string // identity -> formatting locale, from CheckConfig

// SetDefault installs the app's default formatting locale. It must be
// a linked, canonical id; the framework calls it after CheckConfig, so
// an unlinked id here is a framework bug, not a configuration error.
func SetDefault(id string) {
	mustBeLinked(id)
	mu.Lock()
	appDefault = id
	mu.Unlock()
}

// mustBeLinked panics on a default that is not linked: CheckConfig
// refuses that configuration first, so reaching here is a framework bug,
// and formatting in root instead would hide it.
func mustBeLinked(id string) {
	if !Linked(id) {
		panic("locales: default locale " + id + " is not linked")
	}
}

// SetDefaultForTest replaces the default without the linked check,
// clears any installed configuration, and returns a function that puts
// both back, so a test that installs a default or a configuration
// cannot leak it into the next test in the binary (the gallery tests
// expect root fallback when nothing is installed).
func SetDefaultForTest(id string) (restore func()) {
	mu.Lock()
	prevDefault, prevInstalled := appDefault, installed
	appDefault, installed = id, nil
	mu.Unlock()
	return func() {
		mu.Lock()
		appDefault, installed = prevDefault, prevInstalled
		mu.Unlock()
	}
}

// Install makes an accepted configuration the process's: its default
// formats any tag that falls past every linked parent, and its Formats
// answer FormatFor. rastrillo.Handler calls it at boot, after CheckConfig
// accepts the configuration, so request-time formatting reads the same mapping boot
// validated (zh-TW formats as zh-Hant) instead of re-deriving it.
//
// The default and the map change in one critical section: set apart, a
// goroutine formatting between the two would see the new default with
// the old mapping.
func Install(cfg Config) {
	mustBeLinked(cfg.DefaultFormat)
	m := make(map[string]string, len(cfg.Formats))
	for k, v := range cfg.Formats {
		m[k] = v
	}
	mu.Lock()
	appDefault, installed = cfg.DefaultFormat, m
	mu.Unlock()
}

// FormatFor is the formatting locale for a request's identity: the
// installed mapping when the identity was offered, else whatever
// Resolve finds for it, which falls back to the installed default.
func FormatFor(identity string) string {
	mu.RLock()
	f, ok := installed[identity]
	mu.RUnlock()
	if ok {
		return f
	}
	return Resolve(identity).ID
}

func fallback(r Resolution) Resolution {
	mu.RLock()
	def := appDefault
	mu.RUnlock()
	if def != "" {
		r.ID, r.UsedDefault = def, true
	} else {
		r.ID = "root"
	}
	return r
}

// Resolve maps a BCP 47 tag to the linked locale that formats it.
//
// Parent fallback alone is not enough: zh-TW is not a CLDR locale, and
// truncating it to zh would pick Simplified data (TWD as NT$ instead of
// $). So the tag is validated, canonicalised, given its alias's whole
// replacement and its likely script, matched against every shipped
// locale, and only then walked up to a linked parent, then the app's
// default.
func Resolve(tag string) Resolution {
	if _, ok := index[tag]; ok {
		return linkedFrom(tag) // already a CLDR locale id, exactly as spelled
	}
	lang, script, region, ok := parseTag(tag)
	if !ok {
		return fallback(Resolution{Malformed: true})
	}
	if to, ok := aliases[lang]; ok {
		// The whole replacement applies, field by field, wherever the
		// tag left that field empty: swc is sw-CD, so dropping the
		// region would swap its decimal and group marks.
		al, as, ar, _ := parseTag(to)
		lang = al
		if script == "" {
			script = as
		}
		if region == "" {
			region = ar
		}
	}
	defaultScript := likelyScript[lang]
	regionScript := defaultScript
	if s, ok := likelyScript[lang+"-"+region]; ok {
		regionScript = s
	}
	if script == "" {
		script = regionScript
	}
	var candidates []string
	if region != "" {
		candidates = append(candidates, lang+"-"+script+"-"+region)
		if script == regionScript {
			candidates = append(candidates, lang+"-"+region)
		}
	}
	candidates = append(candidates, lang+"-"+script)
	if script == defaultScript {
		candidates = append(candidates, lang)
	}
	for _, c := range candidates {
		if _, ok := index[c]; ok {
			return linkedFrom(c)
		}
	}
	return fallback(Resolution{})
}

// linkedFrom walks CLDR's fallback parents (Parent, which may cross
// packs) from a shipped locale to the nearest linked one, and to the
// app's default when the walk reaches root.
func linkedFrom(wanted string) Resolution {
	r := Resolution{Wanted: wanted}
	for id := wanted; id != "root"; id = Parent(id) {
		if Linked(id) {
			r.ID = id
			return r
		}
		if r.MissingPack == "" {
			r.MissingPack = index[id]
		}
	}
	return fallback(r)
}

// parseTag checks RFC 5646 grammar (privateuse-only and grandfathered
// tags are refused) and returns the canonical-case language, script and
// region. Variants and extensions are validated and then ignored: the
// locale cookie never carries them, and v1 does not honour -u-nu-.
func parseTag(tag string) (lang, script, region string, ok bool) {
	tag = strings.ReplaceAll(strings.TrimSpace(tag), "_", "-")
	if tag == "" || len(tag) > 64 {
		return "", "", "", false
	}
	parts := strings.Split(tag, "-")
	for _, p := range parts {
		if p == "" || len(p) > 8 || !isAlnum(p) {
			return "", "", "", false
		}
	}
	i := 0
	if !isAlpha(parts[0]) || len(parts[0]) < 2 || len(parts[0]) > 8 || len(parts[0]) == 4 {
		return "", "", "", false
	}
	lang = strings.ToLower(parts[0])
	i++
	for n := 0; n < 3 && i < len(parts) && len(lang) <= 3 && len(parts[i]) == 3 && isAlpha(parts[i]); n++ {
		i++ // extlang: grammatical, then ignored
	}
	if i < len(parts) && len(parts[i]) == 4 && isAlpha(parts[i]) {
		script = strings.ToUpper(parts[i][:1]) + strings.ToLower(parts[i][1:])
		i++
	}
	if i < len(parts) && (len(parts[i]) == 2 && isAlpha(parts[i]) || len(parts[i]) == 3 && isDigits(parts[i])) {
		region = strings.ToUpper(parts[i])
		i++
	}
	for i < len(parts) && (len(parts[i]) >= 5 || len(parts[i]) == 4 && parts[i][0] >= '0' && parts[i][0] <= '9') {
		i++ // variants
	}
	for i < len(parts) {
		// extension (singleton + subtags of 2-8) or private use (x +
		// subtags of 1-8, to the end); anything else is malformed.
		single := strings.ToLower(parts[i])
		if len(single) != 1 {
			return "", "", "", false
		}
		i++
		start := i
		for i < len(parts) && (single == "x" || len(parts[i]) >= 2) {
			i++
		}
		if i == start {
			return "", "", "", false
		}
	}
	return lang, script, region, true
}

func isAlnum(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !('a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9') {
			return false
		}
	}
	return true
}

func isAlpha(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !('a' <= c && c <= 'z' || 'A' <= c && c <= 'Z') {
			return false
		}
	}
	return s != ""
}

func isDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return s != ""
}
