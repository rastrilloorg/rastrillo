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
	// An --auth=magic scaffold's auth.go: the owned seam and an example
	// page through it.
	"internal/app/auth.go": "package app\n\nfunc (a *app) owned(r *http.Request) *gorm.DB { return scope.OwnedBy(a.db.G, \"owner\", ownerOf(r)) }\nfunc (a *app) account() { a.owned(r).Count(&n) }\n",
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
		// Round 6: a regex after return, a map literal carrying Scheme,
		// and a template with the .htm extension.
		{"scheme call after a regex following return", "scheme", map[string]string{
			"internal/app/static/u.js": "function isURL(s) { return /https?:\\/\\//.test(s); } localStorage.setItem(\"theme\", \"dark\");\n",
		}, false},
		{"scheme comment after a regex following return", "scheme", map[string]string{
			"internal/app/static/u.js": "function q(s) { return /\"/.test(s); } // do not use localStorage\n",
		}, true},
		{"scheme passed beside Base, as the scaffold does", "scheme",
			map[string]string{"internal/app/render.go": schemeBesideBase},
			true},
		{"scheme passed in a map literal", "scheme", map[string]string{
			"internal/app/render.go": "package app\n\nfunc render() { page := map[string]any{\"Scheme\": req.Scheme} }\n",
		}, true},
		{"photos in a .htm template", "media-grid", map[string]string{
			"internal/app/templates/photos.htm": "{{template \"media-grid\" .}}\n",
			"internal/app/handlers.go":          "package app\n\nfunc store() { s := blobs.Dir(root) }\n",
		}, true},
		// Round 7: division by a property named like a keyword, comments
		// inside a template's style and script, and a raw-string key.
		{"scheme comment after division by .in", "scheme", map[string]string{
			"internal/app/static/s.js": "const half = stats.in / 2; // do not use localStorage\n",
		}, true},
		{"scheme passed with a raw-string key", "scheme", map[string]string{
			"internal/app/render.go": "package app\n\nfunc render() { page := map[string]any{`Scheme`: req.Scheme} }\n",
		}, true},
		// Round 8: an upload field's accept="image/*" is markup, not a
		// comment; a raw-string key in an assignment; and a space before
		// a property named like a keyword.
		{"photos after an image/* upload field", "media-grid", map[string]string{
			"internal/app/templates/photos.html": "<input type=\"file\" accept=\"image/*\">\n{{template \"media-grid\" .}}\n",
			"internal/app/handlers.go":           "package app\n\nfunc store() { s := blobs.Dir(root) }\n",
		}, true},
		{"photos grid hand-made after an image/* field", "media-grid", map[string]string{
			"internal/app/templates/photos.html": "{{template \"media-grid\" .}}\n<input type=\"file\" accept=\"image/*\">\n<style>.g { grid-template-columns: repeat(4, 1fr) }</style>\n",
			"internal/app/handlers.go":           "package app\n\nfunc store() { s := blobs.Dir(root) }\n",
		}, false},
		{"scheme assigned with a raw-string key", "scheme", map[string]string{
			"internal/app/render.go": "package app\n\nfunc render() { page[`Scheme`] = req.Scheme }\n",
		}, true},
		{"scheme comment after division by a spaced property", "scheme", map[string]string{
			"internal/app/static/s.js": "const half = stats. in / 2; // do not use localStorage\n",
		}, true},
		// Round 9: a spread before await is not a property access.
		{"scheme comment after a spread and a regex", "scheme", map[string]string{
			"internal/app/static/s.js": "async function items(s) { return [... await /\"/.exec(s)]; } // do not use localStorage\n",
		}, true},
		{"scheme comment after an unspaced spread and a regex", "scheme", map[string]string{
			"internal/app/static/s.js": "async function items(s) { return [...await /\"/.exec(s)]; } // do not use localStorage\n",
		}, true},
		{"scheme comment after division by a variable named of", "scheme", map[string]string{
			"internal/app/static/s.js": "const of = 8; const options = {...of / 2}; // do not use localStorage\n",
		}, true},
		// An app made with --auth=magic already has the owned seam; an
		// agent scopes through it rather than calling scope itself.
		{"expenses through the scaffold's owned seam", "expenses", map[string]string{
			"internal/app/handlers.go": "package app\n\nfunc list() { q := a.owned(r).Find(&es); f := form.Field{Kind: form.Money} }\n",
		}, true},
		{"expenses manifest replacing the scaffold's account page", "expenses", map[string]string{
			"internal/app/auth.go":   "package app\n\nfunc (a *app) owned(r *http.Request) *gorm.DB { return scope.OwnedBy(a.db.G, \"owner\", ownerOf(r)) }\n",
			"manifest/expenses.toml": "scope = \"user\"\n[[fields]]\nkind = \"money\"\n",
		}, true},
		{"expenses handler added beside the seam in auth.go", "expenses", map[string]string{
			"internal/app/auth.go": "package app\n\nfunc (a *app) owned(r *http.Request) *gorm.DB { return scope.OwnedBy(a.db.G, \"owner\", ownerOf(r)) }\nfunc (a *app) account() { a.owned(r).Count(&n) }\nfunc (a *app) expenses() { a.owned(r).Find(&es); f := form.Field{Kind: form.Money} }\n",
		}, true},
		{"expenses only removing the account page", "expenses", map[string]string{
			"internal/app/auth.go":     "package app\n\nfunc (a *app) owned(r *http.Request) *gorm.DB { return scope.OwnedBy(a.db.G, \"owner\", ownerOf(r)) }\n",
			"internal/app/handlers.go": "package app\n\nfunc list() { q := db.Find(&es); f := form.Field{Kind: form.Money} }\n",
		}, false},
		{"scheme data-theme traded for a toggle", "scheme", map[string]string{
			"internal/app/templates/layout.html": "<html>\n{{template \"scheme-menu\" .}}\n<script>localStorage.setItem('t','dark')</script>\n</html>\n",
		}, false},
		// The phone shells: three sections on a shell with an index,
		// and none behind a Menu button.
		{"sections on the topbar with its Menu page", "sections",
			sectionsOn(topbarLayout, viewIndex), true},
		{"sections on the sidebar, a second layout beside it", "sections",
			map[string]string{
				"internal/app/templates/layout.html": sidebarLayout,
				"internal/app/templates/stage.html":  stageLayout,
				"internal/app/templates/index.html":  trimmedViewIndex,
			}, true},
		{"sections behind the old Menu card", "sections",
			sectionsOn(menuCardLayout, viewIndex), false},
		{"sections under a back strip", "sections",
			sectionsOn(backStripLayout, viewIndex), false},
		{"sections with a hamburger of their own", "sections",
			sectionsOn(hamburgerLayout, viewIndex), false},
		{"sections on the column", "sections", map[string]string{
			"internal/app/templates/events.html": columnNav,
		}, false},
		{"sections whose index is only a comment", "sections",
			sectionsOn(sidebarLayout, commentedViewIndex), false},
		{"sections whose shell is only a selector", "sections",
			map[string]string{
				"internal/app/static/app.css":       sidebarSelector,
				"internal/app/templates/index.html": viewIndex,
			}, false},
		{"sections with the scheme control outside the card", "sections",
			sectionsOn(schemeOutsideCard, viewIndex), false},
		{"sections in .htm templates", "sections", map[string]string{
			"internal/app/templates/layout.htm": topbarLayout,
			"internal/app/templates/menu.htm":   viewIndex,
		}, true},
		{"sections with a spaced end tag, then a footer control", "sections",
			sectionsOn(spacedEndTag, viewIndex), false},
		{"sections whose card opens across lines", "sections",
			sectionsOn(multiLineCard, viewIndex), true},
		{"sections without the scheme control", "sections",
			sectionsOn(schemelessLayout, viewIndex), false},
		{"sections with a hand-rolled toggle", "sections",
			map[string]string{
				"internal/app/templates/layout.html": sidebarLayout,
				"internal/app/templates/index.html":  viewIndex,
				"internal/app/static/theme.js":       mediaToggle,
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
			// The test binary answers incard too: see incard.go's init.
			cmd.Env = append(os.Environ(), "SKILLEVAL_BASE_DIR="+base,
				"SKILLEVAL_BIN="+os.Args[0])
			out, err := cmd.CombinedOutput()
			if passed := err == nil; passed != tc.pass {
				t.Errorf("pass = %v, want %v\n%s", passed, tc.pass, out)
			}
		})
	}
}

