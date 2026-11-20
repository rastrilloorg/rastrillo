package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/objrec"
	"amadan.net/rastrillo/rastrillo/sessions"
)

// fakeClock is Config.Now for a test that moves time.
type fakeClock struct{ at time.Time }

func (clock *fakeClock) now() time.Time { return clock.at }

// peopleFixture is a records-mode Auth on a fake clock that records the
// addresses Config.Refused was called with.
type peopleFixture struct {
	auth    *Auth
	mailer  *captureMailer
	store   objrec.Store
	clock   *fakeClock
	refused []string
}

func newPeople(t *testing.T) *peopleFixture {
	t.Helper()
	fixture := &peopleFixture{clock: &fakeClock{
		at: time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC),
	}}
	fixture.auth, fixture.mailer, fixture.store = newRecordsAuth(t,
		func(config *Config) {
			config.Now = fixture.clock.now
			config.Refused = func(
				response http.ResponseWriter, _ *http.Request, id Identity,
			) {
				fixture.refused = append(fixture.refused, id.Address)
				http.Error(response, "refused", http.StatusForbidden)
			}
		})
	return fixture
}

func (fixture *peopleFixture) signIn(t *testing.T) *http.Cookie {
	t.Helper()
	return signInAt(t, fixture.auth, fixture.mailer, "ana@example.test")
}

func TestSuspendedUserCannotSignIn(t *testing.T) {
	fixture := newPeople(t)
	ctx := context.Background()
	user, err := fixture.auth.EnsureUser(ctx, "ana@example.test")
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.auth.SetSuspended(ctx, user.ID, true); err != nil {
		t.Fatal(err)
	}
	beginSignin(t, fixture.auth, "ana@example.test")
	link := recordsLinkRE.FindString(fixture.mailer.sentBody())
	response := httptest.NewRecorder()
	fixture.auth.Verify(response, redeemRequest(t, link))
	if response.Code != http.StatusForbidden ||
		!slices.Equal(fixture.refused, []string{"ana@example.test"}) {
		t.Fatalf("%d, refused %v; want a 403 through Refused",
			response.Code, fixture.refused)
	}
	if cookie := sessionCookieSet(fixture.auth, response); cookie != nil &&
		cookie.Value != "" {
		t.Fatalf("a suspended person was given a session cookie: %v", cookie)
	}
}

func TestSuspendedUserCannotRefresh(t *testing.T) {
	fixture := newPeople(t)
	cookie := fixture.signIn(t)
	uid := subjectOf(t, fixture.auth, cookie)
	err := fixture.auth.SetSuspended(context.Background(), uid, true)
	if err != nil {
		t.Fatal(err)
	}
	fixture.clock.at = fixture.clock.at.Add(16 * time.Minute)
	response := refreshWith(fixture.auth, cookie)
	cleared := sessionCookieSet(fixture.auth, response)
	if !strings.HasPrefix(response.Header().Get("Location"), "/signin") ||
		cleared == nil || cleared.MaxAge >= 0 {
		t.Fatalf("refresh: %q, cookie %v; want sign-in, cookie deleted",
			response.Header().Get("Location"), cleared)
	}
}

