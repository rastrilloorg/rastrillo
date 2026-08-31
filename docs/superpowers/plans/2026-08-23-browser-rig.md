# Browser Rig Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Land the `harness/` package — a real Chromium with a CDP virtual authenticator driving a rastrillo app on a trustworthy localhost origin, loud on every console error, failed request and junk value — plus the scaffolded `browser_test.go`, the `prfByAssertion` coverage in `webauthn/`, and a CI browser job, exactly as the spec lays out.

**Architecture:** `harness.New` binds a localhost listener first, hands the app its origin (`http://localhost:PORT` — an IP is not a WebAuthn RP ID), serves the handler `build` returns, then launches headless Chromium via chromedp with a CTAP2.1 virtual authenticator (`hasPrf`, `automaticPresenceSimulation` explicitly true). CDP watchers accumulate problems (console errors, exceptions, failed requests, responses ≥ 400 and their log-domain mirrors) that `Screen(selector, note)` flushes after a junk scan rooted at the screen's own selector. Everything but an untagged `doc.go` carries `//go:build browser`, so chromedp stays out of the ordinary build graph — the README invariant, verified by `go list -deps ./...` in CI.

**Tech Stack:** Go 1.24 stdlib (`net`, `net/http/httptest`, `testing`), `github.com/chromedp/chromedp` v0.14.2 + `github.com/chromedp/cdproto` (pinned, already in go.mod: `webauthn`, `log`, `network`, `page`, `runtime` domains), the in-repo `webauthn` package and its `js/webauthn.mjs`, GitHub Actions with a Playwright-installed pinned Chromium. No new dependencies, no go.mod changes.

**Spec:** docs/superpowers/specs/2026-08-23-harness-design.md

## Global Constraints

