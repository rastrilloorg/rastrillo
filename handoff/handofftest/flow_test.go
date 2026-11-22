package handofftest_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/csrf"
	"amadan.net/rastrillo/rastrillo/dbtest"
	"amadan.net/rastrillo/rastrillo/handoff"
	"amadan.net/rastrillo/rastrillo/handoff/handofftest"
	"amadan.net/rastrillo/rastrillo/migrate"
	"amadan.net/rastrillo/rastrillo/rootcall"
	"amadan.net/rastrillo/rastrillo/roster"
	"amadan.net/rastrillo/rastrillo/sessions"
)

const instanceOrigin = "https://acme.test"

var schema = dbtest.FromSet(migrate.Merge(
	sessions.Schema, handoff.Schema, roster.Schema, rootcall.Schema))

func TestMain(tests *testing.M) {
	code := tests.Run()
	schema.Remove()
	os.Exit(code)
}

type memoryStore struct {
	mutex   sync.Mutex
	members map[string]roster.Member
}

func (store *memoryStore) Members(context.Context) ([]roster.Member, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	var members []roster.Member
	for _, member := range store.members {
		members = append(members, member)
	}
	return members, nil
}

func (store *memoryStore) Put(_ context.Context, member roster.Member) error {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	store.members[member.Subject] = member
	return nil
}

func (store *memoryStore) Remove(_ context.Context, subject string) error {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	delete(store.members, subject)
	return nil
}

// browser carries cookies between requests the way a person's would.
type browser struct {
	handler http.Handler
	cookies map[string]*http.Cookie
}

func (person *browser) do(
	t *testing.T, method, target string, form url.Values,
) *http.Response {
	t.Helper()
	body := strings.NewReader(form.Encode())
	request := httptest.NewRequest(method, instanceOrigin+target, body)
	if form != nil {
		request.Header.Set(
			"Content-Type", "application/x-www-form-urlencoded")
	}
	for _, cookie := range person.cookies {
		request.AddCookie(cookie)
	}
	recorder := httptest.NewRecorder()
	person.handler.ServeHTTP(recorder, request)
	response := recorder.Result()
	for _, cookie := range response.Cookies() {
		if cookie.MaxAge < 0 {
			delete(person.cookies, cookie.Name)
			continue
		}
		person.cookies[cookie.Name] = cookie
	}
	return response
}

