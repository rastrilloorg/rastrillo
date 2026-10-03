package auth

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/keymaildev/signin"
)

func listed(servers ...string) func(*Config) {
	return func(c *Config) { c.KeymailServers = servers }
}

// beginKeymail posts kay's address and returns the answer, unforced so
// it is classified.
func beginKeymail(a *Auth, b *browser, address string) *http.Response {
	return b.do(a.Begin, http.MethodPost, "/signin", url.Values{"address": {address}}).Result()
}

func TestKeymailServersRefusesWhatIsNotAHost(t *testing.T) {
	for _, bad := range []string{
		"https://keymail.dev", "keymail.dev/", "keymail.dev/x", "", "key mail.dev", "user@keymail.dev",
		"keymail.test:banana", "keymail.test:99999", "keymail.test:0", "keymail.test:", "[::1", "keymail\x00.test", "keymail.test?x",
		// Unbracketed IPv6: serverKey takes only the last colon as a
		// port, so listed "::1" keys as "::1" (host ":", port 1) and a
		// request to ::1 (host "::1", dialed [::1]:443) keys to the same
		// string — two different authorities sharing one allowlist
		// entry. "::1:8443" is worse: it would admit a request to
		// ::1:8443, dialed [::1:8443]:443, not ::1:8443. IPv6 must be
		// bracketed.
		"::1", "::1:8443", "2001:db8::1",
		// serverKey("." or "..") trims one trailing dot, so their keys
		// are "" and "." — neither can equal a real request's host, but
		// an operator who typos one would see it accepted and never
		// learn it matches nothing.
		".", "..",
	} {
		d := newTestAuthDB(t)
		if _, err := New(Config{DB: d, Origin: "http://app.test", InstanceKey: "k", Mailer: &captureMailer{}, KeymailServers: []string{bad}}); err == nil {
			t.Errorf("KeymailServers %q was accepted; it can never match a host, so every keymail user would silently get a link", bad)
		}
	}
	// The refusals above must not be refusing everything: a port, an
	// IPv6 literal, an explicit :443 and a fully-qualified name are all
	// hosts an operator may fairly write.
	for _, good := range []string{"keymail.test:8443", "[::1]:443", "keymail.dev:443", "keymail.dev.", "KeyMail.Dev"} {
		d := newTestAuthDB(t)
		if _, err := New(Config{DB: d, Origin: "http://app.test", InstanceKey: "k", Mailer: &captureMailer{}, KeymailServers: []string{good}}); err != nil {
			t.Errorf("KeymailServers %q was refused: %v", good, err)
		}
	}
}

// The same server spelled two ways must match on both enforcement
// sides — the guard under the HTTP clients and the authorize-URL
// predicate — or a valid entry silently turns keymail off: every
// keymail user gets a link and nothing is logged. Only case, one
// trailing dot and an explicit :443 are spelling; any other port is a
// different server.
func TestKeymailServersMatchOneServerHoweverItIsSpelled(t *testing.T) {
	ok := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
	})
	for _, c := range []struct {
		listed, host string
		want         bool
	}{
		{"keymail.dev:443", "keymail.dev", true},
		{"keymail.dev", "keymail.dev:443", true},
		{"keymail.dev.", "keymail.dev", true},
		{"keymail.dev", "keymail.dev.", true},
		{"KeyMail.Dev.:443", "keymail.dev", true},
		{"keymail.dev", "KEYMAIL.DEV.:443", true},
		{"[::1]:443", "[::1]", true},
		{"[::1]", "[::1]:443", true},
		{"keymail.test:8443", "keymail.test:8443", true},
		{"keymail.test.:8443", "keymail.test:8443", true},
		{"keymail.test:8443", "keymail.test.:8443", true},
		{"keymail.dev", "keymail.dev:8443", false},
		{"keymail.dev:8443", "keymail.dev", false},
		{"keymail.dev:8443", "keymail.dev:443", false},
		{"keymail.dev", "keymail.dev..", false},
		{"keymail.dev", "rogue.dev", false},
	} {
		a, _ := newTestAuth(t, listed(c.listed))
		a.guard.base = ok
		req, err := http.NewRequest(http.MethodGet, "https://"+c.host+"/.well-known/keymail", nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := a.guard.RoundTrip(req); (err == nil) != c.want {
			t.Errorf("listed %q, the guard on a request to %q: let through = %v, want %v (%v)", c.listed, c.host, err == nil, c.want, err)
		}
		u := (&signin.Keymail{Base: "https://" + c.host, Origin: a.cfg.Origin, RedirectPath: callbackPath}).
			AuthorizeURL(strings.Repeat("s", 43), strings.Repeat("c", 43), false)
		if got := a.validAuthorizeURL(u); got != c.want {
			t.Errorf("listed %q, the predicate on %s: %v, want %v", c.listed, u, got, c.want)
		}
	}
}

