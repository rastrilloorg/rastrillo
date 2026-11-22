package oauth_test

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/oauth"
)

func loopbackOK(address netip.Addr) bool {
	return address == netip.MustParseAddr("127.0.0.1") ||
		oauth.PublicAddr(address)
}

func resolveTo(
	addrs ...string,
) func(context.Context, string) ([]netip.Addr, error) {
	return func(context.Context, string) ([]netip.Addr, error) {
		var out []netip.Addr
		for _, address := range addrs {
			out = append(out, netip.MustParseAddr(address))
		}
		return out, nil
	}
}

type docHandler func(response http.ResponseWriter, request *http.Request,
	id string)

// docServer serves documents over TLS at https://example.com:PORT and
// returns a fetcher that trusts it, the base URL, and a hit counter.
func docServer(
	t *testing.T, handler docHandler,
) (*oauth.Fetcher, string, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	var base string
	srv := httptest.NewTLSServer(http.HandlerFunc(
		func(response http.ResponseWriter, request *http.Request) {
			hits.Add(1)
			handler(response, request, base+request.URL.Path)
		}))
	t.Cleanup(srv.Close)
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	base = "https://example.com:" + port
	roots := x509.NewCertPool()
	roots.AddCert(srv.Certificate())
	fetcher := oauth.NewFetcher()
	fetcher.Trust(roots, resolveTo("127.0.0.1"), loopbackOK)
	return fetcher, base, &hits
}

func writeDoc(response http.ResponseWriter, doc map[string]any) {
	response.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(response).Encode(doc)
}

func goodDoc(response http.ResponseWriter, _ *http.Request, id string) {
	writeDoc(response, map[string]any{
		"client_id": id, "client_name": "Agent",
		"redirect_uris": []string{"https://agent.example/cb"},
	})
}

func TestFetchValidDocument(t *testing.T) {
	fetcher, base, _ := docServer(t, goodDoc)
	client, err := fetcher.Fetch(context.Background(), base+"/client.json")
	if err != nil {
		t.Fatal(err)
	}
	if client.ID != base+"/client.json" || client.Name != "Agent" ||
		!client.Document ||
		len(client.Redirects) != 1 ||
		client.Redirects[0] != "https://agent.example/cb" {
		t.Fatalf("client = %+v", client)
	}
}

func TestFetchAcceptsAuthMethodNone(t *testing.T) {
	fetcher, base, _ := docServer(t, func(
		response http.ResponseWriter, _ *http.Request, id string,
	) {
		writeDoc(response, map[string]any{
			"client_id": id, "client_name": "Agent",
			"redirect_uris":              []string{"https://agent.example/cb"},
			"token_endpoint_auth_method": "none",
		})
	})
	if _, err := fetcher.Fetch(context.Background(), base+"/c"); err != nil {
		t.Fatal(err)
	}
}

