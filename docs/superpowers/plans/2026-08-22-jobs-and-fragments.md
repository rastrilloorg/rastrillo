# Background Jobs and the Fragment Shim Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make background work observable — a `jobs` primitive, a no-JS status-page pattern, and a ~100-line first-party polling shim — demonstrated end-to-end in examples/notes.

**Architecture:** New top-level `jobs` package (in-memory registry + password-style Handlers with app-owned render funcs); `ui` gains the embedded `rastrillo.js` shim, an `rst-spin` CSS token, and a `job-status` partial; `rastrillo new` scaffolds the shim beside tokens.css; notes grows an owner-scoped "Export notes" job proving the whole flow including two-user isolation.

**Tech Stack:** Go stdlib + existing deps only (no new modules). Plain ES5-ish JS, IIFE, no build step.

**Spec:** docs/superpowers/specs/2026-08-22-jobs-and-fragments-design.md

## Global Constraints

- Never merge to main directly; the controller PRs and squash-merges. Commit trailer: `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.
- All Go commands: `CGO_ENABLED=0`. Tests under examples/notes and internal/generate on this machine additionally need `GOFLAGS=-mod=mod`.
- Never `git add -A` at the repo root (untracked device files live there). Add files by path.
- SKILL.md has a hard 15,000-byte budget enforced by the root test in skillmd_test.go; it currently passes at 14,998 bytes. Any addition must be paid for by trims, verified by running that test.
- Exact vocabulary (spec-mandated, do not rename): data attributes `data-poll`, `data-poll-every`, `data-busy`, `data-busy-label`; request header `Rastrillo-Fragment: 1`; response header `Rastrillo-Location`; partial name `job-status`; CSS class `rst-spin`.
- Owner is always the session **Subject** (string), never a numeric user id.
- Doc comments follow the codebase's voice: explain constraints and honest limits, not what the next line does.

---

### Task 1: jobs package core

**Files:**
- Create: `jobs/jobs.go`
- Test: `jobs/jobs_test.go`

**Interfaces:**
- Produces: `jobs.New(logger *slog.Logger) *Jobs`, `(*Jobs).Start(owner, name, location string, fn func(context.Context, func(string)) error) Job`, `(*Jobs).Get(id, owner string) (Job, bool)`, `Job{ID, Owner, Name, Status, Progress, Err, Location string/Status; StartedAt, FinishedAt time.Time}`, `Status` constants `Running/Done/Failed`. Task 2 consumes all of this.

- [ ] **Step 1: Write the failing tests**

`jobs/jobs_test.go` (package `jobs` — internal, so tests can inject the clock):

```go
package jobs

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// wait polls Get until the job leaves Running or the deadline passes.
// Jobs run on real goroutines; tests observe completion the same way a
// status page does, by polling the snapshot.
func wait(t *testing.T, j *Jobs, id, owner string) Job {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		job, ok := j.Get(id, owner)
		if !ok {
			t.Fatalf("job %s vanished while waiting", id)
		}
		if job.Status != Running {
			return job
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("job %s still running after 5s", id)
	return Job{}
}

func TestStartGetRoundTrip(t *testing.T) {
	j := New(slog.New(slog.DiscardHandler))
	release := make(chan struct{})
	job := j.Start("alice", "Export notes", "/exports/x1", func(ctx context.Context, progress func(string)) error {
		progress("2 of 3")
		<-release
		return nil
	})
	if job.ID == "" || job.Status != Running || job.Owner != "alice" {
		t.Fatalf("start snapshot wrong: %+v", job)
	}
	got, ok := j.Get(job.ID, "alice")
	if !ok || got.Name != "Export notes" || got.Location != "/exports/x1" {
		t.Fatalf("get: ok=%v job=%+v", ok, got)
	}
	// Progress set by the goroutine becomes visible across Get calls.
	deadline := time.Now().Add(5 * time.Second)
	for {
		got, _ = j.Get(job.ID, "alice")
		if got.Progress == "2 of 3" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("progress never became visible: %+v", got)
		}
		time.Sleep(5 * time.Millisecond)
	}
	close(release)
	done := wait(t, j, job.ID, "alice")
	if done.Status != Done || done.FinishedAt.IsZero() {
		t.Fatalf("finished job wrong: %+v", done)
	}
}

func TestOwnerIsolation(t *testing.T) {
	j := New(slog.New(slog.DiscardHandler))
	job := j.Start("alice", "Export notes", "", func(ctx context.Context, progress func(string)) error { return nil })
	// Bob probing Alice's job gets the same answer as an unknown id.
	if _, ok := j.Get(job.ID, "bob"); ok {
		t.Fatal("wrong owner could read the job")
	}
	if _, ok := j.Get("no-such-id", "alice"); ok {
		t.Fatal("unknown id answered")
	}
}

