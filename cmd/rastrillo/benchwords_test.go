package main

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/ui"
)

// The agent benchmark (bench/agent) measures Rastrillo on a few app
// briefs. Tuning what an agent reads first to those apps would make the
// framework look better on the benchmark and no better anywhere else,
// so the words each brief lists may not appear in SKILL.md, skill/ or
// anything rastrillo new writes. docs/site and examples/ are not
// scanned: they mention salons and borrowing for their own reasons, and
// an agent reaches them only by looking.
func TestFrameworkCarriesNoBenchmarkWords(t *testing.T) {
	repo, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	words := benchmarkWords(t, repo)
	if len(words) == 0 {
		t.Fatal("no bench/agent/briefs/*/words.txt found")
	}
	pattern := regexp.MustCompile(
		`(?i)\b(` + strings.Join(words, "|") + `)s?\b`)

	scanForWords(t, pattern, filepath.Join(repo, "SKILL.md"), "SKILL.md")
	scanTreeForWords(t, pattern, filepath.Join(repo, "skill"), "skill")

	variants := [][]string{{"--auth=magic"}}
	for _, shell := range ui.LayoutNames() {
		variants = append(variants, []string{"--shell=" + shell})
	}
	for _, flags := range variants {
		t.Run(strings.Join(flags, " "), func(t *testing.T) {
			dir, err := runNewInTemp(t, append(flags, "app")...)
			if err != nil {
				t.Fatal(err)
			}
			scanTreeForWords(t, pattern, filepath.Join(dir, "app"),
				"rastrillo new "+strings.Join(flags, " "))
		})
	}
}

func benchmarkWords(t *testing.T, repo string) []string {
	t.Helper()
	lists, err := filepath.Glob(
		filepath.Join(repo, "bench/agent/briefs/*/words.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var words []string
	for _, list := range lists {
		raw, err := os.ReadFile(list)
		if err != nil {
			t.Fatal(err)
		}
		for _, word := range strings.Fields(string(raw)) {
			words = append(words, regexp.QuoteMeta(word))
		}
	}
	return words
}

func scanTreeForWords(
	t *testing.T, pattern *regexp.Regexp, root, label string,
) {
	t.Helper()
	scanFile := func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		relative, _ := filepath.Rel(root, path)
		scanForWords(t, pattern, path, label+": "+relative)
		return nil
	}
	err := filepath.WalkDir(root, scanFile)
	if err != nil {
		t.Fatal(err)
	}
}

func scanForWords(t *testing.T, pattern *regexp.Regexp, path, label string) {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(nil, 1<<24)
	for number := 1; scanner.Scan(); number++ {
		if found := pattern.FindString(scanner.Text()); found != "" {
			t.Errorf("%s:%d carries the benchmark word %q",
				label, number, found)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Errorf("%s: %v", label, err)
	}
}
