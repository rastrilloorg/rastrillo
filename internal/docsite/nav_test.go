package docsite

import "testing"

// root is the corpus, relative to this package.
const root = "../../docs/site"

// TestNavAndFilesAgreeBothWays is the gate that keeps the table of
// contents honest in both directions. A page nav.json names must exist
// (Load fails outright on that one), and a page on disk that no nav
// entry reaches must not: an unreachable page is worse than a missing
// one, because it looks written and is never read.
func TestNavAndFilesAgreeBothWays(t *testing.T) {
	site, err := Load(root)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	seen := map[string]bool{}
	for _, p := range site.Pages() {
		if seen[p.Slug] {
			t.Errorf("%s: appears in nav.json more than once", p.Slug)
		}
		seen[p.Slug] = true

		if p.Title == "" {
			t.Errorf("%s: no `# ` title", p.Slug)
		}
		if p != site.Index && p.Blurb == "" {
			t.Errorf("%s: no blurb in nav.json (it renders on the /docs index)", p.Slug)
		}
		if p != site.Index && p.Label == "" {
			t.Errorf("%s: no label in nav.json", p.Slug)
		}
	}

	files, err := MarkdownFiles(root)
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	for _, slug := range files {
		if !seen[slug] {
			t.Errorf("docs/site/%s.md exists but no nav.json entry reaches it", slug)
		}
	}
}