func TestWithoutKeymailServersBothClientsAreToday(t *testing.T) {
	a, _ := newTestAuth(t, nil)
	if a.guard != nil || a.flow.Classifier.HTTP != nil || a.exchangeHTTP != nil {
		t.Fatal("with no list, auth must leave both keymail clients as the library's defaults")
	}
	if k := a.flow.Keymail("keymail.test"); k.RedirectPath != callbackPath || k.HTTP != nil {
		t.Fatalf("Keymail = %+v; RedirectPath must be callbackPath, set explicitly", k)
	}
}

// A non-nil empty list is not "unset": it means no server is ever
// dialed, for any address, which is how an app with no real keymail
// federation partners says so. Before this fix keymailServers read
// len(list) == 0 rather than list == nil, so KeymailServers: []string{}
// silently fell back to the default "any delegated server" behavior —
// continuation.go's own predicate ("a.servers == nil") already knew
// the difference; only the parser did not.
func TestEmptyKeymailServersMeansNone(t *testing.T) {
	a, m := newTestAuth(t, func(c *Config) { c.KeymailServers = []string{} })
	if a.guard == nil {
		t.Fatal("KeymailServers: []string{} must still install the guard — a non-nil empty list means \"no servers\", not \"unset\"")
	}
	f := kayFake()
	wireKeymail(a, f)
	res := beginKeymail(a, newBrowser(), "kay@example.org")
	if loc := res.Header.Get("Location"); loc != "/signin?sent=1" {
		t.Fatalf("a claimed address with KeymailServers: []string{} → %q, want a magic link", loc)
	}
	if n := f.hits("keymail.test", ""); n != 0 {
		t.Fatalf("keymail.test received %d requests; with no servers listed, none may ever be contacted", n)
	}
	if m.sentTo() != "kay@example.org" {
		t.Fatalf("the link went to %q", m.sentTo())
	}
}

// wireKeymail always overwrites Classifier.LookupTXT with the fake's
// own, so TestEmptyKeymailServersMeansNone never exercises New's own
// short-circuit; it only proves the guard refuses the HTTP probe a
// delegation names. This test keeps New's lookup and takes the guard
// away instead, putting the fake straight under the classifier: the
// guard refuses a probe before its transport sees it, so behind the
// guard a lookup that answered "not found" (which upstream reads as
// "probe the domain itself") would look exactly like one that names
// nothing. Without the guard, any probe the classifier builds lands
// on the fake and is counted.
func TestEmptyKeymailServersNeverProbes(t *testing.T) {
	a, m := newTestAuth(t, func(c *Config) { c.KeymailServers = []string{} })
	// Checked first so a missing short-circuit fails here rather than
	// sending the test out to the real resolver.
	if a.flow.Classifier.LookupTXT == nil {
		t.Fatal("KeymailServers: []string{} left LookupTXT at its default (net.DefaultResolver), so every sign-in still pays a real DNS round trip whose answer can never matter")
	}
	f := kayFake()
	f.servers["example.org"] = true
	a.flow.Classifier.HTTP = &http.Client{Transport: f}
	res := beginKeymail(a, newBrowser(), "kay@example.org")
	if loc := res.Header.Get("Location"); loc != "/signin?sent=1" {
		t.Fatalf("a claimed address with KeymailServers: []string{} → %q, want a magic link", loc)
	}
	f.mu.Lock()
	seen := f.seen
	f.mu.Unlock()
	if len(seen) != 0 {
		t.Fatalf("the classifier built %d probes (%v); with no servers listed it must not build one", len(seen), seen)
	}
	if m.sentTo() != "kay@example.org" {
		t.Fatalf("the link went to %q", m.sentTo())
	}
}

func TestKeymailServersKeepBothTimeouts(t *testing.T) {
	a, _ := newTestAuth(t, listed("keymail.test"))
	if a.flow.Classifier.HTTP == nil || a.flow.Classifier.HTTP.Timeout != 5*time.Second {
		t.Fatalf("classifier client = %+v, want the library's 5s probe timeout kept", a.flow.Classifier.HTTP)
	}
	if k := a.flow.Keymail("keymail.test"); k.HTTP == nil || k.HTTP.Timeout != 15*time.Second {
		t.Fatalf("exchange client = %+v, want the library's 15s exchange timeout kept", k.HTTP)
	}
}