func TestTheWholeFlow(t *testing.T) {
	now := time.Now()
	clock := func() time.Time { return now }
	root := handofftest.New(t)
	root.Now = clock
	setup := root.Setup()
	db := schema.Open(t)
	store, err := sessions.New(
		sessions.Config{DB: db, Origin: instanceOrigin})
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := roster.NewVerifier(
		setup.Root, instanceOrigin, setup.Keys)
	if err != nil {
		t.Fatal(err)
	}
	receiver, err := roster.New(roster.Config{
		Verifier: verifier,
		Store:    &memoryStore{members: map[string]roster.Member{}},
		DB:       db,
		Roles:    []string{"owner"},
		Now:      clock,
	})
	if err != nil {
		t.Fatal(err)
	}
	entrance, err := handoff.New(handoff.Config{
		Root:      setup.Root,
		Keys:      setup.Keys,
		Origin:    instanceOrigin,
		Sessions:  store,
		DB:        db,
		Suspended: receiver.Suspended,
		Now:       clock,
		Logger:    slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+handoff.EnterPath, entrance.Enter)
	mux.HandleFunc("POST "+handoff.EnterPath, entrance.Consume)
	mux.Handle(roster.Path, receiver)
	mux.Handle("GET /dash", entrance.Renew(http.HandlerFunc(
		func(response http.ResponseWriter, request *http.Request) {
			session, _ := sessions.Current(request)
			response.Write([]byte(session.Subject))
		})))
	// Both are posted by the root, so neither carries same-origin proof.
	protect := csrf.Protect(instanceOrigin,
		csrf.Exempt(handoff.EnterPath, roster.Path))
	person := &browser{
		handler: protect(store.Middleware(mux)),
		cookies: map[string]*http.Cookie{},
	}
	member := roster.Member{
		Subject: "u1", Email: "u1@acme.test", Name: "U One", Role: "owner",
	}

	post := func(token string) int {
		t.Helper()
		request := httptest.NewRequest(
			"POST", instanceOrigin+roster.Path, strings.NewReader(token))
		recorder := httptest.NewRecorder()
		person.handler.ServeHTTP(recorder, request)
		return recorder.Code
	}
	signIn := func() *http.Response {
		t.Helper()
		redirect := person.do(t, "GET", handoff.EnterPath+"?next=/dash", nil)
		if redirect.StatusCode != http.StatusSeeOther {
			t.Fatalf("enter status = %d, want 303", redirect.StatusCode)
		}
		form := root.Assert(t, redirect, "u1")
		return person.do(t, "POST", handoff.EnterPath, form)
	}

	snapshot := root.Roster(t, instanceOrigin, 1, roster.Active, member)
	if code := post(snapshot); code != http.StatusNoContent {
		t.Fatalf("roster status = %d, want 204", code)
	}

	landed := signIn()
	if landed.StatusCode != http.StatusSeeOther ||
		landed.Header.Get("Location") != "/dash" {
		t.Fatalf("consume = %d to %q, want 303 to /dash",
			landed.StatusCode, landed.Header.Get("Location"))
	}
	dash := person.do(t, "GET", "/dash", nil)
	if dash.StatusCode != http.StatusOK {
		t.Fatalf("dash status = %d, want 200", dash.StatusCode)
	}
	if body := readBody(t, dash); body != "u1" {
		t.Errorf("dash body = %q, want u1", body)
	}

	now = now.Add(61 * time.Minute)
	stale := person.do(t, "GET", "/dash", nil)
	if stale.StatusCode != http.StatusSeeOther {
		t.Fatalf("stale dash status = %d, want 303", stale.StatusCode)
	}
	if got, want := stale.Header.Get("Location"),
		handoff.EnterPath+"?next=%2Fdash"; got != want {
		t.Errorf("stale dash Location = %q, want %q", got, want)
	}

	suspension := root.Roster(t, instanceOrigin, 2, roster.Suspended, member)
	if code := post(suspension); code != http.StatusNoContent {
		t.Fatalf("suspension status = %d, want 204", code)
	}
	if refused := signIn(); refused.StatusCode != http.StatusForbidden {
		t.Errorf("consume while suspended = %d, want 403",
			refused.StatusCode)
	}
}

func readBody(t *testing.T, response *http.Response) string {
	t.Helper()
	var builder strings.Builder
	buffer := make([]byte, 512)
	for {
		count, err := response.Body.Read(buffer)
		builder.Write(buffer[:count])
		if err != nil {
			break
		}
	}
	return builder.String()
}

func TestTheFakeRootCallsAGuardedHandlerOnce(t *testing.T) {
	root := handofftest.New(t)
	setup := root.Setup()
	guard, err := rootcall.New(rootcall.Config{
		Root: setup.Root, Keys: setup.Keys, Audience: instanceOrigin,
		DB: schema.Open(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	var plans []string
	handler := guard.Wrap(http.HandlerFunc(
		func(response http.ResponseWriter, request *http.Request) {
			body, _ := io.ReadAll(request.Body)
			plans = append(plans, string(body))
			response.WriteHeader(http.StatusNoContent)
		}))
	call := root.Call(t, http.MethodPost,
		instanceOrigin+"/_carlos/plan", []byte(`{"plan":"paid"}`))
	again := call.Clone(context.Background())
	again.Body = io.NopCloser(strings.NewReader(`{"plan":"paid"}`))
	wants := []int{http.StatusNoContent, http.StatusForbidden}
	for index, request := range []*http.Request{call, again} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != wants[index] {
			t.Fatalf("call %d: status = %d, want %d",
				index+1, response.Code, wants[index])
		}
	}
	if len(plans) != 1 || plans[0] != `{"plan":"paid"}` {
		t.Fatalf("handler read %q, want the plan once", plans)
	}
}
