package rastrillo

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/carlosframework/rastrillo/internal/markup"
)

// The markup vocabulary is attributes (design doc §6-v3), and the whole
// point of flipping it in one commit rather than gradually is that a
// corpus in two spellings teaches both. SKILL.md hands this vocabulary
// to an LLM; docs/site hands it to a person; the partials, the
// styleguide, the scaffold and the examples are what both of them copy.
// One class="rst-…" left behind anywhere in that corpus is a wrong
// answer waiting to be repeated.
//
// So: no class attribute in this repository may carry an rst- name,
// except the seven utilities, which keep class because cross-cutting
// styling is what class is for.
//
// This is the gate stage 3 will lean on too. When the class selectors
// come out of tokens.css, anything this test would have caught renders
// unstyled with nothing to say why.
func TestNoClassSpellingSurvives(t *testing.T) {
	classAttr := regexp.MustCompile(`class=\\?"([^"]*)"`)
	root := repoRoot(t)
	var found []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if spellingExempt[rel] || strings.HasPrefix(d.Name(), ".") && d.Name() != "." {
				return fs.SkipDir
			}
			return nil
		}
		if spellingExempt[rel] {
			return nil
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".html", ".go", ".js", ".md", ".css":
		default:
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		exempt := false
		for i, line := range strings.Split(string(src), "\n") {
			// A narrow, greppable exemption for a block whose subject IS
			// the old spelling — a fixture that has to be written in it,
			// inside a file that otherwise must not be. Whole-file
			// exemptions are the wrong tool for that: a browser drive is
			// a thousand lines of markup and four of them are the
			// fixture.
			if strings.Contains(line, oldSpellingBegin) {
				exempt = true
				continue
			}
			if strings.Contains(line, oldSpellingEnd) {
				exempt = false
				continue
			}
			if exempt {
				continue
			}
			for _, m := range classAttr.FindAllStringSubmatch(line, -1) {
				for _, token := range strings.Fields(m[1]) {
					if !strings.HasPrefix(token, "rst-") {
						continue
					}
					if _, util := markup.Utilities[token]; util {
						continue
					}
					found = append(found, rel+":"+itoa(i+1)+": "+token)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range found {
		t.Errorf("%s is still in the class spelling — run `rastrillo markup --fix` over it", f)
	}
	if len(found) > 0 {
		t.Logf("%d call site(s) left in the old spelling; a corpus in two spellings teaches both", len(found))
	}
}

// The fence is markup.FenceBegin/FenceEnd — one mechanism shared with
// `rastrillo markup`, so a repository has one thing to learn and one
// thing to grep for. Assembled from halves here so this file's own
// prose about them is not read as a fence.
var (
	oldSpellingBegin = markup.FenceBegin
	oldSpellingEnd   = markup.FenceEnd
)

// spellingExempt names the files that must keep class="rst-…" in them,
// and why. It is short on purpose: each entry is a place whose subject
// IS the old spelling.
var spellingExempt = map[string]bool{
	// The grammar itself, and its table of before/after pairs.
	"internal/markup": true,
	// Stage 1's two files and the page that documents the migration
	// carry the fence instead — one mechanism, shared with the codemod,
	// so a repository has one thing to learn and one thing to grep for.
	// The codemod's own fixture app, written in the spelling it converts.
	"cmd/rastrillo/markup_test.go": true,
	// tokens.css still carries a class selector beside every attribute
	// one until stage 3, and the examples vendor a copy of it. Neither
	// is markup.
	"ui/tokens.css":                      true,
	"examples/blog/static/tokens.css":    true,
	"examples/tickets/static/tokens.css": true,
	// The historical record: plans and specs written before the flip,
	// which describe what was true when they were written.
	"docs/superpowers": true,
	// The codemod's own explanation of what it converts, and this file.
	"cmd/rastrillo/markup.go": true,
	"markupspelling_test.go":  true,
}

// repoRoot walks up from the test's working directory to the module
// root, so this gate reads the whole corpus rather than one package.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the working directory")
		}
		dir = parent
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
