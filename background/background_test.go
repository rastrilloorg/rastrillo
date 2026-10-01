package background

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The panic this package exists for, reproduced without waiting for
// it: a fuse armed before Stop must never fire after it, and work
// asked for after Stop must never start. In Tito Go a scheduled
// message's fuse outlived its test's database and its error log
// panicked the binary a minute later, naming an unrelated test.
func TestStopDisarmsAnArmedFuse(t *testing.T) {
	var g Group
	var ran atomic.Int32
	fuse := g.After(50*time.Millisecond, func() { ran.Add(1) })
	if fuse == nil {
		t.Fatal("After returned nil on a running Group")
	}
	g.Stop()
	// Disarmed, not merely ignored when it fires: a timer left armed
	// keeps its closure (and whatever it holds) alive until it does.
	if fuse.Stop() {
		t.Error("the fuse was still armed after Stop")
	}

	// And the kicks that land after the stop: a late request must not
	// reopen work the teardown has already accounted for.
	tm := g.After(time.Millisecond, func() { ran.Add(1) })
	if tm != nil {
		t.Error("After armed a timer on a stopped Group")
	}
	if tm.Stop() {
		t.Error("Stop on the nil Timer a stopped Group returns claimed to stop something")
	}
	if g.Go(func() { ran.Add(1) }) {
		t.Error("Go started work on a stopped Group")
	}
	g.Loop(context.Background(), time.Millisecond, func() { ran.Add(1) })

	time.Sleep(200 * time.Millisecond)
	if n := ran.Load(); n != 0 {
		t.Fatalf("%d pieces of work ran after Stop", n)
	}
}

// Stopping is not only disarming. Work already running holds the
// database, and teardown closes it next, so Stop has to outlast the
// work rather than race it.
func TestStopWaitsForWorkInFlight(t *testing.T) {
	var g Group
	started, release := make(chan struct{}), make(chan struct{})
	done := false
	g.Go(func() {
		close(started)
		<-release
		done = true
	})
	<-started

	stopped := make(chan struct{})
	go func() { g.Stop(); close(stopped) }()
	select {
	case <-stopped:
		t.Fatal("Stop returned while work was still running")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	<-stopped
	// Race-detector-visible: the write happened before Stop returned,
	// so reading it here is the whole guarantee.
	if !done {
		t.Fatal("Stop returned before the work finished")
	}
}

// A fuse that has already fired is work in flight like any other.
func TestStopWaitsForAFiredFuse(t *testing.T) {
	var g Group
	fired, release := make(chan struct{}), make(chan struct{})
	var finished atomic.Bool
	g.After(time.Millisecond, func() {
		close(fired)
		<-release
		finished.Store(true)
	})
	<-fired
	go func() { time.Sleep(50 * time.Millisecond); close(release) }()
	g.Stop()
	if !finished.Load() {
		t.Fatal("Stop returned while a fired fuse was still running")
	}
}

// A loop runs a pass at once, keeps going on its interval, and ends on
// Stop even when nobody cancels its context — the teardown that forgot
// to must still end in a stopped loop, not a wait for one.
func TestLoopEndsOnStopWithoutItsContext(t *testing.T) {
	var g Group
	var passes atomic.Int32
	first := make(chan struct{})
	var once sync.Once
	g.Loop(context.Background(), 5*time.Millisecond, func() {
		passes.Add(1)
		once.Do(func() { close(first) })
	})
	<-first
	time.Sleep(30 * time.Millisecond)

	stopped := make(chan struct{})
	go func() { g.Stop(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not end a loop whose context is never cancelled")
	}
	if n := passes.Load(); n < 2 {
		t.Fatalf("loop made %d passes, want the first plus at least one on the interval", n)
	}
	after := passes.Load()
	time.Sleep(30 * time.Millisecond)
	if passes.Load() != after {
		t.Fatal("the loop made a pass after Stop returned")
	}
}

// And on its context, for the owner that cancels first.
func TestLoopEndsOnItsContext(t *testing.T) {
	var g Group
	ctx, cancel := context.WithCancel(context.Background())
	ran := make(chan struct{}, 1)
	g.Loop(ctx, time.Hour, func() {
		select {
		case ran <- struct{}{}:
		default:
		}
	})
	<-ran
	cancel()
	stopped := make(chan struct{})
	go func() { g.Stop(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("a cancelled loop was still running at Stop")
	}
}

func TestStopIsIdempotentAndSafeWhenUnused(t *testing.T) {
	var g Group
	g.Stop()
	g.Stop()
	select {
	case <-g.Done():
	default:
		t.Fatal("Done is not closed after Stop")
	}
}

// The fence finds what it claims to, skips what it is told to, and
// ignores tests — and its documented blind spot stays a known one.
func TestUntracked(t *testing.T) {
	got, err := Untracked("testdata/untracked", "serve.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("Untracked found %d offences, want 2 (the go statement and time.AfterFunc):\n%s", len(got), strings.Join(got, "\n"))
	}
	if !strings.Contains(got[0], "work.go:9:") || !strings.Contains(got[0], "go statement") {
		t.Errorf("first offence = %q, want work.go's go statement", got[0])
	}
	if !strings.Contains(got[1], "work.go:10:") || !strings.Contains(got[1], "time.AfterFunc") {
		t.Errorf("second offence = %q, want work.go's time.AfterFunc", got[1])
	}

	got, err = Untracked("testdata/untracked")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Errorf("without the allow list Untracked found %d offences, want serve.go's too:\n%s", len(got), strings.Join(got, "\n"))
	}
}

// A timer cancelled on its own is forgotten by the Group at once, not
// when the Group stops. Otherwise a debounce re-armed on every request
// grows the Group by one timer, and one captured closure, per request.
func TestCancelledTimerIsForgotten(t *testing.T) {
	var g Group
	defer g.Stop()
	for i := 0; i < 1000; i++ {
		if !g.After(time.Hour, func() {}).Stop() {
			t.Fatal("Stop on an armed timer reported it had already fired")
		}
	}
	g.mu.Lock()
	n := len(g.timers)
	g.mu.Unlock()
	if n != 0 {
		t.Fatalf("the Group still holds %d cancelled timers", n)
	}
}
