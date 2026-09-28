package sealedcookie

import (
	"bytes"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/crypto"
)

type payload struct {
	O string `json:"o"`
	A string `json:"a"`
}

var (
	keyA = crypto.Derive([]byte("instance"), "rastrillo/test/a")
	keyB = crypto.Derive([]byte("instance"), "rastrillo/test/b")
)

func TestSealThenOpenRoundTrips(t *testing.T) {
	v, err := Seal(keyA, payload{O: "https://app.test", A: "ada@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(v, "v1.") {
		t.Fatalf("value %q lacks the v1. prefix", v)
	}
	// Decoded, not the base64 text: random ciphertext can spell a short
	// word by chance, but not fifteen plaintext bytes.
	raw, _ := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(v, "v1."))
	if bytes.Contains(raw, []byte("ada@example.com")) {
		t.Fatal("the payload is readable in the sealed value")
	}
	var got payload
	if err := Open(keyA, v, &got); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got != (payload{O: "https://app.test", A: "ada@example.com"}) {
		t.Fatalf("round trip gave %+v", got)
	}
}

func TestOpenRefusesAnythingItDidNotSeal(t *testing.T) {
	good, err := Seal(keyA, payload{O: "o", A: "a"})
	if err != nil {
		t.Fatal(err)
	}
	body := strings.TrimPrefix(good, "v1.")
	raw, _ := base64.RawURLEncoding.DecodeString(body)
	raw[len(raw)/2] ^= 0x01
	tampered := "v1." + base64.RawURLEncoding.EncodeToString(raw)

	otherKey, _ := Seal(keyB, payload{O: "o", A: "a"})
	extra, _ := Seal(keyA, struct {
		O string `json:"o"`
		A string `json:"a"`
		Z string `json:"z"`
	}{"o", "a", "z"})
	two, _ := crypto.SealSym(keyA, []byte(`{"o":"o","a":"a"} {"o":"x"}`))
	trailing := "v1." + base64.RawURLEncoding.EncodeToString(two)

	for name, v := range map[string]string{
		"tampered":        tampered,
		"another key":     otherKey,
		"unknown version": "v2." + body,
		"no version":      body,
		"not base64":      "v1.!!!",
		"unknown field":   extra,
		"trailing data":   trailing,
		"empty":           "",
	} {
		var got payload
		if err := Open(keyA, v, &got); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: Open = %v, want ErrInvalid", name, err)
		}
	}
}

func TestFreshJudgesOriginAndTime(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	n := now.Unix()
	for _, c := range []struct {
		name     string
		origin   string
		iat, exp int64
		want     bool
	}{
		{"current", "https://app.test", n - 60, n + 60, true},
		{"another origin", "https://other.test", n - 60, n + 60, false},
		{"expiring exactly now", "https://app.test", n - 600, n, false},
		{"a minute ahead is clock drift", "https://app.test", n + 60, n + 120, true},
		{"further ahead is not", "https://app.test", n + 61, n + 120, false},
		{"exactly the cap", "https://app.test", n - 10, n - 10 + 900, true},
		{"longer than the cap", "https://app.test", n - 10, n - 10 + 901, false},
		{"no issue time", "https://app.test", 0, n + 60, false},
		{"expiring before issue", "https://app.test", n, n - 1, false},
	} {
		if got := Fresh(c.origin, "https://app.test", c.iat, c.exp, now, 15*time.Minute); got != c.want {
			t.Errorf("%s: Fresh = %v, want %v", c.name, got, c.want)
		}
	}
}
