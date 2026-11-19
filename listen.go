package rastrillo

import (
	"fmt"
	"net"
	"os"
	"strconv"
)

// listen resolves the platform's activation contract, exactly matching
// carlosframework/platform's testdata/echoapp: a systemd-activated
// listener (LISTEN_FDS=1, fd 3 — the carlos-app@.socket contract) takes
// priority; otherwise Socket (unix) or a TCP address (tcpAddress).
func listen(socket, addr string) (net.Listener, error) {
	count, _ := strconv.Atoi(os.Getenv("LISTEN_FDS"))
	if count >= 1 {
		pid := os.Getenv("LISTEN_PID")
		if pid == "" || pid == strconv.Itoa(os.Getpid()) {
			return net.FileListener(os.NewFile(3, "listen-fd"))
		}
	}
	if socket != "" {
		os.Remove(socket)
		return net.Listen("unix", socket)
	}
	addr, err := tcpAddress(addr, os.Getenv("PORT"))
	if err != nil {
		return nil, err
	}
	return net.Listen("tcp", addr)
}

// tcpAddress is addr when the app or -addr named one, else ":$PORT",
// else ":8080". All interfaces, because a host that sets PORT routes to
// the container from outside it. A PORT that is set but is not a port
// is refused: falling back to 8080 would fail the host's health check
// with nothing in the log to say why.
func tcpAddress(addr, port string) (string, error) {
	if addr != "" {
		return addr, nil
	}
	if port == "" {
		return ":8080", nil
	}
	number, err := strconv.ParseUint(port, 10, 16)
	if err != nil || number == 0 {
		return "", fmt.Errorf("PORT is %q, which is not a port: set it "+
			"to a number from 1 to 65535, or unset it and pass "+
			"-addr host:port", port)
	}
	return ":" + strconv.FormatUint(number, 10), nil
}
