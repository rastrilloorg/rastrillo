package rastrillo

import (
	"strings"
	"testing"
)

// servedPolicy starts the app as Serve would and returns the policy its
// responses carry.
func servedPolicy(t *testing.T, opts Options) string {
	t.Helper()
	opts.Mux = helloMux(&captured{})
	handler, closeAll, err := Handler(opts)
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	t.Cleanup(func() { _ = closeAll() })
	return get(handler, "/hello", "").Header().
		Get("Content-Security-Policy")
}

func TestCSPAllowAddsSourcesToADirectiveThePolicyHas(t *testing.T) {
	got := servedPolicy(t, Options{CSPAllow: map[string][]string{
		"img-src": {"https://logos.example.com"},
	}})
	want := strings.Replace(DefaultCSP, "img-src 'self' data:",
		"img-src 'self' data: https://logos.example.com", 1)
	if got != want {
		t.Errorf("policy = %q\nwant %q", got, want)
	}
}

// Without default-src's sources, script-src https://cdn.example.com
// would refuse the app's own scripts.
func TestCSPAllowStartsAMissingDirectiveFromDefaultSrc(t *testing.T) {
	got := servedPolicy(t, Options{CSPAllow: map[string][]string{
		"script-src": {"https://cdn.example.com"},
	}})
	want := DefaultCSP + "; script-src 'self' https://cdn.example.com"
	if got != want {
		t.Errorf("policy = %q\nwant %q", got, want)
	}
}

// style-src-elem falls back to style-src before default-src, so
// starting it from default-src would drop the confirm page's hash.
func TestCSPAllowStartsAnElementDirectiveFromItsParent(t *testing.T) {
	got := servedPolicy(t, Options{CSPAllow: map[string][]string{
		"style-src-elem": {"https://fonts.example.com"},
	}})
	want := DefaultCSP + "; style-src-elem 'self' 'unsafe-hashes' " +
		"'sha256-yJxAE4rjdcckohdlnvecSporPcqS9xOaA4hJxi87LMc=' " +
		"'sha256-81gZUb3tp60YCqhJ0dItBPh5hRQiGbfst4gpwCNPTfI=' " +
		"https://fonts.example.com"
	if got != want {
		t.Errorf("policy = %q\nwant %q", got, want)
	}
}

// A comma starts a second policy, so a source added after it would
// land in the wrong one: img-src 'self', script-src 'none' plus an
// image host gave script-src https://images.example.
func TestCSPAllowRefusesAnOptionsCSPWithACommaInIt(t *testing.T) {
	_, _, err := Handler(Options{
		Mux: helloMux(&captured{}),
		CSP: "img-src 'self', script-src 'none'",
		CSPAllow: map[string][]string{
			"img-src": {"https://images.example"},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "refusing to start") ||
		!strings.Contains(err.Error(), "comma") {
		t.Fatalf("Handler error = %v, want a refusal naming the comma",
			err)
	}
}

func TestCSPWithoutCSPAllowIsServedAsWritten(t *testing.T) {
	policy := "img-src 'self',  script-src 'none' ;"
	if got := servedPolicy(t, Options{CSP: policy}); got != policy {
		t.Errorf("policy = %q, want %q byte for byte", got, policy)
	}
}
