package granttoken

import (
	"bytes"
	"encoding/base64"
	stdjson "encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/crypto"
	"amadan.net/rastrillo/rastrillo/internal/wire"
)

const (
	rootOrigin = "https://root.test"
	siteOrigin = "https://site.test"
	issuedAt   = 1700000000
	ninetyDays = 90 * 24 * 60 * 60
)

var memberNames = []string{
	"v", "iss", "aud", "kid", "sub", "gid", "agent", "access", "iat", "exp",
}

func goodClaims() Claims {
	return Claims{
		Version: 1, Issuer: rootOrigin, Audience: siteOrigin,
		KeyID: "key_1", Subject: "person_1", Grant: "grant_1",
		Agent: "Claude", Access: Write, IssuedAt: issuedAt,
		ExpiresAt: issuedAt + ninetyDays,
	}
}

func setup(t *testing.T) (*crypto.Keypair, *Verifier) {
	t.Helper()
	key, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := NewVerifier(
		rootOrigin, siteOrigin, map[string][]byte{"key_1": key.SignPub()},
	)
	if err != nil {
		t.Fatal(err)
	}
	return key, verifier
}

// signedUnder signs a payload without Sign's checks, to reach refusals
// that Sign would stop first.
func signedUnder(
	t *testing.T, key *crypto.Keypair, context string, payload []byte,
) string {
	t.Helper()
	signature, err := crypto.Sign(key, context, payload)
	if err != nil {
		t.Fatal(err)
	}
	return Prefix + wire.Join(payload, signature)
}

func rawToken(t *testing.T, key *crypto.Keypair, payload []byte) string {
	t.Helper()
	return signedUnder(t, key, signingContext, payload)
}

