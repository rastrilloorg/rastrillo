package money

import (
	"errors"
	"math"
	"testing"
)

func TestAddSub(t *testing.T) {
	a := New(1000, "GBP")
	b := New(250, "GBP")

	sum, err := a.Add(b)
	if err != nil || sum.Minor() != 1250 {
		t.Fatalf("Add: got %v, %v; want 1250, nil", sum, err)
	}
	diff, err := a.Sub(b)
	if err != nil || diff.Minor() != 750 {
		t.Fatalf("Sub: got %v, %v; want 750, nil", diff, err)
	}
}

func TestAddSubCurrencyMismatch(t *testing.T) {
	a := New(100, "GBP")
	b := New(100, "USD")
	if _, err := a.Add(b); !errors.Is(err, ErrCurrencyMismatch) {
		t.Fatalf("Add across currencies: got %v, want ErrCurrencyMismatch", err)
	}
	if _, err := a.Sub(b); !errors.Is(err, ErrCurrencyMismatch) {
		t.Fatalf("Sub across currencies: got %v, want ErrCurrencyMismatch", err)
	}
	if _, err := a.Cmp(b); !errors.Is(err, ErrCurrencyMismatch) {
		t.Fatalf("Cmp across currencies: got %v, want ErrCurrencyMismatch", err)
	}
}

func TestAddSubCurrencyCaseIsNotFolded(t *testing.T) {
	// Amount does not normalise: "GBP" and "gbp" are, deliberately,
	// not the same currency to Add/Sub/Cmp. A caller that wants them
	// to match normalises before constructing an Amount.
	a := New(100, "GBP")
	b := New(100, "gbp")
	if _, err := a.Add(b); !errors.Is(err, ErrCurrencyMismatch) {
		t.Fatalf("Add across GBP/gbp: got %v, want ErrCurrencyMismatch", err)
	}
}

func TestAddOverflow(t *testing.T) {
	a := New(math.MaxInt64, "GBP")
	b := New(1, "GBP")
	if _, err := a.Add(b); !errors.Is(err, ErrOverflow) {
		t.Fatalf("Add overflow: got %v, want ErrOverflow", err)
	}
}

func TestSubOverflow(t *testing.T) {
	a := New(math.MinInt64, "GBP")
	b := New(1, "GBP")
	if _, err := a.Sub(b); !errors.Is(err, ErrOverflow) {
		t.Fatalf("Sub overflow: got %v, want ErrOverflow", err)
	}
}

func TestMulInt(t *testing.T) {
	a := New(100, "GBP")
	p, err := a.MulInt(3)
	if err != nil || p.Minor() != 300 {
		t.Fatalf("MulInt: got %v, %v; want 300, nil", p, err)
	}
}

func TestMulIntOverflow(t *testing.T) {
	a := New(math.MaxInt64/2+1, "GBP")
	if _, err := a.MulInt(2); !errors.Is(err, ErrOverflow) {
		t.Fatalf("MulInt overflow: got %v, want ErrOverflow", err)
	}
}

func TestMulIntMinInt64TimesMinusOne(t *testing.T) {
	a := New(math.MinInt64, "GBP")
	if _, err := a.MulInt(-1); !errors.Is(err, ErrOverflow) {
		t.Fatalf("MulInt(MinInt64, -1): got %v, want ErrOverflow", err)
	}
}

func TestNegMinInt64Panics(t *testing.T) {
	// Found by cross-provider review on this branch (gpt-6-luna, max
	// reasoning): math.MinInt64 is reachable through the public API
	// (New, or MulInt producing it) despite the old doc comment calling
	// it unreachable, and a naive -a.minor silently returns a.minor
	// unchanged — the wrong sign, with no error. Neg must fail loudly
	// instead.
	defer func() {
		if recover() == nil {
			t.Fatal("Neg(MinInt64) did not panic; want a panic rather than a silently wrong sign")
		}
	}()
	New(math.MinInt64, "GBP").Neg()
}

func TestNegAndZeroAndIsZero(t *testing.T) {
	z := Zero("GBP")
	if !z.IsZero() {
		t.Fatal("Zero should be IsZero")
	}
	a := New(500, "GBP")
	if a.Neg().Minor() != -500 {
		t.Fatalf("Neg: got %d, want -500", a.Neg().Minor())
	}
	if !a.Neg().Negative() {
		t.Fatal("Neg(500) should be Negative")
	}
	if a.Negative() {
		t.Fatal("500 should not be Negative")
	}
}

func TestCmp(t *testing.T) {
	a := New(100, "GBP")
	b := New(200, "GBP")
	if c, err := a.Cmp(b); err != nil || c != -1 {
		t.Fatalf("Cmp(100,200) = %d, %v; want -1, nil", c, err)
	}
	if c, err := b.Cmp(a); err != nil || c != 1 {
		t.Fatalf("Cmp(200,100) = %d, %v; want 1, nil", c, err)
	}
	if c, err := a.Cmp(a); err != nil || c != 0 {
		t.Fatalf("Cmp(100,100) = %d, %v; want 0, nil", c, err)
	}
}

func TestString(t *testing.T) {
	a := New(1234567, "GBP")
	if got, want := a.String(), "GBP 1234567"; got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
	neg := New(-500, "USD")
	if got, want := neg.String(), "USD -500"; got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}
