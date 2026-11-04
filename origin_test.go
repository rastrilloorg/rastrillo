package rastrillo

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

// originLog is Origin's answer for variable, with what it logged.
func originLog(t *testing.T, variable string) (string, string) {
	t.Helper()
	var logged bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logged, nil))
	return Origin(variable, logger), logged.String()
}

func TestOriginIsTheVariableWhenItIsSet(t *testing.T) {
	t.Setenv("NOTES_ORIGIN", "https://notes.example.com")
	t.Setenv("CARLOS_HOST", "notes.bdf.oncarlos.com")
	origin, logged := originLog(t, "NOTES_ORIGIN")
	if origin != "https://notes.example.com" || logged != "" {
		t.Errorf("Origin = %q, logging %q; want the variable, quietly",
			origin, logged)
	}
}

func TestOriginIsTheInstancesOwnHostOnCarlos(t *testing.T) {
	t.Setenv("NOTES_ORIGIN", "")
	t.Setenv("CARLOS_HOST", "notes.bdf.oncarlos.com")
	origin, logged := originLog(t, "NOTES_ORIGIN")
	if origin != "https://notes.bdf.oncarlos.com" {
		t.Errorf("Origin = %q, want https://notes.bdf.oncarlos.com", origin)
	}
	if !strings.Contains(logged, "level=INFO") ||
		!strings.Contains(logged, "set NOTES_ORIGIN") {
		t.Errorf("Origin logged %q, want an INFO line naming NOTES_ORIGIN",
			logged)
	}
}

func TestOriginOffCarlosIsLocalhostWithAWarning(t *testing.T) {
	t.Setenv("NOTES_ORIGIN", "")
	t.Setenv("CARLOS_HOST", "")
	origin, logged := originLog(t, "NOTES_ORIGIN")
	if origin != "http://localhost:8080" {
		t.Errorf("Origin = %q, want http://localhost:8080", origin)
	}
	if !strings.Contains(logged, "level=WARN") ||
		!strings.Contains(logged, "NOTES_ORIGIN not set") {
		t.Errorf("Origin logged %q, want a warning that NOTES_ORIGIN "+
			"is not set", logged)
	}
}
