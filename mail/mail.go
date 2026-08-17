// Package mail is the framework's one outbound-email surface — the
// third extraction of a shape vitogo (internal/vito/mail), kass
// (internal/mail) and seapointish (smtpMailer) each hand-rolled: a
// one-method Sender interface, a stdlib net/smtp implementation, a
// loudly-labelled log fallback for instances with no relay configured,
// and the header-injection guard all three carried.
//
// Sender's signature deliberately matches keymaildev/signin's Mailer
// interface, so any Sender drops straight into the auth package's
// magic-link flow without an adapter.
//
// Plain text only, one recipient per call. Recording attempts in an
// emails table (vitogo's pattern) is app policy, not framework — wrap a
// Sender if you want a ledger.
package mail

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/smtp"
	"os"
	"strings"
)

// Sender delivers one plain-text email. Implementations must be safe
// for concurrent use.
type Sender interface {
	Send(ctx context.Context, to, subject, body string) error
}

// defaultPort is used when a host is configured without a port — 587
// (submission) rather than 25, since a relay an app points this at is
// far more likely to expect authenticated submission than
// MTA-to-MTA delivery.
const defaultPort = "587"

// SMTP returns a Sender that dials host:port with net/smtp.SendMail.
// Auth is PlainAuth when user is non-empty, none otherwise (a relay
// accepting anonymous submission from its own network). Port defaults
// to 587 when empty.
func SMTP(host, port, from, user, pass string) Sender {
	if port == "" {
		port = defaultPort
	}
	return &smtpSender{host: host, port: port, from: from, user: user, pass: pass}
}

type smtpSender struct {
	host, port, from, user, pass string
}

func (s *smtpSender) Send(_ context.Context, to, subject, body string) error {
	msg, err := message(s.from, to, subject, body)
	if err != nil {
		return err
	}
	var auth smtp.Auth
	if s.user != "" {
		auth = smtp.PlainAuth("", s.user, s.pass, s.host)
	}
	if err := smtp.SendMail(net.JoinHostPort(s.host, s.port), auth, s.from, []string{to}, msg); err != nil {
		return fmt.Errorf("rastrillo/mail: send to %s: %w", to, err)
	}
	return nil
}

// Logged returns a Sender that writes the message to the log instead of
// sending it — the dev/unconfigured fallback. The log line carries the
// full body: for the magic-link flow that is a live credential, which is
// exactly why every Logged line is labelled unmistakably and FromEnv
// warns when it falls back to this.
func Logged(logger *slog.Logger) Sender {
	if logger == nil {
		logger = slog.Default()
	}
	return loggedSender{logger}
}

type loggedSender struct{ logger *slog.Logger }

func (l loggedSender) Send(_ context.Context, to, subject, body string) error {
	l.logger.Warn("rastrillo/mail: NOT SENT (no SMTP configured) — logging instead",
		"to", to, "subject", subject, "body", body)
	return nil
}

// FromEnv builds a Sender from <prefix>_SMTP_{HOST,PORT,FROM,USER,PASS}.
// HOST and FROM are the two values with no sane default: with either
// missing the instance has no relay, and FromEnv returns Logged with a
// loud warning rather than an error — an unconfigured standalone
// instance is the expected common case, not a misconfiguration
// (vitogo's rule). These arrive as ordinary env vars, which on the
// platform means `carlos env` / sealed secrets.
func FromEnv(prefix string, logger *slog.Logger) Sender {
	if logger == nil {
		logger = slog.Default()
	}
	host := strings.TrimSpace(os.Getenv(prefix + "_SMTP_HOST"))
	from := strings.TrimSpace(os.Getenv(prefix + "_SMTP_FROM"))
	if host == "" || from == "" {
		logger.Warn("rastrillo/mail: SMTP not configured — outbound email will be logged, not sent",
			"want_env", prefix+"_SMTP_HOST and "+prefix+"_SMTP_FROM")
		return Logged(logger)
	}
	return SMTP(host,
		strings.TrimSpace(os.Getenv(prefix+"_SMTP_PORT")),
		from,
		os.Getenv(prefix+"_SMTP_USER"),
		os.Getenv(prefix+"_SMTP_PASS"))
}

// message builds a minimal RFC 5322 plain-text email. Address and
// subject values pass through the injection check: a bare CR or LF in a
// header value is header injection's entire attack surface (it starts a
// new header line, or the blank line that starts the body). The three
// source implementations silently stripped the bytes; refusing loudly
// is stricter and surfaces the caller bug instead of mailing a
// mangled header.
func message(from, to, subject, body string) ([]byte, error) {
	for _, v := range []string{from, to, subject} {
		if strings.ContainsAny(v, "\r\n") {
			return nil, fmt.Errorf("rastrillo/mail: header value %q contains a line break", v)
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", to)
	fmt.Fprintf(&b, "Subject: %s\r\n", subject)
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("\r\n")
	b.WriteString(body)
	return []byte(b.String()), nil
}
