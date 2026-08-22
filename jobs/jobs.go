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
//
// Two bounds protect the process from its signed-in callers: an owner
// may have at most maxRunningPerOwner jobs Running at once (Start past
// that answers ErrOwnerBusy), and a job still Running after jobTimeout
// is marked Failed and stops counting — its context expires at the
// same moment, so a well-behaved fn stops too. What no bound can do is
// kill a goroutine: an fn that ignores its context runs invisibly
// until the process restarts. It just no longer blocks its owner.
package jobs

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
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
// back-button, short enough that finished jobs do not pile up. It
// bounds finished jobs only: a Running one is never swept.
const doneTTL = 10 * time.Minute

// maxRunningPerOwner caps how many Running jobs one owner may hold. A
// constant, not config: four concurrent background tasks is generous
// for a person clicking buttons, and an app that outgrows it should
// say so upstream rather than tune it quietly.
const maxRunningPerOwner = 4

// jobTimeout is how long a job may Run before the registry gives up on
// it: its context expires and it is marked Failed. Long enough for any
// job a button should start, short enough that a stuck one frees its
// owner's cap slot the same afternoon.
const jobTimeout = 15 * time.Minute

// ErrOwnerBusy is Start's refusal when the owner is at
// maxRunningPerOwner. Handlers should turn it into copy of their own —
// "wait for a job to finish" — not echo it.
var ErrOwnerBusy = errors.New("too many running jobs")

// errPanicked carries a recovered panic from the fn goroutine to the
// watchdog; the owner sees only the generic failure text.
var errPanicked = errors.New("fn panicked")

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
	logger  *slog.Logger
	now     func() time.Time // swapped by tests to exercise the sweep
	timeout time.Duration    // jobTimeout, shortened by tests

	mu   sync.Mutex
	jobs map[string]*Job
}

func New(logger *slog.Logger) *Jobs {
	if logger == nil {
		logger = slog.Default()
	}
	return &Jobs{logger: logger, now: time.Now, timeout: jobTimeout, jobs: map[string]*Job{}}
}

// Start runs fn in a goroutine and returns the job snapshot
// immediately — or ErrOwnerBusy if the owner already has
// maxRunningPerOwner jobs Running. fn's error text is shown to the
// job's owner — return messages fit for them, and log internals
// yourself. progress replaces the job's Progress text; call it as
// often as you like. fn's context expires after jobTimeout, and the
// job reads Failed from then on — honor the context, and keep jobs
// idempotent, because a deploy is a harder deadline than any timeout.
// The context is otherwise detached from the request and the server:
// jobs outlive their request by definition, and tying them to server
// shutdown waits for a real graceful-drain story. location must be a
// path your own code built, never anything a user supplied — a
// finished job hands it to the shim, which navigates.
func (j *Jobs) Start(owner, name, location string, fn func(ctx context.Context, progress func(string)) error) (Job, error) {
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
	running := 0
	for _, existing := range j.jobs {
		if existing.Owner == owner && existing.Status == Running {
			running++
		}
	}
	if running >= maxRunningPerOwner {
		j.mu.Unlock()
		// name, not owner: this package's log lines never carry the
		// subject (often an email); request logs correlate if needed.
		j.logger.Info("jobs: refused", "name", name, "running", running)
		return Job{}, ErrOwnerBusy
	}
	j.jobs[job.ID] = job
	snap := *job
	j.mu.Unlock()
	j.logger.Info("jobs: start", "id", job.ID, "name", name)
	go j.run(job, fn)
	return snap, nil
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
	ctx, cancel := context.WithTimeout(context.Background(), j.timeout)
	defer cancel()
	progress := func(text string) {
		j.mu.Lock()
		// A finished job's snapshot is settled: a stuck fn calling
		// progress after its timeout must not mutate what the owner
		// already read as Failed.
		if job.Status == Running {
			job.Progress = text
		}
		j.mu.Unlock()
	}
	done := make(chan error, 1)
	go func() {
		// A panicking job must not take the process down or vanish
		// without a trace: it is logged in full here, where the value
		// is, and shown to the owner generically.
		defer func() {
			if p := recover(); p != nil {
				j.logger.Error("jobs: panic", "id", job.ID, "name", job.Name, "panic", p)
				done <- errPanicked
			}
		}()
		done <- fn(ctx, progress)
	}()
	select {
	case err := <-done:
		switch {
		case errors.Is(err, errPanicked):
			j.finish(job, Failed, "something went wrong")
		case errors.Is(err, context.DeadlineExceeded):
			// A well-behaved fn surfacing its expired context reads
			// the same to the owner as the watchdog firing below.
			j.logger.Error("jobs: timeout", "id", job.ID, "name", job.Name)
			j.finish(job, Failed, "took too long")
		case err != nil:
			j.logger.Error("jobs: failed", "id", job.ID, "name", job.Name, "err", err)
			j.finish(job, Failed, err.Error())
		default:
			j.logger.Info("jobs: done", "id", job.ID, "name", job.Name)
			j.finish(job, Done, "")
		}
	case <-ctx.Done():
		j.logger.Error("jobs: timeout", "id", job.ID, "name", job.Name)
		j.finish(job, Failed, "took too long")
		// The goroutine can't be killed (done is buffered, so its send
		// won't block either way) — but log its eventual return: a
		// "stuck" job that finished an hour later is exactly what an
		// operator wants to know when judging whether jobTimeout fits.
		go func() {
			err := <-done
			j.logger.Info("jobs: returned after timeout", "id", job.ID, "name", job.Name, "err", err)
		}()
	}
}

// finish settles the snapshot, exactly once: a job that already left
// Running (the watchdog got there first) keeps its verdict.
func (j *Jobs) finish(job *Job, status Status, errText string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if job.Status != Running {
		return
	}
	job.Status = status
	job.Err = errText
	job.FinishedAt = j.now()
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
