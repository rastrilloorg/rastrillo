package view

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
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
	if w.Code != http.StatusOK && w.Body.Len() != 0 {
		t.Errorf("Render wrote to w itself instead of delegating: code=%d body=%q", w.Code, w.Body.String())
	}
}

func TestFail(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	ctx := &rastrillo.Ctx{Logger: logger}
	w := httptest.NewRecorder()
	err := errors.New("boom")

	Fail(ctx, w, "creating widget", err)

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
