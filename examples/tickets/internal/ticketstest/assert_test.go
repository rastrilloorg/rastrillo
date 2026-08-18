package ticketstest

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// The assertions every test in this package shares. Plain stdlib, no
// assertion library — the same style as the rastrillo repo's own
// tests (see examples/blog/internal/blogtest/assert_test.go).

func wantStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, want, rec.Body.String())
	}
}

func wantContains(t *testing.T, body, want string) {
	t.Helper()
	if !strings.Contains(body, want) {
		t.Errorf("missing %q in:\n%s", want, body)
	}
}

func wantNotContains(t *testing.T, body, unwanted string) {
	t.Helper()
	if strings.Contains(body, unwanted) {
		t.Errorf("unexpected %q in:\n%s", unwanted, body)
	}
}
