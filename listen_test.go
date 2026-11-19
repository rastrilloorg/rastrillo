package rastrillo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Render, Railway, Heroku-likes and Cloud Run tell an app where to
// listen with $PORT alone, so an app with no -addr must follow it.
func TestTCPAddressPrecedence(t *testing.T) {
	tests := []struct {
		name, addr, port, want string
	}{
		{"nothing set keeps the default", "", "", ":8080"},
		{"PORT is used when no address was given", "", "10000", ":10000"},
		{"an explicit address wins over PORT", "127.0.0.1:9000", "10000",
			"127.0.0.1:9000"},
		{"an explicit address wins over a bad PORT", ":9000", "web", ":9000"},
		{"the highest port is allowed", "", "65535", ":65535"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := tcpAddress(test.addr, test.port)
			if err != nil {
				t.Fatalf("tcpAddress(%q, %q): %v", test.addr, test.port, err)
			}
			if got != test.want {
				t.Errorf("tcpAddress(%q, %q) = %q, want %q",
					test.addr, test.port, got, test.want)
			}
		})
	}
}

// A PORT that is not a port is a host misconfigured, and listening on
// :8080 instead would fail its health check with no clue why.
func TestTCPAddressRefusesAPortThatIsNotOne(t *testing.T) {
	for _, port := range []string{
		"web", "0", "65536", "-1", "+80", " 80", "80 ", "80.0", "8080/tcp",
	} {
		_, err := tcpAddress("", port)
		if err == nil {
			t.Errorf("tcpAddress(\"\", %q): want an error, got nil", port)
			continue
		}
		if !strings.Contains(err.Error(), "PORT") ||
			!strings.Contains(err.Error(), "-addr") {
			t.Errorf("tcpAddress(\"\", %q) error = %q, want it to name "+
				"PORT and the -addr fix", port, err)
		}
	}
}

func TestListenReadsPORT(t *testing.T) {
	t.Setenv("LISTEN_FDS", "")
	t.Setenv("PORT", "web")
	listener, err := listen("", "")
	if err == nil {
		listener.Close()
		t.Fatal("listen with PORT=web: want an error, got a listener")
	}
	if !strings.Contains(err.Error(), `"web"`) {
		t.Errorf("listen error = %q, want it to quote the bad PORT", err)
	}
}

// CARLOS hands an exec child its socket; a PORT left in the environment
// must not move it, or even be checked.
func TestListenPrefersTheSocketToPORT(t *testing.T) {
	t.Setenv("LISTEN_FDS", "")
	t.Setenv("PORT", "web")
	directory, err := os.MkdirTemp("", "port")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(directory) })
	socket := filepath.Join(directory, "app.sock")
	listener, err := listen(socket, "")
	if err != nil {
		t.Fatalf("listen(%q): %v", socket, err)
	}
	defer listener.Close()
	if network := listener.Addr().Network(); network != "unix" {
		t.Errorf("listener network = %q, want unix", network)
	}
}
