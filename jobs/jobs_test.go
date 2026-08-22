package jobs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// waitFor polls Get until the job leaves Running or the deadline
// passes. Jobs run on real goroutines; tests observe completion the
// same way a status page does, by polling the snapshot. It reports
// trouble as an error rather than failing the test itself, so a
// spawned goroutine can call it too — t.Fatalf's FailNow is only legal
// on the test's own goroutine.
func waitFor(j *Jobs, id, owner string) (Job, error) {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		job, ok := j.Get(id, owner)
		if !ok {
			return Job{}, fmt.Errorf("job %s vanished while waiting", id)
		}
		if job.Status != Running {
			return job, nil
		}
		time.Sleep(5 * time.Millisecond)
	}
	return Job{}, fmt.Errorf("job %s still running after 5s", id)
}

// wait is waitFor on the test goroutine, where failing fast is legal.
func wait(t *testing.T, j *Jobs, id, owner string) Job {
	t.Helper()
	job, err := waitFor(j, id, owner)
	if err != nil {
		t.Fatal(err)
	}
	return job
}

func TestStartGetRoundTrip(t *testing.T) {
	j := New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	release := make(chan struct{})
	job, err := j.Start("alice", "Export notes", "/exports/x1", func(ctx context.Context, progress func(string)) error {
		progress("2 of 3")
		<-release
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
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
	job, err := j.Start("alice", "Export notes", "", func(ctx context.Context, progress func(string)) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
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
	job, err := j.Start("alice", "Export notes", "", func(ctx context.Context, progress func(string)) error {
		return errors.New("could not write the export")
	})
	if err != nil {
		t.Fatal(err)
	}
	got := wait(t, j, job.ID, "alice")
	if got.Status != Failed || got.Err != "could not write the export" {
		t.Fatalf("failure not recorded: %+v", got)
	}
}

func TestPanicBecomesFailed(t *testing.T) {
	j := New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	job, err := j.Start("alice", "Export notes", "", func(ctx context.Context, progress func(string)) error {
		panic("boom")
	})
	if err != nil {
		t.Fatal(err)
	}
	got := wait(t, j, job.ID, "alice")
	if got.Status != Failed || !strings.Contains(got.Err, "something went wrong") {
		t.Fatalf("panic not recorded as generic failure: %+v", got)
	}
}

func TestSweepDropsFinishedJobsAfterTTL(t *testing.T) {
	j := New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	now := time.Now()
	j.now = func() time.Time { return now }
	job, err := j.Start("alice", "Export notes", "", func(ctx context.Context, progress func(string)) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	wait(t, j, job.ID, "alice")
	now = now.Add(doneTTL + time.Minute)
	if _, ok := j.Get(job.ID, "alice"); ok {
		t.Fatal("finished job survived past the TTL")
	}
}

func TestCapRefusesJobPastTheOwnersLimit(t *testing.T) {
	j := New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	release := make(chan struct{})
	block := func(ctx context.Context, progress func(string)) error {
		<-release
		return nil
	}
	var ids []string
	for i := 0; i < maxRunningPerOwner; i++ {
		job, err := j.Start("alice", "n", "", block)
		if err != nil {
			t.Fatalf("job %d refused under the cap: %v", i, err)
		}
		ids = append(ids, job.ID)
	}
	if _, err := j.Start("alice", "n", "", block); !errors.Is(err, ErrOwnerBusy) {
		t.Fatalf("job past the cap: err=%v, want ErrOwnerBusy", err)
	}
	// The cap is per owner: alice's backlog does not touch bob.
	bob, err := j.Start("bob", "n", "", block)
	if err != nil {
		t.Fatalf("bob refused by alice's cap: %v", err)
	}
	close(release)
	for _, id := range ids {
		wait(t, j, id, "alice")
	}
	wait(t, j, bob.ID, "bob")
	// Finished jobs stop counting.
	if _, err := j.Start("alice", "n", "", func(ctx context.Context, progress func(string)) error { return nil }); err != nil {
		t.Fatalf("start after the backlog finished: %v", err)
	}
}

func TestTimeoutFailsAJobThatIgnoresItsContext(t *testing.T) {
	j := New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	j.timeout = 20 * time.Millisecond
	release := make(chan struct{})
	job, err := j.Start("alice", "Export notes", "/exports/x1", func(ctx context.Context, progress func(string)) error {
		<-release // never looks at ctx
		progress("late progress")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	got := wait(t, j, job.ID, "alice")
	if got.Status != Failed || got.Err != "took too long" || got.FinishedAt.IsZero() {
		t.Fatalf("timed-out job wrong: %+v", got)
	}
	// The stuck goroutine returning later must not overwrite the
	// verdict — not the status, and not the progress text.
	close(release)
	time.Sleep(30 * time.Millisecond)
	got, ok := j.Get(job.ID, "alice")
	if !ok || got.Status != Failed || got.Err != "took too long" {
		t.Fatalf("late return overwrote the timeout: ok=%v job=%+v", ok, got)
	}
	if got.Progress == "late progress" {
		t.Fatalf("late progress mutated a finished job: %+v", got)
	}
}

func TestTimeoutReachesAWellBehavedFn(t *testing.T) {
	j := New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	j.timeout = 20 * time.Millisecond
	stopped := make(chan struct{})
	job, err := j.Start("alice", "n", "", func(ctx context.Context, progress func(string)) error {
		<-ctx.Done()
		close(stopped)
		return ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("fn's context never expired")
	}
	// However the race between fn returning and the watchdog firing
	// lands, the owner reads the same verdict: DeadlineExceeded from a
	// well-behaved fn is normalized to the watchdog's message.
	got := wait(t, j, job.ID, "alice")
	if got.Status != Failed || got.Err != "took too long" {
		t.Fatalf("well-behaved timeout wrong: %+v", got)
	}
}

func TestTimeoutFreesTheOwnersSlots(t *testing.T) {
	j := New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	j.timeout = 20 * time.Millisecond
	release := make(chan struct{})
	defer close(release)
	var ids []string
	for i := 0; i < maxRunningPerOwner; i++ {
		job, err := j.Start("alice", "n", "", func(ctx context.Context, progress func(string)) error {
			<-release
			return nil
		})
		if err != nil {
			t.Fatalf("job %d refused under the cap: %v", i, err)
		}
		ids = append(ids, job.ID)
	}
	for _, id := range ids {
		wait(t, j, id, "alice")
	}
	if _, err := j.Start("alice", "n", "", func(ctx context.Context, progress func(string)) error { return nil }); err != nil {
		t.Fatalf("timed-out jobs still hold the cap: %v", err)
	}
}

func TestConcurrentStartAndGet(t *testing.T) {
	j := New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		// One owner per goroutine: this test exercises registry
		// concurrency, and 20 at once for one owner would trip the cap.
		owner := fmt.Sprintf("owner-%d", i)
		go func() {
			defer wg.Done()
			job, err := j.Start(owner, "n", "", func(ctx context.Context, progress func(string)) error {
				progress("working")
				return nil
			})
			if err != nil {
				t.Errorf("concurrent start refused: %v", err)
				return
			}
			// waitFor, not wait: this is a spawned goroutine, and
			// t.Fatalf there would call FailNow off the test goroutine.
			if _, err := waitFor(j, job.ID, owner); err != nil {
				t.Errorf("concurrent start: %v", err)
				return
			}
		}()
	}
	wg.Wait()
}
