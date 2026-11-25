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

// FuzzReadChallenge posts arbitrary form bodies, as the sign-in door
// receives them, through readChallenge and the seal check. A challenge
// that passes the seal must be the one that was sealed, field for
// field: a second spelling of any sealed value (an issue time, a
// difficulty, the flags) would be a challenge the server never minted.
func FuzzReadChallenge(f *testing.F) {
	key := bytes.Repeat([]byte{2}, 32)
	issued := time.UnixMilli(1_700_000_000_000)
	sealed := Challenge{
		Scope: "signin", Nonce: strings.Repeat("ab", 16),
		Issued: issued, Expires: issued.Add(DefaultMaxAge),
		Difficulty: 12, Flags: flagBound,
	}
	sealed.Seal = sealOf(key, sealed)
	form := url.Values{
		fieldScope: {sealed.Scope}, fieldNonce: {sealed.Nonce},
		fieldIssued:     {strconv.FormatInt(issued.UnixMilli(), 10)},
		fieldExpires:    {strconv.FormatInt(sealed.Expires.UnixMilli(), 10)},
		fieldDifficulty: {"12"}, fieldFlags: {"2"}, fieldSeal: {sealed.Seal},
	}
	valid := form.Encode()
	// A seed that stopped passing the seal would leave the property
	// below with nothing to hold.
	got, reason := readChallenge(&http.Request{PostForm: form})
	if reason != "" || !sealOK(key, got) {
		f.Fatalf("the sealed challenge reads as %q", reason)
	}
	f.Add(valid)
	f.Add(strings.Replace(valid, "pow_flags=2", "pow_flags=02", 1))
	f.Add(strings.Replace(valid, "pow_flags=2", "pow_flags=258", 1))
	f.Add(strings.Replace(valid, "pow_difficulty=12", "pow_difficulty=+12", 1))
	f.Add(valid + "&pow_scope=other")
	f.Add("pow_nonce=&pow_seal=")
	f.Fuzz(func(t *testing.T, body string) {
		request := httptest.NewRequest("POST", "/signin",
			strings.NewReader(body))
		request.Header.Set("Content-Type",
			"application/x-www-form-urlencoded")
		if request.ParseForm() != nil {
			return
		}
		got, reason := readChallenge(request)
		if reason != "" || !sealOK(key, got) {
			return
		}
		if got.Scope != sealed.Scope || got.Nonce != sealed.Nonce ||
			!got.Issued.Equal(sealed.Issued) ||
			!got.Expires.Equal(sealed.Expires) ||
			got.Difficulty != sealed.Difficulty ||
			got.Flags != sealed.Flags {
			t.Fatalf("%q passed the seal as %+v; only %+v was sealed",
				body, got, sealed)
		}
	})
}