- Every go command runs with `export GOFLAGS=-mod=mod CGO_ENABLED=0`.
- gofmt-clean, `go vet ./...` clean, `go test ./...` green after every task — AND `go test -tags browser ./...` green where a browser is available (browser-tagged steps note `RASTRILLO_BROWSER_OPTIONAL` for machines without one; check whether THIS machine has a usable Chromium via the ui/browser_test.go discovery paths and say in the plan which invocation the implementer should run).
- `go list -deps ./...` must never pull chromedp (the README invariant) — add an explicit verification step after the package lands.
- Do not touch SKILL.md (hard byte budget 14,986/15,000).
- Doc comments in the house voice (read ui/browser_test.go's comments for the register).

**This machine's browser (checked while writing this plan):** no `chromium`/`google-chrome` on PATH and no `RASTRILLO_CHROME`, but the Playwright-cache glob that `chromePath` already uses hits `~/.cache/ms-playwright/chromium-1234/chrome-linux64/chrome` (and `chromium-1228`; discovery takes the last hit). So on THIS machine the implementer runs browser-tagged tests plainly, **without** `RASTRILLO_BROWSER_OPTIONAL`:

```sh
export GOFLAGS=-mod=mod CGO_ENABLED=0
go test -tags browser ./harness/ ./ui/ ./webauthn/ -count=1
```

Also run `go vet -tags browser ./...` after every task — it type-checks the tagged files and needs no browser.

All paths below are relative to the worktree root `/home/paulca/.herdr/worktrees/rastrillo/browser-rig`.

---

### Task 1: harness package core — untagged doc, discovery, and the rig boot

The origin chicken-and-egg, resolved in code: listener first, origin computed, `build(origin)` called, then Chromium with the virtual authenticator. Cleanup order is load-bearing (`t.Cleanup` LIFO: server registered first, browser second, so the browser dies before the server it talks to). `AutomaticPresenceSimulation` is set explicitly true — the CDP default is true, but the Go zero value silently sends false and every ceremony hangs.

**Files**
- Create: `harness/doc.go` (untagged — the only untagged file in the package)
- Create: `harness/chrome.go` (`//go:build browser`)
- Create: `harness/rig.go` (`//go:build browser`)
- Test: `harness/rig_test.go` (`//go:build browser`, internal `package harness`)

**Interfaces**
- Consumes: `chromedp.NewExecAllocator`, `chromedp.NewContext`, `chromedp.Cancel`, `webauthn.Enable()`, `webauthn.AddVirtualAuthenticator(*webauthn.VirtualAuthenticatorOptions)` (cdproto), `httptest.NewUnstartedServer`, `net.Listen`.
- Produces:
  - `func ChromePath(t *testing.T) string`
  - `type Rig struct { Origin string; /* unexported: t, ctx, mu, problems, allows, allowedText, requests */ }`
  - `type Option func(*config)`
  - `func New(t *testing.T, build func(origin string) http.Handler, opts ...Option) *Rig`
  - `func (r *Rig) Context() context.Context`
  - `func (r *Rig) Run(actions ...chromedp.Action)`

**Steps**

- [ ] Write the failing test `harness/rig_test.go`:

```go
//go:build browser

package harness

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// The origin chicken-and-egg, resolved: build gets the localhost
// origin before the server exists, the page renders it, and the
// browser reads it back off the page it navigated — one loop through
// listener, handler and Chromium proving the boot order.
func TestNewHandsBuildTheOriginBeforeServing(t *testing.T) {
	var gotOrigin string
	r := New(t, func(origin string) http.Handler {
		gotOrigin = origin
		mux := http.NewServeMux()
		mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprintf(w, `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>rig</title></head><body><p id="origin">%s</p></body></html>`, origin)
		})
		return mux
	})
	if !strings.HasPrefix(r.Origin, "http://localhost:") {
		t.Fatalf("rig origin %q is not a localhost origin — an IP is not a WebAuthn RP ID", r.Origin)
	}
	if gotOrigin != r.Origin {
		t.Fatalf("build was handed %q, the rig navigates %q", gotOrigin, r.Origin)
	}
	var onPage string
	r.Run(
		chromedp.Navigate(r.Origin+"/"),
		chromedp.WaitVisible("#origin", chromedp.ByQuery),
		chromedp.Text("#origin", &onPage, chromedp.ByQuery),
	)
	if onPage != r.Origin {
		t.Fatalf("page shows %q, want %q", onPage, r.Origin)
	}
}
```

- [ ] Verify it fails: `go vet -tags browser ./harness/` — undefined: `New`, `Rig` (a compile error is this step's red).
- [ ] Create `harness/doc.go`:

```go
// Package harness drives a rastrillo app in a real Chromium with a CDP
// virtual authenticator attached — the browser rig behind
// `go test -tags browser ./...`.
//
// Every other file in this package is build-tagged `browser`. The
// README promises, twice, that chromedp stays out of the ordinary
// build graph (`go list -deps ./...` pulls none of it), and an
// untagged package importing chromedp would make that sentence false
// the day it landed. This doc file is what keeps a plain
// `go build ./...` and `go vet ./...` seeing a valid package; the rig
// itself exists only under the tag.
//
// The shape (spec: docs/superpowers/specs/2026-08-23-harness-design.md):
// New binds a localhost listener first — the origin chicken-and-egg a
// passkey app forces — hands the app its origin, then launches
// Chromium with a virtual authenticator; watchers make every console
// error, failed request and 4xx/5xx a test failure; Screen gates each
// screen behind the junk scan, the bug class that renders perfectly
// and says nothing.
package harness
```

- [ ] Create `harness/chrome.go` (discovery moved verbatim from ui/browser_test.go — behavior identical):

```go
//go:build browser

package harness

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// ChromePath finds the Chromium a browser-tagged test drives:
// RASTRILLO_CHROME first, then the usual PATH names, then the
// Playwright cache. Moved here verbatim from ui/browser_test.go so
// every browser test shares one discovery story.
//
// A skip is not a pass: with no browser this fails, unless
// RASTRILLO_BROWSER_OPTIONAL is set, which makes the skip a deliberate
// visible choice rather than an accident.
func ChromePath(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("RASTRILLO_CHROME"); p != "" {
		return p
	}
	for _, name := range []string{"chromium", "chromium-browser", "google-chrome", "google-chrome-stable"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		hits, _ := filepath.Glob(filepath.Join(home, ".cache", "ms-playwright", "chromium-*", "chrome-linux64", "chrome"))
		if len(hits) > 0 {
			return hits[len(hits)-1]
		}
	}
	if os.Getenv("RASTRILLO_BROWSER_OPTIONAL") != "" {
		t.Skip("no chromium found, RASTRILLO_BROWSER_OPTIONAL set — SKIPPED, not passed")
	}
	t.Fatal("no chromium found: set RASTRILLO_CHROME, or RASTRILLO_BROWSER_OPTIONAL to skip deliberately")
	return ""
}
```

- [ ] Create `harness/rig.go`:

```go
//go:build browser

package harness

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/webauthn"
	"github.com/chromedp/chromedp"
)

// Rig is one app under one browser: a real server on a localhost
// origin, a real Chromium, a CDP virtual authenticator, and the
// watchers that make a silent failure impossible.
type Rig struct {
	// Origin is the app's trustworthy origin, "http://localhost:PORT".
	// Always navigate this, never a 127.0.0.1 URL: an IP is not a
	// WebAuthn RP ID. Passkey apps set RPID "localhost" in test wiring.
	Origin string

	t   *testing.T
	ctx context.Context

	mu          sync.Mutex
	problems    []string
	allows      []allowance
	allowedText []string
	requests    map[network.RequestID]requestInfo
}

// requestInfo is what a later network event cannot tell us about
// itself: a response and a load failure carry only a RequestID, so the
// method and URL come from correlating network.EventRequestWillBeSent.
type requestInfo struct{ method, url string }

type config struct {
	withoutPRFAtCreation bool
}

// Option adjusts what New builds.
type Option func(*config)

// New boots the rig. Order is the point: a passkey app needs its
// origin before building its handler (csrf, webauthn config), but the
// port doesn't exist until a listener does — so New binds the
// listener first, computes origin, calls build(origin), serves on that
// listener, and only then launches Chromium with the virtual
// authenticator attached (kass's proven options, verbatim).
//
// Cleanups are registered so t.Cleanup's LIFO tears the browser down
// before the server it is talking to.
func New(t *testing.T, build func(origin string) http.Handler, opts ...Option) *Rig {
	t.Helper()
	var cfg config
	for _, o := range opts {
		o(&cfg)
	}
	chrome := ChromePath(t)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("harness: listen: %v", err)
	}
	origin := fmt.Sprintf("http://localhost:%d", ln.Addr().(*net.TCPAddr).Port)

	srv := httptest.NewUnstartedServer(build(origin))
	srv.Listener.Close()
	srv.Listener = ln
	srv.Start()
	t.Cleanup(srv.Close)

	allocOpts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(chrome),
		chromedp.Flag("headless", true),
		chromedp.NoSandbox,
	)
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(), allocOpts...)
	ctx, cancelCtx := chromedp.NewContext(allocCtx)
	t.Cleanup(func() {
		// Graceful browser shutdown first, then the contexts.
		_ = chromedp.Cancel(ctx)
		cancelCtx()
		cancelAlloc()
	})

	r := &Rig{
		Origin:   origin,
		t:        t,
		ctx:      ctx,
		requests: map[network.RequestID]requestInfo{},
	}

	var boot []chromedp.Action
	boot = append(boot, chromedp.ActionFunc(func(ctx context.Context) error {
		if err := webauthn.Enable().Do(ctx); err != nil {
			return err
		}
		_, err := webauthn.AddVirtualAuthenticator(&webauthn.VirtualAuthenticatorOptions{
			Protocol:            webauthn.AuthenticatorProtocolCtap2,
			Ctap2version:        webauthn.Ctap2versionCtap21,
			Transport:           webauthn.AuthenticatorTransportInternal,
			HasResidentKey:      true,
			HasUserVerification: true,
			HasPrf:              true,
			IsUserVerified:      true,
			// The CDP default is true — but the Go zero value would
			// silently send false, and every ceremony would hang
			// waiting for a presence that never comes. Explicit,
			// always: the one trap in an otherwise clean cdproto
			// mapping.
			AutomaticPresenceSimulation: true,
		}).Do(ctx)
		return err
	}))
	if err := chromedp.Run(r.ctx, boot...); err != nil {
		t.Fatalf("harness: browser boot: %v", err)
	}
	return r
}

// Context is the rig's chromedp context — derive a deadline from it
// when a drive wants a tighter budget than the test binary's own
// timeout (ui/browser_test.go does exactly that).
func (r *Rig) Context() context.Context { return r.ctx }

// Run drives the browser: plain chromedp actions, no DSL — kass's
// drive is a top-to-bottom script and that is the right amount of
// framework. On failure it fails the test with whatever was on
// screen: a failure report always includes what a person would have
// seen.
func (r *Rig) Run(actions ...chromedp.Action) {
	r.t.Helper()
	if err := chromedp.Run(r.ctx, actions...); err != nil {
		r.t.Fatalf("harness: drive failed: %v\non screen:\n%s", err, r.onScreen("body"))
	}
}

// onScreen reads sel's innerText (falling back to the whole body)
// best-effort — it feeds failure reports, so its own errors are
// swallowed rather than masking the real one.
func (r *Rig) onScreen(sel string) string {
	var text string
	expr := fmt.Sprintf(`((document.querySelector(%q) ?? document.body)?.innerText ?? "")`, sel)
	_ = chromedp.Run(r.ctx, chromedp.Evaluate(expr, &text))
	return text
}
```

- [ ] Verify pass: `gofmt -l .` (empty), `go vet ./...`, `go test ./...` (untagged — proves doc.go carries the package), `go vet -tags browser ./...`, `go test -tags browser ./harness/ -count=1 -run TestNewHandsBuildTheOriginBeforeServing`.
- [ ] Verify the README invariant already holds with the package landed: `go list -deps ./... | grep -ci chromedp` prints `0`.
- [ ] Commit:

```
harness: the rig boots — localhost listener first, then the app, then Chromium

The origin chicken-and-egg a passkey app forces, resolved in the boot
order: New binds the listener, hands build its http://localhost:PORT
origin, serves, and only then launches the browser with the virtual
authenticator attached — AutomaticPresenceSimulation explicitly true,
because the Go zero value silently sends false and every ceremony
hangs. Untagged doc.go keeps chromedp out of the ordinary build graph.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
```

---

### Task 2: loud-failure watchers, Allow, and the pre-allowed favicon

Kass's `watch`, ported to `ListenTarget`, honoring the three chromedp-specific facts: the console-error mirror of an HTTP failure arrives via `log.EventEntryAdded` (not `runtime.EventConsoleAPICalled`) and `Allow` filters it by URL; a response's method comes from correlating `network.EventRequestWillBeSent` by RequestID; and the browser's own `/favicon.ico` probe is pre-allowed. `Screen` lands here as wait-then-flush; Task 3 adds the junk scan between.

**Files**
- Create: `harness/watch.go` (`//go:build browser`)
- Create: `harness/screen.go` (`//go:build browser`)
- Modify: `harness/rig.go` (wire `r.watch()` + favicon pre-allow into `New`)
- Test: `harness/watch_test.go` (`//go:build browser`, internal `package harness`)

**Interfaces**
- Consumes: `chromedp.ListenTarget`, cdproto events `*runtime.EventConsoleAPICalled`, `*runtime.EventExceptionThrown`, `*log.EventEntryAdded`, `*network.EventRequestWillBeSent`, `*network.EventResponseReceived`, `*network.EventLoadingFailed`.
- Produces:
  - `func (r *Rig) Allow(method, path string, status int)`
  - `func (r *Rig) Screen(selector, note string)` (wait + flush; junk scan added in Task 3)
  - internal: `func (r *Rig) watch()`, `func (r *Rig) add(problem string)`, `func (r *Rig) take() []string`, `func (r *Rig) fail(selector, note, msg string)`

**Steps**

- [ ] Write the failing test `harness/watch_test.go`:

```go
//go:build browser

package harness

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// probeBuild serves a page that walks into every watcher on purpose:
// optionally a console error, then a 500 the page asked for with POST
// (so the method-correlation is provable), then a plain 404 — and
// reports #done only after the fetches settle, which is what the test
// synchronises on.
func probeBuild(withConsoleError bool) func(string) http.Handler {
	return func(string) http.Handler {
		mux := http.NewServeMux()
		mux.HandleFunc("POST /boom", func(w http.ResponseWriter, req *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		})
		mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, req *http.Request) {
			noise := ""
			if withConsoleError {
				noise = `console.error("kaboom");`
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprintf(w, `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>probe</title></head><body><p id="up">up</p><script>
(async () => {
  %s
  await fetch("/boom", { method: "POST" });
  await fetch("/missing");
  const done = document.createElement("p");
  done.id = "done";
  done.textContent = "done";
  document.body.append(done);
})();
</script></body></html>`, noise)
		})
		return mux
	}
}

// waitForProblems polls until at least n problems accumulated: CDP
// event delivery is asynchronous, and a fixed sleep is either flaky or
// slow. Returns whatever accumulated by the deadline either way — the
// caller's assertions name what is missing.
func waitForProblems(t *testing.T, r *Rig, n int) []string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		r.mu.Lock()
		got := len(r.problems)
		r.mu.Unlock()
		if got >= n || time.Now().After(deadline) {
			return r.take()
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestWatchersCollectEveryLoudFailure(t *testing.T) {
	r := New(t, probeBuild(true))
	r.Run(
		chromedp.Navigate(r.Origin+"/"),
		chromedp.WaitVisible("#done", chromedp.ByQuery),
	)
	probs := waitForProblems(t, r, 3)
	joined := strings.Join(probs, "\n")
	for _, want := range []string{
		"console.error: kaboom",
		// The method rides in from requestWillBeSent, correlated by
		// RequestID — a response has no method of its own.
		"HTTP 500 POST",
		"/boom",
		"HTTP 404 GET",
		"/missing",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("problems are missing %q:\n%s", want, joined)
		}
	}
}

// Allow quietens the response AND the log-domain mirror Chromium emits
// alongside it — the mirror carries only a URL, so it is matched by
// path. With every probe allowed, the drive must end silent.
func TestAllowQuietensExpectedProbesAndTheirMirrors(t *testing.T) {
	r := New(t, probeBuild(false))
	r.Allow(http.MethodPost, "/boom", http.StatusInternalServerError)
	r.Allow(http.MethodGet, "/missing", http.StatusNotFound)
	r.Run(
		chromedp.Navigate(r.Origin+"/"),
		chromedp.WaitVisible("#done", chromedp.ByQuery),
	)
	// The fetches settled before #done appeared, but give straggler
	// events a beat before declaring silence.
	time.Sleep(500 * time.Millisecond)
	if probs := r.take(); len(probs) > 0 {
		t.Errorf("allowed probes still reported problems:\n%s", strings.Join(probs, "\n"))
	}
}

// The browser probes /favicon.ico on its own; New pre-allows the 404
// so every app doesn't rediscover it. The allowance is data, so this
// needs no favicon request to prove the wiring.
func TestFaviconProbeIsPreAllowed(t *testing.T) {
	r := New(t, probeBuild(false))
	if !r.responseAllowed(http.MethodGet, r.Origin+"/favicon.ico", http.StatusNotFound) {
		t.Error("GET /favicon.ico 404 is not pre-allowed")
	}
	if !r.logEntryAllowed(r.Origin + "/favicon.ico") {
		t.Error("the favicon 404's log-domain mirror is not pre-allowed")
	}
}
```

- [ ] Verify it fails: `go vet -tags browser ./harness/` — undefined: `Allow`, `take`, `responseAllowed`, `logEntryAllowed` (compile error is the red).
- [ ] Create `harness/watch.go`:

```go
//go:build browser

package harness

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	cdplog "github.com/chromedp/cdproto/log"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// allowance is one expected probe: a request the app makes on purpose
// that would otherwise read as a failure. Kass's precedent, and its
// only one: the signed-out boot asks /api/me, is told 401, and that is
// how the app finds out to show the sign-in screen.
type allowance struct {
	method string
	path   string
	status int
}

// Allow registers an expected probe: responses with this method, path
// and status stop being problems — and so do the console-error mirrors
// Chromium logs for them, which arrive via the CDP log domain carrying
// only a URL and so are matched by path alone.
func (r *Rig) Allow(method, path string, status int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.allows = append(r.allows, allowance{method: method, path: path, status: status})
}

func (r *Rig) add(problem string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.problems = append(r.problems, problem)
}

// take drains the accumulated problems — Screen's flush.
func (r *Rig) take() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.problems
	r.problems = nil
	return out
}

func pathOf(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	return u.Path
}

func (r *Rig) responseAllowed(method, rawURL string, status int) bool {
	p := pathOf(rawURL)
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, a := range r.allows {
		if a.method == method && a.path == p && a.status == status {
			return true
		}
	}
	return false
}

// logEntryAllowed matches a CDP log entry by URL alone: the log
// domain's mirror of an HTTP failure carries no method or status, so
// an allowed path excuses its mirror too.
func (r *Rig) logEntryAllowed(rawURL string) bool {
	p := pathOf(rawURL)
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, a := range r.allows {
		if a.path == p {
			return true
		}
	}
	return false
}

func (r *Rig) requestInfoFor(id network.RequestID) requestInfo {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.requests[id]
}

// watch wires the loud-failure listeners: console error/assert, thrown
// exceptions, failed requests, responses >= 400 — all accumulate into
// the problem list Screen flushes. Three chromedp-specific facts this
// port honors: a 4xx/5xx response also surfaces as a console-error
// MIRROR, which in CDP arrives via log.EventEntryAdded, not
// runtime.EventConsoleAPICalled; a response has no method of its own,
// so it is correlated from network.EventRequestWillBeSent by
// RequestID; and the browser probes /favicon.ico by itself, which New
// pre-allows. chromedp enables the log and network domains on target
// attach, so there is nothing to switch on here.
func (r *Rig) watch() {
	chromedp.ListenTarget(r.ctx, func(ev any) {
		switch e := ev.(type) {
		case *runtime.EventConsoleAPICalled:
			if e.Type == "error" || e.Type == "assert" {
				var parts []string
				for _, a := range e.Args {
					parts = append(parts, string(a.Value))
				}
				r.add("console." + string(e.Type) + ": " + strings.Join(parts, " "))
			}
		case *runtime.EventExceptionThrown:
			r.add("uncaught: " + e.ExceptionDetails.Error())
		case *cdplog.EventEntryAdded:
			if e.Entry.Level != cdplog.LevelError {
				return
			}
			if r.logEntryAllowed(e.Entry.URL) {
				return
			}
			r.add(fmt.Sprintf("log.error: %s (%s)", e.Entry.Text, e.Entry.URL))
		case *network.EventRequestWillBeSent:
			r.mu.Lock()
			r.requests[e.RequestID] = requestInfo{method: e.Request.Method, url: e.Request.URL}
			r.mu.Unlock()
		case *network.EventResponseReceived:
			status := int(e.Response.Status)
			if status < http.StatusBadRequest {
				return
			}
			method := r.requestInfoFor(e.RequestID).method
			if r.responseAllowed(method, e.Response.URL, status) {
				return
			}
			r.add(fmt.Sprintf("HTTP %d %s %s", status, method, e.Response.URL))
		case *network.EventLoadingFailed:
			if e.Canceled {
				// Navigating away cancels in-flight loads; routine,
				// not a failure.
				return
			}
			req := r.requestInfoFor(e.RequestID)
			r.add(fmt.Sprintf("request failed: %s %s — %s", req.method, req.url, e.ErrorText))
		}
	})
}
```

- [ ] Create `harness/screen.go` (the gate's wait-and-flush half; Task 3 inserts the junk scan):

```go
//go:build browser

package harness

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
)

// screenBudget bounds the wait for a screen to arrive. Wall-clock
// against a real browser: generous enough for a loaded CI box, and far
// faster than Go's default test timeout — a missing screen fails as
// itself, not as a hung suite (ui/browser_test.go's 60s reasoning,
// kept).
const screenBudget = 60 * time.Second

// Screen is the gate a drive passes at every screen boundary: wait for
// selector, then flush the problem list — any accumulated problem
// fails the test naming the screen it surfaced on. "body" is the
// whole-page case for rastrillo's server-rendered apps, which have no
// #app convention to hard-fail on.
func (r *Rig) Screen(selector, note string) {
	r.t.Helper()
	ctx, cancel := context.WithTimeout(r.ctx, screenBudget)
	defer cancel()
	if err := chromedp.Run(ctx, chromedp.WaitVisible(selector, chromedp.ByQuery)); err != nil {
		r.fail(selector, note, fmt.Sprintf("screen %q never arrived: %v", selector, err))
	}
	if probs := r.take(); len(probs) > 0 {
		r.fail(selector, note, "the page reported problems:\n  "+strings.Join(probs, "\n  "))
	}
}

// fail fails the test with what was on screen: a failure report always
// includes what a person would have seen.
func (r *Rig) fail(selector, note, msg string) {
	r.t.Helper()
	r.t.Fatalf("harness: %s: %s\non screen:\n%s", note, msg, r.onScreen(selector))
}
```

- [ ] Edit `harness/rig.go`: insert the watcher wiring in `New`, immediately after the `r := &Rig{...}` literal and before `var boot []chromedp.Action`:

```go
	// The browser probes /favicon.ico on its own; pre-allowed so every
	// app doesn't rediscover it as a mysterious 404.
	r.Allow(http.MethodGet, "/favicon.ico", http.StatusNotFound)
	r.watch()
```

- [ ] Verify pass: `gofmt -l .` (empty), `go vet ./...`, `go test ./...`, `go vet -tags browser ./...`, `go test -tags browser ./harness/ -count=1`.
- [ ] Commit:

```
harness: loud-failure watchers, Allow, and the pre-allowed favicon

Kass's watch ported to ListenTarget, honoring the three chromedp facts
the spec calls out: the console mirror of an HTTP failure arrives via
log.EventEntryAdded and Allow filters it by URL; a response's method
comes from correlating requestWillBeSent by RequestID; the browser's
own favicon probe is excused once, centrally. Screen lands as
wait-then-flush — the junk scan slots in next.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
```

---

### Task 3: the junk scan, AllowText, and what was on screen

The scan reads the screen the way a person would, plus the places a person cannot see: the root's `textContent`, every `input`/`textarea` value, every `[aria-label]` — for `"undefined"`, `"null"`, `"[object Object]"`, `"NaN"` (kass's full set). Each hit shows its surrounding text, because substring `"null"` can false-positive on legitimate prose — `AllowText` exists for the rare deliberate case.

**Files**
- Create: `harness/junk.go` (`//go:build browser`)
- Modify: `harness/screen.go` (insert the scan between wait and flush)
- Test: `harness/junk_test.go` (`//go:build browser`, internal `package harness`)

**Interfaces**
- Consumes: `chromedp.Evaluate`, `encoding/json` (marshalling scan arguments into the JS call).
- Produces:
  - `func (r *Rig) AllowText(s string)`
  - internal: `var junkValues []string`, `const junkScanJS string`, `func (r *Rig) junkHits(selector string) []string`
  - `Screen` now: wait → junk scan → flush.

**Steps**

- [ ] Write the failing test `harness/junk_test.go`:

```go
//go:build browser

package harness

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

func staticPage(body string) func(string) http.Handler {
	return func(string) http.Handler {
		mux := http.NewServeMux()
		mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprintf(w, `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>junk</title></head><body>%s</body></html>`, body)
		})
		return mux
	}
}

// The three places junk hides: rendered text, an input's value, and
// the label a screen reader announces — each hit shown with its
// surroundings, so a prose false-positive is legible at a glance.
func TestJunkScanReadsTextInputsAndARIA(t *testing.T) {
	r := New(t, staticPage(`<main id="app">
<p>the price is undefined today</p>
<input value="[object Object]">
<button aria-label="null">x</button>
</main>`))
	r.Run(chromedp.Navigate(r.Origin+"/"), chromedp.WaitVisible("#app", chromedp.ByQuery))
	hits := r.junkHits("#app")
	joined := strings.Join(hits, "\n")
	for _, want := range []string{
		`undefined in: "the price is undefined today`,
		"[object Object]",
		"null",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("scan missed %q:\n%s", want, joined)
		}
	}
	if len(hits) != 3 {
		t.Errorf("want 3 hits, got %d:\n%s", len(hits), joined)
	}
}

