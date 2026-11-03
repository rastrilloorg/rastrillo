package perftest

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/internal/budgetfile"
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
	bareTokens = "/static/tokens.css"
	selectHref = "/static/select.0123456789abcdef.js"
)

// smallBudget allows 1,500 bytes on a column page, sliced or not, and
// pins tokens.css at 1,200 and select.js at 600.
var smallBudget = firstLoadBudget{
	shells:     map[string]int{"column": 1500, "console": 3000},
	unsliced:   1500,
	references: map[string]int{"tokens.css": 1200, "select.js": 600},
}

// site serves pages by path and files by href, as an app would. Like
// Assets, it answers a bare static/tokens.css with the fingerprinted
// file's bytes, the whole file, unless files gives it bytes of its own.
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
		prefix, bare := strings.CutSuffix(request.URL.Path, bareTokens)
		if body, ok := files[prefix+tokensHref]; ok && bare {
			writer.Write(body)
			return
		}
		http.NotFound(writer, request)
	}))
}

func page(links ...string) string {
	return pageShowing("<h1>Orders</h1>", links...)
}

// pageShowing is a page whose body is markup, linking each of links.
func pageShowing(markup string, links ...string) string {
	var out strings.Builder
	out.WriteString("<!doctype html><head>")
	for _, href := range links {
		if strings.HasSuffix(href, ".css") {
			out.WriteString(`<link rel="stylesheet" href="` + href + `">`)
		} else {
			out.WriteString(`<script defer src="` + href + `"></script>`)
		}
	}
	out.WriteString("</head><body>" + markup + "</body>")
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
	want := "over its 1500-byte budget (1500 bytes for a page linking " +
		"the whole tokens.css); tokens.css grew from 1200 to "
	if len(recorder.errs) != 1 || !strings.Contains(recorder.errs[0], want) {
		t.Errorf("got %q, want a line containing %q", recorder.errs, want)
	}
}

