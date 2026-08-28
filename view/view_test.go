package view

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/carlosframework/rastrillo"
)

func TestRenderNilRenderLogsAnd500(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	ctx := &rastrillo.Ctx{Logger: logger}
	w := httptest.NewRecorder()

	Render(ctx, w, "widgets/show", http.StatusOK, map[string]string{"k": "v"})

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
	if got, want := strings.TrimSpace(w.Body.String()), "Something went wrong."; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
	out := buf.String()
	if !strings.Contains(out, "Ctx.Render is nil") {
		t.Errorf("log output missing nil-Render line: %s", out)
	}
	if !strings.Contains(out, "app's ctx factory must set it") {
		t.Errorf("log output missing ctx-factory guidance: %s", out)
	}
}

func TestRenderWired(t *testing.T) {
	var gotPage string
	var gotStatus int
	var gotData any
	ctx := &rastrillo.Ctx{
		Render: func(ctx *rastrillo.Ctx, w http.ResponseWriter, page string, status int, data any) {
			gotPage = page
			gotStatus = status
			gotData = data
		},
	}
	w := httptest.NewRecorder()
	data := map[string]string{"k": "v"}

	Render(ctx, w, "widgets/show", http.StatusOK, data)

	if gotPage != "widgets/show" {
		t.Errorf("page = %q, want %q", gotPage, "widgets/show")
	}
	if gotStatus != http.StatusOK {
		t.Errorf("status = %d, want %d", gotStatus, http.StatusOK)
	}
	got, ok := gotData.(map[string]string)
	if !ok || got["k"] != "v" {
		t.Errorf("data = %#v, want %#v", gotData, data)
	}
	if w.Code != http.StatusOK || w.Body.Len() != 0 {
		t.Errorf("Render wrote to w itself instead of delegating: code=%d body=%q", w.Code, w.Body.String())
	}
}

func TestFail(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	ctx := &rastrillo.Ctx{Logger: logger}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/widgets", nil)
	err := errors.New("boom")

	Fail(ctx, w, r, "creating widget", err)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
	if got, want := strings.TrimSpace(w.Body.String()), "Something went wrong."; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
	out := buf.String()
	if !strings.Contains(out, "creating widget") {
		t.Errorf("log output missing what: %s", out)
	}
	if !strings.Contains(out, "boom") {
		t.Errorf("log output missing err: %s", out)
	}
	// The reference the user is shown is the reference in the log; that
	// join is the whole reason it exists.
	ref := refIn(t, out)
	if !refShape.MatchString(ref) {
		t.Errorf("logged ref = %q, want six base32 characters", ref)
	}
}

// refShape is NewRef's contract, asserted here too because view's
// helpers are where a caller meets it.
var refShape = regexp.MustCompile(`^[a-z2-7]{6}$`)

// refIn pulls the ref= value out of a slog text line.
func refIn(t *testing.T, logLine string) string {
	t.Helper()
	m := regexp.MustCompile(`ref=([a-z2-7]+)`).FindStringSubmatch(logLine)
	if m == nil {
		t.Fatalf("log line carries no ref=: %s", logLine)
	}
	return m[1]
}

// recorder is an ErrorPage callback that remembers what it was handed
// and writes a page of its own, the way an app's would.
func recorder(status *int, ref *string, path *string) rastrillo.ErrorPageFunc {
	return func(w http.ResponseWriter, r *http.Request, s int, rf string) {
		*status, *ref, *path = s, rf, r.URL.Path
		w.WriteHeader(s)
		fmt.Fprintf(w, "APP PAGE %d %s", s, rf)
	}
}

// With Ctx.ErrorPage wired, Fail renders the app's page at 500 and hands
// it the same reference it logged.
func TestFailRendersTheAppErrorPage(t *testing.T) {
	var buf bytes.Buffer
	var gotStatus int
	var gotRef, gotPath string
	ctx := &rastrillo.Ctx{
		Logger:    slog.New(slog.NewTextHandler(&buf, nil)),
		ErrorPage: recorder(&gotStatus, &gotRef, &gotPath),
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/widgets/7", nil)

	Fail(ctx, w, r, "creating widget", errors.New("boom"))

	if gotStatus != http.StatusInternalServerError || gotPath != "/widgets/7" {
		t.Errorf("ErrorPage got (%d, %q), want (500, \"/widgets/7\")", gotStatus, gotPath)
	}
	if gotRef != refIn(t, buf.String()) {
		t.Errorf("page ref %q != logged ref %q", gotRef, refIn(t, buf.String()))
	}
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
	if !strings.HasPrefix(w.Body.String(), "APP PAGE 500 ") {
		t.Errorf("body = %q, want the app's own page", w.Body.String())
	}
}

// A caller with no request in scope passes nil, and gets the plain-text
// fallback: an error page is a response to a request, and there is no
// Accept header to sniff either.
func TestFailWithANilRequestFallsBackToPlainText(t *testing.T) {
	var gotStatus int
	var gotRef, gotPath string
	ctx := &rastrillo.Ctx{ErrorPage: recorder(&gotStatus, &gotRef, &gotPath)}
	w := httptest.NewRecorder()

	Fail(ctx, w, nil, "creating widget", errors.New("boom"))

	if gotStatus != 0 {
		t.Errorf("ErrorPage ran with a nil request")
	}
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
	if got, want := strings.TrimSpace(w.Body.String()), "Something went wrong."; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}

// NotFound and Forbidden are the same shape at 404 and 403, and carry no
// reference: there is nothing to look up afterwards.
func TestNotFoundAndForbiddenRenderTheAppErrorPage(t *testing.T) {
	for _, c := range []struct {
		name   string
		call   func(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request)
		status int
	}{
		{"NotFound", NotFound, http.StatusNotFound},
		{"Forbidden", Forbidden, http.StatusForbidden},
	} {
		t.Run(c.name, func(t *testing.T) {
			var gotStatus int
			var gotRef, gotPath string
			ctx := &rastrillo.Ctx{ErrorPage: recorder(&gotStatus, &gotRef, &gotPath)}
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodGet, "/widgets/7", nil)

			c.call(ctx, w, r)

			if gotStatus != c.status {
				t.Errorf("ErrorPage status = %d, want %d", gotStatus, c.status)
			}
			if gotRef != "" {
				t.Errorf("ref = %q, want empty: nothing was logged to look up", gotRef)
			}
			if w.Code != c.status {
				t.Errorf("status = %d, want %d", w.Code, c.status)
			}
		})
	}
}

