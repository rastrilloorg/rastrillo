//go:build browser

package harness

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"testing"

	"github.com/chromedp/chromedp"
)

// withPick hands the rig the IPv4 listeners pick binds.
func withPick(pick func() (net.Listener, error)) Option {
	return func(cfg *config) { cfg.pick = pick }
}

// whoPage answers every path with a page naming the server.
func whoPage(who string) http.Handler {
	return http.HandlerFunc(func(
		response http.ResponseWriter, _ *http.Request,
	) {
		response.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(response, `<!doctype html><html lang="en"><head>`+
			`<meta charset="utf-8"><title>who</title></head>`+
			`<body><p id="who">%s</p></body></html>`, who)
	})
}

func rigPort(t *testing.T, rig *Rig) string {
	t.Helper()
	_, port, err := net.SplitHostPort(rig.Origin[len("http://"):])
	if err != nil {
		t.Fatal(err)
	}
	return port
}

func needIPv6Loopback(t *testing.T) {
	t.Helper()
	ln, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Skipf("no IPv6 loopback here, so no stranger can hold it: %v",
			err)
	}
	ln.Close()
}

// Chrome tries ::1 first for localhost, so a rig that held 127.0.0.1
// alone left its port on ::1 for anyone to take.
func TestNoStrangerCanTakeTheRigsPortOnIPv6(t *testing.T) {
	needIPv6Loopback(t)
	rig := New(t, func(string) http.Handler { return whoPage("ours") })
	port := rigPort(t, rig)
	stranger, err := net.Listen("tcp6", net.JoinHostPort("::1", port))
	if err == nil {
		stranger.Close()
		t.Fatalf("a stranger bound [::1]:%s beside the rig", port)
	}
	rig.Run(chromedp.Navigate(rig.Origin + "/"))
	if who := Read(rig, chromedp.Text(chromedp.CSS("#who"))); who != "ours" {
		t.Fatalf("Chrome reached %q, want ours", who)
	}
}

// A stranger already on [::1]:P when the rig's IPv4 bind lands on P,
// which the kernel allows: the rig must give P up, not share it.
func TestTheRigGivesUpAPortAStrangerHoldsOnIPv6(t *testing.T) {
	needIPv6Loopback(t)
	stranger, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Fatal(err)
	}
	strangerServer := &http.Server{Handler: whoPage("wrong server")}
	go strangerServer.Serve(stranger)
	t.Cleanup(func() { strangerServer.Close() })
	taken := strconv.Itoa(stranger.Addr().(*net.TCPAddr).Port)

	picks := 0
	pick := func() (net.Listener, error) {
		picks++
		if picks == 1 {
			return net.Listen("tcp", net.JoinHostPort("127.0.0.1", taken))
		}
		return net.Listen("tcp", "127.0.0.1:0")
	}
	rig := New(t, func(string) http.Handler { return whoPage("ours") },
		withPick(pick))
	if port := rigPort(t, rig); port == taken {
		t.Errorf("the rig serves port %s, which a stranger holds on ::1",
			port)
	}
	rig.Run(chromedp.Navigate(rig.Origin + "/"))
	if who := Read(rig, chromedp.Text(chromedp.CSS("#who"))); who != "ours" {
		t.Fatalf("Chrome reached %q, want ours", who)
	}
}