// AllowText spares deliberate prose; the junk value stays banned
// everywhere else on the screen.
func TestAllowTextSparesDeliberateProse(t *testing.T) {
	r := New(t, staticPage(`<main id="app"><p>this contract is null and void</p></main>`))
	r.AllowText("null and void")
	r.Run(chromedp.Navigate(r.Origin+"/"), chromedp.WaitVisible("#app", chromedp.ByQuery))
	if hits := r.junkHits("#app"); len(hits) > 0 {
		t.Errorf("allowed prose still hit: %v", hits)
	}
	r.Screen("#app", "the prose screen") // and the full gate passes too
}

// A missing scan root is itself a finding, never a silent pass.
func TestJunkScanReportsAMissingRoot(t *testing.T) {
	r := New(t, staticPage(`<p id="up">no app root here</p>`))
	r.Run(chromedp.Navigate(r.Origin+"/"), chromedp.WaitVisible("#up", chromedp.ByQuery))
	hits := r.junkHits("#app")
	if len(hits) != 1 || !strings.Contains(hits[0], "#app") {
		t.Errorf("missing root not reported: %v", hits)
	}
}

// The whole gate, green on a clean page: wait, scan, flush.
func TestScreenPassesACleanPage(t *testing.T) {
	r := New(t, staticPage(`<main id="app"><h1>all well</h1></main>`))
	r.Run(chromedp.Navigate(r.Origin + "/"))
	r.Screen("#app", "clean page")
}
```

- [ ] Verify it fails: `go vet -tags browser ./harness/` — undefined: `junkHits`, `AllowText`.
- [ ] Create `harness/junk.go`:

```go
//go:build browser

package harness

import (
	"encoding/json"
	"fmt"

	"github.com/chromedp/chromedp"
)

// junkValues is what a missing field or an unhandled shape looks like
// once it has been through String() — the bug class that renders
// perfectly and says nothing. Kass's full set. Substring "null" can
// false-positive on legitimate prose, which is why every hit carries
// its surrounding text and AllowText exists for the rare deliberate
// case.
var junkValues = []string{"undefined", "null", "[object Object]", "NaN"}

// AllowText exempts one exact string from the junk scan — for the
// screen whose legitimate prose contains a junk substring ("null and
// void"). The allowance is the surrounding string, not the junk value:
// everything else on the screen is still scanned.
func (r *Rig) AllowText(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.allowedText = append(r.allowedText, s)
}

// junkScanJS reads the screen the way a person would, plus the places
// a person cannot see: input and textarea values, and the labels a
// screen reader announces. The scan root is the screen's own selector
// ("body" is the whole-page case). Each hit carries its surrounding
// text so a prose false-positive is legible at a glance.
const junkScanJS = `((sel, junk, allowed) => {
  const root = document.querySelector(sel);
  if (!root) return ["the junk-scan root " + sel + " is not on the page"];
  const hay = [root.textContent ?? ""];
  for (const f of root.querySelectorAll("input, textarea")) hay.push(f.value ?? "");
  for (const n of root.querySelectorAll("[aria-label]")) hay.push(n.getAttribute("aria-label") ?? "");
  const hits = [];
  for (let text of hay) {
    for (const a of allowed) text = text.split(a).join("");
    for (const j of junk) {
      const at = text.indexOf(j);
      if (at < 0) continue;
      const around = text.slice(Math.max(0, at - 40), at + j.length + 40).replace(/\s+/g, " ").trim();
      hits.push(j + ' in: "' + around + '"');
    }
  }
  return hits;
})`

