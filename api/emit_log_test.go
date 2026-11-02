package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	rastrillo "amadan.net/rastrillo/rastrillo"
)

// deadlineRefuser takes the gate's clearing call to SetWriteDeadline
// and refuses every real deadline, as a writer that cannot set one
// partway through would.
type deadlineRefuser struct{ *httptest.ResponseRecorder }

func (deadlineRefuser) SetWriteDeadline(deadline time.Time) error {
	if deadline.IsZero() {
		return nil
	}
	return errors.New("fake: deadline refused")
}

// slowlyChecked is writesSecret behind a Disclosed check that outlasts
// the gate's window.
func slowlyChecked(response http.ResponseWriter, request *http.Request) {
	Disclosed(request.Context(), func(context.Context,
		rastrillo.Actor) error {
		time.Sleep(150 * time.Millisecond)
		return nil
	})
	writesSecret(response, request)
}

// Refusal logs nothing for unavailable, and these causes are mostly
// configuration, so the gate logs each before it answers 503.
func TestEmitLogsWhyItIsUnavailable(t *testing.T) {
	tests := []struct {
		name, cause string
		answer      func(gate Gate) int
	}{
		{"no_write_deadline", "no write deadline", func(gate Gate) int {
			wrapped := gate.Wrap(http.HandlerFunc(writesSecret))
			return serve(wrapped, http.MethodGet, "/things", "person-read",
				"").Code
		}},
		{"deadline_refused", "set a write deadline", func(gate Gate) int {
			wrapped := gate.Wrap(http.HandlerFunc(writesSecret))
			request := httptest.NewRequest(http.MethodGet, "/things", nil)
			request.Header.Set("Authorization", "Bearer person-read")
			recorder := httptest.NewRecorder()
			wrapped.ServeHTTP(deadlineRefuser{recorder}, request)
			return recorder.Code
		}},
		{"slow_check", "past its deadline", func(gate Gate) int {
			gate.recheckWindow = 50 * time.Millisecond
			wrapped := gate.Wrap(http.HandlerFunc(slowlyChecked))
			return fetch(t, wrapped, http.MethodGet, "/things",
				"person-read").status
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var logs syncBuffer
			gate := gateWith(newRechecker(verifier()))
			gate.Logger = slog.New(slog.NewTextHandler(&logs, nil))
			if status := test.answer(gate); status !=
				http.StatusServiceUnavailable {
				t.Errorf("status %d, want 503", status)
			}
			var warnings []string
			for line := range strings.Lines(logs.String()) {
				if strings.Contains(line, "level=WARN") {
					warnings = append(warnings, line)
				}
			}
			if len(warnings) != 1 ||
				!strings.Contains(warnings[0], test.cause) ||
				!strings.Contains(warnings[0], "method=GET") ||
				!strings.Contains(warnings[0], "path=/things") {
				t.Errorf("Warn lines %q, want one naming %q with the "+
					"method and path", warnings, test.cause)
			}
		})
	}
}
