package measure

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

type Options struct {
	Chrome     string // the Chromium binary
	Lighthouse string // the lighthouse binary; "" records it as not run
	ShotsDir   string
	AxeJS      []byte
}

type Role struct {
	Name     string
	SignedIn bool
}

type Page struct {
	Role, URL  string
	Failure    string // "" when measured
	Weight     Weight
	Axe        map[string]int // impact -> violated rules
	Lighthouse map[string]int // category -> 0..100; nil when not run
}

type Measurement struct {
	Pages    []Page
	Headers  map[string]string // the home page's protective headers
	DeployOK bool              // home answered and sign-in worked
	Failures []string          // notes on what could not be measured
}

const (
	pageBudget       = 90 * time.Second
	quietFor         = time.Second
	loadDeadline     = 30 * time.Second
	lighthouseBudget = 3 * time.Minute
)

// Measure loads each role's page in a browser of its own, so every
// page is a first visit: no cookie, cache, storage or service worker
// from the page before. (A browser context per page would be cheaper,
// but headless Chromium refuses to open a tab in one.) A page that
// cannot be measured is that page's failure and the rest still run; the
// error is for a browser that will not start, which is the harness's.
func Measure(
	ctx context.Context, options Options, mapping Mapping, roles []Role,
) (Measurement, error) {
	measurement := Measurement{}
	if _, err := os.Stat(options.Chrome); err != nil {
		return measurement, fmt.Errorf("browser: %w", err)
	}
	if options.Lighthouse == "" {
		measurement.Failures = append(measurement.Failures,
			"lighthouse: not installed, so not run")
	}

	homeOK, signInOK := false, true
	for _, role := range roles {
		page, headers, notes, err := measurePage(ctx, options, mapping, role)
		if err != nil {
			return measurement, err
		}
		measurement.Pages = append(measurement.Pages, page)
		measurement.Failures = append(measurement.Failures, notes...)
		if role.Name == "home" {
			measurement.Headers = headers
			homeOK = page.Failure == ""
		}
		if role.SignedIn && page.Failure != "" {
			signInOK = false
		}
	}
	measurement.DeployOK = homeOK && signInOK
	return measurement, nil
}

func measurePage(
	ctx context.Context, options Options, mapping Mapping, role Role,
) (Page, map[string]string, []string, error) {
	page := Page{Role: role.Name, URL: mapping.Pages[role.Name]}
	if page.URL == "" {
		page.Failure = "no URL in the mapping"
		return page, nil, nil, nil
	}
	allocatorOptions := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(options.Chrome), chromedp.WindowSize(1280, 900))
	allocator, cancelAllocator := chromedp.NewExecAllocator(ctx, allocatorOptions...)
	defer cancelAllocator()
	tab, cancelTab := chromedp.NewContext(allocator)
	defer cancelTab()
	if err := chromedp.Run(tab, network.Enable()); err != nil {
		return page, nil, nil, fmt.Errorf("browser: %w", err)
	}
	tab, cancelBudget := context.WithTimeout(tab, pageBudget)
	defer cancelBudget()

	signIn := mapping.SignInFor(role.Name)
	if role.SignedIn {
		if problem := signInWith(tab, signIn); problem != "" {
			page.Failure = problem
			return page, nil, nil, nil
		}
	}

	// Leave whatever sign-in landed on first: going from /admin to
	// /admin#overdue is a jump within the page, which loads nothing.
	if err := chromedp.Run(tab, chromedp.Navigate("about:blank")); err != nil {
		page.Failure = "load: " + err.Error()
		return page, nil, nil, nil
	}
	recorder := listen(tab)
	err := chromedp.Run(tab,
		network.ClearBrowserCache(), chromedp.Navigate(page.URL))
	if err != nil {
		page.Failure = "load: " + err.Error()
		return page, nil, nil, nil
	}
	settled := recorder.waitForQuiet()
	document := recorder.document()
	page.Weight = Tally(recorder.events())
	var notes []string
	if !settled {
		notes = append(notes, fmt.Sprintf(
			"%s: requests still open after %s; weight counts what finished",
			role.Name, loadDeadline))
	}

	if problem := documentProblem(page.URL, document); problem != "" {
		page.Failure = problem
		return page, nil, notes, nil
	}
	// A protected page that bounced to the sign-in form answers 200 on
	// the same host, and would otherwise be weighed as the real page.
	if role.SignedIn && stillSigningIn(tab, signIn) {
		page.Failure = "landed on the sign-in form, not the page"
		return page, nil, notes, nil
	}
	page.Axe, err = runAxe(tab, options.AxeJS)
	if err != nil {
		page.Failure = "axe: " + err.Error()
	}
	saveScreenshots(tab, options.ShotsDir, role.Name)
	if !role.SignedIn && options.Lighthouse != "" {
		var note string
		page.Lighthouse, note = runLighthouse(ctx, options, page.URL)
		if note != "" {
			notes = append(notes, role.Name+": "+note)
		}
	}
	return page, SecurityHeaders(document.headers), notes, nil
}