// junkHits runs the scan rooted at selector and returns what it found.
func (r *Rig) junkHits(selector string) []string {
	r.t.Helper()
	r.mu.Lock()
	allowed := append([]string(nil), r.allowedText...)
	r.mu.Unlock()
	if allowed == nil {
		allowed = []string{} // marshal as [], never null — JS iterates it
	}
	var args []string
	for _, v := range []any{selector, junkValues, allowed} {
		b, err := json.Marshal(v)
		if err != nil {
			r.t.Fatalf("harness: junk scan args: %v", err)
		}
		args = append(args, string(b))
	}
	var hits []string
	expr := fmt.Sprintf("%s(%s, %s, %s)", junkScanJS, args[0], args[1], args[2])
	if err := chromedp.Run(r.ctx, chromedp.Evaluate(expr, &hits)); err != nil {
		r.t.Fatalf("harness: junk scan: %v", err)
	}
	return hits
}
```

- [ ] Edit `harness/screen.go`: replace the `Screen` body's flush block

```go
	if probs := r.take(); len(probs) > 0 {
		r.fail(selector, note, "the page reported problems:\n  "+strings.Join(probs, "\n  "))
	}
```

with the scan-then-flush sequence:

```go
	if hits := r.junkHits(selector); len(hits) > 0 {
		r.fail(selector, note, "a JS value leaked to the screen:\n  "+strings.Join(hits, "\n  "))
	}
	if probs := r.take(); len(probs) > 0 {
		r.fail(selector, note, "the page reported problems:\n  "+strings.Join(probs, "\n  "))
	}
```

and update `Screen`'s doc comment to say the full gate: "wait for selector, run the junk scan, then flush the problem list".

- [ ] Verify pass: `gofmt -l .` (empty), `go vet ./...`, `go test ./...`, `go vet -tags browser ./...`, `go test -tags browser ./harness/ -count=1`.
- [ ] Commit:

```
harness: the screen gate — junk scan, AllowText, and what was on screen

Screen now holds every screen to account: waits for it, scans the
root's text, input values and aria-labels for the values that render
perfectly and say nothing (kass's full set, "null" included), then
flushes the watchers. Hits carry their surrounding text because
substring "null" can be honest prose — AllowText is the deliberate
out. Failures always report what was on screen.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
```

---

### Task 4: ui/browser_test.go rides the shared rig

Discovery and the watch fundamentals move to `harness/` (done in Tasks 1–2); this task deletes ui's private copies and re-wires the one select drive onto the rig — behavior identical, except the two changes the spec mandates when the scan moves: the junk set gains the missing `"null"`, and the scan now also reads input values and aria-labels (via `rig.Screen`). The drive keeps its 60s budget and step-tracking via `rig.Context()`.

**Files**
- Modify: `ui/browser_test.go` (full rewrite below)
- Test: the file is its own test.

**Interfaces**
- Consumes: `harness.New`, `(*harness.Rig).Context/Origin/Run/Screen`, `chromedp.Run`, `chromedp/kb`.
- Produces: `func page(t *testing.T, optionCount int) (http.Handler, chan string)` (was `(*httptest.Server, chan string)`); `chromePath`, `problems`, `watch` deleted.

**Steps**

- [ ] Replace `ui/browser_test.go` wholesale with:

```go
//go:build browser

// The browser test for field-select's enhancement — the residue a Go
// test cannot reach, because it needs a real JS engine and real focus
// and keyboard handling.
//
// Build-tagged rather than env-gated so a plain `go test ./...` never
// silently half-runs it, and so chromedp stays out of the ordinary
// build graph. Run it with:
//
//	go test -tags browser ./ui/
//
// It rides the harness package's rig: Chromium discovery
// (RASTRILLO_CHROME, PATH, the Playwright cache — a skip is not a
// pass, RASTRILLO_BROWSER_OPTIONAL makes it deliberate), the
// loud-failure watchers, and the screen gate's junk scan all live
// there now, shared with every browser drive in the family.
//
// KNOWN LIMITATION, stated rather than discovered: this test is
// timing-sensitive under machine load. On an idle box it passes in
// ~0.4s and 20 consecutive runs are green. On a box at load ~9-14 it
// fails roughly 1 run in 4, always the same way — a keystroke arrives
// while focus has drifted, Enter reaches the document instead of the
// combobox, the form submits, the execution context dies, and the next
// step hangs until the deadline. The failure names the step it got to,
// so it is legible rather than mysterious.
//
// CI runs this in its browser job on an otherwise idle runner; the
// load-flake cost falls on whoever runs it deliberately on a busy
// machine. Rerun before believing a failure, and read the reported
// step: a real regression fails at a specific assertion, load flake
// fails at a deadline after "read-mirrored-value" or later. Fixing it
// properly likely means driving the widget through synthesised events
// inside one page evaluation, which trades away the fidelity of real
// CDP input — not obviously the right trade, so it has not been made.
//
// One test, deliberately: a browser drive is expensive, so this one
// drives the whole journey — render, enhance, filter, keyboard-select,
// mirror, submit — and asserts the server received the value.
// Everything cheaper lives in field_select_test.go and shim_test.go.
package ui

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"amadan.net/rastrillo/rastrillo/harness"
)

// page builds the handler serving one form carrying an enhanced
// field-select, the real select.js and tokens.css, and records what a
// submit delivers.
func page(t *testing.T, optionCount int) (http.Handler, chan string) {
	t.Helper()
	tmpl := template.Must(template.New("").Funcs(Funcs()).ParseFS(Templates(), "*.html"))

	got := make(chan string, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /select.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		w.Write(SelectJS())
	})
	mux.HandleFunc("GET /tokens.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css")
		w.Write(TokensCSS())
	})
	mux.HandleFunc("POST /submit", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		select {
		case got <- r.PostFormValue("author"):
		default:
		}
		fmt.Fprint(w, `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>ok</title></head><body><p id="done">received</p></body></html>`)
	})
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		var body strings.Builder
		body.WriteString(`<!doctype html><html lang="en"><head><meta charset="utf-8">` +
			`<title>select</title><link rel="stylesheet" href="/tokens.css">` +
			`<script defer src="/select.js"></script></head><body>` +
			`<form method="post" action="/submit">`)
		if err := tmpl.ExecuteTemplate(&body, "field-select", selectData(optionCount)); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		body.WriteString(`<button type="submit" id="go">Save</button></form></body></html>`)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, body.String())
	})
	return mux, got
}

