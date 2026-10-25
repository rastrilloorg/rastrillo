package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/carlos"
	"amadan.net/rastrillo/rastrillo/provision/provisiontest"
)

// servesAsTheBinary lets the test binary stand in for the built one: the
// platform's start-up test below runs main() as a child process, so it
// exercises the real argv path without a second go build.
const servesAsTheBinary = "PROVISION_LIVE_TEST_AS_BINARY"

func TestMain(m *testing.M) {
	if os.Getenv(servesAsTheBinary) != "" {
		main()
		return
	}
	os.Exit(m.Run())
}

const (
	account = "bdf"
	app     = "provision-live"
)

var domain = account + "." + provisiontest.AppsDomain

// The check is only worth running on Carloku if it passes against a
// platform that answers as the handlers were read. This drives it end to
// end on the fake, through the injected client, so it never reaches
// https://console.test over a network.
func TestCheckPassesOnTheFake(t *testing.T) {
	p := provisiontest.New()
	p.Enable(account, app, domain)
	token := p.Credential(account, app, "instance")
	p.Serve(account, app, provisiontest.Tenant(""))
	p.Unready(1)

	cfg := checkConfig{
		Console: p.Console(), Account: account, App: app, Token: token, Domain: domain,
		Every: time.Millisecond,
	}
	res, err := check(context.Background(), cfg, p.Client())
	if err != nil {
		t.Fatalf("check: %v\n%+v", err, res)
	}
	if !res.OK || res.Repeat != "host taken" || res.Resume != "landed" {
		t.Errorf("ok %v, repeat %q, resume %q; want true, %q, %q", res.OK, res.Repeat, res.Resume, "host taken", "landed")
	}
	if !strings.HasSuffix(res.Host, "."+domain) || !strings.HasPrefix(res.Host, "live-") {
		t.Errorf("host = %q, want live-<6>.%s", res.Host, domain)
	}
	if len(res.Probes) != 2 || res.Probes[0].Status != 503 || res.Probes[1].Status != 200 || res.Probes[1].Instance != res.Host {
		t.Errorf("probes = %+v, want the pending 503 and then a 200 naming %s", res.Probes, res.Host)
	}
	if len(res.Exchanges) == 0 || res.Exchanges[0].Method != "POST" || res.Exchanges[0].Status != 201 {
		t.Errorf("exchanges = %+v, want the first to be the create's 201", res.Exchanges)
	}
	if res.Create != "created" {
		t.Errorf("create = %q, want created", res.Create)
	}

	out, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), token) {
		t.Errorf("the token is in the output, which is pasted into a thread: %s", out)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(out, &keys); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"ok", "host", "exchanges", "probes", "repeat", "resume"} {
		if _, ok := keys[k]; !ok {
			t.Errorf("output has no %q", k)
		}
	}
}

// The platform starts an instance as `<binary> --socket <path> --db <path>`
// and reads its /api/version through that socket. A binary that does not
// serve there, or does not name its host, never reads ready on Carloku.
func TestTheDefaultPathServesAsThePlatformStartsIt(t *testing.T) {
	// A short directory: a unix socket path is limited to about 100 bytes,
	// and t.TempDir includes the test's name.
	dir, err := os.MkdirTemp("", "pl")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "s.sock")
	const host = "live-k3x9qa." + "bdf.oncarlos.com"

	var stderr bytes.Buffer
	cmd := exec.Command(os.Args[0], "--socket", sock, "--db", filepath.Join(dir, "d.db"))
	cmd.Env = append(os.Environ(), servesAsTheBinary+"=1", "CARLOS_HOST="+host)
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	t.Cleanup(func() {
		cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-exited:
		case <-time.After(15 * time.Second):
			cmd.Process.Kill()
			<-exited
		}
	})

	hc := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", sock)
		},
	}}
	deadline := time.Now().Add(20 * time.Second)
	for {
		resp, err := hc.Get("http://unix/api/version")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("GET /api/version = %d, want 200\n%s", resp.StatusCode, stderr.String())
			}
			if got := resp.Header.Get(carlos.HostHeader); got != host {
				t.Fatalf("%s = %q, want %q: Ready cannot tell this instance from a catch-all", carlos.HostHeader, got, host)
			}
			return
		}
		select {
		case e := <-exited:
			exited <- e
			t.Fatalf("the binary exited before it served: %v\n%s", e, stderr.String())
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("nothing served on %s in 20s: %v\n%s", sock, err, stderr.String())
		}
		time.Sleep(50 * time.Millisecond)
	}
}
