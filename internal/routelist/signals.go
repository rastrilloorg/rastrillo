package routelist

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"
)

// Interruptible is a context cancelled by the first SIGINT or SIGTERM.
// Find, List and Unmounted stop on it and clean up, which a process
// killed by the signal cannot do: the children they start are in a
// process group of their own, which a Ctrl-C at the terminal does not
// reach. Not signal.NotifyContext, whose cause does not say which
// signal came: EndBy needs to know. stop releases the signals.
func Interruptible() (ctx context.Context, stop func()) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	ctx, cancel := context.WithCancelCause(context.Background())
	go func() {
		select {
		case received := <-signals:
			cancel(signalled{received})
		case <-ctx.Done():
		}
	}()
	return ctx, func() {
		signal.Stop(signals)
		cancel(nil)
	}
}

// Interruption is the signal that cancelled an Interruptible context,
// or nil if none has.
func Interruption(ctx context.Context) os.Signal {
	var cause signalled
	if errors.As(context.Cause(ctx), &cause) {
		return cause.received
	}
	return nil
}

type signalled struct{ received os.Signal }

func (cause signalled) Error() string { return cause.received.String() }
