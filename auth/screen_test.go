package auth

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/crypto"
	"amadan.net/rastrillo/rastrillo/internal/sealedcookie"
	"amadan.net/rastrillo/rastrillo/lastsignin"
	"amadan.net/rastrillo/rastrillo/secondfactor"
	"amadan.net/rastrillo/rastrillo/sessions"
)

func ptr[T any](v T) *T { return &v }

// newScreenAuth is newTestAuth with the shipped screen switched on.
func newScreenAuth(t *testing.T, mut func(*Config)) (*Auth, *captureMailer) {
	t.Helper()
	return newTestAuth(t, func(c *Config) {
		c.SigninScreen = true
		if mut != nil {
			mut(c)
		}
	})
}

// browser is one cookie jar applied the way a browser applies
// Set-Cookie: a later value wins, a deletion removes. The two-tab tests
// stage "the pair a browser installed last" by copying cookies from one
// jar into another, which is exactly the state the spec reasons about.
type browser struct{ jar map[string]*http.Cookie }

func newBrowser() *browser { return &browser{jar: map[string]*http.Cookie{}} }

func (b *browser) apply(w *httptest.ResponseRecorder) {
	for _, c := range w.Result().Cookies() {
		if c.MaxAge < 0 {
			delete(b.jar, c.Name)
			continue
		}
		b.jar[c.Name] = c
	}
}

// request builds one request from this browser: its cookies, and — for
// a POST — the same-origin evidence a real form submission carries.
func (b *browser) request(method, target string, form url.Values) *http.Request {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	r := httptest.NewRequest(method, "http://app.test"+target, body)
	if form != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if method == http.MethodPost {
		r.Header.Set("Sec-Fetch-Site", "same-origin")
	}
	for _, c := range b.jar {
		r.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value})
	}
	return r
}

func (b *browser) do(h http.HandlerFunc, method, target string, form url.Values) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h(w, b.request(method, target, form))
	b.apply(w)
	return w
}

func (b *browser) clone() *browser {
	c := newBrowser()
	for k, v := range b.jar {
		c.jar[k] = v
	}
	return c
}

func (b *browser) cookie(name string) *http.Cookie { return b.jar[name] }