func TestFetchRefusesBadDocuments(t *testing.T) {
	tests := map[string]docHandler{
		"id mismatch": func(response http.ResponseWriter, _ *http.Request,
			_ string) {
			writeDoc(response, map[string]any{
				"client_id": "https://other.example/c", "client_name": "A",
				"redirect_uris": []string{"https://a.example/cb"},
			})
		},
		"missing name": func(
			response http.ResponseWriter, _ *http.Request, id string,
		) {
			writeDoc(response, map[string]any{"client_id": id,
				"redirect_uris": []string{"https://a.example/cb"}})
		},
		"no redirects": func(
			response http.ResponseWriter, _ *http.Request, id string,
		) {
			writeDoc(
				response,
				map[string]any{"client_id": id, "client_name": "A"},
			)
		},
		"long name": func(response http.ResponseWriter, _ *http.Request,
			id string) {
			writeDoc(response, map[string]any{
				"client_id": id, "client_name": strings.Repeat("a", 101),
				"redirect_uris": []string{"https://a.example/cb"},
			})
		},
		"bidi name": func(response http.ResponseWriter, _ *http.Request,
			id string) {
			writeDoc(response, map[string]any{
				"client_id": id, "client_name": "Agent\u202eevil",
				"redirect_uris": []string{"https://a.example/cb"},
			})
		},
		"not json": func(response http.ResponseWriter, _ *http.Request,
			_ string) {
			_, _ = response.Write([]byte("<html>"))
		},
		"over 64 KiB": func(response http.ResponseWriter, _ *http.Request,
			id string) {
			writeDoc(response, map[string]any{
				"client_id": id, "client_name": strings.Repeat("a", 64<<10),
				"redirect_uris": []string{"https://a.example/cb"},
			})
		},
		"not found": func(response http.ResponseWriter, _ *http.Request,
			_ string) {
			http.NotFound(response, nil)
		},
	}
	// Only public clients are served, so a document asking for client
	// authentication, or carrying a secret, is refused rather than
	// silently treated as public.
	for name, extra := range map[string]map[string]any{
		"private_key_jwt": {"token_endpoint_auth_method": "private_key_jwt"},
		"client_secret_basic": {
			"token_endpoint_auth_method": "client_secret_basic",
		},
		"empty auth method": {"token_endpoint_auth_method": ""},
		"secret":            {"client_secret": "s3cret"},
		"null secret":       {"client_secret": nil},
		"secret expiry":     {"client_secret_expires_at": 0},
	} {
		tests[name] = func(
			response http.ResponseWriter, _ *http.Request, id string,
		) {
			doc := map[string]any{
				"client_id": id, "client_name": "A",
				"redirect_uris": []string{"https://a.example/cb"},
			}
			for key, value := range extra {
				doc[key] = value
			}
			writeDoc(response, doc)
		}
	}
	for name, handler := range tests {
		t.Run(name, func(t *testing.T) {
			fetcher, base, _ := docServer(t, handler)
			if _, err := fetcher.Fetch(context.Background(),
				base+"/c"); err == nil {
				t.Fatal("document accepted")
			}
		})
	}
}

func TestFetchRefusesBadURLs(t *testing.T) {
	fetcher, base, hits := docServer(t, goodDoc)
	for _, address := range []string{
		strings.Replace(base, "https", "http", 1) + "/c",
		base, base + "/c#frag", "https://u@example.com/c", "::",
	} {
		if _, err := fetcher.Fetch(context.Background(), address); err == nil {
			t.Fatalf("%q accepted", address)
		}
	}
	if hits.Load() != 0 {
		t.Fatal("a refused URL reached the server")
	}
}

func TestFetchDoesNotFollowRedirects(t *testing.T) {
	var target atomic.Int32
	fetcher, base, _ := docServer(t,
		func(response http.ResponseWriter, request *http.Request, id string) {
			if request.URL.Path == "/target" {
				target.Add(1)
				goodDoc(response, request, id)
				return
			}
			http.Redirect(response, request, "/target", http.StatusFound)
		})
	if _, err := fetcher.Fetch(context.Background(), base+"/c"); err == nil {
		t.Fatal("redirect accepted")
	}
	if target.Load() != 0 {
		t.Fatal("redirect followed")
	}
}

func TestFetchCache(t *testing.T) {
	now := t0
	cached := func(cc string) docHandler {
		return func(response http.ResponseWriter, request *http.Request,
			id string) {
			if cc != "" {
				response.Header().Set("Cache-Control", cc)
			}
			goodDoc(response, request, id)
		}
	}
	fetch := func(t *testing.T, cc string, advance time.Duration) int32 {
		t.Helper()
		fetcher, base, hits := docServer(t, cached(cc))
		now = t0
		fetcher.SetClock(func() time.Time { return now })
		for _, at := range []time.Duration{0, advance} {
			now = t0.Add(at)
			if _, err := fetcher.Fetch(context.Background(),
				base+"/c"); err != nil {
				t.Fatal(err)
			}
		}
		return hits.Load()
	}
	if got := fetch(t, "max-age=60", 59*time.Second); got != 1 {
		t.Errorf("fresh: %d fetches, want 1", got)
	}
	if got := fetch(t, "max-age=60", 61*time.Second); got != 2 {
		t.Errorf("stale: %d fetches, want 2", got)
	}
	if got := fetch(t, "max-age=999999", 23*time.Hour); got != 1 {
		t.Errorf("long, inside 24h: %d fetches, want 1", got)
	}
	if got := fetch(t, "max-age=999999", 25*time.Hour); got != 2 {
		t.Errorf("long, past 24h: %d fetches, want 2", got)
	}
	if got := fetch(t, "", time.Second); got != 2 {
		t.Errorf("no max-age: %d fetches, want 2", got)
	}
	if got := fetch(t, "no-store, max-age=60", time.Second); got != 2 {
		t.Errorf("no-store: %d fetches, want 2", got)
	}
}

