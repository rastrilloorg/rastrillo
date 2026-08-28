package rastrillo

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
)

// probe records what the wrapped handler saw, so a test can assert on
// both the resolved locale and the path the mux would have matched.
func probe(t *testing.T, l *Locales, req *http.Request) (locale, path, translated string) {
	t.Helper()
	h := l.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		locale, path, translated = LocaleFrom(r), r.URL.Path, T(r, "app.title")
	}))
	h.ServeHTTP(httptest.NewRecorder(), req)
	return locale, path, translated
}

func mwLocales(t *testing.T) *Locales {
	t.Helper()
	l, err := NewLocales([]string{"en", "fr", "de-informal"}, "en", Catalog{"ui.save": "Save"}, testFS())
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestResolutionPrecedence(t *testing.T) {
	l := mwLocales(t)
	tests := []struct {
		name       string
		path       string
		accept     string
		cookie     string
		wantLocale string
		wantPath   string
	}{
		{"path prefix wins over everything", "/fr/orders", "de-informal", "en", "fr", "/orders"},
		{"bare locale prefix becomes /", "/fr", "", "", "fr", "/"},
		{"default locale prefix is stripped too", "/en/orders", "", "", "en", "/orders"},
		{"Accept-Language when there is no prefix", "/orders", "fr", "es", "fr", "/orders"},
		{"Accept-Language honours q order", "/orders", "de-informal;q=0.4, fr;q=0.9", "", "fr", "/orders"},
		{"Accept-Language matches on the primary subtag", "/orders", "fr-CA", "", "fr", "/orders"},
		{"Accept-Language q order beats a lower-q exact match", "/orders", "fr-CA, en;q=0.5", "", "fr", "/orders"},
		{"cookie beats Accept-Language", "/orders", "fr", "de-informal", "de-informal", "/orders"},
		{"cookie only when it names a declared locale", "/orders", "fr", "es", "fr", "/orders"},
		{"prefix still beats the cookie", "/fr/orders", "", "de-informal", "fr", "/orders"},
		{"default when nothing matches", "/orders", "", "", "en", "/orders"},
		{"a path segment that merely looks like one is not a locale", "/design/fr", "", "", "en", "/design/fr"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			if tt.accept != "" {
				req.Header.Set("Accept-Language", tt.accept)
			}
			if tt.cookie != "" {
				req.AddCookie(&http.Cookie{Name: LocaleCookie, Value: tt.cookie})
			}
			gotLocale, gotPath, _ := probe(t, l, req)
			if gotLocale != tt.wantLocale {
				t.Errorf("locale = %q, want %q", gotLocale, tt.wantLocale)
			}
			if gotPath != tt.wantPath {
				t.Errorf("path = %q, want %q", gotPath, tt.wantPath)
			}
		})
	}
}

func TestMiddlewareBindsTranslation(t *testing.T) {
	l := mwLocales(t)
	_, _, fr := probe(t, l, httptest.NewRequest("GET", "/fr/orders", nil))
	if fr != "Commandes" {
		t.Errorf("T(r, \"app.title\") = %q, want Commandes", fr)
	}
	_, _, en := probe(t, l, httptest.NewRequest("GET", "/orders", nil))
	if en != "Orders" {
		t.Errorf("T(r, \"app.title\") = %q, want Orders", en)
	}
}

func TestMiddlewareDoesNotMutateTheIncomingRequest(t *testing.T) {
	l := mwLocales(t)
	req := httptest.NewRequest("GET", "/fr/orders", nil)
	probe(t, l, req)
	if req.URL.Path != "/fr/orders" {
		t.Errorf("incoming request was mutated: path = %q", req.URL.Path)
	}
}

func TestTranslationOutsideAMiddlewareRequest(t *testing.T) {
	// A request that never went through Middleware must not guess a
	// locale: T returns the key, Tf interpolates it and nothing else.
	req := httptest.NewRequest("GET", "/", nil)
	if got := T(req, "ui.save"); got != "ui.save" {
		t.Errorf("T = %q, want the key verbatim", got)
	}
	if got := Tf(req, "hello {name}", "name", "Ada"); got != "hello Ada" {
		t.Errorf("Tf = %q, want %q", got, "hello Ada")
	}
}