// payload marshals claims, then lets edit change the decoded members, so
// a row can spell what a Claims value cannot.
func payload(t *testing.T, claims Claims, edit func(map[string]any)) []byte {
	t.Helper()
	raw, err := stdjson.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	if edit == nil {
		return raw
	}
	decoder := stdjson.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var members map[string]any
	if err := decoder.Decode(&members); err != nil {
		t.Fatal(err)
	}
	edit(members)
	raw, err = stdjson.Marshal(members)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// set returns an edit that sets each name to the value after it.
func set(pairs ...any) func(map[string]any) {
	return func(members map[string]any) {
		for index := 0; index < len(pairs); index += 2 {
			members[pairs[index].(string)] = pairs[index+1]
		}
	}
}

func drop(name string) func(map[string]any) {
	return func(members map[string]any) { delete(members, name) }
}

type claimFault struct {
	name string
	edit func(map[string]any)
	want error
}

func (fault claimFault) payload(t *testing.T) []byte {
	t.Helper()
	return payload(t, goodClaims(), fault.edit)
}

// claims reports the row as a Claims value, and false when none spells
// it: a member missing, null, of the wrong type or unknown.
func (fault claimFault) claims(t *testing.T) (Claims, bool) {
	t.Helper()
	raw := fault.payload(t)
	decoder := stdjson.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var claims Claims
	if decoder.Decode(&claims) != nil {
		return Claims{}, false
	}
	same := bytes.Equal(payload(t, claims, set()), raw)
	return claims, same
}

// invalidClaims are the faults that need no clock, so Sign and Verify
// must refuse every one alike.
func invalidClaims() []claimFault {
	long := strings.Repeat
	faults := []claimFault{
		{"v 2", set("v", 2), ErrVersion},
		{"aud a list", set("aud", []string{siteOrigin, "https://other.test"}),
			ErrClaims},
		{"iat a string", set("iat", "1700000000"), ErrClaims},
		{"an extra scope", set("scope", "x"), ErrClaims},
		{"an http iss", set("iss", "http://root.test"), ErrClaims},
		{"access admin", set("access", "admin"), ErrClaims},
		{"agent with a colon", set("agent", "a:b"), ErrClaims},
		{"agent with a leading space", set("agent", " Claude"), ErrClaims},
		{"an empty gid", set("gid", ""), ErrClaims},
		{"a 65-character gid", set("gid", long("g", 65)), ErrClaims},
		{"a 129-character sub", set("sub", long("s", 129)), ErrClaims},
		{"iat 0", set("iat", 0, "exp", 100), ErrLifetime},
		{"iat -1", set("iat", -1, "exp", 100), ErrLifetime},
		{"exp equal to iat", set("exp", issuedAt), ErrLifetime},
		{"exp before iat", set("exp", issuedAt-1), ErrLifetime},
		{"lifetime 31_622_401", set("exp", issuedAt+31_622_401),
			ErrLifetime},
	}
	for _, name := range memberNames {
		faults = append(faults,
			claimFault{name + " missing", drop(name), ErrClaims},
			claimFault{name + " null", set(name, nil), ErrClaims})
	}
	return faults
}

type refusal struct {
	name  string
	token string
	now   int64
	want  error
}

// wireRefusals are tokens refused before their claims are read.
func wireRefusals(t *testing.T, key *crypto.Keypair) []refusal {
	t.Helper()
	good, err := Sign(key, goodClaims())
	if err != nil {
		t.Fatal(err)
	}
	body := strings.TrimPrefix(good, Prefix)
	encoded, signature, _ := strings.Cut(body, ".")
	raw, _ := base64.RawURLEncoding.DecodeString(signature)
	padded := encoded + "." + base64.URLEncoding.EncodeToString(raw)
	duplicate := strings.Replace(
		string(payload(t, goodClaims(), nil)),
		`"sub":"person_1"`, `"sub":"person_1","sub":"person_2"`, 1,
	)
	return []refusal{
		{"no prefix", body, issuedAt, ErrMalformed},
		{"a second dot", good + ".x", issuedAt, ErrMalformed},
		{"padded base64", Prefix + padded, issuedAt, ErrMalformed},
		{"4097 bytes after the prefix", oversized(t, key), issuedAt,
			ErrMalformed},
		{"a duplicate sub", rawToken(t, key, []byte(duplicate)), issuedAt,
			ErrMalformed},
	}
}

// oversized is a correctly signed token one byte over the limit, so only
// its size can refuse it.
func oversized(t *testing.T, key *crypto.Keypair) string {
	t.Helper()
	base := payload(t, goodClaims(), nil)
	for size := 0; size < 4096; size++ {
		padded := append(base[:len(base)-1:len(base)-1],
			`,"pad":"`+strings.Repeat("x", size)+`"}`...)
		encoded := base64.RawURLEncoding.EncodedLen(len(padded))
		if encoded+1+86 == 4097 {
			return rawToken(t, key, padded)
		}
	}
	t.Fatal("no padding reaches 4097 bytes")
	return ""
}

// trustRefusals are well-formed tokens the verifier does not trust.
func trustRefusals(t *testing.T, key *crypto.Keypair) []refusal {
	t.Helper()
	other, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	good := payload(t, goodClaims(), nil)
	token := rawToken(t, key, good)
	edited := func(edit func(map[string]any)) string {
		return rawToken(t, key, payload(t, goodClaims(), edit))
	}
	const roster, assertion = "rastrillo/roster/v1", "rastrillo/assertion/v1"
	return []refusal{
		{"an unknown kid", edited(set("kid", "key_2")), issuedAt,
			ErrUnknownKey},
		{"signed by another key", rawToken(t, other, good), issuedAt,
			ErrSignature},
		{"signed as a roster", signedUnder(t, key, roster, good), issuedAt,
			ErrSignature},
		{"signed as an assertion", signedUnder(t, key, assertion, good),
			issuedAt, ErrSignature},
		{"another iss", edited(set("iss", "https://evil.test")), issuedAt,
			ErrIssuer},
		{"another aud", edited(set("aud", "https://other.test")), issuedAt,
			ErrAudience},
		{"exp equal to now", token, issuedAt + ninetyDays, ErrExpired},
		{"iat 31 seconds ahead", token, issuedAt - 31, ErrLifetime},
	}
}

func refused(t *testing.T, got Claims, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("got %v, want %v", err, want)
	}
	if got != (Claims{}) {
		t.Fatalf("a refusal returned %+v", got)
	}
}

func TestSignedTokenVerifies(t *testing.T) {
	key, verifier := setup(t)
	want := goodClaims()
	token, err := Sign(key, want)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(token, Prefix) {
		t.Fatalf("token %q lacks the prefix", token)
	}
	got, err := verifier.Verify(token, time.Unix(issuedAt, 0))
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestVerifyRefusesInvalidClaims(t *testing.T) {
	key, verifier := setup(t)
	for _, fault := range invalidClaims() {
		t.Run(fault.name, func(t *testing.T) {
			token := rawToken(t, key, fault.payload(t))
			got, err := verifier.Verify(token, time.Unix(issuedAt, 0))
			refused(t, got, err, fault.want)
		})
	}
}

func TestVerifyRefuses(t *testing.T) {
	key, verifier := setup(t)
	rows := append(wireRefusals(t, key), trustRefusals(t, key)...)
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			got, err := verifier.Verify(row.token, time.Unix(row.now, 0))
			refused(t, got, err, row.want)
		})
	}
	good, err := Sign(key, goodClaims())
	if err != nil {
		t.Fatal(err)
	}
	_, err = verifier.Verify(good, time.Unix(issuedAt-30, 0))
	if err != nil {
		t.Fatalf("iat thirty seconds ahead: %v", err)
	}
}

