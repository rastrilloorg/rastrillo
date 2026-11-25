package sealedcookie

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"testing"
)

// fixedSeal is Seal with a fixed IV. Seal draws its IV at random, so
// each fuzz worker would seal a different value, and a saved corpus
// entry would stop opening.
func fixedSeal(f *testing.F, key []byte, raw string) string {
	f.Helper()
	block, err := aes.NewCipher(key)
	if err != nil {
		f.Fatal(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		f.Fatal(err)
	}
	iv := make([]byte, gcm.NonceSize())
	sealed := gcm.Seal(bytes.Clone(iv), iv, []byte(raw), nil)
	return version + base64.RawURLEncoding.EncodeToString(sealed)
}

// FuzzOpen feeds the value of a sign-in screen cookie, which the
// browser holds and so may edit. Only what was sealed may open: the
// base64 may have a second spelling, the payload may not.
func FuzzOpen(f *testing.F) {
	want := payload{O: "https://app.test", A: "ada@example.com"}
	value := fixedSeal(f, keyA, `{"o":"https://app.test",`+
		`"a":"ada@example.com"}`)
	var check payload
	if err := Open(keyA, value, &check); err != nil || check != want {
		f.Fatalf("the fixed seal opens to %+v, %v", check, err)
	}
	f.Add(value)
	f.Add(value + "A")
	f.Add(value[:len(value)-1])
	f.Add("v2." + value[3:])
	f.Add(value[3:])
	f.Add(fixedSeal(f, keyA, `{"o":"https://app.test","a":"x","z":1}`))
	f.Add(fixedSeal(f, keyA, `{"o":"https://app.test"} {}`))
	f.Add(fixedSeal(f, keyB, `{"o":"https://app.test","a":"x"}`))
	f.Add("v1.")
	f.Fuzz(func(t *testing.T, got string) {
		var opened payload
		if Open(keyA, got, &opened) != nil {
			return
		}
		if opened != want {
			t.Fatalf("%q opened to %+v; only %+v was sealed",
				got, opened, want)
		}
	})
}