func TestResumedUserSignsIn(t *testing.T) {
	fixture := newPeople(t)
	ctx := context.Background()
	user, err := fixture.auth.EnsureUser(ctx, "ana@example.test")
	if err != nil {
		t.Fatal(err)
	}
	for _, suspended := range []bool{true, false} {
		err := fixture.auth.SetSuspended(ctx, user.ID, suspended)
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := subjectOf(t, fixture.auth, fixture.signIn(t)); got != user.ID {
		t.Fatalf("signed in as %q, want %q", got, user.ID)
	}
}

func TestSuspendKeepsOtherFields(t *testing.T) {
	fixture := newPeople(t)
	ctx := context.Background()
	before, err := fixture.auth.EnsureUser(ctx, "ana@example.test")
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.auth.SetSuspended(ctx, before.ID, true); err != nil {
		t.Fatal(err)
	}
	after, err := fixture.auth.User(ctx, before.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Email != before.Email || after.EH != before.EH ||
		!after.Created.Equal(before.Created) ||
		after.Epoch != before.Epoch+1 || !after.Suspended {
		t.Fatalf("before %+v, after %+v", before, after)
	}
	unknown := "aaaaaaaaaaaaaaaaaaaaaaaaaa"
	err = fixture.auth.SetSuspended(ctx, unknown, true)
	if !errors.Is(err, sessions.ErrNoUser) {
		t.Fatalf("an unknown uid: %v, want sessions.ErrNoUser", err)
	}
}

func TestUserByEmailNeverCreates(t *testing.T) {
	fixture := newPeople(t)
	ctx := context.Background()
	_, err := fixture.auth.UserByEmail(ctx, "nobody@example.test")
	if !errors.Is(err, objrec.ErrNotFound) {
		t.Fatalf("an unknown address: %v, want objrec.ErrNotFound", err)
	}
	if keys, _, _ := fixture.store.List(ctx, "emails/", "", 0); len(keys) != 0 {
		t.Fatalf("UserByEmail wrote %v", keys)
	}
	made, err := fixture.auth.EnsureUser(ctx, " Ana@Example.test ")
	if err != nil {
		t.Fatal(err)
	}
	found, err := fixture.auth.UserByEmail(ctx, "ana@example.test")
	if err != nil || found.ID != made.ID {
		t.Fatalf("UserByEmail: %+v, %v; want %s", found, err, made.ID)
	}
}

func TestUsersPages(t *testing.T) {
	fixture := newPeople(t)
	ctx := context.Background()
	addresses := []string{"a@example.test", "b@example.test", "c@example.test"}
	for _, address := range addresses {
		user, err := fixture.auth.EnsureUser(ctx, address)
		if err != nil {
			t.Fatal(err)
		}
		stray := "users/" + user.ID + "/instances/x.json"
		_, err = fixture.store.Create(ctx, stray, []byte("{}"))
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := fixture.auth.Users(ctx, "", 0); err == nil {
		t.Fatal("Users with a limit of 0 should be refused")
	}
	first, cursor, err := fixture.auth.Users(ctx, "", 2)
	if err != nil || len(first) != 2 || cursor == "" {
		t.Fatalf("first page: %d users, cursor %q, %v",
			len(first), cursor, err)
	}
	second, cursor, err := fixture.auth.Users(ctx, cursor, 2)
	if err != nil || len(second) != 1 || cursor != "" {
		t.Fatalf("second page: %d users, cursor %q, %v",
			len(second), cursor, err)
	}
	var got []string
	for _, user := range append(first, second...) {
		got = append(got, user.Email)
	}
	slices.Sort(got)
	if !slices.Equal(got, addresses) {
		t.Fatalf("the pages hold %v, want each of %v once", got, addresses)
	}
}

func TestSessionCurrentRefusesOldEpoch(t *testing.T) {
	ctx := context.Background()
	ways := map[string]func(*Auth, string) error{
		"EndSessions": func(auth *Auth, uid string) error {
			return auth.EndSessions(ctx, uid)
		},
		"SetSuspended": func(auth *Auth, uid string) error {
			return auth.SetSuspended(ctx, uid, true)
		},
	}
	for name, end := range ways {
		t.Run(name, func(t *testing.T) {
			fixture := newPeople(t)
			cookie := fixture.signIn(t)
			request := httptest.NewRequest("GET", "https://ppl.test/", nil)
			request.AddCookie(cookie)
			if _, err := fixture.auth.SessionCurrent(request); err != nil {
				t.Fatalf("before: %v", err)
			}
			uid := subjectOf(t, fixture.auth, cookie)
			if err := end(fixture.auth, uid); err != nil {
				t.Fatal(err)
			}
			if _, ok := fixture.auth.SessionFrom(request); !ok {
				t.Fatal("SessionFrom should answer inside the cookie's life")
			}
			_, err := fixture.auth.SessionCurrent(request)
			if !errors.Is(err, sessions.ErrNoUser) {
				t.Fatalf("SessionCurrent: %v, want sessions.ErrNoUser", err)
			}
		})
	}
}

func TestConfigNowDrivesSessions(t *testing.T) {
	fixture := newPeople(t)
	cookie := fixture.signIn(t)
	request := httptest.NewRequest("GET", "https://ppl.test/account", nil)
	request.AddCookie(cookie)
	teapot := http.HandlerFunc(
		func(response http.ResponseWriter, _ *http.Request) {
			response.WriteHeader(http.StatusTeapot)
		})
	response := httptest.NewRecorder()
	fixture.auth.RequireSession(teapot).ServeHTTP(response, request)
	if response.Code != http.StatusTeapot {
		t.Fatalf("a fresh cookie: %d", response.Code)
	}
	fixture.clock.at = fixture.clock.at.Add(16 * time.Minute)
	response = httptest.NewRecorder()
	fixture.auth.RequireSession(teapot).ServeHTTP(response, request)
	want := "https://ppl.test/_carlos/refresh?next=%2Faccount"
	if response.Code != http.StatusSeeOther ||
		response.Header().Get("Location") != want {
		t.Fatalf("after 16 minutes: %d %q, want 303 to %q",
			response.Code, response.Header().Get("Location"), want)
	}
}

// users.epoch must refuse a suspended user by itself, not only because
// the epoch moved.
func TestSuspendedWithTheSameEpochIsRefused(t *testing.T) {
	fixture := newPeople(t)
	ctx := context.Background()
	cookie := fixture.signIn(t)
	uid := subjectOf(t, fixture.auth, cookie)
	user, version, err := objrec.GetJSON[User](ctx, fixture.store, userKey(uid))
	if err != nil {
		t.Fatal(err)
	}
	user.Suspended = true
	_, err = objrec.UpdateJSON(ctx, fixture.store, userKey(uid), user, version)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("GET", "https://ppl.test/", nil)
	request.AddCookie(cookie)
	_, err = fixture.auth.SessionCurrent(request)
	if !errors.Is(err, sessions.ErrNoUser) {
		t.Fatalf("SessionCurrent: %v, want sessions.ErrNoUser", err)
	}
	fixture.clock.at = fixture.clock.at.Add(16 * time.Minute)
	response := refreshWith(fixture.auth, cookie)
	if !strings.HasPrefix(response.Header().Get("Location"), "/signin") {
		t.Fatalf("refresh: %q, want sign-in", response.Header().Get("Location"))
	}
}

func TestSetSuspendedInBlindModeStoresNoAddress(t *testing.T) {
	var config Config
	plain, _, store := newRecordsAuth(t,
		func(built *Config) { config = *built })
	ctx := context.Background()
	user, err := plain.EnsureUser(ctx, "ana@example.test")
	if err != nil {
		t.Fatal(err)
	}
	if !emailMember(t, store, user.ID) {
		t.Fatal("no address stored before Blind: the check proves nothing")
	}
	blind := reopenBlind(t, config)
	if err := blind.SetSuspended(ctx, user.ID, true); err != nil {
		t.Fatal(err)
	}
	if emailMember(t, store, user.ID) {
		t.Fatal("SetSuspended wrote the address back in blind mode")
	}
}
