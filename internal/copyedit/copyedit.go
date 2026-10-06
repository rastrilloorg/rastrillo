// Package copyedit writes the operator's approved copy into the
// gallery's source and refuses anything else. Copy is approved by id in
// a copy-review result file; a translation is drafted from that
// English, so a translation whose English moved since is a translation
// of something else, and is refused rather than written.
package copyedit

import (
	"encoding/json"
	"fmt"
	"go/format"
	"os"
	"regexp"
	"sort"
	"strings"

	"amadan.net/rastrillo/rastrillo"
)

// Locales are the ones the gallery translates into: every locale the
// framework ships but en, which is the key. Derived rather than listed,
// because a locale the framework gains would otherwise be missing from
// every entry this writes, and nothing here would say so.
var Locales = func() []string {
	var out []string
	for _, l := range rastrillo.BaseLocales() {
		if l != "en" {
			out = append(out, l)
		}
	}
	return out
}()

// Entry is one new prose key: its copy-review id, the English its
// translations were drafted from, and the translations.
type Entry struct {
	ID string            `json:"id"`
	EN string            `json:"en"`
	TR map[string]string `json:"tr"`
}

// Edit is one edit file: the result files whose approvals it trusts,
// and the prose keys and ⟦id⟧ markers those approvals change.
type Edit struct {
	Approved []string `json:"approved"`
	Prose    string   `json:"prose"`
	Remove   []string `json:"remove"`
	Add      []Entry  `json:"add"`
	Fill     []string `json:"fill"`
}

// LoadApproved reads result files and returns id → approved text. A
// file whose action is not approve, or two files approving one id
// differently, is an error: neither is an approval.
func LoadApproved(paths []string) (map[string]string, error) {
	out := map[string]string{}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		var r struct {
			Action  string `json:"action"`
			Strings []struct {
				ID   string `json:"id"`
				Text string `json:"text"`
			} `json:"strings"`
		}
		if err := json.Unmarshal(b, &r); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		if r.Action != "approve" {
			return nil, fmt.Errorf("%s: action is %q, not approve", p, r.Action)
		}
		for _, s := range r.Strings {
			if was, ok := out[s.ID]; ok && was != s.Text {
				return nil, fmt.Errorf("%s: %s approved twice with different text", p, s.ID)
			}
			out[s.ID] = s.Text
		}
	}
	return out, nil
}

// placeholder matches any brace-delimited name, not only lower-case
// ones, so a translation that drops {Name} or {count_2} is caught too.
var placeholder = regexp.MustCompile(`\{[^{}\s]+\}`)

func placeholders(s string) string {
	ps := placeholder.FindAllString(s, -1)
	sort.Strings(ps)
	return strings.Join(ps, " ")
}

// clean is the rule every written string keeps: no em dash, which the
// operator does not want in copy, and no backtick, which would end the
// raw string literal prose.go writes every entry as.
func clean(where, s string) error {
	if strings.Contains(s, "—") || strings.Contains(s, "`") {
		return fmt.Errorf("%s: %q carries an em dash or a backtick", where, s)
	}
	return nil
}

// ApplyProse removes keys from prose.go's source and adds entries before
// the map's closing brace, then formats it.
func ApplyProse(src string, approved map[string]string, remove []string, add []Entry) (string, error) {
	for _, key := range remove {
		start := strings.Index(src, "\t`"+key+"`: {\n")
		if start < 0 {
			return "", fmt.Errorf("prose.go has no key %q to remove", key)
		}
		end := strings.Index(src[start:], "\n\t},\n")
		if end < 0 {
			return "", fmt.Errorf("prose.go's entry for %q never closes", key)
		}
		src = src[:start] + src[start+end+len("\n\t},\n"):]
	}
	var b strings.Builder
	ids, texts := map[string]bool{}, map[string]bool{}
	for _, e := range add {
		// gofmt accepts a duplicate map key and only the compiler refuses
		// it, so a repeat here would be written before anything noticed.
		if ids[e.ID] {
			return "", fmt.Errorf("%s: added twice in one edit", e.ID)
		}
		ids[e.ID] = true
		text, ok := approved[e.ID]
		if !ok {
			return "", fmt.Errorf("%s: no approved text", e.ID)
		}
		if text != e.EN {
			return "", fmt.Errorf("%s: approved %q, but the translations were drafted from %q; redraft them from the approved text", e.ID, text, e.EN)
		}
		if strings.Contains(src, "\t`"+text+"`: {\n") {
			return "", fmt.Errorf("%s: prose.go already has %q", e.ID, text)
		}
		if texts[text] {
			return "", fmt.Errorf("%s: %q is already added by another id in this edit", e.ID, text)
		}
		texts[text] = true
		if err := clean(e.ID, text); err != nil {
			return "", err
		}
		if len(e.TR) != len(Locales) {
			return "", fmt.Errorf("%s: %d translations, want %d", e.ID, len(e.TR), len(Locales))
		}
		b.WriteString("\t`" + text + "`: {\n")
		for _, l := range Locales {
			t, ok := e.TR[l]
			if !ok || strings.TrimSpace(t) == "" {
				return "", fmt.Errorf("%s: no %s translation", e.ID, l)
			}
			if err := clean(e.ID+" "+l, t); err != nil {
				return "", err
			}
			if placeholders(t) != placeholders(text) {
				return "", fmt.Errorf("%s %s: placeholders %q, want %q", e.ID, l, placeholders(t), placeholders(text))
			}
			b.WriteString("\t\t`" + l + "`: `" + t + "`,\n")
		}
		b.WriteString("\t},\n")
	}
	tail := strings.LastIndex(src, "\n}\n")
	if tail < 0 {
		return "", fmt.Errorf("prose.go does not end with the map's closing brace")
	}
	out, err := format.Source([]byte(src[:tail+1] + b.String() + src[tail+1:]))
	return string(out), err
}

var marker = regexp.MustCompile(`⟦([a-z0-9_.]+)⟧`)

// Fill replaces every ⟦id⟧ marker in a source file with the approved
// text, and reports how many it replaced. The text lands inside Go
// string literals of either kind, so a quote, a backslash or a backtick
// is refused rather than escaped: copy is written as approved or not
// at all. A bracket left over afterwards is a marker the id pattern
// could not read (a hyphen, a capital, a space, a missing close), and
// is an error: left alone it would ship to the page as literal text.
func Fill(src string, approved map[string]string) (string, int, error) {
	var err error
	n := 0
	out := marker.ReplaceAllStringFunc(src, func(m string) string {
		id := marker.FindStringSubmatch(m)[1]
		text, ok := approved[id]
		switch {
		case !ok:
			err = fmt.Errorf("%s: no approved text", id)
		case strings.ContainsAny(text, "\"\\"):
			err = fmt.Errorf("%s: %q cannot sit in a Go string literal unescaped", id, text)
		default:
			if e := clean(id, text); e != nil {
				err = e
			}
		}
		n++
		return text
	})
	if err != nil {
		return "", n, err
	}
	if i := strings.IndexAny(out, "⟦⟧"); i >= 0 {
		return "", n, fmt.Errorf("unreadable marker %q", leftover(out[i:]))
	}
	return out, n, nil
}

// leftover is the text of an unreadable marker: through its closing
// bracket if it has one on the same line, else to the end of the line.
func leftover(s string) string {
	if nl := strings.IndexByte(s, '\n'); nl >= 0 {
		s = s[:nl]
	}
	if end := strings.Index(s[len("⟦"):], "⟧"); end >= 0 && strings.HasPrefix(s, "⟦") {
		return s[:len("⟦")+end+len("⟧")]
	}
	return s
}
