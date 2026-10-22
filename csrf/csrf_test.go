package csrf

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSameOrigin(t *testing.T) {
	const origin = "https://app.example.com"
	cases := []struct {
		name    string
		headers map[string]string
		want    bool
	}{
		{"sec-fetch same-origin", map[string]string{"Sec-Fetch-Site": "same-origin"}, true},
		{"sec-fetch none (address bar)", map[string]string{"Sec-Fetch-Site": "none"}, true},
		{"sec-fetch cross-site", map[string]string{"Sec-Fetch-Site": "cross-site"}, false},
		{"origin exact", map[string]string{"Origin": origin}, true},
		{"origin mismatch", map[string]string{"Origin": "https://evil.example"}, false},
		{"referer same origin", map[string]string{"Referer": origin + "/form"}, true},
		{"referer other origin", map[string]string{"Referer": "https://evil.example/form"}, false},
		{"referer same host wrong scheme", map[string]string{"Referer": "http://app.example.com/form"}, false},
		{"referer unparseable", map[string]string{"Referer": "://not-a-url"}, false},
		{"referer bad escape", map[string]string{"Referer": "https://evil.example/%zz"}, false},
		{"no evidence refused", map[string]string{}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "http://app.test/form", nil)
			for k, v := range c.headers {
				r.Header.Set(k, v)
			}
			got := SameOrigin(r, origin)
			if got != c.want {
				t.Errorf("SameOrigin = %v, want %v", got, c.want)
			}
		})
	}
}

func TestProtectGatesMutatingMethodsOnly(t *testing.T) {
	origin := "https://app.example.com"
	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})
	handler := Protect(origin)(next)

	cases := []struct {
		name           string
		method         string
		headers        map[string]string
		wantStatus     int
		wantNextCalled bool
	}{
		// GET should pass through regardless
		{"get with no headers", "GET", map[string]string{}, http.StatusOK, true},
		// HEAD and OPTIONS should pass through regardless
		{"head with no headers", "HEAD", map[string]string{}, http.StatusOK, true},
		{"options with no headers", "OPTIONS", map[string]string{}, http.StatusOK, true},
		// POST without evidence should be rejected
		{"post with no headers", "POST", map[string]string{}, http.StatusForbidden, false},
		// POST with matching Origin should pass through
		{"post with matching origin", "POST", map[string]string{"Origin": origin}, http.StatusOK, true},
		// PUT without evidence should be rejected
		{"put with no headers", "PUT", map[string]string{}, http.StatusForbidden, false},
		// PUT with matching Origin should pass through
		{"put with matching origin", "PUT", map[string]string{"Origin": origin}, http.StatusOK, true},
		// PATCH without evidence should be rejected
		{"patch with no headers", "PATCH", map[string]string{}, http.StatusForbidden, false},
		// PATCH with matching Origin should pass through
		{"patch with matching origin", "PATCH", map[string]string{"Origin": origin}, http.StatusOK, true},
		// DELETE without evidence should be rejected
		{"delete with no headers", "DELETE", map[string]string{}, http.StatusForbidden, false},
		// DELETE with matching Origin should pass through
		{"delete with matching origin", "DELETE", map[string]string{"Origin": origin}, http.StatusOK, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			nextCalled = false
			r := httptest.NewRequest(c.method, "http://app.test/form", nil)
			for k, v := range c.headers {
				r.Header.Set(k, v)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != c.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, c.wantStatus)
			}
			if nextCalled != c.wantNextCalled {
				t.Errorf("next called = %v, want %v", nextCalled, c.wantNextCalled)
			}
		})
	}
}

// exemptPost sends a POST through Protect("https://a.test", Exempt("/hook"))
// and returns the status. headers is the browser evidence, if any.
func exemptPost(t *testing.T, path string, headers map[string]string) int {
	t.Helper()
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := Protect("https://a.test", Exempt("/hook"))(next)
	r := httptest.NewRequest("POST", "http://app.test"+path, nil)
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w.Code
}

func TestExemptPathPassesWithoutBrowserEvidence(t *testing.T) {
	if got := exemptPost(t, "/hook", nil); got != http.StatusOK {
		t.Errorf("POST /hook with no evidence = %d, want 200", got)
	}
}

func TestExemptIsExactPath(t *testing.T) {
	// A prefix or case-folded match would exempt routes nobody named.
	for _, path := range []string{"/hook/", "/HOOK", "/hook/x", "/hookx"} {
		if got := exemptPost(t, path, nil); got != http.StatusForbidden {
			t.Errorf("POST %s with no evidence = %d, want 403", path, got)
		}
	}
}

func TestExemptLeavesOtherPathsProtected(t *testing.T) {
	if got := exemptPost(t, "/other", nil); got != http.StatusForbidden {
		t.Errorf("POST /other with no evidence = %d, want 403", got)
	}
	if got := exemptPost(t, "/other", map[string]string{"Sec-Fetch-Site": "same-origin"}); got != http.StatusOK {
		t.Errorf("POST /other same-origin = %d, want 200", got)
	}
}

func TestExemptPanicsOnARelativeOrEmptyPath(t *testing.T) {
	for _, path := range []string{"", "hook"} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("Exempt(%q) did not panic", path)
				}
			}()
			Exempt(path)
		}()
	}
}