// sectionsOn is a sections answer: the layout, and the index page that
// marks itself with view.
func sectionsOn(layout, index string) map[string]string {
	return map[string]string{
		"internal/app/templates/layout.html": layout,
		"internal/app/templates/menu.html":   index,
	}
}

// The sections task's layouts and pages, cut to what its check reads.
// The new account card is named rst-shell-menu, as the shipped layouts
// name it; the old Menu card carries the attribute.
const (
	topbarLayout = `<div rst-shell-topbar="{{block "view" .}}page{{end}}">
<details rst-dropdown rst-shell-account name="rst-shell-menu">
{{template "scheme-menu" .}}
</details>
</div>
`
	sidebarLayout = `<html data-theme="{{.Scheme}}">
<div rst-shell-sidebar="{{block "view" .}}page{{end}}">
<div rst-shell-rail-foot><details rst-shell-profile name="rst-shell-menu">
<summary>Account</summary>{{template "scheme-menu" .}}</details></div>
</div>
</html>
`
	schemeOutsideCard = `<div rst-shell-topbar="{{block "view" .}}page{{end}}">
<details rst-dropdown rst-shell-account name="rst-shell-menu">
<details rst-dropdown name="rst-menus"></details>
</details>
<footer>{{template "scheme-menu" .}}</footer>
</div>
`
	stageLayout = `<html data-theme="{{.Scheme}}">
<div rst-stage></div>
</html>
`
	menuCardLayout = `<div rst-shell-topbar>
<details rst-shell-menu name="rst-shell-menu">
<summary>{{icon "menu"}}Menu</summary>
{{template "scheme-menu" .}}
</details>
</div>
`
	backStripLayout = `<div rst-shell-console="{{block "view" .}}page{{end}}">
<div rst-shell-back><a href="/">Sections</a></div>
{{template "scheme-menu" .}}
</div>
`
	hamburgerLayout = `<div rst-shell-topbar="{{block "view" .}}page{{end}}">
<button>{{icon "menu"}}</button>
{{template "scheme-menu" .}}
</div>
`
	schemelessLayout = `<div rst-shell-sidebar="{{block "view" .}}page{{end}}">
</div>
`
	spacedEndTag = `<div rst-shell-topbar="{{block "view" .}}page{{end}}">
<details rst-dropdown rst-shell-account name="rst-shell-menu">
<summary>Account</summary>
</details >
<footer>{{template "scheme-menu" .}}</footer>
</div>
`
	multiLineCard = `<div rst-shell-console="{{block "view" .}}page{{end}}">
<details
  rst-dropdown rst-shell-account
  name="rst-shell-menu">
<summary>{{template "profile" .}}</summary>
<div rst-dropdown-menu>
{{block "scheme" .}}{{template "scheme-menu" dict "Card" true}}{{end}}
</div>
</details>
</div>
`
	viewIndex          = "{{define \"view\"}}index{{end}}\n"
	trimmedViewIndex   = "{{- define \"view\" -}} index {{- end}}\n"
	commentedViewIndex = "{{/* {{define \"view\"}}index{{end}} */}}\n"
	columnNav          = "<nav><a href=\"/events\">Events</a></nav>\n"
	sidebarSelector    = "[rst-shell-sidebar] { gap: 0; }\n"
	mediaToggle        = "matchMedia('(prefers-color-scheme: dark)')\n"
)

// schemeBesideBase is render.go's line from rastrillo new since the
// base path joined the page data.
const schemeBesideBase = `package app

func render() {
	values["Scheme"], values["Base"] = localized.Scheme, localized.Base
}
`
