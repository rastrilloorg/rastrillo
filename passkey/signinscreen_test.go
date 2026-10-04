package passkey_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/auth"
	"amadan.net/rastrillo/rastrillo/db"
	"amadan.net/rastrillo/rastrillo/lastsignin"
	"amadan.net/rastrillo/rastrillo/migrate"
	"amadan.net/rastrillo/rastrillo/passkey"
	"amadan.net/rastrillo/rastrillo/secondfactor"
	"amadan.net/rastrillo/rastrillo/sessions"
	"amadan.net/rastrillo/rastrillo/webauthn/authtest"
)

func jarFor(t *testing.T, mode lastsignin.Mode) *lastsignin.Jar {
	t.Helper()
	j, err := lastsignin.New(lastsignin.Config{Origin: testOrigin, InstanceKey: "test-instance-key", AttemptCookie: "rastrillo_attempt", Mode: mode})
	if err != nil {
		t.Fatal(err)
	}
	return j
}

func rememberedCookie(t *testing.T, j *lastsignin.Jar, rec lastsignin.Record) *http.Cookie {
	t.Helper()
	w := httptest.NewRecorder()
	j.Remember(w, rec)
	cs := w.Result().Cookies()
	if len(cs) != 1 {
		t.Fatalf("Remember wrote %d cookies", len(cs))
	}
	return cs[0]
}

// discoverCarrying is discover with cookies on the finish request — the
// attempt and remembered cookies a browser on the sign-in screen sends.
func discoverCarrying(t *testing.T, e env, a *authtest.Authenticator, opts authtest.Options, cookies []*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	challenge := challengeFrom(t, postJSON(t, e.h.DiscoverBegin, nil, nil))
	if opts.RPID == "" {
		opts.RPID, opts.Origin = testRPID, testOrigin
	}
	clientData, authData, sig, err := a.Get(challenge, opts)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{
		"id": b64(a.CredID), "clientDataJSON": b64(clientData), "authenticatorData": b64(authData), "signature": b64(sig),
	})
	r := httptest.NewRequest(http.MethodPost, testOrigin+"/passkey/discover/finish", bytes.NewReader(body))
	for _, c := range cookies {
		r.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value})
	}
	w := httptest.NewRecorder()
	e.h.DiscoverFinish(w, r)
	return w
}

// written names every cookie a response sets, a deletion as "name:clear".
func written(w *httptest.ResponseRecorder) map[string]*http.Cookie {
	out := map[string]*http.Cookie{}
	for _, c := range w.Result().Cookies() {
		name := c.Name
		if c.MaxAge < 0 {
			name += ":clear"
		}
		out[name] = c
	}
	return out
}

func names(m map[string]*http.Cookie) []string {
	var out []string
	for n := range m {
		out = append(out, n)
	}
	return out
}

func TestDiscoverWithAJarEndsTheAttemptAndRemembersThePasskey(t *testing.T) {
	for _, held := range []bool{false, true} {
		j := jarFor(t, lastsignin.On)
		e := newEnvWith(t, func(c *passkey.Config) {
			c.Remember = j
			if held {
				c.OtherFactor = func(string) (bool, error) { return true, nil }
			}
		})
		a, _ := authtest.New()
		enroll(t, e, e.signIn(t, "alice"), a)
		cookies := []*http.Cookie{
			{Name: "rastrillo_attempt", Value: "an earlier typed address"},
			rememberedCookie(t, j, lastsignin.Record{Method: "magiclink", Address: "alice@example.com"}),
		}
		opts := authtest.Options{}
		if held {
			opts.Flags = authtest.FlagUserPresent
		}
		w := discoverCarrying(t, e, a, opts, cookies)
		if w.Code != http.StatusOK {
			t.Fatalf("held=%v: discover finish %d %s", held, w.Code, w.Body.String())
		}
		got := written(w)
		if got["rastrillo_attempt:clear"] == nil {
			t.Errorf("held=%v: the attempt cookie was not ended; wrote %v", held, names(got))
		}
		last := got["rastrillo_last_signin"]
		if last == nil {
			t.Fatalf("held=%v: nothing remembered; wrote %v", held, names(got))
		}
		r := httptest.NewRequest(http.MethodGet, testOrigin+"/signin", nil)
		r.AddCookie(&http.Cookie{Name: last.Name, Value: last.Value})
		if rec, res := j.Read(r); res != lastsignin.Valid || rec != (lastsignin.Record{Method: "passkey"}) {
			t.Errorf("held=%v: remembered %+v, %v; a passkey record carries no address", held, rec, res)
		}
		if held && got["rastrillo_secondfactor"] == nil {
			t.Errorf("held path wrote %v, want the half-session too", names(got))
		}
	}
}