func TestAPageOverItsBudgetWithNothingGrownListsItsFiles(t *testing.T) {
	budget := firstLoadBudget{
		shells:     map[string]int{"column": 500},
		unsliced:   500,
		references: map[string]int{"tokens.css": 1200},
	}
	server := site(map[string]string{"/orders": page(tokensHref)},
		map[string][]byte{tokensHref: incompressible(1000)})
	defer server.Close()
	recorder := checkOne(server, budget)
	want := "(500 bytes for a page linking the whole tokens.css); " +
		"no file grew: it loads tokens.css"
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
	for shell, limit := range frameworkBudget.shells {
		if limit != 16000 {
			t.Errorf("the %s shell's sliced limit is %d, not 16000",
				shell, limit)
		}
	}
	if frameworkBudget.unsliced != 40000 {
		t.Error("an unsliced page's limit is not 40000")
	}
	for _, shell := range ui.LayoutNames() {
		live := frameworkBudget.live[shell]
		sliced := frameworkBudget.shells[shell]
		if live < sliced {
			t.Errorf("the %s shell's live limit, %d, is under its "+
				"sliced limit, %d", shell, live, sliced)
		}
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

// commentSheet is a stylesheet that the slicer reads, one comment of
// random text, gzipping to a little over size bytes.
func commentSheet(size int) []byte {
	text := base64.StdEncoding.EncodeToString(incompressible(size))
	return []byte("/*" + text + "*/")
}

// reported collects what checkFirstLoad says of the pages that pass,
// for as long as the test runs.
func reported(t *testing.T) *[]string {
	var lines []string
	previous := report
	report = func(format string, values ...any) {
		lines = append(lines, fmt.Sprintf(format, values...))
	}
	t.Cleanup(func() { report = previous })
	return &lines
}

func TestASlicedPageIsHeldToSixteenKilobytes(t *testing.T) {
	server := site(map[string]string{"/orders": page(tokensHref)},
		map[string][]byte{
			tokensHref: commentSheet(16500),
			bareTokens: commentSheet(20000),
		})
	defer server.Close()
	recorder := checkOne(server, frameworkBudget)
	want := "GET /orders: "
	if len(recorder.errs) != 1 ||
		!strings.HasPrefix(recorder.errs[0], want) ||
		!strings.Contains(recorder.errs[0], "over its 16000-byte budget") {
		t.Errorf("got %q, want the page held to 16000", recorder.errs)
	}
	if !strings.Contains(strings.Join(recorder.errs, ""),
		"its largest files are tokens.css's slice (") {
		t.Errorf("got %q, want the largest files named", recorder.errs)
	}
}

func TestAnUnslicedPageKeepsTheOldLimitAndSaysHowToSlice(t *testing.T) {
	lines := reported(t)
	whole := commentSheet(30000)
	server := site(map[string]string{"/orders": page(tokensHref)},
		map[string][]byte{tokensHref: whole, bareTokens: whole})
	defer server.Close()
	if recorder := checkOne(server, frameworkBudget); recorder.failed {
		t.Fatalf("an unsliced page was held to less than 40000: %v",
			recorder.errs)
	}
	said := strings.Join(*lines, "\n")
	for _, want := range []string{
		"GET /orders", "PagesConfig.Assets", "pageStylesheet",
		"{{range pageScripts}}",
	} {
		if !strings.Contains(said, want) {
			t.Errorf("the report %q does not name %s", said, want)
		}
	}
	heavier := commentSheet(41000)
	server = site(map[string]string{"/orders": page(bareTokens)},
		map[string][]byte{bareTokens: heavier})
	defer server.Close()
	recorder := checkOne(server, frameworkBudget)
	if len(recorder.errs) != 1 ||
		!strings.Contains(recorder.errs[0], "over its 40000-byte budget") {
		t.Errorf("got %q, want the page held to 40000", recorder.errs)
	}
}

func TestAnUnreadableStylesheetSaysWhyThePageIsUnsliced(t *testing.T) {
	lines := reported(t)
	whole := []byte(".rst-card{color:red;@media (width>1px){color:blue}}")
	server := site(map[string]string{"/orders": page(tokensHref)},
		map[string][]byte{tokensHref: whole})
	defer server.Close()
	if recorder := checkOne(server, frameworkBudget); recorder.failed {
		t.Fatalf("got %q, want a pass", recorder.errs)
	}
	said := strings.Join(*lines, "\n")
	if !strings.Contains(said, "static/tokens.css cannot be sliced") ||
		strings.Contains(said, "PagesConfig.Assets") {
		t.Errorf("the report %q does not say tokens.css is unreadable",
			said)
	}
}

// slicedSite serves markup on GET /orders, linking a slice of a whole
// file that styles rst-card and rst-btn; the slice keeps rst-btn's
// rule alone.
func slicedSite(markup string) *httptest.Server {
	return slicedSiteOf(markup, "[rst-btn]{color:blue}",
		"[rst-btn]{color:blue}.rst-card{color:red}")
}

// slicedSiteOf serves markup on GET /orders, linking slice as a slice
// of whole.
func slicedSiteOf(markup, slice, whole string) *httptest.Server {
	return site(map[string]string{
		"/orders": pageShowing(markup, tokensHref),
	}, map[string][]byte{
		tokensHref: []byte(slice),
		bareTokens: []byte(whole),
	})
}

func TestAHookWithoutRulesInItsSliceFails(t *testing.T) {
	for _, markup := range []string{
		`<button rst-btn>Save</button><article rst-card>Due</article>`,
		// A hook in a class attribute is the case here.
		// markup-spelling: old-spelling begin
		`<button rst-btn>Save</button><div class="due rst-card">1</div>`,
		// markup-spelling: old-spelling end
	} {
		server := slicedSite(markup)
		recorder := checkOne(server, frameworkBudget)
		server.Close()
		joined := strings.Join(recorder.errs, "\n")
		for _, want := range []string{
			"GET /orders", "rst-card", `{{needs "rst-card"}}`,
		} {
			if !strings.Contains(joined, want) {
				t.Errorf("%s: got %q, want %s named", markup, joined, want)
			}
		}
		if len(recorder.errs) != 1 || strings.Contains(joined, "rst-btn") {
			t.Errorf("%s: got %q, want rst-card's failure alone",
				markup, joined)
		}
	}
}

// select.js copies the classes an option's data-rst-class names onto
// the row it draws, so a page shows them though its templates may hold
// only {{.RowClass}}.
func TestAHookAnOptionGivesItsRowIsShown(t *testing.T) {
	// The attribute's value is the case here.
	// markup-spelling: old-spelling begin
	markup := `<select data-rst-select><option rst-btn ` +
		`data-rst-class="due rst-card">1</option></select>`
	// markup-spelling: old-spelling end
	server := slicedSite(markup)
	recorder := checkOne(server, frameworkBudget)
	server.Close()
	joined := strings.Join(recorder.errs, "\n")
	if len(recorder.errs) != 1 ||
		!strings.Contains(joined, `{{needs "rst-card"}}`) {
		t.Errorf("got %q, want rst-card's failure alone", joined)
	}
	kept := slicedSiteOf(markup,
		"[rst-btn]{color:blue}.rst-card{color:red}",
		"[rst-btn]{color:blue}.rst-card{color:red}")
	defer kept.Close()
	if recorder := checkOne(kept, frameworkBudget); recorder.failed {
		t.Errorf("a slice keeping rst-card failed: %v", recorder.errs)
	}
}

func TestAHookTheWholeFileDoesNotStyleIsFine(t *testing.T) {
	// A hook in a class attribute is the case here.
	// markup-spelling: old-spelling begin
	server := slicedSite(`<button rst-btn class="rst-made-up">Save` +
		`</button><div rst-made-up data-note="rst-card">rst-card</div>`)
	// markup-spelling: old-spelling end
	defer server.Close()
	if recorder := checkOne(server, frameworkBudget); recorder.failed {
		t.Errorf("a hook no rule styles failed: %v", recorder.errs)
	}
}

func TestAWholeFileThatDoesNotAnswerFailsByName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		switch request.URL.Path {
		case "/orders":
			writer.Header().Set("Content-Type", "text/html")
			writer.Write([]byte(page(tokensHref)))
		case tokensHref:
			writer.Write(commentSheet(1000))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	recorder := checkOne(server, frameworkBudget)
	want := "GET /orders links " + server.URL + tokensHref +
		", but the whole file to compare it with, " + server.URL +
		bareTokens + ", returns HTTP 404"
	if !slices.Contains(recorder.errs, want) {
		t.Errorf("got %q, want %q", recorder.errs, want)
	}
}

// liveBudget allows a sliced column page 1,500 bytes, a live one 2,000,
// and pins live.js at 500.
var liveBudget = firstLoadBudget{
	shells:     map[string]int{"column": 1500},
	live:       map[string]int{"column": 2000},
	unsliced:   1500,
	references: map[string]int{"live.js": 500},
}

// livePage serves a page linking a slice of size bytes and live.js.
func livePage(size int) *httptest.Server {
	const liveHref = "/static/live.0123456789abcdef.js"
	return site(map[string]string{"/jobs": page(tokensHref, liveHref)},
		map[string][]byte{
			tokensHref: incompressible(size),
			bareTokens: incompressible(3000),
			liveHref:   incompressible(450),
		})
}

func TestALivePageIsHeldToItsShellsLiveLimit(t *testing.T) {
	check := func(server *httptest.Server) *fakeT {
		recorder := &fakeT{}
		done := make(chan struct{})
		go func() {
			defer close(done)
			checkFirstLoad(recorder, noRedirects(), server.URL,
				[]target{{screen: "GET /jobs", path: "/jobs",
					expect: http.StatusOK}}, liveBudget)
		}()
		<-done
		return recorder
	}
	under := livePage(1900)
	defer under.Close()
	if recorder := check(under); recorder.failed {
		t.Errorf("a live page under 2000 plus live.js failed: %v",
			recorder.errs)
	}
	over := livePage(2200)
	defer over.Close()
	recorder := check(over)
	want := "over its 2500-byte budget (2000 bytes for a live column " +
		"page, plus live.js)"
	if len(recorder.errs) != 1 || !strings.Contains(recorder.errs[0], want) {
		t.Errorf("got %q, want a line containing %q", recorder.errs, want)
	}
}

// Review round 1: a hook's rules count only where the slicer would keep
// them for the hooks the page shows.
func TestAHookWhoseRulesNeedAPartnerItDoesNotShowIsFine(t *testing.T) {
	server := slicedSiteOf(
		`<button rst-btn>Save</button><div class="rst-grow">1</div>`,
		"[rst-btn]{color:blue}",
		"[rst-btn]{color:blue}.rst-field-row>.rst-grow{flex:1}")
	defer server.Close()
	if recorder := checkOne(server, frameworkBudget); recorder.failed {
		t.Errorf("a hook whose rule needs rst-field-row failed: %v",
			recorder.errs)
	}
}

func TestAHookNamedInItsSliceOnlyUnderNotFails(t *testing.T) {
	// Hooks in a class attribute are the case here.
	// markup-spelling: old-spelling begin
	markup := `<button class="rst-btn rst-danger">x</button>`
	// markup-spelling: old-spelling end
	server := slicedSiteOf(markup,
		".rst-btn:not(.rst-danger){color:blue}",
		".rst-btn:not(.rst-danger){color:blue}.rst-danger{color:red}")
	defer server.Close()
	recorder := checkOne(server, frameworkBudget)
	want := `{{needs "rst-danger"}}`
	if len(recorder.errs) != 1 ||
		!strings.Contains(recorder.errs[0], "shows rst-danger") ||
		!strings.Contains(recorder.errs[0], want) {
		t.Errorf("got %q, want rst-danger's failure alone", recorder.errs)
	}
}

// recorded is budget with the weight records that budgets.txt text
// gives, as FirstLoad reads them.
func recorded(
	t *testing.T, budget firstLoadBudget, text string,
) firstLoadBudget {
	t.Helper()
	file, err := budgetfile.Parse(strings.NewReader(text))
	if err != nil {
		t.Fatal(err)
	}
	return withRecords(budget, file)
}

func TestARecordRaisesAPagesWeightLimit(t *testing.T) {
	budget := recorded(t, smallBudget,
		"# the orders page\nweight GET /orders 2500 shows every column\n")
	server := site(map[string]string{"/orders": page(tokensHref)},
		map[string][]byte{tokensHref: incompressible(2000)})
	defer server.Close()
	if recorder := checkOne(server, budget); recorder.failed {
		t.Errorf("a page within its record failed: %v", recorder.errs)
	}
	heavier := site(map[string]string{"/orders": page(tokensHref)},
		map[string][]byte{tokensHref: incompressible(3000)})
	defer heavier.Close()
	recorder := checkOne(heavier, budget)
	want := "over its 2500-byte budget (its record, " +
		".rastrillo/budgets.txt line 2)"
	if len(recorder.errs) != 1 || !strings.Contains(recorder.errs[0], want) {
		t.Fatalf("got %q, want a line containing %q", recorder.errs, want)
	}
	want = fmt.Sprintf("raise its record to %d: %s",
		suggestedRecord(t, recorder.errs[0]), plusTenPercent)
	if !strings.Contains(recorder.errs[0], want) {
		t.Errorf("got %q, want a line containing %q", recorder.errs, want)
	}
}

// plusTenPercent is how every weight hint says where its number comes
// from, so an over-limit page and a stale record give the same advice.
const plusTenPercent = "its weight plus 10%, rounded up to the next 100"

// suggestedRecord is the record a failure for the page it reports
// should suggest: the weight it reports, plus 10%, rounded up to the
// next 100 bytes.
func suggestedRecord(t *testing.T, failure string) int {
	t.Helper()
	match := reportedWeight.FindStringSubmatch(failure)
	if match == nil {
		t.Fatalf("no weight in %q", failure)
	}
	total, err := strconv.Atoi(match[1])
	if err != nil {
		t.Fatal(err)
	}
	return (total*11/10 + 99) / 100 * 100
}

// reportedWeight is a failure's weight for its page, in bytes.
var reportedWeight = regexp.MustCompile(`: (\d+) bytes of`)

func TestAPageOverItsLimitSaysHowToRecordIt(t *testing.T) {
	server := site(map[string]string{"/orders": page(tokensHref)},
		map[string][]byte{tokensHref: incompressible(2000)})
	defer server.Close()
	recorder := checkOne(server, smallBudget)
	if len(recorder.errs) != 1 {
		t.Fatalf("got %q, want one failure", recorder.errs)
	}
	want := fmt.Sprintf("record `weight GET /orders %d <reason>` in "+
		".rastrillo/budgets.txt: %s",
		suggestedRecord(t, recorder.errs[0]), plusTenPercent)
	if !strings.Contains(recorder.errs[0], want) {
		t.Errorf("got %q, want a line containing %q", recorder.errs, want)
	}
}

func TestAStaleWeightRecordFails(t *testing.T) {
	cases := map[string]struct {
		record string
		size   int
		want   string
	}{
		"under the default": {"weight GET /orders 2500 why", 1000,
			"GET /orders weighs 10"},
		"under 60% of it": {"weight GET /orders 5000 why", 2000,
			"lower it to"},
		"plus 10%": {"weight GET /orders 5000 why", 2000,
			plusTenPercent},
	}
	for name, test := range cases {
		budget := recorded(t, smallBudget, test.record)
		server := site(map[string]string{"/orders": page(tokensHref)},
			map[string][]byte{tokensHref: incompressible(test.size)})
		recorder := checkOne(server, budget)
		server.Close()
		if len(recorder.errs) != 1 ||
			!strings.Contains(recorder.errs[0], test.want) ||
			!strings.Contains(recorder.errs[0], "line 1") {
			t.Errorf("%s: got %q, want a line containing %q and line 1",
				name, recorder.errs, test.want)
		}
	}
	if !strings.Contains(staleWeightUnderDefaultMsg, "delete") {
		t.Error("a record under the default does not say to delete it")
	}
}

func TestAWeightRecordForAScreenNotWeighedIsStale(t *testing.T) {
	file, err := budgetfile.Parse(strings.NewReader(
		"weight GET /orders 2500 why\nweight GET /gone 2500 why\n"))
	if err != nil {
		t.Fatal(err)
	}
	problems := staleWeights(file,
		[]target{{screen: "GET /orders", path: "/orders"}})
	want := ".rastrillo/budgets.txt line 2: weight record for GET /gone, " +
		"which is not a weighed screen: delete it"
	if !slices.Equal(problems, []string{want}) {
		t.Errorf("got %q, want %q", problems, want)
	}
}
