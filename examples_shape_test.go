package rastrillo_test

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/ui"
)

// No example teaches the old phone shape: a Menu button in the bar, a
// back strip under it, the topbar's tail, the retired drawer, or the
// scheme dropdown of words. Old markup keeps rendering for the apps
// that have it, and its proof is kept where nobody copies from: the
// 0.37 layouts and the pre-index shells, named as legacy fixtures.
// Fiesta holds its own examples to the same rule.

// shapeTrees are what people and agents copy from: the example apps,
// the design-system pages, the styleguide's samples and the ui test
// pages.
var shapeTrees = []string{
	"examples", "internal/designsystem", "ui/styleguide.go", "ui/testdata",
}

// legacyFixtures are the only files in those trees that may keep the
// old markup, because they exist to prove it still renders.
var legacyFixtures = []string{
	"ui/testdata/layouts/*-0.37.html",
	"ui/testdata/legacy/*.html",
}

// oldShellAttributes are the old shells' own attributes. The account
// card's name="rst-shell-menu" is a value, which the new markup keeps,
// so it is not one of them.
var oldShellAttributes = []string{
	"rst-shell-back", "rst-shell-menu", "rst-shell-tail",
	"rst-shell-chrome",
}

// TestNoExampleCarriesTheOldShellMarkup walks the trees from the
// repository root, where go test runs this package.
func TestNoExampleCarriesTheOldShellMarkup(t *testing.T) {
	read := 0
	for _, tree := range shapeTrees {
		err := filepath.WalkDir(tree, func(file string, entry fs.DirEntry,
			err error) error {
			if err != nil || entry.IsDir() || !readsAsMarkup(file) {
				return err
			}
			file = filepath.ToSlash(file)
			if isLegacyFixture(file) {
				return nil
			}
			source, err := os.ReadFile(file)
			if err != nil {
				return err
			}
			read++
			text := string(source)
			if path.Ext(file) == ".go" {
				// A Go string escapes its quotes, which would make
				// every quoted value read as starting with a backslash.
				text = strings.ReplaceAll(text, `\"`, `"`)
			}
			for _, problem := range oldShape(text) {
				t.Errorf("%s writes %s; old markup belongs only "+
					"in the legacy fixtures", file, problem)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if read < 100 {
		t.Fatalf("read %d files, want at least 100; a walk this short "+
			"read the wrong tree", read)
	}
}

// readsAsMarkup says whether a file may hold markup someone copies: a
// template, a page, a Go file that writes one, a test fixture or
// prose. Stylesheets and scripts name the old attributes as selectors,
// which Fiesta keeps so old apps still draw.
func readsAsMarkup(file string) bool {
	switch path.Ext(file) {
	case ".html", ".go", ".tmpl", ".txt", ".md":
		return true
	}
	return false
}

func isLegacyFixture(file string) bool {
	return slices.ContainsFunc(legacyFixtures, func(pattern string) bool {
		matched, _ := path.Match(pattern, file)
		return matched
	})
}

// oldShape reads source's start tags as HTML does and names each old
// shell attribute, in either spelling, and each scheme control that
// is not the icon group.
func oldShape(source string) []string {
	var problems []string
	for _, attribute := range ui.Attributes(source) {
		switch {
		case slices.Contains(oldShellAttributes, attribute.Name):
			problems = append(problems, "the attribute "+attribute.Name)
		case attribute.Name == "rst-scheme" && attribute.Value != "icons":
			problems = append(problems,
				`the scheme dropdown; write [rst-scheme="icons"]`)
		case attribute.Name == "class":
			problems = append(problems, oldClasses(attribute.Value)...)
		}
	}
	return problems
}

// oldClasses names the old attributes' class spellings in a class list.
func oldClasses(list string) []string {
	var problems []string
	for _, class := range strings.Fields(list) {
		name, found := strings.CutPrefix(class, "rst-shell__")
		if found && slices.Contains(oldShellAttributes, "rst-shell-"+name) {
			problems = append(problems, "the class "+class)
		}
		if class == "rst-scheme" {
			problems = append(problems, "the scheme dropdown's class")
		}
	}
	return problems
}
