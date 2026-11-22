package auth

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"sync/atomic"
	"testing"
)

func TestPublicUnicast(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1": false, "::1": false, "10.1.2.3": false,
		"172.16.0.1": false, "192.168.1.1": false,
		"169.254.1.1": false, "fe80::1": false, "fc00::1": false,
		"100.64.0.1": false, "100.127.255.254": false,
		"0.0.0.0": false, "::": false, "224.0.0.1": false,
		"ff02::1": false, "::ffff:127.0.0.1": false,
		"::ffff:100.64.0.1": false,
		"1.1.1.1":           true, "2606:4700::1111": true,
		"100.128.0.1": true,
	}
	for text, want := range cases {
		address := netip.MustParseAddr(text)
		if got := publicUnicast(address); got != want {
			t.Errorf("publicUnicast(%s) = %v, want %v", text, got, want)
		}
	}
}

func TestDefaultKeymailClientRefusesLoopback(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(
		func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer server.Close()
	auth, _ := newTestAuth(t, nil)
	clients := map[string]*http.Client{
		"classifier": auth.flow.Classifier.HTTP,
		"exchange":   auth.exchangeHTTP,
	}
	for name, client := range clients {
		if client == nil {
			t.Fatalf("%s client is nil", name)
		}
		response, err := client.Get(server.URL)
		if err == nil {
			response.Body.Close()
			t.Errorf("%s client reached a loopback server", name)
		}
	}
	if hits.Load() != 0 {
		t.Errorf("loopback server saw %d requests, want 0", hits.Load())
	}
}

func TestListedServersKeepTheGuard(t *testing.T) {
	auth, _ := newTestAuth(t, func(config *Config) {
		config.KeymailServers = []string{"keymail.test"}
	})
	for name, client := range map[string]*http.Client{
		"classifier": auth.flow.Classifier.HTTP,
		"exchange":   auth.exchangeHTTP,
	} {
		if client == nil || client.Transport != http.RoundTripper(auth.guard) {
			t.Errorf("%s client does not use the host guard", name)
		}
	}
}

// counted is a loopback server that counts the requests reaching it;
// the guard must keep that at zero.
func counted(t *testing.T, handler http.HandlerFunc) (
	*httptest.Server, *atomic.Int32,
) {
	t.Helper()
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(
		func(response http.ResponseWriter, request *http.Request) {
			hits.Add(1)
			handler(response, request)
		}))
	t.Cleanup(server.Close)
	return server, &hits
}

// standIn puts a stand-in for DNS in front of client's guarded dialer:
// a name in answers is dialed at the address it maps to, through the
// guard, as if a resolver had returned it. A name in public is dialed
// past the guard, as a server at a public address would be, since a
// test can only serve from loopback.
func standIn(client *http.Client, answers, public map[string]string) {
	transport := client.Transport.(*http.Transport)
	guarded := transport.DialContext
	var open net.Dialer
	transport.DialContext = func(
		ctx context.Context, network, address string,
	) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		if served, ok := public[host]; ok {
			return open.DialContext(ctx, network, served)
		}
		if answer, ok := answers[host]; ok {
			address = net.JoinHostPort(answer, port)
		}
		return guarded(ctx, network, address)
	}
}

// keymailClients are the two clients an app that lists no servers
// dials strangers' servers with.
func keymailClients(t *testing.T) map[string]*http.Client {
	t.Helper()
	auth, _ := newTestAuth(t, nil)
	return map[string]*http.Client{
		"classifier": auth.flow.Classifier.HTTP,
		"exchange":   auth.exchangeHTTP,
	}
}

// A name is no way past the guard: it checks the address the name
// resolved to, whether the system's resolver answered (localhost) or a
// domain's own DNS did (keymail.example).
func TestKeymailClientsRefuseANameThatResolvesToLoopback(t *testing.T) {
	server, hits := counted(t, func(http.ResponseWriter, *http.Request) {})
	_, port, _ := net.SplitHostPort(server.Listener.Addr().String())
	for name, client := range keymailClients(t) {
		standIn(client, map[string]string{"keymail.example": "127.0.0.1"},
			nil)
		for _, host := range []string{"localhost", "keymail.example"} {
			response, err := client.Get("http://" +
				net.JoinHostPort(host, port) + "/")
			if err == nil {
				response.Body.Close()
				t.Errorf("%s client reached loopback as %s", name, host)
			}
		}
	}
	if hits.Load() != 0 {
		t.Errorf("loopback server saw %d requests, want 0", hits.Load())
	}
}

// A public server cannot send the client on to this machine: the
// redirect is dialed through the same guard.
func TestKeymailClientsRefuseARedirectToLoopback(t *testing.T) {
	inside, insideHits := counted(t,
		func(http.ResponseWriter, *http.Request) {})
	outside, outsideHits := counted(t,
		func(response http.ResponseWriter, request *http.Request) {
			http.Redirect(response, request, inside.URL+"/token",
				http.StatusFound)
		})
	for name, client := range keymailClients(t) {
		standIn(client, nil, map[string]string{
			"public.example": outside.Listener.Addr().String()})
		response, err := client.Get("http://public.example/token")
		if err == nil {
			response.Body.Close()
			t.Errorf("%s client followed a redirect to loopback", name)
		}
	}
	if outsideHits.Load() != 2 || insideHits.Load() != 0 {
		t.Errorf("public server saw %d requests, loopback %d; want 2 and 0",
			outsideHits.Load(), insideHits.Load())
	}
}
