package api

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	json "github.com/go-json-experiment/json"

	"amadan.net/rastrillo/rastrillo/op"
)

// envelope is the wire shape every refusal takes: {"error": {...}}.
type envelope struct {
	Error Error `json:"error"`
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) Error {
	t.Helper()
	var env envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("body %q is not an error envelope: %v", rec.Body.String(), err)
	}
	return env.Error
}

// assertHeaders checks the two headers every API response carries. A
// missing no-store would let a proxy cache a token-scoped answer.
func assertHeaders(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
}

// refuse runs Refuse with a logger that keeps the test output clean;
// the tests that read the log line set their own.
func refuse(err error) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/things/1", nil)
	r = WithLogger(r, slog.New(slog.DiscardHandler))
	Refuse(rec, r, err)
	return rec
}

func TestRefuseMapsRefusals(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		status  int
		code    string
		message string
	}{
		{"forbidden", op.Forbidden{Reason: "read-only token"}, 403, "forbidden", "This token can't do that."},
		{"not found", op.NotFound{}, 404, "not_found", "Nothing here."},
		{"conflict", op.Conflict{Message: "The currency changed under you."}, 409, "conflict", "The currency changed under you."},
		{"invalid", op.Invalid{Fields: map[string]string{"Title": "Required."}}, 422, "invalid", "Check the fields and try again."},
		{"wrapped forbidden", fmt.Errorf("checking: %w", op.Forbidden{}), 403, "forbidden", "This token can't do that."},
		{"unauthorized", ErrUnauthorized, 401, "unauthorized", "Send a valid API token in the Authorization header."},
		{"wrapped unauthorized", fmt.Errorf("verify: %w", ErrUnauthorized), 401, "unauthorized", "Send a valid API token in the Authorization header."},
		{"unavailable", fmt.Errorf("verify: %w", ErrUnavailable), 503, "unavailable", "Try again in a moment."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := refuse(tc.err)
			assertHeaders(t, rec)
			if rec.Code != tc.status {
				t.Errorf("status = %d, want %d", rec.Code, tc.status)
			}
			e := decodeError(t, rec)
			if e.Code != tc.code {
				t.Errorf("code = %q, want %q", e.Code, tc.code)
			}
			if e.Message != tc.message {
				t.Errorf("message = %q, want %q", e.Message, tc.message)
			}
			if e.Ref != "" {
				t.Errorf("ref = %q, want none: only a 500 has something to look up", e.Ref)
			}
		})
	}
}

func TestRefuseInvalidRekeysFieldsToWireNames(t *testing.T) {
	rec := refuse(op.Invalid{Fields: map[string]string{
		"Title":       "Title is required",
		"MaxPerOrder": "Must be at least 1",
	}})
	e := decodeError(t, rec)
	want := map[string]string{
		"title":         "Title is required",
		"max_per_order": "Must be at least 1",
	}
	if len(e.Fields) != len(want) {
		t.Fatalf("fields = %v, want %v", e.Fields, want)
	}
	for k, v := range want {
		if e.Fields[k] != v {
			t.Errorf("fields[%q] = %q, want %q (fields: %v)", k, e.Fields[k], v, e.Fields)
		}
	}
}

func TestRefuseUnauthorizedChallenges(t *testing.T) {
	rec := refuse(ErrUnauthorized)
	if got := rec.Header().Get("WWW-Authenticate"); got != "Bearer" {
		t.Errorf("WWW-Authenticate = %q, want Bearer", got)
	}
}

func TestRefuseUnknownErrorIsInternalWithRef(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	redact := func(s string) string { return strings.ReplaceAll(s, "tok_abc_SECRET", "tok_abc") }

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/things", nil)
	r = WithRedact(WithLogger(r, logger), redact)
	Refuse(rec, r, errors.New("store: bad token tok_abc_SECRET"))

	assertHeaders(t, rec)
	if rec.Code != 500 {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	e := decodeError(t, rec)
	if e.Code != "internal" {
		t.Errorf("code = %q, want internal", e.Code)
	}
	if e.Message != "Something went wrong. Quote the reference if you report it." {
		t.Errorf("message = %q", e.Message)
	}
	if e.Ref == "" {
		t.Fatal("ref is empty; a 500 must carry one so the log line can be found")
	}
	log := buf.String()
	if !strings.Contains(log, e.Ref) {
		t.Errorf("log line %q does not carry the ref %q", log, e.Ref)
	}
	if !strings.Contains(log, "tok_abc") {
		t.Errorf("log line %q does not carry the redacted error text", log)
	}
	if strings.Contains(log, "SECRET") {
		t.Errorf("log line %q leaks the token secret past the redactor", log)
	}
}

func TestRefuseLogsForbiddenReason(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	rec := httptest.NewRecorder()
	r := WithLogger(httptest.NewRequest(http.MethodPost, "/api/v1/things", nil), logger)
	Refuse(rec, r, op.Forbidden{Reason: "scoped op with no subject"})
	if !strings.Contains(buf.String(), "scoped op with no subject") {
		t.Errorf("log %q does not carry Forbidden.Reason, which never reaches the response", buf.String())
	}
	if strings.Contains(rec.Body.String(), "scoped op") {
		t.Errorf("body %q leaks Forbidden.Reason", rec.Body.String())
	}
}

// A client that pastes its token into a path segment puts it in
// r.URL.Path, and both of Refuse's log lines carry the path. The
// redactor must reach it, or the secret is logged beside a redacted
// message.
func TestRefuseRedactsPathInLog(t *testing.T) {
	secret := "tickets_k3Jx9QpL2mWa_" + strings.Repeat("A", 43)
	for name, err := range map[string]error{
		"forbidden": op.Forbidden{Reason: "read-only credential"},
		"internal":  errors.New("store failed"),
	} {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			r := httptest.NewRequest(http.MethodPost, "/api/v1/things/"+secret, nil)
			r = WithLogger(r, slog.New(slog.NewTextHandler(&buf, nil)))
			r = WithRedact(r, func(s string) string { return strings.ReplaceAll(s, secret, "tickets_k3Jx9QpL2mWa") })
			Refuse(httptest.NewRecorder(), r, err)
			line := buf.String()
			if strings.Contains(line, secret) {
				t.Fatalf("the token reached the log: %q", line)
			}
			if !strings.Contains(line, "path=/api/v1/things/tickets_k3Jx9QpL2mWa") {
				t.Fatalf("the redacted path is not in the log: %q", line)
			}
		})
	}
}

func TestWriteEncodesNilSliceAsEmptyArray(t *testing.T) {
	rec := httptest.NewRecorder()
	var none []int
	Write(rec, 200, map[string]any{"items": none})
	assertHeaders(t, rec)
	if rec.Code != 200 {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `{"items":[]}` {
		t.Errorf("body = %s, want {\"items\":[]}: a list with nothing in it is [], never null", got)
	}
}

func TestWriteErrorWrapsInEnvelope(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteError(rec, 413, Error{Code: "too_large", Message: "The request body is too large."})
	assertHeaders(t, rec)
	if rec.Code != 413 {
		t.Errorf("status = %d, want 413", rec.Code)
	}
	want := `{"error":{"code":"too_large","message":"The request body is too large."}}`
	if got := strings.TrimSpace(rec.Body.String()); got != want {
		t.Errorf("body = %s, want %s", got, want)
	}
}