func TestFailedCapturesErrorText(t *testing.T) {
	j := New(slog.New(slog.DiscardHandler))
	job := j.Start("alice", "Export notes", "", func(ctx context.Context, progress func(string)) error {
		return errors.New("could not write the export")
	})
	got := wait(t, j, job.ID, "alice")
	if got.Status != Failed || got.Err != "could not write the export" {
		t.Fatalf("failure not recorded: %+v", got)
	}
}

func TestPanicBecomesFailed(t *testing.T) {
	j := New(slog.New(slog.DiscardHandler))
	job := j.Start("alice", "Export notes", "", func(ctx context.Context, progress func(string)) error {
		panic("boom")
	})
	got := wait(t, j, job.ID, "alice")
	if got.Status != Failed || !strings.Contains(got.Err, "something went wrong") {
		t.Fatalf("panic not recorded as generic failure: %+v", got)
	}
}

func TestSweepDropsFinishedJobsAfterTTL(t *testing.T) {
	j := New(slog.New(slog.DiscardHandler))
	now := time.Now()
	j.now = func() time.Time { return now }
	job := j.Start("alice", "Export notes", "", func(ctx context.Context, progress func(string)) error { return nil })
	wait(t, j, job.ID, "alice")
	now = now.Add(doneTTL + time.Minute)
	if _, ok := j.Get(job.ID, "alice"); ok {
		t.Fatal("finished job survived past the TTL")
	}
}

func TestConcurrentStartAndGet(t *testing.T) {
	j := New(slog.New(slog.DiscardHandler))
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			job := j.Start("alice", "n", "", func(ctx context.Context, progress func(string)) error {
				progress("working")
				return nil
			})
			wait(t, j, job.ID, "alice")
		}()
	}
	wg.Wait()
}
```

Note: `slog.DiscardHandler` needs Go ≥ 1.24; if the module's Go version predates it, use `slog.New(slog.NewTextHandler(io.Discard, nil))` instead — check go.mod and match the codebase's existing test idiom (grep the tests for which form they already use).

- [ ] **Step 2: Run tests to verify they fail**

Run: `CGO_ENABLED=0 go test ./jobs/`
Expected: FAIL — package does not exist / undefined symbols.

- [ ] **Step 3: Write the implementation**

`jobs/jobs.go`:

```go
// Package jobs is the observable handle for background work: Start runs
// a function in a goroutine and hands back an ID a status page can poll
// with Get. It is deliberately in-memory and unpersisted — these apps
// are single-process, and a restart kills the goroutine anyway, so a
// persisted row would only persist a lie. A job is a goroutine: a
// deploy ends it mid-flight. Design long jobs to be idempotent and
// re-runnable, and reach for the eventlog when work must survive a
// restart.
//
// Owner is the session Subject (a string — keymail subjects are emails,
// password subjects are numeric strings). Get answers only the owner:
// a wrong owner and an unknown ID are indistinguishable, the same
// someone-else's-row-is-a-404 rule the scope package enforces.
package jobs

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"log/slog"
	"sync"
	"time"
)

// Status is a job's lifecycle position. There is no "queued": Start
// runs the goroutine immediately.
type Status string

const (
	Running Status = "running"
	Done    Status = "done"
	Failed  Status = "failed"
)

// doneTTL is how long a finished job stays answerable after
// FinishedAt — long enough for a status page mid-poll and a curious
// back-button, short enough that the map never grows without bound.
const doneTTL = 10 * time.Minute

// Job is a point-in-time snapshot. Start and Get return copies, never
// shared pointers, so callers read fields without holding any lock.
type Job struct {
	ID         string
	Owner      string
	Name       string // human label: "Export notes"
	Status     Status
	Progress   string // latest progress text, "" until the job sets one
	Err        string // Failed only: shown to the owner
	Location   string // where the owner lands when Done; "" = stay on the status page
	StartedAt  time.Time
	FinishedAt time.Time // zero while Running
}

// Jobs is the registry. Zero value is not usable; call New.
type Jobs struct {
	logger *slog.Logger
	now    func() time.Time // swapped by tests to exercise the sweep

	mu   sync.Mutex
	jobs map[string]*Job
}

func New(logger *slog.Logger) *Jobs {
	if logger == nil {
		logger = slog.Default()
	}
	return &Jobs{logger: logger, now: time.Now, jobs: map[string]*Job{}}
}

// Start runs fn in a goroutine and returns the job snapshot
// immediately. fn's error text is shown to the job's owner — return
// messages fit for them, and log internals yourself. progress replaces
// the job's Progress text; call it as often as you like. fn's context
// is Background: jobs outlive their request by definition, and tying
// them to server shutdown waits for a real graceful-drain story.
func (j *Jobs) Start(owner, name, location string, fn func(ctx context.Context, progress func(string)) error) Job {
	job := &Job{
		ID:        newID(),
		Owner:     owner,
		Name:      name,
		Status:    Running,
		Location:  location,
		StartedAt: j.now(),
	}
	j.mu.Lock()
	j.sweepLocked()
	j.jobs[job.ID] = job
	snap := *job
	j.mu.Unlock()
	j.logger.Info("jobs: start", "id", job.ID, "name", name)
	go j.run(job, fn)
	return snap
}

