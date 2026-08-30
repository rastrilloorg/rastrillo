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
func Rewrite(src []byte) ([]byte, []Note) {
	s := string(src)
	var b strings.Builder
	var notes []Note
	i := 0
	for {
		j := findClassAttr(s, i)
		if j < 0 {
			break
		}
		q, valStart, valEnd, end, ok := readAttrValue(s, j+len("class="))
		if !ok {
			b.WriteString(s[i : j+len("class=")])
			i = j + len("class=")
			continue
		}
		value := s[valStart:valEnd]
		if !strings.Contains(value, "rst-") {
			b.WriteString(s[i:end])
			i = end
			continue
		}
		repl, err := rewriteClassValue(value, q)
		if err != nil {
			notes = append(notes, Note{Line: 1 + strings.Count(s[:j], "\n"), Text: err.Error()})
			b.WriteString(s[i:end])
			i = end
			continue
		}
		// An attribute that translated to nothing at all takes its own
		// leading space with it, or the tag keeps a gap where a class
		// used to be.
		start := j
		if repl == "" {
			for start > i && (s[start-1] == ' ' || s[start-1] == '\t') {
				start--
			}
		}
		b.WriteString(s[i:start])
		b.WriteString(repl)
		i = end
	}
	b.WriteString(s[i:])
	out := toneInMarkup.ReplaceAllString(b.String(), "${1}rst-tone=${2}")
	return []byte(out), notes
}

// findClassAttr returns the index of the next class= that is a real
// attribute — one that follows whitespace, the way an attribute in a
// tag always does — rather than the tail of some longer word.
func findClassAttr(s string, from int) int {
	for i := from; ; {
		k := strings.Index(s[i:], "class=")
		if k < 0 {
			return -1
		}
		k += i
		if k == 0 || isSpace(s[k-1]) {
			return k
		}
		i = k + len("class=")
	}
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

// readAttrValue reads an attribute value that starts at k, in either of
// the two quotings this repository writes markup in: a plain "…", and
// the \"…\" of a Go interpreted string literal. It returns the quote it
// found (so the replacement is written in the same one), the value's
// bounds, and the index just past the closing quote.
func readAttrValue(s string, k int) (q string, valStart, valEnd, end int, ok bool) {
	switch {
	case k < len(s) && s[k] == '"':
		valStart = k + 1
		e := strings.IndexByte(s[valStart:], '"')
		if e < 0 {
			return "", 0, 0, 0, false
		}
		return `"`, valStart, valStart + e, valStart + e + 1, true
	case k+1 < len(s) && s[k] == '\\' && s[k+1] == '"':
		valStart = k + 2
		e := strings.Index(s[valStart:], `\"`)
		if e < 0 {
			return "", 0, 0, 0, false
		}
		return `\"`, valStart, valStart + e, valStart + e + 2, true
	}
	return "", 0, 0, 0, false
}

// attr is one attribute being built: its name, the variant tokens the
// literal class list gave it, and — for a class value that carries a
// template action — the expression that supplies the rest of its value.
type attr struct {
	name     string
	variants []string
	expr     string
}

func rewriteClassValue(value, q string) (string, error) {
	if !strings.Contains(value, "{{") {
		kept, attrs := splitClassTokens(strings.Fields(value))
		return render(kept, attrs, q), nil
	}
	return rewriteTemplatedClassValue(value, q)
}

// splitClassTokens sorts a literal class list into what stays in class
// and what becomes an attribute, keeping first-appearance order so the
// diff reads like the markup it replaces.
func splitClassTokens(tokens []string) (kept []string, attrs []*attr) {
	byName := map[string]*attr{}
	for _, t := range tokens {
		if !strings.HasPrefix(t, "rst-") {
			kept = append(kept, t)
			continue
		}
		name, variant, ok, drop := MigrateClass(t)
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
//	  ->  rst-badge="{{with .Tone}}{{.}}{{end}}"
//	class="rst-btn{{if .Danger}} rst-btn--danger{{else}} rst-btn--primary{{end}}"
//	  ->  rst-btn="{{if .Danger}}danger{{else}}primary{{end}}"
//
// The whole conditional moves inside the value, which is why one rule
// covers both an optional modifier and a choice between two: the
// branch that adds nothing yields an empty value, and [rst-badge]
// matches an empty value exactly as it matches a bare attribute.
//
// Anything else is left alone and reported. A class list whose
// structure this cannot read is a handful of lines for a human, and a
// wrong guess is a screen that renders unstyled.
func rewriteTemplatedClassValue(value, q string) (string, error) {
	head := value[:strings.Index(value, "{{")]
	rest := value[len(head):]
	kept, attrs := splitClassTokens(strings.Fields(head))

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

	name, _, ok, drop := MigrateClass(kind)
	if drop || !ok {
		return "", fmt.Errorf(`class=%q builds a modifier of %s, which does not migrate: left as it was`, value, kind)
	}
	expr := regexp.MustCompile(`\s*`+regexp.QuoteMeta(kind+"--")).ReplaceAllString(rest, "")

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
	a.expr = expr
	return render(kept, attrs, q), nil
}
