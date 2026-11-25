//go:build browser

package harness

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"syscall"
)

// listenLoopback binds one port on both loopback addresses and serves
// them as one listener.
//
// The origin is http://localhost:PORT, and Chrome connects to ::1 first
// for localhost, falling back to 127.0.0.1 only when ::1 refuses. So
// holding 127.0.0.1 alone is not enough: any other process may bind
// [::1]:PORT, and then Chrome's new connections reach it instead of the
// app. The kernel does hand a 127.0.0.1 port that is busy on ::1 to an
// ephemeral bind, and CI jobs on one worker share a network namespace.
//
// pick binds the IPv4 side; nil means a fresh ephemeral port. When ::1
// is taken on that port, the port is given up and another tried. A
// machine with no IPv6 loopback gets IPv4 alone: nothing can hold ::1
// there, and Chrome's attempt on it fails at once.
func listenLoopback(pick func() (net.Listener, error)) (net.Listener, error) {
	if pick == nil {
		pick = func() (net.Listener, error) {
			return net.Listen("tcp", "127.0.0.1:0")
		}
	}
	const attempts = 20
	for range attempts {
		v4, err := pick()
		if err != nil {
			return nil, err
		}
		port := strconv.Itoa(v4.Addr().(*net.TCPAddr).Port)
		v6, err := net.Listen("tcp6", net.JoinHostPort("::1", port))
		switch {
		case err == nil:
			return joinListeners(v4, v6), nil
		case errors.Is(err, syscall.EADDRINUSE):
			v4.Close()
		case errors.Is(err, syscall.EADDRNOTAVAIL),
			errors.Is(err, syscall.EAFNOSUPPORT):
			return v4, nil
		default:
			v4.Close()
			return nil, err
		}
	}
	return nil, fmt.Errorf("no port free on both 127.0.0.1 and ::1 "+
		"in %d attempts", attempts)
}

// pairListener accepts from both of its listeners and reports the IPv4
// one's address.
type pairListener struct {
	v4, v6   net.Listener
	accepted chan acceptResult
	closed   chan struct{}
	close    sync.Once
}

type acceptResult struct {
	conn net.Conn
	err  error
}

func joinListeners(v4, v6 net.Listener) net.Listener {
	pair := &pairListener{
		v4:       v4,
		v6:       v6,
		accepted: make(chan acceptResult),
		closed:   make(chan struct{}),
	}
	go pair.acceptFrom(v4)
	go pair.acceptFrom(v6)
	return pair
}

func (pair *pairListener) acceptFrom(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		select {
		case pair.accepted <- acceptResult{conn, err}:
		case <-pair.closed:
			if conn != nil {
				conn.Close()
			}
			return
		}
		if errors.Is(err, net.ErrClosed) {
			return
		}
	}
}

func (pair *pairListener) Accept() (net.Conn, error) {
	select {
	case result := <-pair.accepted:
		return result.conn, result.err
	case <-pair.closed:
		return nil, net.ErrClosed
	}
}

func (pair *pairListener) Close() error {
	pair.close.Do(func() { close(pair.closed) })
	return errors.Join(pair.v4.Close(), pair.v6.Close())
}

func (pair *pairListener) Addr() net.Addr { return pair.v4.Addr() }