func TestMiddlewarePreservesRawPathWhenStrippingPrefix(t *testing.T) {
	// A locale prefix must be invisible to the app below it: the
	// request %-encoding a route sees for /fr/files/a%2Fb must match
	// what it sees for the unprefixed /files/a%2Fb, or the same URL
	// routes two different ways depending on whether it's localized.
	// EscapedPath() is what a wildcard route ({rest...}) and anything
	// re-parsing the raw path actually consult, so it's what has to
	// agree — a mux registered here (rather than the bare probe()
	// helper used elsewhere in this file) is what exercises that.
	l := mwLocales(t)
	mux := http.NewServeMux()
	var gotEscaped string
	mux.HandleFunc("/files/{rest...}", func(_ http.ResponseWriter, r *http.Request) {
		gotEscaped = r.URL.EscapedPath()
	})
	h := l.Middleware(mux)

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/fr/files/a%2Fb", nil))
	if gotEscaped != "/files/a%2Fb" {
		t.Errorf("EscapedPath() = %q, want /files/a%%2Fb", gotEscaped)
	}

	gotEscaped = ""
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/files/a%2Fb", nil))
	if gotEscaped != "/files/a%2Fb" {
		t.Errorf("unprefixed EscapedPath() = %q, want /files/a%%2Fb", gotEscaped)
	}
}

func TestMiddlewareFallsBackCleanlyWhenTheRawLocaleBoundaryIsSmuggled(t *testing.T) {
	// A raw path like /fr%2Fxx/files/a decodes (Path) to
	// /fr/xx/files/a, so splitPrefix matches locale "fr" against the
	// decoded form and trims it to /xx/files/a — but the %2F means the
	// literal "/fr" substring in RawPath is NOT followed by a real "/":
	// TrimPrefix(RawPath, "/fr") would strip to "%2Fxx/files/a", which
	// has lost its leading slash while still decode-equaling the
	// trimmed Path, so net/url's validity check would wave it through
	// and EscapedPath/RequestURI/String would all come out
	// non-absolute. The middleware must recognize the boundary doesn't
	// provably line up and fall back to clearing RawPath (EscapedPath
	// recomputes cleanly from Path) rather than carry over a
	// mis-aligned trim.
	l := mwLocales(t)
	mux := http.NewServeMux()
	var gotEscaped, gotRequestURI string
	mux.HandleFunc("/xx/files/{rest...}", func(_ http.ResponseWriter, r *http.Request) {
		gotEscaped = r.URL.EscapedPath()
		gotRequestURI = r.URL.RequestURI()
	})
	h := l.Middleware(mux)

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/fr%2Fxx/files/a", nil))

	if gotEscaped != "/xx/files/a" {
		t.Errorf("EscapedPath() = %q, want /xx/files/a", gotEscaped)
	}
	if !strings.HasPrefix(gotEscaped, "/") {
		t.Errorf("EscapedPath() = %q is not absolute (no leading /)", gotEscaped)
	}
	if !strings.HasPrefix(gotRequestURI, "/") {
		t.Errorf("RequestURI() = %q is not absolute (no leading /)", gotRequestURI)
	}
}

func TestLocaleFromWithoutMiddleware(t *testing.T) {
	if got := LocaleFrom(httptest.NewRequest("GET", "/", nil)); got != "" {
		t.Errorf("LocaleFrom = %q, want \"\"", got)
	}
}

func TestLocaleItems(t *testing.T) {
	l := mwLocales(t) // en, fr, de-informal; default en
	var items []LocaleItem
	h := l.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		items = LocaleItems(r)
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/fr/orders?page=2", nil))
	want := []LocaleItem{
		{Code: "en", Name: "English", Href: "/en/orders?page=2"},
		{Code: "fr", Name: "fr", Href: "/fr/orders?page=2", Current: true},
		{Code: "de-informal", Name: "de-informal", Href: "/de-informal/orders?page=2"},
	}
	if !reflect.DeepEqual(items, want) {
		t.Errorf("items = %+v\nwant  %+v", items, want)
	}
}

func TestLocaleItemsEmptyForOneLocaleOrNoMiddleware(t *testing.T) {
	if got := LocaleItems(httptest.NewRequest("GET", "/", nil)); len(got) != 0 {
		t.Errorf("without middleware: %v", got)
	}
	l, _ := NewLocales([]string{"en"}, "en", BaseCatalog(), nil)
	var got []LocaleItem
	l.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = LocaleItems(r) })).
		ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	if len(got) != 0 {
		t.Errorf("one locale: %v", got)
	}
}

