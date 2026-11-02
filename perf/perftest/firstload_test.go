package perftest

import (
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/internal/vendored"
	"amadan.net/rastrillo/rastrillo/ui"
)

// incompressible is size random bytes, whose gzip is a little over it, so a
// test can size a file's gzipped weight.
func incompressible(size int) []byte {
	body := make([]byte, size)
	rand.Read(body)
	return body
}

const (
	tokensHref = "/static/tokens.0123456789abcdef.css"
	selectHref = "/static/select.0123456789abcdef.js"
)

// smallBudget allows 1,500 bytes on a column page, and pins tokens.css
// at 1,200 and select.js at 600.
var smallBudget = firstLoadBudget{
	shells:     map[string]int{"column": 1500, "console": 3000},
	references: map[string]int{"tokens.css": 1200, "select.js": 600},
}

// site serves pages by path and files by href, as an app would.
func site(pages map[string]string, files map[string][]byte) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		if page, ok := pages[request.URL.Path]; ok {
			writer.Header().Set("Content-Type", "text/html; charset=utf-8")
			writer.Write([]byte(page))
			return
		}
		if body, ok := files[request.URL.Path]; ok {
			writer.Write(body)
			return
		}
		http.NotFound(writer, request)
	}))
}

func page(links ...string) string {
	var out strings.Builder
	out.WriteString("<!doctype html><head>")
	for _, href := range links {
		if strings.HasSuffix(href, ".css") {
			out.WriteString(`<link rel="stylesheet" href="` + href + `">`)
		} else {
			out.WriteString(`<script defer src="` + href + `"></script>`)
		}
	}
	out.WriteString("</head><body><h1>Orders</h1></body>")
	return out.String()
}

// noRedirects is a client like the one startServer returns, which hands
// a redirect back rather than following it.
func noRedirects() *http.Client {
	return &http.Client{CheckRedirect: func(
		*http.Request, []*http.Request,
	) error {
		return http.ErrUseLastResponse
	}}
}

// checkOne runs checkFirstLoad over GET /orders on server.
func checkOne(server *httptest.Server, budget firstLoadBudget) *fakeT {
	recorder := &fakeT{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		checkFirstLoad(recorder, noRedirects(), server.URL,
			[]target{{screen: "GET /orders", path: "/orders",
				expect: http.StatusOK}}, budget)
	}()
	<-done
	return recorder
}

func TestFirstLoadCountsOnlyTheFrameworksFiles(t *testing.T) {
	server := site(map[string]string{
		"/orders": page(tokensHref, "/static/app.css", "/pow/pow.js"),
	}, map[string][]byte{
		tokensHref:        incompressible(1000),
		"/static/app.css": incompressible(5000),
		"/pow/pow.js":     incompressible(5000),
	})
	defer server.Close()
	if recorder := checkOne(server, smallBudget); recorder.failed {
		t.Errorf("an app's own files were counted: %v", recorder.errs)
	}
}

func TestAPageOverItsBudgetNamesTheFileThatGrew(t *testing.T) {
	server := site(map[string]string{"/orders": page(tokensHref)},
		map[string][]byte{tokensHref: incompressible(2000)})
	defer server.Close()
	recorder := checkOne(server, smallBudget)
	want := "over its 1500-byte budget (1500 bytes for a column page); " +
		"tokens.css grew from 1200 to "
	if len(recorder.errs) != 1 || !strings.Contains(recorder.errs[0], want) {
		t.Errorf("got %q, want a line containing %q", recorder.errs, want)
	}
}

func TestAPageOverItsBudgetWithNothingGrownListsItsFiles(t *testing.T) {
	budget := firstLoadBudget{
		shells:     map[string]int{"column": 500},
		references: map[string]int{"tokens.css": 1200},
	}
	server := site(map[string]string{"/orders": page(tokensHref)},
		map[string][]byte{tokensHref: incompressible(1000)})
	defer server.Close()
	recorder := checkOne(server, budget)
	want := "(500 bytes for a column page); no file grew: it loads tokens.css"
	if len(recorder.errs) != 1 || !strings.Contains(recorder.errs[0], want) {
		t.Errorf("got %q, want a line containing %q", recorder.errs, want)
	}
}

func TestAComponentScriptRaisesThePagesBudget(t *testing.T) {
	server := site(map[string]string{
		"/orders": page(tokensHref, selectHref),
	}, map[string][]byte{
		tokensHref: incompressible(1000),
		selectHref: incompressible(500),
	})
	defer server.Close()
	if recorder := checkOne(server, smallBudget); recorder.failed {
		t.Errorf("select.js did not raise the budget: %v", recorder.errs)
	}
}

func TestTheShellComesFromThePage(t *testing.T) {
	cases := map[string]string{
		`<div rst-shell-console="page">`: "console",
		`<div rst-shell-sidebar="page">`: "sidebar",
		`<div rst-shell-topbar>`:         "topbar",
		`<body rst-stage>`:               "stage",
		`<body><main rst-page>`:          "column",
	}
	for markup, want := range cases {
		if got := shellOf([]byte(markup)); got != want {
			t.Errorf("shellOf(%s) = %s, want %s", markup, got, want)
		}
	}
}

