package keyring

import (
	"bytes"
	"testing"

	"github.com/carlosframework/rastrillo/crypto"
)

func TestRingStrings(t *testing.T) {
	r := Ring{Namespace: "kass"}
	if got := r.PRFSalt(); got != "kass/prf/v1" {
		t.Fatalf("PRFSalt() = %q, want %q", got, "kass/prf/v1")
	}
}

func TestPurposeDerivation(t *testing.T) {
	r := Ring{Namespace: "kass"}
	seed := bytes.Repeat([]byte{0x42}, 32)
	if !bytes.Equal(r.ContentKey(seed), crypto.Derive(seed, "kass/content/v1")) {
		t.Fatal(`ContentKey is not Derive(seed, "kass/content/v1")`)
	}
	prf := bytes.Repeat([]byte{0x07}, 32)
	if !bytes.Equal(r.WrapKey(prf), crypto.Derive(prf, "kass/wrap/v1")) {
		t.Fatal(`WrapKey is not Derive(prf, "kass/wrap/v1")`)
	}
	if bytes.Equal(r.ContentKey(seed), r.WrapKey(seed)) {
		t.Fatal("content and wrap purposes are not domain-separated")
	}
	other := Ring{Namespace: "app"}
	if bytes.Equal(r.ContentKey(seed), other.ContentKey(seed)) {
		t.Fatal("two namespaces derived the same content key")
	}
}

func TestNewSeed(t *testing.T) {
	a, err := NewSeed()
	if err != nil {
		t.Fatalf("NewSeed: %v", err)
	}
	if len(a) != 32 {
		t.Fatalf("NewSeed length = %d, want 32", len(a))
	}
	b, err := NewSeed()
	if err != nil {
		t.Fatalf("NewSeed: %v", err)
	}
	if bytes.Equal(a, b) {
		t.Fatal("two seeds are identical; rand is broken")
	}
}
