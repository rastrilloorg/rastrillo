package basepath_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"amadan.net/rastrillo/rastrillo/basepath"
)

func TestDirectHostRedirectsToTheProductPath(t *testing.T) {
	for _, c := range []struct{ method, host, target, want string }{
		{"GET", "acme-abc234.bdf.oncarlos.com", "/notes?x=1", "https://example.com/acme/notes?x=1"},
		{"GET", "acme-abc234.bdf.oncarlos.com", "/", "https://example.com/acme"},
		{"GET", "acme-abc234.bdf.oncarlos.com", "/es/private?x=1", "https://example.com/acme/es/private?x=1"},
		{"HEAD", "cv.paulca.com", "/about", "https://example.com/acme/about"},
		{"POST", "acme-abc234.bdf.oncarlos.com", "/notes", "https://example.com/acme/notes"},
		// A path the prefix rule leaves alone as a link is still this
		// app's page when it is a request target: sending //x to the
		// product origin's //x would land the visitor on the hub.
		{"GET", "cv.paulca.com", "//x", "https://example.com/acme//x"},
		// net/http accepts an absolute-form target, and keeps it as
		// r.RequestURI: the redirect is built from its path and query,
		// not glued to the origin whole.
		{"GET", "cv.paulca.com", "http://cv.paulca.com/about?x=1", "https://example.com/acme/about?x=1"},
		{"GET", "cv.paulca.com", "http://cv.paulca.com//x", "https://example.com/acme//x"},
		{"OPTIONS", "cv.paulca.com", "*", "https://example.com/acme"},
	} {
		called := false
		rec := serve(acme, c.method, c.host, c.target, func(http.ResponseWriter, *http.Request) { called = true })
		if called {
			t.Errorf("%s %s on %s: next was called", c.method, c.target, c.host)
		}
		if rec.Code != http.StatusTemporaryRedirect || rec.Header().Get("Location") != c.want {
			t.Errorf("%s %s on %s: %d, Location %q; want 307, %q",
				c.method, c.target, c.host, rec.Code, rec.Header().Get("Location"), c.want)
		}
	}

	// The edge sets X-Forwarded-Host from the request, but any client
	// can send one too, so it must not make a direct host the product's.
	req := httptest.NewRequest("GET", "/notes", nil)
	req.Host = "acme-abc234.bdf.oncarlos.com"
	req.Header.Set("X-Forwarded-Host", "example.com")
	rec := httptest.NewRecorder()
	called := false
	acme.Handler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })).ServeHTTP(rec, req)
	if called || rec.Code != http.StatusTemporaryRedirect {
		t.Errorf("X-Forwarded-Host example.com: %d, next called %v; want 307 and not called", rec.Code, called)
	}
}

func TestDirectHostAnswersPlatformCallsAtTheRoot(t *testing.T) {
	t.Setenv("CARLOS_ADMIN_TOKEN", "tok")
	const instance = "acme-abc234.bdf.oncarlos.com"
	for _, c := range []struct{ method, host, target, auth string }{
		{"GET", "carlos", "/", ""},               // the activator's wake check
		{"GET", "carlos", "/anything/ready", ""}, // a claim's ready_path
		{"GET", instance, "/healthz", ""},
		{"GET", instance, "/api/version", ""},
		{"HEAD", "cv.paulca.com", "/healthz", ""},
		{"POST", instance, "/jobs/nightly", "Bearer tok"},
	} {
		var path, base string
		var called, direct, platform bool
		req := httptest.NewRequest(c.method, c.target, nil)
		req.Host = c.host
		if c.auth != "" {
			req.Header.Set("Authorization", c.auth)
		}
		rec := httptest.NewRecorder()
		acme.Handler(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			called = true
			path, base = r.URL.Path, basepath.Base(r)
			direct, platform = basepath.OnDirectHost(r), basepath.PlatformCall(r)
		})).ServeHTTP(rec, req)
		if !called {
			t.Errorf("%s %s on %s: next was not called (%d, Location %q)",
				c.method, c.target, c.host, rec.Code, rec.Header().Get("Location"))
			continue
		}
		if path != c.target || base != "" || !direct || !platform {
			t.Errorf("%s %s on %s: path %q, Base %q, OnDirectHost %v, PlatformCall %v; want %q, \"\", true, true",
				c.method, c.target, c.host, path, base, direct, platform, c.target)
		}
	}

	for _, c := range []struct{ method, target, auth string }{
		{"POST", "/jobs/nightly", "Bearer wrong"},
		{"GET", "/healthz/x", ""},
		{"GET", "/api/versions", ""},
		{"POST", "/healthz", ""},
	} {
		req := httptest.NewRequest(c.method, c.target, nil)
		req.Host = instance
		if c.auth != "" {
			req.Header.Set("Authorization", c.auth)
		}
		rec := httptest.NewRecorder()
		called := false
		acme.Handler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })).ServeHTTP(rec, req)
		if called || rec.Code != http.StatusTemporaryRedirect {
			t.Errorf("%s %s (auth %q): %d, next called %v; want 307, not called", c.method, c.target, c.auth, rec.Code, called)
		}
	}
}

