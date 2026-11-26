package cspallow

import (
	"crypto/sha512"
	"encoding/base64"
	"slices"
	"strings"
	"testing"
)

// added is Add for a policy and allow list a test expects to pass.
func added(t *testing.T, policy string, allow map[string][]string) string {
	t.Helper()
	got, err := Add(policy, allow)
	if err != nil {
		t.Fatalf("Add(%q, %q): %v", policy, allow, err)
	}
	return got
}

func TestAddAddsToTheAppsOwnPolicy(t *testing.T) {
	got := added(t, "default-src 'none'; img-src 'self'",
		map[string][]string{
			"img-src":     {"https://logos.example.com"},
			"connect-src": {"https://api.example.com"},
		})
	want := "default-src 'none'; " +
		"img-src 'self' https://logos.example.com; " +
		"connect-src https://api.example.com"
	if got != want {
		t.Errorf("policy = %q\nwant %q", got, want)
	}
}

func TestAddAddsNewDirectivesInNameOrder(t *testing.T) {
	allow := map[string][]string{
		"media-src":   {"https://video.example.com"},
		"connect-src": {"https://api.example.com"},
		"font-src":    {"https://fonts.example.com"},
		"worker-src":  {"blob:"},
	}
	want := "default-src 'self'" +
		"; connect-src 'self' https://api.example.com" +
		"; font-src 'self' https://fonts.example.com" +
		"; media-src 'self' https://video.example.com" +
		"; worker-src 'self' blob:"
	for range 10 {
		if got := added(t, "default-src 'self'", allow); got != want {
			t.Fatalf("policy = %q\nwant %q", got, want)
		}
	}
}

func TestAddRefusesAnythingThatIsNotASource(t *testing.T) {
	refused := map[string]map[string][]string{
		"an empty name":     {"": {"https://a.example.com"}},
		"an unknown name":   {"img_src": {"https://a.example.com"}},
		"a capital name":    {"IMG-SRC": {"https://a.example.com"}},
		"no source list":    {"sandbox": {"allow-scripts"}},
		"a second name":     {"img-src; script-src": {"*"}},
		"a semicolon":       {"img-src": {"https://a.example.com;script-src"}},
		"a comma":           {"img-src": {"https://a.example.com,https://b"}},
		"a space":           {"img-src": {"https://a.example.com https://b"}},
		"a newline":         {"img-src": {"https://a.example.com\n"}},
		"a control":         {"img-src": {"https://a.example.com\x00"}},
		"an empty source":   {"img-src": {""}},
		"an open directive": {"form-action": {"https://a.example.com"}},
		"a port, no host":   {"img-src": {"*:"}},
		"a made-up keyword": {"img-src": {"'unknown-keyword'"}},
		"a bad hash":        {"script-src": {"'sha256-@@@'"}},
		"a short hash":      {"script-src": {"'sha256-AAAA'"}},
		"a nonce":           {"script-src": {"'nonce-abc123'"}},
		"a bad escape":      {"script-src": {"https://x.example/%GG/app.js"}},
		"a backslash":       {"script-src": {`https://x.example/js\app.js`}},
	}
	for name, allow := range refused {
		t.Run(name, func(t *testing.T) {
			_, err := Add("default-src 'self'", allow)
			if err == nil {
				t.Fatalf("Add accepted CSPAllow %q", allow)
			}
			if !strings.Contains(err.Error(), "CSPAllow") {
				t.Errorf("error %q does not name CSPAllow", err)
			}
		})
	}
}

// A header built once at startup sends the same nonce on every
// response, which is no nonce at all.
func TestAddRefusesANonceAndSaysWhy(t *testing.T) {
	_, err := Add("default-src 'self'", map[string][]string{
		"script-src": {"'nonce-abc123'"},
	})
	if err == nil || !strings.Contains(err.Error(), "every response") {
		t.Fatalf("Add error = %v, want one saying the nonce "+
			"would repeat on every response", err)
	}
}

func TestAddAcceptsEachKindOfSource(t *testing.T) {
	sum := sha512.Sum384([]byte("console.log(1)"))
	hash := "'sha384-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
	sources := []string{
		"'self'", "'unsafe-inline'", "'unsafe-eval'", "'strict-dynamic'",
		"'wasm-unsafe-eval'", "'report-sample'", "*", "blob:", "data:",
		"https:", hash, "logos.example.com", "*.example.com",
		"https://*.example.com:443/logos/", "http://localhost:*",
		"wss://live.example.com",
	}
	got := added(t, "default-src 'self'",
		map[string][]string{"script-src": sources})
	want := "default-src 'self'; script-src " + strings.Join(sources, " ")
	if got != want {
		t.Errorf("policy = %q\nwant %q", got, want)
	}
}

