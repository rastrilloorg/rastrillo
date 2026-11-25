package secondfactor_test

import (
	"database/sql"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/secondfactor"
	"amadan.net/rastrillo/rastrillo/sessions"
)

// The half-session and the one recovery code are fixed and stored
// afresh before each run, so a run that redeemed or exhausted them does
// not change what the next one is judged on.
const (
	fuzzPending = "pending-token"
	fuzzCode    = "abcdefghij"
)

// FuzzSignInRecovery posts arbitrary form bodies and pending cookies to
// the recovery endpoint, the way out when a factor is lost. Only the
// stored code, retyped in any case and grouping, may complete the
// sign-in, and only for the half-session it was presented with.
func FuzzSignInRecovery(f *testing.F) {
	env := newEnv(f)
	gate, err := secondfactor.New(secondfactor.Config{
		Sessions: env.sess, DB: env.db, Origin: testOrigin,
		Logger: slog.New(slog.DiscardHandler),
	})
	if err != nil {
		f.Fatal(err)
	}
	gate.Add(factor{enrolled: map[string]bool{"fuzz-subject": true}})
	f.Add("code=abcde-fghij", fuzzPending)
	f.Add("code=ABCDE+FGHIJ", fuzzPending)
	f.Add("code=abcdefghij", "another-token")
	f.Add("code=", fuzzPending)
	f.Add("code=zzzzz-zzzzz&code=abcde-fghij", fuzzPending)
	f.Add("code=%61bcdefghij;x=1", fuzzPending)
	f.Add("code=%ZZ", "")
	f.Add("code="+fuzzCode, fuzzPending+"; theme=dark")
	f.Add("code="+fuzzCode, `"`+fuzzPending+`"`)
	f.Add("code="+fuzzCode, "x; rastrillo_secondfactor="+fuzzPending)
	post := func(t testing.TB, body, cookie string) *httptest.ResponseRecorder {
		storeFuzzRecovery(t, env.db)
		request := httptest.NewRequest("POST", testOrigin+"/signin/recovery",
			strings.NewReader(body))
		request.Header.Set("Content-Type",
			"application/x-www-form-urlencoded")
		request.Header.Set("Cookie", "rastrillo_secondfactor="+cookie)
		response := httptest.NewRecorder()
		gate.SignInRecovery(response, request)
		return response
	}
	// A stored code that stopped redeeming would leave the target
	// fuzzing nothing but refusals.
	valid := post(f, "code="+fuzzCode, fuzzPending)
	if valid.Header().Get("Location") != "/home" {
		f.Fatalf("the stored code answered %d to %q", valid.Code,
			valid.Header().Get("Location"))
	}
	f.Fuzz(func(t *testing.T, body, cookie string) {
		response := post(t, body, cookie)
		if response.Code >= 500 {
			t.Fatalf("%q with %q answered %d", body, cookie, response.Code)
		}
		if _, signedIn := sessionFrom(t, env, response); !signedIn {
			return
		}
		form, _ := url.ParseQuery(body)
		typed := strings.NewReplacer("-", "", " ", "").Replace(
			strings.ToLower(form.Get("code")))
		// The credential is the cookie's value as net/http parses it:
		// another cookie beside it, or quotes around it, change the
		// header and not the token presented.
		parsed := &http.Request{Header: http.Header{
			"Cookie": {"rastrillo_secondfactor=" + cookie},
		}}
		presented, err := parsed.Cookie("rastrillo_secondfactor")
		if typed != fuzzCode || err != nil ||
			presented.Value != fuzzPending ||
			response.Header().Get("Location") != "/home" {
			t.Fatalf("%q with %q signed in to %q", body, cookie,
				response.Header().Get("Location"))
		}
	})
}

func storeFuzzRecovery(t testing.TB, db *sql.DB) {
	t.Helper()
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`INSERT OR REPLACE INTO secondfactor_pending (token_hash,
			subject, method, return_to, attempts, expires_at)
			VALUES (?, 'fuzz-subject', 'magiclink', '/home', 0,
			'2999-01-01T00:00:00Z')`,
			[]any{sessions.HashToken(fuzzPending)}},
		{`INSERT OR REPLACE INTO secondfactor_recovery_codes
			(code_hash, subject, created_at)
			VALUES (?, 'fuzz-subject', '2026-01-01T00:00:00Z')`,
			[]any{sessions.HashToken(fuzzCode)}},
	} {
		if _, err := db.Exec(statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
}
