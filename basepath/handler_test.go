package basepath_test

import (
	"bufio"
	"bytes"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"net/textproto"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/basepath"
	"amadan.net/rastrillo/rastrillo/perf"
)

var acme = basepath.Config{Origin: "https://example.com", Slug: "acme"}

func serve(c basepath.Config, method, host, target string, h http.HandlerFunc) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	req.Host = host
	rec := httptest.NewRecorder()
	c.Handler(h).ServeHTTP(rec, req)
	return rec
}

// live serves h behind c.Handler on a real server, so the 1xx, flush
// and hijack paths go through net/http's own writer rather than a
// recorder that accepts anything.
func live(t *testing.T, c basepath.Config, h http.Handler) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(c.Handler(h))
	t.Cleanup(srv.Close)
	return srv
}

// get requests path from srv with Host example.com and no redirect
// following, so a test reads the Location the app sent.
func get(t *testing.T, srv *httptest.Server, path string, trace *httptrace.ClientTrace) *http.Response {
	t.Helper()
	req, err := http.NewRequest("GET", srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "example.com"
	if trace != nil {
		req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
	}
	c := srv.Client()
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func TestProductPathIsStripped(t *testing.T) {
	for target, want := range map[string]struct{ path, escaped, query string }{
		"/acme/notes?x=1":   {"/notes", "/notes", "x=1"},
		"/acme":             {"/", "/", ""},
		"/acme/":            {"/", "/", ""},
		"/acme/files/a%2Fb": {"/files/a/b", "/files/a%2Fb", ""},
		"/acme/q%3Fx":       {"/q?x", "/q%3Fx", ""},
	} {
		var path, escaped, query, requestURI, base, original string
		var direct, routed, called bool
		serve(acme, "GET", "example.com", target, func(_ http.ResponseWriter, r *http.Request) {
			called = true
			path, escaped, query, requestURI = r.URL.Path, r.URL.EscapedPath(), r.URL.RawQuery, r.RequestURI
			base, original = basepath.Base(r), basepath.Original(r)
			direct, routed = basepath.OnDirectHost(r), basepath.PathRouted(r)
		})
		if !called {
			t.Errorf("%s: next was not called", target)
			continue
		}
		if path != want.path || escaped != want.escaped || query != want.query {
			t.Errorf("%s: next saw path %q, escaped %q, query %q; want %q, %q, %q",
				target, path, escaped, query, want.path, want.escaped, want.query)
		}
		if requestURI != target {
			t.Errorf("%s: r.RequestURI = %q, want it unchanged", target, requestURI)
		}
		if original != target {
			t.Errorf("%s: Original = %q, want %q", target, original, target)
		}
		if base != "/acme" || direct || !routed {
			t.Errorf("%s: Base %q, OnDirectHost %v, PathRouted %v; want /acme, false, true", target, base, direct, routed)
		}
	}
}

func TestUncleanPathsAreCanonicalOrRefused(t *testing.T) {
	for _, c := range []struct {
		method, target string
		status         int
		location       string
	}{
		{"GET", "//acme/x", http.StatusMovedPermanently, "/acme/x"},
		{"GET", "/acme/./x?q=1", http.StatusMovedPermanently, "/acme/x?q=1"},
		{"GET", "/acme/./files/a%2Fb", http.StatusMovedPermanently, "/acme/files/a%2Fb"},
		{"GET", "/acme/./a%3Fb%23c", http.StatusMovedPermanently, "/acme/a%3Fb%23c"},
		{"POST", "/acme//x", http.StatusPermanentRedirect, "/acme/x"},
		{"GET", "/acme/../bob", http.StatusNotFound, ""},
		{"GET", "/acmex", http.StatusNotFound, ""},
		{"GET", "/ACME/x", http.StatusNotFound, ""},
		{"GET", "/%61cme/x", http.StatusNotFound, ""},
		{"GET", "/", http.StatusNotFound, ""},
		{"GET", "/static/x.css", http.StatusNotFound, ""},
	} {
		called := false
		rec := serve(acme, c.method, "example.com", c.target, func(http.ResponseWriter, *http.Request) { called = true })
		if called {
			t.Errorf("%s %s: next was called", c.method, c.target)
		}
		if rec.Code != c.status || rec.Header().Get("Location") != c.location {
			t.Errorf("%s %s: %d, Location %q; want %d, %q",
				c.method, c.target, rec.Code, rec.Header().Get("Location"), c.status, c.location)
		}
	}
}

func TestASlugThatIsAlsoARouteIsHandledOnce(t *testing.T) {
	admin := basepath.Config{Origin: "https://example.com", Slug: "admin"}
	var path, link string
	rec := serve(admin, "GET", "example.com", "/admin/admin/x", func(w http.ResponseWriter, r *http.Request) {
		path, link = r.URL.Path, basepath.Path(r, "/admin/x")
		http.Redirect(w, r, "/admin/x", http.StatusSeeOther)
	})
	if path != "/admin/x" {
		t.Errorf("next saw %q, want /admin/x", path)
	}
	if link != "/admin/admin/x" {
		t.Errorf("Path(r, /admin/x) = %q, want /admin/admin/x", link)
	}
	if got := rec.Header().Get("Location"); got != "/admin/admin/x" {
		t.Errorf("Location = %q, want /admin/admin/x", got)
	}
}

func TestHeadersAreRewrittenOnce(t *testing.T) {
	for set, want := range map[string]string{
		"/done":                               "/acme/done",
		"/":                                   "/acme",
		"/?q=1":                               "/acme?q=1",
		"/#top":                               "/acme#top",
		"next":                                "next",
		"https://example.com/_carlos/refresh": "https://example.com/_carlos/refresh",
		"//evil.example/":                     "//evil.example/",
		"/\\evil.example/":                    "/\\evil.example/",
		"":                                    "",
	} {
		rec := serve(acme, "GET", "example.com", "/acme/x", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Location", set)
			w.Header().Set("Rastrillo-Location", set)
			w.WriteHeader(http.StatusSeeOther)
		})
		for _, k := range []string{"Location", "Rastrillo-Location"} {
			if got := rec.Header().Get(k); got != want {
				t.Errorf("%s set to %q reads %q, want %q", k, set, got, want)
			}
		}
	}

	for name, h := range map[string]http.HandlerFunc{
		"a 201": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Location", "/notes/1")
			w.WriteHeader(http.StatusCreated)
		},
		"an implicit 200 on Write": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Location", "/notes/1")
			_, _ = w.Write([]byte("ok"))
		},
		"the first Flush": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Location", "/notes/1")
			w.(http.Flusher).Flush()
		},
		"a handler that writes nothing": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Location", "/notes/1")
		},
		"WriteHeader twice": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Location", "/notes/1")
			w.WriteHeader(http.StatusSeeOther)
			w.WriteHeader(http.StatusSeeOther)
		},
		"a write after the header": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Location", "/notes/1")
			w.WriteHeader(http.StatusSeeOther)
			_, _ = w.Write([]byte("see other"))
			w.(http.Flusher).Flush()
		},
	} {
		rec := serve(acme, "GET", "example.com", "/acme/x", h)
		if got := rec.Result().Header.Get("Location"); got != "/acme/notes/1" {
			t.Errorf("%s: Location = %q, want /acme/notes/1", name, got)
		}
	}
}

