package keyring

import (
	"bytes"
	"testing"

	"github.com/carlosframework/rastrillo/crypto"
)

func TestWrapSeedRoundTrip(t *testing.T) {
	r := Ring{Namespace: "kass"}
	seed, err := NewSeed()
	if err != nil {
		t.Fatalf("NewSeed: %v", err)
	}
	prf, err := crypto.NewKey() // 32 random bytes stand in for a PRF output
	if err != nil {
		t.Fatalf("NewKey: %v", err)
	}

	wrapped, err := r.WrapSeed(prf, seed)
	if err != nil {
		t.Fatalf("WrapSeed: %v", err)
	}
	// SealSym's wire format: iv(12) ‖ ct(len(seed) + 16-byte GCM tag).
	if want := 12 + len(seed) + 16; len(wrapped) != want {
		t.Fatalf("wrapped length = %d, want %d", len(wrapped), want)
	}

	got, err := r.UnwrapSeed(prf, wrapped)
	if err != nil {
		t.Fatalf("UnwrapSeed: %v", err)
	}
	if !bytes.Equal(got, seed) {
		t.Fatal("UnwrapSeed round trip lost the seed")
	}
}

func TestUnwrapSeedWrongPRF(t *testing.T) {
	r := Ring{Namespace: "kass"}
	seed, err := NewSeed()
	if err != nil {
		t.Fatalf("NewSeed: %v", err)
	}
	prf, err := crypto.NewKey()
	if err != nil {
		t.Fatalf("NewKey: %v", err)
	}
	wrong, err := crypto.NewKey()
	if err != nil {
		t.Fatalf("NewKey: %v", err)
	}

	wrapped, err := r.WrapSeed(prf, seed)
	if err != nil {
		t.Fatalf("WrapSeed: %v", err)
	}
	if _, err := r.UnwrapSeed(wrong, wrapped); err == nil {
		t.Fatal("UnwrapSeed succeeded with the wrong credential's PRF output")
	}
}

func TestDeviceAddIsTheSameSeedUnderANewWrap(t *testing.T) {
	r := Ring{Namespace: "kass"}
	seed, err := NewSeed()
	if err != nil {
		t.Fatalf("NewSeed: %v", err)
	}
	prfA, err := crypto.NewKey()
	if err != nil {
		t.Fatalf("NewKey: %v", err)
	}
	prfB, err := crypto.NewKey()
	if err != nil {
		t.Fatalf("NewKey: %v", err)
	}

	wrapA, err := r.WrapSeed(prfA, seed)
	if err != nil {
		t.Fatalf("WrapSeed(A): %v", err)
	}
	wrapB, err := r.WrapSeed(prfB, seed)
	if err != nil {
		t.Fatalf("WrapSeed(B): %v", err)
	}
	if bytes.Equal(wrapA, wrapB) {
		t.Fatal("two wraps are byte-identical; the IV is not random")
	}
	for name, c := range map[string]struct {
		prf, wrapped []byte
	}{"A": {prfA, wrapA}, "B": {prfB, wrapB}} {
		got, err := r.UnwrapSeed(c.prf, c.wrapped)
		if err != nil {
			t.Fatalf("UnwrapSeed(%s): %v", name, err)
		}
		if !bytes.Equal(got, seed) {
			t.Fatalf("wrap %s did not unwrap to the same seed", name)
		}
	}
}

func TestWrapSeedIsNamespaced(t *testing.T) {
	seed, err := NewSeed()
	if err != nil {
		t.Fatalf("NewSeed: %v", err)
	}
	prf, err := crypto.NewKey()
	if err != nil {
		t.Fatalf("NewKey: %v", err)
	}
	wrapped, err := (Ring{Namespace: "kass"}).WrapSeed(prf, seed)
	if err != nil {
		t.Fatalf("WrapSeed: %v", err)
	}
	if _, err := (Ring{Namespace: "app"}).UnwrapSeed(prf, wrapped); err == nil {
		t.Fatal("a kass wrap unwrapped under the app namespace")
	}
}
