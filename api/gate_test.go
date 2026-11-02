package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	rastrillo "amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/op"
)

// writeThing is the write operation both sides of the comparison serve.
var writeThing = desc("things.create", rastrillo.Write).Operation

// gateHandler is what an MCP-shaped caller of Wrap looks like: no route,
// so it calls Admit itself once it knows the operation.
func gateHandler(gate Gate) http.Handler {
	return http.HandlerFunc(func(
		response http.ResponseWriter, request *http.Request,
	) {
		actor, _ := rastrillo.ActorFromContext(request.Context())
		if err := gate.Admit(actor, writeThing); err != nil {
			Refuse(response, request, err)
			return
		}
		if request.URL.Path == "/api/v1/panics" {
			panic("boom")
		}
		Write(response, http.StatusCreated, struct{}{})
	})
}

func panicsRoute() Route {
	return Route{
		Method: http.MethodPost, Pattern: "/api/v1/panics",
		Op: desc("things.panic", rastrillo.Write),
		Handle: func(
			context.Context, *rastrillo.Ctx,
			http.ResponseWriter, *http.Request,
		) {
			panic("boom")
		},
	}
}

func TestTreeAndGateAnswerAlike(t *testing.T) {
	configuration := config()
	configuration.RequireAgentSession = true
	tree := mustNew(t, configuration, createThing(ok), panicsRoute())
	gate := configuration.Gate()
	wrapped := gate.Wrap(gateHandler(gate))

	longSession := strings.Repeat("s", 201)
	cases := []struct {
		name, target, token string
		header              []string
		status              int
		code, message       string
	}{
		{"an unknown token", "/api/v1/things", "bad", nil,
			401, "unauthorized", codes["unauthorized"].message},
		{"an Agent-Session of 201 bytes", "/api/v1/things", "agent-write",
			[]string{"Agent-Session", longSession}, 422, "invalid",
			fmt.Sprintf(msgAgentHeader, "Agent-Session")},
		{"an agent write with no session", "/api/v1/things",
			"agent-write", nil, 422, "invalid", msgAgentSession},
		{"a read-only agent's write with no session", "/api/v1/things",
			"agent-read", nil, 422, "invalid", msgAgentSession},
		{"a handler panic", "/api/v1/panics", "person-write", nil,
			500, "internal", codes["internal"].message},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fromTree := serve(tree, http.MethodPost, tc.target, tc.token,
				`{"title":"x"}`, tc.header...)
			fromGate := serve(wrapped, http.MethodPost, tc.target,
				tc.token, `{"title":"x"}`, tc.header...)
			sides := map[string]*httptest.ResponseRecorder{
				"tree": fromTree, "gate": fromGate,
			}
			for side, recorder := range sides {
				if recorder.Code != tc.status {
					t.Fatalf("%s: status = %d, want %d; body %s",
						side, recorder.Code, tc.status, recorder.Body)
				}
				refused := decodeError(t, recorder)
				if refused.Code != tc.code || refused.Message != tc.message {
					t.Fatalf("%s: answered %+v, want %s %q",
						side, refused, tc.code, tc.message)
				}
				if (tc.code == "internal") != (refused.Ref != "") {
					t.Fatalf("%s: ref %q on a %s", side, refused.Ref, tc.code)
				}
				assertHeaders(t, recorder)
			}
			assertAlike(t, fromTree, fromGate)
		})
	}
}

// assertAlike compares two answers byte for byte, but for the ref,
// which is fresh for every panic.
func assertAlike(t *testing.T, tree, gate *httptest.ResponseRecorder) {
	t.Helper()
	treeError, gateError := decodeError(t, tree), decodeError(t, gate)
	treeError.Ref, gateError.Ref = "", ""
	if !reflect.DeepEqual(treeError, gateError) {
		t.Fatalf("tree answered %+v, gate %+v", treeError, gateError)
	}
	for _, name := range []string{"WWW-Authenticate", "Content-Type"} {
		if tree.Header().Get(name) != gate.Header().Get(name) {
			t.Fatalf("%s: tree %q, gate %q", name,
				tree.Header().Get(name), gate.Header().Get(name))
		}
	}
	if gate.Code == http.StatusInternalServerError {
		return
	}
	if tree.Body.String() != gate.Body.String() {
		t.Fatalf("body: tree %s, gate %s", tree.Body, gate.Body)
	}
}

// Every field of Config that Gate also has must come across. The test
// fills each shared field, so a field added to both and forgotten in
// Config.Gate fails here rather than going quietly unset.
func TestConfigGateCopiesFields(t *testing.T) {
	passThrough := func(next http.Handler) http.Handler { return next }
	// A pointer, so the interface compares by identity: the plain fake
	// holds a map, which == cannot compare.
	configuration := Config{
		Verify:              newRechecker(verifier()),
		Logger:              slog.New(slog.DiscardHandler),
		MaxBody:             10,
		Before:              []func(http.Handler) http.Handler{passThrough},
		After:               []func(http.Handler) http.Handler{passThrough},
		RequireAgentSession: true,
		Redact:              strings.ToUpper,
	}
	gate := reflect.ValueOf(configuration.Gate())
	source := reflect.ValueOf(configuration)
	for index := range gate.NumField() {
		field := gate.Type().Field(index)
		if !field.IsExported() {
			continue
		}
		from := source.FieldByName(field.Name)
		if !from.IsValid() {
			continue
		}
		if from.IsZero() {
			t.Fatalf("Config.%s is zero here; fill it in this test",
				field.Name)
		}
		if !sameValue(from, gate.Field(index)) {
			t.Errorf("Gate.%s = %v, want Config's %v",
				field.Name, gate.Field(index), from)
		}
	}
	if got := configuration.Gate().MaxResponse; got != 0 {
		t.Errorf("MaxResponse = %d, want 0 for the default", got)
	}
}

