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
		{MethodKeymail, "kay@example.org"},
		{MethodMagicLink, "ada@example.com"},
		{MethodPasskey, ""},
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
	newJar(t, On, nil).Remember(w, Record{MethodMagicLink, "ada@example.com"})
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
	newJar(t, On, nil).Remember(w, Record{MethodMagicLink, "ada@example.com"})
	r := carrying(w)
	r.AddCookie(&http.Cookie{Name: "__Host-rastrillo_attempt", Value: "x"})

	off := newJar(t, Off, nil)
	w = httptest.NewRecorder()
	off.Remember(w, Record{MethodMagicLink, "ada@example.com"})
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
	newJar(t, On, nil).Remember(w, Record{MethodMagicLink, "ada@example.com"})
	r := carrying(w)

	j := newJar(t, Forgetting, nil)
	if _, res := j.Read(r); res != Invalid {
		t.Fatalf("Forgetting read %v, want Invalid so the screen clears it", res)
	}
	w = httptest.NewRecorder()
	j.Remember(w, Record{MethodMagicLink, "ada@example.com"})
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
		{MethodPasskey, "ada@example.com"},
		{MethodKeymail, ""},
		{MethodMagicLink, "no-at-sign"},
		{MethodMagicLink, "a@b@c.example"},
		{MethodMagicLink, strings.Repeat("a", 250) + "@x.example"},
		{MethodMagicLink, "ada\n@example.com"},
		{"password", "ada@example.com"},
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
	j.Remember(w, Record{MethodMagicLink, "ada@example.com"})
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
		"an unknown method":         {O: origin, M: "password", A: "ada@example.com", IAT: now, EXP: now + 3600},
		"a passkey with an address": {O: origin, M: MethodPasskey, A: "ada@example.com", IAT: now, EXP: now + 3600},
		"longer than 400 days":      {O: origin, M: MethodPasskey, IAT: now, EXP: now + 401*24*3600},
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
		"a relative origin":       {Origin: "app.test", InstanceKey: "k", AttemptCookie: "a", Mode: On},
		"no instance key":         {Origin: origin, AttemptCookie: "a", Mode: On},
		"On with no attempt name": {Origin: origin, InstanceKey: "k", Mode: On},
		"Forgetting with no name": {Origin: origin, InstanceKey: "k", Mode: Forgetting},
	} {
		if _, err := New(cfg); err == nil {
			t.Errorf("New(%s) succeeded", name)
		}
	}
	if _, err := New(Config{Origin: origin, InstanceKey: "k", Mode: Off}); err != nil {
		t.Errorf("Off needs no attempt cookie name: %v", err)
	}
}