func switchReq(locale, ret string) *http.Request {
	form := url.Values{"locale": {locale}, "return": {ret}}
	req := httptest.NewRequest("POST", LocaleSwitchPath, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	return req
}

func TestSwitchHandlerSetsCookieAndRedirects(t *testing.T) {
	l := mwLocales(t)
	rec := httptest.NewRecorder()
	l.SwitchHandler().ServeHTTP(rec, switchReq("fr", "/orders?page=2"))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status %d, want 303", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "/fr/orders?page=2" {
		t.Errorf("Location = %q", got)
	}
	var c *http.Cookie
	for _, k := range rec.Result().Cookies() {
		if k.Name == LocaleCookie {
			c = k
		}
	}
	if c == nil || c.Value != "fr" || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.MaxAge < 86400*300 {
		t.Errorf("cookie = %+v", c)
	}
}

func TestSwitchHandlerStripsAnExistingPrefixFromReturn(t *testing.T) {
	l := mwLocales(t)
	rec := httptest.NewRecorder()
	l.SwitchHandler().ServeHTTP(rec, switchReq("en", "/fr/orders"))
	if got := rec.Header().Get("Location"); got != "/en/orders" {
		t.Errorf("Location = %q, want /en/orders", got)
	}
}

func TestSwitchHandlerRefusals(t *testing.T) {
	l := mwLocales(t)
	tests := []struct {
		name   string
		req    *http.Request
		status int
	}{
		{"undeclared locale", switchReq("es", "/"), http.StatusBadRequest},
		{"protocol-relative return", switchReq("fr", "//evil.example/"), http.StatusBadRequest},
		{"absolute return", switchReq("fr", "https://evil.example/"), http.StatusBadRequest},
		{"backslash return", switchReq("fr", "/\\evil.example/"), http.StatusBadRequest},
		{"GET", httptest.NewRequest("GET", LocaleSwitchPath, nil), http.StatusMethodNotAllowed},
	}
	crossSite := switchReq("fr", "/")
	crossSite.Header.Set("Sec-Fetch-Site", "cross-site")
	tests = append(tests, struct {
		name   string
		req    *http.Request
		status int
	}{"cross-site", crossSite, http.StatusForbidden})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			l.SwitchHandler().ServeHTTP(rec, tt.req)
			if rec.Code != tt.status {
				t.Errorf("status %d, want %d", rec.Code, tt.status)
			}
			if rec.Header().Get("Set-Cookie") != "" {
				t.Error("a refusal must not set the cookie")
			}
		})
	}
}

func TestSwitchHandlerEmptyReturnGoesHome(t *testing.T) {
	l := mwLocales(t)
	rec := httptest.NewRecorder()
	l.SwitchHandler().ServeHTTP(rec, switchReq("fr", ""))
	if got := rec.Header().Get("Location"); got != "/fr/" {
		t.Errorf("Location = %q, want /fr/", got)
	}
}

func TestSwitchHandlerSecureCookieBehindTLS(t *testing.T) {
	l := mwLocales(t)
	req := switchReq("fr", "/")
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	l.SwitchHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status %d, want 303", rec.Code)
	}
	var found bool
	for _, c := range rec.Result().Cookies() {
		if c.Name == LocaleCookie {
			found = true
			if !c.Secure {
				t.Error("cookie must be Secure when the request arrived over https")
			}
		}
	}
	if !found {
		t.Fatal("locale cookie was not set")
	}
}

func TestLocaleItemsAutonymAppCatalogWins(t *testing.T) {
	fsys := fstest.MapFS{
		"locales/ga.toml": {Data: []byte("rastrillo.ui.locale_name = \"Gaeilge na hApp\"\n")},
	}
	l, err := NewLocales([]string{"en", "ga"}, "en", BaseCatalog(), fsys)
	if err != nil {
		t.Fatal(err)
	}
	var items []LocaleItem
	h := l.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		items = LocaleItems(r)
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/en/orders", nil))
	byCode := map[string]LocaleItem{}
	for _, it := range items {
		byCode[it.Code] = it
	}
	if got := byCode["ga"].Name; got != "Gaeilge na hApp" {
		t.Errorf("ga Name = %q, want %q (app catalog should beat the framework)", got, "Gaeilge na hApp")
	}
	if got := byCode["en"].Name; got != "English" {
		t.Errorf("en Name = %q, want English (framework branch still works)", got)
	}
}