// Get returns the job only to its owner; anything else is (Job{},
// false), which handlers answer with 404 — never a 403 that would
// confirm the ID exists.
func (j *Jobs) Get(id, owner string) (Job, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.sweepLocked()
	job, ok := j.jobs[id]
	if !ok || job.Owner != owner {
		return Job{}, false
	}
	return *job, true
}

func (j *Jobs) run(job *Job, fn func(context.Context, func(string)) error) {
	// A panicking job must not take the process down or vanish without
	// a trace: it is logged in full and shown to the owner generically.
	defer func() {
		if p := recover(); p != nil {
			j.logger.Error("jobs: panic", "id", job.ID, "name", job.Name, "panic", p)
			j.finish(job, Failed, "something went wrong")
		}
	}()
	progress := func(text string) {
		j.mu.Lock()
		job.Progress = text
		j.mu.Unlock()
	}
	if err := fn(context.Background(), progress); err != nil {
		j.logger.Error("jobs: failed", "id", job.ID, "name", job.Name, "err", err)
		j.finish(job, Failed, err.Error())
		return
	}
	j.logger.Info("jobs: done", "id", job.ID, "name", job.Name)
	j.finish(job, Done, "")
}

func (j *Jobs) finish(job *Job, status Status, errText string) {
	j.mu.Lock()
	job.Status = status
	job.Err = errText
	job.FinishedAt = j.now()
	j.mu.Unlock()
}

// sweepLocked drops finished jobs past their TTL. It runs inside Start
// and Get rather than on a timer, so there is no background goroutine
// to leak and nothing to shut down. Callers hold mu.
func (j *Jobs) sweepLocked() {
	cutoff := j.now().Add(-doneTTL)
	for id, job := range j.jobs {
		if job.Status != Running && job.FinishedAt.Before(cutoff) {
			delete(j.jobs, id)
		}
	}
}

func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand does not fail on supported platforms
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
```

- [ ] **Step 4: Run the tests, including the race detector**

Run: `CGO_ENABLED=0 go test -race ./jobs/`
Expected: PASS (the concurrent test and progress-visibility loop exercise the locking).

- [ ] **Step 5: Commit**

```bash
git add jobs/jobs.go jobs/jobs_test.go
git commit -m "jobs: in-memory observable handle for background work"
```

---

### Task 2: jobs handlers — status page and fragment

**Files:**
- Create: `jobs/handlers.go`
- Test: `jobs/handlers_test.go`

**Interfaces:**
- Consumes: Task 1's `Jobs`/`Job`/`Status`; `sessions.Current(r)` and `sessions.WithSession(r, sess)` (both exist on main).
- Produces: `jobs.NewHandlers(Config) (*Handlers, error)`, `Config{Jobs *Jobs; Render, RenderFragment func(http.ResponseWriter, *http.Request, PageData)}`, `PageData{Job Job; FragmentPath string; PollSeconds int}`, `(*Handlers).StatusPage`, `(*Handlers).Fragment` (both `http.HandlerFunc` signatures). Task 5 consumes these.

- [ ] **Step 1: Write the failing tests**

`jobs/handlers_test.go` (package `jobs`). Build requests with `httptest.NewRequest` + `req.SetPathValue("id", …)` and attach identity with `sessions.WithSession(req, sessions.Session{Subject: "alice"})`. Cover, with one test function each:

```go
package jobs

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/carlosframework/rastrillo/sessions"
)

