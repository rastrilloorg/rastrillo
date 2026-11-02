package mcp

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	rastrillo "amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/api"
	"amadan.net/rastrillo/rastrillo/op"
)

// apiCreate is bookmarks.create on the API door, as a generated handler
// writes it: api.Decode, the operation, then Refuse or Write.
func apiCreate(t *testing.T, store *bookmarkStore) string {
	t.Helper()
	var descriptor op.Descriptor
	for _, each := range store.descriptors() {
		if each.ID == "bookmarks.create" {
			descriptor = each
		}
	}
	handler, err := api.New(api.Config{
		Verify: fakeVerifier{},
		Ctx: func(*http.Request) *rastrillo.Ctx {
			return &rastrillo.Ctx{}
		},
		Authorize: op.DefaultPolicy,
		Logger:    slog.New(slog.DiscardHandler),
	}, api.Route{
		Method: http.MethodPost, Pattern: "/api/v1/bookmarks",
		Op: descriptor,
		Handle: func(_ context.Context, rc *rastrillo.Ctx,
			response http.ResponseWriter, request *http.Request,
		) {
			var input bookmarkInput
			if !api.Decode(response, request, &input) {
				return
			}
			row, err := store.create(rc.Actor.Subject, input)
			if err != nil {
				api.Refuse(response, request, err)
				return
			}
			api.Write(response, http.StatusCreated, row)
		},
	})
	if err != nil {
		t.Fatalf("api.New: %v", err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server.URL + "/api/v1/bookmarks"
}

// apiFields is the fields of the API door's refusal of body.
func apiFields(t *testing.T, url, body string) map[string]string {
	t.Helper()
	answer := post(t, http.MethodPost, url, writerToken, "", body)
	var envelope struct{ Error api.Error }
	if err := json.Unmarshal(answer.body, &envelope); err != nil ||
		answer.status != http.StatusUnprocessableEntity {
		t.Fatalf("the API answered %d %s, want a 422",
			answer.status, answer.body)
	}
	return envelope.Error.Fields
}

// Both doors name a refused member the same way: a misspelt one in the
// client's own spelling, a missing one by its wire name.
func TestDecodeFieldsMatchTheAPI(t *testing.T) {
	store := newBookmarkStore()
	apiURL := apiCreate(t, store)
	mcpURL := serve(t, testConfig(), store.descriptors()...)
	for name, arguments := range map[string]string{
		"wrong_case": `{"Title":"x"}`,
		"missing":    `{"url":"https://example.com"}`,
	} {
		want := apiFields(t, apiURL, arguments)
		answer := post(t, http.MethodPost, mcpURL, writerToken,
			"2025-06-18", callBody("bookmarks_create", arguments))
		got := answer.call(t).refusal(t).Fields
		if !reflect.DeepEqual(got, want) || len(want) == 0 {
			t.Errorf("%s: MCP fields %v, the API's %v", name, got, want)
		}
	}
}
