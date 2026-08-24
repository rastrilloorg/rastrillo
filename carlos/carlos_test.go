package carlos

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// tickRequest is the tick as internal/schedule/runner.go BuildRequest
// sends it: POST, no body, the bearer, and the three headers.
func tickRequest(auth, name, kind, at string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/jobs/sync", nil)
	if auth != "" {
		r.Header.Set("Authorization", auth)
	}
	r.Header.Set("X-Carlos-Schedule", name)
	r.Header.Set("X-Carlos-Schedule-Kind", kind)
	r.Header.Set("X-Carlos-Schedule-At", at)
	r.Header.Set("User-Agent", "carlos-schedule/test")
	return r
}

func TestTick(t *testing.T) {
	t.Run("accepts the platform's bearer", func(t *testing.T) {
		t.Setenv(tokenEnv, "sekrit")
		if !Tick(tickRequest("Bearer sekrit", "sync", "schedule", "1765000000")) {
			t.Fatal("a tick carrying the instance token was refused")
		}
	})

	t.Run("accepts any casing of the scheme", func(t *testing.T) {
		// RFC 7235 makes the scheme case-insensitive. The platform always
		// sends "Bearer", so this is for the manual path Tick exists to
		// allow: an ops runbook curling with "bearer" should not get a
		// 403 whose cause is invisible.
		t.Setenv(tokenEnv, "sekrit")
		for _, auth := range []string{"bearer sekrit", "BEARER sekrit", "BeArEr sekrit"} {
			if !Tick(tickRequest(auth, "sync", "schedule", "1765000000")) {
				t.Errorf("Authorization %q was refused", auth)
			}
		}
	})

	t.Run("refuses a wrong token", func(t *testing.T) {
		t.Setenv(tokenEnv, "sekrit")
		for _, auth := range []string{
			"Bearer wrong",
			"Bearer sekri",   // a prefix: the length-mismatch path
			"Bearer sekrits", // and one byte too long
			"sekrit",         // the token with no scheme
			"Basic sekrit",
			"Bearer  sekrit", // two spaces: the scheme is loose, the rest is not
			"Bearersekrit",
			"",
		} {
			if Tick(tickRequest(auth, "sync", "schedule", "1765000000")) {
				t.Fatalf("Authorization %q was accepted", auth)
			}
		}
	})

	t.Run("fails closed with no token configured", func(t *testing.T) {
		// An instance the platform never gave a token to must not
		// accept a tick on the headers alone — anyone can set those.
		t.Setenv(tokenEnv, "")
		for _, auth := range []string{"", "Bearer ", "Bearer anything"} {
			if Tick(tickRequest(auth, "sync", "schedule", "1765000000")) {
				t.Fatalf("Authorization %q accepted with no token in the environment", auth)
			}
		}
	})

	t.Run("does not require the schedule headers", func(t *testing.T) {
		// The same token is how an app's own "run now" path reaches the
		// handler; refusing it would buy nothing.
		t.Setenv(tokenEnv, "sekrit")
		r := httptest.NewRequest(http.MethodPost, "/jobs/sync", nil)
		r.Header.Set("Authorization", "Bearer sekrit")
		if !Tick(r) {
			t.Fatal("an authenticated request with no schedule headers was refused")
		}
	})
}

func TestTickOccurrence(t *testing.T) {
	t.Run("returns the At header of an authentic tick", func(t *testing.T) {
		t.Setenv(tokenEnv, "sekrit")
		occ, ok := TickOccurrence(tickRequest("Bearer sekrit", "sync", "schedule", "1765000000"))
		if !ok || occ != "1765000000" {
			t.Fatalf("TickOccurrence = %q, %v; want \"1765000000\", true", occ, ok)
		}
	})

	t.Run("is stable across a retry of one occurrence", func(t *testing.T) {
		// The dedupe contract: a retry carries the value the first
		// attempt did, so an app keyed on it does the work once.
		t.Setenv(tokenEnv, "sekrit")
		first, _ := TickOccurrence(tickRequest("Bearer sekrit", "sync", "schedule", "1765000000"))
		retry, _ := TickOccurrence(tickRequest("Bearer sekrit", "sync", "schedule", "1765000000"))
		if first != retry {
			t.Fatalf("occurrence key changed between attempts: %q then %q", first, retry)
		}
	})

	t.Run("refuses what Tick refuses", func(t *testing.T) {
		t.Setenv(tokenEnv, "sekrit")
		if occ, ok := TickOccurrence(tickRequest("Bearer wrong", "sync", "schedule", "1765000000")); ok {
			t.Fatalf("an unauthenticated request yielded an occurrence key %q", occ)
		}
	})

	t.Run("refuses an authentic request with no At header", func(t *testing.T) {
		// Which is why it is the key and not the guard: an app's own
		// "Sync now" call carries the token and no schedule headers, and
		// Tick — the guard — still accepts it.
		t.Setenv(tokenEnv, "sekrit")
		r := httptest.NewRequest(http.MethodPost, "/jobs/sync", nil)
		r.Header.Set("Authorization", "Bearer sekrit")
		if _, ok := TickOccurrence(r); ok {
			t.Fatal("a request with no X-Carlos-Schedule-At yielded an occurrence key")
		}
		if !Tick(r) {
			t.Fatal("Tick refused the manual path it is documented to allow")
		}
	})
}