func TestSignRefusesInvalidClaims(t *testing.T) {
	key, _ := setup(t)
	var checked []string
	for _, fault := range invalidClaims() {
		claims, expressible := fault.claims(t)
		if !expressible {
			continue
		}
		checked = append(checked, fault.name)
		if _, err := Sign(key, claims); !errors.Is(err, fault.want) {
			t.Errorf("%s: got %v, want %v", fault.name, err, fault.want)
		}
	}
	// The rows a Claims value cannot spell are skipped, so name the rest:
	// a helper that skipped more would otherwise lose coverage silently.
	wantChecked := []string{
		"v 2", "an http iss", "access admin", "agent with a colon",
		"agent with a leading space", "an empty gid",
		"a 65-character gid", "a 129-character sub", "iat 0", "iat -1",
		"exp equal to iat", "exp before iat", "lifetime 31_622_401",
	}
	if !slices.Equal(checked, wantChecked) {
		t.Errorf("Sign checked %q, want %q", checked, wantChecked)
	}
	if _, err := Sign(nil, goodClaims()); !errors.Is(err, ErrSigning) {
		t.Errorf("nil key: got %v", err)
	}
	future := time.Now().Unix() + 365*24*60*60
	clockFree := map[string]func(map[string]any){
		"expired":              set("iat", 1000, "exp", 2000),
		"issued in the future": set("iat", future, "exp", future+1),
		"another audience":     set("aud", "https://other.test"),
		"the root's own door":  set("aud", rootOrigin),
		"366 days":             set("exp", issuedAt+31_622_400),
	}
	for name, edit := range clockFree {
		claims, _ := claimFault{name, edit, nil}.claims(t)
		if _, err := Sign(key, claims); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestNewVerifierRefusesBadConfiguration(t *testing.T) {
	key, _ := setup(t)
	one := map[string][]byte{"key_1": key.SignPub()}
	nine := map[string][]byte{}
	for index := range 9 {
		nine["key_"+string(rune('a'+index))] = key.SignPub()
	}
	cases := map[string]struct {
		issuer string
		keys   map[string][]byte
	}{
		"http issuer": {"http://root.test", one},
		"zero keys":   {rootOrigin, map[string][]byte{}},
		"nine keys":   {rootOrigin, nine},
		"64-byte key": {rootOrigin, map[string][]byte{
			"key_1": key.SignPub()[1:],
		}},
	}
	for name, config := range cases {
		verifier, err := NewVerifier(config.issuer, siteOrigin, config.keys)
		if !errors.Is(err, ErrConfig) || verifier != nil {
			t.Errorf("%s: got %v, %v", name, verifier, err)
		}
	}
}

func TestNewVerifierAllowsTheRootsOwnDoor(t *testing.T) {
	key, _ := setup(t)
	verifier, err := NewVerifier(
		rootOrigin, rootOrigin, map[string][]byte{"key_1": key.SignPub()},
	)
	if err != nil {
		t.Fatal(err)
	}
	want := goodClaims()
	want.Audience = rootOrigin
	token, err := Sign(key, want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := verifier.Verify(token, time.Unix(issuedAt, 0))
	if err != nil || got != want {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestGoldenVector(t *testing.T) {
	var vector struct {
		PublicKey string `json:"publicKey"`
		Now       int64  `json:"now"`
		Claims    Claims `json:"claims"`
		Token     string `json:"token"`
	}
	raw, err := os.ReadFile("testdata/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := stdjson.Unmarshal(raw, &vector); err != nil {
		t.Fatal(err)
	}
	public, err := base64.RawURLEncoding.DecodeString(vector.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := NewVerifier(
		vector.Claims.Issuer, vector.Claims.Audience,
		map[string][]byte{vector.Claims.KeyID: public},
	)
	if err != nil {
		t.Fatal(err)
	}
	got, err := verifier.Verify(vector.Token, time.Unix(vector.Now, 0))
	if err != nil || got != vector.Claims {
		t.Fatalf("got %+v, %v; want %+v", got, err, vector.Claims)
	}
}

func TestRedactHidesGrantTokens(t *testing.T) {
	cases := map[string]string{
		"Authorization: Bearer rastrillo_grant_eyJ2.abc-_": "Authorization: " +
			"Bearer rastrillo_grant_[redacted]",
		"Authorization: Bearer tickets_k3Jx9QpL2mWa_secret": "Authorization: " +
			"Bearer tickets_k3Jx9QpL2mWa_secret",
	}
	for text, want := range cases {
		if got := Redact(text); got != want {
			t.Errorf("Redact(%q) = %q, want %q", text, got, want)
		}
	}
}
