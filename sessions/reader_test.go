package sessions_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/db"
	"amadan.net/rastrillo/rastrillo/migrate"
	"amadan.net/rastrillo/rastrillo/sessions"
)

// readerSessions is a store on db.Open's two pools, signed in once, so
// a test can hold the writer's only connection and see whether a read
// still gets through.
func readerSessions(t *testing.T) (*sessions.Sessions, *db.DB, *http.Cookie) {
	t.Helper()
	opened, err := db.Open(filepath.Join(t.TempDir(), "s.db"), nil)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { opened.Close() })
	ctx := context.Background()
	if _, err := migrate.Apply(ctx, opened, sessions.Schema); err != nil {
		t.Fatalf("migrate.Apply: %v", err)
	}
	store, err := sessions.New(sessions.Config{
		DB: opened.Writer(), Reader: opened.Reader(),
		Origin: "http://app.test",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	recorder := httptest.NewRecorder()
	signIn := httptest.NewRequest("POST", "http://app.test/signin", nil)
	who := sessions.Session{Subject: "42", Method: "password"}
	if err := store.SignIn(recorder, signIn, who); err != nil {
		t.Fatalf("SignIn: %v", err)
	}
	return store, opened, cookieFrom(t, recorder, store.CookieName())
}

// holdWriter takes the writer pool's only connection, mid-write, and
// returns the release: what every request sees while another writes.
func holdWriter(t *testing.T, opened *db.DB) (release func()) {
	t.Helper()
	tx, err := opened.Writer().Begin()
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	t.Cleanup(func() { tx.Rollback() })
	_, err = tx.Exec(`DELETE FROM sessions WHERE subject = 'nobody'`)
	if err != nil {
		t.Fatalf("write in held transaction: %v", err)
	}
	return func() { tx.Rollback() }
}

// awaitRead runs read in a worker while the writer is held. A read
// still waiting after ten seconds is waiting for the writer, not for a
// slow CI box; release the writer and join the worker before failing,
// so it cannot outlive the pools the cleanup closes.
func awaitRead(t *testing.T, release func(), read func() error) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- read() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		release()
		<-done
		t.Fatal("the read waited for the writer")
	}
}

// The lookup Middleware does on every request reads through Reader, so
// a page load does not wait for a write in progress (#241). On the
// writer it queued behind every write, fsync included under FULL.
func TestLookupDoesNotWaitForTheWriter(t *testing.T) {
	store, opened, cookie := readerSessions(t)
	release := holdWriter(t, opened)

	awaitRead(t, release, func() error {
		var subject string
		page := store.Middleware(http.HandlerFunc(
			func(_ http.ResponseWriter, request *http.Request) {
				current, _ := sessions.Current(request)
				subject = current.Subject
			}))
		request := httptest.NewRequest("GET", "http://app.test/", nil)
		request.AddCookie(cookie)
		page.ServeHTTP(httptest.NewRecorder(), request)
		if subject != "42" {
			return fmt.Errorf("Current subject = %q, want 42", subject)
		}
		return nil
	})
}

// List reads through Reader too: a settings page listing a person's
// sessions should not wait for somebody else's write.
func TestListDoesNotWaitForTheWriter(t *testing.T) {
	store, opened, _ := readerSessions(t)
	release := holdWriter(t, opened)

	awaitRead(t, release, func() error {
		infos, err := store.List("42")
		if err != nil {
			return err
		}
		if len(infos) != 1 {
			return fmt.Errorf("List returned %d sessions, want 1",
				len(infos))
		}
		return nil
	})
}
