package crypto

import (
	"bytes"
	"crypto/ecdh"
	"crypto/elliptic"
	"encoding/base64"
	"math/big"
	"strings"
	"testing"
)

// fuzzKeypairJSON is fixed, as is fuzzKey, so a sealed blob or
// signature saved in the corpus still opens or verifies next run, and
// in every fuzz worker.
const fuzzKeypairJSON = `{"sign_priv_d":"` +
	"0909090909090909090909090909090909090909090909090909090909090909" +
	`","box_priv_d":"` +
	"0505050505050505050505050505050505050505050505050505050505050505" +
	`"}`

var fuzzKey = bytes.Repeat([]byte{3}, 32)

func fuzzKeypair(f *testing.F) *Keypair {
	f.Helper()
	keypair, err := UnmarshalKeypair([]byte(fuzzKeypairJSON))
	if err != nil {
		f.Fatal(err)
	}
	return keypair
}

// fixedSeal is Seal with its ephemeral key and IV fixed. Seal draws
// both at random, so each fuzz worker would seal a different blob and
// none could say which single blob may open.
func fixedSeal(f *testing.F, recipient *Keypair, plaintext []byte) []byte {
	f.Helper()
	ephemeral, err := ecdh.P256().NewPrivateKey(bytes.Repeat([]byte{4}, 32))
	if err != nil {
		f.Fatal(err)
	}
	shared, err := ephemeral.ECDH(recipient.BoxPriv.PublicKey())
	if err != nil {
		f.Fatal(err)
	}
	gcm, err := newGCM(Derive(shared, "fuzz-v1"))
	if err != nil {
		f.Fatal(err)
	}
	iv := make([]byte, ivLen)
	sealed := append(ephemeral.PublicKey().Bytes(), iv...)
	sealed = gcm.Seal(sealed, iv, plaintext, nil)
	if _, err := Open(recipient, "fuzz-v1", sealed); err != nil {
		f.Fatalf("Open refuses the fixed seal: %v", err)
	}
	return sealed
}

// negatedEphemeral swaps an envelope's ephemeral point (X, Y) for
// (X, p-Y). ECDH keeps only the shared X, so the result opens to the
// same plaintext: the envelope has two spellings, not one
// (discussion #419).
func negatedEphemeral(sealed []byte) []byte {
	prime := elliptic.P256().Params().P
	out := bytes.Clone(sealed)
	ordinate := new(big.Int).SetBytes(out[33:65])
	new(big.Int).Sub(prime, ordinate).FillBytes(out[33:65])
	return out
}

// FuzzOpen holds the envelope to what AES-GCM promises: of everything
// the fuzzer builds, only the one blob that was sealed opens, in either
// spelling of its ephemeral point, and only to what was sealed in it.
func FuzzOpen(f *testing.F) {
	keypair := fuzzKeypair(f)
	plaintext := []byte("sealed for the fuzzer")
	sealed := fixedSeal(f, keypair, plaintext)
	f.Add(sealed)
	f.Add(negatedEphemeral(sealed))
	f.Add(sealed[:pubKeyLen+ivLen])
	f.Add(sealed[:pubKeyLen+ivLen-1])
	f.Add(append(bytes.Clone(sealed), 0))
	f.Add(append(make([]byte, pubKeyLen), sealed[pubKeyLen:]...))
	f.Fuzz(func(t *testing.T, blob []byte) {
		opened, err := Open(keypair, "fuzz-v1", blob)
		if err != nil {
			return
		}
		same := bytes.Equal(blob, sealed) ||
			bytes.Equal(blob, negatedEphemeral(sealed))
		if !same || !bytes.Equal(opened, plaintext) {
			t.Fatalf("%x opened to %q; only %x was sealed",
				blob, opened, sealed)
		}
	})
}

