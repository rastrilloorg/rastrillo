package auth

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
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
	} {
		d := newTestAuthDB(t)
		if _, err := New(Config{DB: d, Origin: "http://app.test", InstanceKey: "k", Mailer: &captureMailer{}, KeymailServers: []string{bad}}); err == nil {
			t.Errorf("KeymailServers %q was accepted; it can never match a host, so every keymail user would silently get a link", bad)
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
	if m.to != "ron@example.net" {
		t.Fatalf("the link went to %q", m.to)
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
