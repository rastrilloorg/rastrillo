package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The scaffold's shape, as far as the checks look at it.
var checkBase = map[string]string{
	"internal/app/render.go":             "package app\n\nfunc render() { page[\"Scheme\"] = req.Scheme }\n",
	"internal/app/templates/layout.html": "<html data-theme=\"{{.Scheme}}\">\n{{template \"scheme-menu\" .}}\n</html>\n",
	"internal/app/handlers.go":           "package app\n",
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
			dir := t.TempDir()
			git := func(args ...string) string {
				t.Helper()
				cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@localhost", "-c", "commit.gpgsign=false", "-c", "core.hooksPath=/dev/null"}, args...)...)
				cmd.Dir = dir
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("git %v: %v\n%s", args, err, out)
				}
				return strings.TrimSpace(string(out))
			}
			write := func(files map[string]string) {
				for p, body := range files {
					p = filepath.Join(dir, p)
					if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
						t.Fatal(err)
					}
				}
			}
			git("init", "-q")
			write(checkBase)
			git("add", "-A")
			git("commit", "-qm", "scaffold")
			base := git("rev-parse", "HEAD")
			write(tc.change)
			cmd := exec.Command("sh", filepath.Join(tasks, tc.task+".check.sh"))
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "SKILLEVAL_BASE="+base)
			out, err := cmd.CombinedOutput()
			if passed := err == nil; passed != tc.pass {
				t.Errorf("pass = %v, want %v\n%s", passed, tc.pass, out)
			}
		})
	}
}
