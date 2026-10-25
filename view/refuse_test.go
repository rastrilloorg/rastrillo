package view

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/op"
)

func TestRefuse(t *testing.T) {
	var logged bytes.Buffer
	ctx := &rastrillo.Ctx{Logger: slog.New(slog.NewTextHandler(&logged, nil))}
	for _, c := range []struct {
		err  error
		code int
	}{
		{op.NotFound{}, 404},
		{fmt.Errorf("loading: %w", op.Forbidden{Reason: "role"}), 403},
		{op.Invalid{Fields: map[string]string{"Title": "Title is required"}}, 500},
		{errors.New("db down"), 500},
	} {
		w := httptest.NewRecorder()
		Refuse(ctx, w, httptest.NewRequest("GET", "/things/1", nil), "things: loading things", c.err)
		if w.Code != c.code {
			t.Errorf("Refuse(%v) = %d, want %d", c.err, w.Code, c.code)
		}
	}
	if !strings.Contains(logged.String(), "db down") || !strings.Contains(logged.String(), "things: loading things") {
		t.Errorf("a 500 must be logged with what was being done:\n%s", logged.String())
	}
}