func TestAFailedAssertionTouchesNeitherCookie(t *testing.T) {
	j := jarFor(t, lastsignin.On)
	e := newEnvWith(t, func(c *passkey.Config) { c.Remember = j })
	a, _ := authtest.New()
	enroll(t, e, e.signIn(t, "alice"), a)
	stranger, _ := authtest.New()
	w := discoverCarrying(t, e, stranger, authtest.Options{}, []*http.Cookie{{Name: "rastrillo_attempt", Value: "x"}})
	if w.Code != http.StatusBadRequest || len(w.Result().Cookies()) != 0 {
		t.Fatalf("a failed assertion → %d, wrote %v", w.Code, names(written(w)))
	}
}

func TestAnUnadmittedPasskeyEndsTheAttemptAndRemembersNothing(t *testing.T) {
	j := jarFor(t, lastsignin.On)
	e := newEnvWith(t, func(c *passkey.Config) {
		c.Remember = j
		c.Authorize = func(string) bool { return false }
	})
	a, _ := authtest.New()
	enroll(t, e, e.signIn(t, "alice"), a)
	got := written(discoverCarrying(t, e, a, authtest.Options{}, nil))
	if got["rastrillo_attempt:clear"] == nil || got["rastrillo_last_signin"] != nil {
		t.Fatalf("an unadmitted passkey wrote %v", names(got))
	}
}

// TestDiscoverWithoutTheScreenIsToday: no jar, or the screen's jar with
// the screen off, sets nothing beyond today's session cookie — or, on
// the held path, today's half-session cookie (round 4, finding 29).
func TestDiscoverWithoutTheScreenIsToday(t *testing.T) {
	for name, jar := range map[string]*lastsignin.Jar{"no jar": nil, "an Off jar": jarFor(t, lastsignin.Off)} {
		for _, held := range []bool{false, true} {
			e := newEnvWith(t, func(c *passkey.Config) {
				c.Remember = jar
				if held {
					c.OtherFactor = func(string) (bool, error) { return true, nil }
				}
			})
			a, _ := authtest.New()
			enroll(t, e, e.signIn(t, "alice"), a)
			opts := authtest.Options{}
			want := "rastrillo_session"
			if held {
				opts.Flags, want = authtest.FlagUserPresent, "rastrillo_secondfactor"
			}
			got := written(discoverCarrying(t, e, a, opts, nil))
			if len(got) != 1 || got[want] == nil {
				t.Errorf("%s, held=%v: wrote %v, want only %s", name, held, names(got), want)
			}
		}
	}
}

type discardMailer struct{ body string }

func (m *discardMailer) Send(_ context.Context, _, _, body string) error { m.body = body; return nil }

// TestAPasskeySignInLeavesNoTypedAddressBehind is round 3's finding 25
// end to end: this browser typed Alice's address and was remembered as
// her; Bob then signs in with his passkey on it. The next sign-in page
// must offer neither Alice's address nor her method.
func TestAPasskeySignInLeavesNoTypedAddressBehind(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "screen.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if _, err := migrate.Apply(context.Background(), d, migrate.Merge(sessions.Schema, auth.Schema, passkey.Schema, secondfactor.Schema)); err != nil {
		t.Fatal(err)
	}
	au, err := auth.New(auth.Config{DB: d.Writer(), Origin: testOrigin, InstanceKey: "test-instance-key", Mailer: &discardMailer{}, SigninScreen: true, ProofOff: true})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := sessions.New(sessions.Config{DB: d.Writer(), Origin: testOrigin})
	if err != nil {
		t.Fatal(err)
	}
	h, err := passkey.New(passkey.Config{Sessions: sess, DB: d.Writer(), Origin: testOrigin, Remember: au.RememberJar()})
	if err != nil {
		t.Fatal(err)
	}
	e := env{h: h, sess: sess, db: d.Writer()}
	bob, _ := authtest.New()
	enroll(t, e, e.signIn(t, "bob"), bob)

	jar := map[string]*http.Cookie{}
	keep := func(w *httptest.ResponseRecorder) {
		for _, c := range w.Result().Cookies() {
			if c.MaxAge < 0 {
				delete(jar, c.Name)
			} else {
				jar[c.Name] = c
			}
		}
	}
	form := url.Values{"address": {"alice@example.com"}, "force": {"1"}}
	r := httptest.NewRequest(http.MethodPost, testOrigin+"/signin", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	w := httptest.NewRecorder()
	au.Begin(w, r)
	keep(w)
	w = httptest.NewRecorder()
	au.RememberJar().Remember(w, lastsignin.Record{Method: "magiclink", Address: "alice@example.com"})
	keep(w)

	var cookies []*http.Cookie
	for _, c := range jar {
		cookies = append(cookies, c)
	}
	keep(discoverCarrying(t, e, bob, authtest.Options{}, cookies))

	page := httptest.NewRequest(http.MethodGet, testOrigin+"/signin", nil)
	for _, c := range jar {
		page.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value})
	}
	st := au.SigninState(page)
	if st.Address != "" {
		t.Fatalf("after Bob's passkey sign-in the form is prefilled with %q", st.Address)
	}
	if st.Remembered == nil || *st.Remembered != (auth.Remembered{Method: "passkey"}) {
		t.Fatalf("remembered %+v, want a passkey with no address", st.Remembered)
	}
}