func TestInformationalResponsesDoNotPrefixTwice(t *testing.T) {
	var during []string
	mux := http.NewServeMux()
	mux.HandleFunc("/twice", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", "/done")
		w.WriteHeader(http.StatusEarlyHints)
		during = append(during, w.Header().Get("Location"))
		w.WriteHeader(http.StatusEarlyHints)
		during = append(during, w.Header().Get("Location"))
		w.WriteHeader(http.StatusSeeOther)
	})
	mux.HandleFunc("/own", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", "/done")
		w.WriteHeader(http.StatusEarlyHints)
		// The app's own /acme/done route: the base goes on in front.
		w.Header().Set("Location", "/acme/done")
		w.WriteHeader(http.StatusSeeOther)
	})
	var hinted []string
	srv := live(t, acme, mux)
	trace := &httptrace.ClientTrace{Got1xxResponse: func(code int, h textproto.MIMEHeader) error {
		hinted = append(hinted, h.Get("Location"))
		return nil
	}}

	resp := get(t, srv, "/acme/twice", trace)
	if want := []string{"/acme/done", "/acme/done"}; strings.Join(hinted, " ") != strings.Join(want, " ") {
		t.Errorf("the 103s carried Location %q, want %q", hinted, want)
	}
	if got := resp.Header.Get("Location"); resp.StatusCode != http.StatusSeeOther || got != "/acme/done" {
		t.Errorf("final: %d, Location %q; want 303, /acme/done", resp.StatusCode, got)
	}
	if strings.Join(during, " ") != "/done /done" {
		t.Errorf("after each 103 the handler read Location %q, want its own /done", during)
	}

	hinted = nil
	resp = get(t, srv, "/acme/own", trace)
	if got := resp.Header.Get("Location"); got != "/acme/acme/done" {
		t.Errorf("final Location = %q, want /acme/acme/done", got)
	}

	// A hijacked connection belongs to the handler: no header it set is
	// rewritten, then or after it returns.
	var afterHijack, afterReturn string
	upgrade := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", "/done")
		conn, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Errorf("Hijack: %v", err)
			return
		}
		defer conn.Close()
		afterHijack = w.Header().Get("Location")
		_, _ = rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: x\r\nConnection: Upgrade\r\n\r\n")
		_ = rw.Flush()
	})
	inner := acme.Handler(upgrade)
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inner.ServeHTTP(w, r)
		afterReturn = w.Header().Get("Location")
	}))
	defer srv2.Close()
	req, _ := http.NewRequest("GET", srv2.URL+"/acme/ws", nil)
	req.Host = "example.com"
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "x")
	up, err := srv2.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	up.Body.Close()
	if up.StatusCode != http.StatusSwitchingProtocols {
		t.Errorf("upgrade answered %d, want 101", up.StatusCode)
	}
	if afterHijack != "/done" || afterReturn != "/done" {
		t.Errorf("after Hijack Location read %q, after return %q; want /done both times", afterHijack, afterReturn)
	}
}

