package assertion

import (
	"bytes"
	"crypto/elliptic"
	"encoding/base64"
	stdjson "encoding/json"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/crypto"
)

func good() Claims {
	return Claims{Version: 1, Issuer: "https://id.example.com", Audience: "https://team.docs.example.com", Subject: "person_123", KeyID: "key_1", ID: "AAAAAAAAAAAAAAAAAAAAAA", Nonce: "AQEBAQEBAQEBAQEBAQEBAQ", IssuedAt: 1700000000, ExpiresAt: 1700000120, AuthTime: 1699999900}
}
func setup(t *testing.T) (*crypto.Keypair, *Verifier) {
	t.Helper()
	k, e := crypto.Generate()
	if e != nil {
		t.Fatal(e)
	}
	c := good()
	v, e := NewVerifier(c.Issuer, c.Audience, map[string][]byte{c.KeyID: k.SignPub()})
	if e != nil {
		t.Fatal(e)
	}
	return k, v
}
func rawToken(t *testing.T, k *crypto.Keypair, p []byte) string {
	t.Helper()
	s, e := crypto.Sign(k, signingContext, p)
	if e != nil {
		t.Fatal(e)
	}
	return base64.RawURLEncoding.EncodeToString(p) + "." + base64.RawURLEncoding.EncodeToString(s)
}
func payload(t *testing.T, k *crypto.Keypair, c Claims) []byte {
	t.Helper()
	s, e := Sign(k, c)
	if e != nil {
		t.Fatal(e)
	}
	p, _, _ := strings.Cut(s, ".")
	b, e := base64.RawURLEncoding.DecodeString(p)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func refused(t *testing.T, v *Verifier, s string, now int64, want error) {
	t.Helper()
	c, e := v.Verify(s, time.Unix(now, 0))
	if e == nil || (want != nil && e != want) || !reflect.DeepEqual(c, Claims{}) {
		t.Fatalf("refusal: error=%v wanted=%v zero=%v", e, want, reflect.DeepEqual(c, Claims{}))
	}
}

func TestRoundtripAndFraming(t *testing.T) {
	k, v := setup(t)
	c := good()
	for _, ext := range [][]byte{nil, []byte(`{}`), []byte(`{"theme":"dark","label":"<😀>&"}`)} {
		c.Ext = ext
		b := payload(t, k, c)
		if b[0] != '{' || b[len(b)-1] != '}' || bytes.Contains(b, []byte(`\u003c`)) {
			t.Fatal("unexpected framing")
		}
		if ext != nil && !bytes.Contains(b, append([]byte(`"ext":`), ext...)) {
			t.Fatalf("extension lost: %s", b)
		}
		out, e := v.Verify(rawToken(t, k, b), time.Unix(c.IssuedAt, 999999999))
		if e != nil || !reflect.DeepEqual(out, c) {
			t.Fatalf("roundtrip: %v", e)
		}
	}
}
func TestRandomIDs(t *testing.T) {
	seen := map[string]bool{}
	for range 200 {
		id, e := RandomID()
		if e != nil || !randomID(id) || seen[id] {
			t.Fatal("bad random ID")
		}
		seen[id] = true
	}
}

func TestSignedClaimRefusals(t *testing.T) {
	k, v := setup(t)
	base := payload(t, k, good())
	cases := map[string]string{
		"audience": `"aud":"https://other.docs.example.com"`, "issuer": `"iss":"https://other.example.com"`, "version": `"v":2`, "key": `"kid":"unknown"`, "subject": `"sub":"a@example.com"`, "nonce reuse": `"nonce":"AAAAAAAAAAAAAAAAAAAAAA"`, "auth zero": `"auth_time":0`, "auth future": `"auth_time":1700000001`, "iat zero": `"iat":0`, "iat negative": `"iat":-1`, "exp before": `"exp":1699999999`, "ttl": `"exp":1700000121`, "int overflow": `"exp":9223372036854775808`, "max ttl": `"exp":9223372036854775807`, "exponent": `"iat":17e8`, "fraction": `"iat":1700000000.0`, "string number": `"iat":"1700000000"`, "null": `"sub":null`, "array subject": `"sub":[]`, "bad nonce bits": `"nonce":"AQEBAQEBAQEBAQEBAQEBAQR"`,
	}
	var fields map[string]stdjson.RawMessage
	if e := stdjson.Unmarshal(base, &fields); e != nil {
		t.Fatal(e)
	}
	for name, replacement := range cases {
		t.Run(name, func(t *testing.T) {
			key := strings.SplitN(replacement, ":", 2)[0]
			key = strings.Trim(key, `"`)
			old := `"` + key + `":` + string(fields[key])
			p := bytes.Replace(base, []byte(old), []byte(replacement), 1)
			if bytes.Equal(p, base) {
				t.Fatal("mutation missed")
			}
			var want error
			switch name {
			case "audience":
				want = ErrAudience
			case "issuer":
				want = ErrIssuer
			case "version":
				want = ErrVersion
			case "key":
				want = ErrUnknownKey
			}
			refused(t, v, rawToken(t, k, p), good().IssuedAt, want)
		})
	}
	for key, value := range fields {
		t.Run("missing "+key, func(t *testing.T) {
			p := bytes.Replace(base, []byte(`"`+key+`":`+string(value)+","), nil, 1)
			if bytes.Equal(base, p) {
				p = bytes.Replace(base, []byte(`,"`+key+`":`+string(value)), nil, 1)
			}
			refused(t, v, rawToken(t, k, p), good().IssuedAt, ErrClaims)
		})
	}
}

func TestParserFixtures(t *testing.T) {
	k, v := setup(t)
	paths, e := filepath.Glob("testdata/refused-*.json")
	if e != nil || len(paths) < 8 {
		t.Fatal("fixtures missing")
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			p, e := os.ReadFile(path)
			if e != nil {
				t.Fatal(e)
			}
			refused(t, v, rawToken(t, k, p), good().IssuedAt, nil)
		})
	}
	p, e := os.ReadFile("testdata/paired-surrogates.json")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = v.Verify(rawToken(t, k, p), time.Unix(good().IssuedAt, 0)); e != nil {
		t.Fatal(e)
	}
}
func TestWireAndSignature(t *testing.T) {
	k, v := setup(t)
	s, e := Sign(k, good())
	if e != nil {
		t.Fatal(e)
	}
	p, sig, _ := strings.Cut(s, ".")
	for name, bad := range map[string]string{"newline": p + "\n." + sig, "carriage": p + ".\r" + sig, "padding": p + "=." + sig, "extra dot": s + ".", "short sig": p + "." + sig[:85], "long": strings.Repeat("a", 8193), "empty": ".", "trailing bits": p + "." + sig[:85] + "B"} {
		t.Run(name, func(t *testing.T) { refused(t, v, bad, good().IssuedAt, ErrMalformed) })
	}
	b, _ := base64.RawURLEncoding.DecodeString(p)
	b = bytes.Replace(b, []byte("person_123"), []byte("person_456"), 1)
	refused(t, v, base64.RawURLEncoding.EncodeToString(b)+"."+sig, good().IssuedAt, ErrSignature)
	other, _ := crypto.Generate()
	refused(t, v, rawToken(t, other, b), good().IssuedAt, ErrSignature)
	wrong, _ := crypto.Sign(k, "other-context", b)
	refused(t, v, base64.RawURLEncoding.EncodeToString(b)+"."+base64.RawURLEncoding.EncodeToString(wrong), good().IssuedAt, ErrSignature)
}
func TestTimeBoundaries(t *testing.T) {
	k, v := setup(t)
	c := good()
	s, e := Sign(k, c)
	if e != nil {
		t.Fatal(e)
	}
	for _, now := range []int64{c.IssuedAt - 30, c.IssuedAt, c.ExpiresAt - 1} {
		if _, e = v.Verify(s, time.Unix(now, 0)); e != nil {
			t.Fatal(e)
		}
	}
	refused(t, v, s, c.IssuedAt-31, ErrLifetime)
	refused(t, v, s, c.ExpiresAt, ErrExpired)
	refused(t, v, s, math.MinInt64, ErrLifetime)
	refused(t, v, s, math.MaxInt64, ErrExpired)
	c.IssuedAt = math.MaxInt64 - 120
	c.ExpiresAt = math.MaxInt64
	c.AuthTime = c.IssuedAt
	s, e = Sign(k, c)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = v.Verify(s, time.Unix(c.IssuedAt, 0)); e != nil {
		t.Fatal(e)
	}
}
func TestExtensionLimit(t *testing.T) {
	k, v := setup(t)
	for _, size := range []int{1024, 1025} {
		ext := []byte(`{"a":"` + strings.Repeat("x", size-8) + `"}`)
		c := good()
		c.Ext = ext
		s, e := Sign(k, c)
		if size == 1024 {
			if e != nil {
				t.Fatal(e)
			}
			if _, e = v.Verify(s, time.Unix(c.IssuedAt, 0)); e != nil {
				t.Fatal(e)
			}
		} else {
			if e == nil {
				t.Fatal("oversized signed")
			}
			base := payload(t, k, good())
			p := append(append(base[:len(base)-1], []byte(`,"ext":`)...), ext...)
			p = append(p, '}')
			refused(t, v, rawToken(t, k, p), good().IssuedAt, ErrClaims)
		}
	}
	for _, ext := range []string{`null`, `[]`, `{"a":1,"a":2}`, `{"a":"\ud800"}`, "", ` {}`} {
		c := good()
		c.Ext = []byte(ext)
		if _, e := Sign(k, c); e == nil {
			t.Fatal("bad extension signed")
		}
	}
}
func TestTrustSnapshotAndConcurrency(t *testing.T) {
	k, _ := setup(t)
	c := good()
	pub := k.SignPub()
	keys := map[string][]byte{c.KeyID: pub}
	v, e := NewVerifier(c.Issuer, c.Audience, keys)
	if e != nil {
		t.Fatal(e)
	}
	for i := range pub {
		pub[i] = 0
	}
	delete(keys, c.KeyID)
	s, e := Sign(k, c)
	if e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 20 {
				if _, e := v.Verify(s, time.Unix(c.IssuedAt, 0)); e != nil {
					t.Error(e)
				}
			}
		}()
	}
	wg.Wait()
}
func TestConfigurationAndSigner(t *testing.T) {
	k, _ := setup(t)
	c := good()
	for _, keys := range []map[string][]byte{nil, {}, {"bad!": k.SignPub()}, {"key": make([]byte, 65)}, {"key": k.SignPub()[:64]}} {
		if _, e := NewVerifier(c.Issuer, c.Audience, keys); e != ErrConfig {
			t.Fatal(e)
		}
	}
	keys := map[string][]byte{}
	for i := 0; i < 9; i++ {
		keys[strings.Repeat("a", i+1)] = k.SignPub()
	}
	if _, e := NewVerifier(c.Issuer, c.Audience, keys); e != ErrConfig {
		t.Fatal(e)
	}
	if _, e := NewVerifier(c.Issuer, c.Issuer, map[string][]byte{c.KeyID: k.SignPub()}); e != ErrConfig {
		t.Fatal(e)
	}
	for _, key := range []*crypto.Keypair{nil, {}, {SignPriv: &*k.SignPriv}} {
		if key != nil && key.SignPriv != nil {
			key.SignPriv.Curve = elliptic.P384()
		}
		if _, e := Sign(key, c); e != ErrSigning {
			t.Fatal(e)
		}
	}
	var zero Verifier
	refused(t, &zero, "", 0, ErrConfig)
	refused(t, nil, "", 0, ErrConfig)
}
func TestOriginGrammar(t *testing.T) {
	for _, s := range []string{"https://id.example.com", "https://xn--bcher-kva.example:8443", "https://example.com:1", "https://example.com:65535"} {
		if !origin(s) {
			t.Fatal(s)
		}
	}
	for _, s := range []string{"http://example.com", "https://EXAMPLE.com", "https://example.com/", "https://example.com.", "https://127.0.0.1", "https://127.1", "https://2130706433", "https://[::1]", "https://0x7f000001", "https://0x", "https://1.0x", "https://user@example.com", "https://example.com:443", "https://example.com:0443", "https://example.com:0", "https://example.com:65536", "https://example.com:+1", "https://example.com?x=1", "https://example.com#x", "https://*.example.com", "https://-a.example", "https://a-.example", "https://bücher.example", "https://a_b.example"} {
		if origin(s) {
			t.Fatal(s)
		}
	}
}