// TestEnhancedSelectDrivesTheWholeJourney is the one browser test.
//
// Bug classes it exists to catch — each renders perfectly and says
// nothing wrong:
//
//   - the combobox never mirrors back, so the form submits the old value
//     while the screen shows the new one;
//   - the native select is removed rather than hidden, so the form
//     submits nothing at all;
//   - the label is left pointing at the hidden select, leaving the
//     control the user types into with no accessible name;
//   - a JS error takes the enhancement down and the page still looks fine.
func TestEnhancedSelectDrivesTheWholeJourney(t *testing.T) {
	mux, submitted := page(t, 40)
	rig := harness.New(t, func(string) http.Handler { return mux })

	// A healthy run takes well under a second on an idle machine, but
	// this is wall-clock against a real browser: on a loaded box it is
	// slower by orders of magnitude, and a budget tuned to the idle case
	// fails for no reason. 60s tolerates a busy CI runner while still
	// failing far faster than Go's default test timeout, so a genuine
	// regression surfaces as a deadline rather than a hung suite.
	ctx, cancelTimeout := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancelTimeout()

	var (
		comboCount, nativeCount, optionsShown int
		labelFor, nativeValue                 string
		filterText                            string
		nativeHidden                          bool
	)

	// A bare "context deadline exceeded" tells whoever hits this in CI
	// nothing at all. Record the last step that completed, and report it
	// with the state gathered so far.
	reached := "start"
	at := func(name string) chromedp.Action {
		return chromedp.ActionFunc(func(context.Context) error { reached = name; return nil })
	}
	if err := chromedp.Run(ctx,
		chromedp.Navigate(rig.Origin+"/"), at("navigated"),
		chromedp.WaitVisible(`input[role="combobox"]`, chromedp.ByQuery),
		at("combobox-visible"),
		// The enhancement happened; the native control survived it.
		chromedp.Evaluate(`document.querySelectorAll('input[role="combobox"]').length`, &comboCount),
		chromedp.Evaluate(`document.querySelectorAll('select[name="author"]').length`, &nativeCount),
		// Hidden, not removed. sr-only leaves a ~1px box; anything wider
		// means the select is still taking real layout space.
		chromedp.Evaluate(`(document.querySelector('select[name="author"]')?.getBoundingClientRect().width ?? 999) < 4`, &nativeHidden),
		// The label must name the control the user actually types into.
		chromedp.Evaluate(`document.querySelector('label')?.getAttribute('for') ?? ''`, &labelFor),

		// Every probe above is null-safe on purpose: a missing node should
		// reach the assertions as an empty value, so the failure names the
		// broken invariant instead of surfacing a chromedp node error.
		//
		// Filter, then pick with the keyboard only: a mouse-only
		// combobox is a broken one.
		chromedp.Click(`input[role="combobox"]`, chromedp.ByQuery), at("clicked-combobox"),
		chromedp.SendKeys(`input[role="combobox"]`, "Option 12", chromedp.ByQuery), at("typed-filter"),
		chromedp.Evaluate(`document.querySelectorAll('[role="option"]').length`, &optionsShown),
		chromedp.Evaluate(`document.querySelector('input[role="combobox"]')?.value ?? ''`, &filterText),
		// Synchronise on observable state rather than assuming a keystroke
		// landed. Under load the arrow key can arrive before the filtered
		// list is drawn, or while focus has drifted; then Enter reaches
		// the document instead of the combobox, the form submits, the
		// execution context dies and the next step hangs on a page that
		// no longer exists. Waiting for the highlight turns that into a
		// fast, legible failure at the exact step that did not happen.
		chromedp.WaitVisible(`[role="option"]`, chromedp.ByQuery), at("list-drawn"),
		chromedp.Focus(`input[role="combobox"]`, chromedp.ByQuery), at("focused"),
		chromedp.KeyEvent(kb.ArrowDown), at("arrow-down"),
		chromedp.WaitVisible(`[role="option"].is-active`, chromedp.ByQuery), at("option-highlighted"),
		chromedp.KeyEvent(kb.Enter), at("enter"),

		// The mirror: what the form will actually submit.
		chromedp.Evaluate(`document.querySelector('select[name="author"]')?.value ?? ''`, &nativeValue), at("read-mirrored-value"),

		// Submit rather than Click: what this test asserts is that the
		// form posts the mirrored value, and chromedp.Click waits for the
		// button to be actionable — a wait that intermittently never
		// resolved here even with the value already correctly mirrored.
		// Submitting the form exercises the thing under test without
		// depending on hit-testing an overlay-adjacent button.
		chromedp.Submit(`#go`, chromedp.ByQuery), at("submitted-form"),
		chromedp.WaitVisible(`#done`, chromedp.ByQuery), at("server-responded"),
	); err != nil {
		t.Fatalf("drive failed after %q: %v\n  filterText=%q optionsShown=%d nativeValue=%q labelFor=%q",
			reached, err, filterText, optionsShown, nativeValue, labelFor)
	}

	if comboCount != 1 {
		t.Errorf("expected exactly one combobox, found %d", comboCount)
	}
	if nativeCount != 1 {
		t.Errorf("the native select must survive enhancement, found %d", nativeCount)
	}
	if !nativeHidden {
		t.Error("the native select is still visible; it should be hidden, not removed")
	}
	if !strings.HasSuffix(labelFor, "-combo") {
		t.Errorf("label still points at the hidden select (for=%q): the combobox has no accessible name", labelFor)
	}
	// Focus must select the committed label so typing replaces it. When
	// it does not, the filter text becomes "Option 1Option 12", nothing
	// matches, and Enter commits the select's pre-existing default — a
	// green test that proves nothing. Assert the text we actually typed.
	if filterText != "Option 12" {
		t.Errorf("filter box holds %q, want %q: focus is not replacing the committed label", filterText, "Option 12")
	}
	if optionsShown != 1 {
		t.Errorf("filtering for a unique label showed %d options, want 1", optionsShown)
	}
	// "Option 12" is the 12th option, so value "12". Crucially NOT "1",
	// which is what the select holds by default — an assertion that
	// merely checked non-empty would pass on the bug this test exists for.
	if nativeValue != "12" {
		t.Errorf("native select holds %q, want %q — the combobox did not mirror the keyboard pick back", nativeValue, "12")
	}

	select {
	case v := <-submitted:
		if v != nativeValue {
			t.Errorf("server received author=%q, the select held %q", v, nativeValue)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the form never reached the server")
	}

	// The console check and the junk scan are the rig's screen gate
	// now — which also reads input values and aria-labels, and knows
	// the junk set in full: the "null" this file's in-place scan was
	// missing arrived with the move.
	rig.Screen("body", "after the journey")
}
```

- [ ] Verify pass: `gofmt -l .` (empty), `go vet ./...`, `go test ./...`, `go vet -tags browser ./...`, `go test -tags browser ./ui/ ./harness/ -count=1`. (Under load the ui drive can flake at a deadline — the known limitation; rerun before believing a failure.)
- [ ] Confirm the private copies are gone: `grep -n "func chromePath\|type problems\|func watch" ui/browser_test.go` prints nothing.
- [ ] Commit:

```
ui: the select drive rides the shared rig

Chromium discovery and the loud-failure watchers were this file's
private copies; they live in harness/ now, so the drive keeps only
what is its own — the journey and its assertions. The final junk scan
becomes the rig's screen gate, which brings the "null" the in-place
scan was missing and reads input values and aria-labels too. Behavior
otherwise identical, 60s budget and step-tracking included.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
```

---

### Task 5: real ceremonies in webauthn/ — the harness's own end-to-end proof

The minimal in-repo fixture app the spec's §5 asks for. Nothing in `webauthn/`, `passkey/` or `examples/` serves `webauthn.mjs` ceremonies over HTTP today (`webauthn/authtest` builds ceremonies in-process, `passkey` needs the sessions subsystem and a DB), so the fixture is specified completely here: a plain mux in the test file — a driver page, the real embedded `webauthn.JS()`, and JSON endpoints minting challenges (`webauthn.NewChallenge`) and verifying with the real `Config.Register`/`Config.Verify` (RPID `localhost`, the rig's origin), one in-memory credential. PRF (hmac-secret) is deterministic per credential+salt, which is what makes its bytes assertable.

**Files**
- Create: `webauthn/browser_test.go` (`//go:build browser`, external `package webauthn_test` — the existing `webauthn_test.go` is internal `package webauthn`, so both coexist)
- Test: the file is its own test.

**Interfaces**
- Consumes: `harness.New`, `(*harness.Rig).Run/Screen/Origin`, `webauthn.Config{RPID, Origin}`, `webauthn.Config.Register(challenge, clientDataJSON, attestationObject []byte) (webauthn.Credential, error)`, `webauthn.Config.Verify(cred webauthn.Credential, challenge, clientDataJSON, authData, signature []byte) (uint32, error)`, `webauthn.NewChallenge() ([]byte, error)`, `webauthn.JS() []byte`, `chromedp.Evaluate` with `WithAwaitPromise`.
- Produces: `type fixture struct{...}` with `func (f *fixture) handler(origin string) http.Handler`; `func evalString(r *harness.Rig, expr string) string`; `TestBrowserCeremoniesRoundTripWithPRF`. The driver also ships `driver.probeCreatePRF()` (non-resident on purpose), which Task 6's baseline uses.

**Steps**

- [ ] Write the failing test — create `webauthn/browser_test.go` complete:

```go
//go:build browser

// The harness's own end-to-end proof, and the browser-side proof of
// this package: real WebAuthn ceremonies against Chromium's virtual
// authenticator, driven through js/webauthn.mjs exactly as an app
// serves it, verified by Config.Register and Config.Verify exactly as
// an app calls them — against a minimal in-repo fixture app (a driver
// page, the embedded module, JSON endpoints, one in-memory
// credential). PRF (hmac-secret) is deterministic per credential+salt,
// which is what makes its output assertable at all.
package webauthn_test

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"amadan.net/rastrillo/rastrillo/harness"
	"amadan.net/rastrillo/rastrillo/webauthn"
)

// fixture is the app under drive: one enrolled credential, one pending
// challenge, the real Config doing the verifying. RPID is "localhost"
// because the rig's origin is http://localhost:PORT — the trustworthy
// origin an IP address can never be.
type fixture struct {
	mu        sync.Mutex
	cfg       webauthn.Config
	challenge []byte
	cred      webauthn.Credential
	enrolled  bool
}

const fixturePage = `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>webauthn fixture</title><script type="module" src="/driver.mjs"></script></head><body><p id="page">webauthn fixture</p></body></html>`

// fixtureDriver is the page's half of the drive. It appends #ready
// only after wiring window.driver, so a Screen("#ready", ...) wait
// guarantees the module executed before the test evaluates driver.*
// calls. PRF bytes come back hex-encoded — a value chromedp carries as
// a plain string.
const fixtureDriver = `import { register, authenticate } from "/webauthn.mjs";

const salt = "fixture-prf-salt-v1";
const toHex = (bytes) => Array.from(new Uint8Array(bytes), (b) => b.toString(16).padStart(2, "0")).join("");

async function json(url, body) {
  const res = await fetch(url, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body ?? {}),
  });
  if (!res.ok) throw new Error(url + " answered " + res.status);
  return res.json();
}

window.driver = {
  // register runs the library's creation ceremony and hands the result
  // to the server's Config.Register.
  async register() {
    const begin = await json("/register/begin");
    const cred = await register({
      challenge: begin.challenge,
      rpId: "localhost",
      rpName: "fixture",
      userId: begin.userId,
      userName: "frida@example.com",
      prfSalt: salt,
    });
    await json("/register/finish", {
      credentialId: cred.credentialId,
      clientDataJSON: cred.clientDataJSON,
      attestationObject: cred.attestationObject,
    });
    return toHex(cred.prf);
  },
  // authenticate runs the library's discoverable assertion and hands
  // the result to the server's Config.Verify.
  async authenticate() {
    const begin = await json("/signin/begin");
    const asrt = await authenticate({ challenge: begin.challenge, rpId: "localhost", prfSalt: salt });
    await json("/signin/finish", {
      credentialId: asrt.credentialId,
      clientDataJSON: asrt.clientDataJSON,
      authenticatorData: asrt.authenticatorData,
      signature: asrt.signature,
    });
    return toHex(asrt.prf);
  },
  // probeCreatePRF asks a raw create() for PRF and reports what came
  // back AT CREATION — "" when the extension result is absent. The
  // probe's credential is deliberately non-resident (residentKey
  // "discouraged"), so it never shows up in a later discoverable
  // assertion and cannot make authenticate() ambiguous.
  async probeCreatePRF() {
    const cred = await navigator.credentials.create({
      publicKey: {
        rp: { id: "localhost", name: "fixture" },
        user: { id: crypto.getRandomValues(new Uint8Array(16)), name: "probe@example.com", displayName: "probe" },
        challenge: crypto.getRandomValues(new Uint8Array(32)),
        pubKeyCredParams: [{ type: "public-key", alg: -7 }],
        authenticatorSelection: { residentKey: "discouraged", userVerification: "required" },
        extensions: { prf: { eval: { first: new TextEncoder().encode(salt) } } },
      },
    });
    const first = cred?.getClientExtensionResults()?.prf?.results?.first;
    return first ? toHex(first) : "";
  },
};