// This passes before the middleware exists, and must go on passing:
// only Location and Rastrillo-Location carry app paths the middleware
// owns. Rastrillo-Host names the instance for Plan 4's route check, and
// a body-like header such as Link or Refresh is the app's to write.
func TestOtherHeadersPassThrough(t *testing.T) {
	headers := map[string]string{
		"Rastrillo-Host":   "acme-abc234.bdf.oncarlos.com",
		"Link":             "</x>; rel=preload",
		"Content-Location": "/x",
		"Refresh":          "0; url=/x",
	}
	srv := live(t, acme, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		for k, v := range headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(http.StatusOK)
	}))
	resp := get(t, srv, "/acme/x", nil)
	for k, v := range headers {
		if got := resp.Header.Get(k); got != v {
			t.Errorf("%s = %q at the client, want %q", k, got, v)
		}
	}
}

func TestRewritingWriterKeepsFlushHijackAndUnwrap(t *testing.T) {
	var rec perf.Recorder
	measured := perf.Middleware(&rec, perf.Options{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	release := make(chan struct{})
	var outer http.ResponseWriter
	var unwrapped http.ResponseWriter

	mux := http.NewServeMux()
	mux.HandleFunc("/flush", func(w http.ResponseWriter, _ *http.Request) {
		f, ok := w.(http.Flusher)
		if !ok {
			t.Error("the writer is not an http.Flusher")
			return
		}
		_, _ = w.Write([]byte("first"))
		f.Flush()
		<-release
		_, _ = w.Write([]byte(" second"))
	})
	mux.HandleFunc("/controller", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("x"))
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Errorf("ResponseController.Flush = %v", err)
		}
	})
	mux.HandleFunc("/hijack", func(w http.ResponseWriter, _ *http.Request) {
		conn, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Errorf("Hijack: %v", err)
			return
		}
		defer conn.Close()
		_, _ = rw.WriteString("HTTP/1.1 200 OK\r\nContent-Length: 6\r\nConnection: close\r\n\r\nraw ok")
		_ = rw.Flush()
	})
	mux.HandleFunc("/unwrap", func(w http.ResponseWriter, _ *http.Request) {
		// perf's writer, then basepath's rewriter, then net/http's own.
		rw := w.(interface{ Unwrap() http.ResponseWriter }).Unwrap()
		u, ok := rw.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			t.Errorf("the writer under perf's (%T) has no Unwrap", rw)
			return
		}
		unwrapped = u.Unwrap()
	})
	inner := acme.Handler(measured(mux))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		outer = w
		inner.ServeHTTP(w, r)
	}))
	defer srv.Close()

	resp := get(t, srv, "/acme/flush", nil)
	buf := make([]byte, len("first"))
	if _, err := io.ReadFull(resp.Body, buf); err != nil || string(buf) != "first" {
		t.Errorf("before the handler returned the client read %q, %v; want the flushed chunk", buf, err)
	}
	close(release)
	rest, _ := io.ReadAll(resp.Body)
	if string(rest) != " second" {
		t.Errorf("the rest of the body was %q", rest)
	}

	get(t, srv, "/acme/controller", nil)

	// Hijack by hand: a raw response on the hijacked conn must reach the
	// client intact.
	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_, _ = io.WriteString(conn, "GET /acme/hijack HTTP/1.1\r\nHost: example.com\r\n\r\n")
	raw, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatalf("reading the hijacked response: %v", err)
	}
	body, _ := io.ReadAll(raw.Body)
	if raw.StatusCode != http.StatusOK || !bytes.Equal(body, []byte("raw ok")) {
		t.Errorf("hijacked response: %d %q; want 200 raw ok", raw.StatusCode, body)
	}

	get(t, srv, "/acme/unwrap", nil)
	if unwrapped == nil || unwrapped != outer {
		t.Errorf("the rewriter's Unwrap returned %T, want the writer underneath it (%T)", unwrapped, outer)
	}
}

