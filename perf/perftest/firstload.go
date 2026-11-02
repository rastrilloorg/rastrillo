package perftest

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"testing"
	"time"
)

// firstLoadBudget is what a page's framework CSS and JS may weigh,
// gzipped. shells is the limit for a page of each shell that loads no
// component script; references pins each vendored file's size, which
// is how a failure names the file that grew, and a component script's
// reference is also what it adds to a page's limit.
type firstLoadBudget struct {
	shells, references map[string]int
}

// frameworkBudget is measured from the shipped files, minified and
// gzipped at the default level, with each reference rounded up to the
// next 100 bytes so a compressor change of a few bytes is not a
// failure; theme.css's is the largest theme's. The sidebar, console
// and stage limits are their plain page's weight in its heaviest theme
// plus 10%, rounded up the same way (the page-weight spec, change 3).
var frameworkBudget = firstLoadBudget{
	shells: map[string]int{
		"column": 40000, "topbar": 40000,
		"sidebar": 37400, "console": 37400, "stage": 35700,
	},
	references: map[string]int{
		"tokens.css": 20100, "theme.css": 1800, "shell.css": 500,
		"rastrillo.js": 2400, "busy.js": 1200, "live.js": 4600,
		"light.js": 1900, "anchor.js": 600, "shell.js": 1100,
		"select.js": 7300, "datetime.js": 9500, "calendar.js": 2800,
	},
}

// FirstLoad holds every page the app's GET routes serve to its shell's
// first-load budget: the framework CSS and JS it links, as served,
// gzipped. It takes the inventory Screens does and honours the same
// budgets.txt skips. Unlike Screens it runs under -short and may be
// called more than once, since a weight does not depend on the
// machine.
func FirstLoad(t testing.TB, cfg ScreenConfig) {
	t.Helper()
	targets, problems := inventory(cfg, load(t))
	for _, problem := range problems {
		t.Errorf("%s", problem)
	}
	if len(problems) > 0 {
		t.FailNow()
	}
	client, server, _ := startServer(t, cfg)
	defer closeBounded(server)
	checkFirstLoad(t, client, server.URL, targets, frameworkBudget)
}

// checkFirstLoad weighs each target that answers its expected status
// with a 200 HTML page. A page that cannot be fetched, or answers
// another status, fails rather than going unweighed.
func checkFirstLoad(
	t testing.TB,
	client *http.Client,
	base string,
	targets []target,
	budget firstLoadBudget,
) {
	t.Helper()
	for _, target := range targets {
		pageURL := base + target.path
		response, page, err := get(client, pageURL, isPage)
		switch {
		case err != nil:
			t.Errorf("%s: %v", target.screen, err)
			continue
		case response.StatusCode != target.expect:
			t.Errorf("%s: "+wrongStatusMsg,
				target.screen, response.StatusCode, target.expect)
			continue
		case !isPage(response):
			continue
		}
		weights, err := frameworkWeights(following(client), pageURL, page)
		if err != nil {
			t.Errorf("%s %v", target.screen, err)
			continue
		}
		reportWeight(t, target.screen, shellOf(page), weights, budget)
	}
}

// requestBound caps each request FirstLoad makes, body included, so a
// page or a file that never finishes fails by name instead of hanging
// the lane.
var requestBound = 10 * time.Second

// get fetches address within requestBound and, when read says so, reads
// the whole body, which the client has already unzipped if the server
// sent it gzipped. A stream or any other non-page is never read: it may
// not end.
func get(
	client *http.Client, address string, read func(*http.Response) bool,
) (*http.Response, []byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), requestBound)
	defer cancel()
	request, err := http.NewRequestWithContext(
		ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, nil, err
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, nil, err
	}
	defer response.Body.Close()
	if !read(response) {
		return response, nil, nil
	}
	body, err := io.ReadAll(response.Body)
	return response, body, err
}

// always reads every body: a framework file must be whole to be weighed.
func always(*http.Response) bool { return true }

// isPage reports whether a response is a page a visitor's browser lays
// out: a redirect, an error page or JSON is not.
func isPage(response *http.Response) bool {
	mediaType, _, _ := mime.ParseMediaType(
		response.Header.Get("Content-Type"))
	return response.StatusCode == http.StatusOK && mediaType == "text/html"
}

// following is client, cookies and all, following redirects: a
// stylesheet link that redirects to its fingerprinted URL works in a
// browser, and is weighed where it lands.
func following(client *http.Client) *http.Client {
	copied := *client
	copied.CheckRedirect = nil
	return &copied
}

// fileWeight is one framework file a page links and its gzipped size.
type fileWeight struct {
	name string
	size int
}

// frameworkWeights fetches each framework file page links, resolved
// against the page's URL and weighed once however many links reach it,
// and gzips it. A link that does not answer 200 is an error naming it,
// never a file of 0 bytes.
func frameworkWeights(
	client *http.Client, pageURL string, page []byte,
) ([]fileWeight, error) {
	base, err := url.Parse(pageURL)
	if err != nil {
		return nil, err
	}
	if href := baseHref(page); href != "" {
		if declared, err := base.Parse(href); err == nil {
			base = declared
		}
	}
	var weights []fileWeight
	seen := map[string]bool{}
	for _, href := range linkedFiles(page) {
		link, err := base.Parse(href)
		if err != nil {
			continue
		}
		link.Fragment = ""
		address := link.String()
		name, ok := frameworkName(address)
		if !ok || seen[address] {
			continue
		}
		seen[address] = true
		response, body, err := get(client, address, always)
		if err != nil {
			return nil, fmt.Errorf(linkErrorMsg, href, err)
		}
		if response.StatusCode != http.StatusOK {
			return nil, fmt.Errorf(linkFailedMsg, href, response.StatusCode)
		}
		// A redirect to a sign-in page answers 200 with HTML, which the
		// browser cannot run as the file it asked for.
		landed := response.Request.URL.String()
		if _, ok := frameworkName(landed); !ok {
			return nil, fmt.Errorf(linkRedirectedMsg, href, landed)
		}
		weights = append(weights, fileWeight{name, gzippedSize(body)})
	}
	return weights, nil
}

// gzippedSize is body's size gzipped at the default level, the level a
// platform's edge compresses at, whatever level Assets sends.
func gzippedSize(body []byte) int {
	var compressed bytes.Buffer
	zipper := gzip.NewWriter(&compressed)
	zipper.Write(body)
	zipper.Close()
	return compressed.Len()
}
