package rastrillo

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestIsSidecarInvocation(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{[]string{"sidecar", "run"}, true},
		{[]string{}, false},
		{[]string{"serve"}, false},
		{[]string{"sidecar"}, false},
		{[]string{"sidecar", "run", "extra"}, false},
		{[]string{"run", "sidecar"}, false},
		{[]string{"-socket", "/x"}, false},
	}
	for _, c := range cases {
		if got := isSidecarInvocation(c.args); got != c.want {
			t.Errorf("isSidecarInvocation(%q) = %v, want %v", c.args, got, c.want)
		}
	}
}

func TestSidecarLoopRequiresSidecar(t *testing.T) {
	err := sidecarLoop(context.Background(), Options{})
	if err == nil || !strings.Contains(err.Error(), "Options.Sidecar is nil") {
		t.Fatalf("nil Sidecar: %v, want a loud error", err)
	}
}

func TestSidecarLoopRunsPassesUntilCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	err := sidecarLoop(ctx, Options{
		Sidecar: func(context.Context) (time.Time, error) {
			calls++
			if calls == 3 {
				cancel()
			}
			return time.Now(), nil // due now: re-run immediately
		},
	})
	if err != nil {
		t.Fatalf("sidecarLoop: %v", err)
	}
	if calls != 3 {
		t.Fatalf("passes = %d, want 3", calls)
	}
}

func TestSidecarLoopRetriesAfterError(t *testing.T) {
	old := sidecarInitialBackoff
	sidecarInitialBackoff = time.Millisecond
	defer func() { sidecarInitialBackoff = old }()

	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	err := sidecarLoop(ctx, Options{
		Sidecar: func(context.Context) (time.Time, error) {
			calls++
			if calls >= 2 {
				cancel()
				return time.Time{}, nil
			}
			return time.Time{}, errors.New("flaky dependency")
		},
	})
	if err != nil {
		t.Fatalf("sidecarLoop returned the pass error; passes must retry, not kill the loop: %v", err)
	}
	if calls < 2 {
		t.Fatalf("passes = %d, want the loop to survive the error and retry", calls)
	}
}

func TestSidecarLoopStopsPromptlyOnCancelDuringWait(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- sidecarLoop(ctx, Options{
			// Nothing scheduled: the loop would sleep a full poll
			// interval — cancellation must cut that short.
			Sidecar: func(context.Context) (time.Time, error) { return time.Time{}, nil },
		})
	}()
	time.Sleep(10 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("sidecarLoop: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("sidecarLoop did not stop on cancel during its wait")
	}
}