func TestOutsideTheMiddlewareNothingChanges(t *testing.T) {
	// This passes before the middleware exists, and must go on passing:
	// with no Config installed every accessor is the identity or off, so
	// an app that never sets a base path behaves as it always has.
	r := httptest.NewRequest("GET", "/x?y=1", nil)
	if got := basepath.Base(r); got != "" {
		t.Errorf("Base = %q", got)
	}
	if got := basepath.Path(r, "/x"); got != "/x" {
		t.Errorf("Path(r, /x) = %q", got)
	}
	if got := basepath.Original(r); got != "/x?y=1" {
		t.Errorf("Original = %q", got)
	}
	r.RequestURI = ""
	if got := basepath.Original(r); got != "/x?y=1" {
		t.Errorf("Original with no RequestURI = %q, want r.URL's", got)
	}
	if basepath.OnDirectHost(r) || basepath.PathRouted(r) || basepath.PlatformCall(r) {
		t.Errorf("OnDirectHost %v, PathRouted %v, PlatformCall %v; want all false",
			basepath.OnDirectHost(r), basepath.PathRouted(r), basepath.PlatformCall(r))
	}
	if got := basepath.ProductURL(r); got != "" {
		t.Errorf("ProductURL = %q", got)
	}
}

// pathCases are what Path does under /acme, and so what Join does with
// base /acme: only a path that starts with exactly one / is the app's.
var pathCases = map[string]string{
	"/x":          "/acme/x",
	"/":           "/acme",
	"/?q":         "/acme?q",
	"/#a":         "/acme#a",
	"x":           "x",
	"":            "",
	"https://h/x": "https://h/x",
	"//h/x":       "//h/x",
	"/\\h":        "/\\h",
}

func TestPathOnlyPrefixesRootAbsolutePaths(t *testing.T) {
	serve(acme, "GET", "example.com", "/acme/x", func(_ http.ResponseWriter, r *http.Request) {
		for p, want := range pathCases {
			if got := basepath.Path(r, p); got != want {
				t.Errorf("Path(%q) = %q, want %q", p, got, want)
			}
		}
	})
}

func TestJoinIsThePrefixRule(t *testing.T) {
	for p, want := range pathCases {
		if got := basepath.Join("/acme", p); got != want {
			t.Errorf("Join(/acme, %q) = %q, want %q", p, got, want)
		}
	}
	if got := basepath.Join("", "/x"); got != "/x" {
		t.Errorf("Join(\"\", /x) = %q", got)
	}
	if got := basepath.Join("", ""); got != "" {
		t.Errorf("Join(\"\", \"\") = %q", got)
	}
	check := func(where string, r *http.Request) {
		for p := range pathCases {
			if got, want := basepath.Path(r, p), basepath.Join(basepath.Base(r), p); got != want {
				t.Errorf("%s: Path(r, %q) = %q, Join(Base(r), %q) = %q", where, p, got, p, want)
			}
		}
	}
	check("outside the middleware", httptest.NewRequest("GET", "/x", nil))
	called := false
	serve(acme, "GET", "example.com", "/acme/x", func(_ http.ResponseWriter, r *http.Request) {
		called = true
		check("under /acme", r)
	})
	if !called {
		t.Error("next was not called for /acme/x")
	}
}