func testHandlers(t *testing.T, j *Jobs) *Handlers {
	t.Helper()
	h, err := NewHandlers(Config{
		Jobs: j,
		Render: func(w http.ResponseWriter, r *http.Request, d PageData) {
			w.Write([]byte("PAGE " + string(d.Job.Status) + " " + d.FragmentPath))
		},
		RenderFragment: func(w http.ResponseWriter, r *http.Request, d PageData) {
			w.Write([]byte("FRAG " + string(d.Job.Status)))
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func get(h http.HandlerFunc, id, subject string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", "/jobs/"+id, nil)
	req.SetPathValue("id", id)
	if subject != "" {
		req = sessions.WithSession(req, sessions.Session{Subject: subject})
	}
	w := httptest.NewRecorder()
	h(w, req)
	return w
}

func startBlocked(j *Jobs, owner, location string) (Job, chan struct{}) {
	release := make(chan struct{})
	job := j.Start(owner, "Export notes", location, func(ctx context.Context, progress func(string)) error {
		<-release
		return nil
	})
	return job, release
}
```

- `TestStatusPageRunning`: blocked job → StatusPage answers 200, body contains `PAGE running` and `/jobs/<id>/fragment`.
- `TestStatusPageDoneRedirects`: job with location `/exports/x1`, release + `wait` (reuse Task 1's helper) → StatusPage answers 303 with `Location: /exports/x1`.
- `TestStatusPageDoneWithoutLocationRenders`: location "" → after finish, 200 `PAGE done`.
- `TestStatusPageFailedRenders`: fn returns an error → 200, body `PAGE failed`.
- `TestFragmentRunning`: 200, body `FRAG running`.
- `TestFragmentDoneSetsRastrilloLocation`: finished job with location → 204, header `Rastrillo-Location: /exports/x1`, empty body.
- `TestForeignJobIs404`: Alice's job fetched with subject "bob" → 404, and the body identical to an unknown id's 404 (compare the two recorders' bodies with `strings.TrimSpace`).
- `TestSignedOutIs403`: no session on the request → 403 for both StatusPage and Fragment.

- [ ] **Step 2: Run tests to verify they fail**

Run: `CGO_ENABLED=0 go test ./jobs/`
Expected: FAIL — NewHandlers undefined.

- [ ] **Step 3: Write the implementation**

`jobs/handlers.go`:

```go
package jobs

import (
	"errors"
	"net/http"

	"github.com/carlosframework/rastrillo/sessions"
)

// pollSeconds is the status page's cadence, written by templates into
// both the shim's data-poll-every and the noscript meta refresh so the
// two paths tick together.
const pollSeconds = 2

// Config wires the two render seams the app owns — the same shape as
// password.Config's page renderers. Render draws the full status page;
// RenderFragment draws only the polled partial (ui's job-status
// partial, or the app's own markup). Both are required.
type Config struct {
	Jobs           *Jobs
	Render         func(w http.ResponseWriter, r *http.Request, d PageData)
	RenderFragment func(w http.ResponseWriter, r *http.Request, d PageData)
}

// PageData is what both renderers receive. FragmentPath is the polled
// endpoint for this job; templates put it in data-poll. PollSeconds
// feeds data-poll-every and the noscript meta — emit that meta only
// while Job.Status is Running, or a failed page refreshes forever.
type PageData struct {
	Job          Job
	FragmentPath string
	PollSeconds  int
}

type Handlers struct{ cfg Config }

func NewHandlers(cfg Config) (*Handlers, error) {
	if cfg.Jobs == nil {
		return nil, errors.New("jobs: Config.Jobs is required")
	}
	if cfg.Render == nil {
		return nil, errors.New("jobs: Config.Render is required")
	}
	if cfg.RenderFragment == nil {
		return nil, errors.New("jobs: Config.RenderFragment is required")
	}
	return &Handlers{cfg: cfg}, nil
}

// lookup resolves the request's job or writes the refusal itself. The
// mounting contract is behind sessions.Require, so the 403 here is
// defense in depth; a foreign or unknown id is a plain 404 — never a
// 403 that would confirm the id exists. The id is r.PathValue("id"):
// the stdlib mux populates it natively and chi has since v5.1.
func (h *Handlers) lookup(w http.ResponseWriter, r *http.Request) (Job, bool) {
	sess, ok := sessions.Current(r)
	if !ok {
		http.Error(w, "signed out", http.StatusForbidden)
		return Job{}, false
	}
	job, ok := h.cfg.Jobs.Get(r.PathValue("id"), sess.Subject)
	if !ok {
		http.NotFound(w, r)
		return Job{}, false
	}
	return job, true
}

// StatusPage is GET /jobs/{id}: the loading state the button's 303
// lands on. A finished job with somewhere to go 303s there — that is
// also what the noscript meta-refresh path rides to its result.
func (h *Handlers) StatusPage(w http.ResponseWriter, r *http.Request) {
	job, ok := h.lookup(w, r)
	if !ok {
		return
	}
	if job.Status == Done && job.Location != "" {
		http.Redirect(w, r, job.Location, http.StatusSeeOther)
		return
	}
	h.cfg.Render(w, r, pageData(job))
}

// Fragment is GET /jobs/{id}/fragment: what the shim polls. Done with
// a Location answers 204 + Rastrillo-Location and the shim navigates;
// otherwise the fragment renders, and a finished fragment's markup
// omits data-poll — which is how the shim stops.
func (h *Handlers) Fragment(w http.ResponseWriter, r *http.Request) {
	job, ok := h.lookup(w, r)
	if !ok {
		return
	}
	if job.Status == Done && job.Location != "" {
		w.Header().Set("Rastrillo-Location", job.Location)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	h.cfg.RenderFragment(w, r, pageData(job))
}

func pageData(job Job) PageData {
	return PageData{Job: job, FragmentPath: "/jobs/" + job.ID + "/fragment", PollSeconds: pollSeconds}
}
```

- [ ] **Step 4: Run tests**

Run: `CGO_ENABLED=0 go test -race ./jobs/ ./sessions/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add jobs/handlers.go jobs/handlers_test.go
git commit -m "jobs: status page and fragment handlers"
```

---

### Task 3: ui — the shim, rst-spin, and the job-status partial

**Files:**
- Create: `ui/rastrillo.js`
- Create: `ui/partials/job-status.html`
- Modify: `ui/ui.go` (embed + ShimJS accessor + doc paragraphs)
- Modify: `ui/tokens.css` (rst-spin)
- Test: `ui/shim_test.go` (new); `ui/ui_test.go` (register job-status in TestAllPartialsAreDefined and add a styleguide/render case following the file's existing pattern — read the test file first and follow its conventions exactly)

**Interfaces:**
- Produces: `ui.ShimJS() []byte`; template partial `job-status` taking a dict with keys Name, Status, Progress, Err, PollURL, PollSeconds. Tasks 4 and 5 consume these.

- [ ] **Step 1: Write the failing tests**

`ui/shim_test.go`:

```go
package ui

import (
	"bytes"
	"strings"
	"testing"
)

// The shim has no browser harness — JS behavior is verified by hand
// and by the notes example's no-JS end-to-end path. What a Go test can
// hold honest is the contract the docs promise: the vocabulary the
// file answers to, its inert-by-default IIFE shape, and the absence of
// anything a CSP would reject.
func TestShimContract(t *testing.T) {
	js := string(ShimJS())
	for _, want := range []string{
		"data-poll", "data-poll-every", "data-busy", "data-busy-label",
		"Rastrillo-Fragment", "Rastrillo-Location",
	} {
		if !strings.Contains(js, want) {
			t.Errorf("shim does not mention %q", want)
		}
	}
	if !strings.HasPrefix(strings.TrimSpace(strings.SplitN(js, "\n(function", 2)[0]), "/*") {
		t.Error("shim should open with its contract comment")
	}
	if !strings.Contains(js, "(function () {") || !strings.Contains(js, "})();") {
		t.Error("shim should be a single IIFE")
	}
	if strings.Contains(js, "eval(") || strings.Contains(js, "new Function") {
		t.Error("shim must stay CSP-clean")
	}
}

func TestShimIsSmall(t *testing.T) {
	if n := len(ShimJS()); n > 8*1024 {
		t.Fatalf("shim is %d bytes; the point is that an app owner can read it in one sitting — trim it", n)
	}
	if bytes.Contains(ShimJS(), []byte("\t")) {
		t.Error("shim uses two-space indentation, not tabs")
	}
}
```

In `ui/ui_test.go`: add `"job-status"` to the authoritative partial list in TestAllPartialsAreDefined, and add a render case exercising the partial with a dict for both a running job (asserts `data-poll` and `rst-spin` present) and a done job (asserts `data-poll` absent). Follow the existing test's structure for how partials are parsed and executed.

- [ ] **Step 2: Run tests to verify they fail**

Run: `CGO_ENABLED=0 go test ./ui/`
Expected: FAIL — ShimJS undefined, job-status not defined.

- [ ] **Step 3: Write the shim**

`ui/rastrillo.js` (two-space indentation, exactly this contract):

```js
/* rastrillo.js — the fragment shim. First-party, dependency-free, and
   inert by default: only elements that opt in with a data attribute
   get behavior, and everything it enhances also works with scripts
   disabled (a status page's <noscript> meta refresh). This file is
   app-owned from the moment it is scaffolded — edit it like any other
   static file.

   Vocabulary:
     data-poll="URL"       fetch URL for an HTML fragment, replace this
                           element with it, repeat while the new
                           fragment still carries data-poll
     data-poll-every="2"   seconds between polls (default 2)
     data-busy             on a <form>: on the way out, disable submit
                           buttons and set aria-busy="true"
     data-busy-label="…"   optional button text while busy

   A polled response may answer 204 with a Rastrillo-Location header
   instead of a fragment; the shim navigates there. Fetch errors back
   off (doubling to a 30s cap) and keep trying — a network blip must
   not strand a status page. */
(function () {
  "use strict";

  function poll(el) {
    var base = (parseFloat(el.getAttribute("data-poll-every")) || 2) * 1000;
    var wait = base;
    function tick() {
      fetch(el.getAttribute("data-poll"), { headers: { "Rastrillo-Fragment": "1" } })
        .then(function (res) {
          if (!res.ok && res.status !== 204) throw new Error("status " + res.status);
          var to = res.headers.get("Rastrillo-Location");
          if (to) { window.location.assign(to); return null; }
          return res.text();
        })
        .then(function (html) {
          if (html === null) return; // navigating
          wait = base; // a healthy response resets the backoff
          var tpl = document.createElement("template");
          tpl.innerHTML = html;
          var next = tpl.content.firstElementChild;
          if (!next) return; // fragment with no element: stop politely
          el.replaceWith(next);
          el = next;
          if (el.hasAttribute("data-poll")) schedule();
        })
        .catch(function () {
          wait = Math.min(wait * 2, 30000);
          schedule();
        });
    }
    function schedule() { setTimeout(tick, wait); }
    schedule();
  }

  function busy(form) {
    form.addEventListener("submit", function () {
      // Deferred a tick: disabling a submit button during the submit
      // event would drop its name/value from the submitted form data.
      setTimeout(function () {
        form.setAttribute("aria-busy", "true");
        var buttons = form.querySelectorAll(
          'button[type="submit"], button:not([type]), input[type="submit"]'
        );
        buttons.forEach(function (b) {
          b.disabled = true;
          var label = form.getAttribute("data-busy-label");
          if (label) {
            if (b.tagName === "INPUT") { b.value = label; } else { b.textContent = label; }
          }
        });
      }, 0);
    });
  }

  function scan() {
    document.querySelectorAll("[data-poll]").forEach(poll);
    document.querySelectorAll("form[data-busy]").forEach(busy);
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", scan);
  } else {
    scan();
  }
})();
```

- [ ] **Step 4: Write the partial and the CSS**

`ui/partials/job-status.html` (match the data-contract comment style of the existing partials — read one first):

```html
{{/* job-status — a background job's polled fragment: spinner and
     progress while running, the outcome once it isn't. Emits data-poll
     only while running — omitting it is how the shim stops. Data
     (dict): Name, Status ("running"/"done"/"failed"), Progress, Err,
     PollURL (the fragment endpoint), PollSeconds. */}}
{{define "job-status"}}
<div class="rst-job"{{if eq .Status "running"}} data-poll="{{.PollURL}}" data-poll-every="{{.PollSeconds}}"{{end}}>
{{- if eq .Status "running"}}
  <span class="rst-spin" aria-hidden="true"></span> <strong>{{.Name}}</strong> is running{{with .Progress}} — {{.}}{{end}}…
{{- else if eq .Status "failed"}}
  <strong>{{.Name}}</strong> failed{{with .Err}}: {{.}}{{end}}
{{- else}}
  <strong>{{.Name}}</strong> finished.
{{- end}}
</div>
{{end}}
```

Append to `ui/tokens.css` (match the file's comment voice — read the tail of the file first):

```css
/* rst-spin — the working indicator a status page or busy button wears.
   Pure CSS; reduced-motion users get a steady dimmed ring instead of
   rotation. */
.rst-spin {
  display: inline-block;
  width: 1em;
  height: 1em;
  border: 2px solid currentColor;
  border-right-color: transparent;
  border-radius: 50%;
  vertical-align: -0.15em;
  animation: rst-spin 0.8s linear infinite;
}
@keyframes rst-spin { to { transform: rotate(1turn); } }
@media (prefers-reduced-motion: reduce) {
  .rst-spin { animation: none; opacity: 0.5; }
}
```

In `ui/ui.go`: add `//go:embed rastrillo.js` + `var shimJS []byte` + accessor mirroring TokensCSS's doc voice:

```go
// ShimJS returns rastrillo.js's raw bytes — the fragment shim — for
// rastrillo new's scaffold step to write into a new app's static
// directory beside tokens.css. Like the stylesheet, it is delivered
// once and app-owned from then on. The file's own header comment is
// its contract; TestShimContract holds the two honest.
func ShimJS() []byte { return shimJS }
```

Also extend the package doc's delivery paragraph (the one about tokens.css) with one sentence noting rastrillo.js ships the same way, and — since the package doc's opening says "no JavaScript" idioms — add a sentence making the boundary explicit: the shim never replaces a native idiom; it exists only for work that finishes after the response was sent.

- [ ] **Step 5: Run tests**

Run: `CGO_ENABLED=0 go test ./ui/ ./...` (full tree — the contrast test and any tokens.css assertions must stay green)
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add ui/rastrillo.js ui/shim_test.go ui/partials/job-status.html ui/ui.go ui/tokens.css ui/ui_test.go
git commit -m "ui: fragment shim, job-status partial, rst-spin"
```

---

### Task 4: scaffold ships the shim

**Files:**
- Modify: `cmd/rastrillo/new.go` (find where TokensCSS is written into static/ and mirror it for rastrillo.js; find the scaffolded layout template and add `<script defer src=…rastrillo.js>` using the same URL idiom the scaffold uses for tokens.css — hashed via Assets.Path or plain /static/, whichever the scaffold already does)
- Test: `cmd/rastrillo/new_test.go`

**Interfaces:**
- Consumes: `ui.ShimJS()` from Task 3.

- [ ] **Step 1: Read new.go and new_test.go** to find the tokens.css write, the layout template literal, and the existing test that asserts scaffolded files land. Follow those patterns exactly.

- [ ] **Step 2: Write the failing test additions** — the scaffold test asserts `static/rastrillo.js` exists, its bytes equal `ui.ShimJS()`, and the scaffolded layout contains `rastrillo.js` in a `<script defer` tag. If a scaffold-compiles test exists, it inherently covers the change; still add the byte-equality assertion.

- [ ] **Step 3: Run to verify failure, implement, run again**

Run: `CGO_ENABLED=0 GOFLAGS=-mod=mod go test ./cmd/rastrillo/`
Expected: FAIL first, then PASS after mirroring the tokens.css handling.

- [ ] **Step 4: Commit**

```bash
git add cmd/rastrillo/new.go cmd/rastrillo/new_test.go
git commit -m "scaffold: ship rastrillo.js beside tokens.css"
```

---

### Task 5: notes example — the Export flow

**Files:**
- Modify: `examples/notes/internal/notes/models.go` (Export model), `handlers.go` (startExport, showExport), `app.go` (jobs wiring + routes + shim route), `render.go` (status page + fragment rendering)
- Create: `examples/notes/internal/notes/templates/status.html`
- Modify: `examples/notes/internal/notes/templates/layout.html` (script tag; and genlayout.html identically if it shares chrome), `templates/index.html` (Export button)
- Test: `examples/notes/internal/notestest/export_test.go` (new)

**Interfaces:**
- Consumes: Tasks 1–3 (jobs, Handlers, ui partial + ShimJS).

Read `handlers.go`, `models.go`, and the notestest suite first and match their idioms (owner scoping, flash usage, chi routing, test helpers for signing in two users). The shapes to implement:

- [ ] **Step 1: Model.** In models.go:

```go
// Export is a finished "Export notes" document. Its ID is a random
// token generated before the job starts, so the job's Location is
// known up front; Owner is the session Subject, and showExport keys on
// both — Bob fetching Alice's export is a 404, same as the notes.
type Export struct {
	ID        string `gorm:"primaryKey"`
	Owner     string `gorm:"index"`
	Content   string
	CreatedAt time.Time
}
```

Add `&Export{}` to the AutoMigrate call in app.go.

- [ ] **Step 2: Wiring.** In app.go: `jobsReg := jobs.New(logger)` stored on `app`; build `jobs.NewHandlers` with Render/RenderFragment from render.go (Step 4); routes inside the Require group:

```go
r.Post("/export", a.startExport)
r.Get("/exports/{id}", a.showExport)
r.Get("/jobs/{id}", jh.StatusPage)
r.Get("/jobs/{id}/fragment", jh.Fragment)
```

Outside the group, serve the shim (the example embeds nothing extra — it serves ui's bytes directly, with a comment noting a scaffolded app owns the file in static/ instead):

```go
r.Get("/static/rastrillo.js", func(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Write(ui.ShimJS())
})
```

- [ ] **Step 3: Handlers.** In handlers.go:

```go
// startExport kicks off the background export and 303s to the status
// page — the loading state the button was missing. The export's ID is
// minted here so the job knows its Location before it starts.
func (a *app) startExport(w http.ResponseWriter, r *http.Request) {
	sess, _ := sessions.Current(r)
	exportID := newToken() // reuse/extract the same crypto/rand+base64url idiom jobs uses; 16 bytes
	owner := sess.Subject
	g := a.db
	job := a.jobs.Start(owner, "Export notes", "/exports/"+exportID,
		func(ctx context.Context, progress func(string)) error {
			var notes []Note
			if err := g.WithContext(ctx).Where("owner = ?", owner).Order("id").Find(&notes).Error; err != nil {
				return errors.New("could not read your notes")
			}
			var b strings.Builder
			b.WriteString("# Notes export\n")
			for i, n := range notes {
				// Simulated pace so a demo's status page is actually
				// visible — a real export would just be fast.
				time.Sleep(300 * time.Millisecond)
				progress(fmt.Sprintf("%d of %d", i+1, len(notes)))
				fmt.Fprintf(&b, "\n## %s\n\n%s\n", n.Title, n.Body)
			}
			exp := Export{ID: exportID, Owner: owner, Content: b.String()}
			if err := g.WithContext(ctx).Create(&exp).Error; err != nil {
				return errors.New("could not write the export")
			}
			return nil
		})
	http.Redirect(w, r, "/jobs/"+job.ID, http.StatusSeeOther)
}

// showExport serves the finished document as markdown, keyed on id AND
// owner — the same someone-else's-row-is-a-404 rule as everything else.
func (a *app) showExport(w http.ResponseWriter, r *http.Request) {
	sess, _ := sessions.Current(r)
	var exp Export
	err := a.db.WithContext(r.Context()).
		Where("id = ? AND owner = ?", chi.URLParam(r, "id"), sess.Subject).
		First(&exp).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "something went wrong", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Write([]byte(exp.Content))
}
```

**IMPORTANT — check the Note model's real field names and owner column** (`Owner` vs `UserID` etc.) in models.go and use whatever the existing owner-scoped queries in handlers.go use; the `Where("owner = ?", …)` above is illustrative and must match reality. Same for how `a.db`/`a.jobs` fields are named. If notes' owner column stores a numeric user id rather than the Subject string, convert exactly the way existing handlers do.

- [ ] **Step 4: Rendering.** In render.go: add `"status"` to the pages init loop; the status template tree additionally needs ui's partial and funcs — parse `ui.Templates()` with `ui.Funcs()` into the status page's template (and build the fragment template from the same source). Add:

```go
// renderJobPage and renderJobFragment are jobs.Config's two seams.
// The fragment executes the ui partial alone — no layout — because
// its whole point is to be swapped into a page that already has one.
func (a *app) renderJobPage(w http.ResponseWriter, r *http.Request, d jobs.PageData) {
	renderContent(w, r, "status", statusView(d))
}

func (a *app) renderJobFragment(w http.ResponseWriter, r *http.Request, d jobs.PageData) {
	if err := fragmentTmpl.ExecuteTemplate(w, "job-status", statusView(d)); err != nil {
		slog.Default().Error("notes: render job fragment", "err", err)
	}
}

// statusView flattens jobs.PageData into the dict-shaped keys the
// job-status partial documents.
func statusView(d jobs.PageData) map[string]any {
	return map[string]any{
		"Name": d.Job.Name, "Status": string(d.Job.Status),
		"Progress": d.Job.Progress, "Err": d.Job.Err,
		"PollURL": d.FragmentPath, "PollSeconds": d.PollSeconds,
		"Running": d.Job.Status == jobs.Running,
	}
}
```

`templates/status.html`:

```html
{{define "content"}}
{{if .Content.Running}}<noscript><meta http-equiv="refresh" content="{{.Content.PollSeconds}}"></noscript>{{end}}
<h1>Working…</h1>
{{template "job-status" .Content}}
<p><a href="/">Back to notes</a></p>
{{end}}
```

(If `<noscript><meta>` inside body offends html/template or the layout structure, the accepted fallback is placing it via a layout hook — but browsers honor meta refresh in body noscript; keep it simple unless a test proves otherwise.)

`templates/index.html`: add near the top, matching the page's existing markup style:

```html
<form method="post" action="/export" data-busy data-busy-label="Exporting…">
  <button type="submit" class="rst-btn">Export notes</button>
</form>
```

(CSRF: check how other POST forms in notes templates carry the token and do the same.)

`layout.html` (and `genlayout.html` if it duplicates the chrome): add before `</body>`:

```html
<script defer src="/static/rastrillo.js"></script>
```

- [ ] **Step 5: Tests.** `examples/notes/internal/notestest/export_test.go`, using the suite's existing helpers for building the app and signing in users:

- `TestExportRoundTrip`: sign in Alice, create two notes, POST /export → 303 to `/jobs/{id}`; GET that page → 200 containing `data-poll` and the noscript meta; poll GET `/jobs/{id}` until it answers 303 (deadline 10s) → follow to `/exports/{id}` → 200, `Content-Type` markdown, body contains both note titles.
- `TestExportFragmentSignalsDone`: after the job finishes, GET `/jobs/{id}/fragment` → 204 with `Rastrillo-Location: /exports/…`.
- `TestExportIsolation`: Alice runs an export to completion; Bob (signed in) GETs Alice's `/jobs/{id}` → 404 and Alice's `/exports/{id}` → 404.
- `TestShimServed`: GET /static/rastrillo.js (signed out is fine — it's outside Require) → 200, body contains `data-poll`.

- [ ] **Step 6: Run everything**

Run: `cd examples/notes && CGO_ENABLED=0 GOFLAGS=-mod=mod go test ./...` (use the absolute path, not cd, if the shell's cwd is unreliable)
Expected: PASS. Then `CGO_ENABLED=0 go build ./...` at the repo root.

- [ ] **Step 7: Commit**

```bash
git add examples/notes
git commit -m "notes: background export with status page and fragment shim"
```

(`git add examples/notes` from the repo root is safe — the never-`add -A` rule is about the worktree root's untracked device files.)

---

### Task 6: docs — SKILL.md, README, drift check

**Files:**
- Modify: `SKILL.md`, `README.md`

- [ ] **Step 1: SKILL.md.** Add to the appropriate section (§5's plugin/middleware territory or wherever background work fits the document's flow) a compact teaching of: `jobs.New`/`Start`/`Get` (owner = Subject, foreign id = 404); mount `jobs.NewHandlers`' StatusPage/Fragment behind `sessions.Require` at `/jobs/{id}` and `/jobs/{id}/fragment`; the no-JS contract (noscript meta refresh while running; Done+Location = 303); the shim vocabulary (`data-poll`/`data-poll-every`/`data-busy`, `Rastrillo-Location`, app-owned static/rastrillo.js). **The budget test allows 15,000 bytes and the file is at 14,998 — every byte added must be paid for by a trim elsewhere.** Trim with the same judgment as previous rounds (drop parentheticals, merge clauses); never delete a load-bearing rule. Verify: `CGO_ENABLED=0 go test . -run SkillMD` (confirm the exact test name in skillmd_test.go first).
- [ ] **Step 2: README.** One paragraph in the features/story area: background work is observable (start a job, land on a status page that works with scripts off), and the only JavaScript in the framework is a ~100-line app-owned shim that polls HTML fragments — htmx remains a choice, not a dependency.
- [ ] **Step 3: Full-tree verification**

Run: `CGO_ENABLED=0 GOFLAGS=-mod=mod go test ./...` at the root, then the same under examples/notes, then `go vet ./...`.
Expected: all PASS.

- [ ] **Step 4: Commit**

```bash
git add SKILL.md README.md
git commit -m "docs: teach jobs, the status-page pattern, and the shim"
```
