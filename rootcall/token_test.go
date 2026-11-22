package rootcall

import (
	"encoding/base64"
	stdjson "encoding/json"
	"errors"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/assertion"
	"amadan.net/rastrillo/rastrillo/crypto"
	"amadan.net/rastrillo/rastrillo/internal/wire"
	"amadan.net/rastrillo/rastrillo/nodetest"
	"amadan.net/rastrillo/rastrillo/roster"
)

const (
	rootOrigin     = "https://root.example.com"
	instanceOrigin = "https://box1.example.net"
	issuedAt       = 1700000000
)

func goodClaims() Claims {
	return Claims{
		Version: 1, Issuer: rootOrigin, Audience: instanceOrigin,
		KeyID: "key_1", ID: "AAECAwQFBgcICQoLDA0ODw",
		IssuedAt: issuedAt, ExpiresAt: issuedAt + 60,
		Method: "POST", Target: "/_carlos/slopbox/plan",
		BodyHash: BodyHash([]byte(`{"plan":"paid","seq":3}`)),
	}
}

func setup(t *testing.T) (*crypto.Keypair, *Verifier) {
	t.Helper()
	key, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := NewVerifier(rootOrigin, instanceOrigin,
		map[string][]byte{"key_1": key.SignPub()})
	if err != nil {
		t.Fatal(err)
	}
	return key, verifier
}

// signedUnder signs payload under context without Sign's checks, to
// reach refusals that Sign would stop first.
func signedUnder(
	t *testing.T, key *crypto.Keypair, context string, payload []byte,
) string {
	t.Helper()
	signature, err := crypto.Sign(key, context, payload)
	if err != nil {
		t.Fatal(err)
	}
	return wire.Join(payload, signature)
}

func signedRaw(t *testing.T, key *crypto.Keypair, claims any) string {
	t.Helper()
	payload, err := stdjson.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	return signedUnder(t, key, signingContext, payload)
}

func TestASignedCallVerifies(t *testing.T) {
	key, verifier := setup(t)
	token, err := Sign(key, goodClaims())
	if err != nil {
		t.Fatal(err)
	}
	got, err := verifier.Verify(token, time.Unix(issuedAt, 0))
	if err != nil {
		t.Fatal(err)
	}
	if got != goodClaims() {
		t.Fatalf("got %+v, want %+v", got, goodClaims())
	}
}

type goldenVector struct {
	Now       int64  `json:"now"`
	PublicKey string `json:"publicKey"`
	Request   struct {
		Method, Target, Body string
	} `json:"request"`
	Payload string `json:"payload"`
	Token   string `json:"token"`
}

