package rastrillo

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// serveThenClose listens, serves until stopped, and then runs closeAll,
// which stops Background and closes the database.
//
// SIGTERM and SIGINT stay caught from before the server answers until
// closeAll has returned. A signal that lands while Background drains,
// after Options.Stop began the shutdown, must not kill the process with
// the work unfinished and the database open. Released on return, so a
// process that goes on after a Stop gets the default handling back.
func serveThenClose(
	opts Options, handler http.Handler, closeAll func() error,
	logger *slog.Logger,
) error {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(signals)
	defer closeAll()

	listener, err := listen(opts.Socket, opts.Addr)
	if err != nil {
		return fmt.Errorf("rastrillo: listen: %w", err)
	}
	logger.Info("rastrillo: serving",
		"addr", listener.Addr().String(), "version", BuildVersion)
	return serveUntilStopped(newServer(opts, handler), listener,
		signals, opts.Stop, logger)
}

// serveUntilStopped serves on listener until the server fails, or until
// a signal or a closed stop shuts it down gracefully. A nil stop never
// closes. It returns Shutdown's error, context.DeadlineExceeded, when a
// request outlives the 10-second drain.
func serveUntilStopped(
	server *http.Server, listener net.Listener,
	signals <-chan os.Signal, stop <-chan struct{}, logger *slog.Logger,
) error {
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()

	select {
	case err := <-served:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-signals:
		logger.Info("rastrillo: shutting down")
	case <-stop:
		logger.Info("rastrillo: shutting down", "reason", "Options.Stop")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return server.Shutdown(ctx)
}