func TestAnUnlistedServerIsNeverProbed(t *testing.T) {
	a, m := newTestAuth(t, listed("keymail.test"))
	f := newKeymailFake()
	f.servers["rogue.test"] = true
	f.delegate["example.net"] = "rogue.test"
	f.claimed["ron@example.net"] = true
	wireKeymail(a, f)
	res := beginKeymail(a, newBrowser(), "ron@example.net")
	if loc := res.Header.Get("Location"); loc != "/signin?sent=1" {
		t.Fatalf("an unlisted server's address → %q, want a magic link", loc)
	}
	if n := f.hits("rogue.test", ""); n != 0 {
		t.Fatalf("rogue.test received %d requests; an unlisted server must never be contacted", n)
	}
	if m.sentTo() != "ron@example.net" {
		t.Fatalf("the link went to %q", m.sentTo())
	}
}

func TestAListedServerGetsTheCeremony(t *testing.T) {
	a, _ := newTestAuth(t, listed("KEYMAIL.test"))
	f := kayFake()
	wireKeymail(a, f)
	res := beginKeymail(a, newBrowser(), "kay@example.org")
	if loc := res.Header.Get("Location"); !strings.HasPrefix(loc, "https://keymail.test/oauth/authorize?") {
		t.Fatalf("a listed server's address → %q, want the authorize URL (the list compares case-insensitively)", loc)
	}
}

// Both probes — the well-known document and the federation lookup —
// and every redirect code a client follows.
func TestAProbeCannotLeaveTheListByRedirect(t *testing.T) {
	for _, path := range []string{"/.well-known/keymail", "/api/federation/lookup"} {
		for _, status := range []int{http.StatusFound, http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
			for name, to := range map[string]string{
				"to an unlisted host": "https://rogue.test" + path,
				"down to plain http":  "http://keymail.test" + path,
			} {
				t.Run(fmt.Sprintf("%s %d %s", path, status, name), func(t *testing.T) {
					a, _ := newTestAuth(t, listed("keymail.test"))
					f := kayFake()
					f.redirect[path], f.status = to, status
					wireKeymail(a, f)
					if loc := beginKeymail(a, newBrowser(), "kay@example.org").Header.Get("Location"); loc != "/signin?sent=1" {
						t.Fatalf("a redirected probe → %q, want the magic link a failed probe means", loc)
					}
					if n := f.hits("rogue.test", ""); n != 0 {
						t.Fatalf("rogue.test received %d requests", n)
					}
					if n := f.hits("keymail.test", path); n != 1 {
						t.Fatalf("keymail.test%s was requested %d times, want 1: the https request, and the refused hop never arriving", path, n)
					}
				})
			}
		}
	}
}

// 307 and 308 are the redirects that re-send a POST with its body: the
// code and the PKCE verifier. Neither an unlisted host nor the listed
// host over plain http may receive them.
func TestAnExchangeCannotLeaveTheListByRedirect(t *testing.T) {
	for _, status := range []int{http.StatusFound, http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		for name, to := range map[string]string{
			"to an unlisted host": "https://rogue.test/api/oauth/token",
			"down to plain http":  "http://keymail.test/api/oauth/token",
		} {
			t.Run(fmt.Sprintf("%d %s", status, name), func(t *testing.T) {
				a, _ := newTestAuth(t, listed("keymail.test"))
				f := kayFake()
				wireKeymail(a, f)
				b := newBrowser()
				state := stateOf(t, beginKeymail(a, b, "kay@example.org").Header.Get("Location"))
				f.redirect["/api/oauth/token"], f.status = to, status
				w := b.do(a.Callback, http.MethodGet, "/auth/callback?code=c&state="+state, nil)
				if loc := w.Header().Get("Location"); loc != "/signin?force=1&err=keymail" {
					t.Fatalf("a redirected exchange → %q, want the escape hatch", loc)
				}
				if n := f.hits("rogue.test", ""); n != 0 {
					t.Fatalf("the code and verifier reached rogue.test (%d requests)", n)
				}
				if n := f.hits("keymail.test", "/api/oauth/token"); n != 1 {
					t.Fatalf("keymail.test's token endpoint saw %d requests, want 1: the refused hop must never be sent", n)
				}
			})
		}
	}
}

func TestAPendingCookieForAServerNoLongerListed(t *testing.T) {
	before, _ := newTestAuth(t, listed("keymail.test"))
	f := kayFake()
	wireKeymail(before, f)
	b := newBrowser()
	state := stateOf(t, beginKeymail(before, b, "kay@example.org").Header.Get("Location"))

	after, _ := newTestAuth(t, listed("other.test"))
	wireKeymail(after, f)
	w := b.do(after.Callback, http.MethodGet, "/auth/callback?code=c&state="+state, nil)
	if loc := w.Header().Get("Location"); loc != "/signin?force=1&err=keymail" {
		t.Fatalf("a pending cookie for a delisted server → %q, want the escape hatch, not a dead end", loc)
	}
	if n := f.hits("keymail.test", "/api/oauth/token"); n != 0 {
		t.Fatalf("the delisted server received %d token exchanges", n)
	}
}
