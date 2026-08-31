// markup-spelling: old-spelling begin — see markup.go.

package markup

import (
	"fmt"
	"regexp"
	"strings"
)

// Note is something the rewrite could not do and a human has to look
// at. A codemod that guesses is worse than one that says where it
// stopped, so an unrecognised shape is left exactly as it was and
// reported here.
type Note struct {
	Line int    // 1-based, in the file as it was read
	Text string // what was found, and why it was left alone
}

func (n Note) String() string { return fmt.Sprintf("%d: %s", n.Line, n.Text) }

var (
	rstClassToken = regexp.MustCompile(`rst-[A-Za-z0-9_-]+`)
	tmplAction    = regexp.MustCompile(`\{\{[^{}]*\}\}`)
)

// Rewrite translates one file's markup from the class spelling to the
// attribute spelling. It rewrites class attributes (both the plain
// class="…" of a template and the class=\"…\" of a Go string literal)
// and the data-tone attribute, and touches nothing else — a CSS
// selector, a Go identifier, a comment that only names a class are all
// left alone.
//
// It is idempotent. The output has no class attribute carrying a
// migrating rst- name, so a second pass finds nothing to do.
func Rewrite(src []byte) ([]byte, []Note) { return rewrite(src, true) }

// Respell is Rewrite without the migration's renames: the pure grammar,
// for markup that is already written in today's class vocabulary and
// only wants the other spelling of it. It is what the stage-1 browser
// drive translates its fixture with, where the two spellings must mean
// the same thing rather than one being an upgrade of the other.
func Respell(src []byte) ([]byte, []Note) { return rewrite(src, false) }

func rewrite(src []byte, migrate bool) ([]byte, []Note) {
	s := string(src)
	var b strings.Builder
	var notes []Note
	note := func(at int, format string, args ...any) {
		notes = append(notes, Note{
			Line: 1 + strings.Count(s[:at], "\n"),
			Text: fmt.Sprintf(format, args...),
		})
	}
	fenced := fencedRegions(s)
	i := 0
	for {
		a, ok := nextClassAttr(s, i)
		if !ok {
			break
		}
		// A region a human has fenced off: a page whose subject IS the
		// old spelling, a fixture that has to be written in it. Without
		// this the only opt-out is "do not run the tool on that file",
		// which is not one — every repository migrating its own docs
		// has a paragraph that must keep the spelling it is about.
		if fenced(a.start) {
			b.WriteString(s[i:a.end])
			i = a.end
			continue
		}
		value := s[a.valStart:a.valEnd]
		if !strings.Contains(value, "rst-") {
			b.WriteString(s[i:a.end])
			i = a.end
			continue
		}
		if c, bad := notAClassList(value); bad {
			// Almost always source that builds its markup by
			// concatenation: the reader ran past the end of a string
			// literal and what it is holding is an expression rather
			// than a class list. Rewriting it would produce something
			// that does not compile, so say where it is and move on.
			note(a.start, "class=%q is not a class list — it holds %q, so this markup is probably built by "+
				"concatenation and the reader ran past the end of a literal: left as it was", value, string(c))
			b.WriteString(s[i:a.end])
			i = a.end
			continue
		}
		repl, err := rewriteClassValue(value, a.q, migrate)
		if err != nil {
			note(a.start, "%s", err.Error())
			b.WriteString(s[i:a.end])
			i = a.end
			continue
		}
		// An attribute that translated to nothing at all takes its own
		// leading space with it, or the tag keeps a gap where a class
		// used to be.
		start := a.start
		if repl == "" {
			for start > i && (s[start-1] == ' ' || s[start-1] == '\t') {
				start--
			}
		}
		b.WriteString(s[i:start])
		b.WriteString(repl)
		i = a.end
	}
	b.WriteString(s[i:])
	// The fence is recomputed here rather than reused: the class pass
	// moved every offset after its first rewrite, and the markers
	// themselves are never rewritten, so they are still where they say.
	body := b.String()
	inFence := fencedRegions(body)
	out := replaceOutsideFence(body, toneInMarkup, "${1}rst-tone=${2}", inFence)

	// Escaped markup — a documentation page showing class=&quot;rst-box&quot;
	// as source. Rewriting it would mean deciding what the escaping is
	// for; naming it is the honest half, and it is a shape that exists
	// in any repository with migration notes of its own.
	for _, m := range escapedClassAttr.FindAllStringSubmatchIndex(out, -1) {
		if strings.Contains(out[m[2]:m[3]], "rst-") {
			notes = append(notes, Note{
				Line: 1 + strings.Count(out[:m[0]], "\n"),
				Text: "escaped markup (" + out[m[0]:m[1]] + ") is documentation rather than markup: rewrite it by hand if it should teach the attribute spelling",
			})
		}
	}
	return []byte(out), notes
}

