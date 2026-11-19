package auth

import (
	"bytes"
	"context"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/db"
	"amadan.net/rastrillo/rastrillo/migrate"
	"amadan.net/rastrillo/rastrillo/objrec"
	"amadan.net/rastrillo/rastrillo/sessions"
)

// Auth builds its own session store, so Config.Reader has to reach it:
// otherwise the lookup behind every signed-in page still waits for a
// write on the writer's one connection (#241).
func TestSessionFromDoesNotWaitForTheWriter(t *testing.T) {
	opened, err := db.Open(filepath.Join(t.TempDir(), "auth.db"), nil)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { opened.Close() })
	schema := migrate.Merge(sessions.Schema, Schema)
	_, err = migrate.Apply(context.Background(), opened, schema)
	if err != nil {
		t.Fatalf("migrate.Apply: %v", err)
	}
	centre, err := New(Config{
		DB: opened.Writer(), Reader: opened.Reader(),
		Origin: "http://app.test", InstanceKey: "test-instance-key",
		Mailer: &captureMailer{}, ProofOff: true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	recorder := httptest.NewRecorder()
	signIn := httptest.NewRequest("POST", "http://app.test/auth/verify", nil)
	who := sessions.Session{Subject: "42", Method: "link"}
	if err := centre.sessions.SignIn(recorder, signIn, who); err != nil {
		t.Fatalf("SignIn: %v", err)
	}
	cookie := sessionCookieSet(centre, recorder)

	held, err := opened.Writer().Begin()
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	t.Cleanup(func() { held.Rollback() })
	_, err = held.Exec(`DELETE FROM sessions WHERE subject = 'nobody'`)
	if err != nil {
		t.Fatalf("write in held transaction: %v", err)
	}

	// Ten seconds, so a slow CI box is not mistaken for the writer; on
	// timeout the writer is released and the worker joined before the
	// test fails, so it cannot outlive the pools the cleanup closes.
	found := make(chan bool, 1)
	go func() {
		page := httptest.NewRequest("GET", "http://app.test/", nil)
		page.AddCookie(cookie)
		_, ok := centre.SessionFrom(page)
		found <- ok
	}()
	select {
	case ok := <-found:
		if !ok {
			t.Fatal("SessionFrom found no session")
		}
	case <-time.After(10 * time.Second):
		held.Rollback()
		<-found
		t.Fatal("the session lookup waited for the writer")
	}
}

// Records mode keeps no database, so a Reader there is refused like a
// DB, not silently dropped.
func TestRecordsNewRefusesAReader(t *testing.T) {
	key, err := sessions.NewSigningKey("k1")
	if err != nil {
		t.Fatal(err)
	}
	_, err = New(Config{
		Origin: "https://ppl.test", InstanceKey: "test-instance-key",
		ProofOff: true, Mailer: &captureMailer{},
		Records: objrec.Dir(t.TempDir()),
		Pepper:  bytes.Repeat([]byte{7}, 32),
		Signed:  &sessions.Signed{Name: "ppl", Key: key},
		Reader:  newTestAuthDB(t),
	})
	if err == nil {
		t.Fatal("New accepted a Reader in records mode")
	}
}
