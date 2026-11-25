package keyring

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/elliptic"
	"math/big"
	"testing"

	"amadan.net/rastrillo/rastrillo/crypto"
)

// The member key, PRF output and every seal below are fixed, so a blob
// saved in the corpus still opens next run and in every fuzz worker;
// Grant and WrapSeed draw their IVs and ephemeral keys at random.
var (
	fuzzRing   = Ring{Namespace: "fuzz"}
	fuzzMember = bytes.Repeat([]byte{5}, 32)
	fuzzPRF    = bytes.Repeat([]byte{6}, 32)
	fuzzSecret = []byte("a content key, thirty-two bytes!")
)

func fixedGCM(f *testing.F, key []byte) cipher.AEAD {
	f.Helper()
	block, err := aes.NewCipher(key)
	if err != nil {
		f.Fatal(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		f.Fatal(err)
	}
	return gcm
}

// fixedGrant is Grant with its ephemeral key and IV fixed.
func fixedGrant(f *testing.F) []byte {
	f.Helper()
	member, err := ecdh.P256().NewPrivateKey(fuzzMember)
	if err != nil {
		f.Fatal(err)
	}
	ephemeral, err := ecdh.P256().NewPrivateKey(bytes.Repeat([]byte{4}, 32))
	if err != nil {
		f.Fatal(err)
	}
	shared, err := ephemeral.ECDH(member.PublicKey())
	if err != nil {
		f.Fatal(err)
	}
	key := crypto.Derive(shared, fuzzRing.Namespace+"/grant/v1")
	iv := make([]byte, 12)
	sealed := append(ephemeral.PublicKey().Bytes(), iv...)
	sealed = fixedGCM(f, key).Seal(sealed, iv, fuzzSecret, nil)
	if _, err := fuzzRing.OpenGrant(fuzzMember, sealed); err != nil {
		f.Fatalf("OpenGrant refuses the fixed grant: %v", err)
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

// FuzzOpenGrant feeds grant blobs, as a server or another member would
// hand them over. Only the one granted opens, in either spelling of its
// ephemeral point, only to the key granted, and only under this ring's
// namespace.
func FuzzOpenGrant(f *testing.F) {
	granted := fixedGrant(f)
	f.Add(granted, "fuzz")
	f.Add(negatedEphemeral(granted), "fuzz")
	f.Add(granted, "fuzz/grant/v1")
	f.Add(granted, "")
	f.Add(granted[:77], "fuzz")
	f.Add(append(bytes.Clone(granted), 0), "fuzz")
	f.Fuzz(func(t *testing.T, sealed []byte, namespace string) {
		ring := Ring{Namespace: namespace}
		opened, err := ring.OpenGrant(fuzzMember, sealed)
		if err != nil {
			return
		}
		same := bytes.Equal(sealed, granted) ||
			bytes.Equal(sealed, negatedEphemeral(granted))
		if namespace != fuzzRing.Namespace || !same ||
			!bytes.Equal(opened, fuzzSecret) {
			t.Fatalf("%x opened under %q to %q; only %x was granted",
				sealed, namespace, opened, granted)
		}
	})
}

// FuzzUnwrapSeed is FuzzOpenGrant for a seed wrapped under a passkey's
// PRF output: the wrong PRF output, namespace or blob must all fail.
func FuzzUnwrapSeed(f *testing.F) {
	iv := make([]byte, 12)
	gcm := fixedGCM(f, fuzzRing.WrapKey(fuzzPRF))
	wrapped := gcm.Seal(bytes.Clone(iv), iv, fuzzSecret, nil)
	f.Add(fuzzPRF, wrapped)
	f.Add(fuzzPRF[:31], wrapped)
	f.Add([]byte{}, wrapped)
	f.Add(fuzzPRF, wrapped[:12])
	f.Add(fuzzPRF, wrapped[:11])
	f.Fuzz(func(t *testing.T, prf, blob []byte) {
		seed, err := fuzzRing.UnwrapSeed(prf, blob)
		if err != nil {
			return
		}
		if !bytes.Equal(prf, fuzzPRF) || !bytes.Equal(blob, wrapped) ||
			!bytes.Equal(seed, fuzzSecret) {
			t.Fatalf("%x unwrapped with %x to %q; only %x was wrapped",
				blob, prf, seed, wrapped)
		}
	})
}

// FuzzRemoveWrap folds removals over wrap lists built from the
// fuzzer's bytes, one wrap per byte named by it, the way an
// event-sourced app folds untrusted events. Removal must never leave
// the seed with no wrap, nor touch the list it was given.
func FuzzRemoveWrap(f *testing.F) {
	f.Add([]byte{1}, byte(1))
	f.Add([]byte{1, 2}, byte(1))
	f.Add([]byte{1, 1}, byte(1))
	f.Add([]byte{}, byte(0))
	f.Fuzz(func(t *testing.T, ids []byte, remove byte) {
		var wraps []Wrap
		for _, id := range ids {
			wraps = append(wraps, Wrap{ID: string(rune(id))})
		}
		before := append([]Wrap(nil), wraps...)
		left, err := RemoveWrap(wraps, string(rune(remove)))
		for index := range wraps {
			if wraps[index].ID != before[index].ID {
				t.Fatalf("removing %d changed the input %v", remove, ids)
			}
		}
		if err == nil && (len(left) == 0 || len(left) != len(wraps)-1) {
			t.Fatalf("removing %d from %v left %d wraps", remove, ids,
				len(left))
		}
		if err != nil && left != nil {
			t.Fatalf("removing %d from %v failed but returned %v",
				remove, ids, left)
		}
	})
}
