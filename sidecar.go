package rastrillo

import (
	"context"
	"errors"
	"log/slog"
	"os/signal"
	"syscall"
	"time"
)

// sidecarPollInterval is how long the sidecar loop sleeps after a pass
// that reports no scheduled work (a zero next-due time). A sidecar with
// real scheduled work names its own wake time; one without is just
// checking for new deliveries, and once a minute is the family's polling
// floor.
const sidecarPollInterval = time.Minute

// sidecarMaxBackoff caps the error-retry backoff. A sidecar never dies
// on a pass error — the platform would just respawn it into the same
// failure — it waits and retries, and five minutes keeps a dead
// dependency from turning the loop into a busy-wait.
const sidecarMaxBackoff = 5 * time.Minute

// sidecarInitialBackoff is the first error-retry delay. A var, not a
// const, so the loop's error path is testable without real seconds.
var sidecarInitialBackoff = time.Second

// isSidecarInvocation reports whether argv asks for the sidecar loop:
// the platform's exec backend spawns `<live binary> sidecar run` when
// the host's sidecar env file exists (carlosframework/platform,
// internal/activator/backend_exec.go). Only that exact shape matches —
// anything trailing is a contract drift better rejected loudly by the
// normal flag parsing than absorbed here.
func isSidecarInvocation(args []string) bool {
	return len(args) == 2 && args[0] == "sidecar" && args[1] == "run"
}

// runSidecar is the loop Run enters for a `sidecar run` invocation: call
// the app's pass, sleep until it says it is next due (or the poll
// interval when it reports nothing scheduled), back off on error, and
// exit cleanly on SIGTERM/SIGINT — the platform SIGTERMs sidecars first
// at sleep, before the instance.
func runSidecar(opts Options) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	return sidecarLoop(ctx, opts)
}

// sidecarLoop is runSidecar minus the signal wiring, so the loop is
// testable with an ordinary cancelable context.
func sidecarLoop(ctx context.Context, opts Options) error {
	if opts.Sidecar == nil {
		return errors.New("rastrillo: invoked as `sidecar run` but Options.Sidecar is nil — this app declares no sidecar")
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	logger.Info("rastrillo: sidecar running", "version", BuildVersion)
	backoff := sidecarInitialBackoff
	for {
		due, err := opts.Sidecar(ctx)

		var wait time.Duration
		switch {
		case ctx.Err() != nil:
			logger.Info("rastrillo: sidecar shutting down")
			return nil
		case err != nil:
			logger.Error("rastrillo: sidecar pass failed", "err", err, "retry_in", backoff)
			wait = backoff
			backoff = min(backoff*2, sidecarMaxBackoff)
		default:
			backoff = sidecarInitialBackoff
			wait = sidecarPollInterval
			if !due.IsZero() {
				wait = max(time.Until(due), 0)
			}
		}

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			logger.Info("rastrillo: sidecar shutting down")
			return nil
		case <-timer.C:
		}
	}
}
