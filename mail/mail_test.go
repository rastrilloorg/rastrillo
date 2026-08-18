package mail

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestMessageShape(t *testing.T) {
	msg, err := message("app@example.com", "who@example.com", "Hello", "line one\nline two")
	if err != nil {
		t.Fatalf("message: %v", err)
	}
	s := string(msg)
	for _, want := range []string{
		"From: app@example.com\r\n",
		"To: who@example.com\r\n",
		"Subject: Hello\r\n",
		"Content-Type: text/plain; charset=utf-8\r\n",
		"\r\n\r\nline one\nline two",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("message missing %q in:\n%s", want, s)
		}
	}
}

func TestMessageRefusesHeaderInjection(t *testing.T) {
	cases := []struct{ from, to, subject string }{
		{"a@x", "b@x\r\nBcc: victim@x", "s"},
		{"a@x", "b@x", "s\nX-Forged: 1"},
		{"a@x\rX: 1", "b@x", "s"},
	}
	for _, c := range cases {
		if _, err := message(c.from, c.to, c.subject, "body"); err == nil {
			t.Errorf("message(%q, %q, %q) accepted a header line break", c.from, c.to, c.subject)
		}
	}
}

func TestSMTPSenderRefusesInjectionBeforeDialing(t *testing.T) {
	// An injection error must surface before any network attempt: the
	// host below is unroutable, so reaching the dial would hang or fail
	// with a network error instead of the injection error.
	s := SMTP("invalid.invalid", "1", "app@example.com", "", "")
	err := s.Send(context.Background(), "b@x\r\nBcc: v@x", "s", "body")
	if err == nil || !strings.Contains(err.Error(), "line break") {
		t.Fatalf("Send with injected recipient: %v, want the injection error", err)
	}
}

func TestLoggedSenderLabelsItself(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	if err := Logged(logger).Send(context.Background(), "who@example.com", "Sub", "Body"); err != nil {
		t.Fatalf("Logged Send: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "NOT SENT") {
		t.Fatalf("Logged output is not unmistakably labelled: %s", out)
	}
	if !strings.Contains(out, "who@example.com") || !strings.Contains(out, "Body") {
		t.Fatalf("Logged output dropped the message: %s", out)
	}
}

func TestFromEnvFallsBackToLoggedWithWarning(t *testing.T) {
	t.Setenv("MAILTEST_SMTP_HOST", "")
	t.Setenv("MAILTEST_SMTP_FROM", "")
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	s := FromEnv("MAILTEST", logger)
	if _, ok := s.(loggedSender); !ok {
		t.Fatalf("FromEnv without config = %T, want loggedSender", s)
	}
	if !strings.Contains(buf.String(), "SMTP not configured") {
		t.Fatal("FromEnv fell back silently; the warning is the contract")
	}
}

func TestFromEnvBuildsSMTPSender(t *testing.T) {
	t.Setenv("MAILTEST_SMTP_HOST", "relay.example.com")
	t.Setenv("MAILTEST_SMTP_FROM", "app@example.com")
	t.Setenv("MAILTEST_SMTP_PORT", "")
	t.Setenv("MAILTEST_SMTP_USER", "u")
	t.Setenv("MAILTEST_SMTP_PASS", "p")
	s := FromEnv("MAILTEST", nil)
	sm, ok := s.(*smtpSender)
	if !ok {
		t.Fatalf("FromEnv = %T, want *smtpSender", s)
	}
	if sm.host != "relay.example.com" || sm.from != "app@example.com" || sm.user != "u" {
		t.Fatalf("FromEnv misread env: %+v", sm)
	}
	if sm.port != "587" {
		t.Fatalf("port = %q, want the 587 submission default", sm.port)
	}
}
