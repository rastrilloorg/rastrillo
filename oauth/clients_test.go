package oauth_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/oauth"
)

func newClients(t *testing.T) (*oauth.Clients, oauth.Store, *time.Time) {
	t.Helper()
	store, now := newStore(t), t0
	clients := oauth.NewClients(store, oauth.NewFetcher(), 0,
		func() time.Time { return now })
	return clients, store, &now
}

func post(
	clients *oauth.Clients,
	addr, body string,
) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/oauth/register",
		strings.NewReader(body))
	request.RemoteAddr = addr + ":4000"
	recorder := httptest.NewRecorder()
	clients.Register(recorder, request)
	return recorder
}

const goodBody = `{"client_name":"Agent",
 "redirect_uris":["https://agent.example/cb","http://127.0.0.1:9/cb"],
 "grant_types":["authorization_code","refresh_token"],
 "response_types":["code"],"token_endpoint_auth_method":"none"}`

func TestRegister(t *testing.T) {
	clients, _, _ := newClients(t)
	recorder := post(clients, "192.0.2.1", goodBody)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body)
	}
	if cc := recorder.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q", cc)
	}
	var got struct {
		ClientID     string   `json:"client_id"`
		ClientName   string   `json:"client_name"`
		RedirectURIs []string `json:"redirect_uris"`
		GrantTypes   []string `json:"grant_types"`
		ResponseType []string `json:"response_types"`
		AuthMethod   string   `json:"token_endpoint_auth_method"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^[0-9A-Za-z]{22}$`).MatchString(got.ClientID) {
		t.Errorf("client_id = %q", got.ClientID)
	}
	if got.ClientName != "Agent" || len(got.RedirectURIs) != 2 ||
		len(got.GrantTypes) != 2 || got.ResponseType[0] != "code" ||
		got.AuthMethod != "none" {
		t.Errorf("metadata = %+v", got)
	}
	cl, err := clients.Resolve(context.Background(), got.ClientID)
	if err != nil {
		t.Fatal(err)
	}
	if cl.Name != "Agent" || cl.Document || len(cl.Redirects) != 2 {
		t.Errorf("resolved = %+v", cl)
	}
	again := post(clients, "192.0.2.1", goodBody)
	if again.Body.String() == recorder.Body.String() {
		t.Error("two registrations got the same client_id")
	}
}

// Every client gets both grant types, so the answer says both whatever
// was asked: a client that named one would get the other all the same.
func TestRegisterAnswersGrantTypesGranted(t *testing.T) {
	for _, asked := range []string{
		``, `,"grant_types":[]`, `,"grant_types":["authorization_code"]`,
		`,"grant_types":["refresh_token"]`,
	} {
		clients, _, _ := newClients(t)
		recorder := post(clients, "192.0.2.1", `{"client_name":"A",
 "redirect_uris":["https://a.example/cb"]`+asked+`}`)
		if recorder.Code != http.StatusCreated {
			t.Fatalf("%s: status %d: %s", asked, recorder.Code, recorder.Body)
		}
		var got struct {
			GrantTypes []string `json:"grant_types"`
		}
		_ = json.Unmarshal(recorder.Body.Bytes(), &got)
		if !slices.Equal(got.GrantTypes,
			[]string{"authorization_code", "refresh_token"}) {
			t.Errorf("asked %q: grant_types = %v", asked, got.GrantTypes)
		}
	}
}

func TestRegisterRefuses(t *testing.T) {
	tests := []struct{ name, body, code string }{
		{"http redirect", `{"client_name":"A",
 "redirect_uris":["http://app.example/cb"]}`, "invalid_redirect_uri"},
		{"custom scheme", `{"client_name":"A",
 "redirect_uris":["app://cb"]}`, "invalid_redirect_uri"},
		{"fragment", `{"client_name":"A",
 "redirect_uris":["https://a.example/cb#x"]}`, "invalid_redirect_uri"},
		{"no redirects", `{"client_name":"A"}`, "invalid_redirect_uri"},
		{"quote in redirect", `{"client_name":"A",
 "redirect_uris":["https://a.example/c\"b"]}`, "invalid_redirect_uri"},
		{"space in redirect", `{"client_name":"A",
 "redirect_uris":["https://a.example/c b"]}`, "invalid_redirect_uri"},
		{"client_secret_basic", `{"client_name":"A",
 "redirect_uris":["https://a.example/cb"],
 "token_endpoint_auth_method":"client_secret_basic"}`,
			"invalid_client_metadata"},
		{"implicit grant", `{"client_name":"A",
 "redirect_uris":["https://a.example/cb"],"grant_types":["implicit"]}`,
			"invalid_client_metadata"},
		{"token response", `{"client_name":"A",
 "redirect_uris":["https://a.example/cb"],"response_types":["token"]}`,
			"invalid_client_metadata"},
		{"no name", `{"redirect_uris":["https://a.example/cb"]}`,
			"invalid_client_metadata"},
		{"bidi name", `{"client_name":"A\u202eB",
 "redirect_uris":["https://a.example/cb"]}`, "invalid_client_metadata"},
		{"long name", `{"client_name":"` + strings.Repeat("a", 101) + `",
 "redirect_uris":["https://a.example/cb"]}`, "invalid_client_metadata"},
		{"trailing data", goodBody + `{}`, "invalid_client_metadata"},
		{"not json", `nope`, "invalid_client_metadata"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clients, _, _ := newClients(t)
			recorder := post(clients, "192.0.2.1", tt.body)
			var got struct {
				Error       string `json:"error"`
				Description string `json:"error_description"`
			}
			_ = json.Unmarshal(recorder.Body.Bytes(), &got)
			if recorder.Code != http.StatusBadRequest || got.Error != tt.code ||
				got.Description == "" {
				t.Fatalf("status %d, body %s", recorder.Code, recorder.Body)
			}
			if cc := recorder.Header().Get("Cache-Control"); cc != "no-store" {
				t.Errorf("Cache-Control = %q", cc)
			}
		})
	}
}