func signInWith(tab context.Context, details SignIn) string {
	if details.URL == "" || details.UsernameSelector == "" ||
		details.PasswordSelector == "" || details.SubmitSelector == "" {
		return "sign-in: the mapping gave no sign-in details"
	}
	err := chromedp.Run(tab,
		chromedp.Navigate(details.URL),
		chromedp.WaitVisible(details.UsernameSelector),
		chromedp.SendKeys(details.UsernameSelector, details.Username),
		chromedp.SendKeys(details.PasswordSelector, details.Password),
		chromedp.Click(details.SubmitSelector),
	)
	if err != nil {
		return "sign-in: " + err.Error()
	}
	// A rejected sign-in re-renders the sign-in page with its password
	// field. Either sign of change means it took: leaving the page (an
	// admin page may well have a password field of its own) or the
	// field going (some apps sign in without leaving the URL).
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(500 * time.Millisecond)
		if !stillSigningIn(tab, details) {
			return ""
		}
	}
	return "sign-in: still on the sign-in form after submitting"
}

// stillSigningIn is true while the tab is on the sign-in page's URL and
// its password field is still there.
func stillSigningIn(tab context.Context, details SignIn) bool {
	var location string
	if chromedp.Run(tab, chromedp.Location(&location)) != nil {
		return true
	}
	if !samePage(location, details.URL) {
		return false
	}
	// Visible, not merely present: a hash-routed app may hide its
	// sign-in form once signed in rather than remove it.
	var present bool
	err := chromedp.Run(tab, chromedp.Evaluate(fmt.Sprintf(
		`(f => !!f && f.getClientRects().length > 0)(document.querySelector(%q))`,
		details.PasswordSelector), &present))
	return err != nil || present
}

// samePage compares two URLs by host and path, ignoring a query or a
// fragment, which a sign-in page may add to itself.
func samePage(a, b string) bool {
	first, errA := url.Parse(a)
	second, errB := url.Parse(b)
	if errA != nil || errB != nil {
		return a == b
	}
	return first.Host == second.Host &&
		strings.TrimSuffix(first.Path, "/") == strings.TrimSuffix(second.Path, "/")
}

type document struct {
	url     string
	status  int
	headers http.Header
}

func documentProblem(asked string, final document) string {
	if final.url == "" {
		return "load: no document arrived"
	}
	askedURL, _ := url.Parse(asked)
	finalURL, err := url.Parse(final.url)
	if err != nil || finalURL.Hostname() != askedURL.Hostname() {
		return "redirected off-host to " + final.url
	}
	if askedURL.Scheme == "https" && finalURL.Scheme != "https" {
		return "ended on plain http: " + final.url
	}
	if final.status != http.StatusOK {
		return fmt.Sprintf("answered %d", final.status)
	}
	return ""
}

// recorder keeps what CDP reports about a page's requests. CDP calls
// back from its own goroutine, hence the lock.
type recorder struct {
	mutex        sync.Mutex
	byID         map[network.RequestID]*NetworkEvent
	open         map[network.RequestID]bool
	redirects    []NetworkEvent
	mainFrame    cdp.FrameID
	lastDocument document
	lastActivity time.Time
}

// streams never finish by design (a live page's event stream), so they
// do not hold the page open.
var streams = map[network.ResourceType]bool{
	network.ResourceTypeEventSource: true,
	network.ResourceTypeWebSocket:   true,
}

