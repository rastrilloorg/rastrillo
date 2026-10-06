package sessions_test

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/sessions"
)

func TestFromContextCancelsWhileWaitingForActualWriter(t *testing.T) {
	for _, timeout := range []bool{false, true} {
		t.Run(strconv.FormatBool(timeout), func(t *testing.T) {
			var logs bytes.Buffer
			s, db := newTestSessions(t, func(c *sessions.Config) { c.Logger = slog.New(slog.NewTextHandler(&logs, nil)) })
			r := httptest.NewRequest("GET", "http://app.test/private", nil)
			w := httptest.NewRecorder()
			if e := s.SignIn(w, r, sessions.Session{Subject: "known", Method: "keymail", AuthTime: time.Now().Add(-time.Minute)}); e != nil {
				t.Fatal(e)
			}
			r.AddCookie(cookieFrom(t, w, s.CookieName()))
			want, ok := s.From(r)
			if !ok {
				t.Fatal("setup session missing")
			}
			tx, e := db.BeginTx(t.Context(), nil)
			if e != nil {
				t.Fatal(e)
			}
			defer tx.Rollback()
			ctx, cancel := context.WithCancel(t.Context())
			if timeout {
				cancel()
				ctx, cancel = context.WithTimeout(t.Context(), 2*time.Second)
			}
			defer cancel()
			before := db.Stats().WaitCount
			done := make(chan bool, 1)
			go func() { _, ok := s.FromContext(ctx, r); done <- ok }()
			deadline := time.Now().Add(time.Second)
			for db.Stats().WaitCount == before && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			if db.Stats().WaitCount == before {
				t.Fatal("lookup never waited for SQLite writer")
			}
			if !timeout {
				cancel()
			}
			wait := 250 * time.Millisecond
			if timeout {
				deadline, _ := ctx.Deadline()
				wait += max(time.Until(deadline), 0)
			}
			select {
			case ok := <-done:
				if ok {
					t.Fatal("canceled lookup granted access")
				}
			case <-time.After(wait):
				t.Fatal("context did not cancel queued session lookup")
			}
			if logs.Len() != 0 {
				t.Fatal("routine context cancellation logged as storage error")
			}
			if e = tx.Rollback(); e != nil {
				t.Fatal(e)
			}
			got, ok := s.FromContext(t.Context(), r)
			if !ok || got != want {
				t.Fatal("cancellation changed persisted session")
			}
		})
	}
}

func TestFromContextMatchesExistingReader(t *testing.T) {
	for _, origin := range []string{"http://app.test", "https://app.test"} {
		t.Run(origin, func(t *testing.T) {
			s, db := newTestSessions(t, func(c *sessions.Config) { c.Origin = origin })
			r := httptest.NewRequest("GET", origin+"/private", nil)
			w := httptest.NewRecorder()
			if e := s.SignIn(w, r, sessions.Session{Subject: "opaque-identity", Method: "home-assertion", AuthTime: time.Now().Add(-time.Hour)}); e != nil {
				t.Fatal(e)
			}
			cookie := cookieFrom(t, w, s.CookieName())
			r.AddCookie(cookie)
			check := func(r *http.Request, want bool) {
				t.Helper()
				old, oldOK := s.From(r)
				got, ok := s.FromContext(t.Context(), r)
				if oldOK != want || ok != want || old != got {
					t.Fatal("session readers disagree")
				}
			}
			check(r, true)
			check(httptest.NewRequest("GET", origin+"/private", nil), false)
			bad := httptest.NewRequest("GET", origin+"/private", nil)
			bad.AddCookie(&http.Cookie{Name: s.CookieName(), Value: "malformed"})
			check(bad, false)
			if _, e := db.Exec("UPDATE sessions SET expires_at=?", time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)); e != nil {
				t.Fatal(e)
			}
			check(r, false)
			if _, e := db.Exec("DELETE FROM sessions"); e != nil {
				t.Fatal(e)
			}
			check(r, false)
			if e := db.Close(); e != nil {
				t.Fatal(e)
			}
			check(r, false)
		})
	}
}