// FuzzOpenSym is FuzzOpen for the symmetric half, reached through
// UnwrapKey as well, whose base64 is lax by design (padding, and the
// low bits of the last character), so its property is on the bytes
// the text decodes to rather than the text.
func FuzzOpenSym(f *testing.F) {
	plaintext := []byte("a content key")
	gcm, err := newGCM(fuzzKey)
	if err != nil {
		f.Fatal(err)
	}
	iv := make([]byte, ivLen)
	sealed := gcm.Seal(bytes.Clone(iv), iv, plaintext, nil)
	wrapped := base64.RawURLEncoding.EncodeToString(sealed)
	f.Add(wrapped)
	f.Add(string(sealed))
	f.Add(wrapped + "==")
	f.Add(wrapped[:len(wrapped)-1])
	f.Add(strings.Repeat("A", 16))
	f.Add("")
	f.Fuzz(func(t *testing.T, text string) {
		opened, err := UnwrapKey(fuzzKey, text)
		if err == nil {
			blob, _ := decodeB64url(text)
			if !bytes.Equal(blob, sealed) {
				t.Fatalf("%q unwrapped; only %q was wrapped", text, wrapped)
			}
		}
		if err == nil && !bytes.Equal(opened, plaintext) {
			t.Fatalf("%q unwrapped to %q", text, opened)
		}
		blob := []byte(text)
		_, err = OpenSym(fuzzKey, blob)
		if err == nil && !bytes.Equal(blob, sealed) {
			t.Fatalf("raw %x opened; only %x was sealed", blob, sealed)
		}
	})
}

// FuzzVerify checks a signature speaks for one message only. The
// signature bytes may vary (ECDSA's other s verifies too); the message
// and context may not.
func FuzzVerify(f *testing.F) {
	keypair := fuzzKeypair(f)
	message := []byte("signed for the fuzzer")
	signature, err := Sign(keypair, "fuzz-v1", message)
	if err != nil {
		f.Fatal(err)
	}
	f.Add("fuzz-v1", message, signature)
	f.Add("fuzz-v1\x00", message, signature)
	f.Add("fuzz", append([]byte("-v1\x00"), message...), signature)
	f.Add("fuzz-v1", message, make([]byte, 64))
	f.Add("fuzz-v1", message, signature[:63])
	f.Fuzz(func(t *testing.T, context string, got, gotSignature []byte) {
		if !Verify(keypair.SignPub(), context, got, gotSignature) {
			return
		}
		if context != "fuzz-v1" || !bytes.Equal(got, message) {
			t.Fatalf("a signature verified %q under %q; only %q under "+
				"fuzz-v1 was signed", got, context, message)
		}
	})
}

// FuzzUnmarshalKeypair reads stored key files. Whatever loads must be
// a whole P-256 keypair that stores and loads back as the same keys,
// or a key on disk could come back as one that seals to someone else.
func FuzzUnmarshalKeypair(f *testing.F) {
	f.Add([]byte(fuzzKeypairJSON))
	f.Add([]byte(`{"sign_priv_d":"09","box_priv_d":"05"}`))
	f.Add([]byte(`{"sign_priv_d":"","box_priv_d":""}`))
	f.Add([]byte(`{"sign_priv_d":"` + strings.Repeat("ff", 32) +
		`","box_priv_d":"` + strings.Repeat("ff", 32) + `"}`))
	f.Add([]byte(`null`))
	f.Fuzz(func(t *testing.T, stored []byte) {
		keypair, err := UnmarshalKeypair(stored)
		if err != nil {
			return
		}
		again, err := MarshalKeypair(keypair)
		if err != nil {
			t.Fatalf("%q loaded but cannot be stored: %v", stored, err)
		}
		back, err := UnmarshalKeypair(again)
		if err != nil ||
			!bytes.Equal(back.SignPub(), keypair.SignPub()) ||
			!bytes.Equal(back.BoxPub(), keypair.BoxPub()) {
			t.Fatalf("%q stored as %q, which loads differently: %v",
				stored, again, err)
		}
	})
}