func listen(tab context.Context) *recorder {
	r := &recorder{
		byID:         map[network.RequestID]*NetworkEvent{},
		open:         map[network.RequestID]bool{},
		lastActivity: time.Now(),
	}
	chromedp.ListenTarget(tab, func(event any) {
		r.mutex.Lock()
		defer r.mutex.Unlock()
		switch e := event.(type) {
		case *network.EventRequestWillBeSent:
			r.lastActivity = time.Now()
			// A redirect's own response never reaches loadingFinished;
			// its bytes were still paid for.
			if e.RedirectResponse != nil {
				r.redirects = append(r.redirects, NetworkEvent{
					RequestID: string(e.RequestID), Type: "Document",
					Encoded: int(e.RedirectResponse.EncodedDataLength),
				})
			}
			if e.Type == network.ResourceTypeDocument && r.mainFrame == "" {
				r.mainFrame = e.FrameID
			}
			if !streams[e.Type] {
				r.open[e.RequestID] = true
			}
		case *network.EventResponseReceived:
			r.lastActivity = time.Now()
			event := r.event(e.RequestID)
			event.Type = string(e.Type)
			if parsed, err := url.Parse(e.Response.URL); err == nil {
				event.Host = parsed.Hostname()
			}
			// Only the top frame's document is the page: an iframe's
			// would otherwise lend the page its status and headers.
			if e.Type == network.ResourceTypeDocument && e.FrameID == r.mainFrame {
				r.lastDocument = document{
					url:     e.Response.URL,
					status:  int(e.Response.Status),
					headers: toHeader(e.Response.Headers),
				}
			}
		case *network.EventDataReceived:
			r.lastActivity = time.Now()
			r.event(e.RequestID).Decoded += int(e.DataLength)
		case *network.EventLoadingFinished:
			r.lastActivity = time.Now()
			r.event(e.RequestID).Encoded = int(e.EncodedDataLength)
			delete(r.open, e.RequestID)
		case *network.EventLoadingFailed:
			r.lastActivity = time.Now()
			delete(r.open, e.RequestID)
		}
	})
	return r
}

func (r *recorder) event(id network.RequestID) *NetworkEvent {
	event, known := r.byID[id]
	if !known {
		event = &NetworkEvent{RequestID: string(id)}
		r.byID[id] = event
	}
	return event
}

// waitForQuiet returns true once no request is open and the network has
// been idle for a second, or false at the deadline: the load event
// alone misses scripts that load more.
func (r *recorder) waitForQuiet() bool {
	deadline := time.Now().Add(loadDeadline)
	for time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
		r.mutex.Lock()
		quiet := len(r.open) == 0 && time.Since(r.lastActivity) >= quietFor
		r.mutex.Unlock()
		if quiet {
			return true
		}
	}
	return false
}

func (r *recorder) events() []NetworkEvent {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	events := append([]NetworkEvent(nil), r.redirects...)
	for _, event := range r.byID {
		events = append(events, *event)
	}
	return events
}

func (r *recorder) document() document {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	return r.lastDocument
}

func toHeader(headers network.Headers) http.Header {
	converted := http.Header{}
	for name, value := range headers {
		converted.Set(name, fmt.Sprint(value))
	}
	return converted
}

func runAxe(tab context.Context, axeJS []byte) (map[string]int, error) {
	var results string
	err := chromedp.Run(tab,
		chromedp.Evaluate(string(axeJS), nil),
		chromedp.Evaluate(`axe.run(document).then(r => JSON.stringify({
			violations: r.violations.map(v => ({id: v.id, impact: v.impact}))
		}))`, &results, func(p *runtime.EvaluateParams) *runtime.EvaluateParams {
			return p.WithAwaitPromise(true)
		}),
	)
	if err != nil {
		return nil, err
	}
	return AxeCounts([]byte(results))
}

func saveScreenshots(tab context.Context, dir, role string) {
	if dir == "" || os.MkdirAll(dir, 0o755) != nil {
		return
	}
	for _, width := range []int64{1280, 390} {
		var shot []byte
		err := chromedp.Run(tab,
			chromedp.EmulateViewport(width, 900),
			chromedp.FullScreenshot(&shot, 100))
		if err == nil {
			name := fmt.Sprintf("%s-%d.png", role, width)
			os.WriteFile(filepath.Join(dir, name), shot, 0o644)
		}
	}
}

// runLighthouse returns the scores, or a note saying why there are
// none: a missing score must be explained, not silently absent.
func runLighthouse(
	ctx context.Context, options Options, address string,
) (map[string]int, string) {
	ctx, cancel := context.WithTimeout(ctx, lighthouseBudget)
	defer cancel()
	command := exec.CommandContext(ctx, options.Lighthouse, address,
		"--output=json", "--quiet",
		"--only-categories=performance,accessibility,best-practices,seo",
		"--chrome-flags=--headless=new")
	command.Env = append(os.Environ(), "CHROME_PATH="+options.Chrome)
	report, err := command.Output()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil, fmt.Sprintf("lighthouse: no result in %s", lighthouseBudget)
	}
	if err != nil {
		return nil, "lighthouse: " + err.Error()
	}
	scores, err := LighthouseScores(report)
	if err != nil {
		return nil, "lighthouse: unreadable report: " + err.Error()
	}
	return scores, ""
}
