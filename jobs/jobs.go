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