func TestServeDirectServesAtTheRoot(t *testing.T) {
	c := basepath.Config{Origin: "https://example.com", Slug: "acme", DirectHost: basepath.ServeDirect}

	var path, base, link string
	var called, direct, routed bool
	rec := serve(c, "GET", "cv.paulca.com", "/notes", func(w http.ResponseWriter, r *http.Request) {
		called = true
		path, base, link = r.URL.Path, basepath.Base(r), basepath.Path(r, "/x")
		direct, routed = basepath.OnDirectHost(r), basepath.PathRouted(r)
		http.Redirect(w, r, "/done", http.StatusSeeOther)
	})
	if !called {
		t.Fatalf("next was not called: %d, Location %q", rec.Code, rec.Header().Get("Location"))
	}
	if path != "/notes" || base != "" || !direct || !routed || link != "/x" {
		t.Errorf("path %q, Base %q, OnDirectHost %v, PathRouted %v, Path(r, /x) %q; want /notes, \"\", true, true, /x",
			path, base, direct, routed, link)
	}
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/done" {
		t.Errorf("redirect: %d, Location %q; want 303, /done", rec.Code, rec.Header().Get("Location"))
	}

	path, base, direct = "", "", true
	serve(c, "GET", "example.com", "/acme/notes", func(_ http.ResponseWriter, r *http.Request) {
		path, base, direct = r.URL.Path, basepath.Base(r), basepath.OnDirectHost(r)
	})
	if path != "/notes" || base != "/acme" || direct {
		t.Errorf("product path: path %q, Base %q, OnDirectHost %v; want /notes, /acme, false", path, base, direct)
	}

	called = false
	rec = serve(c, "GET", "example.com", "/notes", func(http.ResponseWriter, *http.Request) { called = true })
	if called || rec.Code != http.StatusNotFound {
		t.Errorf("product origin outside the base: %d, next called %v; want 404", rec.Code, called)
	}
}

func TestProductURL(t *testing.T) {
	// An inner handler that strips a locale, as the locale middleware
	// does: ProductURL is built from what the browser sent, not r.URL.
	stripES := func(r *http.Request) *http.Request {
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/notes"
		r2.URL.RawPath = ""
		return r2
	}
	serveDirect := basepath.Config{Origin: "https://example.com", Slug: "acme", DirectHost: basepath.ServeDirect}

	var got string
	serve(serveDirect, "GET", "cv.paulca.com", "/es/notes?x=1", func(_ http.ResponseWriter, r *http.Request) {
		got = basepath.ProductURL(stripES(r))
	})
	if want := "https://example.com/acme/es/notes?x=1"; got != want {
		t.Errorf("direct host: ProductURL = %q, want %q", got, want)
	}

	got = ""
	serve(acme, "GET", "example.com", "/acme/notes?x=1", func(_ http.ResponseWriter, r *http.Request) {
		got = basepath.ProductURL(r)
	})
	if want := "https://example.com/acme/notes?x=1"; got != want {
		t.Errorf("product path: ProductURL = %q, want %q", got, want)
	}

	// An absolute-form target on the product host: ProductURL takes its
	// path and query, and Original still reports it as sent.
	const absolute = "https://example.com/acme/notes?x=1"
	got = ""
	var original string
	serve(acme, "GET", "example.com", absolute, func(_ http.ResponseWriter, r *http.Request) {
		got, original = basepath.ProductURL(r), basepath.Original(r)
	})
	if want := "https://example.com/acme/notes?x=1"; got != want || original != absolute {
		t.Errorf("product path, absolute-form: ProductURL = %q, Original %q; want %q, %q", got, original, want, absolute)
	}

	got = ""
	serve(serveDirect, "GET", "cv.paulca.com", "http://cv.paulca.com/es/notes?x=1", func(_ http.ResponseWriter, r *http.Request) {
		got = basepath.ProductURL(r)
	})
	if want := "https://example.com/acme/es/notes?x=1"; got != want {
		t.Errorf("direct host, absolute-form: ProductURL = %q, want %q", got, want)
	}

	// The app's own /admin/x route, not a base to remove.
	admin := basepath.Config{Origin: "https://example.com", Slug: "admin", DirectHost: basepath.ServeDirect}
	got = ""
	serve(admin, "GET", "cv.paulca.com", "/admin/x", func(_ http.ResponseWriter, r *http.Request) {
		got = basepath.ProductURL(r)
	})
	if want := "https://example.com/admin/admin/x"; got != want {
		t.Errorf("slug admin, direct /admin/x: ProductURL = %q, want %q", got, want)
	}
}
