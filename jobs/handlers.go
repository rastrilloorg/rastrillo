package jobs

import (
	"errors"
	"net/http"
	"time"

	"amadan.net/rastrillo/rastrillo/sessions"
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
// EventsPath is the SSE endpoint (Events); a template that puts it in
// data-poll-push upgrades a supporting browser from timer polling to
// server push, and apps that ignore it get today's polling exactly.
type PageData struct {
	Job          Job
	FragmentPath string
	EventsPath   string
	PollSeconds  int
}

// Handlers' tick, ttl and heartbeat mirror jobs.Jobs's now/timeout
// test-injection seam: the stream constants, shortened by tests so a
// stream's whole life fits inside a test's deadline.
type Handlers struct {
	cfg       Config
	tick      time.Duration // streamTick
	ttl       time.Duration // streamTTL
	heartbeat time.Duration // streamHeartbeat
}

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
	return &Handlers{cfg: cfg, tick: streamTick, ttl: streamTTL, heartbeat: streamHeartbeat}, nil
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
	return PageData{
		Job:          job,
		FragmentPath: "/jobs/" + job.ID + "/fragment",
		EventsPath:   "/jobs/" + job.ID + "/events",
		PollSeconds:  pollSeconds,
	}
}
