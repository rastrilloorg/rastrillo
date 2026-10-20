package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// The scaffold's shape, as far as the checks look at it.
var checkBase = map[string]string{
	"internal/app/render.go":             "package app\n\nfunc render() { page[\"Scheme\"] = req.Scheme }\n",
	"internal/app/templates/layout.html": "<html data-theme=\"{{.Scheme}}\">\n{{template \"scheme-menu\" .}}\n</html>\n",
	"internal/app/handlers.go":           "package app\n",
	// Long block comments whose openings sit outside a three-line hunk.
	"internal/app/static/tokens.css": "/*\n tokens\n a\n b\n c\n d\n e\n f\n g\n end\n*/\n:root { --x: 1; }\n",
	"internal/app/doc.go":            "package app\n\n/*\nNotes.\na\nb\nc\nd\ne\nf\nend\n*/\n",
	// The real scaffold's manifest README shows the syntax it documents.
	"manifest/README.md": "Example:\n\n    scope = \"user\"\n    kind = \"money\"\n    kind = \"money\"\n",
}

// TestChecks runs each task's check against a commit holding only the
// change named, including the review's counter-examples: a TODO naming
// the feature passed every check, a comment saying not to use float64
// failed one, and editing the layout's existing data-theme line failed
// scheme's.
func TestChecks(t *testing.T) {
	cases := []struct {
		name, task string
		change     map[string]string
		pass       bool
	}{
		{"expenses by the framework", "expenses", map[string]string{
			"internal/app/handlers.go": "package app\n\n// do not use float64 for money\nfunc list() { q := scope.Owned(db, uid); f := form.Field{Kind: form.Money} }\n",
		}, true},
		{"expenses named in a TODO", "expenses", map[string]string{
			"internal/app/handlers.go": "package app\n\n// TODO: scope.Owned money.ParseDecimal form.Money\n",
		}, false},
		{"expenses with a float amount", "expenses", map[string]string{
			"internal/app/handlers.go": "package app\n\ntype E struct{ Amount float64 }\nfunc list() { q := scope.Owned(db, uid); f := form.Field{Kind: form.Money} }\n",
		}, false},
		{"export by the framework", "export-job", map[string]string{
			"internal/app/handlers.go": "package app\n\nfunc wire() { j := jobs.New(logger); h := jobs.NewHandlers(cfg) }\n",
		}, true},
		{"export named in a TODO", "export-job", map[string]string{
			"internal/app/handlers.go": "package app\n\n// TODO: jobs.NewHandlers\n",
		}, false},
		{"photos by the framework", "media-grid", map[string]string{
			"internal/app/templates/photos.html": "{{template \"media-grid\" .}}\n",
			"internal/app/handlers.go":           "package app\n\nfunc store() { s := blobs.Dir(root) }\n",
		}, true},
		{"photos on a hand-made grid", "media-grid", map[string]string{
			"internal/app/templates/photos.html": "{{template \"media-grid\" .}}\n<style>.g{grid-template-columns: repeat(4, 1fr)}</style>\n",
			"internal/app/handlers.go":           "package app\n\nfunc store() { s := blobs.Dir(root) }\n",
		}, false},
		{"scheme untouched", "scheme", nil, true},
		{"scheme layout line edited", "scheme", map[string]string{
			"internal/app/templates/layout.html": "<html class=\"app\" data-theme=\"{{.Scheme}}\">\n{{template \"scheme-menu\" .}}\n</html>\n",
		}, true},
		{"scheme hand-rolled toggle", "scheme", map[string]string{
			"internal/app/static/toggle.js": "localStorage.setItem('theme', 'dark')\n",
		}, false},
		{"scheme unwired from render", "scheme", map[string]string{
			"internal/app/render.go": "package app\n\nfunc render() {}\n",
		}, false},
		// Round 2: comments are a language's, not a line's first
		// characters; a CSS selector is not a comment; and one construct
		// must not cancel another.
		{"export named in a block comment", "export-job", map[string]string{
			"internal/app/handlers.go": "package app\n\n/*\nj := jobs.New(logger)\nh := jobs.NewHandlers(cfg)\n*/\n",
		}, false},
		{"expenses with a trailing comment", "expenses", map[string]string{
			"internal/app/handlers.go": "package app\n\nfunc list() { q := scope.Owned(db, uid); f := form.Field{Kind: form.Money} } // never use float64\n",
		}, true},
		{"photos grid under an id selector", "media-grid", map[string]string{
			"internal/app/templates/photos.html": "{{template \"media-grid\" .}}\n",
			"internal/app/handlers.go":           "package app\n\nfunc store() { s := blobs.Dir(root) }\n",
			"internal/app/static/photos.css":     "#photos { grid-template-columns: repeat(4, 1fr); }\n",
		}, false},
		{"scheme render assignment commented out", "scheme", map[string]string{
			"internal/app/render.go": "package app\n\nfunc render() { /* page[\"Scheme\"] = req.Scheme */ }\n// page[\"Scheme\"] = req.Scheme\n",
		}, false},
		{"scheme toggle on the data-theme line", "scheme", map[string]string{
			"internal/app/templates/layout.html": "<html data-theme=\"{{.Scheme}}\"><script>localStorage.setItem('t','dark')</script>\n{{template \"scheme-menu\" .}}\n</html>\n",
		}, false},
		// Round 3: comments whose opening lies outside the hunk, a URL
		// before a real comment, a string that looks like a comment, and
		// template comments that trim whitespace.
		{"scheme note inside an existing CSS comment", "scheme", map[string]string{
			"internal/app/static/tokens.css": "/*\n tokens\n a\n b\n c\n d\n e\n f\n Do not add localStorage toggles.\n g\n end\n*/\n:root { --x: 1; }\n",
		}, true},
		{"export named inside an existing block comment", "export-job", map[string]string{
			"internal/app/doc.go": "package app\n\n/*\nNotes.\na\nb\nc\nd\ne\nj := jobs.New(logger)\nh := jobs.NewHandlers(cfg)\nf\nend\n*/\n",
		}, false},
		{"export named after a URL", "export-job", map[string]string{
			"internal/app/handlers.go": "package app\n\nvar endpoint = \"https://example.com\" // TODO jobs.New(logger) jobs.NewHandlers(cfg)\n",
		}, false},
		{"export after a string holding /*", "export-job", map[string]string{
			"internal/app/handlers.go": "package app\n\nfunc wire() { accept := \"image/*\"; j := jobs.New(logger); h := jobs.NewHandlers(cfg) }\n",
		}, true},
		{"photos named in a trimming template comment", "media-grid", map[string]string{
			"internal/app/templates/photos.html": "{{- /* template \"media-grid\" .; blobs.Dir(root) */ -}}\n",
		}, false},
		// Round 4: a rename is not an addition, a file name git would
		// quote is still read, a tab may follow a trim marker, and a
		// JavaScript regex is not a string.
		{"scheme layout renamed", "scheme", map[string]string{
			"internal/app/templates/layout.html": "\x00delete",
			"internal/app/templates/shell.html":  "<html data-theme=\"{{.Scheme}}\">\n{{template \"scheme-menu\" .}}\n</html>\n",
		}, true},
		{"scheme toggle in a file named with an accent", "scheme", map[string]string{
			"internal/app/static/thème.js": "localStorage.setItem('t','dark')\n",
		}, false},
		{"photos named in a tab-trimmed template comment", "media-grid", map[string]string{
			"internal/app/templates/photos.html": "{{-\t/* template \"media-grid\" .; blobs.Dir(root) */ -}}\n",
		}, false},
		{"scheme comment after a JS regex holding a quote", "scheme", map[string]string{
			"internal/app/static/q.js": "const quoted = /\"/; // do not use localStorage\n",
		}, true},
		// Round 5: a binary file in the tree, documentation removed, a
		// test standing in for production code, a template comment
		// opened across lines, and division after a postfix ++.
		{"export beside a binary file", "export-job", map[string]string{
			"internal/app/static/logo.png": "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00",
			"internal/app/handlers.go":     "package app\n\nfunc wire() { j := jobs.New(logger); h := jobs.NewHandlers(cfg) }\n",
		}, true},
		{"expenses manifest replacing the README", "expenses", map[string]string{
			"manifest/README.md":     "\x00delete",
			"manifest/expenses.toml": "scope = \"user\"\n[[fields]]\nkind = \"money\"\n",
		}, true},
		{"scheme kept only by a test", "scheme", map[string]string{
			"internal/app/render.go":      "package app\n\nfunc render() {}\n",
			"internal/app/render_test.go": "package app\n\nfunc TestX() { page[\"Scheme\"] = \"dark\" }\n",
		}, false},
		{"photos named in a template comment opened across lines", "media-grid", map[string]string{
			"internal/app/templates/photos.html": "{{-\n/* template \"media-grid\" .; blobs.Dir(root) */ -}}\n",
		}, false},
		{"scheme comment after division by a postfix", "scheme", map[string]string{
			"internal/app/static/q.js": "let n = 4; const half = n++ / 2; // do not use localStorage\n",
		}, true},
		{"scheme data-theme traded for a toggle", "scheme", map[string]string{
			"internal/app/templates/layout.html": "<html>\n{{template \"scheme-menu\" .}}\n<script>localStorage.setItem('t','dark')</script>\n</html>\n",
		}, false},
	}
	tasks, err := filepath.Abs("tasks")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// The harness hands a check two trees: its own copy of the
			// scaffold, and the agent's final tree, with no git in either.
			base, work := t.TempDir(), t.TempDir()
			write := func(root string, files map[string]string) {
				for p, body := range files {
					p = filepath.Join(root, p)
					if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
						t.Fatal(err)
					}
					if body == "\x00delete" {
						os.Remove(p)
						continue
					}
					if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
						t.Fatal(err)
					}
				}
			}
			write(base, checkBase)
			write(work, checkBase)
			write(work, tc.change)
			cmd := exec.Command("sh", filepath.Join(tasks, tc.task+".check.sh"))
			cmd.Dir = work
			cmd.Env = append(os.Environ(), "SKILLEVAL_BASE_DIR="+base)
			out, err := cmd.CombinedOutput()
			if passed := err == nil; passed != tc.pass {
				t.Errorf("pass = %v, want %v\n%s", passed, tc.pass, out)
			}
		})
	}
}
