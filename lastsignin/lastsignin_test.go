package lastsignin

import (
	"bytes"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/internal/sealedcookie"
)

const origin = "https://app.test"

// adaLink is the record most tests write: a magic link, with its address.
var adaLink = Record{Method: MethodMagicLink, Address: "ada@example.com"}

// oficina is a federated record with no address.
var oficina = Record{Method: MethodFederated, Provider: ProviderOficina}

func newJar(t *testing.T, mode Mode, now func() time.Time) *Jar {
	t.Helper()
	j, err := New(Config{Origin: origin, InstanceKey: "k", AttemptCookie: "__Host-rastrillo_attempt", Mode: mode, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	return j
}

// carrying is a request bearing every cookie w set, deletions dropped —
// what the browser sends back on its next request.
func carrying(w *httptest.ResponseRecorder) *http.Request {
	r := httptest.NewRequest(http.MethodGet, origin+"/signin", nil)
	for _, c := range w.Result().Cookies() {
		if c.MaxAge >= 0 {
			r.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value})
		}
	}
	return r
}

func TestRememberThenReadRoundTrips(t *testing.T) {
	j := newJar(t, On, nil)
	for _, rec := range []Record{
		{Method: MethodKeymail, Address: "kay@example.org"},
		{Method: MethodMagicLink, Address: "ada@example.com"},
		{Method: MethodPasskey, Address: ""},
		{Method: MethodPasskey, Address: "ada@example.com"},
		oficina,
		{Method: MethodFederated, Provider: ProviderOficina,
			Address: "ada@example.com"},
	} {
		w := httptest.NewRecorder()
		j.Remember(w, rec)
		if got, res := j.Read(carrying(w)); res != Valid || got != rec {
			t.Errorf("Remember(%+v) then Read = %+v, %v", rec, got, res)
		}
	}
}

func TestTheCookieIsHostOnlyHttpOnlyAndLastsFourHundredDays(t *testing.T) {
	w := httptest.NewRecorder()
	newJar(t, On, nil).Remember(w, adaLink)
	cs := w.Result().Cookies()
	if len(cs) != 1 {
		t.Fatalf("%d cookies, want 1", len(cs))
	}
	c := cs[0]
	if c.Name != "__Host-rastrillo_last_signin" || !c.Secure || !c.HttpOnly ||
		c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.MaxAge != 400*24*3600 {
		t.Fatalf("cookie = %+v", c)
	}
	raw, _ := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(c.Value, "v1."))
	if bytes.Contains(raw, []byte("ada@example.com")) {
		t.Fatal("the address is readable in the cookie")
	}
}

func TestAPlainHTTPOriginGetsTheUnprefixedName(t *testing.T) {
	j, err := New(Config{Origin: "http://app.test", InstanceKey: "k", AttemptCookie: "rastrillo_attempt", Mode: On})
	if err != nil {
		t.Fatal(err)
	}
	if j.CookieName() != "rastrillo_last_signin" {
		t.Fatalf("CookieName = %q; __Host- requires Secure, which a plain-http origin cannot set", j.CookieName())
	}
}

func TestOffWritesReadsAndDeletesNothing(t *testing.T) {
	w := httptest.NewRecorder()
	newJar(t, On, nil).Remember(w, adaLink)
	r := carrying(w)
	r.AddCookie(&http.Cookie{Name: "__Host-rastrillo_attempt", Value: "x"})

	off := newJar(t, Off, nil)
	w = httptest.NewRecorder()
	off.Remember(w, adaLink)
	off.EndAttempt(w)
	off.Clear(w)
	if h := w.Header().Values("Set-Cookie"); len(h) != 0 {
		t.Fatalf("Off wrote %v; an app without the screen must see no cookie traffic", h)
	}
	if _, res := off.Read(r); res != Absent {
		t.Fatalf("Off read %v from a valid cookie, want Absent", res)
	}
}

func TestForgettingDeletesAndNeverReads(t *testing.T) {
	w := httptest.NewRecorder()
	newJar(t, On, nil).Remember(w, adaLink)
	r := carrying(w)

	j := newJar(t, Forgetting, nil)
	if _, res := j.Read(r); res != Invalid {
		t.Fatalf("Forgetting read %v, want Invalid so the screen clears it", res)
	}
	w = httptest.NewRecorder()
	j.Remember(w, adaLink)
	j.EndAttempt(w)
	got := map[string]int{}
	for _, c := range w.Result().Cookies() {
		got[c.Name] = c.MaxAge
	}
	if got["__Host-rastrillo_last_signin"] >= 0 || got["__Host-rastrillo_attempt"] >= 0 || len(got) != 2 {
		t.Fatalf("Forgetting wrote %v, want both cookies deleted and nothing else", got)
	}
}

