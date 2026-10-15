package docsite

import (
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/internal/tmplmigrate"
)

// oldSyntaxOnPurpose names the pages that show the pre-localizer
// syntax deliberately, and how many of their html fences do. A count,
// not a blanket pass: a second old fence on the same page still fails,
// and an exemption the page no longer needs fails too, so it cannot
// outlive its reason.
var oldSyntaxOnPurpose = map[string]int{
	// The codemod's own page shows a template before and after it runs.
	"migrate-templates": 1,
}

// TestHTMLFencesUseTheLocalizer runs the template codemod over every
// html fence and fails on any call it would still rewrite.
//
// ui.Pages is strict: a partial reached without "L", or a locale
// function called without $L first, fails the page instead of
// rendering it. A docs example in the old syntax therefore teaches a
// reader to write a page that errors, and nothing else catches it,
// because the examples are never executed. Task 29's review found
// eight such calls the Task 28 rewrite had missed.
func TestHTMLFencesUseTheLocalizer(t *testing.T) {
	site, err := Load(root)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	seen := map[string]int{}
	for _, p := range site.Pages() {
		for _, f := range p.Fences {
			if f.Lang != "html" {
				continue
			}
			src, out, st, err := rewriteFence(f.Body)
			if err != nil {
				t.Errorf("%s.md:%d: html fence does not parse as a template: %v", p.Slug, f.Line, err)
				continue
			}
			if st.Calls+st.Dicts+st.Wrapped+len(st.NoData) == 0 {
				continue
			}
			seen[p.Slug]++
			if seen[p.Slug] <= oldSyntaxOnPurpose[p.Slug] {
				continue
			}
			// Insertions carry no newline, so line i of the rewrite is
			// line i of the fence; the body starts the line after the
			// opener. A line whose only change is the prelude names no
			// call, so it is not reported: the call is on another line.
			was, now := strings.Split(src, "\n"), strings.Split(out, "\n")
			for i := range was {
				if i < len(now) && was[i] != now[i] && strings.Replace(now[i], tmplmigrate.Prelude, "", 1) != was[i] {
					t.Errorf("%s.md:%d: old-syntax template call; strict pages fail on it. Write:\n%s",
						p.Slug, f.Line+1+i, strings.TrimPrefix(now[i], tmplmigrate.Prelude))
				}
			}
			for _, nd := range st.NoData {
				t.Errorf("%s.md:%d: %s", p.Slug, f.Line, nd)
			}
		}
	}
	for slug, want := range oldSyntaxOnPurpose {
		if seen[slug] < want {
			t.Errorf("%s.md: oldSyntaxOnPurpose allows %d old-syntax fences but the page has %d; lower the count", slug, want, seen[slug])
		}
	}
}

// rewriteFence runs the codemod on a fence, which is usually a
// fragment of a template body rather than a whole file. A fragment
// that uses $L inherits the prelude from the body it was cut from, and
// one that opens {{define}} may stop before its {{end}}; each framing
// supplies what the fragment leaves out, without adding a newline, so
// the rewrite's lines still line up with the fence's. It returns the
// framing that parsed and its rewrite.
func rewriteFence(body string) (src, out string, st tmplmigrate.Stats, err error) {
	var first error
	for _, src := range []string{
		body,
		tmplmigrate.Prelude + body,
		body + "{{end}}",
		tmplmigrate.Prelude + body + "{{end}}",
	} {
		out, st, err := tmplmigrate.Rewrite("fence", src)
		if err == nil {
			return src, out, st, nil
		}
		if first == nil {
			first = err
		}
	}
	return "", "", st, first
}
