package rastrillo

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func schemeReq(scheme, ret string) *http.Request {
	form := url.Values{"scheme": {scheme}, "return": {ret}}
	req := httptest.NewRequest("POST", SchemeSwitchPath, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	return req
}

func schemeCookie(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == SchemeCookie {
			return c
		}
	}
	return nil
}

// Light and dark are stored; the cookie carries the locale cookie's
// attributes except HttpOnly, which it cannot have: rastrillo.js
// writes the same cookie when it applies the choice in place, and a
// browser refuses a script's write over an HttpOnly cookie, so the
// next server render would paint the scheme the reader just left.
func TestSchemeSwitchHandlerSetsCookieAndRedirects(t *testing.T) {
	for _, scheme := range []string{"light", "dark"} {
		rec := httptest.NewRecorder()
		SchemeSwitchHandler().ServeHTTP(rec, schemeReq(scheme, "/orders?page=2"))
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("%s: status %d, want 303", scheme, rec.Code)
		}
		if got := rec.Header().Get("Location"); got != "/orders?page=2" {
			t.Errorf("%s: Location = %q", scheme, got)
		}
		c := schemeCookie(rec)
		if c == nil || c.Value != scheme || c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.MaxAge < 86400*300 {
			t.Errorf("%s: cookie = %+v", scheme, c)
		}
	}
}

// System is no cookie at all, so the OS decides again; the handler
// clears one that is there rather than storing a third value.
func TestSchemeSwitchHandlerSystemClearsTheCookie(t *testing.T) {
	rec := httptest.NewRecorder()
	req := schemeReq("system", "/orders")
	req.AddCookie(&http.Cookie{Name: SchemeCookie, Value: "dark"})
	SchemeSwitchHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/orders" {
		t.Fatalf("status %d Location %q", rec.Code, rec.Header().Get("Location"))
	}
	c := schemeCookie(rec)
	if c == nil || c.MaxAge >= 0 || c.Path != "/" {
		t.Errorf("system must expire the cookie at Path=/: %+v", c)
	}
}

func TestSchemeSwitchHandlerRefusals(t *testing.T) {
	tests := []struct {
		name   string
		req    *http.Request
		status int
	}{
		{"unknown scheme", schemeReq("sepia", "/"), http.StatusBadRequest},
		{"empty scheme", schemeReq("", "/"), http.StatusBadRequest},
		{"protocol-relative return", schemeReq("dark", "//evil.example/"), http.StatusBadRequest},
		{"absolute return", schemeReq("dark", "https://evil.example/"), http.StatusBadRequest},
		{"backslash return", schemeReq("dark", "/\\evil.example/"), http.StatusBadRequest},
		// http.Redirect cleans the path first, so "/a/../\evil" leaves as
		// "/\evil", which browsers read as "//evil": the guard must
		// refuse a backslash anywhere, not only at the front.
		{"backslash after dot-dot", schemeReq("dark", "/a/../\\evil.example/"), http.StatusBadRequest},
		// Browsers strip tab, CR and LF before parsing, so "/\t/evil"
		// resolves scheme-relative. The same policy as sessions.SafeReturnTo.
		{"tab return", schemeReq("dark", "/\t/evil.example/"), http.StatusBadRequest},
		{"GET", httptest.NewRequest("GET", SchemeSwitchPath, nil), http.StatusMethodNotAllowed},
	}
	crossSite := schemeReq("dark", "/")
	crossSite.Header.Set("Sec-Fetch-Site", "cross-site")
	tests = append(tests, struct {
		name   string
		req    *http.Request
		status int
	}{"cross-site", crossSite, http.StatusForbidden})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			SchemeSwitchHandler().ServeHTTP(rec, tt.req)
			if rec.Code != tt.status {
				t.Errorf("status %d, want %d", rec.Code, tt.status)
			}
			if rec.Header().Get("Set-Cookie") != "" {
				t.Error("a refusal must not touch the cookie")
			}
		})
	}
}

func TestSchemeSwitchHandlerEmptyReturnGoesHome(t *testing.T) {
	rec := httptest.NewRecorder()
	SchemeSwitchHandler().ServeHTTP(rec, schemeReq("dark", ""))
	if got := rec.Header().Get("Location"); got != "/" {
		t.Errorf("Location = %q, want /", got)
	}
}

func TestSchemeSwitchHandlerSecureCookieBehindTLS(t *testing.T) {
	req := schemeReq("light", "/")
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	SchemeSwitchHandler().ServeHTTP(rec, req)
	if c := schemeCookie(rec); c == nil || !c.Secure {
		t.Errorf("cookie must be Secure when the request arrived over https: %+v", c)
	}
}

// SchemeFrom is what the server paints the first frame with: the
// cookie's value when it is one of the two schemes, and "" for no
// cookie or a value nothing wrote, so a tampered cookie can only ever
// be System.
func TestSchemeFrom(t *testing.T) {
	for cookie, want := range map[string]string{"light": "light", "dark": "dark", "system": "", "sepia": "", "": ""} {
		req := httptest.NewRequest("GET", "/", nil)
		if cookie != "" {
			req.AddCookie(&http.Cookie{Name: SchemeCookie, Value: cookie})
		}
		if got := SchemeFrom(req); got != want {
			t.Errorf("cookie %q: SchemeFrom = %q, want %q", cookie, got, want)
		}
	}
	if got := SchemeFrom(httptest.NewRequest("GET", "/", nil)); got != "" {
		t.Errorf("no cookie: SchemeFrom = %q, want empty", got)
	}
}