// setCookies names every cookie a response writes, a deletion as
// "name:clear", sorted — the shape "exactly today's cookies" is
// asserted in.
func setCookies(w *httptest.ResponseRecorder) []string {
	var out []string
	for _, c := range w.Result().Cookies() {
		name := c.Name
		if c.MaxAge < 0 {
			name += ":clear"
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// cookieShape is every attribute of every Set-Cookie except its value —
// what two answers must share to be indistinguishable to a watcher.
func cookieShape(w *httptest.ResponseRecorder) []string {
	var out []string
	for _, c := range w.Result().Cookies() {
		out = append(out, fmt.Sprintf("%s path=%s httponly=%v samesite=%v secure=%v maxage=%d",
			c.Name, c.Path, c.HttpOnly, c.SameSite, c.Secure, c.MaxAge))
	}
	sort.Strings(out)
	return out
}

var attemptParam = regexp.MustCompile(`attempt=[A-Za-z0-9_-]+`)

// redirectShape is the Location with the random attempt id blanked.
func redirectShape(w *httptest.ResponseRecorder) string {
	return attemptParam.ReplaceAllString(w.Header().Get("Location"), "attempt=ID")
}

type alwaysEnrolled struct{}

func (alwaysEnrolled) Enrolled(string) (bool, error) { return true, nil }

// holdingGate is a real secondfactor.Gate that holds every sign-in, so
// a test can see the cookies the held path writes today.
func holdingGate(t *testing.T, a *Auth) *secondfactor.Gate {
	t.Helper()
	sess, err := sessions.New(sessions.Config{DB: a.cfg.DB, Origin: a.cfg.Origin})
	if err != nil {
		t.Fatal(err)
	}
	g, err := secondfactor.New(secondfactor.Config{Sessions: sess, DB: a.cfg.DB, Origin: a.cfg.Origin})
	if err != nil {
		t.Fatal(err)
	}
	g.Add(alwaysEnrolled{})
	return g
}

type failingMailer struct{}

func (failingMailer) Send(context.Context, string, string, string) error {
	return errors.New("the mail server is down")
}

func pathOf(link string) string { return strings.TrimPrefix(link, "http://app.test") }

// redeem is pressing Sign in on the page an emailed link lands on: the
// link's token posted back to its path. Opening the link spends
// nothing (confirm_test.go), so every sign-in here posts.
func (b *browser) redeem(a *Auth, link string) *httptest.ResponseRecorder {
	u, err := url.Parse(link)
	if err != nil {
		panic(err)
	}
	return b.do(a.Verify, http.MethodPost, u.Path, url.Values{"token": {u.Query().Get("token")}})
}

// TestScreenOffIsToday holds the switch's promise on every path this
// task touches: with SigninScreen off and KeymailServers unset, whatever
// Remember says, every answer and every cookie is what it was before
// the screen existed — including the second-factor cookie a held
// sign-in writes (round 4, finding 29). The keymail branch's rows are
// in TestScreenOffKeymailIsToday (Task 5).
func TestScreenOffIsToday(t *testing.T) {
	for _, remember := range []*bool{nil, ptr(true), ptr(false)} {
		name := "nil"
		if remember != nil {
			name = strconv.FormatBool(*remember)
		}
		t.Run("Remember="+name, func(t *testing.T) {
			a, m := newTestAuth(t, func(c *Config) { c.Remember = remember })
			b := newBrowser()
			post := func(h http.HandlerFunc, address string, extra url.Values) *httptest.ResponseRecorder {
				form := url.Values{"address": {address}, "force": {"1"}}
				for k, v := range extra {
					form[k] = v
				}
				return b.do(h, http.MethodPost, "/signin", form)
			}
			want := func(what string, w *httptest.ResponseRecorder, loc string, cookies []string) {
				t.Helper()
				if w.Code != http.StatusSeeOther || w.Header().Get("Location") != loc {
					t.Errorf("%s: %d → %q, want 303 → %q", what, w.Code, w.Header().Get("Location"), loc)
				}
				if got := setCookies(w); !reflect.DeepEqual(got, cookies) {
					t.Errorf("%s: Set-Cookie %v, want %v", what, got, cookies)
				}
			}

			want("a link", post(a.Begin, "ada@example.com", url.Values{"expect": {"keymail"}}), "/signin?sent=1", nil)
			adaLink := linkRE.FindString(m.sentBody())
			// These posts all carry force=1, and an error redirect now keeps
			// it (problemURL): a visitor who chose the link must not be
			// classified back to a provider by their next typo.
			want("a bad address", post(a.Begin, "not an address", nil), "/signin?err=address&force=1", nil)
			for i := 0; i < 5; i++ {
				post(a.Begin, "bea@example.com", nil)
			}
			want("over budget", post(a.Begin, "bea@example.com", nil), "/signin?err=rate&force=1", nil)
			want("AnswerAsSent", post(a.AnswerAsSent, "cal@example.com", nil), "/signin?sent=1", nil)

			want("admit", b.redeem(a, adaLink), "/", []string{"rastrillo_session"})

			a.cfg.SecondFactor = holdingGate(t, a).Hold
			post(a.Begin, "dee@example.com", nil)
			want("admit, held", b.redeem(a, linkRE.FindString(m.sentBody())),
				"/signin/confirm", []string{"rastrillo_secondfactor"})

			a.flow.Mailer = failingMailer{}
			want("a failure", post(a.Begin, "eve@example.com", nil), "/signin?err=1&force=1", nil)
		})
	}
}

func TestBeginWritesTheAttempt(t *testing.T) {
	a, _ := newScreenAuth(t, nil)
	b := newBrowser()
	current := func() attempt {
		t.Helper()
		at, st := a.openAttempt(b.request(http.MethodGet, "/signin", nil))
		if st != cookieValid {
			t.Fatalf("attempt cookie is %v, want valid", st)
		}
		return at
	}
	post := func(form url.Values) *httptest.ResponseRecorder {
		return b.do(a.Begin, http.MethodPost, "/signin", form)
	}

	w := post(url.Values{"address": {"  ada@example.com "}, "force": {"1"}})
	id, ok := strings.CutPrefix(w.Header().Get("Location"), "/signin?sent=1&attempt=")
	if !ok || id == "" {
		t.Fatalf("a sent link redirected to %q", w.Header().Get("Location"))
	}
	if at := current(); at.ID != id || at.K != attemptLink || at.A != "ada@example.com" || at.X {
		t.Fatalf("attempt = %+v, want the link just sent, trimmed, with no expect", at)
	}
	var c *http.Cookie
	for _, sc := range w.Result().Cookies() {
		if sc.Name == "rastrillo_attempt" {
			c = sc
		}
	}
	if c == nil || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.MaxAge != 900 {
		t.Fatalf("attempt cookie = %+v", c)
	}

	post(url.Values{"address": {"ada@example.com"}, "force": {"1"}, "expect": {"keymail"}})
	if at := current(); !at.X {
		t.Fatal("expect=keymail with a link sent did not record the surprise")
	}

	if w := post(url.Values{"address": {"not an address"}, "force": {"1"}}); w.Header().Get("Location") != "/signin?err=address&force=1" { // force survives the error: see problemURL
		t.Fatalf("bad address → %q", w.Header().Get("Location"))
	}
	if at := current(); at.K != attemptProblem || at.A != "not an address" {
		t.Fatalf("a refused address left attempt %+v, want kind problem with what was typed", at)
	}

	long := strings.Repeat("a", 250) + "@example.com"
	post(url.Values{"address": {long}, "force": {"1"}})
	if at := current(); at.A != "" {
		t.Fatalf("a %d-byte address was kept as %q; over 254 bytes it is dropped, not truncated", len(long), at.A)
	}

	var rated *httptest.ResponseRecorder
	for i := 0; i < 6; i++ {
		rated = post(url.Values{"address": {"fay@example.com"}, "force": {"1"}})
	}
	if rated.Header().Get("Location") != "/signin?err=rate&force=1" { // force survives the error: see problemURL
		t.Fatalf("sixth try → %q, want ?err=rate", rated.Header().Get("Location"))
	}
	if at := current(); at.K != attemptProblem || at.A != "fay@example.com" {
		t.Fatalf("rate-limited attempt = %+v", at)
	}
}

func TestAnAttemptOpensOnlyWhereItWasSealed(t *testing.T) {
	a, _ := newScreenAuth(t, nil)
	now := a.now().Unix()
	good := attempt{O: a.cfg.Origin, ID: "id", IAT: now, EXP: now + 900, K: attemptLink, A: "ada@example.com"}
	seal := func(key []byte, p any) string {
		v, err := sealedcookie.Seal(key, p)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	check := func(name, value string, want cookieState) {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "http://app.test/signin", nil)
		if value != "" {
			r.AddCookie(&http.Cookie{Name: a.attemptCookie(), Value: value})
		}
		if _, got := a.openAttempt(r); got != want {
			t.Errorf("%s: %v, want %v", name, got, want)
		}
	}
	other, expired, future, long := good, good, good, good
	other.O = "http://other.test"
	expired.IAT, expired.EXP = now-901, now-1
	future.IAT, future.EXP = now+120, now+900
	long.EXP = now + 901
	sealed := seal(a.attemptKey, good)

	// Tampered: decode the sealed value, flip one ciphertext byte, and
	// re-encode — a possibly no-op substitution (a different valid
	// value, a string edit) could pass by accident; a bit flip inside
	// AES-GCM's ciphertext cannot decrypt to anything but garbage, so
	// this can only fail the way a real forgery attempt would.
	body := strings.TrimPrefix(sealed, "v1.")
	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)/2] ^= 0x01
	tampered := "v1." + base64.RawURLEncoding.EncodeToString(raw)

	check("absent", "", cookieAbsent)
	check("sealed here", sealed, cookieValid)
	check("tampered", tampered, cookieInvalid)
	check("another origin", seal(a.attemptKey, other), cookieInvalid)
	check("expired", seal(a.attemptKey, expired), cookieInvalid)
	check("issued in the future", seal(a.attemptKey, future), cookieInvalid)
	check("longer than 15 minutes", seal(a.attemptKey, long), cookieInvalid)
	check("under the continuation's key", seal(crypto.Derive([]byte("test-instance-key"), "rastrillo/auth/continue/v1"), good), cookieInvalid)
	check("unknown version", "v2."+strings.TrimPrefix(sealed, "v1."), cookieInvalid)
	check("unknown field", seal(a.attemptKey, struct {
		attempt
		Z string `json:"z"`
	}{good, "z"}), cookieInvalid)
}

func TestAnswerAsSentAnswersLikeASentLink(t *testing.T) {
	for _, screen := range []bool{false, true} {
		t.Run("SigninScreen="+strconv.FormatBool(screen), func(t *testing.T) {
			a, m := newTestAuth(t, func(c *Config) { c.SigninScreen = screen })
			form := url.Values{"address": {"ada@example.com"}, "force": {"1"}, "expect": {"keymail"}}
			sent := newBrowser().do(a.Begin, http.MethodPost, "/signin", form)
			m.forget()
			refused := newBrowser().do(a.AnswerAsSent, http.MethodPost, "/signin", form)
			if m.sentTo() != "" {
				t.Fatal("AnswerAsSent sent mail")
			}
			if sent.Code != refused.Code || redirectShape(sent) != redirectShape(refused) ||
				!reflect.DeepEqual(cookieShape(sent), cookieShape(refused)) {
				t.Fatalf("a sent link and a refusal differ:\n sent    %d %q %v\n refused %d %q %v",
					sent.Code, redirectShape(sent), cookieShape(sent),
					refused.Code, redirectShape(refused), cookieShape(refused))
			}
			r := newBrowser().request(http.MethodPost, "/signin", form)
			r.Header.Set("Sec-Fetch-Site", "cross-site")
			w := httptest.NewRecorder()
			a.AnswerAsSent(w, r)
			if w.Code != http.StatusForbidden {
				t.Fatalf("a cross-site AnswerAsSent got %d, want 403", w.Code)
			}
		})
	}
}

func TestAdmitRemembersTheWayInAndEndsTheAttempt(t *testing.T) {
	signIn := func(t *testing.T, a *Auth, m *captureMailer, b *browser, address string) *httptest.ResponseRecorder {
		t.Helper()
		b.do(a.Begin, http.MethodPost, "/signin", url.Values{"address": {address}, "force": {"1"}})
		return b.redeem(a, linkRE.FindString(m.sentBody()))
	}
	remembered := func(a *Auth, b *browser) (lastsignin.Record, lastsignin.ReadResult) {
		return a.jar.Read(b.request(http.MethodGet, "/signin", nil))
	}
	has := func(w *httptest.ResponseRecorder, name string) bool {
		for _, c := range setCookies(w) {
			if c == name {
				return true
			}
		}
		return false
	}

	t.Run("a magic link is remembered by address, not subject", func(t *testing.T) {
		a, m := newScreenAuth(t, func(c *Config) {
			c.SubjectFor = func(string) (string, error) { return "ref_1", nil }
		})
		b := newBrowser()
		w := signIn(t, a, m, b, "ada@example.com")
		if w.Header().Get("Location") != "/" {
			t.Fatalf("verify → %q", w.Header().Get("Location"))
		}
		if rec, res := remembered(a, b); res != lastsignin.Valid || rec != (lastsignin.Record{Method: "magiclink", Address: "ada@example.com"}) {
			t.Fatalf("remembered %+v, %v", rec, res)
		}
		if !has(w, "rastrillo_attempt:clear") || b.cookie("rastrillo_attempt") != nil {
			t.Fatal("the verified sign-in left the attempt cookie behind")
		}
	})
	t.Run("a sign-in held for a second factor is remembered too", func(t *testing.T) {
		a, m := newScreenAuth(t, nil)
		a.cfg.SecondFactor = holdingGate(t, a).Hold
		b := newBrowser()
		if w := signIn(t, a, m, b, "ada@example.com"); w.Header().Get("Location") != "/signin/confirm" {
			t.Fatalf("held verify → %q", w.Header().Get("Location"))
		}
		if _, res := remembered(a, b); res != lastsignin.Valid {
			t.Fatalf("a held sign-in was not remembered: %v", res)
		}
	})
	t.Run("refused by Authorize: the attempt ends and nothing is remembered", func(t *testing.T) {
		a, m := newScreenAuth(t, func(c *Config) { c.Authorize = func(string) bool { return false } })
		b := newBrowser()
		w := signIn(t, a, m, b, "ada@example.com")
		if w.Code != http.StatusForbidden || !has(w, "rastrillo_attempt:clear") || has(w, "rastrillo_last_signin") {
			t.Fatalf("refused sign-in: %d, Set-Cookie %v", w.Code, setCookies(w))
		}
	})
	t.Run("SubjectFor fails: nothing is remembered", func(t *testing.T) {
		a, m := newScreenAuth(t, func(c *Config) {
			c.SubjectFor = func(string) (string, error) { return "", errors.New("down") }
		})
		b := newBrowser()
		if w := signIn(t, a, m, b, "ada@example.com"); w.Code != http.StatusInternalServerError || has(w, "rastrillo_last_signin") {
			t.Fatalf("failed SubjectFor: %d, Set-Cookie %v", w.Code, setCookies(w))
		}
	})
	t.Run("Remember=false deletes what was remembered before", func(t *testing.T) {
		on, onMail := newScreenAuth(t, nil)
		b := newBrowser()
		signIn(t, on, onMail, b, "ada@example.com")
		off, offMail := newScreenAuth(t, func(c *Config) { c.Remember = ptr(false) })
		w := signIn(t, off, offMail, b, "ada@example.com")
		if !has(w, "rastrillo_last_signin:clear") || b.cookie("rastrillo_last_signin") != nil {
			t.Fatalf("Remember=false left the old cookie: Set-Cookie %v", setCookies(w))
		}
	})
	t.Run("signing out forgets nothing", func(t *testing.T) {
		a, m := newScreenAuth(t, nil)
		b := newBrowser()
		signIn(t, a, m, b, "ada@example.com")
		w := b.do(a.Signout, http.MethodPost, "/signout", nil)
		if has(w, "rastrillo_last_signin:clear") {
			t.Fatal("Signout deleted the remembered way in")
		}
		if _, res := remembered(a, b); res != lastsignin.Valid {
			t.Fatalf("after signout: %v", res)
		}
	})
	t.Run("a remembered cookie is not a session", func(t *testing.T) {
		a, m := newScreenAuth(t, nil)
		b := newBrowser()
		signIn(t, a, m, b, "ada@example.com")
		only := newBrowser()
		only.jar["rastrillo_last_signin"] = b.cookie("rastrillo_last_signin")
		w := httptest.NewRecorder()
		a.RequireSession(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			t.Fatal("a remembered-method cookie reached a guarded handler")
		})).ServeHTTP(w, only.request(http.MethodGet, "/private", nil))
		if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/signin" {
			t.Fatalf("guarded GET with only a remembered cookie: %d → %q", w.Code, w.Header().Get("Location"))
		}
	})
}