// sameValue compares funcs and slices by what they point at, since Go
// compares neither with ==.
func sameValue(want, got reflect.Value) bool {
	switch want.Kind() {
	case reflect.Func:
		return want.Pointer() == got.Pointer()
	case reflect.Slice:
		if want.Len() != got.Len() {
			return false
		}
		return want.Len() == 0 || want.Pointer() == got.Pointer()
	}
	return want.Interface() == got.Interface()
}

// After is where an app's own per-actor work goes, so a request the
// gate refuses for its operation must never reach it.
func TestAdmitRefusalsSkipAfter(t *testing.T) {
	var afterCalls int
	configuration := config()
	configuration.RequireAgentSession = true
	configuration.After = []func(http.Handler) http.Handler{
		func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(
				response http.ResponseWriter, request *http.Request,
			) {
				afterCalls++
				next.ServeHTTP(response, request)
			})
		},
	}
	tree := mustNew(t, configuration, createThing(decodes(nil)))
	cases := []struct {
		name, token string
		status      int
		calls       int
	}{
		{"a read-only token's write", "person-read", 403, 0},
		{"an agent's write with no session", "agent-write", 422, 0},
		{"a person's write", "person-write", 201, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			afterCalls = 0
			recorder := serve(tree, http.MethodPost, "/api/v1/things",
				tc.token, `{"title":"x"}`)
			if recorder.Code != tc.status {
				t.Fatalf("status = %d, want %d; body %s",
					recorder.Code, tc.status, recorder.Body)
			}
			if afterCalls != tc.calls {
				t.Fatalf("After ran %d times, want %d", afterCalls, tc.calls)
			}
		})
	}
}

func TestAdmitChecksTheSessionBeforeReadOnly(t *testing.T) {
	strict := Gate{RequireAgentSession: true}
	readThing := desc("things.get", rastrillo.Read).Operation
	cases := []struct {
		name      string
		gate      Gate
		actor     rastrillo.Actor
		operation rastrillo.Operation
		status    int
		message   string
	}{
		{"a read-only agent's write with no session", strict, agentRead,
			writeThing, 422, msgAgentSession},
		{"an agent's write with no session", strict, agentWrite,
			writeThing, 422, msgAgentSession},
		{"a read-only token's write", strict, personRead,
			writeThing, 403, codes["forbidden"].message},
		{"a read-only agent's write, sessions optional", Gate{},
			agentRead, writeThing, 403, codes["forbidden"].message},
		{"an agent's read with no session", strict, agentRead,
			readThing, 0, ""},
		{"a person's write", strict, personWrite, writeThing, 0, ""},
	}
	request := WithLogger(httptest.NewRequest(http.MethodPost, "/", nil),
		slog.New(slog.DiscardHandler))
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.gate.Admit(tc.actor, tc.operation)
			if tc.status == 0 {
				if err != nil {
					t.Fatalf("Admit = %v, want nil", err)
				}
				return
			}
			status, refused := Refusal(request, err)
			if status != tc.status || refused.Message != tc.message {
				t.Fatalf("Admit = %v, answered %d %+v; want %d %q",
					err, status, refused, tc.status, tc.message)
			}
		})
	}
}

func TestAdmitForbidsAReadOnlyWrite(t *testing.T) {
	err := Gate{}.Admit(personRead, writeThing)
	var forbidden op.Forbidden
	if !errors.As(err, &forbidden) {
		t.Fatalf("Admit = %v, want op.Forbidden", err)
	}
	if forbidden.Reason != "read-only credential" {
		t.Fatalf("Reason = %q, want %q", forbidden.Reason,
			"read-only credential")
	}
}

func TestGateValidateRefuses(t *testing.T) {
	passThrough := func(next http.Handler) http.Handler { return next }
	valid := Gate{Verify: verifier()}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate = %v for a valid gate", err)
	}
	cases := map[string]Gate{
		"nil Verify": {},
		"nil Before": {Verify: verifier(),
			Before: []func(http.Handler) http.Handler{passThrough, nil}},
		"nil After": {Verify: verifier(),
			After: []func(http.Handler) http.Handler{nil}},
	}
	for name, gate := range cases {
		t.Run(name, func(t *testing.T) {
			if err := gate.Validate(); err == nil {
				t.Fatal("Validate returned no error")
			}
		})
	}
}

func TestDisclosedOutsideWrapDoesNothing(t *testing.T) {
	var called bool
	Disclosed(context.Background(), func(context.Context,
		rastrillo.Actor) error {
		called = true
		return nil
	})
	if called {
		t.Fatal("Disclosed ran its check on registration")
	}
}
