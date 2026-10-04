// Package budgetsize holds each directory of a module to a line budget.
//
// It counts directories, not compiled packages: every .go file, whatever
// its build tags, and internal and external test files together. That is
// a deterministic proxy for how much an edit there makes the toolchain
// reread, and Tito Go's internal/instance (957k lines, 72s to link its
// test binary after a one-line edit) is what it is a proxy against.
package budgetsize

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"amadan.net/rastrillo/rastrillo/internal/budgetfile"
)

type Count struct {
	Dir          string
	Source, Test int
}

// Measure counts lines the way wc -l does: newlines, plus one for a
// final line without one.
func Measure(root string) ([]Count, error) {
	by := map[string]*Count{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p == root {
				return nil
			}
			name := d.Name()
			if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") ||
				name == "testdata" || name == "vendor" || name == "node_modules" {
				return filepath.SkipDir
			}
			// A nested module is its own budget: counting it into the
			// parent would charge the parent for code it does not build.
			if _, err := os.Stat(filepath.Join(p, "go.mod")); err == nil {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		lines := bytes.Count(b, []byte("\n"))
		if len(b) > 0 && b[len(b)-1] != '\n' {
			lines++
		}
		rel, err := filepath.Rel(root, filepath.Dir(p))
		if err != nil {
			return err
		}
		dir := filepath.ToSlash(rel)
		c := by[dir]
		if c == nil {
			c = &Count{Dir: dir}
			by[dir] = c
		}
		if strings.HasSuffix(p, "_test.go") {
			c.Test += lines
		} else {
			c.Source += lines
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	out := make([]Count, 0, len(by))
	for _, c := range by {
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Dir < out[j].Dir })
	return out, nil
}

// Check returns one message per violation. A record for a directory
// with no Go files is stale and fails here, the one place that sees the
// whole module.
func Check(counts []Count, f *budgetfile.File) []string {
	var msgs []string
	seen := map[string]bool{}
	for _, c := range counts {
		seen[c.Dir] = true
		rec := f.Sizes[c.Dir]
		msgs = append(msgs, judge(c.Dir, "source", c.Source, budgetfile.DefaultSource, rec.Source, rec.Line)...)
		msgs = append(msgs, judge(c.Dir, "test", c.Test, budgetfile.DefaultTest, rec.Test, rec.Line)...)
	}
	for dir, rec := range f.Sizes {
		if !seen[dir] {
			msgs = append(msgs, fmt.Sprintf("%s: line %d records a size exemption, but the directory has no Go files: delete the record", dir, rec.Line))
		}
	}
	sort.Strings(msgs)
	return msgs
}

// judge ratchets only a column the record states, so a directory over
// on one count alone can carry a record without the other count tripping
// the 60% rule.
func judge(dir, kind string, measured, def, stated, line int) []string {
	if stated == 0 {
		if measured > def {
			return []string{fmt.Sprintf("%s: %d %s lines, ceiling %d. Split it by feature (docs/site/testing.md), or record an exemption with a reason in %s",
				dir, measured, kind, def, budgetfile.FileName)}
		}
		return nil
	}
	switch {
	case measured > stated:
		return []string{fmt.Sprintf("%s: %d %s lines, over the %d its exemption (line %d) allows", dir, measured, kind, stated, line)}
	case measured <= def:
		return []string{fmt.Sprintf("%s: %d %s lines no longer needs an exemption: write - in the %s column of line %d", dir, measured, kind, kind, line)}
	case measured*10 < stated*6:
		return []string{fmt.Sprintf("%s: measured %d %s lines, exemption says %d: lower it (line %d)", dir, measured, kind, stated, line)}
	}
	return nil
}
