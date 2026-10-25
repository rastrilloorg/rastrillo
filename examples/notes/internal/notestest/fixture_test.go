package notestest

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/apitoken"

	genlocales "notes/gen/locales"
)

// parityBase is the origin the parity test sends every request to, so
// it is also the origin the app is built with: csrf.Protect and the
// session cookie are checked against the same host a browser would
// use.
const parityBase = "http://app.test"

// handlers maps each fixture app's database to its handler. Token gets
// only the database, and in the API half of the scenario nobody has
// signed up when it runs, so it must reach the handler to sign the
// person up through the form.
var handlers sync.Map // *sql.DB -> http.Handler

// fixture is notes' half of the generated parity test
// (gen/paritytest). A subject is a person signed up through the real
// sign-up form, so the owner the page door stores is the same users
// row id the token carries.
type fixture struct{}

func (fixture) New(t *testing.T) (http.Handler, *sql.DB) {
	h, db := newHandler(t, parityBase, genlocales.DefaultLocale, genlocales.Locales, io.Discard)
	handlers.Store(db, h)
	t.Cleanup(func() { handlers.Delete(db) })
	return h, db
}

// SignIn signs subject up the first time, and in after that, so the
// order the scenario asks for people in does not matter.
func (fixture) SignIn(t *testing.T, h http.Handler, db *sql.DB, subject string) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	cl := &client{
		t:    t,
		base: parityBase,
		c: &http.Client{Jar: jar, Transport: inProcess{h},
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		origin: parityBase,
	}
	email := subject + "@parity.test"
	signedIn := cl.signin
	if _, err := userID(db, email); errors.Is(err, sql.ErrNoRows) {
		signedIn = cl.signup
	} else if err != nil {
		t.Fatal(err)
	}
	resp := signedIn(email, "parity-password")
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("signing %s in answered %d, want 303", email, resp.StatusCode)
	}
	return cl.c
}

// Token mints a read and write token for subject's users row through
// the app's own store, signing them up first if no page has.
func (f fixture) Token(t *testing.T, db *sql.DB, subject string) string {
	t.Helper()
	email := subject + "@parity.test"
	id, err := userID(db, email)
	if errors.Is(err, sql.ErrNoRows) {
		h, ok := handlers.Load(db)
		if !ok {
			t.Fatal("Token was given a database New did not make")
		}
		f.SignIn(t, h.(http.Handler), db, subject)
		id, err = userID(db, email)
	}
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := apitoken.New(apitoken.Config{DB: db, Prefix: apitoken.DerivePrefix("notes")})
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := tokens.Mint(context.Background(), strconv.FormatInt(id, 10), apitoken.MintInput{ForSelf: true, Access: rastrillo.Write})
	if err != nil {
		t.Fatal(err)
	}
	return token
}

// Prepare has nothing to seed: bookmarks' operations are generated.
func (fixture) Prepare(t *testing.T, db *sql.DB, subject string) {}

// ValidCreate leaves the values to the generator: every bookmark field
// is free text.
func (fixture) ValidCreate(resource string) map[string]string { return nil }

func userID(db *sql.DB, email string) (int64, error) {
	var id int64
	err := db.QueryRow(`SELECT id FROM users WHERE email = ?`, email).Scan(&id)
	return id, err
}

// inProcess serves every request from h, whatever its host, so the
// parity test's http://app.test reaches the app without a listener.
type inProcess struct{ h http.Handler }

func (p inProcess) RoundTrip(r *http.Request) (*http.Response, error) {
	w := httptest.NewRecorder()
	p.h.ServeHTTP(w, r)
	return w.Result(), nil
}
