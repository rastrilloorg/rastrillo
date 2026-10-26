package pdf

import (
	"fmt"
	"strings"
	"testing"
)

// Distinct numbered words let the test prove that every source word
// reaches one visible text operation, even where cells have different
// lengths and the row continues over several sheets.
func TestOversizedTableRowsContinueWithinPagesAndRepeatHeaders(t *testing.T) {
	for _, total := range []bool{false, true} {
		t.Run(fmt.Sprintf("total=%t", total), func(t *testing.T) {
			words := make([]string, 850)
			for i := range words {
				words[i] = fmt.Sprintf("word%04d", i)
			}
			columns := []Column{{Title: "Narrative", Width: 2}, {Title: "Amount", Width: 1}}
			row := []Cell{{Text: strings.Join(words, " "), Bold: true}, {Text: "123.45"}}
			var rows [][]Cell
			var totalRow []Cell
			if total {
				totalRow = row
			} else {
				rows = [][]Cell{row}
			}

			f := newFlow()
			layoutTable(f, "", columns, rows, totalRow)
			pages := append(f.pages, f.cur)
			if len(pages) < 3 {
				t.Fatalf("oversized row stayed on %d pages", len(pages))
			}
			counts := map[string]int{}
			for n, page := range pages {
				header := false
				content := false
				for _, op := range page {
					if op.kind != 't' {
						continue
					}
					if op.y < marginT || op.y > pageH-marginB {
						t.Errorf("page %d text outside printable body at %.2f: %q", n, op.y, op.text)
					}
					if op.text == "Narrative" {
						header = true
					}
					if strings.Contains(op.text, "word") {
						content = true
					}
					for _, word := range strings.Fields(op.text) {
						counts[word]++
					}
				}
				if !content {
					t.Errorf("page %d has a header without row content", n)
				}
				if !header {
					t.Errorf("page %d lacks repeated header", n)
				}
			}
			for _, word := range words {
				if counts[word] != 1 {
					t.Errorf("%s appears %d times", word, counts[word])
				}
			}
			if counts["123.45"] != 1 {
				t.Errorf("amount printed %d times", counts["123.45"])
			}
			// The emitter must retain the final source token in the actual PDF,
			// not merely in the layout's intermediate operations.
			b := New(opts())
			b.Table("Long narrative", columns, rows, totalRow)
			bs, missing, err := b.Render()
			if err != nil || len(missing) != 0 {
				t.Fatalf("render: %v; missing %v", err, missing)
			}
			if !strings.Contains(string(bs), "word0849") {
				t.Fatal("last source word absent from PDF bytes")
			}
		})
	}
}
