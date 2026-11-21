package rastrillo

import (
	"net/http"
	"time"
)

// Default server timeouts. Both bound connection-level behaviour that no
// legitimate request depends on, which is what makes them safe to apply
// to every rastrillo app without an opt-out.
const (
	defaultReadHeaderTimeout = 20 * time.Second
	defaultIdleTimeout       = 120 * time.Second
)

// newServer builds the http.Server Serve runs.
//
// It exists because the zero-value &http.Server{Handler: handler} this
// replaced set no timeouts at all: a peer that stopped reading, or that
// vanished without a FIN, tied up a connection — and whatever the handler
// held — for as long as the process lived, with nothing anywhere in the
// stack able to fire.
//
// What this does and does not buy is worth being precise about, because
// the gap is where the real incidents live. It bounds two things at the
// connection level: a client that never finishes its headers, and an idle
// keep-alive connection. It does NOT bound a peer that stalls midway
// through a request body or a response — that needs an idle deadline
// around the streaming span itself, which only the handler can place,
// because only the handler knows where that span is. See ReadTimeout's
// doc comment for why the total-deadline fields are not that tool.
func newServer(opts Options, handler http.Handler) *http.Server {
	readHeader := opts.ReadHeaderTimeout
	if readHeader == 0 {
		readHeader = defaultReadHeaderTimeout
	}
	idle := opts.IdleTimeout
	if idle == 0 {
		idle = defaultIdleTimeout
	}
	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: readHeader,
		IdleTimeout:       idle,
		ReadTimeout:       opts.ReadTimeout,
		WriteTimeout:      opts.WriteTimeout,
	}
	// Registered here, with the server it belongs to, so Serve and the
	// tests that drive its shutdown cannot differ. Shutdown runs this
	// before it starts waiting, which is what keeps open streams from
	// eating the drain. Skipped without a hub: an app without live
	// pays nothing.
	if opts.Live != nil {
		srv.RegisterOnShutdown(opts.Live.Close)
	}
	return srv
}
