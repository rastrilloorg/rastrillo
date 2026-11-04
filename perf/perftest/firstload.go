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
	"path"
	"slices"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/internal/budgetfile"
)

// firstLoadBudget is what a page's framework CSS and JS may weigh,
// gzipped. shells is the limit for a page of each shell that links a
// slice of tokens.css and loads no component script, live the same for
// a live page before live.js, and unsliced the limit for any page
// linking the whole file; references pins each
// vendored file's size, which is how a failure names the file that
// grew, and a component script's reference is also what it adds to a
// page's limit. records are the app's budgets.txt weight records, by
// screen, each a page's whole limit in place of these.
type firstLoadBudget struct {
	shells, live, references map[string]int
	unsliced                 int
	records                  map[string]budgetfile.Weight
}

// frameworkBudget is measured from the shipped files, minified and
// gzipped at the default level, with each reference rounded up to the
// next 100 bytes so a compressor change of a few bytes is not a
// failure; theme.css's is the largest theme's. A sliced page in any
// shell gets 16 kB (page-weight 2, change 6): the spec's "about 15 kB",
// raised so the framework's own screens pass with no record. A
// generated list screen links the rules of every branch of its rows
// and pagination, and loads busy.js, light.js and anchor.js for its
// search form, buttons and filter menu: 15,399 bytes. The stage
// shell's sign-in screen, its real first page, is 11,866. A live page
// loads live.js and every component script, charged by reference as on
// any page, and links the slice of every page's hooks. That slice grows
// with the app's own screens, not with the framework, and can never
// pass the whole tokens.css, so a live page's limit is the ceiling:
// every other file's reference, plus 10%, rounded up the same way. A
// limit measured on the framework's own few screens failed the first
// app with an admin area (the chat tour's room page, 2026-11-03).
var frameworkBudget = firstLoadBudget{
	shells: map[string]int{
		"column": 16000, "topbar": 16000,
		"sidebar": 16000, "console": 16000, "stage": 16000,
	},
	live: map[string]int{
		"column": 33000, "topbar": 33000,
		"sidebar": 33000, "console": 33000, "stage": 33000,
	},
	unsliced: 40000,
	references: map[string]int{
		"tokens.css": 20100, "theme.css": 1800, "shell.css": 500,
		"rastrillo.js": 2400, "busy.js": 1200, "live.js": 4700,
		"light.js": 1900, "anchor.js": 600, "shell.js": 1100,
		"transition.js": 400,
		"select.js":     7300, "datetime.js": 9500, "calendar.js": 2800,
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
	file := load(t)
	targets, problems := inventory(cfg, file)
	problems = append(problems, staleWeights(file, targets)...)
	for _, problem := range problems {
		t.Errorf("%s", problem)
	}
	if len(problems) > 0 {
		t.FailNow()
	}
	client, server, _ := startServer(t, cfg)
	defer closeBounded(server)
	checkFirstLoad(t, client, server.URL, targets,
		withRecords(frameworkBudget, file))
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
		weighPage(t, following(client), target.screen, pageURL, page, budget)
	}
}

// weighPage holds page to its limit, and its hooks to its slice.
func weighPage(
	t testing.TB,
	client *http.Client,
	screen, pageURL string,
	page []byte,
	budget firstLoadBudget,
) {
	t.Helper()
	weights, err := frameworkWeights(client, pageURL, page)
	if err != nil {
		t.Errorf("%s %v", screen, err)
		return
	}
	sheet, err := stylesheetOf(client, weights)
	if err != nil {
		t.Errorf("%s %v", screen, err)
		return
	}
	reportWeight(t, screen, shellOf(page), weights, sheet, budget)
	reportUnstyled(t, screen, page, sheet)
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

// fileWeight is one framework file a page links, where it was fetched
// from, its bytes and their gzipped size.
type fileWeight struct {
	name    string
	size    int
	address string
	body    []byte
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
		body, err := fetchFramework(client, address)
		if err != nil {
			return nil, fmt.Errorf(linkFailedMsg, href, err)
		}
		weights = append(weights,
			fileWeight{name, gzippedSize(body), address, body})
	}
	return weights, nil
}

// fetchFramework fetches the framework file at address. One that does
// not answer 200 is an error, never a file of 0 bytes, and so is a
// redirect to a sign-in page, which answers 200 with HTML the browser
// cannot use as the file it asked for.
func fetchFramework(client *http.Client, address string) ([]byte, error) {
	response, body, err := get(client, address, always)
	if err != nil {
		return nil, fmt.Errorf(notLoadedMsg, err)
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(statusMsg, response.StatusCode)
	}
	landed := response.Request.URL.String()
	if _, ok := frameworkName(landed); !ok {
		return nil, fmt.Errorf(redirectedMsg, landed)
	}
	return body, nil
}

// stylesheetName is the framework file a page may link a slice of.
const stylesheetName = "tokens.css"

// pageStylesheet is the tokens.css a page links: the whole file, and
// the slice the page links in its place, if it does. Both are nil for a
// page that links no tokens.css.
type pageStylesheet struct {
	whole, slice []byte
}

func (sheet pageStylesheet) sliced() bool { return sheet.slice != nil }

func (sheet pageStylesheet) unsliced() bool {
	return sheet.whole != nil && sheet.slice == nil
}

// stylesheetOf tells whether the tokens.css among a page's weights is
// a slice by its bytes: Assets answers the bare static/tokens.css
// beside it with the whole file, and serves a slice only under a
// fingerprint, which the whole file's own link also carries.
func stylesheetOf(
	client *http.Client, weights []fileWeight,
) (pageStylesheet, error) {
	for _, weight := range weights {
		if weight.name != stylesheetName {
			continue
		}
		bare, err := url.Parse(weight.address)
		if err != nil {
			return pageStylesheet{}, err
		}
		bare.Path = path.Join(path.Dir(bare.Path), stylesheetName)
		bare.RawPath, bare.RawQuery = "", ""
		whole, err := fetchFramework(client, bare.String())
		if err != nil {
			return pageStylesheet{},
				fmt.Errorf(wholeFailedMsg, weight.address, bare, err)
		}
		if bytes.Equal(whole, weight.body) {
			return pageStylesheet{whole: whole}, nil
		}
		return pageStylesheet{whole: whole, slice: weight.body}, nil
	}
	return pageStylesheet{}, nil
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

// withRecords is budget with the app's weight records from file.
func withRecords(
	budget firstLoadBudget, file *budgetfile.File,
) firstLoadBudget {
	budget.records = file.Weights
	return budget
}

// staleWeights is a problem for each weight record naming a screen that
// is not weighed: like a perf record, it cannot go stale quietly.
func staleWeights(file *budgetfile.File, targets []target) []string {
	weighed := map[string]bool{}
	for _, target := range targets {
		weighed[target.screen] = true
	}
	var problems []string
	for screen, record := range file.Weights {
		if !weighed[screen] {
			problems = append(problems, fmt.Sprintf(staleWeightScreenMsg,
				budgetfile.FileName, record.Line, screen))
		}
	}
	slices.Sort(problems)
	return problems
}