// 'none' beside any other source is ignored by browsers, which warn;
// adding nothing changes nothing, not even a directive's presence.
func TestAddKeepsNoneOnlyWhenItIsAlone(t *testing.T) {
	cases := []struct {
		policy string
		allow  map[string][]string
		want   string
	}{
		{"default-src 'self'; img-src 'none'",
			map[string][]string{"img-src": {"'none'", "https://x.example"}},
			"default-src 'self'; img-src https://x.example"},
		{"default-src 'self'; img-src 'self'",
			map[string][]string{"img-src": {"'NONE'"}},
			"default-src 'self'; img-src 'self'"},
		{"default-src 'none'; img-src 'none'",
			map[string][]string{"img-src": {"'none'"}},
			"default-src 'none'; img-src 'none'"},
		{"default-src 'none'; img-src 'none'",
			map[string][]string{"img-src": nil, "script-src": {}},
			"default-src 'none'; img-src 'none'"},
	}
	for _, test := range cases {
		if got := added(t, test.policy, test.allow); got != test.want {
			t.Errorf("%q with %q = %q\nwant %q",
				test.policy, test.allow, got, test.want)
		}
	}
}

// Workers fell back to script-src's blob:; a new child-src sits ahead
// of it in their fallback list, so worker-src must be written out.
func TestAddKeepsWhatWorkersAllowedWhenAddingChildSrc(t *testing.T) {
	got := added(t, "default-src 'self'; script-src 'self' blob:",
		map[string][]string{"child-src": {"https://frames.example.com"}})
	want := "default-src 'self'; script-src 'self' blob:; " +
		"worker-src 'self' blob:; " +
		"child-src 'self' https://frames.example.com"
	if got != want {
		t.Errorf("policy = %q\nwant %q", got, want)
	}
}

func TestAddKeepsAWorkerNoneWhenAddingChildSrc(t *testing.T) {
	got := added(t, "default-src 'self'; script-src 'none'",
		map[string][]string{"child-src": {"https://frames.example.com"}})
	want := "default-src 'self'; script-src 'none'; worker-src 'none'; " +
		"child-src 'self' https://frames.example.com"
	if got != want {
		t.Errorf("policy = %q\nwant %q", got, want)
	}
}

// style-src-elem fell back to default-src, the very sources a new
// style-src starts from, so it gains the host instead of being pinned:
// a stylesheet from the added host is what the app asked for.
func TestAddLetsElementDirectivesShareANewStyleSrc(t *testing.T) {
	got := added(t, "default-src 'self'",
		map[string][]string{"style-src": {"https://fonts.example.com"}})
	want := "default-src 'self'; style-src 'self' https://fonts.example.com"
	if got != want {
		t.Errorf("policy = %q\nwant %q", got, want)
	}
}

// Whatever is added, every directive still allows what it allowed.
func TestAddNeverRefusesWhatThePolicyAllowed(t *testing.T) {
	cases := []struct {
		policy string
		allow  map[string][]string
	}{
		{"default-src 'self'; script-src 'self' blob:",
			map[string][]string{"child-src": {"https://f.example"}}},
		{"default-src 'self'; script-src 'none'",
			map[string][]string{"child-src": {"https://f.example"}}},
		{"default-src 'self'; style-src 'self' 'sha256-x'",
			map[string][]string{"style-src-elem": {"https://f.example"}}},
		{"default-src 'none'; child-src blob:",
			map[string][]string{"script-src": {"https://f.example"}}},
		{"default-src 'self'; style-src 'self' 'sha256-x'; " +
			"img-src 'self' data:; form-action 'self'",
			map[string][]string{
				"script-src": {"https://f.example"},
				"child-src":  {"https://g.example"}}},
	}
	for _, test := range cases {
		served := added(t, test.policy, test.allow)
		for name := range cspFallbacks {
			before, beforeOpen := effectiveSources(test.policy, name)
			after, afterOpen := effectiveSources(served, name)
			if beforeOpen && !afterOpen {
				t.Errorf("%q: %s was open and is now %q", served, name,
					after)
			}
			for _, source := range before {
				if !afterOpen && !slices.Contains(after, source) {
					t.Errorf("%q: %s lost %s", served, name, source)
				}
			}
		}
	}
}

// effectiveSources is what a browser enforces for name: the first of
// name and its fallbacks the policy has, 'none' read as no sources.
// open reports that the policy has none of them.
func effectiveSources(policy, name string) (sources []string, open bool) {
	present := map[string][]string{}
	for _, directive := range strings.Split(policy, ";") {
		fields := strings.Fields(directive)
		if len(fields) == 0 {
			continue
		}
		if _, seen := present[fields[0]]; !seen {
			present[fields[0]] = fields[1:]
		}
	}
	chain := append([]string{name}, cspFallbacks[name]...)
	for _, candidate := range chain {
		if found, ok := present[candidate]; ok {
			return slices.DeleteFunc(slices.Clone(found), isNone), false
		}
	}
	return nil, true
}
