package money

import (
	"errors"
	"math/rand"
	"testing"
)

func TestAllocateProRataExactExample(t *testing.T) {
	total := New(100, "GBP")
	parts, err := AllocateProRata(total, []int64{1, 1, 1})
	if err != nil {
		t.Fatalf("AllocateProRata: %v", err)
	}
	sum := int64(0)
	for _, p := range parts {
		sum += p.Minor()
	}
	if sum != 100 {
		t.Fatalf("parts sum to %d, want 100", sum)
	}
	// Largest remainder, ties broken by index: 100/3 = 33.33 each,
	// remainder 1 each — the leftover 1 unit goes to index 0.
	want := []int64{34, 33, 33}
	for i, p := range parts {
		if p.Minor() != want[i] {
			t.Errorf("part[%d] = %d, want %d", i, p.Minor(), want[i])
		}
	}
}

func TestAllocateProRataZeroTotal(t *testing.T) {
	total := Zero("GBP")
	parts, err := AllocateProRata(total, []int64{1, 2, 3})
	if err != nil {
		t.Fatalf("AllocateProRata: %v", err)
	}
	for i, p := range parts {
		if !p.IsZero() {
			t.Errorf("part[%d] = %v, want zero", i, p)
		}
	}
}

func TestAllocateProRataZeroWeightSumNonZeroTotal(t *testing.T) {
	total := New(100, "GBP")
	if _, err := AllocateProRata(total, []int64{0, 0}); !errors.Is(err, ErrZeroWeightSum) {
		t.Fatalf("got %v, want ErrZeroWeightSum", err)
	}
}

func TestAllocateProRataNegativeWeight(t *testing.T) {
	total := New(100, "GBP")
	if _, err := AllocateProRata(total, []int64{1, -1}); !errors.Is(err, ErrNegativeWeight) {
		t.Fatalf("got %v, want ErrNegativeWeight", err)
	}
}

func TestAllocateProRataNoWeights(t *testing.T) {
	total := New(100, "GBP")
	if _, err := AllocateProRata(total, nil); !errors.Is(err, ErrNoWeights) {
		t.Fatalf("got %v, want ErrNoWeights", err)
	}
}

func TestAllocateProRataOneWeightGetsEverything(t *testing.T) {
	total := New(12345, "GBP")
	parts, err := AllocateProRata(total, []int64{7})
	if err != nil {
		t.Fatalf("AllocateProRata: %v", err)
	}
	if len(parts) != 1 || parts[0].Minor() != 12345 {
		t.Fatalf("got %v, want a single part of 12345", parts)
	}
}

// TestAllocateProRataProperty is the property test the brief asks
// for: over random totals and random non-negative weights, the parts
// always sum to exactly the total, and when the total is positive no
// part is ever negative.
func TestAllocateProRataProperty(t *testing.T) {
	rng := rand.New(rand.NewSource(20260822))
	for i := 0; i < 2000; i++ {
		n := 1 + rng.Intn(8)
		weights := make([]int64, n)
		anyPositive := false
		for j := range weights {
			weights[j] = rng.Int63n(1000)
			if weights[j] > 0 {
				anyPositive = true
			}
		}
		if !anyPositive {
			weights[0] = 1 // avoid the all-zero-weights error path here
		}
		totalMinor := rng.Int63n(2_000_000_000) - 1_000_000_000 // positive and negative
		total := New(totalMinor, "GBP")

		parts, err := AllocateProRata(total, weights)
		if err != nil {
			t.Fatalf("AllocateProRata(%d, %v): unexpected error %v", totalMinor, weights, err)
		}

		sum := int64(0)
		for _, p := range parts {
			sum += p.Minor()
			if totalMinor > 0 && p.Minor() < 0 {
				t.Fatalf("AllocateProRata(%d, %v): part %d is negative for a positive total", totalMinor, weights, p.Minor())
			}
		}
		if sum != totalMinor {
			t.Fatalf("AllocateProRata(%d, %v): parts sum to %d, want %d", totalMinor, weights, sum, totalMinor)
		}
	}
}

func TestAllocateProRataAllPartsZeroWeightGetNothing(t *testing.T) {
	// A weight of zero must never receive a part of the leftover
	// distribution ahead of a weight that actually earned a share.
	total := New(10, "GBP")
	parts, err := AllocateProRata(total, []int64{0, 1})
	if err != nil {
		t.Fatalf("AllocateProRata: %v", err)
	}
	if parts[0].Minor() != 0 {
		t.Fatalf("zero-weight part = %d, want 0", parts[0].Minor())
	}
	if parts[1].Minor() != 10 {
		t.Fatalf("weighted part = %d, want 10", parts[1].Minor())
	}
}