// Without an ErrorPage — or without a request — they answer net/http's
// own plain text, at the right status.
func TestNotFoundAndForbiddenFallBackToPlainText(t *testing.T) {
	ctx := &rastrillo.Ctx{}
	r := httptest.NewRequest(http.MethodGet, "/widgets/7", nil)

	w := httptest.NewRecorder()
	NotFound(ctx, w, r)
	if w.Code != http.StatusNotFound || w.Body.Len() == 0 {
		t.Errorf("NotFound = %d %q, want a plain 404", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	Forbidden(ctx, w, r)
	if w.Code != http.StatusForbidden || w.Body.Len() == 0 {
		t.Errorf("Forbidden = %d %q, want a plain 403", w.Code, w.Body.String())
	}

	// Nil request, ErrorPage wired: still plain text, never a panic.
	var gotStatus int
	var gotRef, gotPath string
	ctx = &rastrillo.Ctx{ErrorPage: recorder(&gotStatus, &gotRef, &gotPath)}
	w = httptest.NewRecorder()
	NotFound(ctx, w, nil)
	if w.Code != http.StatusNotFound || gotStatus != 0 {
		t.Errorf("NotFound(nil request) = %d, ErrorPage status %d", w.Code, gotStatus)
	}
	w = httptest.NewRecorder()
	Forbidden(ctx, w, nil)
	if w.Code != http.StatusForbidden || gotStatus != 0 {
		t.Errorf("Forbidden(nil request) = %d, ErrorPage status %d", w.Code, gotStatus)
	}
}

// A client that asked for JSON gets JSON — the same status and the same
// reference, in the one shape all three helpers share. The app's
// ErrorPage is NOT consulted: it renders HTML, and a fetch() caller
// asked for something it can parse.
func TestJSONClientsGetJSON(t *testing.T) {
	jsonReq := func() *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/widgets/7", nil)
		r.Header.Set("Accept", "application/json, text/plain;q=0.9")
		return r
	}
	var gotStatus int
	var gotRef, gotPath string
	var buf bytes.Buffer
	ctx := &rastrillo.Ctx{
		Logger:    slog.New(slog.NewTextHandler(&buf, nil)),
		ErrorPage: recorder(&gotStatus, &gotRef, &gotPath),
	}

	w := httptest.NewRecorder()
	Fail(ctx, w, jsonReq(), "creating widget", errors.New("boom"))
	if gotStatus != 0 {
		t.Errorf("ErrorPage ran for a JSON request")
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
	var body struct {
		Status int    `json:"status"`
		Ref    string `json:"ref"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("body %q is not JSON: %v", w.Body.String(), err)
	}
	if body.Status != http.StatusInternalServerError {
		t.Errorf("status field = %d, want 500", body.Status)
	}
	if body.Ref != refIn(t, buf.String()) {
		t.Errorf("ref field %q != logged ref %q", body.Ref, refIn(t, buf.String()))
	}

	// 404/403 have no reference, and the field is omitted rather than
	// sent empty: a client checking for it should not find one.
	for _, c := range []struct {
		call   func(*rastrillo.Ctx, http.ResponseWriter, *http.Request)
		status int
	}{{NotFound, http.StatusNotFound}, {Forbidden, http.StatusForbidden}} {
		w := httptest.NewRecorder()
		c.call(ctx, w, jsonReq())
		if w.Code != c.status {
			t.Errorf("status = %d, want %d", w.Code, c.status)
		}
		if got, want := strings.TrimSpace(w.Body.String()), fmt.Sprintf(`{"status":%d}`, c.status); got != want {
			t.Errorf("body = %q, want %q", got, want)
		}
	}
}

func TestParseID(t *testing.T) {
	cases := []struct {
		in     string
		want   int64
		wantOK bool
	}{
		{"7", 7, true},
		{"0", 0, false},
		{"-3", 0, false},
		{"abc", 0, false},
		{"", 0, false},
	}
	for _, c := range cases {
		r := httptest.NewRequest(http.MethodGet, "/widgets/"+c.in, nil)
		r.SetPathValue("id", c.in)

		got, ok := ParseID(r)
		if ok != c.wantOK {
			t.Errorf("ParseID(%q) ok = %v, want %v", c.in, ok, c.wantOK)
			continue
		}
		if ok && got != c.want {
			t.Errorf("ParseID(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}