// replaceOutsideFence is regexp.ReplaceAllString that leaves fenced
// regions alone.
func replaceOutsideFence(s string, re *regexp.Regexp, repl string, fenced func(int) bool) string {
	var b strings.Builder
	last := 0
	for _, m := range re.FindAllStringSubmatchIndex(s, -1) {
		if fenced(m[0]) {
			continue
		}
		b.WriteString(s[last:m[0]])
		b.Write(re.ExpandString(nil, repl, s, m))
		last = m[1]
	}
	b.WriteString(s[last:])
	return b.String()
}

// The fence, and the only opt-out this tool has. A line carrying the
// begin marker starts a region it will not rewrite; a line carrying the
// end marker closes it. Deliberately one mechanism shared with the
// framework's own spelling gate, so a repository has one thing to
// learn and one thing to grep for.
const (
	FenceBegin = "markup-spelling: old-spelling begin"
	// Assembled from halves, or this line would close the fence that
	// the top of this file opens and the rest of the file would be
	// rewritten by its own tool. The one place that has to be true.
	FenceEnd = "markup-spelling: old-spelling " + "end"
)

// fencedRegions returns a predicate saying whether a byte offset falls
// inside a fenced region.
func fencedRegions(s string) func(int) bool {
	type span struct{ from, to int }
	var spans []span
	open := -1
	at := 0
	for _, line := range strings.SplitAfter(s, "\n") {
		switch {
		case open < 0 && strings.Contains(line, FenceBegin):
			open = at
		case open >= 0 && strings.Contains(line, FenceEnd):
			spans = append(spans, span{open, at + len(line)})
			open = -1
		}
		at += len(line)
	}
	if open >= 0 {
		spans = append(spans, span{open, len(s)})
	}
	return func(i int) bool {
		for _, sp := range spans {
			if i >= sp.from && i < sp.to {
				return true
			}
		}
		return false
	}
}

// escapedClassAttr matches a class attribute written as escaped text —
// what a page showing markup as source carries.
var escapedClassAttr = regexp.MustCompile(`class=(?:&quot;|&#34;|&#x22;)([^&]*)(?:&quot;|&#34;|&#x22;)`)

// classAttr is one class attribute found in the source: the span it
// occupies, the span of its value, and the quoting a replacement's own
// values must be written in.
type classAttr struct {
	start, end       int
	valStart, valEnd int
	q                string
}

// nextClassAttr finds the next class attribute at or after from, in
// every shape HTML allows: any case, whitespace around the =, and a
// value that is double-quoted, single-quoted, escaped for a Go or
// JavaScript string literal, or not quoted at all.
//
// It reads all of them rather than the one shape this framework happens
// to write, because the alternative is the one thing this tool must
// never do: tell an app whose templates use single quotes that there is
// nothing to do, and let it find out at stage 3, when the class
// selectors are gone and nothing says why the page is unstyled.
func nextClassAttr(s string, from int) (classAttr, bool) {
	for i := from; i < len(s); {
		k := indexFold(s, "class", i)
		if k < 0 {
			return classAttr{}, false
		}
		i = k + len("class")
		// Not the tail of a longer word: data-class, superclass.
		if k > 0 && isIdentByte(s[k-1]) {
			continue
		}
		j := skipSpace(s, i)
		// An =, and not the head of ==, so a Go comparison is not markup.
		if j >= len(s) || s[j] != '=' || (j+1 < len(s) && s[j+1] == '=') {
			continue
		}
		lead := byte(' ')
		if k > 0 {
			lead = s[k-1]
		}
		a, ok := readAttrValue(s, skipSpace(s, j+1), lead)
		if !ok {
			continue
		}
		a.start = k
		return a, true
	}
	return classAttr{}, false
}

// indexFold is strings.Index for an ASCII-lowercase needle, ignoring
// case in the haystack.
func indexFold(s, needle string, from int) int {
	for i := from; i+len(needle) <= len(s); i++ {
		if strings.EqualFold(s[i:i+len(needle)], needle) {
			return i
		}
	}
	return -1
}

func skipSpace(s string, i int) int {
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	return i
}

