package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	rastrillo "amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/op"
)

// conflictSecret is what a refusal discloses; once the token is
// narrowed, the answer must not carry it.
const conflictSecret = "Someone else renamed it to Payroll 2027."

// An error answer discloses as surely as a result does, so the gate
// rechecks the operation for it too: narrowed during the call, the
// caller gets the gate's 403, not the refusal the call earned.
func TestCallErrorsAreRechecked(t *testing.T) {
	tests := map[string]func(*fakeRechecker) (any, error){
		"conflict": func(checker *fakeRechecker) (any, error) {
			checker.narrowed.Store(true)
			return nil, op.Conflict{Message: conflictSecret}
		},
		"panic": func(checker *fakeRechecker) (any, error) {
			checker.narrowed.Store(true)
			panic(conflictSecret)
		},
	}
	for name, invoke := range tests {
		t.Run(name, func(t *testing.T) {
			checker := &fakeRechecker{}
			descriptors := withInvoke(bookmarks(), "bookmarks.update",
				func(context.Context, *rastrillo.Ctx, json.RawMessage) (
					any, error,
				) {
					return invoke(checker)
				})
			url := serve(t, countedConfig(checker).config, descriptors...)
			answer := post(t, http.MethodPost, url, writerToken,
				"2025-06-18", callBody("bookmarks_update", `{"id":"1"}`))
			if answer.status != http.StatusForbidden ||
				strings.Contains(string(answer.body), "Payroll") {
				t.Errorf("answer %d %s, want the gate's 403",
					answer.status, answer.body)
			}
		})
	}
}

// A hand-written Invoke that forgets op.Check still meets the app's
// policy: the call runs it before Invoke, as the list does.
func TestCallChecksPolicyBeforeInvoke(t *testing.T) {
	var invoked atomic.Int64
	descriptors := withInvoke(bookmarks(), "bookmarks.get",
		func(context.Context, *rastrillo.Ctx, json.RawMessage) (
			any, error,
		) {
			invoked.Add(1)
			return map[string]any{"id": "1"}, nil
		})
	config := testConfig()
	config.Authorize = func(
		_ context.Context, _ rastrillo.Actor, operation rastrillo.Operation,
	) error {
		if operation.ID == "bookmarks.get" {
			return op.Forbidden{Reason: "not this one"}
		}
		return nil
	}
	url := serve(t, config, descriptors...)
	answer := post(t, http.MethodPost, url, writerToken, "2025-06-18",
		callBody("bookmarks_get", `{"id":"1"}`))
	refused := answer.call(t).refusal(t)
	if refused.Code != "forbidden" || invoked.Load() != 0 {
		t.Errorf("answered %+v after %d Invokes, want forbidden and none",
			refused, invoked.Load())
	}
}

// A tool with an object Result advertises an output schema, and a
// client may then insist on structuredContent: null is the app's bug.
func TestCallResultMustBeAnObject(t *testing.T) {
	for name, value := range map[string]any{
		"nil": nil, "string": "1", "array": []string{"1"},
	} {
		descriptors := withInvoke(bookmarks(), "bookmarks.get",
			func(context.Context, *rastrillo.Ctx, json.RawMessage) (
				any, error,
			) {
				return value, nil
			})
		url := serve(t, testConfig(), descriptors...)
		answer := post(t, http.MethodPost, url, writerToken, "2025-06-18",
			callBody("bookmarks_get", `{"id":"1"}`))
		refused := answer.call(t).refusal(t)
		if refused.Code != "internal" || refused.Ref == "" {
			t.Errorf("a %s result answered %+v, want internal with a ref",
				name, refused)
		}
	}
}