func TestFirstLoadSkipsWhatIsNotAPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		if request.URL.Path == "/moved" {
			http.Redirect(writer, request, "/orders", http.StatusSeeOther)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.Write([]byte(`{"href": "` + selectHref + `"}`))
	}))
	defer server.Close()
	recorder := &fakeT{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		client := &http.Client{CheckRedirect: func(
			*http.Request, []*http.Request,
		) error {
			return http.ErrUseLastResponse
		}}
		checkFirstLoad(recorder, client, server.URL, []target{
			{screen: "GET /moved", path: "/moved", expect: http.StatusSeeOther},
			{screen: "GET /api", path: "/api", expect: http.StatusOK},
		}, smallBudget)
	}()
	<-done
	if recorder.failed {
		t.Errorf("checked what is not a page: %v", recorder.errs)
	}
}

func TestAFrameworkFileThatDoesNotLoadFailsByName(t *testing.T) {
	server := site(map[string]string{"/orders": page(selectHref)}, nil)
	defer server.Close()
	recorder := checkOne(server, smallBudget)
	want := "GET /orders links " + selectHref + ", which returns HTTP 404"
	if !slices.Contains(recorder.errs, want) {
		t.Errorf("got %q, want %q", recorder.errs, want)
	}
}

func TestFrameworkNameStripsTheFingerprint(t *testing.T) {
	cases := map[string]string{
		"/static/tokens.0123456789abcdef.css":            "tokens.css",
		"/acme/static/rastrillo.0123456789abcdef.js?v=1": "rastrillo.js",
		"/static/app.css":                                "",
		"/pow/pow.js":                                    "",
	}
	for href, want := range cases {
		got, ok := frameworkName(href)
		if got != want || ok != (want != "") {
			t.Errorf("frameworkName(%s) = %q, %v; want %q", href, got, ok, want)
		}
	}
}

func TestLinkedFilesReadsStylesheetsAndScripts(t *testing.T) {
	markup := `<link href="/a.css" rel="stylesheet">` +
		`<link rel="preload" href="/font.woff2">` +
		`<script>inline()</script>` +
		`<script defer src="/b.js"></script>` +
		`<link rel="stylesheet" href="/a.css">`
	got := linkedFiles([]byte(markup))
	if want := []string{"/a.css", "/b.js"}; !slices.Equal(got, want) {
		t.Errorf("linkedFiles = %q, want %q", got, want)
	}
}

func TestTheBudgetCoversEveryFileAndShell(t *testing.T) {
	for _, name := range vendored.Names() {
		if frameworkBudget.references[name] == 0 {
			t.Errorf("no reference size for %s", name)
		}
	}
	for _, shell := range ui.LayoutNames() {
		if frameworkBudget.shells[shell] == 0 {
			t.Errorf("no limit for the %s shell", shell)
		}
	}
	if frameworkBudget.shells["column"] != 40000 ||
		frameworkBudget.shells["topbar"] != 40000 {
		t.Error("a plain column or topbar page's limit is not 40000")
	}
}

func TestLinkedFilesReadsEveryValidForm(t *testing.T) {
	markup := `<link rel='stylesheet' href='/a.css'>` +
		`<LINK REL="STYLESHEET" HREF="/b.css">` +
		`<script defer src=/c.js></script>` +
		`<!-- <script src="/commented.js"></script> -->`
	got := linkedFiles([]byte(markup))
	if want := []string{"/a.css", "/b.css", "/c.js"}; !slices.Equal(got, want) {
		t.Errorf("linkedFiles = %q, want %q", got, want)
	}
}

func TestAStageBodyWithOtherAttributesIsStage(t *testing.T) {
	got := shellOf([]byte(`<body class="signin" rst-stage>`))
	if got != "stage" {
		t.Errorf("shellOf = %s, want stage", got)
	}
}

func TestALinkIsWeighedOnceWhereverItResolves(t *testing.T) {
	server := site(map[string]string{
		"/orders": page(tokensHref, "."+tokensHref, tokensHref+"#top"),
	}, map[string][]byte{tokensHref: incompressible(1000)})
	defer server.Close()
	if recorder := checkOne(server, smallBudget); recorder.failed {
		t.Errorf("one file was weighed more than once: %v", recorder.errs)
	}
}

func TestARelativeLinkToAFrameworkFileIsWeighed(t *testing.T) {
	server := site(map[string]string{
		"/static/demo": page("tokens.0123456789abcdef.css"),
	}, map[string][]byte{tokensHref: incompressible(2000)})
	defer server.Close()
	recorder := &fakeT{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		checkFirstLoad(recorder, server.Client(), server.URL,
			[]target{{screen: "GET /static/demo", path: "/static/demo",
				expect: http.StatusOK}}, smallBudget)
	}()
	<-done
	if !recorder.failed {
		t.Error("a relative link to tokens.css was not weighed")
	}
}

func TestAPageThatAnswersAnErrorFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		http.Error(writer, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()
	recorder := checkOne(server, smallBudget)
	if len(recorder.errs) != 1 ||
		!strings.Contains(recorder.errs[0], "answered 500, want 200") {
		t.Errorf("got %q, want the page's 500 reported", recorder.errs)
	}
}

func TestAStalledFileFailsInsteadOfHanging(t *testing.T) {
	defer func(previous time.Duration) { requestBound = previous }(requestBound)
	requestBound = 100 * time.Millisecond
	stall := make(chan struct{})
	defer close(stall)
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		if request.URL.Path == "/orders" {
			writer.Header().Set("Content-Type", "text/html")
			writer.Write([]byte(page(tokensHref)))
			return
		}
		select {
		case <-stall:
		case <-request.Context().Done():
		}
	}))
	defer server.Close()
	recorder := checkOne(server, smallBudget)
	if !recorder.failed {
		t.Error("a stylesheet that never answers passed")
	}
}

func TestARedirectedFileIsWeighedWhereItLands(t *testing.T) {
	server := site(map[string]string{"/orders": page("/static/tokens.css")},
		map[string][]byte{tokensHref: incompressible(2000)})
	defer server.Close()
	redirecting := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		if request.URL.Path == "/static/tokens.css" {
			http.Redirect(writer, request, tokensHref, http.StatusFound)
			return
		}
		server.Config.Handler.ServeHTTP(writer, request)
	}))
	defer redirecting.Close()
	recorder := checkOne(redirecting, smallBudget)
	want := "tokens.css grew from 1200"
	if len(recorder.errs) != 1 || !strings.Contains(recorder.errs[0], want) {
		t.Errorf("got %q, want the redirected file weighed", recorder.errs)
	}
}

// Review round 2: what a regex over the page got wrong.
func TestScriptTextAndQuotedAttributesHideNoLink(t *testing.T) {
	markup := `<script>const begin = "<!--";</script>` +
		`<link rel="stylesheet" href="/a.css">` +
		`<script>const end = "-->";</script>` +
		`<link title="a > b" rel="stylesheet" href="/b.css">`
	got := linkedFiles([]byte(markup))
	if want := []string{"/a.css", "/b.css"}; !slices.Equal(got, want) {
		t.Errorf("linkedFiles = %q, want %q", got, want)
	}
}

func TestOnlyTheStageAttributeMakesAStagePage(t *testing.T) {
	for _, markup := range []string{
		`<body data-note="signin rst-stage-preview">`,
		`<body title="example rst-stage page">`,
	} {
		if got := shellOf([]byte(markup)); got != "column" {
			t.Errorf("shellOf(%s) = %s, want column", markup, got)
		}
	}
}

func TestABaseHrefIsHonoured(t *testing.T) {
	server := site(map[string]string{
		"/reports/1": `<head><base href="/app/">` +
			`<link rel="stylesheet" ` +
			`href="static/tokens.0123456789abcdef.css">` +
			`</head>`,
	}, map[string][]byte{
		"/app" + tokensHref: incompressible(1000),
	})
	defer server.Close()
	recorder := &fakeT{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		checkFirstLoad(recorder, noRedirects(), server.URL,
			[]target{{screen: "GET /reports/1", path: "/reports/1",
				expect: http.StatusOK}}, smallBudget)
	}()
	<-done
	if recorder.failed {
		t.Errorf("the link was not resolved against <base>: %v",
			recorder.errs)
	}
}

func TestARedirectToAPageIsNotAFrameworkFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		switch request.URL.Path {
		case "/static/select.js":
			http.Redirect(writer, request, "/signin", http.StatusFound)
		case "/orders":
			writer.Header().Set("Content-Type", "text/html")
			writer.Write([]byte(page("/static/select.js")))
		default:
			writer.Header().Set("Content-Type", "text/html")
			writer.Write([]byte("<h1>Sign in</h1>"))
		}
	}))
	defer server.Close()
	if recorder := checkOne(server, smallBudget); !recorder.failed {
		t.Error("a sign-in page was weighed as select.js")
	}
}

func TestASigninThatOutlivesItsBoundFails(t *testing.T) {
	defer func(previous time.Duration) { signinBound = previous }(signinBound)
	signinBound = 50 * time.Millisecond
	release := make(chan struct{})
	defer close(release)
	recorder := &fakeT{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		client, server, _ := startServer(recorder, ScreenConfig{
			Handler: http.NotFoundHandler(),
			Signin:  func(*http.Client, string) { <-release },
		})
		_ = client
		closeBounded(server)
	}()
	<-done
	if !recorder.failed {
		t.Error("a sign-in still running past its bound did not fail")
	}
}
