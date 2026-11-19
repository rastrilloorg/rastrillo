package chattest

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// Not parallel: t.Setenv names the admin for the whole process.
func TestTwoPeopleChatInARoom(t *testing.T) {
	t.Setenv("CHAT_ADMINS", "ada@example.com")
	handler, box := newAppWithMail(t)
	ada := signIn(t, handler, box, "ada@example.com")
	bob := signIn(t, handler, box, "bob@example.com")
	added := postAs(t, handler, "/admin/rooms",
		url.Values{"Name": {"General"}}, ada)
	if added.Code != http.StatusSeeOther {
		t.Fatalf("Ada adding a room = %d, want 303:\n%s", added.Code,
			added.Body)
	}
	if home := getAs(t, handler, "/", bob).Body.String(); !strings.Contains(
		home, "General") {
		t.Errorf("Bob's room list lacks General:\n%s", home)
	}
	sent := postAs(t, handler, "/rooms/1/messages",
		url.Values{"Text": {"Hello, Ada"}}, bob)
	if sent.Code != http.StatusSeeOther {
		t.Fatalf("Bob posting = %d, want 303:\n%s", sent.Code, sent.Body)
	}
	room := getAs(t, handler, "/rooms/1", ada).Body.String()
	for _, want := range []string{"General", "bob@example.com",
		"Hello, Ada"} {
		if !strings.Contains(room, want) {
			t.Errorf("Ada's view of the room lacks %q:\n%s", want, room)
		}
	}
}

func TestAnEmptyMessageIsRefused(t *testing.T) {
	t.Setenv("CHAT_ADMINS", "ada@example.com")
	handler, box := newAppWithMail(t)
	ada := signIn(t, handler, box, "ada@example.com")
	postAs(t, handler, "/admin/rooms", url.Values{"Name": {"General"}}, ada)
	refused := postAs(t, handler, "/rooms/1/messages",
		url.Values{"Text": {"   "}}, ada)
	if refused.Code != http.StatusUnprocessableEntity ||
		!strings.Contains(refused.Body.String(), "Write a message") {
		t.Errorf("an empty message = %d, want 422 saying why:\n%s",
			refused.Code, refused.Body)
	}
}

// Someone who is not an admin gets 404, as if the pages were not there.
func TestOnlyAdminsSeeTheAdminPages(t *testing.T) {
	t.Setenv("CHAT_ADMINS", "ada@example.com")
	handler, box := newAppWithMail(t)
	bob := signIn(t, handler, box, "bob@example.com")
	if page := getAs(t, handler, "/admin/rooms", bob); page.Code !=
		http.StatusNotFound {
		t.Errorf("Bob opening the admin = %d, want 404", page.Code)
	}
	added := postAs(t, handler, "/admin/rooms",
		url.Values{"Name": {"Secret"}}, bob)
	if added.Code != http.StatusNotFound {
		t.Errorf("Bob adding a room = %d, want 404", added.Code)
	}
	if page := get(t, handler, "/admin/rooms"); page.Code !=
		http.StatusSeeOther {
		t.Errorf("a signed-out visit = %d, want 303 to sign in",
			page.Code)
	}
}

func postAs(
	t *testing.T, handler http.Handler, target string, form url.Values,
	cookie *http.Cookie,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, target,
		strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Origin", testOrigin)
	request.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}
