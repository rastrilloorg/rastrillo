package rastrillo

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/background"
)

// serveOnSocket runs Serve on a unix socket until it answers, and returns
// a client for it and the channel Serve's result arrives on. The socket
// sits in a short directory of its own, because t.TempDir's long names
// can pass the 108-byte limit on a socket path.
func serveOnSocket(
	t *testing.T, mux *http.ServeMux, stop <-chan struct{},
) (*http.Client, <-chan error) {
	t.Helper()
	directory, err := os.MkdirTemp("", "stop")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(directory) })
	socket := filepath.Join(directory, "app.sock")
	served := make(chan error, 1)
	go func() {
		served <- Serve(Options{
			Mux:    mux,
			Socket: socket,
			Stop:   stop,
			Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		})
	}()
	client := &http.Client{Transport: &http.Transport{
		DialContext: func(
			ctx context.Context, _, _ string,
		) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", socket)
		},
	}}
	deadline := time.Now().Add(5 * time.Second)
	for {
		response, err := client.Get("http://app/healthz")
		if err == nil {
			response.Body.Close()
			return client, served
		}
		select {
		case err := <-served:
			t.Fatalf("Serve returned before it answered: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("Serve never answered: %v", err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func assertStillServing(t *testing.T, served <-chan error) {
	t.Helper()
	select {
	case err := <-served:
		t.Fatalf("Serve returned early: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
}

func assertServeReturnedCleanly(t *testing.T, served <-chan error) {
	t.Helper()
	select {
	case err := <-served:
		if err != nil {
			t.Fatalf("Serve returned %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return within 5s of being stopped")
	}
}

func TestServeReturnsCleanlyWhenStopCloses(t *testing.T) {
	stop := make(chan struct{})
	_, served := serveOnSocket(t, http.NewServeMux(), stop)
	assertStillServing(t, served)
	close(stop)
	assertServeReturnedCleanly(t, served)
}

func TestServeLetsInFlightRequestsFinishWhenStopCloses(t *testing.T) {
	arrived, release := make(chan struct{}), make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /slow", func(
		response http.ResponseWriter, _ *http.Request,
	) {
		close(arrived)
		<-release
		io.WriteString(response, "finished")
	})
	stop := make(chan struct{})
	client, served := serveOnSocket(t, mux, stop)

	type answer struct {
		body string
		err  error
	}
	answered := make(chan answer, 1)
	go func() {
		response, err := client.Get("http://app/slow")
		if err != nil {
			answered <- answer{err: err}
			return
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		answered <- answer{string(body), err}
	}()
	<-arrived

	close(stop)
	assertStillServing(t, served)
	close(release)
	got := <-answered
	if got.err != nil || got.body != "finished" {
		t.Fatalf("in-flight request got %q, %v; want finished",
			got.body, got.err)
	}
	assertServeReturnedCleanly(t, served)
}

// A nil Stop is a channel that never closes, so Serve keeps stopping on
// a signal alone, as it did before Stop existed.
func TestServeWithoutStopStillStopsOnSIGTERM(t *testing.T) {
	_, served := serveOnSocket(t, http.NewServeMux(), nil)
	assertStillServing(t, served)
	process, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if err := process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	assertServeReturnedCleanly(t, served)
}

// The helper's environment variable, set only on the child process that
// TestServeKeepsSignalsCaughtUntilCleanupFinishes starts.
const stopHelperVariable = "RASTRILLO_STOP_SIGNAL_HELPER"

// A SIGTERM that lands while Serve waits for background work, after a
// Stop began the shutdown, must not kill the process: the work and the
// database close would be cut short. Default handling of SIGTERM kills
// the whole process, so this runs Serve in a child of its own.
func TestServeKeepsSignalsCaughtUntilCleanupFinishes(t *testing.T) {
	child := exec.Command(os.Args[0],
		"-test.run=^TestServeStopHelperProcess$")
	child.Env = append(os.Environ(), stopHelperVariable+"=1")
	release, err := child.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	lines := make(chan string)
	go func() {
		scanner := bufio.NewScanner(output)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		close(lines)
	}()
	awaitLine(t, lines, "cleanup waiting")

	if err := child.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	// Default handling would end the child well inside this pause.
	time.Sleep(200 * time.Millisecond)
	io.WriteString(release, "release\n")

	awaitLine(t, lines, "serve returned <nil>")
	for range lines {
	}
	if err := child.Wait(); err != nil {
		t.Fatalf("the child did not exit cleanly: %v", err)
	}
}

func awaitLine(t *testing.T, lines <-chan string, want string) {
	t.Helper()
	timeout := time.After(10 * time.Second)
	for {
		select {
		case line, open := <-lines:
			if !open {
				t.Fatalf("the child ended before printing %q", want)
			}
			if line == want {
				return
			}
		case <-timeout:
			t.Fatalf("the child never printed %q", want)
		}
	}
}

// TestServeStopHelperProcess is the child the test above starts. Stop is
// closed before Serve begins, so Serve drains at once and then waits in
// Background.Stop for a job that holds on until the parent releases it.
func TestServeStopHelperProcess(t *testing.T) {
	if os.Getenv(stopHelperVariable) != "1" {
		t.Skip("runs only as the child of " +
			"TestServeKeepsSignalsCaughtUntilCleanupFinishes")
	}
	var group background.Group
	group.Go(func() {
		<-group.Done()
		fmt.Println("cleanup waiting")
		bufio.NewReader(os.Stdin).ReadString('\n')
	})
	stop := make(chan struct{})
	close(stop)
	err := Serve(Options{
		Router: func(*sql.DB) (*http.ServeMux, error) {
			return http.NewServeMux(), nil
		},
		DBPath:     filepath.Join(t.TempDir(), "app.db"),
		Addr:       "127.0.0.1:0",
		Stop:       stop,
		Background: &group,
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	fmt.Println("serve returned", err)
}
