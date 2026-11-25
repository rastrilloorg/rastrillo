package oauth

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/netip"
	"net/url"
	"strings"
	"testing"
)

// FuzzRedirectAllowed feeds a registered redirect and a requested one,
// both a client's to choose. The code goes wherever the requested one
// points, so an allowed redirect must be a fragment-free URL, https or
// loopback http, on the very host that was registered.
func FuzzRedirectAllowed(f *testing.F) {
	const app = "https://app.example/cb"
	f.Add(app, app)
	f.Add(app, "https://APP.example/cb")
	f.Add(app, "https://app.example@evil.example/cb")
	f.Add(app, "https://app.example\\@evil.example/cb")
	f.Add(app, "https://app.example/cb#x")
	f.Add(app, "https://app.example./cb")
	f.Add("http://127.0.0.1:1/cb", "http://127.0.0.1:2/cb")
	f.Add("http://[::1]:1/cb", "http://[::1]:2/cb")
	f.Add("http://localhost/cb", "http://localhost.evil.example/cb")
	f.Add("https://app.example/a%2Fb", "https://app.example/a/b")
	f.Fuzz(func(t *testing.T, registered, requested string) {
		if !redirectAllowed([]string{registered}, requested) {
			return
		}
		have, haveErr := url.Parse(registered)
		want, wantErr := url.Parse(requested)
		switch {
		case haveErr != nil || wantErr != nil:
			t.Fatalf("%q allowed against %q, and one does not parse",
				requested, registered)
		case want.User != nil || strings.Contains(requested, "#"):
			t.Fatalf("%q allowed with userinfo or a fragment", requested)
		case want.Scheme != "https" &&
			!(want.Scheme == "http" && loopback(want.Hostname())):
			t.Fatalf("%q allowed over %q", requested, want.Scheme)
		case want.Hostname() != have.Hostname() ||
			want.Scheme != have.Scheme:
			t.Fatalf("%q allowed against %q", requested, registered)
		}
	})
}

// FuzzVerified is PKCE's whole promise: the code goes only to whoever
// holds the verifier its challenge was made from.
func FuzzVerified(f *testing.F) {
	const verifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	if !verified(verifier, challenge) {
		f.Fatal("the verifier does not verify its own challenge")
	}
	f.Add(verifier)
	f.Add(verifier + "=")
	f.Add(verifier[:42])
	f.Add(challenge)
	f.Add("")
	f.Fuzz(func(t *testing.T, got string) {
		if verified(got, challenge) && got != verifier {
			t.Fatalf("%q verified; only %q made the challenge",
				got, verifier)
		}
	})
}

// FuzzParseDocument feeds a client metadata document, which the client
// hosts and so writes. Only a public client with a name and redirects,
// under the URL it was fetched from, may come out of it.
func FuzzParseDocument(f *testing.F) {
	const id = "https://client.example/oauth.json"
	f.Add([]byte(`{"client_id":"` + id + `","client_name":"Agent",` +
		`"redirect_uris":["https://client.example/cb"]}`))
	f.Add([]byte(`{"client_id":"` + id + `","client_name":"Agent",` +
		`"redirect_uris":["https://client.example/cb"],` +
		`"client_secret":"s"}`))
	f.Add([]byte(`{"client_id":"` + id + `","client_name":"Agent",` +
		`"redirect_uris":["https://client.example/cb"],` +
		`"Token_Endpoint_Auth_Method":"client_secret_basic"}`))
	f.Add([]byte(`{"client_id":"` + id + `","client_name":"A‮B",` +
		`"redirect_uris":["x"]}`))
	f.Add([]byte(`null`))
	f.Fuzz(func(t *testing.T, body []byte) {
		client, err := parseDocument(id, body)
		if err != nil {
			return
		}
		var members map[string]json.RawMessage
		_ = json.Unmarshal(body, &members)
		_, secret := members["client_secret"]
		if client.ID != id || !validName(client.Name) ||
			len(client.Redirects) == 0 || secret || !client.Document {
			t.Fatalf("%q read as %+v", body, client)
		}
	})
}

// FuzzPublicAddr checks the fetcher's address guard: a client
// document URL whose name resolves inside the network must not be
// dialed, whether the address says so plainly or carries an internal
// IPv4 address inside an IPv6 one.
func FuzzPublicAddr(f *testing.F) {
	for _, seed := range []string{
		"8.8.8.8", "127.0.0.1", "::ffff:10.0.0.1", "::10.0.0.1",
		"64:ff9b::a00:1", "2002:a00:1::", "fe80::1", "fd00::1", "::1",
		"169.254.169.254", "::ffff:169.254.169.254", "100.64.0.1",
		"2606:4700::1111",
	} {
		raw := netip.MustParseAddr(seed).As16()
		f.Add(raw[:])
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) != 16 {
			return
		}
		address := netip.AddrFrom16([16]byte(raw))
		if !publicAddr(address) {
			return
		}
		for _, inner := range []netip.Addr{
			address, address.Unmap(), embeddedIPv4(raw),
		} {
			if inner.IsLoopback() || inner.IsPrivate() ||
				inner.IsLinkLocalUnicast() || inner.IsUnspecified() ||
				inner.IsMulticast() {
				t.Fatalf("%v is public, but carries %v", address, inner)
			}
		}
	})
}

// embeddedIPv4 is the IPv4 address an IPv4-compatible (::/96), NAT64
// (64:ff9b::/96) or 6to4 (2002::/16) address routes to, or the zero
// Addr.
func embeddedIPv4(raw []byte) netip.Addr {
	zero := func(part []byte) bool {
		return strings.Trim(string(part), "\x00") == ""
	}
	switch {
	case zero(raw[:12]),
		raw[0] == 0 && raw[1] == 0x64 && raw[2] == 0xff &&
			raw[3] == 0x9b && zero(raw[4:12]):
		return netip.AddrFrom4([4]byte(raw[12:]))
	case raw[0] == 0x20 && raw[1] == 0x02:
		return netip.AddrFrom4([4]byte(raw[2:6]))
	}
	return netip.Addr{}
}
