package crypto

import (
	"bytes"
	"strings"
	"testing"
)

func TestSealOpenRoundTrip(t *testing.T) {
	kp, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	plain := []byte("the family envelope, round-tripped")

	sealed, err := Seal(kp.BoxPub(), "test-v1", plain)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	got, err := Open(kp, "test-v1", sealed)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("Open = %q, want %q", got, plain)
	}
}

func TestOpenRejectsWrongContext(t *testing.T) {
	kp, _ := Generate()
	sealed, err := Seal(kp.BoxPub(), "context-a", []byte("hi"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if _, err := Open(kp, "context-b", sealed); err == nil {
		t.Fatal("Open with the wrong context succeeded; contexts must domain-separate")
	}
}

func TestOpenRejectsWrongRecipient(t *testing.T) {
	alice, _ := Generate()
	bob, _ := Generate()
	sealed, err := Seal(alice.BoxPub(), "test-v1", []byte("for alice"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if _, err := Open(bob, "test-v1", sealed); err == nil {
		t.Fatal("Open by the wrong recipient succeeded")
	}
}

func TestOpenRejectsTamper(t *testing.T) {
	kp, _ := Generate()
	sealed, err := Seal(kp.BoxPub(), "test-v1", []byte("payload"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	sealed[len(sealed)-1] ^= 0x01
	if _, err := Open(kp, "test-v1", sealed); err == nil {
		t.Fatal("Open of a tampered ciphertext succeeded")
	}
}

func TestOpenRejectsShortInput(t *testing.T) {
	kp, _ := Generate()
	if _, err := Open(kp, "test-v1", make([]byte, 40)); err == nil {
		t.Fatal("Open of a too-short blob succeeded")
	}
}

func TestSignVerifyRoundTrip(t *testing.T) {
	kp, _ := Generate()
	msg := []byte("attest me")
	sig, err := Sign(kp, "attest-v1", msg)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if len(sig) != 64 {
		t.Fatalf("Sign produced %d bytes, want 64 raw r‖s", len(sig))
	}
	if !Verify(kp.SignPub(), "attest-v1", msg, sig) {
		t.Fatal("Verify rejected a fresh signature")
	}
	if Verify(kp.SignPub(), "other-v1", msg, sig) {
		t.Fatal("Verify accepted a signature under the wrong context")
	}
	msg[0] ^= 0x01
	if Verify(kp.SignPub(), "attest-v1", msg, sig) {
		t.Fatal("Verify accepted a signature over a modified message")
	}
}

func TestVerifyRejectsMalformedInputs(t *testing.T) {
	kp, _ := Generate()
	sig, _ := Sign(kp, "c", []byte("m"))
	if Verify(kp.SignPub()[:64], "c", []byte("m"), sig) {
		t.Fatal("Verify accepted a truncated public key")
	}
	if Verify(kp.SignPub(), "c", []byte("m"), sig[:63]) {
		t.Fatal("Verify accepted a truncated signature")
	}
	notAPoint := make([]byte, 65)
	notAPoint[0] = 0x04
	if Verify(notAPoint, "c", []byte("m"), sig) {
		t.Fatal("Verify accepted bytes that are not a curve point")
	}
}

func TestMarshalKeypairRoundTrip(t *testing.T) {
	kp, _ := Generate()
	b, err := MarshalKeypair(kp)
	if err != nil {
		t.Fatalf("MarshalKeypair: %v", err)
	}
	kp2, err := UnmarshalKeypair(b)
	if err != nil {
		t.Fatalf("UnmarshalKeypair: %v", err)
	}
	if !bytes.Equal(kp.SignPub(), kp2.SignPub()) || !bytes.Equal(kp.BoxPub(), kp2.BoxPub()) {
		t.Fatal("keypair did not survive a marshal round trip")
	}
	// The round-tripped keypair must actually work, not just look right.
	sealed, _ := Seal(kp.BoxPub(), "rt", []byte("x"))
	if _, err := Open(kp2, "rt", sealed); err != nil {
		t.Fatalf("Open with round-tripped keypair: %v", err)
	}
}

func TestDeriveIsDeterministicAndSeparated(t *testing.T) {
	key, err := NewKey()
	if err != nil {
		t.Fatalf("NewKey: %v", err)
	}
	a1 := Derive(key, "purpose-a")
	a2 := Derive(key, "purpose-a")
	b := Derive(key, "purpose-b")
	if !bytes.Equal(a1, a2) {
		t.Fatal("Derive is not deterministic")
	}
	if bytes.Equal(a1, b) {
		t.Fatal("Derive does not separate contexts")
	}
	if len(a1) != 32 {
		t.Fatalf("Derive produced %d bytes, want 32", len(a1))
	}
}

func TestSealSymOpenSymRoundTrip(t *testing.T) {
	key, _ := NewKey()
	plain := []byte("symmetric half")
	sealed, err := SealSym(key, plain)
	if err != nil {
		t.Fatalf("SealSym: %v", err)
	}
	got, err := OpenSym(key, sealed)
	if err != nil {
		t.Fatalf("OpenSym: %v", err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("OpenSym = %q, want %q", got, plain)
	}
	sealed[len(sealed)-1] ^= 0x01
	if _, err := OpenSym(key, sealed); err == nil {
		t.Fatal("OpenSym of tampered ciphertext succeeded")
	}
	other, _ := NewKey()
	sealed[len(sealed)-1] ^= 0x01 // untamper
	if _, err := OpenSym(other, sealed); err == nil {
		t.Fatal("OpenSym under the wrong key succeeded")
	}
}

func TestErrorsCarryPackagePrefix(t *testing.T) {
	kp, _ := Generate()
	_, err := Open(kp, "c", []byte("short"))
	if err == nil || !strings.HasPrefix(err.Error(), "rastrillo/crypto:") {
		t.Fatalf("error %v does not carry the rastrillo/crypto: prefix", err)
	}
}
