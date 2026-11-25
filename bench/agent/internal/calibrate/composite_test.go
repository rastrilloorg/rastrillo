package calibrate

import (
	"testing"
)

const tangled = `func Price(sizes []int, member bool) int {
	total := 0
	for _, size := range sizes {
		if size > 10 {
			if member {
				total += size / 2
			} else {
				total += size
			}
		}
	}
	return total
}
`

const padded = `// Price adds up what the dogs cost.
func Price(sizes []int, member bool) int {

	total := 0 // nothing yet

	// Each dog.
	for _, size := range sizes {

		if size > 10 {
			// Members pay half.
			if member {
				total += size / 2

			} else {
				total += size
			}
		}

	}
	return total
}
`

func TestPaddingDoesNotChangeTheComposite(t *testing.T) {
	plainDir, plainFuncs := measured(t, map[string]string{
		"price.go": source(tangled)})
	paddedDir, paddedFuncs := measured(t, map[string]string{
		"price.go": source(padded)})

	plain, _ := Cut(plainDir, []string{"price.go:Price"}, plainFuncs)
	roomy, _ := Cut(paddedDir, []string{"price.go:Price"}, paddedFuncs)

	if Composite(plain, Even) == 0 {
		t.Fatal("a tangled function scores 0")
	}
	if Composite(plain, Even) != Composite(roomy, Even) {
		t.Errorf("padding moved the composite from %v to %v",
			Composite(plain, Even), Composite(roomy, Even))
	}
	if roomy.Lines() <= plain.Lines() {
		t.Errorf("the padded excerpt is %d lines, the plain one %d",
			roomy.Lines(), plain.Lines())
	}
}

const inline = `func Receipt(name string, sizes []int) string {
	total := 0
	for _, size := range sizes {
		if size > 10 {
			total += size
		}
	}
	greeting := "Thank you, " + name
	greeting += "!"
	return greeting + " " + string(rune('0'+total%10))
}
`

const extracted = `func Receipt(name string, sizes []int) string {
	total := 0
	for _, size := range sizes {
		if size > 10 {
			total += size
		}
	}
	return greet(name) + " " + string(rune('0'+total%10))
}

func greet(name string) string {
	greeting := "Thank you, " + name
	greeting += "!"
	return greeting
}
`

// Averaging over functions, or over lines, would let a branchless
// helper dilute the tangled function it was taken from.
func TestExtractingATrivialHelperDoesNotLowerTheComposite(t *testing.T) {
	oneDir, oneFuncs := measured(t, map[string]string{
		"receipt.go": source(inline)})
	twoDir, twoFuncs := measured(t, map[string]string{
		"receipt.go": source(extracted)})

	whole, _ := Cut(oneDir, []string{"receipt.go:Receipt"}, oneFuncs)
	split, _ := Cut(twoDir,
		[]string{"receipt.go:Receipt", "receipt.go:greet"}, twoFuncs)

	if len(split.Funcs) != 2 {
		t.Fatalf("the split excerpt has %d functions", len(split.Funcs))
	}
	if Composite(split, Even) < Composite(whole, Even) {
		t.Errorf("extracting a helper lowered the composite from %v to %v",
			Composite(whole, Even), Composite(split, Even))
	}
}

func TestTheCompositeWeighsEachCount(t *testing.T) {
	excerpt := Excerpt{Funcs: excerptFuncs(
		[5]int{3, 2, 1, 0, 4}, [5]int{1, 1, 0, 2, 0})}
	weights := Weights{Cognitive: 1, Nesting: 2, OverLimit: 3,
		Duplicated: 0, FarShortNames: 0.5}

	// cognitive 4, nesting 3, over-limit 1, duplicated 2, far names 4.
	if got := Composite(excerpt, weights); got != 4+6+3+0+2 {
		t.Errorf("composite = %v, want 15", got)
	}
}