func TestFetchDoesNotCacheFailures(t *testing.T) {
	var fail atomic.Bool
	fail.Store(true)
	fetcher, base, hits := docServer(t,
		func(response http.ResponseWriter, request *http.Request, id string) {
			response.Header().Set("Cache-Control", "max-age=600")
			if fail.Load() {
				http.Error(response, "down", http.StatusInternalServerError)
				return
			}
			goodDoc(response, request, id)
		})
	if _, err := fetcher.Fetch(context.Background(), base+"/c"); err == nil {
		t.Fatal("failure accepted")
	}
	fail.Store(false)
	if _, err := fetcher.Fetch(context.Background(), base+"/c"); err != nil {
		t.Fatalf("the failure was cached: %v", err)
	}
	if hits.Load() != 2 {
		t.Fatalf("%d fetches, want 2", hits.Load())
	}
}

func TestFetchRefusesUnsafeAddresses(t *testing.T) {
	for _, addr := range []string{
		"127.0.0.1", "10.0.0.1", "100.64.0.1", "169.254.169.254", "::1",
		"fd00::1", "fe80::1", "::ffff:127.0.0.1", "::ffff:10.0.0.1",
		"192.168.1.1", "172.16.0.1", "0.0.0.0", "::", "64:ff9b:1::a00:1",
		"::a00:1", "2002:a00:1::1", "fec0::1",
	} {
		t.Run(addr, func(t *testing.T) {
			fetcher, base, hits := docServer(t, goodDoc)
			fetcher.Trust(nil, resolveTo(addr), oauth.PublicAddr)
			_, err := fetcher.Fetch(context.Background(), base+"/c")
			if !errors.Is(err, oauth.ErrUnsafeAddress) {
				t.Fatalf("err = %v, want an unsafe address", err)
			}
			if hits.Load() != 0 {
				t.Fatal("the server was reached")
			}
		})
	}
}

func TestPublicAddr(t *testing.T) {
	for _, address := range []string{"8.8.8.8", "1.1.1.1", "2606:4700::1111"} {
		if !oauth.PublicAddr(netip.MustParseAddr(address)) {
			t.Errorf("%s refused", address)
		}
	}
	for _, address := range []string{
		"0.1.2.3", "192.0.0.8", "192.0.2.1", "198.18.0.1", "198.19.255.1",
		"198.51.100.7", "203.0.113.9", "240.0.0.1", "255.255.255.255",
		"64:ff9b::a00:1", "64:ff9b:1::a00:1", "::a00:1", "2002:a00:1::1",
		"fec0::1", "2001:db8::1", "fc00::1", "fe80::1", "::ffff:10.0.0.1",
	} {
		if oauth.PublicAddr(netip.MustParseAddr(address)) {
			t.Errorf("%s accepted", address)
		}
	}
}

func TestFetchRefusesRebinding(t *testing.T) {
	fetcher, base, _ := docServer(t, goodDoc)
	var calls atomic.Int32
	fetcher.Trust(nil, func(context.Context, string) ([]netip.Addr, error) {
		if calls.Add(1) == 1 {
			return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
		}
		return []netip.Addr{netip.MustParseAddr("10.0.0.1")}, nil
	}, loopbackOK)
	if _, err := fetcher.Fetch(context.Background(), base+"/a"); err != nil {
		t.Fatal(err)
	}
	_, err := fetcher.Fetch(context.Background(), base+"/b")
	if !errors.Is(err, oauth.ErrUnsafeAddress) {
		t.Fatalf("err = %v, want an unsafe address", err)
	}
}

func TestFetcherHasNoProxy(t *testing.T) {
	if oauth.NewFetcher().Proxy() != nil {
		t.Fatal("the fetcher's transport has a Proxy func")
	}
}

func TestFetchIgnoresProxyEnvironment(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	t.Setenv("https_proxy", "http://127.0.0.1:1")
	fetcher, base, _ := docServer(t, goodDoc)
	if _, err := fetcher.Fetch(context.Background(), base+"/c"); err != nil {
		t.Fatalf("the proxy was used: %v", err)
	}
}
