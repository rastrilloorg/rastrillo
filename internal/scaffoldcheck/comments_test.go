package scaffoldcheck

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The scaffold's comment limits: comment lines are at most this share
// of the lines measured, and no block is longer than maxBlock lines.
const (
	maxCommentShare = 0.15
	maxBlock        = 3
)

// directives are comment-shaped lines that are instructions to a tool,
// not prose, so they are not comment lines.
var directives = []string{"//go:", "//rastrillo:", "//lint:"}

// commentCount is what one app's measured Go files hold.
type commentCount struct {
	lines, comments int
	longBlocks      []string
}

// A new app's Go reads as code with short why-comments, on every shell:
// an agent read the whole 35 kB scaffold to learn its shape when a third
// of its lines were comments (discussion 159).
func TestNewAppsKeepTheirCommentsShort(t *testing.T) {
	t.Parallel()
	for _, variant := range everyShell() {
		directory := newApp(t, variant.name, variant.flags...)
		count, err := countComments(directory)
		if err != nil {
			t.Fatal(err)
		}
		share := float64(count.comments) / float64(count.lines)
		if share > maxCommentShare {
			t.Errorf("%s: %d of %d lines are comments (%.1f%%), "+
				"over %.0f%%", variant.name, count.comments, count.lines,
				100*share, 100*maxCommentShare)
		}
		for _, block := range count.longBlocks {
			t.Errorf("%s: %s", variant.name, block)
		}
	}
}

// countComments measures every .go file outside gen/ that is not marked
// //rastrillo:data.
func countComments(root string) (commentCount, error) {
	var count commentCount
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry,
		err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && path == filepath.Join(root, "gen") {
			return filepath.SkipDir
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		source, err := os.ReadFile(path)
		if err != nil || isData(string(source)) {
			return err
		}
		relative, _ := filepath.Rel(root, path)
		count.add(relative, string(source))
		return nil
	})
	return count, err
}

// isData is whether the file's header, above the package clause, marks
// it as a table of data.
func isData(source string) bool {
	header, _, _ := strings.Cut(source, "\npackage ")
	return strings.Contains(header, "//rastrillo:data ")
}

func (count *commentCount) add(path, source string) {
	lines := strings.Split(strings.TrimSuffix(source, "\n"), "\n")
	count.lines += len(lines)
	inside, block, start := false, 0, 0
	for number, line := range lines {
		var comment bool
		comment, inside = isComment(strings.TrimSpace(line), inside)
		if comment {
			count.comments++
			if block == 0 {
				start = number
			}
			block++
			continue
		}
		count.endBlock(path, lines, start, block)
		block = 0
	}
	count.endBlock(path, lines, start, block)
}

// isComment says whether a trimmed line is a comment line, and whether a
// /* comment is still open after it.
func isComment(line string, inside bool) (comment, stillInside bool) {
	switch {
	case inside:
		return true, !strings.Contains(line, "*/")
	case strings.HasPrefix(line, "/*"):
		return true, !strings.Contains(line, "*/")
	case !strings.HasPrefix(line, "//"):
		return false, false
	}
	for _, directive := range directives {
		if strings.HasPrefix(line, directive) {
			return false, false
		}
	}
	return true, false
}

// endBlock records a block over the limit, unless it is a licence or
// attribution, which says so in its first line.
func (count *commentCount) endBlock(
	path string, lines []string, start, length int,
) {
	if length <= maxBlock {
		return
	}
	first := strings.ToLower(lines[start])
	if strings.Contains(first, "licen") ||
		strings.Contains(first, "attribution") {
		return
	}
	count.longBlocks = append(count.longBlocks, fmt.Sprintf(
		"%s:%d: a %d-line comment, over %d: %s", path, start+1, length,
		maxBlock, strings.TrimSpace(lines[start])))
}
