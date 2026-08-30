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
		repl, err := rewriteClassValue(value, q, migrate)
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
// attribute rather than the tail of some longer word. An attribute
// usually follows whitespace, but a test's Go literal opens on one
// (`class="rst-field"`), so the rule is the general one: the byte
// before it is not part of an identifier. That keeps data-class= and
// superclass= out.
func findClassAttr(s string, from int) int {
	for i := from; ; {
		k := strings.Index(s[i:], "class=")
		if k < 0 {
			return -1
		}
		k += i
		if k == 0 || !isIdentByte(s[k-1]) {
			return k
		}
		i = k + len("class=")
	}
}

func isIdentByte(c byte) bool {
	return c == '-' || c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

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