func TestRememberRefusesARecordItCouldNotReadBack(t *testing.T) {
	j := newJar(t, On, nil)
	for _, rec := range []Record{
		{Method: MethodPasskey, Address: "no-at-sign"},
		{Method: MethodPasskey, Provider: ProviderOficina},
		{Method: MethodKeymail, Address: ""},
		{Method: MethodKeymail, Address: "kay@example.org",
			Provider: ProviderOficina},
		{Method: MethodFederated},
		{Method: MethodFederated, Provider: "google"},
		{Method: MethodFederated, Provider: ProviderOficina,
			Address: "ada\r@example.com"},
		{Method: MethodMagicLink, Address: "no-at-sign"},
		{Method: MethodMagicLink, Address: "a@b@c.example"},
		{Method: MethodMagicLink,
			Address: strings.Repeat("a", 250) + "@x.example"},
		{Method: MethodMagicLink, Address: "ada\n@example.com"},
		{Method: "password", Address: "ada@example.com"},
	} {
		w := httptest.NewRecorder()
		j.Remember(w, rec)
		if h := w.Header().Values("Set-Cookie"); len(h) != 0 {
			t.Errorf("Remember(%+v) wrote %v", rec, h)
		}
	}
}

func TestReadRefusesWhatWasNotSealedForThisOriginAndTime(t *testing.T) {
	clock := time.Unix(1_800_000_000, 0)
	at := func(d time.Duration) func() time.Time { return func() time.Time { return clock.Add(d) } }
	j := newJar(t, On, at(0))
	w := httptest.NewRecorder()
	j.Remember(w, adaLink)
	r := carrying(w)

	other, _ := New(Config{Origin: "https://other.test", InstanceKey: "k", AttemptCookie: "a", Mode: On, Now: at(0)})
	rotated, _ := New(Config{Origin: origin, InstanceKey: "rotated", AttemptCookie: "a", Mode: On, Now: at(0)})
	for name, reader := range map[string]*Jar{
		"another origin":       other,
		"a rotated key":        rotated,
		"after 400 days":       newJar(t, On, at(400*24*time.Hour)),
		"issued in the future": newJar(t, On, at(-2*time.Minute)),
	} {
		// other.test's jar reads the same __Host- name, so it sees the cookie.
		if _, res := reader.Read(r); res != Invalid {
			t.Errorf("%s: Read = %v, want Invalid", name, res)
		}
	}

	now := clock.Unix()
	for name, p := range map[string]payload{
		"an unknown method": {O: origin, M: "password",
			A: "ada@example.com", IAT: now, EXP: now + 3600},
		"a passkey with a provider": {O: origin, M: MethodPasskey,
			P: ProviderOficina, IAT: now, EXP: now + 3600},
		"an unknown provider": {O: origin, M: MethodFederated,
			P: "google", IAT: now, EXP: now + 3600},
		"longer than 400 days": {O: origin, M: MethodPasskey,
			IAT: now, EXP: now + 401*24*3600},
	} {
		v, err := sealedcookie.Seal(j.key, p)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(http.MethodGet, origin+"/", nil)
		r.AddCookie(&http.Cookie{Name: j.CookieName(), Value: v})
		if _, res := j.Read(r); res != Invalid {
			t.Errorf("%s: Read = %v, want Invalid", name, res)
		}
	}
}

func TestNewRefusesAConfigItCannotHonour(t *testing.T) {
	for name, cfg := range map[string]Config{
		"a relative origin": {Origin: "app.test", InstanceKey: "k",
			AttemptCookie: "a", Mode: On},
		"no instance key": {Origin: origin, AttemptCookie: "a", Mode: On},
	} {
		if _, err := New(cfg); err == nil {
			t.Errorf("New(%s) succeeded", name)
		}
	}
}

// Home signs people in without auth, so it has no attempt cookie to
// end: its jar is built with none, and ending an attempt writes nothing.
func TestAJarWithNoAttemptCookieEndsNothing(t *testing.T) {
	jar, err := New(Config{Origin: origin, InstanceKey: "k", Mode: On})
	if err != nil {
		t.Fatalf("New with no attempt cookie: %v", err)
	}
	rec := httptest.NewRecorder()
	jar.EndAttempt(rec)
	if set := rec.Header().Values("Set-Cookie"); len(set) != 0 {
		t.Fatalf("EndAttempt wrote %v with no attempt cookie named", set)
	}
}

// Remember writes what its caller says and nothing of the record it
// replaces: a federated sign-in after a remembered link, with no address
// from the caller, must not keep the link's address.
func TestRememberCarriesNoAddressOver(t *testing.T) {
	jar := newJar(t, On, nil)
	jar.Remember(httptest.NewRecorder(), adaLink)
	rec := httptest.NewRecorder()
	jar.Remember(rec, oficina)
	got, res := jar.Read(carrying(rec))
	if res != Valid || got != oficina {
		t.Fatalf("after a federated sign-in the jar reads %+v, %v; want %+v",
			got, res, oficina)
	}
}

func TestRemembersTOTPAndPassphrase(t *testing.T) {
	jar := newJar(t, On, nil)
	for _, method := range []string{MethodTOTP, MethodPassphrase} {
		rec := Record{Method: method, Address: "ada@example.com"}
		response := httptest.NewRecorder()
		jar.Remember(response, rec)
		got, result := jar.Read(carrying(response))
		if result != Valid || got != rec {
			t.Errorf("Remember(%+v) then Read = %+v, %v", rec, got, result)
		}
		for _, bad := range []Record{
			{Method: method},
			{Method: method, Address: "ada@example.com",
				Provider: ProviderOficina},
		} {
			response := httptest.NewRecorder()
			jar.Remember(response, bad)
			headers := response.Header().Values("Set-Cookie")
			if len(headers) != 0 {
				t.Errorf("Remember(%+v) wrote %v", bad, headers)
			}
		}
	}
}
