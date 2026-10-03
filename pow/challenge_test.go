package pow

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

var testKey = []byte("0123456789abcdef0123456789abcdef")

func TestSealInputIsInjective(t *testing.T) {
	// v1 joined fields with NUL, so a scope ending in "\x00b" and a
	// nonce starting with "b\x00" signed the same bytes and a token
	// moved between scopes without forging anything.
	at := time.UnixMilli(1_700_000_000_000)
	a := Challenge{Scope: "a\x00b", Nonce: "n", Issued: at, Expires: at}
	b := Challenge{Scope: "a", Nonce: "b\x00n", Issued: at, Expires: at}
	if bytes.Equal(sealInput(a), sealInput(b)) {
		t.Fatal("two different (scope, nonce) pairs sign the same bytes")
	}
	c := Challenge{Scope: "\x01x", Nonce: "y", Issued: at, Expires: at}
	d := Challenge{Scope: "", Nonce: "\x01xy", Issued: at, Expires: at}
	if bytes.Equal(sealInput(c), sealInput(d)) {
		t.Fatal("a length-like scope byte collides")
	}
}

func TestNewChallengeIsMillisecondAndSealsExpiry(t *testing.T) {
	issued := time.Unix(1_700_000_000, 999_999_999) // late in a second
	c := newChallenge(testKey, issued, 2*time.Hour, "s", 10, 0)
	if c.Issued.UnixMilli() != issued.UnixMilli() {
		t.Fatalf("issued = %v, want millisecond %v", c.Issued.UnixMilli(), issued.UnixMilli())
	}
	if got := c.Expires.Sub(c.Issued); got != 2*time.Hour {
		t.Fatalf("sealed lifetime = %v, want 2h", got)
	}
	if !validNonce(c.Nonce) {
		t.Fatalf("minted nonce %q is not 32 lowercase hex", c.Nonce)
	}
	if !sealOK(testKey, c) {
		t.Fatal("a fresh challenge does not verify")
	}
}

func TestSealRefusesEveryEditedField(t *testing.T) {
	base := newChallenge(testKey, time.Now(), time.Hour, "s", 10, 0)
	edits := map[string]func(*Challenge){
		"scope":      func(c *Challenge) { c.Scope = "t" },
		"nonce":      func(c *Challenge) { c.Nonce = strings.Repeat("0", 32) },
		"issued":     func(c *Challenge) { c.Issued = c.Issued.Add(time.Millisecond) },
		"expires":    func(c *Challenge) { c.Expires = c.Expires.Add(time.Hour) },
		"difficulty": func(c *Challenge) { c.Difficulty = 1 },
		"flags":      func(c *Challenge) { c.Flags = flagTrapOmitted },
	}
	for name, edit := range edits {
		c := base
		edit(&c)
		if sealOK(testKey, c) {
			t.Errorf("editing %s kept the seal valid", name)
		}
	}
}

func postValues(v url.Values) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(v.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.ParseForm()
	return r
}

func valuesOf(c Challenge) url.Values {
	return url.Values{
		fieldScope:      {c.Scope},
		fieldNonce:      {c.Nonce},
		fieldIssued:     {strconv.FormatInt(c.Issued.UnixMilli(), 10)},
		fieldExpires:    {strconv.FormatInt(c.Expires.UnixMilli(), 10)},
		fieldDifficulty: {strconv.Itoa(c.Difficulty)},
		fieldFlags:      {strconv.Itoa(int(c.Flags))},
		fieldSeal:       {c.Seal},
	}
}

func TestReadChallengeRoundTripsFields(t *testing.T) {
	c := newChallenge(testKey, time.Now(), time.Hour, "rastrillo/auth/begin", 12, flagTrapOmitted)
	got, reason := readChallenge(postValues(valuesOf(c)))
	if reason != "" {
		t.Fatalf("readChallenge refused a good challenge: %s", reason)
	}
	if got != c {
		t.Fatalf("round trip = %+v, want %+v", got, c)
	}
}

func TestReadChallengeCallsAPostWithNoFieldsMissing(t *testing.T) {
	if _, reason := readChallenge(postValues(url.Values{"address": {"a@b.c"}})); reason != ReasonMissing {
		t.Fatalf("no challenge fields = %q, want missing", reason)
	}
}

func TestReadChallengeRefusesMalformedFields(t *testing.T) {
	// Review focus 2: garbage is refused before any hashing beyond the
	// HMAC and before the database, with one reason for all of it.
	good := newChallenge(testKey, time.Now(), time.Hour, "s", 10, 0)
	cases := map[string]func(url.Values){
		"huge scope":    func(v url.Values) { v.Set(fieldScope, strings.Repeat("x", 1<<20)) },
		"short nonce":   func(v url.Values) { v.Set(fieldNonce, "abc") },
		"upper nonce":   func(v url.Values) { v.Set(fieldNonce, strings.ToUpper(good.Nonce)) },
		"bad issued":    func(v url.Values) { v.Set(fieldIssued, "soon") },
		"bad expires":   func(v url.Values) { v.Set(fieldExpires, "") },
		"negative bits": func(v url.Values) { v.Set(fieldDifficulty, "-1") },
		"huge bits":     func(v url.Values) { v.Set(fieldDifficulty, "100000") },
		"bad flags":     func(v url.Values) { v.Set(fieldFlags, "999") },
		"short seal":    func(v url.Values) { v.Set(fieldSeal, "00") },
	}
	for name, edit := range cases {
		v := valuesOf(good)
		edit(v)
		if _, reason := readChallenge(postValues(v)); reason != ReasonSealInvalid {
			t.Errorf("%s: reason %q, want seal_invalid", name, reason)
		}
	}
}

func TestAV1PostIsRefused(t *testing.T) {
	// v1 posted pow_issued_at and no scope or expiry. Its seal cannot
	// verify under v2, and its shape is refused before the HMAC.
	v1 := url.Values{
		"pow_nonce":      {strings.Repeat("ab", 16)},
		"pow_issued_at":  {"1800000000"},
		"pow_difficulty": {"18"},
		"pow_seal":       {strings.Repeat("0", 64)},
		"pow_counter":    {"12345"},
	}
	if _, reason := readChallenge(postValues(v1)); reason != ReasonSealInvalid {
		t.Fatalf("a v1 post = %q, want seal_invalid", reason)
	}
}

func TestFieldsCarryEverySealedFieldAndTheTrapUnlessOmitted(t *testing.T) {
	c := newChallenge(testKey, time.Now(), time.Hour, "s", 10, 0)
	html := string(c.Fields())
	for _, name := range []string{fieldScope, fieldNonce, fieldIssued, fieldExpires, fieldDifficulty, fieldFlags, fieldSeal, fieldCounter, fieldHoneypot} {
		if !strings.Contains(html, `name="`+name+`"`) {
			t.Errorf("Fields has no %s", name)
		}
	}
	trapless := newChallenge(testKey, time.Now(), time.Hour, "s", 10, flagTrapOmitted)
	if strings.Contains(string(trapless.Fields()), `name="`+fieldHoneypot+`"`) {
		t.Error("a trap-omitted challenge still renders the honeypot: a password manager that filled it once fills it again")
	}
}
