package docsite

import "testing"

// TestInternalLinksResolve is the gate that stops a docs link 404ing.
//
// It checks the fragment as well as the page, because a link to a
// heading that has been renamed is the failure that actually happens:
// the page still exists, the link still looks right in review, and the
// reader lands at the top of a long page wondering what they missed.
func TestInternalLinksResolve(t *testing.T) {
	site, err := Load(root)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	for _, p := range site.Pages() {
		for _, l := range p.Links {
			target, ok := site.BySlug[l.Target]
			if !ok {
				t.Errorf("%s.md:%d: links to /docs/%s, which is not a page",
					p.Slug, l.Line, l.Target)
				continue
			}
			if l.Fragment == "" {
				continue
			}
			if !hasAnchor(target, l.Fragment) {
				t.Errorf("%s.md:%d: links to /docs/%s#%s, but %s has no heading anchoring %q",
					p.Slug, l.Line, l.Target, l.Fragment, l.Target, l.Fragment)
			}
		}
	}
}

func hasAnchor(p *Page, anchor string) bool {
	for _, h := range p.Headings {
		if h.Anchor == anchor {
			return true
		}
	}
	return false
}

// TestAnchorsAreUnique matters because a duplicate anchor makes a link
// to it ambiguous, and the renderer resolves the ambiguity by silently
// suffixing the second one — so the link goes to the wrong section
// rather than nowhere, which is harder to notice.
func TestAnchorsAreUnique(t *testing.T) {
	site, err := Load(root)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	for _, p := range site.Pages() {
		seen := map[string]string{}
		for _, h := range p.Headings {
			if h.Level == 1 {
				continue
			}
			if prev, dup := seen[h.Anchor]; dup {
				t.Errorf("%s.md: headings %q and %q both anchor to #%s",
					p.Slug, prev, h.Text, h.Anchor)
			}
			seen[h.Anchor] = h.Text
		}
	}
}

// TestAnchorSlugs pins the anchor rule itself. The website's slugify
// must agree with this exactly; check-anchors.mjs compares both against
// the fixture these cases also describe.
func TestAnchorSlugs(t *testing.T) {
	cases := map[string]string{
		"Getting started":          "getting-started",
		"The `db.Open` contract":   "the-db-open-contract",
		"404, never 403":           "404-never-403",
		"Sessions & identity":      "sessions-identity",
		"  Leading and trailing  ": "leading-and-trailing",
		"Hyphen-joined words":      "hyphen-joined-words",
		"`--allow-destructive`":    "allow-destructive",
	}
	for in, want := range cases {
		if got := Anchor(in); got != want {
			t.Errorf("Anchor(%q) = %q, want %q", in, got, want)
		}
	}
}
