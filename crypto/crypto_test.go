package crypto

import (
	"bytes"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
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

// TestMarshalKeypairWritesTwo32ByteScalars pins the stored shape: each
// scalar is 32 bytes, zero-padded, as D.FillBytes wrote it before
// PrivateKey.Bytes replaced it. D = 1 shows the padding.
func TestMarshalKeypairWritesTwo32ByteScalars(t *testing.T) {
	one := strings.Repeat("0", 62) + "01"
	kp, err := UnmarshalKeypair(
		[]byte(`{"sign_priv_d":"01","box_priv_d":"` + one + `"}`))
	if err != nil {
		t.Fatalf("UnmarshalKeypair: %v", err)
	}
	stored, err := MarshalKeypair(kp)
	if err != nil {
		t.Fatalf("MarshalKeypair: %v", err)
	}
	want := `{"sign_priv_d":"` + one + `","box_priv_d":"` + one + `"}`
	if string(stored) != want {
		t.Fatalf("MarshalKeypair wrote %s, want %s", stored, want)
	}
	generated, _ := Generate()
	stored, err = MarshalKeypair(generated)
	var scalars keypairJSON
	if err != nil || json.Unmarshal(stored, &scalars) != nil {
		t.Fatalf("MarshalKeypair wrote %s, %v", stored, err)
	}
	for name, encoded := range map[string]string{
		"sign_priv_d": scalars.SignPrivD, "box_priv_d": scalars.BoxPrivD,
	} {
		if scalar, err := hex.DecodeString(encoded); err != nil ||
			len(scalar) != 32 {
			t.Errorf("%s = %q, want 32 bytes in hex", name, encoded)
		}
	}
}

// TestMarshalKeypairRefusesWhatItCannotStore: a key on another curve,
// a missing key and a signing key with a nil field each get an error,
// not a panic and not a file UnmarshalKeypair would refuse or misread.
func TestMarshalKeypairRefusesWhatItCannotStore(t *testing.T) {
	valid, _ := Generate()
	p224, _ := ecdsa.GenerateKey(elliptic.P224(), rand.Reader)
	p384, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	x25519, _ := ecdh.X25519().GenerateKey(rand.Reader)
	withoutD, withoutX, withoutY := *valid.SignPriv, *valid.SignPriv,
		*valid.SignPriv
	//lint:ignore SA1019 incomplete keys are what this test needs
	withoutD.D, withoutX.X, withoutY.Y = nil, nil, nil
	box := valid.BoxPriv
	for name, kp := range map[string]*Keypair{
		"nil keypair":    nil,
		"empty keypair":  {},
		"no signing key": {BoxPriv: box},
		"no box key":     {SignPriv: valid.SignPriv},
		"zero signing":   {SignPriv: &ecdsa.PrivateKey{}, BoxPriv: box},
		"zero box": {
			SignPriv: valid.SignPriv, BoxPriv: &ecdh.PrivateKey{},
		},
		"signing nil D": {SignPriv: &withoutD, BoxPriv: box},
		"signing nil X": {SignPriv: &withoutX, BoxPriv: box},
		"signing nil Y": {SignPriv: &withoutY, BoxPriv: box},
		"signing P-224": {SignPriv: p224, BoxPriv: box},
		"signing P-384": {SignPriv: p384, BoxPriv: box},
		"box on X25519": {SignPriv: valid.SignPriv, BoxPriv: x25519},
	} {
		if stored, err := MarshalKeypair(kp); err == nil {
			t.Errorf("%s: MarshalKeypair wrote %s, want an error",
				name, stored)
		}
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

// TestUnmarshalKeypairAcceptsShortSignPrivD pins the leniency the
// cross-implementation contract depends on. MarshalKeypair always writes
// a 32-byte FillBytes scalar, but this package is the shared envelope
// implementation for amadan, keymail and seapointish — a sibling that
// hex-encodes its scalar with leading zeros stripped has always parsed
// here, because ScalarBaseMult accepts any length. ecdsa.ParseRawPrivateKey
// requires exactly the curve's byte size, so anything that reaches it must
// be left-padded first or roughly one key in 256 stops loading.
func TestUnmarshalKeypairAcceptsShortSignPrivD(t *testing.T) {
	const boxD = "0000000000000000000000000000000000000000000000000000000000000001"

	short, err := UnmarshalKeypair([]byte(`{"sign_priv_d":"01","box_priv_d":"` + boxD + `"}`))
	if err != nil {
		t.Fatalf("UnmarshalKeypair with a stripped sign_priv_d: %v", err)
	}
	padded, err := UnmarshalKeypair([]byte(`{"sign_priv_d":"` + strings.Repeat("0", 62) + `01","box_priv_d":"` + boxD + `"}`))
	if err != nil {
		t.Fatalf("UnmarshalKeypair with a padded sign_priv_d: %v", err)
	}
	if !bytes.Equal(short.SignPub(), padded.SignPub()) {
		t.Fatalf("stripped and padded sign_priv_d derived different public keys:\n stripped %x\n padded   %x",
			short.SignPub(), padded.SignPub())
	}
	one := append(make([]byte, 31), 1)
	scalar, err := short.SignPriv.Bytes()
	if err != nil || !bytes.Equal(scalar, one) {
		t.Fatalf("stripped sign_priv_d parsed to D = %x (%v), want 1",
			scalar, err)
	}
}