func readGolden(t *testing.T) goldenVector {
	t.Helper()
	raw, err := os.ReadFile("testdata/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var vector goldenVector
	if err := stdjson.Unmarshal(raw, &vector); err != nil {
		t.Fatal(err)
	}
	return vector
}

func TestACallNodeSignedVerifies(t *testing.T) {
	vector := readGolden(t)
	public, err := base64.RawURLEncoding.DecodeString(vector.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := NewVerifier(rootOrigin, instanceOrigin,
		map[string][]byte{"key_1": public})
	if err != nil {
		t.Fatal(err)
	}
	got, err := verifier.Verify(vector.Token, time.Unix(vector.Now, 0))
	if err != nil {
		t.Fatal(err)
	}
	var want Claims
	if err := stdjson.Unmarshal([]byte(vector.Payload), &want); err != nil {
		t.Fatal(err)
	}
	if got != want || got.Method != vector.Request.Method ||
		got.Target != vector.Request.Target {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if BodyHash([]byte(vector.Request.Body)) != got.BodyHash {
		t.Fatal("Go's BodyHash differs from the hash Node signed")
	}
}

func TestNodeVerifiesACallGoSigned(t *testing.T) {
	nodetest.Node(t)
	key, _ := setup(t)
	token, err := Sign(key, goodClaims())
	if err != nil {
		t.Fatal(err)
	}
	input, err := stdjson.Marshal(map[string]string{
		"token":     token,
		"publicKey": base64.RawURLEncoding.EncodeToString(key.SignPub()),
		"body":      `{"plan":"paid","seq":3}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	nodetest.Run(t, nodetest.Cmd{
		Args: []string{"testdata/twin.mjs", "verify"}, Stdin: input,
	})
}

func TestACallSignedUnderAnotherContextIsRefused(t *testing.T) {
	key, verifier := setup(t)
	payload, err := stdjson.Marshal(goodClaims())
	if err != nil {
		t.Fatal(err)
	}
	for _, context := range []string{
		"rastrillo/assertion/v1", "rastrillo/roster/v1",
	} {
		token := signedUnder(t, key, context, payload)
		_, err := verifier.Verify(token, time.Unix(issuedAt, 0))
		if !errors.Is(err, ErrSignature) {
			t.Errorf("%s: got %v, want ErrSignature", context, err)
		}
	}
}

func TestHandoffAndRosterRefuseACall(t *testing.T) {
	key, _ := setup(t)
	token, err := Sign(key, goodClaims())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(issuedAt, 0)
	keys := map[string][]byte{"key_1": key.SignPub()}
	handoff, err := assertion.NewVerifier(rootOrigin, instanceOrigin, keys)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := handoff.Verify(token, now); err == nil {
		t.Error("an assertion verifier accepted a root call")
	}
	// A well-formed assertion signed under this package's context reaches
	// the signature check, so this refusal is the context's doing.
	id, _ := assertion.RandomID()
	nonce, _ := assertion.RandomID()
	payload, err := stdjson.Marshal(assertion.Claims{
		Version: 1, Issuer: rootOrigin, Audience: instanceOrigin,
		Subject: "person_1", KeyID: "key_1", ID: id, Nonce: nonce,
		IssuedAt: issuedAt, ExpiresAt: issuedAt + 60, AuthTime: issuedAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = handoff.Verify(
		signedUnder(t, key, signingContext, payload), now)
	if !errors.Is(err, assertion.ErrSignature) {
		t.Errorf("assertion: got %v, want assertion.ErrSignature", err)
	}
	snapshots, err := roster.NewVerifier(rootOrigin, instanceOrigin, keys)
	if err != nil {
		t.Fatal(err)
	}
	_, err = snapshots.Verify(token, now)
	if !errors.Is(err, roster.ErrSignature) {
		t.Errorf("roster: got %v, want roster.ErrSignature", err)
	}
}

func TestVerifyRefusesTheWrongPartyOrKey(t *testing.T) {
	key, verifier := setup(t)
	cases := map[string]struct {
		change func(*Claims)
		want   error
	}{
		"issuer": {func(claims *Claims) {
			claims.Issuer = "https://other.example.com"
		}, ErrIssuer},
		"audience": {func(claims *Claims) {
			claims.Audience = "https://box2.example.net"
		}, ErrAudience},
		"key": {func(claims *Claims) { claims.KeyID = "key_2" },
			ErrUnknownKey},
	}
	for name, test := range cases {
		claims := goodClaims()
		test.change(&claims)
		token, err := Sign(key, claims)
		if err != nil {
			t.Fatal(err)
		}
		_, err = verifier.Verify(token, time.Unix(issuedAt, 0))
		if !errors.Is(err, test.want) {
			t.Errorf("%s: got %v, want %v", name, err, test.want)
		}
	}
}

func TestVerifyRefusesACallOutsideItsLifetime(t *testing.T) {
	key, verifier := setup(t)
	token, err := Sign(key, goodClaims())
	if err != nil {
		t.Fatal(err)
	}
	cases := map[int64]error{
		issuedAt + 59: nil,
		issuedAt + 60: ErrExpired,
		issuedAt - 30: nil,
		issuedAt - 31: ErrLifetime,
	}
	for at, want := range cases {
		_, err := verifier.Verify(token, time.Unix(at, 0))
		if !errors.Is(err, want) {
			t.Errorf("at %d: got %v, want %v", at, err, want)
		}
	}
}

func TestACallMayLiveAtMost120Seconds(t *testing.T) {
	key, verifier := setup(t)
	claims := goodClaims()
	claims.ExpiresAt = claims.IssuedAt + 121
	if _, err := Sign(key, claims); !errors.Is(err, ErrLifetime) {
		t.Errorf("Sign: got %v, want ErrLifetime", err)
	}
	token := signedRaw(t, key, claims)
	_, err := verifier.Verify(token, time.Unix(issuedAt, 0))
	if !errors.Is(err, ErrLifetime) {
		t.Errorf("Verify: got %v, want ErrLifetime", err)
	}
	claims.ExpiresAt = claims.IssuedAt + 120
	if _, err := Sign(key, claims); err != nil {
		t.Errorf("Sign at 120 seconds: %v", err)
	}
}

func TestVerifyRefusesFieldsItDoesNotKnow(t *testing.T) {
	key, verifier := setup(t)
	claims := map[string]any{}
	raw, _ := stdjson.Marshal(goodClaims())
	if err := stdjson.Unmarshal(raw, &claims); err != nil {
		t.Fatal(err)
	}
	claims["sub"] = "person_1"
	_, err := verifier.Verify(signedRaw(t, key, claims),
		time.Unix(issuedAt, 0))
	if !errors.Is(err, ErrClaims) {
		t.Fatalf("got %v, want ErrClaims", err)
	}
}

func TestSignRefusesMalformedClaims(t *testing.T) {
	key, verifier := setup(t)
	long := "/" + strings.Repeat("a", 2048)
	cases := map[string]func(*Claims){
		"version":      func(claims *Claims) { claims.Version = 2 },
		"issuer":       func(claims *Claims) { claims.Issuer = "http://x.com" },
		"same parties": func(claims *Claims) { claims.Audience = rootOrigin },
		"key id":       func(claims *Claims) { claims.KeyID = "key 1" },
		"short id":     func(claims *Claims) { claims.ID = "AAEC" },
		"no method":    func(claims *Claims) { claims.Method = "" },
		"lower method": func(claims *Claims) { claims.Method = "post" },
		"long method": func(claims *Claims) {
			claims.Method = strings.Repeat("P", 17)
		},
		"no target": func(claims *Claims) { claims.Target = "" },
		"relative":  func(claims *Claims) { claims.Target = "_carlos/x" },
		"absolute":  func(claims *Claims) { claims.Target = rootOrigin + "/" },
		"space":     func(claims *Claims) { claims.Target = "/a b" },
		"fragment":  func(claims *Claims) { claims.Target = "/a#b" },
		"non-ASCII": func(claims *Claims) { claims.Target = "/caf\u00e9" },
		"long":      func(claims *Claims) { claims.Target = long },
		"no hash":   func(claims *Claims) { claims.BodyHash = "" },
		"short":     func(claims *Claims) { claims.BodyHash = "AAEC" },
		"padded":    func(claims *Claims) { claims.BodyHash += "=" },
		"no iat":    func(claims *Claims) { claims.IssuedAt = 0 },
		"backwards": func(claims *Claims) {
			claims.ExpiresAt = claims.IssuedAt
		},
	}
	for name, change := range cases {
		claims := goodClaims()
		change(&claims)
		if _, err := Sign(key, claims); err == nil {
			t.Errorf("%s: Sign accepted it", name)
		}
		_, err := verifier.Verify(signedRaw(t, key, claims),
			time.Unix(issuedAt, 0))
		if err == nil {
			t.Errorf("%s: Verify accepted it", name)
		}
	}
}

func TestVerifyRefusesAMalformedToken(t *testing.T) {
	key, verifier := setup(t)
	token, err := Sign(key, goodClaims())
	if err != nil {
		t.Fatal(err)
	}
	payload, signature, _ := strings.Cut(token, ".")
	duplicate := []byte(`{"v":1,"v":1}`)
	cases := map[string]string{
		"empty":          "",
		"no signature":   payload,
		"padded":         payload + "=." + signature,
		"too large":      strings.Repeat("a", 8193) + "." + signature,
		"duplicate name": signedUnder(t, key, signingContext, duplicate),
		"not an object":  signedUnder(t, key, signingContext, []byte(`[1]`)),
	}
	for name, token := range cases {
		_, err := verifier.Verify(token, time.Unix(issuedAt, 0))
		if !errors.Is(err, ErrMalformed) {
			t.Errorf("%s: got %v, want ErrMalformed", name, err)
		}
	}
}

func TestNewVerifierRefusesABadConfiguration(t *testing.T) {
	key, _ := setup(t)
	keys := map[string][]byte{"key_1": key.SignPub()}
	cases := map[string]func() (*Verifier, error){
		"no keys": func() (*Verifier, error) {
			return NewVerifier(rootOrigin, instanceOrigin, nil)
		},
		"bad issuer": func() (*Verifier, error) {
			return NewVerifier("root.example.com", instanceOrigin, keys)
		},
		"bad audience": func() (*Verifier, error) {
			return NewVerifier(rootOrigin, "https://box1.example.net/", keys)
		},
		"same origin": func() (*Verifier, error) {
			return NewVerifier(rootOrigin, rootOrigin, keys)
		},
	}
	for name, build := range cases {
		if _, err := build(); !errors.Is(err, ErrConfig) {
			t.Errorf("%s: got %v, want ErrConfig", name, err)
		}
	}
	var zero *Verifier
	if _, err := zero.Verify("", time.Now()); !errors.Is(err, ErrConfig) {
		t.Errorf("nil verifier: got %v, want ErrConfig", err)
	}
}

func TestSignRefusesAKeyItCannotSignWith(t *testing.T) {
	_, err := Sign(nil, goodClaims())
	if !errors.Is(err, ErrSigning) {
		t.Fatalf("got %v, want ErrSigning", err)
	}
}

func TestVerifiedClaimsMatchTheirFieldNames(t *testing.T) {
	// The JSON names are the wire format, shared with any root that is
	// not written in Go; this pins them.
	raw, err := stdjson.Marshal(goodClaims())
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := stdjson.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	var names []string
	for name := range fields {
		names = append(names, name)
	}
	want := []string{"aud", "body", "exp", "iat", "iss", "jti", "kid",
		"method", "target", "v"}
	slices.Sort(names)
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("fields = %v, want %v", names, want)
	}
}