func isIdentByte(c byte) bool {
	return c == '-' || c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// readAttrValue reads an attribute value that starts at k. The quote it
// reports is the one a replacement writes its own values in; an
// unquoted value becomes a double-quoted one, because a variant list
// has a space in it and an unquoted attribute cannot hold one.
func readAttrValue(s string, k int, lead byte) (classAttr, bool) {
	closed := func(open, quote string) (classAttr, bool) {
		valStart := k + len(open)
		e := strings.Index(s[valStart:], quote)
		if e < 0 {
			return classAttr{}, false
		}
		return classAttr{
			valStart: valStart,
			valEnd:   valStart + e,
			end:      valStart + e + len(quote),
			q:        quote,
		}, true
	}
	switch {
	case strings.HasPrefix(s[k:], `\"`):
		return closed(`\"`, `\"`)
	case strings.HasPrefix(s[k:], `\'`):
		return closed(`\'`, `\'`)
	case k < len(s) && s[k] == '"':
		return closed(`"`, `"`)
	case k < len(s) && s[k] == '\'':
		return closed(`'`, `'`)
	case unquotedAttrOK(lead) && k < len(s) && s[k] != '>' && s[k] != ' ' && s[k] != '\t' && s[k] != '\n':
		e := k
		for e < len(s) && s[e] != ' ' && s[e] != '\t' && s[e] != '\n' && s[e] != '>' && s[e] != '/' {
			e++
		}
		if !looksLikeAClassList(s[k:e]) {
			return classAttr{}, false
		}
		return classAttr{valStart: k, valEnd: e, end: e, q: `"`}, true
	}
	return classAttr{}, false
}

// unquotedAttrOK: an unquoted value is only read where an attribute can
// actually begin. Without this, the "class=" of a URL query string
// (?class=x&y=1) reads as markup and gets rewritten into nonsense.
func unquotedAttrOK(lead byte) bool {
	switch lead {
	case ' ', '\t', '\n', '\r', '<', '`', '"', '\'', '(':
		return true
	}
	return false
}

// looksLikeAClassList keeps the unquoted reader off the two things that
// wear the same shape and are not markup: a URL query, and a class
// attribute written as escaped text (class=&quot;rst-box&quot;), which
// the escaped-markup pass reports instead.
func looksLikeAClassList(value string) bool {
	if value == "" {
		return false
	}
	for i := 0; i < len(value); i++ {
		switch value[i] {
		case '&', '?', '%', '#', '=', ';', '{', '}':
			return false
		}
	}
	return true
}

// notAClassList reports the character that says a value is not an HTML
// class list at all. None of them can appear in a class attribute — a
// quote would have ended it — so finding one means the reader is
// holding source rather than markup, which is what happens when the
// markup is built by concatenating string literals.
func notAClassList(value string) (byte, bool) {
	for i := 0; i < len(value); i++ {
		switch value[i] {
		case '"', '\'', '`', '<', '>', '\\':
			return value[i], true
		}
	}
	return 0, false
}

// attr is one attribute being built: its name, the variant tokens the
// literal class list gave it, and — for a class value that carries a
// template action — the expression that supplies the rest of its value.
type attr struct {
	name     string
	variants []string
	expr     string
	// When a conditional can produce no variant at all, the whole
	// value — the = and the quotes with it — is written inside that
	// conditional, so the attribute renders bare rather than empty:
	// rst-person{{if .Large}}="lg"{{end}}. html/template reads an
	// action in that position and both branches leave the parser in
	// the same state, so it is a shape it can compile.
	open, closes string
}

func rewriteClassValue(value, q string, migrate bool) (string, error) {
	if !strings.Contains(value, "{{") {
		kept, attrs := splitClassTokens(strings.Fields(value), migrate)
		return render(kept, attrs, q), nil
	}
	return rewriteTemplatedClassValue(value, q, migrate)
}

// splitClassTokens sorts a literal class list into what stays in class
// and what becomes an attribute, keeping first-appearance order so the
// diff reads like the markup it replaces.
func splitClassTokens(tokens []string, migrate bool) (kept []string, attrs []*attr) {
	byName := map[string]*attr{}
	for _, t := range tokens {
		if !strings.HasPrefix(t, "rst-") {
			kept = append(kept, t)
			continue
		}
		name, variant, ok, drop := classFor(t, migrate)
		if drop {
			continue
		}
		if !ok {
			kept = append(kept, t)
			continue
		}
		a := byName[name]
		if a == nil {
			a = &attr{name: name}
			byName[name] = a
			attrs = append(attrs, a)
		}
		if variant != "" {
			a.variants = append(a.variants, variant)
		}
	}
	return kept, attrs
}

func render(kept []string, attrs []*attr, q string) string {
	var parts []string
	if len(kept) > 0 {
		parts = append(parts, "class="+q+strings.Join(kept, " ")+q)
	}
	for _, a := range attrs {
		if a.open != "" {
			parts = append(parts, a.name+a.open+"="+q+a.expr+q+a.closes)
			continue
		}
		value := strings.Join(a.variants, " ")
		if a.expr != "" {
			if value != "" {
				value += " "
			}
			value += a.expr
		}
		if value == "" {
			parts = append(parts, a.name)
			continue
		}
		parts = append(parts, a.name+"="+q+value+q)
	}
	return strings.Join(parts, " ")
}

// rewriteTemplatedClassValue handles the one shape a class list takes
// when a template writes part of it: a literal head, then a conditional
// that adds a modifier of a kind the head already names.
//
//	class="rst-badge{{with .Tone}} rst-badge--{{.}}{{end}}"
//	  ->  rst-badge{{with .Tone}}="{{.}}"{{end}}
//	class="rst-btn{{if .Danger}} rst-btn--danger{{else}} rst-btn--primary{{end}}"
//	  ->  rst-btn="{{if .Danger}}danger{{else}}primary{{end}}"
//
// Which of the two depends on whether the conditional can produce no
// modifier at all. A branchless {{if}} or {{with}} can, so the = and
// the quotes go inside it and the attribute renders bare — which is
// what a reader copying the gallery should see. A choice between two
// modifiers always produces one, so the conditional goes in the value,
// where it reads as what it is.
//
// Anything else is left alone and reported. A class list whose
// structure this cannot read is a handful of lines for a human, and a
// wrong guess is a screen that renders unstyled.
func rewriteTemplatedClassValue(value, q string, migrate bool) (string, error) {
	head := value[:strings.Index(value, "{{")]
	rest := value[len(head):]
	kept, attrs := splitClassTokens(strings.Fields(head), migrate)

	tokens := rstClassToken.FindAllString(rest, -1)
	if len(tokens) == 0 {
		return "", fmt.Errorf(`class=%q carries a template action and no rst- class inside it: left as it was`, value)
	}
	kind := ""
	for _, t := range tokens {
		k := strings.Index(t, "--")
		if k < 0 {
			return "", fmt.Errorf(`class=%q: %q inside a template action is not a modifier, so the value slot cannot carry it: left as it was`, value, t)
		}
		if kind == "" {
			kind = t[:k]
		} else if kind != t[:k] {
			return "", fmt.Errorf(`class=%q names modifiers of two kinds (%s, %s) inside one template action: left as it was`, value, kind, t[:k])
		}
	}
	// Everything in the conditional that is not an action must be one
	// of those modifiers, or this is a shape the rule above does not
	// describe.
	known := map[string]bool{}
	for _, t := range tokens {
		known[t] = true
	}
	for _, f := range strings.Fields(tmplAction.ReplaceAllString(rest, " ")) {
		if !known[f] {
			return "", fmt.Errorf(`class=%q has literal text (%q) beside the modifiers in its template action: left as it was`, value, f)
		}
	}

	name, _, ok, drop := classFor(kind, migrate)
	if drop || !ok {
		return "", fmt.Errorf(`class=%q builds a modifier of %s, which does not migrate: left as it was`, value, kind)
	}
	strip := regexp.MustCompile(`\s*` + regexp.QuoteMeta(kind+"--"))

	a := (*attr)(nil)
	for _, cand := range attrs {
		if cand.name == name {
			a = cand
		}
	}
	if a == nil {
		a = &attr{name: name}
		attrs = append(attrs, a)
	}

	actions := tmplAction.FindAllString(rest, -1)
	branchless := len(a.variants) == 0 &&
		len(actions) >= 2 &&
		strings.HasPrefix(actions[len(actions)-1], "{{end") &&
		strings.HasSuffix(rest, actions[len(actions)-1])
	for _, act := range actions {
		if strings.HasPrefix(act, "{{else") {
			branchless = false
		}
	}
	if branchless {
		a.open = actions[0]
		a.closes = actions[len(actions)-1]
		a.expr = strip.ReplaceAllString(rest[len(a.open):len(rest)-len(a.closes)], "")
		return render(kept, attrs, q), nil
	}
	a.expr = strip.ReplaceAllString(rest, "")
	return render(kept, attrs, q), nil
}
