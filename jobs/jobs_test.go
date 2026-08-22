package jobs

import (
	"context"
	"errors"
	"io"
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
	j := New(slog.New(slog.NewTextHandler(io.Discard, nil)))
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
	j := New(slog.New(slog.NewTextHandler(io.Discard, nil)))
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
	j := New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	job := j.Start("alice", "Export notes", "", func(ctx context.Context, progress func(string)) error {
		return errors.New("could not write the export")
	})
	got := wait(t, j, job.ID, "alice")
	if got.Status != Failed || got.Err != "could not write the export" {
		t.Fatalf("failure not recorded: %+v", got)
	}
}

func TestPanicBecomesFailed(t *testing.T) {
	j := New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	job := j.Start("alice", "Export notes", "", func(ctx context.Context, progress func(string)) error {
		panic("boom")
	})
	got := wait(t, j, job.ID, "alice")
	if got.Status != Failed || !strings.Contains(got.Err, "something went wrong") {
		t.Fatalf("panic not recorded as generic failure: %+v", got)
	}
}

func TestSweepDropsFinishedJobsAfterTTL(t *testing.T) {
	j := New(slog.New(slog.NewTextHandler(io.Discard, nil)))
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
	j := New(slog.New(slog.NewTextHandler(io.Discard, nil)))
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