const ready = document.createElement("p");
ready.id = "ready";
ready.textContent = "driver up";
document.body.append(ready);
`

func (f *fixture) handler(origin string) http.Handler {
	f.cfg = webauthn.Config{RPID: "localhost", Origin: origin}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, fixturePage)
	})
	mux.HandleFunc("GET /webauthn.mjs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		w.Write(webauthn.JS())
	})
	mux.HandleFunc("GET /driver.mjs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		fmt.Fprint(w, fixtureDriver)
	})
	mux.HandleFunc("POST /register/begin", f.registerBegin)
	mux.HandleFunc("POST /register/finish", f.registerFinish)
	mux.HandleFunc("POST /signin/begin", f.signinBegin)
	mux.HandleFunc("POST /signin/finish", f.signinFinish)
	return mux
}

// mintChallenge stores a fresh ceremony challenge, exactly as an app
// would keep it across the round trip. A nil return means the error
// was already written.
func (f *fixture) mintChallenge(w http.ResponseWriter) []byte {
	challenge, err := webauthn.NewChallenge()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return nil
	}
	f.mu.Lock()
	f.challenge = challenge
	f.mu.Unlock()
	return challenge
}

func (f *fixture) registerBegin(w http.ResponseWriter, r *http.Request) {
	challenge := f.mintChallenge(w)
	if challenge == nil {
		return
	}
	userID := make([]byte, 16)
	rand.Read(userID)
	writeJSON(w, map[string]string{
		"challenge": base64.RawURLEncoding.EncodeToString(challenge),
		"userId":    base64.RawURLEncoding.EncodeToString(userID),
	})
}

func (f *fixture) registerFinish(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CredentialID      string `json:"credentialId"`
		ClientDataJSON    string `json:"clientDataJSON"`
		AttestationObject string `json:"attestationObject"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	clientData, err1 := base64.RawURLEncoding.DecodeString(body.ClientDataJSON)
	attestation, err2 := base64.RawURLEncoding.DecodeString(body.AttestationObject)
	if err1 != nil || err2 != nil {
		http.Error(w, "bad base64url", http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	challenge := f.challenge
	f.mu.Unlock()
	cred, err := f.cfg.Register(challenge, clientData, attestation)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	f.cred, f.enrolled = cred, true
	f.mu.Unlock()
	writeJSON(w, map[string]string{"status": "enrolled"})
}

func (f *fixture) signinBegin(w http.ResponseWriter, r *http.Request) {
	if challenge := f.mintChallenge(w); challenge != nil {
		writeJSON(w, map[string]string{"challenge": base64.RawURLEncoding.EncodeToString(challenge)})
	}
}

func (f *fixture) signinFinish(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CredentialID      string `json:"credentialId"`
		ClientDataJSON    string `json:"clientDataJSON"`
		AuthenticatorData string `json:"authenticatorData"`
		Signature         string `json:"signature"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	cred, enrolled, challenge := f.cred, f.enrolled, f.challenge
	f.mu.Unlock()
	if !enrolled {
		http.Error(w, "nobody is enrolled", http.StatusBadRequest)
		return
	}
	credID, err0 := base64.RawURLEncoding.DecodeString(body.CredentialID)
	clientData, err1 := base64.RawURLEncoding.DecodeString(body.ClientDataJSON)
	authData, err2 := base64.RawURLEncoding.DecodeString(body.AuthenticatorData)
	signature, err3 := base64.RawURLEncoding.DecodeString(body.Signature)
	if err0 != nil || err1 != nil || err2 != nil || err3 != nil {
		http.Error(w, "bad base64url", http.StatusBadRequest)
		return
	}
	if !bytes.Equal(credID, cred.ID) {
		http.Error(w, "unknown credential", http.StatusBadRequest)
		return
	}
	count, err := f.cfg.Verify(cred, challenge, clientData, authData, signature)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	f.cred.SignCount = count
	f.mu.Unlock()
	writeJSON(w, map[string]string{"status": "verified"})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// evalString evaluates one driver call in the page, awaiting its
// promise — an unhandled rejection fails the test through Run, with
// the screen in the report.
func evalString(r *harness.Rig, expr string) string {
	var out string
	r.Run(chromedp.Evaluate(expr, &out, func(p *runtime.EvaluateParams) *runtime.EvaluateParams {
		return p.WithAwaitPromise(true)
	}))
	return out
}

// TestBrowserCeremoniesRoundTripWithPRF is the whole life of a
// passkey-wrapped secret, for real: enrolment through register() with
// a PRF salt, the server verifying with Config.Register; a
// discoverable sign-in through authenticate(), verified with
// Config.Verify — and the PRF bytes identical across both, because
// hmac-secret is deterministic per credential+salt. That equality is
// what lets an E2EE app open on a second sign-in what it sealed at
// enrolment.
func TestBrowserCeremoniesRoundTripWithPRF(t *testing.T) {
	f := &fixture{}
	rig := harness.New(t, f.handler)
	rig.Run(chromedp.Navigate(rig.Origin + "/"))
	rig.Screen("#ready", "fixture booted")

	regPRF := evalString(rig, "driver.register()")
	if len(regPRF) != 64 {
		t.Fatalf("registration PRF is %q, want 32 bytes of hex", regPRF)
	}
	authPRF := evalString(rig, "driver.authenticate()")
	if authPRF != regPRF {
		t.Fatalf("assertion PRF %q != registration PRF %q — PRF must be deterministic per credential+salt", authPRF, regPRF)
	}
	rig.Screen("#ready", "after both ceremonies")
}
```

- [ ] Verify the red honestly: `go test -tags browser ./webauthn/ -count=1 -run TestBrowserCeremoniesRoundTripWithPRF` must run (it may pass immediately — the code under test already exists; the red for this task is the compile-and-run of a test that did not exist. If it FAILS, that is a real finding about the virtual authenticator or the fixture: debug before moving on, and if creation-time PRF turns out absent in this Chromium build, say so loudly — Task 6's baseline depends on it).
- [ ] Verify pass: `gofmt -l .` (empty), `go vet ./...`, `go test ./...`, `go vet -tags browser ./...`, `go test -tags browser ./webauthn/ -count=1`.
- [ ] Commit:

```
webauthn: real ceremonies against the virtual authenticator

The harness's own end-to-end proof and this package's browser half in
one test: a minimal in-repo fixture serves the embedded webauthn.mjs
and verifies with the real Config, while Chromium's virtual
authenticator enrols and signs in with a PRF salt. The PRF bytes must
match across creation and assertion — hmac-secret is deterministic per
credential+salt, which is the property every E2EE consumer leans on.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
```

---

### Task 6: WithoutPRFAtCreation — the own-property shim, and the §4 fallback test

The CDP authenticator cannot withhold PRF at creation (`HasPrf` is all-or-nothing), so the condition is forced one level up. The mechanism is the hard-won part: an **own-property override on the returned credential** — never a prototype patch (that strips PRF from the fallback's own assertion too), never a naive Proxy (brand-checked `response`/`rawId` getters throw "Illegal invocation") — registered via `Page.addScriptToEvaluateOnNewDocument` in the **main world, before Navigate**. The test first proves the unshimmed create DOES return PRF; without that baseline the shim proves nothing. Shipped as a rig option so apps can rehearse the two-prompt path too.

**Files**
- Create: `harness/prfshim.go` (`//go:build browser`)
- Modify: `harness/rig.go` (register the shim in `New`'s boot actions, before any navigation)
- Test: `webauthn/browser_test.go` (append `TestRegisterFallsBackToPRFByAssertion`)

**Interfaces**
- Consumes: `page.AddScriptToEvaluateOnNewDocument(source string)` (cdproto/page; no `WithWorldName` — main world is the default and the point), the Task 5 fixture and `evalString`.
- Produces: `func WithoutPRFAtCreation() Option`; `const prfShimJS string`.

**Steps**

- [ ] Write the failing test — append to `webauthn/browser_test.go`:

```go
// TestRegisterFallsBackToPRFByAssertion covers the branch that shipped
// from kass untested: prfSalt requested, creation returns no PRF,
// register() fetches it with an immediate assertion
// (webauthn.mjs's prfByAssertion) — one test upstream, for every
// consumer.
func TestRegisterFallsBackToPRFByAssertion(t *testing.T) {
	// The baseline first, unshimmed: create() DOES return PRF here.
	// Without it the shim below proves nothing — an authenticator that
	// never answered PRF at creation would take the fallback path
	// whether or not the shim works.
	base := harness.New(t, (&fixture{}).handler)
	base.Run(chromedp.Navigate(base.Origin + "/"))
	base.Screen("#ready", "baseline fixture booted")
	if got := evalString(base, "driver.probeCreatePRF()"); len(got) != 64 {
		t.Fatalf("unshimmed create returned PRF %q, want 32 bytes of hex — the virtual authenticator is not answering the extension at creation", got)
	}

	// Now the shimmed rig: creation PRF is withheld by an own-property
	// override on the returned credential, so register() must take the
	// assertion fallback — credentials.get is untouched and serves real
	// PRF there.
	f := &fixture{}
	rig := harness.New(t, f.handler, harness.WithoutPRFAtCreation())
	rig.Run(chromedp.Navigate(rig.Origin + "/"))
	rig.Screen("#ready", "shimmed fixture booted")
	if got := evalString(rig, "driver.probeCreatePRF()"); got != "" {
		t.Fatalf("shimmed create still returned PRF %q — the shim is not holding, so register() would never fall back", got)
	}
	regPRF := evalString(rig, "driver.register()")
	if len(regPRF) != 64 {
		t.Fatalf("fallback registration PRF is %q, want 32 bytes of hex", regPRF)
	}
	// PRF (hmac-secret) is deterministic per credential+salt, so the
	// bytes the fallback fetched must equal a straight assertion's.
	authPRF := evalString(rig, "driver.authenticate()")
	if authPRF != regPRF {
		t.Fatalf("straight assertion PRF %q != fallback PRF %q — the fallback fetched something other than the credential's real PRF", authPRF, regPRF)
	}
	rig.Screen("#ready", "after the fallback ceremonies")
}
```

- [ ] Verify it fails: `go vet -tags browser ./webauthn/` — undefined: `harness.WithoutPRFAtCreation`.
- [ ] Create `harness/prfshim.go`:

```go
//go:build browser

package harness

// WithoutPRFAtCreation rehearses the browsers that refuse to return
// PRF output during creation — webauthn.mjs's two-prompt fallback,
// where register() runs an immediate assertion to fetch it. The CDP
// virtual authenticator cannot withhold PRF at creation (HasPrf is
// all-or-nothing, SetResponseOverrideBits included), so the condition
// is forced one level up with a page-level shim, registered in the
// main world before any navigation.
//
// The mechanism matters; two tempting shapes fail. Patching
// PublicKeyCredential.prototype.getClientExtensionResults strips PRF
// from assertions too — the fallback's own
// assertion.getClientExtensionResults() goes through the same
// prototype method, so the patch would break the very path under
// test. A naive Proxy around the credential throws "Illegal
// invocation" on the brand-checked response/rawId getters. What works,
// and what register()'s access pattern permits (it reads only rawId,
// response.*, and calls getClientExtensionResults() on the instance):
// wrap navigator.credentials.create and define an OWN property on the
// returned credential that answers {} — creation succeeds, the
// extension result is empty, and credentials.get stays untouched, so
// the virtual authenticator serves real PRF on the fallback assertion.
func WithoutPRFAtCreation() Option {
	return func(c *config) { c.withoutPRFAtCreation = true }
}

// prfShimJS is the shim WithoutPRFAtCreation registers via
// Page.addScriptToEvaluateOnNewDocument — main world (no isolated
// world name), so the page's own module sees the wrapped create.
const prfShimJS = `(() => {
  const create = navigator.credentials.create.bind(navigator.credentials);
  navigator.credentials.create = async (options) => {
    const credential = await create(options);
    if (credential) {
      Object.defineProperty(credential, "getClientExtensionResults", {
        value: () => ({}),
        configurable: true,
      });
    }
    return credential;
  };
})();`
```

- [ ] Edit `harness/rig.go`: add `"github.com/chromedp/cdproto/page"` to the imports, and insert into `New`, immediately after `var boot []chromedp.Action` and before the authenticator action:

```go
	if cfg.withoutPRFAtCreation {
		// Registered before Navigate ever runs, in the main world —
		// see WithoutPRFAtCreation for why this exact shape.
		boot = append(boot, chromedp.ActionFunc(func(ctx context.Context) error {
			_, err := page.AddScriptToEvaluateOnNewDocument(prfShimJS).Do(ctx)
			return err
		}))
	}
```

- [ ] Verify pass: `gofmt -l .` (empty), `go vet ./...`, `go test ./...`, `go vet -tags browser ./...`, `go test -tags browser ./harness/ ./webauthn/ -count=1`.
- [ ] Commit:

```
harness: WithoutPRFAtCreation, and prfByAssertion finally runs

The CDP virtual authenticator cannot withhold PRF at creation, so the
rig option forces the condition one level up: an own-property override
on the credential create() returns — never a prototype patch, which
would strip PRF from the fallback's own assertion, and never a Proxy,
which trips the brand checks — registered on new documents before any
navigation. The webauthn test proves the unshimmed baseline first,
then that the fallback's bytes equal a straight assertion's.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
```

---

### Task 7: the scaffold — browser_test.go, a README, and the new_test.go pins

`rastrillo new` gains `internal/<pkg>test/browser_test.go` under `//go:build browser` (the existing `harness_test.go` — the httptest HTTP harness — keeps its name and meaning) and a scaffolded `README.md` whose Browser drive section states the whole interface, including that CI participation is the app's own call. `new_test.go` pins both via the `readScaffold` pattern, and the existing scaffold-build test gains a cheap `go vet -tags browser ./...` so the scaffolded file is known to compile under the tag.

**Files**
- Modify: `cmd/rastrillo/new.go` (two new template consts, two files-map entries, summary prints)
- Modify: `cmd/rastrillo/new_test.go` (new `TestNewScaffoldsBrowserDrive`; extend `TestScaffoldedAppTestsPass`)
- Test: `cmd/rastrillo/new_test.go`

**Interfaces**
- Consumes: the files map in `runNew` (`fmt.Sprintf(tmpl, name, pkg, strings.ToUpper(pkg))` — the `mainTemplate` trio), `readScaffold(t, parts...)`, `parser.ParseFile`.
- Produces: `const browserTestTemplate string`, `const readmeTemplate string`; scaffolded files `<name>/internal/<pkg>test/browser_test.go` and `<name>/README.md`.

**Steps**

- [ ] Write the failing test — append to `cmd/rastrillo/new_test.go`:

```go
// The scaffold ships the browser drive: a build-tagged browser_test.go
// booting the whole app through the framework's harness package (the
// existing harness_test.go — the httptest HTTP harness — keeps its
// name and its meaning), and a README whose Browser drive section
// states the whole interface, CI call included.
func TestNewScaffoldsBrowserDrive(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := runNew([]string{"my-blog"}); err != nil {
		t.Fatalf("runNew: %v", err)
	}
	src := readScaffold(t, "my-blog", "internal", "myblogtest", "browser_test.go")
	for _, want := range []string{
		"//go:build browser",
		"package myblogtest",
		"amadan.net/rastrillo/rastrillo/harness",
		"harness.New(t, func(origin string) http.Handler {",
		"myblog.App(d, origin, logger)",
		`rig.Screen("body", "home")`,
		"RASTRILLO_BROWSER_OPTIONAL",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("scaffolded browser_test.go missing %q", want)
		}
	}
	if _, err := parser.ParseFile(token.NewFileSet(), "browser_test.go", src, parser.AllErrors); err != nil {
		t.Errorf("scaffolded browser_test.go does not parse: %v", err)
	}

	readme := readScaffold(t, "my-blog", "README.md")
	for _, want := range []string{
		"go test -tags browser ./...",
		"RASTRILLO_CHROME",
		"RASTRILLO_BROWSER_OPTIONAL",
		"A skip is not a pass",
		"this app's call",
	} {
		if !strings.Contains(readme, want) {
			t.Errorf("scaffolded README.md missing %q", want)
		}
	}
}
```

- [ ] Verify it fails: `go test ./cmd/rastrillo/ -run TestNewScaffoldsBrowserDrive` — fails reading the missing scaffolded files.
- [ ] Edit `cmd/rastrillo/new.go` — add the two templates after `indexTestTemplate`:

```go
// browserTestTemplate is the scaffolded browser drive, delivered once
// like the harness: app-owned from here on. The existing
// harness_test.go is the httptest HTTP harness and keeps its name —
// the browser rig takes a different one everywhere (file, tag,
// invocation), because "harness" already means the HTTP one throughout
// the scaffold's prose.
const browserTestTemplate = `//go:build browser

// The browser drive: the whole app in a real Chromium, wired exactly
// as cmd/%[1]s/main.go wires it — loud on any console error, failed
// request, 4xx/5xx, or JS value that leaked to the screen unrendered.
// Not part of the plain suite:
//
//	go test -tags browser ./...
//
// It needs a Chromium: on PATH, via RASTRILLO_CHROME, or in a
// Playwright cache. A skip is not a pass — with no browser this fails,
// unless RASTRILLO_BROWSER_OPTIONAL is set, which makes the skip a
// deliberate visible choice rather than an accident.
package %[2]stest

import (
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"testing"

	"amadan.net/rastrillo/rastrillo/db"
	"amadan.net/rastrillo/rastrillo/harness"
	"github.com/chromedp/chromedp"

	%[2]s "%[1]s/internal/%[2]s"
)

// TestBrowserWalk is the minimal loud walk: the home screen arrives
// with nothing wrong anywhere the rig watches. Grow it with the app —
// each screen a flow reaches earns a Screen() line, and an expected
// probe (a signed-out 401, say) earns a rig.Allow.
func TestBrowserWalk(t *testing.T) {
	rig := harness.New(t, func(origin string) http.Handler {
		// The scaffold's own wiring, origin included: the rig hands
		// the app its localhost origin before serving — the same seam
		// main.go fills from %[3]s_ORIGIN.
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		d, err := db.Open(filepath.Join(t.TempDir(), "app.db"), logger)
		if err != nil {
			t.Fatalf("db.Open: %%v", err)
		}
		t.Cleanup(func() { d.Close() })
		mux, err := %[2]s.App(d, origin, logger)
		if err != nil {
			t.Fatalf("App: %%v", err)
		}
		return mux
	})
	rig.Run(chromedp.Navigate(rig.Origin + "/"))
	rig.Screen("body", "home")
}
`

// readmeTemplate is the scaffolded app README. Small on purpose:
// AGENTS.md carries the conventions; this says what the app is and
// how the browser drive runs — including that CI participation is the
// app's own call.
const readmeTemplate = `# %[1]s

A [rastrillo](https://github.com/rastrilloorg/rastrillo) app. ` + "`make ci`" + `
is the gate — vet, gofmt, tests, migration check, one definition for
CI and for you. AGENTS.md carries the working conventions.

## Browser drive

` + "`internal/%[2]stest/browser_test.go`" + ` walks the app in a real
Chromium — loud on any console error, failed request, or JS value that
leaked to the screen:

` + "```sh" + `
go test -tags browser ./...   # real browser, loud on any console error
                              # — not part of the plain suite
` + "```" + `

It needs a Chromium: on PATH, via ` + "`RASTRILLO_CHROME`" + `, or in a
Playwright cache. **A skip is not a pass:** with no browser it fails,
unless ` + "`RASTRILLO_BROWSER_OPTIONAL=1`" + ` makes the skip a deliberate,
visible choice. ` + "`make ci`" + ` runs the plain suite only — whether CI
also runs the browser drive, and on what runner, is this app's call.
`
```

- [ ] Edit `cmd/rastrillo/new.go` — in `runNew`'s files map, after the `index_test.go` entry, add:

```go
		// The browser drive, on the same delivered-once terms as the
		// harness. browser_test.go, never harness_test.go: that name
		// (and word) already belongs to the httptest HTTP harness.
		filepath.Join(name, "internal", pkg+"test", "browser_test.go"): fmt.Sprintf(browserTestTemplate, name, pkg, strings.ToUpper(pkg)),
		filepath.Join(name, "README.md"):                               fmt.Sprintf(readmeTemplate, name, pkg),
```

- [ ] Edit `cmd/rastrillo/new.go` — update two summary prints in `runNew`: change

```go
	fmt.Printf("  internal/%stest/     (harness + example tests, passing out of the box)\n", pkg)
```

to

```go
	fmt.Printf("  internal/%stest/     (harness + example tests, passing out of the box;\n", pkg)
	fmt.Println("                        browser_test.go = the browser drive, go test -tags browser ./...)")
```

and after the `CLAUDE.md` print line add:

```go
	fmt.Println("  README.md            (what this app is, and how the browser drive runs)")
```

- [ ] Edit `cmd/rastrillo/new_test.go` — in `TestScaffoldedAppTestsPass`, after the `go test ./...` block, add:

```go
	// The browser drive must at least compile under its tag; vet
	// type-checks the test files without needing a browser. This is
	// the "compiles under -tags browser in the existing scaffold-build
	// test" the spec asks for, at vet cost rather than test cost.
	vet := exec.Command("go", "vet", "-tags", "browser", "./...")
	vet.Dir = "blogapp"
	if out, err := vet.CombinedOutput(); err != nil {
		t.Fatalf("scaffolded app fails go vet -tags browser:\n%s", out)
	}
```

- [ ] Verify pass: `gofmt -l .` (empty), `go vet ./...`, `go test ./cmd/rastrillo/ -count=1` (includes the slow scaffold-build test — it now also proves the tagged compile), then the full `go test ./...`.
- [ ] Commit:

```
new: scaffold the browser drive — browser_test.go and a README that says how

internal/<pkg>test/browser_test.go boots the whole app through
harness.New with the scaffold's own wiring and walks the home screen —
the minimal loud walk, growing with the app. browser_test, not
harness_test: that name already means the httptest harness throughout
the scaffold. The new README states the whole interface — one tag, one
invocation, a skip is not a pass — and that CI participation is the
app's own call. The scaffold-build test now vets under -tags browser,
so the delivered file is known to compile.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
```

---

### Task 8: CI runs the browser tag; the README says the whole story; the build-graph promise gets a check

rastrillo's ci.yml runs untagged today, so §4's "finally covered" branch would still never run. Add a `browser` job with a pinned Chromium (Playwright's installer into the exact cache path `ChromePath` already globs — the simplest honest pin; ubuntu-latest's preinstalled Chrome floats, apt's chromium is a snap shim), `RASTRILLO_BROWSER_OPTIONAL` unset. Add the executable form of the README invariant to the main job. Extend README §Browser tests for the harness package, the webauthn coverage and the scaffolded usage.

**Files**
- Modify: `.github/workflows/ci.yml`
- Modify: `README.md` (§"Browser tests")
- Test: CI itself on the PR; locally the same commands run by hand.

**Interfaces**
- Consumes: `harness.ChromePath`'s Playwright-cache glob (`~/.cache/ms-playwright/chromium-*/chrome-linux64/chrome`); the existing ci.yml job layout.
- Produces: a `browser` CI job; a `chromedp stays out of the ordinary build graph` step; the extended README section.

**Steps**

- [ ] Edit `.github/workflows/ci.yml` — in the `test` job, insert directly after the `root module` step:

```yaml
      # The README promises, twice, that chromedp stays out of the
      # ordinary build graph. This is that sentence, executable.
      - name: chromedp stays out of the ordinary build graph
        run: |
          if go list -deps ./... | grep -i chromedp; then
            echo "go list -deps ./... pulls chromedp — the README's promise is broken"
            exit 1
          fi
```

- [ ] Edit `.github/workflows/ci.yml` — append a new job at the end of `jobs:` (sibling of `test`):

```yaml
  # The browser drive: rastrillo's own browser-tagged tests — the ui
  # select journey, the harness's own checks, and webauthn's PRF
  # ceremonies including the prfByAssertion fallback — on every PR.
  # RASTRILLO_BROWSER_OPTIONAL stays unset on purpose: a skip is not a
  # pass, so a runner that loses its browser fails loudly.
  browser:
    runs-on: ubuntu-latest
    env:
      GOFLAGS: -mod=mod
      CGO_ENABLED: 0
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod

      # A pinned Playwright pins the Chromium build it installs, and it
      # lands in the cache path harness.ChromePath already globs — no
      # RASTRILLO_CHROME needed. The runner's preinstalled Chrome
      # floats with the image and apt's chromium is a snap shim, so
      # this is the simplest honest pin. Bump it deliberately.
      - name: install pinned chromium
        run: npx --yes playwright@1.54.1 install --with-deps chromium

      # Only the packages carrying browser-tagged tests: the plain
      # suite already ran in the test job, and -tags browser ./...
      # would rerun all of it here for nothing.
      - name: browser-tagged tests
        run: go test -tags browser ./harness/ ./ui/ ./webauthn/ -count=1
```

- [ ] Edit `README.md` — replace the body of §"Browser tests" (everything from the line after `## Browser tests` down to, but not including, the paragraph beginning "`chromedp` is pinned to v0.14.2") with (four-backtick fence here only because the replacement itself contains a fenced block — the README gets the inner text verbatim):

````markdown
Almost everything here is covered by ordinary Go tests. What is not —
a real JS engine, real focus, a real authenticator — runs under one
build tag, framework and scaffolded apps alike:

```
go test -tags browser ./...
```

Build-tagged, so a plain `go test ./...` never half-runs a browser and
chromedp stays out of the ordinary build graph (`go list -deps ./...`
pulls none of it — CI runs that sentence as a step). A Chromium is
found on `PATH`, via `RASTRILLO_CHROME`, or in a Playwright cache.
**A skip is not a pass:** with no browser the tagged tests fail,
unless `RASTRILLO_BROWSER_OPTIONAL=1` makes the skip a deliberate,
visible choice. CI's `browser` job runs them with a pinned Chromium on
every PR.

Three packages carry the tag:

- `ui/` — `field-select`'s searchable enhancement gets a single
  chromedp drive of the whole journey — render, enhance, filter,
  keyboard-select, mirror back, submit — asserting the server received
  the value a user picked. Its assertions are written as the bug
  classes they catch, and each was verified by breaking the script on
  purpose and watching the test fail — including the one that found a
  real bug during development, where the filter box kept the committed
  label so typing appended to it, matched nothing, and silently
  committed the pre-existing value.
- `harness/` — the browser rig itself, the library those drives are
  built on: `harness.New` binds a localhost listener first, hands the
  app its origin (`http://localhost:PORT` — an IP is not a WebAuthn RP
  ID), then launches a Chromium with a CDP virtual authenticator, PRF
  included. Watchers turn every console error, failed request and
  4xx/5xx into a test failure (`rig.Allow` excuses expected probes);
  `rig.Screen` gates each screen behind a junk scan of its text, input
  values and aria-labels for `undefined`, `null`, `[object Object]`,
  `NaN` — the bug class that renders perfectly and says nothing.
- `webauthn/` — real ceremonies against the virtual authenticator:
  enrolment, PRF at creation, sign-in, and the two-prompt
  `prfByAssertion` fallback, forced by `harness.WithoutPRFAtCreation()`
  because the CDP authenticator cannot withhold PRF at creation on its
  own.

`rastrillo new` scaffolds `internal/<pkg>test/browser_test.go` on the
same tag — the minimal loud walk, boots the whole app through
`harness.New` and grows with it.
````

- [ ] Verify locally, in order:
  - `gofmt -l .` (empty), `go vet ./...`, `go test ./... -count=1`
  - `go list -deps ./... | grep -ci chromedp` prints `0` — the explicit post-landing verification of the README invariant
  - `go vet -tags browser ./...`
  - `go test -tags browser ./harness/ ./ui/ ./webauthn/ -count=1` (this machine: found via the Playwright cache glob; no env vars needed)
- [ ] Commit:

```
ci: a pinned Chromium runs the browser tag, and the build-graph promise
becomes a step

Landing the prfByAssertion test without CI wiring would leave the
"finally covered" branch still never running — so a browser job runs
the three tagged packages on every PR, with the Chromium pinned
through Playwright's installer into the exact cache path discovery
already globs, and RASTRILLO_BROWSER_OPTIONAL unset because a skip is
not a pass. The README's twice-made promise that go list -deps pulls
no chromedp is now an executable step, and its Browser tests section
tells the whole three-package story.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
```

---

## Self-review checklist

**Spec coverage** — every numbered spec section maps to tasks:
- §1 corrections (chromedp not Playwright; `Handler` seam; CDP can't withhold creation-PRF; `browser_test.go` naming) — Tasks 1, 5, 6, 7 respect all four; the scaffold file is `browser_test.go`, `harness_test.go` untouched.
- §2 shape — untagged `doc.go` + `//go:build browser` everywhere else (Task 1); `New(t, build, opts...)` binds the listener first, localhost origin, kass's authenticator options verbatim with `AutomaticPresenceSimulation` explicitly true, LIFO cleanup via `t.Cleanup` + `chromedp.Cancel` (Task 1); `ChromePath` moved verbatim, `RASTRILLO_BROWSER_OPTIONAL` semantics kept, ui switched to it (Tasks 1, 4); watchers with the three chromedp facts — `log.EventEntryAdded` mirror filtered by URL, method via `EventRequestWillBeSent` correlation, favicon pre-allowed (Task 2); `Screen` waits, scans rooted at the selector (`"body"` = whole page), flushes naming the screen; junk set gains `"null"`, hits show surrounding text, `AllowText` exists (Task 3); failure reports carry on-screen `innerText` (Tasks 1–3); `rig.Run` wraps `chromedp.Run`, no DSL (Task 1).
- §3 — scaffolded `browser_test.go` boots via `harness.New` with the scaffold's own wiring, navigates `/`, `Screen("body", "home")`; one tag `browser`; the README section with the exact sh block; no Makefile, no CLI verb; CI browser job with pinned Chromium, `RASTRILLO_BROWSER_OPTIONAL` unset; apps' CI call stated in the scaffolded README (Tasks 7, 8).
- §4 — own-property shim (never prototype patch, never Proxy), `Page.addScriptToEvaluateOnNewDocument` main world before Navigate, baseline unshimmed-create-returns-PRF asserted first, fallback PRF equals a straight assertion's, lives in `webauthn/` under the tag, shipped as `harness.WithoutPRFAtCreation()` (Task 6).
- §5 — the PRF test as the harness's e2e proof against a fully specified in-repo fixture (Task 5 — nothing existing in `webauthn/`/`examples/` serves webauthn.mjs ceremonies, checked); junk-scan and watcher tests against static handlers (Tasks 2, 3); `new_test.go` `readScaffold` assertions + tagged compile via `go vet -tags browser` in the scaffold-build test (Task 7).
- §6 out of scope — nothing here touches the RPID drill, screenshots, multi-browser or parallel rigs. No scope beyond the spec was added; the only judgment calls are recorded in the tasks (Screen's 60s wait bound reusing ui's stated budget reasoning; the scaffolded README being a new file because the scaffold has none today; the probe credential being non-resident so it cannot make discoverable assertions ambiguous).

**Placeholder scan** — every code step above is complete, compilable text: no `...` bodies, no "similar to Task N", no TODOs. The two `Edit`-style steps (Task 2's `New` insertion, Task 6's shim insertion, Task 3's `Screen` body swap, Task 7's files-map/print edits) quote the exact lines to add and the exact anchor text to place them against.

**Type consistency** — checked against the pinned sources: `webauthn.VirtualAuthenticatorOptions` field names (`Ctap2version`, `HasPrf`, `AutomaticPresenceSimulation`, `IsUserVerified`) match cdproto v0.0.0-20250724212937; `log.EventEntryAdded.Entry.{Level,Text,URL}`, `network.EventRequestWillBeSent.Request.{Method,URL}`, `network.EventResponseReceived.Response.{Status,URL}` (Status is int64, cast to int), `network.EventLoadingFailed.{Canceled,ErrorText}` all exist as used; `page.AddScriptToEvaluateOnNewDocument(source)` defaults to the main world; chromedp v0.14.2 auto-enables the log and network domains on attach, so the watchers need no Enable calls; `chromedp.EvaluateOption` is `func(*runtime.EvaluateParams) *runtime.EvaluateParams` and `WithAwaitPromise` exists; `Config.Register`/`Config.Verify`/`NewChallenge`/`JS` signatures match `webauthn/webauthn.go` and `js.go`; the scaffold trio `(name, pkg, strings.ToUpper(pkg))` matches `mainTemplate`'s existing call shape, and all `%` verbs inside scaffold templates are `%%`-escaped.