func TestIndependentFixture(t *testing.T) {
	var f struct{ Payload, PublicKey, Signature string }
	b, e := os.ReadFile("testdata/golden.json")
	if e != nil {
		t.Fatal(e)
	}
	if e = stdjson.Unmarshal(b, &f); e != nil {
		t.Fatal(e)
	}
	pub, e := base64.RawURLEncoding.DecodeString(f.PublicKey)
	if e != nil {
		t.Fatal(e)
	}
	c := good()
	v, e := NewVerifier(c.Issuer, c.Audience, map[string][]byte{c.KeyID: pub})
	if e != nil {
		t.Fatal(e)
	}
	out, e := v.Verify(base64.RawURLEncoding.EncodeToString([]byte(f.Payload))+"."+f.Signature, time.Unix(c.IssuedAt, 0))
	if e != nil || !reflect.DeepEqual(out, c) {
		t.Fatalf("foreign fixture: %v", e)
	}
}
func TestNodeVerifiesGo(t *testing.T) {
	node, e := exec.LookPath("node")
	if e != nil {
		t.Skip("node unavailable")
	}
	k, _ := setup(t)
	s, e := Sign(k, good())
	if e != nil {
		t.Fatal(e)
	}
	in, _ := stdjson.Marshal(map[string]string{"token": s, "publicKey": base64.RawURLEncoding.EncodeToString(k.SignPub())})
	cmd := exec.Command(node, "testdata/twin.mjs", "verify")
	cmd.Stdin = bytes.NewReader(in)
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("node verify: %v %s", e, out)
	}
}