func TestRegisterRateLimited(t *testing.T) {
	clients, _, now := newClients(t)
	for index := 1; index <= 10; index++ {
		if recorder := post(clients, "192.0.2.1",
			goodBody); recorder.Code != 201 {
			t.Fatalf("request %d: status %d", index, recorder.Code)
		}
	}
	answer := post(clients, "192.0.2.1", goodBody)
	if answer.Code != http.StatusTooManyRequests {
		t.Fatalf("the eleventh got %d", answer.Code)
	}
	if answer.Header().Get("Cache-Control") != "no-store" {
		t.Error("429 is cacheable")
	}
	if ra := answer.Header().Get("Retry-After"); ra != "60" {
		t.Errorf("Retry-After = %q, want 60", ra)
	}
	if recorder := post(clients, "192.0.2.2", goodBody); recorder.Code != 201 {
		t.Fatalf("another address got %d", recorder.Code)
	}
	*now = now.Add(61 * time.Second)
	if recorder := post(clients, "192.0.2.1", goodBody); recorder.Code != 201 {
		t.Fatalf("after a minute got %d", recorder.Code)
	}
}

func TestRegisterLimitsIPv6ByPrefix(t *testing.T) {
	clients, _, _ := newClients(t)
	for index := range 10 {
		addr := fmt.Sprintf("[2001:db8:1:2::%x]", index+1)
		if recorder := post(clients, addr, goodBody); recorder.Code != 201 {
			t.Fatalf("request %d: status %d", index, recorder.Code)
		}
	}
	if recorder := post(clients, "[2001:db8:1:2:ffff::1]",
		goodBody); recorder.Code != 429 {
		t.Fatalf("same /64 got %d, want 429", recorder.Code)
	}
	if recorder := post(clients, "[2001:db8:1:3::1]",
		goodBody); recorder.Code != 201 {
		t.Fatalf("another /64 got %d", recorder.Code)
	}
}

func TestLimiterIsBounded(t *testing.T) {
	clients, _, now := newClients(t)
	for index := range 5000 {
		clients.Allow(fmt.Sprintf("10.%d.%d.1", index/256, index%256), *now)
	}
	if count := clients.LimiterSize(); count != 4096 {
		t.Fatalf("%d keys held, want 4096", count)
	}
	// The newest keys survive; the oldest were evicted.
	for index := range 9 {
		if !clients.Allow("10.19.135.1", *now) {
			t.Fatalf("recent key lost its count after %d more", index)
		}
	}
	if clients.Allow("10.19.135.1", *now) {
		t.Fatal("the tenth hit was allowed")
	}
}

type failingPrune struct{ oauth.Store }

func (failingPrune) PruneClients(context.Context, time.Time) error {
	return errors.New("disk on fire")
}

func TestRegisterLogsPruneFailure(t *testing.T) {
	store, now := newStore(t), t0
	clients := oauth.NewClients(failingPrune{store}, oauth.NewFetcher(), 0,
		func() time.Time { return now })
	var buf bytes.Buffer
	clients.SetLogger(slog.New(slog.NewTextHandler(&buf, nil)))
	if recorder := post(clients, "192.0.2.1", goodBody); recorder.Code != 201 {
		t.Fatalf("status %d", recorder.Code)
	}
	if !strings.Contains(buf.String(), "disk on fire") {
		t.Fatalf("prune failure not logged: %q", buf.String())
	}
}

func TestRegisterPrunesHourly(t *testing.T) {
	clients, store, now := newClients(t)
	ctx := context.Background()
	stale := func(id string) {
		t.Helper()
		err := store.SaveClient(ctx, oauth.Client{ID: id, Name: "Old",
			Redirects:    []string{"https://o.example/cb"},
			RegisteredAt: t0.Add(-90 * 24 * time.Hour),
			UsedAt:       now.Add(-31 * 24 * time.Hour)})
		if err != nil {
			t.Fatal(err)
		}
	}
	has := func(id string) bool {
		_, err := store.Client(ctx, id)
		return err == nil
	}
	stale("one")
	post(clients, "192.0.2.1", goodBody)
	if has("one") {
		t.Fatal("an unused client survived the first registration")
	}
	stale("two")
	*now = now.Add(30 * time.Minute)
	post(clients, "192.0.2.1", goodBody)
	if !has("two") {
		t.Fatal("pruned twice within the hour")
	}
	*now = now.Add(31 * time.Minute)
	post(clients, "192.0.2.1", goodBody)
	if has("two") {
		t.Fatal("not pruned after the hour")
	}
}

func TestResolve(t *testing.T) {
	ctx := context.Background()
	fetcher, base, _ := docServer(t, goodDoc)
	store := newStore(t)
	now := t0
	clients := oauth.NewClients(
		store,
		fetcher,
		0,
		func() time.Time { return now },
	)

	doc, err := clients.Resolve(ctx, base+"/client.json")
	if err != nil || !doc.Document || doc.Name != "Agent" {
		t.Fatalf("document client = %+v, %v", doc, err)
	}
	for _, id := range []string{"missing", "https://example.com", ""} {
		if _, err := clients.Resolve(ctx, id); !errors.Is(
			err,
			oauth.ErrNotFound,
		) {
			t.Errorf("Resolve(%q) = %v, want ErrNotFound", id, err)
		}
	}
}
