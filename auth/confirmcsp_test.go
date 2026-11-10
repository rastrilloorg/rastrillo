package auth

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo"
)

// The fallback confirm page styles itself with one <style> block, and
// the default CSP admits it by its hash. Without that the browser drops
// the block and the page a person signs in from renders unstyled. This
// hashes the block the page actually serves and reads the policy the
// framework actually sends, so neither can change without the other.
func TestDefaultCSPAdmitsTheConfirmPageStyle(t *testing.T) {
	auth, mailer := newTestAuth(t, nil)
	beginSignin(t, auth, "person@example.com")
	link := linkRE.FindString(mailer.sentBody())
	mux := http.NewServeMux()
	mux.HandleFunc("GET /auth/verify", auth.Verify)
	handler, closeAll, err := rastrillo.Handler(rastrillo.Options{Mux: mux})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = closeAll() })

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", link, nil))
	match := styleBlockPattern.FindStringSubmatch(response.Body.String())
	if match == nil {
		t.Fatalf("the confirm page has no <style> block:\n%s",
			response.Body.String())
	}
	sum := sha256.Sum256([]byte(match[1]))
	source := "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
	policy := response.Header().Values("Content-Security-Policy")
	if !admitsStyle(policy, source) {
		t.Errorf("no style-src in %q admits the confirm page's "+
			"<style> block, %s", policy, source)
	}
}

var styleBlockPattern = regexp.MustCompile(`(?s)<style>(.*?)</style>`)

// admitsStyle reports a policy whose style-src carries source. A page
// with two policies must pass both, but only one names style-src here.
func admitsStyle(policies []string, source string) bool {
	for _, policy := range policies {
		for _, directive := range strings.Split(policy, ";") {
			fields := strings.Fields(directive)
			if len(fields) > 0 && fields[0] == "style-src" {
				return strings.Contains(directive, source)
			}
		}
	}
	return false
}
