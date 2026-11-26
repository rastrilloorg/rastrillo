package tour

import (
	"fmt"
	"slices"
	"strings"
)

// applyPatch applies a unified diff, as git diff writes it, to text. A
// hunk is found by its lines, whole lines only, nearest where its
// header says once earlier hunks' growth is counted, and never before
// the hunk applied ahead of it. So a scaffold that grew above the
// change still takes it, and a hunk whose lines are gone is an error:
// a step that no longer fits the scaffold fails the tour's test instead
// of reverting it.
func applyPatch(text, patch string) (string, error) {
	hunks, err := parseHunks(patch)
	if err != nil {
		return "", err
	}
	lines := strings.SplitAfter(text, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	moved, floor := 0, 0
	for index, hunk := range hunks {
		want := max(hunk.start-1+moved, floor)
		at := nearest(lines, hunk.before, want, floor)
		if at < 0 {
			return "", fmt.Errorf("hunk %d no longer matches the file; "+
				"it expected:\n%s", index+1, strings.Join(hunk.before, ""))
		}
		lines = slices.Replace(lines, at, at+len(hunk.before),
			hunk.after...)
		floor = at + len(hunk.after)
		moved = floor - (hunk.start - 1 + len(hunk.before))
	}
	return strings.Join(lines, ""), nil
}

// nearest is the index from floor on where lines holds before as whole
// lines, the one nearest want when there are several, or -1. A hunk
// with no lines before it is an insertion, made at want.
func nearest(lines, before []string, want, floor int) int {
	if len(before) == 0 {
		return min(want, len(lines))
	}
	best := -1
	for at := floor; at+len(before) <= len(lines); at++ {
		if !slices.Equal(lines[at:at+len(before)], before) {
			continue
		}
		if best < 0 || distance(at, want) < distance(best, want) {
			best = at
		}
	}
	return best
}

func distance(from, to int) int {
	if from > to {
		return from - to
	}
	return to - from
}

// hunk is one change: the lines it expects, context included, the lines
// that replace them, and where its header says it starts and how many
// lines each side holds. Each line keeps its newline; one without is
// the file's last.
type hunk struct {
	before, after           []string
	start, oldSize, newSize int
}

func parseHunks(patch string) ([]hunk, error) {
	var hunks []hunk
	last := byte(0) // the kind of the line read before this one
	for _, line := range strings.SplitAfter(patch, "\n") {
		switch {
		case line == "":
		case strings.HasPrefix(line, "@@"):
			next, err := header(line)
			if err != nil {
				return nil, err
			}
			hunks = append(hunks, next)
		case len(hunks) == 0 && isHeader(line):
		case len(hunks) == 0:
			return nil, fmt.Errorf("the patch has %q before its first @@",
				strings.TrimSpace(line))
		case strings.HasPrefix(line, `\`):
			// "\ No newline at end of file" ends the line before it.
			current := &hunks[len(hunks)-1]
			if last != '+' {
				trimLast(current.before)
			}
			if last != '-' {
				trimLast(current.after)
			}
		default:
			last = line[0]
			if err := hunks[len(hunks)-1].add(line); err != nil {
				return nil, err
			}
		}
	}
	return hunks, counted(hunks)
}

// header reads "@@ -start,size +start,size @@"; a size left out is 1.
func header(line string) (hunk, error) {
	var next hunk
	oldRange, newRange, found := strings.Cut(
		strings.TrimPrefix(line, "@@ -"), " +")
	newRange, _, _ = strings.Cut(newRange, " ")
	var newStart int
	if !found || !readRange(oldRange, &next.start, &next.oldSize) ||
		!readRange(newRange, &newStart, &next.newSize) {
		return hunk{}, fmt.Errorf("the patch has a hunk header it "+
			"cannot read: %q", strings.TrimSpace(line))
	}
	if next.oldSize == 0 {
		next.start++ // -N,0 inserts after line N
	}
	return next, nil
}

func readRange(text string, start, size *int) bool {
	*size = 1
	first, second, sized := strings.Cut(text, ",")
	if _, err := fmt.Sscanf(first, "%d", start); err != nil {
		return false
	}
	if sized {
		if _, err := fmt.Sscanf(second, "%d", size); err != nil {
			return false
		}
	}
	return true
}

// counted refuses a patch whose hunks hold other than the lines their
// headers promise: a cut-short patch would otherwise delete lines.
func counted(hunks []hunk) error {
	if len(hunks) == 0 {
		return fmt.Errorf("the patch has no hunks")
	}
	for index, hunk := range hunks {
		if len(hunk.before) != hunk.oldSize ||
			len(hunk.after) != hunk.newSize {
			return fmt.Errorf("hunk %d holds %d lines before and %d "+
				"after, but its header says %d and %d", index+1,
				len(hunk.before), len(hunk.after), hunk.oldSize,
				hunk.newSize)
		}
	}
	return nil
}

// add files one line of a hunk under before, after or both.
func (current *hunk) add(line string) error {
	body := line[1:]
	switch line[0] {
	case ' ':
		current.before = append(current.before, body)
		current.after = append(current.after, body)
	case '-':
		current.before = append(current.before, body)
	case '+':
		current.after = append(current.after, body)
	default:
		return fmt.Errorf("the patch has a line that is not context, - or "+
			"+: %q", strings.TrimSpace(line))
	}
	return nil
}

func trimLast(lines []string) {
	if len(lines) > 0 {
		lines[len(lines)-1] = strings.TrimSuffix(lines[len(lines)-1], "\n")
	}
}

// isHeader is a line git diff writes above the first hunk.
func isHeader(line string) bool {
	for _, prefix := range []string{"--- ", "+++ ", "diff ", "index "} {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}
