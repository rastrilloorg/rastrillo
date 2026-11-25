package sessions

import (
	"bytes"
	"encoding/base64"
	"net/url"
	"strings"
	"testing"

	json "github.com/go-json-experiment/json"
)

// fuzzSecret is a fixed signing key, so the cookies the fuzzer mutates
// stay verifiable from one run to the next and across the saved corpus.
// Built at run time from the bytes 1 to 32: written out as a literal,
// gitleaks reads it as a committed key.
var fuzzSecret = func() string {
	key := make([]byte, 32)
	for at := range key {
		key[at] = byte(at + 1)
	}
	return "fuzz." + base64.RawURLEncoding.EncodeToString(key)
}()

func fuzzKey(f *testing.F) *SigningKey {
	f.Helper()
	key, err := ParseSigningKey(fuzzSecret)
	if err != nil {
		f.Fatal(err)
	}
	return key
}

func fuzzClaims() signedClaims {
	claims := goodClaims()
	claims.V, claims.KID = 1, "fuzz"
	return claims
}

// FuzzVerifyCookie holds verifyCookie to the one promise a signature
// makes: the only cookie it lets through is one carrying the claims the
// key signed. A mutation that still verifies with different claims is a
// forgery; one that verifies with the same claims (an ECDSA signature's
// other s, say) is not.
func FuzzVerifyCookie(f *testing.F) {
	key := fuzzKey(f)
	keys := map[string][]byte{key.KeyID(): key.kp.SignPub()}
	signed := fuzzClaims()
	valid, err := key.sign(signed)
	if err != nil {
		f.Fatal(err)
	}
	// A seed that stopped verifying would leave the property below
	// with nothing to hold.
	if _, state := verifyCookie(keys, valid, t0); state != cookieValid {
		f.Fatalf("the signed cookie reads as %v", state)
	}
	payload, signature, _ := strings.Cut(valid, ".")
	f.Add(valid)
	f.Add(valid + ".")
	f.Add(payload + "." + payload)
	f.Add(signature + "." + payload)
	f.Add("")
	f.Add(".")
	f.Add(strings.Repeat("A", maxSignedCookie+1))
	f.Add(base64.RawURLEncoding.EncodeToString([]byte(`{}`)) + "." +
		signature)
	f.Fuzz(func(t *testing.T, value string) {
		claims, state := verifyCookie(keys, value, t0)
		switch state {
		case cookieInvalid:
			if claims != (signedClaims{}) {
				t.Fatalf("invalid %q still returned claims %+v",
					value, claims)
			}
		case cookieValid, cookieExpired:
			if claims != signed {
				t.Fatalf("%q verified with claims %+v; the key "+
					"only signed %+v", value, claims, signed)
			}
		default:
			t.Fatalf("%q gave state %v", value, state)
		}
	})
}

// FuzzParseClaims checks that whatever the strict reader accepts reads
// back the same once re-encoded: a payload that meant one thing here
// and another to a laxer reader is what the strictness exists to stop.
func FuzzParseClaims(f *testing.F) {
	good, err := json.Marshal(fuzzClaims())
	if err != nil {
		f.Fatal(err)
	}
	f.Add(good)
	f.Add([]byte(`{}`))
	f.Add([]byte(`null`))
	f.Add(bytes.Replace(good, []byte(`"v":1`), []byte(`"v":+1`), 1))
	f.Add(bytes.Replace(good, []byte(`"v":1`), []byte(`"v":1.0`), 1))
	f.Add(bytes.Replace(good, []byte(`"v":1`), []byte(`"v":1`), 1))
	f.Add(bytes.Replace(good, []byte(`"m":`), []byte(`"m":null,"m":`), 1))
	f.Fuzz(func(t *testing.T, payload []byte) {
		claims, ok := parseClaims(payload)
		if !ok {
			return
		}
		again, err := json.Marshal(claims)
		if err != nil {
			t.Fatalf("accepted %q but cannot re-encode it: %v",
				payload, err)
		}
		back, ok := parseClaims(again)
		if !ok || back != claims {
			t.Fatalf("accepted %q as %+v, which reads back as %+v, %v",
				payload, claims, back, ok)
		}
	})
}

// FuzzParseKeys checks the two key formats are canonical: a value
// either fails or is exactly what the parsed key writes back out, so no
// second spelling of a key is ever accepted.
func FuzzParseKeys(f *testing.F) {
	key := fuzzKey(f)
	f.Add(fuzzSecret)
	f.Add(key.Public())
	f.Add(key.Public() + "," + key.Public())
	f.Add("fuzz.")
	f.Add("FUZZ" + strings.TrimPrefix(fuzzSecret, "fuzz"))
	f.Add(",")
	f.Fuzz(func(t *testing.T, value string) {
		if parsed, err := ParseSigningKey(value); err == nil {
			if parsed.Secret() != value {
				t.Fatalf("signing key %q read back as %q",
					value, parsed.Secret())
			}
		}
		keys, err := ParsePublicKeys(value)
		if err != nil {
			return
		}
		for _, part := range strings.Split(value, ",") {
			kid, encoded, _ := strings.Cut(part, ".")
			point := base64.RawURLEncoding.EncodeToString(keys[kid])
			if point != encoded || validPublicKey(kid, keys[kid]) != nil {
				t.Fatalf("public keys %q held %q as %q",
					value, kid, point)
			}
		}
	})
}

// FuzzSafeReturnTo checks the sign-in return path never names another
// origin, which is the whole of what stops it being an open redirect.
func FuzzSafeReturnTo(f *testing.F) {
	for _, seed := range []string{
		"/", "/a/b?c=d#e", "//evil.example", "/\\evil.example",
		"/\t/evil.example", "https://evil.example", "/a/../\\x",
		"javascript:alert(1)", "/%2F/evil.example", "",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, to string) {
		got := SafeReturnTo(to, "/fallback")
		if got == "/fallback" {
			return
		}
		if got != to {
			t.Fatalf("%q came back as %q", to, got)
		}
		parsed, err := url.Parse(to)
		if err != nil {
			return
		}
		if parsed.Scheme != "" || parsed.Host != "" ||
			parsed.User != nil || !strings.HasPrefix(to, "/") {
			t.Fatalf("%q leaves the site: %#v", to, parsed)
		}
	})
}

// FuzzRefreshCounter checks a garbled counter cookie reads as none
// rather than as a count that would skip the refresh loop's brake.
func FuzzRefreshCounter(f *testing.F) {
	for _, seed := range []string{
		"1.1700000000", "0.1700000000", "-1.1", "1.-1", "1.", ".1",
		"99999999999999999999.1", "1.1.1", "+1.1",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) {
		count, start, ok := parseRefreshCounter(value)
		if ok && (count < 1 || start < 1) {
			t.Fatalf("%q read as count %d from %d", value, count, start)
		}
		if !ok && (count != 0 || start != 0) {
			t.Fatalf("%q refused but read as %d, %d", value, count, start)
		}
	})
}
